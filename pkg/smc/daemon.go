package smc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

// ControlKind selects what a Control asks the read loop to do.
type ControlKind int

const (
	// ControlSelectReader watches one reader instead of every attached one. An
	// empty Reader name means all of them, which is how the loop starts.
	ControlSelectReader ControlKind = iota
	// ControlRefreshReaders re-lists the attached readers.
	ControlRefreshReaders
	// ControlReadNow reads the card that is sitting in the reader again.
	ControlReadNow
	// ControlReportStatus re-publishes the status without changing anything, so
	// a client that connects late learns the current state instead of waiting
	// for the next change.
	ControlReportStatus
)

// Control is a request the read loop acts on while it is waiting for a card.
//
// It is a card logic type rather than a wire type: pkg/server carries
// model.Command and the host translates, so pkg/smc does not depend on the
// server.
type Control struct {
	Kind     ControlKind
	Reader   string
	Complete *ControlResult
}

// DaemonConfig is what a read loop runs with.
type DaemonConfig struct {
	// Broadcast receives every event the loop produces. Nil is allowed and
	// simply means a host that does not want the events.
	Broadcast chan model.Message
	// Options is what each read covers. Nil means the defaults.
	Options *OptionsStore
	// Control carries requests the loop acts on while it waits. Nil means no
	// client can steer it.
	Control <-chan Control
	// Reader names the reader to watch from the start, which is where a
	// configured [card] reader lands. Empty watches every attached one. A
	// name that is not attached falls back to watching everything on the
	// first resolve, exactly like a select request naming a missing reader.
	Reader string
	// Selection overrides Reader and is checked between bounded transport waits.
	Selection *ReaderStore
}

// errNoCard is the answer to a read request when nothing is inserted.
// ErrReadBusy lets clients distinguish a rejected overlapping read from a hardware failure.
var ErrReadBusy = errors.New("a card read is already in progress")

var errNoCard = errors.New("no card is inserted")

// daemon is the read loop's state.
//
// It is a type because the loop has to answer a client while it is blocked
// waiting for a card: which readers are attached, which one it is watching, and
// whether asking for another read makes sense. All of that belongs to the loop,
// and the loop is also the only place that can act on a request.
//
// Requests are handled by returning to the top of the loop rather than by
// cancelling a wait that is already in flight. The backend status call takes no
// context, so a wait cannot be interrupted cleanly, and abandoning one would
// leave two calls writing the same reader state. Making the wait bounded is
// what makes the loop steerable at all.
type daemon struct {
	card            *SmartCard
	store           *OptionsStore
	broadcast       chan model.Message
	control         <-chan Control
	ctx             context.Context
	readerSelection *ReaderStore
	requested       string

	readers  []string
	selected string
	state    string
	health   string

	// pendingRead is a read request that arrived with no card in the reader.
	// It is answered with an error rather than dropped, so the client that
	// pressed the button learns why nothing happened.
	pendingRead     bool
	pendingComplete *ControlResult

	// held is the session left open after a read. It stays open until the
	// removal is observed, as it always has. A re-read has to close it first,
	// because PC/SC sessions are exclusive and one card cannot be read twice at
	// once.
	held transport.Card
}

// StartDaemonWith reads cards as they arrive until ctx is done, and answers
// Control requests in between. It is what the agent uses to serve its control
// channel; a host with a fixed configuration can keep using StartDaemonCtx.
func (s *SmartCard) StartDaemonWith(ctx context.Context, cfg DaemonConfig) error {
	if s.transport == nil {
		return errors.New("no transport configured")
	}
	store := cfg.Options
	if store == nil {
		store = NewOptionsStore(*defaultOptions())
	}

	reader := cfg.Reader
	if cfg.Selection != nil {
		reader = cfg.Selection.Get()
	}
	d := &daemon{
		card:            s,
		store:           store,
		broadcast:       cfg.Broadcast,
		control:         cfg.Control,
		selected:        reader,
		requested:       reader,
		readerSelection: cfg.Selection,
		ctx:             ctx,
		state:           model.StateWaiting,
		health:          "starting",
	}
	return d.run(ctx)
}

func (d *daemon) run(ctx context.Context) error {
	// However the loop ends — cancelled, a failed wait, the process stopping —
	// the held session is closed here. A card session left open by an exiting
	// process outlives the process in the PC/SC broker's bookkeeping on macOS:
	// the reader then reports the card locked exclusively, and every later
	// insert fails until the card is removed or the broker is restarted.
	defer d.release()
	defer func() { d.completeRead(errors.New("reader loop stopped")) }()

	d.broadcastStatus()

	if err := d.awaitReaders(ctx); err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		reader, err := d.waitPresent(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			return err
		}

		d.publish(model.Message{
			Event:   "smc-inserted",
			Payload: map[string]string{"message": "Connected to " + reader},
		})

		if !d.read(ctx, reader) {
			// The card was taken out while the read was still fighting for the
			// session. The removal has been observed already, so waiting for
			// it again would block: the backend reports a change from the last
			// observed state, and that change has been consumed.
			d.release()
			d.setState(model.StateWaiting)
			d.publish(model.Message{
				Event:   "smc-removed",
				Payload: map[string]string{"message": "Disonnected from " + reader},
			})
			continue
		}

		if err := d.waitRemoval(ctx, reader); err != nil {
			if ctx.Err() != nil {
				return err
			}
			return err
		}

		d.release()
		d.setState(model.StateWaiting)
		d.publish(model.Message{
			Event:   "smc-removed",
			Payload: map[string]string{"message": "Disonnected from " + reader},
		})
	}
}

// waitPresent blocks until a card is in one of the watched readers and returns
// that reader's name.
//
// An idle poll window and a client request both bring it back to the top, which
// is what lets the loop notice a reader plugged in after it started and a
// client asking for a different one.
func (d *daemon) waitPresent(ctx context.Context) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		if d.pendingRead {
			// Asked to read again with nothing inserted. Say so instead of
			// waiting: the client is waiting for an answer too.
			d.pendingRead = false
			d.publishError(errNoCard.Error())
			d.completeRead(errNoCard)
			continue
		}

		watch := d.watchList()
		if len(watch) == 0 {
			if err := d.awaitReaders(ctx); err != nil {
				return "", err
			}
			continue
		}

		index, err := d.card.transport.WaitCardPresent(ctx, watch)
		switch {
		case err == nil:
			if index < 0 || index >= len(watch) {
				// No usable index is not a lost card, it is a backend that
				// reported nothing useful. Go round rather than guess.
				continue
			}
			return watch[index], nil
		case errors.Is(err, transport.ErrCardTimeout):
			// An idle window. This is also where a reader attached after the
			// loop started is picked up, without anyone asking for it.
			d.resolveReaders()
			d.drainControl()
			continue
		default:
			if ctx.Err() != nil {
				return "", err
			}
			log.Printf("waiting card error %s\n", err.Error())
			return "", err
		}
	}
}

// waitRemoval blocks until the card in reader is taken out.
//
// It also serves the read request. A card that stays in the reader can then be
// read again without being taken out and put back, which is the case an
// operator hits when a card is seated badly or a kiosk slot grips it.
//
// After a re-read it goes back to waiting for the removal rather than for a
// card, because the card a present-wait would find is the one already sitting
// there. Returning to the outer loop instead would read the same card over and
// over.
func (d *daemon) waitRemoval(ctx context.Context, reader string) error {
	// Only the reader that was actually read from. An empty reader answers
	// "empty" on the first poll, so passing the whole list reports a removal
	// that never happened: the client is told the card is gone, the session is
	// closed, and the loop then sits waiting for a card that is still in.
	watch := []string{reader}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if _, err := d.card.transport.WaitCardRemove(ctx, watch); err != nil {
			if errors.Is(err, transport.ErrCardTimeout) {
				d.drainControl()
				if d.pendingRead {
					d.pendingRead = false
					if !d.read(ctx, reader) {
						// The card left during the re-read's busy retry. The
						// removal is already observed, which is all this wait was
						// for.
						return nil
					}
				}
				continue
			}
			if ctx.Err() != nil {
				return err
			}
			return fmt.Errorf("waiting card removal: %w", err)
		}
		return nil
	}
}

// read reads the card in reader and publishes the result. It reports whether
// the card is still seated, which is whether run still has a removal left to
// wait for.
//
// One options snapshot per read, so a change made while this card is being read
// applies to the next one rather than halfway through this one.
//
// A read whose connect lost the exclusive-access race is retried here for as
// long as the card stays seated. The holder — on macOS, CryptoTokenKit's probe
// of PKI cards — does let go after a few seconds, and asking the operator to
// pull the card and put it back is the one thing the loop must not do: from
// the reader's point of view nothing is wrong with the card at all. Other
// failures are not retried; they publish the error and wait for the removal
// or a re-read request as before, because a card that fails the same way on
// every attempt would otherwise be hammered forever.
func (d *daemon) read(ctx context.Context, reader string) bool {
	d.release()
	d.setState(model.StateReading)

	card, data, err := d.card.readCard(reader, d.store.Get())
	if card != nil {
		// Held open until the removal is observed, or until a re-read needs it.
		d.held = card
	}

	notified := false
	for errors.Is(err, transport.ErrCardBusy) {
		if !notified {
			notified = true
			d.health = "reader-busy"
			d.broadcastStatus()
			d.publishError(fmt.Sprintf("%s; retrying while the card stays inserted", err))
		}
		switch d.busyHold(ctx, reader) {
		case busyRetry:
			// Still seated after the window: another full read.
		case busyGone:
			d.completeRead(errNoCard)
			// The card left while the holder still owned it, and the watch
			// consumed that state change. Reporting gone lets run skip the
			// removal wait, which would block forever on a change that has
			// already been seen.
			return false
		default:
			d.completeRead(fmt.Errorf("card watch failed"))
			// The watch broke rather than the card leaving. Claim the card is
			// still seated and let the removal wait surface the failure, the
			// way it does for every other transport error.
			return true
		}
		// Each retry opens its own session, so nothing may be held from the
		// last one.
		d.release()
		card, data, err = d.card.readCard(reader, d.store.Get())
		if card != nil {
			d.held = card
		}
	}

	if err != nil {
		d.health = "read-failed"
		d.publishError(err.Error())
	} else if data != nil {
		d.health = "ready"
		data.Reader = reader
		d.publish(model.Message{Event: "smc-data", Payload: data})
	}

	d.completeRead(err)
	d.setState(model.StateCardPresent)
	return true
}

// busyWait is what a hold between busy retries concluded.
type busyWait int

const (
	// busyRetry means the card is still seated: try the read again.
	busyRetry busyWait = iota
	// busyGone means the card was taken out during the hold.
	busyGone
	// busyBroken means the watch itself failed and said nothing about the
	// card.
	busyBroken
)

// busyHold waits out one poll window between busy retries, and reports what it
// saw.
//
// The wait is a removal watch rather than a sleep, so a card pulled out while
// the holder still owned it ends the retrying instead of sending a read into
// an empty reader. It also keeps answering control requests, which is what
// makes a hold that lasts as long as the operator leaves the card in
// harmless.
func (d *daemon) busyHold(ctx context.Context, reader string) busyWait {
	if _, err := d.card.transport.WaitCardRemove(ctx, []string{reader}); err != nil {
		if !errors.Is(err, transport.ErrCardTimeout) {
			if ctx.Err() == nil {
				log.Printf("watching the busy card: %s", err.Error())
			}
			return busyBroken
		}
		// Still seated after the window: worth another connect.
	} else {
		return busyGone
	}

	d.drainControl()
	// A re-read request is what this loop is already doing.
	d.pendingRead = false
	if ctx.Err() != nil {
		return busyBroken
	}
	return busyRetry
}

// drainControl applies everything a client has queued, without blocking.
//
// Only safe from the loop itself and only when no transport call is in flight,
// which is exactly what the bounded wait guarantees.
func (d *daemon) drainControl() {
	d.syncSelection()
	for {
		select {
		case cmd, ok := <-d.control:
			if !ok {
				d.control = nil
				return
			}
			d.applyControl(cmd)
		default:
			return
		}
	}
}

func (d *daemon) applyControl(cmd Control) {
	switch cmd.Kind {
	case ControlSelectReader:
		d.selected = cmd.Reader
		// Re-list first, so a client cannot pin the loop to a name that is not
		// attached and leave it waiting for a reader that will never answer.
		d.resolveReaders()
		log.Printf("watching reader %s", d.selection())
		d.broadcastStatus()
	case ControlRefreshReaders:
		ok := d.resolveReaders()
		d.broadcastStatus()
		if cmd.Complete != nil {
			if ok {
				cmd.Complete.Finish(nil)
			} else {
				cmd.Complete.Finish(d.problem())
			}
		}
	case ControlReadNow:
		if d.pendingRead || d.state == model.StateReading {
			if cmd.Complete != nil {
				cmd.Complete.Finish(ErrReadBusy)
			}
		} else if len(d.watchList()) == 0 {
			d.publishError(errNoCard.Error())
			if cmd.Complete != nil {
				cmd.Complete.Finish(ErrNoReaders)
			}
		} else {
			d.pendingRead = true
			d.pendingComplete = cmd.Complete
		}
	case ControlReportStatus:
		d.broadcastStatus()
		if cmd.Complete != nil {
			cmd.Complete.Finish(nil)
		}
	default:
		log.Printf("ignoring unknown control kind %d", cmd.Kind)
	}
}

// awaitReaders waits until at least one reader is attached.
func (d *daemon) awaitReaders(ctx context.Context) error {
	for {
		d.syncSelection()
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.resolveReaders() {
			return nil
		}

		d.publishError(d.problem().Error())
		log.Println("Cannot find a smart card reader, Wait 2 seconds")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case cmd, ok := <-d.control:
			if !ok {
				d.control = nil
				continue
			}
			// A refresh while nothing is attached is a reasonable thing to
			// ask for, so answer it rather than making the client wait.
			d.applyControl(cmd)
		case <-time.After(readerRetryInterval):
		}
	}
}

// resolveReaders re-lists the attached readers and keeps the loop in step.
//
// It runs on every idle window as well as on request, so a reader plugged in
// after the agent started appears without a restart. It reports whether the
// loop now has at least one reader.
func (d *daemon) resolveReaders() bool {
	list, err := d.card.transport.ListReaders()
	if err != nil || len(list) == 0 {
		health := "no-reader"
		if err != nil {
			health = "pcsc-unavailable"
		}
		changed := len(d.readers) > 0 || d.health != health
		d.readers = nil
		d.health = health
		if changed {
			d.broadcastStatus()
		}
		return false
	}
	if d.health == "starting" || d.health == "no-reader" || d.health == "pcsc-unavailable" {
		d.health = "ready"
	}

	changed := !sameStrings(list, d.readers)
	if changed {
		log.Printf("Available %d readers:", len(list))
		for i, reader := range list {
			log.Printf("[%d] %s\n", i, reader)
		}
		d.readers = list
	}

	// A selection that is not attached cannot be waited on. This is checked on
	// every resolve, not only when the list changed: a client can name a reader
	// that never existed, and waiting on it would stall the loop for good.
	if d.selected != "" && !containsString(list, d.selected) {
		log.Printf("selected reader %q is not attached, watching all readers", d.selected)
		d.selected = ""
		changed = true
	}

	if changed {
		d.broadcastStatus()
	}
	return len(d.readers) > 0
}

// problem is the error to report while no reader is attached.
func (d *daemon) problem() error {
	if _, err := d.card.transport.ListReaders(); err != nil {
		return err
	}
	return ErrNoReaders
}

// watchList is the readers to wait on: the selected one, or every attached one.
func (d *daemon) watchList() []string {
	if d.selected != "" && containsString(d.readers, d.selected) {
		return []string{d.selected}
	}
	return d.readers
}

func (d *daemon) selection() string {
	if d.selected == "" {
		return "all readers"
	}
	return d.selected
}

// release closes the session held open after a read.
func (d *daemon) release() {
	if d.held == nil {
		return
	}
	if err := d.held.Disconnect(); err != nil {
		log.Printf("Error disconnecting card: %v", err)
	}
	d.held = nil
}

// setState records what the loop is doing and says so once per change.
//
// The log belongs here rather than next to the transport call: the waits are
// bounded now, so the loop asks again every poll window, and logging there
// would write a line every couple of seconds for as long as the agent runs.
func (d *daemon) setState(state string) {
	if d.state == state {
		return
	}
	d.state = state
	if state == model.StateWaiting && (d.health == "reader-busy" || d.health == "read-failed") {
		d.health = "ready"
	}
	switch state {
	case model.StateWaiting:
		log.Println("Waiting for a Card Inserted")
	case model.StateReading:
		log.Println("Reading a Card")
	case model.StateCardPresent:
		log.Println("Waiting for a Card Removed")
	}
	d.broadcastStatus()
}

func (d *daemon) broadcastStatus() {
	payload := model.Status{
		Readers:  append([]string(nil), d.readers...),
		Selected: d.selected,
		State:    d.state,
		Health:   d.health,
	}
	d.publish(model.Message{Event: "smc-status", Payload: payload})
}

func (d *daemon) publishError(message string) {
	d.publish(model.Message{
		Event:   "smc-error",
		Payload: map[string]string{"message": message},
	})
}

// publish sends one event, if the host wants events at all. The loop blocks
// on it, so a host that does not drain the channel stalls the loop.
func (d *daemon) publish(msg model.Message) {
	if d.broadcast == nil {
		return
	}
	var done <-chan struct{}
	if d.ctx != nil {
		done = d.ctx.Done()
	}
	select {
	case d.broadcast <- msg:
	case <-done:
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func (d *daemon) syncSelection() {
	if d.readerSelection == nil {
		return
	}
	requested := d.readerSelection.Get()
	if requested != d.requested {
		d.requested = requested
		d.applyControl(Control{Kind: ControlSelectReader, Reader: requested})
	}
}

func (d *daemon) completeRead(err error) {
	if d.pendingComplete != nil {
		f := d.pendingComplete
		d.pendingComplete = nil
		f.Finish(err)
	}
}

// ControlResult lets a host observe completion without binding card logic to a transport.
type ControlResult struct{ Finish func(error) }

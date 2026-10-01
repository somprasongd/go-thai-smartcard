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
	Kind   ControlKind
	Reader string
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
}

// errNoCard is the answer to a read request when nothing is inserted.
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
	card      *SmartCard
	store     *OptionsStore
	broadcast chan model.Message
	control   <-chan Control

	readers  []string
	selected string
	state    string

	// pendingRead is a read request that arrived with no card in the reader.
	// It is answered with an error rather than dropped, so the client that
	// pressed the button learns why nothing happened.
	pendingRead bool

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

	d := &daemon{
		card:      s,
		store:     store,
		broadcast: cfg.Broadcast,
		control:   cfg.Control,
		selected:  cfg.Reader,
		state:     model.StateWaiting,
	}
	return d.run(ctx)
}

func (d *daemon) run(ctx context.Context) error {
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

		d.read(reader)

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
					d.read(reader)
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

// read reads the card in reader and publishes the result.
//
// One options snapshot per read, so a change made while this card is being read
// applies to the next one rather than halfway through this one.
func (d *daemon) read(reader string) {
	d.release()
	d.setState(model.StateReading)

	card, data, err := d.card.readCard(reader, d.store.Get())
	if card != nil {
		// Held open until the removal is observed, or until a re-read needs it.
		d.held = card
	}

	if err != nil {
		d.publishError(err.Error())
	} else if data != nil {
		data.Reader = reader
		d.publish(model.Message{Event: "smc-data", Payload: data})
	}

	d.setState(model.StateCardPresent)
}

// drainControl applies everything a client has queued, without blocking.
//
// Only safe from the loop itself and only when no transport call is in flight,
// which is exactly what the bounded wait guarantees.
func (d *daemon) drainControl() {
	for {
		select {
		case cmd := <-d.control:
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
		d.resolveReaders()
		d.broadcastStatus()
	case ControlReadNow:
		d.pendingRead = true
	case ControlReportStatus:
		d.broadcastStatus()
	default:
		log.Printf("ignoring unknown control kind %d", cmd.Kind)
	}
}

// awaitReaders waits until at least one reader is attached.
func (d *daemon) awaitReaders(ctx context.Context) error {
	for {
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
		case cmd := <-d.control:
			// A refresh while nothing is attached is a reasonable thing to
			// ask for, so answer it rather than making the client wait.
			if cmd.Kind == ControlRefreshReaders || cmd.Kind == ControlSelectReader {
				d.applyControl(cmd)
			}
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
		return false
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
	d.broadcast <- msg
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

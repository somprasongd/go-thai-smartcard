package smc_test

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

// scriptedTransport models what PC/SC actually does, which a plain fake cannot:
// a reader the backend has never observed answers immediately with its current
// state, an observed reader only answers when its state changes, and the scan
// stops at the first match, leaving the readers after it unobserved.
//
// That last part is the whole point of the removal test below. A reader that is
// still unobserved reports "empty" on the very next poll, so passing a reader
// that was never read from to WaitCardRemove looks like a card being taken out.
type scriptedTransport struct {
	mu      sync.Mutex
	readers []string
	present map[string]bool
	armed   map[string]bool
	// connects counts sessions, so a test can assert a re-read reopened one.
	connects int
	// disconnects counts closed sessions, so a test can assert the daemon
	// lets go of the card when it stops.
	disconnects int
	// busyConnects makes the next Connect calls fail with a sharing
	// violation, which is what the retry has to ride out.
	busyConnects int
	// pause keeps an idle daemon from spinning while a test decides what to do.
	pause time.Duration
}

func newScriptedTransport(pause time.Duration, readers ...string) *scriptedTransport {
	t := &scriptedTransport{
		readers: readers,
		present: map[string]bool{},
		armed:   map[string]bool{},
		pause:   pause,
	}
	return t
}

func (t *scriptedTransport) setPresent(reader string, present bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.present[reader] = present
}

func (t *scriptedTransport) addReader(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.readers = append(t.readers, name)
}

func (t *scriptedTransport) ListReaders() ([]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.readers...), nil
}

func (t *scriptedTransport) Connect(string) (transport.Card, error) {
	t.mu.Lock()
	t.connects++
	busy := t.busyConnects > 0
	if busy {
		t.busyConnects--
	}
	t.mu.Unlock()
	if busy {
		// What a PC/SC backend reports when another handle holds the card:
		// errors.Is(err, transport.ErrCardBusy) must hold for the daemon's
		// retry to fire.
		return nil, fmt.Errorf("connect to %q: %w: scard: Sharing violation", "test reader", transport.ErrCardBusy)
	}
	// A fresh session every time, replaying from the start, so the same card can
	// be read more than once.
	return &scriptedCard{trace: personalTrace(false), transport: t}, nil
}

func (t *scriptedTransport) poll(readers []string, want bool) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, name := range readers {
		observed, wasArmed := t.armed[name]
		current := t.present[name]
		if !wasArmed {
			t.armed[name] = current
			if current == want {
				return i, nil
			}
			continue
		}
		if observed != current {
			t.armed[name] = current
			if current == want {
				return i, nil
			}
		}
	}
	return -1, transport.ErrCardTimeout
}

func (t *scriptedTransport) WaitCardPresent(_ context.Context, readers []string) (int, error) {
	index, err := t.poll(readers, true)
	if err != nil {
		time.Sleep(t.pause)
	}
	return index, err
}

func (t *scriptedTransport) WaitCardRemove(_ context.Context, readers []string) (int, error) {
	index, err := t.poll(readers, false)
	if err != nil {
		time.Sleep(t.pause)
	}
	return index, err
}

func (t *scriptedTransport) Close() error { return nil }

type scriptedCard struct {
	trace     *transport.Trace
	pos       int
	transport *scriptedTransport
}

func (c *scriptedCard) Status() (transport.Status, error) {
	atr, err := c.trace.ATR()
	return transport.Status{Atr: atr}, err
}

func (c *scriptedCard) Transmit(cmd []byte) ([]byte, error) {
	if c.pos >= len(c.trace.Exchanges) {
		return nil, errors.New("scripted: trace exhausted")
	}
	want := c.trace.Exchanges[c.pos]
	if got := hex.EncodeToString(cmd); got != want.Command {
		return nil, errors.New("scripted: apdu mismatch")
	}
	c.pos++
	return hex.DecodeString(want.Response)
}

func (c *scriptedCard) Disconnect() error {
	c.transport.mu.Lock()
	c.transport.disconnects++
	c.transport.mu.Unlock()
	return nil
}

// harness runs a daemon against a scripted transport and collects its events.
type harness struct {
	t          *testing.T
	transport  *scriptedTransport
	control    chan smc.Control
	cancel     context.CancelFunc
	done       chan error
	mu         sync.Mutex
	events     []model.Message
	statuses   []model.Status
	dataEvents []*model.Data

	// stopGuard keeps stop idempotent: a test may stop the daemon itself,
	// and t.Cleanup stops it again afterwards.
	stopOnce sync.Once
}

func newHarness(t *testing.T, pause time.Duration, readers ...string) *harness {
	t.Helper()
	return newHarnessWithReader(t, pause, "", readers...)
}

// newHarnessWithReader is newHarness with the daemon seeded to watch one
// reader from the start, which is what a configured [card] reader produces.
func newHarnessWithReader(t *testing.T, pause time.Duration, reader string, readers ...string) *harness {
	t.Helper()

	tr := newScriptedTransport(pause, readers...)
	card := smc.NewSmartCardWith(tr)

	h := &harness{
		t:         t,
		transport: tr,
		control:   make(chan smc.Control, 8),
		done:      make(chan error, 1),
	}

	broadcast := make(chan model.Message, 256)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel

	go func() {
		h.done <- card.StartDaemonWith(ctx, smc.DaemonConfig{
			Broadcast: broadcast,
			Options:   smc.NewOptionsStore(smc.Options{}),
			Control:   h.control,
			Reader:    reader,
		})
	}()

	// The daemon blocks on every event it publishes, so something has to keep
	// draining for the whole test.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-broadcast:
				h.mu.Lock()
				h.events = append(h.events, msg)
				switch payload := msg.Payload.(type) {
				case model.Status:
					h.statuses = append(h.statuses, payload)
				case *model.Data:
					h.dataEvents = append(h.dataEvents, payload)
				}
				h.mu.Unlock()
			}
		}
	}()

	t.Cleanup(h.stop)
	return h
}

func (h *harness) stop() {
	h.stopOnce.Do(func() {
		h.cancel()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			h.t.Error("daemon did not stop")
		}
	})
}

func (h *harness) counts() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]int{}
	for _, msg := range h.events {
		out[msg.Event]++
	}
	return out
}

func (h *harness) reads() []*model.Data {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*model.Data(nil), h.dataEvents...)
}

func (h *harness) lastStatus() (model.Status, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.statuses) == 0 {
		return model.Status{}, false
	}
	return h.statuses[len(h.statuses)-1], true
}

// errorMessages is every smc-error text seen so far. Callers run on the test
// goroutine while the collector appends, so the lock matters here.
func (h *harness) errorMessages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, msg := range h.events {
		if msg.Event != "smc-error" {
			continue
		}
		if payload, ok := msg.Payload.(map[string]string); ok {
			out = append(out, payload["message"])
		}
	}
	return out
}

// settle waits for a condition to hold, so tests assert on a settled daemon
// rather than on whatever happened to be true when they looked.
func (h *harness) settle(what string, check func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s; events seen: %v", what, h.counts())
}

func (h *harness) settleReads(n int) []*model.Data {
	h.t.Helper()
	h.settle("reads", func() bool { return len(h.reads()) >= n })
	return h.reads()
}

// The bug this guards: with two readers, one of them empty, asking
// WaitCardRemove about the whole list reported the empty reader as a removal
// while the card was still sitting in the other one. The client was told the
// card was gone, the session was closed, and the loop then waited for a card
// that was already inserted.
func TestDaemonDoesNotAnnounceARemovalThatDidNotHappen(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A", "Reader B")
	h.transport.setPresent("Reader A", true)

	h.settleReads(1)

	// Well past several poll windows with the card never taken out.
	time.Sleep(150 * time.Millisecond)

	if got := h.counts()["smc-removed"]; got != 0 {
		t.Errorf("announced %d removal(s) while the card was still inserted", got)
	}
	if got := len(h.reads()); got != 1 {
		t.Errorf("read the same inserted card %d times, want 1", got)
	}
}

// Taking the card out is still reported, once.
func TestDaemonAnnouncesTheRealRemoval(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")
	h.transport.setPresent("Reader A", true)

	h.settleReads(1)
	h.transport.setPresent("Reader A", false)

	h.settle("the removal event", func() bool { return h.counts()["smc-removed"] == 1 })
}

// The point of read-now: a card that stays in the reader can be read again
// without being taken out and put back.
func TestDaemonReadNowReadsACardThatStaysInserted(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")
	h.transport.setPresent("Reader A", true)

	h.settleReads(1)

	h.control <- smc.Control{Kind: smc.ControlReadNow}

	reads := h.settleReads(2)
	if len(reads) != 2 {
		t.Fatalf("got %d reads, want the original and one re-read", len(reads))
	}
	if reads[0].Reader != "Reader A" {
		t.Errorf("first read Reader = %q, want %q", reads[0].Reader, "Reader A")
	}
	if reads[1].Reader != "Reader A" {
		t.Errorf("second read Reader = %q, want %q", reads[1].Reader, "Reader A")
	}
	if got := h.counts()["smc-removed"]; got != 0 {
		t.Errorf("a re-read reported %d removal(s)", got)
	}

	// A re-read needs a fresh session, because PC/SC sessions are exclusive.
	h.transport.mu.Lock()
	connects := h.transport.connects
	h.transport.mu.Unlock()
	if connects < 2 {
		t.Errorf("connects = %d, want a new session for the re-read", connects)
	}
}

// A read request with nothing inserted has to be answered, not swallowed: the
// client that pressed the button is waiting too.
func TestDaemonReadNowWithNoCardAnswersWithAnError(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")

	h.control <- smc.Control{Kind: smc.ControlReadNow}

	h.settle("an error for the empty reader", func() bool {
		for _, message := range h.errorMessages() {
			if message == "no card is inserted" {
				return true
			}
		}
		return false
	})
	if got := len(h.reads()); got != 0 {
		t.Errorf("read %d cards with nothing inserted", got)
	}
}

// The status a client steers by: which readers exist, which one is watched, and
// whether the daemon is in a state where another read makes sense.
func TestDaemonStatusFollowsTheCard(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")

	h.settle("the waiting status", func() bool {
		status, ok := h.lastStatus()
		return ok && status.State == model.StateWaiting
	})

	status, _ := h.lastStatus()
	if len(status.Readers) != 1 || status.Readers[0] != "Reader A" {
		t.Errorf("Readers = %v, want [Reader A]", status.Readers)
	}
	if status.Selected != "" {
		t.Errorf("Selected = %q, want empty for all readers", status.Selected)
	}

	h.transport.setPresent("Reader A", true)
	h.settleReads(1)

	h.settle("the card-present status", func() bool {
		status, ok := h.lastStatus()
		return ok && status.State == model.StateCardPresent
	})
}

// Selecting a reader narrows the wait to it, and a card in a reader that is not
// selected is left alone.
func TestDaemonSelectingAReaderNarrowsTheWatch(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A", "Reader B")

	// Select first, then seat the card: a selection can only narrow the wait
	// from the next window on, so a card already in when the daemon starts is
	// read before any client has had a chance to choose.
	h.control <- smc.Control{Kind: smc.ControlSelectReader, Reader: "Reader A"}
	h.settle("the selection status", func() bool {
		status, ok := h.lastStatus()
		return ok && status.Selected == "Reader A"
	})

	h.transport.setPresent("Reader B", true)

	// Reader B has a card but is not the selected one.
	time.Sleep(100 * time.Millisecond)
	if got := len(h.reads()); got != 0 {
		t.Fatalf("read %d cards from an unselected reader", got)
	}

	// Selecting B picks up the card that is already sitting in it, without
	// anyone touching it.
	h.control <- smc.Control{Kind: smc.ControlSelectReader, Reader: "Reader B"}
	h.settleReads(1)
}

// A reader attached after the agent started shows up on its own, which is what
// makes the refresh button a convenience rather than a requirement.
func TestDaemonPicksUpAReaderAttachedLater(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")

	h.settle("the first reader", func() bool {
		status, ok := h.lastStatus()
		return ok && len(status.Readers) == 1
	})

	h.transport.addReader("Reader B")
	h.transport.setPresent("Reader B", true)

	h.settleReads(1)
	h.settle("both readers listed", func() bool {
		status, ok := h.lastStatus()
		return ok && len(status.Readers) == 2
	})
}

// A selection naming a reader that is not attached cannot be waited on, so it
// falls back to watching everything rather than stalling.
func TestDaemonDropsASelectionThatIsGone(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")

	h.control <- smc.Control{Kind: smc.ControlSelectReader, Reader: "Gone Reader"}
	h.settle("the fallback status", func() bool {
		status, ok := h.lastStatus()
		return ok && status.Selected == ""
	})

	h.transport.setPresent("Reader A", true)
	h.settleReads(1)
}

// A configured [card] reader narrows the watch from the first resolve, the
// same way a set-reader request would.
func TestDaemonStartsWatchingAConfiguredReader(t *testing.T) {
	h := newHarnessWithReader(t, time.Millisecond, "Reader B", "Reader A", "Reader B")
	h.transport.setPresent("Reader B", true)

	h.settleReads(1)

	status, ok := h.lastStatus()
	if !ok {
		t.Fatal("no status was broadcast")
	}
	if status.Selected != "Reader B" {
		t.Errorf("selected = %q, want the configured reader", status.Selected)
	}
}

// A configured reader that is not attached falls back to watching everything
// on the first resolve, rather than waiting for a reader that will never
// answer.
func TestDaemonFallsBackWhenTheConfiguredReaderIsMissing(t *testing.T) {
	h := newHarnessWithReader(t, time.Millisecond, "Reader Z", "Reader A")
	h.transport.setPresent("Reader A", true)

	h.settleReads(1)

	status, ok := h.lastStatus()
	if !ok {
		t.Fatal("no status was broadcast")
	}
	if status.Selected != "" {
		t.Errorf("selected = %q, want empty (watching all) after the configured reader was not attached", status.Selected)
	}
}

// A connect that loses the exclusive-access race must not fail the read: the
// daemon rides out a sharing violation with retries and reads the card once
// the reader frees up.
func TestDaemonRetriesAConnectThatLostTheExclusiveRace(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")
	h.transport.mu.Lock()
	h.transport.busyConnects = 2
	h.transport.mu.Unlock()
	h.transport.setPresent("Reader A", true)

	reads := h.settleReads(1)
	if len(reads) == 0 {
		t.Fatal("the read never happened")
	}
	h.transport.mu.Lock()
	connects := h.transport.connects
	h.transport.mu.Unlock()
	if connects < 3 {
		t.Errorf("connect was called %d times, want at least 3 (two busy rejections plus the read)", connects)
	}
}

// A holder that never lets go is not a reason to stop serving the reader: the
// busy error is published once, the loop keeps retrying while the card stays
// seated, and it is the removal — not a retry count — that ends it.
func TestDaemonKeepsRetryingWhileTheHolderStaysBusy(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")
	h.transport.mu.Lock()
	h.transport.busyConnects = 999
	h.transport.mu.Unlock()
	h.transport.setPresent("Reader A", true)

	h.settle("a busy error", func() bool {
		for _, msg := range h.errorMessages() {
			if strings.Contains(msg, "sharing violation") {
				return true
			}
		}
		return false
	})
	if got := len(h.reads()); got != 0 {
		t.Errorf("read %d card(s) that were never connected to", got)
	}

	h.transport.setPresent("Reader A", false)
	h.settle("the removal that ends the retrying", func() bool {
		return h.counts()["smc-removed"] == 1
	})
	h.settle("the waiting status", func() bool {
		status, ok := h.lastStatus()
		return ok && status.State == model.StateWaiting
	})
}

// A daemon that stops while holding a session has to close it. A session left
// open by an exiting process outlives the process in the PC/SC broker — on
// macOS the reader then reports the card locked exclusively, and every later
// insert fails until the card is removed or the broker restarts.
func TestDaemonShutdownClosesTheHeldSession(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")
	h.transport.setPresent("Reader A", true)

	h.settleReads(1)

	h.transport.mu.Lock()
	if h.transport.disconnects != 0 {
		h.transport.mu.Unlock()
		t.Fatal("the held session was closed while the card was still seated and served")
	}
	h.transport.mu.Unlock()

	// The card stays seated; only the daemon goes away.
	h.stop()

	h.transport.mu.Lock()
	defer h.transport.mu.Unlock()
	if h.transport.disconnects == 0 {
		t.Error("the daemon stopped holding the card and never disconnected the session")
	}
}

// The common macOS case: CryptoTokenKit seizes an inserted PKI card and lets
// go a few seconds later. The read must come out without anyone touching the
// reader, with one notice about the busy card — not one per retry — and with
// no removal invented on the way.
func TestDaemonReadsACardOnceTheHolderReleasesIt(t *testing.T) {
	h := newHarness(t, time.Millisecond, "Reader A")
	h.transport.mu.Lock()
	// More busy rejections than the connect burst makes attempts, so only the
	// daemon-level retry can produce the read.
	h.transport.busyConnects = 13
	h.transport.mu.Unlock()
	h.transport.setPresent("Reader A", true)

	reads := h.settleReads(1)
	if reads[0].Reader != "Reader A" {
		t.Errorf("read Reader = %q, want %q", reads[0].Reader, "Reader A")
	}
	if got := h.counts()["smc-removed"]; got != 0 {
		t.Errorf("a busy retry reported %d removal(s)", got)
	}

	busyErrors := 0
	for _, msg := range h.errorMessages() {
		if strings.Contains(msg, "sharing violation") {
			busyErrors++
			if !strings.Contains(msg, "retrying while the card stays inserted") {
				t.Errorf("busy error %q does not say the read is being retried", msg)
			}
		}
	}
	if busyErrors != 1 {
		t.Errorf("published %d busy error(s), want exactly one notice", busyErrors)
	}

	h.settle("the card-present status", func() bool {
		status, ok := h.lastStatus()
		return ok && status.State == model.StateCardPresent
	})
}

package transport

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/somprasongd/go-thai-smartcard/internal/atomicfile"
)

// Exchange is a single command/response APDU pair, stored as hex so that a
// trace stays readable and diffable in a text editor.
type Exchange struct {
	Command  string `json:"command"`
	Response string `json:"response"`
}

// Trace is a recorded card session.
//
// Record a trace once against a real reader with cmd/record, then replay it
// with FakeCard to exercise the card logic without hardware.
type Trace struct {
	Name      string     `json:"name"`
	Note      string     `json:"note,omitempty"`
	Atr       string     `json:"atr"`
	Exchanges []Exchange `json:"exchanges"`
}

// LoadTrace reads a trace from disk.
func LoadTrace(path string) (*Trace, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read trace: %w", err)
	}
	t := &Trace{}
	if err := json.Unmarshal(b, t); err != nil {
		return nil, fmt.Errorf("parse trace %s: %w", path, err)
	}
	return t, nil
}

// Save writes the trace to disk, creating parent directories as needed.
func (t *Trace) Save(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create trace dir: %w", err)
		}
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trace: %w", err)
	}
	b = append(b, '\n')
	if err := atomicfile.Write(path, b, 0o600); err != nil {
		return fmt.Errorf("write trace: %w", err)
	}
	return nil
}

// ATR returns the recorded answer to reset.
func (t *Trace) ATR() ([]byte, error) {
	atr, err := hex.DecodeString(t.Atr)
	if err != nil {
		return nil, fmt.Errorf("decode atr %q: %w", t.Atr, err)
	}
	return atr, nil
}

// RecordingCard wraps a Card and appends every successful exchange to a Trace.
//
// Use it to capture ground truth from a real card: run the reader through the
// same code path a real session takes, keep the resulting trace private and outside Git.
type RecordingCard struct {
	Card
	trace *Trace
	mu    sync.Mutex
}

var _ Card = (*RecordingCard)(nil)

// Record wraps card so that its exchanges accumulate into a new trace.
func Record(card Card, name string) *RecordingCard {
	return &RecordingCard{
		Card:  card,
		trace: &Trace{Name: name},
	}
}

// Trace returns the trace collected so far. The ATR is filled in from the
// first Status call, because that is the only place it is available.
func (c *RecordingCard) Trace() *Trace {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.trace
}

// Status records the ATR and then delegates.
func (c *RecordingCard) Status() (Status, error) {
	st, err := c.Card.Status()
	if err != nil {
		return st, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.trace.Atr == "" {
		c.trace.Atr = hex.EncodeToString(st.Atr)
	}
	return st, nil
}

// Transmit delegates and records the exchange.
func (c *RecordingCard) Transmit(cmd []byte) ([]byte, error) {
	rsp, err := c.Card.Transmit(cmd)
	if err != nil {
		return rsp, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trace.Exchanges = append(c.trace.Exchanges, Exchange{
		Command:  hex.EncodeToString(cmd),
		Response: hex.EncodeToString(rsp),
	})
	return rsp, nil
}

// FakeCard replays a Trace in recorded order.
//
// Replay is strict: each command must match the recorded command at the same
// position, so a change in the APDU sequence shows up as a test failure rather
// than as silently different data.
type FakeCard struct {
	trace *Trace
	atr   []byte
	pos   int

	mu           sync.Mutex
	disconnected bool
}

var _ Card = (*FakeCard)(nil)

// NewFakeCard builds a replaying card from a trace.
func NewFakeCard(t *Trace) (*FakeCard, error) {
	atr, err := t.ATR()
	if err != nil {
		return nil, err
	}
	return &FakeCard{trace: t, atr: atr}, nil
}

// Status reports the recorded ATR.
func (c *FakeCard) Status() (Status, error) {
	return Status{Atr: c.atr}, nil
}

// Transmit returns the recorded response for the next exchange.
func (c *FakeCard) Transmit(cmd []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pos >= len(c.trace.Exchanges) {
		return nil, fmt.Errorf("fake card: no recorded response for command %s (exchange %d of %d)",
			hex.EncodeToString(cmd), c.pos, len(c.trace.Exchanges))
	}

	want := c.trace.Exchanges[c.pos]
	got := hex.EncodeToString(cmd)
	if got != want.Command {
		return nil, fmt.Errorf("fake card: exchange %d mismatch: got command %s, trace has %s",
			c.pos, got, want.Command)
	}
	c.pos++

	rsp, err := hex.DecodeString(want.Response)
	if err != nil {
		return nil, fmt.Errorf("fake card: decode response for exchange %d: %w", c.pos, err)
	}
	return rsp, nil
}

// Disconnect marks the card as released.
func (c *FakeCard) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.disconnected = true
	return nil
}

// Disconnected reports whether Disconnect has been called, so tests can assert
// that the caller released the card.
func (c *FakeCard) Disconnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disconnected
}

// ExchangesUsed reports how many recorded exchanges have been consumed.
func (c *FakeCard) ExchangesUsed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pos
}

// FakeTransport serves a single FakeCard and reports the reader it is attached
// to as always present, which is enough to drive a full read through SmartCard
// without hardware.
//
// A nil card models a reader with nothing in it: the waits then report
// ErrCardTimeout rather than a card, so a test can exercise the daemon's idle
// path and its reader list without an empty reader answering immediately.
type FakeTransport struct {
	readers []string
	card    Card
	closed  bool

	mu           sync.Mutex
	presentIndex int
}

var _ Transport = (*FakeTransport)(nil)

// NewFakeTransport returns a transport backed by card.
func NewFakeTransport(readers []string, card Card) *FakeTransport {
	return &FakeTransport{readers: readers, card: card}
}

// ListReaders returns the fixed reader list.
func (t *FakeTransport) ListReaders() ([]string, error) {
	return t.readers, nil
}

// Connect returns the fake card.
func (t *FakeTransport) Connect(string) (Card, error) {
	return t.card, nil
}

// WaitCardPresent reports the configured reader as present immediately, or
// ErrCardTimeout when the transport was built without a card.
func (t *FakeTransport) WaitCardPresent(ctx context.Context, _ []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	if t.card == nil {
		return -1, ErrCardTimeout
	}
	return t.presentIndex, nil
}

// WaitCardRemove returns immediately; tests drive removal explicitly.
func (t *FakeTransport) WaitCardRemove(ctx context.Context, _ []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	if t.card == nil {
		return -1, ErrCardTimeout
	}
	return t.presentIndex, nil
}

// Close marks the transport as closed.
func (t *FakeTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

// Closed reports whether Close has been called.
func (t *FakeTransport) Closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

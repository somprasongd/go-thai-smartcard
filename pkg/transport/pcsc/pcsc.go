//go:build !js

// Package pcsc implements transport.Transport on top of the PC/SC API.
//
// This is the only package in the module that depends on ebfe/scard, which is
// a cgo binding. The build constraint keeps it out of js/wasm builds, where
// cgo does not exist.
package pcsc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

// statusChangeTimeout bounds a single SCardGetStatusChange call.
//
// It exists so that context cancellation is noticed within a bounded time.
// It does not delay card detection: the OS wakes the call as soon as the card
// state changes, so insertion and removal are still observed immediately.
const statusChangeTimeout = 2 * time.Second

type pcscTransport struct {
	ctx *scard.Context

	// readers and states are kept across Wait calls on purpose. Change
	// detection is relative to CurrentState, so discarding them per call
	// would make GetStatusChange return straight away and spin.
	readers []string
	states  []scard.ReaderState
}

var _ transport.Transport = (*pcscTransport)(nil)

// New establishes a PC/SC context and returns a transport using it.
func New() (transport.Transport, error) {
	ctx, err := scard.EstablishContext()
	if err != nil {
		return nil, fmt.Errorf("establish pc/sc context: %w", err)
	}
	return &pcscTransport{ctx: ctx}, nil
}

// ListReaders returns the attached readers.
func (t *pcscTransport) ListReaders() ([]string, error) {
	readers, err := t.ctx.ListReaders()
	if err != nil {
		return nil, fmt.Errorf("list readers: %w", err)
	}
	return readers, nil
}

// Connect opens an exclusive session with the card in reader.
func (t *pcscTransport) Connect(reader string) (transport.Card, error) {
	card, err := t.ctx.Connect(reader, scard.ShareExclusive, scard.ProtocolAny)
	if err != nil {
		return nil, fmt.Errorf("connect to %q: %w", reader, err)
	}
	return &pcscCard{card: card}, nil
}

// WaitCardPresent blocks until a card is present.
func (t *pcscTransport) WaitCardPresent(ctx context.Context, readers []string) (int, error) {
	t.ensureStates(readers)
	return t.wait(ctx, scard.StatePresent)
}

// WaitCardRemove blocks until the card is gone.
func (t *pcscTransport) WaitCardRemove(ctx context.Context, readers []string) (int, error) {
	t.ensureStates(readers)
	return t.wait(ctx, scard.StateEmpty)
}

// Close releases the PC/SC context.
func (t *pcscTransport) Close() error {
	t.ctx.Release()
	return nil
}

// ensureStates initialises reader states, keeping them when the reader set is
// unchanged.
func (t *pcscTransport) ensureStates(readers []string) {
	if t.states != nil && sameReaders(t.readers, readers) {
		return
	}
	t.readers = append([]string(nil), readers...)
	t.states = make([]scard.ReaderState, len(readers))
	for i, name := range readers {
		t.states[i].Reader = name
		t.states[i].CurrentState = scard.StateUnaware
	}
}

// wait returns the index of the first reader reporting want, or
// transport.ErrCardTimeout when the poll window expires with no change.
//
// It used to loop here on timeout, so the daemon only ever saw a card event.
// That left it unable to notice a client asking for a different reader or for
// another read, and unable to notice a reader plugged in after it started.
// Handing the timeout back puts that decision where it belongs. Calling again
// with the same reader list resumes from the observed state, not a re-arm.
func (t *pcscTransport) wait(ctx context.Context, want scard.StateFlag) (int, error) {
	for {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		if err := t.ctx.GetStatusChange(t.states, statusChangeTimeout); err != nil {
			// The window expiring with no card change is the normal "nothing
			// happened yet" case, reported as such rather than swallowed.
			if isTimeout(err) {
				return -1, transport.ErrCardTimeout
			}
			return -1, fmt.Errorf("get status change: %w", err)
		}
		for i := range t.states {
			t.states[i].CurrentState = t.states[i].EventState
			if t.states[i].EventState&want != 0 {
				return i, nil
			}
		}
	}
}

// isTimeout reports whether err is PC/SC's SCARD_E_TIMEOUT, which
// GetStatusChange returns when the timeout elapses before any reader state
// changes.
func isTimeout(err error) bool {
	return errors.Is(err, scard.ErrTimeout)
}

func sameReaders(a, b []string) bool {
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

type pcscCard struct {
	card *scard.Card
}

var _ transport.Card = (*pcscCard)(nil)

func (c *pcscCard) Status() (transport.Status, error) {
	st, err := c.card.Status()
	if err != nil {
		return transport.Status{}, fmt.Errorf("card status: %w", err)
	}
	return transport.Status{Atr: st.Atr}, nil
}

func (c *pcscCard) Transmit(cmd []byte) ([]byte, error) {
	rsp, err := c.card.Transmit(cmd)
	if err != nil {
		return nil, fmt.Errorf("transmit %x: %w", cmd, err)
	}
	return rsp, nil
}

func (c *pcscCard) Disconnect() error {
	if err := c.card.Disconnect(scard.UnpowerCard); err != nil {
		return fmt.Errorf("disconnect: %w", err)
	}
	return nil
}

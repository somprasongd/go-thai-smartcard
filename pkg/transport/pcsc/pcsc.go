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

// explain annotates a PC/SC error that came back as
// SCARD_W_SECURITY_VIOLATION. pcsc-lite enables polkit by default upstream
// (since late 2023) and its default denies any process without an active
// local session — exactly what a system service is. Root would pass for free,
// but the service runs as a dedicated user (decision 17), so the refusal is
// expected until the packaged polkit rule grants it, and the error says so
// instead of failing generically.
func explain(err error) error {
	if !errors.Is(err, scard.ErrSecurityViolation) {
		return err
	}
	return fmt.Errorf("%w (SCARD_W_SECURITY_VIOLATION): pcscd's polkit policy denies a system service by default. Install the rule packaged at /etc/polkit-1/rules.d/50-thai-smartcard.pcscd.rules, which grants the service user org.debian.pcsc-lite.access_pcsc and org.debian.pcsc-lite.access_card, then restart pcscd", err)
}

// New establishes a PC/SC context and returns a transport using it.
func New() (transport.Transport, error) {
	ctx, err := scard.EstablishContext()
	if err != nil {
		return nil, fmt.Errorf("establish pc/sc context: %w", explain(err))
	}
	return &pcscTransport{ctx: ctx}, nil
}

// ListReaders returns the attached readers.
func (t *pcscTransport) ListReaders() ([]string, error) {
	readers, err := t.ctx.ListReaders()
	if err != nil {
		return nil, fmt.Errorf("list readers: %w", explain(err))
	}
	return readers, nil
}

// Connect opens a session with the card in reader and locks it against every
// other handle for the session's lifetime.
//
// The lock does not come from SCARD_SHARE_EXCLUSIVE. An exclusive connect is
// refused the moment any other handle exists, and on macOS CryptoTokenKit
// parks a shared handle on every inserted PKI card for as long as the card
// stays seated — an exclusive connect sits in sharing violations forever, no
// matter how often it is retried, and the only reads that ever got through
// were the ones that beat the probe to a fresh insert. A shared connect
// coexists with that parked handle, and the transaction opened right after
// keeps the other side off the wire anyway: while the transaction is open,
// another handle's transmits fail, which is the guarantee the exclusive
// session was chosen for.
func (t *pcscTransport) Connect(reader string) (transport.Card, error) {
	card, err := t.ctx.Connect(reader, scard.ShareShared, scard.ProtocolAny)
	if err != nil {
		// A sharing violation means another handle holds the card in a way
		// even a shared connect cannot cross (an exclusive holder); callers
		// can recognise it as transient and retry the connect (see
		// transport.ErrCardBusy).
		if errors.Is(err, scard.ErrSharingViolation) {
			return nil, fmt.Errorf("connect to %q: %w: %v%s", reader, transport.ErrCardBusy, err, t.heldExclusiveNote(reader))
		}
		return nil, fmt.Errorf("connect to %q: %w", reader, explain(err))
	}
	if err := card.BeginTransaction(); err != nil {
		// The handle is ours but the wire is not, which is the same shape of
		// transient as the connect refusal above — someone else is mid-
		// transaction, and they let go eventually.
		_ = card.Disconnect(scard.LeaveCard)
		if errors.Is(err, scard.ErrSharingViolation) {
			return nil, fmt.Errorf("begin transaction on %q: %w: %v%s", reader, transport.ErrCardBusy, err, t.heldExclusiveNote(reader))
		}
		return nil, fmt.Errorf("begin transaction on %q: %w", reader, explain(err))
	}
	return &pcscCard{card: card}, nil
}

// heldExclusiveNote describes a card the reader reports locked exclusively —
// the one sharing violation no amount of retrying can beat, because PC/SC
// offers no way to take a card back from another handle. Empty when the lock
// is the transient kind a later attempt can win.
//
// The check is a stateless UNAWARE status query: it answers immediately and
// touches none of the state the removal watches track.
func (t *pcscTransport) heldExclusiveNote(reader string) string {
	states := []scard.ReaderState{{Reader: reader, CurrentState: scard.StateUnaware}}
	if err := t.ctx.GetStatusChange(states, statusChangeTimeout); err != nil {
		return ""
	}
	if states[0].EventState&scard.StateExclusive == 0 {
		return ""
	}
	return " (the reader reports the card locked exclusively by another process, and no retry can take it back — remove the card and insert it again)"
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
			return -1, fmt.Errorf("get status change: %w", explain(err))
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
	// End the transaction Connect opened, because the disconnect is refused
	// while one is active. "No transaction active" is the state this call is
	// moving out of anyway, so its error is not worth reporting.
	_ = c.card.EndTransaction(scard.LeaveCard)
	if err := c.card.Disconnect(scard.UnpowerCard); err != nil {
		// An invalid handle or a removed card means the broker has already
		// ended this session on its own — the card was pulled, or the reader
		// let the handle die while the card sat idle. Releasing is the goal,
		// and it is already met, so neither is an error worth reporting.
		if errors.Is(err, scard.ErrInvalidHandle) || errors.Is(err, scard.ErrRemovedCard) {
			return nil
		}
		return fmt.Errorf("disconnect: %w", err)
	}
	return nil
}

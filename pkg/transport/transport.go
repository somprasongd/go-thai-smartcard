// Package transport defines the narrow interface that the Thai ID card
// reading logic talks to.
//
// The point of the interface is that PC/SC becomes one implementation among
// several instead of a hard dependency of pkg/smc. A CCID transport driven over
// WebUSB, or a remote transport proxying to another host, can be dropped in
// without touching the card logic.
package transport

import (
	"context"
	"errors"
)

// ErrCardTimeout means the wait ended because its poll window expired with no
// card change, not because a card arrived or left.
//
// The waits are deliberately bounded rather than blocking forever. The caller
// has to stay in charge: it is the layer that can answer a client asking which
// reader to watch or for another read, and a wait that never returns is a wait
// that never hears the answer. Callers should treat this as "nothing happened
// yet" and call again.
//
// This does not make card detection slower. The backend wakes the underlying
// status call as soon as a card changes; the window only bounds the idle case.
var ErrCardTimeout = errors.New("transport: card wait timed out")

// ErrCardBusy means a Connect failed because another handle holds the card
// exclusively (the PC/SC sharing violation). It is transient: the holder may
// be the previous session of this very transport, released a moment before a
// fast re-insert, or another process that seizes inserted cards — on macOS,
// CryptoTokenKit's smart card service does that to PKI cards. Callers may
// retry the connect after a short wait; backends report it by wrapping this
// sentinel so a plain errors.Is check works without leaking backend types.
var ErrCardBusy = errors.New("card busy: another handle holds it exclusively (sharing violation)")

// Status is the subset of card status the library actually uses.
type Status struct {
	Atr []byte
}

// Card is a card in an open session.
//
// Implementations only need to report status, exchange raw APDUs and release
// the card. They must not leak backend specific types.
type Card interface {
	// Status returns the current status, including the answer to reset.
	Status() (Status, error)

	// Transmit sends a command APDU and returns the response APDU.
	Transmit(cmd []byte) ([]byte, error)

	// Disconnect ends the session with the card.
	Disconnect() error
}

// Transport provides access to smart card readers.
//
// Implementations must not expose their backend types through this interface.
// Callers pass plain reader names so that a non PC/SC backend can satisfy it
// unchanged.
type Transport interface {
	// ListReaders returns the names of the readers currently attached.
	ListReaders() ([]string, error)

	// Connect opens a session with the card in reader and locks the card
	// against every other handle for the session's lifetime — through
	// whatever mechanism the backend has that does not require being the
	// only handle, since other processes legitimately park handles on
	// inserted cards (on macOS, CryptoTokenKit does that to PKI cards).
	//
	// It fails with ErrCardBusy when the lock cannot be taken because
	// another process is actively using the card; callers may retry after a
	// short wait. Backends report it by wrapping this sentinel so a plain
	// errors.Is check works without leaking backend types.
	Connect(reader string) (Card, error)

	// WaitCardPresent returns the index in readers of a reader that has a card,
	// or ErrCardTimeout when its poll window expires with no change. It returns
	// early when ctx is done.
	//
	// Implementations must return once the window expires rather than looping
	// internally: the caller decides what to do between windows, and a wait that
	// never returns can never act on a client request.
	//
	// Implementations must also carry reader state across repeated calls with
	// the same reader list: card change detection is normally relative to the
	// previously observed state, so re-initialising it on every call turns the
	// wait into a busy loop.
	WaitCardPresent(ctx context.Context, readers []string) (int, error)

	// WaitCardRemove returns the index in readers of a reader that has become
	// empty, or ErrCardTimeout when its poll window expires with no change. It
	// returns early when ctx is done.
	//
	// Callers should pass only the readers they actually read from. A reader
	// that was already empty answers "empty" immediately, so including one
	// reports a removal that never happened.
	WaitCardRemove(ctx context.Context, readers []string) (int, error)

	// Close releases the resources held by the transport.
	Close() error
}

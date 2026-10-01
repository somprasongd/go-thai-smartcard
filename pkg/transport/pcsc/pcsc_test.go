package pcsc

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ebfe/scard"
)

// GetStatusChange returns SCARD_E_TIMEOUT when the poll window elapses with no
// reader state change, which is the normal "nothing happened yet" case. If that
// is treated as a failure the wait ends while the reader is still idle, so the
// daemon exits and the agent restarts it in a loop instead of waiting for a
// card.
func TestIsTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "bare timeout",
			err:  scard.ErrTimeout,
			want: true,
		},
		{
			name: "timeout wrapped by an intermediate layer",
			err:  fmt.Errorf("get status change: %w", scard.ErrTimeout),
			want: true,
		},
		{
			name: "unknown reader is a real failure",
			err:  scard.ErrUnknownReader,
			want: false,
		},
		{
			name: "invalid handle is a real failure",
			err:  scard.ErrInvalidHandle,
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTimeout(tt.err); got != tt.want {
				t.Errorf("isTimeout(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// Reader states are kept across Wait calls so that change detection stays
// relative to the previous observation. Re-initialising them on every call
// would turn the wait into a busy loop, so ensureStates only rebuilds when the
// reader list actually changed.
func TestEnsureStatesKeepsStateForUnchangedReaders(t *testing.T) {
	tr := &pcscTransport{}
	readers := []string{"Reader A", "Reader B"}

	tr.ensureStates(readers)
	first := tr.states
	if len(first) != 2 {
		t.Fatalf("got %d states, want 2", len(first))
	}
	// Pretend a card was observed, so the states are no longer unaware.
	first[0].CurrentState = scard.StatePresent

	tr.ensureStates(readers)
	if &tr.states[0] != &first[0] {
		t.Error("ensureStates rebuilt the states for an unchanged reader list")
	}
	if tr.states[0].CurrentState != scard.StatePresent {
		t.Error("ensureStates discarded the observed card state")
	}

	tr.ensureStates([]string{"Reader A", "Reader C"})
	if tr.states[0].CurrentState == scard.StatePresent {
		t.Error("ensureStates should reset the states when the reader list changes")
	}
}

func TestSameReaders(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{name: "identical", a: []string{"a", "b"}, b: []string{"a", "b"}, want: true},
		{name: "both empty", a: nil, b: []string{}, want: true},
		{name: "different length", a: []string{"a"}, b: []string{"a", "b"}, want: false},
		{name: "different name", a: []string{"a", "b"}, b: []string{"a", "c"}, want: false},
		{name: "different order", a: []string{"a", "b"}, b: []string{"b", "a"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameReaders(tt.a, tt.b); got != tt.want {
				t.Errorf("sameReaders(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

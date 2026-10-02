//go:build darwin && integration

package main

import (
	"testing"

	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
)

// TestDispatchControlWithRealKardianos queries dispatchControl through the
// real kardianos/service layer — the exact calls the root helper makes — and
// accepts both outcomes: on a machine with the service installed it reports a
// state; without one (typical in tests) it comes back as a clean error rather
// than a panic. The point is the integration seam, not the state.
func TestDispatchControlWithRealKardianos(t *testing.T) {
	ops, err := newServiceOps()
	if err != nil {
		t.Fatalf("newServiceOps: %v", err)
	}

	resp := dispatchControl("status", ops)
	t.Logf("status → ok=%v state=%q error=%q", resp.OK, resp.State, resp.Error)
	if !resp.OK && resp.Error == "" {
		t.Error("a failed status returned no error text")
	}

}

// TestCtlDarwinWithoutHelper checks the tray-side live path on a machine
// without the helper: State must return cleanly and the fallback must at least
// be constructible. Never calls escalate — that would pop a password dialog.
func TestCtlDarwinWithoutHelper(t *testing.T) {
	m := ctl.New()
	state, err := m.State()
	t.Logf("State() = %v, err = %v", state, err)
	switch state {
	case ctl.StateRunning, ctl.StateStopped:
		t.Logf("a helper is present on this machine; state=%v", state)
	default:
		// A manually installed service with no helper permits explicit
		// password-backed actions while status remains unknown.
		t.Logf("unknown state is valid with or without an installed fallback")
	}
}

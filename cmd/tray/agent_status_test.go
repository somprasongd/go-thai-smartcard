package main

import (
	"errors"
	"testing"

	"fyne.io/systray"
	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
)

func TestAgentIndicatorReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, connection string
		state            ctl.State
		err              error
		shade            string
	}{
		{"foreground despite stopped service", "connected", ctl.StateStopped, nil, "green"},
		{"foreground without installed service", "connected", ctl.StateUnknown, errors.New("missing"), "green"},
		{"running but unreachable", "down", ctl.StateRunning, nil, "amber"},
		{"stopped", "down", ctl.StateStopped, nil, "red"},
		{"unauthorized", "unauthorized", ctl.StateRunning, nil, "amber"},
		{"websocket disabled", "unsupported", ctl.StateRunning, nil, "amber"},
		{"unknown", "down", ctl.StateUnknown, nil, "gray"},
		{"query failed", "down", ctl.StateStopped, errors.New("denied"), "gray"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shade, title := agentIndicator(systemLanguage(), tc.connection, tc.state, tc.err)
			if shade != tc.shade || title == "" {
				t.Fatalf("got %s %q", shade, title)
			}
		})
	}
}

func TestToggleUsesCurrentServiceState(t *testing.T) {
	m := &testService{state: ctl.StateRunning}
	action, err := toggleAction(m)
	if err != nil || action != "stop" {
		t.Fatalf("pause: %q %v", action, err)
	}
	if err := controlService(m, action); err != nil {
		t.Fatal(err)
	}
	action, err = toggleAction(m)
	if err != nil || action != "start" {
		t.Fatalf("resume: %q %v", action, err)
	}
	if err := controlService(m, action); err != nil {
		t.Fatal(err)
	}
	if m.starts != 1 || m.stops != 1 {
		t.Fatalf("starts %d stops %d", m.starts, m.stops)
	}
	m.state = ctl.StateUnknown
	if _, err := toggleAction(m); err == nil {
		t.Fatal("unknown state must not toggle")
	}
	m.queryErr = errors.New("denied")
	if _, err := toggleAction(m); err == nil {
		t.Fatal("denied state must not toggle")
	}
}

func TestServiceToggleAvailability(t *testing.T) {
	tr := &tray{mAgent: &systray.MenuItem{}, mAgentToggle: &systray.MenuItem{}, mAgentRestart: &systray.MenuItem{}}
	state := ctl.StateRunning
	tr.serviceState = func() (ctl.State, error) { return state, nil }
	tr.refreshServiceState()
	if tr.mAgentToggle.Disabled() || tr.mAgentRestart.Disabled() {
		t.Fatal("running service controls disabled")
	}
	state = ctl.StateStopped
	tr.refreshServiceState()
	if tr.mAgentToggle.Disabled() || !tr.mAgentRestart.Disabled() {
		t.Fatal("stopped service must allow Resume only")
	}
	state = ctl.StateUnknown
	tr.refreshServiceState()
	if !tr.mAgentToggle.Disabled() {
		t.Fatal("unknown service must not toggle")
	}
}

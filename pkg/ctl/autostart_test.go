package ctl

import (
	"errors"
	"testing"
)

type startupManager struct {
	state              State
	queryErr, startErr error
	starts             int
	concurrentStart    bool
}

func (m *startupManager) State() (State, error) { return m.state, m.queryErr }
func (m *startupManager) Start() error {
	m.starts++
	if m.startErr == nil || m.concurrentStart {
		m.state = StateRunning
	}
	return m.startErr
}
func (*startupManager) Stop() error    { return nil }
func (*startupManager) Restart() error { return nil }

func TestEnsureRunning(t *testing.T) {
	failure := errors.New("denied")
	for _, tt := range []struct {
		name               string
		state              State
		queryErr, startErr error
		concurrent         bool
		wantStarts         int
		wantErr            error
	}{
		{"running", StateRunning, nil, nil, false, 0, nil},
		{"stopped", StateStopped, nil, nil, false, 1, nil},
		{"transition or unknown", StateUnknown, nil, nil, false, 0, nil},
		{"status denied", StateUnknown, failure, nil, false, 0, failure},
		{"start denied", StateStopped, nil, failure, false, 1, failure},
		{"boot won race", StateStopped, nil, failure, true, 1, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &startupManager{state: tt.state, queryErr: tt.queryErr, startErr: tt.startErr, concurrentStart: tt.concurrent}
			err := EnsureRunning(m)
			if !errors.Is(err, tt.wantErr) || m.starts != tt.wantStarts {
				t.Fatalf("err=%v starts=%d", err, m.starts)
			}
			if err == nil {
				if err := EnsureRunning(m); err != nil || m.starts != tt.wantStarts {
					t.Fatalf("second startup: %v, starts=%d", err, m.starts)
				}
			}
		})
	}
}

type noninteractiveManager struct {
	startupManager
	automatic int
}

func (m *noninteractiveManager) startAutomatic() error {
	m.automatic++
	return errors.New("helper unavailable")
}
func TestEnsureRunningDoesNotUseInteractiveFallback(t *testing.T) {
	m := &noninteractiveManager{startupManager: startupManager{state: StateStopped}}
	if EnsureRunning(m) == nil || m.automatic != 1 || m.starts != 0 {
		t.Fatal("automatic startup used the interactive fallback")
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
)

type testService struct {
	state                   ctl.State
	queryErr, actionErr     error
	starts, stops, restarts int
	leaveRunning            bool
}

func (m *testService) State() (ctl.State, error) { return m.state, m.queryErr }
func (m *testService) Start() error {
	m.starts++
	if m.actionErr == nil {
		m.state = ctl.StateRunning
	}
	return m.actionErr
}
func (m *testService) Stop() error {
	m.stops++
	if m.actionErr == nil && !m.leaveRunning {
		m.state = ctl.StateStopped
	}
	return m.actionErr
}
func (m *testService) Restart() error { m.restarts++; return m.actionErr }

func TestTrayStartup(t *testing.T) {
	for _, tt := range []struct {
		name           string
		state          ctl.State
		override       string
		manual, cancel bool
		want           int
	}{
		{"stopped", ctl.StateStopped, "", false, false, 1},
		{"running", ctl.StateRunning, "", false, false, 0},
		{"pending", ctl.StateUnknown, "", false, false, 0},
		{"explicit URL", ctl.StateStopped, "http://127.0.0.1:9900", false, false, 0},
		{"manual control won", ctl.StateStopped, "", true, false, 0},
		{"tray closing", ctl.StateStopped, "", false, true, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &testService{state: tt.state}
			tr := &tray{manager: m, manualControl: tt.manual}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			tr.autoStart(ctx, &agentClient{override: tt.override})
			if m.starts != tt.want {
				t.Fatalf("starts=%d want %d", m.starts, tt.want)
			}
		})
	}
}

func TestStartupSkipsVerifiedForegroundAgent(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"instance_id": "foreground"})
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "endpoint.json")
	endpointFile(t, path, "foreground", s.URL, 1)
	m := &testService{state: ctl.StateStopped}
	tr := &tray{manager: m}
	client := newAgentClient("")
	client.userPath = path
	tr.autoStart(context.Background(), client)
	if m.starts != 0 {
		t.Fatal("started system service beside live foreground agent")
	}
	endpointFile(t, path, "stale", s.URL, 1)
	tr.autoStart(context.Background(), client)
	if m.starts != 1 {
		t.Fatal("stale metadata blocked startup")
	}
}

func TestControlService(t *testing.T) {
	denied := errors.New("denied")
	for _, tt := range []struct {
		name, action          string
		state                 ctl.State
		err                   error
		pending               bool
		wantStarts, wantStops int
		wantErr               bool
	}{
		{"already started", "start", ctl.StateRunning, nil, false, 0, 0, false},
		{"already stopped", "stop-quit", ctl.StateStopped, nil, false, 0, 0, false},
		{"stop and quit", "stop-quit", ctl.StateRunning, nil, false, 0, 1, false},
		{"stop denied keeps tray", "stop-quit", ctl.StateRunning, denied, false, 0, 1, true},
		{"stop pending keeps tray", "stop-quit", ctl.StateRunning, nil, true, 0, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &testService{state: tt.state, actionErr: tt.err, leaveRunning: tt.pending}
			err := controlService(m, tt.action)
			if (err != nil) != tt.wantErr || m.starts != tt.wantStarts || m.stops != tt.wantStops {
				t.Fatalf("err=%v starts=%d stops=%d", err, m.starts, m.stops)
			}
		})
	}
}

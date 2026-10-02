//go:build darwin

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
)

// fakeOps answers whatever the test scripted, without touching launchd.
type fakeOps struct {
	controlErr error
	status     string
	statusErr  error
}

func (f *fakeOps) Control(action string) error { return f.controlErr }
func (f *fakeOps) Status() (string, error)     { return f.status, f.statusErr }

// ask sends one request over a real socket to serveControl and returns the
// response — the same round trip the tray makes against the root helper.
func ask(t *testing.T, ln net.Listener, action string) ctl.Response {
	t.Helper()
	conn, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := json.NewEncoder(conn).Encode(ctl.Request{Action: action}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var resp ctl.Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestServeControl(t *testing.T) {
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "control.sock"))
	if err != nil {
		t.Fatal(err)
	}
	go serveControl(ln, &fakeOps{status: ctl.StateRunning.String()})

	tests := []struct {
		name      string
		action    string
		wantOK    bool
		wantState string
		wantErr   string
	}{
		{name: "status reports what the service layer said", action: "status", wantOK: true, wantState: "running"},
		{name: "start is forwarded", action: "start", wantOK: true},
		{name: "stop is forwarded", action: "stop", wantOK: true},
		{name: "restart is forwarded", action: "restart", wantOK: true},
		{name: "an unknown action is refused", action: "rm -rf", wantOK: false, wantErr: `unknown action "rm -rf"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := ask(t, ln, tt.action)
			if resp.OK != tt.wantOK {
				t.Errorf("ok = %v (error %q), want %v", resp.OK, resp.Error, tt.wantOK)
			}
			if resp.State != tt.wantState {
				t.Errorf("state = %q, want %q", resp.State, tt.wantState)
			}
			if tt.wantErr != "" && resp.Error != tt.wantErr {
				t.Errorf("error = %q, want %q", resp.Error, tt.wantErr)
			}
		})
	}
}

func TestDispatchControlForwardsErrors(t *testing.T) {
	ops := &fakeOps{
		controlErr: errors.New("launchd refused"),
		status:     ctl.StateUnknown.String(),
		statusErr:  errors.New("not installed"),
	}

	resp := dispatchControl("stop", ops)
	if resp.OK || resp.Error != "launchd refused" {
		t.Errorf("stop response = %+v, want the control error", resp)
	}

	resp = dispatchControl("status", ops)
	if resp.OK || resp.State != ctl.StateUnknown.String() || resp.Error != "not installed" {
		t.Errorf("status response = %+v, want the status error with an unknown state", resp)
	}
}

func TestRunControlHelperCreatesASocket(t *testing.T) {
	// Only the socket plumbing is exercised here: the real helper would
	// touch launchd, so the ops are faked. The socket lives in a short temp
	// dir — macOS caps unix socket paths at 104 bytes, and t.TempDir() names
	// are long enough to trip it.
	dir, err := os.MkdirTemp("", "smcctl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "control.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	go serveControl(ln, &fakeOps{status: "stopped"})

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("socket file: %v", err)
	}
	resp := ask(t, ln, "status")
	if !resp.OK || resp.State != "stopped" {
		t.Errorf("status response = %+v, want stopped", resp)
	}
}

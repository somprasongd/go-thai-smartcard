//go:build darwin

package ctl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The control path is the root helper the .pkg installs
// (/Library/LaunchDaemons/com.thaismartcard.control.plist running
// `thai-smartcard-agent control-helper`): a local unix socket, group admin,
// that forwards to kardianos/service in-process. When the helper is missing —
// a manual `service install` — the tray falls back to osascript, which
// prompts for an administrator password on every action.
type helper struct{}

// New returns the macOS control mechanism.
func New() Manager { return helper{} }

// SocketPath is where the control helper listens. Both the tray and the
// helper (cmd/agent control-helper) reference this one constant.
const SocketPath = "/var/run/thai-smartcard-control.sock"

const requests = 3 * time.Second

func send(action string) (*Response, error) {
	conn, err := net.DialTimeout("unix", SocketPath, requests)
	if err != nil {
		return nil, fmt.Errorf("control helper: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(requests))
	if err := json.NewEncoder(conn).Encode(Request{Action: action}); err != nil {
		return nil, err
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return nil, fmt.Errorf("control helper: %w", err)
	}
	if !resp.OK {
		return &resp, errors.New(resp.Error)
	}
	return &resp, nil
}

// available reports whether the helper is listening, so the tray can pick
// the fallback before showing a menu that would fail.
func (helper) available() bool {
	c, err := net.DialTimeout("unix", SocketPath, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func (helper) State() (State, error) {
	return helperState(send, "/Library/LaunchDaemons/"+serviceName+".plist")
}

func helperState(query func(string) (*Response, error), plist string) (State, error) {
	resp, err := query("status")
	if err != nil {
		if resp != nil {
			return StateUnknown, err
		}
		// Unknown state still permits the explicit password-backed actions.
		// Do not prompt while the tray polls status.
		if _, statErr := os.Stat(plist); statErr == nil {
			return StateUnknown, nil
		}
		return StateUnknown, err
	}
	switch resp.State {
	case "running":
		return StateRunning, nil
	case "stopped":
		return StateStopped, nil
	}
	return StateUnknown, fmt.Errorf("control helper: unknown state %q", resp.State)
}

// Automatic startup must not fall back to an administrator dialog.
func (helper) startAutomatic() error {
	_, err := send("start")
	return err
}

func (helper) Start() error {
	if _, err := send("start"); err != nil {
		return escalate("service start")
	}
	return nil
}

func (helper) Stop() error {
	if _, err := send("stop"); err != nil {
		return escalate("service stop")
	}
	return nil
}

func (helper) Restart() error {
	if _, err := send("restart"); err != nil {
		return escalate("service restart")
	}
	return nil
}

// escalate is the fallback for a machine without the helper: the same
// service command, elevated through the system's administrator dialog. It
// prompts on every action, which is why the helper is the primary path.
func escalate(args string) error {
	script := fmt.Sprintf(`do shell script "/usr/local/bin/thai-smartcard-agent %s" with administrator privileges`, args)
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("elevated %s: %s", args, strings.TrimSpace(string(out)))
	}
	return nil
}

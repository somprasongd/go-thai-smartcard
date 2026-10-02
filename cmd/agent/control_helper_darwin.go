//go:build darwin

// The tray's control helper: run as root by the packaged LaunchDaemon
// (com.thaismartcard.control), it listens on a local unix socket restricted
// to group admin and forwards start/stop/restart/status to kardianos/service
// in-process. That is the whole job — no config, no card, no network
// (docs/plan/tray-agent-control.md).

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kardianos/service"
	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
)

// serviceOps is what the helper is allowed to do to the agent service. It is
// an interface so the protocol can be tested without touching launchd.
type serviceOps interface {
	// Control runs one of start, stop, restart.
	Control(action string) error
	// Status reports running, stopped or unknown.
	Status() (string, error)
}

type kardianosOps struct {
	svc service.Service
}

func (k *kardianosOps) Control(action string) error { return service.Control(k.svc, action) }

func (k *kardianosOps) Status() (string, error) {
	st, err := k.svc.Status()
	if err != nil {
		return ctl.StateUnknown.String(), err
	}
	if st == service.StatusRunning {
		return ctl.StateRunning.String(), nil
	}
	if st == service.StatusStopped {
		return ctl.StateStopped.String(), nil
	}
	return ctl.StateUnknown.String(), nil
}

func newServiceOps() (serviceOps, error) {
	svc, err := newAgentService("")
	if err != nil {
		return nil, err
	}
	return &kardianosOps{svc: svc}, nil
}

// runControlHelper serves the control socket until it fails. Run by launchd
// as root; never by a user.
func runControlHelper() error {
	ops, err := newServiceOps()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ctl.SocketPath), 0o755); err != nil {
		return err
	}
	// A stale socket file from a crashed helper would fail Listen.
	os.Remove(ctl.SocketPath)
	ln, err := net.Listen("unix", ctl.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", ctl.SocketPath, err)
	}
	// The socket is the authorisation: group admin may connect, nobody
	// else. The helper runs as root, so the chown happens after the listen.
	if group, err := user.LookupGroup("admin"); err == nil {
		if gid, err := strconv.Atoi(group.Gid); err == nil {
			os.Chown(ctl.SocketPath, 0, gid)
		}
	}
	if err := os.Chmod(ctl.SocketPath, 0o660); err != nil {
		return err
	}
	log.Printf("control helper listening on %s", ctl.SocketPath)
	return serveControl(ln, ops)
}

// serveControl accepts one JSON request per connection and answers with one
// response. It is exported from runControlHelper only in shape: the loop is
// what tests drive over a temp socket.
func serveControl(ln net.Listener, ops serviceOps) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go handleControlConn(conn, ops)
	}
}

func handleControlConn(conn net.Conn, ops serviceOps) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	var req ctl.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	if err := json.NewEncoder(conn).Encode(dispatchControl(req.Action, ops)); err != nil {
		log.Printf("control helper: write response: %v", err)
	}
}

// dispatchControl answers exactly one request. Only the four known actions
// exist; anything else is refused.
func dispatchControl(action string, ops serviceOps) ctl.Response {
	switch action {
	case "status":
		state, err := ops.Status()
		if err != nil {
			return ctl.Response{OK: false, Error: err.Error(), State: state}
		}
		return ctl.Response{OK: true, State: state}
	case "start", "stop", "restart":
		if err := ops.Control(action); err != nil {
			return ctl.Response{OK: false, Error: err.Error()}
		}
		return ctl.Response{OK: true}
	}
	return ctl.Response{OK: false, Error: fmt.Sprintf("unknown action %q", action)}
}

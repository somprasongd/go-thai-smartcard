// Package ctl lets the tray start, stop and restart the agent's system
// service.
//
// The agent runs as root / SYSTEM / a dedicated system user (v3 plan,
// decision 12), so controlling it is a privileged operation and each OS
// authorises it differently: Linux goes through systemctl with a polkit rule
// shipped by the package, Windows through the Service Control Manager with a
// service ACL the installer sets, and macOS through a small root helper the
// package installs (see docs/plan/tray-agent-control.md). The tray never
// spawns an agent process of its own — everything here asks the OS service
// manager.
package ctl

import "errors"

// State is the service state as the OS reports it.
type State int

const (
	// StateUnknown means the state could not be determined: the control
	// mechanism is missing (no systemd, helper not installed) or failed.
	StateUnknown State = iota
	StateStopped
	StateRunning
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateRunning:
		return "running"
	}
	return "unknown"
}

// serviceName must match the kardianos/service name the agent registers
// under (cmd/agent service_desktop.go: serviceName).
const serviceName = "thai-smartcard-agent"

// ErrUnsupported means this platform has no working control mechanism.
var ErrUnsupported = errors.New("ctl: no control mechanism on this platform")

// Request is one command from the tray to the control helper.
type Request struct {
	Action string `json:"action"` // start, stop, restart, status
}

// Response is the helper's single reply. The helper is macOS-only, but the
// wire types live here so both ends share one definition.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	State string `json:"state,omitempty"` // running, stopped, unknown
}

// Manager controls the agent's system service.
type Manager interface {
	State() (State, error)
	Start() error
	Stop() error
	Restart() error
}

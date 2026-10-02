//go:build linux

package ctl

import (
	"fmt"
	"os/exec"
	"strings"
)

// The unit is authorised by the packaged polkit rule
// (49-thai-smartcard.agent.rules): an active local session may manage this
// one unit without a password. Anything else — no systemd, no polkit agent —
// fails and the tray falls back to the terminal hint.
type systemctl struct{}

// New returns the Linux control mechanism.
func New() Manager { return systemctl{} }

func run(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func (systemctl) State() (State, error) {
	out, err := exec.Command("systemctl", "is-active", serviceName+".service").Output()
	state, perr := parseIsActive(string(out), exitCodeOf(err), err)
	if perr != nil {
		return StateUnknown, fmt.Errorf("systemctl is-active: %w", perr)
	}
	return state, nil
}

func (systemctl) Start() error   { return run("start", serviceName+".service") }
func (systemctl) Stop() error    { return run("stop", serviceName+".service") }
func (systemctl) Restart() error { return run("restart", serviceName+".service") }

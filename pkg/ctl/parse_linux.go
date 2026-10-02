//go:build linux

package ctl

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// parseIsActive maps `systemctl is-active` output to a State. The command
// prints the state on stdout in every case and exits 0 when active, 3 when
// inactive or failed, and something else when systemctl itself is unusable
// (no systemd, no polkit bus) — in which case the state is unknown.
func parseIsActive(output string, exitCode int, err error) (State, error) {
	state := strings.TrimSpace(output)
	switch state {
	case "active":
		return StateRunning, err
	case "inactive", "failed":
		return StateStopped, nil
	}
	if err != nil {
		return StateUnknown, err
	}
	return StateUnknown, fmt.Errorf("unexpected systemctl is-active output %q", state)
}

// exitCodeOf unwraps the exit status of a failed exec, or -1 when the failure
// was not a process exit (missing binary, for example).
func exitCodeOf(err error) int {
	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

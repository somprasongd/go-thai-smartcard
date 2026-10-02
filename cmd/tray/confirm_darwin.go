//go:build darwin

package main

import (
	"fmt"
	"os/exec"
)

// confirm asks the operator before Stop runs, through the system dialog. It
// returns whether the action should proceed.
func confirm(title, message string) bool {
	script := fmt.Sprintf(
		`display dialog %q with title %q buttons {"Cancel", "Stop"} default button "Stop" cancel button "Cancel"`,
		message, title,
	)
	return exec.Command("osascript", "-e", script).Run() == nil
}

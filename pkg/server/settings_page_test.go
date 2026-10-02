package server

import (
	"os/exec"
	"testing"
)

// Execute the bundled page's actual script with a small DOM harness. This
// checks navigation and one-time token preservation without a reader/browser.
func TestSettingsPagePortNavigation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is needed for the settings script behavior check")
	}
	command := exec.Command(node, "web/settings_test.cjs")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("settings script: %v\n%s", err, output)
	}
}

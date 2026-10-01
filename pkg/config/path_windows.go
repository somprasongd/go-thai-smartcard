//go:build windows

package config

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

var goos = "windows"

// runningAsSystem reports whether this process can own the service config
// path. A service runs as SYSTEM, which is elevated; a plain user is not, so
// `agent run` in a user shell lands on the per-user path.
func runningAsSystem() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// servicePath is where a system service's config lives.
func servicePath() string {
	return filepath.Join(os.Getenv("ProgramData"), "ThaiSmartcard", "config.toml")
}

// userPath is where `agent run` in the foreground as a user keeps its config.
func userPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return "config.toml"
	}
	return filepath.Join(base, configDirName(), "config.toml")
}

//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"runtime"
)

var goos = runtime.GOOS

// runningAsSystem reports whether this process can own the service config
// path: on Unix, root can write /etc and /Library.
func runningAsSystem() bool {
	return os.Geteuid() == 0
}

// servicePath is where a system service's config lives. The agent runs as a
// service on every platform (decision 12), so this path is the primary one.
func servicePath() string {
	if goos == "darwin" {
		return "/Library/Application Support/ThaiSmartcard/config.toml"
	}
	return "/etc/thai-smartcard/config.toml"
}

// userPath is where `agent run` in the foreground as a user keeps its config.
func userPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			return "config.toml"
		}
		if goos == "darwin" {
			base = filepath.Join(home, "Library", "Application Support")
		} else {
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, configDirName(), "config.toml")
}

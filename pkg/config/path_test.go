package config

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	path := DefaultPath()
	if path == "" {
		t.Fatal("DefaultPath is empty")
	}
	if filepath.Base(path) != "config.toml" {
		t.Errorf("DefaultPath = %q, want it to end in config.toml", path)
	}
	if runtime.GOOS != "js" {
		if !filepath.IsAbs(path) {
			t.Errorf("DefaultPath = %q, want an absolute path", path)
		}
	}
}

func TestUserPathIsUnderTheUserConfigDir(t *testing.T) {
	// The per-user paths exist only for `agent run` in the foreground
	// (decision 12), and must never land in the service directories.
	p := userPath()
	switch runtime.GOOS {
	case "linux":
		if !strings.Contains(p, "thai-smartcard") {
			t.Errorf("userPath = %q, want it under thai-smartcard", p)
		}
	case "darwin":
		if !strings.Contains(p, filepath.Join("Library", "Application Support", "ThaiSmartcard")) {
			t.Errorf("userPath = %q, want it under ~/Library/Application Support/ThaiSmartcard", p)
		}
	case "windows":
		if !strings.Contains(p, filepath.Join("AppData", "Roaming", "ThaiSmartcard")) {
			t.Errorf("userPath = %q, want it under %%AppData%%\\ThaiSmartcard", p)
		}
	}
}

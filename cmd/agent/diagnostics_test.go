package main

import (
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticsCountsLogsWithoutReadingOrFollowingLinks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"agent.log", "agent.log.backup", "agent.log.lock", "unrelated"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Symlink(filepath.Join(dir, "unrelated"), filepath.Join(dir, "agent.log.link"))
	size, count, ok := logUsage(dir)
	if !ok || count != 2 || size != 18 {
		t.Fatalf("usage %d %d %v", size, count, ok)
	}
}
func TestDiagnosticsDistinguishesAppliedAndPendingLogging(t *testing.T) {
	active := config.Default().Logging
	pending := active
	pending.MaxBackups = 0
	d := diagnosticSnapshot(filepath.Join(t.TempDir(), "config.toml"), false, active, pending)
	if !d.Logging.RestartRequired || d.Logging.FileLogging || d.Logging.Effective.MaxBackups != 3 || d.Logging.Configured.MaxBackups != 0 {
		t.Fatalf("%+v", d.Logging)
	}
}

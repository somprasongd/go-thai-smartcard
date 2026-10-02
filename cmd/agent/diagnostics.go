package main

import (
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/server"
)

func diagnosticSnapshot(path string, managed bool, logging, configured config.Logging) server.DiagnosticSnapshot {
	policy := func(v config.Logging) model.LogPolicy {
		return model.LogPolicy{Mode: v.Mode, MaxSizeMB: v.MaxSizeMB, MaxBackups: v.MaxBackups, MaxAgeDays: v.MaxAgeDays}
	}
	usage := &model.LogUsage{Effective: policy(logging), Configured: policy(configured), RestartRequired: logging != configured}
	d := server.DiagnosticSnapshot{Logging: usage, OS: runtime.GOOS, Architecture: runtime.GOARCH, RunMode: "foreground"}
	if managed {
		d.RunMode = "service"
	}
	if logging.Mode == "file" || (logging.Mode == "auto" && managed) {
		usage.FileLogging = true
		d.LogDirectory, _ = filepath.Abs(filepath.Join(filepath.Dir(path), "logs"))
		usage.Bytes, usage.Files, usage.UsageAvailable = logUsage(d.LogDirectory)
	}
	return d
}

// Count sizes only, never read content or follow symlinks to unrelated files.
func logUsage(path string) (int64, int, bool) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, 0, false
	}
	var size int64
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".lock") || !(name == "agent.log" || strings.HasPrefix(name, "agent.log.") || name == "startup.log") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		size += info.Size()
		count++
	}
	return size, count, true
}

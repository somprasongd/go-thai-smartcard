package main

import (
	"path/filepath"
	"runtime"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/server"
)

func diagnosticSnapshot(path string, managed bool, logging config.Logging) server.DiagnosticSnapshot {
	d := server.DiagnosticSnapshot{OS: runtime.GOOS, Architecture: runtime.GOARCH, RunMode: "foreground"}
	if managed {
		d.RunMode = "service"
	}
	if logging.Mode == "file" || (logging.Mode == "auto" && managed) {
		d.LogDirectory, _ = filepath.Abs(filepath.Join(filepath.Dir(path), "logs"))
	}
	return d
}

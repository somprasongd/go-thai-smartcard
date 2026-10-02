package main

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/somprasongd/go-thai-smartcard/internal/logfile"
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
)

// startLogging leaves interactive runs on stderr unless file mode is explicit.
// Keeping the directory beside the chosen config also respects systemd's write
// sandbox and isolates configurations used for tests/development.
func startLogging(configPath string, cfg config.Logging, managed bool) (func(), error) {
	if cfg.Mode == "console" || (cfg.Mode == "auto" && !managed) {
		return func() {}, nil
	}
	return attachLog(filepath.Join(filepath.Dir(configPath), "logs", "agent.log"), logfile.Options{
		MaxBytes: int64(cfg.MaxSizeMB) * 1024 * 1024, MaxBackups: cfg.MaxBackups,
		MaxAge: time.Duration(cfg.MaxAgeDays) * 24 * time.Hour,
	})
}
func attachLog(path string, opts logfile.Options) (func(), error) {
	writer, err := logfile.Open(path, opts)
	if err != nil {
		return nil, fmt.Errorf("open bounded log: %w", err)
	}
	old := log.Writer()
	log.SetOutput(writer)
	return func() { log.SetOutput(old); writer.Close() }, nil
}

// reportStartupFailure has its own small bounded file so a bad config and an
// automatic restart loop cannot grow launchd's stderr file or prune the main
// log according to limits that failed to load. A failing filesystem falls back
// to stderr, which the service manager owns.
func reportStartupFailure(path string, managed bool, err error) {
	if managed {
		close, openErr := attachLog(filepath.Join(filepath.Dir(path), "logs", "startup.log"), logfile.Options{MaxBytes: 1024 * 1024, MaxBackups: 0, MaxAge: 24 * time.Hour})
		if openErr == nil {
			defer close()
			log.Printf("startup failed: %v", err)
			return
		}
	}
	log.Printf("startup failed: %v", err)
}

package main

import (
	"bytes"
	"errors"
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForegroundConsoleAndManagedFileLogging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	old := log.Writer()
	defer log.SetOutput(old)
	var console bytes.Buffer
	log.SetOutput(&console)
	cfg := config.Default().Logging
	stop, err := startLogging(path, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	log.Print("console event")
	stop()
	if console.Len() == 0 {
		t.Fatal("foreground did not retain console output")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "logs")); !os.IsNotExist(err) {
		t.Fatal("foreground created log files")
	}
	stop, err = startLogging(path, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	log.Print("managed event")
	stop()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "logs", "agent.log"))
	if err != nil || !bytes.Contains(raw, []byte("managed event")) {
		t.Fatal("service log missing")
	}
	cfg.Mode = "file"
	stop, err = startLogging(path, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	log.Print("explicit file event")
	stop()
	raw, _ = os.ReadFile(filepath.Join(filepath.Dir(path), "logs", "agent.log"))
	if !bytes.Contains(raw, []byte("explicit file event")) {
		t.Fatal("explicit file mode ignored")
	}
}

func TestStartupFailureLogIsSeparateAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	old := log.Writer()
	defer log.SetOutput(old)
	var console bytes.Buffer
	log.SetOutput(&console)
	reportStartupFailure(path, true, errors.New(strings.Repeat("synthetic startup error ", 60000)))
	file := filepath.Join(filepath.Dir(path), "logs", "startup.log")
	info, err := os.Stat(file)
	if err != nil || info.Size() > 1024*1024 {
		t.Fatal("unbounded startup failure log", err)
	}
	backups, _ := filepath.Glob(file + ".*.bak")
	if len(backups) != 0 {
		t.Fatal("startup history exceeded zero backups")
	}
	if console.Len() != 0 {
		t.Fatal("startup error also grew stderr capture")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "logs", "agent.log")); !os.IsNotExist(err) {
		t.Fatal("invalid config touched main log")
	}
}

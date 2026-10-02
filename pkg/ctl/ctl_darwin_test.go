//go:build darwin

package ctl

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMissingHelperAllowsInstalledServiceFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.plist")
	missing := func(string) (*Response, error) { return nil, errors.New("missing socket") }
	if _, err := helperState(missing, path); err == nil {
		t.Fatal("absent service reported available")
	}
	if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if state, err := helperState(missing, path); state != StateUnknown || err != nil {
		t.Fatalf("installed fallback: %v, %v", state, err)
	}
	denied := func(string) (*Response, error) { return &Response{OK: false}, errors.New("permission denied") }
	if _, err := helperState(denied, path); err == nil {
		t.Fatal("helper rejection was hidden")
	}
}

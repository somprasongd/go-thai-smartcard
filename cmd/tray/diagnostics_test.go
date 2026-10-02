package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogFolderMustBeExistingAbsoluteDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := logDirectory(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "relative", file, filepath.Join(dir, "missing")} {
		if _, err := logDirectory(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}

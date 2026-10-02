package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRestorePreservesCommentsAndRejectsHandEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Write(path, Default()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte("# operator's original comment\n"), raw...)
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	version, _ := Fingerprint(path)
	next := Default()
	next.Server.Port = 9999
	saved, err := SaveVersion(path, next, version)
	if err != nil {
		t.Fatal(err)
	}
	if err = Restore(path, original, true, saved); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("rollback changed original bytes")
	}
	version, _ = Fingerprint(path)
	saved, err = SaveVersion(path, next, version)
	if err != nil {
		t.Fatal(err)
	}
	handEdit := append([]byte("# newer hand edit\n"), original...)
	if err = os.WriteFile(path, handEdit, 0600); err != nil {
		t.Fatal(err)
	}
	if err = Restore(path, original, true, saved); !errors.Is(err, ErrStale) {
		t.Fatalf("restore error = %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != string(handEdit) {
		t.Fatal("rollback overwrote a hand edit")
	}
}

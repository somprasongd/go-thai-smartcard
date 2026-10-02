package logfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T, opts Options) (*Writer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs", "agent.log")
	w, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w, path
}
func history(t *testing.T, path string) []string {
	t.Helper()
	names, err := filepath.Glob(path + ".*.bak")
	if err != nil {
		t.Fatal(err)
	}
	return names
}
func assertBudget(t *testing.T, path string, max int64, count int) {
	t.Helper()
	files := append(history(t, path), path)
	if len(files) > count+1 {
		t.Fatal("backup count exceeded")
	}
	var total int64
	for _, file := range files {
		s, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		total += s.Size()
		if s.Size() > max {
			t.Fatalf("oversized log: %d", s.Size())
		}
	}
	if total > max*int64(count+1) {
		t.Fatal("disk budget exceeded")
	}
}
func TestSizeRotationAndOversizedMessage(t *testing.T) {
	w, path := openTest(t, Options{MaxBytes: 16, MaxBackups: 3})
	raw := []byte(strings.Repeat("0123456789abcdef", 4))
	if n, err := w.Write(raw); err != nil || n != len(raw) {
		t.Fatalf("write: %d %v", n, err)
	}
	assertBudget(t, path, 16, 3)
	files := history(t, path)
	files = append(files, path)
	var got []byte
	for _, f := range files {
		b, _ := os.ReadFile(f)
		got = append(got, b...)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("oversized message was dropped instead of split")
	}
	for i := 0; i < 100; i++ {
		if _, err := w.Write([]byte("a log event\n")); err != nil {
			t.Fatal(err)
		}
		assertBudget(t, path, 16, 3)
	}
}
func TestIdleExpiryDailyRotationAndUnrelatedFiles(t *testing.T) {
	w, path := openTest(t, Options{MaxBytes: 32, MaxBackups: 3, MaxAge: 7 * 24 * time.Hour})
	// The maintenance goroutine shares the same lock with the injected clock.
	w.mu.Lock()
	now := time.Now().UTC()
	w.now = func() time.Time { return now }
	w.mu.Unlock()
	if _, err := w.Write([]byte("first day\n")); err != nil {
		t.Fatal(err)
	}
	unrelated := path + ".notes.bak"
	os.WriteFile(unrelated, []byte("keep"), 0600)
	w.mu.Lock()
	now = now.Add(24 * time.Hour)
	w.mu.Unlock()
	if err := w.Maintain(); err != nil {
		t.Fatal(err)
	}
	if len(history(t, path)) != 2 {
		t.Fatal("daily idle rotation missing")
	} // own backup + unrelated
	w.mu.Lock()
	now = now.Add(8 * 24 * time.Hour)
	w.mu.Unlock()
	if err := w.Maintain(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("unrelated file removed")
	}
	files := history(t, path)
	if len(files) != 1 || files[0] != unrelated {
		t.Fatal("expired history survived idle maintenance")
	}
}
func TestRestartPrunesOldActiveAndOversizedHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	os.WriteFile(path, []byte("stale"), 0600)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(path, old, old)
	name := path + "." + time.Now().UTC().Format("20060102T150405.000000000Z") + ".123.bak"
	os.WriteFile(name, bytes.Repeat([]byte("x"), 100), 0600)
	w, err := Open(path, Options{MaxBytes: 16, MaxBackups: 3, MaxAge: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if len(history(t, path)) != 0 {
		t.Fatal("stale active/oversized backup retained on restart")
	}
	assertBudget(t, path, 16, 3)
}
func TestLockCloseAndPrivatePermissions(t *testing.T) {
	w, path := openTest(t, Options{MaxBytes: 16, MaxBackups: 0})
	if other, err := Open(path, Options{MaxBytes: 16}); err == nil {
		other.Close()
		t.Fatal("second process writer accepted")
	}
	if runtime.GOOS != "windows" {
		for _, file := range []string{path, path + ".lock", filepath.Dir(path)} {
			s, _ := os.Stat(file)
			if s.Mode().Perm()&0077 != 0 {
				t.Fatal("log path is not private")
			}
		}
	}
	if _, err := w.Write(bytes.Repeat([]byte("x"), 100)); err != nil {
		t.Fatal(err)
	}
	if len(history(t, path)) != 0 {
		t.Fatal("max_backups=0 retained history")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() { w.Close() })
	}
	wg.Wait()
	if _, err := w.Write([]byte("closed")); !errors.Is(err, os.ErrClosed) {
		t.Fatal("write after close accepted")
	}
	next, err := Open(path, Options{MaxBytes: 16})
	if err != nil {
		t.Fatal("lock was not released", err)
	}
	next.Close()
}
func TestConcurrentWritesStayBounded(t *testing.T) {
	w, path := openTest(t, Options{MaxBytes: 128, MaxBackups: 3})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Go(func() {
			for n := 0; n < 100; n++ {
				if _, err := fmt.Fprintln(w, "concurrent event"); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	assertBudget(t, path, 128, 3)
}
func TestRotationFailureReturnsErrorWithoutGrowingFile(t *testing.T) {
	w, path := openTest(t, Options{MaxBytes: 4, MaxBackups: 1})
	w.Write([]byte("full"))
	original := w.path
	w.path = filepath.Join(path, "invalid-directory")
	if n, err := w.Write([]byte("more")); err == nil || n != 0 {
		t.Fatal("rotation failure hidden")
	}
	w.path = original
	info, _ := os.Stat(path)
	if info.Size() != 4 {
		t.Fatal("failed rotation grew active log")
	}
}

func TestNormalRecordsDoNotSplitAtRotation(t *testing.T) {
	w, path := openTest(t, Options{MaxBytes: 16, MaxBackups: 1})
	w.Write([]byte("first record\n"))
	w.Write([]byte("next record\n"))
	raw, _ := os.ReadFile(path)
	if string(raw) != "next record\n" {
		t.Fatal("ordinary log record split across files")
	}
}

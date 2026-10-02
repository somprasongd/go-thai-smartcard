// Package logfile bounds disk usage for a long-running process, independently
// of stdout redirection by its service manager.
package logfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/somprasongd/go-thai-smartcard/internal/atomicfile"
)

// Options caps both each file and the retained history; zero age disables
// age expiry but the backup count still bounds total space.
type Options struct {
	MaxBytes   int64
	MaxBackups int
	MaxAge     time.Duration
}

// Writer serializes rotation with writes. A process lock prevents two agents
// from rotating the same file independently.
type Writer struct {
	mu         sync.Mutex
	path       string
	opts       Options
	file, lock *os.File
	size       int64
	day        string
	lastWrite  time.Time
	closeOnce  sync.Once
	closeErr   error
	now        func() time.Time
	closed     bool
	stop       chan struct{}
	done       chan struct{}
}

// Open applies retention before accepting writes and starts idle maintenance.
func Open(path string, opts Options) (*Writer, error) {
	if opts.MaxBytes <= 0 || opts.MaxBackups < 0 || opts.MaxAge < 0 {
		return nil, errors.New("invalid log retention limits")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := atomicfile.OpenAppend(path + ".lock")
	if err != nil {
		return nil, err
	}
	if err = lockFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("log already in use: %w", err)
	}
	w := &Writer{path: path, opts: opts, lock: lock, now: time.Now, stop: make(chan struct{}), done: make(chan struct{})}
	if err = w.open(); err != nil {
		lock.Close()
		return nil, err
	}
	if err = w.maintain(); err != nil {
		w.file.Close()
		lock.Close()
		return nil, err
	}
	go w.run()
	return w, nil
}

func (w *Writer) open() error {
	f, err := atomicfile.OpenAppend(w.path)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return errors.New("log is not a regular file")
	}
	w.file = f
	w.size = info.Size()
	w.day = info.ModTime().UTC().Format(time.DateOnly)
	w.lastWrite = info.ModTime()
	if w.size == 0 {
		w.day = w.now().UTC().Format(time.DateOnly)
	}
	return nil
}

// Write splits even an oversized message so no active or retained file exceeds
// MaxBytes. Errors reach the caller instead of silently dropping log bytes.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if err := w.maintain(); err != nil {
		return 0, err
	}
	// Keep normal log records intact; only messages larger than an entire
	// file need splitting.
	if w.size > 0 && int64(len(p)) <= w.opts.MaxBytes && w.size+int64(len(p)) > w.opts.MaxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	written := 0
	for len(p) > 0 {
		if w.size >= w.opts.MaxBytes {
			if err := w.rotate(); err != nil {
				return written, err
			}
		}
		room := w.opts.MaxBytes - w.size
		chunk := p
		if int64(len(chunk)) > room {
			chunk = chunk[:int(room)]
		}
		n, err := w.file.Write(chunk)
		w.size += int64(n)
		if n > 0 {
			w.lastWrite = w.now()
		}
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
		if n < len(chunk) {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

// Maintain expires history while the process is quiet, and rotates daily so
// low-volume files cannot accumulate records older than the retention window.
func (w *Writer) Maintain() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return os.ErrClosed
	}
	return w.maintain()
}
func (w *Writer) maintain() error {
	if w.size == 0 {
		w.day = w.now().UTC().Format(time.DateOnly)
	}
	if w.size > 0 && (w.day != w.now().UTC().Format(time.DateOnly) || w.size > w.opts.MaxBytes) {
		return w.rotate()
	}
	return w.prune()
}
func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	// Timestamp names preserve the last write time, so an idle old active
	// file expires immediately instead of being retained as a fresh backup.
	// CreateTemp reserves a collision-free private destination.
	backup, err := os.CreateTemp(filepath.Dir(w.path), filepath.Base(w.path)+"."+w.lastWrite.UTC().Format("20060102T150405.000000000Z")+".*.bak")
	if err != nil {
		return errors.Join(err, w.open())
	}
	name := backup.Name()
	backup.Close()
	// Windows cannot rename over an existing placeholder. The writer's process
	// lock protects this namespace while the destination is replaced.
	os.Remove(name)
	if err = os.Rename(w.path, name); err != nil {
		return errors.Join(err, w.open())
	}
	if err = w.prune(); err != nil {
		return errors.Join(err, w.open())
	}
	return w.open()
}
func (w *Writer) prune() error {
	entries, err := os.ReadDir(filepath.Dir(w.path))
	if err != nil {
		return err
	}
	type backup struct {
		path string
		at   time.Time
		size int64
	}
	var backups []backup
	prefix := filepath.Base(w.path) + "."
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".bak") || !entry.Type().IsRegular() {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".bak"), ".")
		if len(parts) != 3 {
			continue
		}
		stamp := parts[0] + "." + parts[1]
		at, err := time.Parse("20060102T150405.000000000Z", stamp)
		if err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		backups = append(backups, backup{filepath.Join(filepath.Dir(w.path), name), at, info.Size()})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].at.After(backups[j].at) })
	for i, b := range backups {
		if i >= w.opts.MaxBackups || b.size > w.opts.MaxBytes || (w.opts.MaxAge > 0 && !b.at.After(w.now().Add(-w.opts.MaxAge))) {
			if err := os.Remove(b.path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
func (w *Writer) run() {
	defer close(w.done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			// A failure is retried by the next write/tick. Do not log recursively into
			// a writer that may itself be unable to write.
			_ = w.Maintain()
		}
	}
}

// Close waits for maintenance to finish before releasing the process lock.
func (w *Writer) Close() error {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.stop)
		w.mu.Unlock()
		<-w.done
		w.closeErr = errors.Join(w.file.Close(), w.lock.Close())
	})
	return w.closeErr
}

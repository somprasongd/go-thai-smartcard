// Package atomicfile replaces files without exposing partially written bytes.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Pending keeps a complete replacement beside its destination until commit.
type Pending struct{ path, temporary string }

// Prepare makes the replacement durable before a transaction changes runtime.
func Prepare(path string, data []byte, mode os.FileMode) (*Pending, error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".smc-*")
	if err != nil {
		return nil, err
	}
	p := &Pending{path: path, temporary: f.Name()}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			p.Abort()
		}
	}()
	if err = setMode(f, mode); err != nil {
		return nil, err
	}
	if _, err = f.Write(data); err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	ok = true
	return p, nil
}

// Commit exposes the complete new file in one platform-specific replacement.
func (p *Pending) Commit() error { return replace(p.temporary, p.path) }

// Abort also works after commit, when the staging filename no longer exists.
func (p *Pending) Abort() { _ = os.Remove(p.temporary) }

// Write never truncates the old destination before the new bytes are ready.
func Write(path string, data []byte, mode os.FileMode) error {
	p, err := Prepare(path, data, mode)
	if err != nil {
		return err
	}
	defer p.Abort()
	return p.Commit()
}

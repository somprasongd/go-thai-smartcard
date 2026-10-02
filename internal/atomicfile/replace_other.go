//go:build !windows

package atomicfile

import "os"

func replace(from, to string) error              { return os.Rename(from, to) }
func ReadFile(path string) ([]byte, error)       { return os.ReadFile(path) }
func setMode(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }

//go:build linux || darwin

package discovery

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func secureDirectory(path string, service bool) error {
	mode := os.FileMode(0700)
	if service {
		mode = 0755
	}
	return os.Chmod(path, mode)
}

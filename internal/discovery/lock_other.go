//go:build !linux && !darwin && !windows

package discovery

import (
	"errors"
	"os"
)

func lockFile(*os.File) error {
	return errors.New("desktop discovery is not supported on this platform")
}
func secureDirectory(string, bool) error { return nil }

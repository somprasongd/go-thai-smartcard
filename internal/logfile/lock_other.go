//go:build !linux && !darwin && !windows

package logfile

import (
	"errors"
	"os"
)

func lockFile(*os.File) error { return errors.New("file logging is unsupported on this platform") }

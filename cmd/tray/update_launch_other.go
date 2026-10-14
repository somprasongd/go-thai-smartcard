//go:build !windows && !darwin && !linux

package main

import "fmt"

func openInstaller(string) error {
	return fmt.Errorf("opening installers is unsupported on this operating system")
}

//go:build linux

package main

import "os/exec"

func openInstaller(path string) error {
	return runInstallerOpener(exec.Command("xdg-open", path))
}

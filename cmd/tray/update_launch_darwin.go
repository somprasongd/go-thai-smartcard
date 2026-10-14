//go:build darwin

package main

import "os/exec"

func openInstaller(path string) error {
	return runInstallerOpener(exec.Command("open", path))
}

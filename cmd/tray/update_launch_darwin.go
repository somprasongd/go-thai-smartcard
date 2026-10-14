//go:build darwin

package main

import "os/exec"

func openInstaller(path string) error {
	cmd := exec.Command("open", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

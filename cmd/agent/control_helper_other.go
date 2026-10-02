//go:build !darwin

package main

import "errors"

// The control helper backs the macOS tray only: Linux authorises systemctl
// through polkit and Windows through the service ACL, so neither needs a
// helper process (docs/plan/tray-agent-control.md).
func runControlHelper() error {
	return errors.New("control-helper is only implemented on macOS")
}

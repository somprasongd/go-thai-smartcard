//go:build linux

package main

import (
	"errors"
	"os/exec"
)

// confirm asks the operator before Stop runs. Desktops ship one of zenity
// (GNOME and most others) or kdialog (KDE); where neither exists — a bare
// window manager — the confirmation degrades to executing without a dialog,
// which the plan documents as the accepted fallback.
func confirm(title, message string) bool {
	if err := exec.Command("zenity", "--question", "--title", title, "--text", message,
		"--ok-label", "หยุด / Stop", "--cancel-label", "ยกเลิก / Cancel").Run(); err == nil {
		return true
	} else if !looksMissing(err) {
		return false // the dialog ran and was cancelled
	}
	if err := exec.Command("kdialog", "--title", title, "--yesno", message).Run(); err == nil {
		return true
	} else if !looksMissing(err) {
		return false
	}
	return true
}

// looksMissing reports whether err means the helper binary is not installed,
// as opposed to the user answering "no".
func looksMissing(err error) bool {
	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		return false // the dialog ran; the user answered
	}
	return true // not executable / not found
}

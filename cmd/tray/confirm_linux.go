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
func confirm(l language, title, message string) bool {
	if err := exec.Command("zenity", "--question", "--title", title, "--text", message,
		"--ok-label", l.text("หยุด", "Stop"), "--cancel-label", l.text("ยกเลิก", "Cancel")).Run(); err == nil {
		return true
	} else if !looksMissing(err) {
		return false // the dialog ran and was cancelled
	}
	if err := exec.Command("kdialog", "--title", title, "--yesno", message, "--yes-label", l.text("หยุด", "Stop"), "--no-label", l.text("ยกเลิก", "Cancel")).Run(); err == nil {
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

func alert(l language, title, message string) {
	if err := exec.Command("zenity", "--error", "--title", title, "--text", message, "--ok-label", l.text("ตกลง", "OK")).Run(); err != nil && looksMissing(err) {
		_ = exec.Command("kdialog", "--title", title, "--error", message).Run()
	}
}

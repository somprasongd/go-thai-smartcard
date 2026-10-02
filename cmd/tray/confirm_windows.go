//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"syscall"
	"unsafe"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	messageBoxWProc = user32.NewProc("MessageBoxExW")
)

const (
	mbOKCANCEL = 0x00000001
	mbIconWarn = 0x00000030
	idOK       = 1
)

// confirm asks the operator before Stop runs, through the system dialog. It
// returns whether the action should proceed.
func confirm(l language, title, message string) bool {
	return messageBox(l, title, message, mbOKCANCEL|mbIconWarn|0x100) == idOK
}

func alert(l language, title, message string) {
	messageBox(l, title, message, 0x10)
}

func messageBox(l language, title, message string, flags uintptr) uintptr {
	text, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return 0
	}
	caption, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	langID := uintptr(0x0409) // English for every non-Thai UI.
	if l == thai {
		langID = 0x041e
	}
	rc, _, _ := messageBoxWProc.Call(
		0, // no owner window: the tray has none
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(caption)),
		flags,
		langID,
	)
	return rc
}

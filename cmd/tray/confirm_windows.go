//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"syscall"
	"unsafe"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	messageBoxWProc = user32.NewProc("MessageBoxW")
)

const (
	mbOKCANCEL = 0x00000001
	mbIconWarn = 0x00000030
	idOK       = 1
)

// confirm asks the operator before Stop runs, through the system dialog. It
// returns whether the action should proceed.
func confirm(title, message string) bool {
	text, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return false
	}
	caption, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	rc, _, _ := messageBoxWProc.Call(
		0, // no owner window: the tray has none
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(caption)),
		mbOKCANCEL|mbIconWarn,
	)
	return rc == idOK
}

//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
int trayConfirmNative(const char *, const char *, const char *, const char *);
void trayAlertNative(const char *, const char *, const char *);
*/
import "C"
import "unsafe"

// The dialog belongs to the tray, and Cocoa runs it on the application thread.
// No separate scripting process owns the prompt or its language preferences.
func confirm(l language, title, message string) bool {
	t, m := C.CString(title), C.CString(message)
	stop, cancel := C.CString(l.text("หยุด", "Stop")), C.CString(l.text("ยกเลิก", "Cancel"))
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(m))
	defer C.free(unsafe.Pointer(stop))
	defer C.free(unsafe.Pointer(cancel))
	return C.trayConfirmNative(t, m, stop, cancel) != 0
}

func alert(l language, title, message string) {
	t, m, ok := C.CString(title), C.CString(message), C.CString(l.text("ตกลง", "OK"))
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(m))
	defer C.free(unsafe.Pointer(ok))
	C.trayAlertNative(t, m, ok)
}

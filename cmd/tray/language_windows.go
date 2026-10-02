//go:build windows

package main

import "golang.org/x/sys/windows"

func systemLanguage() language {
	id, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	// A LANGID's low ten bits identify the primary language (LANG_THAI).
	return language(id&0x3ff == 0x1e)
}

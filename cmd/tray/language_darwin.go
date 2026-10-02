//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Foundation
int preferredLanguageIsThai(void);
*/
import "C"

func systemLanguage() language { return language(C.preferredLanguageIsThai() != 0) }

//go:build darwin && !cgo

package main

import "log"

// A native macOS tray requires cgo; a stub build must never approve a Stop.
func confirm(language, string, string) bool   { return false }
func alert(_ language, title, message string) { log.Printf("%s: %s", title, message) }

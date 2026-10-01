//go:build !linux

package main

// macOS and Windows show a tray icon without a DBus name to check, so the
// StatusNotifier probe is a no-op there.

func checkStatusNotifier(string) {}

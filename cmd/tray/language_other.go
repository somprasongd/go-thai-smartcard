//go:build (!linux && !windows && !darwin) || (darwin && !cgo)

package main

func systemLanguage() language { return english }

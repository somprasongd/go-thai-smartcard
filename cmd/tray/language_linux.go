//go:build linux

package main

import "os"

func systemLanguage() language { return languageFor(localeFromEnvironment(os.Getenv)) }

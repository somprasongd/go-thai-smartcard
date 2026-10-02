//go:build js

package main

import (
	"errors"
	"flag"
	"log"
)

// A browser build is not a system service: the subcommand exists to fail with
// a clear message rather than to be silently missing, and no manager ever
// launches the wasm agent, so main always runs in the foreground.

func serviceCommand(args []string) {
	fs := flag.NewFlagSet("service", flag.ExitOnError)
	_ = fs.Parse(args)
	log.Fatal("the service command is not supported on this platform")
}

func runManaged(string) bool {
	return false
}

var errNotUsed = errors.New("not used on js")

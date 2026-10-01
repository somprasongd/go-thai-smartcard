//go:build js

package config

// A browser build is not a service and has no user config dir, so both paths
// collapse to the working directory. It keeps pkg/config compiling for wasm.

var goos = "js"

func runningAsSystem() bool { return false }

func servicePath() string { return "config.toml" }

func userPath() string { return "config.toml" }

//go:build !darwin && !js

package main

import "github.com/kardianos/service"

func configureServiceOutput(service.KeyValue) {}

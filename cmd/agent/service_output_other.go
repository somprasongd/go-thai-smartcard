//go:build !darwin

package main

import "github.com/kardianos/service"

func configureServiceOutput(service.KeyValue) {}

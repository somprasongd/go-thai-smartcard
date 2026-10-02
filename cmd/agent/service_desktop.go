//go:build !js

// The service subcommand, built on kardianos/service: it registers, starts and
// stops the agent as a system service on Windows (Service), Linux (systemd,
// Upstart, SysV) and macOS (launchd). Decision 12: the agent is always a
// system service; the tray never spawns it, and a bare invocation in a
// terminal stays the development mode.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kardianos/service"
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
)

// serviceName is what the service is registered as on every platform. The
// packaged unit file uses the same name, so `service status` answers for the
// packaged service too.
const serviceName = "thai-smartcard-agent"

// serviceCommand implements `agent service install|uninstall|start|stop|
// restart|status`. The installed service points at one config file, resolved
// the same way the agent resolves it, so the service's environment and the
// config path cannot drift apart.
func serviceCommand(args []string) {
	fs := flag.NewFlagSet("service", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config.toml to bake into the installed service")
	_ = fs.Parse(args)

	action := "status"
	if rest := fs.Args(); len(rest) > 0 {
		action = rest[0]
	}

	path := *configPath
	if path == "" {
		// An installer runs elevated, so this resolves to the service path —
		// exactly what the installed service should use.
		path = config.DefaultPath()
	}

	svc, err := newAgentService(path)
	if err != nil {
		log.Fatalf("service: %v", err)
	}
	// status is not one of service.Control's actions; the handle answers it
	// directly.
	switch action {
	case "status":
		state, err := svc.Status()
		if err != nil {
			log.Fatalf("service status: %v", err)
		}
		fmt.Printf("%s: %s\n", serviceName, describeStatus(state))
		return
	case "install", "uninstall", "start", "stop", "restart":
		if err := service.Control(svc, action); err != nil {
			log.Fatalf("service %s: %v", action, err)
		}
	default:
		log.Fatalf("service: unknown action %q; use install, uninstall, start, stop, restart or status", action)
	}
	if action == "install" {
		fmt.Printf("installed %s, running with %s; start it with `%s service start`\n", serviceName, path, os.Args[0])
	}
}

// runManaged runs the agent under the service manager when this process was
// launched by one, and reports whether it did. A terminal invocation is
// interactive and returns false, leaving main in the foreground path.
func runManaged(configPath string) bool {
	if service.Interactive() {
		return false
	}
	svc, err := newAgentService(configPath)
	if err != nil {
		log.Fatalf("service: %v", err)
	}
	if err := svc.Run(); err != nil {
		log.Fatal(err)
	}
	return true
}

// newAgentService wires the agent loop into the service manager's Start/Stop.
func newAgentService(configPath string) (service.Service, error) {
	prg := &agentProgram{configPath: configPath}
	return service.New(prg, &service.Config{
		Name:        serviceName,
		DisplayName: "Thai Smartcard Agent",
		Description: "Reads Thai national ID cards and broadcasts the data to connected clients.",
		// The config file is the whole configuration; the service needs no
		// other arguments. The packaged systemd unit carries the same pair.
		Arguments: []string{"--config", configPath},
		Option: service.KeyValue{
			// systemd: the unit file packaged with the .deb/.rpm sets these
			// (and After=pcscd.service, which the library cannot express on
			// Linux) — the keys here only matter for `service install`.
			"Restart":           "always",
			"RestartSec":        "5s",
			"SuccessExitStatus": "0",
		},
	})
}

// describeStatus turns kardianos's numeric Status into words. The library
// (v1.2.2) has no String method, and "thai-smartcard-agent: 2" reads like an
// error to whoever ran it.
func describeStatus(s service.Status) string {
	switch s {
	case service.StatusRunning:
		return "running"
	case service.StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// agentProgram is the service.Interface the manager drives.
type agentProgram struct {
	configPath string

	cancel context.CancelFunc
	done   chan struct{}
}

func (p *agentProgram) Start(_ service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		runAgentMode(ctx, p.configPath, true)
	}()
	return nil
}

func (p *agentProgram) Stop(_ service.Service) error {
	if p.cancel == nil {
		return nil
	}
	p.cancel()
	// The read loop releases the card and the PC/SC context on shutdown;
	// give it a moment rather than leaving them held by a dying process.
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		log.Printf("service: the agent did not stop within 10s")
	}
	return nil
}

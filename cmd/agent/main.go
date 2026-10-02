// Command agent reads a Thai national ID card and broadcasts what it finds to
// connected clients.
//
// Configuration comes from one config.toml file (see pkg/config): --config
// points at the file, --version prints the version. There are no environment
// variables; a stale SMC_* variable only earns a warning naming its
// replacement. `agent service install|uninstall|start|stop|restart|status`
// manages the system service; a bare invocation runs in the foreground when
// typed at a terminal and under the service manager otherwise.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/somprasongd/go-thai-smartcard/internal/discovery"
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/server"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
)

// version is overridden at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "service" {
		serviceCommand(os.Args[2:])
		return
	}

	// macOS only: the tray's privileged helper (LaunchDaemon), documented in
	// docs/plan/tray-agent-control.md. Never run by hand.
	if len(os.Args) > 1 && os.Args[1] == "control-helper" {
		if err := runControlHelper(); err != nil {
			log.Fatalf("control-helper: %v", err)
		}
		return
	}

	// `agent run` is the spelled-out form of a bare invocation, so
	// `run --config x` parses the same as `--config x`.
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	var (
		configPath  = flag.String("config", "", "path to config.toml (default: the service config directory)")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.CommandLine.Parse(args)

	if *showVersion {
		fmt.Println(version)
		return
	}

	if runManaged(*configPath) {
		// A service manager drove Start and Stop; the agent ran inside it.
		return
	}

	// Foreground: a terminal, or a manager the service wrapper does not know.
	// Shut down cleanly on Ctrl+C and SIGTERM, so the card and the PC/SC
	// context are released rather than abandoned.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runAgent(ctx, *configPath)
}

// runAgent is the whole agent: config, listener, read loop. It blocks until
// ctx is done. The foreground run and the service wrapper both land here.
func runAgent(ctx context.Context, configPath string) { runAgentMode(ctx, configPath, false) }

func runAgentMode(ctx context.Context, configPath string, managed bool) {
	// Resolve before anything: the listener's settings routes read and write
	// this path, so an unresolved "" would silently leave /settings and
	// /api/* unregistered and /settings would serve the bundled test page.
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	cfg, notice, err := loadConfig(configPath)
	if err != nil {
		reportStartupFailure(configPath, managed, err)
		os.Exit(1)
	}
	stopLogging, err := startLogging(configPath, cfg.Logging, managed)
	if err != nil {
		reportStartupFailure(configPath, managed, err)
		os.Exit(1)
	}
	defer stopLogging()
	if notice != "" {
		log.Print(notice)
	}

	for _, warning := range config.EnvWarnings(os.LookupEnv) {
		log.Printf("WARNING: %s", warning)
	}

	broadcast := make(chan model.Message)
	// Buffered because a page asks for the options and the status back to back
	// on connect. pkg/server drops a command it cannot hand over immediately
	// rather than stall a connection, so on an unbuffered channel a burst of two
	// loses the second one and the page starts with its reader list empty.
	command := make(chan model.Command, 16)
	// Buffered so a burst of page actions does not have to wait for the read
	// loop to finish a card before it is even offered.
	control := make(chan smc.Control, 8)

	// The daemon re-reads this store on every card insert, which is what lets
	// a settings change apply to the next card without a restart.
	store := smc.NewOptionsStore(cardOptions(cfg.Card))
	selection := smc.NewReaderStore(cfg.Card.Reader)

	instance, err := discovery.NewInstanceID()
	if err != nil {
		log.Fatalf("instance identity: %v", err)
	}
	runtimeSettings := &settingsCoordinator{path: configPath, current: cfg, store: store, selection: selection, ready: make(chan struct{})}
	runtimeSettings.openPublisher = func() (*discovery.Publisher, error) {
		path := discovery.ServicePath()
		if !managed {
			var err error
			path, err = discovery.UserPath()
			if err != nil {
				return nil, err
			}
		}
		return discovery.Open(path, instance, managed)
	}
	// One status cache for the whole process: the broadcast pump keeps the
	// newest reader list in it, and /api/readers serves that to the settings
	// page. Listener generations come and go; the cache survives them.
	statusCache := &server.StatusCache{}
	runtimeSettings.serverConfig = func(next config.Config) server.ServerConfig {
		result := serverCfg(next, configPath, broadcast, command, nil)
		result.InstanceID = instance
		result.Status = statusCache
		result.Diagnostics = func() server.DiagnosticSnapshot { return diagnosticSnapshot(configPath, managed, cfg.Logging) }
		result.ApplySettings = runtimeSettings.apply
		return result
	}
	mgr, err := server.Start(runtimeSettings.serverConfig(cfg))
	if err != nil {
		log.Fatalf("%v", err)
	}
	runtimeSettings.manager = mgr
	runtimeSettings.publishStartup()
	close(runtimeSettings.ready)
	defer mgr.Close()
	defer func() {
		if runtimeSettings.publisher != nil {
			runtimeSettings.publisher.Close()
		}
	}()

	controller := &optionsController{
		store:     store,
		broadcast: broadcast,
		control:   control,
		ctx:       ctx,
	}

	controllerDone := make(chan struct{})
	go func() { defer close(controllerDone); controller.run(ctx, command) }()
	defer func() { <-controllerDone }()
	runCardDaemon(ctx, smc.DaemonConfig{Broadcast: broadcast, Options: store, Control: control, Selection: selection}, smc.NewTransport, cardRetryInterval)
	log.Println("Received shutdown signal, exiting.")
}

// serverCfg builds the listener configuration for one generation.
func serverCfg(cfg config.Config, path string, broadcast chan model.Message, command chan model.Command, onChange func(config.Config)) server.ServerConfig {
	return server.ServerConfig{
		Listen:         cfg.Server.Listen,
		Port:           cfg.Server.Port,
		Transports:     cfg.Server.Transports,
		AllowedOrigins: cfg.Server.AllowedOrigins,
		Token:          cfg.Server.Token,
		Broadcast:      broadcast,
		Command:        command,
		Version:        version,
		TLS:            cfg.TLS,
		ConfigPath:     path,
		OnChange:       onChange,
	}
}

func cardOptions(card config.Card) smc.Options {
	return smc.Options{
		ShowFaceImage: card.ReadFaceImage,
		ShowNhsoData:  card.ReadNHSO,
		ShowLaserData: card.ReadLaserID,
	}
}

// loadConfig reads the config file, writing the defaults when there is none,
// and stops the agent on a file it cannot use. The strict loader is the point:
// a service that starts on a typoed config while the operator believes the
// typoed value applied is worse than one that does not start.
func loadConfig(path string) (config.Config, string, error) {
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	switch {
	case err == nil:
		return cfg, "", nil
	case errors.Is(err, config.ErrNotFound):
		cfg = config.Default()
		if werr := config.Write(path, cfg); werr != nil {
			return cfg, fmt.Sprintf("no config file at %s and none could be written (%v); running on the defaults", path, werr), nil
		} else {
			return cfg, fmt.Sprintf("no config file at %s, wrote the defaults; hand edits need a restart", path), nil
		}
	default:
		return config.Config{}, "", err
	}
}

// optionsController answers the harmless control commands a client sends —
// status, refresh and re-read. What the agent reads is configuration with one
// source of truth (config.toml via /api/settings), so the socket neither
// carries nor changes it.
//
// It lives in the agent rather than in pkg/smc because the broadcast belongs
// to this process, and pkg/smc stays free of the server's concerns. The card
// socket is read-only: options and the reader change through /settings or the
// tray, never over the socket.
type optionsController struct {
	ctx       context.Context
	store     *smc.OptionsStore
	broadcast chan model.Message
	// control reaches the read loop, which is the only thing that knows which
	// readers are attached and whether a card is in one.
	control chan<- smc.Control
}

// run consumes commands until the process is shutting down or the channel is
// closed.
func (c *optionsController) run(ctx context.Context, command <-chan model.Command) {
	for {
		select {
		case <-ctx.Done():
			return
		case cmd, ok := <-command:
			if !ok {
				return
			}
			c.handle(cmd)
		}
	}
}

// handle answers exactly one command. Every answer goes back over the same
// broadcast as the card events, so a page needs only the one connection it
// already has; an action it does not know is an error, never a panic. The
// removed set-options and set-reader land here too, answered as unknown.
func (c *optionsController) handle(cmd model.Command) {
	switch cmd.Action {
	case "get-status":
		// Answered by the read loop, which owns the reader list and its own
		// state. It re-publishes without changing anything.
		c.sendCommandControl(cmd, smc.ControlReportStatus)
	case "refresh-readers":
		c.sendCommandControl(cmd, smc.ControlRefreshReaders)
	case "read-now":
		// Harmless when no card is in: the loop answers with an error rather
		// than reading nothing silently.
		c.sendCommandControl(cmd, smc.ControlReadNow)
	default:
		if cmd.Reply != nil {
			cmd.Reply.Send("failed", "unknown_action")
		}
		c.fail(fmt.Sprintf("unknown action %q", cmd.Action))
	}
}

// Responses are connection-scoped. The gate guarantees accepted precedes the
// terminal result even if the read loop consumes the queued control immediately.
func (c *optionsController) sendCommandControl(cmd model.Command, kind smc.ControlKind) {
	reply := func(status, code string) {
		if cmd.Reply != nil {
			cmd.Reply.Send(status, code)
		}
	}
	if c.control == nil {
		reply("failed", "control_unavailable")
		return
	}
	gate := make(chan struct{})
	var once sync.Once
	ctl := smc.Control{Kind: kind}
	if cmd.Reply != nil {
		ctl.Complete = &smc.ControlResult{Finish: func(err error) {
			<-gate
			once.Do(func() {
				if errors.Is(err, smc.ErrReadBusy) {
					reply("busy", "reader_busy")
				} else if err != nil {
					reply("failed", "operation_failed")
				} else {
					reply("completed", "")
				}
			})
		}}
	}
	select {
	case c.control <- ctl:
		reply("accepted", "")
		close(gate)
	default:
		reply("busy", "reader_queue_full")
	}
}

func (c *optionsController) fail(message string) {
	log.Println("control command rejected:", message)
	c.send(model.Message{
		Event:   "smc-error",
		Payload: map[string]string{"message": message},
	})
}

func (c *optionsController) send(msg model.Message) {
	if c.broadcast == nil {
		return
	}
	var done <-chan struct{}
	if c.ctx != nil {
		done = c.ctx.Done()
	}
	select {
	case c.broadcast <- msg:
	case <-done:
	}
}

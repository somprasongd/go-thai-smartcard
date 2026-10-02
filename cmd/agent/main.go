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
	"reflect"
	"sync"
	"syscall"
	"time"

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
func runAgent(ctx context.Context, configPath string) {
	// Resolve before anything: the listener's settings routes read and write
	// this path, so an unresolved "" would silently leave /settings and
	// /api/* unregistered and /settings would serve the bundled test page.
	if configPath == "" {
		configPath = config.DefaultPath()
	}
	cfg := loadConfig(configPath)

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

	// A save from /settings or the tray comes back here: card options apply to
	// the next insert, a server or TLS change restarts the listener.
	var mu sync.Mutex
	current := cfg
	var mgr *server.Manager
	// Declared first so the closure can hand itself to the next listener
	// generation: a restarted listener keeps applying saves.
	var applySave func(config.Config)
	applySave = func(next config.Config) {
		mu.Lock()
		prev := current
		current = next
		mu.Unlock()

		if !reflect.DeepEqual(next.Card, prev.Card) {
			store.Set(cardOptions(next.Card))
			if next.Card.Reader != prev.Card.Reader {
				// The read loop drains this channel between poll windows; a
				// dropped request is recoverable and the tray retries.
				select {
				case control <- smc.Control{Kind: smc.ControlSelectReader, Reader: next.Card.Reader}:
				default:
					log.Printf("dropping a reader control request, the read loop is busy")
				}
			}
			// Tell the pages what is read now, so their displays catch up
			// without waiting for the next card.
			broadcast <- model.Message{Event: "smc-options", Payload: toModelOptions(store.Get())}
		}

		if !reflect.DeepEqual(next.Server, prev.Server) || !reflect.DeepEqual(next.TLS, prev.TLS) {
			// A token change lands here too, because the socket guard of the
			// running listener holds the old one.
			if err := mgr.Replace(serverCfg(next, configPath, broadcast, command, applySave)); err != nil {
				log.Printf("could not restart the listener: %v; the old listener keeps serving", err)
			} else {
				log.Printf("listener restarted on %s:%d", next.Server.Listen, next.Server.Port)
			}
		}
	}

	mgr, err := server.Start(serverCfg(cfg, configPath, broadcast, command, applySave))
	if err != nil {
		log.Fatalf("%v", err)
	}

	controller := &optionsController{
		store:     store,
		broadcast: broadcast,
		control:   control,
	}

	go controller.run(ctx, command)

	smcReader := smc.NewSmartCard()
	defer func() {
		if err := smcReader.Close(); err != nil {
			log.Printf("Error closing smart card transport: %v", err)
		}
	}()

	go func() {
		for {
			err := smcReader.StartDaemonWith(ctx, smc.DaemonConfig{
				Broadcast: broadcast,
				Options:   store,
				Control:   control,
				Reader:    cfg.Card.Reader,
			})
			if ctx.Err() != nil {
				return
			}
			if err == nil {
				// The loop is not supposed to return on its own. Back off
				// rather than spin, in case that ever changes.
				log.Println("Daemon returned without an error, wait 2 seconds")
				time.Sleep(2 * time.Second)
				continue
			}

			log.Printf("Error occurred in daemon process (%v), wait 2 seconds to retry or press Ctrl+C to exit.", err.Error())

			broadcast <- model.Message{
				Event: "smc-error",
				Payload: map[string]string{
					"message": fmt.Sprintf("Error occurred in daemon process, %v.", err.Error()),
				},
			}

			time.Sleep(2 * time.Second)
		}
	}()

	<-ctx.Done()
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
func loadConfig(path string) config.Config {
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	switch {
	case err == nil:
		return cfg
	case errors.Is(err, config.ErrNotFound):
		cfg = config.Default()
		if werr := config.Write(path, cfg); werr != nil {
			log.Printf("no config file at %s and none could be written (%v); running on the defaults", path, werr)
		} else {
			log.Printf("no config file at %s, wrote the defaults; hand edits need a restart", path)
		}
		return cfg
	default:
		log.Fatalf("%v", err)
		return config.Config{}
	}
}

// optionsController answers the read-only control commands a client sends.
//
// It lives in the agent rather than in pkg/smc because the broadcast belongs
// to this process, and pkg/smc stays free of the server's concerns. The card
// socket is read-only: options and the reader change through /settings or the
// tray, never over the socket.
type optionsController struct {
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
	case "get-options":
		c.broadcastOptions()
	case "get-status":
		// Answered by the read loop, which owns the reader list and its own
		// state. It re-publishes without changing anything.
		c.sendControl(smc.Control{Kind: smc.ControlReportStatus})
	case "refresh-readers":
		c.sendControl(smc.Control{Kind: smc.ControlRefreshReaders})
	case "read-now":
		// Harmless when no card is in: the loop answers with an error rather
		// than reading nothing silently.
		c.sendControl(smc.Control{Kind: smc.ControlReadNow})
	default:
		c.fail(fmt.Sprintf("unknown action %q", cmd.Action))
	}
}

// sendControl hands a request to the read loop without blocking.
//
// The loop is busy reading a card most of the time it spends awake, and it
// drains this channel between transport waits. Blocking here would instead
// stall the command loop, and with it the server's outbound path. A dropped
// request is recoverable: the client can send it again.
func (c *optionsController) sendControl(cmd smc.Control) {
	if c.control == nil {
		return
	}
	select {
	case c.control <- cmd:
	default:
		log.Printf("dropping a reader control request, the read loop is busy")
	}
}

// broadcastOptions answers with the options in force, so a page that connects
// late learns the current state instead of assuming its own.
func (c *optionsController) broadcastOptions() {
	current := c.store.Get()
	c.send(model.Message{
		Event:   "smc-options",
		Payload: toModelOptions(current),
	})
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
	c.broadcast <- msg
}

// toModelOptions copies the card options onto the wire type, so the two
// definitions cannot drift apart silently.
func toModelOptions(opts smc.Options) model.Options {
	return model.Options{
		ShowFaceImage: opts.ShowFaceImage,
		ShowNhsoData:  opts.ShowNhsoData,
		ShowLaserData: opts.ShowLaserData,
	}
}

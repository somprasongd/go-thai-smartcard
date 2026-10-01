package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/server"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

func main() {
	port := util.GetEnv("SMC_AGENT_PORT", "9898")
	showImage := util.GetEnvBool("SMC_SHOW_IMAGE", true)
	showLaser := util.GetEnvBool("SMC_SHOW_LASER", true)
	showNhso := util.GetEnvBool("SMC_SHOW_NHSO", false)
	// On by default so the page's toggles work without extra setup, but see
	// the warning below before leaving it that way.
	allowRemoteOptions := util.GetEnvBool("SMC_ALLOW_REMOTE_OPTIONS", true)

	broadcast := make(chan model.Message)
	// Buffered because a page asks for the options and the status back to back
	// on connect. pkg/server drops a command it cannot hand over immediately
	// rather than stall a connection, so on an unbuffered channel a burst of two
	// loses the second one and the page starts with its reader list empty.
	command := make(chan model.Command, 16)
	// Buffered so a burst of page actions does not have to wait for the read
	// loop to finish a card before it is even offered.
	control := make(chan smc.Control, 8)

	serverCfg := server.ServerConfig{
		Broadcast: broadcast,
		Command:   command,
		Port:      port,
	}
	go server.Serve(serverCfg)

	opts := &smc.Options{
		ShowFaceImage: showImage,
		ShowNhsoData:  showNhso,
		ShowLaserData: showLaser,
	}
	// The daemon re-reads this store on every card insert, which is what lets
	// the page change what the next read covers.
	store := smc.NewOptionsStore(*opts)
	controller := &optionsController{
		store:       store,
		broadcast:   broadcast,
		allowRemote: allowRemoteOptions,
		control:     control,
	}

	if allowRemoteOptions {
		log.Println("WARNING: SMC_ALLOW_REMOTE_OPTIONS is on, so a page served by this agent can change which applets are read while it runs.")
		log.Println("WARNING: this agent serves its page with permissive CORS and binds every interface, so anything on the same network that can open a socket to it can send set-options and then read the name, address, ID number and face image the page shows.")
		log.Println("WARNING: set SMC_ALLOW_REMOTE_OPTIONS=false to pin the options to SMC_SHOW_IMAGE, SMC_SHOW_LASER and SMC_SHOW_NHSO for this process.")
	}

	// Shut the read loop down cleanly on Ctrl+C, so the card and the PC/SC
	// context are released rather than abandoned.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
				Broadcast:     broadcast,
				Options:       store,
				Control:       control,
				RemoteControl: func() bool { return allowRemoteOptions },
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

// optionsPayload is the smc-options broadcast.
//
// remote_control is not part of the card options: it tells the page whether its
// toggles are wired to the agent, so a page that gets a false can disable them
// rather than let the operator press a switch that does nothing.
type optionsPayload struct {
	model.Options
	RemoteControl bool `json:"remote_control"`
}

// optionsController applies the control commands the page sends.
//
// It lives in the agent rather than in pkg/smc because the gate and the
// broadcast belong to this process, and pkg/smc stays free of the server's
// concerns.
type optionsController struct {
	store       *smc.OptionsStore
	broadcast   chan model.Message
	allowRemote bool
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
// already has; an action it does not know is an error, never a panic.
func (c *optionsController) handle(cmd model.Command) {
	switch cmd.Action {
	case "set-options":
		if !c.gate("set-options") {
			return
		}
		if cmd.Options == nil {
			c.fail("set-options needs an options object")
			return
		}
		c.store.Set(smc.Options{
			ShowFaceImage: cmd.Options.ShowFaceImage,
			ShowNhsoData:  cmd.Options.ShowNhsoData,
			ShowLaserData: cmd.Options.ShowLaserData,
		})
		log.Printf("options set: face image %v, nhso %v, laser %v",
			cmd.Options.ShowFaceImage, cmd.Options.ShowNhsoData, cmd.Options.ShowLaserData)
		c.broadcastOptions()
	case "get-options":
		c.broadcastOptions()
	case "get-status":
		// Answered by the read loop, which owns the reader list and its own
		// state. It re-publishes without changing anything.
		c.sendControl(smc.Control{Kind: smc.ControlReportStatus})
	case "set-reader":
		if !c.gate("set-reader") {
			return
		}
		// An empty name is a request to watch every reader, which is the
		// state the agent starts in.
		c.sendControl(smc.Control{Kind: smc.ControlSelectReader, Reader: cmd.Reader})
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

// gate reports whether a command that changes the agent's behaviour is
// allowed, saying so when it is not.
func (c *optionsController) gate(action string) bool {
	if c.allowRemote {
		return true
	}
	c.fail(fmt.Sprintf("%s is disabled, set SMC_ALLOW_REMOTE_OPTIONS=true to allow it", action))
	return false
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
		Event: "smc-options",
		Payload: optionsPayload{
			Options:       toModelOptions(current),
			RemoteControl: c.allowRemote,
		},
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

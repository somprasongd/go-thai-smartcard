package server

import (
	"encoding/json"
	"log"

	socketio "github.com/somprasongd/go-socketio-v4"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

// commandEvent is the socket.io event a client emits to reach the agent's
// options. It matches the raw WebSocket path so the page needs one name for
// both transports.
const commandEvent = "smc-command"

type socketIO struct {
	*socketio.Server
	ns      *socketio.Namespace
	command chan model.Command
}

// NewSocketIO returns the socket.io transport, speaking the socket.io v4
// protocol on top of the go-socketio-v4 engine. command may be nil, which
// leaves the control channel off. Origin and token policy live in the
// socket guard that wraps this handler.
func NewSocketIO(command chan model.Command) *socketIO {
	srv := socketio.New(nil)
	ns := srv.DefaultNamespace()
	s := &socketIO{Server: srv, ns: ns, command: command}

	// The connection is unused: the agent has one set of options, so a
	// command is not scoped to the client that sent it. A malformed frame is
	// logged and dropped here, and the agent is never asked to answer it.
	ns.OnEvent(commandEvent, func(c *socketio.Socket, args []any, ack func(response ...any)) {
		if len(args) == 0 {
			log.Printf("socket.io %s: empty payload", commandEvent)
			return
		}
		// The v4 parser hands the argument over decoded; the shared command
		// decoder wants the JSON bytes, object or quoted-string alike.
		payload, err := json.Marshal(args[0])
		if err != nil {
			log.Printf("socket.io %s: %v", commandEvent, err)
			return
		}
		s.commandWithReply(payload, func(msg model.Message) { _ = c.Emit(msg.Event, msg.Payload) })
	})

	ns.OnConnect(func(c *socketio.Socket) {
		log.Println("connected:", c.ID())
	})

	ns.OnDisconnect(func(c *socketio.Socket, reason string) {
		log.Println("closed", reason)
	})
	return s
}

// closeConnections retires every live session: parked polls go out with the
// close packet and WebSocket clients see the disconnect.
func (s *socketIO) closeConnections() {
	s.EngineIO().Close()
}

func (s *socketIO) Broadcast(msg model.Message) {
	s.ns.Emit(msg.Event, msg.Payload)
}

// commandWithReply decodes an inbound smc-command and forwards it to the
// agent, handing the reply back as the given event.
func (s *socketIO) commandWithReply(payload json.RawMessage, send func(model.Message)) {
	cmd, err := decodeCommand(payload)
	if err != nil {
		log.Printf("socket.io %s: %v", commandEvent, err)
		return
	}
	bindCommandReply(&cmd, send)
	forwardCommand(s.command, cmd)
}

package server

import (
	"encoding/json"
	"log"
	"net/http"

	socketio "github.com/googollee/go-socket.io"
	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

// commandEvent is the socket.io event a client emits to reach the agent's
// options. It matches the raw WebSocket path so the page needs one name for
// both transports.
const commandEvent = "smc-command"

// Easier to get running with CORS.
var allowOriginFunc = func(r *http.Request) bool {
	return true
}

type socketIO struct {
	*socketio.Server
	command chan model.Command
}

// NewSocketIO returns the socket.io transport. command may be nil, which
// leaves the control channel off.
func NewSocketIO(command chan model.Command) *socketIO {
	server := socketio.NewServer(&engineio.Options{
		Transports: []transport.Transport{
			&polling.Transport{
				CheckOrigin: allowOriginFunc,
			},
			&websocket.Transport{
				CheckOrigin: allowOriginFunc,
			},
		},
	})
	srv := &socketIO{Server: server, command: command}
	// The connection is unused: the agent has one set of options, so a
	// command is not scoped to the client that sent it. A malformed frame is
	// logged and dropped here, and the agent is never asked to answer it.
	server.OnEvent("/", commandEvent, func(_ socketio.Conn, payload json.RawMessage) {
		srv.onCommand(payload)
	})

	server.OnConnect("/", func(s socketio.Conn) error {
		s.SetContext("")
		log.Println("connected:", s.ID())
		return nil
	})

	server.OnError("/", func(s socketio.Conn, e error) {
		log.Println("meet error:", e)
	})

	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		log.Println("closed", reason)
	})
	return srv
}

func (s *socketIO) Broadcast(msg model.Message) {
	s.BroadcastToNamespace("/", msg.Event, msg.Payload)
}

// onCommand handles an inbound smc-command event.
func (s *socketIO) onCommand(payload json.RawMessage) {
	cmd, err := decodeCommand(payload)
	if err != nil {
		log.Printf("socket.io %s: %v", commandEvent, err)
		return
	}
	forwardCommand(s.command, cmd)
}

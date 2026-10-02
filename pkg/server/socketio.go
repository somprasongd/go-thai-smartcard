package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"

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
	mu      sync.Mutex
	clients map[string]socketio.Conn
	closed  bool
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
	srv := &socketIO{Server: server, command: command, clients: make(map[string]socketio.Conn)}
	// The connection is unused: the agent has one set of options, so a
	// command is not scoped to the client that sent it. A malformed frame is
	// logged and dropped here, and the agent is never asked to answer it.
	server.OnEvent("/", commandEvent, func(c socketio.Conn, payload json.RawMessage) {
		srv.commandWithReply(payload, func(msg model.Message) { c.Emit(msg.Event, msg.Payload) })
	})

	server.OnConnect("/", func(s socketio.Conn) error {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		if srv.closed {
			return errors.New("listener retired")
		}
		srv.clients[s.ID()] = s
		s.SetContext("")
		log.Println("connected:", s.ID())
		return nil
	})

	server.OnError("/", func(s socketio.Conn, e error) {
		log.Println("meet error:", e)
	})

	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		srv.mu.Lock()
		delete(srv.clients, s.ID())
		srv.mu.Unlock()
		log.Println("closed", reason)
	})
	return srv
}

func (s *socketIO) closeConnections() {
	s.mu.Lock()
	s.closed = true
	clients := make([]socketio.Conn, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
	_ = s.Server.Close()
}

func (s *socketIO) Broadcast(msg model.Message) {
	s.BroadcastToNamespace("/", msg.Event, msg.Payload)
}

// onCommand handles an inbound smc-command event.
func (s *socketIO) onCommand(payload json.RawMessage) {
	s.commandWithReply(payload, func(model.Message) {})
}

func (s *socketIO) commandWithReply(payload json.RawMessage, send func(model.Message)) {
	cmd, err := decodeCommand(payload)
	if err != nil {
		log.Printf("socket.io %s: %v", commandEvent, err)
		return
	}
	bindCommandReply(&cmd, send)
	forwardCommand(s.command, cmd)
}

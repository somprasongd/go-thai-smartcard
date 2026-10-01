package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	// pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	// pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	// maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// func getCmd(input string) string {
// 	inputArr := strings.Split(input, " ")
// 	return inputArr[0]
// }

// func getMessage(input string) string {
// 	inputArr := strings.Split(input, " ")
// 	var result string
// 	for i := 1; i < len(inputArr); i++ {
// 		result += inputArr[i]
// 	}
// 	return result
// }

type connection struct {
	// The websocket connection.
	ws *websocket.Conn
}

func (c *connection) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(mt, payload)
}

// subscriber is touched from two sides at once: every connection's handler
// goroutine registers and unregisters it, while the broadcast goroutine walks
// it on each card event. An unguarded map there is a data race, and Go aborts
// the process on a concurrent map iteration and write, so every access takes mu.
type subscriber struct {
	mu sync.Mutex
	// put registered clients.
	clients map[*connection]bool
}

func (s *subscriber) register(c *connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clients == nil {
		s.clients = make(map[*connection]bool)
	}
	s.clients[c] = true
}

func (s *subscriber) unregister(c *connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clients != nil {
		delete(s.clients, c)
	}
}

// snapshot returns the clients registered now. Broadcast writes to them after
// the lock is released, so a slow peer cannot hold up a page that is connecting.
func (s *subscriber) snapshot() []*connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	conns := make([]*connection, 0, len(s.clients))
	for c := range s.clients {
		conns = append(conns, c)
	}
	return conns
}

type ws struct {
	subscriber
	command chan model.Command
}

// NewWS returns the raw WebSocket transport. command may be nil, which leaves
// the control channel off.
func NewWS(command chan model.Command) *ws {
	s := ws{command: command}
	return &s
}

func (s *ws) Handler(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	c := &connection{ws: ws}
	s.subscriber.register(c)

	defer ws.Close()
	// ws.SetReadLimit(maxMessageSize)
	// ws.SetReadDeadline(time.Now().Add(pongWait))
	// ws.SetPongHandler(func(string) error { ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	// Continuosly read and write message
	for {
		mt, message, err := ws.ReadMessage()
		if err != nil {
			log.Println("read failed:", err)
			s.subscriber.unregister(c)
			break
		}
		// Only a JSON text frame is part of the control contract. Anything
		// else is ignored rather than closed on, so a page that still sends
		// a stray binary frame keeps its broadcast connection.
		if mt != websocket.TextMessage {
			log.Printf("ignoring binary websocket frame of %d bytes", len(message))
			continue
		}
		cmd, err := decodeCommand(message)
		if err != nil {
			log.Println("websocket command:", err)
			continue
		}
		forwardCommand(s.command, cmd)
	}
}

func (s *ws) Broadcast(msg model.Message) {
	m, _ := json.Marshal(msg)
	for _, c := range s.subscriber.snapshot() {
		c.write(websocket.TextMessage, m)
	}
}

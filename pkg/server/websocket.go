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
	writeWait         = 10 * time.Second
	pongWait          = 60 * time.Second
	pingPeriod        = pongWait * 9 / 10
	maxMessageSize    = 4096
	outboundQueueSize = 16
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type connection struct {
	ws       *websocket.Conn
	send     chan []byte
	done     chan struct{}
	stopOnce sync.Once
}

func (c *connection) stop() {
	c.stopOnce.Do(func() {
		close(c.done)
		if c.ws != nil {
			_ = c.ws.Close()
		}
	})
}

// One writer owns data, ping and deadlines. A full bounded queue retires only
// this peer, rather than blocking card delivery to every other subscriber.
func (c *connection) enqueue(payload []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- payload:
		return true
	default:
		c.stop()
		return false
	}
}

func (c *connection) writeLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer c.stop()
	for {
		var kind int
		var payload []byte
		select {
		case <-c.done:
			return
		case payload = <-c.send:
			kind = websocket.TextMessage
		case <-ticker.C:
			kind = websocket.PingMessage
		}
		if err := c.ws.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			return
		}
		if err := c.ws.WriteMessage(kind, payload); err != nil {
			return
		}
	}
}

// subscriber is touched from two sides at once: every connection's handler
// goroutine registers and unregisters it, while the broadcast goroutine walks
// it on each card event. An unguarded map there is a data race, and Go aborts
// the process on a concurrent map iteration and write, so every access takes mu.
type subscriber struct {
	mu sync.Mutex
	// put registered clients.
	clients map[*connection]bool
	closed  bool
}

func (s *subscriber) register(c *connection) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		c.stop()
		return false
	}
	if s.clients == nil {
		s.clients = make(map[*connection]bool)
	}
	s.clients[c] = true
	return true
}

func (s *ws) closeConnections() {
	s.mu.Lock()
	s.closed = true
	clients := make([]*connection, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		c.stop()
	}
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
	c := &connection{ws: ws, send: make(chan []byte, outboundQueueSize), done: make(chan struct{})}
	if !s.subscriber.register(c) {
		return
	}

	defer c.stop()
	defer s.subscriber.unregister(c)
	go c.writeLoop()
	ws.SetReadLimit(maxMessageSize)
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(pongWait)) })
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
		bindCommandReply(&cmd, func(msg model.Message) {
			raw, err := json.Marshal(msg)
			if err == nil {
				c.enqueue(raw)
			}
		})
		forwardCommand(s.command, cmd)
	}
}

func (s *ws) Broadcast(msg model.Message) {
	m, _ := json.Marshal(msg)
	for _, c := range s.subscriber.snapshot() {
		if !c.enqueue(m) {
			s.subscriber.unregister(c)
		}
	}
}

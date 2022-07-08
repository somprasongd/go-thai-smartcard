package ws

import (
	"bytes"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Hub maintains the set of active clients and broadcasts messages to the
type hub struct {
	// put registered clients.
	clients map[*connection]bool
	// Inbound messages from the clients.
	Broadcast chan Message

	// Register requests from the clients.
	register chan subscription

	// Unregister requests from clients.
	unregister chan subscription
}

type Message struct {
	Event   string `json:"event"`
	Payload any    `json:"payload,omitempty"`
}

var Hub = &hub{
	Broadcast:  make(chan Message),
	register:   make(chan subscription),
	unregister: make(chan subscription),
	clients:    make(map[*connection]bool),
}

func (h *hub) Run() {
	for {
		select {
		case s := <-h.register:
			connections := h.clients
			if connections == nil {
				connections = make(map[*connection]bool)
				h.clients = connections
			}
			h.clients[s.conn] = true
		case s := <-h.unregister:
			connections := h.clients
			if connections != nil {
				if _, ok := connections[s.conn]; ok {
					delete(connections, s.conn)
					close(s.conn.send)
				}
			}
		case m := <-h.Broadcast:
			connections := h.clients
			for c := range connections {
				select {
				case c.send <- m:
				default:
					close(c.send)
					delete(connections, c)
				}
			}
		}
	}
}

const (

	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type subscription struct {
	conn *connection
}

type connection struct {
	// The websocket connection.
	ws *websocket.Conn
	// Buffered channel of outbound messages.
	send chan Message
}

func (s *subscription) readPump() {
	c := s.conn
	defer func() {
		//Unregister
		Hub.unregister <- *s
		c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		// Reading incoming message...
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Printf("error: %v", err)
			}
			break
		}
		msg = bytes.TrimSpace(bytes.Replace(msg, newline, space, -1))
		log.Println("Receive message:", string(msg))
	}
}
func (s *subscription) writePump() {
	c := s.conn
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		// Listerning message when it comes will write it into writer and then send it to the client
		case message, ok := <-c.send:
			if !ok {
				c.write(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.ws.WriteJSON(message); err != nil {
				return
			}
			// if err := c.write(websocket.TextMessage, message); err != nil {
			// 	return
			// }
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}
func (c *connection) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(mt, payload)
}
func HandleWs(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	c := &connection{send: make(chan Message, 256), ws: ws}
	s := subscription{c}
	Hub.register <- s
	go s.writePump()
	go s.readPump()
}

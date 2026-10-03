package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

// The shapes a client may send, over either transport. set-options and
// set-reader are no longer part of the contract; a client that still sends
// them decodes to the action name alone and is answered with an error, which
// is what the last test pins down.
const (
	legacySetOptions = `{"action":"set-options","options":{"show_face_image":true,"show_nhso":false,"show_laser":true}}`
	getStatusJSON    = `{"action":"get-status"}`
	quotedSetOption  = `"{\"action\":\"set-options\",\"options\":{\"show_face_image\":true,\"show_nhso\":false,\"show_laser\":true}}"`
)

func TestDecodeCommand(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    model.Command
		wantErr bool
	}{
		{
			name:    "the get-status shape from the contract",
			payload: getStatusJSON,
			want:    model.Command{Action: "get-status"},
		},
		{
			// A removed action still decodes: the agent, not the decoder,
			// answers it with the unknown-action error.
			name:    "a removed action decodes to its name alone",
			payload: legacySetOptions,
			want:    model.Command{Action: "set-options"},
		},
		{
			name:    "the same object wrapped in a JSON string, as socket.io delivers it",
			payload: quotedSetOption,
			want:    model.Command{Action: "set-options"},
		},
		{
			name:    "surrounding whitespace is tolerated",
			payload: "  \n" + getStatusJSON + "\t",
			want:    model.Command{Action: "get-status"},
		},
		{
			// The agent rejects an unknown action with an error broadcast;
			// decoding only has to hand it over untouched.
			name:    "an unknown action decodes and is left for the agent",
			payload: `{"action":"drop-everything"}`,
			want:    model.Command{Action: "drop-everything"},
		},
		{
			name:    "a JSON array is not a command",
			payload: `[{"action":"get-options"}]`,
			wantErr: true,
		},
		{
			name:    "a non-JSON frame is rejected rather than half read",
			payload: `set-options show_face_image=true`,
			wantErr: true,
		},
		{
			name:    "an empty frame is rejected",
			payload: "",
			wantErr: true,
		},
		{
			name:    "a truncated string is rejected",
			payload: `"{"action":"get-options"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeCommand([]byte(tt.payload))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("decodeCommand(%q) = %+v, want an error", tt.payload, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeCommand(%q): %v", tt.payload, err)
			}
			if got.Action != tt.want.Action {
				t.Errorf("Action = %q, want %q", got.Action, tt.want.Action)
			}
		})
	}
}

// A slow consumer must not be able to wedge the read loop of a connection, so
// the send gives up instead of waiting.
func TestForwardCommandDoesNotBlock(t *testing.T) {
	tests := []struct {
		name    string
		command chan model.Command
		want    bool
	}{
		{name: "a nil channel drops the command", command: nil, want: false},
		{name: "an unread channel drops the command", command: make(chan model.Command), want: false},
		{name: "a waiting consumer receives it", command: make(chan model.Command, 1), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := model.Command{Action: "get-status"}

			dropped := make(chan struct{})
			go func() {
				forwardCommand(tt.command, cmd)
				close(dropped)
			}()

			select {
			case <-dropped:
			case <-time.After(2 * time.Second):
				t.Fatal("forwardCommand blocked on a consumer that is not reading")
			}

			if !tt.want {
				if len(tt.command) != 0 {
					t.Error("a command was queued although the test expects it to be dropped")
				}
				return
			}
			if got := <-tt.command; got.Action != cmd.Action {
				t.Errorf("forwarded %+v, want %+v", got, cmd)
			}
		})
	}
}

// readFrameUntil keeps reading until a frame with the wanted prefix shows
// up. Under a loaded CI the server's reply can land seconds after the write,
// and nothing guarantees which frame a bare client sees first. A timeout
// dumps every goroutine stack, so a stall points at its holder.
func readFrameUntil(t *testing.T, c *websocket.Conn, prefix string, budget time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		if !time.Now().Before(deadline) {
			t.Fatalf("no frame with prefix %q within %s\n\n%s", prefix, budget, allStacks())
		}
		_ = c.SetReadDeadline(deadline)
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read while waiting for %q: %v\n\n%s", prefix, err, allStacks())
		}
		if strings.HasPrefix(string(data), prefix) {
			return string(data)
		}
	}
}

func allStacks() string {
	buf := make([]byte, 4<<20)
	n := runtime.Stack(buf, true)
	return string(buf[:n])
}

// connectSocketIOClient walks one raw client through the v4 handshake —
// engine.io OPEN, then the socket.io namespace connect — and returns it
// ready to emit event frames. The transport under test is our own library
// now, so exercising the wire is exactly the point.
func connectSocketIOClient(t *testing.T, h *httptest.Server) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/socket.io/?EIO=4&transport=websocket", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	readFrameUntil(t, c, "0", 20*time.Second)
	if err := c.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		t.Fatalf("namespace connect: %v", err)
	}
	readFrameUntil(t, c, "40", 20*time.Second)
	return c
}

func TestSocketIOInboundCommand(t *testing.T) {
	tests := []struct {
		name     string
		frame    string // the full wire frame, sent once the namespace is joined
		command  chan model.Command
		wantRecv bool
		want     model.Command
	}{
		{
			name:     "a get-status event reaches the agent",
			frame:    `42["smc-command",` + getStatusJSON + `]`,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want:     model.Command{Action: "get-status"},
		},
		{
			name:     "a legacy set-options event decodes to the action name",
			frame:    `42["smc-command",` + legacySetOptions + `]`,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want:     model.Command{Action: "set-options"},
		},
		{
			name:     "a JSON encoded event payload reaches the agent",
			frame:    `42["smc-command",` + quotedSetOption + `]`,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want:     model.Command{Action: "set-options"},
		},
		{
			name:     "a malformed event is dropped without reaching the agent",
			frame:    `42["smc-command","not json at all"]`,
			command:  make(chan model.Command, 1),
			wantRecv: false,
		},
		{
			name:     "an event without a payload is dropped",
			frame:    `42["smc-command"]`,
			command:  make(chan model.Command, 1),
			wantRecv: false,
		},
		{
			name:     "a well formed event is dropped when no channel is wired",
			frame:    `42["smc-command",` + getStatusJSON + `]`,
			command:  nil,
			wantRecv: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSocketIO(tt.command)
			defer s.closeConnections() // retire sessions before the server goes away
			srv := httptest.NewServer(s)
			defer srv.Close()

			c := connectSocketIOClient(t, srv)
			if err := c.WriteMessage(websocket.TextMessage, []byte(tt.frame)); err != nil {
				t.Fatalf("send event: %v", err)
			}

			if !tt.wantRecv {
				select {
				case got := <-tt.command:
					t.Fatalf("the agent received %+v, want nothing", got)
				case <-time.After(200 * time.Millisecond):
				}
				// The connection has to survive the ignored frame: a dropped
				// command must not cost the page its broadcasts.
				if err := c.WriteMessage(websocket.TextMessage, []byte(tt.frame)); err != nil {
					t.Errorf("the connection did not survive the ignored frame: %v", err)
				}
				return
			}
			select {
			case got := <-tt.command:
				if got.Action != tt.want.Action {
					t.Errorf("Action = %q, want %q", got.Action, tt.want.Action)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the command never reached the agent")
			}
		})
	}
}

// The WebSocket path is exercised end to end, because the read loop is where
// the frame type and the read loop's own failure handling live.
func TestWebSocketInboundCommand(t *testing.T) {
	tests := []struct {
		name     string
		mt       int
		payload  string
		command  chan model.Command
		wantRecv bool
		want     model.Command
	}{
		{
			name:     "a text frame reaches the agent",
			mt:       websocket.TextMessage,
			payload:  getStatusJSON,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want:     model.Command{Action: "get-status"},
		},
		{
			name:     "a legacy set-options frame decodes to the action name",
			mt:       websocket.TextMessage,
			payload:  legacySetOptions,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want:     model.Command{Action: "set-options"},
		},
		{
			name:     "a binary frame is ignored",
			mt:       websocket.BinaryMessage,
			payload:  getStatusJSON,
			command:  make(chan model.Command, 1),
			wantRecv: false,
		},
		{
			name:     "a malformed text frame is dropped",
			mt:       websocket.TextMessage,
			payload:  "{not json",
			command:  make(chan model.Command, 1),
			wantRecv: false,
		},
		{
			name:     "a command with no channel wired does not panic",
			mt:       websocket.TextMessage,
			payload:  getStatusJSON,
			command:  nil,
			wantRecv: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewWS(tt.command)
			srv := httptest.NewServer(http.HandlerFunc(s.Handler))
			defer srv.Close()

			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer client.Close()

			if err := client.WriteMessage(tt.mt, []byte(tt.payload)); err != nil {
				t.Fatalf("write: %v", err)
			}

			if tt.wantRecv {
				select {
				case got := <-tt.command:
					if got.Action != tt.want.Action {
						t.Errorf("Action = %q, want %q", got.Action, tt.want.Action)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("the command never reached the agent")
				}
				return
			}

			// Nothing should arrive, but the connection has to survive it:
			// a dropped frame must not cost the page its broadcasts.
			select {
			case got := <-tt.command:
				t.Fatalf("the agent received %+v, want nothing", got)
			case <-time.After(200 * time.Millisecond):
			}
			if err := client.WriteMessage(websocket.TextMessage, []byte(getStatusJSON)); err != nil {
				t.Errorf("the connection did not survive the ignored frame: %v", err)
			}
		})
	}
}

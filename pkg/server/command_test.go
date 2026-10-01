package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

// The two shapes the page may send, over either transport.
const (
	setOptionsJSON  = `{"action":"set-options","options":{"show_face_image":true,"show_nhso":false,"show_laser":true}}`
	getOptionsJSON  = `{"action":"get-options"}`
	quotedSetOption = `"{\"action\":\"set-options\",\"options\":{\"show_face_image\":true,\"show_nhso\":false,\"show_laser\":true}}"`
)

func TestDecodeCommand(t *testing.T) {
	wantOptions := model.Options{ShowFaceImage: true, ShowLaserData: true}

	tests := []struct {
		name    string
		payload string
		want    model.Command
		wantErr bool
	}{
		{
			name:    "the set-options shape from the contract",
			payload: setOptionsJSON,
			want: model.Command{
				Action:  "set-options",
				Options: &wantOptions,
			},
		},
		{
			name:    "the same object wrapped in a JSON string, as socket.io delivers it",
			payload: quotedSetOption,
			want: model.Command{
				Action:  "set-options",
				Options: &wantOptions,
			},
		},
		{
			name:    "get-options carries no options",
			payload: getOptionsJSON,
			want:    model.Command{Action: "get-options"},
		},
		{
			name:    "surrounding whitespace is tolerated",
			payload: "  \n" + getOptionsJSON + "\t",
			want:    model.Command{Action: "get-options"},
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
			switch {
			case tt.want.Options == nil && got.Options != nil:
				t.Errorf("Options = %+v, want none", got.Options)
			case tt.want.Options != nil && got.Options == nil:
				t.Errorf("Options is nil, want %+v", tt.want.Options)
			case tt.want.Options != nil && *got.Options != *tt.want.Options:
				t.Errorf("Options = %+v, want %+v", *got.Options, *tt.want.Options)
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
			cmd := model.Command{Action: "get-options"}

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

func TestSocketIOInboundCommand(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		command  chan model.Command
		wantRecv bool
		want     model.Command
	}{
		{
			name:     "a set-options event reaches the agent",
			payload:  setOptionsJSON,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want: model.Command{
				Action:  "set-options",
				Options: &model.Options{ShowFaceImage: true, ShowLaserData: true},
			},
		},
		{
			name:     "a JSON encoded event payload reaches the agent",
			payload:  quotedSetOption,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want: model.Command{
				Action:  "set-options",
				Options: &model.Options{ShowFaceImage: true, ShowLaserData: true},
			},
		},
		{
			name:     "a get-options event reaches the agent",
			payload:  getOptionsJSON,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want:     model.Command{Action: "get-options"},
		},
		{
			name:     "a malformed event is dropped without reaching the agent",
			payload:  "not json at all",
			command:  make(chan model.Command, 1),
			wantRecv: false,
		},
		{
			name:     "a well formed event is dropped when no channel is wired",
			payload:  getOptionsJSON,
			command:  nil,
			wantRecv: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSocketIO(tt.command)
			defer s.Close()

			// The registered handler is called directly: a full engine.io
			// handshake would only prove the third party library still works.
			s.onCommand(json.RawMessage(tt.payload))

			if !tt.wantRecv {
				if len(tt.command) != 0 {
					t.Fatalf("the agent received %+v, want nothing", <-tt.command)
				}
				return
			}
			select {
			case got := <-tt.command:
				if got.Action != tt.want.Action {
					t.Errorf("Action = %q, want %q", got.Action, tt.want.Action)
				}
				if tt.want.Options != nil && (got.Options == nil || *got.Options != *tt.want.Options) {
					t.Errorf("Options = %+v, want %+v", got.Options, tt.want.Options)
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
			payload:  setOptionsJSON,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want: model.Command{
				Action:  "set-options",
				Options: &model.Options{ShowFaceImage: true, ShowLaserData: true},
			},
		},
		{
			name:     "a get-options text frame reaches the agent",
			mt:       websocket.TextMessage,
			payload:  getOptionsJSON,
			command:  make(chan model.Command, 1),
			wantRecv: true,
			want:     model.Command{Action: "get-options"},
		},
		{
			name:     "a binary frame is ignored",
			mt:       websocket.BinaryMessage,
			payload:  setOptionsJSON,
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
			payload:  getOptionsJSON,
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
					if tt.want.Options != nil && (got.Options == nil || *got.Options != *tt.want.Options) {
						t.Errorf("Options = %+v, want %+v", got.Options, tt.want.Options)
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
			if err := client.WriteMessage(websocket.TextMessage, []byte(getOptionsJSON)); err != nil {
				t.Errorf("the connection did not survive the ignored frame: %v", err)
			}
		})
	}
}

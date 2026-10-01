package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
)

// newTestController wires a controller onto a buffered broadcast channel, so a
// test can call handle directly and read the answer without a reader running.
func newTestController(seed smc.Options) (*optionsController, chan model.Message) {
	broadcast := make(chan model.Message, 8)
	return &optionsController{
		store:     smc.NewOptionsStore(seed),
		broadcast: broadcast,
	}, broadcast
}

// answers drains every broadcast the controller made.
func answers(broadcast chan model.Message) []model.Message {
	var got []model.Message
	for {
		select {
		case msg := <-broadcast:
			got = append(got, msg)
		default:
			return got
		}
	}
}

// The card socket is read-only: what the agent reads and which reader it
// watches change through /settings or the tray. The removed write actions land
// in the unknown-action path, with an error that says so.
func TestOptionsControllerIsReadOnly(t *testing.T) {
	seed := smc.Options{ShowFaceImage: true, ShowLaserData: true}

	tests := []struct {
		name        string
		cmd         model.Command
		wantMessage string
	}{
		{
			name:        "set-options is gone",
			cmd:         model.Command{Action: "set-options"},
			wantMessage: `unknown action "set-options"`,
		},
		{
			name:        "set-reader is gone",
			cmd:         model.Command{Action: "set-reader"},
			wantMessage: `unknown action "set-reader"`,
		},
		{
			name:        "an unknown action is an error, not a panic",
			cmd:         model.Command{Action: "delete-everything"},
			wantMessage: `unknown action "delete-everything"`,
		},
		{
			name:        "a missing action is an error",
			cmd:         model.Command{},
			wantMessage: `unknown action ""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, broadcast := newTestController(seed)

			c.handle(tt.cmd)

			if got := c.store.Get(); got != seed {
				t.Errorf("store = %+v, want it untouched at %+v", got, seed)
			}
			got := answers(broadcast)
			if len(got) != 1 {
				t.Fatalf("got %d broadcasts, want 1: %+v", len(got), got)
			}
			if got[0].Event != "smc-error" {
				t.Errorf("event = %q, want smc-error", got[0].Event)
			}
			payload, ok := got[0].Payload.(map[string]string)
			if !ok {
				t.Fatalf("payload = %T, want map[string]string", got[0].Payload)
			}
			if !strings.Contains(payload["message"], tt.wantMessage) {
				t.Errorf("message = %q, want it to mention %q", payload["message"], tt.wantMessage)
			}
		})
	}
}

// get-options answers with what is in force, so a page that connects late
// learns the current state instead of guessing.
func TestOptionsControllerGetOptions(t *testing.T) {
	c, broadcast := newTestController(smc.Options{ShowFaceImage: true, ShowLaserData: true})

	c.handle(model.Command{Action: "get-options"})

	got := answers(broadcast)
	if len(got) != 1 {
		t.Fatalf("got %d broadcasts, want 1: %+v", len(got), got)
	}
	if got[0].Event != "smc-options" {
		t.Errorf("event = %q, want smc-options", got[0].Event)
	}
	// The page is written against this payload, so pin it byte for byte. There
	// is no remote_control any more: the socket is read-only, always.
	payload, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wantJSON := `{"event":"smc-options","payload":{"show_face_image":true,"show_nhso":false,"show_laser":true}}`
	if string(payload) != wantJSON {
		t.Errorf("payload =\n%s\nwant\n%s", payload, wantJSON)
	}
}

func TestOptionsControllerRun(t *testing.T) {
	c, broadcast := newTestController(smc.Options{})
	command := make(chan model.Command, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		c.run(ctx, command)
		close(done)
	}()

	command <- model.Command{Action: "get-options"}

	deadline := time.After(2 * time.Second)
	for len(answers(broadcast)) == 0 {
		select {
		case <-deadline:
			t.Fatal("the command was never answered")
		case <-time.After(5 * time.Millisecond):
		}
	}

	close(command)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return when the command channel closed")
	}

	// And the context ends it too.
	command = make(chan model.Command, 1)
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	go func() {
		c.run(ctx2, command)
		close(done2)
	}()
	cancel2()
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return when the process shut down")
	}
}

// Reader and re-read requests go to the read loop, not to the options store:
// only the loop knows which readers are attached and whether a card is in one.
// Narrowing the watch is not offered over the socket any more — only the
// harmless requests are forwarded.
func TestOptionsControllerForwardsReaderControl(t *testing.T) {
	tests := []struct {
		name string
		cmd  model.Command
		want smc.Control
	}{
		{
			name: "refresh-readers asks the loop to re-list",
			cmd:  model.Command{Action: "refresh-readers"},
			want: smc.Control{Kind: smc.ControlRefreshReaders},
		},
		{
			name: "read-now asks for the inserted card again",
			cmd:  model.Command{Action: "read-now"},
			want: smc.Control{Kind: smc.ControlReadNow},
		},
		{
			name: "get-status re-publishes without changing anything",
			cmd:  model.Command{Action: "get-status"},
			want: smc.Control{Kind: smc.ControlReportStatus},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			control := make(chan smc.Control, 4)
			controller := &optionsController{
				store:     smc.NewOptionsStore(smc.Options{}),
				broadcast: make(chan model.Message, 8),
				control:   control,
			}

			controller.handle(tt.cmd)

			select {
			case got := <-control:
				if got != tt.want {
					t.Errorf("control = %+v, want %+v", got, tt.want)
				}
			default:
				t.Fatal("nothing was forwarded to the read loop")
			}

			// The loop answers these, so the controller must not invent an
			// answer of its own.
			if len(answers(controller.broadcast)) != 0 {
				t.Error("the controller answered a reader request itself")
			}
		})
	}
}

// The read loop spends most of its time inside a card. Blocking the command
// loop there would stall the server's outbound path with it, so a request that
// arrives while the loop is busy is dropped rather than queued without bound.
func TestOptionsControllerDoesNotBlockOnABusyLoop(t *testing.T) {
	// Unbuffered and never read: the send can only succeed if it is dropped.
	control := make(chan smc.Control)
	controller := &optionsController{
		store:     smc.NewOptionsStore(smc.Options{}),
		broadcast: make(chan model.Message, 8),
		control:   control,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		controller.handle(model.Command{Action: "read-now"})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handle blocked on a read loop that is not reading its control channel")
	}
}

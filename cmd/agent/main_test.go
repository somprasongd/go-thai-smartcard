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

const disabledMessage = "set-options is disabled, set SMC_ALLOW_REMOTE_OPTIONS=true to allow it"

// newTestController wires a controller onto a buffered broadcast channel, so a
// test can call handle directly and read the answer without a reader running.
func newTestController(allowRemote bool, seed smc.Options) (*optionsController, chan model.Message) {
	broadcast := make(chan model.Message, 8)
	return &optionsController{
		store:       smc.NewOptionsStore(seed),
		broadcast:   broadcast,
		allowRemote: allowRemote,
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

func TestOptionsControllerSetOptions(t *testing.T) {
	c, broadcast := newTestController(true, smc.Options{ShowLaserData: true})

	c.handle(model.Command{
		Action:  "set-options",
		Options: &model.Options{ShowFaceImage: true, ShowLaserData: false},
	})

	want := smc.Options{ShowFaceImage: true}
	if got := c.store.Get(); got != want {
		t.Errorf("store = %+v, want %+v", got, want)
	}

	got := answers(broadcast)
	if len(got) != 1 {
		t.Fatalf("got %d broadcasts, want 1: %+v", len(got), got)
	}
	if got[0].Event != "smc-options" {
		t.Errorf("event = %q, want smc-options", got[0].Event)
	}
	// The page is written against this payload, so pin it byte for byte.
	payload, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wantJSON := `{"event":"smc-options","payload":{"show_face_image":true,"show_nhso":false,"show_laser":false,"remote_control":true}}`
	if string(payload) != wantJSON {
		t.Errorf("payload =\n%s\nwant\n%s", payload, wantJSON)
	}
}

// get-options answers with what is in force, so a page that connects late or
// after a rejected set-options is not left guessing.
func TestOptionsControllerGetOptions(t *testing.T) {
	tests := []struct {
		name        string
		allowRemote bool
		seed        smc.Options
		wantJSON    string
	}{
		{
			name:        "a gate that is on is reported as such",
			allowRemote: true,
			seed:        smc.Options{ShowFaceImage: true, ShowLaserData: true},
			wantJSON:    `{"event":"smc-options","payload":{"show_face_image":true,"show_nhso":false,"show_laser":true,"remote_control":true}}`,
		},
		{
			name:        "a gate that is off is reported as such",
			allowRemote: false,
			seed:        smc.Options{ShowNhsoData: true},
			wantJSON:    `{"event":"smc-options","payload":{"show_face_image":false,"show_nhso":true,"show_laser":false,"remote_control":false}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, broadcast := newTestController(tt.allowRemote, tt.seed)

			c.handle(model.Command{Action: "get-options"})

			got := answers(broadcast)
			if len(got) != 1 {
				t.Fatalf("got %d broadcasts, want 1: %+v", len(got), got)
			}
			payload, err := json.Marshal(got[0])
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(payload) != tt.wantJSON {
				t.Errorf("payload =\n%s\nwant\n%s", payload, tt.wantJSON)
			}
		})
	}
}

// A rejected command has to leave the options exactly as they were, and say
// why. Anything else lets a page believe it changed what the agent reads.
func TestOptionsControllerRejectsCommands(t *testing.T) {
	seed := smc.Options{ShowFaceImage: true, ShowLaserData: true}

	tests := []struct {
		name        string
		allowRemote bool
		cmd         model.Command
		wantMessage string
	}{
		{
			name:        "set-options is refused when the gate is off",
			allowRemote: false,
			cmd: model.Command{
				Action:  "set-options",
				Options: &model.Options{ShowFaceImage: false},
			},
			wantMessage: disabledMessage,
		},
		{
			name:        "an unknown action is an error, not a panic",
			allowRemote: true,
			cmd:         model.Command{Action: "delete-everything"},
			wantMessage: `unknown action "delete-everything"`,
		},
		{
			name:        "a missing action is an error",
			allowRemote: true,
			cmd:         model.Command{Options: &model.Options{ShowFaceImage: true}},
			wantMessage: `unknown action ""`,
		},
		{
			name:        "set-options without an options object is an error",
			allowRemote: true,
			cmd:         model.Command{Action: "set-options"},
			wantMessage: "set-options needs an options object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, broadcast := newTestController(tt.allowRemote, seed)

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

// A page must still be able to see the options while the gate is off, so it
// can show the current state and disable its toggles.
func TestOptionsControllerGetOptionsWorksWhileTheGateIsOff(t *testing.T) {
	c, broadcast := newTestController(false, smc.Options{ShowFaceImage: true})

	c.handle(model.Command{Action: "get-options"})

	got := answers(broadcast)
	if len(got) != 1 || got[0].Event != "smc-options" {
		t.Fatalf("got %+v, want a single smc-options broadcast", got)
	}
}

func TestOptionsControllerRun(t *testing.T) {
	tests := []struct {
		name     string
		stopWith func(cancel context.CancelFunc, command chan model.Command)
	}{
		{
			name: "the loop ends when the command channel closes",
			stopWith: func(_ context.CancelFunc, command chan model.Command) {
				close(command)
			},
		},
		{
			name: "the loop ends when the process shuts down",
			stopWith: func(cancel context.CancelFunc, _ chan model.Command) {
				cancel()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, broadcast := newTestController(true, smc.Options{})
			command := make(chan model.Command, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan struct{})
			go func() {
				c.run(ctx, command)
				close(done)
			}()

			command <- model.Command{
				Action:  "set-options",
				Options: &model.Options{ShowLaserData: true},
			}

			deadline := time.After(2 * time.Second)
			for !c.store.Get().ShowLaserData {
				select {
				case <-deadline:
					t.Fatal("the command was never applied")
				case <-time.After(5 * time.Millisecond):
				}
			}
			if len(broadcast) == 0 {
				t.Error("the applied options were not broadcast")
			}

			tt.stopWith(cancel, command)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("run did not return")
			}
		})
	}
}

// Reader and re-read requests go to the read loop, not to the options store:
// only the loop knows which readers are attached and whether a card is in one.
func TestOptionsControllerForwardsReaderControl(t *testing.T) {
	tests := []struct {
		name    string
		cmd     model.Command
		want    smc.Control
		wantOut bool
	}{
		{
			name: "set-reader carries the reader name",
			cmd:  model.Command{Action: "set-reader", Reader: "Identiv CLOUD 2700 R"},
			want: smc.Control{Kind: smc.ControlSelectReader, Reader: "Identiv CLOUD 2700 R"},
		},
		{
			name: "an empty reader name means watch them all",
			cmd:  model.Command{Action: "set-reader"},
			want: smc.Control{Kind: smc.ControlSelectReader, Reader: ""},
		},
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
		{
			// The loop answers this one, so the controller must not invent an
			// answer of its own.
			name:    "a reader request produces no direct broadcast",
			cmd:     model.Command{Action: "get-status"},
			want:    smc.Control{Kind: smc.ControlReportStatus},
			wantOut: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			control := make(chan smc.Control, 4)
			controller := &optionsController{
				store:       smc.NewOptionsStore(smc.Options{}),
				broadcast:   make(chan model.Message, 8),
				allowRemote: true,
				control:     control,
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

			if !tt.wantOut && len(answers(controller.broadcast)) != 0 {
				t.Error("the controller answered a reader request itself")
			}
		})
	}
}

// Choosing which reader to watch is a configuration change, so it is gated like
// set-options. Reading again and re-listing are not: neither changes what the
// agent reads or exposes, and both would be useless if a locked down agent
// ignored them.
func TestOptionsControllerGatesSetReaderOnly(t *testing.T) {
	tests := []struct {
		name      string
		cmd       model.Command
		forwarded bool
	}{
		{name: "set-reader is refused", cmd: model.Command{Action: "set-reader", Reader: "X"}, forwarded: false},
		{name: "read-now still gets through", cmd: model.Command{Action: "read-now"}, forwarded: true},
		{name: "refresh-readers still gets through", cmd: model.Command{Action: "refresh-readers"}, forwarded: true},
		{name: "get-status still gets through", cmd: model.Command{Action: "get-status"}, forwarded: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			control := make(chan smc.Control, 4)
			broadcast := make(chan model.Message, 8)
			controller := &optionsController{
				store:       smc.NewOptionsStore(smc.Options{}),
				broadcast:   broadcast,
				allowRemote: false,
				control:     control,
			}

			controller.handle(tt.cmd)

			select {
			case got := <-control:
				if !tt.forwarded {
					t.Errorf("forwarded %+v although remote control is off", got)
				}
			default:
				if tt.forwarded {
					t.Error("nothing was forwarded")
				}
			}

			var refused bool
			for _, msg := range answers(broadcast) {
				if msg.Event != "smc-error" {
					continue
				}
				if payload, ok := msg.Payload.(map[string]string); ok &&
					strings.Contains(payload["message"], "SMC_ALLOW_REMOTE_OPTIONS") {
					refused = true
				}
			}
			if !tt.forwarded && !refused {
				t.Error("a refused set-reader was not explained to the client")
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
		store:       smc.NewOptionsStore(smc.Options{}),
		broadcast:   make(chan model.Message, 8),
		allowRemote: true,
		control:     control,
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

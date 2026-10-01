package server

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

type ServerConfig struct {
	Port      string
	Broadcast chan model.Message
	// Command carries control commands from the clients to the agent. It may
	// be nil, which leaves the control channel off: inbound frames are then
	// dropped instead of reaching a consumer that is not there.
	Command chan model.Command
}

//go:embed index.html
var indexPage []byte

func Serve(cfg ServerConfig) {
	socketServer := NewSocketIO(cfg.Command)
	go func() {
		if err := socketServer.Serve(); err != nil {
			log.Fatalf("socketio listen error: %s\n", err)
		}
	}()
	defer socketServer.Close()

	webSocket := NewWS(cfg.Command)

	go func() {
		for {
			msg, ok := <-cfg.Broadcast
			if ok {
				socketServer.Broadcast(msg)
				webSocket.Broadcast(msg)
			}
		}
	}()

	http.Handle("/socket.io/", socketServer)
	http.HandleFunc("/ws", webSocket.Handler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(indexPage)
	})

	log.Println("Serving at localhost:" + cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

// decodeCommand parses one inbound control frame.
//
// socket.io delivers the event argument already JSON encoded, so a page that
// emits a plain object arrives wrapped in a JSON string, while the raw
// WebSocket frame in the contract is the object itself. Both are accepted
// rather than making the page guess which one the agent wants.
func decodeCommand(payload []byte) (model.Command, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return model.Command{}, fmt.Errorf("empty command")
	}

	if payload[0] == '"' {
		var inner string
		if err := json.Unmarshal(payload, &inner); err != nil {
			return model.Command{}, fmt.Errorf("decode command string: %w", err)
		}
		payload = []byte(inner)
	}

	var cmd model.Command
	if err := json.Unmarshal(payload, &cmd); err != nil {
		return model.Command{}, fmt.Errorf("decode command: %w", err)
	}
	return cmd, nil
}

// forwardCommand hands a command to the agent without blocking. Every
// connection is read on its own goroutine, so a blocking send would let one
// client that is not being served wedge every other client's read loop. The
// command is recoverable, the page can send it again, so dropping it is the
// lesser failure.
func forwardCommand(command chan model.Command, cmd model.Command) {
	if command == nil {
		return
	}
	select {
	case command <- cmd:
	default:
		log.Printf("dropping %q command, the agent is not reading commands", cmd.Action)
	}
}

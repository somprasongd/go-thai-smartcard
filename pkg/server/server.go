package server

import (
	_ "embed"
	"log"
	"net/http"

	socketio "github.com/googollee/go-socket.io"
	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

type ServerConfig struct {
	Port      string
	Broadcast chan model.Message
}

// Easier to get running with CORS.
var allowOriginFunc = func(r *http.Request) bool {
	return true
}

//go:embed index.html
var indexPage []byte

func Serve(cfg ServerConfig) {
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

	go func() {
		for {
			m, ok := <-cfg.Broadcast
			if ok {
				server.BroadcastToNamespace("/", m.Event, m.Payload)
			}
		}
	}()

	go func() {
		if err := server.Serve(); err != nil {
			log.Fatalf("socketio listen error: %s\n", err)
		}
	}()
	defer server.Close()

	http.Handle("/socket.io/", server)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(indexPage)
	})

	log.Println("Serving at localhost:" + cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

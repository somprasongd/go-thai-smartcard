package server

import (
	_ "embed"
	"log"
	"net/http"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

type ServerConfig struct {
	Port      string
	Broadcast chan model.Message
}

//go:embed index.html
var indexPage []byte

func Serve(cfg ServerConfig) {
	socketServer := newSocketServer()
	go func() {
		if err := socketServer.Serve(); err != nil {
			log.Fatalf("socketio listen error: %s\n", err)
		}
	}()
	defer socketServer.Close()

	go Hub.Run()

	go func() {
		for {
			m, ok := <-cfg.Broadcast
			if ok {
				socketServer.BroadcastToNamespace("/", m.Event, m.Payload)
				Hub.Broadcast <- m
			}
		}
	}()

	http.Handle("/socket.io/", socketServer)
	http.HandleFunc("/ws", handleWs)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write(indexPage)
	})

	log.Println("Serving at localhost:" + cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

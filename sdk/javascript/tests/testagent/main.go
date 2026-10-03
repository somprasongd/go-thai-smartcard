// testagent exposes the real server with synthetic command outcomes, without
// loading a config, connecting a reader or importing a transport backend.
package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/server"
)

func main() {
	broadcast := make(chan model.Message, 16)
	commands := make(chan model.Command, 16)
	mgr, err := server.Start(server.ServerConfig{
		Listen: "127.0.0.1", Transports: []string{"ws"},
		Broadcast: broadcast, Command: commands,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer mgr.Close()
	fmt.Printf("ws://%s/ws\n", mgr.PlainAddr())
	go func() {
		for cmd := range commands {
			if cmd.Reply == nil {
				continue
			}
			if cmd.Action == "refresh-readers" {
				cmd.Reply.Send("busy", "reader_busy")
				continue
			}
			cmd.Reply.Send("accepted", "")
			switch cmd.Action {
			case "get-status":
				broadcast <- model.Message{Event: "smc-status", Payload: model.Status{
					Readers: []string{"synthetic-reader"}, Selected: "synthetic-reader", State: model.StateCardPresent,
				}}
			case "read-now":
				broadcast <- model.Message{Event: "smc-data", Payload: model.Data{
					Reader: "synthetic-reader", Personal: &model.Personal{Name: model.Name{FullName: "SYNTHETIC CARD"}},
				}}
			}
			cmd.Reply.Send("completed", "")
		}
	}()
	// The test owns stdin: EOF shuts down the real server and its sockets.
	_, _ = io.Copy(io.Discard, os.Stdin)
}

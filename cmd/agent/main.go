package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/server"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

func main() {
	// load env
	port := util.GetEnv("SMC_AGENT_PORT", "9898")
	showImage := util.GetEnvBool("SMC_SHOW_IMAGE", true)
	showNhso := util.GetEnvBool("SMC_SHOW_NHSO", false)

	broadcast := make(chan model.Message)

	serverCfg := server.ServerConfig{
		Broadcast: broadcast,
		Port:      port,
	}
	go server.Serve(serverCfg)

	opts := &smc.Options{
		ShowFaceImage: showImage,
		ShowNhsoData:  showNhso,
	}
	smc := smc.NewSmartCard()
	err := smc.StartDaemon(broadcast, opts)
	if err != nil {
		log.Println("Error occurred while start daemon process, press Ctrl+C to exit.")
		go func() {
			for {
				message := model.Message{
					Event: "smc-error",
					Payload: map[string]string{
						"message": fmt.Sprintf("Error occurred while start daemon process, %v", err.Error()),
					},
				}
				broadcast <- message
				time.Sleep(1 * time.Second)
			}
		}()
		// Listen for syscall signals for process to interrupt/quit
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		s := <-sig
		log.Printf("Received %v signal...", s)
	}
}

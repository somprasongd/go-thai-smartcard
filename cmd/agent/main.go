package main

import (
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

	opts := smc.Options{
		ShowFaceImage: showImage,
		ShowNhsoData:  showNhso,
		Broadcast:     broadcast,
	}
	smc := smc.NewSmartCard(&opts)
	err := smc.StartDemon()
	if err != nil {
		panic(err)
	}
}

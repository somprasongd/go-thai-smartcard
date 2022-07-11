package main

import (
	"encoding/json"
	"log"

	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
)

func main() {
	opts := &smc.Options{
		ShowFaceImage: true,
		ShowNhsoData:  true,
	}
	smc := smc.NewSmartCard()
	// reader := "Identive CLOUD 2700 R Smart Card Reader"
	// data, err := smc.Read(&reader, opts)
	data, err := smc.Read(nil, opts)
	if err != nil {
		log.Println(err)
	}
	dataJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf(err.Error())
	}
	log.Printf("Card Data\n%s\n", string(dataJSON))
}

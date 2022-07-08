package main

import (
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
)

func main() {
	opts := smc.Options{
		ShowFaceImage: true,
		ShowNhsoData:  true,
	}
	smc := smc.NewSmartCard(&opts)
	err := smc.StartDemon()
	panic(err)

}

package main

import "github.com/somprasongd/go-thai-smartcard/pkg/smc"

// var c chan bool

// func init() {
// 	c = make(chan bool)
// }

func main() {
	c := make(chan struct{})
	println("Web Assembly is ready")
	smc := smc.NewSmartCard()
	err := smc.StartDaemon(nil, nil)
	panic(err)
	<-c
}

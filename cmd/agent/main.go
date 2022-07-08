package main

import (
	"fmt"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
)

func ListReaders() ([]string, error) {
	// Establish a PC/SC context
	context, err := scard.EstablishContext()
	if err != nil {
		fmt.Println("Error EstablishContext:", err)
		return nil, err
	}

	// Release the PC/SC context (when needed)
	defer context.Release()
	// List available readers
	readers, err := context.ListReaders()
	// if err != nil {
	// 	fmt.Println("Error ListReaders:", err)
	// 	return nil, err
	// }
	return readers, err
}

// func ListReaders(context *scard.Context) []string {
// 	// List available readers
// 	readers, err := context.ListReaders()
// 	if err != nil {
// 		fmt.Println("Error ListReaders:", err)
// 		return nil
// 	}
// 	return readers
// }

var cmd_get_response []byte

func main() {
	smc.Connect(nil)

}

// func readCID(card *scard.Card, personal *model.Personal) {
// 	s, err := util.ReadData(card, apdu.CMDCid(), cmd_get_response)
// 	if err != nil {
// 		fmt.Println("Error Read CID:", err)
// 		return
// 	}
// 	personal.Cid = s
// }

// func readName(card *scard.Card, personal *model.Personal) {
// 	s, err := util.ReadData(card, apdu.CMDThaiName(), cmd_get_response)
// 	if err != nil {
// 		fmt.Println("Error Read Thai name:", err)
// 		return
// 	}
// 	personal.Name = s
// }

package smc

import (
	"fmt"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

type personalReader struct {
	card    *scard.Card
	respCmd []byte
}

func NewPersonalReader(card *scard.Card, respCmd []byte) *personalReader {
	return &personalReader{
		card,
		respCmd,
	}
}

func (r *personalReader) Check() error {
	// Send command APDU
	_, err := r.card.Transmit(apdu.PersonalCMD.Select)
	return err
}

func (r *personalReader) ReadCID() string {
	s, err := util.ReadData(r.card, apdu.PersonalCMD.Cid, r.respCmd)
	if err != nil {
		fmt.Println("Error Read CID:", err)
		return ""
	}
	return s
}

func (r *personalReader) ReadName() string {
	s, err := util.ReadDataThai(r.card, apdu.PersonalCMD.NameThai, r.respCmd)
	if err != nil {
		fmt.Println("Error Read Thai name:", err)
		return ""
	}
	return s
}

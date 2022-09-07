package smc

import (
	"log"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

type cardReader struct {
	card    *scard.Card
	respCmd []byte
}

func NewCardReader(card *scard.Card, respCmd []byte) *cardReader {
	return &cardReader{
		card,
		respCmd,
	}
}

func (r *cardReader) Select() error {
	// Send command APDU
	_, err := r.card.Transmit(apdu.CardCMD.Select)
	return err
}

func (r *cardReader) ReadLaserId() string {
	s, err := util.ReadLaserData(r.card, apdu.CardCMD.LaserId, r.respCmd)
	if err != nil {
		log.Println("Error Read LaserId:", err)
		return ""
	}
	return s
}

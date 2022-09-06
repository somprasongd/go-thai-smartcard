package smc

import (
	"log"

	"github.com/ebfe/scard"
	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

type admReader struct {
	card    *scard.Card
	respCmd []byte
}

func NewAdmReader(card *scard.Card, respCmd []byte) *admReader {
	return &admReader{
		card,
		respCmd,
	}
}

func (r *admReader) Select() error {
	// Send command APDU
	_, err := r.card.Transmit(apdu.AdmCMD.Select)
	return err
}

func (r *admReader) ReadLaserId() string {
	s, err := util.ReadLaserData(r.card, apdu.AdmCMD.LaserId, r.respCmd)
	if err != nil {
		log.Println("Error Read LaserId:", err)
		return ""
	}
	return s
}

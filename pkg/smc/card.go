package smc

import (
	"log"

	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

// CardReader reads the laser code from the card applet.
type CardReader struct {
	*reader
}

// NewCardReader returns a reader for the card applet.
func NewCardReader(card transport.Card, respCmd []byte) *CardReader {
	return &CardReader{newReader(card, respCmd)}
}

// Select selects the card applet.
func (r *CardReader) Select() error {
	_, err := r.card.Transmit(apdu.CardCMD.Select)
	return err
}

// ReadLaserId reads the laser code printed on the card.
func (r *CardReader) ReadLaserId() string {
	s, err := r.readLaserData(apdu.CardCMD.LaserId)
	if err != nil {
		log.Println("Error Read LaserId:", err)
		return ""
	}
	return s
}

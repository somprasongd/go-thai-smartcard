package smc

import (
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport/pcsc"
)

// defaultTransport returns the PC/SC backed transport.
func defaultTransport() (transport.Transport, error) {
	return pcsc.New()
}

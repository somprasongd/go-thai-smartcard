package smc

import (
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

// NewTransport returns the transport the library uses when no other one is
// supplied.
//
// It is exported so that tools built around the library, such as cmd/record,
// resolve the backend the same way the daemon does instead of reaching for
// PC/SC directly. That keeps backend choice in one place, so adding another
// one later does not leave the capture tool stranded on the old path.
func NewTransport() (transport.Transport, error) {
	return defaultTransport()
}

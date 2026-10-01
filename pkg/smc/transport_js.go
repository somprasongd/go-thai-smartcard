//go:build js

package smc

import (
	"errors"

	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

// errNoDefaultTransport explains why NewSmartCard has nothing to work with on
// js/wasm. The PC/SC binding is cgo, which wasm does not have, so a browser
// build has to supply its own transport through NewSmartCardWith.
var errNoDefaultTransport = errors.New("pc/sc is unavailable on js/wasm: use NewSmartCardWith to supply a transport")

func defaultTransport() (transport.Transport, error) {
	return nil, errNoDefaultTransport
}

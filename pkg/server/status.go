package server

import (
	"encoding/json"
	"sync"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

// StatusCache remembers the newest smc-status that passed the broadcast pump.
//
// The read loop owns the reader list and publishes it on the card sockets
// only, and the settings page deliberately has no card socket, so
// /api/readers serves this cached copy instead. The cache is exactly as fresh
// as the last broadcast — card events and any connected client's get-status
// refresh it — which is the right trade for a dropdown that labels a saved
// reader as unavailable rather than claiming to have polled the hardware.
// One cache is shared by every listener generation, so a settings save that
// restarts the listener does not blank it.
type StatusCache struct {
	mu     sync.Mutex
	status model.Status
	have   bool
}

// Record decodes one broadcast message, keeping only smc-status. The payload
// arrives as whatever the read loop published, so it goes through JSON rather
// than assuming a concrete type.
func (c *StatusCache) Record(msg model.Message) {
	if msg.Event != "smc-status" {
		return
	}
	raw, err := json.Marshal(msg.Payload)
	if err != nil {
		return
	}
	var st model.Status
	if err := json.Unmarshal(raw, &st); err != nil {
		return
	}
	c.mu.Lock()
	c.status = st
	c.have = true
	c.mu.Unlock()
}

// Snapshot returns the cached status and whether one has been seen at all.
func (c *StatusCache) Snapshot() (model.Status, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.have
}

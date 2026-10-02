package server

import (
	"encoding/json"
	"sync"
	"time"

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
	mu          sync.Mutex
	status      model.Status
	have        bool
	health      string
	lastReadAt  time.Time
	lastErrorAt time.Time
}

// Record decodes one broadcast message, keeping only smc-status. The payload
// arrives as whatever the read loop published, so it goes through JSON rather
// than assuming a concrete type.
func (c *StatusCache) Record(msg model.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch msg.Event {
	case "smc-data":
		c.lastReadAt = time.Now().UTC()
		c.health = "ready"
		return
	case "smc-error":
		c.lastErrorAt = time.Now().UTC()
		return
	case "smc-health":
		raw, _ := json.Marshal(msg.Payload)
		var h struct {
			State string `json:"state"`
		}
		if json.Unmarshal(raw, &h) == nil {
			c.health = knownHealth(h.State)
			if c.health == "pcsc-unavailable" {
				c.status.Readers = nil
			}
		}
		return
	case "smc-status":
	default:
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
	c.status = st
	c.health = knownHealth(st.Health)
	if st.Health == "" {
		if len(st.Readers) == 0 {
			c.health = "no-reader"
		} else {
			c.health = "ready"
		}
	}
	c.have = true
}

// Snapshot returns the cached status and whether one has been seen at all.
func (c *StatusCache) Snapshot() (model.Status, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.have
}

// HealthSnapshot is operational metadata only; it never contains a card payload.
type HealthSnapshot struct {
	State       string     `json:"state"`
	Readers     []string   `json:"readers"`
	Selected    string     `json:"selected"`
	CardState   string     `json:"card_state"`
	LastReadAt  *time.Time `json:"last_read_at,omitempty"`
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
}

func knownHealth(state string) string {
	switch state {
	case "ready", "no-reader", "reader-busy", "read-failed", "pcsc-unavailable", "starting":
		return state
	}
	return "starting"
}
func (c *StatusCache) Health() HealthSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := HealthSnapshot{State: knownHealth(c.health), Readers: append([]string{}, c.status.Readers...), Selected: c.status.Selected, CardState: c.status.State}
	if !c.lastReadAt.IsZero() {
		v := c.lastReadAt
		h.LastReadAt = &v
	}
	if !c.lastErrorAt.IsZero() {
		v := c.lastErrorAt
		h.LastErrorAt = &v
	}
	return h
}

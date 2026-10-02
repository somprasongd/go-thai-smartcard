package model

import "time"

// Health contains operational state without card payloads or raw error text.
type Health struct {
	State         string     `json:"state"`
	Readers       []string   `json:"readers"`
	Selected      string     `json:"selected"`
	CardState     string     `json:"card_state"`
	LastReadAt    *time.Time `json:"last_read_at,omitempty"`
	LastErrorAt   *time.Time `json:"last_error_at,omitempty"`
	LastErrorCode string     `json:"last_error_code,omitempty"`
}

// Diagnostics is an allowlist for support reports; secrets and card data have no fields.
type Diagnostics struct {
	Version      string   `json:"version"`
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	RunMode      string   `json:"run_mode"`
	Endpoint     string   `json:"endpoint"`
	ServiceState string   `json:"service_state,omitempty"`
	Transports   []string `json:"transports"`
	TLS          bool     `json:"tls"`
	Health       Health   `json:"health"`
	LogDirectory string   `json:"log_directory,omitempty"`
}

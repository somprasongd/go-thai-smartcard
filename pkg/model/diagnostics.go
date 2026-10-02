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
	Version      string    `json:"version"`
	OS           string    `json:"os"`
	Architecture string    `json:"architecture"`
	RunMode      string    `json:"run_mode"`
	Endpoint     string    `json:"endpoint"`
	ServiceState string    `json:"service_state,omitempty"`
	Transports   []string  `json:"transports"`
	TLS          bool      `json:"tls"`
	Health       Health    `json:"health"`
	Logging      *LogUsage `json:"logging,omitempty"`
	LogDirectory string    `json:"log_directory,omitempty"`
}

// LogPolicy mirrors only non-secret retention values for support tooling.
type LogPolicy struct {
	Mode       string `json:"mode"`
	MaxSizeMB  int    `json:"max_size_mb"`
	MaxBackups int    `json:"max_backups"`
	MaxAgeDays int    `json:"max_age_days"`
}

// LogUsage distinguishes the applied policy from changes awaiting a restart.
type LogUsage struct {
	Effective       LogPolicy `json:"effective"`
	Configured      LogPolicy `json:"configured"`
	RestartRequired bool      `json:"restart_required"`
	FileLogging     bool      `json:"file_logging"`
	UsageAvailable  bool      `json:"usage_available"`
	Bytes           int64     `json:"bytes"`
	Files           int       `json:"files"`
}

package config

import (
	"strings"
	"testing"
)

func TestLoggingDefaultsAndValidation(t *testing.T) {
	d := Default().Logging
	if d.Mode != "auto" || d.MaxSizeMB != 10 || d.MaxBackups != 3 || d.MaxAgeDays != 7 {
		t.Fatal("unexpected retention defaults")
	}
	for _, body := range []string{
		"mode = 'unknown'", "max_size_mb = 0", "max_size_mb = 1025",
		"max_backups = -1", "max_backups = 1001", "max_age_days = -1", "max_age_days = 36501",
	} {
		_, err := Load(writeTemp(t, "config.toml", "[logging]\n"+body+"\n"))
		if err == nil || !strings.Contains(err.Error(), "logging.") {
			t.Fatalf("invalid limits accepted: %s", body)
		}
	}
	cfg, err := Load(writeTemp(t, "config.toml", "[logging]\nmode='file'\nmax_size_mb=1\nmax_backups=0\nmax_age_days=0\n"))
	if err != nil || cfg.Logging.Mode != "file" {
		t.Fatalf("valid bounded zero-history config: %v", err)
	}
}

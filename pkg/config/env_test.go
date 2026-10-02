package config

import (
	"strings"
	"testing"
)

// lookup returns a LookupEnv stand-in over the given map.
func lookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestEnvWarnings(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string // substrings; a nil entry means "no warning at all"
	}{
		{
			name: "a clean environment earns no warnings",
			env:  map[string]string{},
			want: nil,
		},
		{
			name: "SMC_AGENT_PORT maps to the port key",
			env:  map[string]string{"SMC_AGENT_PORT": "1234"},
			want: []string{"SMC_AGENT_PORT", "[server]", "port = 1234"},
		},
		{
			name: "SMC_SHOW_IMAGE maps to read_face_image",
			env:  map[string]string{"SMC_SHOW_IMAGE": "false"},
			want: []string{"SMC_SHOW_IMAGE", "[card]", "read_face_image = false"},
		},
		{
			name: "SMC_SHOW_LASER maps to read_laser_id",
			env:  map[string]string{"SMC_SHOW_LASER": "true"},
			want: []string{"SMC_SHOW_LASER", "[card]", "read_laser_id = true"},
		},
		{
			name: "SMC_SHOW_NHSO maps to read_nhso",
			env:  map[string]string{"SMC_SHOW_NHSO": "true"},
			want: []string{"SMC_SHOW_NHSO", "[card]", "read_nhso = true"},
		},
		{
			// Decision 13: the one variable with no replacement is named as
			// removed rather than mapped to a fabricated key.
			name: "SMC_ALLOW_REMOTE_OPTIONS has no replacement",
			env:  map[string]string{"SMC_ALLOW_REMOTE_OPTIONS": "true"},
			want: []string{"SMC_ALLOW_REMOTE_OPTIONS", "no replacement", "/settings"},
		},
		{
			name: "a non-boolean value is named, not mapped",
			env:  map[string]string{"SMC_SHOW_IMAGE": "junk"},
			want: []string{"SMC_SHOW_IMAGE", `"junk"`, "CHANGELOG"},
		},
		{
			name: "a non-numeric port is named, not mapped",
			env:  map[string]string{"SMC_AGENT_PORT": "abc"},
			want: []string{"SMC_AGENT_PORT", `"abc"`},
		},
		{
			name: "two set variables produce two warnings",
			env:  map[string]string{"SMC_SHOW_IMAGE": "true", "SMC_SHOW_NHSO": "false"},
			want: []string{"SMC_SHOW_IMAGE", "SMC_SHOW_NHSO"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnvWarnings(lookup(tt.env))
			if tt.want == nil {
				if len(got) != 0 {
					t.Errorf("got %d warnings, want none: %q", len(got), got)
				}
				return
			}
			joined := strings.Join(got, "\n---\n")
			for _, want := range tt.want {
				if !strings.Contains(joined, want) {
					t.Errorf("warnings\n%s\ndo not mention %q", joined, want)
				}
			}
		})
	}
}

func TestEnvWarningsNeverInventKeysForGoneVariables(t *testing.T) {
	got := strings.Join(EnvWarnings(lookup(map[string]string{
		"SMC_ALLOW_REMOTE_OPTIONS": "true",
	})), "\n")
	// The no-replacement warning must not print a TOML snippet an
	// administrator would paste and then trip the strict loader with.
	if strings.Contains(got, "=") {
		t.Errorf("the no-replacement warning invented a key: %q", got)
	}
}

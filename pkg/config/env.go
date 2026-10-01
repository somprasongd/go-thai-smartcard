package config

import (
	"fmt"
	"strconv"
)

// envVar is one v2 environment variable and what became of it. The agent no
// longer reads the environment (decision 1); a variable still set only earns
// a warning naming its replacement, and is never honoured.
type envVar struct {
	name string
	// replacement renders the TOML that matches the value. It returns "" when
	// the value cannot be turned into TOML, and the warning then says so
	// instead of printing something unparsable.
	replacement func(value string) string
	// gone marks the one variable with no replacement: what it gated no
	// longer exists (decision 13), and no key may be invented for it.
	gone bool
}

var envVars = []envVar{
	{
		name: "SMC_AGENT_PORT",
		replacement: func(v string) string {
			port, err := strconv.Atoi(v)
			if err != nil || port < 1 || port > 65535 {
				return ""
			}
			return "[server]\nport = " + strconv.Itoa(port)
		},
	},
	{
		name: "SMC_SHOW_IMAGE",
		replacement: func(v string) string {
			return cardReplacement("read_face_image", v)
		},
	},
	{
		name: "SMC_SHOW_LASER",
		replacement: func(v string) string {
			return cardReplacement("read_laser_id", v)
		},
	},
	{
		name: "SMC_SHOW_NHSO",
		replacement: func(v string) string {
			return cardReplacement("read_nhso", v)
		},
	},
	{
		name: "SMC_ALLOW_REMOTE_OPTIONS",
		gone: true,
	},
}

func cardReplacement(key, v string) string {
	value, err := strconv.ParseBool(v)
	if err != nil {
		return ""
	}
	return "[card]\n" + key + " = " + strconv.FormatBool(value)
}

// EnvWarnings returns one warning per SMC_* variable that is still set, saying
// it is no longer read and printing the TOML that matches it, for the
// administrator to paste into config.toml. A variable with no replacement is
// named as removed rather than mapped to a fabricated key (decision 13). A
// variable whose value cannot be mapped is named with its key rather than
// silently skipped.
//
// lookup is os.LookupEnv; it is a parameter so tests can stand in for the
// environment and no test ever touches the real one.
func EnvWarnings(lookup func(string) (string, bool)) []string {
	var out []string
	for _, e := range envVars {
		value, ok := lookup(e.name)
		if !ok {
			continue
		}
		if e.gone {
			out = append(out, fmt.Sprintf("%s is no longer read and has no replacement; settings change only through /settings. Delete the line from the service's unit file or plist.", e.name))
			continue
		}
		if snippet := e.replacement(value); snippet != "" {
			out = append(out, fmt.Sprintf("%s is no longer read. Paste this into config.toml instead:\n%s", e.name, snippet))
			continue
		}
		out = append(out, fmt.Sprintf("%s is set to %s, which does not map to a config value, and is no longer read. See the upgrade notes in CHANGELOG.md.", e.name, strconv.Quote(value)))
	}
	return out
}

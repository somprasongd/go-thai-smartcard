package main

import "strings"

type language bool

const (
	english language = false
	thai    language = true
)

func languageFor(locale string) language {
	primary := strings.ToLower(strings.TrimSpace(locale))
	if i := strings.IndexAny(primary, "-_.@:"); i >= 0 {
		primary = primary[:i]
	}
	return language(primary == "th")
}

func (l language) text(th, en string) string {
	if l == thai {
		return th
	}
	return en
}

// Message language follows gettext precedence. LANGUAGE chooses the first
// preferred language, but C/POSIX explicitly requests untranslated messages.
func localeFromEnvironment(getenv func(string) string) string {
	locale := ""
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale = getenv(name); locale != "" {
			break
		}
	}
	if locale == "" || locale == "C" || locale == "POSIX" || strings.HasPrefix(locale, "C.") {
		return "en"
	}
	if preferred := getenv("LANGUAGE"); preferred != "" {
		return strings.Split(preferred, ":")[0]
	}
	return locale
}

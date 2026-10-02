package main

import "testing"

func TestLanguageFor(t *testing.T) {
	for _, tt := range []struct {
		tag  string
		want language
	}{
		{"th", thai}, {"TH-th", thai}, {"th_TH.UTF-8", thai}, {"th-TH:en", thai},
		{"en-TH", english}, {"ja-JP", english}, {"fr:th", english}, {"", english}, {"thai", english},
	} {
		if got := languageFor(tt.tag); got != tt.want {
			t.Errorf("%q=%v want %v", tt.tag, got, tt.want)
		}
	}
	if thai.text("ไทย", "English") != "ไทย" || english.text("ไทย", "English") != "English" {
		t.Fatal("language selection")
	}
}

func TestLocalePrecedence(t *testing.T) {
	for _, tt := range []struct {
		env  map[string]string
		want language
	}{
		{map[string]string{"LANG": "th_TH.UTF-8"}, thai},
		{map[string]string{"LANG": "th_TH.UTF-8", "LC_MESSAGES": "de_DE.UTF-8"}, english},
		{map[string]string{"LANG": "en_US.UTF-8", "LC_MESSAGES": "en_US.UTF-8", "LC_ALL": "th_TH.UTF-8"}, thai},
		{map[string]string{"LANG": "en_US.UTF-8", "LANGUAGE": "th:en"}, thai},
		{map[string]string{"LANG": "th_TH.UTF-8", "LANGUAGE": "ja:th"}, english},
		{map[string]string{"LANG": "th_TH.UTF-8", "LANGUAGE": "th", "LC_ALL": "C"}, english},
		{map[string]string{"LANG": "C.UTF-8", "LANGUAGE": "th"}, english},
		{map[string]string{"LANG": "POSIX", "LANGUAGE": "th"}, english},
		{map[string]string{}, english},
		{map[string]string{"LANGUAGE": "th"}, english},
	} {
		if got := languageFor(localeFromEnvironment(func(k string) string { return tt.env[k] })); got != tt.want {
			t.Errorf("%v=%v want %v", tt.env, got, tt.want)
		}
	}
}

func TestSystemLanguage(t *testing.T) {
	l := systemLanguage()
	if l != thai && l != english {
		t.Fatalf("invalid system language: %v", l)
	}
	t.Logf("current system UI: %s", l.text("Thai", "English"))
}

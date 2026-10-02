//go:build darwin

package main

import (
	"bytes"
	"github.com/kardianos/service"
	"strings"
	"testing"
	"text/template"
)

func TestLaunchdOutputDoesNotCreateUnboundedLogs(t *testing.T) {
	opts := service.KeyValue{}
	configureServiceOutput(opts)
	tplt, err := template.New("plist").Parse(opts["LaunchdConfig"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	args := struct {
		Name, Path string
		Arguments  []string
	}{"thai-smartcard-agent", "/usr/local/bin/thai-smartcard-agent", []string{"--config", "/Library/Application Support/ThaiSmartcard/config.toml"}}
	if err := tplt.Execute(&buf, args); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"StandardOutPath", "StandardErrorPath"} {
		if !strings.Contains(buf.String(), "<key>"+key+"</key><string>/dev/null</string>") {
			t.Fatal("unbounded launchd output", key)
		}
	}
	if !strings.Contains(buf.String(), "--config") || !strings.Contains(buf.String(), args.Arguments[1]) {
		t.Fatal("lost service arguments")
	}
}

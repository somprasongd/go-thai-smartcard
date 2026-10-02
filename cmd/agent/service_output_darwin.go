//go:build darwin

package main

import "github.com/kardianos/service"

// The application owns bounded log files. Sending raw launchd output to null
// avoids a second unbounded .err.log beside them; fatal config errors are
// recorded separately by reportStartupFailure.
func configureServiceOutput(opts service.KeyValue) { opts["LaunchdConfig"] = agentLaunchdConfig }

const agentLaunchdConfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>{{html .Name}}</string>
<key>ProgramArguments</key><array><string>{{html .Path}}</string>
{{range .Arguments}}<string>{{html .}}</string>{{end}}</array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>/dev/null</string>
<key>StandardErrorPath</key><string>/dev/null</string>
</dict></plist>`

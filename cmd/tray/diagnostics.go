package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

func (t *tray) troubleshoot(action string) {
	err := t.runTroubleshoot(action)
	if err != nil {
		alert(t.lang, t.lang.text("วิเคราะห์ปัญหาไม่สำเร็จ", "Troubleshoot failed"), err.Error())
	}
}
func (t *tray) runTroubleshoot(action string) error {
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(t.baseURL() + "/api/diagnostics")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("diagnostics: HTTP %d", resp.StatusCode)
	}
	var d model.Diagnostics
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&d); err != nil {
		return err
	}
	switch action {
	case "copy":
		state, err := t.serviceManager().State()
		if err != nil {
			d.ServiceState = "unavailable"
		} else {
			d.ServiceState = state.String()
		}
		raw, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		return copyDiagnosticText(raw)
	case "logs":
		path, err := logDirectory(d.LogDirectory)
		if err != nil {
			return err
		}
		cmd := exec.Command("xdg-open", path)
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("open", path)
		}
		if runtime.GOOS == "windows" {
			cmd = exec.Command("explorer.exe", path)
		}
		return cmd.Run()
	}
	return fmt.Errorf("unknown troubleshoot action")
}
func logDirectory(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file logging is not active; open Settings to select file logging and restart")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("log directory must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("log path is not a directory")
	}
	return filepath.Clean(path), nil
}
func copyDiagnosticText(raw []byte) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$input | Set-Clipboard")
	default:
		for _, choice := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
			if path, err := exec.LookPath(choice[0]); err == nil {
				cmd = exec.Command(path, choice[1:]...)
				break
			}
		}
	}
	if cmd == nil {
		return fmt.Errorf("clipboard utility unavailable; use Copy on the diagnostics page")
	}
	cmd.Stdin = bytes.NewReader(raw)
	return cmd.Run()
}

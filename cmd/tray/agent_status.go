package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"runtime"
	"sync"

	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
)

// Connection readiness takes precedence over service status: a foreground
// agent can be healthy even when the installed service is stopped or absent.
func agentIndicator(l language, connection string, state ctl.State, serviceErr error) (string, string) {
	switch connection {
	case "connected":
		return "green", l.text("agent กำลังทำงาน", "Agent is running")
	case "unauthorized":
		return "amber", l.text("agent ต้องการสิทธิ์เชื่อมต่อ", "Agent authentication required")
	case "unsupported":
		return "amber", l.text("agent ไม่เปิด WebSocket", "Agent WebSocket unavailable")
	}
	if serviceErr == nil {
		switch state {
		case ctl.StateStopped:
			return "red", l.text("agent หยุดอยู่", "Agent is stopped")
		case ctl.StateRunning:
			return "amber", l.text("agent กำลังรอการเชื่อมต่อ", "Agent is waiting for connection")
		}
	}
	return "gray", l.text("ติดต่อ agent ไม่ได้", "Agent is unavailable")
}

// Caller holds serviceUI so polling and connection events cannot overwrite
// each other's menu state. The endpoint comes only from discovery or --url.
func (t *tray) renderAgentStatus() {
	if t.mAgentStatus == nil {
		return
	}
	shade, title := agentIndicator(t.lang, t.connection, t.observedState, t.serviceErr)
	endpoint := t.baseURL()
	tooltip := title + " — " + endpoint
	if u, err := url.Parse(endpoint); err == nil {
		port := u.Port()
		if port == "" {
			if u.Scheme == "https" {
				port = "443"
			} else if u.Scheme == "http" {
				port = "80"
			}
		}
		if port != "" {
			tooltip += " (" + t.lang.text("พอร์ต ", "port ") + port + ")"
		}
	}
	t.mAgentStatus.SetTitle(title)
	t.mAgentStatus.SetTooltip(tooltip)
	t.mAgentStatus.SetIcon(statusIcons()[shade])
}

var iconOnce sync.Once
var dotIcons map[string][]byte

func statusIcons() map[string][]byte {
	iconOnce.Do(func() {
		dotIcons = make(map[string][]byte)
		for name, c := range map[string]color.RGBA{
			"green": {38, 190, 73, 255}, "amber": {240, 170, 30, 255},
			"red": {225, 65, 65, 255}, "gray": {145, 145, 145, 255},
		} {
			im := image.NewRGBA(image.Rect(0, 0, 16, 16))
			for y := 0; y < 16; y++ {
				for x := 0; x < 16; x++ {
					dx, dy := 2*x-15, 2*y-15
					if dx*dx+dy*dy <= 144 {
						im.SetRGBA(x, y, c)
					}
				}
			}
			var b bytes.Buffer
			_ = png.Encode(&b, im)
			data := b.Bytes()
			if runtime.GOOS == "windows" {
				// Windows menu icons use ICO; modern Windows accepts PNG image payloads.
				header := make([]byte, 22)
				binary.LittleEndian.PutUint16(header[2:], 1)
				binary.LittleEndian.PutUint16(header[4:], 1)
				header[6], header[7] = 16, 16
				binary.LittleEndian.PutUint16(header[10:], 1)
				binary.LittleEndian.PutUint16(header[12:], 32)
				binary.LittleEndian.PutUint32(header[14:], uint32(len(data)))
				binary.LittleEndian.PutUint32(header[18:], 22)
				data = append(header, data...)
			}
			dotIcons[name] = data
		}
	})
	return dotIcons
}

// Resolve against current OS state at click time, rather than a stale menu label.
func toggleAction(m ctl.Manager) (string, error) {
	state, err := m.State()
	if err != nil {
		return "", err
	}
	switch state {
	case ctl.StateRunning:
		return "stop", nil
	case ctl.StateStopped:
		return "start", nil
	default:
		return "", fmt.Errorf("service state is unknown")
	}
}

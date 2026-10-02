// Command tray is the Thai Smartcard tray app: a thin client of the agent.
// It shows the reader and card state from the /ws broadcast and opens the
// test and settings pages; it never touches the config file and never spawns
// the agent (decision 12) — when it cannot reach the agent it says so with
// the command to start it and keeps polling.
//
// The tray needs cgo (fyne-io/systray), which is why it is its own binary and
// not a flag of the agent: the agent stays cross-compilable.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"fyne.io/systray"
	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

//go:embed icon.png
var iconBytes []byte

var agentURL = flag.String("url", "http://127.0.0.1:9898", "base URL of the running agent")

func main() {
	flag.Parse()
	systray.Run(onReady, nil)
}

// tray is the menu and the state behind it. Everything the menu shows comes
// from the agent over the network: the card state from the /ws broadcast.
// Exposure and the socket token are one decision and the token's one-time
// showing only exists on the settings page, so the menu carries no switch
// for them either.
type tray struct {
	mStatus *systray.MenuItem
	mTest   *systray.MenuItem
	mConfig *systray.MenuItem
	mQuit   *systray.MenuItem
}

func onReady() {
	systray.SetIcon(iconBytes)
	// No SetTitle: the menu bar shows the icon alone, which is the macOS
	// convention. (Windows and the Linux appindicator never displayed a title
	// anyway.) Hovering names the app.
	systray.SetTooltip("Thai Smartcard Agent")

	t := &tray{}

	t.mStatus = systray.AddMenuItem("กำลังต่อกับ agent…", "Waiting for the agent")
	t.mStatus.Disable()
	systray.AddSeparator()
	t.mTest = systray.AddMenuItem("เปิดหน้าทดสอบ / Open test page", "Open the agent's test page")
	t.mConfig = systray.AddMenuItem("ตั้งค่า / Settings", "Open /settings")
	systray.AddSeparator()
	// Quit closes the tray for this session only; the agent keeps running.
	t.mQuit = systray.AddMenuItem("ออกจาก tray / Quit", "Close the tray; the agent keeps running")

	go t.watchCardState()

	if runtime.GOOS == "linux" {
		// GNOME without the AppIndicator extension never shows the icon;
		// say so instead of failing silently.
		go checkStatusNotifier(*agentURL)
	}

	for {
		select {
		case <-t.mTest.ClickedCh:
			openBrowser(*agentURL)
		case <-t.mConfig.ClickedCh:
			openBrowser(*agentURL + "/settings")
		case <-t.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// watchCardState keeps the status line and the reader state current: one
// WebSocket to the agent, reconnected for as long as the tray lives. The
// agent's own page uses the same events.
func (t *tray) watchCardState() {
	for {
		if !t.connectCardState() {
			t.setDown()
		}
		time.Sleep(5 * time.Second)
	}
}

// connectCardState runs one WebSocket connection to completion. It reports
// whether it ever got connected, so a down agent earns the "not running" line.
func (t *tray) connectCardState() bool {
	wsURL := strings.Replace(*agentURL, "http", "ws", 1) + "/ws"
	// A dial without a timeout would park this goroutine forever on an agent
	// that accepts but never answers, and the status line would never recover.
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return false
	}
	defer conn.Close()
	t.setUp()
	for {
		var msg model.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return true
		}
		if msg.Event == "smc-status" {
			if status, ok := msg.Payload.(map[string]any); ok {
				t.applyStatus(status)
			}
		}
	}
}

func (t *tray) applyStatus(status map[string]any) {
	state, _ := status["state"].(string)
	selected, _ := status["selected"].(string)
	readers, _ := status["readers"].([]any)
	names := make([]string, 0, len(readers))
	for _, r := range readers {
		if s, ok := r.(string); ok {
			names = append(names, s)
		}
	}

	var line string
	switch state {
	case model.StateReading:
		line = "กำลังอ่านบัตร… / reading"
	case model.StateCardPresent:
		line = "บัตรอยู่ในเครื่องอ่าน / card in"
	default:
		line = "รอบัตร / waiting"
	}
	if len(names) > 0 {
		watching := selected
		if watching == "" {
			watching = fmt.Sprintf("ทุกเครื่องอ่าน (%d)", len(names))
		}
		line += " — " + watching
	}
	t.mStatus.SetTitle(line)
	t.mStatus.SetTooltip(line)
}

func (t *tray) setUp() {
	t.mStatus.SetTitle("เชื่อมต่อ agent แล้ว / connected")
}

// setDown says the agent is not running, with the way to start it on this
// platform. The tray never starts the agent itself (decision 12).
func (t *tray) setDown() {
	hint := startHint()
	line := "agent ไม่ทำงาน — " + hint
	t.mStatus.SetTitle("agent ไม่ทำงาน / agent is not running")
	t.mStatus.SetTooltip(line)
	log.Printf("cannot reach the agent at %s; %s", *agentURL, hint)
}

func startHint() string {
	switch runtime.GOOS {
	case "windows":
		return "start it with `net start thai-smartcard-agent`"
	case "darwin":
		return "start it with `sudo thai-smartcard-agent service start`"
	default:
		return "start it with `sudo systemctl start thai-smartcard-agent`"
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("open %s: %v", url, err)
	}
}

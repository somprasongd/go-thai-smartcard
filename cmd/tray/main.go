// Command tray is the Thai Smartcard tray app: a thin client of the agent.
// It shows the reader and card state from the /ws broadcast, opens the test
// and settings pages, and can start, stop and restart the agent's system
// service (docs/plan/tray-agent-control.md). It never touches the config
// file and never spawns an agent process of its own (decision 12) — the
// control actions ask the OS service manager, which is what owns the single
// installed run; when it cannot reach the agent it says so and keeps
// polling.
//
// The tray needs cgo (fyne-io/systray), which is why it is its own binary and
// not a flag of the agent: the agent stays cross-compilable.
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/somprasongd/go-thai-smartcard/pkg/ctl"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

//go:embed icon.png
var iconBytes []byte

// Windows loads tray icons from ICO files rather than PNG images.
//
//go:embed icon.ico
var windowsIconBytes []byte

var agentURL = flag.String("url", "", "explicit agent URL (default: discover local agent, then http://127.0.0.1:9898)")

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
	mStatus       *systray.MenuItem
	mTest         *systray.MenuItem
	mConfig       *systray.MenuItem
	mAgent        *systray.MenuItem
	mAgentStart   *systray.MenuItem
	mAgentStop    *systray.MenuItem
	mAgentRestart *systray.MenuItem
	mQuit         *systray.MenuItem
	mu            sync.RWMutex
	url           string
	notifyOnce    sync.Once
}

func onReady() {
	regularIcon := iconBytes
	if runtime.GOOS == "windows" {
		regularIcon = windowsIconBytes
	}
	// Let macOS choose the monochrome color for the menu bar background.
	systray.SetTemplateIcon(iconBytes, regularIcon)
	// No SetTitle: the menu bar shows the icon alone, which is the macOS
	// convention. (Windows and the Linux appindicator never displayed a title
	// anyway.) Hovering names the app.
	systray.SetTooltip("Thai Smartcard Agent")

	t := &tray{url: "http://127.0.0.1:9898"}

	t.mStatus = systray.AddMenuItem("กำลังต่อกับ agent…", "Waiting for the agent")
	t.mStatus.Disable()
	systray.AddSeparator()
	t.mTest = systray.AddMenuItem("เปิดหน้าทดสอบ / Open test page", "Open the agent's test page")
	t.mConfig = systray.AddMenuItem("ตั้งค่า / Settings", "Open /settings")

	// Start/stop/restart go through the OS service manager (pkg/ctl); the
	// tray never spawns an agent of its own (decision 12). The items are
	// enabled or disabled against the service's real state by pollService.
	t.mAgent = systray.AddMenuItem("Agent", "Control the agent service")
	t.mAgentStart = t.mAgent.AddSubMenuItem("เริ่ม agent / Start agent", "Start the agent service")
	t.mAgentStop = t.mAgent.AddSubMenuItem("หยุด agent… / Stop agent…", "Stop the agent service — the reader will not work until started again")
	t.mAgentRestart = t.mAgent.AddSubMenuItem("รีสตาร์ท agent / Restart agent", "Restart the agent service — what a hand-edited config.toml needs")

	systray.AddSeparator()
	// Quit closes the tray for this session only; the agent keeps running.
	t.mQuit = systray.AddMenuItem("ออกจาก tray / Quit", "Close the tray; the agent keeps running")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newAgentClient(*agentURL)
	go t.pollService(ctx)
	go client.run(ctx, t.setEndpoint, t.connectionState, func(msg model.Message) {
		if msg.Event == "smc-status" {
			if status, ok := msg.Payload.(map[string]any); ok {
				t.applyStatus(status)
			}
		}
	})

	for {
		select {
		case <-t.mTest.ClickedCh:
			openBrowser(t.baseURL())
		case <-t.mConfig.ClickedCh:
			openBrowser(t.baseURL() + "/settings")
		case <-t.mAgentStart.ClickedCh:
			go t.serviceAction("start")
		case <-t.mAgentStop.ClickedCh:
			go t.serviceAction("stop")
		case <-t.mAgentRestart.ClickedCh:
			go t.serviceAction("restart")
		case <-t.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// serviceAction runs one control action after any confirmation Stop needs,
// then refreshes the menu. It runs on its own goroutine: a systemctl restart
// or an osascript prompt waiting for a password can take seconds, and the
// menu loop must not freeze behind it.
func (t *tray) serviceAction(action string) {
	if action == "stop" && !confirm(
		"Thai Smartcard",
		"หยุด agent แล้วจะอ่านบัตรไม่ได้จนกว่าจะเริ่มใหม่ — Stop the agent? The reader will not work until started again.",
	) {
		return
	}
	m := ctl.New()
	var err error
	switch action {
	case "start":
		err = m.Start()
	case "stop":
		err = m.Stop()
	case "restart":
		err = m.Restart()
	}
	if err != nil {
		log.Printf("agent %s: %v", action, err)
	}
	t.refreshServiceState()
}

// pollService keeps the Agent submenu enabled or disabled against the
// service's real state. It polls because the OS service managers offer no
// change callback a tray could subscribe to.
func (t *tray) pollService(ctx context.Context) {
	t.refreshServiceState()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.refreshServiceState()
		}
	}
}

func (t *tray) refreshServiceState() {
	state, err := ctl.New().State()
	if err != nil {
		// No working control mechanism: no systemd, no polkit agent, the
		// helper not installed and the operator cancelled the prompt. Keep
		// the items disabled and let the tooltip say why, exactly like the
		// terminal hint does.
		t.mAgentStart.Disable()
		t.mAgentStop.Disable()
		t.mAgentRestart.Disable()
		t.mAgent.SetTooltip("ควบคุม service ไม่ได้ — " + err.Error())
		return
	}
	t.mAgentStart.Enable()
	t.mAgentStop.Enable()
	t.mAgentRestart.Enable()
	switch state {
	case ctl.StateRunning:
		t.mAgentStart.Disable()
		t.mAgent.SetTooltip("agent กำลังทำงาน / running")
	case ctl.StateStopped:
		t.mAgentStop.Disable()
		t.mAgentRestart.Disable()
		t.mAgent.SetTooltip("agent หยุดอยู่ / stopped")
	}
}

func (t *tray) baseURL() string { t.mu.RLock(); defer t.mu.RUnlock(); return t.url }
func (t *tray) setEndpoint(url string) {
	t.mu.Lock()
	t.url = url
	t.mu.Unlock()
	if runtime.GOOS == "linux" {
		t.notifyOnce.Do(func() { go checkStatusNotifier(url) })
	}
}
func (t *tray) connectionState(state string) {
	switch state {
	case "connected":
		t.setUp()
	case "unauthorized":
		t.mStatus.SetTitle("agent ต้องการสิทธิ์เชื่อมต่อ / authentication required")
		t.mStatus.SetTooltip("Agent is reachable but the card socket refused authentication")
	case "unsupported":
		t.mStatus.SetTitle("agent ไม่เปิด WebSocket / WebSocket unavailable")
		t.mStatus.SetTooltip("Open Settings to enable the ws transport")
	default:
		t.setDown()
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

// setDown says the agent is not reachable, with the terminal way to start it
// for this platform as a fallback next to the Agent menu's Start item.
func (t *tray) setDown() {
	hint := startHint()
	line := "ติดต่อ agent ไม่ได้ — " + hint
	t.mStatus.SetTitle("ติดต่อ agent ไม่ได้ / cannot reach agent")
	t.mStatus.SetTooltip(line)
	log.Printf("cannot reach the agent at %s; %s", t.baseURL(), hint)
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

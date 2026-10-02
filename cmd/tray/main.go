// Command tray is the Thai Smartcard tray app: a thin client of the agent.
// It shows the reader and card state from the /ws broadcast, opens the test
// and settings pages, and can start, stop and restart the agent's system
// service (docs/plan/tray-agent-control.md). It never touches the config
// file and never spawns an agent process of its own (decision 12) — the
// control actions ask the OS service manager, which is what owns the single
// installed run; on startup it asks the service manager to start a stopped service, then
// keeps polling until the endpoint is ready.
//
// The tray needs cgo (fyne-io/systray), which is why it is its own binary and
// not a flag of the agent: the agent stays cross-compilable.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
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
	mStatus          *systray.MenuItem
	mTest            *systray.MenuItem
	mTroubleshoot    *systray.MenuItem
	mDiagnostics     *systray.MenuItem
	mCopyDiagnostics *systray.MenuItem
	mOpenLogs        *systray.MenuItem
	mConfig          *systray.MenuItem
	mAgent           *systray.MenuItem
	mAgentToggle     *systray.MenuItem
	mAgentStatus     *systray.MenuItem
	connection       string
	lastReadAt       string
	observedState    ctl.State
	serviceErr       error
	mAgentRestart    *systray.MenuItem
	mQuit            *systray.MenuItem
	mStopQuit        *systray.MenuItem
	lang             language
	serviceOps       sync.Mutex
	manualControl    bool
	manager          ctl.Manager
	serviceUI        sync.Mutex
	serviceState     func() (ctl.State, error)
	mu               sync.RWMutex
	url              string
	notifyOnce       sync.Once
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
	t := &tray{url: "http://127.0.0.1:9898", lang: systemLanguage(), manager: ctl.New()}
	l := t.lang
	systray.SetTooltip(l.text("บริการอ่านบัตรประชาชน", "Thai Smartcard Agent"))

	t.mAgentStatus = systray.AddMenuItem(l.text("กำลังตรวจสอบ agent…", "Checking agent…"), "")
	t.mAgentStatus.Disable()
	t.mStatus = systray.AddMenuItem(l.text("กำลังเชื่อมต่อ agent…", "Connecting to agent…"), l.text("รอการเชื่อมต่อ agent", "Waiting for the agent"))
	t.mStatus.Disable()
	systray.AddSeparator()
	t.mTest = systray.AddMenuItem(l.text("เปิดหน้าทดสอบ", "Open test page"), l.text("เปิดหน้าทดสอบการอ่านบัตร", "Open the agent's test page"))
	t.mConfig = systray.AddMenuItem(l.text("ตั้งค่า", "Settings"), l.text("เปิดหน้าตั้งค่า", "Open settings"))

	t.mTroubleshoot = systray.AddMenuItem(l.text("แก้ไขปัญหา", "Troubleshoot"), l.text("ตรวจสถานะและบันทึกการทำงาน", "Inspect operational diagnostics"))
	t.mDiagnostics = t.mTroubleshoot.AddSubMenuItem(l.text("เปิดหน้าวิเคราะห์ปัญหา", "Open diagnostics"), "")
	t.mCopyDiagnostics = t.mTroubleshoot.AddSubMenuItem(l.text("คัดลอกข้อมูลวิเคราะห์", "Copy diagnostics"), "")
	t.mOpenLogs = t.mTroubleshoot.AddSubMenuItem(l.text("เปิดโฟลเดอร์ log", "Open log folder"), "")

	// Restart and Pause/Resume go through the OS service manager (pkg/ctl); the
	// tray never spawns an agent of its own (decision 12). The items are
	// enabled or disabled against the service's real state by pollService.
	t.mAgent = systray.AddMenuItem(l.text("บริการ agent", "Agent service"), l.text("ควบคุมบริการอ่านบัตร", "Control the agent service"))
	t.mAgentRestart = t.mAgent.AddSubMenuItem(l.text("เริ่ม agent ใหม่", "Restart agent"), l.text("เริ่มบริการใหม่", "Restart the agent service"))
	t.mAgentToggle = t.mAgent.AddSubMenuItem(l.text("พัก agent…", "Pause agent…"), l.text("หยุดบริการอ่านบัตรจนกว่าจะทำงานต่อ", "Stop the service until Resume is selected"))
	t.mAgentToggle.Disable()

	systray.AddSeparator()
	// Quit closes the tray for this session only; the agent keeps running.
	t.mStopQuit = systray.AddMenuItem(l.text("หยุด agent และออก…", "Stop agent and quit…"), l.text("หยุดบริการอ่านบัตรแล้วปิด tray", "Stop card reading and close the tray"))
	t.mQuit = systray.AddMenuItem(l.text("ออกจาก tray", "Quit tray"), l.text("ปิด tray โดย agent ยังทำงานอยู่", "Close the tray; the agent keeps running"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newAgentClient(*agentURL)
	go func() { t.autoStart(ctx, client); t.pollService(ctx) }()
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

		case <-t.mDiagnostics.ClickedCh:
			openBrowser(t.baseURL() + "/diagnostics?lang=" + t.lang.text("th", "en"))
		case <-t.mCopyDiagnostics.ClickedCh:
			go t.troubleshoot("copy")
		case <-t.mOpenLogs.ClickedCh:
			go t.troubleshoot("logs")
		case <-t.mAgentToggle.ClickedCh:
			go t.serviceAction("toggle")
		case <-t.mAgentRestart.ClickedCh:
			go t.serviceAction("restart")
		case <-t.mStopQuit.ClickedCh:
			go t.serviceAction("stop-quit")
		case <-t.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

// serviceAction runs one control action after any confirmation Pause needs,
// then refreshes the menu. It runs on its own goroutine: a systemctl restart
// or an osascript prompt waiting for a password can take seconds, and the
// menu loop must not freeze behind it.
func (t *tray) serviceAction(action string) {
	// Ignore duplicate clicks while a prompt or operation is in progress; a
	// queued toggle could otherwise resume immediately after a pause.
	if !t.serviceOps.TryLock() {
		return
	}
	defer t.serviceOps.Unlock()
	t.manualControl = true
	m := t.serviceManager()
	if action == "toggle" {
		var err error
		action, err = toggleAction(m)
		if err != nil {
			t.refreshServiceState()
			return
		}
	}
	if (action == "stop" || action == "stop-quit") && !confirm(t.lang,
		t.lang.text("บริการอ่านบัตรประชาชน", "Thai Smartcard"),
		t.lang.text("หยุด agent แล้วทุกโปรแกรมจะอ่านบัตรไม่ได้จนกว่าจะเริ่มใหม่ ต้องการหยุดหรือไม่?", "Stop the agent? All clients will lose card reading until it is started again."),
	) {
		return
	}
	err := controlService(m, action)
	if err != nil {
		log.Printf("agent %s: %v", action, err)
		alert(t.lang, t.lang.text("ควบคุม agent ไม่สำเร็จ", "Agent control failed"),
			t.lang.text("ดำเนินการไม่สำเร็จ กรุณาตรวจสอบสถานะ service\n", "The operation failed. Check the service status.\n")+err.Error())
	} else if action == "stop-quit" {
		systray.Quit()
		return
	}
	t.refreshServiceState()
}

func controlService(m ctl.Manager, action string) error {
	switch action {
	case "start":
		if state, err := m.State(); err == nil && state == ctl.StateRunning {
			return nil
		}
		return m.Start()
	case "stop", "stop-quit":
		if state, err := m.State(); err == nil && state == ctl.StateStopped {
			return nil
		}
		if err := m.Stop(); err != nil {
			return err
		}
		if action == "stop-quit" {
			state, err := m.State()
			if err != nil {
				return err
			}
			if state != ctl.StateStopped {
				return fmt.Errorf("service did not reach stopped state (%s)", state)
			}
		}
		return nil
	case "restart":
		return m.Restart()
	}
	return fmt.Errorf("unknown service action %q", action)
}

func (t *tray) serviceManager() ctl.Manager {
	if t.manager != nil {
		return t.manager
	}
	return ctl.New()
}

// Startup is a single attempt, never a watchdog: an intentional Stop stays
// stopped until a menu action or a new tray session. Explicit URLs and live
// foreground agents must not start an unrelated machine service.
func (t *tray) autoStart(ctx context.Context, client *agentClient) {
	if client.override != "" || client.foregroundRunning(ctx) {
		return
	}
	t.serviceOps.Lock()
	defer t.serviceOps.Unlock()
	if ctx.Err() != nil || t.manualControl {
		return
	}
	if err := ctl.EnsureRunning(t.serviceManager()); err != nil {
		log.Printf("automatic agent start: %v", err)
	}
}

// pollService keeps the Agent submenu enabled or disabled against the
// service's real state. It polls because the OS service managers offer no
// change callback a tray could subscribe to.
func (t *tray) pollService(ctx context.Context) {
	t.refreshServiceState()
	t.refreshHealth(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.refreshServiceState()
			t.refreshHealth(ctx)
		}
	}
}

func (t *tray) refreshServiceState() {
	t.serviceUI.Lock()
	defer t.serviceUI.Unlock()
	stateFn := t.serviceState
	if stateFn == nil {
		stateFn = t.serviceManager().State
	}
	state, err := stateFn()
	t.observedState, t.serviceErr = state, err
	t.renderAgentStatus()
	t.mAgentToggle.Disable()
	t.mAgentRestart.Disable()
	if t.mStopQuit != nil {
		t.mStopQuit.Disable()
	}
	if err != nil {
		t.mAgent.SetTooltip(t.lang.text("ควบคุม service ไม่ได้ — ", "Service control unavailable — ") + err.Error())
		return
	}
	t.mAgentRestart.Enable()
	if t.mStopQuit != nil {
		t.mStopQuit.Enable()
	}
	t.mAgent.SetTooltip(t.lang.text("ไม่ทราบสถานะ service", "Service state unknown"))
	switch state {
	case ctl.StateRunning:
		t.mAgentToggle.SetTitle(t.lang.text("พัก agent…", "Pause agent…"))
		t.mAgentToggle.SetTooltip(t.lang.text("หยุดบริการอ่านบัตรจนกว่าจะทำงานต่อ", "Stop card reading for all clients until Resume"))
		t.mAgentToggle.Enable()
		t.mAgent.SetTooltip(t.lang.text("agent กำลังทำงาน", "Agent running"))
	case ctl.StateStopped:
		t.mAgentToggle.SetTitle(t.lang.text("ให้ agent ทำงานต่อ", "Resume agent"))
		t.mAgentToggle.SetTooltip(t.lang.text("เริ่มบริการอ่านบัตร", "Start the agent service"))
		t.mAgentToggle.Enable()
		t.mAgentRestart.Disable()
		t.mAgent.SetTooltip(t.lang.text("agent หยุดอยู่", "Agent stopped"))
	}
}

func (t *tray) baseURL() string { t.mu.RLock(); defer t.mu.RUnlock(); return t.url }
func (t *tray) setEndpoint(url string) {
	t.mu.Lock()
	t.url = url
	t.mu.Unlock()
	t.serviceUI.Lock()
	t.renderAgentStatus()
	t.serviceUI.Unlock()
	if runtime.GOOS == "linux" {
		t.notifyOnce.Do(func() { go checkStatusNotifier(url, t.lang) })
	}
}
func (t *tray) connectionState(state string) {
	t.serviceUI.Lock()
	defer t.serviceUI.Unlock()
	t.connection = state
	t.renderAgentStatus()
	switch state {
	case "connected":
		t.setUp()
	case "unauthorized":
		t.mStatus.SetTitle(t.lang.text("agent ต้องการสิทธิ์เชื่อมต่อ", "Agent authentication required"))
		t.mStatus.SetTooltip(t.lang.text("เชื่อมต่อ agent ได้ แต่ไม่มีสิทธิ์รับข้อมูลบัตร", "Agent is reachable but the card socket refused authentication"))
	case "unsupported":
		t.mStatus.SetTitle(t.lang.text("agent ไม่เปิด WebSocket", "Agent WebSocket unavailable"))
		t.mStatus.SetTooltip(t.lang.text("เปิดหน้าตั้งค่าเพื่อเปิด WebSocket", "Open Settings to enable WebSocket"))
	default:
		t.setDown()
	}
}

func (t *tray) applyStatus(status map[string]any) {
	t.serviceUI.Lock()
	defer t.serviceUI.Unlock()
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
		line = t.lang.text("กำลังอ่านบัตร…", "Reading card…")
	case model.StateCardPresent:
		line = t.lang.text("บัตรอยู่ในเครื่องอ่าน", "Card in reader")
	default:
		line = t.lang.text("รอบัตร", "Waiting for card")
	}
	if len(names) > 0 {
		watching := selected
		if watching == "" {
			watching = fmt.Sprintf(t.lang.text("ทุกเครื่องอ่าน (%d)", "All readers (%d)"), len(names))
		}
		line += " — " + watching
	}
	health, _ := status["health"].(string)
	switch health {
	case "no-reader":
		line = t.lang.text("ไม่พบเครื่องอ่านบัตร", "No card reader")
	case "reader-busy":
		line = t.lang.text("เครื่องอ่านถูกใช้งาน กำลังลองใหม่", "Reader busy; retrying")
	case "read-failed":
		line = t.lang.text("อ่านบัตรไม่สำเร็จ", "Card read failed")
	case "pcsc-unavailable":
		line = t.lang.text("บริการ PC/SC ไม่พร้อม", "PC/SC unavailable")
	}
	if stamp, ok := status["last_read_at"].(string); ok {
		t.lastReadAt = stamp
	}
	tooltip := line
	if t.lastReadAt != "" {
		tooltip += " — " + t.lang.text("อ่านสำเร็จล่าสุด: ", "Last successful read: ") + t.lastReadAt
	}
	t.mStatus.SetTitle(line)
	t.mStatus.SetTooltip(tooltip)
}

func (t *tray) setUp() {
	t.mStatus.SetTitle(t.lang.text("เชื่อมต่อ agent แล้ว", "Agent connected"))
}

// setDown says the agent is not reachable, with the terminal way to start it
// for this platform as a fallback when service control is unavailable.
func (t *tray) setDown() {
	hint := startHint(t.lang)
	line := t.lang.text("ติดต่อ agent ไม่ได้ — ", "Cannot reach agent — ") + hint
	t.mStatus.SetTitle(t.lang.text("ติดต่อ agent ไม่ได้", "Cannot reach agent"))
	t.mStatus.SetTooltip(line)
	log.Printf("cannot reach the agent at %s; %s", t.baseURL(), hint)
}

func startHint(l language) string {
	switch runtime.GOOS {
	case "windows":
		return l.text("เริ่มด้วยคำสั่ง `net start thai-smartcard-agent`", "Start with `net start thai-smartcard-agent`")
	case "darwin":
		return l.text("เริ่มด้วยคำสั่ง `sudo thai-smartcard-agent service start`", "Start with `sudo thai-smartcard-agent service start`")
	default:
		return l.text("เริ่มด้วยคำสั่ง `sudo systemctl start thai-smartcard-agent`", "Start with `sudo systemctl start thai-smartcard-agent`")
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

// Poll only public operational metadata; no card data or config is fetched.
func (t *tray) refreshHealth(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL()+"/api/health", nil)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var h struct {
		State      string `json:"state"`
		Readers    []any  `json:"readers"`
		Selected   string `json:"selected"`
		CardState  string `json:"card_state"`
		LastReadAt string `json:"last_read_at"`
	}
	if json.NewDecoder(resp.Body).Decode(&h) != nil {
		return
	}
	t.applyStatus(map[string]any{"health": h.State, "state": h.CardState, "readers": h.Readers, "selected": h.Selected, "last_read_at": h.LastReadAt})
}

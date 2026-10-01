// Command tray is the Thai Smartcard tray app: a thin client of the agent's
// /api. It never touches the config file and never spawns the agent
// (decision 12) — when it cannot reach the agent it says so with the command
// to start it and keeps polling.
//
// The tray needs cgo (fyne-io/systray), which is why it is its own binary and
// not a flag of the agent: the agent stays cross-compilable.
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
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
// from the agent over the network: the card state from the /ws broadcast, the
// options and the token from /api/settings.
type tray struct {
	client *http.Client

	mu       sync.Mutex
	cfg      config.Config // the last served config, without the token
	version  string
	tokenSet bool

	mStatus *systray.MenuItem
	mTest   *systray.MenuItem
	mConfig *systray.MenuItem
	mExpose *systray.MenuItem
	mFace   *systray.MenuItem
	mLaser  *systray.MenuItem
	mNhso   *systray.MenuItem
	mQuit   *systray.MenuItem
}

func onReady() {
	systray.SetIcon(iconBytes)
	systray.SetTitle("Thai Smartcard")
	systray.SetTooltip("Thai Smartcard Agent")

	t := &tray{client: &http.Client{Timeout: 5 * time.Second}}

	t.mStatus = systray.AddMenuItem("กำลังต่อกับ agent…", "Waiting for the agent")
	t.mStatus.Disable()
	systray.AddSeparator()
	t.mTest = systray.AddMenuItem("เปิดหน้าทดสอบ / Open test page", "Open the agent's test page")
	t.mConfig = systray.AddMenuItem("ตั้งค่า / Settings", "Open /settings")
	systray.AddSeparator()
	// Decision 16: a checkable toggle only while a token exists; before that
	// it opens /settings, where the token ceremony happens.
	t.mExpose = systray.AddMenuItemCheckbox("เปิดให้เครือข่ายเข้าถึง / Expose to network", "Serve the agent beyond localhost", false)
	systray.AddSeparator()
	t.mFace = systray.AddMenuItemCheckbox("อ่านรูปหน้า / Read face image", "read_face_image", false)
	t.mLaser = systray.AddMenuItemCheckbox("อ่านเลขหลังบัตร / Read laser ID", "read_laser_id", false)
	t.mNhso = systray.AddMenuItemCheckbox("อ่านสิทธิการรักษา / Read NHSO", "read_nhso", false)
	systray.AddSeparator()
	// Quit closes the tray for this session only; the agent keeps running.
	t.mQuit = systray.AddMenuItem("ออกจาก tray / Quit", "Close the tray; the agent keeps running")

	go t.watchCardState()
	go t.followOptionClicks()
	go t.refreshSettings()

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
		case <-t.mExpose.ClickedCh:
			go t.toggleExpose()
		case <-t.mFace.ClickedCh:
			go t.toggleCard(func(c *config.Card) { c.ReadFaceImage = !c.ReadFaceImage })
		case <-t.mLaser.ClickedCh:
			go t.toggleCard(func(c *config.Card) { c.ReadLaserID = !c.ReadLaserID })
		case <-t.mNhso.ClickedCh:
			go t.toggleCard(func(c *config.Card) { c.ReadNHSO = !c.ReadNHSO })
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
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
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

// followOptionClicks mirrors the agent's answers back into the checkmarks,
// so a hand edit or a second settings page cannot leave the menu lying.
func (t *tray) followOptionClicks() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		t.refreshSettings()
	}
}

func (t *tray) refreshSettings() {
	cfg, version, tokenSet, err := t.fetchSettings()
	if err != nil {
		// The settings toggles stay as they are; the status line already
		// says the agent is unreachable.
		return
	}
	t.mu.Lock()
	t.cfg, t.version, t.tokenSet = cfg, version, tokenSet
	t.mu.Unlock()

	setChecked(t.mFace, cfg.Card.ReadFaceImage)
	setChecked(t.mLaser, cfg.Card.ReadLaserID)
	setChecked(t.mNhso, cfg.Card.ReadNHSO)

	exposed := !config.IsLoopbackListen(cfg.Server.Listen)
	if tokenSet {
		t.mExpose.Enable()
		setChecked(t.mExpose, exposed)
		t.mExpose.SetTooltip("Serve the agent beyond localhost; a token exists")
	} else {
		setChecked(t.mExpose, false)
		t.mExpose.SetTooltip("Open /settings first: the agent generates the token there")
	}
}

type settingsResponse struct {
	Config   config.Config `json:"config"`
	Version  string        `json:"version"`
	TokenSet bool          `json:"token_set"`
}

func (t *tray) fetchSettings() (config.Config, string, bool, error) {
	resp, err := t.client.Get(*agentURL + "/api/settings")
	if err != nil {
		return config.Config{}, "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return config.Config{}, "", false, fmt.Errorf("GET /api/settings: %s", resp.Status)
	}
	var body settingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return config.Config{}, "", false, err
	}
	return body.Config, body.Version, body.TokenSet, nil
}

// toggleExpose implements the expose toggle. Off is always a direct toggle —
// it is the safe direction. On is direct only while a token exists; before
// that it opens /settings, where the agent generates and shows one.
func (t *tray) toggleExpose() {
	t.mu.Lock()
	tokenSet := t.tokenSet
	t.mu.Unlock()
	if !tokenSet {
		openBrowser(*agentURL + "/settings")
		return
	}

	err := t.save(func(c *config.Config) {
		if config.IsLoopbackListen(c.Server.Listen) {
			c.Server.Listen = "0.0.0.0"
		} else {
			c.Server.Listen = "127.0.0.1"
		}
	})
	t.afterSave(err)
}

func (t *tray) toggleCard(mutate func(*config.Card)) {
	err := t.save(func(c *config.Config) { mutate(&c.Card) })
	t.afterSave(err)
}

// save PUTs the config with the fingerprint it was served. A 409 means the
// file changed on disk since it was served: refetch and ask the user to
// repeat the action (decision 14).
func (t *tray) save(mutate func(*config.Config)) error {
	t.mu.Lock()
	cfg, version := t.cfg, t.version
	t.mu.Unlock()
	mutate(&cfg)

	raw, err := json.Marshal(map[string]any{"config": cfg, "version": version})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, *agentURL+"/api/settings", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SMC-Settings", "1")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		t.refreshSettings()
		return nil
	case http.StatusConflict:
		t.refreshSettings()
		return errors.New("the file changed on disk; reload done, repeat the action")
	default:
		return fmt.Errorf("save failed: %s", resp.Status)
	}
}

func (t *tray) afterSave(err error) {
	if err != nil {
		log.Println(err)
		return
	}
}

// setChecked drives the menu checkmark from state, because the click and the
// answer do not land in the same order.
func setChecked(item *systray.MenuItem, checked bool) {
	if checked != item.Checked() {
		if checked {
			item.Check()
		} else {
			item.Uncheck()
		}
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

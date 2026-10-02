package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/internal/discovery"
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/server"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

func vacantPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func coordinatorFixture(t *testing.T) (*settingsCoordinator, string, chan model.Message) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := config.Default()
	cfg.Server.Port = vacantPort(t)
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	endpointPath := filepath.Join(dir, "runtime", "endpoint.json")
	broadcast := make(chan model.Message)
	c := &settingsCoordinator{path: path, current: cfg, store: smc.NewOptionsStore(cardOptions(cfg.Card)), selection: smc.NewReaderStore(cfg.Card.Reader)}
	c.openPublisher = func() (*discovery.Publisher, error) { return discovery.Open(endpointPath, "fixture", false) }
	c.serverConfig = func(next config.Config) server.ServerConfig {
		result := serverCfg(next, path, broadcast, nil, nil)
		result.InstanceID = "fixture"
		result.ApplySettings = c.apply
		return result
	}
	mgr, err := server.Start(c.serverConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	c.manager = mgr
	c.publishStartup()
	t.Cleanup(func() {
		mgr.Close()
		if c.publisher != nil {
			c.publisher.Close()
		}
	})
	return c, endpointPath, broadcast
}

func TestSettingsPortMigrationReturnsCompleteResponseAndBroadcasts(t *testing.T) {
	c, path, broadcast := coordinatorFixture(t)
	old := c.manager.PlainAddr()
	oldSocket, _, err := websocket.DefaultDialer.Dial("ws://"+old+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer oldSocket.Close()
	version, _ := config.Fingerprint(c.path)
	next := c.current
	next.Server.Port = vacantPort(t)
	body, _ := json.Marshal(map[string]any{"config": next, "version": version})
	req, _ := http.NewRequest(http.MethodPut, "http://"+old+"/api/settings", bytes.NewReader(body))
	req.Header.Set("X-SMC-Settings", "1")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal("settings response was cut", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %s", res.StatusCode, raw)
	}
	var result struct {
		EndpointURL string `json:"endpoint_url"`
		Version     string `json:"version"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	e, err := discovery.Read(path)
	if err != nil || e.BaseURL != result.EndpointURL || e.Generation != 2 {
		t.Fatalf("endpoint %#v, response %s, error %v", e, raw, err)
	}
	loaded, err := config.Load(c.path)
	if err != nil || loaded.Server.Port != next.Server.Port {
		t.Fatalf("config = %#v, %v", loaded, err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(result.EndpointURL, "http", "ws", 1)+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	msg := model.Message{Event: "smc-status", Payload: map[string]any{"state": model.StateCardPresent}}
	select {
	case broadcast <- msg:
	case <-time.After(time.Second):
		t.Fatal("broadcast loop blocked after migration")
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var got model.Message
	if err = conn.ReadJSON(&got); err != nil || got.Event != "smc-status" {
		t.Fatalf("broadcast %v, %v", got, err)
	}
	_ = oldSocket.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = oldSocket.ReadMessage(); err == nil {
		t.Fatal("old socket was not retired")
	}
	deadline := time.Now().Add(time.Second)
	for {
		probe, err := net.DialTimeout("tcp", old, 50*time.Millisecond)
		if err != nil {
			break
		}
		probe.Close()
		if time.Now().After(deadline) {
			t.Fatal("old port remained open")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSettingsFailuresPreserveConfigEndpointAndRuntime(t *testing.T) {
	for _, kind := range []string{"occupied", "bad-certificate", "publish", "stale", "invalid", "config-write", "endpoint-prepare"} {
		t.Run(kind, func(t *testing.T) {
			c, path, _ := coordinatorFixture(t)
			original, _ := os.ReadFile(c.path)
			original = append([]byte("# hand-written comment\n"), original...)
			os.WriteFile(c.path, original, 0600)
			endpoint, _ := os.ReadFile(path)
			old := c.manager.PlainAddr()
			version, _ := config.Fingerprint(c.path)
			next := c.current
			next.Server.Port = vacantPort(t)
			switch kind {
			case "occupied":
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				next.Server.Port = ln.Addr().(*net.TCPAddr).Port
			case "bad-certificate":
				next.TLS.Enabled = true
				next.TLS.Mode = "files"
				next.TLS.Port = vacantPort(t)
				next.TLS.CertFile = "missing.crt"
				next.TLS.KeyFile = "missing.key"
			case "publish":
				c.commitPublication = func(*discovery.Publication) error { return errors.New("injected publication failure") }
			case "stale":
				version = "stale"
			case "invalid":
				next.Server.Port = 0
			case "config-write":
				c.path = filepath.Join(c.path, "not-a-directory")
				version = ""
			case "endpoint-prepare":
				c.preparePublication = func(*discovery.Publisher, string) (*discovery.Publication, error) {
					return nil, errors.New("injected endpoint prepare failure")
				}
			}
			if result, err := c.apply(next, version); err == nil {
				if result.AfterResponse != nil {
					result.AfterResponse()
				}
				t.Fatal("failed save reported success")
			}
			if c.manager.PlainAddr() != old || c.current.Server.Port == next.Server.Port {
				t.Fatal("failed save changed runtime/current")
			}
			if kind != "config-write" {
				raw, _ := os.ReadFile(c.path)
				if !bytes.Equal(raw, original) {
					t.Fatal("failed save changed config bytes")
				}
			}
			{
				raw, _ := os.ReadFile(path)
				if !bytes.Equal(raw, endpoint) {
					t.Fatal("failed save changed endpoint")
				}
			}
		})
	}
}

func TestSettingsRollbackRefusesInterveningHandEdit(t *testing.T) {
	c, _, _ := coordinatorFixture(t)
	version, _ := config.Fingerprint(c.path)
	next := c.current
	next.Server.Port = vacantPort(t)
	handEdit := []byte("# administrator changed the file during apply\n")
	c.commitPublication = func(*discovery.Publication) error {
		if err := os.WriteFile(c.path, handEdit, 0600); err != nil {
			t.Fatal(err)
		}
		return errors.New("publish failed")
	}
	_, err := c.apply(next, version)
	if err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("error = %v", err)
	}
	raw, _ := os.ReadFile(c.path)
	if !bytes.Equal(raw, handEdit) {
		t.Fatal("rollback overwrote hand edit")
	}
}

func TestSettingsConcurrentSavesHaveOneWinner(t *testing.T) {
	c, _, _ := coordinatorFixture(t)
	version, _ := config.Fingerprint(c.path)
	first := c.current
	second := c.current
	first.Server.Port = vacantPort(t)
	second.Server.Port = vacantPort(t)
	results := make(chan error, 2)
	for _, next := range []config.Config{first, second} {
		go func(next config.Config) {
			result, err := c.apply(next, version)
			if result.AfterResponse != nil {
				result.AfterResponse()
			}
			results <- err
		}(next)
	}
	winners, stale := 0, 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				winners++
			} else if errors.Is(err, config.ErrStale) {
				stale++
			} else {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent saves deadlocked")
		}
	}
	if winners != 1 || stale != 1 {
		t.Fatalf("winners %d, stale %d", winners, stale)
	}
}

func TestStartupDiscoveryFailureDoesNotStopServingAndCanRecover(t *testing.T) {
	c, path, _ := coordinatorFixture(t)
	c.publisher.Close()
	c.publisher = nil
	open := c.openPublisher
	c.openPublisher = func() (*discovery.Publisher, error) { return nil, errors.New("runtime directory unavailable") }
	c.publishStartup()
	client := &http.Client{Timeout: time.Second}
	res, err := client.Get("http://" + c.manager.PlainAddr() + "/api/info")
	if err != nil {
		t.Fatal("discovery failure stopped the agent", err)
	}
	res.Body.Close()
	version, _ := config.Fingerprint(c.path)
	next := c.current
	next.Server.Port = vacantPort(t)
	if _, err = c.apply(next, version); err == nil {
		t.Fatal("changed endpoint without a publisher")
	}
	c.openPublisher = open
	result, err := c.apply(next, version)
	if err != nil {
		t.Fatal("discovery did not recover", err)
	}
	if result.AfterResponse != nil {
		result.AfterResponse()
	}
	e, err := discovery.Read(path)
	if err != nil || e.BaseURL != result.EndpointURL {
		t.Fatalf("recovered endpoint = %#v, %v", e, err)
	}
}

func TestSettingsAPIDistinguishesValidationConflictAndApplyFailure(t *testing.T) {
	for _, test := range []struct {
		kind   string
		status int
	}{
		{"invalid", 400}, {"stale", 409}, {"occupied", 500}, {"rollback-conflict", 500},
	} {
		t.Run(test.kind, func(t *testing.T) {
			c, _, _ := coordinatorFixture(t)
			url := "http://" + c.manager.PlainAddr() + "/api/settings"
			version, _ := config.Fingerprint(c.path)
			next := c.current
			next.Server.Port = vacantPort(t)
			switch test.kind {
			case "invalid":
				next.Server.Port = 0
			case "stale":
				version = "stale"
			case "occupied":
				ln, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer ln.Close()
				next.Server.Port = ln.Addr().(*net.TCPAddr).Port
			case "rollback-conflict":
				c.commitPublication = func(*discovery.Publication) error {
					if err := os.WriteFile(c.path, []byte("# hand edit after the transaction wrote\n"), 0600); err != nil {
						t.Fatal(err)
					}
					return errors.New("publish failed")
				}
			}
			body, _ := json.Marshal(map[string]any{"config": next, "version": version})
			req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
			req.Header.Set("X-SMC-Settings", "1")
			client := &http.Client{Timeout: 2 * time.Second}
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			raw, _ := io.ReadAll(res.Body)
			if res.StatusCode != test.status {
				t.Fatalf("status %d, want %d: %s", res.StatusCode, test.status, raw)
			}
			if test.kind == "rollback-conflict" && !bytes.Contains(raw, []byte("rollback failed")) {
				t.Fatal("rollback error was hidden", string(raw))
			}
		})
	}
}

func TestSettingsReaderSelectionSurvivesFullControlQueue(t *testing.T) {
	c, _, _ := coordinatorFixture(t)
	controls := make(chan smc.Control, 8)
	for len(controls) < cap(controls) {
		controls <- smc.Control{Kind: smc.ControlReportStatus}
	}
	next := c.current
	next.Card.Reader = "second reader"
	version, _ := config.Fingerprint(c.path)
	result, err := c.apply(next, version)
	if err != nil {
		t.Fatal(err)
	}
	if result.AfterResponse != nil {
		result.AfterResponse()
	}
	if got := c.selection.Get(); got != next.Card.Reader {
		t.Fatalf("persisted selection lost: %q", got)
	}
	loaded, _, err := config.LoadVersion(c.path)
	if err != nil || loaded.Card.Reader != c.selection.Get() {
		t.Fatal("disk/runtime selection differ")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := make(chan model.Message, 32)
	done := make(chan error, 1)
	go func() {
		done <- smc.NewSmartCardWith(transport.NewFakeTransport([]string{"second reader"}, nil)).StartDaemonWith(ctx, smc.DaemonConfig{Control: controls, Selection: c.selection, Broadcast: messages})
	}()
	defer func() { cancel(); <-done }()
	select {
	case msg := <-messages:
		status, ok := msg.Payload.(model.Status)
		if !ok || status.Selected != next.Card.Reader {
			t.Fatal("daemon did not observe saved selection")
		}
	case <-time.After(time.Second):
		t.Fatal("daemon blocked behind full control queue")
	}
}

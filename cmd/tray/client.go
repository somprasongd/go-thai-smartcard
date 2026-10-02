package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/internal/discovery"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

type agentClient struct {
	override    string
	userPath    string
	paths       []string
	http        *http.Client
	poll, retry time.Duration
}

func newAgentClient(override string) *agentClient {
	paths := []string{}
	userPath := ""
	if path, err := discovery.UserPath(); err == nil {
		paths = append(paths, path)
		userPath = path
	}
	paths = append(paths, discovery.ServicePath())
	return &agentClient{userPath: userPath, override: strings.TrimRight(override, "/"), paths: paths, http: &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, poll: 2 * time.Second, retry: 5 * time.Second}
}

func (c *agentClient) foregroundRunning(ctx context.Context) bool {
	if c.userPath == "" {
		return false
	}
	e, err := discovery.Read(c.userPath)
	return err == nil && c.verified(ctx, e)
}

func (c *agentClient) verified(ctx context.Context, e discovery.Endpoint) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.BaseURL+"/api/info", nil)
	if err != nil {
		return false
	}
	res, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var info struct {
		InstanceID string `json:"instance_id"`
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&info) == nil && info.InstanceID == e.InstanceID
}

func (c *agentClient) resolve(ctx context.Context, current discovery.Endpoint, connected bool, force bool) discovery.Endpoint {
	if c.override != "" {
		return discovery.Endpoint{BaseURL: c.override}
	}
	for _, path := range c.paths {
		e, err := discovery.Read(path)
		if err != nil {
			continue
		}
		if connected && e == current && !force {
			return e
		}
		if c.verified(ctx, e) {
			return e
		}
	}
	// A missing/half-replaced/stale discovery file must not interrupt a live
	// connection. Once disconnected the verified service candidate may win.
	if connected {
		return current
	}
	return discovery.Endpoint{BaseURL: discovery.DefaultURL}
}

type readResult struct {
	conn    *websocket.Conn
	message model.Message
	err     error
}

func readMessages(ctx context.Context, conn *websocket.Conn, events chan<- readResult) {
	for {
		var msg model.Message
		err := conn.ReadJSON(&msg)
		select {
		case events <- readResult{conn, msg, err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

// run owns the connection and resolver state. Only its callbacks cross into
// tray UI state, so a late read from a retired connection cannot repaint it.
func (c *agentClient) run(ctx context.Context, onEndpoint func(string), onState func(string), onMessage func(model.Message)) {
	events := make(chan readResult, 16)
	ticker := time.NewTicker(c.poll)
	defer ticker.Stop()
	retry := time.NewTimer(c.retry)
	defer retry.Stop()
	var conn *websocket.Conn
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()
	current := c.resolve(ctx, discovery.Endpoint{}, false, true)
	onEndpoint(current.BaseURL)
	connect := func() {
		u, err := url.Parse(current.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			onState("unavailable")
			retry.Reset(c.retry)
			return
		}
		if u.Scheme == "https" {
			u.Scheme = "wss"
		} else {
			u.Scheme = "ws"
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/ws"
		dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
		var response *http.Response
		conn, response, err = dialer.DialContext(ctx, u.String(), nil)
		if response != nil && response.Body != nil {
			defer response.Body.Close()
		}
		if err != nil {
			state := "unavailable"
			if response != nil {
				if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
					state = "unauthorized"
				}
				if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusOK {
					state = "unsupported"
				}
			}
			onState(state)
			retry.Reset(c.retry)
			return
		}
		retry.Stop()
		onState("connected")
		// Ask for the current status rather than wait for a future card event.
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		_ = conn.WriteJSON(model.Command{Action: "get-status"})
		go readMessages(ctx, conn, events)
	}
	reconcile := func(force bool) bool {
		next := c.resolve(ctx, current, conn != nil, force)
		if next == current {
			return false
		}
		if conn != nil {
			_ = conn.Close()
			conn = nil
		}
		current = next
		onEndpoint(current.BaseURL)
		return true
	}
	connect()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reconcile(false) {
				connect()
			}
		case <-retry.C:
			if conn == nil {
				reconcile(true)
				connect()
			}
		case event := <-events:
			if event.conn != conn {
				continue
			}
			if event.err != nil {
				_ = conn.Close()
				conn = nil
				onState("unavailable")
				retry.Reset(c.retry)
				continue
			}
			onMessage(event.message)
		}
	}
}

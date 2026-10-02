package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/internal/discovery"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

func endpointFile(t *testing.T, path, id, base string, generation uint64) {
	t.Helper()
	raw, err := json.Marshal(discovery.Endpoint{SchemaVersion: 1, InstanceID: id, Generation: generation, BaseURL: base})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestClientRechecksEndpointBeforeRetryAfterDisconnection(t *testing.T) {
	connections := make(chan string, 8)
	var attempts, probes atomic.Int32
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/info" {
			probes.Add(1)
			json.NewEncoder(w).Encode(map[string]string{"instance_id": "retry"})
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		n := attempts.Add(1)
		connections <- strconv.Itoa(int(n))
		if n == 1 {
			return
		}
		for {
			if _, _, err = conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer s.Close()
	path := filepath.Join(t.TempDir(), "endpoint.json")
	endpointFile(t, path, "retry", s.URL, 1)
	c := newAgentClient("")
	c.paths = []string{path}
	c.poll = 20 * time.Millisecond
	c.retry = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.run(ctx, func(string) {}, func(string) {}, func(model.Message) {}) }()
	defer func() { cancel(); <-done }()
	awaitIdentity(t, connections, "1")
	awaitIdentity(t, connections, "2")
	if probes.Load() < 2 {
		t.Fatal("retry did not verify the endpoint again")
	}
}

func mockAgent(t *testing.T, id *atomic.Value, connections chan<- string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/info" {
			json.NewEncoder(w).Encode(map[string]string{"instance_id": id.Load().(string)})
			return
		}
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		identity := id.Load().(string)
		select {
		case connections <- identity:
		default:
		}
		var command model.Command
		if err = conn.ReadJSON(&command); err != nil {
			return
		}
		if command.Action != "get-status" {
			return
		}
		if err = conn.WriteJSON(model.Message{Event: "smc-status", Payload: map[string]any{"instance": identity}}); err != nil {
			return
		}
		for {
			if _, _, err = conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func TestResolverPrecedenceValidationAndExplicitOverride(t *testing.T) {
	var userID, serviceID atomic.Value
	userID.Store("user")
	serviceID.Store("service")
	u := mockAgent(t, &userID, make(chan string, 8))
	s := mockAgent(t, &serviceID, make(chan string, 8))
	dir := t.TempDir()
	user := filepath.Join(dir, "user.json")
	service := filepath.Join(dir, "service.json")
	endpointFile(t, service, "service", s.URL, 1)
	c := newAgentClient("")
	c.paths = []string{user, service}
	resolve := func(want string) {
		t.Helper()
		got := c.resolve(context.Background(), discovery.Endpoint{}, false, true)
		if got.BaseURL != want {
			t.Fatalf("resolved %s, want %s", got.BaseURL, want)
		}
	}
	resolve(s.URL)
	endpointFile(t, user, "user", u.URL, 1)
	resolve(u.URL)
	endpointFile(t, user, "wrong-instance", u.URL, 1)
	resolve(s.URL)
	os.WriteFile(user, []byte("{partial"), 0600)
	resolve(s.URL)
	os.Remove(user)
	os.Remove(service)
	resolve(discovery.DefaultURL)
	c.override = "http://127.0.0.1:1234"
	resolve(c.override)
}

func awaitIdentity(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("connection %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tray did not connect to", want)
	}
}

func TestClientFollowsChangesWhileOldSocketIsStillConnected(t *testing.T) {
	connections := make(chan string, 16)
	var firstID, secondID atomic.Value
	firstID.Store("first")
	secondID.Store("second")
	first := mockAgent(t, &firstID, connections)
	second := mockAgent(t, &secondID, connections)
	path := filepath.Join(t.TempDir(), "endpoint.json")
	endpointFile(t, path, "first", first.URL, 1)
	c := newAgentClient("")
	c.paths = []string{path}
	c.poll = 20 * time.Millisecond
	c.retry = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	urls := make(chan string, 16)
	messages := make(chan model.Message, 16)
	go func() {
		defer close(done)
		c.run(ctx, func(u string) { urls <- u }, func(string) {}, func(m model.Message) { messages <- m })
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("client did not stop")
		}
	}()
	awaitIdentity(t, connections, "first")
	if got := <-urls; got != first.URL {
		t.Fatal(got)
	}
	// File disappearance cannot tear down a still healthy connection.
	os.Remove(path)
	select {
	case got := <-connections:
		t.Fatal("unexpected reconnect", got)
	case <-time.After(70 * time.Millisecond):
	}
	endpointFile(t, path, "second", second.URL, 2)
	awaitIdentity(t, connections, "second")
	if got := <-urls; got != second.URL {
		t.Fatal("menu URL did not move", got)
	}
	// A replacement process at the same URL must also cause reconnection.
	secondID.Store("restarted")
	endpointFile(t, path, "restarted", second.URL, 1)
	awaitIdentity(t, connections, "restarted")
	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-messages:
			if payload, ok := msg.Payload.(map[string]any); ok && payload["instance"] == "restarted" {
				return
			}
		case <-deadline:
			t.Fatal("no status from restarted instance")
		}
	}
}

func TestClientDistinguishesAuthenticationFailure(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }))
	defer s.Close()
	c := newAgentClient(s.URL)
	c.poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	states := make(chan string, 8)
	go func() {
		defer close(done)
		c.run(ctx, func(string) {}, func(s string) { states <- s }, func(model.Message) {})
	}()
	defer func() { cancel(); <-done }()
	select {
	case state := <-states:
		if state != "unauthorized" {
			t.Fatal(state)
		}
	case <-time.After(time.Second):
		t.Fatal("no authentication state")
	}
}

func TestClientRetainsExplicitURLWhenDiscoveryChanges(t *testing.T) {
	connections := make(chan string, 8)
	var id atomic.Value
	id.Store("explicit")
	s := mockAgent(t, &id, connections)
	path := filepath.Join(t.TempDir(), "endpoint.json")
	endpointFile(t, path, "different", "http://127.0.0.1:1", 1)
	c := newAgentClient(s.URL)
	c.paths = []string{path}
	c.poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); c.run(ctx, func(string) {}, func(string) {}, func(model.Message) {}) }()
	defer func() { cancel(); <-done }()
	awaitIdentity(t, connections, "explicit")
	endpointFile(t, path, "different", "http://127.0.0.1:2", 2)
	select {
	case got := <-connections:
		t.Fatal("explicit URL reconnected", got)
	case <-time.After(70 * time.Millisecond):
	}
}

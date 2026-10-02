package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

// TestAPIReadersServesCachedStatus checks the route, the loopback guard and
// the shape: the reader list comes from the cache, not from any hardware.
func TestAPIReadersServesCachedStatus(t *testing.T) {
	cache := &StatusCache{}
	cache.Record(model.Message{
		Event: "smc-status",
		Payload: model.Status{
			Readers:  []string{"Identive CLOUD 2700 R"},
			Selected: "Identive CLOUD 2700 R",
			State:    model.StateCardPresent,
		},
	})

	srv := newStatusServer(t, cache)
	code, body, _ := getJSON(t, srv.URL+"/api/readers", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/readers: status %d, want 200", code)
	}
	readers, ok := body["readers"].([]any)
	if !ok || len(readers) != 1 || readers[0] != "Identive CLOUD 2700 R" {
		t.Fatalf("readers = %v, want the cached one", body["readers"])
	}
	if body["selected"] != "Identive CLOUD 2700 R" {
		t.Fatalf("selected = %v, want the cached selection", body["selected"])
	}
}

// An agent that has not broadcast a status yet answers an empty list rather
// than null, so a page can iterate it without a special case.
func TestAPIReadersWithoutStatusIsEmptyList(t *testing.T) {
	srv := newStatusServer(t, &StatusCache{})
	code, body, _ := getJSON(t, srv.URL+"/api/readers", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/readers: status %d, want 200", code)
	}
	readers, ok := body["readers"].([]any)
	if !ok || len(readers) != 0 {
		t.Fatalf("readers = %v, want an empty list", body["readers"])
	}
}

// The broadcast pump is what fills the cache, even when no socket transport
// re-publishes the message.
func TestBroadcastPumpFeedsStatusCache(t *testing.T) {
	broadcast := make(chan model.Message, 4)
	cache := &StatusCache{}
	srv := newStatusServer(t, cache, broadcast)
	defer srv.Close()

	broadcast <- model.Message{
		Event:   "smc-status",
		Payload: model.Status{Readers: []string{"Reader A"}},
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := cache.Snapshot(); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the pump never recorded the broadcast status")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// newStatusServer is newSettingsServer plus a status cache, and optionally a
// broadcast channel wired into the mux the way the agent runs it.
func newStatusServer(t *testing.T, cache *StatusCache, broadcast ...chan model.Message) *httptest.Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Write(path, config.Default()); err != nil {
		t.Fatal(err)
	}
	cfg := ServerConfig{
		Listen:     "127.0.0.1",
		Port:       9898,
		Transports: []string{"ws"},
		Version:    "test",
		ConfigPath: path,
		Status:     cache,
	}
	if len(broadcast) > 0 {
		cfg.Broadcast = broadcast[0]
	}
	srv := httptest.NewServer(newMux(cfg, nil))
	t.Cleanup(srv.Close)
	return srv
}

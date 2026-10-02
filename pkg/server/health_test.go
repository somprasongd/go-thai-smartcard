package server

import (
	"encoding/json"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthKeepsOperationalMetadataOnly(t *testing.T) {
	c := &StatusCache{}
	c.Record(model.Message{Event: "smc-data", Payload: map[string]string{"personal": "SYNTHETIC_PRIVATE_CARD"}})
	c.Record(model.Message{Event: "smc-error", Payload: map[string]string{"message": "SYNTHETIC_PRIVATE_ERROR"}})
	c.Record(model.Message{Event: "smc-status", Payload: model.Status{Readers: []string{"Reader"}, Health: "reader-busy", State: model.StateReading}})
	api := &settingsAPI{status: c}
	r := httptest.NewRequest(http.MethodGet, "http://localhost/api/health", nil)
	w := httptest.NewRecorder()
	api.serveHealth(w, r)
	if strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal("health retained card or raw error")
	}
	var h HealthSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h.State != "reader-busy" || h.LastReadAt == nil || h.LastErrorAt == nil {
		t.Fatalf("%+v", h)
	}
	c.Record(model.Message{Event: "smc-health", Payload: map[string]string{"state": "pcsc-unavailable"}})
	if len(c.Health().Readers) != 0 {
		t.Fatal("failed PC/SC retained stale readers")
	}
}
func TestHealthUsesLoopbackGuard(t *testing.T) {
	guard := &settingsGuard{}
	h := guard.wrap(http.HandlerFunc((&settingsAPI{}).serveHealth))
	r := httptest.NewRequest(http.MethodGet, "http://localhost/api/health", nil)
	r.RemoteAddr = "192.0.2.1:1000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote health status %d", w.Code)
	}
}

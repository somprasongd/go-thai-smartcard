package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

func TestStalledSubscriberDoesNotBlockHealthySubscriber(t *testing.T) {
	s := NewWS(nil)
	stalled := &connection{send: make(chan []byte, 1), done: make(chan struct{})}
	healthy := &connection{send: make(chan []byte, 2), done: make(chan struct{})}
	stalled.send <- []byte("backlog")
	s.register(stalled)
	s.register(healthy)
	done := make(chan struct{})
	go func() { s.Broadcast(model.Message{Event: "smc-status"}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber blocked broadcast")
	}
	select {
	case <-stalled.done:
	default:
		t.Fatal("full queue was not retired")
	}
	select {
	case <-healthy.send:
	default:
		t.Fatal("healthy subscriber missed event")
	}
	if len(s.snapshot()) != 1 {
		t.Fatal("stalled subscriber retained")
	}
	s.closeConnections()
}

func TestOversizeWebSocketCommandClosesPeer(t *testing.T) {
	s := NewWS(make(chan model.Command, 1))
	srv := httptest.NewServer(http.HandlerFunc(s.Handler))
	defer srv.Close()
	defer s.closeConnections()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", maxMessageSize+1))); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = c.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("expected 1009, got %v", err)
	}
}

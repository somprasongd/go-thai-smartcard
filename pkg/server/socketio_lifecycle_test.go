package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	enginews "github.com/googollee/go-socket.io/engineio/transport/websocket"
	"github.com/gorilla/websocket"
)

func TestSocketIORetirementStopsServeAndIncompleteHandshake(t *testing.T) {
	for generation := 0; generation < 6; generation++ {
		s := NewSocketIO(nil)
		done := make(chan error, 1)
		go func() { done <- s.Serve() }()
		h := httptest.NewServer(s)
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/socket.io/?EIO=3&transport=websocket", nil)
		if err != nil {
			h.Close()
			t.Fatal(err)
		}
		c.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err = c.ReadMessage(); err != nil {
			t.Fatal(err)
		}
		// The client has Engine.IO OPEN but has never joined a namespace.
		s.closeConnections()
		select {
		case err := <-done:
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Serve: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("retirement leaked Serve")
		}
		c.SetReadDeadline(time.Now().Add(time.Second))
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				break
			}
			// Frames written before retirement may already be buffered.
		}
		c.Close()
		h.Close()
	}
}

func TestSocketIORetirementDuringHandshakes(t *testing.T) {
	for n := 0; n < 20; n++ {
		s := NewSocketIO(nil)
		done := make(chan error, 1)
		go func() { done <- s.Serve() }()
		h := httptest.NewServer(s)
		client := &http.Client{Timeout: time.Second}
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Go(func() {
				res, err := client.Get(h.URL + "/socket.io/?EIO=3&transport=polling")
				if err == nil {
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}
			})
		}
		s.closeConnections()
		wg.Wait()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Serve remained blocked")
		}
		h.Close()
	}
}

func TestEngineCloseReleasesQueuedHandshakes(t *testing.T) {
	e := engineio.NewServer(&engineio.Options{Transports: []transport.Transport{&enginews.Transport{CheckOrigin: allowOriginFunc}}})
	h := httptest.NewServer(e)
	defer h.Close()
	var clients []*websocket.Conn
	// With no Accept consumer, the second and later initialization workers block
	// on the connection queue. Close must release those sessions too.
	for i := 0; i < 4; i++ {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/?EIO=3&transport=websocket", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, err = c.ReadMessage(); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
	}
	e.Close()
	if _, err := e.Accept(); !errors.Is(err, io.EOF) {
		t.Fatalf("Accept after Close: %v", err)
	}
	for _, c := range clients {
		if _, _, err := c.ReadMessage(); err == nil {
			t.Fatal("queued handshake left open")
		}
	}
	if e.Count() != 0 {
		t.Fatal("sessions retained after Close")
	}
}

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The v4 socket.io transport is retired through its engine.io endpoint:
// closeConnections must reach a live client and end the session, not leave
// it hanging on a dead connection.
func TestSocketIORetirementReachesLiveClient(t *testing.T) {
	s := NewSocketIO(nil)
	h := httptest.NewServer(s)
	defer h.Close()

	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/socket.io/?EIO=4&transport=websocket", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	readFrame := func(what string) string {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return string(data)
	}

	// engine.io OPEN, then the socket.io namespace connect: MESSAGE "4"
	// carrying CONNECT "0".
	if f := readFrame("open"); !strings.HasPrefix(f, "0") {
		t.Fatalf("first frame = %q, want engine.io OPEN", f)
	}
	if err := c.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		t.Fatal(err)
	}
	if f := readFrame("namespace connect"); !strings.HasPrefix(f, "40") {
		t.Fatalf("namespace reply = %q, want a socket.io CONNECT ack", f)
	}

	s.closeConnections()

	// The client must learn the session ended — the engine.io close packet
	// with a shutdown behind it, or at least the transport saying goodbye.
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := c.ReadMessage()
	if err == nil {
		if strings.TrimSpace(string(data)) != "1" {
			t.Fatalf("post-close frame = %q, want the engine.io close packet", data)
		}
		if _, _, err := c.ReadMessage(); err == nil {
			t.Fatal("connection stayed open after the close packet")
		}
		return
	}
	if strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "EOF") {
		return
	}
	t.Fatalf("expected the session to end, got %v", err)
}

// A second live client on the same server is retired by the same call, and
// calling retirement twice is harmless.
func TestSocketIORetirementIsRepeatable(t *testing.T) {
	s := NewSocketIO(nil)
	h := httptest.NewServer(s)
	defer h.Close()

	dial := func() *websocket.Conn {
		t.Helper()
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/socket.io/?EIO=4&transport=websocket", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil { // OPEN
			t.Fatal(err)
		}
		return c
	}
	first := dial()
	defer first.Close()
	second := dial()
	defer second.Close()

	s.closeConnections()
	s.closeConnections() // must not panic or block

	for _, c := range []*websocket.Conn{first, second} {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				break // the session is done for this client
			}
		}
	}
}

// Polling clients mid-handshake must not wedge the endpoint when the
// generation retires: every request finishes, none hangs past its deadline.
func TestSocketIORetirementWithPollingHandshakes(t *testing.T) {
	s := NewSocketIO(nil)
	h := httptest.NewServer(s)
	defer h.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			res, err := client.Get(h.URL + "/socket.io/?EIO=4&transport=polling")
			if err == nil {
				_, _ = io.Copy(io.Discard, res.Body)
				res.Body.Close()
			}
		})
	}
	s.closeConnections()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a polling handshake outlived retirement")
	}
}

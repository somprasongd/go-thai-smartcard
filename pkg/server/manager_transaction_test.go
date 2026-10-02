package server

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

func TestManagerReusesTLSAndUnchangedHTTPBindings(t *testing.T) {
	cert, key := writeCert(t, t.TempDir(), "a")
	cfg := ServerConfig{Listen: "127.0.0.1", Port: freePort(t), Transports: []string{"ws"}, TLS: config.TLS{Enabled: true, Port: freePort(t), CertFile: cert, KeyFile: key}}
	mgr, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	oldTLS := mgr.tlsLn
	oldPlain := mgr.plainLn
	cfg.AllowedOrigins = []string{"http://example.com"}
	if err = mgr.Replace(cfg); err != nil {
		t.Fatal(err)
	}
	if mgr.plainLn != oldPlain || mgr.tlsLn != oldTLS {
		t.Fatal("rebound unchanged sockets")
	}
	cfg.Port = freePort(t)
	if err = mgr.Replace(cfg); err != nil {
		t.Fatal(err)
	}
	if mgr.tlsLn != oldTLS {
		t.Fatal("rebound unchanged TLS socket")
	}
	if res, err := http.Get("http://" + mgr.PlainAddr()); err != nil {
		t.Fatal(err)
	} else {
		res.Body.Close()
	}
}

func TestManagerReplaceCanChangeBroadcastSource(t *testing.T) {
	cfg := ServerConfig{Listen: "127.0.0.1", Port: freePort(t), Transports: []string{"ws"}}
	cfg.Command = make(chan model.Command, 1)
	mgr, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	for range 2 {
		cfg.Broadcast = make(chan model.Message)
		if err = mgr.Replace(cfg); err != nil {
			t.Fatal(err)
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+mgr.PlainAddr()+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		// Dial returns when the upgrade response arrives, before the handler
		// necessarily registers its subscriber. A round-trip control command
		// proves registration finished before this one-shot broadcast.
		if err := conn.WriteJSON(model.Command{Action: "get-status"}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-cfg.Command:
		case <-time.After(time.Second):
			t.Fatal("replacement websocket handler was not ready")
		}

		select {
		case cfg.Broadcast <- model.Message{Event: "smc-status"}:
		case <-time.After(time.Second):
			t.Fatal("replacement broadcast source was not consumed")
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		var msg model.Message
		if err = conn.ReadJSON(&msg); err != nil || msg.Event != "smc-status" {
			t.Fatalf("broadcast %#v, %v", msg, err)
		}
		conn.Close()
		close(cfg.Broadcast)
	}
}

func TestManagerRetiresSocketIOConnections(t *testing.T) {
	cfg := ServerConfig{Listen: "127.0.0.1", Port: freePort(t), Transports: []string{"socketio"}}
	mgr, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+mgr.PlainAddr()+"/socket.io/?EIO=4&transport=websocket", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// v4 handshake: engine.io OPEN from the server, then the client joins
	// the default namespace and gets its ack back.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, payload, err := conn.ReadMessage(); err != nil || !strings.HasPrefix(string(payload), "0") {
		t.Fatal("socket.io handshake", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		t.Fatal("socket.io handshake", err)
	}
	if _, payload, err := conn.ReadMessage(); err != nil || !strings.HasPrefix(string(payload), "40") {
		t.Fatal("socket.io handshake", err)
	}
	cfg.Port = freePort(t)
	if err = mgr.Replace(cfg); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	for {
		if _, _, err = conn.ReadMessage(); err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("socket.io connection was not retired")
			}
			break
		}
	}
}

func TestManagerFailedPrepareAndAppliedRollbackKeepOldListener(t *testing.T) {
	cfg := ServerConfig{Listen: "127.0.0.1", Port: freePort(t), Transports: []string{"ws"}}
	mgr, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	old := mgr.PlainAddr()
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	next := cfg
	next.Port = busy.Addr().(*net.TCPAddr).Port
	if _, err = mgr.Prepare(next); err == nil {
		t.Fatal("bound occupied port")
	}
	next.Port = freePort(t)
	p, err := mgr.Prepare(next)
	if err != nil {
		t.Fatal(err)
	}
	p.Apply()
	newAddress := mgr.PlainAddr()
	p.Rollback()
	if mgr.PlainAddr() != old {
		t.Fatal("rollback changed active endpoint")
	}
	if res, err := http.Get("http://" + old); err != nil {
		t.Fatal(err)
	} else {
		res.Body.Close()
	}
	if conn, err := net.DialTimeout("tcp", newAddress, time.Second); err == nil {
		conn.Close()
		t.Fatal("rolled back listener remained open")
	}
}

func TestManagerRetiresUpgradedWebSockets(t *testing.T) {
	cfg := ServerConfig{Listen: "127.0.0.1", Port: freePort(t), Transports: []string{"ws"}}
	mgr, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+mgr.PlainAddr()+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cfg.Port = freePort(t)
	p, err := mgr.Prepare(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p.Apply()
	// The old socket is retained through apply, until response retirement.
	if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"get-status"}`)); err != nil {
		t.Fatal(err)
	}
	p.Retire()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = conn.ReadMessage(); err == nil {
		t.Fatal("retired upgraded socket remained open")
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("retirement only timed out; socket was not closed")
	}
}

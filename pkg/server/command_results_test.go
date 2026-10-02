package server

import (
	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCommandResultIsPrivateToRequester(t *testing.T) {
	s := NewWS(make(chan model.Command))
	srv := httptest.NewServer(http.HandlerFunc(s.Handler))
	defer srv.Close()
	defer s.closeConnections()
	connect := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b := connect(), connect()
	defer a.Close()
	defer b.Close()
	if err := a.WriteJSON(model.Command{Action: "read-now", RequestID: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	_ = a.SetReadDeadline(time.Now().Add(time.Second))
	var result struct {
		Event   string
		Payload model.CommandResult
	}
	if err := a.ReadJSON(&result); err != nil {
		t.Fatal(err)
	}
	if result.Event != "smc-command-result" || result.Payload.RequestID != "synthetic" || result.Payload.Status != "busy" {
		t.Fatalf("%+v", result)
	}
	_ = b.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, _, err := b.ReadMessage(); err == nil {
		t.Fatal("result leaked to another client")
	}
}

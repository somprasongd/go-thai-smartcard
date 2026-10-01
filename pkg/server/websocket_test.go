package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

// TestWebSocketBroadcastWhileClientsChurn drives register, unregister and
// Broadcast at the same time, which is what a card insert does while pages
// open and close. It only proves anything under `go test -race`: without the
// race detector an unguarded subscriber map usually survives it, and with it
// the unguarded version is reported at once.
func TestWebSocketBroadcastWhileClientsChurn(t *testing.T) {
	s := NewWS(nil)
	srv := httptest.NewServer(http.HandlerFunc(s.Handler))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	stop := make(chan struct{})
	var broadcasting sync.WaitGroup
	broadcasting.Add(1)
	go func() {
		defer broadcasting.Done()
		for {
			select {
			case <-stop:
				return
			default:
				s.Broadcast(model.Message{Event: "smc-inserted", Payload: map[string]string{"message": "x"}})
			}
		}
	}()

	var dialing sync.WaitGroup
	for i := 0; i < 8; i++ {
		dialing.Add(1)
		go func() {
			defer dialing.Done()
			for j := 0; j < 20; j++ {
				c, _, err := websocket.DefaultDialer.Dial(url, nil)
				if err != nil {
					t.Errorf("dial: %v", err)
					return
				}
				c.SetReadDeadline(time.Now().Add(time.Second))
				c.ReadMessage()
				c.Close()
			}
		}()
	}
	dialing.Wait()
	close(stop)
	broadcasting.Wait()
}

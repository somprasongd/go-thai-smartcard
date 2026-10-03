package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
	"path/filepath"
	"testing"
)

type captureTransport struct {
	*transport.FakeTransport
	waits, connects int
	canceled        bool
}

func (t *captureTransport) WaitCardPresent(ctx context.Context, _ []string) (int, error) {
	t.waits++
	if t.canceled {
		return -1, context.Canceled
	}
	if t.waits == 1 {
		return -1, transport.ErrCardTimeout
	}
	return 0, nil
}
func (t *captureTransport) Connect(reader string) (transport.Card, error) {
	t.connects++
	return t.FakeTransport.Connect(reader)
}

type captureCard struct {
	atr          []byte
	disconnected bool
	statusErr    error
}

func (c *captureCard) Status() (transport.Status, error) {
	return transport.Status{Atr: c.atr}, c.statusErr
}
func (c *captureCard) Transmit(cmd []byte) ([]byte, error) {
	if len(cmd) >= 2 && cmd[1] == 0xc0 {
		return []byte("1234567890123\x90\x00"), nil
	}
	if len(cmd) >= 2 && cmd[1] == 0xa4 {
		return []byte{0x90, 0x00}, nil
	}
	return []byte{0x61, 13}, nil
}
func (c *captureCard) Disconnect() error { c.disconnected = true; return nil }

func TestCaptureWaitsAndUsesATRResponseCommand(t *testing.T) {
	for _, atr := range [][]byte{{0x3b, 0x67}, {0x3b, 0x00}} {
		card := &captureCard{atr: atr}
		tr := &captureTransport{FakeTransport: transport.NewFakeTransport([]string{"reader"}, card)}
		out := filepath.Join(t.TempDir(), "trace.json")
		if err := capture(context.Background(), tr, out, "synthetic", "", "", false, false, false); err != nil {
			t.Fatal(err)
		}
		if tr.waits != 2 || tr.connects != 1 || !card.disconnected {
			t.Fatal("capture did not wait/release")
		}
		trace, err := transport.LoadTrace(out)
		if err != nil {
			t.Fatal(err)
		}
		expected := util.GetResponseCommand(atr)
		found := false
		for _, ex := range trace.Exchanges {
			cmd, _ := hex.DecodeString(ex.Command)
			if len(cmd) >= 2 && cmd[1] == 0xc0 {
				found = true
				if !bytes.Equal(cmd[:4], expected[:4]) {
					t.Fatalf("wrong ATR GET RESPONSE prefix: %x", cmd[:4])
				}
			}
		}
		if !found {
			t.Fatal("no GET RESPONSE recorded")
		}
	}
}
func TestCaptureCancellationAndStatusFailure(t *testing.T) {
	card := &captureCard{atr: []byte{0x3b, 0x67}, statusErr: errors.New("status unavailable")}
	tr := &captureTransport{FakeTransport: transport.NewFakeTransport([]string{"reader"}, card), canceled: true}
	if err := capture(context.Background(), tr, filepath.Join(t.TempDir(), "trace.json"), "synthetic", "", "", false, false, false); !errors.Is(err, context.Canceled) || tr.connects != 0 {
		t.Fatal("connected before insertion")
	}
	tr.canceled = false
	if err := capture(context.Background(), tr, filepath.Join(t.TempDir(), "trace.json"), "synthetic", "", "", false, false, false); err == nil || !card.disconnected {
		t.Fatal("failed capture left session open")
	}
}

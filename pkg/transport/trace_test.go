package transport_test

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

func TestTraceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "trace.json")

	original := &transport.Trace{
		Name: "thai-id",
		Note: "synthetic",
		Atr:  "3b679e0000000941318000000000318000",
		Exchanges: []transport.Exchange{
			{Command: "00a4040008a000000054480001", Response: "9000"},
		},
	}
	if err := original.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := transport.LoadTrace(path)
	if err != nil {
		t.Fatalf("LoadTrace: %v", err)
	}
	if loaded.Name != original.Name || loaded.Note != original.Note || loaded.Atr != original.Atr {
		t.Errorf("metadata mismatch: got %+v, want %+v", loaded, original)
	}
	if len(loaded.Exchanges) != 1 || loaded.Exchanges[0] != original.Exchanges[0] {
		t.Errorf("exchanges mismatch: got %+v, want %+v", loaded.Exchanges, original.Exchanges)
	}

	atr, err := loaded.ATR()
	if err != nil {
		t.Fatalf("ATR: %v", err)
	}
	if got, want := hex.EncodeToString(atr), original.Atr; got != want {
		t.Errorf("ATR = %s, want %s", got, want)
	}
}

func TestLoadTraceErrors(t *testing.T) {
	if _, err := transport.LoadTrace(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected an error for a missing file")
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.LoadTrace(bad); err == nil {
		t.Error("expected an error for invalid json")
	}
}

func TestFakeCardReplaysInOrder(t *testing.T) {
	card, err := transport.NewFakeCard(&transport.Trace{
		Atr: "3b67",
		Exchanges: []transport.Exchange{
			{Command: "00a4", Response: "9000"},
			{Command: "80b0", Response: "6c04"},
		},
	})
	if err != nil {
		t.Fatalf("NewFakeCard: %v", err)
	}

	steps := []struct {
		cmd []byte
		rsp string
	}{
		{cmd: []byte{0x00, 0xa4}, rsp: "9000"},
		{cmd: []byte{0x80, 0xb0}, rsp: "6c04"},
	}
	for i, step := range steps {
		rsp, err := card.Transmit(step.cmd)
		if err != nil {
			t.Fatalf("exchange %d: %v", i, err)
		}
		if got := hex.EncodeToString(rsp); got != step.rsp {
			t.Errorf("exchange %d response = %s, want %s", i, got, step.rsp)
		}
	}

	if card.ExchangesUsed() != 2 {
		t.Errorf("ExchangesUsed = %d, want 2", card.ExchangesUsed())
	}
	if card.Disconnected() {
		t.Error("card should not be disconnected yet")
	}
	if err := card.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if !card.Disconnected() {
		t.Error("card should report as disconnected")
	}
}

func TestFakeCardReportsCommandMismatch(t *testing.T) {
	card, err := transport.NewFakeCard(&transport.Trace{
		Atr:       "3b67",
		Exchanges: []transport.Exchange{{Command: "00a4", Response: "9000"}},
	})
	if err != nil {
		t.Fatalf("NewFakeCard: %v", err)
	}

	_, err = card.Transmit([]byte{0xff, 0xff})
	if err == nil {
		t.Fatal("expected an error for an unexpected command")
	}
	if got := err.Error(); got == "" {
		t.Error("expected a descriptive error")
	}
}

func TestFakeCardReportsExhaustedTrace(t *testing.T) {
	card, err := transport.NewFakeCard(&transport.Trace{Atr: "3b67"})
	if err != nil {
		t.Fatalf("NewFakeCard: %v", err)
	}
	if _, err := card.Transmit([]byte{0x00}); err == nil {
		t.Error("expected an error once the trace is exhausted")
	}
}

func TestFakeCardRejectsBadAtr(t *testing.T) {
	if _, err := transport.NewFakeCard(&transport.Trace{Atr: "zz"}); err == nil {
		t.Error("expected an error for a non hex atr")
	}
}

// recordingStub captures what a wrapper forwards.
type recordingStub struct {
	atr  []byte
	rsp  []byte
	cmds [][]byte
	err  error
}

func (s *recordingStub) Status() (transport.Status, error) {
	return transport.Status{Atr: s.atr}, nil
}

func (s *recordingStub) Transmit(cmd []byte) ([]byte, error) {
	s.cmds = append(s.cmds, append([]byte(nil), cmd...))
	if s.err != nil {
		return nil, s.err
	}
	return s.rsp, nil
}

func (s *recordingStub) Disconnect() error { return nil }

func TestRecordingCardCapturesAtrAndExchanges(t *testing.T) {
	stub := &recordingStub{
		atr: []byte{0x3b, 0x67, 0x4e},
		rsp: []byte{0x90, 0x00},
	}
	rec := transport.Record(stub, "thai-id")

	// The ATR is only captured on the first Status call.
	rec.Transmit([]byte{0x00, 0xa4})
	if _, err := rec.Status(); err != nil {
		t.Fatalf("Status: %v", err)
	}
	rec.Transmit([]byte{0x80, 0xb0})

	trace := rec.Trace()
	if trace.Name != "thai-id" {
		t.Errorf("Name = %q, want %q", trace.Name, "thai-id")
	}
	if got, want := trace.Atr, "3b674e"; got != want {
		t.Errorf("Atr = %q, want %q", got, want)
	}
	if len(trace.Exchanges) != 2 {
		t.Fatalf("got %d exchanges, want 2", len(trace.Exchanges))
	}
	if trace.Exchanges[0].Command != "00a4" || trace.Exchanges[0].Response != "9000" {
		t.Errorf("exchange 0 = %+v", trace.Exchanges[0])
	}
	if trace.Exchanges[1].Command != "80b0" {
		t.Errorf("exchange 1 = %+v", trace.Exchanges[1])
	}
	if len(stub.cmds) != 2 {
		t.Errorf("stub saw %d commands, want 2", len(stub.cmds))
	}
}

func TestFakeTransportHonoursCancelledContext(t *testing.T) {
	tr := transport.NewFakeTransport([]string{"r0"}, &recordingStub{atr: []byte{0x3b, 0x67}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := tr.WaitCardPresent(ctx, nil); err == nil {
		t.Error("expected an error from WaitCardPresent on a cancelled context")
	}
	if _, err := tr.WaitCardRemove(ctx, nil); err == nil {
		t.Error("expected an error from WaitCardRemove on a cancelled context")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !tr.Closed() {
		t.Error("transport should report as closed")
	}
}

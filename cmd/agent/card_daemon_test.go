package main

import (
	"context"
	"errors"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
	"reflect"
	"testing"
	"time"
)

type retryTransport struct {
	transport.Transport
	wait   func(context.Context, []string) (int, error)
	closed bool
	card   transport.Card
}

func (r *retryTransport) WaitCardPresent(ctx context.Context, readers []string) (int, error) {
	return r.wait(ctx, readers)
}
func (r *retryTransport) Close() error { r.closed = true; return nil }

func TestCardDaemonReopensBackendAndUsesLatestSelection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	selection := smc.NewReaderStore("first")
	first := &retryTransport{Transport: transport.NewFakeTransport([]string{"first", "second"}, nil)}
	first.wait = func(_ context.Context, readers []string) (int, error) {
		if !reflect.DeepEqual(readers, []string{"first"}) {
			t.Errorf("first watch: %v", readers)
		}
		selection.Set("second")
		return -1, errors.New("broker restarted")
	}
	second := &retryTransport{Transport: transport.NewFakeTransport([]string{"first", "second"}, nil)}
	second.wait = func(_ context.Context, readers []string) (int, error) {
		if !reflect.DeepEqual(readers, []string{"second"}) {
			t.Errorf("retry lost selection: %v", readers)
		}
		cancel()
		return -1, ctx.Err()
	}
	calls := 0
	runCardDaemon(ctx, smc.DaemonConfig{Selection: selection}, func() (transport.Transport, error) {
		calls++
		switch calls {
		case 1:
			return nil, errors.New("broker unavailable at startup")
		case 2:
			return first, nil
		default:
			return second, nil
		}
	}, time.Millisecond)
	if calls != 3 || !first.closed || !second.closed {
		t.Fatalf("factory calls=%d, backend cleanup=%v/%v", calls, first.closed, second.closed)
	}
}

func TestCardDaemonCancellationReleasesCardBeforeTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	card, _ := transport.NewFakeCard(&transport.Trace{Atr: "3b67"})
	backend := &cleanupTransport{FakeTransport: transport.NewFakeTransport([]string{"reader"}, card), card: card, cancel: cancel}
	runCardDaemon(ctx, smc.DaemonConfig{}, func() (transport.Transport, error) { return backend, nil }, time.Millisecond)
	if !backend.Closed() || !card.Disconnected() {
		t.Fatal("shutdown did not release resources")
	}
	if !backend.cardReleasedAtClose {
		t.Fatal("transport closed before card session")
	}
}

type cleanupTransport struct {
	*transport.FakeTransport
	card                *transport.FakeCard
	cancel              context.CancelFunc
	cardReleasedAtClose bool
}

func (t *cleanupTransport) WaitCardRemove(ctx context.Context, _ []string) (int, error) {
	t.cancel()
	return -1, ctx.Err()
}
func (t *cleanupTransport) Close() error {
	t.cardReleasedAtClose = t.card.Disconnected()
	return t.FakeTransport.Close()
}

package smc_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/apdu"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/smc"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
)

func TestOptionsStoreGetSet(t *testing.T) {
	tests := []struct {
		name string
		seed smc.Options
		set  smc.Options
		// doSet is false in the case that only covers the seed, so the zero
		// value can be the value under test.
		doSet bool
		want  smc.Options
	}{
		{
			name: "a seeded store returns its seed",
			seed: smc.Options{ShowFaceImage: true, ShowLaserData: true},
			want: smc.Options{ShowFaceImage: true, ShowLaserData: true},
		},
		{
			name:  "Set replaces every field rather than merging",
			seed:  smc.Options{ShowFaceImage: true, ShowLaserData: true, ShowNhsoData: true},
			set:   smc.Options{ShowLaserData: true},
			doSet: true,
			want:  smc.Options{ShowLaserData: true},
		},
		{
			name:  "the store can be switched off entirely",
			seed:  smc.Options{ShowFaceImage: true, ShowLaserData: true},
			set:   smc.Options{},
			doSet: true,
			want:  smc.Options{},
		},
		{
			name:  "a later Set wins over an earlier one",
			seed:  smc.Options{},
			set:   smc.Options{ShowFaceImage: true, ShowNhsoData: true},
			doSet: true,
			want:  smc.Options{ShowFaceImage: true, ShowNhsoData: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := smc.NewOptionsStore(tt.seed)
			if got := store.Get(); got != tt.seed {
				t.Errorf("Get() = %+v, want the seed %+v", got, tt.seed)
			}
			if tt.doSet {
				store.Set(tt.set)
			}
			if got := store.Get(); got != tt.want {
				t.Errorf("Get() after Set = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Get hands out a copy, so a caller cannot reach back into the store through
// the value it was given. The daemon relies on that to read a consistent
// snapshot while a command goroutine is writing.
func TestOptionsStoreGetReturnsACopy(t *testing.T) {
	store := smc.NewOptionsStore(smc.Options{ShowFaceImage: true})

	snapshot := store.Get()
	snapshot.ShowFaceImage = false
	snapshot.ShowLaserData = true

	if got := store.Get(); got != (smc.Options{ShowFaceImage: true}) {
		t.Errorf("store changed through a returned copy: %+v", got)
	}
}

// A nil store is the "no options configured" case: the daemon and the command
// loop can call it without each checking.
func TestOptionsStoreNilIsUsable(t *testing.T) {
	var store *smc.OptionsStore

	store.Set(smc.Options{ShowLaserData: true})

	if got := store.Get(); !got.ShowFaceImage {
		t.Errorf("Get() on a nil store = %+v, want the default options", got)
	}
}

// The store is written by the command loop and read by the daemon loop, so it
// has to hold up under both at once. Meaningful under -race.
func TestOptionsStoreConcurrentGetAndSet(t *testing.T) {
	tests := []struct {
		name  string
		turn  func(smc.Options) smc.Options
		check func(smc.Options) bool
	}{
		{
			name:  "face image toggles",
			turn:  func(o smc.Options) smc.Options { o.ShowFaceImage = !o.ShowFaceImage; return o },
			check: func(o smc.Options) bool { return true },
		},
		{
			name:  "laser toggles",
			turn:  func(o smc.Options) smc.Options { o.ShowLaserData = !o.ShowLaserData; return o },
			check: func(o smc.Options) bool { return true },
		},
		{
			name: "nhso toggles",
			turn: func(o smc.Options) smc.Options { o.ShowNhsoData = !o.ShowNhsoData; return o },
			check: func(o smc.Options) bool {
				// Each field moves on its own, so a torn read would show up
				// as an option that is not the zero value and not the one
				// that was written.
				return o == (smc.Options{}) || o == (smc.Options{ShowNhsoData: true})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := smc.NewOptionsStore(smc.Options{})

			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(2)
				go func() {
					defer wg.Done()
					for j := 0; j < 200; j++ {
						store.Set(tt.turn(store.Get()))
					}
				}()
				go func() {
					defer wg.Done()
					for j := 0; j < 200; j++ {
						if got := store.Get(); !tt.check(got) {
							t.Errorf("Get() = %+v, want an option that was actually written", got)
							return
						}
					}
				}()
			}
			wg.Wait()
		})
	}
}

// gatedTransport holds a card in the reader until the test lets it go, so the
// daemon cannot run past the read the test is asserting on. Without it the
// fake transport reports the card present and gone immediately, and the loop
// would race through the trace.
type gatedTransport struct {
	*transport.FakeTransport
	removed chan struct{}
	once    sync.Once
}

func newGatedTransport(t *testing.T, trace *transport.Trace) *gatedTransport {
	t.Helper()

	card, err := transport.NewFakeCard(trace)
	if err != nil {
		t.Fatalf("NewFakeCard: %v", err)
	}
	tr := transport.NewFakeTransport([]string{"Fake Reader 0"}, card)
	t.Cleanup(func() { _ = tr.Close() })

	return &gatedTransport{FakeTransport: tr, removed: make(chan struct{})}
}

func (t *gatedTransport) WaitCardRemove(ctx context.Context, readers []string) (int, error) {
	select {
	case <-t.removed:
		return t.FakeTransport.WaitCardRemove(ctx, readers)
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// release lets the card leave the reader, once.
func (t *gatedTransport) release() {
	t.once.Do(func() { close(t.removed) })
}

// twoReadLaserTrace is one personal read, a second personal read, then the
// laser applet. Replay is strict and positional, so a second read needs its
// own personal exchanges recorded; with the laser applet only at the end, a
// read that asks for the laser is the only one that can consume it.
func twoReadLaserTrace() *transport.Trace {
	b := newTraceBuilder()
	appendPersonal(b, false)
	appendPersonal(b, false)
	b.selectApplet(apdu.CardCMD.Select)
	b.readLaser(apdu.CardCMD.LaserId, payloadLaser)
	return b.trace
}

// The point of the store: a change made between two inserts has to be visible
// to the next read, without restarting the daemon. The trace holds two
// personal reads followed by the laser applet, so the second read can only
// complete with a laser id if the option was applied in between.
func TestStartDaemonCtxWithAppliesAnOptionChangeMidLoop(t *testing.T) {
	gate := newGatedTransport(t, twoReadLaserTrace())
	reader := smc.NewSmartCardWith(gate)

	broadcast := make(chan model.Message, 64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := smc.NewOptionsStore(smc.Options{})
	done := make(chan error, 1)
	go func() {
		done <- reader.StartDaemonCtxWith(ctx, broadcast, store)
	}()

	// The daemon blocks on its broadcasts when nothing reads them, so keep
	// draining for the whole test and let the assertions poll what arrived.
	var mu sync.Mutex
	var reads []*model.Data
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-broadcast:
				if msg.Event != "smc-data" {
					continue
				}
				data, _ := msg.Payload.(*model.Data)
				mu.Lock()
				reads = append(reads, data)
				mu.Unlock()
			}
		}
	}()

	collected := func() []*model.Data {
		mu.Lock()
		defer mu.Unlock()
		return append([]*model.Data(nil), reads...)
	}
	waitForReads := func(t *testing.T, n int) []*model.Data {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if got := collected(); len(got) >= n {
				return got
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %d reads, saw %d", n, len(collected()))
		return nil
	}

	first := waitForReads(t, 1)[0]
	if first.Card != nil {
		t.Fatalf("first read already has a laser id: %+v", first.Card)
	}
	if first.Personal == nil {
		t.Fatal("first read has no personal data")
	}

	store.Set(smc.Options{ShowLaserData: true})
	gate.release()

	second := waitForReads(t, 2)[1]
	if second.Card == nil {
		t.Fatalf("second read has no card section although ShowLaserData was turned on: %+v", second)
	}
	if second.Card.LaserId != "AB1234567890" {
		t.Errorf("LaserId = %q", second.Card.LaserId)
	}

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("daemon returned no error after cancel")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop after cancel")
	}
}

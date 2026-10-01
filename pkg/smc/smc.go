package smc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"github.com/somprasongd/go-thai-smartcard/pkg/transport"
	"github.com/somprasongd/go-thai-smartcard/pkg/util"
)

// ErrNoReaders is returned when no reader is attached.
var ErrNoReaders = errors.New("not available readers")

// readerRetryInterval is how long to wait before retrying after a transient
// reader problem, such as the reader being unplugged.
const readerRetryInterval = 2 * time.Second

// Options selects which parts of the card to read.
type Options struct {
	ShowFaceImage bool
	ShowNhsoData  bool
	ShowLaserData bool
}

func defaultOptions() *Options {
	return &Options{
		ShowFaceImage: true,
		ShowNhsoData:  false,
		ShowLaserData: false,
	}
}

// OptionsStore holds the options a running daemon reads for every card insert.
//
// The daemon re-reads the options on each insert rather than capturing them
// once, so a long running agent picks up a change from a client without a
// restart. Get returns a copy for the same reason: readCard takes an Options
// value, and handing it the live struct would let it read a struct that
// another goroutine is writing.
type OptionsStore struct {
	mu   sync.RWMutex
	opts Options
}

// NewOptionsStore returns a store seeded with opts.
func NewOptionsStore(opts Options) *OptionsStore {
	return &OptionsStore{opts: opts}
}

// Get returns a copy of the current options.
func (s *OptionsStore) Get() Options {
	if s == nil {
		return *defaultOptions()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.opts
}

// Set replaces the current options.
func (s *OptionsStore) Set(opts Options) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opts = opts
}

// SmartCard reads Thai ID cards through a transport.
type SmartCard struct {
	transport transport.Transport
}

// NewSmartCard returns a reader backed by PC/SC.
//
// It preserves the original constructor for existing callers. Code that needs
// a different backend, or that wants to inject a fake, should use
// NewSmartCardWith.
func NewSmartCard() *SmartCard {
	t, err := defaultTransport()
	if err != nil {
		log.Printf("Error establishing smart card transport: %v", err)
	}
	return &SmartCard{transport: t}
}

// NewSmartCardWith returns a reader backed by the given transport.
func NewSmartCardWith(t transport.Transport) *SmartCard {
	return &SmartCard{transport: t}
}

// ListReaders returns the attached readers.
func (s *SmartCard) ListReaders() ([]string, error) {
	if s.transport == nil {
		return nil, errors.New("no transport configured")
	}
	return s.transport.ListReaders()
}

// Close releases the underlying transport.
func (s *SmartCard) Close() error {
	if s.transport == nil {
		return nil
	}
	return s.transport.Close()
}

// Read waits for a card, reads it once, and returns.
func (s *SmartCard) Read(readerName *string, opts *Options) (*model.Data, error) {
	if s.transport == nil {
		return nil, errors.New("no transport configured")
	}
	if opts == nil {
		opts = defaultOptions()
	}

	readers, err := s.readers(readerName)
	if err != nil {
		return nil, err
	}

	// The transport wait is bounded so a host can steer the daemon. A single
	// read has nothing to steer, so it simply asks again on each idle window.
	var index int
	for {
		log.Println("Waiting for a Card Inserted")
		var err error
		index, err = s.transport.WaitCardPresent(context.Background(), readers)
		if errors.Is(err, transport.ErrCardTimeout) {
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}

	card, data, err := s.readCard(readers[index], *opts)
	if card != nil {
		defer func() {
			if derr := card.Disconnect(); derr != nil {
				log.Printf("Error disconnecting card: %v", derr)
			}
		}()
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// StartDaemon reads cards as they are inserted and removed, until ctx is done
// or an unrecoverable error occurs.
func (s *SmartCard) StartDaemon(broadcast chan model.Message, opts *Options) error {
	return s.StartDaemonCtx(context.Background(), broadcast, opts)
}

// StartDaemonCtx is StartDaemon with cancellation, so a host can shut the
// loop down without killing the process.
func (s *SmartCard) StartDaemonCtx(ctx context.Context, broadcast chan model.Message, opts *Options) error {
	if opts == nil {
		opts = defaultOptions()
	}
	return s.StartDaemonCtxWith(ctx, broadcast, NewOptionsStore(*opts))
}

// StartDaemonCtxWith is StartDaemonCtx with the options in a store, so a host
// can change what the next card insert reads while the loop keeps running.
//
// The loop cannot be steered from here: use StartDaemonWith when a client needs
// to pick a reader or ask for another read.
func (s *SmartCard) StartDaemonCtxWith(ctx context.Context, broadcast chan model.Message, store *OptionsStore) error {
	return s.StartDaemonWith(ctx, DaemonConfig{Broadcast: broadcast, Options: store})
}

// readers resolves the reader list to use for a single read.
func (s *SmartCard) readers(readerName *string) ([]string, error) {
	if readerName != nil {
		return []string{*readerName}, nil
	}
	readers, err := s.transport.ListReaders()
	if err != nil {
		return nil, err
	}
	if len(readers) == 0 {
		return nil, ErrNoReaders
	}
	return readers, nil
}

// readCard connects to the card in reader and reads the selected applets.
//
// opts is a value rather than a pointer because the daemon snapshots it per
// insert, and a shared pointer could be swapped underneath the read.
//
// The named return values matter: a panic while reading is recovered and
// turned into an error. Without them the function used to return
// (nil, nil, nil), so a caller could not tell a crash from an empty read.
func (s *SmartCard) readCard(reader string, opts Options) (card transport.Card, data *model.Data, err error) {
	log.Printf("Connecting to card with %s", reader)

	card, err = s.transport.Connect(reader)
	if err != nil {
		log.Printf("connecting card error %s", err.Error())
		return card, nil, err
	}

	defer func() {
		if rcv := recover(); rcv != nil {
			err = fmt.Errorf("panic while reading card: %v", rcv)
			data = nil
			log.Println("Recover readCard:", rcv)
		}
	}()

	status, err := card.Status()
	if err != nil {
		log.Printf("get card status error %s", err.Error())
		return card, nil, err
	}

	cmd := util.GetResponseCommand(status.Atr)

	result := model.Data{}

	personalReader := NewPersonalReader(card, cmd)
	if err := personalReader.Select(); err != nil {
		return card, nil, fmt.Errorf("select personal applet: %w", err)
	}
	result.Personal = personalReader.Read(opts.ShowFaceImage)

	if opts.ShowLaserData {
		cardReader := NewCardReader(card, cmd)
		if err := cardReader.Select(); err != nil {
			return card, nil, fmt.Errorf("select card applet: %w", err)
		}
		result.Card = &model.Card{LaserId: cardReader.ReadLaserId()}
	}

	if opts.ShowNhsoData {
		nhsoReader := NewNhsoReader(card, cmd)
		if err := nhsoReader.Select(); err != nil {
			return card, nil, fmt.Errorf("select nhso applet: %w", err)
		}
		result.Nhso = nhsoReader.Read()
	}

	return card, &result, nil
}

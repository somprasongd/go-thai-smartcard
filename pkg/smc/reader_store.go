package smc

import "sync"

// ReaderStore keeps persisted selection independent of the best-effort command
// queue, so a busy read cannot lose a settings update and daemon retries see it.
type ReaderStore struct {
	mu     sync.RWMutex
	reader string
}

// NewReaderStore seeds the configuration that every daemon retry will observe.
func NewReaderStore(reader string) *ReaderStore { return &ReaderStore{reader: reader} }

// Get returns a snapshot so a settings save cannot race a transport wait.
func (s *ReaderStore) Get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reader
}

// Set retains the latest selection even while the card is being read.
func (s *ReaderStore) Set(reader string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reader = reader
}

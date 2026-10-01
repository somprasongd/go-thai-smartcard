package server

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// Manager owns the agent's HTTP listener and can move it to a new
// ServerConfig. A save from /settings or the tray that changes [server] or
// [tls] ends with a restart of the listener on the new values; the process,
// the read loop and the broadcast channel keep running through it.
type Manager struct {
	mu   sync.Mutex
	cfg  ServerConfig
	srv  *http.Server
	done chan struct{}
}

// Start builds the first listener and serves it. A failed bind is returned:
// for the agent that is fatal, because a service that cannot bind its port is
// not half-running.
func Start(cfg ServerConfig) (*Manager, error) {
	m := &Manager{}
	if err := m.replace(cfg); err != nil {
		return nil, err
	}
	return m, nil
}

// Replace restarts the listener on cfg. The new listener binds first, so a
// bad address leaves the old one serving and the save is reported as failed
// rather than taking the agent down. When the bind succeeds the old listener
// closes; connected pages reconnect to the new one.
func (m *Manager) Replace(cfg ServerConfig) error {
	return m.replace(cfg)
}

func (m *Manager) replace(cfg ServerConfig) error {
	addr := net.JoinHostPort(cfg.Listen, strconv.Itoa(cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	done := make(chan struct{})
	srv := &http.Server{Handler: newMux(cfg, done)}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server: %v", err)
		}
	}()

	m.mu.Lock()
	old := m.srv
	oldDone := m.done
	m.cfg, m.srv, m.done = cfg, srv, done
	m.mu.Unlock()

	if old != nil {
		// Close rather than Shutdown: a WebSocket connection never goes idle,
		// so a graceful drain would wait forever on the pages being cut. They
		// reconnect on their own.
		old.Close()
		close(oldDone)
	}
	return nil
}

// Config returns the configuration the listener currently serves.
func (m *Manager) Config() ServerConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

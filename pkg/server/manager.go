package server

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
)

// Manager owns the agent's HTTP listener and can move it to a new
// ServerConfig. A save from /settings or the tray that changes [server] or
// [tls] ends with a restart of the listener on the new values; the process,
// the read loop and the broadcast channel keep running through it.
//
// With TLS on there are two listeners: HTTPS on tls.port, and the plain HTTP
// listener forced to loopback, because a config page and a card broadcast
// have no business crossing a LAN in clear text once a certificate exists.
type Manager struct {
	mu sync.Mutex

	cfg      ServerConfig
	srv      *http.Server
	done     chan struct{}
	plainLn  net.Listener
	tlsLn    net.Listener
	plainStr string
	tlsStr   string
}

// Start builds the first listener and serves it. A failed bind or a bad
// certificate is returned: for the agent that is fatal, because a service
// that cannot serve is not half-running.
func Start(cfg ServerConfig) (*Manager, error) {
	m := &Manager{}
	if err := m.replace(cfg); err != nil {
		return nil, err
	}
	return m, nil
}

// Replace restarts the listener on cfg. The new listeners bind first, so a
// bad address or an unreadable certificate leaves the old listener serving
// and the save is reported as failed rather than taking the agent down. When
// the binds succeed the old listeners close; connected pages reconnect.
func (m *Manager) Replace(cfg ServerConfig) error {
	return m.replace(cfg)
}

func (m *Manager) replace(cfg ServerConfig) error {
	// Bind everything before anything is closed, so a failure leaves the
	// previous generation untouched.
	plainHost := cfg.Listen
	if cfg.TLS.Enabled && !config.IsLoopbackListen(plainHost) {
		// With TLS available the plain listener loses its network reach:
		// anything that needs the LAN uses https.
		plainHost = "127.0.0.1"
		log.Printf("TLS is on, forcing the plain HTTP listener to loopback (was %s)", cfg.Listen)
	}
	plainLn, err := net.Listen("tcp", net.JoinHostPort(plainHost, strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("listen on %s: %w", net.JoinHostPort(plainHost, strconv.Itoa(cfg.Port)), err)
	}

	var tlsLn net.Listener
	var loader *certLoader
	if cfg.TLS.Enabled {
		loader = &certLoader{certFile: cfg.TLS.CertFile, keyFile: cfg.TLS.KeyFile}
		if err := loader.load(); err != nil {
			plainLn.Close()
			return fmt.Errorf("load tls certificate: %w", err)
		}
		tlsLn, err = net.Listen("tcp", net.JoinHostPort(cfg.Listen, strconv.Itoa(cfg.TLS.Port)))
		if err != nil {
			plainLn.Close()
			return fmt.Errorf("listen on %s: %w", net.JoinHostPort(cfg.Listen, strconv.Itoa(cfg.TLS.Port)), err)
		}
		tlsLn = tls.NewListener(tlsLn, &tls.Config{GetCertificate: loader.getCertificate})
	}

	done := make(chan struct{})
	srv := &http.Server{Handler: newMux(cfg, done)}
	go func() {
		if err := srv.Serve(plainLn); err != nil && err != http.ErrServerClosed {
			log.Printf("http server: %v", err)
		}
	}()
	if tlsLn != nil {
		go func() {
			if err := srv.Serve(tlsLn); err != nil && err != http.ErrServerClosed {
				log.Printf("https server: %v", err)
			}
		}()
	}

	m.mu.Lock()
	old := m.srv
	oldDone := m.done
	m.cfg, m.srv, m.done = cfg, srv, done
	m.plainLn, m.tlsLn = plainLn, tlsLn
	m.plainStr = plainLn.Addr().String()
	if tlsLn != nil {
		m.tlsStr = tlsLn.Addr().String()
	} else {
		m.tlsStr = ""
	}
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

// Close stops the listener. The agent never calls it — a service runs until
// the process exits — but tests do.
func (m *Manager) Close() {
	m.mu.Lock()
	srv, done := m.srv, m.done
	m.srv, m.done = nil, nil
	m.mu.Unlock()
	if srv != nil {
		srv.Close()
	}
	if done != nil {
		close(done)
	}
}

// PlainAddr is the address the plain HTTP listener is bound to, "127.0.0.1:…"
// included, or "" before Start. With TLS on this is loopback even when the
// config asks for more.
func (m *Manager) PlainAddr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.plainStr
}

// TLSAddr is the address the HTTPS listener is bound to, or "" when TLS is
// off.
func (m *Manager) TLSAddr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tlsStr
}

// certLoader serves the TLS certificate from files mode, reloading when the
// certificate file changes. The check happens per handshake, so a renewed
// certificate is picked up without a restart and without a file watcher; a
// load that fails keeps the previous certificate and logs, because refusing
// every handshake over a bad renewal would take the listener down for good.
type certLoader struct {
	certFile string
	keyFile  string

	mu   sync.Mutex
	cert *tls.Certificate
	mod  int64 // mtime of the loaded certificate file, in nanoseconds
}

func (c *certLoader) load() error {
	cert, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return err
	}
	st, err := stat(c.certFile)
	if err == nil {
		c.mod = st
	}
	c.cert = &cert
	return nil
}

func (c *certLoader) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if st, err := stat(c.certFile); err == nil && st != c.mod {
		if err := c.load(); err != nil {
			// Keep serving the previous certificate and say why.
			log.Printf("tls: reloading %s failed, keeping the previous certificate: %v", c.certFile, err)
			return c.cert, nil
		}
		log.Printf("tls: reloaded %s", c.certFile)
	}
	return c.cert, nil
}

func stat(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.ModTime().UnixNano(), nil
}

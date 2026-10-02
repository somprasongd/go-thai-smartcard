package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

type generation struct {
	cfg       ServerConfig
	handler   http.Handler
	done      chan struct{}
	broadcast chan model.Message
	once      sync.Once
}

func (g *generation) close() { g.once.Do(func() { close(g.done) }) }

type router struct{ current atomic.Pointer[generation] }

func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	g := r.current.Load()
	if g == nil {
		http.Error(w, "listener is not ready", http.StatusServiceUnavailable)
		return
	}
	g.handler.ServeHTTP(w, req)
}

type binding struct {
	key         string
	ln          net.Listener
	srv         *http.Server
	router      *router
	certificate atomic.Pointer[certLoader]
	started     bool
}

func (b *binding) start(g *generation) {
	b.router.current.Store(g)
	if b.started {
		return
	}
	b.started = true
	ln := b.ln
	if b.certificate.Load() != nil {
		ln = tls.NewListener(ln, &tls.Config{GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return b.certificate.Load().getCertificate(hello)
		}})
	}
	go func() {
		if err := b.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("listener: %v", err)
		}
	}()
}
func (b *binding) close() { _ = b.srv.Close(); _ = b.ln.Close() }
func (b *binding) retire() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.srv.Shutdown(ctx); err != nil {
			b.close()
		}
	}()
}

// Manager retains unchanged sockets and serializes listener transactions.
// A generation stays alive until its settings response has been flushed.
type Manager struct {
	mu               sync.Mutex
	changes          sync.Mutex
	cfg              ServerConfig
	active           *generation
	bindings         map[string]*binding
	plainLn, tlsLn   net.Listener
	plainStr, tlsStr string
	stop             chan struct{}
	updates          chan struct{}
	stopOnce         sync.Once
}

func Start(cfg ServerConfig) (*Manager, error) {
	m := &Manager{bindings: make(map[string]*binding), stop: make(chan struct{}), updates: make(chan struct{}, 1)}
	p, err := m.Prepare(cfg)
	if err != nil {
		return nil, err
	}
	p.Apply()
	p.Retire()
	go m.pump()
	return m, nil
}
func (m *Manager) wakePump() {
	select {
	case m.updates <- struct{}{}:
	default:
	}
}
func (m *Manager) pump() {
	var closedSource chan model.Message
	for {
		m.mu.Lock()
		var source chan model.Message
		if m.active != nil {
			source = m.active.cfg.Broadcast
		}
		m.mu.Unlock()
		if source == closedSource {
			source = nil
		}
		select {
		case <-m.updates:
			continue
		case <-m.stop:
			return
		case msg, ok := <-source:
			if !ok {
				closedSource = source
				continue
			}
			m.mu.Lock()
			g := m.active
			m.mu.Unlock()
			if g != nil && g.broadcast != nil && g.cfg.Broadcast == source {
				select {
				case g.broadcast <- msg:
				case <-g.done:
				case <-m.stop:
					return
				}
			}
		}
	}
}

// Prepared reserves changed sockets without retiring the current generation.
// The caller must finish it with Abort, Rollback, or Retire.
type Prepared struct {
	manager              *Manager
	cfg                  ServerConfig
	next                 map[string]*binding
	fresh                []*binding
	loaders              map[*binding]*certLoader
	oldLoaders           map[*binding]*certLoader
	plain, tls           *binding
	oldBindings          map[string]*binding
	oldGeneration        *generation
	oldConfig            ServerConfig
	oldPlain, oldTLS     string
	oldPlainLn, oldTLSLn net.Listener
	applied              bool
	finished             bool
}

func (m *Manager) Prepare(cfg ServerConfig) (*Prepared, error) {
	m.changes.Lock()
	m.mu.Lock()
	p := &Prepared{manager: m, cfg: cfg, next: make(map[string]*binding), loaders: make(map[*binding]*certLoader), oldLoaders: make(map[*binding]*certLoader), oldBindings: m.bindings, oldGeneration: m.active, oldConfig: m.cfg, oldPlain: m.plainStr, oldTLS: m.tlsStr, oldPlainLn: m.plainLn, oldTLSLn: m.tlsLn}
	m.mu.Unlock()
	host := cfg.Listen
	if cfg.TLS.Enabled && !config.IsLoopbackListen(host) {
		host = "127.0.0.1"
	}
	bind := func(protocol, address string) (*binding, error) {
		key := protocol + ":" + address
		if b := p.oldBindings[key]; b != nil {
			p.next[key] = b
			return b, nil
		}
		ln, err := net.Listen("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("listen on %s: %w", address, err)
		}
		r := &router{}
		b := &binding{key: key, ln: ln, router: r, srv: &http.Server{Handler: r}}
		p.next[key] = b
		p.fresh = append(p.fresh, b)
		return b, nil
	}
	var err error
	p.plain, err = bind("http", net.JoinHostPort(host, strconv.Itoa(cfg.Port)))
	if err == nil && cfg.TLS.Enabled {
		loader := &certLoader{certFile: cfg.TLS.CertFile, keyFile: cfg.TLS.KeyFile}
		if err = loader.load(); err == nil {
			p.tls, err = bind("https", net.JoinHostPort(cfg.Listen, strconv.Itoa(cfg.TLS.Port)))
			if err == nil {
				p.loaders[p.tls] = loader
				p.oldLoaders[p.tls] = p.tls.certificate.Load()
			}
		}
	}
	if err != nil {
		p.Abort()
		return nil, err
	}
	return p, nil
}
func (p *Prepared) PlainAddr() string { return p.plain.ln.Addr().String() }
func (p *Prepared) Apply() {
	m := p.manager
	g := &generation{cfg: p.cfg, done: make(chan struct{})}
	cfg := p.cfg
	if cfg.Broadcast != nil {
		g.broadcast = make(chan model.Message, 64)
		cfg.Broadcast = g.broadcast
	}
	g.handler = newMux(cfg, g.done)
	m.mu.Lock()
	for b, loader := range p.loaders {
		b.certificate.Store(loader)
	}
	for _, b := range p.next {
		b.start(g)
	}
	m.active = g
	m.cfg = p.cfg
	m.bindings = p.next
	m.plainLn = p.plain.ln
	m.plainStr = p.plain.ln.Addr().String()
	m.tlsLn = nil
	m.tlsStr = ""
	if p.tls != nil {
		m.tlsLn = p.tls.ln
		m.tlsStr = p.tls.ln.Addr().String()
	}
	p.applied = true
	m.mu.Unlock()
	m.wakePump()
}
func (p *Prepared) Abort() {
	if p.finished {
		return
	}
	for _, b := range p.fresh {
		b.close()
	}
	p.finished = true
	p.manager.changes.Unlock()
}
func (p *Prepared) Rollback() {
	if p.finished {
		return
	}
	if !p.applied {
		p.Abort()
		return
	}
	m := p.manager
	m.mu.Lock()
	failed := m.active
	for _, b := range p.oldBindings {
		b.router.current.Store(p.oldGeneration)
	}
	for b, loader := range p.oldLoaders {
		if loader != nil {
			b.certificate.Store(loader)
		}
	}
	m.active = p.oldGeneration
	m.cfg = p.oldConfig
	m.bindings = p.oldBindings
	m.plainStr = p.oldPlain
	m.tlsStr = p.oldTLS
	m.plainLn = p.oldPlainLn
	m.tlsLn = p.oldTLSLn
	m.mu.Unlock()
	m.wakePump()
	failed.close()
	p.Abort()
}
func (p *Prepared) Retire() {
	if p.finished {
		return
	}
	if p.oldGeneration != nil {
		p.oldGeneration.close()
	}
	for key, b := range p.oldBindings {
		if p.next[key] != b {
			b.retire()
		}
	}
	p.finished = true
	p.manager.changes.Unlock()
}
func (m *Manager) Replace(cfg ServerConfig) error {
	p, err := m.Prepare(cfg)
	if err != nil {
		return err
	}
	p.Apply()
	p.Retire()
	return nil
}
func (m *Manager) Config() ServerConfig { m.mu.Lock(); defer m.mu.Unlock(); return m.cfg }
func (m *Manager) PlainAddr() string    { m.mu.Lock(); defer m.mu.Unlock(); return m.plainStr }
func (m *Manager) TLSAddr() string      { m.mu.Lock(); defer m.mu.Unlock(); return m.tlsStr }
func (m *Manager) Close() {
	m.changes.Lock()
	defer m.changes.Unlock()
	m.stopOnce.Do(func() { close(m.stop) })
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		m.active.close()
		m.active = nil
	}
	for _, b := range m.bindings {
		b.close()
	}
	m.bindings = make(map[string]*binding)
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

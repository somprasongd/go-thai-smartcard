// Package discovery publishes only the running agent's local address, never
// configuration, credentials or card data.
package discovery

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/somprasongd/go-thai-smartcard/internal/atomicfile"
)

const DefaultURL = "http://127.0.0.1:9898"

// Endpoint carries routing identity only; publishing config here would expose
// the service's secrets to every local user who can read discovery metadata.
type Endpoint struct {
	SchemaVersion int    `json:"schema_version"`
	InstanceID    string `json:"instance_id"`
	Generation    uint64 `json:"generation"`
	BaseURL       string `json:"base_url"`
}

// NewInstanceID distinguishes restarts even when the OS reuses a PID or port.
func NewInstanceID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// UserPath follows launch-user identity independently of a custom config path.
func UserPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "thai-smartcard", "endpoint.json"), nil
}

// ServicePath gives logged-in users one readable slot outside private config.
func ServicePath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "ThaiSmartcardEndpoint", "endpoint.json")
	case "darwin":
		return "/Library/Application Support/ThaiSmartcardEndpoint/endpoint.json"
	default:
		return "/var/lib/thai-smartcard/endpoint.json"
	}
}

// LocalURL converts wildcard binds into a reachable loopback URL. A listener
// bound only to a LAN address cannot supply the local settings API.
func LocalURL(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	} else if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("listener %s has no loopback endpoint", address)
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

// Validate prevents discovery from redirecting the tray to remote hosts or
// putting credentials in a URL intended for public metadata.
func (e Endpoint) Validate() error {
	u, err := url.Parse(e.BaseURL)
	if err != nil {
		return err
	}
	ip := net.ParseIP(u.Hostname())
	if e.SchemaVersion != 1 || e.InstanceID == "" || e.Generation == 0 ||
		u.Scheme != "http" || u.User != nil || u.Port() == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" ||
		(u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return errors.New("invalid local endpoint")
	}
	return nil
}

// Read accepts only supported, complete metadata; callers retain live sockets
// when this fails instead of treating a missing file as an agent shutdown.
func Read(path string) (Endpoint, error) {
	var e Endpoint
	raw, err := atomicfile.ReadFile(path)
	if err != nil {
		return e, err
	}
	if len(raw) > 4096 {
		return e, errors.New("endpoint file is too large")
	}
	if err = json.Unmarshal(raw, &e); err != nil {
		return e, err
	}
	return e, e.Validate()
}

// Publisher holds an OS lock so a second process cannot replace a live slot.
type Publisher struct {
	mu             sync.Mutex
	path, instance string
	mode           os.FileMode
	lock           *os.File
	generation     uint64
}

// Open separates public service metadata from private foreground metadata.
func Open(path, instance string, service bool) (*Publisher, error) {
	dirMode, fileMode := os.FileMode(0700), os.FileMode(0600)
	if service {
		dirMode, fileMode = 0755, 0644
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return nil, err
	}
	if err := secureDirectory(filepath.Dir(path), service); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, err
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("endpoint slot is already owned or cannot be locked: %w", err)
	}
	return &Publisher{path: path, instance: instance, mode: fileMode, lock: f}, nil
}

// Publication delays changing discovery until its listener is actually active.
type Publication struct {
	publisher *Publisher
	file      *atomicfile.Pending
	Endpoint  Endpoint
}

// Prepare discovers filesystem failures before config or runtime is changed.
func (p *Publisher) Prepare(baseURL string) (*Publication, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := Endpoint{1, p.instance, p.generation + 1, baseURL}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	f, err := atomicfile.Prepare(p.path, append(raw, '\n'), p.mode)
	if err != nil {
		return nil, err
	}
	return &Publication{p, f, e}, nil
}

// Commit advances generation only after the atomic replacement succeeds.
func (p *Publication) Commit() error {
	p.publisher.mu.Lock()
	defer p.publisher.mu.Unlock()
	if err := p.file.Commit(); err != nil {
		return err
	}
	p.publisher.generation = p.Endpoint.Generation
	return nil
}

// Abort removes staged metadata without changing the published generation.
func (p *Publication) Abort() { p.file.Abort() }

// Close removes only this instance's announcement and releases its slot lock.
func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, err := Read(p.path); err == nil && e.InstanceID == p.instance {
		_ = os.Remove(p.path)
	}
	if p.lock != nil {
		_ = p.lock.Close()
		p.lock = nil
	}
}

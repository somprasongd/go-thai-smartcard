// Package config loads, validates and writes the agent's config.toml.
//
// The file is the only source of configuration: environment variables are not
// read (decision 1 in docs/plan/v3-config-service-tray.md). Loading is strict —
// an unknown key or a bad value stops the agent and names the file and line,
// because a service that silently falls back to its defaults after a typo is
// worse than one that does not start.
package config

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	toml "github.com/BurntSushi/toml"
	"github.com/somprasongd/go-thai-smartcard/internal/atomicfile"
)

// Transport names accepted in [server] transports.
const (
	TransportWS       = "ws"
	TransportSocketIO = "socketio"
)

// tlsModeFiles is the only TLS mode until "auto" mode ships.
const tlsModeFiles = "files"

// ErrNotFound is returned by Load when the config file does not exist. The
// agent answers it by writing the defaults, when the directory allows it.
var ErrNotFound = errors.New("config file does not exist")

// ErrStale is returned by Save when the file on disk no longer matches the
// fingerprint the caller was served, so a hand edit is never silently
// overwritten by a save from the UI (decision 14).
var ErrStale = errors.New("the file changed on disk, reload before saving")

// Server is the [server] table: what the agent serves and who may connect.
type Server struct {
	// Listen is the address to bind, an IP or "localhost". "0.0.0.0" exposes
	// the agent to the network and requires a token.
	Listen string `toml:"listen" json:"listen"`
	Port   int    `toml:"port" json:"port"`
	// Transports selects what is served: "ws", "socketio", or both. An empty
	// list is a config error, and a disabled transport is never constructed.
	Transports []string `toml:"transports" json:"transports"`
	// AllowedOrigins lists the origins allowed to open a card socket. "*"
	// allows any origin, which is the default: it keeps a web app on its own
	// origin working out of the box, and the settings page warns about it.
	AllowedOrigins []string `toml:"allowed_origins" json:"allowed_origins"`
	// Token is required when listen is not loopback. The agent generates it
	// (decision 16); a hand-written file that exposes the agent without one is
	// refused by validate.
	Token string `toml:"token" json:"token"`
}

// Card is the [card] table: which applets are read from an inserted card, and
// which reader is watched. Changes apply on the next card insert.
type Card struct {
	ReadFaceImage bool `toml:"read_face_image" json:"read_face_image"`
	ReadLaserID   bool `toml:"read_laser_id" json:"read_laser_id"`
	ReadNHSO      bool `toml:"read_nhso" json:"read_nhso"`
	// Reader names the reader to watch; empty watches every attached one.
	Reader string `toml:"reader" json:"reader"`
}

// TLS is the [tls] table. Files mode is phase 3; the schema is here from the
// start so an administrator's file does not become invalid when it arrives.
type TLS struct {
	Enabled bool `toml:"enabled" json:"enabled"`
	Port    int  `toml:"port" json:"port"`
	// Mode is "files"; "auto" (a local CA) comes later.
	Mode string `toml:"mode" json:"mode"`
	// CertFile and KeyFile are the operator-supplied certificate and key.
	CertFile string `toml:"cert_file" json:"cert_file"`
	KeyFile  string `toml:"key_file" json:"key_file"`
	// Hostnames is for "auto" mode and unused in "files" mode.
	Hostnames []string `toml:"hostnames" json:"hostnames"`
}

// Config is the whole file.
type Config struct {
	Server Server `toml:"server" json:"server"`
	Card   Card   `toml:"card" json:"card"`
	TLS    TLS    `toml:"tls" json:"tls"`
}

// Default returns the configuration a fresh install runs with: loopback only,
// WebSocket only, every origin allowed, face image and laser ID on, NHSO off.
func Default() Config {
	return Config{
		Server: Server{
			Listen:         "127.0.0.1",
			Port:           9898,
			Transports:     []string{TransportWS},
			AllowedOrigins: []string{"*"},
		},
		Card: Card{
			ReadFaceImage: true,
			ReadLaserID:   true,
		},
		TLS: TLS{
			Port: 9899,
			Mode: tlsModeFiles,
		},
	}
}

// Load reads the file at path on top of the defaults and validates it.
//
// It returns ErrNotFound when the file does not exist. Any other error — an
// unparsable file, an unknown key, a bad value — names the file and, wherever
// the offending key can be found in it, the line, so an administrator editing
// over SSH can go straight to it.
func Load(path string) (Config, error) {
	raw, err := atomicfile.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, ErrNotFound
		}
		return Config{}, err
	}
	return decode(raw, path)
}

func decode(raw []byte, path string) (Config, error) {
	cfg := Default()
	md, err := toml.Decode(string(raw), &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}

	var problems []string
	for _, key := range md.Undecoded() {
		problems = append(problems, fmt.Sprintf("%s:%d: unknown key %q", path, findLine(raw, key), key.String()))
	}
	for _, pr := range validate(cfg) {
		problems = append(problems, fmt.Sprintf("%s:%d: %s: %s", path, findLine(raw, toml.Key(pr.key)), strings.Join(pr.key, "."), pr.msg))
	}
	if len(problems) > 0 {
		return Config{}, errors.New(strings.Join(problems, "\n"))
	}
	return cfg, nil
}

// problem is one bad value: the dotted key it lives at and what is wrong with
// it. findLine turns the key into a line number for the error message.
type problem struct {
	key []string
	msg string
}

func validate(cfg Config) []problem {
	var p []problem

	switch {
	case cfg.Server.Listen == "":
		p = append(p, problem{[]string{"server", "listen"}, `is empty; set an address such as "127.0.0.1"`})
	case cfg.Server.Listen != "localhost" && net.ParseIP(cfg.Server.Listen) == nil:
		p = append(p, problem{[]string{"server", "listen"}, fmt.Sprintf("%q is not an IP address", cfg.Server.Listen)})
	}

	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		p = append(p, problem{[]string{"server", "port"}, fmt.Sprintf("%d is out of range (1-65535)", cfg.Server.Port)})
	}

	if len(cfg.Server.Transports) == 0 {
		p = append(p, problem{[]string{"server", "transports"}, `is empty; it must list "ws", "socketio" or both`})
	}
	seen := map[string]bool{}
	for _, t := range cfg.Server.Transports {
		switch {
		case t != TransportWS && t != TransportSocketIO:
			p = append(p, problem{[]string{"server", "transports"}, fmt.Sprintf("%q is not a transport; use %q or %q", t, TransportWS, TransportSocketIO)})
		case seen[t]:
			p = append(p, problem{[]string{"server", "transports"}, fmt.Sprintf("lists %q twice", t)})
		}
		seen[t] = true
	}

	if len(cfg.Server.AllowedOrigins) == 0 {
		p = append(p, problem{[]string{"server", "allowed_origins"}, `is empty; list at least one origin, or ["*"] to allow any`})
	}
	for _, o := range cfg.Server.AllowedOrigins {
		if strings.TrimSpace(o) == "" {
			p = append(p, problem{[]string{"server", "allowed_origins"}, fmt.Sprintf("has an empty entry %q", o)})
		}
	}

	if cfg.Server.Token != "" && strings.IndexFunc(cfg.Server.Token, isSpace) >= 0 {
		p = append(p, problem{[]string{"server", "token"}, "must not contain whitespace"})
	}
	// The strict loader refuses a hand-written file that exposes the agent
	// without a token; the UI path cannot produce that state, because token
	// generation and the write are one save (decision 16).
	if !IsLoopbackListen(cfg.Server.Listen) && cfg.Server.Token == "" {
		p = append(p, problem{[]string{"server", "token"}, `is empty while listen is not loopback; the agent refuses to serve card data to the network without a token. Save once from /settings to generate one, or set it by hand`})
	}

	if cfg.TLS.Port < 1 || cfg.TLS.Port > 65535 {
		p = append(p, problem{[]string{"tls", "port"}, fmt.Sprintf("%d is out of range (1-65535)", cfg.TLS.Port)})
	}
	if cfg.TLS.Port == cfg.Server.Port {
		p = append(p, problem{[]string{"tls", "port"}, fmt.Sprintf("collides with server.port %d", cfg.Server.Port)})
	}
	switch cfg.TLS.Mode {
	case tlsModeFiles:
	case "":
		p = append(p, problem{[]string{"tls", "mode"}, `is empty; it must be "files"`})
	default:
		p = append(p, problem{[]string{"tls", "mode"}, fmt.Sprintf("%q is not a mode; use %q", cfg.TLS.Mode, tlsModeFiles)})
	}
	if cfg.TLS.Enabled && cfg.TLS.CertFile == "" {
		p = append(p, problem{[]string{"tls", "cert_file"}, "is empty while tls.enabled is true"})
	}
	if cfg.TLS.Enabled && cfg.TLS.KeyFile == "" {
		p = append(p, problem{[]string{"tls", "key_file"}, "is empty while tls.enabled is true"})
	}

	return p
}

func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// Enabled reports whether the named transport is in the list. A disabled
// transport is never constructed: no handler, no goroutines.
func (s Server) Enabled(transport string) bool {
	for _, t := range s.Transports {
		if t == transport {
			return true
		}
	}
	return false
}

// IsLoopbackListen reports whether the listen address only reaches the local
// machine: "localhost", ::1, or anything in 127.0.0.0/8.
func IsLoopbackListen(listen string) bool {
	if listen == "localhost" {
		return true
	}
	ip := net.ParseIP(listen)
	return ip != nil && ip.IsLoopback()
}

// OriginAllowed reports whether origin is listed in origins, or origins
// contains "*". Comparison is case-insensitive, because a browser sends the
// host lowercased and a hand-written uppercase origin should not silently
// fail. An empty origin is the caller's business: only a request that carries
// no Origin header at all (a non-browser client) may skip this check.
func OriginAllowed(origins []string, origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}
	for _, o := range origins {
		if strings.EqualFold(o, "*") || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

// OriginAllowed reports whether origin may open a card socket under this
// config: it must be listed in allowed_origins, or the list must contain "*".
func (c Config) OriginAllowed(origin string) bool {
	return OriginAllowed(c.Server.AllowedOrigins, origin)
}

// Fingerprint returns the hash of the file's bytes, or "" when the file does
// not exist. GET /api/settings hands it out and PUT /api/settings echoes it
// back, so a save can refuse to overwrite a file that changed since it was
// served (decision 14). It is the hash of the bytes rather than the mtime
// because mtime granularity depends on the filesystem and nanoseconds since
// the epoch do not survive JavaScript's float64 numbers.
func Fingerprint(path string) (string, error) {
	raw, err := atomicfile.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Save validates cfg, refuses to overwrite a file whose fingerprint no longer
// matches expectVersion, and writes the file. ErrStale means the file changed
// on disk since the caller was served it; the caller answers 409 and reloads
// rather than silently undoing a hand edit (decision 14).
func Save(path string, cfg Config, expectVersion string) error {
	_, err := SaveVersion(path, cfg, expectVersion)
	return err
}

// SaveVersion returns the fingerprint of the bytes this save wrote, rather
// than re-reading a file that an administrator may already have edited.
func SaveVersion(path string, cfg Config, expectVersion string) (string, error) {
	if pr := validate(cfg); len(pr) > 0 {
		return "", fmt.Errorf("config is not valid: %s: %s", strings.Join(pr[0].key, "."), pr[0].msg)
	}
	current, err := Fingerprint(path)
	if err != nil {
		return "", err
	}
	if current != expectVersion {
		return "", ErrStale
	}
	if err := Write(path, cfg); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	render(&buf, cfg)
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// Write renders cfg as a commented file and replaces path with it.
//
// The file is written from a template rather than re-marshalled, because
// BurntSushi/toml drops comments on write and the schema is small enough to
// template. The comments are what an administrator editing over SSH relies
// on, and a comment added by hand is lost on the next save — the banner at the
// top of the written file says so, as do the settings page and the README.
// The mode is 0600 because the file can hold the socket token.
func Write(path string, cfg Config) error {
	var buf bytes.Buffer
	render(&buf, cfg)
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return atomicfile.Write(path, buf.Bytes(), 0o600)
}

// Validate lets a settings transaction reject a candidate before reserving
// listeners or replacing any files.
func Validate(cfg Config) error {
	if pr := validate(cfg); len(pr) > 0 {
		return fmt.Errorf("config is not valid: %s: %s", strings.Join(pr[0].key, "."), pr[0].msg)
	}
	return nil
}

// Restore preserves the original bytes, including comments, but refuses to
// overwrite a hand edit made after the transaction's own write.
func Restore(path string, original []byte, existed bool, expected string) error {
	current, err := Fingerprint(path)
	if err != nil {
		return err
	}
	if current != expected {
		return ErrStale
	}
	if !existed {
		return os.Remove(path)
	}
	return atomicfile.Write(path, original, 0o600)
}

const banner = `# thai-smartcard-agent configuration.
#
# Saving from /settings or the tray app rewrites this file from scratch:
# comments added by hand are lost on the next save. Hand edits take effect
# after a restart. The file can hold the socket token, so keep it private.
`

func render(w *bytes.Buffer, cfg Config) {
	w.WriteString(banner)
	fmt.Fprintf(w, "[server]\n")
	fmt.Fprintf(w, "listen = %s      # %q exposes the agent to the network; anything else local stays loopback\n",
		strconv.Quote(cfg.Server.Listen), "0.0.0.0")
	fmt.Fprintf(w, "port = %d\n", cfg.Server.Port)
	fmt.Fprintf(w, "transports = [%s] # %q, %q, or both; a client written for the other one stops\n",
		quoteList(cfg.Server.Transports), TransportWS, TransportSocketIO)
	fmt.Fprintf(w, "allowed_origins = [%s] # origins allowed to open a card socket; %q is any\n",
		quoteList(cfg.Server.AllowedOrigins), "*")
	fmt.Fprintf(w, "token = %s      # required when listen is not loopback; /settings generates it\n\n",
		strconv.Quote(cfg.Server.Token))

	fmt.Fprintf(w, "[card]\n")
	fmt.Fprintf(w, "read_face_image = %v\n", cfg.Card.ReadFaceImage)
	fmt.Fprintf(w, "read_laser_id = %v\n", cfg.Card.ReadLaserID)
	fmt.Fprintf(w, "read_nhso = %v        # treatment entitlement\n", cfg.Card.ReadNHSO)
	fmt.Fprintf(w, "reader = %s      # empty watches every attached reader\n\n", strconv.Quote(cfg.Card.Reader))

	fmt.Fprintf(w, "[tls]\n")
	fmt.Fprintf(w, "enabled = %v\n", cfg.TLS.Enabled)
	fmt.Fprintf(w, "port = %d\n", cfg.TLS.Port)
	fmt.Fprintf(w, "mode = %q        # %q; \"auto\" (a local CA) comes later\n", cfg.TLS.Mode, tlsModeFiles)
	fmt.Fprintf(w, "cert_file = %s\n", strconv.Quote(cfg.TLS.CertFile))
	fmt.Fprintf(w, "key_file = %s\n", strconv.Quote(cfg.TLS.KeyFile))
	fmt.Fprintf(w, "hostnames = [%s]\n", quoteList(cfg.TLS.Hostnames))
}

func quoteList(list []string) string {
	quoted := make([]string, len(list))
	for i, s := range list {
		quoted[i] = strconv.Quote(s)
	}
	return strings.Join(quoted, ", ")
}

// RandomToken returns a fresh 128-bit random token, hex encoded. The agent
// generates the token itself, never the client (decision 16).
func RandomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// findLine reports the 1-based line the key is written on, or 0 when the scan
// cannot find it. It walks table headers and bare keys rather than parsing:
// the point is a useful error for an administrator editing by hand, not
// complete position data.
func findLine(raw []byte, key toml.Key) int {
	if len(key) == 0 {
		return 0
	}
	lines := strings.Split(string(raw), "\n")
	var section []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			section = headerPath(trimmed)
			if samePath(section, key) {
				return i + 1
			}
			continue
		}
		if len(key) > 1 && samePath(section, key[:len(key)-1]) && keyName(trimmed) == key[len(key)-1] {
			return i + 1
		}
		if len(key) == 1 && len(section) == 0 && keyName(trimmed) == key[0] {
			return i + 1
		}
	}
	return 0
}

// headerPath turns a "[a.b]" line into ["a", "b"].
func headerPath(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "[")
	line = strings.TrimSuffix(line, "]")
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = line[:idx]
	}
	parts := strings.Split(strings.TrimSpace(line), ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// keyName returns the bare key of a "key = value" line.
func keyName(line string) string {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return ""
	}
	name := strings.TrimSpace(line[:idx])
	return strings.Trim(name, `"'`)
}

func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

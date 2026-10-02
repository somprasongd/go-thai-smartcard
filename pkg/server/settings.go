package server

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
)

// settingsHeader is the fixed custom header a write to /api/settings must
// carry. A cross-origin page cannot set it without a preflight, and there are
// no CORS headers to answer one, so no page the user visits can drive a save
// even when the browser is on loopback with the agent.
const settingsHeader = "X-SMC-Settings"

// settingsGuard enforces the rules for /settings and /api/* (plan: rules for
// /settings and /api/*). Loopback alone is not enough, because the user's
// browser is on loopback too — the guard's job is to keep the browser's other
// tabs out while the card sockets stay cross-origin readable.
type settingsGuard struct{}

func (g *settingsGuard) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Rule 5, half one: a reverse proxy on the same host must not be able
		// to forward the whole internet to these routes. The agent never sets
		// these headers itself, so their presence means a proxy spoke.
		if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" {
			http.Error(w, "proxy headers are not accepted here", http.StatusForbidden)
			return
		}

		// Rule 5, half two: the TCP peer must be on loopback. An SSH tunnel
		// arrives as 127.0.0.1 and passes; a LAN peer does not, so there is
		// deliberately no network-reachable way to change settings.
		if !remoteAddrIsLoopback(r) {
			http.Error(w, "settings are loopback only", http.StatusForbidden)
			return
		}

		// Rule 2: the Host must name this machine. A public hostname that
		// resolves to 127.0.0.1 is the DNS-rebinding move, and it dies here.
		if !hostIsLocal(r.Host) {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}

		// Rule 3: a browser always sends Origin on a cross-origin request, and
		// on a same-origin fetch it sends its own. Either way it must be the
		// agent's own origin; anything else is another page nosing in.
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r) {
			http.Error(w, "cross-origin requests are not accepted here", http.StatusForbidden)
			return
		}

		// Rule 4: writes need the fixed custom header. Rule 1 is what makes
		// this mean anything: no CORS headers are ever set, so a cross-origin
		// writer cannot pass the preflight the custom header triggers.
		if r.Method != http.MethodGet && r.Header.Get(settingsHeader) != "1" {
			http.Error(w, "missing "+settingsHeader, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// remoteAddrIsLoopback reports whether the TCP peer is on the local machine.
func remoteAddrIsLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostIsLocal reports whether the Host header names the local machine, with
// or without a port.
func hostIsLocal(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// sameOrigin reports whether origin is the agent's own: the scheme it is
// serving on plus the Host header the request already carries.
func sameOrigin(origin string, r *http.Request) bool {
	return strings.EqualFold(origin, ownOrigin(r))
}

func ownOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// settingsAPI serves /api/info and /api/settings.
//
// The config file is the single source of truth: every GET re-reads it and
// every PUT goes through config.Save, so a hand edit is picked up by the next
// request and a save that would clobber one is refused (decision 14). The
// socket token is never sent to a client; a GET reports token_set, and a PUT
// that needs one has the agent generate it (decision 16).
type settingsAPI struct {
	path string
	// transports is the transport list of the mux generation this handler
	// belongs to, reported by /api/info so the bundled page can show a
	// disabled transport as disabled rather than offline.
	transports []string
	// version is the agent's own version, for /api/info.
	version string
	// tlsEnabled is reported by /api/info until TLS serving ships.
	tlsEnabled bool
	// onChange is called with the saved config after the response is written.
	// The agent applies the card options and restarts the listener; it is
	// called in its own goroutine and may be nil.
	onChange      func(config.Config)
	applySettings func(config.Config, string) (SettingsResult, error)
	instanceID    string
	// status is the broadcast-pump cache behind /api/readers. It may be nil,
	// which leaves the endpoint answering an empty list.
	status      *StatusCache
	diagnostics func() DiagnosticSnapshot
}

// servedConfig carries the settings the page edits. Logging remains a
// startup-only file setting; no response ever carries the socket token.
type servedConfig struct {
	Server config.Server `json:"server"`
	Card   config.Card   `json:"card"`
	TLS    config.TLS    `json:"tls"`
}

// serveInfo answers /api/info. The bundled page asks for it on boot to learn
// what is enabled before it connects.
func (api *settingsAPI) serveInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     api.version,
		"transports":  api.transports,
		"tls":         api.tlsEnabled,
		"instance_id": api.instanceID,
	})
}

// serveReaders answers /api/readers with the reader list as of the newest
// status broadcast. An agent that has not reported yet answers an empty list,
// and the bundled page says so rather than pretending it asked the hardware.
func (api *settingsAPI) serveReaders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	readers := []string{}
	selected, state := "", ""
	if api.status != nil {
		if st, ok := api.status.Snapshot(); ok {
			if st.Readers != nil {
				readers = st.Readers
			}
			selected = st.Selected
			state = st.State
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"readers":  readers,
		"selected": selected,
		"state":    state,
	})
}

func (api *settingsAPI) serveSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.get(w, r)
	case http.MethodPut:
		api.put(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *settingsAPI) get(w http.ResponseWriter, r *http.Request) {
	cfg, version, err := api.read()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config":    served(cfg),
		"version":   version,
		"token_set": cfg.Server.Token != "",
	})
}

type putSettings struct {
	Config config.Config `json:"config"`
	// Version is the fingerprint GET served, echoed back. A mismatch is a 409.
	Version string `json:"version"`
	// RegenerateToken replaces the socket token and returns the new one once.
	RegenerateToken bool `json:"regenerate_token"`
}

func (api *settingsAPI) put(w http.ResponseWriter, r *http.Request) {
	var body putSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "decode request: "+err.Error(), http.StatusBadRequest)
		return
	}

	current, _, err := api.read()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	next := body.Config
	// Logging is an administrator/file setting applied on restart. A settings
	// page or older client must not erase it by sending only server/card/TLS.
	next.Logging = current.Logging
	// The token lives in the file, not in a GET response, so a save that
	// echoes the served config back must not wipe it. It is merged here and
	// replaced below only when the agent generates a new one.
	next.Server.Token = current.Server.Token

	// Decision 16: the agent generates the token. The first PUT that turns
	// exposure on while there is no token makes one, writes it in this same
	// save, and returns it in this response only; the regenerate action goes
	// through the same path. There is no clear action: a token is enforced
	// only beyond loopback and exposure without one is impossible, so a token
	// on a loopback agent is inert and regeneration is the only way it
	// changes.
	generated := ""
	exposing := !config.IsLoopbackListen(next.Server.Listen)
	if body.RegenerateToken || (exposing && next.Server.Token == "") {
		token, err := config.RandomToken()
		if err != nil {
			http.Error(w, "generate token: "+err.Error(), http.StatusInternalServerError)
			return
		}
		generated = token
		next.Server.Token = token
	}

	result := SettingsResult{}
	var saveErr error
	if api.applySettings != nil {
		result, saveErr = api.applySettings(next, body.Version)
	} else {
		saveErr = config.Save(api.path, next, body.Version)
	}
	if err := saveErr; err != nil {
		var applyErr *SettingsError
		if errors.As(err, &applyErr) {
			http.Error(w, err.Error(), applyErr.Status)
			return
		}
		if errors.Is(err, config.ErrStale) {
			http.Error(w, "the file changed on disk, reload before saving", http.StatusConflict)
			return
		}
		// A config the strict loader refuses is the caller's mistake, not a
		// server fault.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if result.AfterResponse != nil {
		defer func() {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			result.AfterResponse()
		}()
	}

	version := result.Version
	err = nil
	if version == "" {
		version, err = config.Fingerprint(api.path)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"config":    served(next),
		"version":   version,
		"token_set": next.Server.Token != "",
	}
	if generated != "" {
		// Shown once, by the page or the tray; it is never sent again.
		resp["token"] = generated
	}
	if result.EndpointURL != "" {
		resp["endpoint_url"] = result.EndpointURL
	}
	writeJSON(w, http.StatusOK, resp)

	if api.applySettings == nil && api.onChange != nil {
		go api.onChange(next)
	}
}

// read returns the current config and its fingerprint. The fingerprint is
// recomputed on every request, never cached at startup, so a hand edit made
// after the agent started is caught (decision 14).
func (api *settingsAPI) read() (config.Config, string, error) {
	cfg, version, err := config.LoadVersion(api.path)
	if errors.Is(err, config.ErrNotFound) {
		// The agent writes the defaults at startup; a missing file here means
		// the directory was not writable. Serve the defaults rather than
		// 500ing a page that could otherwise be read.
		return config.Default(), "", nil
	}
	if err != nil {
		return config.Config{}, "", err
	}
	return cfg, version, nil
}

// served strips the token from a config for the wire.
func served(cfg config.Config) servedConfig {
	out := servedConfig{Server: cfg.Server, Card: cfg.Card, TLS: cfg.TLS}
	out.Server.Token = ""
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}

func (api *settingsAPI) serveHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h := HealthSnapshot{State: "starting", Readers: []string{}}
	if api.status != nil {
		h = api.status.Health()
	}
	writeJSON(w, http.StatusOK, h)
}

package server

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
)

type ServerConfig struct {
	// Listen is the address to bind, from config.toml [server] listen. An
	// empty value binds every interface; the config loader never produces
	// one, and the default is loopback (decision 7).
	Listen string
	Port   int
	// Transports selects what is registered: "ws", "socketio", or both. A
	// disabled transport is never constructed: no handler, no engine.io
	// server, no goroutines.
	Transports []string
	// AllowedOrigins are the origins allowed to open a card socket, from
	// config.toml [server] allowed_origins. "*" allows any origin. A request
	// with no Origin header is a non-browser client and skips the check.
	AllowedOrigins []string
	// Token is the socket token, checked in one middleware in front of both
	// transports. It is required when Listen is not loopback.
	Token     string
	Broadcast chan model.Message
	// Command carries control commands from the clients to the agent. It may
	// be nil, which leaves the control channel off: inbound frames are then
	// dropped instead of reaching a consumer that is not there.
	Command chan model.Command
	// Version is the agent's own version, reported by /api/info.
	Version string
	// TLS is the [tls] table. When Enabled, the agent serves HTTPS on
	// TLS.Port from TLS.CertFile/TLS.KeyFile and forces the plain listener
	// to loopback.
	TLS config.TLS
	// ConfigPath is the config file the settings API reads and writes. Empty
	// leaves /api/* and /settings unregistered.
	ConfigPath string
	// OnChange is called with the saved config after a successful PUT and
	// after the response is written; the agent applies the card options and
	// restarts the listener there. It may be nil.
	OnChange func(config.Config)
	// ApplySettings persists and applies one serialized transaction. When set,
	// it owns the save instead of the legacy Save/OnChange path.
	ApplySettings func(config.Config, string) (SettingsResult, error)
	// InstanceID lets discovery reject a stale endpoint from another process.
	InstanceID string
}

// SettingsResult delays retiring old connections until the response is flushed.
type SettingsResult struct {
	Version       string
	EndpointURL   string
	AfterResponse func()
}

// SettingsError preserves HTTP error classification across the apply callback.
type SettingsError struct {
	Status int
	Err    error
}

func (e *SettingsError) Error() string { return e.Err.Error() }
func (e *SettingsError) Unwrap() error { return e.Err }

//go:embed web/index.html
var indexPage []byte

//go:embed web/settings.html
var settingsPage []byte

// Serve serves forever on one listener. The agent uses Manager instead, so a
// settings save can restart the listener; Serve is the one-generation form
// for callers that never reconfigure.
func Serve(cfg ServerConfig) {
	if len(cfg.Transports) == 0 {
		// config.Load refuses an empty list already; this guards a caller
		// that built a ServerConfig by hand.
		log.Fatal("server: no transports configured, refusing to start")
	}
	mux := newMux(cfg, nil)
	addr := net.JoinHostPort(cfg.Listen, strconv.Itoa(cfg.Port))
	log.Println("Serving at " + addr + " (" + strings.Join(cfg.Transports, ", ") + ")")
	log.Fatal(http.ListenAndServe(addr, mux))
}

// newMux builds one generation of the handler: the enabled transports behind
// the socket guard, the settings routes behind their own guard, the bundled
// pages, and the pump that fans every broadcast out to the transports. done
// is closed when the generation stops, which ends the pump. The transports
// connections are retired on done. The socket.io server object is retained:
// closing its engine while a handshake enqueues a session can panic in the
// third-party library, so retirement closes tracked clients instead.
func newMux(cfg ServerConfig, done <-chan struct{}) *http.ServeMux {
	guard := newSocketGuard(cfg.AllowedOrigins, cfg.Token, cfg.Listen)
	settings := &settingsGuard{}
	api := &settingsAPI{
		path:          cfg.ConfigPath,
		transports:    cfg.Transports,
		version:       cfg.Version,
		tlsEnabled:    cfg.TLS.Enabled,
		onChange:      cfg.OnChange,
		applySettings: cfg.ApplySettings,
		instanceID:    cfg.InstanceID,
	}

	var socketServer *socketIO
	if hasTransport(cfg.Transports, config.TransportSocketIO) {
		socketServer = NewSocketIO(cfg.Command)
		go func() {
			// io.EOF is what Serve returns when the server is closed; it is
			// not an error to report.
			if err := socketServer.Serve(); err != nil && !errors.Is(err, io.EOF) {
				log.Fatalf("socketio listen error: %s\n", err)
			}
		}()
	}

	var webSocket *ws
	if hasTransport(cfg.Transports, config.TransportWS) {
		webSocket = NewWS(cfg.Command)
	}
	if done != nil {
		go func() {
			<-done
			if webSocket != nil {
				webSocket.closeConnections()
			}
			if socketServer != nil {
				socketServer.closeConnections()
			}
		}()
	}

	mux := http.NewServeMux()
	if socketServer != nil {
		mux.Handle("/socket.io/", guard.wrap(socketServer))
	}
	if webSocket != nil {
		mux.Handle("/ws", guard.wrap(http.HandlerFunc(webSocket.Handler)))
	}
	if cfg.ConfigPath != "" {
		mux.Handle("/api/info", settings.wrap(http.HandlerFunc(api.serveInfo)))
		mux.Handle("/api/settings", settings.wrap(http.HandlerFunc(api.serveSettings)))
		mux.Handle("/settings", settings.wrap(servePage(settingsPage)))
	}
	mux.HandleFunc("/", servePage(indexPage))

	if cfg.Broadcast != nil {
		go func() {
			for {
				select {
				case <-done:
					return
				case msg, ok := <-cfg.Broadcast:
					if ok {
						if socketServer != nil {
							socketServer.Broadcast(msg)
						}
						if webSocket != nil {
							webSocket.Broadcast(msg)
						}
					}
				}
			}
		}()
	}
	return mux
}

// servePage writes one embedded page with its content type set, so neither
// page depends on content sniffing.
func servePage(page []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(page)
	}
}

// hasTransport reports whether the list names the transport.
func hasTransport(list []string, name string) bool {
	for _, t := range list {
		if t == name {
			return true
		}
	}
	return false
}

// decodeCommand parses one inbound control frame.
//
// socket.io delivers the event argument already JSON encoded, so a page that
// emits a plain object arrives wrapped in a JSON string, while the raw
// WebSocket frame in the contract is the object itself. Both are accepted
// rather than making the page guess which one the agent wants.
func decodeCommand(payload []byte) (model.Command, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return model.Command{}, fmt.Errorf("empty command")
	}

	if payload[0] == '"' {
		var inner string
		if err := json.Unmarshal(payload, &inner); err != nil {
			return model.Command{}, fmt.Errorf("decode command string: %w", err)
		}
		payload = []byte(inner)
	}

	var cmd model.Command
	if err := json.Unmarshal(payload, &cmd); err != nil {
		return model.Command{}, fmt.Errorf("decode command: %w", err)
	}
	return cmd, nil
}

// forwardCommand hands a command to the agent without blocking. Every
// connection is read on its own goroutine, so a blocking send would let one
// client that is not being served wedge every other client's read loop. The
// command is recoverable, the page can send it again, so dropping it is the
// lesser failure.
func forwardCommand(command chan model.Command, cmd model.Command) {
	if command == nil {
		return
	}
	select {
	case command <- cmd:
	default:
		log.Printf("dropping %q command, the agent is not reading commands", cmd.Action)
	}
}

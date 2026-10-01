package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// passthrough is what the guard protects in the unit tests: a handler that
// only proves it was reached.
var passthrough = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestSocketGuardOrigin(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		origin  string
		want    int
	}{
		// Decision 9: "*" is the default, so the main use — a web app on its
		// own origin — works out of the box.
		{name: "the star allows any origin", origins: []string{"*"}, origin: "http://app.example", want: http.StatusOK},
		{name: "a listed origin passes", origins: []string{"http://app.example"}, origin: "http://app.example", want: http.StatusOK},
		{name: "comparison ignores case", origins: []string{"http://app.example"}, origin: "http://APP.example", want: http.StatusOK},
		{name: "an unlisted origin is refused", origins: []string{"http://app.example"}, origin: "http://evil.example", want: http.StatusForbidden},
		{name: "a lookalike origin is refused", origins: []string{"http://app.example"}, origin: "http://app.example.evil.example", want: http.StatusForbidden},
		{
			// No Origin header means a non-browser client: native apps and
			// test tools do not send one, and the list would lock them out.
			name:    "a request with no origin header is not a browser",
			origins: []string{"http://app.example"},
			origin:  "",
			want:    http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newSocketGuard(tt.origins, "", "127.0.0.1")
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			g.wrap(passthrough).ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestSocketGuardToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"

	tests := []struct {
		name          string
		token         string // the configured token
		listen        string // the configured listen address
		query         string // the ?token= value to send
		authorization string // the Authorization header to send
		want          int
	}{
		{
			name:  "loopback needs no token",
			token: token, listen: "127.0.0.1",
			want: http.StatusOK,
		},
		{
			name:  "a correct query token passes",
			token: token, listen: "0.0.0.0", query: token,
			want: http.StatusOK,
		},
		{
			name:  "a correct bearer token passes",
			token: token, listen: "0.0.0.0", authorization: "Bearer " + token,
			want: http.StatusOK,
		},
		{
			name:  "exposure without a token is refused",
			token: token, listen: "0.0.0.0",
			want: http.StatusUnauthorized,
		},
		{
			name:  "a wrong query token is refused",
			token: token, listen: "0.0.0.0", query: "not-the-token",
			want: http.StatusUnauthorized,
		},
		{
			name:  "a wrong bearer token is refused",
			token: token, listen: "0.0.0.0", authorization: "Bearer wrong",
			want: http.StatusUnauthorized,
		},
		{
			name:  "an authorization header without the bearer prefix is refused",
			token: token, listen: "0.0.0.0", authorization: "Basic " + token,
			want: http.StatusUnauthorized,
		},
		{
			// Decision 10: if both are present the header wins, so a page
			// cannot satisfy the check by mixing a good query with a bad
			// header or the other way round.
			name: "with both present the header wins", token: token, listen: "0.0.0.0",
			query: "not-the-token", authorization: "Bearer " + token,
			want: http.StatusOK,
		},
		{
			name: "with both present a bad header costs the good query", token: token, listen: "0.0.0.0",
			query: token, authorization: "Bearer wrong",
			want: http.StatusUnauthorized,
		},
		{
			// Fail closed: no token configured while listening on every
			// interface. The strict loader refuses this state; the guard
			// refuses it too for a hand-built ServerConfig.
			name:  "exposure with no configured token refuses everything",
			token: "", listen: "0.0.0.0",
			want: http.StatusUnauthorized,
		},
		{
			// A client may always present the token, loopback or not, and a
			// wrong one is still wrong.
			name:  "a wrong token is refused on loopback too",
			token: token, listen: "127.0.0.1", query: "wrong",
			want: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newSocketGuard([]string{"*"}, tt.token, tt.listen)
			req := httptest.NewRequest(http.MethodGet, "/ws?token="+tt.query, nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			rec := httptest.NewRecorder()
			g.wrap(passthrough).ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

// The check happens before the upgrade, so a refused browser gets a plain
// error page and never opens a socket it would receive card data on.
func TestSocketGuardRefusesBeforeTheUpgrade(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"

	tests := []struct {
		name   string
		url    string
		header http.Header
		want   int
	}{
		{
			name: "a bad token never upgrades",
			url:  "ws" + strings.TrimPrefix(tokenServer(t, []string{"*"}, "", "0.0.0.0").URL, "http") + "/ws",
			want: http.StatusUnauthorized,
		},
		{
			name:   "a refused origin never upgrades",
			url:    "ws" + strings.TrimPrefix(tokenServer(t, []string{"http://app.example"}, "", "127.0.0.1").URL, "http") + "/ws",
			header: http.Header{"Origin": []string{"http://evil.example"}},
			want:   http.StatusForbidden,
		},
		{
			name: "a good query token upgrades",
			url:  "ws" + strings.TrimPrefix(tokenServer(t, []string{"*"}, token, "0.0.0.0").URL, "http") + "/ws?token=" + token,
			want: http.StatusSwitchingProtocols,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
			c, resp, err := dialer.Dial(tt.url, tt.header)
			if c != nil {
				c.Close()
			}
			if resp == nil {
				t.Fatalf("dial: %v (no response)", err)
			}
			if resp.StatusCode != tt.want {
				t.Errorf("handshake status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}

// tokenServer builds a one-route server whose /ws sits behind the guard, the
// way newMux wires it. origins nil means ["*"].
func tokenServer(t *testing.T, origins []string, token, listen string) *httptest.Server {
	t.Helper()
	if origins == nil {
		origins = []string{"*"}
	}
	g := newSocketGuard(origins, token, listen)
	mux := http.NewServeMux()
	mux.Handle("/ws", g.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The upgrade target: the status tells the test how far the request
		// got. The upgrade itself fails for the plain requests, which is fine.
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.Close()
	})))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestNewMuxRegistersOnlyEnabledTransports(t *testing.T) {
	// A disabled transport is never constructed: no /socket.io/ handler, no
	// engine.io server, no goroutines. The bundled page catch-all answers
	// every other path, so "not there" is observable as the page's HTML in
	// place of the transport's answer.

	tests := []struct {
		name       string
		transports []string
	}{
		{name: "the default list serves ws only", transports: []string{"ws"}},
		{name: "socketio alone", transports: []string{"socketio"}},
		{name: "both transports registered", transports: []string{"ws", "socketio"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newMux(ServerConfig{
				Listen:     "127.0.0.1",
				Transports: tt.transports,
			})

			srv := httptest.NewServer(mux)
			defer srv.Close()

			wsStatus, wsBody := getStatusBody(t, srv.URL+"/ws")
			_, sioBody := getStatusBody(t, srv.URL+"/socket.io/?transport=polling")

			wsOn := hasTransport(tt.transports, "ws")
			sioOn := hasTransport(tt.transports, "socketio")

			if wsOn {
				// A plain GET reaches the upgrade target, which refuses it
				// with 400; the bundled page would have answered 200.
				if wsStatus != http.StatusBadRequest {
					t.Errorf("/ws status = %d, want 400 from the websocket handler", wsStatus)
				}
			} else if !isIndexBody(wsBody) {
				t.Errorf("/ws served %q, want the bundled page", wsBody)
			}

			if sioOn {
				// engine.io answers a polling request with its own error
				// page, never with the bundled HTML.
				if isIndexBody(sioBody) {
					t.Error("/socket.io/ served the bundled page, want an engine.io answer")
				}
			} else if !isIndexBody(sioBody) {
				t.Errorf("/socket.io/ served %q, want the bundled page", sioBody)
			}
		})
	}
}

func getStatusBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

// isIndexBody reports whether the body is the bundled page.
func isIndexBody(body string) bool {
	return strings.Contains(body, "SMC — Thai ID Card Agent")
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	status, _ := getStatusBody(t, url)
	return status
}

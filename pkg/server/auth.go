package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
)

// socketGuard gates the card sockets. It sits in front of /ws and /socket.io/,
// before the upgrade or the handshake, so a refused client never reaches a
// transport and there is no unauthenticated connection state to manage
// (decision 10).
//
// The card sockets must stay cross-origin readable — a web app on its own
// origin is the main use — so the origin list, not the loopback rule of the
// settings routes, is what protects them. The token is what protects a
// listener that reaches beyond loopback.
type socketGuard struct {
	// origins is config.toml [server] allowed_origins. "*" allows any origin.
	origins []string
	// token is the shared socket token; empty means none is configured.
	token string
	// requireToken is true when the agent listens beyond loopback. The strict
	// loader guarantees a token exists in that state; every connection must
	// then present it.
	requireToken bool
}

// newSocketGuard builds the guard for a listen address and origin list. A
// listen address that is empty or unparseable counts as beyond loopback and
// requires a token: it is fail-closed for a hand-built ServerConfig, and the
// config loader never produces one.
func newSocketGuard(origins []string, token, listen string) *socketGuard {
	return &socketGuard{
		origins: origins,
		token:   token,
		// Fail closed: an unparseable or empty listen is treated as exposure,
		// never as loopback.
		requireToken: !config.IsLoopbackListen(listen),
	}
}

// wrap returns the handler with the guard in front of it.
func (g *socketGuard) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A browser always sends Origin on a socket handshake; a client
		// without one is not a browser and is not subject to the list. The
		// main use — a web app on its own origin — lives or dies here.
		if origin := r.Header.Get("Origin"); origin != "" && !config.OriginAllowed(g.origins, origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}

		presented, ok := g.presentedToken(r)
		if !ok {
			// A malformed Authorization header is a refusal, not one to be
			// second-guessed by falling through to the query parameter.
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if presented != "" {
			if !g.tokenMatches(presented) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else if g.requireToken {
			// Covers a required token that was not presented and the
			// misconfigured state of exposure without any token at all:
			// both fail closed rather than serve the card to the network.
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// presentedToken returns the token the client sent. Browsers can only send the
// query parameter, so it is the one form a page has; other clients may send
// Authorization: Bearer instead, which keeps the token out of URLs. When both
// are present the header wins: either one satisfies the check, never a mix.
func (g *socketGuard) presentedToken(r *http.Request) (string, bool) {
	if header := r.Header.Get("Authorization"); header != "" {
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			return "", false
		}
		return strings.TrimSpace(header[len(prefix):]), true
	}
	// The agent never logs request URLs, so the token does not end up in a log.
	return r.URL.Query().Get("token"), true
}

// tokenMatches compares in constant time, so a wrong token is not learnable
// byte by byte from timing. Both sides are hashed first: ConstantTimeCompare
// leaks the length of its inputs, and the hash hides it.
func (g *socketGuard) tokenMatches(presented string) bool {
	if g.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare(sum(presented), sum(g.token)) == 1
}

func sum(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

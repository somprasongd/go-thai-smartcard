package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
)

// writeCert writes a fresh self-signed pair named <base>.pem/<base>.key and
// returns the two paths. Each call has a different serial, so a test can tell
// which certificate is being served.
func writeCert(t *testing.T, dir, base string) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "smc-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	certPath := filepath.Join(dir, base+".pem")
	keyPath := filepath.Join(dir, base+".key")
	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	certOut.Close()
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	der, err = x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	keyOut.Close()
	return certPath, keyPath
}

// freePort reserves a port and immediately releases it. Small race, but the
// tests are the only claimant on a CI host.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// startTLSManager starts a manager with TLS on and both listeners on free
// ports, and returns it plus the plain and https base URLs.
func startTLSManager(t *testing.T, listen string) (*Manager, string, string) {
	t.Helper()
	dir := t.TempDir()
	certPath, keyPath := writeCert(t, dir, "a")

	cfg := ServerConfig{
		Listen:     listen,
		Port:       freePort(t),
		Transports: []string{"ws"},
		Version:    "test",
		ConfigPath: func() string {
			p := filepath.Join(t.TempDir(), "config.toml")
			if err := config.Write(p, config.Default()); err != nil {
				t.Fatal(err)
			}
			return p
		}(),
		TLS: config.TLS{
			Enabled:  true,
			Port:     freePort(t),
			Mode:     "files",
			CertFile: certPath,
			KeyFile:  keyPath,
		},
	}
	mgr, err := Start(cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(mgr.Close)
	return mgr, "http://" + mgr.PlainAddr(), "https://" + mgr.TLSAddr()
}

func insecure() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // self-signed test certificate
		},
	}
}

func TestTLSManagerServesBothListeners(t *testing.T) {
	_, plainURL, httpsURL := startTLSManager(t, "127.0.0.1")

	resp, err := insecure().Get(httpsURL + "/api/info")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("https /api/info = %d (%s)", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), `"tls":true`) {
		t.Errorf("/api/info over https does not report tls: %s", raw)
	}

	resp2, err := http.Get(plainURL + "/")
	if err != nil {
		t.Fatalf("plain get: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("plain / = %d, want 200", resp2.StatusCode)
	}
}

func TestTLSForcesThePlainListenerToLoopback(t *testing.T) {
	// Decision 7's counterpart: with TLS on, the plain listener loses its
	// network reach even when listen asks for every interface.
	mgr, plainURL, _ := startTLSManager(t, "0.0.0.0")

	host, _, err := net.SplitHostPort(mgr.PlainAddr())
	if err != nil {
		t.Fatalf("plain addr %q: %v", mgr.PlainAddr(), err)
	}
	if host != "127.0.0.1" {
		t.Errorf("plain listener bound to %q, want loopback", host)
	}

	// The https listener keeps the configured listen address. A dual-stack
	// bind of 0.0.0.0 reports "::", so both unspecified forms pass.
	tlsHost, _, err := net.SplitHostPort(mgr.TLSAddr())
	if err != nil {
		t.Fatalf("tls addr %q: %v", mgr.TLSAddr(), err)
	}
	if tlsHost != "0.0.0.0" && tlsHost != "::" {
		t.Errorf("tls listener bound to %q, want the configured address", tlsHost)
	}

	// And the plain listener still answers, on loopback.
	resp, err := http.Get(plainURL + "/api/info")
	if err != nil {
		t.Fatalf("plain get: %v", err)
	}
	resp.Body.Close()
}

func TestTLSReloadsChangedCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeCert(t, dir, "a")

	cfg := ServerConfig{
		Listen:     "127.0.0.1",
		Port:       freePort(t),
		Transports: []string{"ws"},
		ConfigPath: func() string {
			p := filepath.Join(t.TempDir(), "config.toml")
			config.Write(p, config.Default())
			return p
		}(),
		TLS: config.TLS{
			Enabled:  true,
			Port:     freePort(t),
			Mode:     "files",
			CertFile: certPath,
			KeyFile:  keyPath,
		},
	}
	mgr, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	first := servedSerial(t, mgr.TLSAddr())

	// A renewed certificate lands in the same files: the next handshake
	// picks it up, because files mode reloads on the mtime change.
	cert2, key2 := writeCert(t, dir, "b")
	raw, err := os.ReadFile(cert2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(key2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := servedSerial(t, mgr.TLSAddr()); got != first {
			return // reloaded
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the certificate was never reloaded after the files changed")
}

func TestTLSKeepsServingWhenAReloadFails(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeCert(t, dir, "a")

	cfg := ServerConfig{
		Listen:     "127.0.0.1",
		Port:       freePort(t),
		Transports: []string{"ws"},
		ConfigPath: func() string {
			p := filepath.Join(t.TempDir(), "config.toml")
			config.Write(p, config.Default())
			return p
		}(),
		TLS: config.TLS{
			Enabled:  true,
			Port:     freePort(t),
			Mode:     "files",
			CertFile: certPath,
			KeyFile:  keyPath,
		},
	}
	mgr, err := Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	first := servedSerial(t, mgr.TLSAddr())

	// A half-written renewal: the mtime changes, the content does not parse.
	// The loader keeps the old certificate instead of failing every
	// handshake until an operator notices.
	if err := os.WriteFile(certPath, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if got := servedSerial(t, mgr.TLSAddr()); got != first {
			t.Fatalf("handshake %d served a new certificate after a failed reload", i)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestTLSStartFailsOnAMissingCertificate(t *testing.T) {
	// The strict-config principle applies to certificates: a manager that
	// starts without its certificate while claiming https is worse than one
	// that reports the failure.
	_, err := Start(ServerConfig{
		Listen:     "127.0.0.1",
		Port:       freePort(t),
		Transports: []string{"ws"},
		TLS: config.TLS{
			Enabled:  true,
			Port:     freePort(t),
			Mode:     "files",
			CertFile: filepath.Join(t.TempDir(), "absent.pem"),
			KeyFile:  filepath.Join(t.TempDir(), "absent.key"),
		},
	})
	if err == nil {
		t.Fatal("start succeeded without a certificate")
	}
}

// servedSerial performs one https handshake and reports the served
// certificate's serial, so a test can tell certificates apart.
func servedSerial(t *testing.T, addr string) *big.Int {
	t.Helper()
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 3 * time.Second},
		"tcp", addr,
		&tls.Config{InsecureSkipVerify: true}, // self-signed test certificate
	)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("no certificate served")
	}
	return state.PeerCertificates[0].SerialNumber
}

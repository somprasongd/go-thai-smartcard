package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	// Decision 7: loopback by default; today's agent binds every interface.
	if !IsLoopbackListen(cfg.Server.Listen) {
		t.Errorf("default listen = %q, want loopback", cfg.Server.Listen)
	}
	// Decision 6: ws is the default transport, socket.io is opt-in.
	if len(cfg.Server.Transports) != 1 || cfg.Server.Transports[0] != TransportWS {
		t.Errorf("default transports = %v, want [ws]", cfg.Server.Transports)
	}
	// Decision 9: every origin allowed out of the box.
	if !cfg.OriginAllowed("http://anything.example") {
		t.Error("default allowed_origins does not allow an arbitrary origin")
	}
	if !cfg.Card.ReadFaceImage || !cfg.Card.ReadLaserID || cfg.Card.ReadNHSO {
		t.Errorf("default card options = %+v, want image and laser on, nhso off", cfg.Card)
	}
	if cfg.Server.Token != "" {
		t.Error("a fresh install must not carry a token")
	}
	if cfg.TLS.Enabled {
		t.Error("TLS is off until it is configured")
	}
}

func TestLoadRoundTrip(t *testing.T) {
	// What Write renders must load back to the same config, comments aside.
	want := Config{
		Server: Server{
			Listen:         "0.0.0.0",
			Port:           9911,
			Transports:     []string{TransportWS, TransportSocketIO},
			AllowedOrigins: []string{"http://app.hospital.example"},
			Token:          "ab12cd34",
		},
		Card: Card{
			ReadFaceImage: false,
			ReadLaserID:   true,
			ReadNHSO:      true,
			Reader:        "Identive CLOUD 2700 R",
		},
		TLS: TLS{
			Enabled:  true,
			Port:     9912,
			Mode:     tlsModeFiles,
			CertFile: "/etc/ssl/smc.crt",
			KeyFile:  "/etc/ssl/smc.key",
			// Write renders every list, so an empty one comes back as an
			// empty slice rather than nil.
			Hostnames: []string{},
		},
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Write(path, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestLoadDefaultsThenFile(t *testing.T) {
	// Only the keys the file names change; everything else stays at its
	// default, so a minimal file is a complete configuration.
	path := writeTemp(t, "config.toml", "[card]\nread_nhso = true\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Card.ReadNHSO {
		t.Error("read_nhso = false, want true from the file")
	}
	def := Default()
	if cfg.Server.Port != def.Server.Port {
		t.Errorf("port = %d, want the default %d", cfg.Server.Port, def.Server.Port)
	}
	if !cfg.Card.ReadFaceImage {
		t.Error("read_face_image = false, want the default true")
	}
}

func TestLoadStrict(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantKey string // a substring that must appear in the error
	}{
		{
			name: "an unknown key is refused",
			body: "[server]\nprot = 9898\n",
			// Decision 1's whole point: a typo must stop the agent, not fall
			// back to defaults while the operator thinks it applied.
			wantKey: `unknown key "server.prot"`,
		},
		{
			name:    "an unknown table is refused",
			body:    "[servr]\nport = 9898\n",
			wantKey: `unknown key "servr"`,
		},
		{
			name:    "a bad type is refused",
			body:    "[server]\nport = \"9898\"\n",
			wantKey: "server.port",
		},
		{
			name:    "an out of range port is refused",
			body:    "[server]\nport = 70000\n",
			wantKey: "out of range",
		},
		{
			name:    "an unparseable listen address is refused",
			body:    "[server]\nlisten = \"local host\"\n",
			wantKey: "server.listen",
		},
		{
			name:    "an empty transport list is refused",
			body:    "[server]\ntransports = []\n",
			wantKey: "is empty",
		},
		{
			name:    "an unknown transport is refused",
			body:    "[server]\ntransports = [\"grpc\"]\n",
			wantKey: "not a transport",
		},
		{
			name:    "a duplicated transport is refused",
			body:    "[server]\ntransports = [\"ws\", \"ws\"]\n",
			wantKey: "twice",
		},
		{
			name:    "an empty origin list is refused",
			body:    "[server]\nallowed_origins = []\n",
			wantKey: "is empty",
		},
		{
			name:    "an empty origin entry is refused",
			body:    "[server]\nallowed_origins = [\"\"]\n",
			wantKey: "empty entry",
		},
		{
			name: "exposing without a token is refused",
			body: "[server]\nlisten = \"0.0.0.0\"\n",
			// Decision 16: the strict loader refuses a hand-written file that
			// exposes the agent without the token ceremony having happened.
			wantKey: "token",
		},
		{
			name:    "a token with whitespace is refused",
			body:    "[server]\ntoken = \"abc def\"\n",
			wantKey: "whitespace",
		},
		{
			name:    "an empty tls mode is refused",
			body:    "[tls]\nmode = \"\"\n",
			wantKey: "tls.mode",
		},
		{
			name:    "an unknown tls mode is refused",
			body:    "[tls]\nmode = \"auto\"\n",
			wantKey: "tls.mode",
		},
		{
			name:    "tls enabled without a certificate is refused",
			body:    "[tls]\nenabled = true\n",
			wantKey: "cert_file",
		},
		{
			name:    "a tls port colliding with the server port is refused",
			body:    "[server]\nport = 9898\n[tls]\nenabled = false\nport = 9898\n",
			wantKey: "collides",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, "config.toml", tt.body)

			_, err := Load(path)
			if err == nil {
				t.Fatalf("load succeeded, want an error mentioning %q", tt.wantKey)
			}
			if !strings.Contains(err.Error(), tt.wantKey) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tt.wantKey)
			}
			// The error names the file, so an administrator with two hosts
			// knows which one to fix.
			if !strings.Contains(err.Error(), "config.toml") {
				t.Errorf("error = %q, want it to name the file", err.Error())
			}
		})
	}
}

func TestLoadErrorNamesTheLine(t *testing.T) {
	// The strict rule is "names the file and line" (loading rules), so an
	// administrator editing over SSH can go straight to the key.
	body := "# comment\n\n[server]\nport = 9898\nprot = 1\n"
	path := writeTemp(t, "config.toml", body)

	_, err := Load(path)
	if err == nil {
		t.Fatal("load succeeded, want an unknown-key error")
	}
	if !strings.Contains(err.Error(), "config.toml:5") {
		t.Errorf("error = %q, want it to name line 5", err.Error())
	}
}

func TestIsLoopbackListen(t *testing.T) {
	tests := []struct {
		listen string
		want   bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"localhost", true},
		{"127.8.8.8", true}, // the whole 127/8 range is loopback
		{"0.0.0.0", false},
		{"192.168.1.10", false},
		{"", false},
		{"hospital.example", false},
	}
	for _, tt := range tests {
		if got := IsLoopbackListen(tt.listen); got != tt.want {
			t.Errorf("IsLoopbackListen(%q) = %v, want %v", tt.listen, got, tt.want)
		}
	}
}

func TestOriginAllowed(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		origin  string
		want    bool
	}{
		{
			name:    "the star allows everything",
			origins: []string{"*"},
			origin:  "http://app.example",
			want:    true,
		},
		{
			name:    "a listed origin is allowed",
			origins: []string{"http://app.example"},
			origin:  "http://app.example",
			want:    true,
		},
		{
			name:    "comparison ignores case",
			origins: []string{"http://APP.example"},
			origin:  "http://app.EXAMPLE",
			want:    true,
		},
		{
			name:    "an unlisted origin is refused",
			origins: []string{"http://app.example"},
			origin:  "http://other.example",
			want:    false,
		},
		{
			name:    "an empty origin is the caller's decision and returns true",
			origins: []string{"http://app.example"},
			origin:  "",
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.Server.AllowedOrigins = tt.origins
			if got := cfg.OriginAllowed(tt.origin); got != tt.want {
				t.Errorf("OriginAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestFingerprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	first, err := Fingerprint(path)
	if err != nil {
		t.Fatalf("fingerprint of a missing file: %v", err)
	}
	if first != "" {
		t.Errorf("fingerprint of a missing file = %q, want an empty string", first)
	}

	if err := os.WriteFile(path, []byte("a = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	one, err := Fingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	same, err := Fingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if one == "" || one != same {
		t.Errorf("fingerprint of identical bytes differs: %q vs %q", one, same)
	}

	if err := os.WriteFile(path, []byte("a = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	two, err := Fingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if two == one {
		t.Error("fingerprint did not change with the file's bytes")
	}
}

func TestSaveRefusesAStaleFingerprint(t *testing.T) {
	// Decision 14: a hand edit made after the file was served is not lost
	// silently to the next save from the UI.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := Write(path, Default()); err != nil {
		t.Fatal(err)
	}
	served, err := Fingerprint(path)
	if err != nil {
		t.Fatal(err)
	}

	// A hand edit after the file was served.
	if err := os.WriteFile(path, []byte("# hand edit\n"+banner), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Save(path, Default(), served)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v, want ErrStale", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "# hand edit") {
		t.Error("the hand edit was overwritten by a save with a stale fingerprint")
	}

	// With the current fingerprint the same save goes through.
	current, err := Fingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Default(), current); err != nil {
		t.Errorf("save with the current fingerprint: %v", err)
	}
}

func TestSaveRefusesAnInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default()
	cfg.Server.Port = -1
	if err := Save(path, cfg, ""); err == nil {
		t.Fatal("saved a config with a negative port")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused save must not leave a file behind")
	}
}

func TestWriteModeAndBanner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := Write(path, Default()); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The file can hold the socket token, so it is mode 0600.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "# thai-smartcard-agent configuration.") {
		t.Error("the written file does not start with the banner")
	}
	// The banner is where the agent says that a save from the UI drops
	// hand-written comments (loading rules).
	if !strings.Contains(string(raw), "comments added by hand are lost") {
		t.Error("the banner does not warn that hand comments are lost on save")
	}
}

func TestWriteCreatesMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thai-smartcard", "config.toml")
	if err := Write(path, Default()); err != nil {
		t.Fatalf("write into a missing directory: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("load back: %v", err)
	}
}

func TestRandomToken(t *testing.T) {
	a, err := RandomToken()
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	b, err := RandomToken()
	if err != nil {
		t.Fatalf("random token: %v", err)
	}
	// Decision 16: a random 128-bit token, so 32 hex characters.
	if len(a) != 32 {
		t.Errorf("token = %q (%d chars), want 32 hex characters", a, len(a))
	}
	if a == b {
		t.Error("two generated tokens are the same")
	}
}

// writeTemp writes body to name inside a temp dir and returns its path.
func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

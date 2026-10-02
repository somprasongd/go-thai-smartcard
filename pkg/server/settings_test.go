package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/somprasongd/go-thai-smartcard/pkg/config"
)

// newSettingsServer builds a mux with the settings routes wired to a temp
// config file, the way newMux does for the agent. The returned path is the
// file the API reads and writes.
func newSettingsServer(t *testing.T, body string) (*httptest.Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := config.Write(path, config.Default()); err != nil {
		t.Fatal(err)
	}

	mux := newMux(ServerConfig{
		Listen:     "127.0.0.1",
		Port:       9898,
		Transports: []string{"ws"},
		Version:    "test",
		ConfigPath: path,
	}, nil)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, path
}

// getJSON fetches an /api route with the headers a browser on the same origin
// would send.
func getJSON(t *testing.T, url string, header http.Header) (int, map[string]any, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The Origin a browser sends is scheme://host:port, without the path.
	if u := mustURL(t, url); u != nil {
		req.Header.Set("Origin", u.Scheme+"://"+u.Host)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp.StatusCode, body, resp.Header
}

// putJSON saves with the custom header the settings guard requires.
func putJSON(t *testing.T, url string, body any, extra http.Header) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if u := mustURL(t, url); u != nil {
		req.Header.Set("Origin", u.Scheme+"://"+u.Host)
	}
	req.Header.Set(settingsHeader, "1")
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp.StatusCode, decoded
}

func testConfig() config.Config {
	cfg := config.Default()
	return cfg
}

func TestAPIInfo(t *testing.T) {
	srv, _ := newSettingsServer(t, "")
	status, body, _ := getJSON(t, srv.URL+"/api/info", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["version"] != "test" {
		t.Errorf("version = %v, want test", body["version"])
	}
	transports, ok := body["transports"].([]any)
	if !ok || len(transports) != 1 || transports[0] != "ws" {
		t.Errorf("transports = %v, want [ws]", body["transports"])
	}
	if body["tls"] != false {
		t.Errorf("tls = %v, want false", body["tls"])
	}
}

func TestGetSettingsHidesTheToken(t *testing.T) {
	// The file holds a token; no response may carry it (decision 16).
	body := "[server]\ntoken = \"abcdef0123456789\"\n"
	srv, _ := newSettingsServer(t, body)

	status, got, _ := getJSON(t, srv.URL+"/api/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "abcdef0123456789") {
		t.Errorf("the token leaked in the response: %s", raw)
	}
	if got["token_set"] != true {
		t.Errorf("token_set = %v, want true", got["token_set"])
	}
	cfg, ok := got["config"].(map[string]any)
	if !ok {
		t.Fatalf("config = %T, want an object", got["config"])
	}
	server, ok := cfg["server"].(map[string]any)
	if !ok {
		t.Fatalf("config.server = %T, want an object", cfg["server"])
	}
	if server["token"] != "" {
		t.Errorf("config.server.token = %v, want an empty string", server["token"])
	}
	if got["version"] == "" {
		t.Error("version is empty, want the file's fingerprint")
	}
}

func TestSettingsVersionRoundTrip(t *testing.T) {
	// Decision 14: a save echoes the fingerprint it was served; a file that
	// changed in between is refused with 409 and left alone.
	srv, path := newSettingsServer(t, "")

	status, served, _ := getJSON(t, srv.URL+"/api/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("get = %d, want 200", status)
	}
	version := served["version"].(string)

	cfg := testConfig()
	cfg.Card.ReadNHSO = true

	// A hand edit lands after the file was served.
	if err := os.WriteFile(path, []byte("# hand edit\n"+mustTemplate(t, testConfig())), 0o600); err != nil {
		t.Fatal(err)
	}

	status, _ = putJSON(t, srv.URL+"/api/settings", map[string]any{
		"config":  cfg,
		"version": version,
	}, nil)
	if status != http.StatusConflict {
		t.Fatalf("stale save status = %d, want 409", status)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "# hand edit") {
		t.Error("the stale save overwrote a hand edit")
	}

	// Refetching yields the new fingerprint, and the same save goes through.
	_, refetched, _ := getJSON(t, srv.URL+"/api/settings", nil)
	status, saved := putJSON(t, srv.URL+"/api/settings", map[string]any{
		"config":  cfg,
		"version": refetched["version"],
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("fresh save status = %d, want 200", status)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("load saved file: %v", err)
	}
	if !loaded.Card.ReadNHSO {
		t.Error("the saved config did not reach the file")
	}
	if saved["version"] == refetched["version"] {
		t.Error("the fingerprint did not change after a save")
	}
}

func TestSettingsPUTRules(t *testing.T) {
	srv, _ := newSettingsServer(t, "")

	tests := []struct {
		name    string
		method  string
		headers http.Header
		want    int
	}{
		{
			name:   "a write without the custom header is refused",
			method: http.MethodPut,
			want:   http.StatusForbidden,
		},
		{
			name:   "a write with the wrong custom header value is refused",
			method: http.MethodPut,
			headers: http.Header{
				settingsHeader: []string{"yes"},
			},
			want: http.StatusForbidden,
		},
		{
			name:   "a write behind a proxy is refused",
			method: http.MethodPut,
			headers: http.Header{
				settingsHeader:    []string{"1"},
				"X-Forwarded-For": []string{"203.0.113.9"},
			},
			want: http.StatusForbidden,
		},
		{
			name:   "a write with a Forwarded header is refused too",
			method: http.MethodPut,
			headers: http.Header{
				settingsHeader: []string{"1"},
				"Forwarded":    []string{"for=203.0.113.9"},
			},
			want: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, srv.URL+"/api/settings", strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			for k, vs := range tt.headers {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}

func TestSettingsRouteRules(t *testing.T) {
	srv, _ := newSettingsServer(t, "")

	t.Run("a foreign Host is refused, which is what blocks DNS rebinding", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/settings", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "evil.example"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("a cross-origin request is refused and no CORS header ever appears", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/settings", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "http://evil.example")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want none: no CORS headers is rule 1", got)
		}
	})

	t.Run("the agent's own origin passes", func(t *testing.T) {
		status, _, _ := getJSON(t, srv.URL+"/api/settings", nil)
		if status != http.StatusOK {
			t.Errorf("status = %d, want 200", status)
		}
	})

	t.Run("the settings page is served", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/settings")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		raw := new(bytes.Buffer)
		raw.ReadFrom(resp.Body)
		if !strings.Contains(raw.String(), "SMC — Settings") {
			t.Error("the settings page content is not what was served")
		}
	})

	t.Run("a proxy cannot pretend to be loopback on a get either", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/settings", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})
}

func TestSettingsTokenGeneration(t *testing.T) {
	// Decision 16: the agent generates the token. The PUT that turns exposure
	// on while there is no token makes one, writes it in the same save, and
	// returns it in that response only.
	srv, path := newSettingsServer(t, "")

	status, served, _ := getJSON(t, srv.URL+"/api/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("get = %d, want 200", status)
	}

	cfg := testConfig()
	cfg.Server.Listen = "0.0.0.0"

	status, saved := putJSON(t, srv.URL+"/api/settings", map[string]any{
		"config":  cfg,
		"version": served["version"],
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("put = %d, want 200: %v", status, saved)
	}
	token, ok := saved["token"].(string)
	if !ok || len(token) != 32 {
		t.Fatalf("token = %v, want a 32 character value returned once", saved["token"])
	}

	// The file holds it, so a restart keeps working.
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Token != token {
		t.Errorf("file token = %q, want the generated one", loaded.Server.Token)
	}

	// And no later GET carries the value.
	_, again, _ := getJSON(t, srv.URL+"/api/settings", nil)
	raw, _ := json.Marshal(again)
	if strings.Contains(string(raw), token) {
		t.Errorf("a later GET returned the token: %s", raw)
	}
	if again["token_set"] != true {
		t.Errorf("token_set = %v, want true", again["token_set"])
	}
}

func TestSettingsRegenerateToken(t *testing.T) {
	body := "[server]\nlisten = \"0.0.0.0\"\ntoken = \"aaaa1111aaaa1111aaaa1111aaaa1111\"\n"
	srv, path := newSettingsServer(t, body)

	_, served, _ := getJSON(t, srv.URL+"/api/settings", nil)

	cfg := testConfig()
	cfg.Server.Listen = "0.0.0.0"
	cfg.Server.Token = "aaaa1111aaaa1111aaaa1111aaaa1111"

	status, saved := putJSON(t, srv.URL+"/api/settings", map[string]any{
		"config":           cfg,
		"version":          served["version"],
		"regenerate_token": true,
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("put = %d, want 200: %v", status, saved)
	}
	fresh, ok := saved["token"].(string)
	if !ok || fresh == "aaaa1111aaaa1111aaaa1111aaaa1111" {
		t.Fatalf("token = %v, want a new value returned once", saved["token"])
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Token != fresh {
		t.Errorf("file token = %q, want the regenerated one", loaded.Server.Token)
	}
}

func TestSettingsSaveRefusesAnInvalidConfig(t *testing.T) {
	srv, path := newSettingsServer(t, "")

	_, served, _ := getJSON(t, srv.URL+"/api/settings", nil)

	cfg := testConfig()
	cfg.Server.Transports = nil // the strict loader refuses an empty list

	status, _ := putJSON(t, srv.URL+"/api/settings", map[string]any{
		"config":  cfg,
		"version": served["version"],
	}, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the file did not survive a refused save: %v", err)
	}
}

// The token the page never saw must survive a save that echoes the served
// config back: the agent merges it from the file, not from the request.
func TestSettingsSaveKeepsTheToken(t *testing.T) {
	body := "[server]\ntoken = \"aaaa1111aaaa1111aaaa1111aaaa1111\"\n"
	srv, path := newSettingsServer(t, body)

	_, served, _ := getJSON(t, srv.URL+"/api/settings", nil)

	status, _ := putJSON(t, srv.URL+"/api/settings", map[string]any{
		"config":  testConfig(),
		"version": served["version"],
	}, nil)
	if status != http.StatusOK {
		t.Fatalf("put = %d, want 200", status)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Server.Token != "aaaa1111aaaa1111aaaa1111aaaa1111" {
		t.Errorf("file token = %q, want it to survive the save", loaded.Server.Token)
	}
}

func mustTemplate(t *testing.T, cfg config.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.toml")
	if err := config.Write(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func mustURL(t *testing.T, raw string) *neturl.URL {
	t.Helper()
	u, err := neturl.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

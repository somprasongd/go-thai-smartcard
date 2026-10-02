package server

import (
	"github.com/somprasongd/go-thai-smartcard/pkg/model"
	"net/http"
	"runtime"
)

// DiagnosticSnapshot is an explicit allowlist, independent of config/card data.
type DiagnosticSnapshot = model.Diagnostics

func (api *settingsAPI) serveDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d := DiagnosticSnapshot{RunMode: "unknown", OS: runtime.GOOS, Architecture: runtime.GOARCH}
	if api.diagnostics != nil {
		d = api.diagnostics()
	}
	d.Version = api.version
	d.Endpoint = ownOrigin(r)
	d.Transports = api.transports
	d.TLS = api.tlsEnabled
	d.Health = HealthSnapshot{State: "starting", Readers: []string{}}
	if api.status != nil {
		d.Health = api.status.Health()
	}
	writeJSON(w, http.StatusOK, d)
}

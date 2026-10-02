# Local endpoint discovery and transactional settings

Accepted on 2026-10-02. This extends the v3 thin-client decision: the tray reads
public endpoint metadata, never `config.toml`, and never spawns an agent.

## Save contract

One process coordinator serializes saves across HTTP generations. Validate and
check the served fingerprint, reserve changed ports and validate certificates,
then stage public metadata before writing anything. Config replacement is atomic,
private, and retains the old raw bytes for rollback. Apply starts the candidate
generation while keeping old listeners alive; publish its actual loopback URL.
Only successful application updates the current configuration and card options.
Return the new fingerprint and `endpoint_url`, flush the response, then retire
old generations and their WebSocket/socket.io connections. Unchanged HTTP/TLS
bindings are reused instead of colliding with the process's own sockets.

An invalid configuration returns 400, a stale fingerprint 409, and bind/persist/
publication failures 500. Failed publication rolls runtime back and restores the
original config bytes. Restoration checks the just-written fingerprint; a hand
edit is preserved and a rollback conflict is returned and logged explicitly.
This is an in-process transaction, not a cross-file crash-atomic database:
startup reloads the persisted configuration and republishes the running endpoint.

Legacy `ServerConfig.OnChange` remains available. The agent uses the new
`ApplySettings` callback and `SettingsResult.AfterResponse` to retire connections
only after the old request has received its complete response.

## Discovery contract

JSON: `schema_version: 1`, random per-process `instance_id`, increasing
`generation`, and a local HTTP `base_url`. No token, config path or card data.
The corresponding `/api/info` returns `instance_id`. Wildcard binds normalize
to loopback; IPv6 URLs are bracketed. A specific LAN-only bind is not published
as a local settings endpoint, and discovery does not alter socket authentication.

Foreground and managed-service slots are selected by launch mode, not effective
UID. Their paths are:

| Mode | Endpoint file |
| :-- | :-- |
| Foreground | `os.UserCacheDir()/thai-smartcard/endpoint.json` |
| Linux service | `/var/lib/thai-smartcard/endpoint.json` |
| macOS service | `/Library/Application Support/ThaiSmartcardEndpoint/endpoint.json` |
| Windows service | `%ProgramData%\ThaiSmartcardEndpoint\endpoint.json` |

Service metadata is publicly readable but writable
only by the service owner/administrators; user metadata is private to that user.
Windows uses explicit ACLs and replacement-compatible file sharing. Linux packages
own the runtime directory as `thai-smartcard` and allow it in systemd writable paths.

An OS process lock permits one publisher per slot. Atomic replacement never
exposes partial JSON. Normal shutdown removes only this process's metadata;
crash leftovers must pass live instance verification before selection. A startup
publication failure warns but does not stop the card agent; explicit `--url`
remains available. Settings refuses changing an endpoint it cannot publish.

## Tray and browser behavior

Precedence: explicit `--url`, verified user endpoint, verified service endpoint,
then the legacy `http://127.0.0.1:9898`. Read at startup, before retries, and every
two seconds even while connected. Verify new candidates via `/api/info` with a
one-second timeout. Changed URL, instance or generation closes the old socket
and updates all menu links. Missing/corrupt metadata never cuts a healthy socket.
Handshake timeout is three seconds; disconnected retries are five seconds apart.
Authentication and disabled-WebSocket errors are distinct from an unreachable agent.

Settings navigates to the returned URL after success. Any token currently shown
on the page suppresses automatic navigation and exposes a link instead. Other
tabs on the previous port must be reopened from the tray; browsers cannot read
the discovery file. Remote discovery and token distribution are outside scope.

## Validation and rollout

Regression tests use temporary listeners and synthetic card-state messages;
no reader or trace is required. Cover bind/certificate/persistence/publication
failures, exact-byte rollback, hand-edit conflicts, concurrent saves, TLS socket
reuse, complete old-port responses, retired upgraded sockets, ongoing broadcasts,
tray precedence, malformed/stale identities, live migration and same-URL restart.
Execute the bundled Settings script to verify navigation and token retention.

Local gate: `make check`, `go test -race ./pkg/server/ ./cmd/tray/ ./cmd/agent/
./internal/...`, and a native tray build. Cross-compilation checks platform code
but does not prove OS permissions or installers: record Windows and Linux runtime
ACL/ownership/atomic replacement and package acceptance separately before claiming
all three platforms are verified. No release, install or service restart is implied.

Local record (2026-10-02): macOS arm64 passed the local gate, race detector,
native tray build, agent and card-logic wasm builds, and the actual Settings
script's navigation/token harness. Windows amd64 and Linux amd64 shared packages
and the tray client compile with `go test -exec=true`; those commands do not run
target tests. Packaging YAML ownership/modes and systemd writable paths were
checked statically. No native Windows/Linux runtime or installer acceptance is
claimed; the local Docker daemon was unavailable for a Linux container run.

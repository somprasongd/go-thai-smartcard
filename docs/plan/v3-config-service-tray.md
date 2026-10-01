# Plan: config file, service, settings UI and tray (v3)

Status: proposal, work not started on `v3` yet. Written 2026-10-01 against
`main` at v2.0.0; since then the ws race fix shipped from `main` as v2.0.1
(decision 8), and `v3` branched from `main` after that release.

## Goal

Make the agent behave like a desktop app (the model is Ollama: tray icon,
settings screen, "expose to the network" switch) while keeping the headless
install that runs as a system service.

- Configuration comes from **one config file**, not environment variables.
- The card options (face image, laser ID, NHSO entitlement), the reader, and
  whether the agent listens beyond localhost are changed **through a UI only**
  (tray or the `/settings` page), never over the card socket.
- A browser normally runs on the same machine as the agent, but a kiosk can run
  it on another machine, so exposing the agent to the LAN must be possible and
  safe enough to turn on.
- The default client transport is raw WebSocket. socket.io is opt-in.

Non-goals: authentication for the card data beyond a shared token, a remote
management API, auto-update.

## Decisions

| # | Decision | Why |
| :- | :------- | :-- |
| 1 | Config file is TOML, `config.toml`. Env vars (`SMC_*`) are **not read** | Ollama's env-first model needs `launchctl setenv` on macOS and the environment dialog on Windows, which is the pain this plan removes. TOML has comments and suits hand-editing over SSH |
| 2 | `allow_remote_options` is removed. `set-options` and `set-reader` are removed from the socket contract | Settings change from a UI only. The card socket becomes read-only apart from harmless requests |
| 3 | The agent is the only writer of the config file. The tray and `/settings` call `/api/settings` | A service runs as root/SYSTEM, the tray as the user, so the tray cannot write `/etc` or `ProgramData` itself |
| 4 | The bundled page stays in the agent, split into `/` (test page, read-only) and `/settings` | The tray is optional (headless, GNOME without an extension) and a webview in it would add cgo and per-OS dependencies |
| 5 | Two binaries: `thai-smartcard-agent` and `thai-smartcard-tray` | The tray needs cgo; the agent should stay cross-compilable and keep building for wasm |
| 6 | Default transport is `ws`; `socketio` is opt-in | See [Transports](#transports) |
| 7 | Default `listen` is `127.0.0.1` | Today the agent binds every interface (`http.ListenAndServe(":"+port)`) while logging "localhost" |
| 8 | Ships as **v3.0.0** | Removes exported `pkg/util` env helpers, stops reading `SMC_*`, changes the default listen address and the default transport |
| 9 | `server.allowed_origins` defaults to `["*"]` | Keeps the main use working out of the box: a web app on its own origin reading from the local agent. While it is `"*"` the settings page and the tray show a warning that any site the user visits can read the card |
| 10 | The socket token is passed as `?token=` (browsers) or `Authorization: Bearer` (other clients), checked once before the upgrade (see [Socket token](#socket-token)) | One check for both transports, no unauthenticated state to manage |
| 11 | `remote_control` is removed from `smc-options` and `smc-status` | It only reported whether `set-options`/`set-reader` were accepted; both are gone, so it would be a constant |
| 12 | The agent is always a system service. Installers install and start it; the tray **never spawns** the agent. If it cannot reach the agent it says so and shows how to start it. `agent run` (foreground) is for development | A tray that can also spawn an agent creates a second run mode with its own config path, its own PC/SC permissions and a possible port clash, and doubles the test matrix. Ollama can spawn because it has no service mode on the desktop; this project must also serve kiosks and headless hosts |
| 13 | No `migrate` subcommand. The upgrade path is a changelog table mapping each `SMC_*` variable to its config key — including a row for `SMC_ALLOW_REMOTE_OPTIONS`, the one variable with no replacement, saying so rather than leaving a silent gap — plus a startup log that prints the equivalent TOML for any stale `SMC_*` it finds (see [Migration table](#migration-table)) | A service's environment lives in its unit file or plist, not in the shell an administrator would run `migrate` from, so the subcommand would not see the values in use. An administrator grepping a unit file against the table must find every variable accounted for |
| 14 | A save from `/settings` or the tray refuses to overwrite a config file that changed on disk since it was **served**. The check is the file's fingerprint — the hash of its bytes — carried through the API: `GET /api/settings` returns it, `PUT /api/settings` echoes it back, and a mismatch is a `409` | Otherwise a hand edit made after the agent started is lost silently by the next save. The fingerprint rather than the mtime: mtime granularity depends on the filesystem, and nanoseconds since the epoch do not survive JavaScript's float64 numbers. See [Loading rules](#loading-rules) |
| 15 | No "Start at login" toggle in the tray. The installer registers the tray at login and users turn it off in the OS's login items | The agent runs without the tray, so the toggle only affects the icon; it would cost three platform mechanisms and drift from the OS setting. See [Start at login](#start-at-login) |
| 16 | The agent generates the socket token, not the client. Any `PUT` that turns exposure on while `token` is empty makes a random 128-bit `crypto/rand` token, writes it in the same save, and returns it in that response only; `GET /api/settings` never returns the value, only `token_set`. The tray's "Expose to network" is a toggle only while a token exists and otherwise opens `/settings`; turning exposure off is always a direct toggle | One generation path for the page, the tray and any future caller, and no way to expose the agent to the LAN without the token ceremony having happened somewhere the token can be shown and copied. See [Socket token](#socket-token) |

## Findings in the current code

- `pkg/server/server.go` binds `:port`, i.e. all interfaces, and both transports
  accept any origin (`CheckOrigin` returns true in `socketio.go` and
  `websocket.go`). Any page the user opens can connect to `ws://localhost:9898`
  and receive the card data. This is true even with `listen = 127.0.0.1`, so
  `allowed_origins` matters on loopback too (see
  [Open decisions](#open-decisions)).
- `pkg/server/websocket.go`: `subscriber.clients` was a plain map written by
  each connection's handler goroutine while the broadcast goroutine iterated
  it, with no lock — a data race that could abort the process with "concurrent
  map iteration and map write" — and `upgrader.CheckOrigin` was reassigned on
  every request. **Fixed in v2.0.1**: a mutex now guards every access, the
  broadcast iterates a snapshot taken under the lock so a slow peer cannot
  stall a connecting one, `CheckOrigin` is set once at the package level, and
  `TestWebSocketBroadcastWhileClientsChurn` exercises it under
  `go test -race`. `v3` branched from `main` after that release, so the fix is
  already in the branch.
- The `Makefile` exports `SMC_PORT`, but `cmd/agent/main.go` reads
  `SMC_AGENT_PORT`, so `make dev` has never changed the port. The migration
  table in the changelog must use the names the agent actually reads
  (`SMC_AGENT_PORT`, `SMC_SHOW_IMAGE`, `SMC_SHOW_LASER`, `SMC_SHOW_NHSO`,
  `SMC_ALLOW_REMOTE_OPTIONS`).
- The page already asks for `get-options` and `get-status` on connect, so a
  read-only page needs no protocol change beyond dropping the write commands.
- `OptionsStore` already applies new options on the next card insert, and
  `smc.Control` already carries reader selection. The settings API reuses both.

## Config file

### Location

| Platform | Path |
| :------- | :--- |
| Linux, service | `/etc/thai-smartcard/config.toml` |
| macOS, service | `/Library/Application Support/ThaiSmartcard/config.toml` |
| Windows, service | `%ProgramData%\ThaiSmartcard\config.toml` |
| `agent run` as a user (development) | `~/.config/thai-smartcard/config.toml` on Linux, `~/Library/Application Support/ThaiSmartcard/config.toml` on macOS, `%AppData%\ThaiSmartcard\config.toml` on Windows |

The service paths come first because the agent is always a system service
(decision 12); the per-user paths exist only for running it in the foreground.

`--config <path>` overrides it. That flag and `--version` are the only flags;
the path is not a setting.

### Schema

```toml
[server]
listen = "127.0.0.1"      # "0.0.0.0" exposes the agent to the network
port = 9898
transports = ["ws"]       # "ws", "socketio", or both
allowed_origins = ["*"]   # origins allowed to open a socket; "*" is any
token = ""                # required when listen is not loopback

[card]
read_face_image = true
read_laser_id = true
read_nhso = false         # treatment entitlement
reader = ""               # empty watches every reader

[tls]                     # phase 3
enabled = false
port = 9899
mode = "files"            # "files", later "auto"
cert_file = ""
key_file = ""
hostnames = []
```

### Loading rules

- Defaults, then the file. There is no other source.
- Strict: an unknown key or a bad value stops the agent and names the file and
  line. A service that silently falls back to defaults after a typo is worse
  than one that does not start. `toml.MetaData.Undecoded()` in
  `BurntSushi/toml` gives the unknown keys.
- A missing file is written with the defaults and comments, if the directory is
  writable.
- The agent writes the file from a template with comments, not by
  re-marshalling, because `BurntSushi/toml` drops comments on write and the
  schema is small enough to template. A comment an administrator adds by hand
  is therefore **lost on the next save from the UI**. Three places say so: a
  banner at the top of the written file, a line above the save button on
  `/settings`, and the README.
- A save also refuses to overwrite a file that changed since it was served
  (decision 14). `GET /api/settings` returns the file's fingerprint next to
  the config, and `PUT /api/settings` echoes it back. The agent re-hashes the
  file at request time, never from a value cached at startup, so a hand edit
  made after the agent started is caught too. A mismatch is a `409` with "the
  file changed on disk, reload before saving". Two pages editing at once get
  the same refusal — the second save is told to reload rather than silently
  undo the first — and the tray answers a `409` by refetching and asking the
  user to repeat the action.
- If a `SMC_*` variable is present at start, log a warning that it is no
  longer read, and print the TOML that matches it, e.g. `SMC_SHOW_NHSO=true` becomes
  `read_nhso = true` under `[card]`, for the administrator to paste. A variable
  with no replacement prints a comment naming it as removed, never a fabricated
  key: `# SMC_ALLOW_REMOTE_OPTIONS is no longer read and has no replacement;
  settings change only through /settings`. Do not honour the variable. The
  changelog carries the full mapping, including the no-replacement row, as a
  table (decision 13).
- Hand edits need a restart (`service restart`). No file watcher at first.
- Changed from a UI: `card.*` apply on the next card insert; `server.*` and
  `tls.*` restart the listener.

### Migration table

The changelog's upgrade note carries this table as-is. It uses the variable
names the agent actually reads (see
[Findings](#findings-in-the-current-code)); `SMC_PORT` never existed.

| v2 variable | v3 config key | Note |
| :---------- | :------------ | :--- |
| `SMC_AGENT_PORT` | `[server] port` | |
| `SMC_SHOW_IMAGE` | `[card] read_face_image` | |
| `SMC_SHOW_LASER` | `[card] read_laser_id` | |
| `SMC_SHOW_NHSO` | `[card] read_nhso` | |
| `SMC_ALLOW_REMOTE_OPTIONS` | — no replacement | What it gated no longer exists: the card socket is read-only, and settings change only through `/settings`. Delete the line from the unit file; there is nothing to set instead. Both `true` and `false` users lose nothing — a `false` install's pinned values now live in `config.toml`, and a `true` install's remote reconfiguration is gone by design |

## Transports

`server.transports` selects what the agent serves. Default `["ws"]`.

- `ws` registers `/ws` (gorilla/websocket).
- `socketio` registers `/socket.io/` (go-socket.io v1.6.2, which speaks the
  socket.io v2 protocol, so clients use the 2.x client as the README shows).
- An empty list is a config error.
- A disabled transport is never constructed: no `/socket.io/` handler, no
  engine.io server, no goroutines.
- The bundled page learns what is enabled from `GET /api/info`
  (`{version, transports, tls}`, same-origin only). With socket.io off it does
  not load the CDN script and shows that transport as disabled instead of
  "offline".
- Breaking: clients written for socket.io stop working after the upgrade until
  `transports = ["ws", "socketio"]` is set. The upgrade note in the changelog
  must say so first.
- Later (not planned for v3): with socket.io off by default the dependency on
  go-socket.io could move behind a build tag to shrink the binary.

## Settings UI and API

### Surfaces

- `GET /settings` is a self-contained page: plain HTML and JS, no CDN, because
  hospital networks often cannot reach the internet. It reads and writes
  `/api/settings`.
- `GET/PUT /api/settings` reads and writes the config file and applies changes:

  ```
  GET /api/settings → { "config": { … }, "version": "<hash of the file bytes>", "token_set": true }
  PUT /api/settings ← { "config": { … }, "version": "<as served by the GET>" }
                    → 200, or 409 "the file changed on disk, reload before saving"
  ```

  `version` is the optimistic-concurrency check of decision 14, re-verified
  against the file at request time. `token` never appears in a response;
  `token_set` reports whether one exists (decision 16).
- The tray calls the same API. It never touches the file.

### Rules for `/settings` and `/api/*`

Loopback is not enough, because the user's browser is on loopback too. The card
sockets must stay cross-origin readable; these routes must not.

1. No CORS headers.
2. `Host` must be `localhost`, `127.0.0.1` or `[::1]` (blocks DNS rebinding).
3. If `Origin` is present it must equal the agent's own origin.
4. Writes require a fixed custom header, e.g. `X-SMC-Settings: 1`. A
   cross-origin page then needs a preflight, which fails without CORS.
5. `RemoteAddr` must be loopback, and the request is refused if it carries
   `X-Forwarded-For` or `Forwarded`, so a reverse proxy on the same host does
   not turn the whole internet into "loopback".

A kiosk with no screen is configured by editing the file over SSH, or through an
SSH tunnel to `/settings`. There is deliberately no network-reachable way to
change settings.

Possible hardening later, for hosts with several users: carry the tray's calls
over a unix socket / named pipe with per-user permissions, as Tailscale does.

### The test page (`/`)

The current `pkg/server/index.html`. It moves to `pkg/server/web/index.html`
with `settings.html` beside it, embedded with `//go:embed web`. Its option
toggles and reader picker become read-only displays that link to `/settings`.
Its display presets (kiosk, dev, counter) are client-side and stay.

### Socket token

Required when `listen` is not loopback, optional otherwise.

- Browsers pass `?token=<token>` on the `/ws` URL, or the socket.io 2.x
  client's `query` option. Browsers cannot set headers on a WebSocket, so a
  query parameter is the one form they can send.
- Other clients (native apps, server-side code, test tools) may send
  `Authorization: Bearer <token>` instead, which keeps the token out of URLs.
- If both are present the header wins. Either one satisfies the check.
- Checked in one middleware in front of both `/ws` and `/socket.io/`, before the
  upgrade or handshake. A bad token gets `401` and never sees an event, so
  there is no unauthenticated connection state and no per-client bookkeeping.
- Constant-time comparison. The agent never logs request URLs or queries.
- The agent generates the token, not the client (decision 16). The first `PUT`
  that turns exposure on while `token` is empty makes a random 128-bit
  `crypto/rand` token, writes it in the same save, and returns it **in that
  response only**; `GET /api/settings` reports `token_set` and never the value.
  The settings page shows it once with a copy button and offers "Regenerate
  token" through the same path, for a token shown and lost. The config file
  holds it, so the file is mode `0600`. The strict loader still refuses a
  hand-written file that exposes without a token; the UI path cannot produce
  that state, because generation and write are one save.
- Limits to state in the README: without TLS the token and the card data cross
  the LAN in clear text however the token is sent, and a token embedded in a
  web app's JavaScript keeps other hosts and other sites out but not the person
  using that page.
- Alternatives considered: `Sec-WebSocket-Protocol` (ws only, awkward for
  socket.io), a first-message `auth` (needs an unauthenticated state and
  per-client gating of broadcasts), a one-time ticket from an HTTP endpoint
  (extra round trip and a CORS carve-out).

## Wire contract changes

- Removed actions: `set-options`, `set-reader` (answered with the existing
  `unknown action` error).
- Kept: `get-options`, `get-status`, `refresh-readers`, `read-now`.
- Removed field: `remote_control` in `smc-options` and `smc-status`
  (`pkg/model`, `pkg/smc/daemon.go`, `cmd/agent`). It used to report whether the
  agent accepted `set-options` and `set-reader`. A client that read a missing
  field as "allowed" (the old bundled page did) must be told in the changelog
  that the answer is now always "no".
- Removed: `util.GetEnv`, `GetEnvInt`, `GetEnvBool` (`pkg/util/env.go`), the
  `SMC_*` variables in the `Makefile`, and the Configuration and Runtime options
  sections of the README.

## Architecture

```
thai-smartcard-agent   headless; PC/SC, ws/socket.io, /, /settings, /api/*
thai-smartcard-tray    cgo; thin client of the agent's /api
```

### Agent as a service

[`kardianos/service`](https://github.com/kardianos/service) covers Windows
Service, systemd/Upstart/SysV and launchd:

```
thai-smartcard-agent service install|uninstall|start|stop|status
thai-smartcard-agent run          # foreground
```

- It cannot express `Dependencies` on Linux or launchd, so the systemd unit
  template sets `After=pcscd.service` itself.
- On every platform the agent is a system service (Linux systemd, macOS
  LaunchDaemon, Windows Service), desktop or headless. There is no per-user
  agent mode.

#### Who starts the agent

The installer for each platform installs the service and starts it
(`.deb`/`.rpm` post-install, the macOS `.pkg`, the Windows installer). The tray
installer depends on the agent's, so installing the tray alone is not possible.
The tray never starts the agent itself. When it cannot reach `/api` it shows
"agent is not running" with the command to start it for that platform, and
keeps polling. `agent run` in a terminal remains for development.

### Tray

- Menu: reader and card state, Open test page, Open settings, Expose to network,
  read image / laser ID / NHSO toggles, Quit. Quit closes the tray for the
  current session only; the agent keeps running.
- "Expose to network" is a checkable toggle only while a token exists
  (`token_set`); before that, choosing it opens `/settings`, where the token is
  generated and shown once (decision 16). Turning exposure **off** is always a
  direct toggle, in every state, because it is the safe direction.
- Library: [`fyne-io/systray`](https://github.com/fyne-io/systray) (requires
  cgo; on Linux it uses the StatusNotifier DBus interface).
- Linux: GNOME needs the AppIndicator extension to show the icon. At start the
  tray checks for `org.kde.StatusNotifierWatcher`; if it is absent it sends a
  notification linking to `/settings` instead of failing silently.
- macOS: the tray must live in a signed, notarized `.app`.
- Windows: build with `-ldflags -H=windowsgui` so no console opens.

#### Start at login

There is no "Start at login" toggle in the tray (decision 15). The installer
registers the tray to start at login, and a user who does not want it turns it
off in the operating system's own list of login items.

| Platform | What the installer registers | Where a user turns it off |
| :------- | :--------------------------- | :------------------------ |
| Windows | a value under `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Run` pointing at the tray — machine-wide, deliberately not `HKCU` (see below). A service cannot show a tray icon, so a login entry is the only way to get one | Settings > Apps > Startup, or Task Manager > Startup — these write a per-user `StartupApproved\Run` override, so each user can still turn the machine-wide entry off for themselves |
| macOS | a login item or LaunchAgent for the tray's `.app` (`SMAppService` on recent macOS; to be checked against the minimum macOS version supported) | System Settings > General > Login Items |
| Linux | `/etc/xdg/autostart/thai-smartcard-tray.desktop`, installed by the package | the desktop's startup applications settings, which write a per-user override in `~/.config/autostart/` |

On Windows the entry is machine-wide on purpose. The installer runs elevated,
usually as an administrator who is not the kiosk user, so an `HKCU` value would
land in the administrator's hive and the tray would never start for the person
actually at the machine — and a development machine, where the two are the same
account, would never catch it. Every interactive login gets the tray, which is
acceptable because it is only a viewer for the machine-wide service. The
installer offers "start the tray at login (all users)", on by default, and the
uninstaller removes the value. The all-users Startup folder under
`%ProgramData%` is an equivalent; the registry is chosen because installers
conventionally manage it and it uninstalls cleanly.

This is unrelated to the service. The agent runs whether or not the tray does,
so a tray that is not running costs the user only the icon and the shortcut to
`/settings`, and `kardianos/service` has no part in it.

Why not a toggle: it would need three platform-specific mechanisms and three
test paths; the toggle would drift from reality whenever the user changes the
operating system's own setting, unless the menu re-reads it from the OS each
time it opens; and the operating systems already give users a place to do this.
The part of Ollama's settings screen visible in the reference screenshot does
not offer it either.

If a toggle is wanted later: read the state from the OS every time the menu
opens and keep none of its own, use only per-user mechanisms that need no
administrator rights, and hide the toggle where the OS or a policy does not
allow it. A small library for XDG autostart, LaunchAgents and the Windows Run
key may exist (`emersion/go-autostart` was seen in passing) but has not been
checked.

### TLS

Needed only when an `https://` page talks to an agent on a LAN IP (`wss://` is
required there). Not needed for `http://` pages, which is the normal kiosk case.

- `files` mode (phase 3): the operator supplies `cert_file` and `key_file`. The
  agent serves HTTPS on `tls.port`, reloads the files when their mtime changes,
  and when TLS is on the plain HTTP listener is forced to loopback.
- A real certificate for a LAN **IP** cannot come from a public CA. The usable
  routes are a hospital-owned domain with an internal DNS record and a DNS-01
  certificate, or the organisation's own PKI.
- `auto` mode (later): the agent makes a local CA and issues a leaf with the
  host's IPs in the SAN, regenerating the leaf when the IP set changes while the
  CA stays. The CA must be installed in the trust store of the machine running
  the **browser**. Created on first start with TLS enabled, not by the
  installer, because the SAN depends on the runtime IP; `cert init` lets an
  installer trigger it. Files are `0600`, owned by the service user.
- A lone self-signed certificate is not a solution: a script's `wss://` fails
  silently, the user has to visit the URL and accept a warning per browser, and
  a locked-down kiosk cannot.
- Alternative for kiosks that control the browser command line: Chrome's
  `--ignore-certificate-errors-spki-list`. Untested here; the key must survive
  renewals.

## Phases

| Phase | Content | Release |
| :---- | :------ | :------ |
| 1 | `config.toml` loader (no env), `listen`, `transports`, `allowed_origins`, `token`; `/api/settings`, `/settings`, `/api/info`; split `web/`; remove `set-options`, `set-reader`, `remote_control`, `util` env helpers; fix the ws subscriber race; README, CHANGELOG, Makefile | **v3.0.0** |
| 2 | `service` subcommand, systemd unit template, `.deb`/`.rpm` for the agent | v3.1.0 |
| 3 | TLS `files` mode | v3.2.0 |
| 4 | Tray (macOS, Windows, Linux), tray packages, installers that install and start the agent service and register the tray at login | v3.3.0 |
| 5 | TLS `auto` mode | later |

Phase 1 is one release on purpose. Shipping the config file without
`/settings` would take the toggles away from the bundled page with nothing to
replace them. It is built as several PRs so each can be reviewed on its own.

### Phase 1 pull requests

Branch from `main` into an integration branch `v3`. Every PR targets `v3`, and
`v3` merges into `main` once, when the last PR is in, so `main` never holds a
half-changed wire contract. The gate for each PR is the local one from
`AGENTS.md`.

| PR | Content | Touches the wire contract |
| :- | :------ | :------------------------ |
| 1 | ~~Fix the ws subscriber race in `pkg/server/websocket.go` (lock around `clients`, set `CheckOrigin` once)~~ **Done**: shipped from `main` as v2.0.1 (2026-10-01) before `v3` branched, so the branch already carries it | no |
| 2 | `pkg/config`: schema, defaults, strict load, templated write with the banner, the fingerprint of decision 14 with its refusal on a stale one, tests (table-driven, no reader needed). Not wired into the agent yet | no |
| 3 | Wire the config into `cmd/agent`: `--config`, `listen`, `transports`, the stale-`SMC_*` warning with its TOML snippet; delete `pkg/util/env.go`; update the `Makefile` (see [Makefile and documentation](#makefile-and-documentation)) and the `AGENTS.md` setup commands and layout | no (listening address and default transport change) |
| 4 | `allowed_origins` and token middleware in front of `/ws` and `/socket.io/`, with tests for each rule | no (new rejections) |
| 5 | `/api/info`, `/api/settings`, `/settings`, the `web/` split, read-only test page; remove `set-options`, `set-reader` and `remote_control`. These go together because removing the toggles without `/settings` leaves nothing to replace them. `/api/settings` carries the `version` round-trip (decision 14) and the server-side token generation with `token_set` and the regenerate action (decision 16) | **yes** |
| 6 | Update every Markdown file (`README.md`, `AGENTS.md`, `CHANGELOG.md`) as listed under [Makefile and documentation](#makefile-and-documentation): connect over ws first, the stale-comment warning, the proxy warning, the upgrade note and the `SMC_*` → config key table | docs |

### Phase 1 checklist

- [x] PR 1: ws race fixed; `TestWebSocketBroadcastWhileClientsChurn` runs
      clean under `go test -race ./pkg/server/`. Shipped from `main` as
      v2.0.1, already in `v3` through its base
- [ ] PR 2: `pkg/config` loader and writer, unknown-key and bad-value cases, the
      fingerprint refusal on a stale save, the banner
- [ ] PR 3: agent uses the config only; stale `SMC_*` prints a TOML snippet;
      `Makefile` has no `SMC_*`, runs packages not files, and has `check`;
      `.gitignore` covers `config.dev.toml`
- [ ] PR 4: origin and token checks; tests for each disabled transport
- [ ] PR 5: `/settings` with the pre-save comment warning; the five route rules
      tested; the `version` round-trip (a `409` on a stale save), token
      generation shown once, `token_set` on GET and the regenerate action
      tested
- [ ] PR 6: README, AGENTS.md and CHANGELOG match the code; the CHANGELOG
      table uses `SMC_AGENT_PORT`, not `SMC_PORT`; no remaining mention of
      `SMC_*`, `set-options`, `set-reader`, `remote_control` or
      `./cmd/agent/main.go` outside the changelog's history (check with
      `grep -rn`)
- [ ] `go build ./... && go test ./... && go vet ./... && test -z "$(gofmt -l .)"`
      and `GOOS=js GOARCH=wasm go build ./pkg/smc/` on `v3` before the merge to
      `main`

## Makefile and documentation

Every phase leaves the `Makefile` and the Markdown files matching the code it
ships. The v3 work changes enough of both that they are listed here, and phase 1
is not done until they are updated (PR 3 for the `Makefile`, PR 6 for the
Markdown).

### Makefile

Today it exports `SMC_*` variables and runs single files
(`go run ./cmd/agent/main.go`), which stops working as soon as `cmd/agent` has
more than one file.

- Remove the `SMC_*` assignments and `export` lines. They are dead after PR 3,
  and `SMC_PORT` was already wrong (the agent reads `SMC_AGENT_PORT`).
- `dev` runs the package, `go run ./cmd/agent --config ./config.dev.toml`, with a
  git-ignored `config.dev.toml` that the agent writes with defaults on first run.
  Add `config.dev.toml` to `.gitignore` next to `/testdata`.
- `example` runs `./cmd/example` as a package for the same reason.
- Add `test` (`go test -race ./...`), `vet`, `fmt-check`
  (`test -z "$(gofmt -l .)"`), and `check`, which runs the whole local gate from
  `AGENTS.md` including `GOOS=js GOARCH=wasm go build ./pkg/smc/`.
- `build-*` targets build the packages `./cmd/agent` (not `main.go`), and
  `build-mac` gains an arm64 target. `build-linux` currently tars the whole
  `./bin/...` path; fix that when touching it.
- `cmd/tray` (phase 4) builds with `CGO_ENABLED=1` and natively per OS, so it
  gets its own target and is left out of the cross-compiling `build-*` targets.
- `build-wasm` builds `cmd/agent` today and works. The `service` subcommand
  (phase 2) may stop it building for `js`; at that point either hide the
  service code behind a `!js` build constraint or drop the target, and keep
  `GOOS=js GOARCH=wasm go build ./pkg/smc/` as the check.

### Markdown files

| File | What changes |
| :--- | :----------- |
| `README.md` | **Quick start:** run with `--config`, command is `go run ./cmd/agent`. **The bundled page:** now read-only, links to `/settings`. **Connect a client:** WebSocket first (it is the default), then socket.io with the `transports` setting; document `?token=` and `Authorization: Bearer`. **Configuration** and **Runtime options:** rewritten around `config.toml` (locations, every key, the loading rules, that the UI rewrites the file and drops hand-written comments, that hand edits need a restart); `set-options`, `set-reader`, `remote_control` and `SMC_ALLOW_REMOTE_OPTIONS` removed. **Architecture:** the two binaries, the service and the settings API rules, including the reverse-proxy warning. **Run as a service:** `service install` replaces the hand-written systemd unit with `Environment=` lines; keep a short manual-unit and PM2 section only if still supported, pointing at the config file. **Upgrading from v2:** the `SMC_*` to config key table. Table of contents updated to match |
| `AGENTS.md` | See below |
| `CHANGELOG.md` | The v3.0.0 entry: Added, Changed and Removed, the `SMC_*` to config key table, and a first paragraph that names each breaking change (env no longer read, default listen is loopback, default transport is `ws`, `set-options`/`set-reader`/`remote_control` gone, `pkg/util` env helpers gone) |
| `docs/plan/` | This file is marked done phase by phase and kept as the record |

`AGENTS.md` needs these changes, most in PR 3 and PR 6:

- **Setup commands:** `go run ./cmd/agent` instead of `./cmd/agent/main.go`,
  mention `--config`, add `go test -race ./...`, and refer to `make check`.
- **Project layout:** add `pkg/config`, `pkg/server/web`, `docs/plan`, and
  `cmd/tray` once it exists; `pkg/util` now holds only `GetResponseCommand`.
- **Architecture constraints:** only `cmd/*` reads the config file, and
  `pkg/smc` keeps receiving plain `Options`; cgo and GUI dependencies stay in
  `cmd/tray` so the agent stays cross-compilable.
- **Code style / Security:** replace "configured entirely through `SMC_*`
  environment variables" with the config file. The file can hold the socket
  token, so it is mode `0600` and a real `config.toml` is never committed.
- **Testing instructions:** config tests are table-driven and need no reader;
  `pkg/server` tests run under `-race`.
- **PR & commit conventions:** describe the integration-branch convention used
  for `v3` (PRs target it, it merges to `main` once), and fix the contradiction
  between "never push to `main` directly" and the release workflow, which pushes
  `main` and the tag. State which one wins (the release workflow, for a release
  commit only).
- **Release workflow:** the paragraph that says the current `[Unreleased]` work
  "ships as v2.0.0" is stale since v2.0.0 and v2.0.1 are out; update the MAJOR
  example, and change the MINOR example from "a new `SMC_*` option" to "a new
  config key".

## Open decisions

1. **Decided:** the socket token is `?token=` for browsers plus
   `Authorization: Bearer` for other clients (decision 10).
2. **Decided:** `remote_control` is removed outright (decision 11). The risk is
   a third-party client that treats a missing field as "allowed" and then
   offers switches the agent rejects; the changelog must call it out.
3. **Decided:** `allowed_origins` defaults to `["*"]`. Revisit once the settings
   page can list the origins that have actually connected.
4. **Linux service user and PC/SC.** Some distributions restrict pcsc-lite to
   active sessions through a polkit rule, which can lock out a service user.
   Test on Debian/Ubuntu before fixing the unit and package contents.
5. **Dropped:** the `kardianos/service` per-user mode no longer matters, since
   there is no per-user agent (decision 12). The service mode is still worth a
   test on each OS before phase 2.
6. **Browsers and `ws://localhost` from an `https://` page.** Chrome and Firefox
   exempt loopback; Safari blocks it. Firefox handled WebSocket separately from
   other mixed content. Test every browser the deployments use.
7. **Chrome Local Network Access.** Public pages reaching loopback or a private
   IP need a per-site permission, and WebSocket is covered in recent releases.
   Kiosks need the policy set in advance. The version numbers came from
   secondary sources and should be checked against Chrome's documentation.
8. **Decided:** the ws race fix shipped from `main` as **v2.0.1**
   (2026-10-01) ahead of the v3 work: it is a crash bug in the released
   v2.0.0 and independent of the rest, so affected users do not wait for the
   whole change. Changelog versioned and committed, annotated tag on the
   changelog commit, tag and `main` pushed, GitHub release cut from the
   changelog section, and the fix plus its race test verified on `v3`, which
   branched from `main` after the release.

## Sources

- Ollama configuration is environment based, with `~/.ollama/server.json` only
  for cloud: <https://docs.ollama.com/faq>
- `fyne-io/systray`: <https://github.com/fyne-io/systray>
- `kardianos/service`: <https://github.com/kardianos/service>
- Local Network Access: <https://developer.chrome.com/blog/local-network-access>
- Mixed content and localhost in Firefox:
  <https://bugzilla.mozilla.org/show_bug.cgi?id=903966>
- Let's Encrypt IP certificates (short-lived profile):
  <https://letsencrypt.org/2026/03/11/shorter-certs-certbot>
- `--ignore-certificate-errors-spki-list`:
  <https://codereview.chromium.org/2753123002>
- `BurntSushi/toml`: <https://github.com/BurntSushi/toml>

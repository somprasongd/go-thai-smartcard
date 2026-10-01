# Plan: config file, service, settings UI and tray (v3)

Status: proposal, nothing here is implemented yet. Written 2026-10-01 against
`main` at v2.0.0.

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
| 13 | No `migrate` subcommand. The upgrade path is a changelog table mapping each `SMC_*` variable to its config key, plus a startup log that prints the equivalent TOML for any stale `SMC_*` it finds | A service's environment lives in its unit file or plist, not in the shell an administrator would run `migrate` from, so the subcommand would not see the values in use |
| 14 | A save from `/settings` or the tray refuses to overwrite a config file that changed on disk since it was loaded | Otherwise a hand edit made after the agent started is lost silently by the next save |

## Findings in the current code

- `pkg/server/server.go` binds `:port`, i.e. all interfaces, and both transports
  accept any origin (`CheckOrigin` returns true in `socketio.go` and
  `websocket.go`). Any page the user opens can connect to `ws://localhost:9898`
  and receive the card data. This is true even with `listen = 127.0.0.1`, so
  `allowed_origins` matters on loopback too (see
  [Open decisions](#open-decisions)).
- `pkg/server/websocket.go`: `subscriber.clients` is a plain map written by each
  connection's handler goroutine and iterated by the broadcast goroutine, with
  no lock. That is a data race and can crash the process with "concurrent map
  iteration and map write". `upgrader.CheckOrigin` is also reassigned on every
  request. Fix this in phase 1, since transports become configurable.
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
- A save also compares the file's mtime with the one recorded when it was
  loaded. If it differs, the save is refused with "the file changed on disk,
  reload before saving", so a hand edit made after the agent started is not
  overwritten by the agent's in-memory copy (decision 14).
- If a `SMC_*` variable is present at start, log a warning that it is no longer
  read, and print the TOML that matches it, e.g. `SMC_SHOW_NHSO=true` becomes
  `read_nhso = true` under `[card]`, for the administrator to paste. Do not
  honour the variable. The changelog carries the full mapping as a table
  (decision 13).
- Hand edits need a restart (`service restart`). No file watcher at first.
- Changed from a UI: `card.*` apply on the next card insert; `server.*` and
  `tls.*` restart the listener.

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
- `GET/PUT /api/settings` reads and writes the config file and applies changes.
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
- The settings page generates a random 128-bit token the first time "expose to
  network" is turned on and shows it once. The config file holds it, so the file
  is mode `0600`.
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
  read image / laser ID / NHSO toggles, Start at login, Quit.
- Library: [`fyne-io/systray`](https://github.com/fyne-io/systray) (requires
  cgo; on Linux it uses the StatusNotifier DBus interface).
- Linux: GNOME needs the AppIndicator extension to show the icon. At start the
  tray checks for `org.kde.StatusNotifierWatcher`; if it is absent it sends a
  notification linking to `/settings` instead of failing silently.
- macOS: the tray must live in a signed, notarized `.app`.
- Windows: build with `-ldflags -H=windowsgui` so no console opens.

#### Start at login

"Start at login" belongs to the tray, a GUI program in the user's session, so it
is unrelated to the service and `kardianos/service` has no part in it. The
library has a `UserService` setting but nothing for run-at-login. The tray
implements the toggle itself on each platform, none of which needs
administrator rights:

| Platform | On | Off |
| :------- | :- | :-- |
| Windows | write the tray's path under `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` (a service cannot show a tray icon, so this is the only way to get one at login) | delete the value |
| macOS | a LaunchAgent in `~/Library/LaunchAgents/` that runs the tray, or a login item registered from the `.app` (`SMAppService` on recent macOS; to be checked against the minimum macOS version supported) | unload and remove it |
| Linux | the package installs `/etc/xdg/autostart/thai-smartcard-tray.desktop` as the default; a user turning it off writes `~/.config/autostart/thai-smartcard-tray.desktop` with `Hidden=true`, and turning it back on removes that override | |

A small library for XDG autostart, LaunchAgents and the Windows Run key may
exist (`emersion/go-autostart` was seen in passing) but has not been checked;
decide whether to depend on one when writing phase 4.

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
| 4 | Tray (macOS, Windows, Linux), tray packages, installers that install and start the agent service, start-at-login | v3.3.0 |
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
| 1 | Fix the ws subscriber race in `pkg/server/websocket.go` (lock around `clients`, set `CheckOrigin` once). Independent of everything below, so it can ship from `main` as v2.0.1 first (open decision 8) | no |
| 2 | `pkg/config`: schema, defaults, strict load, templated write with the banner, mtime check, tests (table-driven, no reader needed). Not wired into the agent yet | no |
| 3 | Wire the config into `cmd/agent`: `--config`, `listen`, `transports`, the stale-`SMC_*` warning with its TOML snippet; delete `pkg/util/env.go` and the `Makefile` variables | no (listening address and default transport change) |
| 4 | `allowed_origins` and token middleware in front of `/ws` and `/socket.io/`, with tests for each rule | no (new rejections) |
| 5 | `/api/info`, `/api/settings`, `/settings`, the `web/` split, read-only test page; remove `set-options`, `set-reader` and `remote_control`. These go together because removing the toggles without `/settings` leaves nothing to replace them | **yes** |
| 6 | README rewrite (connect over ws first, the stale-comment warning, the proxy warning), CHANGELOG with the upgrade note and the `SMC_*` → config key table | docs |

### Phase 1 checklist

- [ ] PR 1: ws race fixed, with a test run under `go test -race`
- [ ] PR 2: `pkg/config` loader and writer, unknown-key and bad-value cases, the
      mtime refusal, the banner
- [ ] PR 3: agent uses the config only; stale `SMC_*` prints a TOML snippet
- [ ] PR 4: origin and token checks; tests for each disabled transport
- [ ] PR 5: `/settings` with the pre-save comment warning; the five route rules
      tested
- [ ] PR 6: CHANGELOG table uses `SMC_AGENT_PORT`, not `SMC_PORT`
- [ ] `go build ./... && go test ./... && go vet ./... && test -z "$(gofmt -l .)"`
      and `GOOS=js GOARCH=wasm go build ./pkg/smc/` on `v3` before the merge to
      `main`

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
8. **Ship the ws race fix as v2.0.1 first?** It is a crash bug in the released
   v2.0.0 and independent of the rest. Releasing it from `main` ahead of v3
   means affected users do not wait for the whole change. It adds one release to
   cut by hand under the `AGENTS.md` workflow.

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

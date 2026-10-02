# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- The Windows uninstaller tried to `net stop` a tray service that does not
  exist; it now force-closes the tray process.

### Changed

- `service status` prints a readable state (`running` / `stopped`) instead of
  the underlying library's raw number.

## [4.0.1] - 2026-10-02

A tray-only UI change: the icon stands alone in the macOS menu bar, and the
read image / laser ID / NHSO switches moved out of the menu — what the agent
reads is configuration with one source of truth, changed in `/settings`.

### Changed

- The tray icon stands alone in the menu bar on macOS — the "Thai Smartcard"
  title next to it is gone (hovering still names the app).
- The read image / laser ID / NHSO switches are removed from the tray menu:
  what the agent reads is configuration with one source of truth, changed in
  `/settings`. The tray menu is now the status line, the two page shortcuts,
  the expose-to-network toggle and quit.

## [4.0.0] - 2026-10-02

Removing a protocol action is a breaking change (the versioning policy calls
it MAJOR): **the `smc-options` broadcast and the `get-options` action are
gone.** What the agent reads is configuration with one source of truth —
`config.toml`, read through `GET /api/settings` — and the card sockets no
longer carry a derived copy of it. A client that showed the reading options
reads `/api/settings` instead. The remaining control actions are `get-status`,
`refresh-readers` and `read-now`.

### Removed

- **Breaking:** the `smc-options` broadcast and the `get-options` action. What
  the agent reads is configuration with one source of truth — `config.toml`,
  read through `GET /api/settings` — and the card sockets no longer carry a
  derived copy of it. A client that showed the reading options reads
  `/api/settings` instead; the bundled page's "card data to read" display is
  gone and its top bar links to `/settings`. The remaining control actions are
  `get-status`, `refresh-readers` and `read-now`; anything else is answered
  with an `smc-error` naming the unknown action.

## [3.0.1] - 2026-10-02

### Fixed

- The address parser dropped the soi from every address that carried both a
  moo and a soi: it looked for both labels in the moo's slot, and the soi then
  leaked into the street. The card gives the soi a slot of its own; slots are
  now identified by their label rather than by their position, and the trailing
  padding of the raw field no longer reaches the parsed values
  ([#7](https://github.com/somprasongd/go-thai-smartcard/issues/7)).
- `/settings` and `/api/*` were not served when the agent was started without
  `--config`: the resolved config path never reached the listener, so the
  routes were not registered and `/settings` fell through to the bundled test
  page. The bare invocation is the default one, so this hit every
  `go run ./cmd/agent` and `agent run`.
- A card insert could fail with a PC/SC sharing violation — losing the
  exclusive-access race against the just-released previous session on a fast
  re-insert, or against another process that seizes inserted cards (on macOS,
  CryptoTokenKit). The agent now recognises the violation as a busy reader
  and retries the connect with a short backoff before giving up; the new
  `transport.ErrCardBusy` sentinel is how backends report it.

### Changed

- Building now requires Go 1.27 or newer (the `go` directive was 1.18 when the
  v3 work started; `golang.org/x/sys` — needed for the Windows service-user
  check — already raised the floor to 1.26, and this makes the documented
  minimum match reality). README's browser-transport issue reference fixed
  (#5, not #10).

## [3.0.0] - 2026-10-02

This release is breaking. In one sentence each: **environment variables are not
read any more** — configuration comes from `config.toml`; **the agent binds
loopback by default** (`listen = "127.0.0.1"`) where v2 bound every interface;
**the default transport is `ws`** and socket.io clients stop working until
`transports = ["ws", "socketio"]` is set; **the card socket is read-only** —
`set-options`, `set-reader` and the `remote_control` field are removed, and
settings change through the new `/settings` page or the API; and **the
`pkg/util` env helpers are removed** (`GetEnv`, `GetEnvInt`, `GetEnvBool`).
See [Upgrading from v2](README.md#upgrading-from-v2) in the README for the full
`SMC_*` → config key table.

### Added

- `config.toml`, the single configuration file (see the README for locations
  and every key). Loading is strict: an unknown key or a bad value stops the
  agent and names the file and line, a missing file is written with the
  defaults, and a file that exposes the agent without a socket token is
  refused.
- `/settings`, a self-contained settings page (plain HTML, no CDN), and
  `GET/PUT /api/settings` behind it. A save applies `[card]` on the next card
  insert and restarts the listener on `[server]`/`[tls]` changes. Saves carry
  the file's fingerprint — a file that changed on disk since it was served is
  a `409`, so a hand edit is never silently overwritten. The socket token is
  generated by the agent, shown once in the save response, and never returned
  again (`token_set` says whether one exists).
- `GET /api/info` (`{version, transports, tls}`), which the bundled page asks
  before connecting: a switched-off transport shows as disabled and its
  socket.io client script is never fetched.
- The settings routes are loopback only and guarded: no CORS headers, a local
  `Host` requirement that blocks DNS rebinding, own-origin only, a fixed
  custom header (`X-SMC-Settings: 1`) on writes, and a refusal of
  `X-Forwarded-For`/`Forwarded` so a same-host proxy cannot turn the internet
  into loopback.
- `allowed_origins` and the socket token on the card sockets. One middleware
  in front of `/ws` and `/socket.io/` checks the origin list and the token
  (`?token=` for browsers, `Authorization: Bearer` for other clients, header
  wins) before the upgrade or handshake; a bad token is a `401` that never
  sees an event. The comparison is constant-time.
- `--config <path>` and `--version` flags to the agent. Without `--config` the
  service config directory is used.
- `config.Server.Enabled`, `config.OriginAllowed`, `config.IsLoopbackListen`
  and `config.RandomToken` for hosts of the config package.
- A stale `SMC_*` environment variable logs a warning that names the TOML
  which replaces it, for pasting into `config.toml`.
- Tests: `pkg/config` (loader, strict validation, the templated writer, the
  fingerprint and its stale-save refusal, the env warnings — table-driven, no
  reader needed), and `pkg/server` settings/auth tests covering each route
  rule, the `version` round-trip, and token generation.
- `thai-smartcard-tray` (`cmd/tray`), an optional tray app for macOS, Windows
  and Linux: reader and card state, shortcuts to the test and settings pages,
  the expose-to-network toggle, and the read image / laser ID / NHSO switches.
  It is a thin client of the agent's `/api` — it never touches the config file
  and never starts the agent; when it cannot reach the agent it says so with
  the command to start it and keeps polling. On GNOME without the AppIndicator
  extension it sends a notification pointing at `/settings` instead of failing
  silently. The tray needs cgo and is its own binary, so the agent stays
  cross-compilable.
- Installers for all three platforms, built by the packaging workflow:
  a Windows Inno Setup installer (installs and starts the service, registers
  the tray at login machine-wide under `HKLM\…\Run`, which each user can turn
  off in Settings > Apps > Startup), a macOS `.pkg` built with `pkgbuild`
  (installs and starts the service, puts the tray in `/Applications`, and
  registers it at login via a LaunchAgent; signed and notarized when the
  Apple Developer secrets are configured), and a Linux tray package that
  depends on the agent's and ships
  `/etc/xdg/autostart/thai-smartcard-tray.desktop`.
- TLS `files` mode (`[tls]` in `config.toml`): the operator supplies
  `cert_file` and `key_file`, the agent serves HTTPS on `tls.port` and reloads
  the files when their mtime changes, so a renewed certificate needs no
  restart. With TLS on, the plain HTTP listener is forced to loopback. A bad
  certificate stops the listener restart rather than leaving the agent
  claiming `https` it cannot serve; `/api/info` reports whether TLS is on.
- `thai-smartcard-agent service install|uninstall|start|stop|restart|status`
  ([kardianos/service](https://github.com/kardianos/service)), which registers
  the agent as a system service on Windows, Linux and macOS. A bare invocation
  runs in the foreground at a terminal and under the service manager otherwise;
  `run` is the spelled-out foreground form. The installed service points at the
  config file in the service location.
- `.deb` and `.rpm` for the agent, built from one nfpm config by the packaging
  workflow when a release tag is pushed (one job, `ubuntu-latest`; artifacts
  attach to the hand-cut release). The package installs and starts the service,
  and carries the systemd unit — which sets `After=pcscd.service`, something
  `service install` cannot express on Linux — the default config, and the
  polkit rule below.
- The service runs as the dedicated `thai-smartcard` system user, not root,
  with a polkit rule (`/etc/polkit-1/rules.d/50-thai-smartcard.pcscd.rules`)
  granting it pcsc-lite's `org.debian.pcsc-lite.access_pcsc` and
  `org.debian.pcsc-lite.access_card` actions. Upstream pcsc-lite enables polkit
  by default and denies any process without an active local session, which is
  exactly what a system service is; the rule is inert where pcscd has no
  polkit, so one package works everywhere.

### Changed

- **Breaking:** the agent binds `127.0.0.1` by default. v2 bound every
  interface while logging "localhost"; exposing the agent is now an explicit
  `listen = "0.0.0.0"`, and the strict loader requires a socket token in that
  state.
- **Breaking:** the default transport is `ws`. socket.io is opt-in with
  `transports = ["ws", "socketio"]`; a disabled transport is never
  constructed.
- The bundled page is read-only: the applet toggles and the reader picker are
  displays that link to `/settings`. Refreshing readers and reading an
  inserted card again stay. The display presets (kiosk, dev, counter) are
  client-side and unchanged. The page moved to `pkg/server/web/`.
- The README documents a browser support matrix and Chrome's Local Network
  Access paths: serve the kiosk UI from the agent, or set the
  `LocalNetworkAccessAllowedForUrls` policy for an external `https` app —
  dismissing Chrome's prompt three times blocks the site permanently.
- `Makefile`: no `SMC_*` exports, `dev` runs the package with
  `config.dev.toml`, `example` runs the package, `build-*` targets build
  `./cmd/agent` as a package, `build-mac` gains an arm64 target, and new
  `test`, `vet`, `fmt-check` and `check` targets wrap the local gate.
- `smc.DaemonConfig` gained `Reader`, which seeds the reader a configured
  `[card] reader` watches from the first resolve.
- The settings API applies options through a callback rather than importing
  the card logic: only `cmd/*` reads the config file.
- When pcscd refuses the connection the agent names
  `SCARD_W_SECURITY_VIOLATION` and points at the polkit rule instead of
  failing generically.

### Removed

- **Breaking:** the socket write actions `set-options` and `set-reader`. They
  are answered with the existing `unknown action` error. The bundled page's
  toggles moved to `/settings`; a third-party client that offered them should
  point its users at `/settings` too.
- **Breaking:** the `remote_control` field of `smc-options` and `smc-status`.
  It only reported whether `set-options`/`set-reader` were accepted; both are
  gone. A client that read a missing field as "allowed" must now assume the
  answer is always "no".
- **Breaking:** `util.GetEnv`, `GetEnvInt` and `GetEnvBool`
  (`pkg/util/env.go`), and every `SMC_*` variable. Nothing in the agent reads
  the environment any more. See the README's upgrade table for the mapping;
  `SMC_ALLOW_REMOTE_OPTIONS` has no replacement.
- The permissive CORS behaviour of the card sockets as the *only* guard:
  origins are now checked against `allowed_origins` (default `["*"]`, which
  keeps the main use working — the settings page warns about it).

## [2.0.1] - 2026-10-01

### Fixed

- A data race in the WebSocket transport. The set of connected clients was a
  plain map that each connection's goroutine wrote while the broadcast goroutine
  iterated it, so a page opening or closing as a card event went out could
  abort the agent with "concurrent map iteration and map write". Access is now
  guarded by a mutex, and the handler no longer reassigns the shared
  `CheckOrigin` on every request.

## [2.0.0] - 2026-10-01

The transport split below is a breaking change for anyone importing
`pkg/util` or building `cmd/wasm`.

### Added

- `pkg/transport`, a narrow `Transport`/`Card`/`Status` interface that the card
  logic in `pkg/smc` talks to, so PC/SC is one implementation rather than a
  hard dependency. Callers pass plain reader names and no backend type leaks
  through the interface.
- `pkg/transport/pcsc`, the PC/SC implementation, behind a `!js` build
  constraint.
- `transport.FakeCard` and trace replay (`pkg/transport/trace.go`), which
  replay a recorded card session so the card logic can be tested without a
  reader.
- `cmd/record`, which captures a real card session to a trace file and is the
  source of those traces. `cmd/record -list` reports the readers PC/SC can see,
  which is the first check when a capture fails.
- `SmartCard.Close()`, `SmartCard.StartDaemonCtx(ctx, ...)`,
  `smc.NewSmartCardWith(t)` and `smc.NewTransport()`.
- A runtime options control channel. A connected client can ask which applets
  the agent is reading (`{"action":"get-options"}`) or change them
  (`{"action":"set-options","options":{…}}`) over socket.io or the raw
  WebSocket, and the next card insert uses the new set without a restart. The
  agent answers with a new `smc-options` broadcast carrying `remote_control`,
  so a client can tell whether its toggles are wired up. Gated by
  `SMC_ALLOW_REMOTE_OPTIONS`, which defaults to on and logs a startup warning:
  the page is served with permissive CORS, so on a shared network anything that
  can reach the port can both reconfigure and read the card. Set it to false to
  pin the options to the `SMC_SHOW_*` variables.
- `smc.OptionsStore` and `SmartCard.StartDaemonCtxWith`, which hold the options
  a running daemon re-reads per card. `StartDaemonCtx` still accepts a
  `*Options` and is unchanged for existing callers.
- `model.Options` and `model.Command`, so `pkg/server` can carry a command
  without importing `pkg/smc`.
- `ServerConfig.Command`, and `NewSocketIO`/`NewWS` now take the command
  channel. A nil channel leaves the control channel off.
- Tests: `pkg/smc/smc_test.go`, `pkg/model/personal_test.go`,
  `pkg/util/smcard_test.go`, `pkg/transport/trace_test.go`,
  `pkg/transport/pcsc/pcsc_test.go`, `pkg/smc/options_store_test.go`,
  `pkg/server/command_test.go` and `cmd/agent/main_test.go`.

- Reader control and re-reads. `get-status`, `set-reader`, `refresh-readers`
  and `read-now` join `get-options` and `set-options` on the control channel.
  A client can narrow the agent to one reader, re-list readers plugged in
  later, and read a card that is already inserted without taking it out. The
  agent answers with a new `smc-status` broadcast carrying the readers, the
  selection, what it is doing (`waiting`, `reading`, `card-present`) and
  `remote_control`, so a client can drive its switches from it.
  `model.Data` gains `reader`, which is how a client tells two cards apart when
  several readers are attached.
- `SmartCard.StartDaemonWith(ctx, DaemonConfig)`, which runs a read loop that
  can be steered. `StartDaemonCtxWith` still takes a fixed options store and is
  unchanged for callers that need no control channel.
- Tests: `pkg/smc/daemon_test.go`, covering a removal that must not be reported,
  a re-read of a card that stays inserted, a re-read request with no card, the
  status broadcast, narrowing the watch, a reader attached later, and a
  selection naming a reader that is not there.

### Changed

- `Transport.WaitCardPresent` and `WaitCardRemove` now return
  `transport.ErrCardTimeout` when their poll window expires with no card change,
  instead of looping internally forever. PC/SC's status call takes no context, so
  a wait could not be cancelled cleanly; bounding it is what lets the read loop
  notice a client's request between windows. A transport that loops internally
  now also leaves the daemon unable to see a reader plugged in later.
- Fixed a multi-reader bug: the loop waited for a removal across *every* reader,
  so a reader that was already empty answered "empty" straight away. The client
  was told a card had been removed, the session was closed, and the loop then
  sat waiting for a card that was still inserted. It now waits only on the
  reader it actually read from.
- The read loop re-lists its readers between idle poll windows, so a reader
  attached after startup appears without a restart, and a selection naming a
  reader that is gone falls back to watching all of them.
- The command channel is buffered. A page asks for the options and the status
  back to back on connect; on an unbuffered channel the server, which drops a
  command rather than stall a connection, lost the second one.
- The read loop logs a line per state change rather than per poll window, which
  would have written a line every couple of seconds for as long as it runs.
- The bundled page (`pkg/server/index.html`) is rebuilt. It shows every field
  the card returns rather than the name, address and photo alone, adds a table
  view listing every JSON key for debugging, a raw payload panel and a unified
  event log, and separates "not connected to the agent" from "waiting for a
  card" in its empty state. Three presets — kiosk, dev and counter — with Thai
  and English labels. Masking the ID number and blurring the portrait are
  separate switches. Card values are inserted with `textContent` rather than
  `innerHTML`, and a `Content-Security-Policy` is set.
- Fixed the page rendering the portrait as `image/png`. The card stores a JPEG
  (a recorded trace decodes to `FF D8 FF E0 … 4A 46 49 46`).
- Fixed the page's WebSocket handler splitting each frame on newlines. The
  server sends one JSON object per frame, `{event, payload}`.
- The page now reports socket.io as unavailable and keeps working over the
  WebSocket when the socket.io client cannot be fetched from its CDN, rather
  than failing silently on a host without internet access.
- `pkg/smc` now holds only card logic — applet selection, the two step
  command/GET RESPONSE exchange, TIS-620 decoding and parsing — with the
  backend behind `pkg/transport`. The card logic builds for `js/wasm` again;
  a browser build supplies its own transport.
- `NewSmartCard()` resolves PC/SC in one place and owns the resulting
  transport. `cmd/agent` closes it on exit, so the card and the PC/SC context
  are released rather than abandoned.
- `cmd/agent` handles `SIGINT` and `SIGTERM` through `signal.NotifyContext` and
  exits the read loop on shutdown instead of only logging the signal. The retry
  loop also backs off when the daemon returns without an error, instead of
  spinning.
- README: documented the transport split, per-platform reader requirements
  (Linux `libpcsclite-dev`/`pcscd`, the Windows Identiv installer, and the
  macOS IFD CCID driver over Apple's own) and the recorded-trace test workflow.

### Fixed

- `model.NewNameFromRaw` and `model.NewAddressFromRaw` no longer panic with an
  index out of range when a raw field comes back truncated or empty. Short
  input now yields an empty `Name`/`Address` rather than taking the whole read
  down with it.

### Removed

- `cmd/wasm`, a stub that panicked on start. The Makefile's `build-wasm`
  target already pointed at `cmd/agent`, so nothing referenced it.
- The PC/SC helpers in `pkg/util` that exposed `*scard` types:
  `EstablishContext`, `ReleaseContext`, `ListReaders`, `InitReaderStates`,
  `WaitUntilCardPresent`, `WaitUntilCardRemove`, `ConnectCard`,
  `DisconnectCard`, `ReadData`, `ReadDataThai` and `ReadLaserData`. They are
  replaced by `pkg/transport`. `util.GetResponseCommand` is the only one of
  the three remaining, as it has no backend type in its signature.

## [1.3.1] - 2023-10-11

### Changed

- The bundled example page now renders the cardholder's name, address and
  photo.

### Documentation

- README updates.

## [1.3.0] - 2023-02-03

### Added

- Laser ID reading, enabled with `SMC_SHOW_LASER`.

### Changed

- Changed the response format.
- Updated the Makefile build targets.

## [1.2.1] - 2022-08-10

### Fixed

- Check `card.Status` before `card.Transmit`, so a card that was removed
  mid-read no longer fails the transmit.
- Recover while reading a card, handling the "The smart card has been
  removed" event.
- Removed unused code.

## [1.2.0] - 2022-08-08

### Added

- Retry the daemon when it errors, instead of exiting.

## [1.1.0] - 2022-08-05

### Fixed

- Handle errors when starting the daemon process, which previously crashed the
  process on a reader or PC/SC failure.

### Changed

- Refactored code.
- Updated README.

## [1.0.0] - 2022-07-11

First release.

### Added

- Read the CID and Thai name from a Thai ID card, moved under `pkg/`.
- socket.io and WebSocket servers that broadcast the read data, the face
  image, and insert/remove/error events to connected clients.
- Configuration via environment variables, with `SMC_PORT` renamed to
  `SMC_AGENT_PORT`.
- Wait until a smart card reader is available instead of failing at startup,
  and a fix for waiting on readers in macOS.
- PM2 and systemd run instructions in the README.

[unreleased]: https://github.com/somprasongd/go-thai-smartcard/compare/v4.0.1...HEAD
[4.0.1]: https://github.com/somprasongd/go-thai-smartcard/compare/v4.0.0...v4.0.1
[4.0.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v3.0.1...v4.0.0
[3.0.1]: https://github.com/somprasongd/go-thai-smartcard/compare/v3.0.0...v3.0.1
[3.0.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v2.0.1...v3.0.0
[2.0.1]: https://github.com/somprasongd/go-thai-smartcard/compare/v2.0.0...v2.0.1
[2.0.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.3.1...v2.0.0
[1.3.1]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.2.1...v1.3.0
[1.2.1]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/somprasongd/go-thai-smartcard/releases/tag/v1.0.0

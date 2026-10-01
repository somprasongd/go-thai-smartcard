# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[unreleased]: https://github.com/somprasongd/go-thai-smartcard/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.3.1...v2.0.0
[1.3.1]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.2.1...v1.3.0
[1.2.1]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/somprasongd/go-thai-smartcard/releases/tag/v1.0.0

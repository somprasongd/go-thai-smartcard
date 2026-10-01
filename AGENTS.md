# AGENTS.md

Go agent that reads a Thai national ID card and broadcasts the data (and face
image) to clients over socket.io and WebSockets.

## Setup commands

- Install deps: `go mod download`
- Build:        `go build ./...`
- Test:         `go test ./...`
- Race check:   `go test -race ./pkg/server/` (see Testing instructions)
- Lint:         `go vet ./...` and `gofmt -l .` (no linter config in the repo)
- Run agent:    `go run ./cmd/agent` — add `--config <path>` to choose a config
  file; without it the service config directory is used. `make dev` runs with a
  git-ignored `config.dev.toml`, written with the defaults on first run.
  `go run ./cmd/agent service install|uninstall|start|stop|restart|status`
  manages the system service (`service.go`, hidden behind `!js`)
- Library demo: `go run ./cmd/example`

Go 1.18+ per `go.mod`. `make dev`, `make example`, the `build-*` targets and
`make check` (the whole local gate, including the wasm build) wrap the same
commands; `build-wasm` targets `cmd/agent`.

## Project layout

- `cmd/agent` — the daemon: resolves the transport, reads cards on a loop, broadcasts events
- `cmd/record` — captures a real card session to a trace file, for tests and for checking readers
- `cmd/example` — minimal library usage, doubles as the README's example
- `pkg/config` — the config.toml loader: defaults, strict validation, the templated writer, the fingerprint the settings API round-trips
- `pkg/smc` — card logic only: applet selection, command/GET RESPONSE, TIS-620, parsing
- `pkg/transport` — the `Transport`/`Card`/`Status` interface `pkg/smc` talks to
- `pkg/transport/pcsc` — the PC/SC backend (`//go:build !js`)
- `pkg/apdu` — command APDU constants
- `pkg/model` — response types and raw-field parsers
- `pkg/server` — socket.io, WebSocket and the bundled pages (`pkg/server/web`, embedded)
- `pkg/util` — `GetResponseCommand` and the small byte helpers (hex decode, base64)
- `packaging/` — the nfpm config, systemd unit, polkit rule and install scripts the packages ship
- `docs/plan/` — written plans for larger changes, kept as the record of what was decided
- `testdata/` — trace files, **gitignored**

## Architecture constraints

`pkg/smc` must not import `github.com/ebfe/scard` or anything else backend
specific. All reader access goes through `pkg/transport.Transport`, which hands
back plain reader names and `[]byte` APDUs so a non-PC/SC backend can satisfy it
unchanged. Resolve the backend in one place (`smc.NewTransport()`), not in
callers. `cmd/record` needs the transport itself rather than a `SmartCard`, which
is why it takes the transport path.

Only `cmd/*` reads the config file. `pkg/config` is the loader; everything
below it receives plain values — `pkg/smc` keeps receiving `Options` and a
reader name, `pkg/server` a listen address and a transport list.

The backend is behind build constraints (`transport_default.go` `!js`,
`transport_js.go` `js`, `pcsc.go` `!js`), so `pkg/smc` still builds for wasm:

```sh
GOOS=js GOARCH=wasm go build ./pkg/smc/
```

Adding a `pcsc.go` path to a new platform means adding the matching build-tagged
file, or the whole module stops building there.

## Code style

- `gofmt` clean, no linter configured — keep it that way
- Doc comments on exported identifiers explain *why* and what breaks, not what the
  signature already says
- Names match the card's own vocabulary: `Read`, `StartDaemonCtx`, `Options`
- Card data is TIS-620, not UTF-8. Decode at the boundary, in `pkg/smc`

## Testing instructions

- Unit tests: `go test ./...` (standard library `testing`, no assertion library)
- Run concurrency tests under the race detector: `go test -race ./pkg/server/`.
  `TestWebSocketBroadcastWhileClientsChurn` is written for it, and a plain
  `go test` is not a reliable check on its own
- Config tests (`pkg/config`) are table-driven and need no reader; the settings
  and auth tests in `pkg/server` use `httptest`, no reader either
- **No reader is required.** `pkg/transport.FakeCard` replays a recorded trace,
  and `NewFakeTransport` injects it via `smc.NewSmartCardWith`
- `TestReadFromRecordedTrace` skips itself until `testdata/trace-real.json`
  exists, so a green run does not prove the trace-backed path works
- To capture one, you need a real reader and a physical card:
  `go run ./cmd/record -out testdata/trace-real.json`
- `cmd/record -list` prints the readers PC/SC can see — run it first when a
  capture fails, since a reader PC/SC cannot see will never work
- Replay is strict: every command must match the recorded one at the same
  position, so an intentional APDU change means re-recording the trace
- Add tests alongside the code they cover; keep raw-field parsers table-driven

## PR & commit conventions

- Branch from `main`; never push feature or fix work to it directly. The one
  exception is a release: the versioned-changelog commit and its tag are pushed
  to `main`, as the release workflow below says — that push wins for a release
  commit only. A change too large for one PR uses an integration branch (the
  v3 work uses `v3`, see `docs/plan/`): each PR targets it, and it merges into
  `main` once, so `main` never holds half of a breaking change
- There is no CI yet, so this is the gate to run locally before a PR — or
  `make check`, which wraps it including the wasm build:
  `go build ./... && go test ./... && go vet ./... && test -z "$(gofmt -l .)"`,
  plus `go test -race ./pkg/server/` when you touch the server
- There is no test CI, and the gate for a PR stays local (above). The one
  workflow (`.github/workflows/package.yml`) is packaging only: triggered by a
  pushed release tag, one job per OS on hosted runners, attaching the packages
  to the hand-cut GitHub release. It never cuts a release and never runs tests
- Commit messages in this repo are short, lowercase and imperative, without
  conventional-commit prefixes — e.g. `add get laser id`, `check card.Status
  before card.Transmit`. Match that rather than introducing a new format
- `CHANGELOG.md` follows Keep a Changelog. Add an entry under `[Unreleased]`
  for user-visible or breaking changes, then follow the release workflow below

## Release workflow

Order matters: **changelog → commit → tag → release**. Tag the commit that
already contains the versioned changelog, so the tag always describes the whole
release and never points at a commit whose changelog still reads `[Unreleased]`.
There is no CI, so every step is run by hand and each one has to be checked.

### 1. Pick the version

Semantic Versioning, on the API surface rather than the commit count:

- **MAJOR** — an exported symbol is removed or changed incompatibly, or
  something a deployment relies on changes: a default, a configuration source, a
  protocol action. v2.0.0 deleted the `pkg/util` PC/SC helpers and `cmd/wasm`;
  v3.0.0 stopped reading `SMC_*`, changed the default listen address and the
  default transport, and removed the socket write actions
- **MINOR** — new capability, e.g. a new config key or a new broadcast event
- **PATCH** — bug fixes only

### 2. Version the changelog, then commit it

In `CHANGELOG.md`, replace the `[Unreleased]` heading with
`## [X.Y.Z] - YYYY-MM-DD`, leave `[Unreleased]` empty above it, and add the
compare link at the bottom:

```md
[1.4.0]: https://github.com/somprasongd/go-thai-smartcard/compare/v1.3.1...v1.4.0
```

Then verify and commit, in the repo's own message style:

```sh
go build ./... && go test ./... && go vet ./... && test -z "$(gofmt -l .)"
git add CHANGELOG.md && git commit -m "update changelog for vX.Y.Z"
```

### 3. Tag the release commit

Use an annotated tag with a short imperative message, matching the style of the
existing `v1.1.0`/`v1.2.0` tags:

```sh
git tag -a vX.Y.Z -m "<one-line summary of the release>"
git push origin main && git push origin vX.Y.Z
```

### 4. Publish the release, using the changelog as the body

The changelog section is the release notes. Extracting it keeps the two from
drifting, which is what happened before — older release bodies are free-form
Thai or English text that matches no changelog entry.

```sh
awk '/^## \[X.Y.Z\]/{f=1;next} /^## \[/{f=0} f' CHANGELOG.md > /tmp/notes.md
gh release create vX.Y.Z --title "vX.Y.Z" --notes-file /tmp/notes.md
```

Title it with the bare tag. Earlier releases mix `Version 1.2.1` with a bare
`v1.3.1`; do not add new variants.

### 5. Confirm, and know how to undo

```sh
gh release view vX.Y.Z
```

`v1.2.0` carries a tag but has no GitHub release at all, which is exactly what
step 4 exists to prevent — if a release is created under the wrong tag or with
the wrong notes, fix it rather than tagging around it:

```sh
gh release delete vX.Y.Z --yes && git push --delete origin vX.Y.Z
```

## Security

- **Never commit a trace.** A trace holds a real person's name, address, ID
  number and photograph. `/testdata` is gitignored for this reason; `dist/`
  contains built binaries and is ignored too
- If a trace is ever pushed, deleting it in a later commit is not enough — the
  data is public once pushed and history has to be rewritten
- Keep secrets out of the repo. The agent is configured from one `config.toml`
  (see `pkg/config`); no environment variables are read

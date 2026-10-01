# AGENTS.md

Go agent that reads a Thai national ID card and broadcasts the data (and face
image) to clients over socket.io and WebSockets.

## Setup commands

- Install deps: `go mod download`
- Build:        `go build ./...`
- Test:         `go test ./...`
- Lint:         `go vet ./...` and `gofmt -l .` (no linter config in the repo)
- Run agent:    `go run ./cmd/agent/main.go`
- Library demo: `go run ./cmd/example/main.go`

Go 1.18+ per `go.mod`. `make dev`, `make example` and the `build-*` targets wrap
the same commands; `build-wasm` targets `cmd/agent`.

## Project layout

- `cmd/agent` — the daemon: resolves the transport, reads cards on a loop, broadcasts events
- `cmd/record` — captures a real card session to a trace file, for tests and for checking readers
- `cmd/example` — minimal library usage, doubles as the README's example
- `pkg/smc` — card logic only: applet selection, command/GET RESPONSE, TIS-620, parsing
- `pkg/transport` — the `Transport`/`Card`/`Status` interface `pkg/smc` talks to
- `pkg/transport/pcsc` — the PC/SC backend (`//go:build !js`)
- `pkg/apdu` — command APDU constants
- `pkg/model` — response types and raw-field parsers
- `pkg/server` — socket.io, WebSocket and the bundled example page
- `pkg/util` — env helpers, `GetResponseCommand`
- `testdata/` — trace files, **gitignored**

## Architecture constraints

`pkg/smc` must not import `github.com/ebfe/scard` or anything else backend
specific. All reader access goes through `pkg/transport.Transport`, which hands
back plain reader names and `[]byte` APDUs so a non-PC/SC backend can satisfy it
unchanged. Resolve the backend in one place (`smc.NewTransport()`), not in
callers. `cmd/record` needs the transport itself rather than a `SmartCard`, which
is why it takes the transport path.

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

- Branch from `main`; never push to it directly. There is no CI yet, so
  `go build ./... && go test ./... && go vet ./...` is the gate to run locally
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

- **MAJOR** — an exported symbol is removed or changed incompatibly. The
  current `[Unreleased]` work is one of these: it deletes the `pkg/util` PC/SC
  helpers and `cmd/wasm`, so it ships as **v2.0.0**, not a patch.
- **MINOR** — new capability, e.g. a new `SMC_*` option or a new broadcast event
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
- Keep `.env`-style secrets out of the repo; the agent is configured entirely
  through `SMC_*` environment variables

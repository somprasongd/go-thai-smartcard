[← README](../README.md)

## Architecture

```
thai-smartcard-agent   headless; PC/SC, ws/socket.io, /, /settings, /api/*
thai-smartcard-tray    cgo; thin client of the agent's /api (installed separately)
```

Reading a card is split into two layers:

- `pkg/smc` holds the card logic: selecting applets, the two step
  command/GET RESPONSE exchange, TIS-620 decoding, and parsing the result.
- `pkg/transport` is the interface that logic talks to. PC/SC is one
  implementation of it, in `pkg/transport/pcsc`.

`NewSmartCard()` uses PC/SC. `NewSmartCardWith(t)` takes any transport, which is
how the logic is tested without a reader and how a second backend can be added
without touching the card logic. Tools that need the transport itself rather
than a `SmartCard`, such as `cmd/record`, call `smc.NewTransport()` so the
backend is resolved in one place:

```go
type Transport interface {
	ListReaders() ([]string, error)
	Connect(reader string) (Card, error)
	WaitCardPresent(ctx context.Context, readers []string) (int, error)
	WaitCardRemove(ctx context.Context, readers []string) (int, error)
	Close() error
}
```

Only `cmd/*` reads the config file: `pkg/config` is the loader, and everything
below it receives plain values.

Run the native agent on Linux, macOS or Windows to read cards through PC/SC.
Browser clients receive card data from the agent over WebSocket or socket.io.

## Testing without a reader

`pkg/transport.FakeCard` replays a recorded trace. Record one against real
hardware, then run the card logic against it:

```sh
go run ./cmd/record -out testdata/trace-real.json
go test ./...
```

Traces contain real personal data — a name, an address, an ID number and a
photograph — and `testdata/` is gitignored for that reason. Deleting one in a
later commit is not enough once it has been pushed; history has to be rewritten.
See [testdata/README.md](../testdata/README.md).

Replay is strict: every command must match the recorded one at the same
position, so an intentional APDU change means re-recording the trace.

## Verification

Run `make check` before submitting changes. It builds, tests, vets and checks
formatting, and builds/vets the standalone library example.

```sh
go test -race ./pkg/server/ ./cmd/tray/
node pkg/server/web/settings_test.cjs
node pkg/server/web/index_test.cjs
```

PR and main CI verify Linux, macOS and Windows. Linux/macOS also run race
checks. Tests use synthetic readers; physical-card, installer and reboot
acceptance require separate validation. Release-tag CI builds packages only.

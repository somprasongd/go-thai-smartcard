[← README](../README.md)

## Use as a library

Use Go 1.27 or newer and a native PC/SC reader on Linux, macOS or Windows.
On Linux, install the build dependencies and start PC/SC first:

```sh
sudo apt-get install build-essential pkg-config libpcsclite-dev pcscd
sudo systemctl start pcscd
```

macOS needs Xcode Command Line Tools (`xcode-select --install`); PC/SC is provided
by the OS. Windows uses native bindings and does not require cgo. See
[Reader requirements](readers.md#reader-requirements) for reader/driver setup.

In your own Go project:

```sh
mkdir card-demo
cd card-demo
go mod init example.com/card-demo
go get github.com/somprasongd/go-thai-smartcard/pkg/smc@c49ac0a1a9f226d2138ab13b9f9687cbaa60c1f7
```

The revision is pinned deliberately: the current module path has no major-version
suffix, so `@latest` resolves to the older v1.3.1 API rather than the v5 release.
Go resolves the pinned commit to a pseudo-version. Do not add `/v5` to imports
until the library itself adopts that module path.

Copy [examples/read-card/main.go](../examples/read-card/main.go) into `main.go`,
then run `go run .`. The example imports the public package directly:

```go
import "github.com/somprasongd/go-thai-smartcard/pkg/smc"
```

It checks transport initialization, waits for a card, reads it once, writes JSON
to stdout and closes the transport. Errors go to stderr with a nonzero exit code.
`Read(nil, opts)` watches all attached readers; pass a reader name pointer to
select one. Face-image reading is enabled; NHSO and laser data are opt-in through
`smc.Options`.

To run the standalone example from this repository:

```sh
cd examples/read-card
GOWORK=off go run .
```

Its own `go.mod` pins the published dependency without a local `replace`, so it
also checks that the library can be consumed from another project. `make example`
runs the same demo. A physical reader and card are needed for a successful read.
To keep running and publish instead, use `StartDaemonCtx`, which takes a channel
of `model.Message`.

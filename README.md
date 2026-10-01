# go-thai-smartcard

Reads a Thai national ID card and publishes what it finds to connected clients
over WebSockets and, opt-in, [socket.io](https://socket.io/).

The agent runs in the background, waits for a card, reads it and broadcasts the
result. A test page and a settings page are served from the same port, so you
can look at a card and configure the agent without writing any client code. Use
it as a library if you would rather build your own.

- [Quick start](#quick-start)
- [The bundled page](#the-bundled-page)
- [Settings](#settings)
- [Connect a client](#connect-a-client)
- [Configuration](#configuration)
- [Use as a library](#use-as-a-library)
- [Architecture](#architecture)
- [Reader requirements](#reader-requirements)
- [Testing without a reader](#testing-without-a-reader)
- [Run as a service](#run-as-a-service)
- [Browsers](#browsers)
- [Upgrading from v2](#upgrading-from-v2)
- [Other versions](#other-versions)

## Quick start

Requires [Go](https://go.dev/dl/) 1.18 or newer, and a reader that PC/SC can
see. Check the reader first — nothing else works until it does:

```sh
go run ./cmd/record -list
```

Then run the agent and open the page:

```sh
go mod download
go run ./cmd/agent
```

<http://localhost:9898>

Insert a card. The page fills in. `go run ./cmd/agent` reads
`config.toml` from the service config directory (see
[Configuration](#configuration)); `--config <path>` points at another file, and
`make dev` runs with a git-ignored `config.dev.toml` written with the defaults
on first run. To ship it as a binary:

```sh
go build -o bin/thai-smartcard-agent ./cmd/agent

# Windows
go build -o bin/thai-smartcard-agent.exe ./cmd/agent
```

## The bundled page

`http://localhost:9898` serves a page built for reading a card at a glance. It
shows every field the card returns rather than the name and address alone, and
adds what it can work out from them — age, days until expiry, the gender label,
and whether the ID number passes its checksum — marked apart from what the card
itself says.

Three presets set the display switches together:

| Preset | ID number | Portrait | Screen after removal | Type |
| :----- | :-------- | :------- | :------------------- | :--- |
| **kiosk** | masked | blurred | cleared after a few seconds | normal |
| **dev** | shown | shown | kept | normal, raw payload open |
| **counter** | shown | shown | kept | enlarged |

Masking the number and blurring the portrait are separate switches, because they
are separate decisions: a screen that must not print the ID usually still has to
show the face, and one that blurs the face usually still has to read the number
back. Masking the ID does not hide the photo.

The page is **read-only**: what the agent reads and which reader it watches are
configuration, shown as displays that link to [settings](#settings) rather than
changed here. Refreshing the reader list and reading a card that is already
inserted stay, because neither changes what the agent reads.

Two things to know about it. socket.io's client library is fetched from a CDN
**only when socket.io is enabled** — the page asks `/api/info` first, and shows
a switched-off transport as disabled instead of loading the script. And a
switched-off transport is not an error: the WebSocket carries the data either
way.

## Settings

<http://localhost:9898/settings> is a self-contained page (plain HTML, no CDN,
so it works on a hospital network that cannot reach the internet) for everything
the agent reads and who may connect to it. The tray app, where one is
installed, calls the same API.

What a save does:

- `[card]` — applies on the next card insert, no restart.
- `[server]` and `[tls]` — the listener restarts on the new values;
  connected pages reconnect.
- The file is rewritten from a template. **Comments added by hand are lost on
  the next save.** A hand edit takes effect after a restart.

A save refuses to overwrite a file that changed on disk since it was served:
`GET /api/settings` returns a fingerprint of the file's bytes, `PUT
/api/settings` echoes it back, and a mismatch is a `409` with "the file changed
on disk, reload before saving". Two pages saving at once get the same refusal —
the second is told to reload rather than silently undoing the first.

The token section generates the socket token (see
[Connect a client](#connect-a-client)) and shows it **once**, with a copy
button; the agent holds it in `config.toml` and never sends it again.
"Regenerate token" replaces it, which makes every client using the old one
reconnect with the new.

### The settings API and its rules

`GET /api/info` returns `{version, transports, tls}` and `GET/PUT /api/settings`
read and write the config file. These routes are loopback only, and they are
deliberately **not** reachable through the card sockets' permissive origin
rules:

1. No CORS headers, ever.
2. The `Host` header must be `localhost`, `127.0.0.1` or `[::1]` — which is
   what blocks DNS rebinding.
3. An `Origin` header, when present, must be the agent's own.
4. Writes require the fixed custom header `X-SMC-Settings: 1`, which a
   cross-origin page cannot send without a preflight — and there are no CORS
   headers to answer one.
5. The TCP peer must be on loopback, and a request carrying
   `X-Forwarded-For` or `Forwarded` is refused, so a reverse proxy on the same
   host cannot turn the whole internet into "loopback".

> **Do not put a reverse proxy in front of `/settings` or `/api/*`.** The
> proxy-header refusal is the guard, not a misconfiguration to fix: there is
> deliberately no network-reachable way to change settings. A headless kiosk is
> configured by editing `config.toml` over SSH, or through an SSH tunnel to the
> settings page.

## Connect a client

The agent broadcasts five events. `smc-data` carries the card; the rest say what
happened around it.

| Event | Payload |
| :---- | :------ |
| `smc-data` | the card, see [what a card contains](#what-a-card-contains) |
| `smc-inserted` | `{message}` — a card arrived |
| `smc-removed` | `{message}` — the card was taken out |
| `smc-error` | `{message}` — a read failed, or an action was refused |
| `smc-options` | what the agent is reading |
| `smc-status` | readers, the selected one, and what the agent is doing |

The control channel is **read-only**: `get-options`, `get-status`,
`refresh-readers` and `read-now` are the actions there are. The removed
`set-options` and `set-reader` are answered with an `smc-error` naming the
unknown action, and `remote_control` is gone from `smc-options` and
`smc-status` — a client that read a missing field as "allowed" should now
assume the answer is always "no".

### Via WebSocket (the default)

The raw WebSocket sends one JSON object per frame, shaped `{event, payload}`.
It is the default transport; socket.io is opt-in (see
[Configuration](#configuration)).

```html
<script>
  const conn = new WebSocket('ws://' + location.host + '/ws');

  conn.onopen = function () {
    conn.send(JSON.stringify({ action: 'get-status' }));
  };

  conn.onmessage = function (evt) {
    const msg = JSON.parse(evt.data);
    switch (msg.event) {
      case 'smc-data':
        console.log(msg.payload.personal.name.full_name);
        break;
      case 'smc-error':
        console.error(msg.payload.message);
        break;
      default:
        console.log(msg.event, msg.payload);
    }
  };
</script>
```

### Via socket.io

Enable it first with `transports = ["ws", "socketio"]`, then use the socket.io
**2.x** client (the Go server speaks the v2 protocol):

```html
<script src="https://cdnjs.cloudflare.com/ajax/libs/socket.io/2.2.0/socket.io.js"></script>
<script>
  const socket = io.connect('http://localhost:9898', {
    query: { token: 'THE-TOKEN' }   // when the agent requires one
  });

  socket.on('smc-data', function (data) {
    console.log(data.personal.name.full_name, 'from', data.reader);
  });
</script>
```

### The socket token

Required when the agent listens beyond loopback, optional otherwise. One check
in front of both transports, before the upgrade or handshake: a bad token gets
`401` and never sees an event.

- Browsers pass `?token=<token>` on the socket URL — it is the only form a
  browser can send. socket.io 2.x takes it as the `query` option.
- Other clients may send `Authorization: Bearer <token>` instead, which keeps
  the token out of URLs. If both are present the header wins.
- The comparison is constant-time and the agent never logs request URLs.

The agent generates the token: the first save that turns exposure on makes a
random one and shows it once. Without TLS the token and the card data cross the
LAN in clear text however the token is sent, and a token embedded in a web
app's JavaScript keeps other hosts and other sites out but not the person using
that page.

### What a card contains

`smc-data` has `personal` and, when the agent is reading them, `card` and
`nhso`. The two are omitted rather than empty when their applet is off.

```jsonc
{
  "personal": {
    "cid": "1234567890123",          // 13 digits
    "name":     { "prefix": "…", "first_name": "…", "middle_name": "…",
                  "last_name": "…", "full_name": "…" },
    "name_eng": { /* the same five fields */ },
    "dob": "1994-12-31",             // already converted to Gregorian
    "gender": "1",                   // "1" or "2"; no label is applied
    "card_issuer": "…",
    "issue_date": "2019-05-15",
    "expire_date": "2026-05-15",
    "address": { "house_no": "…", "moo": "…", "soi": "…", "street": "…",
                 "subdistrict": "…", "district": "…", "province": "…",
                 "address": "…" },
    "base64_img": "…"                // JPEG, not PNG
  },
  "card": { "laser_id": "…" },
  "nhso": { "main_inscl": "…", "sub_inscl": "…", "main_hospital": "…",
            "sub_hospital": "…", "paid_type": "…", "issue_date": "…",
            "expire_date": "…", "update_date": "…",
            "change_hospital_amount": "…" },
  "reader": "Identive CLOUD 2700 R"  // which reader it was read from
}
```

Text off the card is TIS-620 and is decoded on the way out, so it arrives as
UTF-8. Dates are converted from the Thai calendar. `reader` is additive: a
client reading `data.personal` works without it.

## Configuration

Everything is configured with one file, `config.toml`. Environment variables
are not read.

| Platform | Path |
| :------- | :--- |
| Linux, service | `/etc/thai-smartcard/config.toml` |
| macOS, service | `/Library/Application Support/ThaiSmartcard/config.toml` |
| Windows, service | `%ProgramData%\ThaiSmartcard\config.toml` |
| `agent run` as a user (development) | `~/.config/thai-smartcard/config.toml` (Linux), `~/Library/Application Support/ThaiSmartcard/config.toml` (macOS), `%AppData%\ThaiSmartcard\config.toml` (Windows) |

`--config <path>` overrides all of that. A missing file is written with the
defaults and comments when the directory allows it.

```toml
[server]
listen = "127.0.0.1"      # "0.0.0.0" exposes the agent to the network
port = 9898
transports = ["ws"]       # "ws", "socketio", or both
allowed_origins = ["*"]   # origins allowed to open a card socket; "*" is any
token = ""                # required when listen is not loopback

[card]
read_face_image = true
read_laser_id = true
read_nhso = false         # treatment entitlement
reader = ""               # empty watches every reader

[tls]
enabled = false
port = 9899
mode = "files"
cert_file = ""            # both required when enabled
key_file = ""
hostnames = []
```

The loading rules worth knowing:

- **Strict.** An unknown key or a bad value stops the agent and names the file
  and line. A service that silently falls back to defaults after a typo is
  worse than one that does not start.
- A file that exposes the agent (`listen` beyond loopback) without a token is
  refused; the settings page generates one.
- Defaults first, then the file — a file that names only some keys is a
  complete configuration.
- Hand edits need a restart (`service restart`); there is no file watcher.
- Saving from the UI rewrites the file from a template, so hand-added comments
  are lost on the next save.
- A stale `SMC_*` environment variable is not read; the agent logs a warning
  with the TOML that replaces it, for pasting into the file.

### TLS

TLS is needed when an `https://` page talks to the agent — on a LAN IP in any
browser, and even on loopback in Safari (see
[Browsers](#browsers)). It is not needed for `http://` pages, which is the
normal kiosk case.

In `files` mode the operator supplies `cert_file` and `key_file`; the agent
serves HTTPS on `tls.port` and reloads the files when their mtime changes, so a
renewed certificate needs no restart. When TLS is on the plain HTTP listener is
forced to loopback — anything that needs the LAN uses `https`.

A real certificate for a LAN **IP** cannot come from a public CA. The usable
routes are a hospital-owned domain with an internal DNS record and a DNS-01
certificate, or the organisation's own PKI. A lone self-signed certificate is
not a solution: a script's `wss://` fails silently and a locked-down kiosk
cannot accept the per-browser warning. The certificate must be trusted by the
machine running the **browser**, not by the agent's machine.

## Use as a library

See [cmd/example/main.go](cmd/example/main.go) for a minimal read. The shape of
it:

```go
reader := smc.NewSmartCard()             // PC/SC
name := "Reader 0"                       // or nil for every reader
data, err := reader.Read(&name, &smc.Options{ShowFaceImage: true})
```

`Read` waits for a card, reads it once and returns. To keep running and publish
instead, use `StartDaemonCtx`, which takes a channel of `model.Message`.

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

Because `pkg/transport/pcsc` is behind a `!js` build constraint and the default
constructor is split across build constraints too, the card logic builds for
`js/wasm`:

```sh
GOOS=js GOARCH=wasm go build ./pkg/smc/
```

A browser build supplies its own transport. Note that WebUSB cannot actually
deliver one: Chrome blocks the Smart Card USB interface class (`0x0B`) from
`navigator.usb.requestDevice()`, in a plain page and in a regular extension
alike; only Google's allowlisted privileged extensions bypass the blocklist.
Web NFC does not help either, since a Thai ID card has a contact chip rather
than a contactless one. Browsers on mobile are narrower still, so an agent
remains the only backend that works everywhere. The interface is what makes it
worth retrying if a browser ever exposes PC/SC properly; see issue
[#10](https://github.com/somprasongd/go-thai-smartcard/issues/10).

## Reader requirements

A card reader has to be visible to PC/SC before anything will read a card. Check
what is visible:

```sh
go run ./cmd/record -list
```

If it prints no readers, the reader is not reachable and installing the
toolchain will not help.

| Platform | What is needed |
| :------- | :------------- |
| **Linux** | `sudo apt install build-essential libpcsclite-dev pcscd`. The CCID driver in `libccid` is normally already present. |
| **Windows** | The reader vendor's driver. For an Identiv uTrust, the `Identiv uTrust Installer` package picks the right driver for the Windows version. |
| **macOS** | Nothing to download. If the reader does not show up or connects fail, see below. |

Recording a trace does not have to happen on the same machine as the agent.
`cmd/record` is cross platform, so it can be built and run wherever the reader
works:

```sh
GOOS=windows go build -o record.exe ./cmd/record
```

### If macOS cannot see the reader

macOS carries **two** CCID drivers:

- `/usr/libexec/SmartCardServices/drivers/ifd-ccid.bundle` — IFD CCID, the open
  source `libccid` driver.
- `/System/Library/CryptoTokenKit/usbsmartcardreaderd.slotd` — Apple's own
  driver, which matches readers by USB interface *class* rather than by vendor
  and product ID.

Most readers work with either. When one does not, switching to IFD CCID usually
fixes it. Both are user space, so do not go looking in
`/System/Library/Extensions`:

```sh
sudo defaults write /Library/Preferences/com.apple.security.smartcard useIFDCCID -bool true
sudo killall usbsmartcardreaderd
```

Then unplug and replug the reader. A reboot is not needed:
`com.apple.usbsmartcardreaderd` is an on-demand launchd daemon whose launch event
fires when a USB interface with `bInterfaceClass = 11` appears, so replugging
starts it again and it picks up the new setting. Verify with
`go run ./cmd/record -list`.

The Identiv uTrust 2700 R is in IFD CCID's table as `0x04E6:0x5810`. If the
IFD CCID version macOS ships is too old for your reader, Thales publishes a
newer CCID installer at
[supportportal.thalesgroup.com KB0027738](https://supportportal.thalesgroup.com/csm?id=kb_article_view&sysparm_article=KB0027738).

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
See [testdata/README.md](testdata/README.md).

Replay is strict: every command must match the recorded one at the same
position, so an intentional APDU change means re-recording the trace.

## Run as a service

The agent is a system service on every platform (Windows Service, systemd,
launchd); a tray app, where one is installed, is only a viewer and never starts
the agent. The `.deb`/`.rpm` do all of this for you — they install the service,
start it, and ship the polkit rule and the default config — and are built by
the packaging workflow from a release tag. By hand:

### Linux

```sh
sudo install -m 0755 ./bin/thai-smartcard-agent /usr/local/bin/thai-smartcard-agent
sudo thai-smartcard-agent service install
sudo thai-smartcard-agent service start
thai-smartcard-agent service status
```

`service install` registers the service with the manager, pointed at the config
file in the service location. It is driven by
[kardianos/service](https://github.com/kardianos/service), which cannot express
`After=pcscd.service` on Linux — the packaged unit file sets that itself, which
is one reason to prefer the `.deb`:

```ini
[Unit]
Description=Thai Smartcard Agent
After=pcscd.service
Wants=pcscd.service

[Service]
Type=simple
User=thai-smartcard
Group=thai-smartcard
ExecStart=/usr/bin/thai-smartcard-agent --config /etc/thai-smartcard/config.toml
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

**pcsc-lite and polkit.** Upstream pcsc-lite has enabled polkit by default since
late 2023, and its default denies any process without an active local session —
exactly what a system service is. The packaged rule
(`/etc/polkit-1/rules.d/50-thai-smartcard.pcscd.rules`) grants the dedicated
`thai-smartcard` service user the two pcsc-lite actions
(`org.debian.pcsc-lite.access_pcsc`, `org.debian.pcsc-lite.access_card`), and is
inert where pcscd has no polkit. If the agent logs
`SCARD_W_SECURITY_VIOLATION`, that rule is what is missing: install it, restart
`pcscd`, and the reader shows up. Hand-installing without the package means
writing that rule by hand, or running the service as root — which works but
widens the agent for no gain.

### Windows and macOS

`service install`, `service start` and `service status` work the same way
through the Windows Service Control Manager and launchd. The service runs with
`--config` pointed at the config file in the service location
(`%ProgramData%\ThaiSmartcard\config.toml` on Windows,
`/Library/Application Support/ThaiSmartcard/config.toml` on macOS).

### PM2

```bash
# Linux and macOS
npm install -g pm2
pm2 start --name smc -- ./bin/thai-smartcard-agent --config /etc/thai-smartcard/config.toml
pm2 startup
pm2 save
```

```bash
# Windows
npm install -g pm2 pm2-windows-startup
pm2-startup install
pm2 start --name smc -- .\bin\thai-smartcard-agent.exe --config %ProgramData%\ThaiSmartcard\config.toml
pm2 save
```

## Browsers

Where a page can reach the agent's socket:

| Browser | Page served by the agent (`http://…`) | External `https://` page → `ws://127.0.0.1` |
| :------ | :------------------------------------ | :------------------------------------------ |
| Chrome, Edge | works | works — loopback is treated as trustworthy |
| Firefox | works | works since Firefox 55 |
| Safari | works | **blocked, by policy** — `wss://` with a trusted certificate is the only path |

The agent-served page is same-origin `http`, which works in every browser; a
Safari kiosk should serve its UI from the agent. This is why the TLS settings
exist even though the normal kiosk case is plain `http`.

Chrome's **Local Network Access** adds one more gate for a *public* `https`
page that reaches the agent on loopback or a private IP: the user sees a
per-site permission prompt, and WebSocket connections are covered (Chrome 147,
rollout from April 2026). Two ways through:

1. Serve the kiosk UI from the agent — local-to-local is exempt.
2. Have IT set the `LocalNetworkAccessAllowedForUrls` policy for the external
   app's origin.

Dismissing Chrome's prompt three times blocks the site **permanently** for that
profile, so a kiosk needs the policy set in advance rather than relying on the
prompt.

## Upgrading from v2

This release is breaking in five ways, in one sentence each:

1. **Environment variables are not read any more.** Copy the values into
   `config.toml` with the table below; a stale variable only earns a warning.
2. **The agent binds loopback by default.** The old behaviour silently served
   the card to the whole network; to expose it again set
   `listen = "0.0.0.0"` — and a token is then required.
3. **The default transport is `ws`.** socket.io clients stop working until
   `transports = ["ws", "socketio"]` is set.
4. **The card socket is read-only.** `set-options`, `set-reader` and
   `remote_control` are gone; settings change through `/settings` or the tray.
5. **The `pkg/util` env helpers are removed** (`GetEnv`, `GetEnvInt`,
   `GetEnvBool`). Code that imported them moves to its own configuration
   source.

The variable names below are the ones the agent actually read. (`SMC_PORT` was
exported by an old Makefile but never read by the agent.)

| v2 variable | v3 config key | Note |
| :---------- | :------------ | :--- |
| `SMC_AGENT_PORT` | `[server] port` | |
| `SMC_SHOW_IMAGE` | `[card] read_face_image` | |
| `SMC_SHOW_LASER` | `[card] read_laser_id` | |
| `SMC_SHOW_NHSO` | `[card] read_nhso` | |
| `SMC_ALLOW_REMOTE_OPTIONS` | — no replacement | What it gated no longer exists: the card socket is read-only, and settings change only through `/settings`. Delete the line from the unit file; there is nothing to set instead. Both `true` and `false` users lose nothing — a `false` install's pinned values now live in `config.toml`, and a `true` install's remote reconfiguration is gone by design |

## Other versions

- [Java](https://github.com/somprasongd/jThaiSmartCard)
- [Nodejs](https://github.com/somprasongd/thai-smartcard-nodejs)

## Donate

สนับสนุนได้ผ่านทาง Promptpay

<img src="https://bit.ly/3gusiz8">

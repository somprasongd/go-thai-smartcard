# go-thai-smartcard

Reads a Thai national ID card and publishes what it finds to connected clients
over [socket.io](https://socket.io/) and
[WebSockets](https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API).

The agent runs in the background, waits for a card, reads it and broadcasts the
result. A page is served from the same port, so you can look at a card without
writing any client code. Use it as a library if you would rather build your own.

- [Quick start](#quick-start)
- [The bundled page](#the-bundled-page)
- [Connect a client](#connect-a-client)
- [Configuration](#configuration)
- [Runtime options](#runtime-options)
- [Use as a library](#use-as-a-library)
- [Architecture](#architecture)
- [Reader requirements](#reader-requirements)
- [Testing without a reader](#testing-without-a-reader)
- [Run as a service](#run-as-a-service)
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
go run ./cmd/agent/main.go
```

<http://localhost:9898>

Insert a card. The page fills in. To ship it as a binary:

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

Three presets set the switches together:

| Preset | ID number | Portrait | Screen after removal | Type |
| :----- | :-------- | :------- | :------------------- | :--- |
| **kiosk** | masked | blurred | cleared after a few seconds | normal |
| **dev** | shown | shown | kept | normal, raw payload open |
| **counter** | shown | shown | kept | enlarged |

Masking the number and blurring the portrait are separate switches, because they
are separate decisions: a screen that must not print the ID usually still has to
show the face, and one that blurs the face usually still has to read the number
back. Masking the ID does not hide the photo.

There is also a reader picker, a refresh button for readers plugged in later, and
a read-again button that is live while a card is sitting in the reader.

Two things to know about it. socket.io's client library is fetched from a CDN,
so on a host that cannot reach it the page reports socket.io as unavailable and
keeps working over the dependency-free WebSocket. And it is served with
permissive CORS, which is what lets a page on another origin connect — see
[the note in Runtime options](#runtime-options) before putting it on a shared
network.

## Connect a client

The agent broadcasts five events. `smc-data` carries the card; the rest say what
happened around it.

| Event | Payload |
| :---- | :------ |
| `smc-data` | the card, see [what a card contains](#what-a-card-contains) |
| `smc-inserted` | `{message}` — a card arrived |
| `smc-removed` | `{message}` — the card was taken out |
| `smc-error` | `{message}` — a read failed, or a command was refused |
| `smc-options` | what the agent is reading, plus `remote_control` |
| `smc-status` | readers, the selected one, and what the agent is doing |

### Via socket.io

socket.io delivers the payload bare, on a named event.

```html
<script src="https://cdnjs.cloudflare.com/ajax/libs/socket.io/2.2.0/socket.io.js"></script>
<script>
  const socket = io.connect('http://localhost:9898');

  socket.on('connect', function () {
    // ask what the agent is reading and which readers it can see
    socket.emit('smc-command', { action: 'get-options' });
    socket.emit('smc-command', { action: 'get-status' });
  });

  socket.on('smc-data', function (data) {
    console.log(data.personal.name.full_name, 'from', data.reader);
  });
  socket.on('smc-options', function (options) {
    console.log('reading', options);
  });
  socket.on('smc-status', function (status) {
    console.log(status.readers, status.state);
  });
  socket.on('smc-inserted', function (msg) {
    console.log(msg.message);
  });
  socket.on('smc-removed', function (msg) {
    console.log(msg.message);
  });
  socket.on('smc-error', function (msg) {
    console.error(msg.message);
  });
</script>
```

### Via WebSocket

The raw WebSocket sends one JSON object per frame, shaped `{event, payload}`.

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

Either transport can also send commands, and both answer on the same broadcast,
so a client only needs the one connection it already has. See
[Runtime options](#runtime-options).

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
  "reader": "Identive CLOUD 2700 R"  // added, see Runtime options
}
```

Text off the card is TIS-620 and is decoded on the way out, so it arrives as
UTF-8. Dates are converted from the Thai calendar. `reader` is additive: a
client reading `data.personal` works without it.

## Configuration

Everything is configured with environment variables.

| ENV | Default | Description |
| :-- | :-----: | :---------- |
| **SMC_AGENT_PORT** | "9898" | Port to serve on. |
| **SMC_SHOW_IMAGE** | "true" | Read the face image. |
| **SMC_SHOW_LASER** | "true" | Read the laser ID. |
| **SMC_SHOW_NHSO** | "false" | Read the social security record. |
| **SMC_ALLOW_REMOTE_OPTIONS** | "true" | Let a client change the above and pick a reader while the agent runs. See [Runtime options](#runtime-options). |

## Runtime options

The applets are chosen at startup, but a connected client can also change them
while the agent runs; the next card insert uses the new set, with no restart.

A client sends any of these:

```json
{ "action": "get-options" }
{ "action": "set-options", "options": { "show_face_image": true, "show_nhso": false, "show_laser": true } }
{ "action": "get-status" }
{ "action": "set-reader", "reader": "Identive CLOUD 2700 R" }
{ "action": "refresh-readers" }
{ "action": "read-now" }
```

The agent answers on the same broadcast as the card events:

```json
{ "event": "smc-options", "payload": { "show_face_image": true, "show_nhso": false, "show_laser": true, "remote_control": true } }
{ "event": "smc-status", "payload": { "readers": ["Identive CLOUD 2700 R"], "selected": "", "state": "waiting", "remote_control": true } }
```

`state` is `waiting` (watching for a card), `reading`, or `card-present` (read,
waiting to be taken out — the state in which `read-now` means something).

`remote_control` reports whether `set-options` and `set-reader` are permitted.
When it is `false` the options are pinned to the `SMC_SHOW_*` variables and a
refused command comes back as `smc-error`. Reading again and refreshing the
reader list stay available, since neither changes what the agent reads.

Over socket.io a command is an event named `smc-command`:

```javascript
socket.emit('smc-command', { action: 'get-status' });
```

Over the raw WebSocket it is the same object as a text frame:

```javascript
conn.send(JSON.stringify({ action: 'get-status' }));
```

> **Turn it off for a shared deployment.** The agent binds every interface and
> serves its page with permissive CORS, so with remote options enabled anything
> on the same network that can reach the port can change what is read — and can
> read back the name, address, ID number and portrait the page shows. Set
> `SMC_ALLOW_REMOTE_OPTIONS=false` to pin the options per process. The agent
> prints a warning on startup while it is enabled. This is already true of the
> broadcasts themselves; the gate only stops a client from reconfiguring.

### Several readers

`set-reader` narrows the agent to one reader; an empty name watches all of them,
which is how it starts. A name that is not attached falls back to watching
everything rather than waiting for a reader that will never answer.

The agent re-lists its readers between idle poll windows, so one plugged in
after startup shows up without a restart. `refresh-readers` asks for the same
thing on demand, which is what the button in the page does.

`read-now` reads a card that is already inserted. That is the case an operator
hits when a card is seated badly or a kiosk slot grips it: without it the agent
waits for the card to be taken out and put back. It answers with `smc-data`, or
with `smc-error` saying no card is inserted if there is nothing to read.

Both take effect within a couple of seconds, bounded by the PC/SC poll window.
Card detection itself is not slowed by that window — the OS wakes the status
call the moment a card changes — only the reaction to a command is.

`data.reader` names the reader each `smc-data` came from. With several readers
attached, `smc-inserted` only says a card arrived, so a client cannot otherwise
tell two cards apart when they arrive close together.

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

Both waits return `transport.ErrCardTimeout` when their poll window expires with
no card change, rather than looping internally forever. The caller is what can
answer a client asking for a different reader or another read, and a wait that
never returns is a wait that never hears the answer. The same windows are where
a reader plugged in later gets noticed.

`WaitCardRemove` should be given only the readers that were read from. A reader
that was already empty answers "empty" immediately, so including one reports a
removal that never happened.

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

### systemd (Linux)

```bash
cd ~
git clone https://github.com/somprasongd/go-thai-smartcard
cd go-thai-smartcard
go build -o ./bin/thai-smartcard-agent ./cmd/agent
```

```bash
nano /lib/systemd/system/thai-smartcard-agent.service
```

```ini
[Unit]
Description=thai-smartcard-agent

[Service]
Environment="SMC_AGENT_PORT=9898"
Environment="SMC_SHOW_IMAGE=true"
Environment="SMC_SHOW_NHSO=false"
Environment="SMC_SHOW_LASER=true"
Environment="SMC_ALLOW_REMOTE_OPTIONS=false"
Type=simple
Restart=always
RestartSec=5s
ExecStart=~/go-thai-smartcard/bin/thai-smartcard-agent

[Install]
WantedBy=multi-user.target
```

```bash
systemctl enable --now thai-smartcard-agent
```

Set `SMC_ALLOW_REMOTE_OPTIONS=false` there unless you want clients on the
network reconfiguring the agent.

### PM2

```bash
# Linux and macOS
npm install -g pm2
pm2 start ./bin/thai-smartcard-agent --name smc
pm2 startup
pm2 save
```

```bash
# Windows
npm install -g pm2 pm2-windows-startup
pm2-startup install
pm2 start .\bin\thai-smartcard-agent.exe --name smc
pm2 save
```

## Other versions

- [Java](https://github.com/somprasongd/jThaiSmartCard)
- [Nodejs](https://github.com/somprasongd/thai-smartcard-nodejs)

## Donate

สนับสนุนได้ผ่านทาง Promptpay

<img src="https://bit.ly/3gusiz8">

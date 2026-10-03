[← README](../README.md)

## Connect a client

For a reusable typed client with reconnect and command promises, see the
[JavaScript SDK](../sdk/javascript/README.md) and
[runnable examples](../examples/web-client/README.md). Local examples can use the SDK build
without installing the npm package.

The agent broadcasts card and status events. `smc-data` carries the card; the rest say what
happened around it.

| Event | Payload |
| :---- | :------ |
| `smc-data` | the card, see [what a card contains](#what-a-card-contains) |
| `smc-inserted` | `{message}` — a card arrived |
| `smc-removed` | `{message}` — the card was taken out |
| `smc-error` | `{message}` — a read failed, or an action was refused |
| `smc-status` | readers, the selected one, and what the agent is doing |

What the agent reads is configuration with **one source of truth** —
`config.toml`, read through `GET /api/settings` — and the card sockets carry no
derived copy of it.

The control channel is **read-only**: `get-status`, `refresh-readers` and
`read-now` are the actions there are. The removed `get-options`, `set-options`
and `set-reader` are answered with an `smc-error` naming the unknown action,
and the `remote_control` field is gone — a client that read it as "allowed"
should assume the answer is always "no".

### Troubleshooting

Open **Troubleshoot → Open diagnostics** from the tray, or `/diagnostics`.
The page and `/api/diagnostics` use the settings guard. Reports contain version,
OS/architecture, foreground/service mode, endpoint, transports, TLS, reader
health and active log directory; they exclude configuration, tokens, card data
and log contents. **Copy diagnostics** in the tray also adds the OS service
state. On Linux copying uses wl-copy, xclip or xsel; the page offers a manual
copy fallback if clipboard access is unavailable. **Open log folder** opens
only an existing absolute directory reported by the running agent.

### Reader health

`GET /api/health` uses the same loopback/Host/Origin/proxy-header guard as settings.
It reports `starting`, `ready`, `no-reader`, `reader-busy`, `read-failed` or
`pcsc-unavailable`, reader names, current card state and timestamps of the last
successful read/error. It contains no card payload, token or raw error text.
The tray's green agent dot means connected; its separate reader row displays
hardware health and the last successful read in the tooltip.

### Command results

Commands may include an optional `request_id` (at most 128 bytes). Such requests
receive `smc-command-result` **only on the requesting connection**, with
`{request_id, action, status, code}`. Status is `accepted` followed by
`completed` or `failed`, or `busy` when a queue/reader cannot accept the work.
Accepted means queued, not successfully read. A completed read means the read
finished successfully; card data continues to use the existing broadcast.
Unknown actions fail. Requests without an ID keep the legacy event contract.
Clients should use a timeout and avoid submitting the same command through both
transports. The bundled page sends on one transport and times out after 30s.
Inbound WebSocket frames are limited to 4 KiB; slow subscribers are disconnected
when their 16-message delivery queue fills, and ping/pong detects broken peers.

### Via WebSocket (the default)

The raw WebSocket sends one JSON object per frame, shaped `{event, payload}`.
It is the default transport; socket.io is opt-in (see
[Configuration](configuration.md#configuration)).

```html
<script>
  const conn = new WebSocket('ws://127.0.0.1:9898/ws');

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
**4.x** client (the Go server speaks the v4 protocol):

```html
<script src="https://cdnjs.cloudflare.com/ajax/libs/socket.io/4.8.1/socket.io.js"></script>
<script>
  const socket = io('http://localhost:9898', {
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
  browser can send. socket.io 4.x takes it as the `query` option.
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

## Browsers

Start with the bundled `http://localhost:9898` page to verify the agent and reader.
For a page hosted elsewhere, browser permissions and HTTPS policy also affect
whether it can connect to the local agent.

- An HTTP page can use `ws://127.0.0.1:9898/ws`.
- For an HTTPS page, use `wss://` with a certificate trusted by the browser
  when required by its mixed-content policy. Safari blocks insecure local
  WebSockets from HTTPS pages; see the [WebKit issue](https://bugs.webkit.org/show_bug.cgi?id=173161).
- Chrome 147 added Local Network Access restrictions for WebSockets. Allow the
  site's local-network permission, or have IT configure
  `LocalNetworkAccessAllowedForUrls`; see [Chrome's release notes](https://developer.chrome.com/release-notes/147).

For TLS setup, see [Configuration](configuration.md#tls). The hostname in the
socket URL must match the certificate. `127.0.0.1` always means the machine
running the browser; use the agent machine's address for a LAN deployment.

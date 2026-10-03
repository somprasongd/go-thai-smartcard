[← README](../README.md)

## The bundled page

`http://localhost:9898` serves a page built for reading a card at a glance. It
shows every field the card returns rather than the name and address alone, and
adds what it can work out from them — age, days until expiry, the gender label,
and whether the ID number passes its checksum — marked apart from what the card
itself says.

Three presets set the display switches together:

| Preset | ID number | Portrait | Screen after removal | Type |
| :----- | :-------- | :------- | :------------------- | :--- |
| **kiosk** | masked | blurred | cleared the moment the card leaves | normal |
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

### Previous card data after connection loss

When all card transports disconnect, displayed data is no longer confirmed
current. Settings has a **Card data after connection loss** display preference:
clear immediately, keep with an old-data warning, or clear after 5–3600 seconds.
The preset default clears immediately for kiosk and keeps with a warning for
counter/dev. This is saved only in this browser and origin, applies immediately
across open tabs, and does not change agent configuration or require restart.
Use **Save display preference** in that section independently of the agent's
config Save button. If the agent URL/port changes, set this preference again at
the new origin; its preset default applies there.

The warning includes the browser's time of receiving the last successful read.
Reconnect and status messages do not make old card data current; only a new
successful `smc-data` event does. A delayed expiry continues after reconnect,
and a new read cancels it. Copies of old data require confirmation. Losing only
one transport while the other is connected does not mark the data old. The
existing card-removal clearing setting is unchanged. Card data itself is never
persisted by this preference.

## Settings

<http://localhost:9898/settings> is a self-contained page (plain HTML, no CDN,
so it works on a hospital network that cannot reach the internet) for everything
the agent reads and who may connect to it — the one place that changes them.
`listen` renders as an "Expose to network" checkbox, the one decision most
deployments ever make about it; a deliberate custom bind (a specific interface
address) keeps the free-text field. The socket-token section appears only while
exposed — a loopback token is enforced nowhere.

What a save does:

- `[card]` — applies on the next card insert, no restart.
- `[server]` and `[tls]` — changed ports are reserved before applying the save;
  unchanged listeners are reused. A failed bind or endpoint publication keeps
  the previous configuration and running listener. The agent process and card
  read loop do not restart. Settings moves to the new URL after success; when
  a one-time token is shown, copy it before using the link to the new page.
  Reopen other browser tabs on an old port from the tray.
- The file is rewritten from a template. **Comments added by hand are lost on
  the next save.** A hand edit takes effect after a restart.

A save refuses to overwrite a file that changed on disk since it was served:
`GET /api/settings` returns a fingerprint of the file's bytes, `PUT
/api/settings` echoes it back, and a mismatch is a `409` with "the file changed
on disk, reload before saving". Two pages saving at once get the same refusal —
the second is told to reload rather than silently undoing the first.

The token section generates the socket token (see
[Connect a client](web-client.md#connect-a-client)) and shows it **once**, with a copy
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

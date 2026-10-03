[← README](../README.md)

## Tray

`thai-smartcard-tray` is an optional menu-bar / system-tray app: the icon
stands alone in the menu bar, and the menu holds the reader and card state
and shortcuts to the test and settings pages. Exposure, the token, and the
read image / laser ID / NHSO switches are not in the menu — they are all one
decision each with the token's one-time showing, which only exists on the
[settings](usage.md#settings) page. It is a **thin client**
of the agent's `/ws` — it never touches the config file or spawns an agent
process. It discovers the running agent from public endpoint metadata and follows
port changes every two seconds, even while its old socket is connected. The
test and settings menu links follow the same URL. Unreachable agents,
authentication refusals, and a disabled WebSocket transport have distinct messages.

URL precedence is an explicit `--url`, a verified per-user endpoint, a verified
service endpoint, then `http://127.0.0.1:9898` for older agents. For example:

```sh
thai-smartcard-tray --url http://127.0.0.1:9999
```

When opened without `--url`, the tray checks the installed service once and
starts it only if its state is **stopped**. A running or transitioning service
is left to the OS; the tray connects when its endpoint becomes ready. A verified
foreground/dev agent also suppresses automatic service startup. Opening an
explicit `--url` never automatically starts a local service. Automatic startup
never asks for an administrator password on macOS; manual service actions retain
the password fallback. An intentional Stop is not undone by background polling.

Menu labels, status messages and tooltips use the logged-in user's primary UI
language: Thai for `th`, English for all other languages. The language is selected
when the tray opens. This is independent of the browser page's language and card
data language. See [tray lifecycle and language](../docs/plan/tray-lifecycle-language.md).

An explicit URL never follows discovery. The metadata contains only a local
URL, process instance ID, schema version and generation; it contains no token,
configuration or card data. The tray verifies the instance via `/api/info`.

### Restart / pause / resume from the tray

The **Agent service** submenu controls the agent's system service on
every OS, without a terminal and without elevating the tray itself. Each OS
authorises it through its own mechanism — the tray never spawns an agent
process of its own; the service manager owns the single installed run:

| OS | How the tray is authorised |
| :- | :------------------------- |
| Linux | a polkit rule shipped by the `.deb`/`.rpm` lets an **active local session** manage this one unit; the tray runs `systemctl` |
| Windows | the installer grants **Interactive Users** start/stop on this one service (`sc sdset`); the tray calls the Service Control Manager directly |
| macOS | the `.pkg` installs a root helper (`com.thaismartcard.control`) that listens on a **local unix socket restricted to group `admin`** and forwards to the service; a manual install without the helper falls back to an administrator-password dialog per action |

The first tray row shows agent readiness independently of the reader/card row:
green means the card WebSocket is connected; amber means the service is running
but not connected, or authentication/WebSocket settings prevent receiving data;
red means the service is stopped; gray means it is unavailable and the service
state cannot be determined. Text accompanies every color. Hover over the status
row to see the discovered URL and port. A connected foreground agent takes
precedence over the installed service's status.

The Agent service submenu has **Restart** and **Pause / Resume**. Pause stops
the OS service and card reading for all clients, so it asks for confirmation;
Resume starts the service again. Restart does not ask for confirmation (it is what a
hand-edited `config.toml` needs — saves from
[settings](usage.md#settings) apply live). Where the mechanism is missing — no
polkit agent on a headless Linux, a manual Windows install without the
installer's ACL — the menu items disable themselves and the tooltip says why;
the terminal commands below always work.

To let more than administrators control the agent on macOS (for example a
dedicated kiosk account that is not an admin), widen the helper socket's group:

```sh
sudo chgrp staff /var/run/thai-smartcard-control.sock
```

| Agent mode | Endpoint file |
| :-- | :-- |
| Linux service | `/var/lib/thai-smartcard/endpoint.json` |
| macOS service | `/Library/Application Support/ThaiSmartcardEndpoint/endpoint.json` |
| Windows service | `%ProgramData%\ThaiSmartcardEndpoint\endpoint.json` |
| Foreground / dev | `<os.UserCacheDir()>/thai-smartcard/endpoint.json` |

One publisher owns each slot through a process lock. Multiple foreground agents
under one user need explicit tray URLs for the additional instances. If endpoint
publication fails at startup, the agent continues with a warning and remains
usable via an explicit URL. A port change through Settings is refused until the
new endpoint can be published. Discovery is local only and does not bypass socket
authentication: a network-exposed agent may still refuse the tray's unauthenticated
socket. A listener bound only to a specific LAN IP has no discoverable loopback URL.

Other browser tabs on the previous port cannot discover the new URL from the
filesystem; reopen the test page from the tray. See the
[endpoint discovery plan](../docs/plan/tray-endpoint-discovery.md) for the save and
rollback contract.

The tray installer requires the agent's. The installer registers the tray to
start at login; turn it off in the operating system's own list of login items —
Windows: Settings > Apps > Startup (or Task Manager > Startup), macOS: System
Settings > General > Login Items, Linux: your desktop's startup applications
settings. **Quit tray** closes the icon for this session and keeps the shared
agent available to other clients. **Stop agent and quit…** asks for confirmation,
stops the service, and closes the tray only after a stopped state is confirmed;
an error keeps the tray open. Reopening the default tray starts a stopped service.

The service is registered to start at boot (Windows automatic service, macOS
LaunchDaemon with RunAtLoad, Linux enabled systemd unit); the tray starts at user
login. There is no required startup order or fixed delay: the OS owns one named
service and the tray follows endpoint readiness. A tray is not available before
an interactive login. Closing the tray does not unregister either startup entry.

On GNOME the tray needs the AppIndicator extension;
without it the tray sends a notification pointing at `/settings` instead of
failing silently.

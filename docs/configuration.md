[← README](../README.md)

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

[logging]
mode = "auto"            # services use files; foreground uses stderr
max_size_mb = 10          # MiB per file
max_backups = 3
max_age_days = 7
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

### Logs and disk retention

The agent's default `[logging] mode = "auto"` uses private rotating files when
running as a service, and stderr when running in a terminal. `mode = "file"`
uses files in either launch mode; `mode = "console"` delegates retention to
whoever captures stderr (for example journald or a container runtime).

Files are beside the selected config: `<config directory>/logs/agent.log`.
With the standard service config locations, that is:

| Platform | Log directory |
| --- | --- |
| Linux | `/etc/thai-smartcard/logs/` |
| macOS | `/Library/Application Support/ThaiSmartcard/logs/` |
| Windows | `%ProgramData%\ThaiSmartcard\logs\` |
| Development with `--config ./config.dev.toml` and file mode | `./logs/` (Git ignored) |

Default limits are 10 **MiB** per file, at most three backups and a maximum
backup age of seven days. The active file plus backups use at most 40 MiB of
log content. Count and age both apply, so busy agents may retain less than
seven days. Files also rotate at a UTC day change; maintenance checks each
minute even while no new events arrive. Age is measured from the backup's
last log write. Oversized existing backups are removed when limits shrink.
An oversized individual write is split across files within the same limits.
Files are 0600 (private DACL on Windows); newly created directories are 0700.
One process lock prevents concurrent agents rotating the same log.

Logging is set in `config.toml` or the Application logs section of `/settings`
and applies on restart. The page shows disk usage and a pending-restart notice;
diagnostics distinguishes configured limits from the running process's limits.
Older API clients that omit logging preserve the existing policy. `max_size_mb` accepts
1–1024; `max_backups` accepts 0–1000 (0 keeps only the active file);
`max_age_days` accepts 0–36500 (0 disables age expiry while count still applies).
For a 5 MiB file and two backups, for example:

```toml
[logging]
mode = "auto"
max_size_mb = 5
max_backups = 2
max_age_days = 3
```

That limits the main log content to 15 MiB. A service that cannot load its
config records the failure in a separate `startup.log`, capped at 1 MiB with
no backups, then exits with an error. File I/O failure cannot be made durable
on an unwritable/full filesystem; startup fails and stderr is the fallback.
No card trace is created by any logging mode.

On macOS, new service registrations and package upgrades send launchd's raw
stdout/stderr to `/dev/null`, so there is no second growing `.err.log` next to
the rotating application log. An existing manual registration needs reinstalling
with the new binary (`service uninstall`, `service install`, `service start`).
Old `/var/log/thai-smartcard-agent.err.log` and `.out.log` files are outside this
policy; after confirming the old registration has stopped using them, remove
those files separately if no longer needed. A package upgrade changes the
registration but preserves those historical files.

To clear current application logs manually, stop the service, remove the files
in its `logs` directory, and start it again; the agent recreates its files.
Archived `agent.log.*.bak` files may be removed while the service is running.
Do not delete the active log or `.lock` file during a run. Rotation and expiry
are automatic; no scheduled deletion command is required.

### TLS

TLS is needed when an `https://` page talks to the agent — on a LAN IP in any
browser, and even on loopback in Safari (see
[Browsers](web-client.md#browsers)). It is not needed for `http://` pages, which is the
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

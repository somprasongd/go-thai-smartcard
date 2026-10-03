[← README](../README.md)

# Installation

## Install a release package

Download the package for your OS from
[Releases](https://github.com/somprasongd/go-thai-smartcard/releases).
Install it on the computer connected to the card reader.

- **Windows:** run `thai-smartcard-setup-<version>.exe` with administrator
  privileges and follow the installer.
- **macOS:** open `thai-smartcard-agent-<version>.pkg` and follow the installer.
- **Debian/Ubuntu:** `sudo apt install ./thai-smartcard-agent_<version>_amd64.deb`.
- **RPM-based Linux:** `sudo dnf install ./thai-smartcard-agent-<version>.x86_64.rpm`.

Linux tray packages are separate; install the tray package after the agent if
needed. Packages register the native service. Use `thai-smartcard-agent service
status` to check it, then open [localhost:9898](http://localhost:9898) and insert
a card. The reader must be visible to PC/SC; see [reader setup](readers.md).

## Run from source

Requires [Go](https://go.dev/dl/) 1.27 or newer, and a reader that PC/SC can
see. Install the [native build prerequisites](library.md#use-as-a-library),
clone the repository and run these commands from its root. Check the reader first — nothing else works until it does:

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
[Configuration](configuration.md#configuration)); `--config <path>` points at another file, and
`make dev` runs with a git-ignored `config.dev.toml` written with the defaults
on first run. To ship it as a binary:

```sh
go build -o bin/thai-smartcard-agent ./cmd/agent

# Windows
go build -o bin/thai-smartcard-agent.exe ./cmd/agent
```

## Run as a service

The agent is a system service on every platform (Windows Service, systemd,
launchd); a tray app, where one is installed, controls the installed service through
the OS service manager and never spawns an agent process. The `.deb`/`.rpm` do
all of this for you — they install the service,
start it, and ship the polkit rule and the default config — and are built by
the packaging workflow from a release tag. By hand:

### Controlling the service

One subcommand drives the platform's own manager — launchd on macOS, systemd
on Linux, the Service Control Manager on Windows:

```sh
thai-smartcard-agent service start      # start
thai-smartcard-agent service stop       # stop
thai-smartcard-agent service restart    # restart, e.g. after a hand edit of config.toml
thai-smartcard-agent service status     # running / stopped
thai-smartcard-agent service install    # register with the manager
thai-smartcard-agent service uninstall  # remove the registration; config.toml stays
```

On macOS and Linux the state-changing ones need `sudo`; on Windows run them
from an **Administrator** terminal. Stopping the agent deliberately leaves the
tray app alone. Plain **Quit tray** keeps the service running; the separate
**Stop agent and quit…** menu offers a confirmed stop followed by closing the tray.

- **Linux** — the packaged unit (`.deb`/`.rpm`) sets `After=pcscd.service`,
  which `service install` cannot express; prefer the package. The service is
  also visible to `systemctl status thai-smartcard-agent`.
- **Windows** — the service is also visible to `sc query thai-smartcard-agent`
  and `net start thai-smartcard-agent`.
- **macOS** — the registration is a LaunchDaemon at
  `/Library/LaunchDaemons/thai-smartcard-agent.plist`.

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

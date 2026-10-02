# Plan: start / stop the agent from the tray (all OSes)

Status: proposal, written 2026-10-02 after v3.0.1. Decisions are proposed
defaults, not settled — the table lists them so they can be accepted or
overturned one by one.

## Goal

Add **Start agent / Stop agent / Restart agent** to the tray menu, on macOS,
Windows and Linux, without breaking the guarantees the v3 plan was built on:
the agent stays a system service with one run mode, and the tray stays a thin,
unprivileged viewer.

The concrete UX gap this closes: today, quitting the tray leaves the agent
running (by design), and when the agent is *down* the tray can only print the
terminal command to start it. A kiosk operator should not need a terminal to
bring the reader back.

## Relationship to decision 12 of the v3 plan

Decision 12 says "the tray **never spawns** the agent". That stays true: the
tray never runs a child agent process with its own config path and PC/SC
context — that is what would create a second run mode. What this plan adds is
*control*: the tray asks the OS service manager (launchd, the Windows Service
Control Manager, systemd) to start, stop or restart the one installed service.
The "Who starts the agent" section of the v3 plan is amended accordingly: the
tray can now *request* a start through the OS, but the OS service manager
still owns the process.

## The core problem: escalation

The tray runs as the logged-in user. The agent service runs as root / SYSTEM /
a dedicated system user. Starting and stopping it is a privileged operation.
Each OS solves this differently, and the asymmetry drives the design:

| OS | Native mechanism | New component needed? |
| :- | :--------------- | :-------------------- |
| **Windows** | Service ACLs (SDDL): a service can grant `SERVICE_START`/`SERVICE_STOP` to Interactive Users, who then call the SCM directly — no elevation, no dialog | **No** — one `sc sdset` line in the installer |
| **Linux** | polkit: the package already ships a polkit rule (decision 17); ship a second rule scoping `org.freedesktop.systemd1.manage-units` to this unit for active local sessions, and the tray just runs `systemctl` | **No** — one more rule file in the package |
| **macOS** | launchctl in the *system domain* is all-or-nothing root; there is no per-service ACL | **Yes** — a small privileged helper (details below) |

This asymmetry is why no single trick (e.g. the tray shipping a sudoers file)
covers all three.

## Design per OS

### Linux — polkit rule (no new component)

The `.deb`/`.rpm` gain a second rule file,
`/etc/polkit-1/rules.d/49-thai-smartcard.agent.rules`:

```js
// Let an active local session start/stop/restart the agent without a
// password. Scoped to this unit only.
polkit.addRule(function(action, subject) {
    if (action.id == "org.freedesktop.systemd1.manage-units" &&
        action.lookup("unit") == "thai-smartcard-agent.service" &&
        subject.local && subject.active) {
        return polkit.Result.YES;
    }
});
```

The tray then runs `systemctl start|stop|restart thai-smartcard-agent.service`
and `systemctl is-active` for state. Where polkit is absent (headless, no
desktop agent), systemd falls back to its password prompt or the call fails —
the tray disables the items and keeps the terminal hint, same as today.

### Windows — service SDDL (no new component)

After `service install`, the installer runs one extra elevated command:

```
sc sdset thai-smartcard-agent "D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;LCRPWP;;;IU)"
```

The trailing ACE grants **Interactive Users** List + Start + Stop on this one
service. The tray calls `golang.org/x/sys/windows/svc/mgr`
(`Query()` for state, `Start()`/`StopService()` — no elevation, no UAC, no
console). Manual installs that skip the installer get no tray control; the
menu stays disabled with the terminal hint (documented).

### macOS — privileged helper (one small new component)

`launchctl` in the system domain needs root, and there is no per-service ACL —
so the tray needs somebody root on its side. Three options were weighed:

| Option | UX | Cost |
| :----- | :- | :--- |
| `osascript … with administrator privileges` per action | password dialog on *every* click | zero new components |
| SMAppService / SMJobBless XPC helper | invisible after install | heavy: signed helper app, embedded plist, deprecated APIs |
| **LaunchDaemon helper with a local unix socket** (proposed) | invisible after install | one plist + a `control-helper` subcommand of the agent |

The helper reuses what already ships: **the agent binary itself gains a
`control-helper` subcommand**, and the `.pkg` postinstall drops
`/Library/LaunchDaemons/com.thaismartcard.control.plist` running it. The
helper listens on `/var/run/thai-smartcard-control.sock` (mode `0660`,
group `admin`), accepts one JSON request per connection
(`{"action":"start"|"stop"|"restart"|"status"}`), and answers by calling the
same `kardianos/service` `Control()`/`Status()` code the terminal command
uses — in-process, as root. No launchctl string handling, no new binary, no
password prompts. The socket is local-only by construction; it is not a
network surface.

If the helper is absent (manual `service install` without the pkg), the macOS
tray falls back to `osascript` with admin privileges — works, but prompts.

## Architecture

New package `pkg/ctl` — one interface, three build-tagged implementations:

```go
type State int // Running, Stopped, Unknown

type Manager interface {
    State() (State, error)
    Start() error
    Stop() error
    Restart() error
}
```

- `ctl_linux.go` — exec systemctl (polkit does the authorisation)
- `ctl_windows.go` — `svc/mgr` against the SCM (SDDL does the authorisation)
- `ctl_darwin.go` — unix socket to the helper; `osascript` fallback
- (no js build needed — the tray is desktop-only; `pkg/ctl` is not imported
  by the agent's wasm path)

Only `cmd/tray` imports `pkg/ctl`. The agent gains the `control-helper`
subcommand (macOS build only) that serves the socket — it runs `service`
operations in-process, so the single-run-mode guarantee holds: start/stop go
*through the service manager*, never a spawned child.

### Tray menu

New "Agent" submenu between status and settings:

```
● กำลังทำงาน / Running          ← service state (from ctl, not just /api)
Start agent        (เมื่อหยุดอยู่)
Stop agent…        (ยืนยันก่อน — kiosk จะอ่านบัตรไม่ได้จนกว่าจะเริ่มใหม่)
Restart agent…     (ใช้หลังแก้ config.toml มือ)
```

- Stop asks for confirmation; Start and Restart do not.
- Actions run async; the menu item shows a busy state; on error the toast
  says so.
- The status line keeps its current reachability reporting (`/api/info`),
  and gains the service state so "not running" can be distinguished from
  "running but not reachable" (crashed listener) and from "stopped".

## Security analysis

- **Local DoS**: anyone in the permitted group can stop the kiosk's reader.
  This is the point of the feature, and the marginal risk is small — a person
  at the machine can already cut power. Default scopes: Linux = active local
  session (polkit `subject.active`); Windows = Interactive Users; macOS =
  group `admin` on the socket. A kiosk whose operator account is not an admin
  would need the socket group widened (documented flag in the pkg postinstall)
  — **open decision 2**.
- **Never network**: the control path is SCM / systemctl / a unix socket with
  `0660`. There is deliberately no network-reachable start/stop, consistent
  with the v3 settings rules.
- **No privilege daemon on Linux/Windows** — the OS's own mechanisms carry
  the authorisation, so there is no new long-running privileged code except
  the macOS helper (which only forwards to kardianos; it validates nothing
  beyond the socket permissions).

## Packaging changes

| OS | Change |
| :- | :--- |
| Linux `.deb`/`.rpm` | add the polkit `manage-units` rule file |
| Windows installer | add `sc sdset` in `[Run]` after `service install` |
| macOS `.pkg` | add the `com.thaismartcard.control` LaunchDaemon plist + run the helper once to create the socket; the plist is removed by the uninstaller path |

## Testing

- `pkg/ctl`: unit tests per implementation where the OS allows
  (`systemctl is-active` parsing, SDDL string builder, the helper's JSON
  protocol over a real socket on darwin).
- Manual matrix (the plan's usual): stop → status shows Stopped and `/api`
  unreachable → start → reachable within seconds; restart after a hand edit;
  tray started *before* the agent; helper missing on macOS; polkit-absent
  Linux; non-admin kiosk user per OS (open decision 2).
- Regression: tray Quit still never touches the service; `agent run`
  unaffected.

## Release

- **v3.1.0** (MINOR — new capability; nothing breaking).
- CHANGELOG under Added; README "Tray" and "Run as a service" sections gain
  the control story; the v3 plan's decision-12 amendment is recorded here and
  marked there.

## Open decisions

1. **macOS helper vs prompts**: proposed = helper (invisible after install),
   with `osascript` as the documented fallback when the helper is missing.
   Alternative = prompts only, no helper.
2. **Who may control**: proposed = Linux active local session, Windows
   Interactive Users, macOS group `admin`. Alternative = lock down to a
   dedicated group/user (tighter, but a plain kiosk operator then cannot use
   the feature it exists for).
3. **Restart menu item**: proposed = yes (the only UI for "hand edits need a
   restart"). Alternative = start/stop only.
4. **Confirm on Stop**: proposed = yes, one confirmation. Alternative = no
   confirmation anywhere.
5. **Menu shape**: proposed = an "Agent" submenu with Start/Stop/Restart.
   Alternative = flat items in the root menu.

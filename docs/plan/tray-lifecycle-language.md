# Tray lifecycle and system language

2026-10-02 extension to the service-control and v3 plans, requested after the
review fixes. The machine-wide agent can serve browsers and other users without
a tray. Plain Quit therefore keeps the service running; an explicit Stop agent
and quit item offers the coupled lifecycle with confirmation.

## Lifecycle

| Trigger / state | Action |
| --- | --- |
| Machine boot | Installed agent service autostarts through the OS |
| Interactive login | Installed tray login entry opens the tray |
| Default tray opens, service stopped | Request Start through OS manager once |
| Service running | Connect; no Start request |
| Service unknown/transitioning or status denied | No automatic Start; retain polling and manual actions where supported |
| Explicit `--url` or verified foreground/dev agent | No automatic local service Start |
| Stop menu | Stop; this tray's polling never restarts the service |
| Quit tray | Close only the tray |
| Stop agent and quit | Confirm, Stop, verify stopped, then Quit; retain tray on error |
| Open default tray again | Repeat the status check; start only if stopped |

The tray never spawns a child agent or reads config. Startup, Start, Stop and
Restart requests within a tray are serialized. A manual action wins over a
pending startup check. Multiple logged-in users may request Start concurrently;
the OS manager owns one named service, and an already-running result after a
racing Start is accepted. This does not require ordering or a boot/login delay.
The endpoint client retries independently of service control and follows
published listener readiness. An automatic Start failure is logged and is not
retried by a watchdog. macOS automatic startup uses only the root helper so
login never opens a password prompt; explicit actions keep the existing fallback.

Existing installer contracts already supply boot/login startup:

- Windows: automatic service (kardianos/service default), HKLM Run tray entry
  enabled by the installer's default login task.
- macOS: agent LaunchDaemon RunAtLoad/KeepAlive, tray LaunchAgent RunAtLoad
  with KeepAlive false so Quit remains closed for the session.
- Linux: `systemctl enable --now`, XDG autostart desktop entry.

No installer ordering change is needed. Users may disable the tray's startup
entry through OS settings. A second user opening a tray can start a service that
someone else stopped; anyone requiring a permanent stop must disable the service
and/or tray startup in OS administration, rather than relying on a tray session.

## Language

Choose language once when the tray opens. A primary Thai language tag (`th`,
`th-TH`, `th_TH.UTF-8`) selects Thai. Every other primary language selects English;
a secondary Thai preference never turns a non-Thai primary UI into Thai. All
application menu labels, tooltips, status text and confirmation messages use one
language. Reader names, URLs, service diagnostics and commands retain their
original values. Browser and card-data language preferences remain independent.

- macOS: native Foundation [NSLocale.preferredLanguages](https://developer.apple.com/documentation/foundation/nslocale/preferredlanguages), first preference.
- Windows: native [GetUserDefaultUILanguage](https://learn.microsoft.com/en-us/windows/win32/api/winnls/nf-winnls-getuserdefaultuilanguage), Thai primary LANGID.
- Linux: [gettext locale precedence](https://www.gnu.org/software/gettext/manual/html_node/Locale-Environment-Variables) and [LANGUAGE](https://www.gnu.org/software/gettext/manual/html_node/The-LANGUAGE-variable.html); C/POSIX requests English, otherwise the first LANGUAGE entry overrides the message locale.

Native dialog frameworks may use their OS-provided button resources; application
text and menu choices are localized by the tray. Detection failures fall back
to English. The tray reads OS locale preferences only, never agent config.

## Verification

Unit tests use fake service managers; they never start/stop an installed service.
Coverage includes stopped/running/unknown/denied states, concurrent boot winning
Start, the noninteractive macOS path, explicit URLs, live/stale foreground metadata,
manual control winning startup, cancellation, stop failure/pending state retaining
the tray, and Thai/non-Thai/locale precedence.

Native boot/login and service-control acceptance still requires each OS's installed
packages: reboot, verify one service process, log in, verify one tray login entry,
Quit without stopping reads, Stop and quit, reopen and verify reading resumes.
Cross-builds alone do not establish those outcomes.

Local results (macOS, 2026-10-02): `make check` and race tests for server, tray
and ctl passed. Windows/Linux tray and ctl tests cross-compiled with
`go test -exec=true` (compilation only). Native system-language detection selected
English on this host. The new tray was run against the development agent on
9900 and established its connection; the test tray was terminated and the agent
remained listening. Visual menu verification could not proceed because the Mac
was locked. No machine reboot, installed-service stop/start, or native
Windows/Linux desktop run was performed in this check.


UI follow-up after the Mac was unlocked (2026-10-02): a temporary source copy
under `/tmp` added a window to open the tray's actual NSMenu because the computer
control tool cannot select a windowless menu-bar application. The product source
has no test window. Native menu inspection showed English-only labels under the
host's real English preference, a card-present state and one reader. Clicking
Quit exited the tray while the original agent PID remained listening on 9900.

A process-local volatile AppleLanguages preference selected Thai without changing
OS settings. The actual menu and confirmation dialog displayed Thai only. The
confirmation now uses Cocoa inside the tray instead of a separate osascript
process. UI testing found that a Thai cancel label did not provide a working
Escape shortcut; explicit handling was added. Escape and Return both cancelled,
retaining the tray and running state. Clicking Stop stopped a fake service manager
and then exited the tray; no installed machine service was stopped. The original
physical-card agent remained running. Reboot/login and installed-service lifecycle
acceptance remain separate from these UI checks.

After the native-dialog change, `make check`, server/tray/ctl race tests and
Windows/Linux tray/ctl cross-compilation passed again. The current native binary
was rebuilt in `bin/thai-smartcard-tray`. The test also exposed a Makefile target
being skipped by a root-level `tray` binary; marking the tray target phony fixed
it and `make tray` now runs its build. Temporary UI harness files were removed.

## Live launchd start/stop check after commit

2026-10-02, implementation commit `82c14cb`: the machine had no installed
`thai-smartcard-agent` system service or control-helper socket, and noninteractive
sudo required an administrator password. A temporary **user LaunchAgent** named
`thai-smartcard-agent-lifecycle-test-20261002` was therefore registered through
kardianos/service's `UserService` option, using the current agent binary, the
same RunAtLoad/KeepAlive template and a private temporary config on port 9901.
This is a real launchd service check, distinct from the earlier fake-manager UI
check and from a root-owned system-service acceptance test.

Observed results:

- Install reported stopped; Start made `/api/info` ready and opened exactly one
  listener (PID 12291).
- `ctl.EnsureRunning` on the running service retained that PID; no duplicate
  process appeared.
- Stop removed the listener and made HTTP unavailable; status reported stopped.
- `ctl.EnsureRunning` after Stop started a new process (PID 14339), restoring
  HTTP readiness. A second EnsureRunning retained the new PID.
- A card read initially waited while both agents shared the physical reader.
  Temporarily stopping the original foreground agent allowed `read-now` over
  WebSocket to succeed: a 13-digit ID and nonempty face image were validated
  without printing or saving personal data.
- The temporary service was stopped, uninstalled and its LaunchAgent registration
  checked absent; port 9901 was closed. The original foreground command and
  `config.dev.toml` were restored, with one listener on 9900 (new PID 20349).

Root-owned LaunchDaemon/control-helper permissions, tray control of that system
service and reboot/login autostart were not established by this user-service
check. Those require installation with administrator rights and native boot/login
acceptance. No trace or card payload was committed.

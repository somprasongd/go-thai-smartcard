# Tray status and two service controls

The tray uses a separate disabled agent-status row above reader/card status.
Its colored icon and text describe endpoint readiness, rather than inferring
readiness solely from the installed service. A connected foreground agent is
healthy even if that service is stopped or absent.

| Evidence | Color | Status |
| --- | --- | --- |
| Card WebSocket connected | Green | Agent is running |
| Authentication refused / WebSocket disabled | Amber | Specific connection problem |
| Service running but endpoint not connected | Amber | Waiting for connection |
| Service stopped and endpoint not connected | Red | Agent is stopped |
| Endpoint disconnected, service unknown or status query failed | Gray | Agent is unavailable |

The status tooltip shows the discovered endpoint and port (including default
HTTP/HTTPS ports). The menu title stays short. Discovery and explicit `--url`
remain the only endpoint sources; the tray does not read agent configuration.
Colors always have accompanying text, using Thai for Thai system language and
English otherwise.

The Agent service submenu replaces Start / Stop / Restart with:

- Restart, enabled for a running or manually controlled unknown service.
- Pause when running; Resume when stopped. Unknown state disables this toggle.

Pause means stopping the OS service, with confirmation because all clients lose
card reading. Resume starts that service. This does not implement a separate
card-reader-only suspension. The startup attempt remains one-shot, so it does
not undo an intentional Pause. A fresh OS status query resolves the toggle at
click time; duplicate clicks during a prompt/action are ignored. Quit and
Stop agent and quit keep their existing semantics.

Validation: local build/test/vet/format/wasm gate and server/tray/ctl race checks
passed. Windows/Linux compile checks are separate from native runtime evidence.
Native visual validation was attempted but macOS was locked; the new menu's
appearance and tooltip have not yet been observed in this session.

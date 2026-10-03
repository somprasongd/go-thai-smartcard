[← README](../README.md)

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
   `remote_control` are gone; settings change through `/settings`.
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

# Thai smartcard client

`@somprasongd/thai-smartcard-client` connects your application to the native
Thai smartcard agent over WebSocket. It provides typed events, reconnect with
exponential backoff and jitter, and promises for command results. It has no
runtime dependencies. ESM only; modern browsers and Node.js 22.14+ with a native
WebSocket API. socket.io is not included in this first SDK version.

Licensed under [Apache-2.0](LICENSE); see [NOTICE](NOTICE) for attribution.

## Usage

```sh
npm install @somprasongd/thai-smartcard-client
```

```ts
import { SmartCardClient, SmartCardClientError } from '@somprasongd/thai-smartcard-client';

const client = new SmartCardClient({url: 'ws://127.0.0.1:9898/ws'});
client.on('connection', ({state}) => showConnection(state));
client.on('status', status => showReader(status));
client.on('card', data => {
  if (data.personal) fillForm(data.personal);
});
client.on('error', error => showError(error.code));
client.on('agent-error', error => showError(error.message));

try {
  await client.connect();
  await client.readNow();
} catch (error) {
  if (error instanceof SmartCardClientError) showError(error.code);
}

// When leaving the page:
client.destroy();
```

The UI functions above belong to your application. A runnable plain-browser
example and a TypeScript consumer are in `examples/web-client/` in the source repo.

## Connection and command behavior

- Register listeners before `connect()`. It resolves on socket open, not reader
  readiness. A reader can be absent while the connection is healthy.
- Every open/reconnect sends `get-status`. Initial connection failure rejects
  `connect()`; with reconnect enabled the client continues retrying in the
  background. A later `connection` event tells you it connected.
- `getStatus()`, `refreshReaders()` and `readNow()` return a completed command
  result. `accepted` only means queued. `failed` and `busy` reject with
  `SmartCardClientError`, including the agent's `result.code`.
- Results are matched by request ID on the same connection. Card and status
  payloads are broadcasts, delivered through `card` and `status` events. They
  are not correlated with your command. `readNow()` does not return card data.
- Command timeout and loss of a connection give `outcome: 'unknown'`: the
  agent may have completed the command. The SDK never queues offline commands,
  replays commands after reconnect, or automatically retries busy commands.
- Reconnection alone does not make previously displayed card data current. The
  SDK does not cache card data. Your application decides whether to clear it or
  mark it old when disconnected, and resets freshness only on a new `card` event.
- `on()` returns an unsubscribe function. Synchronous listener exceptions are
  reported as `LISTENER_ERROR` without interrupting command completion. Handle
  rejected promises yourself inside asynchronous listeners.
- `close()` stops retries and settles pending work, retaining subscriptions for
  a later `connect()`. `destroy()` also removes subscriptions and is permanent.

Options: `connectTimeoutMs` (5000), `commandTimeoutMs` (30000), `reconnect`
(true), `reconnectMinDelayMs` (1000), `reconnectMaxDelayMs` (30000),
`maxPendingCommands` (16). Retry waits are randomized between half and all of
the current exponential delay, capped at the maximum. `token` is optional;
it is sent as a socket query parameter. Do not put tokens or card data in logs.
`webSocketFactory` can provide a compatible non-browser socket implementation.

Events: `connection`, `card`, `status`, `inserted`, `removed`, `agent-error`,
`command-result`, `error`. Payload type declarations reflect the Go model;
runtime validation checks message envelopes and command-result routing, not
every nested card field. Only connect to your trusted native agent.

## Compatibility and local development

This SDK targets the current request-ID WebSocket contract documented in
`docs/web-client.md`, present in agent v5.0.1. Older agents without command-result
support will time out. Agent configuration controls what fields are read.
TLS, allowed origins and browser local-network permissions still apply; see
`docs/web-client.md` in the source repo.

```sh
cd sdk/javascript
npm ci
npm run check
npm run test:integration # Go toolchain required; no reader needed
```

The checks use synthetic sockets for lifecycle failures and install/import/typecheck the
actual packed tarball in a disposable consumer. Integration runs the real Go
WebSocket server with synthetic command handling. They do not prove physical card
reader behavior or browser permission/certificate behavior.

For publishing, follow `docs/client-sdk-release.md` in the source repo.

# JavaScript client SDK

Keep the first SDK in this repository at `sdk/javascript`, with independent
package versions and `sdk-vX.Y.Z` release tags. Package name:
`@somprasongd/thai-smartcard-client`, matching the owner's npm account.

The first SDK is ESM TypeScript compiled to JavaScript plus declarations. Use
native WebSocket; no runtime dependencies or socket.io adapter. Target modern
browsers and Node 22.14+. Expose typed subscriptions, connection lifecycle,
three command methods and explicit cleanup. Commands settle only on terminal
request-ID results. A timeout or connection loss leaves the outcome unknown;
never replay commands. Synchronize status on every successful open.

Keep card data as independent broadcasts. The original protocol could not
correlate card payloads to command IDs. The additive source change now tags a
client-requested re-read with the same `request_id`; `readNow()` remains
command-only, and `readCard()` joins the matching broadcast without caching,
persisting or logging personal data. Automatic insertion reads remain untagged.
The v6.2.0 agent release carries this protocol extension.

Checks cover synthetic connection failures, accepted/terminal responses,
concurrency, stale sockets, retries, listener cleanup and package installation.
Native reader and browser permission behavior remain separate verification.
Use GitHub OIDC trusted publishing after an interactive first publish. Choose
the package license before publishing. The repository and SDK now use Apache-2.0,
with LICENSE and NOTICE included in the SDK package.

Adopting the SDK inside the bundled agent page is a later change: its existing
dual-transport UI needs a deliberate adapter and migration, while this first
SDK has one WebSocket transport. The server protocol does not change here.

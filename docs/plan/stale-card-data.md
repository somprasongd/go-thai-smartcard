# Previous card data and disconnect display policy

A card result on screen is no longer confirmed current once both WebSocket and
socket.io are disconnected. The page marks it old, displays the time it received
the result, and closes an enlarged portrait so the warning remains visible.
One connected transport prevents this transition. Reconnection and status
messages never clear the old-data flag; a successful new card result does.

Settings stores only a display preference at `smc.stalePolicy`, scoped to the
browser and origin. This is deliberately separate from the agent config and its
Save button, consistent with browser-local display/privacy preferences. The
section has its own Save display preference button and reports storage failures.
Changing URL/port uses the new origin's default until configured there.

Choices:

- Preset default: kiosk clears immediately, counter/dev keep with a warning.
- Clear immediately.
- Keep with an old-data warning.
- Clear after 5–3600 whole seconds; default duration is 30 seconds.

The delay starts at the first loss of all connections. Repeated disconnect/error
notifications and reconnects do not reset it. A new read or clearing data cancels
it. Changing the policy from Settings applies across open card-page tabs and
uses the already elapsed stale age. Existing card-removal auto-clear is unchanged.
Copying old card results, including field copies, requires explicit confirmation.
Card data and timestamps are not written to browser storage by this feature.

Validation includes Node behavior checks for defaults/overrides, surviving
transports, stale state across reconnect/status events, expiry after reconnect,
fresh-read timer cancellation, cross-tab policy updates, copy confirmation,
input validation, and saving/resetting the display preference. Native browser
checks used a synthetic local WebSocket fixture, without a card/reader, and
confirmed Settings in Thai/English, a warning after reconnect, a cross-tab policy
change clearing old data, and a fresh result removing the warning.

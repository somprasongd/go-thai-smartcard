# Bounded agent logs

Accepted 2026-10-02 to prevent an always-running agent consuming unbounded disk
space. Existing review fixes on fix/review-findings remain in place.

- `[logging]` is the only policy source: auto/file/console, MiB limit, backup
  count and age. Defaults: 10 MiB, 3 backups, 7 days. Auto selects files for
  managed runs and stderr for foreground runs. Hand edits apply on restart.
- The settings API preserves the file-only policy when older clients or the
  bundled page save their server/card/TLS fields. It is not a new browser flow.
- One private writer beside the selected config bounds each file, splits large
  writes, rotates on UTC day change and prunes history by both count and age.
  A minute ticker handles idle expiry; startup prunes before writes. A process
  lock prevents two agents sharing a rotating file. No dependency was added.
- Config failure logs have a separate 1 MiB/no-backup cap and a nonzero process
  exit; they cannot prune main history using defaults after validation failed.
- macOS launchd captures are disabled in new registrations and migrated by
  package postinstall so stderr does not form a second unbounded log. Old
  captures are preserved for explicit removal after migration. Console mode
  delegates capture/retention to the caller; on macOS use a deliberate wrapper
  if choosing console for a service whose launchd output is discarded.
- New private directories are 0700; append files use the existing Windows DACL
  handling as well as Unix 0600. No trace or card payload is added to logging.
- Rotation failures return errors and stop appending rather than exceed limits.
  Deletion failures retry on writes/maintenance; unavailable storage cannot
  guarantee durable logs. File contents are operational logs, not an audit DB.

Verification covers disk/count bounds, oversized messages, daily and idle age
expiry, restart cleanup, unrelated-file preservation, concurrent writes/Close,
process exclusion, error propagation and private permissions; config validation,
settings preservation and console/service selection are also tested. Native
Windows SCM/ACL installation and native Linux service deployment remain separate
from local macOS tests and cross-build evidence.

## Local evidence on 2026-10-02

- `make check`, required server/tray races, and race checks for logfile,
  atomicfile, config and agent passed.
- Windows agent/tray/logfile cross-build and wasm agent build passed.
- A native file-mode agent received 300 synthetic 16 KiB command events.
  With 1 MiB / 2 backups it rotated repeatedly and retained 2 backups;
  total content was 2,843,772 bytes, below 3 MiB. Every file was 0600.
- The same file-mode agent read the inserted real card twice through WebSocket;
  personal fields/checksum, Thai text, address, laser and JPEG validation passed,
  with an NHSO response. No card values were printed or put in logs for testing.
- The final agent is back on the original dev config at port 9900, whose auto
  mode remains console output. Production service installation was not changed
  on this computer; installer migration was syntax checked and the generated
  launchd template covered by tests.

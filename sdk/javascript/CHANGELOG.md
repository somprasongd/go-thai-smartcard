# Changelog

## [Unreleased]

## [0.2.0] - 2026-10-14

### Added

- Add `readCard()` to join a completed `read-now` command with only the card
  payload bearing its matching request ID; keep `readNow()` command-only.
  Requires an agent with the v6.2.0 request-ID protocol extension.

## [0.1.1] - 2026-10-03

### Fixed

- Verify already published archives across Node gzip implementations without
  accepting changes to package contents or invalid registry integrity.

## [0.1.0] - 2026-10-03

### Added

- Apache-2.0 license and project attribution, included in the npm package.
- Initial typed WebSocket client with reconnect, connection and command
  deadlines, request-ID command results and subscription cleanup.
- Plain-browser and TypeScript examples, synthetic lifecycle checks and
  packed-package installation checks.

[0.1.0]: https://github.com/somprasongd/go-thai-smartcard/tree/sdk-v0.1.0/sdk/javascript
[0.1.1]: https://github.com/somprasongd/go-thai-smartcard/compare/sdk-v0.1.0...sdk-v0.1.1
[0.2.0]: https://github.com/somprasongd/go-thai-smartcard/compare/sdk-v0.1.1...sdk-v0.2.0

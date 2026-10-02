# Local socket.io patch

Source: github.com/googollee/go-socket.io v1.6.2 (MIT; LICENSE retained).
Only engineio/server.go has behavior changes; types.go is gofmt normalized. This keeps the deployed protocol/API while
fixing listener retirement: a separate shutdown signal unblocks Accept without
closing a channel asynchronous session initialization may still send to. Track
initializing and accepted sessions so Close also releases unfinished handshakes.
Session registration and shutdown share a lock; initialization uses a local error.

Regression coverage lives in pkg/server/socketio_lifecycle_test.go and runs under
`go test -race ./pkg/server/`. Revisit this patch when updating upstream.

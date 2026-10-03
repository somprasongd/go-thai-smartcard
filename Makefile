dev:
	go run ./cmd/agent --config ./config.dev.toml

example:
	go run ./cmd/example

test:
	go test -race ./...

vet:
	go vet ./...

fmt-check:
	test -z "$(gofmt -l .)"

# The whole local gate from AGENTS.md. Run it before opening a PR.
check:
	go build ./... && go test ./... && go vet ./... && test -z "$(gofmt -l .)"

build-linux:
	go build -o ./bin/thai-smartcard-agent.linux-amd64 ./cmd/agent
	tar -czvf ./bin/thai-smartcard-agent.linux-amd64.tar.gz -C ./bin thai-smartcard-agent.linux-amd64

build-mac:
	go build -o ./bin/thai-smartcard-agent.darwin-amd64 ./cmd/agent
	go build -o ./bin/thai-smartcard-agent.darwin-arm64 ./cmd/agent

build-win:
	go build -o ./bin/thai-smartcard-agent.windows-amd64.exe ./cmd/agent

# The .deb/.rpm are built by the packaging workflow on a release tag, and
# locally by these targets when nfpm is installed. One config serves both.
# NFPM_VERSION sets the package version (the config expands it); a release
# build passes the tag, e.g. `make package-deb NFPM_VERSION=3.1.0`.
package-deb:
	go build -o ./bin/thai-smartcard-agent.linux-amd64 ./cmd/agent
	nfpm package -p deb -f packaging/nfpm.yaml -t ./bin/thai-smartcard-agent_amd64.deb

package-rpm:
	go build -o ./bin/thai-smartcard-agent.linux-amd64 ./cmd/agent
	nfpm package -p rpm -f packaging/nfpm.yaml -t ./bin/thai-smartcard-agent.x86_64.rpm

# The tray needs cgo and builds natively per OS (fyne-io/systray), so it is
# left out of the cross-compiling build-* targets. On Windows add
# -ldflags "-H=windowsgui" so no console opens.
# A direct go build may leave a binary named tray at the repository root.
.PHONY: tray
tray:
	CGO_ENABLED=1 go build -o ./bin/thai-smartcard-tray ./cmd/tray

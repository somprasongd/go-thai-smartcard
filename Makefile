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

# The whole local gate from AGENTS.md, including the wasm build of the card
# logic. Run it before opening a PR.
check:
	go build ./... && go test ./... && go vet ./... && test -z "$(gofmt -l .)" && GOOS=js GOARCH=wasm go build ./pkg/smc/

build-linux:
	go build -o ./bin/thai-smartcard-agent.linux-amd64 ./cmd/agent
	tar -czvf ./bin/thai-smartcard-agent.linux-amd64.tar.gz -C ./bin thai-smartcard-agent.linux-amd64

build-mac:
	go build -o ./bin/thai-smartcard-agent.darwin-amd64 ./cmd/agent
	go build -o ./bin/thai-smartcard-agent.darwin-arm64 ./cmd/agent

build-win:
	go build -o ./bin/thai-smartcard-agent.windows-amd64.exe ./cmd/agent

build-wasm:
	GOOS=js GOARCH=wasm go build -o bin/wasm/thai-smartcard-agent.wasm ./cmd/agent

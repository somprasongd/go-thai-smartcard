dev:
	go run ./cmd/agent/main.go

build:
	go build -o bin/thai-smartcard-agent ./cmd/agent/main.go

build-win:
	go build -o bin/thai-smartcard-agent.exe ./cmd/agent/main.go
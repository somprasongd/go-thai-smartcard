SMC_AGENT_PORT=9898
SMC_SHOW_IMAGE=true
SMC_SHOW_NHSO=false

export SMC_PORT
export SMC_SHOW_IMAGE
export SMC_SHOW_NHSO

dev:
	go run ./cmd/agent/main.go

example:
	go run ./cmd/example/main.go

build:
	go build -o bin/thai-smartcard-agent ./cmd/agent/main.go

build-win:
	go build -o bin/thai-smartcard-agent.exe ./cmd/agent/main.go
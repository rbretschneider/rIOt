.PHONY: build-server build-agent build-agent-all build-agent-apply build-agent-apply-all build-maintenance build-maintenance-all build-web docker migrate-up migrate-down dev clean test test-go test-web coverage

BINARY_SERVER=riot-server
BINARY_AGENT=riot-agent
BINARY_APPLY=riot-agent-apply
BINARY_MAINTENANCE=riot-maintenance
VERSION?=1.0.0
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"
LINUX_ARCH?=$(shell go env GOARCH)
LINUX_ARM?=7
LINUX_SUFFIX=linux-$(if $(filter arm,$(LINUX_ARCH)),armv$(LINUX_ARM),$(LINUX_ARCH))
DB_URL?=postgres://riot:riot@localhost:5432/riot?sslmode=disable

# Build server binary with embedded frontend
build-server: build-web
	cp -r web/dist cmd/riot-server/dist
	go build -tags embed_frontend $(LDFLAGS) -o bin/$(BINARY_SERVER) ./cmd/riot-server
	rm -rf cmd/riot-server/dist

# Build server without frontend (dev mode)
build-server-dev:
	go build $(LDFLAGS) -o bin/$(BINARY_SERVER) ./cmd/riot-server

# Build agent for current platform
build-agent:
	go build $(LDFLAGS) -o bin/$(BINARY_AGENT) ./cmd/riot-agent

# Cross-compile the agent and its Linux privileged helpers for supported targets
build-agent-all: build-agent-apply-all build-maintenance-all
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-amd64 ./cmd/riot-agent
	GOOS=linux GOARCH=386 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-386 ./cmd/riot-agent
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-arm64 ./cmd/riot-agent
	GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-armv7 ./cmd/riot-agent
	GOOS=linux GOARCH=arm GOARM=6 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-armv6 ./cmd/riot-agent
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-darwin-amd64 ./cmd/riot-agent
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-darwin-arm64 ./cmd/riot-agent
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-windows-amd64.exe ./cmd/riot-agent

# Linux helpers for the current architecture (override LINUX_ARCH/LINUX_ARM to cross-build).
build-agent-apply:
	GOOS=linux GOARCH=$(LINUX_ARCH) GOARM=$(LINUX_ARM) go build -ldflags "-s -w -X main.targetAssetName=riot-agent-$(LINUX_SUFFIX)" -o bin/$(BINARY_APPLY) ./cmd/riot-agent-apply

build-maintenance:
	GOOS=linux GOARCH=$(LINUX_ARCH) GOARM=$(LINUX_ARM) go build -ldflags "-s -w" -o bin/$(BINARY_MAINTENANCE) ./cmd/riot-maintenance

build-agent-apply-all:
	GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.targetAssetName=riot-agent-linux-amd64" -o bin/$(BINARY_APPLY)-linux-amd64 ./cmd/riot-agent-apply
	GOOS=linux GOARCH=386 go build -ldflags "-s -w -X main.targetAssetName=riot-agent-linux-386" -o bin/$(BINARY_APPLY)-linux-386 ./cmd/riot-agent-apply
	GOOS=linux GOARCH=arm64 go build -ldflags "-s -w -X main.targetAssetName=riot-agent-linux-arm64" -o bin/$(BINARY_APPLY)-linux-arm64 ./cmd/riot-agent-apply
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w -X main.targetAssetName=riot-agent-linux-armv7" -o bin/$(BINARY_APPLY)-linux-armv7 ./cmd/riot-agent-apply
	GOOS=linux GOARCH=arm GOARM=6 go build -ldflags "-s -w -X main.targetAssetName=riot-agent-linux-armv6" -o bin/$(BINARY_APPLY)-linux-armv6 ./cmd/riot-agent-apply

build-maintenance-all:
	GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o bin/$(BINARY_MAINTENANCE)-linux-amd64 ./cmd/riot-maintenance
	GOOS=linux GOARCH=386 go build -ldflags "-s -w" -o bin/$(BINARY_MAINTENANCE)-linux-386 ./cmd/riot-maintenance
	GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o bin/$(BINARY_MAINTENANCE)-linux-arm64 ./cmd/riot-maintenance
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w" -o bin/$(BINARY_MAINTENANCE)-linux-armv7 ./cmd/riot-maintenance
	GOOS=linux GOARCH=arm GOARM=6 go build -ldflags "-s -w" -o bin/$(BINARY_MAINTENANCE)-linux-armv6 ./cmd/riot-maintenance

# Build frontend
build-web:
	cd web && npm ci && npm run build

# Build Docker image
docker:
	docker build --build-arg VERSION=$(VERSION) -t riot-server:$(VERSION) .

# Run database migrations
migrate-up:
	go run -tags migrate ./cmd/riot-server -migrate-up

migrate-down:
	go run -tags migrate ./cmd/riot-server -migrate-down

# Development mode — run server without embedded frontend
dev:
	RIOT_DB_URL="$(DB_URL)" go run ./cmd/riot-server

# Run all tests
test: test-go test-web

# Run Go tests
test-go:
	go test -race -count=1 ./...

# Run frontend tests
test-web:
	cd web && npm run test:run

# Generate coverage report
coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	rm -rf bin/ web/dist cmd/riot-server/dist coverage.out coverage.html

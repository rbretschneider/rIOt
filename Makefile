.PHONY: build-server build-agent build-agent-all build-web docker migrate-up migrate-down dev clean

BINARY_SERVER=riot-server
BINARY_AGENT=riot-agent
VERSION?=1.0.0
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"
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

# Cross-compile agent for all supported targets
build-agent-all:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-amd64 ./cmd/riot-agent
	GOOS=linux GOARCH=386 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-386 ./cmd/riot-agent
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-arm64 ./cmd/riot-agent
	GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-armv7 ./cmd/riot-agent
	GOOS=linux GOARCH=arm GOARM=6 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-linux-armv6 ./cmd/riot-agent
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-darwin-amd64 ./cmd/riot-agent
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-darwin-arm64 ./cmd/riot-agent
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_AGENT)-windows-amd64.exe ./cmd/riot-agent

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

# Clean build artifacts
clean:
	rm -rf bin/ web/dist cmd/riot-server/dist

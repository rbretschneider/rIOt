# rIOt — Remote Infrastructure Oversight Tool

## Project Overview
Self-hosted infrastructure monitoring platform for homelab environments.
Three components: Go agent (deployed on devices), Go server (with PostgreSQL), React+TS dashboard (embedded in server binary).

## Architecture
- **Agent**: lightweight Go daemon, collects telemetry, pushes to server via HTTP
- **Server**: Go binary with chi router, serves REST API + WebSocket + embedded frontend
- **Database**: PostgreSQL (companion container)
- **Dashboard**: React + TypeScript + Tailwind CSS, compiled and embedded via `go:embed`

## Build Commands
```bash
make build-server       # Build server binary (with embedded frontend)
make build-agent        # Build agent for current platform
make build-agent-all    # Cross-compile agent for all targets
make build-web          # Build frontend
make docker             # Build Docker image
make migrate-up         # Run database migrations
make migrate-down       # Rollback last migration
make dev                # Run server in dev mode
```

## Key Conventions
- Go module: `github.com/DesyncTheThird/rIOt`
- HTTP router: chi v5
- Database driver: pgx v5
- Migrations: golang-migrate
- Logging: log/slog (structured JSON)
- Config: env vars for server, YAML for agent
- API prefix: `/api/v1/`
- Default port: 7331
- Auth: per-device API keys via `X-rIOt-Key` header

## Directory Structure
- `cmd/riot-server/` — Server entrypoint
- `cmd/riot-agent/` — Agent entrypoint
- `internal/models/` — Shared data types
- `internal/server/` — Server code (handlers, middleware, db, websocket, events)
- `internal/agent/` — Agent code (collectors, config, lifecycle)
- `migrations/` — SQL migration files
- `web/` — React frontend (Vite)
- `scripts/` — Install scripts, systemd units

## Testing
```bash
go test ./...           # Run all Go tests
cd web && npm test      # Run frontend tests
```

## Database
- PostgreSQL 16, connection via `RIOT_DB_URL` env var
- Migrations run automatically on server startup
- Retention: heartbeats 7d, telemetry 30d (configurable), events 90d

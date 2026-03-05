# rIOt — Remote Infrastructure Oversight Tool

Self-hosted infrastructure monitoring for homelab environments. Deploy a lightweight agent on each device and get a single-pane-of-glass view of your entire fleet through a real-time web dashboard.

## Features

- **Lightweight agent** — single static binary, < 30 MB RAM, runs on everything from a Raspberry Pi Zero to a Threadripper workstation
- **Rich telemetry** — CPU, memory, disk, network, services, processes, Docker containers, pending updates, security status
- **Docker container management** — dedicated per-device container dashboard with search, grouping via `riot.*` labels, real-time container events, and optional remote terminal (exec into running containers from the browser)
- **Real-time dashboard** — dark-mode React UI with live WebSocket updates
- **Offline resilience** — agent buffers telemetry locally when the server is unreachable
- **Simple deployment** — one `docker compose up` for the server, one-liner install for agents
- **Admin authentication** — password-protected dashboard with JWT session cookies
- **Configurable alert rules** — threshold-based alerts on any telemetry metric with cooldown deduplication and device filtering
- **Notification channels** — alert delivery via ntfy and webhooks, with test-send support and delivery logging
- **Uptime probes** — scheduled HTTP and DNS probes with history and status tracking
- **Fleet management** — agent version overview and bulk update across devices
- **Remote commands** — send commands (e.g., Docker restart) to agents from the dashboard
- **Security overview** — fleet-wide view of SELinux/AppArmor, firewall, open ports, failed logins
- **Per-device API keys** — generated at registration, individually revocable and rotatable
- **TLS support** — Let's Encrypt autocert or manual cert/key files

## Architecture

```
Agent (Go)  ──HTTP POST──►  Server (Go + embedded React UI)  ──►  PostgreSQL
                                   │
Dashboard (browser)  ◄──WebSocket──┘
```

The server is a single Go binary that embeds the compiled React frontend. No nginx, no separate web container. PostgreSQL runs alongside as the only other service.

---

## Server Installation

### Docker Compose (recommended)

Create a `docker-compose.yml`:

```yaml
services:
  riot-server:
    image: ghcr.io/rbretschneider/riot-server:latest
    ports:
      - "7331:7331"
    environment:
      - RIOT_DB_URL=postgres://riot:riot@riot-db:5432/riot?sslmode=disable
      - RIOT_PORT=7331
      - RIOT_API_KEY=changeme        # Master key used for agent registration
      - RIOT_ADMIN_PASSWORD=changeme # Dashboard login password
      - RIOT_RETENTION_DAYS=30
    depends_on:
      riot-db:
        condition: service_healthy
    restart: unless-stopped

  riot-db:
    image: postgres:16-alpine
    environment:
      - POSTGRES_USER=riot
      - POSTGRES_PASSWORD=riot
      - POSTGRES_DB=riot
    volumes:
      - riot-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U riot"]
      interval: 5s
      timeout: 5s
      retries: 5
    restart: unless-stopped

volumes:
  riot-data:
```

```bash
docker compose up -d
```

The dashboard is available at `http://<server-ip>:7331`.

### Docker Run

```bash
# Start PostgreSQL
docker run -d --name riot-db \
  -e POSTGRES_USER=riot \
  -e POSTGRES_PASSWORD=riot \
  -e POSTGRES_DB=riot \
  -v riot-data:/var/lib/postgresql/data \
  postgres:16-alpine

# Start rIOt server
docker run -d --name riot-server \
  -p 7331:7331 \
  --link riot-db \
  -e RIOT_DB_URL=postgres://riot:riot@riot-db:5432/riot?sslmode=disable \
  -e RIOT_API_KEY=changeme \
  -e RIOT_ADMIN_PASSWORD=changeme \
  ghcr.io/rbretschneider/riot-server:latest
```

### Server Environment Variables

| Variable | Default | Description |
|---|---|---|
| `RIOT_DB_URL` | `postgres://riot:riot@localhost:5432/riot?sslmode=disable` | PostgreSQL connection string |
| `RIOT_PORT` | `7331` | HTTP listen port |
| `RIOT_API_KEY` | `changeme` | Master API key for agent registration |
| `RIOT_ADMIN_PASSWORD` | — | Dashboard login password (bcrypt-hashed at startup) |
| `RIOT_JWT_SECRET` | auto-generated | Secret for JWT session tokens (auto-generated if omitted; set for stable sessions across restarts) |
| `RIOT_RETENTION_DAYS` | `30` | Days to keep telemetry snapshots |
| `RIOT_GITHUB_REPO` | `rbretschneider/rIOt` | GitHub `owner/repo` for update checks |
| `RIOT_ALLOWED_ORIGINS` | — | Comma-separated CORS allowed origins |
| `RIOT_TLS_ENABLED` | `false` | Enable TLS (`true` or `1`) |
| `RIOT_TLS_DOMAIN` | — | Let's Encrypt autocert domain (implies TLS enabled) |
| `RIOT_TLS_CERT_DIR` | — | Autocert cache directory |
| `RIOT_TLS_CERT_FILE` | — | Manual TLS certificate file path |
| `RIOT_TLS_KEY_FILE` | — | Manual TLS key file path |

---

## Agent Installation

### One-liner (Linux / macOS)

```bash
curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- http://<server-ip>:7331 <master-api-key>
```

This will:
1. Detect your architecture
2. Download the correct agent binary from GitHub Releases
3. Write a default config to `/etc/riot/agent.yaml`
4. Install and start a systemd service (Linux) or print manual run instructions (macOS)

### Manual Install

1. Download the agent binary for your platform from [Releases](https://github.com/rbretschneider/rIOt/releases):

   | Platform | Binary |
   |---|---|
   | Linux x86_64 | `riot-agent-linux-amd64` |
   | Linux ARM64 (RPi 3/4/5) | `riot-agent-linux-arm64` |
   | Linux ARMv7 (RPi 2/3 32-bit) | `riot-agent-linux-armv7` |
   | Linux ARMv6 (RPi Zero/1) | `riot-agent-linux-armv6` |
   | Linux i386 | `riot-agent-linux-386` |
   | macOS Intel | `riot-agent-darwin-amd64` |
   | macOS Apple Silicon | `riot-agent-darwin-arm64` |
   | Windows x86_64 | `riot-agent-windows-amd64.exe` |

2. Place the binary somewhere in your `PATH` (e.g. `/usr/local/bin/riot-agent`).

3. Create a config file at `/etc/riot/agent.yaml`:

   ```yaml
   server:
     url: "http://<server-ip>:7331"
     api_key: "<master-api-key>"
     tls_verify: false

   agent:
     device_name: ""              # leave empty to auto-detect hostname
     tags:
       - homelab
     poll_interval: 60            # full telemetry every 60s
     heartbeat_interval: 15       # lightweight ping every 15s

   collectors:
     enabled:
       - system
       - cpu
       - memory
       - disk
       - network
       - os_info
       - updates
       - services
       - processes
       - docker
       - security

   docker:
     enabled: "auto"               # "auto" (detect), "true", or "false"
     collect_stats: true            # per-container CPU/memory stats
     terminal_enabled: false        # set to true to allow remote exec from dashboard
   ```

4. Run the agent:

   ```bash
   riot-agent -config /etc/riot/agent.yaml
   ```

### Windows

Download `riot-agent-windows-amd64.exe` from [Releases](https://github.com/rbretschneider/rIOt/releases) and create a config file at `%PROGRAMDATA%\riot\agent.yaml` with the same format as above. Run from a terminal or set up as a Windows service using [NSSM](https://nssm.cc/) or `sc.exe`.

### Agent Config Reference

| Field | Default | Description |
|---|---|---|
| `server.url` | `http://localhost:7331` | rIOt server URL |
| `server.api_key` | — | Master API key (used for initial registration; replaced with per-device key after) |
| `server.tls_verify` | `true` | Verify TLS certificates |
| `agent.device_name` | hostname | Display name override |
| `agent.tags` | `[]` | Tags for grouping/filtering |
| `agent.poll_interval` | `60` | Seconds between full telemetry pushes |
| `agent.heartbeat_interval` | `15` | Seconds between heartbeat pings |
| `collectors.enabled` | all | List of collectors to run |
| `docker.enabled` | `auto` | Docker collection mode: `auto` (detect), `true`, `false` |
| `docker.socket_path` | auto-detect | Override the Docker socket path |
| `docker.collect_stats` | `true` | Collect per-container CPU/memory stats |
| `docker.terminal_enabled` | `false` | Allow remote `docker exec` from the dashboard |

### Available Collectors

| Collector | Data Collected |
|---|---|
| `system` | Hardware identity — CPU model, cores, RAM, board model, serial, BIOS, virtualization |
| `cpu` | Usage %, per-core, load average, temperature, frequency |
| `memory` | RAM total/used/free/cached/buffers, swap, usage % |
| `disk` | Block devices, mounted filesystems with usage |
| `network` | Interfaces, IPs, MACs, state, bytes tx/rx |
| `os_info` | OS name/version, kernel, uptime, timezone |
| `updates` | Package manager, pending updates, security updates |
| `services` | systemd services — name, state, enabled |
| `processes` | Top 15 by CPU, top 15 by memory |
| `docker` | Docker containers — name, image, status, ports, CPU/mem stats, `riot.*` labels, real-time events |
| `security` | SELinux/AppArmor, firewall, open ports, failed logins |

---

## API

All endpoints are under `/api/v1/`. Agent endpoints require the `X-rIOt-Key` header. Dashboard endpoints require admin authentication (JWT cookie).

### Public

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Server health check |
| `POST` | `/api/v1/auth/login` | Admin login (returns JWT cookie) |
| `POST` | `/api/v1/auth/logout` | Clear session cookie |
| `GET` | `/api/v1/auth/check` | Check authentication status |

### Agent (device key auth)

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/devices/register` | Register a new device (master key) |
| `POST` | `/api/v1/devices/:id/heartbeat` | Lightweight heartbeat |
| `POST` | `/api/v1/devices/:id/telemetry` | Full telemetry push |
| `POST` | `/api/v1/devices/:id/docker-events` | Real-time Docker container events |
| `GET` | `/api/v1/update/check` | Agent update check |

### Dashboard (admin auth)

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/v1/devices` | List all devices |
| `GET` | `/api/v1/devices/:id` | Device detail + latest telemetry |
| `GET` | `/api/v1/devices/:id/history` | Paginated telemetry history |
| `GET` | `/api/v1/devices/:id/containers` | List containers for a device |
| `GET` | `/api/v1/devices/:id/containers/:cid` | Container detail |
| `DELETE` | `/api/v1/devices/:id` | Remove a device |
| `POST` | `/api/v1/devices/:id/rotate-key` | Rotate device API key |
| `POST` | `/api/v1/devices/:id/commands` | Send command to agent |
| `GET` | `/api/v1/devices/:id/commands` | List device command history |
| `GET` | `/api/v1/summary` | Fleet summary stats |
| `GET` | `/api/v1/events` | Event/alert list |
| `GET` | `/api/v1/update/server` | Server update check |
| `GET/POST/PUT/DELETE` | `/api/v1/settings/alert-rules[/:id]` | Alert rule CRUD |
| `GET/POST/PUT/DELETE` | `/api/v1/settings/notification-channels[/:id]` | Notification channel CRUD |
| `POST` | `/api/v1/settings/notification-channels/:id/test` | Test notification channel |
| `GET` | `/api/v1/settings/notifications/log` | Notification delivery log |
| `GET` | `/api/v1/fleet/agent-versions` | Agent version summary |
| `POST` | `/api/v1/fleet/bulk-update` | Bulk update agents |
| `GET` | `/api/v1/security/overview` | Security overview |
| `GET` | `/api/v1/security/devices` | Per-device security details |
| `GET/POST/PUT/DELETE` | `/api/v1/probes[/:id]` | Uptime probe CRUD |
| `POST` | `/api/v1/probes/:id/run` | Run probe on demand |
| `GET` | `/api/v1/probes/:id/results` | Probe result history |

### WebSocket

| Endpoint | Description |
|---|---|
| `/ws` | Live dashboard updates (admin auth) |
| `/ws/agent` | Agent WebSocket (terminal relay) |
| `/ws/terminal/:deviceId/:containerId` | Browser-to-container terminal (admin auth) |

---

## Building from Source

Requires Go 1.24+ and Node.js 20+.

```bash
# Build server with embedded frontend
make build-server

# Build agent for your current platform
make build-agent

# Cross-compile agent for all 8 targets
make build-agent-all

# Build Docker image locally
make docker

# Run server in dev mode (no embedded frontend)
make dev
```

---

## Testing

```bash
# Run all tests
make test

# Go tests only
make test-go

# Frontend tests only
make test-web

# Go coverage report
make coverage
```

CI runs automatically on push to main and on PRs via GitHub Actions (`.github/workflows/ci.yml`).

---

## Releasing

Version is derived from git tags — there is no version file to edit.

```bash
# 1. Ensure all tests pass
make test

# 2. Tag the commit
git tag -a v1.2.0 -m "v1.2.0"

# 3. Push with tags — triggers the release workflow
git push origin main --tags
```

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which:
- Builds and pushes the server Docker image to `ghcr.io/rbretschneider/riot-server` (tagged `1.2.0`, `1.2`, `latest`)
- Cross-compiles 8 agent binaries with SHA-256 checksums
- Creates a GitHub Release with auto-generated release notes

---

## Data Retention

| Data | Retention |
|---|---|
| Heartbeats | 7 days |
| Telemetry snapshots | 30 days (configurable via `RIOT_RETENTION_DAYS`) |
| Events | 90 days |
| Device registry | Forever (until manually deleted) |

A background worker runs hourly to purge expired data.

---

## Docker Container Management

Devices running Docker get a dedicated container dashboard at `/devices/:id/containers` in the web UI, with search, filtering, and grouped container cards.

### `riot.*` Labels

Add Docker labels to your containers to control how they appear in the dashboard:

| Label | Description |
|---|---|
| `riot.group` | Group name — containers with the same group are displayed together |
| `riot.name` | Display name override (default: container name) |
| `riot.icon` | Emoji or icon shown next to the container |
| `riot.description` | Human-readable description |
| `riot.url` | Link to the service (shown as a clickable link in the UI) |
| `riot.priority` | Sort order within group (default: `50`, lower = higher) |
| `riot.hide` | Set to `true` to hide the container from the dashboard |
| `riot.tags` | Comma-separated tags for filtering |

Example in a `docker-compose.yml`:

```yaml
services:
  jellyfin:
    image: jellyfin/jellyfin
    labels:
      riot.group: "Media"
      riot.name: "Jellyfin"
      riot.icon: "🎬"
      riot.description: "Media server"
      riot.url: "http://jellyfin.local:8096"
      riot.priority: "10"
      riot.tags: "media,streaming"
```

### Remote Terminal

The agent can optionally allow remote `docker exec` from the dashboard, providing a browser-based terminal into running containers. This is disabled by default for security.

To enable, set `docker.terminal_enabled: true` in the agent config (`/etc/riot/agent.yaml`):

```yaml
docker:
  terminal_enabled: true
```

The terminal uses a WebSocket relay: browser connects to the server, which proxies to the agent's persistent WebSocket connection, which attaches to a `docker exec` session on the target container.

### Real-time Container Events

When Docker is available, the agent watches the Docker event stream and forwards container lifecycle events (start, stop, die, OOM, pause, unpause, create, destroy) to the server in real-time. These appear in the dashboard event feed and trigger alerts for OOM kills.

---

## License

MIT

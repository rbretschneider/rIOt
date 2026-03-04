# rIOt — Requirements Document
**Version:** 0.1.0-draft  
**Status:** Phase 1 Scope  
**Last Updated:** 2026-03-03

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [System Architecture](#2-system-architecture)
3. [Client Agent](#3-client-agent)
4. [Server & Ingestion Engine](#4-server--ingestion-engine)
5. [Dashboard UI](#5-dashboard-ui)
6. [Data Collection Specification](#6-data-collection-specification)
7. [Communication Protocol](#7-communication-protocol)
8. [Security](#8-security)
9. [Deployment](#9-deployment)
10. [Phase 1 Scope Boundaries](#10-phase-1-scope-boundaries)
11. [Phase 2 Preview (Out of Scope Now)](#11-phase-2-preview-out-of-scope-now)
12. [Non-Functional Requirements](#12-non-functional-requirements)
13. [Open Questions](#13-open-questions)

---

## 1. Project Overview

**rIOt** is a self-hosted infrastructure monitoring platform designed for homelab environments. It provides deep visibility into heterogeneous fleets of devices — from Raspberry Pi Zero/1 class hardware through to high-end Threadripper workstations — by deploying a lightweight agent on each device and ingesting telemetry into a central dashboard.

### Goals

- Give operators a single-pane-of-glass view of every device in their homelab
- Collect rich system telemetry: hardware specs, OS state, network, storage, update status, and more
- Be self-hostable with zero cloud dependency
- Stay lightweight enough to run on <1 GB RAM ARM devices
- Be extensible for remote management in a future phase

### Non-Goals (Phase 1)

- Remote command execution / configuration management
- Package installation or OS upgrades via dashboard
- Public cloud device management

---

## 2. System Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Homelab Network                   │
│                                                     │
│  ┌──────────────┐   ┌──────────────┐   ┌─────────┐ │
│  │  rIOt Agent  │   │  rIOt Agent  │   │  rIOt   │ │
│  │  (RPi 1B)    │   │  (Threadrip) │   │  Agent  │ │
│  └──────┬───────┘   └──────┬───────┘   └────┬────┘ │
│         │                  │                │       │
│         └──────────┬───────┘                │       │
│                    │        ───────────────-┘       │
│                    ▼                                 │
│         ┌──────────────────┐                        │
│         │   rIOt Server    │  (Docker Container)    │
│         │  ┌────────────┐  │                        │
│         │  │  Ingestion │  │                        │
│         │  │   Engine   │  │                        │
│         │  └─────┬──────┘  │                        │
│         │        │         │                        │
│         │  ┌─────▼──────┐  │                        │
│         │  │  Data Store│  │                        │
│         │  └─────┬──────┘  │                        │
│         │        │         │                        │
│         │  ┌─────▼──────┐  │                        │
│         │  │  Dashboard │  │                        │
│         │  │     UI     │  │                        │
│         │  └────────────┘  │                        │
│         └──────────────────┘                        │
└─────────────────────────────────────────────────────┘
```

### Components

| Component | Description |
|---|---|
| **rIOt Agent** | Lightweight Go daemon deployed on each monitored device. Ships as a single static binary per platform. Collects telemetry and pushes to server via HTTP + WebSocket. |
| **rIOt Server** | Single Go binary handling: HTTP ingestion API, WebSocket live-push, REST API, and serving the compiled frontend as embedded static assets. Runs in one Docker container. |
| **PostgreSQL** | Companion container in the same `docker-compose` stack. Persists all telemetry, device registry, and events. |
| **rIOt Dashboard** | React/TS SPA compiled at build time and embedded into the Go server binary via `go:embed`. No separate web server or container needed. |

### Single-Container Design

The entire platform runs via one `docker-compose up` with two services: the **rIOt Go server** and **Postgres**. The frontend is compiled (`npm run build`) and embedded into the Go binary at build time — zero separate web server, zero nginx, zero extra containers. Postgres data persists via a named Docker volume.

```
docker-compose up
  └── riot-server   (Go binary: API + WebSocket + embedded UI)
  └── riot-db       (Postgres 16)
```

---

## 3. Client Agent

### 3.1 Overview

The agent is a small, long-running daemon that periodically collects system telemetry and forwards it to the rIOt Server via HTTP/HTTPS or a lightweight message queue.

### 3.2 Platform Support

| Platform | Priority | Notes |
|---|---|---|
| Linux (ARM32) | **P0** | RPi 1/2/Zero, old SBCs |
| Linux (ARM64) | **P0** | RPi 3/4/5, modern SBCs |
| Linux (x86_64) | **P0** | Servers, workstations |
| macOS (x86_64 / ARM) | P1 | Go binary, same codebase |
| Windows (x86_64) | P1 | Go binary; some collectors need Windows API calls (e.g. `golang.org/x/sys/windows`); no PowerShell shim |

### 3.3 Resource Constraints

The agent **must** operate within the following envelope on the lowest-spec target:

| Constraint | Target |
|---|---|
| Idle RAM usage | < 30 MB |
| CPU usage (idle, between polls) | < 1% |
| CPU usage (during collection) | < 10% burst, < 3 seconds |
| Disk footprint (binary) | < 20 MB |
| Network (per check-in) | < 50 KB |

### 3.4 Language / Runtime

- **Go** — the entire rIOt suite (agent + server) is written in Go
- Single static binary per target platform, no runtime dependencies
- Cross-compiles to all target architectures with `GOARCH` / `GOOS` env vars
- Windows collectors use `golang.org/x/sys/windows` for platform-specific APIs (disk, network, services)
- Key libraries: `github.com/shirou/gopsutil` (cross-platform system metrics), `github.com/gorilla/websocket`, `github.com/lib/pq` (Postgres driver)

### 3.5 Configuration

Agent is configured via a single config file (YAML or TOML):

```yaml
# /etc/riot/agent.yaml
server:
  url: "http://192.168.1.100:7331"
  api_key: "<shared-secret>"
  tls_verify: true

agent:
  device_name: "pi-nas-01"       # override auto-detected hostname
  tags:
    - "homelab"
    - "storage"
  poll_interval: 60              # seconds between full telemetry push
  heartbeat_interval: 15         # seconds between lightweight heartbeat pings

collectors:
  enabled:
    - system
    - cpu
    - memory
    - disk
    - network
    - os
    - updates
    - services
```

### 3.6 Operating Modes

- **Heartbeat mode:** lightweight ping every N seconds — confirms device is alive, sends uptime + CPU/mem snapshot
- **Full telemetry mode:** complete system snapshot pushed every M seconds (default: 60s)
- **On-boot registration:** first run registers the device with the server and uploads a full hardware profile

### 3.7 Agent Lifecycle

```
Boot → Load Config → Register with Server → Start Heartbeat Loop
                                          → Start Full Telemetry Loop
                                          → Handle Shutdown Gracefully
```

- Agent should run as a **systemd service** on Linux
- Should auto-reconnect on network loss with exponential backoff
- Should buffer telemetry locally (SQLite or flat file) if server is unreachable, and flush on reconnect

---

## 4. Server & Ingestion Engine

### 4.1 Overview

The server is the backend brain of rIOt. It receives telemetry from agents, stores it, and exposes it via a REST API consumed by the dashboard.

### 4.2 Deployment

- Distributed as a **single Docker image** (or `docker-compose` stack)
- Should be runnable with a single `docker run` command or `docker-compose up`
- Persists data via a mounted Docker volume

### 4.3 Technology Stack

| Layer | Technology | Rationale |
|---|---|---|
| API Server | Go | Consistent with agent language; single binary; excellent HTTP/WebSocket support |
| Data Store | **PostgreSQL** | Reliable, scalable, great time-series support via partitioning; pairs well with TimescaleDB extension if needed later |
| Dashboard Server | **Bundled — Go serves compiled frontend static assets** | Single container: Go binary embeds the built frontend and serves it alongside the API |
| WebSocket | Go `gorilla/websocket` or stdlib `net/http` upgrade | Live telemetry push to dashboard |
| Message Queue | Not needed in Phase 1 — direct HTTP POST + WebSocket broadcast is sufficient | NATS/Redis Pub-Sub for Phase 2 if fan-out becomes a concern |

### 4.4 API Endpoints (Phase 1)

```
POST   /api/v1/devices/register         # Agent first-time registration
POST   /api/v1/devices/:id/heartbeat    # Lightweight ping
POST   /api/v1/devices/:id/telemetry    # Full telemetry push
GET    /api/v1/devices                  # List all devices
GET    /api/v1/devices/:id              # Device detail + latest snapshot
GET    /api/v1/devices/:id/history      # Historical telemetry
DELETE /api/v1/devices/:id              # Remove device

GET    /api/v1/summary                  # Fleet-wide summary stats
GET    /health                          # Server health check
```

### 4.5 Data Retention

| Data Type | Retention |
|---|---|
| Heartbeats | 7 days |
| Full telemetry snapshots | 30 days (configurable) |
| Hardware profile (static) | Forever (until device removed) |
| Events / alerts | 90 days |

---

## 5. Dashboard UI

### 5.1 Overview

A modern single-page web app served by the rIOt Server container. Should feel like a professional tool — clean, dense with information, dark-mode first.

### 5.2 Views

#### Fleet Overview (Home)
- Total device count
- Status breakdown: Online / Updating / Disconnected / Inactive (with color-coded bar similar to reference image)
- Sortable, filterable device table showing:
  - Device name (with link to detail)
  - Short UUID / device ID
  - Status badge (Online / Offline / Warning)
  - Heartbeat indicator
  - Last seen timestamp
  - OS + kernel version
  - Architecture (x86_64, arm64, arm32)
  - IP address
  - Current uptime
  - Pending updates (count badge)
  - Tags
- Global search / filter bar
- Ability to tag/group devices

#### Device Detail Page
- Full hardware profile (see Section 6)
- Live-ish metrics: CPU %, RAM %, disk usage per mount
- Network interfaces with IP + MAC
- Mounted filesystems table
- Running services list
- Installed packages (filterable)
- Pending security + kernel updates
- OS and kernel info
- Recent event log for this device
- Last seen / heartbeat history sparkline

#### Alerts / Events (Phase 1 basic)
- Device went offline
- Device came back online
- Update available (kernel, security patch)
- Disk usage > threshold
- RAM usage > threshold

### 5.3 Tech Stack

- **Frontend:** React + TypeScript (or SvelteKit)
- **Styling:** Tailwind CSS — dark-mode default
- **Charts:** Recharts or Chart.js for sparklines / gauges
- **State:** React Query or SWR for polling server API

### 5.4 Design Principles

- Dark mode by default (optional light mode toggle)
- Dense, information-rich tables (not card-soup)
- Sortable and filterable on every column
- Responsive but desktop-primary
- No external CDN dependencies — fully self-hosted

---

## 6. Data Collection Specification

This is the full telemetry spec the agent should collect and the server should store.

### 6.1 Hardware Identity (collected once at registration, refreshed on boot)

| Field | Example |
|---|---|
| Hostname | `pi-nas-01` — used as the primary display name |
| Device UUID | Auto-generated UUIDv4, persisted in `/etc/riot/id`. Stable across reboots and hostname changes. Used as the immutable internal identifier. |
| Short ID | First 8 chars of UUID (e.g. `f47ac10b`) — shown in tables as a compact unique reference |
| Architecture | `aarch64`, `x86_64`, `armv7l` |
| CPU model | `ARM Cortex-A53 @ 1.4 GHz` |
| CPU core count (physical / logical) | `4 / 4` |
| Total RAM (MB) | `3840` |
| Board / machine model | `Raspberry Pi 4 Model B Rev 1.4` |
| Serial number (if available) | From `/proc/cpuinfo` or DMI |
| BIOS / firmware version | From DMI or `/proc/device-tree` |
| BIOS date | — |
| Virtualization type | `none`, `kvm`, `docker`, `lxc`, etc. |

> **Naming strategy:** The device's **hostname** is the human-readable display name throughout the UI. The **UUIDv4** is the stable system identifier used in all API routes and database keys. The **short ID** (first 8 hex chars) is shown in table columns for quick visual reference — similar to how git shows short commit hashes. If the hostname changes, the display name updates automatically on next registration; the UUID never changes.

### 6.2 Operating System

| Field | Example |
|---|---|
| OS name | `Ubuntu 22.04.4 LTS` |
| OS ID | `ubuntu` |
| OS version | `22.04.4` |
| OS codename | `jammy` |
| Kernel version | `6.5.0-1015-raspi` |
| Kernel architecture | `aarch64` |
| Init system | `systemd` |
| Boot time | `2025-03-01T08:22:00Z` |
| Uptime (seconds) | `172800` |
| Timezone | `America/New_York` |
| Locale | `en_US.UTF-8` |

### 6.3 CPU Metrics (polled every heartbeat interval)

| Field | Example |
|---|---|
| CPU usage % (overall) | `12.4` |
| Per-core usage % | `[10.1, 14.2, 11.0, 13.8]` |
| Load average (1m / 5m / 15m) | `0.42 / 0.38 / 0.31` |
| CPU temperature (°C, if available) | `52.3` |
| CPU frequency (MHz, current / max) | `1200 / 1400` |

### 6.4 Memory (polled every heartbeat interval)

| Field | Example |
|---|---|
| Total RAM (MB) | `3840` |
| Used RAM (MB) | `1920` |
| Free RAM (MB) | `1920` |
| Cached (MB) | `640` |
| Buffers (MB) | `128` |
| Swap total / used (MB) | `2048 / 0` |
| RAM usage % | `50.0` |

### 6.5 Disk / Storage

| Field | Example |
|---|---|
| Block devices list | `sda`, `mmcblk0`, `nvme0n1` |
| Per-device: model, size, type (HDD/SSD/NVMe/SD) | `Samsung 870 EVO, 500 GB, SSD` |
| Per-device: smart status (if available) | `PASSED` / `FAILED` / `N/A` |
| Mounted filesystems | (see below) |

**Mounted Filesystem Fields:**

| Field | Example |
|---|---|
| Mount point | `/`, `/mnt/data`, `/boot/firmware` |
| Device | `/dev/sda1` |
| Filesystem type | `ext4`, `btrfs`, `vfat`, `nfs` |
| Total size (GB) | `120.0` |
| Used (GB) | `45.2` |
| Free (GB) | `74.8` |
| Usage % | `37.7` |
| Mount options | `rw,relatime` |
| Is network mount | `false` / `true` |

### 6.6 Network

| Field | Example |
|---|---|
| Per-interface: name | `eth0`, `wlan0`, `docker0` |
| Per-interface: MAC address | `dc:a6:32:1a:2b:3c` |
| Per-interface: IPv4 address(es) | `192.168.1.42/24` |
| Per-interface: IPv6 address(es) | `fe80::dea6:32ff:...` |
| Per-interface: state (UP/DOWN) | `UP` |
| Per-interface: speed (Mbps) | `1000` |
| Per-interface: bytes sent / received | `1.2 GB / 4.5 GB` |
| Default gateway | `192.168.1.1` |
| DNS servers | `['1.1.1.1', '8.8.8.8']` |
| Hostname (FQDN) | `pi-nas-01.home.local` |

### 6.7 Updates & Patch Status

| Field | Example |
|---|---|
| Package manager | `apt`, `dnf`, `pacman`, `apk`, `brew`, `winget` |
| Total installed packages | `842` |
| Pending updates (total count) | `12` |
| Pending security updates (count) | `3` |
| Pending kernel update | `true` / `false` |
| Pending kernel version | `6.5.0-1016-raspi` |
| List of pending updates | `[{name, current_ver, new_ver, is_security}]` |
| Last `apt update` / equivalent run | `2025-03-02T04:00:00Z` |
| Unattended-upgrades enabled | `true` / `false` |

### 6.8 Running Services (systemd / init.d)

| Field | Example |
|---|---|
| Service name | `nginx.service` |
| State | `active (running)` / `inactive` / `failed` |
| Enabled at boot | `true` |
| PID | `1234` |
| Memory usage (MB) | `24.5` |

Collect all enabled services + any failed services regardless of enabled state.

### 6.9 Processes (Top N)

Collect top 15 processes by CPU, top 15 by memory at each full telemetry interval:

| Field | Example |
|---|---|
| PID | `1234` |
| Name | `kworker` |
| CPU % | `4.2` |
| RAM % | `1.1` |
| RAM MB | `42` |
| User | `www-data` |
| Command | `/usr/bin/nginx -g 'daemon off;'` |

### 6.10 Docker (if installed)

| Field | Example |
|---|---|
| Docker version | `24.0.5` |
| Containers (total / running / stopped) | `10 / 7 / 3` |
| Per container: name, image, status, CPU %, RAM MB, ports | — |
| Docker volumes (count + total size) | — |

### 6.11 Kernel & Security

| Field | Example |
|---|---|
| SELinux status | `enforcing` / `permissive` / `disabled` / `N/A` |
| AppArmor status | `enabled` / `disabled` / `N/A` |
| Firewall (ufw/firewalld) status | `active` / `inactive` |
| Failed login attempts (last 24h) | `3` |
| Last successful login | `2025-03-02T19:30:00Z` |
| Logged-in users (count) | `1` |
| sudo available | `true` |
| Open ports (listening) | `[22, 80, 443, 9090]` |

---

## 7. Communication Protocol

### 7.1 Transport

- **Agent → Server:** HTTP/HTTPS POST (JSON) for registration, heartbeat, and full telemetry pushes
- **Server → Dashboard:** WebSocket for live data streaming (device status changes, live metrics, alerts)
- **Dashboard → Server:** REST API (GET) for historical data, device lists, detail pages
- Default port: **7331** (configurable)
- TLS: optional in Phase 1, supported via config

### 7.2 WebSocket Design

The server maintains a WebSocket endpoint at `/ws` for the dashboard. On connect, the server:
1. Sends a full current snapshot of all devices
2. Streams incremental updates as agents check in (heartbeats, telemetry, status changes)
3. Broadcasts alerts (device offline, threshold exceeded) in real time

This gives the dashboard a live feel similar to Glances or Netdata without polling.

```
Dashboard ──── WS /ws ────► Server
                              │
Agent ── POST /telemetry ──►  ├── writes to Postgres
                              └── broadcasts update to all WS clients
```

### 7.3 Authentication

- Each device gets its own API key, generated at registration time and stored in `/etc/riot/agent.yaml`
- Key is passed in every request header: `X-rIOt-Key: <device-key>`
- The server associates each key with a specific `device_id` — requests with an unknown key are rejected
- Keys are displayed and can be revoked per-device in the dashboard under Settings → Devices

### 7.4 Payload Format

All telemetry payloads are JSON. Example heartbeat:

```json
{
  "device_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "timestamp": "2025-03-03T14:00:00Z",
  "type": "heartbeat",
  "data": {
    "uptime": 172800,
    "cpu_percent": 12.4,
    "mem_percent": 50.0,
    "load_avg_1m": 0.42,
    "disk_root_percent": 37.7
  }
}
```

Full telemetry uses the same envelope with `"type": "telemetry"` and a larger `data` object matching Section 6.

### 7.5 Reliability

- If the server is unreachable, the agent should queue up to **100 telemetry payloads** locally (flat file / SQLite)
- On reconnect, flush queued payloads in order
- Heartbeats are **not** queued — only full telemetry snapshots

---

## 8. Security

| Concern | Approach |
|---|---|
| Agent → Server auth | Shared API key in HTTP header |
| Transport encryption | HTTPS with self-signed cert acceptable; bring-your-own cert supported |
| Data at rest | Unencrypted SQLite/Postgres in Phase 1; encryption at rest is a Phase 2 concern |
| Principle of least privilege | Agent runs as unprivileged user where possible; some collectors (SMART, open ports) require elevated privileges — document clearly |
| No inbound connections to agents | Agents always push to server; server never calls into agents (Phase 1) |

---

## 9. Deployment

### 9.1 Agent Installation

Target: one-liner install script for Linux:

```bash
curl -sSL https://your-riot-server/install.sh | sudo bash
```

Script should:
1. Detect architecture and OS
2. Download the appropriate agent binary
3. Write default config to `/etc/riot/agent.yaml`
4. Install and enable systemd service
5. Register with server

Also support manual binary install and a `.deb` / `.rpm` package for Phase 2.

### 9.2 Server Deployment

```bash
docker run -d \
  --name riot-server \
  -p 7331:7331 \
  -v riot-data:/data \
  -e RIOT_API_KEY=changeme \
  ghcr.io/yourorg/riot-server:latest
```

Or via `docker-compose.yml`:

```yaml
version: "3.9"
services:
  riot:
    image: ghcr.io/yourorg/riot-server:latest
    ports:
      - "7331:7331"
    volumes:
      - riot-data:/data
    environment:
      - RIOT_API_KEY=changeme
      - RIOT_RETENTION_DAYS=30
      - RIOT_TLS=false

volumes:
  riot-data:
```

### 9.3 Supported Agent Targets (Binaries to Ship)

| Target Triple | Devices |
|---|---|
| `linux/arm/v6` | RPi Zero, RPi 1 |
| `linux/arm/v7` | RPi 2, RPi 3 (32-bit OS) |
| `linux/arm64` | RPi 3/4/5, modern SBCs |
| `linux/amd64` | x86_64 servers |
| `linux/386` | Legacy 32-bit x86 |
| `darwin/amd64` | macOS Intel |
| `darwin/arm64` | macOS Apple Silicon |
| `windows/amd64` | Windows 10/11 |

---

## 10. Phase 1 Scope Boundaries

### In Scope ✅

- Agent: system telemetry collection (all of Section 6)
- Agent: heartbeat + full telemetry push
- Agent: local buffering on server disconnect
- Agent: systemd service + install script (Linux)
- Server: HTTP ingestion API
- Server: SQLite or Postgres storage
- Server: REST API for dashboard
- Dashboard: Fleet overview table
- Dashboard: Device detail page
- Dashboard: Basic online/offline alerting

### Out of Scope ❌ (Phase 2)

- Remote command execution
- Remote package installation / updates
- Config push to agents
- Multi-user auth (single-user / API-key only in Phase 1)
- Mobile app
- External alerting (PagerDuty, Slack, email) — Phase 2
- Agent auto-update mechanism
- RBAC / teams

---

## 11. Phase 2 Preview (Out of Scope Now)

- **Remote management:** execute commands, restart services, trigger updates via dashboard
- **Fleet actions:** bulk update, bulk restart
- **Alerting integrations:** Slack, email, webhook, Pushover
- **Custom metrics:** agent plugin system for user-defined collectors
- **Agent auto-update:** server pushes new agent binaries
- **Multi-user auth:** login, roles, teams
- **Ansible/Terraform-style config push**
- **InfluxDB / Prometheus export endpoint**

---

## 12. Non-Functional Requirements

| Requirement | Target |
|---|---|
| Agent startup time | < 2 seconds |
| Agent memory (RPi 1 / <1 GB target) | < 30 MB RSS |
| Dashboard page load | < 2 seconds on LAN |
| Server ingest throughput | Handle 500 devices pushing every 60s without issue |
| Server uptime | Designed for 24/7 unattended operation |
| Time to first value (TTFV) | New device appears in dashboard within 30 seconds of agent start |
| Log verbosity | Configurable: error / warn / info / debug |
| Offline resilience | Agent continues collecting + buffering indefinitely with no server |

---

## 13. Resolved Architecture Decisions

| # | Question | Decision |
|---|---|---|
| 1 | SQLite vs Postgres? | **Postgres** — runs as companion container in docker-compose |
| 2 | Dashboard bundled or separate container? | **Bundled** — Go binary embeds compiled frontend via `go:embed`; single container deploy |
| 3 | WebSocket or polling for dashboard? | **WebSocket** — server pushes live updates to dashboard as agents check in |
| 4 | Windows agent approach? | **Go binary** — same codebase, Windows-specific collectors via `golang.org/x/sys/windows` |
| 5 | Fleet-wide or per-device API keys? | **Per-device keys** — generated at registration, revocable individually from dashboard |
| 6 | Device naming strategy? | **Hostname as display name + UUIDv4 as stable internal ID + 8-char short ID for tables** |

---

*Built with rIOt — Remote Infrastructure Oversight Tool*

<p align="center">
  <img src="docs/logo.png" alt="rIOt logo" width="200" />
</p>

<h1 align="center">rIOt — Remote Infrastructure Oversight Tool</h1>

<p align="center">
  Self-hosted infrastructure monitoring for homelab environments.<br/>
  Deploy a lightweight agent on each device and get a single-pane-of-glass view of your entire fleet through a real-time web dashboard.
</p>

<p align="center">
  <a href="https://rbretschneider.github.io/rIOt/"><strong>Live Demo</strong></a>
</p>

> **README last updated for v2.21.1**

## Features

- **Lightweight agent** — single static binary, under 30 MB RAM, runs on everything from a Raspberry Pi Zero to a Threadripper workstation
- **Rich telemetry** — CPU, memory, disk, network, services, processes, Docker containers, pending updates, security status, journal logs, NUT UPS monitoring, reverse proxy/web server inspection, USB device inventory
- **Docker container management** — dedicated per-device container dashboard with search, grouping via `riot.*` labels, real-time container events, image update detection, remote start/stop/restart/update, and optional remote terminal (exec into running containers from the browser)
- **Real-time dashboard** — dark-mode React UI with live WebSocket updates
- **Offline resilience** — agent buffers telemetry locally when the server is unreachable; resilient DNS caching with disk persistence for surviving DNS outages
- **Zero-config setup** — a setup wizard configures the admin password, TLS, and mTLS on first visit; agents auto-pin the server certificate (SSH-like TOFU)
- **Simple deployment** — single `docker compose up` for the server, one-liner install for agents
- **Open registration** — devices register automatically; optionally require a registration key via Settings
- **Admin authentication** — password-protected dashboard with JWT session cookies and in-app password changes
- **UPS monitoring** — auto-detects NUT `upsc`, displays battery charge, load, voltage, runtime, and status; alerts on battery switchover and low battery; fleet status dot turns yellow when a device is on battery power
- **USB device monitoring** — enumerates all connected USB devices with vendor/product names (resolved via sysfs + usb.ids database), serial numbers, device class, and speed; one-click alert creation to monitor for device disconnection (e.g. Coral TPU, Z-Wave stick, UPS HID)
- **Advanced alerting** — threshold-based alerts on numeric metrics plus state-based monitoring for services, network interfaces, processes, USB devices, and UPS power events; one-click alert creation from device view; pre-built templates
- **Event acknowledgement** — unread alert badge on the Alerts tab with per-event and bulk acknowledgement
- **Notification channels** — alert delivery via ntfy and webhooks, with test-send support, delivery logging, and automatic retry queue
- **mTLS device authentication** — optional certificate-based device identity with automatic CA management, bootstrap key enrollment, and zero external tooling
- **Uptime probes** — scheduled HTTP, DNS, and ping/ICMP probes with history and status tracking
- **Fleet management** — agent version overview, bulk update, and patch status across devices
- **Remote commands** — send commands to agents from the dashboard: Docker start/stop/restart/update, OS patching, enable automatic updates, agent update, system reboot (with per-command permission controls)
- **Host terminal** — browser-based SSH-like shell access to devices via WebSocket relay (opt-in per agent)
- **Web server monitoring** — auto-detects nginx and Caddy reverse proxies; shows sites/virtual hosts, SSL certificates with expiry tracking, upstreams/backends, and security config (rate limiting, access controls, security headers); certificate expiry alerts
- **Security scoring** — per-device 0–100 hardening score based on CIS-inspired checks across access control, patching, network, Docker, and system categories; interactive detail modal with per-finding remediation guidance and one-click fix buttons for actionable issues (enable auto-updates, apply patches)
- **Security overview** — fleet-wide view of SELinux/AppArmor, firewall, open ports, failed logins
- **Server log viewer** — browse and search server logs directly from the dashboard
- **Per-device API keys** — generated at registration, individually revocable and rotatable
- **TLS support** — self-signed (auto-generated), Let's Encrypt autocert, or manual cert/key files
- **Dead man's switch** — optional agent heartbeat to external healthcheck services (e.g., Healthchecks.io)

## Architecture

```
Agent (Go)  ──HTTP POST──►  Server (Go + embedded React UI)  ──►  PostgreSQL
                                   │
Dashboard (browser)  ◄──WebSocket──┘
```

The server is a single Go binary with the compiled React frontend embedded. No reverse proxy, no separate web container. PostgreSQL is the only additional service.

---

## Server Installation

### Docker Compose (recommended)

```bash
# 1. Grab the compose file and .env template
curl -O https://raw.githubusercontent.com/rbretschneider/rIOt/main/docker-compose.prod.yml
curl -O https://raw.githubusercontent.com/rbretschneider/rIOt/main/.env.example
cp .env.example .env

# 2. Edit .env — at minimum, change the Postgres password
vi .env

# 3. Start everything
docker compose -f docker-compose.prod.yml up -d
```

On first launch the server starts in **setup mode**. Open `http://<server-ip>:7331` in a browser and the setup wizard will walk you through:
- Setting an admin password
- Configuring TLS (self-signed, Let's Encrypt, manual cert, or none)
- Enabling mTLS device authentication (optional)

Everything else (JWT secret, TLS certs, admin password hash) is stored in the database — no extra env vars needed.

#### `.env.example`

```bash
# PostgreSQL credentials (change the password!)
POSTGRES_USER=riot
POSTGRES_PASSWORD=riot
POSTGRES_DB=riot

# Optional overrides (uncomment if needed)
# RIOT_RETENTION_DAYS=30
# RIOT_ALLOWED_ORIGINS=https://riot.example.com
```

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
  ghcr.io/rbretschneider/riot-server:latest
```

Then open the browser for the setup wizard.

### Server Environment Variables

Most settings are configured through the setup wizard and stored in the database. The following environment variables are available as overrides:

| Variable | Default | Description |
|---|---|---|
| `RIOT_DB_URL` | `postgres://riot:riot@localhost:5432/riot?sslmode=disable` | PostgreSQL connection string |
| `RIOT_PORT` | `7331` | HTTP listen port |
| `RIOT_RETENTION_DAYS` | `30` | Days to keep telemetry snapshots |
| `RIOT_ALLOWED_ORIGINS` | — | Comma-separated CORS allowed origins |
| `RIOT_GITHUB_REPO` | `rbretschneider/rIOt` | GitHub `owner/repo` for update checks |
| `RIOT_API_KEY` | — | Registration key override (if set, agents must present this key to register; also configurable in Settings) |
| `RIOT_ADMIN_PASSWORD` | — | Admin password override (bypasses wizard; bcrypt-hashed at startup) |
| `RIOT_JWT_SECRET` | auto-generated | JWT session secret (auto-generated if omitted; set for stable sessions across restarts) |
| `RIOT_TLS_ENABLED` | `false` | Enable TLS (`true` or `1`); overrides wizard setting |
| `RIOT_TLS_DOMAIN` | — | Let's Encrypt autocert domain (implies TLS enabled) |
| `RIOT_TLS_CERT_DIR` | — | Autocert cache directory |
| `RIOT_TLS_CERT_FILE` | — | Manual TLS certificate file path |
| `RIOT_TLS_KEY_FILE` | — | Manual TLS key file path |
| `RIOT_MTLS_ENABLED` | `false` | Enable mTLS device authentication (see [mTLS](#mtls-device-authentication)) |

---

## Agent Installation

### One-liner (Linux / macOS)

```bash
curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://<server-ip>:7331
```

This will:
1. Detect your architecture
2. Download the correct agent binary from GitHub Releases
3. Write a default config to `/etc/riot/agent.yaml`
4. On first connect, automatically pin the server's TLS certificate (TOFU)
5. Install and start a systemd service (Linux) or print manual run instructions (macOS)

**Optional flags:**

```bash
# Verify server cert fingerprint on first connect (shown in Settings > General)
curl ... | sudo bash -s -- https://server:7331 --fingerprint SHA256:xxxx

# Provide a registration key (if the server requires one)
curl ... | sudo bash -s -- https://server:7331 --key mykey

# Install a specific version
curl ... | sudo bash -s -- https://server:7331 --version 2.3.0
```

By default, registration is **open** — any device that can reach the server can register. You can optionally set a registration key in **Settings > General** to gate registration.

### Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/uninstall.sh | sudo bash
```

Add `--keep-config` to preserve `/etc/riot` (agent config and device ID).

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
     url: "https://<server-ip>:7331"
     tls_verify: true

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
       - logs
       - ups
       - webservers
       - usb

   docker:
     enabled: "auto"               # "auto" (detect), "true", or "false"
     collect_stats: true            # per-container CPU/memory stats
     terminal_enabled: false        # set to true to allow remote exec from dashboard
     check_updates: true            # check registries for newer container images

   commands:
     allow_reboot: false            # set to true to allow remote reboot
     allow_patching: false          # set to true to allow remote OS updates and enable auto-updates

   host_terminal:
     enabled: false                 # set to true to allow host shell access
   ```

   On first HTTPS connect, the agent will automatically fetch and pin the server's certificate (TOFU). To verify the fingerprint up front, add `server_cert_pin: "SHA256:xxxx"` (available in Settings > General).

4. Run the agent:

   ```bash
   riot-agent -config /etc/riot/agent.yaml
   ```

### Windows

Download `riot-agent-windows-amd64.exe` from [Releases](https://github.com/rbretschneider/rIOt/releases) and create a config file at `%PROGRAMDATA%\riot\agent.yaml` with the same format as above. Run from a terminal or set up as a Windows service using [NSSM](https://nssm.cc/) or `sc.exe`.

### Sudoers Rules

The installer creates `/etc/sudoers.d/riot-agent` with least-privilege rules that let the `riot` user perform specific operations as root without a password. Each rule is locked to an exact command — no shell access, no wildcard escalation.

| Rule | Used By | Purpose |
|---|---|---|
| `/usr/bin/apt-get update` | Remote patching | Refresh APT package index |
| `/usr/bin/apt-get -y upgrade ...` | Remote patching | Install available package updates (Debian/Ubuntu) |
| `/usr/bin/apt-get -y dist-upgrade ...` | Remote patching | Install updates including dependency changes |
| `/usr/bin/dnf makecache` | Remote patching | Refresh DNF package cache (Fedora/RHEL) |
| `/usr/bin/dnf -y update` | Remote patching | Install available package updates |
| `/usr/bin/dnf -y --security update` | Remote patching | Install security-only updates |
| `/usr/bin/systemctl reboot` | Remote reboot | Reboot the device from the dashboard |
| `/bin/sh -c mv ... && cp ... && chmod ...` | Agent self-update | Atomically swap the agent binary on disk |
| `/usr/bin/systemd-run --unit=riot-agent-update sh -c *` | Agent self-update | Run the update in a transient systemd unit |
| `/usr/bin/systemctl reset-failed riot-agent-update` | Agent self-update | Clear failed state from a previous update unit |
| `/usr/sbin/nginx -t` | Web server collector | Test nginx config validity and extract config path |
| `/usr/sbin/nginx -T` | Web server collector | Dump full resolved nginx config (needed to read SSL cert references) |

**Existing installs**: If you add the `webservers` collector to an existing device, add the nginx sudoers rules manually:

```bash
sudo tee -a /etc/sudoers.d/riot-agent > /dev/null << 'EOF'
riot ALL=(root) NOPASSWD: /usr/sbin/nginx -t
riot ALL=(root) NOPASSWD: /usr/sbin/nginx -T
EOF
sudo visudo -cf /etc/sudoers.d/riot-agent
```

New installs via `install.sh` include all rules automatically.

### Agent Config Reference

| Field | Default | Description |
|---|---|---|
| `server.url` | `http://localhost:7331` | rIOt server URL |
| `server.api_key` | — | Registration key (only needed if the server requires one; per-device key is generated after registration) |
| `server.tls_verify` | `true` | Verify TLS certificates |
| `server.server_cert_pin` | — | SHA256 fingerprint for TOFU verification (auto-populated on first connect) |
| `server.ca_cert_file` | — | Custom CA certificate for TLS verification (auto-populated by TOFU) |
| `server.client_cert` | — | mTLS client certificate path (auto-populated after enrollment) |
| `server.client_key` | — | mTLS client key path (auto-populated after enrollment) |
| `server.bootstrap_key` | — | Single-use mTLS enrollment key (cleared after enrollment) |
| `agent.device_name` | hostname | Display name override |
| `agent.tags` | `[]` | Tags for grouping/filtering |
| `agent.poll_interval` | `60` | Seconds between full telemetry pushes |
| `agent.heartbeat_interval` | `15` | Seconds between heartbeat pings |
| `agent.auto_update` | `true` | Automatically install agent updates when available |
| `collectors.enabled` | all | List of collectors to run |
| `docker.enabled` | `auto` | Docker collection mode: `auto` (detect), `true`, `false` |
| `docker.socket_path` | auto-detect | Override the Docker socket path |
| `docker.collect_stats` | `true` | Collect per-container CPU/memory stats |
| `docker.terminal_enabled` | `false` | Allow remote `docker exec` from the dashboard |
| `docker.check_updates` | `true` | Check container registries for newer images (30-min cache) |
| `commands.allow_reboot` | `false` | Allow remote reboot command from the dashboard |
| `commands.allow_patching` | `false` | Allow remote OS patching and enable-auto-updates commands from the dashboard |
| `host_terminal.enabled` | `false` | Allow browser-based host shell access from the dashboard |
| `host_terminal.shell` | auto-detect | Override default shell (e.g., `/bin/bash`) |
| `dns_cache.refresh_interval_seconds` | `1800` | How often to refresh cached DNS entries (seconds) |
| `dns_cache.staleness_warning_hours` | `24` | Hours before a stale DNS cache entry triggers a warning |
| `dns_cache.cache_file` | OS default | Path to the DNS cache file |
| `deadman.enabled` | `false` | Enable dead man's switch heartbeat |
| `deadman.url` | — | Healthcheck ping URL (e.g. `https://hc-ping.com/<uuid>`) |
| `deadman.interval_seconds` | `60` | Seconds between healthcheck pings |

### Available Collectors

| Collector | Data Collected |
|---|---|
| `system` | Hardware identity — CPU model, cores, RAM, board model, serial, BIOS, virtualization |
| `cpu` | Usage %, per-core, load average, temperature, frequency |
| `memory` | RAM total/used/free/cached/buffers, swap, usage % |
| `disk` | Block devices, mounted filesystems with usage |
| `network` | Interfaces, IPs, MACs, state, bytes tx/rx, default gateway, DNS servers |
| `os_info` | OS name/version, kernel, uptime, timezone, locale, init system |
| `updates` | Package manager, pending updates, security updates, kernel update status, unattended-upgrades status |
| `services` | systemd services — name, state, enabled, PID, memory usage |
| `processes` | Top 15 by CPU, top 15 by memory — PID, name, CPU %, memory %, user |
| `docker` | Docker containers — name, image, status, ports, CPU/mem stats, `riot.*` labels, real-time events, image update detection |
| `security` | SELinux/AppArmor, firewall, open ports, failed logins, logged-in users |
| `logs` | Recent journald entries (info and above); auto-deduplicates on the server |
| `ups` | NUT UPS status — battery charge, runtime, load, voltage, model (requires `upsc`) |
| `webservers` | Reverse proxy detection (nginx, Caddy) — sites, SSL certificates, upstreams, security config (requires nginx sudoers rules; see below) |
| `usb` | Connected USB devices — vendor/product names (via sysfs + `/usr/share/hwdata/usb.ids` fallback), serial numbers, device class, speed; supports disconnect alerts |

**Note:** The `usb` collector is Linux-only. It reads from `/sys/bus/usb/devices/` and uses the system `usb.ids` database (shipped with `usbutils` or `hwdata`) to resolve vendor/product names for devices that don't self-report (e.g., Google Coral TPU). No additional packages or permissions are required.

**Note:** Existing agent installs use a whitelist from the installer — new collectors like `usb` are **not** picked up automatically. You must add the collector name to `collectors.enabled` in each agent's `/etc/riot/agent.yaml` and restart the agent.

---

## Alerting

rIOt provides flexible alerting with two modes:

### Threshold Alerts

Traditional numeric alerts — fire when a metric crosses a threshold:

- CPU usage > 90%
- Memory usage > 90%
- Disk usage > 90%
- Updates available > 0

### State Alerts

Monitor service, network, process, USB, and UPS state changes:

- **Service monitoring** — alert when a systemd service enters a specific state (stopped, failed, etc.)
- **Network interface monitoring** — alert when a NIC goes down
- **Process monitoring** — alert when a named process is not running
- **USB device monitoring** — alert when a USB device disappears (matched by vendor:product ID, serial number, or device description)
- **UPS monitoring** — alert when UPS switches to battery or battery charge drops below threshold
- **Certificate expiry** — warning when an SSL certificate has fewer than 30 days remaining; critical when expired

### Alert Templates

Pre-built templates are available in Settings > Alert Rules > "Create from Template" for common scenarios. Templates pre-fill the metric, operator, threshold, and severity — just add a target name if needed.

### One-Click Alert Creation

From the device detail view, click the alert icon next to any service, process, network interface, USB device, or UPS to instantly create a targeted alert rule.

### Event Acknowledgement

The Alerts tab in the navigation bar shows a red badge with the count of unacknowledged warning/critical events. Events can be acknowledged individually or in bulk from the Alerts page. The badge updates in real-time via WebSocket.

---

## mTLS Device Authentication

rIOt supports optional mTLS (mutual TLS) for certificate-based device authentication. The entire process is managed through the dashboard — no external tools like `openssl` are required.

### How It Works

1. The server runs a private ECDSA P-256 Certificate Authority, auto-generated on first start and stored in the database
2. Admins create time-limited bootstrap keys from the dashboard
3. Agents enroll by presenting a bootstrap key — the server signs a client certificate and returns it
4. All subsequent agent communication uses the client certificate for authentication
5. Certificates can be viewed and revoked from the dashboard

### Setup

**1. Enable mTLS on the server:**

Add the environment variable to your Docker Compose or server startup:

```yaml
environment:
  - RIOT_MTLS_ENABLED=true
  - RIOT_TLS_ENABLED=true
  - RIOT_TLS_CERT_FILE=/path/to/server.crt
  - RIOT_TLS_KEY_FILE=/path/to/server.key
```

The server automatically generates and stores a CA on first start. No manual CA creation needed.

**2. Create a bootstrap key in the dashboard:**

Navigate to **Settings > Certificates > Bootstrap Keys** and click **Create Key**. Set an optional label and expiry (default: 24 hours). The plaintext key is shown exactly once — copy it.

**3. Configure the agent:**

```yaml
server:
  url: "https://<server-ip>:7331"
  bootstrap_key: "<key-from-dashboard>"
```

**4. Start the agent:**

On first run, the agent automatically:
- Generates an ECDSA P-256 key pair
- Creates a Certificate Signing Request (CSR)
- Sends the CSR + bootstrap key to the server
- Receives and saves the signed certificate and CA cert
- Updates its config file (clears the bootstrap key, sets cert paths)
- Connects using mTLS for all future communication

No manual steps required after writing the initial config — the agent handles everything.

### Certificate Management

From the dashboard (**Settings > Certificates**):

- **CA info** — download the CA certificate
- **Device certificates** — view all issued certs with device ID, serial, expiry, and status
- **Revoke** — immediately revoke a device certificate
- **Bootstrap keys** — create, list, and delete enrollment keys

---

## DNS Resilience

The agent includes a resilient DNS resolver that caches DNS lookups to disk. If DNS becomes unavailable, the agent falls back to cached IPs to maintain connectivity with the server.

- **Automatic caching** — DNS results are cached and refreshed in the background (default: every 30 minutes)
- **Disk persistence** — cache survives agent restarts (stored at `/etc/riot/dns-cache.json` or `%PROGRAMDATA%\riot\dns-cache.json`)
- **Staleness warnings** — logs warnings when cached entries become stale (24h warning, 72h critical)
- **IP fallback** — on DNS failure, the agent tries cached IPs with the original hostname in the `Host` header and TLS `ServerName`

### Notification Retry Queue

Failed notification deliveries (ntfy, webhook) are queued to disk and retried automatically. The queue persists across server restarts with a configurable maximum size (default: 100 items).

### Dead Man's Switch

The agent can send periodic pings to an external healthcheck service (e.g., Healthchecks.io, Uptime Kuma) to detect agent failures from outside your infrastructure.

```yaml
deadman:
  enabled: true
  url: "https://hc-ping.com/<your-uuid>"
  interval_seconds: 60
```

On consecutive failures, the agent increases retry frequency. When connectivity recovers, it resumes the normal interval.

---

## Notification Channels

Alert delivery is supported via:

- **ntfy** — push notifications via [ntfy.sh](https://ntfy.sh) or a self-hosted ntfy server
- **Webhooks** — JSON POST to any URL with custom headers

Configure channels in **Settings > Notifications**. Each channel can be tested with a one-click test notification. All delivery attempts are logged in **Settings > Notifications > Delivery Log**.

---

## API

All endpoints are under `/api/v1/`. Agent endpoints require the `X-rIOt-Key` header (or mTLS client certificate). Dashboard endpoints require admin authentication (JWT cookie).

### Public

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Server health check |
| `POST` | `/api/v1/auth/login` | Admin login (returns JWT cookie) |
| `POST` | `/api/v1/auth/logout` | Clear session cookie |
| `POST` | `/api/v1/auth/change-password` | Change admin password |
| `GET` | `/api/v1/auth/check` | Check authentication status |
| `GET` | `/api/v1/server-cert` | Server TLS certificate + fingerprint (for agent TOFU) |
| `GET` | `/api/v1/setup/status` | Setup wizard status |
| `POST` | `/api/v1/setup/complete` | Complete setup wizard |

### Agent (device key or mTLS auth)

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/devices/register` | Register a new device |
| `POST` | `/api/v1/devices/:id/heartbeat` | Lightweight heartbeat |
| `POST` | `/api/v1/devices/:id/telemetry` | Full telemetry push |
| `POST` | `/api/v1/devices/:id/docker-events` | Real-time Docker container events |
| `GET` | `/api/v1/update/check` | Agent update check |

### Enrollment (mTLS)

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/enroll` | Enroll a device with bootstrap key + CSR |
| `GET` | `/api/v1/ca.pem` | Download CA certificate |

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
| `GET` | `/api/v1/events/unread-count` | Count of unacknowledged events |
| `POST` | `/api/v1/events/:id/acknowledge` | Acknowledge a single event |
| `POST` | `/api/v1/events/acknowledge-all` | Acknowledge all events |
| `GET` | `/api/v1/update/server` | Server update check |
| `GET/POST/PUT/DELETE` | `/api/v1/settings/alert-rules[/:id]` | Alert rule CRUD |
| `GET` | `/api/v1/settings/alert-templates` | List alert templates |
| `GET/POST/PUT/DELETE` | `/api/v1/settings/notification-channels[/:id]` | Notification channel CRUD |
| `POST` | `/api/v1/settings/notification-channels/:id/test` | Test notification channel |
| `GET` | `/api/v1/settings/notifications/log` | Notification delivery log |
| `GET` | `/api/v1/settings/registration` | Get registration key setting |
| `PUT` | `/api/v1/settings/registration` | Set registration key (empty = open registration) |
| `GET` | `/api/v1/settings/certs` | List device certificates |
| `POST` | `/api/v1/settings/certs/:serial/revoke` | Revoke a device certificate |
| `GET/POST/DELETE` | `/api/v1/settings/bootstrap-keys[/:hash]` | Bootstrap key CRUD |
| `GET` | `/api/v1/fleet/agent-versions` | Agent version summary |
| `GET` | `/api/v1/fleet/patch-status` | Fleet patch status overview |
| `POST` | `/api/v1/fleet/bulk-update` | Bulk update agents |
| `POST` | `/api/v1/fleet/bulk-patch` | Bulk patch devices |
| `GET` | `/api/v1/security/overview` | Security overview |
| `GET` | `/api/v1/security/devices` | Per-device security details |
| `GET/POST` | `/api/v1/settings/logs` | Server log viewer |
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
| Notification log | 90 days |
| Probe results | 30 days (configurable via `RIOT_RETENTION_DAYS`) |
| Device registry | Forever (until manually deleted) |

A background worker purges expired data hourly.

---

## Docker Container Management

Devices running Docker get a dedicated container dashboard in the web UI with search, filtering, and grouped container cards.

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

### Container Updates

The agent periodically checks container image registries for newer versions (every 30 minutes). Containers with available updates are highlighted in the dashboard with an amber "Update" badge.

From the container detail panel, click **Update** to pull the new image and recreate the container:

- **Compose-managed containers** (detected via standard Docker Compose labels): uses `docker compose pull` + `docker compose up -d` for safe recreation
- **Standalone containers**: pulls the new image, then stops, removes, and recreates the container with the same configuration

This works for any container, including the rIOt server itself — the agent on the server's host can update the server container.

To disable update checking, set `docker.check_updates: false` in the agent config.

### Host Terminal

The agent can optionally allow browser-based SSH-like shell access to the host device. This is disabled by default for security.

To enable, set `host_terminal.enabled: true` in the agent config (`/etc/riot/agent.yaml`):

```yaml
host_terminal:
  enabled: true
  shell: "/bin/bash"  # optional: override default shell
```

Access the terminal from the device detail page via the Terminal link.

### Remote Terminal

The agent can optionally allow remote `docker exec` from the dashboard, providing a browser-based terminal into running containers. This is disabled by default for security.

To enable, set `docker.terminal_enabled: true` in the agent config (`/etc/riot/agent.yaml`):

```yaml
docker:
  terminal_enabled: true
```

The terminal uses a WebSocket relay: the browser connects to the server, which proxies through the agent's persistent WebSocket connection into a `docker exec` session on the target container.

### Real-time Container Events

When Docker is available, the agent watches the Docker event stream and forwards container lifecycle events (start, stop, die, OOM, pause, unpause, create, destroy) to the server in real-time. These appear in the dashboard event feed and trigger alerts for OOM kills.

---

## Security Score

Each device receives a 0–100 security hardening score computed from its telemetry. The score appears as a circular gauge in the device header and clicking it opens a detail modal with per-finding explanations, severity levels, and remediation guidance.

### Categories & Checks

| Category | Checks | Weight |
|---|---|---|
| **Access Control** | Firewall active, mandatory access control (AppArmor/SELinux), failed login count, active sessions | 22 pts |
| **Patching** | Pending security updates, total pending updates, kernel update pending, automatic updates enabled | 25 pts |
| **Network** | Open port count, insecure ports (FTP/Telnet), TLS certificate validity, web server config, security headers, rate limiting | 25 pts |
| **Docker** | Restart policies, health checks, memory limits, sensitive volume mounts | 15 pts (skipped if no Docker) |
| **System** | Failed services, system uptime, DNS configuration | 10 pts |

Scores are normalized to 0–100 and assigned a letter grade: **A** (≥90), **B** (≥75), **C** (≥60), **D** (≥40), **F** (<40). Categories are omitted when the corresponding collector data is absent, so the score only reflects what can actually be evaluated.

### One-Click Fixes

Certain failing checks can be fixed directly from the modal:

| Finding | Button | Command Sent | Requires |
|---|---|---|---|
| Automatic updates not configured | **Enable** | `enable_auto_updates` — installs and configures `unattended-upgrades` (Debian/Ubuntu) or `dnf-automatic` (RHEL/Fedora) | `commands.allow_patching: true` |
| Pending security updates | **Patch Now** | `os_update` with `mode: security` | `commands.allow_patching: true` |
| Pending package updates | **Update All** | `os_update` with `mode: full` | `commands.allow_patching: true` |

Fix buttons only appear when the device is online and connected. Each button requires confirmation before executing.

---

## License

MIT

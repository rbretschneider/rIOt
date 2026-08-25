<p align="center">
  <img src="docs/logo.png" alt="rIOt logo" width="200" />
</p>

<h1 align="center">rIOt — Remote Infrastructure Oversight Tool</h1>

<p align="center">
  Self-hosted infrastructure monitoring for homelab environments.<br/>
  Deploy a lightweight agent on each device and get a single-pane-of-glass view of your entire fleet through a real-time web dashboard.
</p>

<h4>Please note, this is in active dev and still receiving breaking changes. It is relatively stable at version 2.24 but still may receive breaking changes. If you installed prior to v2.24 i recommend starting over as there were several issues resolved.</h4>

<p align="center">
  <a href="https://rbretschneider.github.io/rIOt/"><strong>Live Demo</strong></a>
</p>

> **README last updated for v2.38.0**

## Features

- **Lightweight agent** — single static binary, under 30 MB RAM, runs on everything from a Raspberry Pi Zero to a Threadripper workstation
- **Rich telemetry** — CPU, memory, disk, network, services, processes, Docker containers, pending updates, security status, journal logs, NUT UPS monitoring, reverse proxy/web server inspection, USB device inventory, hardware details (PCI devices, disk drives, serial ports, GPUs), cron jobs and scheduled tasks, NVIDIA GPU runtime metrics (temperature, utilization, memory, fan speed, power draw)
- **Device summary export** — export a device's hardware and software inventory as a Markdown file or copy it to the clipboard directly from the device detail page; the export covers system identity, OS, CPU, memory, storage, GPUs, network interfaces, USB devices, Docker containers, and UPS — generated on demand from the most recent telemetry snapshot
- **Docker container management** — dedicated per-device container dashboard with search, grouping via `riot.*` labels, real-time container events, image update detection, remote start/stop/restart/update, container log viewer, and optional remote terminal (exec into running containers from the browser)
- **Real-time dashboard** — dark-mode React UI with live WebSocket updates
- **Offline resilience** — agent buffers telemetry locally when the server is unreachable; resilient DNS caching with disk persistence for surviving DNS outages
- **Zero-config setup** — a setup wizard configures the admin password, TLS, and mTLS on first visit; agents auto-pin the server certificate (SSH-like TOFU)
- **Simple deployment** — single `docker compose up` for the server, one-liner install for agents
- **Open registration** — devices register automatically; optionally require a registration key via Settings
- **Admin authentication** — password-protected dashboard with JWT session cookies and in-app password changes
- **UPS monitoring** — auto-detects NUT `upsc`, displays battery charge, load, voltage, runtime, and status; alerts on battery switchover and low battery; fleet status dot turns yellow when a device is on battery power
- **USB device monitoring** — enumerates all connected USB devices with vendor/product names (resolved via sysfs + usb.ids database), serial numbers, device class, and speed; one-click alert creation to monitor for device disconnection (e.g. Coral TPU, Z-Wave stick, UPS HID)
- **Advanced alerting** — threshold-based alerts on numeric metrics plus state-based monitoring for services, network interfaces, processes, USB devices, and UPS power events; near-real-time SSH/sudo/console/su authentication failure alerting (Linux; ≤60s latency, journald origin-filtered); include/exclude device scoping on all alert rules; one-click alert creation from device view; pre-built templates
- **Event acknowledgement** — unread alert badge on the Alerts tab with per-event and bulk acknowledgement
- **Notification channels** — alert delivery via email (SMTP), Telegram, Discord, Slack, ntfy, Pushover, Gotify, and webhooks, with test-send support, delivery logging, and automatic retry queue
- **mTLS device authentication** — optional certificate-based device identity with automatic CA management, bootstrap key enrollment, automatic certificate renewal (agents renew when <30 days remain), server TLS regeneration from the dashboard, server-enforced cert + API key auth on all device routes, and zero external tooling
- **Uptime probes** — unified Probes page showing server probes (HTTP, DNS, ping/ICMP — run by the server) and device probes (shell, HTTP, port check, file check, container exec — run by agents) in two separate sections; per-device probe assertion templates for response validation; full history and status tracking for both types
- **Fleet management** — agent version overview, bulk update, and patch status across devices
- **Remote commands** — send commands to agents from the dashboard: Docker start/stop/restart/update, OS patching, enable automatic updates, agent update, system reboot, system shutdown (with per-command permission controls); all commands generate informational events with container context for full audit trail; command output capture with per-device activity log
- **Host terminal** — browser-based SSH-like shell access to devices via WebSocket relay (opt-in per agent); visibility toggleable via Settings > Features
- **Feature toggles** — 16 individually toggleable dashboard features (including device terminal and Docker terminal) via Settings > Features with search/filter; toggles control UI visibility only — agents continue collecting all data
- **Web server monitoring** — auto-detects nginx, Caddy, and Ferron reverse proxies; shows sites/virtual hosts, SSL certificates with expiry tracking, upstreams/backends, and security config (rate limiting, access controls, security headers); certificate expiry alerts
- **Security page** — dedicated security hub with per-device hardening scores (0–100, letter grade A–F, color-coded), fleet average score banner, pending security update counts, automatic OS update status, SSL/TLS certificate expiry summary (devices running the `webservers` collector), firewall, SELinux/AppArmor, open ports, and failed logins; table sortable by score, failed logins, and pending security updates (worst scores shown first by default); click any score to open the detail modal with per-finding remediation guidance and one-click fix buttons
- **Server log viewer** — browse and search server logs directly from the dashboard
- **Per-device API keys** — generated at registration, individually revocable and rotatable
- **TLS support** — self-signed (auto-generated), Let's Encrypt autocert, or manual cert/key files; configurable Subject Alternative Names (SANs) for remote access via DDNS
- **Activity log** — per-device command history with detail modal showing full output, parameters, duration, and exit code
- **Temperature monitoring** — color-coded CPU and disk drive temperatures in the dashboard (green/yellow/orange/red thresholds)
- **Agent diagnostics** — `riot-agent doctor` command for troubleshooting connectivity, TLS, permissions, and collector health
- **Automation scheduling** — configurable maintenance windows for OS patching and Docker updates with quick presets (Off-Hours, Midnight, Early Morning, Business Hours) or custom time windows; manage via Settings > Agents
- **Reboot-class package gating** — optionally hold GPU driver and kernel packages at the OS level (`apt-mark hold` / dnf5 `excludepkgs`) so they can only be applied during a maintenance window, followed by an automatic reboot, preventing out-of-band driver/kernel upgrades from breaking GPU containers mid-day; two-sided opt-in, off by default, with reboot-required detection and GPU-container blast-radius visibility ([details](#reboot-class-package-gating))
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
| `GOMEMLIMIT` | `2GiB` | Go runtime memory limit. Controls how aggressively the garbage collector runs. The default is appropriate for most deployments; increase if monitoring many devices with 50+ containers each, or decrease on memory-constrained hosts. Too low causes GC thrashing (high CPU). |

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
# mTLS: enroll the agent with a bootstrap key (required when mTLS is enabled on the server)
curl ... | sudo bash -s -- https://server:7331 --bootstrap-key <KEY>

# Verify server cert fingerprint on first connect (shown in Settings > General)
curl ... | sudo bash -s -- https://server:7331 --fingerprint SHA256:xxxx

# Provide a registration key (if the server requires one)
curl ... | sudo bash -s -- https://server:7331 --key mykey

# Install a specific version
curl ... | sudo bash -s -- https://server:7331 --version 2.3.0

# Combine flags as needed
curl ... | sudo bash -s -- https://server:7331 --bootstrap-key <KEY> --key mykey --fingerprint SHA256:xxxx
```

By default, registration is **open** — any device that can reach the server can register. You can optionally set a registration key in **Settings > General** to gate registration.

### Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/uninstall.sh | sudo bash
```

Add `--keep-config` to preserve `/etc/riot` (agent config and device ID).

### Agent Diagnostics

Run `riot-agent doctor` to troubleshoot a misbehaving agent. The command checks server connectivity, TLS certificate validity, file permissions, collector health, and system dependencies (e.g. `smartctl`, `upsc`, Docker socket). It prints a pass/fail summary with actionable suggestions for each failing check.

On Linux, when the `logs` or `security` collector is enabled, `doctor` also runs a **Journal Read Access** check. This detects the silent-zero failure mode where `journalctl` is installed but the agent user cannot read system-wide journal entries — which would cause the auth-failure detector to always report zero without any error. If the check warns that the `riot` user is not in the `systemd-journal` group, run:

```bash
sudo usermod -a -G systemd-journal riot && sudo systemctl restart riot-agent
```

Then re-run `riot-agent doctor` to confirm the check passes.

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
       - container_logs
       - security
       - logs
       - ups
       - webservers
       - usb
       - hardware
       - cron

   docker:
     enabled: "auto"               # "auto" (detect), "true", or "false"
     collect_stats: true            # per-container CPU/memory stats
     terminal_enabled: false        # set to true to allow remote exec from dashboard
     check_updates: true            # check registries for newer container images

   commands:
     allow_reboot: false            # set to true to allow remote reboot
     allow_shutdown: false          # set to true to allow remote shutdown (device requires physical/OOB access to power back on)
     allow_patching: false          # set to true to allow remote OS updates and enable auto-updates
     hold_reboot_class: false       # set to true to hold GPU driver + kernel packages at the OS level (see Reboot-Class Package Gating)

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
| `/usr/bin/apt-mark hold *` | Reboot-class holds | Hold GPU driver / kernel packages (Debian/Ubuntu); subcommand locked, package name charset-validated agent-side |
| `/usr/bin/apt-mark unhold *` | Reboot-class holds | Release rIOt-managed holds during an in-window patch run or on disable |
| `install -m 0644 -o root -g root /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` | Reboot-class holds | Place the rIOt-owned `excludepkgs` fragment (dnf5); both paths fixed, zero variable arguments |
| `rm -f /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` | Reboot-class holds | Remove the fragment on disable / empty set (dnf5); exact-path |
| `/usr/bin/systemctl reboot` | Remote reboot | Reboot the device from the dashboard |
| `sudo systemctl poweroff` | Remote shutdown | Shut down the device from the dashboard (Linux) |
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

**Reboot-class holds on existing agents**: the four `apt-mark` / dnf-fragment rules above are added by `install.sh`, but an in-place agent self-update (`agent_update`) only swaps the binary — it does **not** rewrite `/etc/sudoers.d/riot-agent`. To enable [Reboot-Class Package Gating](#reboot-class-package-gating) on an already-deployed host, **re-run the installer** (`curl … | sudo bash`, which `install.sh` supports idempotently) so the new rules are written. Until then the agent reports `hold_enforcement: no_privilege` and the device page shows a "Hold enforcement inactive" warning rather than silently failing to protect anything. `riot-agent doctor` runs the same privilege probes and flags the gap.

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
| `collectors.smart_interval` | `4h` | Interval between SMART disk health scans (e.g. `1h`, `4h`, `12h`) |
| `collectors.webservers.nginx.access_log` | — | Absolute path to the nginx access log file. When set, the `webservers` collector parses the log each interval and includes HTTP status code counts in telemetry. Omit to disable access log monitoring (no default path is assumed). |
| `docker.enabled` | `auto` | Docker collection mode: `auto` (detect), `true`, `false` |
| `docker.socket_path` | auto-detect | Override the Docker socket path |
| `docker.collect_stats` | `true` | Collect per-container CPU/memory stats |
| `docker.terminal_enabled` | `false` | Allow remote `docker exec` from the dashboard |
| `docker.check_updates` | `true` | Check container registries for newer images (30-min cache) |
| `commands.allow_reboot` | `false` | Allow remote reboot command from the dashboard |
| `commands.allow_shutdown` | `false` | Allow remote shutdown command from the dashboard (device requires physical or out-of-band access to power back on) |
| `commands.allow_patching` | `false` | Allow remote OS patching and enable-auto-updates commands from the dashboard |
| `commands.hold_reboot_class` | `false` | Hold GPU driver + kernel packages at the OS level so they can only be applied during a maintenance-window patch run (Linux apt/dnf5 only — see [Reboot-Class Package Gating](#reboot-class-package-gating)). Requires the server-side `reboot_class: gated` policy and the new sudoers rules from `install.sh`. |
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
| `disk` | Block devices, mounted filesystems with usage, disk I/O metrics (reads/writes, bytes, queue depth); pool and union filesystems are automatically identified and displayed in a separate "Storage Pools" card section on the device detail page — detected by filesystem type (ZFS, Btrfs, bcachefs, mergerfs, unionfs, Unraid `shfs`/`fuse.shfs`) or by device path (mdraid arrays `/dev/md*`, LVM and device-mapper volumes `/dev/mapper/*` and `/dev/dm-*`; Docker device-mapper and live-boot overlays are excluded) |
| `network` | Interfaces, IPs, MACs, state, bytes tx/rx, default gateway, DNS servers |
| `os_info` | OS name/version, kernel, uptime, timezone, locale, init system |
| `updates` | Package manager, pending updates, security updates, kernel update status, unattended-upgrades status; classifies each pending update as `gpu_driver` / `kernel` / standard and detects reboot-required state (`/var/run/reboot-required` on apt, `dnf needs-restarting -r` on dnf) — see [Reboot-Class Package Gating](#reboot-class-package-gating) |
| `services` | systemd services — name, state, enabled, PID, memory usage |
| `processes` | Top 15 by CPU, top 15 by memory — PID, name, CPU %, memory %, user |
| `docker` | Docker containers — name, image, status, ports, CPU/mem stats, `riot.*` labels, real-time events, image update detection |
| `container_logs` | Docker container stdout/stderr logs — fetched via Docker API, stored server-side with 7-day retention, viewable in the container detail Logs tab |
| `security` | SELinux/AppArmor, firewall, open ports, failed logins (24h rolling count), per-interval auth failure count (`failed_logins_interval` — Linux only; entries are origin-filtered to trusted journald sources before counting, see [Auth Failure Alerting](#auth-failure-alerting)), logged-in users |
| `logs` | Recent journald entries (info and above); auto-deduplicates on the server |
| `ups` | NUT UPS status — battery charge, runtime, load, voltage, model (requires `upsc`) |
| `webservers` | Reverse proxy detection (nginx, Caddy, Ferron) — sites, SSL certificates, upstreams, security config (requires nginx sudoers rules; see below); optional nginx access log monitoring (HTTP error rate counting — see [Nginx Access Log Monitoring](#nginx-access-log-monitoring)) |
| `usb` | Connected USB devices — vendor/product names (via sysfs + `/usr/share/hwdata/usb.ids` fallback), serial numbers, device class, speed; supports disconnect alerts |
| `hardware` | PCI devices (vendor/device/class/driver via sysfs + `/usr/share/hwdata/pci.ids`), disk drives (model, serial, size, type — NVMe/SSD/HDD, transport, scheduler, **SMART health/temp/power-on hours/reallocated sectors**), serial ports, GPUs (filtered from PCI display class devices, optional VRAM via DRM). Linux-only; SMART requires `smartmontools`. |
| `cron` | Cron jobs and scheduled tasks — user crontabs, system crontabs (`/etc/crontab`, `/etc/cron.d/*`), systemd timers with next/last run times (Linux); scheduled tasks via `schtasks` (Windows) |
| `gpu` | NVIDIA GPU runtime metrics — temperature, utilization %, memory used/total, fan speed, power draw/limit, per GPU (requires `nvidia-smi`; Linux-only; see [GPU Monitoring](#gpu-monitoring)) |

**Note:** The `usb` and `hardware` collectors are Linux-only. They read from sysfs (`/sys/bus/usb/devices/`, `/sys/bus/pci/devices/`, `/sys/block/`, `/sys/class/tty/`) and use the system ID databases (shipped with `usbutils` or `hwdata`) to resolve vendor/product names. No additional packages are required for basic hardware info. **SMART disk health** requires `smartmontools` — the installer installs it automatically and adds a sudoers rule for `smartctl`. SMART scans run every 4 hours by default (configurable via `collectors.smart_interval` in agent YAML).

**Note:** Existing agent installs use a whitelist from the installer — new collectors like `hardware`, `container_logs`, and `gpu` are **not** picked up automatically. You must add the collector name to `collectors.enabled` in each agent's `/etc/riot/agent.yaml` and restart the agent.

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
- **Nginx error rates** — alert when HTTP 5xx or 4xx error counts exceed a threshold per telemetry interval (requires `webservers` collector with `access_log` configured — see [Nginx Access Log Monitoring](#nginx-access-log-monitoring))
- **Auth failure** — alert when any SSH, sudo, console, or su authentication failure is observed in the current telemetry interval (Linux only; matched against trusted journald sources — see [Auth Failure Alerting](#auth-failure-alerting))

### Alert Templates

Pre-built templates are available in Settings > Alert Rules > "Create from Template" for common scenarios. Templates pre-fill the metric, operator, threshold, and severity — just add a target name if needed.

### Alert Scoping (Include / Exclude)

Every alert rule supports **Include** and **Exclude** device lists. When both are empty the rule is global (applies to all devices). Add hostnames to the Include list to scope a rule to specific devices, or add hostnames to the Exclude list to exempt devices from a global rule. Exclude always wins — a device in both lists is excluded. Tags are also supported for matching.

### One-Click Alert Creation

From the device detail view, click the alert icon next to any service, process, network interface, USB device, UPS, or GPU to instantly create a targeted alert rule.

### Event Acknowledgement

The Alerts tab in the navigation bar shows a red badge with the count of unacknowledged warning/critical events. Events can be acknowledged individually or in bulk from the Alerts page. The badge updates in real-time via WebSocket.

---

## GPU Monitoring

The `gpu` collector reads runtime metrics from NVIDIA GPUs via `nvidia-smi`. It is Linux-only and is **not enabled by default**.

### Requirements

- Linux agent (the collector is a no-op on macOS and Windows)
- NVIDIA proprietary driver installed (driver version 400 or newer)
- `nvidia-smi` present in `PATH` (ships with the NVIDIA driver, typically at `/usr/bin/nvidia-smi`)
- The `riot` user must be able to execute `nvidia-smi` without `sudo` — `nvidia-smi` is a user-space read-only tool and does not require elevated privileges

### Enabling the GPU Collector

> **Important:** The `gpu` collector is not in the default `collectors.enabled` list. You must add it manually.

1. Edit `/etc/riot/agent.yaml` on each device with an NVIDIA GPU:

   ```yaml
   collectors:
     enabled:
       - system
       - cpu
       # ... your existing collectors ...
       - gpu      # add this line
   ```

2. Restart the agent:

   ```bash
   sudo systemctl restart riot-agent
   ```

3. Verify with `riot-agent doctor` — the output will show `gpu` as enabled and report whether `nvidia-smi` is found in `PATH`.

### What Is Collected

For each NVIDIA GPU on the host, the `gpu` collector reports:

| Field | Unit | Notes |
|---|---|---|
| Temperature | °C | Omitted if GPU does not support temperature reporting |
| Fan speed | % | Omitted on GPUs without a controllable fan (e.g., blower-cooled server cards) |
| GPU utilization | % | Shader utilization |
| Memory utilization | % | Memory controller utilization |
| Memory used / total | MiB | |
| Power draw / limit | W | Omitted on GPUs without power management support |
| GPU name, UUID, PCI bus ID | — | Static identifiers; reported every collection cycle |

If `nvidia-smi` is not installed or reports a field as unsupported, that field is omitted from the telemetry payload. The collector never returns an error for a missing `nvidia-smi` binary — it silently returns no data.

Multiple GPUs are fully supported. Each GPU appears as a separate entry in the GPU Telemetry card on the device detail page.

### Relationship to the hardware Collector

The `hardware` collector provides static GPU identity (vendor, model, PCI slot, driver, VRAM) by reading sysfs and the DRM subsystem. The `gpu` collector provides runtime metrics via `nvidia-smi`. The two collectors are independent — you can enable either or both.

### GPU Alerts

Two alert templates are available in **Settings > Alert Rules > Create from Template** under the `gpu` category:

| Template | Metric | Default Threshold | Severity | Cooldown |
|---|---|---|---|---|
| GPU Temperature Warning | `gpu_temp` | > 80°C | warning | 1 hour |
| GPU Temperature Critical | `gpu_temp` | > 90°C | critical | 30 minutes |

GPU alert rules fire per-GPU. If a device has multiple GPUs and one exceeds the threshold, the alert fires and the event message identifies the GPU by name and index.

You can also create custom alert rules using the following GPU metrics:

| Metric | Description |
|---|---|
| `gpu_temp` | GPU temperature in °C |
| `gpu_util_percent` | GPU utilization percentage |
| `gpu_mem_percent` | GPU memory controller utilization percentage |
| `gpu_power_watts` | GPU power draw in watts |

---

## Nginx Access Log Monitoring

The `webservers` collector can parse the nginx access log and report per-interval HTTP status code counts. This lets you create alert rules that fire when error rates spike — for example, when a service starts returning 5xx errors.

Only the default nginx **combined** log format is supported. Custom log formats are not parsed; lines that do not match are silently skipped.

This feature uses O(1) memory regardless of traffic volume. It is safe to run on Raspberry Pi and other memory-constrained devices.

### Requirements

- Linux agent (the webservers collector is Linux-only)
- nginx installed and the `webservers` collector enabled in the agent config
- The agent process must have read access to the access log file (typically `/var/log/nginx/access.log`)

The nginx sudoers rules installed by the install script cover config inspection but not access log reads. If the agent runs as a non-root user (the `riot` service account), grant read access to the log file:

```bash
sudo setfacl -m u:riot:r /var/log/nginx/access.log
```

Or adjust the log file group and permissions:

```bash
sudo chown root:riot /var/log/nginx/access.log
sudo chmod 640 /var/log/nginx/access.log
```

### Enabling Nginx Access Log Monitoring

1. Edit `/etc/riot/agent.yaml` on the device running nginx:

   ```yaml
   collectors:
     enabled:
       - system
       - cpu
       # ... your existing collectors ...
       - webservers      # must be in the enabled list
     webservers:
       nginx:
         access_log: /var/log/nginx/access.log
   ```

2. Restart the agent:

   ```bash
   sudo systemctl restart riot-agent
   ```

On first start after enabling, the agent seeks to the end of the access log and begins counting from that point forward. It does not process historical entries. The first telemetry push after enabling will report zero counts.

The agent stores its read position in `/etc/riot/nginx-access-log.offset` and resumes from that offset on restart. Log rotation is detected automatically — when the file shrinks below the stored offset, the agent resets to the beginning of the new file.

### What Is Collected

Per telemetry interval (default 60 seconds), the agent counts:

| Metric | Description |
|---|---|
| `total_requests` | Total HTTP requests parsed |
| `status_2xx` | Count of 2xx (success) responses |
| `status_3xx` | Count of 3xx (redirect) responses |
| `status_4xx` | Count of 4xx (client error) responses |
| `status_5xx` | Count of 5xx (server error) responses |

Only aggregate counts leave the agent. No IP addresses, URIs, user-agent strings, or other data from the access log are transmitted.

### Creating Nginx Alert Rules

Go to **Settings > Alert Rules** and click **Create from Template**. Two pre-built templates are available under the `webserver` category:

| Template | Metric | Default Threshold | Severity | Cooldown |
|---|---|---|---|---|
| Nginx 5xx Errors High | `nginx_5xx_count` | > 10 per interval | critical | 5 minutes |
| Nginx 4xx Errors High | `nginx_4xx_count` | > 50 per interval | warning | 15 minutes |

You can also create custom rules using these metrics in the metric dropdown:

| Metric | Description |
|---|---|
| `nginx_5xx_count` | 5xx response count per telemetry interval |
| `nginx_4xx_count` | 4xx response count per telemetry interval |
| `nginx_request_count` | Total request count per telemetry interval |

Thresholds are raw counts per interval, not rates per second. A threshold of `10` on `nginx_5xx_count` means "more than 10 5xx errors occurred since the last telemetry push." Adjust thresholds based on your `agent.poll_interval` setting (default 60 seconds) and your application's typical error baseline.

All standard alert rule features apply: device scoping (include/exclude), cooldown periods, severity levels, and notification dispatch. Nginx alert rules do not fire on devices that do not report nginx access metrics — you can safely create a global rule without affecting devices that do not have nginx.

### Limitations

- Only the nginx **combined** log format is supported.
- Access log parsing does not run if `webservers` is absent from `collectors.enabled`, even if `access_log` is configured.
- No per-URL, per-upstream, or per-virtual-host breakdown. Counts are global across all nginx traffic.
- Nginx `error.log` is not parsed — only `access.log`.
- Access log metrics appear in telemetry data only, not in the lightweight heartbeat.

---

## Auth Failure Alerting

The `security` collector reports `failed_logins_interval`, a per-interval count of SSH, sudo, console, and su authentication failures observed in journald since the previous telemetry push. This enables alert rules that fire within a single telemetry cycle (default 60 seconds) when any auth failure occurs — far faster than the 24-hour rolling `FailedLogins24h` count used by the security score.

This feature is **Linux-only**. Non-Linux agents omit the field from their telemetry payload; auth-failure alert rules are not evaluated against them.

### How origin filtering works

Before counting a log line, the agent verifies the journal entry came from a trusted system daemon. It requires:

1. The entry's kernel-attested `_UID` field equals `0` (written by a root-owned process).
2. The entry's `_SYSTEMD_UNIT` or `SYSLOG_IDENTIFIER` is in a fixed allow-list: `ssh.service`, `sshd.service`, `sudo.service`, `systemd-logind.service`, `login.service`, `sshd`, `sudo`, `login`, `su`.

Lines written by unprivileged users via `logger(1)` have a non-zero `_UID` and are rejected regardless of their content. This prevents a local shell user from forging auth-failure signals.

### What is matched

Only lines from trusted sources (above) that contain one of these substrings are counted:

- `Failed password` — SSH password authentication failure
- `authentication failure` — general PAM failure
- `Invalid user` — SSH invalid username
- `pam_unix(sudo:auth): authentication failure` — sudo PAM failure

Each matching line increments the counter once. There is no per-IP or per-user deduplication — the count is a raw line count (AC-007).

### First-interval behavior

On the first telemetry push after agent start, `failed_logins_interval` is always `0`. The agent does not backfill auth failures from before it started. If the agent restarts (for example during an upgrade), one telemetry interval reports zero regardless of journal activity during that window.

### Setting up the Auth Failure alert rule

1. Go to **Settings > Alert Rules** and click **Create from Template**.
2. Select **Auth Failure** under the **security** category.
3. Review the default settings: metric `failed_logins_interval`, operator `>`, threshold `0`, severity `warning`, cooldown `300s`.
4. On internet-facing hosts with public SSH, routine bot traffic will trigger this at every cooldown window at the default threshold. Either raise the threshold (e.g. `> 5`) or narrow the **Include Devices** scope to internal hosts before saving.
5. Attach a notification channel and save.

### Metric reference

| Metric | Type | Platform | Description |
|--------|------|----------|-------------|
| `failed_logins_interval` | integer | Linux only | Count of auth failure log lines from trusted journald sources in the last telemetry interval |

### `no-op` on non-Linux and no config change required

The `failed_logins_interval` metric travels inside the existing `security` collector payload. No new collector name exists, so no change to `collectors.enabled` in `agent.yaml` is needed after upgrading agents.

---

## mTLS Device Authentication

mTLS (mutual TLS) makes every agent prove its identity with a certificate before the server accepts any data. When enabled, agents need **both** a valid client certificate **and** their API key — if either is missing or revoked, the server rejects the request. No external tools like `openssl` are needed; the server runs its own CA and the agent handles enrollment automatically.

### Step-by-Step Setup

#### Step 1: Enable mTLS during server setup

When you first open the rIOt dashboard, the setup wizard asks whether to enable mTLS. **Check the box.** That's it for the server side.

If you're using environment variables instead of the wizard:

```yaml
environment:
  - RIOT_MTLS_ENABLED=true
  - RIOT_TLS_ENABLED=true    # mTLS requires TLS
```

The server automatically generates a private Certificate Authority (CA) on first start. You don't need to create any certificates yourself.

#### Step 2: Create a bootstrap key

A bootstrap key is a one-time password that lets a new agent request a certificate. Each key can only be used once.

1. Open the dashboard
2. Go to **Settings > Certificates**
3. Click **Create Key**
4. Give it a label (e.g. "pi-cameras") and set an expiry (default: 24 hours)
5. **Copy the key** — it's shown exactly once, you can't retrieve it later

You need **one key per device**. If you're installing 5 agents, create 5 keys.

#### Step 3: Install the agent with the bootstrap key

Use the install script with the `--bootstrap-key` flag:

```bash
curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh \
  | sudo bash -s -- https://<server-ip>:7331 --bootstrap-key <YOUR-KEY>
```

That's it. On first boot, the agent will automatically:

1. Generate a cryptographic key pair
2. Send a certificate request to the server using your bootstrap key
3. Receive a signed certificate back from the server's CA
4. Save the certificate and CA cert to `/etc/riot/`
5. Clear the bootstrap key from the config (it's single-use)
6. Connect using the certificate + API key for all future communication

You'll see the device appear in the dashboard within seconds.

#### Step 4: Verify it worked

Go to **Settings > Certificates** in the dashboard. You should see:

- Your device listed under **Device Certificates** with a serial number and expiry date
- The bootstrap key you used marked as **Used**

#### What happens if I don't use a bootstrap key?

If mTLS is enabled on the server and you install an agent **without** `--bootstrap-key`, the agent will register with an API key only but the server will **reject all data** (heartbeats, telemetry, etc.) because it requires a client certificate. The device will show as offline.

**Fix:** Uninstall the agent, create a new bootstrap key, and reinstall with `--bootstrap-key`.

### Manual Install with mTLS

If you're not using the install script, add the bootstrap key to your agent config:

```yaml
server:
  url: "https://<server-ip>:7331"
  tls_verify: true
  bootstrap_key: "<key-from-dashboard>"
```

Start the agent. After enrollment completes, the config is automatically updated to:

```yaml
server:
  url: "https://<server-ip>:7331"
  tls_verify: true
  client_cert: "/etc/riot/client.crt"
  client_key: "/etc/riot/client.key"
  ca_cert_file: "/etc/riot/ca.crt"
```

The `bootstrap_key` line is removed automatically.

### Revoking a Device

If a device is compromised or decommissioned:

1. Go to **Settings > Certificates**
2. Find the device's certificate
3. Click **Revoke**

The server immediately stops accepting requests from that certificate. The device will need to be re-enrolled with a new bootstrap key to reconnect.

### Certificate Renewal

Device certificates are valid for 10 years. Agents automatically check their certificate on startup and renew when fewer than 30 days remain. Renewal uses the existing mTLS connection — no bootstrap key is needed. The old certificate is revoked automatically.

If a certificate has already expired, the agent cannot renew (it can't authenticate). In that case, create a new bootstrap key and re-enroll the device.

### Server TLS Certificate

The server's self-signed TLS certificate is also valid for 10 years. When mTLS is enabled, the server certificate is signed by the rIOt CA so agents automatically trust it after renewal. To regenerate the server certificate (e.g. after adding new IP SANs), go to **Settings > Certificates** and click **Regenerate TLS Certificate**. The server restarts TLS automatically.

### Bootstrap Key Cleanup

Used and expired bootstrap keys are automatically purged by the server's hourly retention worker. No manual cleanup is needed.

### Certificate Management

From **Settings > Certificates**:

- **CA certificate** — view and download the server's CA cert
- **Device certificates** — all issued certs with device ID, serial number, expiry, and revocation status
- **Bootstrap keys** — create, list, and delete enrollment keys; see which keys have been used and by which device

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

| Channel | Config Keys | Notes |
|---------|-------------|-------|
| **Email (SMTP)** | `host`, `port`, `username`, `password`, `from`, `to` | Any SMTP server (Gmail, Mailgun, self-hosted). STARTTLS supported. |
| **ntfy** | `server_url`, `topic`, `token` | Push notifications via [ntfy.sh](https://ntfy.sh) or self-hosted. |
| **Telegram** | `bot_token`, `chat_id` | Free. Create a bot via [@BotFather](https://t.me/BotFather). |
| **Discord** | `webhook_url` | Server Settings > Integrations > Webhooks. Free. |
| **Slack** | `webhook_url` | Create an [Incoming Webhook](https://api.slack.com/messaging/webhooks) app. Free. |
| **Pushover** | `api_token`, `user_key` | [pushover.net](https://pushover.net) — $5 one-time per platform. |
| **Gotify** | `server_url`, `app_token` | Self-hosted push notification server. Free. |
| **Webhooks** | `url`, `headers` | Generic JSON POST to any URL with optional custom headers. |

Configure channels in **Settings > Notifications**. Each channel can be tested with a one-click test notification. All delivery attempts are logged in **Settings > Notifications > Delivery Log**.

---

## API

All endpoints are under `/api/v1/`. Agent endpoints require the `X-rIOt-Key` header. When mTLS is enabled, agent endpoints additionally require a valid client certificate (both are checked). Dashboard endpoints require admin authentication (JWT cookie).

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
| `POST` | `/api/v1/renew` | Renew device certificate (requires valid mTLS client cert) |
| `GET` | `/api/v1/ca.pem` | Download CA certificate |

### Dashboard (admin auth)

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/v1/devices` | List all devices |
| `GET` | `/api/v1/devices/:id` | Device detail + latest telemetry |
| `GET` | `/api/v1/devices/:id/history` | Paginated telemetry history |
| `GET` | `/api/v1/devices/:id/containers` | List containers for a device |
| `GET` | `/api/v1/devices/:id/containers/:cid` | Container detail |
| `GET` | `/api/v1/devices/:id/containers/:cid/logs` | Container log history (query: `limit`, `stream`, `since`) |
| `GET` | `/api/v1/devices/:id/containers/:cname/metrics` | Container metric history (query: `hours`) |
| `DELETE` | `/api/v1/devices/:id` | Remove a device |
| `POST` | `/api/v1/devices/:id/rotate-key` | Rotate device API key |
| `POST` | `/api/v1/devices/:id/commands` | Send command to agent |
| `GET` | `/api/v1/devices/:id/commands` | List device command history |
| `GET` | `/api/v1/devices/:id/commands/:cmdId/output` | Command output detail |
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
| `POST` | `/api/v1/settings/tls/regenerate` | Regenerate server TLS certificate |
| `GET` | `/api/v1/fleet/agent-versions` | Agent version summary |
| `GET` | `/api/v1/fleet/patch-status` | Fleet patch status overview — per device: `pending_updates`, `security_count`, `reboot_class_count`, `reboot_required`. Add `?detail=true` for the full `updates` list (each carries a `class`: `gpu_driver` / `kernel` / empty) plus `package_manager` |
| `POST` | `/api/v1/fleet/bulk-update` | Bulk update agents |
| `POST` | `/api/v1/fleet/bulk-patch` | Bulk patch devices |
| `GET` | `/api/v1/fleet/heartbeats` | 60-minute heartbeat history for every device in one request (query: `window`, e.g. `?window=60m`); used by the fleet dashboard |
| `GET` | `/api/v1/fleet/containers` | Flat list of all containers across the fleet with CPU, memory, restart count, update status, and Compose stack name; used by the fleet dashboard leaderboard |
| `GET` | `/api/v1/security/overview` | Security overview |
| `GET` | `/api/v1/security/devices` | Per-device security details |
| `GET/POST` | `/api/v1/settings/logs` | Server log viewer |
| `GET/PUT` | `/api/v1/settings/automation` | Automation interval config |
| `GET/POST/PUT/DELETE` | `/api/v1/probes[/:id]` | Server uptime probe CRUD (HTTP, Ping, DNS) |
| `POST` | `/api/v1/probes/:id/run` | Run server probe on demand |
| `GET` | `/api/v1/probes/:id/results` | Server probe result history |
| `GET` | `/api/v1/device-probes` | List all device probes across all devices, enriched with device hostname |
| `GET/POST/PUT/DELETE` | `/api/v1/devices/:id/device-probes[/:pid]` | Device probe CRUD for a specific device |
| `POST` | `/api/v1/devices/:id/device-probes/:pid/run` | Run device probe on demand |
| `GET` | `/api/v1/devices/:id/device-probes/:pid/results` | Device probe result history |
| `GET` | `/api/v1/devices/:id/summary` | Device inventory summary as Markdown (`text/markdown`; includes `Content-Disposition` for direct download) |

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
| Container metrics | 7 days |
| Container logs | 7 days |
| Device logs | 30 days (configurable via `RIOT_RETENTION_DAYS`) |
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

## Reboot-Class Package Gating

GPU driver and kernel packages are special: applying one out-of-band can break a running system until the host reboots. The motivating case is a GPU driver upgrade on a host running GPU-dependent Docker containers (NVIDIA container runtime) — the kernel-module/user-space-library version mismatch breaks every GPU container until the machine is rebooted, turning a routine `apt upgrade` into an unplanned outage.

**Reboot-class** = NVIDIA/AMD GPU driver packages **plus** kernel/dkms packages. Reboot-class package gating holds these at the OS level so they can move only during a maintenance window, and a run that applies one always ends in a reboot. It is **off by default** and is a **two-sided opt-in** — nothing changes unless you enable it on both the server and the agent.

### Platform support

- **Supported:** Linux with **apt** (Debian/Ubuntu, via `apt-mark hold`) and **dnf5** (Fedora 41+, RHEL 10+, via a rIOt-owned `excludepkgs` drop-in fragment).
- **Not supported for OS-level holds:** dnf4 (classification and reboot-required detection still work, but there is no drop-in directory to lock packages — the agent reports `hold_enforcement: unsupported`), pacman, apk, and Windows/macOS. GPU driver rollback, container restart orchestration, and Intel GPU classification are out of scope.

### What "reboot-class" covers

| Class | apt examples | dnf examples |
|---|---|---|
| `gpu_driver` | `nvidia-driver-*`, `nvidia-dkms-*`, `libnvidia-*`, `nvidia-utils-*`, `xserver-xorg-video-nvidia-*`, `amdgpu-*`, `rock-dkms`/`rocm-dkms` | `nvidia-driver*`, `akmod-nvidia*`, `kmod-nvidia*`, `xorg-x11-drv-nvidia*`, `amdgpu*`, ROCm kernel-module packages |
| `kernel` | `linux-image-*`, `linux-headers-*`, `linux-modules-*`, `linux-generic*`, any `*-dkms` not already a GPU driver | `kernel`, `kernel-core`, `kernel-modules*`, `kernel-headers`, `kernel-devel`, any `*-dkms` not already a GPU driver |

A package matching both (e.g. `nvidia-dkms-550`) is classified `gpu_driver` (GPU precedence). Deliberately **not** reboot-class (upgradeable any time): `linux-firmware`, the container toolkit (`libnvidia-container*`, `nvidia-container-toolkit`, `nvidia-docker2`), and ROCm user-space libraries — these are user-space plumbing, not part of the driver/kernel-module ABI pair.

### Enabling it (both sides required)

1. **Re-run `scripts/install.sh` on each agent host.** Hold enforcement needs new sudoers rules (`apt-mark hold/unhold`, or the fixed-path dnf fragment writer). An in-place `agent_update` does **not** install them — see [Sudoers Rules](#sudoers-rules). Until they are present the agent reports `hold_enforcement: no_privilege` and the device page shows a **"Hold enforcement inactive"** warning; enforcement fails **closed and visibly**, never as a silent no-op.

2. **Agent side** — in each host's `/etc/riot/agent.yaml`:

   ```yaml
   commands:
     hold_reboot_class: true    # hold GPU driver + kernel packages
     allow_patching: true       # required for the automated in-window patch run
     allow_reboot: true         # required for the automatic post-apply reboot
   ```

   Then `sudo systemctl restart riot-agent`. Without `allow_patching`, no automated patch run occurs. Without `allow_reboot`, the agent applies reboot-class packages but does **not** reboot — instead it raises a `reboot_required` event, leaving the reboot for you to schedule. The `allow_reboot` veto is absolute: no server setting or command parameter can override it.

3. **Server side** — set the OS-patch maintenance window's reboot-class policy to `gated`. This policy lives on the `os_patch` maintenance window in the automation config (default `off`) and is applied via `PUT /api/v1/settings/automation`, alongside the window schedule managed in **Settings > Agents**:

   ```jsonc
   {
     "os_patch": {
       "mode": "window",
       "start_time": "03:00",
       "end_time": "05:00",
       "reboot_class": "gated"   // "off" (default) or "gated"
     }
     // ... docker_update window ...
   }
   ```

   Only when the policy is `gated` **and** the dispatch occurs inside the OS-patch window does the orchestrator include reboot-class packages in the run. Manually dispatched `os_update` commands never include them — the server strips the parameter, so the automated in-window run is the only release path.

### What happens during an in-window run

When both opt-ins are active and the OS-patch window opens with reboot-class updates pending:

1. The agent releases **only its own** holds (operator-created `apt-mark hold`s are never touched), recording a crash-recovery marker first.
2. The package manager applies updates.
3. Holds are re-applied against the updated package set on **every** exit path — success, upgrade failure, or crash.
4. If a reboot-class package's version actually changed **and** `allow_reboot` is true, the agent reports the result (stating which reboot-class packages were applied and that a reboot was initiated) and then reboots. If `allow_reboot` is false, it raises `reboot_required` instead. No reboot occurs if only standard packages were applied.

Outside the window, holds keep GPU driver and kernel packages pinned, so a casual `apt upgrade` or `unattended-upgrades` skips them.

### Reboot-required detection and alert

Independently of holds, the agent reports whether the host needs a reboot to activate installed updates — `/var/run/reboot-required` (+ `/var/run/reboot-required.pkgs`) on apt, `dnf needs-restarting -r` on dnf. A once-per-transition **Reboot Required** event is emitted (warning severity, 24-hour cooldown), and a matching alert rule/template ships enabled so it can fan out to your notification channels. Detection failures degrade quietly to "not required". Populating this state also fixes the security score's kernel-update check, which previously always passed silently because the underlying field was never set.

### GPU container blast radius

The Docker collector flags containers that request GPU access (via `HostConfig.DeviceRequests`/`Devices` — `--gpus`, compose `device_requests`, or `/dev/nvidia*` / `/dev/dri` / `/dev/kfd` device mappings). The device detail page shows an "N GPU containers" count and a per-container GPU badge, so you can judge what a driver update would disrupt before scheduling the window.

### Hold-enforcement status

Every telemetry cycle the agent reports a `hold_enforcement` state:

| State | Meaning |
|---|---|
| `active` | Holds are being enforced; `held_packages` lists what rIOt currently holds. |
| `no_privilege` | The feature is enabled but the sudoers rules are missing — **re-run the installer**. Shown as a red warning on the device page. |
| `unsupported` | dnf4 host — OS-level holds require dnf5. Classification and reboot-required detection still work. |

`held_packages` is non-empty only when the state is `active`, so an unenforced host can never present holds as "protected".

---

## Fleet Dashboard

The `/dashboard` route provides a fleet-wide health view. It is separate from the `/` Devices route, which is a sortable per-device management table. The dashboard answers "is my fleet healthy right now?" without requiring you to click into individual devices.

Navigate to it via the **Dashboard** link in the header, or go directly to `http://<server>:7331/dashboard`.

### Sections

**Per-device performance charts** — One card per device. Each card carries two stacked charts over the last 60 minutes:

- **Top chart** (0–100% Y-axis, 220px tall): four lines — **CPU%** (blue), **RAM%** (green), **Disk I/O %** (amber, busy-time of the most-utilised drive — same field as the device detail page), **load saturation** (purple, computed as `min(load_avg_1m × 25, 100)`).
- **Bottom chart** (bytes/sec Y-axis, 100px tall): two lines — network **In** (cyan) and **Out** (orange). Y-axis auto-scales (B/s → KB/s → MB/s → GB/s) and the tooltip shows the rate in human-readable units. Counters are summed across all non-loopback interfaces (physical, bridges, virtual NICs, etc.).

Colors are consistent across every device card so a glance comparison is meaningful. Cards lay out two-up at viewports ≥768px, one column below.

**Container leaderboard** — A top-10 table of containers across the fleet, sortable by CPU, memory, restart count, or update availability. Each row shows hostname, container name, Compose stack, current CPU%, memory in MiB, restart count, and an update-available indicator.

**Activity river** — A live strip showing the 20 most recent events. New events fade in at the top; the oldest drops when the cap is reached. Filter chips (Alerts, Docker, Agent, Security) are additive; selecting multiple chips shows events matching any of the active chips.

### Live updates

The dashboard subscribes to the existing WebSocket connection — no additional connection is opened. Chart re-renders are batched to a 5-second cadence to keep frame budget predictable on large fleets.

When the WebSocket disconnects, a banner appears and all sections continue showing the last-known values. The banner clears automatically when the connection is restored.

### Browser and viewport support

The full design targets desktop viewports of 768px and above. Below 768px the dashboard degrades gracefully: sections stack vertically and charts shrink to fit. No horizontal page scroll occurs at any viewport width down to 360px. Sub-768px layouts are functional but not a polish target for v1.

### v1 limitations

- **Network is host-summed, not per-interface.** Throughput is summed across all non-loopback interfaces. Per-interface drilldown lives on the device detail page.
- **Fixed 60-minute window.** The dashboard always shows the last 60 minutes. No time-range selector exists in v1.

---

## License

MIT

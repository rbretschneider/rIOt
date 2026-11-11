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
- **Docker container management** — fleet-wide searchable table and optional grouped tiles, bulk actions, stable container links, stored and live logs, Inspect, Activity, I/O charts, first-class Compose stacks, inventory and previewed cleanup, plus optional remote terminals
- **Unified workspace** — grouped application sidebar, compact search/notifications/account bar, consistent resource headers and neutral surfaces across Devices, Overview, Docker, monitoring and settings; thin scrollbars are the UI default
- **Real-time dashboard** — dark-mode React UI with live WebSocket updates; every timestamp shows in the server's time zone in 12-hour form by default; click the utility-bar clock to choose the server, browser-local or UTC zone and a 12-hour or 24-hour clock ([details](#time-display))
- **Offline resilience** — agent buffers telemetry locally when the server is unreachable; resilient DNS caching with disk persistence for surviving DNS outages
- **Zero-config setup** — a setup wizard configures the admin password, TLS, and mTLS on first visit; agents auto-pin the server certificate (SSH-like TOFU)
- **Simple deployment** — single `docker compose up` for the server, one-liner install for agents
- **Open registration** — devices register automatically; optionally require a registration key via Settings
- **Admin authentication** — password-protected dashboard with JWT session cookies and in-app password changes
- **Optional SSO login** — sign in through an external OIDC identity provider (e.g. authentik) alongside the built-in password login; identity federation only — no separate user tier, and password login always remains available (see [Single Sign-On](#single-sign-on-oidc))
- **UPS monitoring** — auto-detects NUT `upsc`, displays battery charge, load, voltage, runtime, and status; alerts on battery switchover and low battery; fleet status dot turns yellow when a device is on battery power
- **USB device monitoring** — enumerates all connected USB devices with vendor/product names (resolved via sysfs + usb.ids database), serial numbers, device class, and speed; one-click alert creation to monitor for device disconnection (e.g. Coral TPU, Z-Wave stick, UPS HID)
- **Advanced alerting** — threshold-based alerts on numeric metrics plus state-based monitoring for services, network interfaces, processes, USB devices, and UPS power events; near-real-time SSH/sudo/console/su authentication failure alerting (Linux; ≤60s latency, journald origin-filtered); include/exclude device scoping on all alert rules; one-click alert creation from device view; pre-built templates
- **Event acknowledgement** — unread alert badge on the Alerts tab with per-event and bulk acknowledgement
- **Notification channels** — alert delivery via email (SMTP), Telegram, Discord, Slack, ntfy, Pushover, Gotify, and webhooks, with test-send support and delivery logging
- **mTLS device authentication** — optional certificate-based device identity with automatic CA management, bootstrap key enrollment, automatic certificate renewal (agents renew when <30 days remain), server TLS regeneration from the dashboard, server-enforced cert + API key auth on all device routes, and zero external tooling
- **Signed, fail-closed agent auto-update** — agents install new versions automatically, but only after verifying an ed25519 signature on the release checksums against a public key compiled into the agent; anything unsigned or unverifiable is refused and the running version keeps working. The `riot` user holds no arbitrary-root sudo rule (see [Agent Auto-Update and Release Signing](#agent-auto-update-and-release-signing))
- **Uptime probes** — unified Probes page showing server probes (HTTP, DNS, ping/ICMP — run by the server) and device probes (shell, HTTP, port check, file check, container exec — run by agents) in two separate sections; per-device probe assertion templates for response validation; full history and status tracking for both types
- **Fleet management** — agent version overview, bulk update, and patch status across devices
- **Bounded apt recovery** — independent signed maintenance worker repairs interrupted configuration and attempts one no-removal dependency repair before updates; repair-only mode is available through the existing API. Explicit root migration required; see [OS maintenance](docs/ops/os-maintenance.md)
- **Remote commands** — send commands to agents from the dashboard: Docker start/stop/restart/update, OS patching, enable automatic updates, agent update, system reboot, system shutdown (with per-command permission controls); all commands generate informational events with container context for full audit trail; command output capture with per-device activity log
- **Command lifecycle** — every command is tracked by ID through queued, dispatched, acknowledged, running, and verifying to a final state, and the dashboard shows the real state and current step; queued commands expire instead of firing days late and can be cancelled; agents keep final results in a durable outbox and deliver them over HTTP when the WebSocket is down ([details](#command-lifecycle))
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
- **Patch badge** — each device shows one badge, **Auto**, **Manual**, **Blocked** or **Needs you**, with a four-line explanation (automatic patching, reboot, what needs you, data age). **Needs you** appears only when updates are pending or a reboot is required and nothing will apply them, or when rIOt refused an update for a reason only you can fix. **Blocked** (amber) means rIOt refused an update for a reason you cannot act on. A setting alone, such as "auto-patch off" on a host with nothing pending, is shown as neutral information under **Manual**. A reboot date is shown as an estimate in the server's display zone, or as Unknown when it cannot be predicted ([details](#patch-badge), [time zone](#time-display))
- **DietPi updates in the patch flow** — opt in per device with `commands.allow_extra_updaters: [dietpi]` and a root re-run of `install.sh`. The maintenance worker then runs DietPi's own `dietpi-update` after apt during a patch, and the dashboard shows DietPi updates next to apt updates. No sudoers line is added ([details](#dietpi-updates))
- **Manual patch with an opt-in reboot** — the Patch confirmation offers **Reboot automatically if the patch requires it**. "Reboot needed" always means a reboot is required now, never that a pending kernel update will need one later. Package lists that are older than the apt configuration show "Package lists out of date" instead of a phantom update count ([details](#manual-patch-and-reboot))
- **Reboots and shutdowns wait for the package manager** — every agent-initiated reboot and every shutdown on a dpkg host waits up to 10 minutes for a running `apt`/`dpkg` to finish. If the lock stays held, or its state cannot be determined, the reboot or shutdown is skipped and reported, never forced. The shutdown result is truthful: a shutdown that sudo refuses reports `failed`. On a Linux host set up by the installer, remote shutdown is opt-in (SHUTDOWN-SUDO-001). The installer adds the one `systemctl poweroff` sudo rule only when you answer yes to "Allow remote shutdown from dashboard?" or when `commands.allow_shutdown` is `true` and you re-run `install.sh` as root ([how to enable it](#shutdown-on-linux-and-the-sudo-rule)). Upgrade the server before the agents ([details](#reboots-and-the-dpkg-lock))
- **Expected activity** — an update, restart, stack action, patch, reboot, shutdown or agent update that you or a schedule starts no longer raises a "container died" or "device offline" alert. rIOt records the disruption as an `info` event that names the action and who started it, shows what each device is doing on the Devices page, and raises one alert that names the action only when something is still down afterwards. Every window is bounded, the powered-off state after a shutdown is quiet, and no agent change is needed ([details](#expected-activity))
- **Dead man's switch** — optional agent heartbeat to external healthcheck services (e.g., Healthchecks.io)

## Dashboard navigation

The application sidebar groups the existing pages into one workspace:

| Group | Pages |
|---|---|
| Infrastructure | **Overview** (`/dashboard`) for performance and recent activity; **Devices** (`/`) for every registered machine |
| Docker | **Containers**, **Stacks**, **Hosts**, **Images**, **Volumes**, **Networks** |
| Observe | **Probes**, **Alerts**, **Security**, **Activity** (Docker command and event history) |
| Administration | **Alert Rules**, **Agents**, **Settings** |

Navigation stays visible on desktop screens at least 1280px wide. On narrower screens, use the navigation toggle; selecting a page or pressing Escape closes it. Search, notifications and account controls remain in the compact utility bar. Search also opens with **Ctrl+K** or **Cmd+K**, except while typing in a terminal. Use **Skip to content** when navigating by keyboard.

**Devices** is the complete machine list, including machines without Docker. **Docker > Hosts** and Docker host filters show only machines known to support Docker, including offline Docker hosts. Turning off the Docker feature hides its navigation and Activity; it does not remove machines from Devices. Resource pages keep their existing URLs, and their owning collection stays selected in the sidebar. Settings keeps its subsection links.

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
| `RIOT_ALLOWED_ORIGINS` | — | Comma-separated allowed origins, each `scheme://host[:port]` with no trailing slash. Used for CORS and accepted by the [origin check](#origin-check-on-state-changing-requests) on state-changing requests |
| `RIOT_GITHUB_REPO` | `rbretschneider/rIOt` | GitHub `owner/repo` for update checks |
| `RIOT_API_KEY` | — | Loaded into the server config at startup, but **not currently enforced by device registration**. Only the registration key saved in **Settings > General** (stored in the database) gates registration. Enforcing this variable is a tracked follow-up. See [Device Registration and Re-registration](#device-registration-and-re-registration) |
| `RIOT_ADMIN_PASSWORD` | — | Admin password override (bypasses wizard; bcrypt-hashed at startup) |
| `RIOT_JWT_SECRET` | auto-generated | JWT session secret (auto-generated if omitted; set for stable sessions across restarts) |
| `RIOT_TLS_ENABLED` | `false` | Enable TLS (`true` or `1`); overrides wizard setting |
| `RIOT_TLS_DOMAIN` | — | Let's Encrypt autocert domain (implies TLS enabled) |
| `RIOT_TLS_CERT_DIR` | — | Autocert cache directory |
| `RIOT_TLS_CERT_FILE` | — | Manual TLS certificate file path |
| `RIOT_TLS_KEY_FILE` | — | Manual TLS key file path |
| `RIOT_MTLS_ENABLED` | `false` | Enable mTLS device authentication (see [mTLS](#mtls-device-authentication)) |
| `RIOT_OIDC_ISSUER_URL` | — | Optional SSO: IdP issuer URL. Dormant unless this, `RIOT_OIDC_CLIENT_ID`, and `RIOT_OIDC_CLIENT_SECRET` are all set (see [Single Sign-On](#single-sign-on-oidc)) |
| `RIOT_OIDC_CLIENT_ID` | — | Optional SSO: OAuth client ID (see [Single Sign-On](#single-sign-on-oidc)) |
| `RIOT_OIDC_CLIENT_SECRET` | — | Optional SSO: OAuth client secret — treat as a secret (see [Single Sign-On](#single-sign-on-oidc)) |
| `RIOT_OIDC_BUTTON_LABEL` | `Sign in with SSO` | Optional SSO: text shown on the login screen's SSO button (see [Single Sign-On](#single-sign-on-oidc)) |
| `RIOT_TRUSTED_PROXIES` | — (trust nobody) | Comma-separated CIDRs trusted to supply forwarding headers; set only when rIOt sits behind a reverse proxy (see [Single Sign-On](#single-sign-on-oidc)) |
| `RIOT_PPROF_ADDR` | — (disabled) | Opt-in profiling listener on a literal loopback `ip:port`, for example `127.0.0.1:6060`. The port must differ from `RIOT_PORT`. Anything else is rejected and pprof stays off (see [Profiling (pprof)](#profiling-pprof)) |
| `RIOT_DISPLAY_TZ` | — (UTC) | **Preferred.** IANA time zone name (for example `America/New_York`) used for the dashboard's Server time zone and for times the server writes into text. Invalid values are ignored with one warning and the server still starts. Falls back to `TZ`, then UTC. Changes display only. Prefer it over `TZ`, which also changes the process zone and so the offsets in server log timestamps (see [Time Display](#time-display)) |
| `TZ` | — | Fallback for `RIOT_DISPLAY_TZ` when that is unset or invalid, because many Docker setups already set it. It also changes the server process's own zone, which shifts the offsets of log timestamps. Stored data and API fields stay UTC either way. If the server already sets a valid `TZ`, that zone becomes the dashboard's Server zone on upgrade |
| `GOMEMLIMIT` | `2GiB` | Go runtime memory limit. Controls how aggressively the garbage collector runs. The default is appropriate for most deployments; increase if monitoring many devices with 50+ containers each, or decrease on memory-constrained hosts. Too low causes GC thrashing (high CPU). |

### Profiling (pprof)

rIOt includes the Go profiler (pprof) for diagnosing memory and CPU problems. It is off by default. The main listener never serves it: `/debug/pprof` and everything under it returns `404` on the port set by `RIOT_PORT` (default `7331`), whether or not profiling is enabled.

To enable profiling, set `RIOT_PPROF_ADDR` to a literal loopback `ip:port`. The server then starts a second listener, separate from the dashboard, that serves only the pprof endpoints.

| Rule | Detail |
|---|---|
| Host | A literal IP address: IPv4 in `127.0.0.0/8` (for example `127.0.0.1`) or IPv6 `::1`. Hostnames, including `localhost`, are rejected. |
| Port | A plain decimal number from `1` to `65535`, with no leading zero. |
| Port collision | The port must differ from `RIOT_PORT`. On Linux the profiler binds first, and a shared port would stop the main listener from starting. |

If the value breaks any rule, the server logs an `ERROR` that names the reason and starts normally with profiling off. A bind failure (for example the port is already in use) is handled the same way. A successful start logs a `WARN` line with the bound address.

`RIOT_PPROF_ADDR` takes effect only if it reaches the server process. The compose files in this repository list the server's environment explicitly, so add `- RIOT_PPROF_ADDR=127.0.0.1:6060` to the `riot-server` service's `environment:` list.

#### Reaching pprof in Docker

Inside a container, loopback means the container itself. A published port (`-p 6060:6060`) cannot reach a loopback listener, so run the client inside the container with `docker exec`. The image is Alpine-based and includes BusyBox `wget`.

```bash
# Heap profile, saved to the host
docker exec riot-server wget -qO- http://127.0.0.1:6060/debug/pprof/heap > heap.pb.gz

# 30-second CPU profile (the request blocks for 30 seconds)
docker exec riot-server wget -qO- "http://127.0.0.1:6060/debug/pprof/profile?seconds=30" > cpu.pb.gz

# Inspect a saved profile on a machine with Go installed
go tool pprof heap.pb.gz
```

Replace `6060` with the port you configured, and `riot-server` with your container name if you changed it.

If you run the server directly on a host (not in a container), use `curl http://127.0.0.1:6060/debug/pprof/heap` from that host.

#### Security notes

- pprof endpoints are unauthenticated. Anyone who can open a connection to the listener can read heap contents, goroutine stacks and command-line arguments, and can start 30-second CPU profiles. Enable it only while you investigate, then unset the variable and restart.
- Under `network_mode: host`, or when running the binary directly, the listener binds the host's loopback, so every local user on that host can reach it.
- The listener shuts down together with the server.

---

## Agent Installation

### One-liner (Linux / macOS)

```bash
curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://<server-ip>:7331
```

This will:
1. Detect your architecture
2. Download the correct agent binary from GitHub Releases (Linux: installed `root:root`)
3. Linux only: authenticate the release manifest with the pinned Ed25519 key, then verify and install the matching signed apply helper, maintenance helper and fixed service. Missing, altered or unsigned assets abort before replacing the running install.
4. Write a default config to `/etc/riot/agent.yaml`
5. On first connect, automatically pin the server's TLS certificate (TOFU)
6. Install and start a systemd service (Linux) or print manual run instructions (macOS)

An explicit root installer re-run migrates an existing host, preserving configuration and device identity while backing up managed files. It refuses active or unresolved maintenance before stopping the agent. On a registered Linux host with a running agent, it also tells the server just before the stop, so the restart does not page you; see [Installer reinstalls](#installer-reinstalls). Use an installer and release that both publish the new privileged assets; see [Upgrading existing agents](#upgrading-existing-agents) and the [maintenance migration guide](docs/ops/os-maintenance.md). The first release containing MAINT-RECOVERY-001 is required; v2.72.0 does not contain these assets.

Apt hosts require apt 2+, systemd 241+, sudo 1.9.10+, Python 3, and either OpenSSL 3 or Python 3 with `cryptography` already installed. The installer does not bootstrap prerequisites through apt or pip, with one exception: on any systemd Linux host without a D-Bus system bus (for example DietPi minimal images) it installs and starts `dbus` (apt or dnf) before taking package locks. The agent runs as the unprivileged `riot` user, and non-root `systemctl` needs the system bus; without it every host action (reboot, OS update, agent update) is refused. Unsupported versions fail before replacing the install; arrange prerequisite upgrades separately.

**Optional flags:**

```bash
# mTLS: enroll the agent with a bootstrap key (required when mTLS is enabled on the server)
curl ... | sudo bash -s -- https://server:7331 --bootstrap-key <KEY>

# Verify server cert fingerprint on first connect (shown in Settings > General)
curl ... | sudo bash -s -- https://server:7331 --fingerprint SHA256:xxxx

# Provide a registration key (if the server requires one)
curl ... | sudo bash -s -- https://server:7331 --key mykey

# Install a compatible published release (replace X.Y.Z; required helpers must exist)
curl ... | sudo bash -s -- https://server:7331 --version X.Y.Z

# Combine flags as needed
curl ... | sudo bash -s -- https://server:7331 --bootstrap-key <KEY> --key mykey --fingerprint SHA256:xxxx
```

By default, registration is **open** — any device that can reach the server can register. You can optionally set a registration key in **Settings > General** to gate registration. See [Device Registration and Re-registration](#device-registration-and-re-registration) for the exact rules, including what happens when an agent loses its API key.

### Installer questions

On a fresh install the installer asks which remote features to allow. Every question defaults to no. The installer asks all of them first, before it takes a package lock, stops the agent or changes a file. Pressing Ctrl-C at a question leaves the host untouched. You can answer a question and then see the installer refuse for another reason, such as an old `sudo`. Nothing has changed in that case.

| Order | Question | Platform | Config setting it controls |
|---|---|---|---|
| 1 | `Allow remote reboot from dashboard?` | Linux, macOS | `commands.allow_reboot` |
| 2 | `Allow remote shutdown from dashboard?` | Linux only | `commands.allow_shutdown` |
| 3 | `Allow remote OS patching from dashboard?` | Linux, macOS | `commands.allow_patching` |
| 4 | `Allow rIOt to apply DietPi updates during OS patching?` | Linux, only when DietPi is present | `commands.allow_extra_updaters` (`[dietpi]`) |
| 5 | `Allow remote terminal access to this host?` | Linux, macOS | `host_terminal.enabled` |
| 6 | `Allow remote exec into Docker containers?` | Only when Docker is installed | `docker.terminal_enabled` |

The shutdown question and the reboot question are independent. Each has its own config key and its own sudo rule. The DietPi question adds no sudo rule at all; see [DietPi updates](#dietpi-updates).

How each mode answers:

| Mode | Answer to every question, including shutdown |
|---|---|
| Interactive (default) | `y` or `yes`, in any case, means yes. An empty answer or any other text means no. The installer reads the answer from the terminal, so it works with `curl ... \| sudo bash`. |
| `-y` or `--yes` | Yes. The installer prints `y (auto)` after each question. This is the documented "enable all remote features" mode, so it includes remote shutdown. |
| `--non-interactive` | No. The installer asks nothing. |
| No terminal available | No. The installer asks nothing. |

**Only a fresh install asks.** The installer treats a run as fresh when `/etc/riot/agent.yaml` does not exist. A reinstall asks no question, whatever the flags (including `-y`). It keeps the existing config byte for byte and never edits it. To change a remote feature later, edit `/etc/riot/agent.yaml` yourself. For remote shutdown, also re-run the installer: see [Shutdown on Linux and the sudo rule](#shutdown-on-linux-and-the-sudo-rule).

**Reinstall with `--bootstrap-key`.** If a config already exists, the installer deletes it and writes a new one, and it asks no questions. Every remote feature in the new config is off, including shutdown, and the installer adds no poweroff rule. If the host had shutdown enabled before, turn it on again as described in the shutdown section.

**Reinstall and remote shutdown.** On Linux, a reinstall decides the poweroff rule from the existing config and prints one line saying what it decided:

```text
==> Remote shutdown: commands.allow_shutdown is true in /etc/riot/agent.yaml; granting sudo systemctl poweroff
==> Remote shutdown: not enabled in /etc/riot/agent.yaml; no poweroff grant (only a plain "allow_shutdown: true" under "commands:" in a regular file enables it)
==> Remote shutdown: re-enrolment resets remote features; no poweroff grant
```

The first line appears when the config enables shutdown. The second appears when it does not. The third appears with `--bootstrap-key`. A fresh install prints no such line.

**Config file safety checks.** On Linux, the installer stops with an error, before it changes anything, when `/etc/riot/agent.yaml` exists but is not a regular file (for example a directory or a broken symlink), or when the file has more than one hard link. Move the file aside and re-run. The installer repeats the check after it locks `/etc/riot`. If the check fails then, the installer leaves the agent stopped and restores the previous sudo policy where it had added a poweroff rule. Fix the file and re-run the installer.

### Device Registration and Re-registration

Every agent calls `POST /api/v1/devices/register` when it starts and once every 24 hours afterwards. The request carries a `device_id` when the agent has one. The server treats a device ID as a label, not a secret. Knowing a device's ID never lets a caller change that device's record or obtain a key for it.

#### New devices

A request with no `device_id`, or with a `device_id` the server does not know, creates a new device. The server assigns the device ID and issues the device's first API key. These rules are unchanged by the re-registration rules below.

- If no registration key is set, any caller that can reach the server can register. This is the default.
- If a registration key is set in **Settings > General**, the request must carry it in the `X-rIOt-Key` header. The agent sends the value of `server.api_key`. A missing or wrong value returns `401`.
- A device's own API key is never accepted as a registration key for a device ID the server does not know.

The server compares the registration key in constant time.

> **Registration key source.** Device registration enforces only the registration key saved in **Settings > General**, which the server stores in its database. The `RIOT_API_KEY` environment variable is loaded at startup but is not currently checked by device registration, so setting it does not gate registration. Enforcing it is a tracked follow-up.

#### Existing devices

A request that names a device the server already knows must prove the caller is that device. The server applies the first rule below that matches:

| Order | What the request presents | Result |
|---|---|---|
| 1 | An API key that belongs to a different device | `403` |
| 2 | A verified client certificate issued to a different device | `403` |
| 3 | The device's own API key in `X-rIOt-Key` | Accepted. The record updates. No new key is issued. |
| 4 | A verified, unrevoked mTLS client certificate issued to this device | Accepted. The record updates and the server issues a new API key. Existing keys stay valid. |
| 5 | The registration key, and the device has no API key at all | Accepted. The record updates and the server issues the device's first API key. |
| 6 | Anything else | `401` |

Notes:

- **Own key (rule 3).** This is the normal path for a running agent. It works whether or not a registration key is set. The agent's startup and 24-hour re-registrations use it.
- **Client certificate (rule 4).** The server trusts only a certificate that the TLS handshake verified against the rIOt CA and that is not revoked. It ignores certificate information in HTTP headers, so a reverse proxy that terminates TLS cannot supply one. If the server cannot read the revocation list, the certificate counts for nothing. The certificate path needs mTLS enabled on the server. It is unavailable in Let's Encrypt (autocert) TLS mode. This is how a device enrolled with mTLS obtains its API key without an operator.
- **Registration key (rule 5).** This path is for a device that exists but has no API key, such as one created by mTLS enrollment on a server that has a registration key. It works once. After the server issues a key, the registration key no longer works for that device. If the server cannot check whether the device has a key, the request fails with `401`.
- **Mixed credentials.** Rules 1 and 2 run first. A request that presents another device's key or certificate gets `403` even when it also presents a valid proof for the named device.
- **No side effects on rejection.** A rejected request changes nothing. The server does not update the record, issue a key, broadcast to dashboards, or raise a device-online event.
- **No agent configuration change.** Agents already send their own key on every registration request, so existing agents need no change.

#### Rejection responses

Every rejection from the registration endpoint uses one of two fixed bodies. The body never contains a device ID, hostname, key, or reason. It is identical whether or not the named device exists.

| Status | Body | Meaning |
|---|---|---|
| `400` | `{"error":"invalid request body"}` | The body is not valid JSON or is larger than 1 MiB. |
| `401` | `{"error":"unauthorized"}` | No acceptable proof for an existing device, or a missing or wrong registration key for a new device. |
| `403` | `{"error":"forbidden"}` | The request presented a key or certificate that belongs to a different device. |

The server logs each rejection as one `WARN` line, `device registration rejected`. The line holds the device ID (shown as `[invalid]` when it contains unexpected characters), the client address, a short reason class such as `wrong-device-key`, and the status. It never contains a key. Earlier versions returned the text `invalid registration key`. That text no longer appears.

The endpoint allows 10 requests per minute per client address.

#### Recovering an agent that lost its API key

An agent keeps its API key in `server.api_key` in `/etc/riot/agent.yaml` and its device ID in a separate ID file. If the agent is reinstalled or its config is wiped, it can end up with the ID file but no API key. The server then rejects its registration with `401`, because a device ID alone proves nothing. The agent log shows `registration failed: {"error":"unauthorized"}`, and the device stays offline.

This applies whenever the device already has an API key on the server, with or without a registration key. An agent cannot use the registration key to reclaim a device that already has an API key.

Pick one of the two recovery options.

**Option 1: Rotate the key and keep the device.** The device keeps its history and settings.

1. As an admin, rotate the device's key with `POST /api/v1/devices/{id}/rotate-key`. The dashboard has no Rotate key button, so call the admin API. Both requests send an `Origin` header, which the [origin check](#origin-check-on-state-changing-requests) requires. Behind a reverse proxy, use the public URL and see that section. Replace `<server>`, `<password>`, and `<device-id>`:

   ```bash
   curl -sk -c riot-session.txt -H 'Origin: https://<server>:7331' \
     -H 'Content-Type: application/json' \
     -d '{"password":"<password>"}' https://<server>:7331/api/v1/auth/login
   curl -sk -b riot-session.txt -H 'Origin: https://<server>:7331' -X POST \
     https://<server>:7331/api/v1/devices/<device-id>/rotate-key
   rm riot-session.txt
   ```

   The second command returns `{"api_key":"...","device_id":"..."}`. Rotation deletes every existing key for the device and creates the one it returns. Copy the key now. The server stores only a hash and cannot show it again.

2. On the device, set the key in `/etc/riot/agent.yaml`:

   ```yaml
   server:
     api_key: "<the key from step 1>"
   ```

3. Restart the agent: `sudo systemctl restart riot-agent`.

**Option 2: Register as a new device.** Use this when the old device is not worth keeping.

1. On the device, delete the ID file:
   - Linux and macOS: `sudo rm /etc/riot/id`
   - Windows: delete `%PROGRAMDATA%\riot\id`
   - If `/etc/riot/agent.yaml` has an `agent.device_id` value, remove it. The agent reads that value before the ID file.
2. If the server has a registration key, set it as `server.api_key` in `/etc/riot/agent.yaml`.
3. Restart the agent: `sudo systemctl restart riot-agent`.
4. The agent registers as a new device with a new device ID. In the dashboard, delete the old device. It stays listed as offline until you do.

**mTLS-enrolled devices.** A device enrolled with mTLS recovers on its own. Its client certificate proves its identity, and the server issues a fresh API key on its next registration. It needs no operator action unless its certificate has been revoked.

#### Future-dated timestamps

Agents send a timestamp with every heartbeat and telemetry snapshot. The server trusts a timestamp that is in the past or up to 5 minutes ahead of the server clock. A timestamp more than 5 minutes ahead is replaced with the time the server received the request. The data is still stored and the request still succeeds. The server never rejects telemetry because of its timestamp.

This stops an agent with a wrong clock from posting a snapshot dated far in the future, which would otherwise count as the device's latest state until that date.

- Timestamps in the past are stored as sent.
- The server writes at most one `WARN` log line per device and kind (snapshot or heartbeat) per hour for clamped timestamps. The line reports how many were clamped since the previous line.
- Every query that reads snapshots or heartbeats also skips stored rows dated more than 5 minutes ahead. Such a row can exist only if an earlier version stored it. The server does not delete or edit it. A device whose only rows are future-dated shows no data until it posts one fresh snapshot.
- The fleet table never shows a negative relative time. A timestamp ahead of the browser clock shows as `0s ago`.

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

When Docker is enabled, `doctor` also checks every Compose project directory against the agent's [compose directory trust](#compose-directory-trust) rules and prints the rule that fails. Run it as the agent's service user (`sudo -u riot riot-agent doctor`) so it sees what the agent sees.

On Linux, when auto-update is enabled (the default), `doctor` also prints an **Agent Update** section for the fixed signed-update privilege and helper. On apt hosts with `commands.allow_patching: true`, it also checks detached maintenance prerequisites, trusted helper paths, the lock-aware apply protocol, and effective service/sudo policy. Run it as the service account: `sudo -u riot /usr/local/bin/riot-agent -config /etc/riot/agent.yaml doctor`. These checks list privileges and inspect status; they do not run repair or start maintenance. See the [maintenance guide](docs/ops/os-maintenance.md).

On Linux, when `commands.allow_shutdown` is `true`, `doctor` also prints a **Remote Shutdown** section. It asks sudo whether the `riot` user may run `systemctl poweroff` without a password. It lists the rule and never runs the command. It cannot prompt for a password, and it gives up after 10 seconds. Run as root, it checks the `riot` user's rules, not root's. The section appears only on Linux and only when `allow_shutdown` is `true`. With `allow_shutdown: false` it prints nothing. The `Shutdown:` line under **Permissions** is unchanged. The two outcomes:

```text
✓ shutdown sudo rule present: riot may run systemctl poweroff without a password
✗ shutdown sudo rule missing or unverifiable (systemctl poweroff): <reason>; remote shutdown will report failed
```

`<reason>` is `not allowed without a password`, `sudo not found` or `timed out after 10s`. The fail line is followed by a hint to re-run the install script as root. A missing rule means the host is in the "config only" state: `allow_shutdown: true` is set but the installer has not added the rule. See [Shutdown on Linux and the sudo rule](#shutdown-on-linux-and-the-sudo-rule).

`doctor` also prints a **Collection Limits** section with the values the agent actually uses, after clamping. On Linux it prints a **Memory Limit** section for the installed service unit. See [Agent Resource Bounds](#agent-resource-bounds).

### Agent Resource Bounds

An agent keeps its disk, memory and network use bounded, even when the server is unreachable or rejects its data. The collection limits below apply with the agent binary update. Memory policy, verifier sweep and detached maintenance helpers/units need a root installer re-run ([Upgrading existing agents](#upgrading-existing-agents)).

#### Configurable limits

Six optional keys in the [Agent Config Reference](#agent-config-reference) set the limits. An integer outside its range is clamped to the nearest end of the range. A value that is not an integer falls back to the default. The agent logs one warning per adjusted key and starts normally. No existing config needs an edit. `riot-agent doctor` prints the values in use under **Collection Limits**.

#### Container and journal logs

- One container-log pass sends at most 5,000 lines and 4 MiB across all containers. The agent never scans more than 20,000 lines or 8 MiB of one container's stream.
- The default keeps the newest 50 lines and 16 KiB per container. Later passes resume from the previous cursor with a bounded scan and report omitted lines.
- The journal collector no longer passes `-n 500` to `journalctl`. It streams the journal through a ring that keeps the newest 5,000 entries and at most 2 MiB, and scans at most 500,000 lines or 256 MiB per pass. The agent logs how many entries it skipped. Authentication-failure counting covers every line read.
- A line longer than `max_line_bytes` is cut at a character boundary and ends in ` [truncated]`. The `fetch_logs` command applies the same cut.
- A telemetry body over 6 MiB loses its container logs first, then its journal entries.

#### Offline buffer

While the server cannot take telemetry, the agent keeps snapshots in a local SQLite file. The file holds at most 100 items and at most `agent.buffer_max_mb` of payload bytes. The cap counts payload bytes, so the file on disk is about that many MiB plus SQLite overhead. When a new snapshot does not fit, the agent drops the oldest items first and logs a warning. The file returns freed space to the disk after a flush.

- A buffer file from an older agent is converted to this layout once, at the first start. If the conversion fails (for example the disk is full), the agent keeps the file and tries again at the next start.
- A corrupt buffer file is moved aside with its `-journal`, `-wal` and `-shm` files, replacing any earlier set. The agent then starts with an empty buffer.
- The agent checks the file's integrity at every start. At the 64 MiB maximum this takes a few seconds on a slow disk.

#### How the agent treats the server's answer

The agent sends buffered snapshots oldest first and removes each one as soon as the server has decided its fate. A snapshot the server acknowledged is never sent twice.

| Server answer | Result |
|---|---|
| 2xx | Delivered. The item is removed |
| 400, 404, 413 and any other 4xx except those below | Permanent. The item is dropped with a warning, so it is never retried forever |
| 401, 403, 408, 429 | The item is kept. A server that briefly cannot look up keys can answer 401, so a 401 never costs data |
| 5xx, or no answer | The item is kept and retried on a later tick |

A live push that the server rejects permanently is dropped, not buffered. After a 429, 401 or 403 the agent sends no telemetry (it buffers instead) until the `Retry-After` time has passed, clamped to between 1 second and 1 hour. Without that header it waits 30 seconds, doubling after each further refusal up to 10 minutes. A 5xx that carries `Retry-After` is handled the same way. A success resets the wait. Heartbeats are not affected.

#### First registration and the config directory

Before a new agent asks the server for an identity, it proves it can durably write one. It writes and syncs 4 KiB in the directory of the device ID file and in the directory of `agent.yaml`, then removes the probe files. If either write fails (the disk is full or the directory is read-only), the agent logs `cannot write the device identity; not registering`. It waits 5 seconds, then 10, 20 and so on up to 10 minutes, and tries again. It does not contact the server until the probe passes. This stops a full disk from creating a new ghost device on every restart. A new agent on a read-only config directory therefore waits in this pre-flight by design. Fix the permissions or free the space, and the agent continues without a restart.

If the server issues an identity that the agent then cannot save, the agent logs the path and the error, waits 60 seconds and exits with status 1. A periodic re-registration that returns the same device ID only logs a warning when it cannot save. A different ID that cannot be saved is fatal in the same way.

**The agent needs write access to the config directory, not only to `agent.yaml`.** The agent saves `agent.yaml` by writing a temporary file beside it, syncing it and renaming it over the original, so a crash or a full disk cannot leave a truncated file. Installs made by `install.sh` are unaffected, because the installer gives the agent user ownership of `/etc/riot`. A manual install must be adjusted in these cases:

| Setup | What happens | Fix |
|---|---|---|
| Config directory owned by root, `agent.yaml` writable by the agent user | The temporary file cannot be created. A new agent waits in the pre-flight | Make the directory writable by the agent user |
| `agent.yaml` is a symlink | The rename replaces the symlink with a regular file | Point `-config` at the real file, in a directory the agent can write |
| `agent.yaml` is a single-file container bind mount | The rename fails because the mount point is busy | Mount the directory instead of the file |

#### Self-update

A binary download stops at 64 MiB. A larger announced size is refused without being read, and a longer stream is cut off. The partial file is deleted and the agent reports `agent_update_failed`. At start the agent removes its own staged update files (`riot-agent.update`, `.checksums`, `.sig`) once they are older than one hour. The update verifier (`riot-agent-apply`) does the same in both modes for its leftover `.riot-agent.apply.<16 hex characters>` temp files. It removes only regular files older than one hour, in a directory that is owned by root and not writable by group or others.

#### Memory limit

The unit written by `install.sh` sets `MemoryMax=50%`, `Environment=GOMEMLIMIT=200MiB` and `OOMPolicy=continue`. `MemoryMax` caps the agent's whole cgroup at half of the host's RAM. `GOMEMLIMIT` makes the Go runtime collect garbage well before that cap. `OOMPolicy=continue` keeps systemd from stopping the unit when it kills one process in it for running out of memory. `MemoryMax=50%` needs systemd 239 or newer and `OOMPolicy` needs systemd 243 or newer. An older systemd ignores a directive it does not know and logs a warning. cgroup v1 hosts do not enforce `MemoryMax`.

On Linux, `riot-agent doctor` prints a **Memory Limit** section from `systemctl show riot-agent`: `MemoryMax` in bytes (`infinity` is a failure), `OOMPolicy`, `KillMode` and whether `GOMEMLIMIT` is set. A host that shows a failure or warning there has not had the installer re-run. `doctor` cannot show whether the verifier sweep is installed.

> **Package maintenance isolation.** After the root migration for MAINT-RECOVERY-001, supported apt updates run in a fixed independent `riot-maintenance.service`, outside the agent cgroup and its memory cap. Agent restart detaches observation; it does not stop that worker. Missing or unsafe privileged installation refuses apt maintenance. DNF retains its existing inline behavior and interruption risks. Package scripts, host OOM and power loss can still interrupt or change the system; see [OS maintenance and recovery](docs/ops/os-maintenance.md).

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

   The agent user must be able to write to the directory that holds `agent.yaml`, not only to the file. See [First registration and the config directory](#first-registration-and-the-config-directory).

4. Run the agent:

   ```bash
   riot-agent -config /etc/riot/agent.yaml
   ```

   A manual install does not create the update verifier, the update sudoers rule, or the systemd unit, so it cannot complete automatic updates. Use the installer for managed hosts, or set `agent.auto_update: false` and update the binary yourself.

### Windows

Download `riot-agent-windows-amd64.exe` from [Releases](https://github.com/rbretschneider/rIOt/releases) and create a config file at `%PROGRAMDATA%\riot\agent.yaml` with the same format as above. Run from a terminal or set up as a Windows service using [NSSM](https://nssm.cc/) or `sc.exe`.

### Sudoers Rules

The root installer creates `/etc/sudoers.d/riot-agent` with platform-specific fixed privileges. Supported apt transactions run only through the signed worker; direct apt/dpkg sudo grants and legacy root-shell update grants are removed from this managed file. Unrelated administrator policies are preserved and must also be reviewed: a constrained managed file cannot cancel broader grants elsewhere.

| Rule or operation | Purpose |
|---|---|
| `/usr/bin/systemctl start --no-block riot-maintenance.service` | Start the one fixed independent apt worker; no caller-selected unit, arguments or environment |
| Anchored `apt-mark` hold/unhold argument expressions | Permit only package-name operands; sudo 1.9.10+ is required. Wildcard hold rules are unsafe and rejected. |
| Fixed DNF makecache/update/security-update commands and fixed dnf5 hold-fragment paths | Existing DNF behavior on DNF hosts; no new apt recovery capability |
| `/usr/bin/systemd-run --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply` | Run the signed root-owned update verifier, with maintenance exclusion |
| `/usr/bin/systemctl reset-failed riot-agent-update` | Clear a previous update unit failure |
| `/usr/bin/systemctl reboot` | Existing separate reboot privilege |
| `/usr/bin/systemctl poweroff` | Opt-in remote shutdown privilege (SHUTDOWN-SUDO-001). The installer writes this one rule only when `commands.allow_shutdown` is `true` for the run (see [Shutdown on Linux and the sudo rule](#shutdown-on-linux-and-the-sudo-rule)). The rule is `riot ALL=(root) NOPASSWD: <systemctl path> poweroff`, with no extra arguments and no wildcard. It does not allow `halt`, `kexec`, `suspend`, `poweroff --force` or the `shutdown`, `poweroff` and `halt` programs. |
| Exact nginx `-t` / `-T` and SMART read commands, when present | Collector reads; SMART rules name discovered disks rather than allowing arbitrary options |

The DietPi extra-updater step (EXTRA-UPD-001) adds **no** sudoers rule. It runs inside the root worker that the `systemctl start` rule above already starts. See [DietPi updates](#dietpi-updates).

Executable paths are resolved on the host. The installer validates candidate and complete sudo policy with `visudo`, and service units with `systemd-analyze verify`. Do not restore broad shell, bare `smartctl`, or wildcard hold rules to bypass a failed check. See the [fixed privilege contract and migration procedure](docs/ops/os-maintenance.md).

**Existing installs**: If you add the `webservers` collector to an existing device, add the nginx sudoers rules manually:

```bash
sudo tee -a /etc/sudoers.d/riot-agent > /dev/null << 'EOF'
riot ALL=(root) NOPASSWD: /usr/sbin/nginx -t
riot ALL=(root) NOPASSWD: /usr/sbin/nginx -T
EOF
sudo visudo -cf /etc/sudoers.d/riot-agent
```

New installs via `install.sh` include all rules automatically.

**Reboot-class holds on existing agents:** an agent-only update cannot install the required privileged rules or helpers. Re-run the matching released root installer and verify the effective policy with doctor. On apt, the new policy constrains hold/unhold names at the sudo boundary as well as in the agent; legacy wildcard rules must be migrated. See [OS maintenance and recovery](docs/ops/os-maintenance.md).

### Agent Config Reference

| Field | Default | Description |
|---|---|---|
| `server.url` | `http://localhost:7331` | rIOt server URL |
| `server.api_key` | — | Registration key for a first registration (only needed if the server requires one). After registration the agent replaces it with its own per-device API key. To recover an agent that lost its key, set a rotated key here (see [Recovering an agent that lost its API key](#recovering-an-agent-that-lost-its-api-key)) |
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
| `agent.auto_update` | `true` | Automatically install agent updates when available. Updates are installed only if the release signature verifies (fail-closed). Set to `false` to disable. Dev builds never auto-update. See [Agent Auto-Update and Release Signing](#agent-auto-update-and-release-signing) |
| `agent.buffer_max_mb` | `50` | Cap on the payload bytes of the offline telemetry buffer, in MiB, range 5-64. The file on disk is about this size plus SQLite overhead. The buffer also holds at most 100 items. See [Agent Resource Bounds](#agent-resource-bounds) |
| `collectors.enabled` | all | List of collectors to run |
| `collectors.smart_interval` | `4h` | Interval between SMART disk health scans (e.g. `1h`, `4h`, `12h`) |
| `collectors.container_logs.max_lines` | `50` | Newest log lines kept per container per collection, range 1-1000 |
| `collectors.container_logs.max_bytes` | `16384` | Log bytes kept per container per collection (newest first), range 1024-262144 |
| `collectors.container_logs.max_line_bytes` | `16384` | Container-log lines longer than this are cut at a character boundary and end in ` [truncated]`, range 1024-16384 |
| `collectors.logs.max_entries_per_pass` | `5000` | Newest journal entries sent per collection, range 500-20000 |
| `collectors.logs.max_line_bytes` | `4096` | Journal messages longer than this are cut the same way, range 1024-16384 |
| `collectors.webservers.nginx.access_log` | — | Absolute path to the nginx access log file. When set, the `webservers` collector parses the log each interval and includes HTTP status code counts in telemetry. Omit to disable access log monitoring (no default path is assumed). |
| `docker.enabled` | `auto` | Docker collection mode: `auto` (detect), `true`, `false` |
| `docker.socket_path` | auto-detect | Override the Docker socket path |
| `docker.collect_stats` | `true` | Collect per-container CPU/memory stats |
| `docker.terminal_enabled` | `false` | Allow remote `docker exec` from the dashboard |
| `docker.check_updates` | `true` | Check container registries for newer images (30-min cache) |
| `docker.compose_roots` | not enforced | List of absolute directories, for example `[/opt/stacks]`. When set, compose updates run only in a project directory inside one of them. An entry that is empty or relative makes every compose update fail until it is fixed. Required on Windows. Restart the agent after changing it. See [Compose directory trust](#compose-directory-trust) |
| `commands.allow_reboot` | `false` | Allow remote reboot command from the dashboard, and the reboot that **Reboot automatically if the patch requires it** requests after a manual patch (see [Manual patch and reboot](#manual-patch-and-reboot)). Nothing the server sends overrides it. The agent also reports this value to the server (display-only) so the patch badge can say a reboot is manual — see [Patch badge](#patch-badge) |
| `commands.allow_shutdown` | `false` | Allow remote shutdown command from the dashboard (device requires physical or out-of-band access to power back on). The shutdown waits for the dpkg lock; see [Reboots and the dpkg lock](#reboots-and-the-dpkg-lock). On a Linux host set up by the installer, shutdown also needs the one `systemctl poweroff` sudo rule. Setting this key to `true` does not add the rule: re-run `install.sh` as root, and `doctor` reports the rule's state. See [Shutdown on Linux and the sudo rule](#shutdown-on-linux-and-the-sudo-rule) |
| `commands.allow_patching` | `false` | Allow remote OS patching and enable-auto-updates commands from the dashboard |
| `commands.allow_extra_updaters` | `[]` (none) | List of platform updaters outside apt that rIOt may apply after apt during an OS patch. The only value today is `dietpi`. Write it as `allow_extra_updaters: [dietpi]`, with plain lowercase names. Setting it is only the agent-side half of the opt-in: it also needs `commands.allow_patching: true` and a root re-run of `install.sh`, which writes the root-side half. An unknown name is ignored with one warning in the agent log per config load. See [DietPi updates](#dietpi-updates) |
| `commands.hold_reboot_class` | `false` | Hold GPU driver + kernel packages at the OS level so they can only be applied during a maintenance-window patch run (Linux apt/dnf5 only — see [Reboot-Class Package Gating](#reboot-class-package-gating)). The agent applies reboot-class packages only when the run asks for them, this setting is `true` and hold enforcement works; otherwise it never applies them or reboots automatically. Requires the server-side `reboot_class: gated` policy and the new sudoers rules from `install.sh`. |
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
| `updates` | Package manager, pending updates, security updates, kernel update status, unattended-upgrades status; classifies each pending update as `gpu_driver` / `kernel` / standard and detects reboot-required state (on apt: `/var/run/reboot-required`, or an installed kernel newer than the running one; on dnf: `dnf needs-restarting -r`); on apt it also reports whether the package lists are `fresh`, `stale` or `unknown` (`lists_freshness`); on a DietPi host it also reports DietPi as an extra updater (`extra_updaters`, read-only, no new collector name needed) — see [Reboot-Class Package Gating](#reboot-class-package-gating), [Manual patch and reboot](#manual-patch-and-reboot) and [DietPi updates](#dietpi-updates) |
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

**Note:** The `usb` and `hardware` collectors are Linux-only. They read from sysfs (`/sys/bus/usb/devices/`, `/sys/bus/pci/devices/`, `/sys/block/`, `/sys/class/tty/`) and use the system ID databases (shipped with `usbutils` or `hwdata`) to resolve vendor/product names. No additional packages are required for basic hardware info. **SMART disk health** requires separately installed `smartmontools`. The installer grants fixed SMART read commands for discovered disks; re-run the reviewed installer when adding disks. SMART scans run every 4 hours by default (configurable via `collectors.smart_interval` in agent YAML).

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

### Expected Activity

Container deaths and device offline transitions that an action started from rIOt causes are recorded as `info` events and do not alert. The same holds for the restart of an agent that updates itself ([agent self-updates](#agent-self-updates)) and for the restart that an `install.sh` re-run causes ([installer reinstalls](#installer-reinstalls)). See [Expected Activity](#expected-activity).

### Alert Templates

Pre-built templates are available in Administration > Alert Rules > "Create from Template" for common scenarios. Templates pre-fill the metric, operator, threshold, and severity — just add a target name if needed.

### Alert Scoping (Include / Exclude)

Every alert rule supports **Include** and **Exclude** device lists. When both are empty the rule is global (applies to all devices). Add hostnames to the Include list to scope a rule to specific devices, or add hostnames to the Exclude list to exempt devices from a global rule. Exclude always wins — a device in both lists is excluded. Tags are also supported for matching.

### One-Click Alert Creation

From the device detail view, click the alert icon next to any service, process, network interface, USB device, UPS, or GPU to instantly create a targeted alert rule.

### Event Acknowledgement

The Alerts tab in the navigation bar shows a red badge with the count of unacknowledged warning/critical events. Events can be acknowledged individually or in bulk from the Alerts page. The badge updates in real-time via WebSocket.

---

## Expected Activity

When you, or one of your schedules, start an action from rIOt, the action disrupts things on purpose. A container update kills and recreates a container. A reboot takes a device offline. rIOt knows about these actions, so it does not alert on the disruption they cause. It records the disruption instead, names the action, and alerts only when something is still down afterwards.

The server does all of this. No agent upgrade, no agent configuration change and no `install.sh` re-run is needed.

### How it works

Every rIOt command is a record (see [Command Lifecycle](#command-lifecycle)). Twelve actions open an **expected-activity window** for their command. The window opens when the server dispatches the command to the agent. A command that is still queued opens no window and suppresses nothing. A window applies to user commands and to scheduled commands alike: the auto-update policies and the automated patch run.

Two windows do not come from a command: the restart of an agent that updates itself on its own schedule, and the restart that an `install.sh` re-run causes. Each has its own rules and its own 10-minute bound. See [Agent self-updates](#agent-self-updates) and [Installer reinstalls](#installer-reinstalls).

While a window covers an event, rIOt:

- stores the event as an `info` event of its usual type (`container_died` or `device_offline`), linked to the command, instead of a `warning`;
- sends no notification on any channel;
- names the action and who started it in the message.

Examples:

- `Container plex died during Docker update by ryan (expected)`
- `Container plex died during Docker update by auto-update policy for plex (expected)`
- `Container sonarr died during Docker stack redeploy of media by ryan (expected)`
- `Device vermont went offline during Reboot by ryan (expected, back by 2:32 PM EDT)`
- `Device vermont came online after Reboot by ryan (offline 1m42s)`

Nothing is dropped. Every suppressed event stays in the event list. The actor phrase is `by <name>` for a command you started, `by auto-update policy for <target>` for an auto-update policy, and `by the auto-patch schedule` for the automated patch run. A command from a session without a display name reads `by an operator`.

### Which actions open a window

| Action | What the window suppresses | How long it stays open after the command ends | Longest it can last, counted from dispatch |
|---|---|---|---|
| `reboot` | `device_offline` for the device, and `container_died` for every container on it | Until the device has been back online for 3 minutes after an observed return, and at most 15 minutes after success | 35 minutes (20-minute execution deadline plus 15) |
| `os_update` (patch, with or without a reboot, manual or automatic) | The same as `reboot` | Until the device has been online for 3 minutes, and at most 15 minutes after success. A patch that does not reboot closes 3 minutes after success | 55 minutes (40 plus 15) |
| `shutdown` | The same as `reboot` | After success, the first offline moves the device to the [powered-off state](#powered-off-after-a-shutdown). If the device never goes offline, the window closes 15 minutes after success | 35 minutes (20 plus 15) |
| `agent_update` (one device, or the fleet **Update agents** button) | `device_offline` for the device only. A container death during an agent update still alerts | Until the device has been back online for 1 minute after an observed return, and at most 10 minutes after success | 25 minutes (15 plus 10) |
| `docker_update`, `docker_bulk_update`, `docker_restart`, `docker_stack_redeploy`, `docker_stack_restart`, `docker_stack_up` | `container_died` for the containers the command targets | 2 minutes after the command ends, whatever the outcome | The execution deadline plus 2 minutes: 37 minutes for a single update or a stack action, 17 for a restart, up to 6 hours 2 minutes for a bulk update |
| `docker_stop`, `docker_stack_down` | `container_died` for the containers the command targets | 2 minutes after the command ends | The execution deadline plus 2 minutes: 17 minutes for a stop, 37 for a stack down |

Notes on the table:

- A container is a target when its name matches one of the command's container targets, or when it belongs to a Compose project that the command targets.
- "An observed return" for `reboot` and `agent_update` means the agent registered again, or the device went from offline to online, after the command finished. A device that never dropped off does not close the window early. The window then closes at its bound.
- A window never outlives its longest time, even when no heartbeat, event or result ever arrives. After that, normal alerting resumes on its own.
- Several windows can be open on one device. An event is suppressed when at least one open window covers it. The event links to the command that holds the container's own target when there is one.
- When the command fails, times out or is cancelled, the window stops suppressing `device_offline` at once. It still covers container deaths for 2 minutes, so that a rollback or a Docker restart can be seen.
- Actions outside the table open no window, so their disruption alerts as before: `docker_start`, `docker_stack_pull`, `docker_remove_items`, `docker_inventory_refresh`, `docker_check_updates`, `enable_auto_updates`, `fetch_logs`, `run_device_probe` and `agent_uninstall`.

### What always alerts

- **Out-of-memory kills.** `container_oom` is never suppressed. When a window covers the container, the alert names the action that is in progress.
- **Containers the command does not target.** A container-class window covers only its own targets. An unrelated container that dies during an update alerts as it does without a window. This replaces the earlier per-device "update in progress" counter, which downgraded every death on the device.
- **Containers excluded from a rule.** A container that you excluded from the `container_died` rule stays excluded, inside or outside a window.
- **Anything after the window closes.** A death or an offline transition after the window ends alerts normally.
- **Failures after the action.** See the next section.

### Escalation: one alert that names the action

When a window ends and something is still down, rIOt raises one **escalation**. It goes through your existing alert rules: `device_offline` for a device, `container_died` for a container. The rule's severity, scope and notify setting apply. When no enabled rule matches, rIOt stores the escalation as a `warning` event and sends no notification.

| Situation | Escalation |
|---|---|
| A host or agent command ends `failed` or `timed_out` while the device is offline | One escalation within a minute, for example `hawaii is offline: Reboot by ryan timed out (command 1a2b3c4d)` |
| A `reboot`, `os_update` or `agent_update` succeeded and the device is not back at the return bound (15 minutes for a host, 10 for an agent) | One escalation, for example `vermont still offline 15m after Reboot by ryan (command 1a2b3c4d)` |
| A host or agent command is still active at its longest time and the device is offline | One escalation that says the action sent no result |
| When a window closes, a container whose death it suppressed has not been seen running again | One escalation per container, for example `Container sonarr still not running 2m after Docker stack redeploy by ryan (succeeded, command 9f8e7d6c)` |

The rules for escalations:

- A container counts as running again when rIOt sees a start event for it, or a later telemetry snapshot that lists it as running. The check does not apply to `docker_stop`, `docker_stack_down` and `shutdown`, because those leave things down on purpose.
- When the device is offline as the window closes, the container check waits. It runs 3 minutes after the device returns. If the device does not return within 24 hours, rIOt skips the check and stores one warning event that says so.
- Each command and device, and each command and container, produces at most one escalation, including across server restarts. An escalation ignores the cooldown of the rule and does not start one.
- One command raises at most 20 container escalations. After that, one summary escalation gives the number of further containers that are still not running.
- Escalation notifications are capped at 5 per device per rolling hour. The events are always stored. Notifications beyond the cap are held, and one summary notification follows: `<N> escalation notifications for <host> were held since <h:mm AM/PM zone>; see Alerts`. That is at most 6 pushes per device per hour. A server restart resets the count.
- **Escalation messages contain the operator's display name** (`by <name>`), which can be an SSO full name or an email address. Notification channels forward the message text, including to third-party services such as ntfy, Pushover, Telegram, Discord and Slack. Keep that in mind when you route the `container_died` and `device_offline` rules.

### Powered off after a shutdown

When a `shutdown` command succeeds and the device then goes offline, the device enters the **powered-off state**. rIOt stores an `info` event, `Device <host> powered off by ryan at 2:02 PM EDT`, and raises no `device_offline` alert or escalation for it. The state has no time limit and no reminder. A device that you shut down on purpose stays quiet until it comes back.

- The state ends on any sign of life: a registration, or a fresh heartbeat. The online event then says `came online after Shutdown by ryan (powered off 72h)`.
- A powered-off row never hides a later outage. If the device returns and then goes offline again, that transition alerts normally.
- A `shutdown` that ends `failed` or `timed_out` does not create the state. A later offline alerts normally.
- rIOt keeps at most one powered-off record per device. The record survives a server restart.

### What you see on the dashboard

The **Devices** page (`/`) shows what each device is doing, taken from the server. A second browser, a reload, and an action started by a schedule all show the same label. An open page changes within 5 seconds, without a reload.

| Situation | Label |
|---|---|
| A host or agent command is queued | `<Action> queued`, for example `Reboot queued` |
| `reboot` running | `Rebooting` |
| `reboot` succeeded, device returning | `Rebooting, back by <time>` |
| `shutdown` running | `Shutting down` |
| Powered-off state | `Powered off by <name> at <time>` |
| `os_update` running | `Patching`, with the percentage when the agent reports one |
| `os_update` succeeded, device offline | `Rebooting after patch, back by <time>` |
| `os_update` succeeded, device online | `Patched, settling until <time>` |
| `agent_update` running | `Updating agent 2.77.0 → 2.78.0`, or `Updating agent` when a version is unknown |
| `agent_update` succeeded | `Restarting agent, back by <time>` |
| An [agent self-update](#agent-self-updates) is in progress | `Updating agent 2.78.0 → 2.80.0`, `by agent auto-update`, with the start time. It clears when the window ends: at the registration on the new version, at a failure, or at the 10-minute bound |
| One Docker action open | `Docker: updating plex`, `Docker: recreating stack media` |
| Two or more Docker actions open | `Docker: <N> actions in progress` |
| Escalated device | `<Action> failed: offline` or `Not back <bound> after <Action>`, in failure styling |

Beside the label, the page shows who started the action and when. The **device page** header shows the same label, with a link to the command.

- The label appears under the hostname, and the status dot stays visible. A device that is offline inside a window shows a hollow dot titled `Offline (expected)`.
- The **Offline** count excludes devices that are offline inside a window and devices in the powered-off state. A separate **Expected offline** card counts them. The card appears only when its count is above zero. An escalated device counts as offline.
- The failure label stays until the device next reports online, or for 24 hours after the escalation, whichever comes first.
- When a host window and a Docker window are open together, the host label shows.
- Times in labels follow the time zone and clock choice in the utility bar (see [Time Display](#time-display)). Times in event messages, such as `back by 1:32 PM EDT`, are written by the server in its display zone (`RIOT_DISPLAY_TZ`), in 12-hour form with a zone label, whatever the browser choice is, so the two can differ.
- The label `Rebooting (waiting for dpkg lock)` (or `Shutting down (waiting for dpkg lock)`) appears only when the agent reports that step, and only while the device is online and the command is running. Agents older than the first release after v2.88.0 do not report it, so a reboot that waits for the lock shows `Rebooting`. See [Agent-side activity reporting](#agent-side-activity-reporting).
- If `GET /api/v1/activity` fails, the page shows the plain status, as before.

### Agent self-updates

An agent updates itself on its own schedule when a new release is published, and it restarts. That is not a command, so it has no command record, but the server recognises it from the message the agent already sends and treats the restart as expected activity.

**How a self-update is recognised.** The agent sends `agent_update_started` with the message `Agent update started: 2.78.0 → 2.80.0`. The server accepts the message only in exactly that form: the arrow is `→`, both versions look like `2.80.0` (an optional leading `v`, three numbers, an optional suffix of up to 16 characters), they differ, and the whole text is at most 256 bytes of printable UTF-8. Anything else is treated as before: the event is stored as the agent sent it and nothing changes for the device's alerts. The agent's own `command_id` was never read before [agent-side activity reporting](#agent-side-activity-reporting), which reads it only under strict checks. A start counts only when it comes from the device's own authenticated agent. Every agent since v2.10.0 sends the message in this form.

**One record per self-update.** The server stores a **self-update record** for the device: its versions, the start time (the server's time, never the agent's), the originator `by agent auto-update` and the outcome. The start event, and the agent's later `agent_update_completed` and `agent_update_failed` events, are stored as sent and linked to the record.

In the device's command history the record appears as `Agent update 2.78.0 → 2.80.0` `by agent auto-update`. It is read-only: it has no cancel and no retry, and it is never sent to an agent. Opening it shows the start time, the outcome (`In progress`, `Returned, confirming`, `Succeeded`, `Failed` or `Not confirmed`), the result text and the linked events. The server's own events are always listed in full. The agent's events are limited to 50, with a count such as `N more agent events not shown`.

**Self-update or fleet command.** The server decides when it receives the start:

- A start while an `agent_update` command of the same device is in flight, or finished at most 10 minutes earlier, belongs to that command (an update started from the device page or by **Update agents**). It creates no record, and the label keeps the command's actor, for example `by ryan`.
- A command that is still queued, or that was never sent to the agent, does not capture a start.
- This timing rule is the fallback. An agent from the first release after v2.88.0 names its command or its own attempt in the event, and the server uses that instead (see [Agent-side activity reporting](#agent-side-activity-reporting)).
- A command dispatched while a record is open does not conflict with it. The record holds no conflict keys. The most recently dispatched of the two sets the label.

**Bounds.**

- A device has at most **one open record**. A repeated start while one is open is linked to it and does not extend the window.
- At most **6 records per device in any 24 hours**. The first start beyond that stores one `agent_self_update_paused` warning event, at most once per device per 24 hours, with no notification: `Agent self-update tracking paused on hawaii: more than 6 agent updates started in 24h; offline alerts during agent updates are not suppressed`. The start creates no record, so offline alerts work as before.
- A record that is still not resolved 1 hour after its bound is a **stuck record**. It stores one `agent_self_update_paused` warning event (no notification) and one server log warning: `Agent self-update tracking paused on hawaii: record 1a2b3c4d (Agent update 2.78.0 → 2.80.0) could not be completed 1h after its bound; the next agent update replaces it`. The next valid start on that device retires the stuck record as `timed_out` and creates a new one, so a stuck record never blocks tracking.
- A record stores at most one outcome event and one escalation, each exactly once, including across server restarts. Records are purged with other commands; a record whose window is not yet processed is exempt from the purge, as for any command.

**What the window suppresses.** `device_offline` for that device only, from the start for **at most 10 minutes**. A container that dies still alerts. A suppressed offline is stored as an `info` event: `Device hawaii went offline during Agent update 2.78.0 → 2.80.0 by agent auto-update (expected, back by 6:01 PM EDT)`. The online event reads `Device hawaii came online after Agent update 2.78.0 → 2.80.0 by agent auto-update (offline 42s)`. Nothing is dropped, and nothing is notified.

**How the window ends.** At the earliest of:

1. the device registers again reporting the new version (the normal case: the restarted agent registers first);
2. the agent reports `agent_update_failed`, or the device registers on any other version;
3. the 10-minute bound. There is no settle period. After the window ends, a later offline alerts exactly as with no window.

An `agent_update_completed` that arrives after the registration does not extend the window. It only records the outcome as `succeeded`.

An older agent, and the unsigned path, report `agent_update_completed` before the restart. The restart that follows is still covered, until the device registers again or the bound.

**Outcomes.** Each record ends once: `succeeded`, `failed`, or `timed_out` (shown as "not confirmed").

| Situation | What rIOt stores | Notification |
|---|---|---|
| The device registers on the new version, or the agent reports `agent_update_completed`; the device reports the new version at the bound | `succeeded`, no extra event | none |
| The agent reports `agent_update_failed` | `failed` and one `agent_self_update_failed` warning: `Agent update 2.78.0 → 2.80.0 by agent auto-update failed on vermont: <reason>`. The reason is the agent's text, cleaned and cut to 200 bytes | none |
| The device registers on another version | `failed` and one `agent_self_update_failed` warning: `PiVPN came back on agent 2.78.0 after Agent update 2.78.0 → 2.80.0 by agent auto-update (expected 2.80.0)` | none |
| At 10 minutes the device is online and not confirmed | `timed_out` and one `agent_self_update_unconfirmed` warning: `Agent update 2.78.0 → 2.80.0 by agent auto-update on vermont was not confirmed within 10m (agent reports 2.78.0)` | none |
| At 10 minutes the device is offline | `timed_out` and **one escalation** through the existing `device_offline` rule: `vermont still offline 10m after Agent update 2.78.0 → 2.80.0 by agent auto-update (record 1a2b3c4d)` | one push, within a minute, under the usual limit of 5 escalation pushes per device per hour |

A device that registers on the new version shows `Returned, confirming` in the history. The record becomes `succeeded` when the agent reports `agent_update_completed`, or at the 10-minute bound at the latest.

**Escalation.** Whatever the outcome, a device whose suppressed offline is still ongoing when its window ends escalates once, through the same rule and limit as in the table. Where the update failed, the text is `vermont is offline: Agent update 2.78.0 → 2.80.0 by agent auto-update failed (record 1a2b3c4d)`. The escalated device shows `Not back 10m after Agent update 2.78.0 → 2.80.0`, or `Agent update 2.78.0 → 2.80.0 failed: offline` after a failure, in failure styling until it next comes online or for 24 hours.

**On the dashboard.** From the start until the window ends, the Devices page and the device page show `Updating agent 2.78.0 → 2.80.0`, `by agent auto-update` and the start time, whether the device is online or offline. A device that is offline inside the window counts under **Expected offline**. The label clears when the window ends. The device page links to the record.

**The abuse budget.** A device's own agent decides when to send a start, so a compromised agent can make its own outages quiet: at most **about 57 minutes per rolling 24 hours** of real offline time, as at most 6 outages of under about 10 minutes each. Each is stored as an `info` event that names the update. An outage that is still going at the 10-minute bound escalates once. A self-update that comes back on another version leaves a `warning` event but no push. A second record cannot extend the first: a window that ends with its suppressed offline still ongoing always escalates. The effect is limited to the agent's own device. The cap of 6 stays, because the legitimate rate can reach about 5 a day (an update check every 6 hours, plus one 30 seconds after every agent start).

**Accepted residuals.**

- A real outage on a device that starts a self-update is hidden for up to 10 minutes, then escalates once.
- If the start event is lost, no record exists and the offline alerts as before, unless the server restart grace covers it. An older agent sends the start once. An agent from the first release after v2.88.0 retries it (see [Agent-side activity reporting](#agent-side-activity-reporting)).
- An agent that restarts in place without its connection dropping for 45 seconds raises no offline event at all.
- If the agent's periodic re-registration (every 24 hours) lands more than about 30 seconds late inside a window, the record closes early as `failed` and the restart alerts. That fails toward alerting.
- A self-update that fails while the device stays online produces a warning event and no push.
- A self-update that starts after a shutdown (or another host action) was dispatched, and before the device goes offline, takes over that offline's attribution, and can raise one extra push for a host that was powered off on purpose.
- **Back-to-back releases alert on the second restart.** A second start within 10 minutes of the first record is linked to that record. It does not open a new window and does not extend the first one, even when the device has already registered again. If the device restarts a second time, that offline is not suppressed and alerts as with no window. This fails toward alerting.
- An `agent_update_completed` or `agent_update_failed` from an older agent's fleet command that arrives within 10 minutes of a record's start is attributed to the record, not to the command. This also fails toward alerting. A newer agent names its command in these events, and the server then links them to that command.
- If the server cannot store a start, no record exists and the device alerts as before. If it cannot resolve a record, the coverage still ends at the bound and the escalation still fires. Only the outcome event is delayed, and the stuck-record warning above makes a long stall visible.

**Server restarts.** The records are rebuilt from the database, so a restart inside a window does not turn a covered offline into an alert, loses no escalation and sends none twice. A record whose bound passed while the server was down waits for the first offline-detector pass after the restart grace, so a device that died meanwhile escalates instead of being labelled "not confirmed". A record that cannot be resolved for an hour after its bound stores one `agent_self_update_paused` warning (no push); the next start replaces it.

**Server only, any agent.** No agent upgrade, no agent configuration change and no `install.sh` re-run is needed. It works with every agent that sends the start message in the form above (every release since v2.10.0, including the deployed v2.78.0 to v2.80.0). Migration `000034_agent_self_update` runs at server start. It adds the `agent_self_update` command origin, a shape check on those rows, a unique index (one open record per device) and an index for the 24-hour count. Its down migration deletes the self-update records, all of them, so rolling it back erases the self-update history. The `agent_self_update_*` events stay as plain warnings, and their `command_id` then points to a command that no longer exists.

**New fields and endpoints.**

- `GET /api/v1/activity` entries gain `agent_to_version`.
- `GET /api/v1/devices/:id/commands/:commandId/events` returns the events linked to a command or record (see [Command events](#command-events)).
- Three event types: `agent_self_update_failed`, `agent_self_update_unconfirmed` and `agent_self_update_paused`. The server does not route them through alert rules and never sends them to a notification channel.
- `command_attr` on a stored event gains the value `outcome`, and `inferred` now also marks the agent's own update events that the server linked to a record by timing. The value `agent` marks the update events that the server linked by a validated reference the agent sent (see [Agent-side activity reporting](#agent-side-activity-reporting)).

**Behaviour changes for every device.**

- A re-registration now also sets the device's `last_heartbeat` to the registration time. The server already treated a registration as proof of life. Without this, an agent that restarts and registers before its last heartbeat is 45 seconds old would be marked offline by the next check, and the `device_offline` alert would arrive right after the device came back. The change suppresses nothing: a window still ends at the registration.
- The severity that an agent sends with `agent_update_available`, `agent_update_started`, `agent_update_completed` and `agent_update_failed` is limited to `info`, `warning` and `critical`. Anything else is stored as `info` (`warning` for `agent_update_failed`). Agents send `info` and `warning`, so nothing changes for them.

### Agent-side activity reporting

A new agent tells the server more about what it is doing, so the server no longer has to guess. This applies to reboot, shutdown and agent updates. Every part falls back to the behaviour described above for an agent that does not report it.

**The reliable "waiting for the package lock" label.** A standalone `reboot` or `shutdown` command waits for the dpkg lock for at most 10 minutes. The agent now reports that wait as the command step `wait_dpkg_lock`, once per command, and only when the lock was really held. The Devices page and the device page then show `Rebooting (waiting for dpkg lock)` or `Shutting down (waiting for dpkg lock)`, with `by ryan` and the start time. The suffix shows only **while the device is online and the command is still running**. It disappears within about 5 seconds of the next step, the command's result, a failure or the device going offline, whichever comes first. The command history shows `Waiting for dpkg lock`, then `Initiating` once the power action has started, or `Failed at wait_dpkg_lock` when the command ends without a power action after the wait began (for example when the wait timed out). The report never delays the wait or the power action: it is sent without waiting for an answer, and the step that ends the wait is queued only after the agent has run the power command, never before. A reboot that follows a patch run (`os_update`) is not a standalone command and reports no step. An older agent reports no step, so its label stays `Rebooting`. The first agent release that reports the step is the first release after v2.88.0, the release that ships this change. `GET /api/v1/activity` reports the wait as `waiting_dpkg_lock` (see [Activity endpoint](#activity-endpoint)).

**Versions and a reference on update events.** `agent_update_started`, `agent_update_completed` and `agent_update_failed` now carry their `from_version` and `to_version` and one reference that names their attempt:

- an update started from the dashboard, **Update agents** or a schedule carries `command_id`, the ID of that `agent_update` command;
- the agent's own scheduled update carries `attempt_id`, a random 128-bit value the agent keeps in its hand-off file so the restarted agent finishes the same attempt. It stores at most 64 bytes per self-update record, in the record's parameters (no database migration).

The message text of every update event is unchanged, so an older server still recognises a self-update from it. The server treats every reference and version as untrusted. It uses a command reference only for the device's own `agent_update` command, started by Ryan or a schedule, that was dispatched and either has not finished or finished at most 10 minutes earlier; such an event is linked to that command and never creates or changes a self-update record. An attempt reference links the event to the record that owns it, whatever its age, and a repeat of a start never creates a record or opens a window again. An attempt reference that no record owns links a completion or failure only to a record that has no attempt ID, so one attempt can never close another attempt's record. Valid structured versions win over the message. The server reads the versions of a start only; those of a completion or failure are not used. Anything missing, invalid or unreadable is processed exactly as described above in [Agent self-updates](#agent-self-updates), by message and timing.

**Installer reinstall records are never a reference.** The record of an [installer reinstall](#installer-reinstalls) has the origin `installer`. It is not a command that Ryan or a schedule started, and no agent ever receives it. A `command_id` that names it is invalid. An `attempt_id` never matches it, because attempt IDs are stored only on self-update records. The timing rule never attributes an update event to it either. An agent that restarts because of a reinstall therefore never links its update events to the reinstall record, and the reinstall window is neither created nor changed by update events. The update **into** the first release that reports these fields is finished by the new agent from a hand-off the old agent wrote, so it carries no reference and is recognised by message and timing as before. The next update uses the new fields.

**Retries.** The agent sends each `agent_update_started`, `agent_update_completed` and `agent_update_failed` event at most 5 times, within 90 seconds of the first attempt, when the server answers with an error or a timeout (5xx, 429 or no answer); it never repeats after a success or after any other 4xx answer. Each attempt has its own 10-second limit. `agent_update_available` is still sent once. A self-update waits until its start event is accepted, refused with a final answer or has used these 5 attempts, before it applies the update, so a start lost while riot-server restarts is retried before the agent restarts: up to 90 seconds plus one 10-second request of extra delay, with the host lock held. An update started as a command never waits. The server stores a reference-linked event once per device, linked command or record, and type, and answers a repeat with `{"status":"duplicate"}` (see [Agent events](#agent-events)); other repeated events can be stored up to 5 times, only when an answer was lost. When the attempts are used up, the agent writes one warning (`agent update event not delivered`) and keeps nothing.

**Agent restarts that rIOt starts.** The only agent restarts that rIOt starts today are the ones the root update helper `riot-agent-apply` performs inside an `agent_update` command (from the dashboard, **Update agents** or a schedule) and in the agent's own self-update. An `install.sh` re-run also restarts the agent, but Ryan starts it on the host, and it is covered by its own notice and record (see [Installer reinstalls](#installer-reinstalls)), not by update-event references. The command's restart is now tied to that exact command through the command reference, so the actor stays `by ryan`, and its offline and online events are linked to the command. The AGENT-PRIV one-click apply that earlier planning named has not been merged and does not restart the agent, so there is no separate path. The rule for any future restart that rIOt starts: it must be a command whose events carry that command's reference, and it must never rely on timing or create a self-update record.

**Accepted residuals.**

- Older agents keep the self-update and plain `Rebooting` behaviour until they update.
- If both the step change and the result are lost because the host went down first, the label says `waiting` for at most the 45-second offline threshold, then `Rebooting`.
- A start lost because the server was unreachable for longer than the retry bound still alerts, unless the server restart grace covers it.
- A command that was reverted to queued and then run by the agent has no valid reference, because it was not dispatched. Its start falls back to the self-update rule and can create a record `by agent auto-update`.
- On an older server a repeated event whose answer was lost can be stored up to 5 times.
- De-duplication holds for the single server process this deployment runs.

**Upgrade.** This is an agent and a server change. Agents receive it through the normal agent self-update or **Update agents**. No `install.sh` re-run, no new privilege and no configuration change is needed. Every mix of old and new agents and servers keeps working: a new agent with an older server is understood from the unchanged message, and the older server ignores the new fields.

### Installer reinstalls

Re-running `install.sh` on a device that is already registered stops the agent and starts a new one. For a short time the device looks offline. When the installer can, it tells the server first, so the server expects that restart and does not page you. This is the **reinstall notice**. It is best effort: when it does not go through, the device pages exactly as it did before.

**When the notice is sent.** The installer sends exactly one notice when all of these hold:

- It runs on Linux, as a re-run on a host that already has `/etc/riot/agent.yaml` with a device ID and a device key. A fresh install, a run with `--bootstrap-key` and macOS never send one.
- The `riot-agent` service is loaded and running.
- Every check that can end the run without touching the agent has passed (the maintenance lock, the signed assets, the sudoers check, the backup). The installer sends the notice directly before it stops the agent. A run that ends earlier sends nothing.

The notice goes to the `server.url` in the host's `agent.yaml`. The server URL on the installer command line is ignored on a re-run, because the existing config is kept. When the host has no device ID or no key in `agent.yaml`, or the agent is not running, the installer sends nothing and prints nothing.

**The one status line.** Whenever the installer attempts a notice, it prints exactly one line and nothing else. The line never contains a server response, a URL or any part of the device key.

Accepted:

```
==> Server notified: agent reinstall (offline alerts paused up to 10m)
```

Not accepted:

```
==> Server not notified (<reason>); a device-offline alert may follow
```

In every case the install continues and its exit status is unchanged. The reason is one of these fixed phrases:

| Reason | What it means |
|---|---|
| `curl not available` | The host has no `curl`. The installer uses no other client. |
| `agent.yaml not readable` | The installer could not safely read `agent.yaml`: it is not a plain file, it is larger than 64 KiB, it has an unusual layout, or the device ID, key or URL does not have the expected form. |
| `TLS verification is off in agent.yaml` | `tls_verify: false` with no `/etc/riot/server.crt`. See "Trust settings the installer skips" below. |
| `trust settings not supported` | A trust setting that the installer cannot apply exactly as the agent does. See "Trust settings the installer skips" below. |
| `reinstall tracking paused on server` | The device already had 6 reinstall records in the last 24 hours. |
| `device already offline on server` | The server already shows the device offline, so there is nothing to expect. |
| `server could not record it` | The server accepted the request but could not store the record. |
| `unexpected server response` | The reply was a redirect, not the expected answer, or longer than 4096 bytes. |
| `server does not support reinstall notices` | The server is older than this feature (it answers 400, 404 or 405). Deploy the server first. |
| `device key rejected` | The server answered 401 or 403. |
| `server busy` | The server answered 429. |
| `server error` | The server answered 5xx. |
| `server name not resolved` | DNS failed. |
| `server unreachable` | The connection was refused. |
| `timed out` | The notice used up its time. |
| `TLS verification failed` | The server's certificate did not match what the agent trusts. The installer sent no key. |
| `connection failed` | Any other connection error. |
| `internal error` | The notice code failed in a way it does not recognise. |

**It never blocks the install.** The notice has a hard limit of 10 seconds, whatever the network does. It asks no question, reads nothing from the installer's input, never changes the exit status and never changes a later step. The device key goes to the server only in the `X-rIOt-Key` header of this one request. It never appears on a command line, in the installer's output or in a file that remains after the run. The installer reads at most 4096 bytes of the reply and removes its working files before it exits.

**Trust settings the installer skips.** The notice trusts exactly what the running agent trusts and nothing more. The installer reads the agent's settings and applies them to its own connection. A setting that it cannot apply exactly as the agent does makes it skip the notice, and it prints the reason. The stock installer and agent never write these settings:

- `tls_verify: false` without a pinned `/etc/riot/server.crt`.
- `server_cert_pin` without a pinned `/etc/riot/server.crt`.
- A `ca_cert_file` other than `/etc/riot/ca.crt` or `/etc/riot/server.crt`, when there is no `/etc/riot/server.crt`.
- A `client_cert` and `client_key` pair other than `/etc/riot/client.crt` and `/etc/riot/client.key`.
- A trust file that is not a regular file with one link, or that changes while the installer reads it.
- A `tls_verify` value other than `true` or `false`.

When `/etc/riot/server.crt` exists, the installer accepts only that certificate, as the agent does. The host's system certificate store is not consulted. With only `http://`, there is no TLS setting to apply, and the notice carries the key in clear, as every heartbeat already does.

**What the server does with a notice.** The server accepts a notice only when it carries the current key of the device that the URL names. A missing, wrong or revoked key, or a key for another device, creates nothing and changes no alerting. For an accepted notice the server opens a **reinstall record** when all of these hold:

- the server shows the device online;
- the device has no open reinstall record (a second notice while one is open creates nothing and does not extend the window);
- the device is under the rate cap of 6 records in any rolling 24 hours.

Otherwise it creates no record, and the device alerts as before. The notice itself is never stored as an event. Only its request type, `agent_reinstall_started`, travels on the existing `POST /api/v1/devices/:id/events` route, and nothing else in the body is used. The server answers `200` with `{"status":"ok","reinstall":"created"}`, or `open_record`, `capped`, `device_offline` or `unavailable`. A client cannot post the server's own event types `agent_reinstall_unconfirmed` and `agent_reinstall_paused`: they get `400`.

**The window.** The window opens when the record is created and covers `device_offline` for that device only, for **at most 10 minutes** from the notice. A container that dies still alerts. A suppressed offline is stored as an `info` event, linked to the record, and sends no notification: `Device hawaii went offline during Agent reinstall by installer (expected, back by 2:32 PM EDT)`. The online event reads `Device hawaii came online after Agent reinstall by installer (offline 52s)`.

**How the window ends.** At the earliest of:

1. the device registers again after the notice (the restarted agent registers first). The record ends `succeeded`, whatever version the agent reports;
2. the 10-minute bound.

There is no settle period. After the window ends, a later offline alerts exactly as with no window.

**Outcomes.** A record ends once.

| Situation at the 10-minute bound | What rIOt stores | Notification |
|---|---|---|
| The device registered again | `succeeded`, no extra event | none |
| No registration, the device is offline | `timed_out` ("Not confirmed") and **one escalation** through the existing `device_offline` rule: `hawaii still offline 10m after Agent reinstall by installer (record 1a2b3c4d)` | one push, within a minute, under the usual limit of 5 escalation pushes per device per hour |
| No registration, the device is online | `timed_out` and one `agent_reinstall_unconfirmed` warning: `Agent reinstall by installer on hawaii was not confirmed within 10m (agent did not restart)` | none |

The second row covers a run that failed after the agent stopped. The third covers a run that ended after the notice and before the stop, so the agent never restarted. A server restart inside a window loses nothing and sends nothing twice: the record is rebuilt from the database.

**The rate cap.** At most **6 reinstall records per device in any rolling 24 hours**, counted separately from the cap of agent self-updates. The first notice beyond it stores one `agent_reinstall_paused` warning for the device, at most once per 24 hours, with no notification: `Agent reinstall tracking paused on hawaii: more than 6 reinstalls in 24h; offline alerts during reinstalls are not suppressed`. A record that is still unresolved 1 hour after its bound stores one `agent_reinstall_paused` warning as well, and the next accepted notice replaces it. The server never sends either event to a notification channel.

**On the dashboard.** From the notice until the window ends, the Devices page and the device page show `Reinstalling agent`, `by installer` and the start time, whether the device is online or offline. The Devices page keeps rows to one line, so `by installer` and the start time are in the label's hover text. A device that is offline inside the window counts under **Expected offline** and still shows as offline. The label comes from the server, shows in any browser and clears within 5 seconds of the window ending. An escalated device shows `Not back 10m after Agent reinstall` until it next comes online or for 24 hours.

The record appears in the device's command history as `Agent reinstall` `by installer`, with the outcome `In progress`, `Succeeded` or `Not confirmed`. It is read-only: it has no cancel and no retry, an agent can never claim it, and it is never sent to an agent. The device page label links to it.

**Together with agent self-updates.** The two records are independent. When both apply to a device, each ends at its own bound and the one with the latest start sets the label. A self-update start that follows a reinstall is never attributed to the reinstall record, whether it carries a reference or not: a reinstall record is never a valid command reference, never matches an attempt ID and is never picked by the timing rule (see [Agent-side activity reporting](#agent-side-activity-reporting)).

**The abuse budget.** A holder of a device's key can make offline alerts silent for less than 60 minutes per day through reinstall notices, and less than 120 minutes per day together with agent auto-update; each silent outage lasts under about 10 minutes, is stored as an info event, and pages if it lasts past the bound. The effect is limited to the key's own device. A notice for an offline device is refused, so records cannot be chained across one outage.

**Accepted residuals.**

- A real outage that coincides with a reinstall is hidden for up to 10 minutes, then escalates once.
- A run whose notice fails (server down, network, trust mismatch) pages as before.
- A run that aborts after the notice and before the stop leaves the device online. The window stays open until the bound (at most 10 minutes) and ends `Not confirmed` with one warning event and no push.
- A run that aborts after the stop leaves the agent down. No prompt runs between the stop and the start, so only a failure can cause this. The device escalates once at the bound.
- The first registration after the notice ends the window as `succeeded`, including the agent's own re-registration every 24 hours if it falls inside a run that aborted before the stop. That ends the window early and fails toward alerting.
- Overlapping windows can cause one extra escalation push for one outage, or one extra "not back" push after a deliberate shutdown. Both fail toward alerting.
- A compromised `riot` user can choose which regular file sits at the four `/etc/riot` trust paths, make the installer print a misleading notice line, or make it abort after the stop. Each errs toward alerting, at worst a page at the bound. That user already controls the agent's own trust and holds the device key.
- On a server with an `http://` address, the key travels in clear, as in every heartbeat.
- If the installer is killed with `SIGKILL`, its working directory under `/tmp` (root-only, mode 0700) can remain. It can hold a copy of `agent.yaml` with the device key and copies of the trust files.
- raw.githubusercontent.com can serve the previous `install.sh` for about 5 minutes after a change is merged.

**Rollout.** This is a server deploy. Deploy the server release that contains migration `000035_installer_reinstall`. No device needs an agent upgrade, a configuration change or an extra `install.sh` re-run: every registered device with a running agent qualifies, whatever agent version it runs. The installer reaches devices from the `main` branch as soon as it is merged. The combinations:

| Installer | Server | Result |
|---|---|---|
| old | old | The device pages as before. |
| new | old | One `Server not notified (server does not support reinstall notices)` line. The install is unchanged and the device pages as before. |
| old | new | No notice and no record. The device pages as before. |
| new | new | Silent. The device shows `Reinstalling agent`, and escalates once if it is not back in 10 minutes. |

The first `install.sh` re-run on a registered device after the server deploy is the check. It prints `==> Server notified: agent reinstall (offline alerts paused up to 10m)` and sends no push.

**Migration and rollback.** Migration `000035_installer_reinstall` runs automatically at server start. It widens the command origin check with `installer`, adds a shape check (running, succeeded or timed out; never queued; bound of 10 minutes; no parameters; no target keys), a unique index (one open record per device) and an index for the 24-hour count. It adds no column and no table. To roll the server back, **migrate down to schema 34 first, then deploy the previous image.** An older binary would otherwise read the `installer` records as ordinary `agent_update` commands. The down migration deletes every reinstall record, so rolling it back erases the reinstall history. The `agent_reinstall_*` events stay as plain warnings. The installer needs no rollback: against an older server it prints the `not notified` line.

**New fields and event types.**

- `GET /api/v1/activity` entries gain `origin` (`installer` for a reinstall).
- Commands from `GET /api/v1/devices/:id/commands` carry `origin: "installer"` for a reinstall record.
- Two event types: `agent_reinstall_unconfirmed` and `agent_reinstall_paused`. The server does not route them through alert rules.

### Server restarts and the restart grace

The windows are rebuilt from the command records, so a server restart does not turn a suppressed event into an alert, lose an escalation that became due while the server was down, or send one twice. Escalations that fell due during the downtime fire on the first check after the grace period.

For 2 minutes after the server starts, it moves no device from online to offline. Heartbeats paused while the server was down, and agents need time to reconnect. After the grace, a device whose last heartbeat is older than 45 seconds goes offline. The check runs every 30 seconds, so a dead device goes offline 45 to 75 seconds after its last heartbeat. A device that was already offline before the restart stays offline and raises no new event.

A restart loop does not get an endless grace. Starts less than 2 minutes apart form a series, and the total period without offline detection is at most 4 minutes from the first start of the series. Each start that continues a series writes one server log warning, `restart grace shortened`. If the server cannot read or write the series record, it allows no grace.

### Safety and bounds

- **Fail open.** When the server cannot tell whether a window covers an event, it alerts as it did before this feature. That covers a window cache that did not load, a cache that is more than 90 seconds out of date, and a window evaluation loop that has not completed a cycle in 15 seconds.
- **Cross-device isolation.** A window covers events on its own device only. An agent that names another device's command gets no suppression.
- **Unbounded growth.** There is none. Escalations are at most 22 per command. Windows are cached at most 200 per device. Commands whose window is not yet processed are exempt from command retention and from the command storage budget, so a purge never removes a pending check. That set is bounded. Names in event messages are printable text of at most 128 bytes. Suppressed events are rate limited upstream, because the server admits one Docker event per container and action per minute per device.
- **Logs.** A suppressed event writes no log line above debug level. An escalation writes one `info` line.

### Known limits

- **A real problem inside a host window stays hidden until the window ends.** A genuine outage or a genuine container crash on a device during a `reboot`, `os_update` or `shutdown` window raises only `info` events. The escalation follows when the window closes: up to 15 minutes after success, or within a minute of a failed or timed-out command.
- **A container that dies inside a window and comes back raises nothing.** The `info` event is the only record. One that dies again after the window closes alerts normally.
- **A failed command whose targets are healthy is not pushed.** It stays a `warning` event, as before.
- **Work outside rIOt still alerts.** An `ssh` reboot, hardware work and unattended upgrades are not expected activity.
- **An `os_update` that reboots more than about 105 seconds after its result can close the window first.** The later offline then alerts.
- **A reboot that reports "initiated" but never happens** keeps container-death suppression until the 15-minute bound.
- **A push can be lost.** If the server's notification queue is full, a push takes more than 30 seconds, or the server crashes right after it stores an escalation, that one notification is lost. The event is stored.
- **A stuck notification backend** can occupy one of the two delivery workers. Further pushes queue, then drop with a server log warning. Events and checks are unaffected.
- **A device's own agent can open, shape and close windows on its own device only.** It cannot affect any other device. Every window has the bounds above. For a self-update window, the budget is stated under [Agent self-updates](#agent-self-updates).
- **A command record that is deleted by hand while its window is open** cannot raise an escalation. rIOt stores one warning event, `expect_backstop_skipped`, that says the checks were skipped.
- **Tracking limit.** More than 200 open windows on one device are not all tracked. The extra ones suppress nothing, their checks are delayed, and rIOt stores a warning event about it at most once an hour per device.

### Behaviour changes from earlier versions

- A container that an update does not target now alerts when it dies during that update. The earlier per-device "update in progress" counter is removed.
- A `docker_restart`, `docker_stop` and a stack action now suppress their targets' deaths. A `shutdown` and an `agent_update` now suppress `device_offline`.
- A reboot that waits for the dpkg lock, and a patch that runs for 30 minutes before it reboots, no longer alert. The earlier rule covered only a reboot or patch created in the last 2 minutes.
- A device with the status `warning`, for example a UPS on battery, now goes offline when its heartbeats stop. Before this change it never did.
- A `warning` device that registers again raises no `came online` event, because it was not offline.
- Offline detection waits 2 minutes after a server start, not 30 seconds.
- The `came online` event after a window names the action, the actor and the time the device was offline.
- An agent's own self-update restart is now expected activity for up to 10 minutes. A device registration also refreshes the device's last heartbeat. See [Agent self-updates](#agent-self-updates).
- The restart that an `install.sh` re-run causes on a registered Linux host is now expected activity for up to 10 minutes, when the installer's notice reaches the server. A re-run prints one new status line. See [Installer reinstalls](#installer-reinstalls).

### Upgrade notes

- **Server only.** Upgrade the server. Agents need no upgrade, no configuration change and no `install.sh` re-run. The feature works with every released agent version.
- **Migration `000033_expected_activity`** runs automatically at server start. It adds two columns and two indexes to `commands`, a table of escalation markers (`command_escalations`) and a table for the powered-off state (`device_powered_off`). It marks commands that finished more than an hour before the upgrade as processed, so old commands cannot raise escalations.
- **Rollback.** The down migration removes the new columns, indexes and tables. After a down and an up migration, the markers of commands that finished in the last hour are gone, so those commands can escalate a second time.
- Nothing changes in your alert rules. The two existing rules, `device_offline` and `container_died`, carry the escalations.
- Two new event types appear in the event list: `expect_backstop_skipped` (a check was skipped, with the reason) and `expect_notifications_held`.
- **Agent self-updates** need the same: the server only, no agent upgrade and no `install.sh` re-run. Migration `000034_agent_self_update` runs at start, and its down migration deletes the self-update records. Details: [Agent self-updates](#agent-self-updates).
- **Installer reinstalls** need a server deploy and nothing on the devices: no agent upgrade, no configuration change and no extra `install.sh` re-run. Migration `000035_installer_reinstall` runs at start. To roll back, migrate down to schema 34 first, then deploy the previous image. Details: [Installer reinstalls](#installer-reinstalls).

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

Two alert templates are available in **Administration > Alert Rules > Create from Template** under the `gpu` category:

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

Go to **Administration > Alert Rules** and click **Create from Template**. Two pre-built templates are available under the `webserver` category:

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

1. Go to **Administration > Alert Rules** and click **Create from Template**.
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

## Agent Auto-Update and Release Signing

Agents check for a new release 30 seconds after start and every 6 hours after that. Auto-update is on by default. An agent installs a new version only if it can prove the release came from the rIOt maintainer. If it cannot, it refuses and keeps running the current version.

> **Behavior change (AGENT-HARDEN-001).** Agents used to install an update even when the checksum could not be fetched or was not published, and the installer gave the `riot` user a sudo rule that could run any command as root. Both are gone. Read [Upgrading existing agents](#upgrading-existing-agents) before relying on the new behavior: the sudoers and service-unit changes reach a host only when the installer is re-run on it.

### How an update is verified

1. The agent asks the server whether a newer version exists. The server selects only the exact `riot-agent-<platform>` asset (`.exe` on Windows), excluding helpers and sidecars regardless of asset order. If that binary is absent, it supplies no substitute. The server only tells the agent where the files are; the agent never trusts the server to say they are genuine.
2. The agent downloads the binary, `checksums.txt`, and `checksums.txt.sig` from the GitHub release. The two small files are capped at 1 MiB and 30 seconds each.
3. The agent verifies the ed25519 signature in `checksums.txt.sig` over the exact bytes of `checksums.txt`. The public key is compiled into the agent binary. No config file, environment variable, or server response can replace it.
4. The agent checks that the SHA-256 of the downloaded binary equals the `riot-agent-<platform>` line in the now-trusted `checksums.txt`.
5. The agent starts the root-owned verifier `/usr/local/lib/riot/riot-agent-apply` through one fixed sudo rule. The verifier repeats steps 3 and 4 on the exact bytes it installs, writes them to `/usr/local/bin/riot-agent`, and restarts the `riot-agent` service. The agent is not the security boundary because it runs as the unprivileged `riot` user.

Validation failures refuse the update and report the cause. A maintenance conflict defers the privileged handoff before stopping or replacing the agent. Merely scheduling the verifier is not completion: success requires fresh root acknowledgement for the expected digest and verification of the running binary after restart. The agent does not exit just because it staged an update or launched the verifier; see [maintenance coordination](docs/ops/os-maintenance.md#holds-restart-recovery-and-update-coordination).

Refusal causes you may see in the event message:

| Message contains | Cause |
|---|---|
| `release is unsigned (no checksums.txt.sig)` | The release has no signature asset. This includes every release published before this change. |
| `release has no checksums.txt` | The release has no checksum file. |
| `signature fetch failed` / `checksum fetch failed` | A download returned a non-200 status, timed out, or exceeded 1 MiB. |
| `signature invalid` | The signature does not verify against the agent's compiled-in key. The release was signed with a different key, or `checksums.txt` was altered. |
| `checksum unavailable` | `checksums.txt` has no entry (or more than one) for this platform. |
| `checksum mismatch` | The downloaded binary does not match the signed checksum. |

Dashboard-triggered agent updates use the same pipeline and the same refusals. Dev builds (`version` is `dev`) never auto-update.

### What the `riot` user can do as root

The installer no longer writes any sudoers rule that runs a shell or accepts wildcard arguments for updates. The update rules are exactly these two lines in `/etc/sudoers.d/riot-agent`:

```
riot ALL=(root) NOPASSWD: /usr/bin/systemd-run --unit=riot-agent-update /usr/local/lib/riot/riot-agent-apply
riot ALL=(root) NOPASSWD: /usr/bin/systemctl reset-failed riot-agent-update
```

The paths in your file come from `command -v` on the host. They can differ from the example.

Two properties keep this rule from being a root shortcut:

- `/usr/local/bin/riot-agent` and `/usr/local/lib/riot/riot-agent-apply` are owned by `root:root`. The `riot` user cannot overwrite either file.
- The verifier installs only bytes covered by a valid signature. A compromised `riot` account that stages its own file and runs the rule gets a refusal. The verifier also rejects symlinked, non-regular, or oversized staged files, and its install target is fixed at build time.

Supported apt patching uses the independent fixed worker, with package-name-only hold rules enforced by sudo argument regex. The signed apply helper also takes the shared maintenance lock before stopping or replacing the agent. New helpers, units and privilege rules require a root installer migration; see [Sudoers Rules](#sudoers-rules).

### Disabling auto-update

Set the flag in `/etc/riot/agent.yaml` and restart the agent:

```yaml
agent:
  auto_update: false
```

```bash
sudo systemctl restart riot-agent
```

With the flag set to `false`, the agent makes no update check and the dashboard "update agent" command returns an error. Omitting the flag keeps auto-update enabled.

### Upgrading existing agents

An agent-only update replaces only the agent binary. It cannot update sudoers, units, either root-owned helper, or the root exclusion protocol. **An existing hardened signed updater also needs this migration** before it can coordinate with the detached worker. Missing or old helpers and unsafe legacy policies fail closed for apt maintenance.

Once a release containing MAINT-RECOVERY-001 is published, obtain and review that release's `scripts/install.sh`, then explicitly run it as root with the matching `--version` (without the `v` prefix), your existing server URL and applicable installation flags. Preserve the current config and device identity; do not use `--bootstrap-key` for an ordinary migration because it resets enrollment. Do not pair the new installer with v2.72.0 or an older release: those releases lack the maintenance assets, and installation will abort. Prefer a reviewed tagged installer over a moving `main` URL during migration.

The installer authenticates every required asset before replacement, checks host/native package locks, backs up managed files, replaces legacy riot-owned agent inodes with fresh root-owned signed bytes, installs both helpers, narrows managed sudo rules and replaces the blind boot copy with signed `--boot` verification. Unknown administrator rules and drop-ins are retained for review. Invalid policy, unsafe paths, busy package work or unresolved maintenance refuse migration; do not delete locks or bypass the checks.

After migration run:

```bash
sudo -u riot /usr/local/bin/riot-agent -config /etc/riot/agent.yaml doctor
```

**Remote shutdown (SHUTDOWN-SUDO-001).** The same root re-run also applies the opt-in poweroff sudo rule on a server whose `/etc/riot/agent.yaml` has `allow_shutdown: true` under `commands:`. A server that needs the AGENT-HARDEN-001 or PATCH-FLOW-001 re-run can get all of it in one run. See [Shutdown on Linux and the sudo rule](#shutdown-on-linux-and-the-sudo-rule).

The [maintenance guide](docs/ops/os-maintenance.md) covers prerequisites, legacy/current/unsupported installations, result inspection and recovery. A real systemd PID 1 smoke test remains required before production rollout of this feature. Helpers are not refreshed by future agent-only updates; use the matching reviewed root installer to update privileged assets.

### Known limitations

- **Signed rollback.** The version number is not part of the signed data. The agent refuses an update when the server advertises a version that is not newer. A compromised server can advertise a high version and serve an older, genuinely signed binary, and the agent cannot detect it. A signed version signal is a recorded follow-up.
- **Linux only.** Installing a verified update works on Linux hosts running systemd. macOS and Windows agents do not complete automatic updates.
- **Key rotation is manual.** See [Rotating the signing key](#rotating-the-signing-key).

### For maintainers: release signing

Every release is signed in GitHub Actions. The private key is the Actions secret `AGENT_SIGNING_PRIVATE_KEY`. The matching public key is committed as `PublicKeyBase64` in `internal/releasekey/releasekey.go` and compiled into both `riot-agent` and `riot-agent-apply`. The release workflow:

1. Builds `riot-agent-<platform>` for 8 targets, both `riot-agent-apply-<platform>` and `riot-maintenance-<platform>` for the 5 Linux targets, and publishes the fixed `riot-maintenance.service`. All required Linux assets are included in the signed manifest; agent entries precede helper entries for older checksum consumers.
2. Writes `checksums.txt` (SHA-256 of every `dist/` file, including the verifiers).
3. Signs it with `go run ./cmd/riot-signer sign`, producing `checksums.txt.sig` (base64 of the 64-byte signature, one line).
4. Runs `go run ./cmd/riot-signer verify` against the **committed** public key and fails the release if the signature does not verify or the committed key is not a valid 32-byte ed25519 key.
5. Attaches the agents, both Linux helpers, fixed maintenance service, `install.sh`, `checksums.txt` and `checksums.txt.sig` to the GitHub Release.

**The repository ships with a placeholder public key. Complete the one-time setup in [Releasing](#releasing) before you push the next release tag, or the release workflow fails.**

#### Rotating the signing key

Rotation is deliberately manual. Both the agent and the verifier embed the key, and the verifier updates only when the installer is re-run. The release workflow checks every signature against the key committed in the same commit, so the new public key and the new private key must change together.

1. Run `go run ./cmd/riot-signer genkey` and keep the new private key offline.
2. Commit the new public key to `internal/releasekey/releasekey.go` and set `AGENT_SIGNING_PRIVATE_KEY` to the new private key.
3. Tag a release. The release workflow signs it with the new key and verifies it against the new committed key.
4. Re-run the installer on every Linux host. The installer downloads an agent and a verifier that embed the new key.

Until a host is re-run, it still embeds the old key. It refuses the new release with `signature invalid` and stays on its current version. This is a visible stop, not a bypass.

If the private key is compromised, an attacker can sign malicious updates that every agent embedding the matching public key accepts. Rotate immediately with the steps above. A host stays exposed to the old key until the installer is re-run on it. Disable auto-update (`agent.auto_update: false`) on hosts you cannot re-run promptly.

---

## Single Sign-On (OIDC)

rIOt's admin dashboard can optionally accept sign-in through an external OpenID Connect identity provider (for example authentik), alongside the built-in password login. SSO federates **identity only** — a successful login mints the exact same `riot_session` cookie that a password login does. rIOt never stores an IdP token, never adds a second class of user, and never disables password login. If you never set the environment variables below, this feature does not exist as far as your deployment is concerned: no button, no new endpoints reachable, no outbound calls to any identity provider.

### How access control works — read this before enabling

- The identity provider answers exactly one question: **"who is this?"** It does not tell rIOt what that person should be allowed to do inside rIOt.
- **Any identity your IdP authenticates and authorizes for the rIOt application is granted full rIOt admin** — the same access a correct password grants. There is no secondary approval step, no invitation, and no allowlist inside rIOt itself.
- **The IdP's application-to-group binding *is* your access control.** Restricting who can sign in is entirely your responsibility, enforced at the identity provider — not inside rIOt. An authentik application with no group binding is open to *every* authenticated user of that authentik instance. Create the binding **before** you write any `RIOT_OIDC_*` variable into `.env`.
- The first successful login from a previously unseen identity is recorded as a **`WARN`-level log entry** ("new SSO identity granted admin", with issuer and subject) that is persisted and visible in **Settings > Logs**. Check there right after you enable SSO, and again any time you see an admin session you don't recognize. A repeat login from an already-seen identity logs at `INFO` only and does not repeat the warning — this is the loudest signal rIOt can give you for a decision it does not make itself.

### Enabling SSO

SSO stays completely dormant until all three of `RIOT_OIDC_ISSUER_URL`, `RIOT_OIDC_CLIENT_ID`, and `RIOT_OIDC_CLIENT_SECRET` are set. Follow these steps **in order**:

1. **Create the group binding first.** In your identity provider, create (or choose) a group for rIOt admins and bind the rIOt application to it before doing anything else here.
2. **Register the application.** If you use authentik with a script that registers OIDC applications, run it like this:

   ```bash
   python register_oidc_app.py riot \
     --redirect-uri https://<your-riot-host>:7331/api/v1/auth/oidc/callback \
     --launch-url  https://<your-riot-host>:7331/ \
     --group       "riot admins"
   ```

   Substitute the scheme, host, and port rIOt is actually reached at. `--group` is **required** for rIOt — the script prints a loud warning if you omit it, and that warning is not a substitute for creating the binding by hand. The script emits house-idiom `OIDC_*` keys; re-prefix them to `RIOT_OIDC_*` when you copy them into `.env`.
3. **Use strict, exact-match redirect URIs — never regex or wildcard.** Register exactly one redirect URI per scheme + host + port that rIOt is actually reached by; rIOt derives its own redirect URI from the incoming request, so an entry that doesn't match byte-for-byte fails at the IdP. If your first attempt fails with a redirect mismatch, **fix the registered URI** rather than switching the IdP to regex/wildcard matching — that would remove the one control keeping a poisoned `Host` header from being useful to an attacker. Re-running the registration script with a corrected `--redirect-uri` *appends* to the existing list rather than replacing it, so also delete the stale, wrong entry by hand in the IdP.
4. **Verify the binding before enabling for real.** Sign in with a test account that is deliberately *not* in the bound group and confirm the IdP refuses it — the rIOt login screen should show "The identity provider refused the sign-in for this account." Do this before writing the real variables into a production `.env`.
5. **Set the environment variables and restart the server.** Changing any of them requires a restart — there is no hot reload.

| Variable | Required | Default | Description |
|---|---|---|---|
| `RIOT_OIDC_ISSUER_URL` | to enable SSO | — | The IdP's per-application issuer URL, e.g. `https://auth.example.com/application/o/riot/` |
| `RIOT_OIDC_CLIENT_ID` | to enable SSO | — | OAuth client ID issued by the IdP |
| `RIOT_OIDC_CLIENT_SECRET` | to enable SSO | — | OAuth client secret issued by the IdP — a secret; never commit it, `.env.example` carries a placeholder only |
| `RIOT_OIDC_BUTTON_LABEL` | no | `Sign in with SSO` | Text shown on the login screen's SSO button |

If any one of the first three is missing or empty after trimming whitespace, SSO stays dormant: the login screen shows no SSO button, `GET /api/v1/auth/oidc` answers `{"available": false, "label": ""}`, and `/api/v1/auth/oidc/start` / `/api/v1/auth/oidc/callback` both respond `404`.

### Behind a reverse proxy: `RIOT_TRUSTED_PROXIES`

If rIOt sits behind a TLS-terminating reverse proxy (nginx, Caddy, Traefik, etc.), set `RIOT_TRUSTED_PROXIES` to the proxy's CIDR (or its exact IP as a `/32`).

**This is what turns on the `Secure` attribute on the session cookie.** Leave it unset behind a proxy, and rIOt ignores `X-Forwarded-Proto`, sees what looks like a plain-HTTP request from the proxy, and issues `riot_session` *without* `Secure` — even though your browser is talking `https` to the proxy the entire time. That is a real exposure: a `Secure`-less admin session cookie can be sent over a stray cleartext `http://` request to the same host. Setting `RIOT_TRUSTED_PROXIES` fixes it.

Secondarily — and only relevant once the above is handled — leaving it unset behind a proxy also collapses every client's rate-limit bucket and every audit-logged IP address onto the proxy's own address instead of the real client's.

| Variable | Required | Default | Description |
|---|---|---|---|
| `RIOT_TRUSTED_PROXIES` | no | empty (trust nobody) | Comma-separated CIDR blocks (bare IPs are treated as `/32`/`/128`) trusted to supply `X-Forwarded-For`, `X-Real-IP`, `True-Client-IP`, and `X-Forwarded-Proto`. Set only when rIOt sits behind a reverse proxy |

Leave `RIOT_TRUSTED_PROXIES` unset for rIOt's default direct-exposure deployment on `:7331` (where rIOt terminates its own TLS) — it already gets `Secure` correctly in that shape and needs no configuration. Never set it to `0.0.0.0/0`; that hands your rate-limit key and audit IP back to whoever can write the header.

For the response headers rIOt sets, including HSTS behind a proxy, see [Security Headers](#security-headers).

### What SSO does not do

- It does not replace first-run setup — the setup wizard always runs first on a fresh install, and the SSO button never appears (and the endpoints 404) until setup is complete, regardless of whether the `RIOT_OIDC_*` variables are set.
- It does not remove or weaken password login. The password form and "remember me" work exactly as before, at all times, even if the identity provider is unreachable.
- It does not add users, roles, or permission tiers. rIOt remains single-admin: every successful SSO login receives the identical `sub: "admin"` session a password login would.
- It does not persist any token from the identity provider. Only an audit record (issuer, subject, email, first-login and last-login timestamps) is kept for visibility — nothing in the login path reads it to make an access decision.
- Signing out of rIOt does not sign you out of the identity provider, and signing out of the identity provider does not sign you out of rIOt — there is no single sign-out.

---

## Security Headers

rIOt sets the same security headers on every response: the dashboard, the API, static files, redirects and errors. They need no configuration.

| Header | Value |
|---|---|
| `Content-Security-Policy` | See the table below |
| `X-Frame-Options` | `DENY` |
| `X-Content-Type-Options` | `nosniff` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Strict-Transport-Security` | `max-age=31536000`, on HTTPS requests only (see [HSTS](#hsts)) |

### Content Security Policy

| Directive | Allows |
|---|---|
| `default-src` | `'self'` |
| `script-src` | `'self'` only. Inline scripts and `eval` are blocked. |
| `style-src` | `'self'` and inline styles |
| `img-src`, `font-src` | `'self'` and `data:` |
| `connect-src` | `'self'`, plus `ws://` and `wss://` on the host the browser used to reach rIOt (for the live dashboard and terminal WebSockets) |
| `form-action` | `'self'`. A form on a rIOt page can only submit back to rIOt. |
| `object-src` | `'none'` |
| `base-uri` | `'self'` |
| `frame-ancestors` | `'none'` |

The WebSocket sources come from the request's `Host` header. rIOt includes the host only if it contains nothing but letters, digits, `.`, `-`, `:`, `[` and `]` and is at most 262 bytes. Any other value falls back to `connect-src 'self'`.

**The dashboard cannot be embedded in an iframe.** `frame-ancestors 'none'` and `X-Frame-Options: DENY` make browsers refuse to render it inside another page, such as a panel in another dashboard application. Open rIOt in its own tab or window instead.

If the browser console shows a Content Security Policy "Refused to ..." message on a rIOt page, report it as a bug and include the blocked URL.

### HSTS

`Strict-Transport-Security: max-age=31536000` (one year) tells a browser to use HTTPS for the hostname. rIOt sends it under the same rule that sets the `Secure` flag on the session cookie:

- The connection to rIOt is TLS, or
- The request comes from a proxy listed in `RIOT_TRUSTED_PROXIES` that sends `X-Forwarded-Proto: https`.

rIOt never sends HSTS on plain HTTP, and ignores `X-Forwarded-Proto` from peers that are not trusted proxies. The header has no `includeSubDomains` or `preload`. There is no setting to turn it off.

Read these consequences before you serve rIOt over HTTPS on a hostname:

- **It applies to every port.** Once a browser has seen the header from `https://riot.example.com:7331`, it uses HTTPS for every port on `riot.example.com`. A plain-HTTP service on the same hostname (on another port) stops loading in that browser for up to a year.
- **There is no click-through.** While the pin is active, the browser blocks the page on any certificate error (expired, self-signed, wrong name) and removes the "proceed anyway" option. Keep the certificate valid, or use a certificate your browsers trust.
- **IP-address hosts are exempt.** Browsers ignore HSTS for sites reached by IP address, so `https://192.0.2.10:7331` is not pinned.
- **Reset it in the browser.** The server cannot withdraw the pin early. Remove the hostname from the browser's HSTS list, or wait for it to expire.

### Origin check on state-changing requests

Every `POST`, `PUT`, `PATCH` and `DELETE` that uses the admin session cookie must come from rIOt's own origin. This covers the dashboard API, `/api/v1/auth/*` and `/api/v1/setup/*`. The check runs before the request body is read, so a refused request changes nothing. It answers `403` with `{"error":"origin_refused"}` unless one of these is true:

- `Sec-Fetch-Site` is `same-origin`, or
- `Origin` equals the origin the request was made to (the scheme and `Host` rIOt sees), or
- `Origin` is listed in `RIOT_ALLOWED_ORIGINS`.

When the request has a body, its `Content-Type` must be `application/json` (`application/json; charset=utf-8` is accepted). A request without a body needs no `Content-Type`.

Not affected: `GET`, `HEAD` and `OPTIONS` requests, the agent endpoints (device key or mTLS) and the WebSocket endpoints. The dashboard needs no change, because browsers send `Origin` or `Sec-Fetch-Site` on these requests.

The check runs before authentication. A cross-origin write without a valid session now gets `403`, where it used to get `401`.

**Admin scripts.** A script that calls the cookie-authenticated API without browser headers gets `403`. Send an `Origin` header equal to the URL you call and, if the request has a body, a JSON content type:

```bash
curl -sk -H 'Origin: https://riot.example.com' -H 'Content-Type: application/json' \
  -d '{"password":"<password>"}' https://riot.example.com/api/v1/auth/login
```

**Behind a TLS-terminating proxy.** The proxy usually rewrites `Host`, so the browser's `Origin` no longer equals what rIOt sees. Browsers still send `Sec-Fetch-Site: same-origin`, which rIOt accepts, so the dashboard keeps working. For everything else:

1. Set `RIOT_TRUSTED_PROXIES` as described in [Behind a reverse proxy](#behind-a-reverse-proxy-riot_trusted_proxies). rIOt then trusts the proxy's `X-Forwarded-Proto` when it rebuilds the request origin.
2. Set `RIOT_ALLOWED_ORIGINS` to the public origin if the proxy rewrites or strips `Origin`, or if scripts reach rIOt through the proxy.

**`RIOT_ALLOWED_ORIGINS` format.** Entries are comma-separated. Each entry must equal the `Origin` header exactly, ignoring letter case. Write `scheme://host[:port]` with no path and no trailing slash. `https://riot.example.com` matches. `https://riot.example.com/` never matches, because browsers send no trailing slash. Leave out a default port (`:443` for `https`, `:80` for `http`), because browsers omit it from `Origin`.

**Logging.** rIOt logs a refusal at most once per client address per minute. The log line holds the client address and the method, never a header value.

---

## DNS Resilience

The agent includes a resilient DNS resolver that caches DNS lookups to disk. If DNS becomes unavailable, the agent falls back to cached IPs to maintain connectivity with the server.

- **Automatic caching** — DNS results are cached and refreshed in the background (default: every 30 minutes)
- **Disk persistence** — cache survives agent restarts (stored at `/etc/riot/dns-cache.json` or `%PROGRAMDATA%\riot\dns-cache.json`)
- **Staleness warnings** — logs warnings when cached entries become stale (24h warning, 72h critical)
- **IP fallback** — on DNS failure, the agent tries cached IPs with the original hostname in the `Host` header and TLS `ServerName`

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

### Secrets are write-only

The API and the dashboard never return a stored notification secret. Wherever a response would contain one, it contains the fixed placeholder `[rIOt-redacted]` instead. An empty value (`""`) means the secret is not set, so a client can tell "configured" from "empty".

| Channel | Redacted values |
|---------|-----------------|
| Email (SMTP) | `password` |
| ntfy | `token`, `topic` |
| Telegram | `bot_token` |
| Discord, Slack | `webhook_url` |
| Pushover | `api_token`, `user_key` |
| Gotify | `app_token` |
| Webhook | `url` and the value of every header (header names stay visible) |
| Any other type | every config value |

Credentials embedded in any other URL value (for example `https://user:pass@host` in `server_url`) are also redacted, and come back as `https://[rIOt-redacted]@host`.

This applies to the list, create and update responses of `/api/v1/settings/notification-channels`.

**Editing a channel.** The editor shows "Configured (unchanged)" under each stored secret.

- Leave the field alone, or erase what you typed, to keep the stored value.
- Type a new value to replace it.
- Click **Clear** (available for the ntfy token and the SMTP password) to remove it. The field shows "Will be cleared on save" with an **Undo** link.

**API clients.** On `PUT /api/v1/settings/notification-channels/:id`, a secret that is omitted or sent as `[rIOt-redacted]` keeps its stored value. A new value replaces it, and `""` clears it. For a webhook, the `headers` object you send is the complete set of headers: send `[rIOt-redacted]` as a header's value to keep that header's stored value. Changing a channel's type keeps no old secrets. The placeholder is never stored: on create, or where nothing is stored to keep, it becomes empty. `PUT` to a channel id that does not exist returns `404`.

Because reads no longer return secrets, you cannot copy a channel to another server by reading it and posting it back. The copy has empty secrets. Enter them again.

**Test notifications** use the stored secret. rIOt also removes secrets from the error text of failed sends, so they do not appear in the test-send response, the delivery log or the server log. Delivery log rows written before this change can still contain a leaked secret until the 90-day retention removes them.

**What this does not protect against.**

- Anyone who can edit channels. An administrator can point a stored channel at a destination they control and trigger a test or an alert, and rIOt sends the stored secret there. For example, they can change the host of a URL that carries embedded credentials. Treat channel-edit access as access to the secrets.
- Secrets at rest. Secrets are still stored unencrypted in the database, and so appear in database dumps and backups.

The protection is against disclosure through API responses, logs, browser caches and screen shares.

---

## Time Display

The dashboard shows every absolute timestamp in the **Server** time zone, in **12-hour** form, with the zone written out (for example `2026-10-27 1:05:09 PM EDT`). Relative times such as "5m ago" are unchanged, and show the exact time when you hover over them. Chart axis ticks carry no zone label (`1:05 PM`); the chart tooltip does.

### Choosing the time zone and the clock

Click the clock in the header to open the time-display panel. It has two groups of options. The choice applies at once to every page, including chart axes and chart tooltips, and the panel stays open while you choose. Escape, a click outside the panel, or a second click on the clock closes it.

| Group | Options | Default |
|---|---|---|
| Time zone | **Server** (the server's display zone, shown with its name), **Local** (your browser's zone, shown with its name), **UTC** | Server |
| Clock | **12-hour** (`2026-10-27 1:05:09 PM EDT`), **24-hour** (`2026-10-27 13:05:09 EDT`) | 12-hour |

Midnight is `12:00 AM` and noon is `12:00 PM`. Dates stay `YYYY-MM-DD`. Systemd timer schedules (for example `Daily at 6:00 AM`) follow the clock choice but describe the agent host's own time, so they are not converted to another zone.

While the dashboard has not yet learned the server zone (the page is still loading), or when the server reports no valid zone, **Server** shows UTC and the panel reads `Server (UTC)`.

### Browser storage

The browser stores the choice in `localStorage` under two keys. Neither ever holds a zone name.

| Key | Values |
|---|---|
| `riot-time-zone` | `Server`, `UTC` or `Local` |
| `riot-hour-cycle` | `12h` or `24h` |

An unreadable, blocked or unrecognized value falls back to `Server` and `12h`. If the browser refuses to save, the choice still applies until the page is reloaded. The setting is per browser and is not synced to the server or to other browsers.

A choice saved by an older build is mapped once. An old `UTC` becomes **Server**, because the old build wrote `UTC` whenever the clock was toggled back and so it did not prove a deliberate choice. An old `Local` stays **Local**. A **UTC** choice made after this change persists as UTC. A saved `riot-hour-cycle` value marks a choice made after this change.

### Server display zone: `RIOT_DISPLAY_TZ`

Set `RIOT_DISPLAY_TZ` to an IANA zone name, for example `America/New_York`. It is the preferred setting: it changes display only. The server picks the first valid value of `RIOT_DISPLAY_TZ`, then `TZ`, then UTC. `TZ` is honoured as a fallback because many Docker setups already set it, but it also changes the server process's own zone and so the offsets in its log timestamps. Because of that fallback, a server that already sets a valid `TZ` and does not set `RIOT_DISPLAY_TZ` uses that `TZ` as its Server zone from the first start after upgrading. Check the value of `TZ` in your compose file before you upgrade, and set `RIOT_DISPLAY_TZ` if the Server zone should differ.

A value is valid when it is at most 64 characters, has 1 to 3 `/`-separated segments that start with a letter and use only letters, digits, `_`, `+` and `-`, is not `Local`, and is known to the server's zone database. Surrounding whitespace is ignored and a blank value counts as unset. A value that is set but invalid is skipped with one `WARN` log line that names the variable and shows the rejected value (cleaned of control and invisible characters, at most 64 characters). The server always starts. At every start one `INFO` line states the effective zone and where it came from. The server binary embeds a fallback zone database, so a valid name also works on a host that has none.

The compose files in this repository list the server's environment explicitly, so add `- RIOT_DISPLAY_TZ=America/New_York` to the `riot-server` service's `environment:` list. A value that only appears in `.env` does not reach the container. The zone is read at start; changing it needs a restart, and the dashboard picks it up on its next page load.

The dashboard reads the zone with `GET /api/v1/server/time-display` (administrator session only), which returns `{"display_zone":"America/New_York"}`, or `UTC` when no valid zone is configured. The dashboard uses the name only after checking it again in the browser; anything it does not accept is treated as UTC.

```
GET /api/v1/server/time-display
Cookie: riot_session=<administrator session token>

200 OK
Content-Type: application/json
Cache-Control: no-store

{"display_zone":"America/New_York"}
```

Without an administrator session the endpoint answers `401` with `{"error":"unauthorized"}` and never discloses the zone. Only `GET` is routed.

### Text the server writes

Text the server writes for a person uses the display zone and the 12-hour form, with a zone label, whatever the browser choice is:

- the patch badge lines (`Yes. Next window starts 2026-10-28 2:00 AM EDT.`, the reboot estimate and the as-of time) and the date in `Auto (reboot on 2026-10-27)`, which is the date in the display zone and has no time or label;
- the `Generated` line of the device summary file, and the date in the summary download's file name;
- the new event messages for a powered-off device, a suppressed offline (`back by 1:20 PM EDT`) and held escalation notifications (`held since 1:05 PM EDT`).

Of these event messages, only the held-escalation summary is sent to notification channels; the powered-off and expected-offline messages appear on the dashboard (Alerts, device events, activity history) and are not pushed. Event messages stored before an upgrade keep their old text. For a zone without a letter abbreviation the label is `GMT` and the offset (`GMT-3`, `GMT+5:45`), the same form the browser uses. Server text uses the zone database's abbreviation and the dashboard uses the browser's, so a few zones read differently on the two (for example `Asia/Kolkata` is `IST` in a push and `GMT+5:30` on the dashboard).

### What stays UTC

API time fields (RFC 3339 with a `Z` suffix, such as `reboot_at`, `updates_at` and an event's `created_at`), stored timestamps, and the partition, quota, retention and maintenance-window boundaries are UTC whatever the display zone is. The maintenance-window editor in Settings and the Docker activity "From (UTC)" and "To (UTC)" filters are unchanged.

---

## API

All endpoints are under `/api/v1/`. Agent endpoints require the `X-rIOt-Key` header. When mTLS is enabled, agent endpoints additionally require a valid client certificate (both are checked). Dashboard endpoints require admin authentication (JWT cookie). State-changing dashboard requests must also pass the [origin check](#origin-check-on-state-changing-requests).

### Public

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Health check. Returns `200` with `"status":"ok"` when the database is reachable, and `503` with `"status":"degraded"` when it is not. Answers within about 2 seconds even if the database hangs |
| `GET` | `/readyz` | Readiness check. Returns `200` with `"status":"ready"` only when the database is reachable and schema migrations have completed, otherwise `503` with `"status":"not_ready"`. Never blocked by first-run setup |
| `POST` | `/api/v1/auth/login` | Admin login (returns JWT cookie) |
| `POST` | `/api/v1/auth/logout` | Clear session cookie |
| `POST` | `/api/v1/auth/change-password` | Change admin password |
| `GET` | `/api/v1/auth/check` | Check authentication status |
| `GET` | `/api/v1/auth/oidc` | SSO availability probe — `{"available": bool, "label": string}`. Never `404`; answers `{"available": false}` when SSO is dormant or setup is incomplete |
| `GET` | `/api/v1/auth/oidc/start` | Begin an SSO login — redirects to the identity provider. `404` when SSO is dormant or setup is incomplete |
| `GET` | `/api/v1/auth/oidc/callback` | SSO callback from the identity provider — redirects to the dashboard on success, or to the login screen with `?sso_error=<code>` on failure. `404` when SSO is dormant or setup is incomplete |
| `GET` | `/api/v1/server-cert` | Server TLS certificate + fingerprint (for agent TOFU) |
| `GET` | `/api/v1/setup/status` | Setup wizard status |
| `POST` | `/api/v1/setup/complete` | Complete setup wizard |

### Agent (device key or mTLS auth)

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/devices/register` | Register a new device, or re-register an existing one. Re-registration requires proof of the device's identity (see [Device Registration and Re-registration](#device-registration-and-re-registration)) |
| `POST` | `/api/v1/devices/:id/heartbeat` | Lightweight heartbeat |
| `POST` | `/api/v1/devices/:id/telemetry` | Full telemetry push. The optional `updates.lists_freshness` and `updates.supports_reboot_if_required` fields are accepted from any agent version. The server reads an unknown `lists_freshness` value as `unknown`, and anything other than JSON `true` in `supports_reboot_if_required` as not supported. Neither can cause a rejection |
| `POST` | `/api/v1/devices/:id/docker-events` | Real-time Docker container events |
| `POST` | `/api/v1/devices/:id/events` | Report an agent event, such as the start, completion or failure of an agent update. See [Agent events](#agent-events) |
| `POST` | `/api/v1/devices/:id/commands/:cid/report` | Report command progress or the final result over HTTP when the WebSocket is down. See [Command endpoints](#command-endpoints) |
| `GET` | `/api/v1/update/check` | Agent update check. Returns the release asset locations, including `checksum_url` and `signature_url` (a locator only; the agent trusts its compiled-in key, not this response) |

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
| `POST` | `/api/v1/devices/:id/rotate-key` | Rotate device API key. Deletes all of the device's keys and returns one new key (`api_key`) once. This is the recovery step for an agent that lost its key |
| `POST` | `/api/v1/devices/:id/commands` | Send command to agent. Returns `409` when the target already has a command in flight |
| `GET` | `/api/v1/devices/:id/commands` | List device command history (query: `limit`, `offset`, `action`, `status`, `active`, `target_key`) |
| `GET` | `/api/v1/devices/:id/commands/:cmdId` | Fetch one command with its state, step, progress, and per-state timestamps |
| `POST` | `/api/v1/devices/:id/commands/:cmdId/cancel` | Cancel a `queued` command. Returns `409` once the command is dispatched |
| `GET` | `/api/v1/devices/:id/commands/:cmdId/output` | Command output detail |
| `GET` | `/api/v1/devices/:id/commands/:cmdId/events` | Events linked to a command or agent self-update record, server-authored and agent-sourced separately |
| `POST` | `/api/v1/devices/:id/docker/bulk-update` | Update several containers with one `docker_bulk_update` command. Targets that already have a command in flight are skipped and reported |
| `GET` | `/api/v1/summary` | Fleet summary stats |
| `GET` | `/api/v1/events` | Event/alert list |
| `GET` | `/api/v1/events/unread-count` | Count of unacknowledged events |
| `POST` | `/api/v1/events/:id/acknowledge` | Acknowledge a single event |
| `POST` | `/api/v1/events/acknowledge-all` | Acknowledge all events |
| `GET` | `/api/v1/update/server` | Server update check |
| `GET` | `/api/v1/server/time-display` | The server display zone for the dashboard's Server time zone: `{"display_zone":"America/New_York"}`, or `UTC` when none is valid. Administrator session only (`401` `{"error":"unauthorized"}` otherwise; a device API key is not accepted); `Cache-Control: no-store`; `GET` only (see [Time Display](#time-display)) |
| `GET/POST/PUT/DELETE` | `/api/v1/settings/alert-rules[/:id]` | Alert rule CRUD |
| `GET` | `/api/v1/settings/alert-templates` | List alert templates |
| `GET/POST/PUT/DELETE` | `/api/v1/settings/notification-channels[/:id]` | Notification channel CRUD. Secrets are write-only (see [Notification Channels](#notification-channels)); `PUT` to an unknown id returns `404` |
| `POST` | `/api/v1/settings/notification-channels/:id/test` | Test notification channel |
| `GET` | `/api/v1/settings/notifications/log` | Notification delivery log |
| `GET` | `/api/v1/settings/registration` | Get registration key setting |
| `PUT` | `/api/v1/settings/registration` | Set registration key (empty = open registration) |
| `GET` | `/api/v1/settings/certs` | List device certificates |
| `POST` | `/api/v1/settings/certs/:serial/revoke` | Revoke a device certificate |
| `GET/POST/DELETE` | `/api/v1/settings/bootstrap-keys[/:hash]` | Bootstrap key CRUD |
| `POST` | `/api/v1/settings/tls/regenerate` | Regenerate server TLS certificate |
| `GET` | `/api/v1/fleet/agent-versions` | Agent version summary |
| `GET` | `/api/v1/fleet/patch-status` | Fleet patch status overview — per device: `pending_updates`, `security_count`, `reboot_class_count`, `reboot_required`. Add `?detail=true` for the full `updates` list (each carries a `class`: `gpu_driver` / `kernel` / empty) plus `package_manager`. By default lists only devices with at least one pending update or a reboot requirement; add `?all=true` for one row per registered device. Each row also carries the optional fields `os_patch_mode`, `next_window_start`, `os_patch_cooldown_minutes`, `allow_reboot`, `updates_at` and `patch_badge` — see [Patch badge](#patch-badge). Every row also carries `lists_freshness` (`fresh`, `stale` or `unknown`; always present). A row also carries `extra_updaters` (at most 8 entries, only those with an update waiting) when a DietPi update is pending, and a host whose only waiting update is a DietPi update is listed. `reboot_required` is true only when a reboot is required now, never for a pending kernel update — see [Manual patch and reboot](#manual-patch-and-reboot) |
| `POST` | `/api/v1/fleet/bulk-update` | Bulk update agents. The response adds `conflicts`, the number of devices skipped because a host command was already in flight |
| `POST` | `/api/v1/fleet/bulk-patch` | Bulk patch devices. The response adds `conflicts`, as for bulk-update |
| `GET` | `/api/v1/activity` | What each device is doing right now, computed by the server from commands and device state. See [Activity endpoint](#activity-endpoint) |
| `GET` | `/api/v1/fleet/heartbeats` | 60-minute heartbeat history for every device in one request (query: `window`, e.g. `?window=60m`); used by the fleet dashboard |
| `GET` | `/api/v1/fleet/containers` | Flat list of all containers across the fleet with CPU, memory, restart count, update status, Compose stack name, and auto-update state (`auto_update_policy`: `target`, `enabled`, optional `covered_by`; omitted when policies cannot be loaded); used by the fleet dashboard leaderboard and the Containers table |
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
| `/ws` | Live dashboard updates (admin auth). Message types are listed under [WebSocket messages](#websocket-messages) |
| `/ws/agent` | Agent WebSocket: command delivery, command lifecycle reports, and the terminal relay |
| `/ws/terminal/:deviceId/:containerId` | Browser-to-container terminal (admin auth) |

### Activity endpoint

`GET /api/v1/activity` returns what each device is doing right now, as the server computes it from the command records and the device state (see [Expected Activity](#expected-activity)). The dashboard uses it for the labels on the Devices page and the device page.

- **Authentication:** an admin session (the dashboard cookie). A request without one gets `401`. A device key is not accepted.
- **Request:** no parameters, no body.
- **Rate limiting:** this endpoint has none of its own.
- **Current state, not a delta.** The server answers from memory, so the call is cheap. A client that reconnects its WebSocket calls it again.

**`200`** returns an object:

| Field | Type | Meaning |
|---|---|---|
| `generated_at` | RFC 3339 time | When the server built the answer |
| `newest_agent_version` | string | The newest agent version the server knows. Empty when unknown |
| `activities` | array | At most one entry per device that has an activity. `[]` when none. Never `null` |

Each entry of `activities`:

| Field | Type | Meaning |
|---|---|---|
| `device_id` | string | The device |
| `kind` | string | `host`, `agent`, `docker`, `powered_off` or `escalated` |
| `phase` | string | `queued`, `in_progress`, `return`, `settling`, `powered_off` or `escalated` |
| `action` | string | The command's action, for example `reboot` |
| `command_id` | string | The command that caused the activity |
| `actor_phrase` | string | Who started it: `by ryan`, `by auto-update policy for plex` or `by the auto-patch schedule`. Printable text of at most 128 bytes per name |
| `started_at` | RFC 3339 time | When the server dispatched the command, or created it while it is queued. For `powered_off`, when the shutdown succeeded |
| `back_by` | RFC 3339 time or `null` | When the device is due back, in the return phase |
| `waiting_dpkg_lock` | boolean | `true` while the device is online and its running `reboot` or `shutdown` command reports the step `wait_dpkg_lock`. Always `false` for an agent that does not report the step (agents older than the first release after v2.88.0). See [Agent-side activity reporting](#agent-side-activity-reporting) |
| `progress` | integer or `null` | The command's reported progress, in percent |
| `agent_from_version` | string | For `agent_update`, the agent version when the command was created. For an [agent self-update](#agent-self-updates), the version it started from |
| `agent_to_version` | string | For an agent self-update (and its escalation), the version it updates to. Empty for every command |
| `target` | string | For Docker actions, the container or stack name. Empty otherwise |
| `docker_actions` | integer | The number of Docker actions open on the device |
| `covers_offline` | boolean | `true` when the window now suppresses `device_offline` for this device |
| `device_online` | boolean | The server's view of the device |
| `escalation` | object or `null` | When escalated: `reason` (`failed`, `timed_out`, `no_result` or `not_back`), `bound` (for example `"15m"`) and `at` (RFC 3339 time) |
| `powered_off_at` | RFC 3339 time or `null` | For the powered-off state, when the shutdown succeeded |

**`503`** with `{"error":"activity unavailable","code":"activity_unavailable"}` when the server runs without the tracker. The dashboard then shows the plain status. When the tracker has not finished loading, the server answers `200` with the last activities it holds. The activity is for display. Alert decisions fail open on their own.

Example request:

```bash
curl -s -b "riot_session=<session cookie>" https://riot.example.com/api/v1/activity
```

Example response:

```json
{
  "generated_at": "2026-10-26T14:05:12Z",
  "newest_agent_version": "2.78.0",
  "activities": [
    {
      "device_id": "d3f1c2a4-0000-4000-8000-000000000001",
      "kind": "host",
      "phase": "return",
      "action": "reboot",
      "command_id": "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
      "actor_phrase": "by ryan",
      "started_at": "2026-10-26T14:02:00Z",
      "back_by": "2026-10-26T14:17:30Z",
      "waiting_dpkg_lock": false,
      "progress": null,
      "agent_from_version": "",
      "target": "",
      "docker_actions": 0,
      "covers_offline": true,
      "device_online": false,
      "escalation": null,
      "powered_off_at": null
    }
  ]
}
```

### Command endpoints

All command endpoints sit under `/api/v1/devices/:id`. The admin endpoints require the dashboard session cookie. The report endpoint requires the device key in `X-rIOt-Key`, plus the client certificate when mTLS is enabled.

A command ID is a lowercase UUID. The server answers `404` with `{"error":"command not found","code":"command_not_found"}` for a missing command, a command that belongs to another device, and an ID that is not a UUID. The three cases return the same body.

**The command object**

| Field | Type | Description |
|---|---|---|
| `id`, `device_id`, `action`, `params` | string, string, string, object | Identity and request |
| `status` | string | One of the ten [states](#states-and-what-the-dashboard-shows). Rows written by older servers are returned in canonical form |
| `result_msg` | string | Result or failure message from the agent, or the reason the server expired or cancelled the command |
| `step`, `progress` | string, integer or `null` | Current step and an optional percent from 0 to 100 |
| `failure_step`, `stderr_tail` | string, string | The step that failed, and the last 8 KiB of error text |
| `origin` | string | `user`, `auto_update`, `auto_patch`, or `agent_self_update` (a record of an agent's own [self-update](#agent-self-updates): read-only, never sent to an agent, `params` hold `from_version` and `to_version`) |
| `policy_id` | integer or `null` | The auto-update policy that created the command |
| `target_key`, `target_keys` | string, string array | The conflict keys for the command. `target_key` is the first entry. Examples: `host`, `docker:container:web`, `docker:compose:/srv/stack` |
| `expires_at`, `exec_deadline_s`, `deadline_at` | RFC 3339, integer, RFC 3339 | The queued expiry, the execution deadline in seconds, and the absolute deadline (set at dispatch) |
| `dispatched_at`, `acknowledged_at`, `started_at`, `verifying_at`, `finished_at` | RFC 3339 | Per-state timestamps. Each is absent until the command reaches that state |
| `created_at`, `updated_at`, `duration_ms`, `exit_code` | | As before |

**`POST /api/v1/devices/:id/commands`**

Request body: `{"action": "<action>", "params": {...}}`. `action` must be a supported action. `params` is optional.

| Status | Body |
|---|---|
| `201` | The command object. `status` is `queued`, or `dispatched` when the agent was connected and the server sent it immediately |
| `400` | `{"error":"invalid request body"}` or `{"error":"invalid action"}`. For `os_update`, the three `reboot_if_required` rejections below also use `400` |
| `409` | The conflict body below |
| `500` | `{"error":"failed to create command"}` |

The `409` conflict body names the command that holds the target:

```json
{
  "error": "a command is already in progress for this target",
  "code": "conflict",
  "existing_command_id": "3f2c9a7e-5b1d-4c8e-9a0f-1d2e3f4a5b6c",
  "existing_status": "running",
  "target_key": "docker:container:web"
}
```

The dashboard uses `existing_command_id` to attach to the command that is already running.

**`os_update` parameters**

`POST /api/v1/devices/:id/commands` with `"action": "os_update"` accepts these `params`:

| Param | Type | Rules |
|---|---|---|
| `mode` | string, optional | `full` (default), `security` or `repair`. See [OS maintenance](docs/ops/os-maintenance.md) |
| `reboot_if_required` | boolean, optional | `true` releases rIOt's reboot-class holds for this one run and lets the agent reboot after the run, only if a reboot is required then. Absent means `false`. The server does not store `false`. Any other JSON type is rejected |
| `include_reboot_class` | — | Never accepted from a client. The server removes the key in any capitalization. It sets `include_reboot_class: true` itself only for a valid `reboot_if_required: true` |
| `exclude_extra_updaters` | boolean, optional | `true` leaves extra updaters (DietPi) out of this one run. Absent or `false` includes them. Any other JSON type, or a differently capitalized key, gets `400` with `invalid_request` and creates no command. The server does not store `false` |
| `extra_updater_budget` | — | Never accepted from a client. The server removes the key in any capitalization, for every action. It stamps `extra_updater_budget: true` itself, which gives the run the longer [DietPi bounds](#dietpi-updates) |

The server checks `reboot_if_required: true` against the device's latest telemetry before it creates the command. A rejected request creates no command and no event.

| Status | Body | When |
|---|---|---|
| `201` | The command object. For an authorized request, `params` holds `reboot_if_required: true` and `include_reboot_class: true` | The request is valid |
| `400` | `{"error":"invalid request","code":"invalid_request"}` | `reboot_if_required` is present and is not a JSON boolean (`"true"`, `1`, `null`, an object or an array). Also: the key is written in another capitalization, or `reboot_if_required: true` comes with `mode: repair` |
| `400` | `{"error":"reboots are turned off on this agent (commands.allow_reboot: false)","code":"reboot_not_allowed"}` | `reboot_if_required: true`, and the latest telemetry reports `allow_reboot: false` |
| `400` | `{"error":"the agent must be updated to authorize a reboot with a patch","code":"reboot_authorization_unsupported"}` | `reboot_if_required: true`, and the device has no telemetry, no `updates` section, no `allow_reboot` value, or no `supports_reboot_if_required: true`. The last case is an agent older than this feature, or an agent with the `updates` collector disabled |
| `500` | `{"error":"failed to create command"}` | The server could not read the device's latest telemetry |

Example request body:

```json
{"action": "os_update", "params": {"mode": "full", "reboot_if_required": true}}
```

Example `201` response (abbreviated):

```json
{
  "id": "3f2c9a7e-5b1d-4c8e-9a0f-1d2e3f4a5b6c",
  "device_id": "b7e1c2d4-0a1b-4c3d-8e9f-0a1b2c3d4e5f",
  "action": "os_update",
  "params": {"mode": "full", "reboot_if_required": true, "include_reboot_class": true},
  "status": "dispatched"
}
```

The command-sent event for an authorized run ends with ` (reboot authorized if required)`. The final `result_msg` of the run ends with one sentence that states the outcome. See [What the result says](#what-the-result-says).

**`POST /api/v1/devices/:id/docker/bulk-update`**

Request body: `{"container_ids": ["<id>", ...]}`. The server creates one `docker_bulk_update` command for the targets that do not conflict.

- `201`: the command object with an added `conflicts` array. Each entry is `{"container_id", "target_key", "existing_command_id"}` for a skipped target. The array is empty when nothing was skipped.
- `409`: every target conflicts. The body has the conflict fields above plus the `conflicts` array.
- `400`: `container_ids` is missing or empty.

**`GET /api/v1/devices/:id/commands`**

Returns a JSON array of command objects. The array is newest first, except with `active=true`, where it is oldest first.

| Query parameter | Description |
|---|---|
| `limit` | Page size. Default `50` |
| `offset` | Rows to skip |
| `action` | Return only this action |
| `status` | Comma-separated states. Accepts the ten canonical states and the legacy values `pending`, `sent`, `success`, and `error` |
| `active=true` | Return every non-terminal command for the device (`queued` through `verifying`). With this parameter, `limit`, `offset`, and `action` do not apply |
| `target_key` | Return only commands whose `target_keys` contain this key. Maximum 512 bytes. Combine with `active=true` to find the in-flight command for one container or stack |

An unknown `status` value, or a `target_key` longer than 512 bytes, returns `400` with `{"error":"invalid status filter","code":"invalid_filter"}` (or `"invalid target_key filter"`).

Example: `GET /api/v1/devices/<id>/commands?active=true&target_key=docker:container:web`.

**`GET /api/v1/devices/:id/commands/:cid`**

Returns the command object (`200`) or `404`.

**`POST /api/v1/devices/:id/commands/:cid/cancel`**

Cancels a `queued` command. The request body is empty.

| Status | Body |
|---|---|
| `200` | The command object with `status` set to `cancelled` |
| `404` | The not-found body above |
| `409` | `{"error":"command cannot be cancelled after it has been dispatched","code":"not_cancellable","status":"<current state>"}` |

**`POST /api/v1/devices/:id/commands/:cid/report`** (agent)

The agent calls this endpoint to report a command when the WebSocket is down. The agent is the only intended caller. The server applies the device scope from the authenticated key, so a device can report only its own commands.

Request body, capped at 512 KiB:

| Field | Type | Description |
|---|---|---|
| `kind` | string, required | `ack`, `progress`, or `result` |
| `command_id` | string, optional | Must equal the `:cid` path value when present |
| `state` | string | For `progress`: `running` or `verifying` |
| `step` | string | Matches `^[a-z0-9_]{1,64}$`. Any other value is stored as empty and the report still applies |
| `progress` | integer | 0 to 100. The server drops an out-of-range value and still applies the state change |
| `message` | string | Progress message. The server keeps the first 1 KiB |
| `result` | object | For `result`: the final result with `status` (`success` or `error`), `message`, `exit_code`, `duration_ms`, `output`, and the optional `step`, `outcome`, `stderr`, and `reason` |

The server removes NUL bytes, replaces invalid UTF-8, and truncates oversized text instead of rejecting the report.

| Status | Body | Agent action |
|---|---|---|
| `200` | `{"command_id","result":"accepted"\|"duplicate"\|"coalesced","status"}` | Remove the outbox entry |
| `400` | `{"error","code":"invalid_report"}` for an ID mismatch, an unknown `kind` or `state`, a missing `result`, or a non-UUID ID | Drop the report |
| `401` | Missing or invalid `X-rIOt-Key`. The endpoint also answers `401` when a request reaches it without device authentication, because it never trusts the `:id` in the URL | Retry with backoff |
| `403` | The key does not match the device in the URL | Retry with backoff |
| `404` | `{"error":"command not found","code":"command_not_found"}` | Remove the entry |
| `409` | `{"error","code":"already_terminal"\|"invalid_transition","status"}` | Remove the entry |
| `413` | `{"error":"report too large","code":"invalid_report"}` | Drop the report |
| `429` | `{"error":"too many requests","code":"rate_limited"}` | Retry with backoff |

A `result` that repeats the outcome of an already-final command returns `200` with `"result":"duplicate"` and changes nothing. A `result` with a different outcome returns `409` `already_terminal`. Identical or too-frequent `running` progress reports return `200` with `"result":"coalesced"` and write nothing. The rate limit is 120 reports per minute per device with a burst of 60, shared by HTTP and WebSocket reports.

### Command events

`GET /api/v1/devices/:id/commands/:commandId/events` (admin session) returns the events linked to one command or [agent self-update record](#agent-self-updates), in two groups, so an agent that floods its own record cannot push the server's own events out of the view.

**`200`**

| Field | Type | Meaning |
|---|---|---|
| `server_events` | array | Every event the server wrote for the record: the suppressed offline and the online note (`command_attr` `expected`), an escalation (`escalation`), the outcome event (`outcome`) and the three `agent_self_update_*` types. Oldest first, at most 500. At most 19 for a self-update record |
| `server_events_omitted` | integer | Server-authored events that were not returned (`0` for a self-update record) |
| `agent_events` | array | The agent's own events linked to the record (`command_attr` `inferred` or `agent`). Oldest first, at most 50 |
| `agent_events_omitted` | integer | Agent-sourced events that were not returned |

Both arrays are `[]` when empty, never `null`. **`404`** with `{"error":"command not found","code":"command_not_found"}` for a malformed ID, a missing command and a command of another device; the three cases return the same body. **`500`** with `{"error":"failed to list events"}`.

### Agent events

`POST /api/v1/devices/:id/events` is how an agent, and the installer, report an event about themselves. It needs the device's `X-rIOt-Key` header, and a client certificate when mTLS is on. The key must belong to the device in the URL.

**Request body** (JSON, at most 1 MiB):

| Field | Type | Required | Meaning |
|---|---|---|---|
| `type` | string | yes | One of `agent_update_available`, `agent_update_started`, `agent_update_completed`, `agent_update_failed`, `extra_updater_started`, `extra_updater_finished` (see [DietPi updates](#dietpi-updates)) or `agent_reinstall_started` (see [Installer reinstalls](#installer-reinstalls)). Any other value gets `400` |
| `severity` | string | no | `info`, `warning` or `critical`. For the four `agent_update_*` types any other value is stored as `info` (`warning` for `agent_update_failed`) |
| `message` | string | no | Stored as sent. The server never rewrites it for `agent_update_*` events |
| `command_id` | string | no | On `agent_update_started`, `_completed` and `_failed`: the ID of the `agent_update` command that caused the update. It must be a canonical UUID. Sent by an update that Ryan or a schedule started. Never sent together with `attempt_id` |
| `attempt_id` | string | no | On the same three types: the agent's own correlation ID for a self-update. 1 to 64 bytes of ASCII letters, digits and hyphens. The agent sends 32 hexadecimal characters. Never sent together with `command_id` |
| `from_version`, `to_version` | string | no | On the same three types, always as a pair: the version the update leaves and the one it installs. At most 32 bytes each, in the form `2.88.0` with an optional leading `v` and an optional `-` or `+` suffix of up to 16 characters. The server uses them on `agent_update_started` only, and only when both are valid and different |
| `updater`, `outcome`, `reason` | string | no | Used by the `extra_updater_*` types only |

The server treats `command_id`, `attempt_id`, `from_version` and `to_version` as untrusted. It reads them only when the key that signed the request belongs to the device in the URL. An invalid or unknown value never changes the status code. The server ignores the field and processes the event by its message and timing, as in [Agent self-updates](#agent-self-updates). A request that carries both `command_id` and `attempt_id` counts as carrying neither. Agents from before these fields existed omit all four, and the server accepts that unchanged. An older server ignores the fields.

**Responses**

| Status | Body | When |
|---|---|---|
| `200` | `{"status":"ok"}` | The event was stored |
| `200` | `{"status":"duplicate"}` | The event repeats one the server already stored for the same device, the same linked command or record, and the same type, where the server linked it by a valid reference. Nothing is stored or broadcast again. The agent treats it as success. The `extra_updater_*` types answer `duplicate` by their own rule |
| `200` | `{"status":"ok","reinstall":"<result>"}` | Only for `agent_reinstall_started`. See [Installer reinstalls](#installer-reinstalls) |
| `400` | `{"error":"invalid request body"}` | The body is not valid JSON |
| `400` | `{"error":"unsupported event type"}` | The `type` is not in the list above, including the server-written `agent_reinstall_unconfirmed` and `agent_reinstall_paused` |
| `401` | `{"error":"missing X-rIOt-Key header"}` or `{"error":"invalid api key"}` | No key, or a wrong or revoked key |
| `403` | `{"error":"api key does not match device"}` | The key belongs to another device |
| `413` | `{"code":"payload_too_large"}` | The body is larger than 1 MiB |
| `429` | `{"code":"rate_limited"}` | The device sent more than the allowance of 2 requests per second (burst 120), shared with `POST /api/v1/devices/:id/docker-events`. The reply carries `Retry-After: 15` |
| `500` | `{"error":"failed to create event"}` | The event could not be stored |

The agent repeats an `agent_update_started`, `_completed` or `_failed` event after a `5xx` or `429` answer or a timeout, and after no other answer. See [Agent-side activity reporting](#agent-side-activity-reporting) for the bound.

**Example.** A self-update of the agent reports its start. The request:

```
POST /api/v1/devices/3f2c9a7e-0d4b-4f0e-9a51-7a1c2b6d8e90/events
X-rIOt-Key: <device key>
Content-Type: application/json

{
  "type": "agent_update_started",
  "severity": "info",
  "message": "Agent update started: 2.88.0 → 2.89.0",
  "attempt_id": "9c1f0e2a7b3d4c58a6e1f2b3c4d5e6f7",
  "from_version": "2.88.0",
  "to_version": "2.89.0"
}
```

The reply:

```
HTTP/1.1 200 OK
Content-Type: application/json

{"status":"ok"}
```

A repeat of that request, sent because the first reply was lost, gets `{"status":"duplicate"}` and stores nothing. An update that Ryan started from the dashboard sends `"command_id": "<the command's UUID>"` in place of `attempt_id`, and the server links the event to that command.

### WebSocket messages

**Server to dashboard (`/ws`)**

| `type` | When | Notes |
|---|---|---|
| `command_update` | Once per accepted command state change | Replaces `command_result`. `data` holds `command_id`, `device_id`, `action`, `target_key`, `target_keys`, `status`, `step`, `progress`, `message`, `failure_step`, `origin`, and `updated_at`. Rejected and coalesced reports produce no message. The silent relabelling of pre-upgrade rows produces none either |
| `telemetry` | Each accepted live snapshot | The envelope gains `collected_at` (the agent's collection time, never more than 5 minutes ahead of `received_at`, the same allowance as the ingest clamp) and `received_at` (the server's receive time) |
| `heartbeat` | Each heartbeat | The envelope gains `received_at` |
| `activity_update` | A device's activity appears, changes or ends | `{"type":"activity_update","device_id":"…","data":<activity>}`. `data` has the same shape as one entry of `GET /api/v1/activity`. `data` is an explicit `null` when the activity ends. The server sends it only on a change, so a dashboard that reconnects refetches `GET /api/v1/activity` |

The dashboard discards a `telemetry` message whose `collected_at` is at or before the latest one it already applied for that device. It reads a device's last-seen time from `received_at`, never from the browser clock. The server stores every snapshot for history but does not replace a device's current state with an older one.

**Agent and server (`/ws/agent`)**

| Direction | `type` | `data` |
|---|---|---|
| Server to agent | `command` | `command_id`, `action`, `params`. Unchanged |
| Server to agent | `command_receipt` | `command_id`, `result` (`accepted`, `duplicate`, `coalesced`, `already_terminal`, `invalid_transition`, or `not_found`), `status`. Sent after each `command_result` |
| Agent to server | `command_ack` | `command_id`, `at` |
| Agent to server | `command_progress` | `command_id`, `state` (`running` or `verifying`), `step`, optional `progress` and `message`, `at` |
| Agent to server | `command_result` | The final result, with the optional `step`, `outcome`, `stderr`, and `reason` fields added |

The server scopes every agent message to the device that authenticated the socket. A message that names a command from another device is ignored. The server drops a message with a non-UUID command ID or a message over the rate limit, and sends no receipt for it. Old servers ignore `command_ack` and `command_progress`.

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

# Build the Linux update verifier (not part of make build-agent-all).
# Replace <suffix> with the platform, e.g. linux-amd64. Without -X the verifier
# refuses every update. Linux targets only (set GOOS=linux when cross-building).
go build -ldflags "-X main.targetAssetName=riot-agent-<suffix>" -o riot-agent-apply ./cmd/riot-agent-apply

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

Run one test or one group by name:

```bash
go test ./internal/server/ -run TestEX_AC048 -v
go test ./... -run TestEX_ -count=1
go test ./... -run TestEX2_ -count=1
```

A passing run ends each package with `ok`. The `TestEX_` tests cover [Expected Activity](#expected-activity). The `TestEX2_` tests cover [agent self-updates](#agent-self-updates), including the safety scan that fails if a test or helper of that feature could run a power action, a package manager or an agent. The self-update web tests run with `cd web && npx vitest run -t "EXPECT-002"`.

Some tests in `internal/server/db`, `storage`, `commands`, `handlers` and `server` need a real PostgreSQL. They skip, and do not fail, unless `RIOT_TEST_PG_URL` is set. The database name in the URL must contain `test`, for example `postgres://user:pass@localhost:5432/riot_test?sslmode=disable`, because these tests create schemas and run migrations up and down. Set `RIOT_TEST_PG_ALLOW=1` only if you accept that the tests run against a database whose name has no `test` in it. Use a throwaway database.

CI runs automatically on push to main and on PRs via GitHub Actions (`.github/workflows/ci.yml`). One job, `test-go-integration`, spins up a disposable PostgreSQL 16 service and runs the partitioning integration suite (`go test -tags=integration ./internal/server/partition/...`) against it — this is the only CI job that requires Postgres; `test-go` and `test-web` do not. Running that suite locally requires your own PostgreSQL 16 instance (set `RIOT_TEST_DB_URL`); without it, the integration tests skip cleanly rather than fail.

Frontend contributors: the shared UI components, design tokens and the rules that tests enforce (no native `confirm()`, no hex colors in primitives, one formatting module) are described in [web/src/components/ui/README.md](web/src/components/ui/README.md).

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
- Cross-compiles 8 agent binaries and 5 Linux `riot-agent-apply-*` update verifiers, with SHA-256 checksums
- Signs `checksums.txt` with the ed25519 release key and verifies the signature against the committed public key (the release fails if either step fails)
- Creates a GitHub Release with auto-generated release notes, `checksums.txt`, and `checksums.txt.sig`

### One-time setup: release signing (required before the next tag)

> **Do this before you push the next `v*` tag.** `internal/releasekey/releasekey.go` currently holds the placeholder `REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY`. With the placeholder, the `Sign checksums` step fails (no secret) and the `Verify signature against embedded key` step fails (invalid key), so no GitHub Release is created. The server Docker image job is independent and still publishes. Agents keep running their current version, but no signed release exists for them to update to until you finish these steps.

1. **Generate the keypair.** Run from the repository root:

   ```bash
   go run ./cmd/riot-signer genkey
   ```

   The tool prints two lines. Copy both values:

   ```
   PUBLIC_KEY=<44-character base64 string>
   PRIVATE_KEY=<88-character base64 string>
   ```

2. **Commit the public key.** In `internal/releasekey/releasekey.go`, replace `REPLACE_WITH_BASE64_ED25519_PUBLIC_KEY` with the `PUBLIC_KEY` value, keeping the quotes:

   ```go
   var PublicKeyBase64 = "<PUBLIC_KEY value>"
   ```

   Commit the change. The public key is not secret.

3. **Set the private key as a repository secret.** In GitHub, open **Settings > Secrets and variables > Actions > New repository secret**. Name it `AGENT_SIGNING_PRIVATE_KEY` and paste the `PRIVATE_KEY` value. You can also run `gh secret set AGENT_SIGNING_PRIVATE_KEY` and paste the value at the prompt. Do not pass the key as a command-line argument, because it would land in your shell history.

4. **Store the private key offline.** Keep a copy in a password manager or other offline store, then clear it from your terminal scrollback. Do not commit it, and do not put it in an issue or chat.
   - If you lose it, you cannot sign future releases. Recovery is a key rotation.
   - If it leaks, anyone holding it can sign malicious updates that agents accept. Rotate immediately (see [Rotating the signing key](#rotating-the-signing-key)).

5. **Tag a release and confirm the signing steps pass.** Follow the tag commands above. In the GitHub Actions run for the tag, open the `agent-binaries` job and confirm:
   - `Sign checksums` and `Verify signature against embedded key` both show a green check. The verify step logs `OK: dist/checksums.txt.sig verifies against the committed release public key`.
   - The GitHub Release lists `checksums.txt`, `checksums.txt.sig`, and five `riot-agent-apply-*` assets (`linux-amd64`, `linux-386`, `linux-arm64`, `linux-armv7`, `linux-armv6`).
   - No log line contains the private key.

Optional local check before tagging. This runs the same sign-and-verify the workflow runs, with a throwaway file:

```bash
echo test > /tmp/checksums.txt
AGENT_SIGNING_PRIVATE_KEY=<PRIVATE_KEY value> go run ./cmd/riot-signer sign /tmp/checksums.txt /tmp/checksums.txt.sig
go run ./cmd/riot-signer verify /tmp/checksums.txt /tmp/checksums.txt.sig
```

`verify` prints `OK: ...` and exits 0 when the committed public key matches the private key. It exits 1 with `committed release public key is unusable` while the placeholder is still in place. Run it in a shell where the private key will not be saved to history (for example, prefix the line with a space if your shell ignores space-prefixed commands).

After the first signed release, run `riot-agent doctor` on a re-installed host and confirm the **Agent Update** section passes. See [Agent Auto-Update and Release Signing](#agent-auto-update-and-release-signing) for what operators must do to receive the hardened update path.

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
| Container logs | 7 days; `RIOT_CONTAINER_LOG_RETENTION_DAYS` accepts 1–90 |
| Device logs | 30 days (configurable via `RIOT_RETENTION_DAYS`) |
| Device registry | Forever (until manually deleted) |

A background worker purges expired data hourly for `events`, `notification_log`, server logs, device probe results, and stale bootstrap keys.

### Partitioned tables and drop-based retention

The six high-churn tables — `heartbeats`, `telemetry_snapshots`, `probe_results`, `container_metrics`, `container_logs`, and `device_logs` — are range-partitioned by day and retained by dropping expired whole partitions. Container logs have their own retention setting, described below. Before upgrading an existing database, follow the [Docker upgrade procedure](docs/ops/docker-upgrade.md). Partitioning changes two operational realities:

- **On first boot after upgrading to the version that ships this**, the server converts each of the six tables to a partitioned table automatically, before it starts serving requests. The conversion is a metadata operation (rename + attach), not a data copy, so it does not scale with table size and does not rewrite existing rows — but it does briefly hold an exclusive lock on each table while the swap happens, so expect a short pause in server availability during that one boot, proportional to the number of tables rather than their size. Subsequent boots are a no-op.
- **The conversion is effectively one-way.** The migration's down-direction can restore the original plain tables, but only recovers pre-conversion data — anything written after the conversion is discarded by the downgrade. Treat downgrading past this version as a dev/test operation only, not a production rollback.

Because a day-partition is only dropped once it is *entirely* past the retention window, actual retention on these six tables is "at least the configured window" — up to one extra day of data may be retained beyond it. This does not affect the numbers in the table above; it is a rounding-up, not a change to the configured duration.

This does **not** reclaim disk space already lost to bloat from before the conversion — see [`docs/ops/postgres-bloat.md`](docs/ops/postgres-bloat.md) for the one-time `VACUUM FULL` / `pg_repack` reclaim that partitioning does not perform on its own.

---

## Docker Container Management

Open **Containers** under **Docker** in the application sidebar for containers across every Docker host. The default table supports search, host and stack filters, status chips, sorting and selection. **Tiles** keeps the existing Compose, network and standalone grouping. Filters and selection can be shared in a URL; opening a link never starts an action. Older per-device Docker links redirect into the fleet section. Docker shares the application navigation and resource header pattern; it has no additional sidebar.

The table keeps **Name** pinned and gives it remaining space, while **CPU** and **Memory** each use a fixed 160px column with equal graph-track widths. Their existing percentage and memory-limit scales stay distinct. Known update availability remains beside the name even when **Image** is hidden. Long names truncate; the tooltip retains the full container name and image. Narrow views scroll inside the table without squeezing the metric columns. Missing or invalid sort defaults to **Stack ascending**, keeping Compose members together per host, then ordered by container name; containers without a stack appear together as **Standalone**. Explicit sort URLs, saved columns and table/tiles preferences remain in effect. The update marker is informational: its absence does not prove an image is current, and existing update eligibility and confirmations still apply.

Container pages use host ID and container name, so a recreate keeps the same link. Overview keeps network and mount information; Logs combines stored history and an opt-in Live tail; Activity records commands and observed events; Inspect shows redacted Docker configuration; Stats includes CPU, memory and network/block I/O. Terminal still needs its existing feature and agent settings. **Ctrl+K** opens the resource search palette; terminal keystrokes are exempt.

### Image freshness and auto-update switches

The Containers table has a **Freshness** column and an **Auto-update** column. Freshness shows **Out of date**, **Up to date**, **Pinned** or **Unknown**. A failed or missing update check shows **Unknown**, never **Up to date**. Agents that only report the older true/false update field show **Out of date** or **Up to date**. Existing users get both columns once, even with a saved column set; hide them like any other column afterwards.

Each Containers and Stacks row has an auto-update switch. Flipping it saves at once, with no confirmation. Turning it off stores the policy as disabled; it does not delete it. A container switch saves a policy under the container's name exactly as the dispatcher matches it. A stack switch saves a policy for the Compose project with the stack's agreed working directory. Turning a stack on follows the stack page rules: the Compose paths must be agreed and the host must support stack actions. Turning a stack off always works.

A container is controlled by its stack when it carries both Compose labels (project and working directory) and an enabled stack policy exists for that project on the same host. Its switch then shows on, read-only, with a **Controlled by stack** link. Turning the stack off shows the container's own policy again.

A container policy and a stack policy cannot share a name on the same host while the other one is enabled. Such a save answers `409` with code `auto_update_name_clash` and stores nothing. Turn the other policy off first.

**Out of date** and **Auto-update off** filters on both pages combine with search and the other filters, and are kept in the URL. On Stacks, Out of date means at least one member is out of date. Old Containers links with `status=updates` open with **Out of date** applied; combined with a status such as `running`, the filters now narrow (AND) instead of widening.

While the Docker update window in **Settings > Agents** is **disabled**, enabled policies never run, and every switch that is on shows a notice that links there. If you switch many rows on and later set a window, the first window can start many updates; the stagger setting spaces them out.

### Stored and live logs

Enable `container_logs` in each agent's existing `collectors.enabled` list to retain history. Live logs are on demand and do not write to the database or the agent's offline buffer. Every new Logs view starts with Live off. Older agents keep history access and show **Requires agent update** for unsupported live/read capabilities.

```yaml
collectors:
  # Add these names to the existing list; keep the other collectors you use.
  enabled: [system, cpu, memory, disk, network, docker, container_logs, docker_inventory]
  container_logs:
    max_lines: 50
    max_bytes: 16384
```

The stored collector keeps the newest lines within both limits per container per collection. Accepted ranges are 1–1,000 lines and 1,024–262,144 bytes. The server independently caps each container and device, and adds structured gap markers for omitted lines. Stopped containers and raw TTY logs are supported. Lines with clocks outside the retention window are kept at receive time, with the original time shown as a clock-skew marker. Downloads retain these markers.

Known secret patterns are masked at the agent and server. Environment values are hidden unless their key is on the documented safe list. Obfuscated credentials under innocuous names can remain in logs, labels or read-only configuration; review downloads before sharing them.

### Stacks and inventory

**Stacks** lists Compose projects separately per host, including projects taken Down through rIOt. Pull, Update, Redeploy, Restart, Down and Up use the host's agreed Compose paths. Down never removes volumes or images. A changed source path needs acknowledgement; conflicting members refuse actions. Compose and `.env` views are read-only, and values are redacted before leaving the host. File reads are supported on Linux agents only and require the agent service user to have read permission.

Choose **Update** directly on a Stacks row to confirm in place, or on stack details even when no updates are known or services are stopped. The collection dialog freshly checks the selected stack's agreed Compose directory/files and host's stack-action capability before enabling confirmation. Update pulls images and runs Compose up: changed services may be recreated and stopped services may start. Confirmation identifies the host/project/paths and warns when rIOt infrastructure may interrupt the connection. Progress and failures appear in the dialog; closing it after acceptance does not cancel the command, and reopening can adopt active work. Failures require an explicit retry. Collection filters and position stay in place; existing path review and other-action confirmations still apply.

Known positive counts appear as an amber **Update (N)** button and an Updates pill (**1 update** or **N updates**); zero/nonpositive counts keep the neutral control and numeric value, without promising an up-to-date image or changing the confirmation flow.

Compose files beneath container-writable mounts are refused. The host-local `docker.compose_trusted_roots` setting can explicitly trust operator-controlled roots; it is separate from the existing `docker.compose_roots` confinement setting. Run `riot-agent doctor` as the service user to inspect effective settings and affected stacks.

**Images**, **Volumes** and **Networks** require the opt-in `docker_inventory` collector. The server stores only the latest inventory. Cleanup starts with an exact preview, excludes in-use resources, and rechecks identity at execution. Named volumes are unselected by default and require host-name confirmation. Superseded image removal additionally requires the host-local `docker.prune_superseded_images: true` setting. Refresh stale inventory before executing a preview. Cleanup never calls blanket Docker prune APIs or forces image/volume removal.

### Storage controls and upgrades

`RIOT_CONTAINER_LOG_RETENTION_DAYS` defaults to 7 and accepts 1–90. Lowering it irreversibly deletes expired whole days on a subsequent rotation; raising it cannot restore deleted history. Normal retention keeps the configured full days plus the current day. Storage pressure can shorten it, and the Logs view reports the effective value.

`RIOT_CONTAINER_LOG_MAX_GB` defaults to 16 GiB and `RIOT_COMMAND_RESULTS_MAX_GB` to 4 GiB. Per-device allowances refill continuously: logs 512 MiB/day, telemetry 1 GiB/day, device logs 256 MiB/day and command results 64 MiB/day. At low allowance, priority lines are kept first, stdout is sampled, telemetry is downsampled, and command output is truncated. Quota levels persist across restarts. Downsampled telemetry marks omitted sections, which readers treat as unknown or restore from the latest available observation.

The headroom guard uses `RIOT_DB_FS_PROBE_PATH` on the database filesystem, or `RIOT_DB_MAX_GB` for a configured capacity. The shipped Compose files mount an empty **read-only** probe volume and keep the database files confined to the database container. For custom volume layouts, the probe must share the database filesystem. `RIOT_DB_FREE_LOW_GB` and `RIOT_DB_FREE_EMERGENCY_GB` override automatic thresholds. Critical space narrows storage to priority logs and hourly snapshots while preserving today's partitions. Without a valid headroom source, daily quotas and fleet budgets remain active.

Both Compose files rotate each service's Docker logs at 10 MiB × 5 files. Use the same `json-file` options in custom deployments, or configure equivalent daemon-wide rotation. Device registration defaults to 500 total devices and 100 new registrations per rolling day (`RIOT_MAX_DEVICES`, `RIOT_REGISTRATIONS_PER_DAY`); existing devices can still re-register.

Upgrade the server before the agents. Follow the [migration fingerprint and backup procedure](docs/ops/docker-upgrade.md) before deploying this schema. Existing terminal audit rows now have retention: optionally export `terminal_sessions` before the upgrade if you need a longer archive. Treat that export and the database dump as credentials. The [Docker API reference](docs/ops/docker-api.md) covers history, live sessions, stacks and cleanup previews.

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

#### How update detection works

The agent decides per container. It compares the image ID the container is running with the image ID its tag resolves to on the host, and compares the registry digest with the local digest. Hover the badge to see the reason:

| Reason | Badge text | Meaning |
|---|---|---|
| `registry_newer` | Update available: newer image in registry | The registry holds a newer image for the tag than the host does. |
| `pulled_not_recreated` | Update available: newer image pulled, container not recreated | The host already has the newer image, but this container still runs the old one. |

Two other results never show the amber badge:

- **Pinned by digest.** An image reference that contains `@sha256:` or is a bare `sha256:` ID makes no registry call. The container detail shows "Pinned by digest".
- **Update check failed.** The registry lookup failed. The container detail shows "Update check failed". The agent never reports a failed check as up to date.

The agent caches registry lookups per image reference: 30 minutes for a successful check and 5 minutes for a failed one. Two containers on the same tag share one registry lookup but can get different verdicts. After an update, the agent invalidates only the image references involved. The **Check Updates** action still clears the whole cache.

#### Updating a container

From the container detail panel, click **Update** to pull the new image and recreate the container. The dashboard then follows the command by ID (see [Command Lifecycle](#command-lifecycle)).

- **Compose-managed containers** (detected via standard Docker Compose labels): the agent runs `docker compose pull`, then `docker compose up -d`. Ten seconds after `up`, it checks that every service that was running before is running again. Compose updates have no automatic rollback. A failure reports the step that failed.
- **Standalone containers**: the agent keeps the old container until the replacement passes verification. See the sequence below.

This works for any container, including the rIOt server itself — the agent on the server's host can update the server container.

**Standalone update sequence (rename-aside rollback)**

1. **Pull.** The agent reads the full pull stream. A registry error fails the command at `pull` and leaves the running container untouched.
2. **Skip if unchanged.** When the pulled image ID equals the running container's image ID, the agent recreates nothing and reports "already up to date".
3. **Stop and rename aside.** The agent stops the old container and renames it to `<name>.riot-old-<first 8 characters of the command ID>`.
4. **Create the replacement.** It takes the original name, configuration, and network settings. The agent starts it only if the old container was running.
5. **Verify.** If the old container was running, the replacement must be running and not restarting 10 seconds after start.
6. **Clean up.** Only after verification passes, the agent removes the old container.

If create, start, or verification fails, the agent removes the replacement, gives the old container its original name back, and returns it to its previous run state. The command ends `failed` with "rolled back to previous container".

If the rollback itself fails, the command ends `failed` at step `rollback` and names the set-aside container. The agent never removes the old container in that case. A leftover set-aside container blocks the next update of that container: the new command fails at `precheck` and names the leftover. Inspect it, then rename it back or remove it by hand.

**Network dependents**

A dependent is a container that shares another container's network stack. Its network mode references the parent by name, by full container ID, or by Compose `service:` name. Before the agent recreates a parent, it stops the running dependents of that parent only. When the update ends, the agent starts every dependent that was running beforehand and re-attaches it to the current parent. This happens on every outcome: success, failure, step timeout, or a recovered panic. A dependent that was already stopped stays stopped.

If any dependent cannot be restored, the command ends `failed` at step `restart_dependents`. The message lists the affected dependents and states the outcome of the main update. Restoring dependents has its own 2-minute budget, separate from the pull and `up` limits.

**Step limits**

| Step | Limit |
|---|---|
| Compose `pull` | 15 minutes |
| Compose `up` | 10 minutes |
| Standalone `pull` | 15 minutes |

When a Compose step reaches its limit, the agent sends SIGTERM to the `docker compose` process and waits 60 seconds. It sends SIGKILL only if the process is still running. A standalone pull is cancelled at its limit. In both cases the command ends `failed` with the step name and "timed out after <limit>". Lifecycle events such as `update_failed` and the final result use a fresh 10-second budget, so they still reach the server after a timeout.

**If the agent stops mid-update**

On startup, the agent reconciles any Docker update that was running when it stopped:

- It renames a set-aside container back when no container holds the original name, and restarts it if it was running.
- It leaves both containers untouched when the original name is already in use, and says so in the result.
- It restarts dependents that the update had stopped.
- It deletes a leftover sanitized compose file, but only from a directory that passes the [compose directory trust](#compose-directory-trust) check.
- It never removes a container.

The command then ends `failed` with "agent restarted during execution; outcome unknown", followed by a `reconciled:` summary. Agent self-update, reboot, and shutdown commands are excluded and keep their own completion reporting.

On SIGTERM, the agent waits up to 20 seconds for running commands to finish. A command that arrives during shutdown fails at `precheck` with "agent shutting down". The agent also refuses `agent_update` and `agent_uninstall` while a Docker update runs, and refuses Docker updates while an agent update or uninstall runs.

To disable update checking, set `docker.check_updates: false` in the agent config.

#### Compose directory trust

The agent runs `docker compose` with Docker rights, which are root-equivalent on the host. A container label can name any directory, and an image can set any label. The agent therefore never takes a compose directory from a label alone.

Before every compose invocation, and before it writes or removes its sanitized compose copy, the agent validates the project directory and every file compose will read. This covers stack updates, updates of a single Compose service, the recreate of network dependents, and the cleanup of leftover sanitized files after a restart.

A refused update fails at `precheck` with `compose directory refused (<rule>)`, which names the path and the rule. Nothing is stopped or recreated. A refusal counts as a failed run for the auto-update backoff, so a persistent refusal is retried only on the backoff schedule. The refusal text is in the command result in the dashboard and in the agent log.

**After upgrading an agent (Linux)**

1. Upgrade the agent through auto-update or by re-running the installer.
2. Run the doctor as the agent's service user: `sudo -u riot riot-agent doctor`. The service user is the `User=` line of `/etc/systemd/system/riot-agent.service`; the installer uses `riot`.
3. Read the compose lines under **Docker**: `Compose roots`, then one line per compose project. A pass reads `project "<name>": "<dir>" is trusted`. A failure shows the rule.
4. Fix every failing project with the table below, then run the doctor again.

Run the doctor as the service user, not as root. Root can read files and mount sources that the agent user cannot, so a pass as root can hide a refusal. The doctor prints a warning when it runs as a different user than the service. The doctor also lists containers it ignores as compose sources, and mount sources it compared through their readable prefix (see "Container-writable locations").

**What the agent requires**

- **Where the directory comes from.** A stack update names the compose project (`compose_project`). The agent looks the project up among the Docker containers and needs exactly one working directory for it. A directory sent by the server is only a cross-check. Containers whose image defines the `com.docker.compose.project.working_dir` label are ignored as a source of directories, because an image can forge it. Images built by compose do not carry that label.
- **Who may write.** The resolved directory and every parent up to `/` must be owned by root or by an account that already has Docker rights. Those accounts are the agent user, the owner of the Docker socket, and the members of the socket's group when the socket grants that group read and write. No other account may write to the path:
  - It must not be world-writable. A sticky parent such as `/tmp` is the one exception, and never for the stack directory itself.
  - It must not be group-writable by a group that has a member without Docker rights. A group whose only members are trusted accounts (a user-private group) is accepted.
  - A group-writable path with an extended ACL is refused.
  - The stack directory is symlink-resolved first, so a symlinked stack directory is accepted. Every component of the resolved path must then be a real directory.
- **Which files.** At least one of `compose.yaml`, `compose.yml`, `docker-compose.yml` or `docker-compose.yaml` must be in the directory itself, because compose would otherwise walk up to a parent. Compose, override and `.env` files must be regular files that follow the ownership rules above, or symlinks that stay inside the directory. The agent passes the validated files to compose as explicit `-f` arguments, in compose's own order, so compose loads nothing else.
- **`COMPOSE_FILE` in `.env`.** If `.env` sets `COMPOSE_FILE`, the agent validates each listed file and uses those files instead. It reads `.env` only for `COMPOSE_FILE` and `COMPOSE_PATH_SEPARATOR` (exact names), skips a leading UTF-8 byte order mark, and never echoes `.env` content. It refuses a form it cannot read with certainty: interpolation, `export`, backslashes, inline comments, duplicates and empty entries.
- **Read access to `.env`.** The agent user must be able to read `.env`, as compose, which runs as the same user, must too.
- **Container-writable locations.** The directory and its compose files must not be at or under the Docker data root. They must not be at or under a read-write bind mount source or volume of any container, running or stopped, because a compromised image could rewrite them. Containers that mount the Docker socket are exempt. A stack directory that only contains a bind source (`./config:/config`) is accepted. A stack that mounts its own directory read-write (`.:/app`) is refused.
- **Unreadable mount sources.** A read-write mount source can sit below a directory the agent user may not enter, such as a home directory with mode `0750`. The agent compares such a source through its readable part only when all of these hold. Otherwise every compose update on the host is refused with `container_writable`, naming the container and the mount.
  - The last readable directory and all its parents pass the ownership, mode and ACL rules above, so no ordinary user can plant a symlink below it.
  - No other read-write mount of any container is at or above that directory.
  - The failure is a permission error, not a loop or a missing component.

  A source below a directory owned by an ordinary user is therefore refused.
- **One directory, one project.** The directory must belong to exactly one project, the one passed to compose as `--project-name`.
- **Pinned environment.** The compose child runs with `DOCKER_HOST` set to the configured socket. It inherits no `COMPOSE_*` or `DOCKER_CONTEXT` variable from the agent's environment. A `COMPOSE_PROFILES` or `COMPOSE_FILE` exported to the agent service therefore no longer reaches compose; put such settings in the project's `.env`.

**Refusal rules**

Each rule below is a possible `<rule>` in `compose directory refused (<rule>)`. Replace `/opt/stacks/app` with the path in the refusal message.

| Rule | Meaning | How to fix |
|---|---|---|
| `windows_requires_roots` | A Windows agent has no `docker.compose_roots`. Windows ownership cannot be verified. | Set `docker.compose_roots` to the directories that hold your stacks. Restart the agent. |
| `compose_roots_invalid` | An entry in `docker.compose_roots` is empty or not an absolute path. Every compose update is refused. | Correct the entry in `agent.yaml`. Restart the agent. |
| `outside_compose_roots` | The directory, or a `COMPOSE_FILE` entry, is not inside any root. A root that does not exist matches nothing. | Add the stack's parent directory to `docker.compose_roots`, or move the stack. Restart the agent. |
| `not_absolute` | The working directory is empty or not an absolute path. | Recreate the stack with `docker compose up -d` so its label holds an absolute path. |
| `invalid_project` | The project name has characters outside `a-z`, `0-9`, `_` and `-`, or starts with `_` or `-`. | Rename the project (`name:` in the compose file, or `-p`) and recreate the stack. |
| `project_not_found` | No container of a trusted project has this directory as its working directory. | Run `docker compose up -d` in the directory so its containers carry the label. If the doctor lists the containers as ignored, see `label_provenance`. |
| `ambiguous_project` | Two projects claim the directory, or one project has containers in two different real directories. | Recreate the stack from a single directory so every container carries the same project and directory. |
| `project_mismatch` | The directory belongs to a different project than the one named. | Recreate the stack so the project name and directory agree. |
| `work_dir_mismatch` | The server named a directory, but the project runs in another one. Typical after a stack moves. | Wait for the next telemetry report and retry. If it persists, recreate the stack from its current directory. |
| `label_provenance` | The container's image defines the working-directory label, so the label cannot be trusted. | Rebuild or replace the image without that label. The doctor names the container. |
| `daemon_unavailable` | The agent cannot read Docker's state (info, containers or volumes). | Check that Docker is running and that the agent user can read the socket (`docker.socket_path`). The agent log has the cause. |
| `docker_data_root` | The directory or a compose file is under the Docker data root (`/var/lib/docker` by default). | Move the stack outside the data root. |
| `container_writable` | A container can write to the directory or a compose file through a read-write mount. The message names the container, source and destination. A "cannot resolve" message means a mount source sits below a directory the agent user cannot enter. | Mount it read-only (`:ro`), mount a narrower subdirectory that holds no compose files, or move the stack. For "cannot resolve", let the agent user enter the directories above the source, for example by adding it to their group, or satisfy the conditions in "Unreadable mount sources". |
| `owner` | A path is owned by an account without Docker rights. | `sudo chown root:root /opt/stacks/app`, and the same for the file or parent the message names. |
| `group_writable` | A path is writable by a group that has a member without Docker rights. | `sudo chmod g-w /opt/stacks/app`, or limit the group to accounts with Docker rights. |
| `acl` | A group-writable path has an extended ACL, or its ACL cannot be read. | Remove the group write bit (`sudo chmod g-w <path>`), or remove the ACL (`sudo setfacl -b <path>`). |
| `world_writable` | A path is writable by everyone. | `sudo chmod o-w <path>`. |
| `resolve` | The directory, a symlink or a `COMPOSE_FILE` entry cannot be resolved: it is missing, a symlink is broken, or the agent user cannot enter a parent. | Create or repair the path. Check that the agent user can enter every parent directory. |
| `stat` | The agent cannot read the metadata of a path component. | Check that the path exists and that the agent user can enter its parent directories. |
| `not_directory` | A component of the resolved path is a symlink or not a directory. This means the path changed during the check. | Retry the update. If it repeats, find what is changing the path. |
| `not_regular` | A compose, override, `.env` or `COMPOSE_FILE` entry is not a regular file (it is a directory, device, socket or a symlink to one). | Replace it with a regular file. |
| `symlink_escape` | A compose or `.env` symlink points outside the project directory. | Replace the link with the real file, or point it at a file inside the directory. |
| `no_compose_file` | The directory has none of the four default compose file names, and `.env` sets no `COMPOSE_FILE`. | Put a compose file in the directory, or set `COMPOSE_FILE` in its `.env`. |
| `env_unreadable` | The agent user cannot read `.env`. | Make `.env` readable by the agent user's group. If the agent user is in group `docker`: `sudo chgrp docker /opt/stacks/app/.env && sudo chmod 0640 /opt/stacks/app/.env`. |
| `env_unparsed` | `.env` is over 1 MiB, or sets `COMPOSE_FILE` or `COMPOSE_PATH_SEPARATOR` in a form the agent cannot interpret with certainty. | Use one plain line, such as `COMPOSE_FILE=compose.yaml:extra.yaml`, with no `$`, `export`, backslash, inline comment, space around `=` or empty entry. |
| `dir_changed` | The directory or its compose files changed between the checks of one update. The agent re-checks before it writes a sanitized copy and before it stops anything. | Retry the update. If it repeats, find what is rewriting the files. Images may already be pulled; no container was stopped. |

**`docker.compose_roots`**

Set `docker.compose_roots` to narrow where compose may run:

```yaml
docker:
  compose_roots:
    - /opt/stacks
```

When set, the directory and any `COMPOSE_FILE` entry must be inside one of the listed directories. Roots are compared after symlinks are resolved. When unset, roots are not enforced on Linux and the ownership rules above still apply. The agent reads the setting at start-up, so restart the agent after changing it.

**Windows.** The agent cannot verify ownership or permissions on Windows. Compose updates are refused with `windows_requires_roots` until `docker.compose_roots` is set. Roots compare case-insensitively. On Windows, the roots, the label-provenance rule and the container-writable rule are the protection.

**Multiple working directories.** After compose recreates only some services of a stack in a symlinked directory, the stack's containers carry more than one working-directory label. The server never skips such a stack. It dispatches the update by project name alone. The agent resolves the labels to real directories. It updates the stack when they are one directory, and refuses with `ambiguous_project` when they differ.

**Compatibility**

| Pairing | Result |
|---|---|
| New agent, old server | Works. An old server sends only `compose_work_dir`. The agent finds the project from the Docker containers and applies every rule. |
| Old agent, new server | Works but stays unhardened until the agent is upgraded. The old agent ignores `compose_project` and runs compose in the directory from the label. One exception: when a project's containers report more than one working directory, the server sends no directory. An old agent fails that command at `precheck` with "container, container_id or compose_work_dir is required". Upgrade the agent. |
| Dashboard manual updates | Work with both. |

Hosts that updated stacks before the upgrade can see new refusals. The common causes are a stack directory owned by an ordinary user (`owner`), a stack under a path that another container mounts read-write (`container_writable`), and a `.env` that the agent user cannot read (`env_unreadable`).

**Not covered (residual risks)**

- Files that trusted compose content references (`include`, `extends`, `env_file`, build contexts) are trusted transitively, as they are when an administrator runs `docker compose up` by hand.
- The agent reads account membership from `/etc/passwd` and `/etc/group`. User-private-group trust can therefore over-trust in three cases it cannot see: `Group=` or `SupplementaryGroups=` on systemd units that run services; group passwords in `gshadow`; and writers that ran in a container that has since been removed.
- With LDAP, SSSD or similar sources, systemd userdb records or systemd-homed, the agent turns member enumeration off. It then trusts only root, the agent user and the socket owner.
- FUSE mounts with `user_allow_other` can report an owner of the mounter's choosing.
- An account that already has Docker rights is root-equivalent and is not defended against.

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

When Docker is available, the agent watches the Docker event stream and forwards container lifecycle events (start, stop, die, OOM, pause, unpause, create, destroy) to the server in real-time. These appear in the dashboard event feed. Container deaths and OOM kills trigger alerts, except a death that an action you started is expected to cause (see [Expected Activity](#expected-activity)). An OOM kill always alerts.

---

## Command Lifecycle

Every command that the dashboard sends to an agent has an ID and moves through a fixed set of states. The server enforces which moves are allowed. A late or duplicate report from an agent cannot change a finished command. The dashboard follows each command by ID, so it shows the real state and the current step instead of a guess from the HTTP round trip. Twelve of the actions also open an [expected-activity window](#expected-activity) that stops their disruption from alerting.

### States and what the dashboard shows

| State | Meaning | Label shown for an update |
|---|---|---|
| `queued` | The server created the command and has not delivered it. | Queued |
| `dispatched` | The server claimed the command and sent it to the agent. | Sent to agent |
| `acknowledged` | The agent received the command and has not started it. | Starting |
| `running` | The agent is executing a step. | Updating… *step* |
| `verifying` | The agent is checking that the update worked. | Verifying |
| `succeeded` | The agent reported success. | Updated (for 5 seconds, then the normal view) |
| `failed` | The agent reported failure. | Failed at *step*: *message* (until dismissed) |
| `timed_out` | The agent sent no result before the execution deadline. The command may have run. | Timed out: outcome unknown |
| `expired` | The command waited in `queued` past its TTL. The server never delivered it. | Expired: never delivered |
| `cancelled` | The operator cancelled the command while it was `queued`. | Cancelled |

Notes on reading the states:

- `expired` is safe to retry, because the command never ran. `timed_out` is not, because the command was delivered and the outcome is unknown. A command can also time out when the agent never received it, for example when a heartbeat response is lost on the network.
- `succeeded`, `failed`, `cancelled`, and `expired` are final. A `timed_out` command accepts one late final result from the agent and then becomes `succeeded` or `failed`.
- Non-update commands show "Running… *step*" while running and "Done" when they succeed.
- During `running`, the step appears as one of: Checking, Resolving container, Pulling image, Stopping dependents, Recreating, Verifying, Restarting dependents, Rolling back, Cleaning up. An unknown or empty step shows "Working".

Where you see this:

- **Container detail and Compose stack section.** The update control stays disabled while the command is in flight. Reloading the page picks up the in-flight command again. If another tab already started the same update, the dashboard attaches to that command instead of starting a second one.
- **Container tiles.** While an update is in flight, stop and die events do not flip the tile to `exited`. It shows "Updating…" until the command ends.
- **Activity log.** Rows written before this release show the same labels as new rows.
- **Live updates.** The server pushes each accepted state change over the dashboard WebSocket. If no push arrives for 5 seconds, the dashboard re-fetches the command and keeps re-fetching every 5 seconds until the command ends. No fixed timer ends tracking.

### Queued TTLs and execution deadlines

Every command gets an expiry when it is created. A command that is still `queued` at that time becomes `expired` and is never delivered, even if the agent reconnects later. The server also checks the expiry at the moment it claims a command for delivery, so a stale reboot cannot slip through while the sweeper is behind.

Once a command is `dispatched`, the execution deadline runs from the dispatch time. Progress reports do not move the deadline.

| Command class | Actions | Queued TTL | Execution deadline |
|---|---|---|---|
| Power: reboot | `reboot` | 15 minutes | 20 minutes (a reboot can wait up to 10 minutes for the dpkg lock, see [Reboots and the dpkg lock](#reboots-and-the-dpkg-lock)) |
| Power: shutdown | `shutdown` | 15 minutes | 20 minutes (a shutdown can wait up to 10 minutes for the dpkg lock, see [Reboots and the dpkg lock](#reboots-and-the-dpkg-lock)) |
| OS patch | `os_update`, `enable_auto_updates` | 30 minutes | 40 minutes |
| Docker update | `docker_update` | 60 minutes | 35 minutes |
| Docker bulk update | `docker_bulk_update` | 60 minutes | min(35 minutes × number of targets + 5 minutes, 6 hours) |
| Agent update | `agent_update` | 60 minutes | 15 minutes |
| Other | Every other action, such as `docker_start`, `docker_stop`, `docker_restart`, and `docker_check_updates` | 30 minutes | 15 minutes |

For auto-dispatched commands under a `window` maintenance window, the expiry is the earlier of the TTL and the end of the current window. A command that the automation queues in a window therefore never runs after the window closes.

A background sweeper applies both rules. It runs once at server start and then every 60 seconds.

### One in-flight command per target

Each device and target can have at most one non-terminal command.

| Target | Used by |
|---|---|
| Host | `reboot`, `shutdown`, `os_update`, `enable_auto_updates`, `agent_update` (one shared device-level target) |
| Compose project | A Compose container or a whole stack, identified by the project working directory |
| Container name | A standalone container |

Docker targets and the host target do not conflict. A reboot can be queued while a Docker update runs. Two single-service updates in the same Compose project do conflict.

When a conflict occurs, the API answers `409` with the existing command's ID (see [Command endpoints](#command-endpoints)). A bulk update skips each conflicting target, reports it, and still creates a command for the targets that do not conflict. Auto-update and auto-patch skip a conflicting dispatch and write a log entry.

### Cancelling a queued command

While a command is `queued`, the dashboard shows a **Cancel** button. A cancelled command is never delivered. Cancel and delivery are mutually exclusive: exactly one of them wins a race. After dispatch, a cancel request returns `409` with "command cannot be cancelled after it has been dispatched". The dashboard offers no cancel after dispatch because stopping a running Docker update halfway is not safe.

If a WebSocket send fails in a way that leaves delivery uncertain, the server re-queues the command and records that an earlier send may have reached the agent. The history message for a later `expired` or `cancelled` result says so.

### Auto-update behavior

Per-container and per-stack auto-update policies follow these rules:

- **The cooldown starts on success.** The server sets a policy's last-triggered time only when a command that the policy created reaches `succeeded`. The time is the moment of that transition. The cooldown length is the Docker update cooldown in **Settings > Agents**.
- **Failures back off.** When a policy's command ends `failed`, `timed_out`, `expired`, or `cancelled`, the server counts a consecutive failure *n*. The policy then waits min(15 minutes × 2^(*n*−1), 6 hours) before it dispatches again.
- **Success resets the count to 0.**
- **A policy never dispatches while its own command is in flight.** A conflict skip does not count toward the backoff.
- **Backoff retries obey the maintenance window and stagger rules.** Backoff never lets a dispatch happen outside a configured window.

| Consecutive failures | Wait before the next dispatch |
|---|---|
| 1 | 15 minutes |
| 2 | 30 minutes |
| 3 | 1 hour |
| 4 | 2 hours |
| 5 | 4 hours |
| 6 or more | 6 hours |

A container-level policy sends both the container name and the container's current ID. New agents resolve the target by name, because container IDs change on every recreate, and fall back to the ID only when no name is given. A leading `/` does not affect name matching. Legacy agents resolve by ID only.

### Reliable result delivery

New agents report each command in stages: an acknowledgement before execution, a progress report at each step change, and a final result. Only the final result is durable. Acknowledgements and progress reports are best effort.

1. The agent writes the final result to its outbox on disk before the first delivery attempt.
2. When the WebSocket is connected, the agent sends the result over it and waits 10 seconds for the server's `command_receipt`.
3. Without a receipt, or when the WebSocket is down, the agent posts the result to `POST /api/v1/devices/:id/commands/:cid/report` with its device key.
4. The agent removes the entry only after the server confirms it: a 2xx response that echoes the command ID, a `409 already_terminal` or `409 invalid_transition` response, a `404 command_not_found` response, or a receipt.
5. On any other failure the agent retries with backoff that starts at 5 seconds, doubles each time, and stops growing at 5 minutes.

The outbox holds at most 200 entries and 1 MiB on disk. It drops entries older than 24 hours. When it is full, it drops the oldest entry first and writes a warning log, `outbox entry dropped`. Entries survive an agent restart.

The agent also remembers the IDs of commands it has accepted (up to 1000 IDs for 48 hours) and never runs the same ID twice. If the agent cannot save an ID to disk, it refuses `reboot`, `shutdown`, `os_update`, and `agent_update` instead of risking a second run. Other actions run in degraded mode and log an error.

**Agent state files.** The agent stores these files in its data directory: `/var/lib/riot/` on Linux and macOS, `%PROGRAMDATA%\riot\` on Windows. The agent writes them with mode `0600` and creates the directory with mode `0700`. They need no new privileges and no sudoers rule.

| File | Contents | Bounds |
|---|---|---|
| `command-outbox.json` | Final results not yet confirmed by the server | 200 entries, 1 MiB, 24 hours |
| `seen-commands.json` | IDs of commands the agent already accepted | 1000 entries, 48 hours |
| `inflight-commands.json` | One marker per running command (action, target, last step, and Docker recovery details) | Running commands, plus records whose recovery is still pending |
| `patch-reboot-intents.json` | The IDs of patch commands that already claimed their one reboot, each with its claim time | See [Reboot-intent file](#reboot-intent-file): 30-day age limit, 256 entries, 64 KiB read limit |
| `patch-reboot-intents.json.corrupt` | One copy of an unreadable `patch-reboot-intents.json`, kept for inspection | One file, at most 64 KiB, overwritten by the next copy |

### Upgrade notes

Read this section before you deploy the release that adds the command lifecycle.

**Rollout order**

1. Upgrade the server first. Migration `000023` runs on startup.
2. Upgrade agents afterwards, with the normal agent update. Agents do not need `install.sh` to be re-run, and no collector whitelist change is needed. The release adds no collector, no sudoers rule, and no new agent privilege.

**Version skew**

| Combination | Result |
|---|---|
| Old agent, new server | Works. The server sends both the container name and the container ID. The old agent never acknowledges, so the command stays `dispatched` until its result arrives. Old agents keep their own bugs until upgraded: results travel over the WebSocket only, dependents are not restored, and standalone updates have no rollback. |
| New agent, old server | Works in WebSocket-only legacy mode. The old server ignores acknowledgements and progress reports and has no report endpoint. The agent sees a `404` or `405` without an error code, delivers results over the WebSocket, and probes the HTTP endpoint again no more than once per hour. |
| New agent, new server | Full lifecycle. |

**Migration `000023`**

- **Status mapping.** The migration rewrites stored statuses in place: `pending` becomes `queued`, `sent` becomes `dispatched`, `success` becomes `succeeded`, and `error` becomes `failed`. The server also maps legacy values when it reads, so a row that an older server writes during a rollback window still reads correctly.
- **Historical rows are relabelled silently.** The migration marks every existing row as written before the lifecycle and backfills its expiry and deadline. The first sweep then moves old `dispatched` rows (formerly `sent`, with no result ever received) to `timed_out` and overdue queued rows to `expired`. The sweep writes one `legacy commands relabelled` log line with the counts. It sends no dashboard broadcast, creates no event, and sends no notification.
- **Auto-update reset.** The migration clears `last_triggered_at` on every container-level auto-update policy. Stack policies keep their value. Container-level policies never worked before this release: the server sent a container name and the agent required an ID, so every run failed and started the cooldown. They will fire for the first time in the next maintenance window. Expect a burst of container recreations on the first telemetry that arrives inside the next Docker maintenance window, limited by the stagger setting. Disable or delete the policies you do not want to run before you upgrade.
- **Down migration.** The down migration maps canonical values back to legacy ones (`cancelled` and `expired` become `error`, never `queued`). It first fails every queued `reboot`, `shutdown`, and `os_update` with `include_reboot_class`, regardless of expiry, so a downgrade cannot deliver a stale power action. It cannot restore the cleared `last_triggered_at` values.
- **Migration number collisions.** The migration uses the number `000023`. If another unmerged branch also adds a migration with that number, renumber whichever branch merges second above the current maximum before you deploy. Check the `schema_migrations` table of the target database first.

**Changes that affect API and WebSocket consumers**

- The `status` field of a command now holds one of the ten states above. Code that matches `pending`, `sent`, `success`, or `error` must change.
- Dashboard WebSocket clients receive `command_update` instead of `command_result`.
- Creating a command can now return `409`. The history endpoint returns `400` for an unknown `status` filter value.

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

   Only when the policy is `gated` **and** the dispatch occurs inside the OS-patch window does the orchestrator include reboot-class packages in the run. A manually dispatched `os_update` includes them only when the operator ticks **Reboot automatically if the patch requires it** and the server accepts the request (see [Manual patch and reboot](#manual-patch-and-reboot)). The server removes any `include_reboot_class` a client sends and sets it itself, so the in-window automated run and a ticked manual run are the only two release paths.

### What happens during an in-window run

When both opt-ins are active and the OS-patch window opens with reboot-class updates pending:

1. The agent releases **only its own** holds (operator-created `apt-mark hold`s are never touched), recording a crash-recovery marker first.
2. The package manager applies updates.
3. Holds are re-applied against the updated package set on **every** exit path — success, upgrade failure, or crash.
4. If a reboot-class package's version actually changed **and** `allow_reboot` is true, the agent reports the result (stating which reboot-class packages were applied and that a reboot was initiated) and then reboots once the dpkg lock is free. It waits up to 10 minutes for the lock; if the lock stays held, the reboot is skipped and a warning event says why (see [Reboots and the dpkg lock](#reboots-and-the-dpkg-lock)). If `allow_reboot` is false, it raises `reboot_required` instead. No reboot occurs if only standard packages were applied.

Outside the window, holds keep GPU driver and kernel packages pinned, so a casual `apt upgrade` or `unattended-upgrades` skips them.

### Reboot-required detection and alert

Independently of holds, the agent reports whether the host needs a reboot now to activate installed updates. On apt, that is true when `/var/run/reboot-required` (+ `/var/run/reboot-required.pkgs`) exists, or when an installed kernel is newer than the running one. On dnf, it is true when `dnf needs-restarting -r` says so. [Manual patch and reboot](#manual-patch-and-reboot) explains the kernel comparison. A pending update that is not installed yet never counts. A once-per-transition **Reboot Required** event is emitted (warning severity, 24-hour cooldown), and a matching alert rule/template ships enabled so it can fan out to your notification channels. Detection failures degrade quietly to "not required". Populating this state also fixes the security score's kernel-update check, which previously always passed silently because the underlying field was never set.

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

## Patch badge

Every device on the Devices page shows one badge in the **Updates** column. The same badge appears next to the **Auto-patch** switch on the device page and in the **Pending OS Patches** panel (open it with **Review Patches**). The server works the badge out and writes its text, so all three places always agree. The badge is advisory only: it never starts, stops or changes a patch or a reboot.

No setup is needed beyond the maintenance window you manage in **Settings > Agents**. There is no new environment variable and no database migration.

### Badge values

| Badge | Meaning |
|---|---|
| **Auto** | Nothing needs you. No reboot is required now. When a pending kernel/GPU update will need a reboot and nothing will do it automatically, the Reboot line says so, and the badge stays **Auto** because no reboot is required yet. |
| **Auto (reboot on YYYY-MM-DD)** | A reboot is required now or will be needed after a pending kernel/GPU update installs, and it will happen at the next OS-patch window start. The date is the date in the server's display zone (see [Time Display](#time-display)). This is an estimate and can move. |
| **Auto (reboot date Unknown)** | A reboot is required now or will be needed after a pending kernel/GPU update installs. It will be automatic, but the time cannot be predicted (for example OS-patch mode is `anytime`, or the agent does not report `allow_reboot`). |
| **Manual** | Nothing needs you, and nothing patches this host automatically. For example: auto-patch is off, automatic patching is disabled server-wide, or the agent does not allow patching, and no update is waiting. The badge is grey, not red. It does not count toward "N hosts need you". |
| **Blocked** | rIOt did not start an update that is waiting, and the reason is not something you can fix by an action in rIOt. Amber, not red. It is not a failure of the patch run, and it does not count toward "N hosts need you". Hover the badge to read the reason ([details](#refused-updates-blocked-and-needs-you)). |
| **Needs you** | At least one item needs a person. Hover or focus the badge to read each item and what to do. |

Hover the badge, or move to it with the keyboard, to open the explanation. Press Escape to close it. It has four lines: **Automatic patching**, **Reboot**, **Needs you** and **As of** (when the update data was reported, in the server's display zone, 12-hour form with a zone label, for example `2026-10-27 1:05 PM EDT (3 min ago)`). The server writes these times, so the browser's time-zone choice does not change them (see [Time Display](#time-display)). A note about unattended-upgrades or about checks that need a newer agent can follow. When the badge has information items, an **Information** block lists them below the four lines, with the text "Nothing here needs you."

The **N hosts need you** button above the device table shows only the devices whose badge is **Needs you**. It is a toggle. **Manual**, **Auto** and **Blocked** hosts are never counted. When at least one host is **Blocked**, an **N blocked** button beside it filters to those hosts. The two filters exclude each other.

### What makes a device "Needs you"

**Needs you** appears only when work is waiting and nothing will do it. The badge uses these terms:

- **Waiting work** is any of: apt updates pending (and the package lists are not stale), an extra-updater (DietPi) update pending, or a reboot required now.
- **Automatic patching set up** means auto-patch is on for the device, patching is usable server-wide, the agent allows patching and the package manager is apt or dnf.
- An **action item** is a reason listed under **Needs you**. An **information item** is a reason listed under **Information**. Information never changes the badge to **Needs you**.

The badge says **Needs you** if, and only if, at least one action item exists. Each reason is an action item only under the condition shown, and information otherwise:

| Reason (code) | Action item when | Otherwise |
|---|---|---|
| Auto-patch is off for the device (`auto_patch_off`) | Updates are waiting that nothing will apply | Information |
| Automatic patching is disabled server-wide, or the OS-patch window has no upcoming start (`patching_disabled_server`) | Same as above | Information |
| The agent does not allow patching, `commands.allow_patching` is false (`agent_patching_not_allowed`) | Same as above | Information |
| rIOt cannot patch the host: no patch data, or a package manager other than apt or dnf (`cannot_patch_host`) | Apt updates are pending on a host that reports them | Information, including when no patch data was reported |
| Packages are held or excluded by someone other than rIOt (`held_packages`, up to 10 names, then "and N more") | A held package has a newer version available | Information |
| Kernel and GPU packages are held by rIOt but the `reboot_class` policy is not `gated` (`reboot_class_held_no_release`) | A kernel or GPU update is pending | Information |
| Hold enforcement is not working on the host (`hold_enforcement_broken`) | Never | Always information |
| A new Ubuntu release is available. rIOt never starts a release upgrade (`manual_approval_update`) | Always | Not applicable |
| The latest finished patch run failed or timed out (`last_run_failed`). Expired and cancelled commands are ignored | Work is waiting | Information. The text adds "Nothing is pending now." and keeps the link to the failed command |
| A reboot is required now (`reboot_required` is true) and no automatic reboot is set up (`manual_reboot_needed`). The explanation lists the causes. When the agent has `commands.allow_reboot: false`, the action says to reboot outside rIOt. A pending kernel/GPU update that is not installed yet never raises this item | Always, when it applies | Not applicable |
| A DietPi update is pending and DietPi is not enabled (`extra_update_not_applied`). The text names the update and why it is not applied, and the action says what to do | Always, when it applies | Not applicable |
| A DietPi update is pending and will be applied (`extra_update_pending`) | Never | Always information. The text says when it will be applied |
| DietPi's update state is unknown (`extra_update_unknown`) | Never | Always information. The text names the reason. rIOt never says "up to date" for an unknown state |
| The latest patch run refused a pending DietPi update for a reason you can fix (`extra_refused`): the path check found something not root-only, the update source is not the default, or the patch window is too short | Always, when the update is still pending and enabled | Information, once the update is no longer pending: "The last patch run skipped DietPi: ..." |
| The latest patch run refused a pending DietPi update for a reason you cannot act on (`extra_blocked`): rIOt could not isolate the step, or a temporary condition | Never. It makes the badge **Blocked** and is listed under **Blocked** | Information, once the update is no longer pending |

Each extra-updater code appears once per updater. The Automatic patching, Reboot and As of lines are unchanged.

### Refused updates: Blocked and Needs you

A patch run can refuse to start a DietPi update and still succeed. The apt part ran, DietPi did not start, and nothing on the host changed. The command stays `success`, and the first line of its result names the refusal, for example `apt: succeeded; dietpi: skipped (unsafe_dietpi_path)`. The agent also sends one warning event for the refusal. The badge then tells you whether you can do something about it.

| Class | Reasons | Badge |
|---|---|---|
| Blocked: rIOt's own isolation check, or a temporary condition | `tmp_not_private`, `mount_propagation_unverified`, `rundir_not_private`, `dietpi_busy`, `min_interval`, `dpkg_locked` | **Blocked** |
| Blocked: a stock-host false positive from a worker older than the fix | `unsafe_dietpi_path` whose note starts with `/run/dietpi ` | **Blocked**. The text says to re-run `install.sh` as root on the host |
| Needs you: a path under `/boot/dietpi` or `/var/tmp/dietpi` is not root-only | `unsafe_dietpi_path` with any other path | **Needs you**, with the action "Investigate the named path on this host (owner and permissions); rIOt will not run DietPi until it is root-only." This is a possible tampering signal |
| Needs you: only you can change it | `non_default_update_source`, `insufficient_time` | **Needs you**, with the action for that reason |

A **Blocked** text for an isolation reason says that the host's mount layout prevented rIOt from isolating the step. It never promises that the next run fixes it. The temporary reasons say that rIOt retries on the next patch run. The reasons and their fixes are in the [refusal table](docs/ops/os-maintenance.md#refusal-reasons).

If a host has both a **Needs you** item and a Blocked item, the badge is **Needs you** and the Blocked reason is still listed. A **Blocked** badge stays until the next OS patch run finishes. Re-running `install.sh` does not clear it by itself.

A host with nothing pending, no reboot required and auto-patch off therefore reads **Manual**. For example, vermont used to read **Needs you** for "auto-patch off". It now reads **Manual**, with `auto_patch_off` under **Information**, and it is not counted in "N hosts need you". A host with updates pending and auto-patch off still reads **Needs you**. A deliberate hold on a package that has an update stays **Needs you** until the cause is removed.

### When a reboot is automatic

An automatic reboot after a patch run happens only when all of these hold:

1. Auto-patch is on for the device and the OS-patch window is usable, so a patch run is dispatched.
2. The server's reboot-class policy is `gated` and a reboot-class update is pending.
3. The agent has `commands.allow_reboot: true`.
4. The agent has `commands.hold_reboot_class: true` and hold enforcement works. Without it the agent never applies reboot-class packages, so it never reboots automatically.

### Agent facts are self-reported

The agent reports `allow_reboot`, `allow_patching`, `hold_reboot_class`, the packages held or excluded outside rIOt (apt holds, dnf `excludepkgs`/`exclude` and the versionlock list; at most 50 names), and whether an Ubuntu release upgrade is available. These come from the agent and are shown for information only. The server re-checks their format and length, and no dispatch decision reads them. Holds placed through apt preferences pins or dnf excludes in unusual files are not detected.

Ubuntu release check: the agent reads `/var/lib/ubuntu-release-upgrader/release-upgrade-available` first. If it is missing, it runs the system's `do-release-upgrade -c` as the agent user, with a 20 second limit, at most once every 12 hours. That check makes **one outbound request per 12 hours** to Ubuntu's release servers. Other distributions report it as unavailable. No sudo and no new privilege is used.

Agents older than the release that added these facts do not send them. Until such an agent is updated, its badge shows a "Some checks need a newer agent" note, and the checks that need those facts are skipped.

### API fields

`GET /api/v1/fleet/patch-status` still returns a JSON array. Each element gains optional fields. Existing fields are unchanged. Add `?all=true` for one row per registered device, including fully patched and offline ones.

| Field | Type | Present when | Meaning |
|---|---|---|---|
| `os_patch_mode` | string: `window`, `anytime`, `disabled` | The mode is set | Server-wide OS-patch window mode. |
| `next_window_start` | string, RFC 3339 UTC | Mode is `window` and a start can be computed | Next window start. |
| `os_patch_cooldown_minutes` | number | The cooldown is non-zero | OS-patch window cooldown. |
| `allow_reboot` | boolean | The device's agent reports it | The device's `commands.allow_reboot`. |
| `updates_at` | string, RFC 3339 | Update data exists | When the update data was reported. |
| `lists_freshness` | string: `fresh`, `stale`, `unknown` | Always | Whether the host's apt package lists are older than its apt configuration. Agents that do not report it, dnf hosts and unrecognized values read as `unknown`. Display only: no dispatch decision reads it. See [Stale package lists](#stale-package-lists). |
| `patch_badge` | object | The device is registered | `kind` (`auto`, `auto_reboot_date`, `auto_reboot_unknown`, `manual`, `blocked`, `needs_you`), `label`, `explanation` (`automatic`, `reboot`, `needs_you` action items with `code`, `text`, `action` and optional `command_id` and `updater`, `needs_you_summary` (counts action items only), `blocked` (refusals you cannot act on, in the same shape as `needs_you`; always present, possibly empty), `info` (information items in the same shape), `as_of`, `notes`) and `reboot_at` for `auto_reboot_date` only. `manual` and `info` are new; a client that does not know `manual` should treat it as a neutral badge. `blocked` is new (DIETPI-PATH-001): a client that matches `kind` exactly must accept it, and should treat it as an amber warning that is not "needs you". |
| `extra_updaters` | array | A DietPi update is pending | The extra-updater entries that have an update waiting, at most 8: `id`, `status`, `installed_version`, `available_version`, `pending_count`, `readiness`, `notices`. Display only: no dispatch decision reads it. See [DietPi updates](#dietpi-updates). |

The error response is unchanged: `500 {"error":"failed to get telemetry"}`.

---

## Manual patch and reboot

The device page **Patch** button patches one device. This section covers what "reboot needed" means, the **Reboot automatically if the patch requires it** option on the Patch confirmation, and the **Package lists out of date** label that replaces a phantom update count.

Before this change, three things misled operators. The badge asked for a reboot while the kernel was still only pending. A patch run that rewrote apt configuration left a count of updates that did not exist. And the operator could not authorize the reboot a patch needed as part of the patch itself.

There is no new environment variable, no database migration, no new collector and no new sudoers rule. One part needs a root re-install per device; see [What ships where](#what-ships-where).

### What "reboot required" means

"Reboot required" means the host needs a reboot now. A kernel or GPU update that is pending, but not installed yet, never counts. The badge describes that case as a future reboot.

The agent reports `reboot_required: true` when at least one of these holds:

| Package manager | Condition |
|---|---|
| apt | `/var/run/reboot-required` exists |
| apt | An installed kernel is newer than the running kernel (rule below) |
| dnf | `dnf needs-restarting -r` exits with 1 |

When the kernel rule fires, the agent adds one line to `reboot_required_reasons`: `kernel <installed> installed; running <running>`.

**How the apt kernel rule works.** The agent reads the running kernel from `/proc/sys/kernel/osrelease`. It lists the installed kernels from the `/boot/vmlinuz-<release>` file names. It compares releases with dpkg's version ordering.

Only kernels of the running kernel's flavor are compared. The flavor is the run of trailing `-` tokens that contain a letter and no `.` or `+`. Examples: `amd64`, `generic`, `rpi-v8`, `rt-amd64`, `cloud-amd64`. So a `6.12.48+deb13-rt-amd64` kernel is a different flavor from `6.12.48+deb13-amd64`. Installing one never marks the other as needing a reboot.

The rule counts as "not required" when the running kernel cannot be read, `/boot` cannot be listed, or `/boot` holds more than 256 entries. It reads no more than 256 entries and creates no files.

Two effects follow:

- A host that you deliberately booted into an older kernel reports `reboot_required: true` until it boots the newest installed kernel of its flavor.
- A GPU-driver-only update on a host that writes no `/var/run/reboot-required` file does not count as "reboot required". A ticked manual patch therefore does not reboot for it. The automated in-window run still reboots when it applies reboot-class packages.

### The Patch confirmation

Open the device page and click **Patch**. The confirmation contains one checkbox: **Reboot automatically if the patch requires it**.

- The box is unchecked every time the dialog opens. rIOt never remembers it, per device or per session.
- Ticking the box does three things for this one run: the run installs pending kernel and GPU driver updates that rIOt otherwise holds, the agent checks after the run whether a reboot is required, and the agent reboots only if one is.
- Ticking the box does not mean "reboot no matter what". When no reboot is required after the run, the host stays up.
- Automatic patching and the fleet bulk patch never set this option.

The dialog also says what happens to pending kernel and GPU driver updates. It shows these statements only when such an update is pending:

| Situation | Statement |
|---|---|
| Box unticked, rIOt holds are active | Kernel and GPU driver updates are held by rIOt and will not be installed by this run. |
| Box ticked | Kernel and GPU driver updates will be installed. The host reboots after the run if a reboot is required. |
| Box unticked, rIOt holds are not active | Kernel and GPU driver updates will be installed. A reboot will be needed afterwards, and rIOt will not reboot the host. |
| A reboot is already required (any box state) | A reboot is already required on this host. With the box ticked, the host reboots after the run. |
| Package lists are stale | Package lists are out of date. Running Patch refreshes them first, so the packages installed may differ from the pending list. |

**When the box is disabled.** The dialog shows the reason in text beside the box:

| Reason shown | Cause | Fix |
|---|---|---|
| Reboots are turned off on this agent (commands.allow_reboot: false). | The agent's latest telemetry reports `allow_reboot: false`. | Set `commands.allow_reboot: true` in `/etc/riot/agent.yaml` and restart the agent. |
| The agent must be updated to use this option. | The agent has not reported `allow_reboot` and `supports_reboot_if_required: true`. It is older than this feature, or its `updates` collector is disabled. | Update the agent. If the `updates` collector is off, add `updates` to `collectors.enabled` and restart. |

**What the agent does after a ticked run.**

1. The helper (apt) or the agent (dnf) runs the patch. The server validated the request first; see [`os_update` parameters](#command-endpoints).
2. If the run did not succeed, the agent does not reboot. Failed, timed out and interrupted runs all count. rIOt restores its holds on every outcome.
3. The agent checks whether a reboot is required now, with the same function the updates collector uses.
4. If a reboot is required and `commands.allow_reboot` is still `true`, the agent records its single reboot for this command, then states the decision in the result.
5. The agent queues the result, then waits at least 2 seconds. It waits up to 30 seconds more for the server to confirm it has the result. Then it reboots. If the server stays unreachable, the reboot proceeds anyway and the queued result is delivered after boot.
6. The agent checks `commands.allow_reboot`, the host lock and the maintenance state once more immediately before it reboots. A failed check cancels the reboot, raises one warning event on the device and refreshes telemetry.
7. On a dpkg host, the agent then waits up to 10 minutes for the dpkg lock before it reboots. If the lock stays held, or its state is unknown, the reboot is skipped and a warning event says why. The result from step 4 is already sent and still says `reboot started: yes`. The warning event is the final word. See [Reboots and the dpkg lock](#reboots-and-the-dpkg-lock).

A command causes at most one reboot. It cannot loop: each reboot needs a new, ticked dispatch by an operator.

`mode: repair` never upgrades or reboots. The server rejects a ticked `repair` request.

**Fresh state afterwards.** After any `os_update` ends without a reboot, the agent sends new update telemetry right away. After the agent starts, including after the reboot, its first telemetry push goes out as soon as startup recovery finishes. Recovery is bounded at 2 minutes. It takes long only when an interrupted Docker update must be reconciled. The device page therefore does not wait for the next poll to drop its pre-patch counts.

### When only a reboot is needed

When a device has zero pending updates, `reboot_required` is true and the package lists are not stale, the device page header shows **Reboot now** as the primary action instead of **Patch**. It opens the same confirmation as the Power menu reboot.

If the agent has `commands.allow_reboot: false`, **Reboot now** is disabled. Visible text says to reboot the host outside rIOt.

When updates are pending and a reboot is also required, **Patch** stays the primary action. The dialog says a reboot is already required. **Reboot** stays in the Power menu.

### What the result says

A ticked run adds one sentence to the end of the command's `result_msg`, after ` | `:

```
Reboot authorized; reboot required after run: yes; reboot started: yes
Reboot authorized; reboot required after run: no; reboot not started: not required
```

When no reboot started, the sentence ends with one of these reasons:

| Reason | Meaning |
|---|---|
| `not required` | The run succeeded and no reboot was required. |
| `allow_reboot off` | A reboot was required, but `commands.allow_reboot` is `false` on the agent. The usual `reboot_required` event fires. |
| `run not successful` | The run failed, timed out or was interrupted. |
| `suppressed (reboot already claimed for this command)` | The command already used its one reboot, for example after a redelivery. |
| `suppressed (reboot intent could not be recorded)` | The agent could not save its reboot record, or the record file is unreadable. No reboot happens. |
| `suppressed (reboot intent store full)` | 256 patch reboots happened within 72 hours on this host. |
| `suppressed (result recovered after agent restart)` | The agent restarted during the run. A recovered result never reboots. |

The result keeps the sentence when the text is truncated, because the sentence is last. A request that the agent refuses before the run starts (a host lock conflict, or other maintenance running) carries no sentence, because no run happened.

### Stale package lists

An apt host reports its package lists as `fresh`, `stale` or `unknown` in `lists_freshness`.

**Stale** means the lists cannot match the current apt configuration. The agent judges the lists stale when either of these holds:

- No package lists exist under `/var/lib/apt/lists`.
- A file that apt reads was modified after the lists were last refreshed.

The files checked are `/etc/apt/sources.list`, the `*.list` and `*.sources` files in `/etc/apt/sources.list.d/`, and the files in `/etc/apt/preferences.d/` that have no dot in the name or end in `.pref`. Backup files such as `.dpkg-old`, `.dpkg-dist`, `.save` and `~` are ignored. The single file `/etc/apt/preferences`, `/etc/apt/apt.conf.d/` and keyrings are not checked. The agent takes the refresh time from the modification time of `/var/lib/apt/lists`, or of `/var/lib/apt/periodic/update-success-stamp` on Ubuntu when that is later.

**Unknown** means the check could not finish: an error, more than 1,000 entries, more than 2 seconds, or a configuration file dated more than 5 minutes in the future. An unknown host shows today's `Patch (N)` count. dnf hosts never report `stale`, and older agents report nothing, which reads as `unknown`.

When a host is stale:

- The device page **Patch** button reads "Package lists out of date — refresh pending" and shows no count. The button still works, because a patch run refreshes the lists first. Its tooltip says so.
- The Pending OS Patches panel shows the same label in place of the count. The Devices table shows "Lists out of date". The panel's package total leaves stale hosts out and says how many it left out. Security counts stay visible, on the device page too, and are marked "(lists out of date)".
- The patch badge drops the pending kernel/GPU wording and every reason that comes only from the pending list. It adds the note "Package lists are out of date on this host. Pending updates will be recounted after the next refresh; running Patch refreshes the package lists first."
- The raw count and package list are still reported (`pending_updates`) for diagnostics.

Nothing refreshes the lists by itself. The next manual or automatic patch run does. Freshness is display only: no automatic patching decision reads it.

### Post-run package list refresh

The `riot-maintenance` helper runs `apt-get update` before each `full` or `security` upgrade. A package can rewrite apt configuration during the upgrade, after that refresh. The result is a stale list and a count of packages that do not exist.

The new helper compares a fingerprint of the apt configuration files taken just before its refresh with one taken after the upgrade and final verification. If the fingerprint changed, or either one could not be read, the helper runs one more refresh.

- It runs at most one extra refresh per run, and only for `full` and `security` modes.
- The extra refresh has a 5-minute limit. The helper skips it when less than 7 minutes remain in its 30-minute budget.
- Its output is appended to the run output under the `[post_refresh]` marker, inside the existing 256 KiB bound.
- A failed or skipped extra refresh does not change the run outcome. The run summary gets a note: `post-run package list refresh failed: ...` or `post-run package list refresh skipped: ...`.
- After every successful refresh the helper sets the modification time of `/var/lib/apt/lists` to now. A refresh that downloads nothing otherwise leaves that time unchanged, and the agent would keep reporting `stale`. If the helper cannot do this, it adds a `[lists_stamp]` line to the run output and carries on. It refuses a symlink or a directory not owned by root.

On a run that includes an extra updater (DietPi), the check happens once, after the updater's step, not after the upgrade. A DietPi update that rewrites apt configuration, such as `/etc/apt/preferences.d/dietpi-raspberrypi`, therefore no longer leaves "Package lists out of date" behind. On such a run the 80-minute budget is the time basis only when the helper verified the service's 85-minute backstop in that run. Otherwise the 30-minute basis applies. The full rules are in [Runs with extra updaters](docs/ops/os-maintenance.md#runs-with-extra-updaters). The fix reaches a device only through a root re-run of `install.sh`.

The helper protocol did not change, so old and new agents and helpers work together.

### What ships where

| Change | Reaches a device by |
|---|---|
| Server: request validation, the badge text, `lists_freshness` in the fleet patch status, the dashboard | The normal server update |
| Agent: the kernel rule, list freshness, the `supports_reboot_if_required` fact, the authorized reboot, the reboot-intent file, fresh telemetry after a run | The normal agent update |
| Helper: the post-run refresh and the lists stamp | **Re-run the root `install.sh` on each device**, using the matching release |

Deploy the server first, then update the agents.

Agent-only updates do not refresh the privileged helper. See [Upgrading existing agents](#upgrading-existing-agents) and [OS maintenance and recovery](docs/ops/os-maintenance.md).

Until `install.sh` is re-run on a device, that device keeps the old helper. Everything else in this section works. The difference: when a package rewrites apt configuration during a run, the host reports `stale`. It stays `stale` until a refresh changes a package list file. Debian archives publish several times a day. Running Patch refreshes the lists too, but with the old helper a refresh that downloads nothing does not move the modification time the agent reads. Re-running `install.sh` removes this limit.

| Server | Agent | Result |
|---|---|---|
| New | Old | The checkbox is disabled with "The agent must be updated". A ticked request sent through the API gets `400 reboot_authorization_unsupported`. The agent reports no list freshness, so the host shows `Patch (N)` as before. |
| Old | New | The old server ignores the new telemetry fields. |

### Reboot-intent file

The agent records every reboot it starts or refuses after a patch in `patch-reboot-intents.json` in its data directory (`/var/lib/riot/` on Linux). The record is what limits a command to one reboot, including after a redelivery or an agent restart. The automated in-window run uses the same file. The file needs no operator action.

| Rule | Value |
|---|---|
| Format | `{"version":2,"intents":{"<command-id>":"<claim time, RFC 3339>"}}` |
| Age limit | The agent removes entries older than 30 days at every claim. |
| Capacity | At most 256 entries. When full, the agent evicts the oldest entries, but never one younger than 72 hours. If it cannot make room, the claim fails and no reboot happens. |
| 72-hour floor | A command can be redelivered or replayed for up to 48 hours. An entry younger than 72 hours therefore always stays. |
| Old format | A file in the earlier `{"<id>":true}` format is read once, and every entry gets today as its claim time. The agent rewrites it as version 2 at the next claim. |
| Unreadable file | If the file is corrupt or over 64 KiB, no reboot happens while the file was last written less than 72 hours ago. After that, the agent copies the bad content once to `patch-reboot-intents.json.corrupt` (at most 64 KiB, overwritten by the next copy), starts a new file and carries on. |
| Clock moved backwards | If an entry is dated more than 5 minutes in the future, that claim prunes and evicts nothing. |
| Logging | One Info line per claim that removed entries. One Warn line per refused claim. |

An agent downgraded after it wrote the version 2 file cannot read it, and refuses post-patch reboots until you delete the file. Downgrades are not supported.

---

## DietPi updates

DietPi hosts update DietPi itself with DietPi's own tool, `dietpi-update`, which apt cannot replace. EXTRA-UPD-001 lets rIOt apply that update as one extra step of an OS patch, after apt, so a DietPi host needs one Patch instead of two separate procedures. DietPi is the only "extra updater" today. The step is generic, so a later updater can use the same contract.

The feature is opt-in per device and is not part of any release until the release that contains EXTRA-UPD-001 is published. See the [CHANGELOG](CHANGELOG.md). The operator guide, with the refusal reasons and limits, is in [docs/ops/os-maintenance.md](docs/ops/os-maintenance.md#extra-updaters-dietpi).

### What you get

- **Device page.** An **Other updaters** list shows DietPi next to the apt updates: installed version, available version, status, the reason in words when the state is unknown, and each notice. The **Patch** button names a pending, enabled DietPi update, for example `Patch (3 + DietPi)` or `Patch (DietPi)`.
- **Patch dialog.** When a DietPi update is pending and enabled, the dialog shows **Apply DietPi update in this run**. The box is ticked every time the dialog opens. Unticking it sends `exclude_extra_updaters: true` for that run. The dialog states the version change, that DietPi runs after apt and may take up to 40 minutes. It also says DietPi stops and restarts the services it manages (for example CUPS or VPN software) and runs `apt-get autopurge`, which removes packages apt considers no longer needed. A major upgrade adds a note that the kernel or firmware may change and a reboot is likely.
- **Fleet.** A host whose only waiting update is a DietPi update appears in the Pending OS Patches panel, with a row such as "DietPi 10.8.2". Bulk patch includes it. Automatic patching fires for it when DietPi is enabled and pending, even with no apt updates.
- **Result.** The command result starts with both parts, for example `apt: succeeded; dietpi: succeeded`. A DietPi step that started and did not succeed (`failed`, `timed_out` or `interrupted`) makes the whole command `failed`. A step that rIOt refused before it started does not: the command stays `success`, the first line names the skip, for example `apt: succeeded; dietpi: skipped (unsafe_dietpi_path)`, and the badge reports the refusal ([details](#refused-updates-blocked-and-needs-you)).
- **Badge.** DietPi items follow the [patch badge](#patch-badge) rules.

rIOt reports **unknown**, never "up to date", when DietPi has nothing pending. DietPi leaves no proof that its own check ran and found nothing.

### How it works

1. The `updates` collector reads DietPi's files, read-only, and reports DietPi as an extra updater. No new collector name is needed in `collectors.enabled`.
2. During an `os_update` in `full` or `security` mode, the root worker `riot-maintenance` runs `/boot/dietpi/dietpi-update 1` after apt and after rIOt's holds are restored. It never runs in `repair` mode, and never after an apt failure.
3. The step has a 40-minute limit. Its output is kept in its own section of at most 128 KiB. It runs at most once per command and at most once per 60 minutes per host.
4. The step never reboots the host. A reboot goes only through the existing [reboot gate](#manual-patch-and-reboot).
5. DietPi is run only when its update source owner is `MichaIng`. A `DEV_GITOWNER` in `/boot/dietpi.txt` with any other value is refused.
6. Before the step starts, the worker checks that root alone controls what root runs: `/boot/dietpi`, `/boot/dietpi.txt`, and three entries under `/var/tmp/dietpi` (the directory, `logs` and `dietpi-update_reinstalls`). A stock DietPi host passes. `/run/dietpi` is not checked, because DietPi makes it world-writable; the step gets a private, empty one instead.
7. After a successful step, the worker removes the host's `/run/dietpi/.update_available` and `/run/dietpi/.live_patches`, as DietPi would have. rIOt believes those two flags only when root owns them, nobody else can write them and they have one link.

No sudoers line is added. The privilege is the root worker, a root-owned policy file `/etc/riot-maintenance/extra-updaters.json` and a unit drop-in `/etc/systemd/system/riot-maintenance.service.d/50-riot-extra-updaters.conf`. Only `install.sh`, run as root, writes them.

### Bounds and the patch window

These apply only to a run that includes DietPi. Every other run keeps its bounds (apt 30 minutes, backstop 35 minutes, agent 31 minutes 30 seconds, deadline 40 minutes).

| Bound | Value |
|---|---|
| DietPi step | 40 minutes |
| Worker (overall) | 80 minutes |
| Service backstop (`RuntimeMaxSec`) | 85 minutes |
| Agent hard maximum | 88 minutes |
| Command deadline | 95 minutes |

Set the OS-patch window (**Settings > Agents**) to at least 90 minutes. Keep it clear of 06:25, the default time of DietPi's own `cron.daily` check. The window is in UTC and `cron.daily` uses the host's local time. Details and the residual risk are in the [operator guide](docs/ops/os-maintenance.md#bounds).

### Turn it on, one server at a time

The servers below are the fleet. Run each command on the server named in the step.

#### hawaii

hawaii is Debian and has no DietPi. No action is needed for this feature.

1. On hawaii, change nothing. Do not add `dietpi` to its `commands.allow_extra_updaters`. If it were listed, the installer would print that DietPi is not present and record nothing.

#### vermont

vermont is Debian (OpenMediaVault) and has no DietPi. No action is needed for DietPi.

1. On vermont, change nothing for DietPi.
2. After the server update, the dashboard shows vermont's patch badge as **Manual**, not **Needs you**, when vermont has auto-patch off and nothing pending. vermont is then not counted in "N hosts need you". Hover the badge: `auto_patch_off` is listed under **Information**. If vermont has updates pending or a reboot required, it still reads **Needs you**.

#### PiVPN

PiVPN runs DietPi. DietPi stops and restarts its VPN software while the DietPi step runs, for up to 40 minutes. Do not run the Patch in step 7 over the VPN that you use to reach this host.

1. On PiVPN, open `/etc/riot/agent.yaml` as root. Under the single top-level `commands:` block, set both lines. Keep your existing `allow_reboot` and `allow_shutdown` values, or set them as you want them:

   ```yaml
   commands:
     allow_patching: true
     allow_extra_updaters: [dietpi]
   ```

   Write `[dietpi]` with plain lowercase letters and no quotes. The installer reads a quoted name as "no updaters".

2. On PiVPN, confirm that DietPi's own update check is on:

   ```bash
   grep -n '^CONFIG_CHECK_DIETPI_UPDATES' /boot/dietpi.txt
   ```

   The output must be `CONFIG_CHECK_DIETPI_UPDATES=1`. If it shows another value, set `CONFIG_CHECK_DIETPI_UPDATES=1` in `/boot/dietpi.txt`. Without it, rIOt reports DietPi as unknown and never applies it.

3. On PiVPN, confirm that the update source is the default:

   ```bash
   grep -n '^DEV_GIT' /boot/dietpi.txt
   ```

   There must be no `DEV_GITOWNER` line, or its value must be empty or `MichaIng`. Any other owner is refused.

4. On PiVPN, re-run the installer once, as root, with your existing server URL. Replace `X.Y.Z` with the release that contains EXTRA-UPD-001. Do not pass `--bootstrap-key`: it resets the config and every remote feature in it.

   ```bash
   curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://<server-ip>:7331 --version X.Y.Z
   ```

   A re-run asks no questions. It must print `==> DietPi updates: root-side opt-in recorded (/etc/riot-maintenance/extra-updaters.json); maintenance backstop 85min`. If it prints `no root-side opt-in`, the installer did not read your list. Fix the line from step 1 and re-run.

5. On PiVPN, know what else this one re-run did. It also applies the installer changes still pending from AGENT-HARDEN-001, PATCH-FLOW-001 and SHUTDOWN-SUDO-001 (the opt-in `systemctl poweroff` sudo rule, added only when `allow_shutdown: true` is in the config). See [Upgrading existing agents](#upgrading-existing-agents) and [Shutdown on Linux and the sudo rule](#shutdown-on-linux-and-the-sudo-rule). You do not need a second run for them.

6. On PiVPN, run doctor as the service account:

   ```bash
   sudo -u riot /usr/local/bin/riot-agent -config /etc/riot/agent.yaml doctor
   ```

   The **Extra Updaters** section must show `✓` for `dietpi detected`, the root-side opt-in, the maintenance helper and the maintenance service. Fix every `✗` line as its hint says before you go on.

7. In the dashboard, open PiVPN's device page and run one manual Patch before you rely on the schedule. This is the first time the launcher meets the real DietPi updater on this host, so watch it. Check **Apply DietPi update in this run** is ticked. Tick **Reboot automatically if the patch requires it** only if PiVPN has `allow_reboot: true` and you want the reboot now. When the run ends, read the first line of the command result in the Activity log. `apt: succeeded; dietpi: succeeded` is the goal. If DietPi shows `skipped` or `failed`, use the [refusal table](docs/ops/os-maintenance.md#refusal-reasons) and fix it before you trust the schedule.

8. In **Settings > Agents**, check the OS-patch window is at least 90 minutes long and clear of 06:25 on PiVPN's clock.

9. On PiVPN, plan for a reboot. The first DietPi 9 to 10 update on a Raspberry Pi moves the boot partition from `/boot` to `/boot/firmware` and needs a reboot afterwards. The DietPi step never reboots. The reboot goes through rIOt's reboot gate, and every route needs `commands.allow_reboot: true` on PiVPN. Either tick **Reboot automatically if the patch requires it** on the manual Patch, or use Reboot on the device page afterwards. A scheduled run reboots only under [reboot-class gating](#reboot-class-package-gating).

#### PiCUPS

PiCUPS runs DietPi. DietPi stops and restarts its CUPS print service while the DietPi step runs, for up to 40 minutes. Printing is unavailable during that time.

1. On PiCUPS, open `/etc/riot/agent.yaml` as root. Under the single top-level `commands:` block, set both lines. Keep your existing `allow_reboot` and `allow_shutdown` values, or set them as you want them:

   ```yaml
   commands:
     allow_patching: true
     allow_extra_updaters: [dietpi]
   ```

   Write `[dietpi]` with plain lowercase letters and no quotes.

2. On PiCUPS, confirm that DietPi's own update check is on:

   ```bash
   grep -n '^CONFIG_CHECK_DIETPI_UPDATES' /boot/dietpi.txt
   ```

   The output must be `CONFIG_CHECK_DIETPI_UPDATES=1`. If it shows another value, set `CONFIG_CHECK_DIETPI_UPDATES=1` in `/boot/dietpi.txt`.

3. On PiCUPS, confirm that the update source is the default:

   ```bash
   grep -n '^DEV_GIT' /boot/dietpi.txt
   ```

   There must be no `DEV_GITOWNER` line, or its value must be empty or `MichaIng`.

4. On PiCUPS, re-run the installer once, as root, with your existing server URL. Replace `X.Y.Z` with the release that contains EXTRA-UPD-001. Do not pass `--bootstrap-key`.

   ```bash
   curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://<server-ip>:7331 --version X.Y.Z
   ```

   It must print `==> DietPi updates: root-side opt-in recorded (/etc/riot-maintenance/extra-updaters.json); maintenance backstop 85min`.

5. On PiCUPS, know what else this one re-run did. It also applies the installer changes still pending from AGENT-HARDEN-001, PATCH-FLOW-001 and SHUTDOWN-SUDO-001 (the opt-in `systemctl poweroff` sudo rule, added only when `allow_shutdown: true` is in the config). You do not need a second run for them.

6. On PiCUPS, run doctor as the service account:

   ```bash
   sudo -u riot /usr/local/bin/riot-agent -config /etc/riot/agent.yaml doctor
   ```

   The **Extra Updaters** section must show `✓` for `dietpi detected`, the root-side opt-in, the maintenance helper and the maintenance service. Fix every `✗` line as its hint says.

7. In the dashboard, open PiCUPS's device page and run one manual Patch before you rely on the schedule. Check **Apply DietPi update in this run** is ticked. When the run ends, read the first line of the command result: `apt: succeeded; dietpi: succeeded` is the goal. If DietPi shows `skipped` or `failed`, use the [refusal table](docs/ops/os-maintenance.md#refusal-reasons).

8. In **Settings > Agents**, check the OS-patch window is at least 90 minutes long and clear of 06:25 on PiCUPS's clock.

9. On PiCUPS, plan for a reboot. The first DietPi 9 to 10 update on a Raspberry Pi moves the boot partition from `/boot` to `/boot/firmware` and needs a reboot afterwards. The DietPi step never reboots. The reboot goes through rIOt's reboot gate, as described for PiVPN.

### Fix for stock DietPi hosts (DIETPI-PATH-001)

On PiCUPS, the first DietPi run was refused and the whole patch showed red, although apt had succeeded. The refusal was `unsafe_dietpi_path`: the path check treated `/run/dietpi` as unsafe because stock DietPi makes it world-writable (mode 0777). The check failed on every stock DietPi host, and doctor was green beforehand. DIETPI-PATH-001 fixes this.

What changed:

- **Stock DietPi passes the path check.** `/run/dietpi` is no longer checked. `/var/tmp/dietpi` is checked exactly: the directory, `logs` and `dietpi-update_reinstalls`. The RAMlog store below `logs` is not walked. `/boot/dietpi` and `/boot/dietpi.txt` are still fully walked.
- **The step gets a private `/run/dietpi`.** During the DietPi step, `/run/dietpi` is an empty tmpfs that only that step sees, so a local user cannot use a planted symlink to make root overwrite a file. If rIOt cannot set it up, DietPi is not started and the result says `skipped (rundir_not_private)`.
- **Flags are cleaned up and trusted only from root.** After a successful step, rIOt removes the host's `.update_available` and `.live_patches`. rIOt believes those flags only when root owns them, nobody else can write them and they have one link.
- **A refusal no longer fails a good apt run.** The command is `success`, the summary still names the skip, and rIOt stores one warning event.
- **New Blocked badge.** Refusals you cannot act on show **Blocked** (amber) and are not counted in "N hosts need you". A path refusal under `/boot/dietpi` or `/var/tmp/dietpi` shows **Needs you** with an action to investigate, because it can be a sign of tampering. See [Refused updates: Blocked and Needs you](#refused-updates-blocked-and-needs-you).
- **No sudoers, config or database change.**

The server update fixes the badge. The DietPi run itself is fixed only by a root `install.sh` re-run on each DietPi host, which installs the new worker and agent together. Never copy the worker binary alone. Replace `X.Y.Z` in the steps below with the release that contains DIETPI-PATH-001 (see the [CHANGELOG](CHANGELOG.md)).

#### hawaii

hawaii runs the rIOt server.

1. On hawaii, do the normal server update to the release that contains DIETPI-PATH-001. Do this first, before any Pi. Nothing else changes on hawaii.
2. In the dashboard, open the Devices page. PiCUPS's red badge turns into an amber **Blocked** badge with the text "DietPi update blocked: /run/dietpi permissions (fixed by this rIOt release; re-run install.sh as root on this host to apply the fix)". The server update alone does this. It runs no patch and changes nothing on PiCUPS.

#### vermont

vermont is Debian and has no DietPi. Nothing to do.

#### PiCUPS

Printing is unavailable while the DietPi step runs, for up to 40 minutes.

1. On PiCUPS, re-run the installer once, as root, with your existing server URL. Do not pass `--bootstrap-key`: it resets the config and every remote feature in it.

   ```bash
   curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://<server-ip>:7331 --version X.Y.Z
   ```

   It must print `==> DietPi updates: root-side opt-in recorded (/etc/riot-maintenance/extra-updaters.json); maintenance backstop 85min`.

2. On PiCUPS, run doctor as the service account:

   ```bash
   sudo -u riot /usr/local/bin/riot-agent -config /etc/riot/agent.yaml doctor
   ```

   The **Extra Updaters** section must show `✓` on every line. Doctor does not check the `/run/dietpi` isolation (see [Known limits](docs/ops/os-maintenance.md#known-limits)), so a green doctor does not prove the step will start.

3. In the dashboard, open PiCUPS's device page and run one manual Patch. Check **Apply DietPi update in this run** is ticked. When the run ends, read the first line of the command result: `apt: succeeded; dietpi: succeeded` is the goal. The **Blocked** badge clears when this run finishes, not when the installer finishes.

4. If the result says `dietpi: skipped (<reason>)`, look the reason up in the [refusal table](docs/ops/os-maintenance.md#refusal-reasons). A `skipped (unsafe_dietpi_path)` that names a path under `/boot/dietpi` or `/var/tmp/dietpi` means that path is not root-only. Check who changed it before you run Patch again.

5. On PiCUPS, plan for a reboot after the first DietPi 9 to 10 update, as described in step 9 of the PiCUPS steps above.

6. Optional: apply the [host hardening for DietPi hosts](docs/ops/os-maintenance.md#host-hardening-for-dietpi-hosts-recommended) on PiCUPS.

#### PiVPN

DietPi stops and restarts the VPN software while the DietPi step runs, for up to 40 minutes. Do not run the Patch in step 4 over the VPN that you use to reach this host. PiVPN's patch was held until this fix.

1. On PiVPN, check that the agent config has both opt-in lines:

   ```bash
   grep -n 'allow_patching\|allow_extra_updaters' /etc/riot/agent.yaml
   ```

   The output must show `allow_patching: true` and `allow_extra_updaters: [dietpi]`. If PiVPN never had the EXTRA-UPD-001 re-run, set both lines first, as in steps 1 to 3 of the PiVPN steps above. The one re-run below then covers both EXTRA-UPD-001 and DIETPI-PATH-001.

2. On PiVPN, re-run the installer once, as root, with your existing server URL and without `--bootstrap-key`:

   ```bash
   curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://<server-ip>:7331 --version X.Y.Z
   ```

   It must print `==> DietPi updates: root-side opt-in recorded (/etc/riot-maintenance/extra-updaters.json); maintenance backstop 85min`. If it prints `no root-side opt-in`, the installer did not read your list. Fix the line from step 1 and re-run.

3. On PiVPN, run doctor as the service account:

   ```bash
   sudo -u riot /usr/local/bin/riot-agent -config /etc/riot/agent.yaml doctor
   ```

   The **Extra Updaters** section must show `✓` on every line.

4. In the dashboard, open PiVPN's device page and run one manual Patch. Check **Apply DietPi update in this run** is ticked. Read the first line of the command result: `apt: succeeded; dietpi: succeeded` is the goal. If DietPi shows `skipped`, use the [refusal table](docs/ops/os-maintenance.md#refusal-reasons).

5. On PiVPN, plan for a reboot after the first DietPi 9 to 10 update, as described in step 9 of the PiVPN steps above.

6. Optional: apply the [host hardening for DietPi hosts](docs/ops/os-maintenance.md#host-hardening-for-dietpi-hosts-recommended) on PiVPN.

### Turn it off

1. On the server, remove `dietpi` from `commands.allow_extra_updaters` in `/etc/riot/agent.yaml`, or set the list to `[]`.
2. On the server, re-run the installer as root, as in step 4 above. It prints `==> DietPi updates: root-side opt-in revoked`. It writes the empty policy first and then shortens the service backstop back to 35 minutes. It deletes no file.

### What ships where

| Change | Reaches a device by |
|---|---|
| Server: the badge rules (**Manual**, **Needs you** only when work is waiting), the DietPi budget stamp and events, the dashboard | The normal server update |
| Agent: DietPi detection, readiness, the request, the result lines, doctor | The normal agent update |
| Root: the maintenance worker, the launcher, the policy file and the unit drop-in | **Re-run `install.sh` as root on each DietPi host**, using the matching release |
| Server: the **Blocked** badge and the reclassified refusals (DIETPI-PATH-001) | The normal server update. It also turns a stored old-worker refusal, such as PiCUPS's red badge, into **Blocked** |
| Root, DIETPI-PATH-001: the new path check, the private `/run/dietpi`, the flag cleanup and the truthful outcome | **Re-run `install.sh` as root on each DietPi host.** The new worker and the new agent must arrive together |

Deploy the server first, then update the agents. The badge change needs only the server and the dashboard. An agent that is updated and configured before the root re-run reports DietPi with readiness `unsupported_helper` (old helper) or `root_side_missing`. The badge then shows an action item that says to re-run `install.sh` as root.

---

## Reboots and the dpkg lock

> **Upgrade the server before the agents (REBOOT-DPKG-001).** A new agent can wait up to 10 minutes before it reboots. An old server gives a reboot 10 minutes in total. If the lock frees in the last minute, the old server marks the command `timed_out` and rejects the late result, but the host still reboots. Agents install new releases on their own (every 6 hours by default, see [Agent Auto-Update and Release Signing](#agent-auto-update-and-release-signing)), and an old server that sees the new release can offer it to them. Upgrade the server as soon as the release is published. If a reboot shows `timed_out`, check the host's uptime before you send another one.

> **The same rule applies to shutdown (SHUTDOWN-DPKG-001).** A new agent can wait up to 10 minutes before it powers a host off. An old server gives a shutdown 10 minutes in total (`exec_deadline_s` `600`). A new server gives it 20 minutes (`1200`). If the lock frees in the last minute, the old server marks the command `timed_out` and rejects the late result, but the host still powers off. If a shutdown shows `timed_out`, check whether the host is off before you send another one. A second shutdown can power the host off again after someone switches it back on, while the 15-minute queued expiry has not run out.

Every reboot and every shutdown the agent starts waits until no process holds the dpkg lock, so neither cuts through a running `apt`, `dpkg` or unattended upgrade. The wait applies to hosts that have dpkg (Debian, Ubuntu and derivatives). A host without dpkg, and any Windows or macOS host, reboots and shuts down as before with no wait.

The wait itself needs no install step. The check runs as the unprivileged `riot` user with no sudo and no new sudoers rule, so the wait needs no `install.sh` re-run. No configuration key, collector or environment variable changes. It ships with the normal agent update. Remote shutdown itself is separate: its result is truthful, so a Linux host that has no poweroff sudo rule reports `failed`. That rule is an opt-in that only a root run of `install.sh` adds. See [Shutdown on Linux and the sudo rule](#shutdown-on-linux-and-the-sudo-rule).

### Which power actions wait

| Power action | Started by | Waits |
|---|---|---|
| Standalone reboot | The **Reboot** command in the Power menu, **Reboot now**, or `action: reboot` through the API | Yes |
| Authorized post-patch reboot | A manual Patch with **Reboot automatically if the patch requires it** ticked | Yes |
| Automatic post-patch reboot | The OS-patch maintenance window, after reboot-class packages were applied | Yes |
| Shutdown | The **Shutdown** command in the Power menu, or `action: shutdown` through the API | Yes |

### How the wait works

1. The agent checks the lock. A free lock adds no delay and no log line.
2. If the lock is held, the agent logs one line and checks again every 2 seconds. The wait ends 10 minutes after the first check that found the lock held. One attempt has one wait.
3. When the lock looks free, the agent re-checks the action's config flag (`commands.allow_reboot` for a reboot, `commands.allow_shutdown` for a shutdown), whether the agent is shutting down, and the host maintenance state. Then it checks the lock once more. The reboot or shutdown starts only if that last check is also free.
4. If the lock is taken again, the wait continues under the same 10-minute limit.
5. At the limit, or when the lock state is unknown, the agent skips the reboot or shutdown. It never forces it and never retries by itself. Send a new command, or patch again, when you want another attempt.
6. When the agent service stops, a waiting reboot or shutdown ends within a few seconds and is reported as skipped. A shutdown skipped this way is never run again after the agent starts.

A reboot or shutdown holds the host's command slot while it waits. Another host-level command on the same device (`os_update`, `reboot`, `shutdown`, `agent_update`) conflicts until it ends. A post-patch reboot keeps the host reservation for up to about 10.5 minutes.

The reboot and shutdown limits are separate settings that happen to have the same values today. The two actions read their own constants, so changing one does not change the other.

### How the agent detects the lock

The agent never opens or takes the dpkg lock files, which only root can read. Each check does this:

1. It looks up the device and inode of `/var/lib/dpkg/lock-frontend` and `/var/lib/dpkg/lock` with `stat`.
2. It confirms that it runs in the host (initial) pid namespace.
3. It reads `/proc/locks` and looks for a lock on either file. A waiting lock request does not count.
4. If a lock matches, it reads the holder's name from `/proc/<pid>/comm` and its user id from `/proc/<pid>/status`.

A check reads at most 1 MiB of `/proc/locks` and takes at most 1 second. If `stat` reports an anonymous device (btrfs, some overlay or tmpfs roots), the inode alone decides the match and the report says `inode-only match`. That path has not been verified on real hardware.

### When the agent cannot tell, it skips

The agent reboots or shuts down only when it can show the lock is free. These cases skip the reboot or shutdown at once, with no wait. On a host without dpkg, none of them applies.

| Case | Cause text in the report |
|---|---|
| The agent is not in the initial pid namespace, for example `PrivatePIDs=yes` in its unit, or a container. Its `/proc/locks` would not show every holder. | `agent is not in the host pid namespace` |
| The agent cannot read its own pid namespace link, or the value is malformed | `cannot verify the pid namespace: <detail>` |
| dpkg is present (`/var/lib/dpkg/status` or `/usr/bin/dpkg` exists) but both lock files are missing or hidden. This includes a non-Debian host that has the `dpkg` binary installed. | `dpkg is installed but its lock files are missing or hidden` |
| The lock files disappear while the agent waits | `dpkg lock files disappeared during the wait` |
| `/proc/locks` cannot be read (`ProcSubset=pid` in the unit hides it), or a line is longer than 4 KiB | `cannot read /proc/locks: <error>` |
| `/proc/locks` is larger than 1 MiB | `lock information exceeds 1 MiB` |
| `/proc/locks` has a line the agent does not recognize | `unrecognized /proc/locks line <n>` |
| A check takes longer than 1 second | `lock check exceeded 1s` |
| `stat` fails with an error other than "not found" | `cannot stat <path>: <error>` |

The shipped units (`scripts/riot-agent.service` and the unit that `install.sh` writes) set none of `PrivatePIDs=`, `ProtectProc=` or `ProcSubset=`. A test checks both. Do not add `PrivatePIDs=yes` or `ProcSubset=pid` to the agent unit: every reboot and shutdown on a dpkg host would then skip. Run the agent on the host, not in a container, if reboots and shutdowns must work.

### What you see

**Standalone reboot.** The command result carries one of these messages:

| Outcome | Status | Message |
|---|---|---|
| Lock free | `success` | `reboot initiated` |
| Lock freed after a wait | `success` | `reboot initiated after waiting <duration> for the dpkg lock` |
| Held for 10 minutes | `failed` | `reboot skipped: dpkg lock held past the timeout (10m), <holder>` |
| State unknown | `failed` | `reboot skipped: dpkg lock state unknown: <cause>` |
| Agent stopping | `failed`, reason `shutdown` | `reboot skipped: agent shutting down` |
| A guard failed | `failed`, reason `conflict` when the host is busy | `reboot skipped: guard failed: <guard>` |

`<guard>` is `commands.allow_reboot is off`, `host busy: <what is running>`, or `agent command time budget exhausted`. A skip is not a timeout: it never shows the "Timed out: outcome unknown" label. When `commands.allow_reboot` is `false`, the reboot fails before the wait with the existing message `reboot not allowed by agent config (set commands.allow_reboot: true)`.

**Shutdown.** The command result carries one of these messages:

| Outcome | Status | Message |
|---|---|---|
| Lock free, poweroff accepted | `success` | `shutdown initiated` |
| Lock freed after a wait | `success` | `shutdown initiated after waiting <duration> for the dpkg lock` |
| Held for 10 minutes | `failed` | `shutdown skipped: dpkg lock held past the timeout (10m), <holder>` |
| State unknown | `failed` | `shutdown skipped: dpkg lock state unknown: <cause>` |
| Agent stopping | `failed`, reason `shutdown` | `shutdown skipped: agent shutting down` |
| A guard failed | `failed`, reason `conflict` when the host is busy | `shutdown skipped: guard failed: <guard>` |
| The poweroff command did not start, or the system refused it | `failed` | `shutdown: <error>` |
| An earlier poweroff command is still running | `failed` | `shutdown: a previous poweroff command is still running` |

`<guard>` is `host busy: <what is running>` or `agent command time budget exhausted`. A skip is not a timeout: it never shows the "Timed out: outcome unknown" label. When `commands.allow_shutdown` is `false`, the shutdown fails before the wait with the existing message `shutdown not allowed by agent config (set commands.allow_shutdown: true)`.

`<error>` is the text the system returned. It holds printable characters only and is at most 200 bytes. For example: `shutdown: exit status 1: sudo: a terminal is required to read the password; either use the -S option ...`.

Every failed shutdown, including a skip, also raises the existing warning event `Command shutdown failed: <message>` on the device. Notification channels forward that text, so the holder's name, pid and uid reach them.

**What "shutdown initiated" means.** The agent starts the poweroff command and watches its exit status for 3 seconds. It reports `shutdown initiated` when the command exits with status 0, when it is still running after 3 seconds, or when a signal ends it. The last case is what happens when the system starts powering off and stops the agent's child processes. It reports `shutdown: <error>` when the command cannot start or exits with a non-zero status. `shutdown initiated` means the operating system accepted the poweroff command. It does not mean the host has finished powering off. If the poweroff command fails after the 3 seconds, the result stays `shutdown initiated` and the agent writes one `WARN` line to its log.

**Dashboard label.** While a standalone reboot or shutdown waits, a new agent reports the step `wait_dpkg_lock`, and the Devices page and the device page show `Rebooting (waiting for dpkg lock)` or `Shutting down (waiting for dpkg lock)`. See [Agent-side activity reporting](#agent-side-activity-reporting).

**Post-patch reboots.** A skipped authorized or automatic post-patch reboot raises one warning event on the device and refreshes telemetry, so the host keeps showing that a reboot is required:

```
authorized reboot for command <command-id> was not started: <reason>
post-patch reboot for command <command-id> was not started: <reason>
```

`<reason>` is `dpkg lock held past the timeout (10m), <holder>`, `dpkg lock state unknown: <cause>`, `agent shutting down`, or a guard text. The first line is the manual Patch with the box ticked. The second is the maintenance-window reboot. The event during an agent shutdown is best effort. The skip is also written to the agent log as a `WARN` line (`os_update: authorized reboot not started` or `os_update: post-patch reboot not started`).

**The holder.** Reports name the holder in one of these forms:

```
held by unattended-upgr (pid 1234, uid 0)
holder not visible (pid 1234, uid 0)
holder not visible
```

An inode-only match adds `, inode-only match` inside the brackets. The name is the first 16 characters of the process name, with every character outside letters, digits and `. _ + : -` replaced by `?`.

**Agent log.** A reboot or shutdown that waits writes exactly two `INFO` lines and nothing per check: `dpkg lock held, waiting before reboot` (or `... before shutdown`) and `dpkg lock wait ended`. The second has the fields `path` (`reboot`, `shutdown`, `post_patch` or `authorized_post_patch`), `command_id`, `outcome` (`cleared`, `lock_timeout`, `lock_unknown`, `shutdown` or `guard`), `waited` and `holder`.

A shutdown whose poweroff command fails after the 3-second check writes one more line, at most once per command: `WARN shutdown: poweroff command failed after the start check`, with an `error` field and no `command_id`. A command that fails inside the 3 seconds writes no such line. Its result carries the error.

### Shutdown on Linux and the sudo rule

Remote shutdown on Linux is opt-in. The agent runs `sudo systemctl poweroff` as the unprivileged `riot` user. That works only when `/etc/sudoers.d/riot-agent` contains a rule for it. The installer writes that rule only for a host that opts in. Installers before SHUTDOWN-SUDO-001 never wrote it, so a host installed by one of them still has no poweroff rule. The installer's reboot rule is separate (see [Sudoers Rules](#sudoers-rules)).

On a Linux host with no poweroff rule, sudo refuses the command because it has no terminal to ask for a password. Releases before SHUTDOWN-DPKG-001 did not check the result. They reported `shutdown initiated` and the host stayed on.

Since SHUTDOWN-DPKG-001 the agent reports the refusal. On a host with no poweroff rule a shutdown ends `failed`, for example:

```
shutdown: exit status 1: sudo: a terminal is required to read the password; either use the -S option ...
```

This is the true result: a remote shutdown never worked on a host like this. The agent's shutdown command, its arguments and its `allow_shutdown` gate are unchanged. The only change is that the installer can now add the missing rule.

On Windows, `shutdown /s /t 5` returns at once with status 0, so a working Windows host still reports `shutdown initiated`. A refusal now shows as `failed` there too.

#### What the installer grants

When shutdown is enabled for the run, the installer adds exactly one line to `/etc/sudoers.d/riot-agent`, directly after the reboot line:

```text
riot ALL=(root) NOPASSWD: <systemctl path> poweroff
```

`<systemctl path>` is the same absolute `systemctl` path as in the reboot line, for example `/usr/bin/systemctl`. The rule names only the `riot` user and has no extra arguments and no wildcard. It does not allow `systemctl halt`, `kexec` or `suspend`, `systemctl poweroff --force`, or the `shutdown`, `poweroff` and `halt` programs. Every other line of the file is unchanged. The installer validates the file with `visudo` before and after it installs it, as it does for every run.

The installer enables shutdown for a run in these cases:

| Run | Shutdown is enabled when |
|---|---|
| Fresh install (no `/etc/riot/agent.yaml`) | You answer yes to `Allow remote shutdown from dashboard?`, or you pass `-y`. `--non-interactive` and a missing terminal answer no. The installer then writes `allow_shutdown: true` into the new config. |
| Reinstall (config exists), no `--bootstrap-key` | The existing config enables it, and nothing else decides: `-y` does not turn it on and `--non-interactive` does not turn it off. |
| Reinstall with `--bootstrap-key` | Never. The installer rewrites the config with every remote feature off. |
| macOS | Never. The question is not asked. |

The config enables shutdown only when a top-level `commands:` block contains the plain line `allow_shutdown: true`: lowercase, no quotes, in a regular file. The installer treats anything else as not enabled, so a doubtful file never gets the rule. These all count as not enabled: `false`, a missing key, a commented-out line, the key under another section such as `docker:`, and values such as `True`, `yes`, `on` or `"true"`. So do unusual file layouts, such as tab indentation, Windows line endings, more than one YAML document, flow style (`commands: {...}`), anchors, a first line that is indented, a symlink, or a file over 64 KiB. If the installer reads your `true` as not enabled, rewrite the line as `allow_shutdown: true`, indented under `commands:`, and re-run. The config and the rule always agree after a successful run: the rule exists only when the final config enables shutdown.

#### Turn on remote shutdown on an existing server

Do this on each server where you want the dashboard's Shutdown command to work. The fleet servers are hawaii, vermont, PiVPN and PiCUPS. Servers where you do not want remote shutdown need nothing.

1. On the server, open `/etc/riot/agent.yaml`. Under the top-level `commands:` block, set `allow_shutdown: true`. Create the block if it is missing:

   ```yaml
   commands:
     allow_shutdown: true
   ```

2. Re-run the installer as root, with your existing server URL. Use an installer that includes SHUTDOWN-SUDO-001. An older installer ignores `allow_shutdown` and writes a sudo file without the poweroff rule. Do not pass `--bootstrap-key`; it resets the config.

   ```bash
   curl -sSL https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh | sudo bash -s -- https://<server-ip>:7331
   ```

3. Check the output. The installer prints `==> Remote shutdown: commands.allow_shutdown is true in /etc/riot/agent.yaml; granting sudo systemctl poweroff`. If it prints `not enabled`, it did not read your `true`; see the rules above.
4. Run `riot-agent doctor`. The **Remote Shutdown** section must show the `✓ shutdown sudo rule present` line.

Editing the config alone does not add the rule. With `allow_shutdown: true` and no rule, a shutdown reports `failed` and `doctor` prints the `✗` line.

The same re-run also applies the other privileges that need an `install.sh` re-run (AGENT-HARDEN-001 and PATCH-FLOW-001; see [Upgrading existing agents](#upgrading-existing-agents)). If a server still needs those, do one re-run for all three. Agent-only updates never add or remove the poweroff rule. No server or agent upgrade order is required for it.

#### Turn off remote shutdown

1. In `/etc/riot/agent.yaml`, set `allow_shutdown: false`, or remove the line.
2. Re-run the installer as root, as above. The new `/etc/sudoers.d/riot-agent` has no poweroff line, and the installer prints `==> Remote shutdown: not enabled in /etc/riot/agent.yaml; no poweroff grant ...`.

If you set the value to `false` and do not re-run the installer, the sudo rule stays until the next run. The agent still refuses a shutdown, because its own `allow_shutdown` gate is `false`. `doctor` does not check the rule when the value is `false`, so it does not report the leftover rule.

#### If the installer stops or the result looks wrong

| What you see | What it means | What to do |
|---|---|---|
| `shutdown: exit status 1: sudo: a terminal is required ...` in a shutdown result, and `doctor` prints `✗ shutdown sudo rule missing or unverifiable` | `allow_shutdown` is `true` but the host has no poweroff rule. This is the case after an agent-only update, or after you edited the config without re-running the installer. | Re-run the installer as root. |
| `==> Remote shutdown: not enabled ...` although the file says `true` | The installer reads only the plain form described above. | Rewrite the line as `allow_shutdown: true` under `commands:` and re-run. |
| `agent.yaml exists but is not a regular file` or `has N links` | The config is a directory, a broken symlink or a hard-linked file. The installer changed nothing. | Move the file aside and re-run. |
| `agent.yaml commands.allow_shutdown changed during installation; previous managed policy restored; the agent is left stopped` | The config changed between the installer's first read and its final check. The installer put the previous sudo policy back. | Check the file, then re-run the installer. It starts the agent again. |
| Installer fails after it wrote the sudo file but before it replaced the agent binary | The installer restores the previous sudo file on its way out when that run had added the poweroff rule. | Fix the cause and re-run. |

#### Security note

`/etc/riot/agent.yaml` belongs to the `riot` user. A compromised agent could set `allow_shutdown: true` there, and your next root re-run of the installer would then add the poweroff rule. That can only turn the host off. It needs your deliberate re-run, and the installer prints a `Remote shutdown:` line that tells you what it read. Read that line on every reinstall.

### How long a shutdown can take, and how to cancel it

A shutdown can power the host off well after you click **Shut Down**:

- If the device is connected and the dpkg lock is held, the host powers off up to about 10 minutes after the click.
- If the device was not connected when you sent the command, the command first waits in the queue for up to 15 minutes. Then it waits up to 10 more minutes for the lock. The worst case is about 25 minutes from click to poweroff.
- The host stays off until someone powers it on. Make sure you have physical or out-of-band access before you send the command.

The dashboard cannot cancel a waiting shutdown, and it shows no progress for the wait. To cancel one, restart the agent on the host:

```
sudo systemctl restart riot-agent
```

Within a few seconds the waiting command ends with `shutdown skipped: agent shutting down`. The agent never runs it again after it starts. This works only while the agent is waiting for the lock. Once the poweroff command has started, nothing cancels it. A shutdown still queued for an offline device is delivered when the device reconnects, unless its 15-minute expiry passes first.

Editing `commands.allow_shutdown` does not cancel a waiting shutdown. The agent reads its configuration only at start-up, so a running agent never sees the change.

### Time limits

| Limit | Value |
|---|---|
| Wait for the lock, per reboot attempt | 10 minutes |
| Wait for the lock, per shutdown attempt | 10 minutes |
| Check interval | 2 seconds |
| Start check of the poweroff command (shutdown only) | 3 seconds |
| Agent time budget for a standalone reboot | 12 minutes |
| Agent time budget for a shutdown | 12 minutes (was 1 minute) |
| Server execution deadline for a standalone reboot | 20 minutes |
| Server execution deadline for a shutdown | 20 minutes (was 10 minutes) |
| Queued expiry for a reboot or shutdown | 15 minutes, unchanged |

The server stores the 20-minute deadline in `exec_deadline_s` (`1200`) when it creates a reboot or a shutdown. A command created before the server upgrade keeps `600`. If an agent dies during a wait, the server marks the command `timed_out` about 20 minutes after dispatch.

### Local users can block reboots and shutdowns

Any local user on a managed host can delay or block rIOt reboots and shutdowns. For example, a user can hold enough byte-range locks to push `/proc/locks` past 1 MiB. On a filesystem that reports an anonymous device, a user can also make a lock on their own file match the inode number of a dpkg lock file. This is the price of failing closed. Each skip is reported with the holder's name, pid and uid.

A process can choose its own name, so do not trust the name alone. A real package manager holds the lock as `uid 0`. When a skip names another uid, or a name that does not match the pid's process, investigate that host.

### Known limits

- A waiting reboot or shutdown cannot be cancelled from the dashboard. It can start up to 10 minutes after the command reaches the agent. Stopping the agent ends the wait.
- A shutdown for a device that was offline can power the host off about 25 minutes after you sent it (15 minutes queued, plus the wait). The host then stays off. See [How long a shutdown can take, and how to cancel it](#how-long-a-shutdown-can-take-and-how-to-cancel-it).
- On a Linux host set up by the installer, shutdown reports `failed` until the host has the poweroff sudo rule. Only a root run of `install.sh` adds it, and only when `commands.allow_shutdown` is `true`. Agent-only updates never add or remove it. See [Shutdown on Linux and the sudo rule](#shutdown-on-linux-and-the-sudo-rule).
- If you set `allow_shutdown: false` and do not re-run the installer, the poweroff sudo rule stays on the host. The agent still refuses a shutdown, and `doctor` does not report the leftover rule.
- The installer reads `allow_shutdown` with a strict, narrow reader. A valid but unusual spelling of `true` (`yes`, `True`, quoted) counts as not enabled, and the host stays without the rule.
- A shutdown reported as `initiated` may still not power the host off. A signal that ends the poweroff command within the first 3 seconds counts as accepted. An unrelated signal in that window, such as an agent restart or the out-of-memory killer, can make the result wrong.
- A gap of up to a few seconds remains between the final lock check and the moment systemd begins the reboot or shutdown. A package process that takes the lock in that gap can still be interrupted.
- The result of a ticked Patch is written before the wait. Its `reboot started: yes` can be followed by a skip. Read the warning event.

---

## Fleet Dashboard

The `/dashboard` route provides a fleet-wide health view. It is separate from the `/` Devices route, which is a sortable per-device management table. The dashboard answers "is my fleet healthy right now?" without requiring you to click into individual devices.

Navigate to it via **Infrastructure > Overview** in the application sidebar, or go directly to `http://<server>:7331/dashboard`.

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

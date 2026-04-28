# Changelog

All notable changes to rIOt are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions correspond to git tags. See [Releases](https://github.com/rbretschneider/rIOt/releases) for binaries and full release notes.

---

## [Unreleased]

### Added

- [FLEET-DASH] New `/dashboard` route: a fleet-wide health view separate from the existing `/` device table. The dashboard renders three sections — per-device performance charts (one card per device, four lines: CPU%, RAM%, root-disk%, normalized load), a sortable container leaderboard, and a live activity river. Navigation link added to the header alongside Devices, Probes, Security, and Alerts.
- [FLEET-DASH] New `GET /api/v1/fleet/heartbeats?window=60m` endpoint (admin auth). Returns the last 60 minutes of heartbeats for every device in a single round trip, grouped by device ID, plus a list of device IDs that have GPU telemetry. The `window` parameter accepts values in the range `1s` – `3600s` / `1m` – `60m`; values outside this range or with invalid format return 400. The fleet-wide 60-minute cap is deliberate and does not match the per-device endpoint's 168-hour cap.
- [FLEET-DASH] New `GET /api/v1/fleet/containers` endpoint (admin auth). Returns a flat list of containers across the fleet with CPU%, memory usage/limit, restart count, update-available flag, Compose stack name, and device hostname. The response projects only the fields needed by the leaderboard — full telemetry blobs, environment variables, mount paths, networks, and container labels (other than the Compose project name) are not included.
- [FLEET-DASH] The existing WebSocket connection (`/ws`) is reused for dashboard live updates. No new WebSocket topic or second connection is introduced; chart re-renders are batched to a 5-second cadence to keep frame budget predictable on large fleets.
- [FLEET-DASH] Per-device charts use a consistent color palette across all cards (blue=CPU, green=RAM, amber=Disk, purple=Load) so a glance at multiple cards is comparable.
- [FLEET-DASH] Dashboard degrades gracefully when the WebSocket disconnects: a non-blocking banner appears and all sections continue showing the last-known values. The banner clears automatically on reconnect.
- [FLEET-DASH] Full design targets desktop viewports ≥768px. Below 768px the layout stacks vertically and remains readable; no horizontal page scroll occurs at any viewport width down to 360px.

- [SYS-EXPORT-001] New "Download Summary" and "Copy to Clipboard" buttons on the device detail page that export a device's hardware and software inventory as a Markdown document. The export covers system identity (hostname, UUID, board, BIOS), OS, CPU, memory, storage (physical drives with SMART health + filesystems), GPUs, network interfaces, USB devices, Docker containers (name, image, state, status only), and UPS. Sections are omitted entirely when the device has no data for that category. The downloaded file is named `{hostname}-summary-{YYYY-MM-DD}.md`. Both buttons are disabled when the device has no telemetry data. The clipboard button shows a "Copied!" confirmation for 2 seconds on success and "Copy Failed" on error.
- [SYS-EXPORT-001] New `GET /api/v1/devices/{id}/summary` endpoint that returns a pre-formatted Markdown inventory document (`text/markdown; charset=utf-8`) generated from the device's most recent telemetry snapshot. Returns 404 when the device does not exist or has no telemetry data. Requires admin JWT authentication.

- [GPU-001] New `gpu` collector that reads runtime metrics from NVIDIA GPUs via `nvidia-smi`: temperature, utilization %, memory used/total, fan speed, and power draw/limit. Linux-only. Must be explicitly added to `collectors.enabled` in the agent config — not enabled by default. Silently returns no data when `nvidia-smi` is not installed.
- [GPU-001] GPU Telemetry card on the device detail page, visible when GPU telemetry data is present. Displays each GPU's name, index, temperature (color-coded: green < 60°C, amber 60–79°C, orange 80–89°C, red ≥ 90°C), utilization gauge, memory used/total with percentage gauge, fan speed, and power draw/limit. Updates in real-time via WebSocket.
- [GPU-001] Two GPU alert templates in Settings > Alert Rules > Create from Template (category `gpu`): "GPU Temperature Warning" (> 80°C, warning, 1h cooldown) and "GPU Temperature Critical" (> 90°C, critical, 30m cooldown). Alert events identify the GPU by name and index.
- [GPU-001] Four new alert metrics for custom GPU rules: `gpu_temp` (temperature in °C), `gpu_util_percent` (utilization %), `gpu_mem_percent` (memory controller utilization %), `gpu_power_watts` (power draw in W). All metrics are evaluated per-GPU.
- [GPU-001] `riot-agent doctor` now checks for `nvidia-smi` in `PATH` when the `gpu` collector is enabled, consistent with the `smartctl` check for `hardware` and `upsc` check for `ups`.

- [POOL-001] Storage pool filesystems (mergerfs, ZFS, Btrfs, bcachefs, unionfs) now appear as a distinct "Storage Pools" card section above the regular Filesystems table on the device detail page. Each card shows mount point, filesystem type, a color-coded capacity gauge, and used/total/free in human-readable units (GB or TB). Devices without any pool filesystem show no change.
- [POOL-001] `Filesystem` telemetry model gains an `is_pool` boolean field (JSON: `is_pool`, omitted when false). Updated agents set this field automatically — no agent configuration change is required. Old agents without the field are handled by a client-side fallback that classifies pool types from `fs_type`.
- [POOL-001] `internal/models.PoolFSTypes` and `IsPoolFSType()` exported from the models package as the single authoritative pool-type list for Go code. `web/src/utils/filesystem.ts` exports `POOL_FS_TYPES`, `isPoolFilesystem()`, and `formatCapacity()` as the frontend equivalents.

- [POOL-002] Storage pool detection now recognizes Unraid arrays (`shfs`, `fuse.shfs` filesystem types), Linux software RAID arrays (device path prefix `/dev/md`), and LVM/device-mapper volumes (device path prefixes `/dev/mapper/` and `/dev/dm-`). Docker device-mapper volumes (`/dev/mapper/docker-*`) and live-boot overlay devices (`/dev/mapper/live-rw`, `/dev/mapper/live-base`) are explicitly excluded. All newly detected pool types appear in the existing "Storage Pools" card section without any configuration change.
- [POOL-002] Pool cards on the device detail page now display the underlying block device path (e.g., `/dev/md0`, `/dev/mapper/vg0-data`) below the mount point, making mdraid and LVM pools identifiable at a glance.

- [SEC-001] Security page now displays a per-device "Sec. Updates" column showing the count of pending security-classified package updates; amber/red when greater than zero.
- [SEC-001] Security page now displays a per-device "Auto-Updates" column showing whether unattended OS upgrades are enabled; green for enabled, amber for disabled, dash when no update telemetry is available.
- [SEC-001] Security page now displays a fleet score banner (arithmetic mean of all per-device scores) above the device table, color-coded by letter grade.
- [SEC-001] Security page now displays a "Certs Expiring" overview card showing the count of SSL/TLS certificates expiring within 30 days across the fleet; only shown when at least one device reports web server certificate data. Requires the `webservers` collector to be enabled on at least one device.
- [SEC-001] Security page table is now sortable by Score (default, ascending — worst first), Failed Logins, and Sec. Updates columns.
- [SEC-001] New shared utility module `web/src/utils/security.ts` exporting `gradeColor`, `gradeStrokeColor`, and `gradeFromScore` for consistent score display across the dashboard.
- [SEC-001] `MiniScore` component extracted to `web/src/components/MiniScore.tsx` for reuse.

- [NGINX-ALERTS] New nginx access log monitoring capability in the `webservers` collector. When `collectors.webservers.nginx.access_log` is set in the agent config, the agent tails the nginx access log each telemetry interval and reports per-interval counts of total requests, 2xx, 3xx, 4xx, and 5xx responses. Memory usage is O(1) regardless of log volume — safe for Raspberry Pi. Byte offset is persisted to `/etc/riot/nginx-access-log.offset` so the agent resumes correctly after a restart. Log rotation is detected automatically.
- [NGINX-ALERTS] Three new alertable metrics for nginx access log data: `nginx_5xx_count` (5xx responses per interval), `nginx_4xx_count` (4xx responses per interval), `nginx_request_count` (total requests per interval). All three appear in the Settings > Alert Rules metric dropdown with pre-filled defaults.
- [NGINX-ALERTS] Two new alert templates in Settings > Alert Rules > Create from Template under the `webserver` category: "Nginx 5xx Errors High" (metric `nginx_5xx_count`, threshold > 10, critical, 5-minute cooldown) and "Nginx 4xx Errors High" (metric `nginx_4xx_count`, threshold > 50, warning, 15-minute cooldown).
- [NGINX-ALERTS] Three new event types: `nginx_5xx_high`, `nginx_4xx_high`, `nginx_request_high`. Alert events include the actual count and the device hostname in the message. No hardcoded fallback thresholds — nginx alerts only fire when the user has explicitly configured a rule.

- [LOG-001] New `failed_logins_interval` metric on Linux agents — a per-interval count of SSH, sudo, console, and su authentication failures observed in journald since the previous telemetry push. Detection latency is at most one telemetry interval (default 60 seconds). Non-Linux agents omit the field; no `collectors.enabled` config change is required on existing deployments.
- [LOG-001] Entries are origin-filtered to trusted journald sources before counting (`_UID=0` and an allow-listed `_SYSTEMD_UNIT` or `SYSLOG_IDENTIFIER`). Lines written by unprivileged users via `logger(1)` are rejected, preventing local shell users from forging auth-failure signals.
- [LOG-001] New **Auth Failure** alert template in Settings > Alert Rules > Create from Template (category `security`): metric `failed_logins_interval`, operator `>`, threshold `0`, severity `warning`, cooldown `300s`. The template description warns that the default threshold fires on every auth failure; on internet-facing SSH hosts, raise the threshold before enabling.
- [LOG-001] New event types: `auth_failure` (fired by the alert evaluator when the rule trips) and `detector_initialized` (informational agent startup log acknowledging the first-interval-zero window).
- [LOG-001] `riot-agent doctor` gains a **Journal Read Access** check on Linux when the `logs` or `security` collector is enabled. The check detects the silent-zero failure mode where `journalctl` is installed but the agent user is not in the `systemd-journal` group. Remediation: `sudo usermod -a -G systemd-journal riot && sudo systemctl restart riot-agent`.

### Changed

- [FLEET-DASH] Header navigation gains a "Dashboard" link pointing to `/dashboard`. The existing `/` route and `FleetOverview` device table are unchanged.

- [POOL-002] `internal/models.IsPoolFSType()` replaced by `IsPoolFilesystem(fsType, device string) bool`. The new function combines filesystem-type and device-path detection in one call. The disk collector call site in `internal/agent/collectors/disk.go` is updated accordingly. Any code calling `IsPoolFSType` directly must migrate to `IsPoolFilesystem`.
- [POOL-002] `POOL_FS_TYPES` in `web/src/utils/filesystem.ts` and `PoolFSTypes` in `internal/models/telemetry.go` each gain two entries: `shfs` and `fuse.shfs`. The frontend `isPoolFilesystem()` fallback (used for pre-POOL-002 agents) now also checks device path prefixes in addition to filesystem type.

- [SEC-001] The Security page is now the primary location for security posture data. The per-device security score column has moved from Fleet Overview to the Security page. Fleet Overview no longer shows security scores.
- [SEC-001] `GET /api/v1/security/devices` response extended with three new fields: `pending_security_count` (int), `unattended_upgrades` (bool or null), `certs_expiring_soon` (int). Existing consumers that ignore unknown fields are unaffected.
- [SEC-001] `GET /api/v1/security/overview` response extended with two new fields: `certs_expiring_soon` (int), `total_certs` (int).

### Known Limitations (v1)

- [FLEET-DASH] The container leaderboard's "Restarts" sort uses a cumulative restart count rather than a true rolling 60-minute window. A follow-up story is needed to add rolling restart-count projection from historical telemetry.
- [FLEET-DASH] Per-device cards do not yet include a network throughput line. Heartbeats do not carry per-interface byte rates; the FLEET-NET follow-up story extends the heartbeat schema and adds the network line.
- [FLEET-DASH] Disk line on per-device cards reflects root-mount utilization only. Non-root mounts are not surfaced in v1.

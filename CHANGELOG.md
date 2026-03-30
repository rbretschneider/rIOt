# Changelog

All notable changes to rIOt are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions correspond to git tags. See [Releases](https://github.com/rbretschneider/rIOt/releases) for binaries and full release notes.

---

## [Unreleased]

### Added

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

### Changed

- [POOL-002] `internal/models.IsPoolFSType()` replaced by `IsPoolFilesystem(fsType, device string) bool`. The new function combines filesystem-type and device-path detection in one call. The disk collector call site in `internal/agent/collectors/disk.go` is updated accordingly. Any code calling `IsPoolFSType` directly must migrate to `IsPoolFilesystem`.
- [POOL-002] `POOL_FS_TYPES` in `web/src/utils/filesystem.ts` and `PoolFSTypes` in `internal/models/telemetry.go` each gain two entries: `shfs` and `fuse.shfs`. The frontend `isPoolFilesystem()` fallback (used for pre-POOL-002 agents) now also checks device path prefixes in addition to filesystem type.

- [SEC-001] The Security page is now the primary location for security posture data. The per-device security score column has moved from Fleet Overview to the Security page. Fleet Overview no longer shows security scores.
- [SEC-001] `GET /api/v1/security/devices` response extended with three new fields: `pending_security_count` (int), `unattended_upgrades` (bool or null), `certs_expiring_soon` (int). Existing consumers that ignore unknown fields are unaffected.
- [SEC-001] `GET /api/v1/security/overview` response extended with two new fields: `certs_expiring_soon` (int), `total_certs` (int).

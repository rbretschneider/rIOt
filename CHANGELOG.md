# Changelog

All notable changes to rIOt are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versions correspond to git tags. See [Releases](https://github.com/rbretschneider/rIOt/releases) for binaries and full release notes.

---

## [Unreleased]

### Changed

- [SEC-001] The Security page is now the primary location for security posture data. The per-device security score column has moved from Fleet Overview to the Security page. Fleet Overview no longer shows security scores.
- [SEC-001] `GET /api/v1/security/devices` response extended with three new fields: `pending_security_count` (int), `unattended_upgrades` (bool or null), `certs_expiring_soon` (int). Existing consumers that ignore unknown fields are unaffected.
- [SEC-001] `GET /api/v1/security/overview` response extended with two new fields: `certs_expiring_soon` (int), `total_certs` (int).

### Added

- [SEC-001] Security page now displays a per-device "Sec. Updates" column showing the count of pending security-classified package updates; amber/red when greater than zero.
- [SEC-001] Security page now displays a per-device "Auto-Updates" column showing whether unattended OS upgrades are enabled; green for enabled, amber for disabled, dash when no update telemetry is available.
- [SEC-001] Security page now displays a fleet score banner (arithmetic mean of all per-device scores) above the device table, color-coded by letter grade.
- [SEC-001] Security page now displays a "Certs Expiring" overview card showing the count of SSL/TLS certificates expiring within 30 days across the fleet; only shown when at least one device reports web server certificate data. Requires the `webservers` collector to be enabled on at least one device.
- [SEC-001] Security page table is now sortable by Score (default, ascending — worst first), Failed Logins, and Sec. Updates columns.
- [SEC-001] New shared utility module `web/src/utils/security.ts` exporting `gradeColor`, `gradeStrokeColor`, and `gradeFromScore` for consistent score display across the dashboard.
- [SEC-001] `MiniScore` component extracted to `web/src/components/MiniScore.tsx` for reuse.

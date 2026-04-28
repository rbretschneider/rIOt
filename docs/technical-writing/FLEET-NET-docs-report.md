# FLEET-NET Documentation Report

## Files Touched

### `README.md`

- **Per-device performance charts** section rewritten to describe the new two-chart card layout (top: 4-line percent chart; bottom: 2-line network bytes/sec chart). Cyan/orange palette for In/Out. Auto-scaling axis. Loopback excluded.
- **v1 limitations** list updated:
  - Removed the "Network throughput line deferred" bullet (no longer accurate — FLEET-NET ships the line).
  - Added "Network is host-summed, not per-interface" bullet (real v1 limitation worth surfacing).
  - Disk-line bullet preserved unchanged.
  - 60-min window bullet preserved unchanged.

### `CHANGELOG.md`

- Added two FLEET-NET entries under `### Added`:
  - Heartbeat schema extension with field names, exclusion semantics, rollover behavior, backward compat guarantees, and dashboard sub-chart description.
  - Frontend hardening note for `finiteOrZero` defensive coercion (SEC-FLEET-NET-001).
- Removed the "Per-device cards do not yet include a network throughput line" bullet from `### Known Limitations (v1)` (no longer accurate).
- Replaced it with a "Network throughput is host-summed only" bullet (real v1 limitation).

### Inline code comments

Audited new code for "what" comments. None added. Existing comments are all "why":
- `internal/agent/heartbeat.go:23-25` documents why name-based loopback detection (avoids syscall) — appropriate.
- `internal/agent/heartbeat.go:27-33` documents `computeNetRates` semantics — appropriate.
- `web/src/hooks/useFleetMetrics.ts:40-49` documents `finiteOrZero` — appropriate (security reference).

## API Documentation

No new endpoints. The `/api/v1/fleet/heartbeats` response shape gains the two new fields automatically once agents are upgraded — no separate API doc update required.

## Stale Content Found

None outside the FLEET-NET scope.

## Documentation Status

All documentation artifacts current. No follow-up doc work required.

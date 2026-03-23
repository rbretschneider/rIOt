# Implementation Report

- **Story ID:** POOL-002
- **Title:** Expand Storage Pool Detection for Unraid, mdraid, and LVM
- **Engineer:** Senior Dev Agent
- **Date:** 2026-03-23

---

## Detected Stack

| Layer | Technology |
|-------|-----------|
| Go module | `github.com/DesyncTheThird/rIOt` (go.mod) |
| Go version | 1.23 |
| Frontend | React + TypeScript + Tailwind CSS (Vite) |
| Test runner (Go) | `go test ./...` |
| Test runner (Frontend) | Vitest via `npm run test:run` |

---

## Completed Components

| File | Action | Notes |
|------|--------|-------|
| `internal/models/telemetry.go` | Modified | Added `strings` import; added `shfs`, `fuse.shfs` to `PoolFSTypes`; removed `IsPoolFSType()`; added `IsPoolFilesystem(fsType, device string) bool` with fs-type check, mdraid prefix, device-mapper prefix with exclusions, and dm- prefix |
| `internal/agent/collectors/disk.go` | Modified | Updated call site from `models.IsPoolFSType(p.Fstype)` to `models.IsPoolFilesystem(p.Fstype, p.Device)` |
| `internal/models/telemetry_pool_test.go` | Rewritten | Removed all `IsPoolFSType` references; rewrote with `IsPoolFilesystem` tests covering all 10 Go-side ACs plus edge cases |
| `web/src/utils/filesystem.ts` | Modified | Added `shfs`, `fuse.shfs` to `POOL_FS_TYPES`; updated `isPoolFilesystem()` fallback with device-path detection (mdraid, LVM, Docker exclusion, live-boot exclusion); updated doc comments |
| `web/src/utils/filesystem.test.ts` | Modified | Added tests for AC-001, AC-002, AC-011, AC-012, AC-013 and related exclusion cases; updated POOL_FS_TYPES coverage assertion |
| `web/src/pages/DeviceDetail.tsx` | Modified | Added device path display line on pool cards between the mount point/fs_type header row and the GaugeBar |

---

## Test Summary

### AC Mapping

| AC ID | Test File | Test Name | Status |
|-------|-----------|-----------|--------|
| AC-001 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC001_ShfsIsPool` | PASS |
| AC-002 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC002_FuseShfsIsPool` | PASS |
| AC-003 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC003_MdraidExt4IsPool` | PASS |
| AC-004 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC004_MdraidMultiDigitIsPool` | PASS |
| AC-005 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC005_LvmMapperIsPool` | PASS |
| AC-006 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC006_LvmDmIsPool` | PASS |
| AC-007 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC007_DockerMapperExcluded` | PASS |
| AC-008 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC008_LiveBootExcluded` | PASS |
| AC-009 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC009_RegularExt4NotPool` | PASS |
| AC-010 | `telemetry_pool_test.go` | `TestIsPoolFilesystem_AC010_ExistingPoolTypesDetected` | PASS |
| AC-011 | `filesystem.test.ts` | `[AC-011] Frontend fallback for old agents -- fuse.shfs fs_type` | PASS |
| AC-012 | `filesystem.test.ts` | `[AC-012] Frontend fallback for old agents -- mdraid device path` | PASS |
| AC-013 | `filesystem.test.ts` | `[AC-013] Frontend fallback for old agents -- LVM device path` | PASS |
| AC-014 | `web/src/pages/DeviceDetail.tsx` | Rendering change (manual/visual) — device path rendered conditionally in JSX | N/A (no component test for DeviceDetail, per ADD Section 8 note) |

### Go Test Run Output

```
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/agent	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.316s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	(cached)
?   	github.com/DesyncTheThird/rIOt/internal/server/db	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	(cached)
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	(cached)
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	(cached)
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
```

### Frontend Test Run Output

```
 RUN  v4.0.18 D:/Repos/rIOt/web

 ✓ src/utils/security.test.ts (18 tests) 11ms
 ✓ src/utils/filesystem.test.ts (39 tests) 18ms
 ✓ src/api/client.test.ts (9 tests) 45ms
 ✓ src/components/StatusBadge.test.tsx (3 tests) 38ms
 ✓ src/utils/cron.test.ts (24 tests) 43ms
 ✓ src/components/GaugeBar.test.tsx (5 tests) 51ms
 ✓ src/components/ConfirmModal.test.tsx (6 tests) 148ms
 ✓ src/pages/FleetOverview.test.tsx (4 tests) 185ms
 ✓ src/components/ActivityLog.test.tsx (9 tests) 827ms
 ✓ src/pages/Security.test.tsx (23 tests) 978ms
 ✓ src/pages/DeviceDetail.test.tsx (12 tests) 429ms
 ✓ src/pages/Probes.test.tsx (29 tests) 1315ms

 Test Files  12 passed (12)
       Tests  181 passed (181)
    Start at  15:35:35
    Duration  3.24s
```

Note: `ECONNREFUSED` stderr output in the frontend test run is pre-existing noise from tests that attempt real HTTP connections (ActivityLog, Security, etc.) — these tests pass and this output predates POOL-002.

---

## Deviations from ADD

None. All changes match the ADD specification exactly:

- `IsPoolFSType` removed, `IsPoolFilesystem(fsType, device string) bool` added.
- `PoolFSTypes` updated with `fuse.shfs` and `shfs` in alphabetical order.
- Device-path detection order matches ADD Section 5 exactly (md, mapper with exclusions, dm-).
- Frontend `isPoolFilesystem()` fallback mirrors Go logic precisely.
- Pool card device path rendered conditionally (`{fs.device && ...}`) to avoid rendering an empty span when device is an empty string, which is a safe hardening not contradicted by the ADD.

---

## Notes for QA

- **AC-014 (pool card device path):** The device path is rendered only when `fs.device` is non-empty (`{fs.device && <span ...>{fs.device}</span>}`). For ZFS pools that use non-device paths like `tank/data` this still renders correctly. For the demo NAS device (btrfs at `/volume1` with device `/dev/md0`), the path will display on the pool card.
- **Backward compatibility:** The `is_pool` field remains `omitempty`. Pre-POOL-002 agents send no `is_pool` field; the frontend fallback handles those via `fs.is_pool === undefined` path.
- **Docker exclusion scope:** Only `/dev/mapper/docker-*` is excluded. The `/dev/dm-*` prefix has no Docker-specific exclusion because Docker thin-pool dm- devices are indistinguishable from LVM by path alone. This is explicitly documented in ADD Section 3 (AD-002) and is correct per design.
- **Live-boot exclusions:** Exact string match for `/dev/mapper/live-rw` and `/dev/mapper/live-base`. Any other `/dev/mapper/live-*` variants (e.g., `live-osimg`) would be classified as pools — this is the intended behavior per the FRD exclusion list which only names these two.
- **Agent config:** No new collector name was added. The `disk` collector already exists and is in every device's whitelist. No agent.yaml changes are required.

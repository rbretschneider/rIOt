# Implementation Report

- **Story ID:** POOL-001
- **Title:** Storage Pool Filesystem Visual Identification and Presentation
- **Engineer:** Senior Dev Agent
- **Date:** 2026-03-23

---

## Detected Stack

- **Backend:** Go 1.23, chi v5 router, pgx v5, slog
- **Frontend:** React 18, TypeScript, Tailwind CSS, Vite, Vitest, React Query
- **Test runners:** `go test ./...` (Go), `vitest run` (frontend)

---

## Completed Components

| File | Action | Notes |
|------|--------|-------|
| `internal/models/telemetry.go` | MODIFIED | Added `IsPool bool` field with `json:"is_pool,omitempty"` tag to `Filesystem` struct; added `PoolFSTypes` var and `IsPoolFSType()` helper immediately after the struct |
| `internal/agent/collectors/disk.go` | MODIFIED | Added `IsPool: models.IsPoolFSType(p.Fstype)` field assignment in filesystem construction, following the same pattern as `IsNetworkMount` |
| `internal/models/telemetry_pool_test.go` | CREATED | Unit tests for `IsPoolFSType()` covering all 6 pool types, 5 non-pool types, and empty string |
| `web/src/types/models.ts` | MODIFIED | Added `is_pool?: boolean` to `Filesystem` interface as optional field with explanatory comment |
| `web/src/utils/filesystem.ts` | CREATED | Exports `POOL_FS_TYPES`, `isPoolFilesystem()`, and `formatCapacity()`; includes cross-reference comment to Go authoritative list |
| `web/src/utils/filesystem.test.ts` | CREATED | 24 unit tests covering AC-001, AC-002, AC-006, AC-007, AC-008 |
| `web/src/pages/DeviceDetail.tsx` | MODIFIED | Added import of `isPoolFilesystem` and `formatCapacity`; refactored Filesystems section to partition into pool cards (with GaugeBar) and regular filesystem table |
| `web/src/api/demo-data.ts` | MODIFIED | Added `is_pool: true` to the btrfs `/volume1` entry for `nas-synology` demo device |

---

## Test Summary

### AC Mapping

| AC ID | Test File | Tests | Status |
|-------|-----------|-------|--------|
| AC-001 | `internal/models/telemetry_pool_test.go` | `TestIsPoolFSType/[AC-001] mergerfs filesystem is classified as a pool`, `TestIsPoolFSType/[AC-001] All pool filesystem types return true` | PASS |
| AC-001 | `web/src/utils/filesystem.test.ts` | `[AC-001] mergerfs filesystem is classified as a pool` (2 tests) | PASS |
| AC-002 | `internal/models/telemetry_pool_test.go` | `TestIsPoolFSType/[AC-002] Regular filesystem ext4 is not classified as a pool`, `TestIsPoolFSType/[AC-002] Regular filesystem types are not classified as pools`, `TestIsPoolFSType/[AC-002] Empty string is not classified as a pool` | PASS |
| AC-002 | `web/src/utils/filesystem.test.ts` | `[AC-002] Regular filesystem is not classified as a pool` (3 tests) | PASS |
| AC-003 | Visual / QA validation | Pool cards render above table; GaugeBar and all fields displayed; pool excluded from table | Structural implementation confirmed |
| AC-004 | Visual / QA validation | `poolFilesystems.length > 0` guard ensures subsection not rendered when empty | Structural implementation confirmed |
| AC-005 | `web/src/components/GaugeBar.test.tsx` | Existing GaugeBar tests cover color thresholds (>75% amber, >90% red) | PASS (pre-existing) |
| AC-006 | `web/src/utils/filesystem.test.ts` | `[AC-006] Large capacity values display in TB` (8 tests including 14000 GB = 13.67 TB) | PASS |
| AC-007 | `web/src/utils/filesystem.test.ts` | `[AC-007] Backward compatibility with old agents` (5 tests) | PASS |
| AC-008 | `internal/models/telemetry_pool_test.go` | `TestIsPoolFSType/[AC-008] ZFS filesystem is classified as a pool`, `TestIsPoolFSType/[AC-008] Btrfs filesystem is classified as a pool` | PASS |
| AC-008 | `web/src/utils/filesystem.test.ts` | `[AC-008] ZFS and Btrfs filesystems are classified as pools` (6 tests) | PASS |

### Test Run Output

**Go tests (`go test ./...`):**
```
?       github.com/DesyncTheThird/rIOt/cmd/riot-agent           [no test files]
?       github.com/DesyncTheThird/rIOt/cmd/riot-server          [no test files]
ok      github.com/DesyncTheThird/rIOt/internal/agent           (cached)
ok      github.com/DesyncTheThird/rIOt/internal/agent/collectors (cached)
ok      github.com/DesyncTheThird/rIOt/internal/models          0.386s
ok      github.com/DesyncTheThird/rIOt/internal/resilient       (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server          (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server/auth     (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server/ca       (cached)
?       github.com/DesyncTheThird/rIOt/internal/server/db       [no test files]
ok      github.com/DesyncTheThird/rIOt/internal/server/events   (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server/handlers  0.512s
?       github.com/DesyncTheThird/rIOt/internal/server/logstore [no test files]
ok      github.com/DesyncTheThird/rIOt/internal/server/middleware (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server/notify   (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server/probes   (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server/scoring  (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server/updates  (cached)
ok      github.com/DesyncTheThird/rIOt/internal/server/websocket (cached)
?       github.com/DesyncTheThird/rIOt/internal/testutil        [no test files]
```

**Frontend tests (`npm run test:run`):**
```
Test Files  12 passed (12)
      Tests 161 passed (161)
   Start at  10:17:08
   Duration  3.24s
```
Baseline before this story: 11 files / 137 tests. New: 1 file added (filesystem.test.ts), 24 new tests. No regressions.

---

## Deviations from ADD

None. All component changes were implemented exactly as specified in ADD Section 4. No new dependencies were introduced.

---

## Notes for QA

### Test Data
- The demo device `nas-synology` now shows a Storage Pools subsection with a single btrfs pool at `/volume1` (14400 GB total, 8640 GB used, 5760 GB free, 60% usage). The capacity will display as TB: "8.44 TB used of 14.06 TB (5.63 TB free)".
- All other demo devices have only regular ext4 filesystems; the Storage Pools subsection must not appear for them.

### Backward Compatibility
- The `is_pool` field uses `omitempty` on the Go side, so agents that have not been updated will not transmit this field. The frontend `isPoolFilesystem()` function falls back to `fs_type` classification in that case.
- Existing stored telemetry without `is_pool` in the JSONB column will deserialize with `is_pool` as `undefined` (TypeScript) or `false` (Go zero value) — both are handled correctly.

### Edge Cases to Probe
- A device with ONLY pool filesystems (no regular filesystems): pool cards should render, the regular table should be absent.
- A device with ONLY regular filesystems: no Storage Pools subsection should appear, regular table renders as before.
- A device with a mix: pools appear above the table, pool entries excluded from the table.
- Single-disk btrfs root partition will appear in Storage Pools section per BR-003 (this is expected behavior, not a bug).
- The `formatCapacity(1000)` boundary: 1000 GB = 0.98 TB (1000/1024 = 0.976... rounds to 0.98). QA should confirm this displays correctly.

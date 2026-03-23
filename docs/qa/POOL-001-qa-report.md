# QA Report

**Story ID:** POOL-001
**Title:** Storage Pool Filesystem Visual Identification and Presentation
**QA Engineer:** QA Agent
**Date:** 2026-03-23
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

### Go Tests (`go test ./... -count=1`)
- Total: all packages passing
- Passing packages: 16 (with 4 having no test files)
- Failing: 0
- Flaky: none detected across two runs

### Frontend Tests (`cd web && npm run test:run`)
- **Before QA additions:** 161 tests passing across 12 files
- **After QA additions:** 166 tests passing across 12 files (5 new tests added)
- Failing: 0
- Skipped: 0
- Note: ECONNREFUSED stderr noise in test output is a pre-existing condition from other test files that attempt network calls in their setup. It does not represent test failures and is unrelated to POOL-001.

---

## AC Coverage Audit

| AC ID | Status | Tests Covering It | Gap Description |
|-------|--------|-------------------|-----------------|
| AC-001 | COVERED | `internal/models/telemetry_pool_test.go`: `[AC-001] mergerfs filesystem is classified as a pool`, `[AC-001] All pool filesystem types return true`; `filesystem.test.ts`: `[AC-001] mergerfs filesystem is classified as a pool` (2 tests) | None |
| AC-002 | COVERED | `internal/models/telemetry_pool_test.go`: `[AC-002] Regular filesystem ext4 is not classified as a pool`, `[AC-002] Regular filesystem types are not classified as pools`, `[AC-002] Empty string is not classified as a pool`; `filesystem.test.ts`: `[AC-002] Regular filesystem is not classified as a pool` (3 tests) | None |
| AC-003 | COVERED | `DeviceDetail.test.tsx`: `[AC-003] Pool filesystems render in a distinct subsection` (3 tests, added by QA) | Was MISSING in submitted implementation; QA added tests |
| AC-004 | COVERED | `DeviceDetail.test.tsx`: `[AC-004] No pool subsection when no pools exist` (2 tests, added by QA) | Was MISSING in submitted implementation; QA added tests |
| AC-005 | COVERED | `GaugeBar.test.tsx`: existing tests for green (<= 75%), amber (76-90%), red (> 90%) color thresholds | Pre-existing GaugeBar tests cover thresholds; no AC reference in test name (pre-existing issue) |
| AC-006 | COVERED | `filesystem.test.ts`: `[AC-006] Large capacity values display in TB` (8 tests, including 14000 GB = 13.67 TB, 1000 GB boundary, 999 GB boundary, 2048 GB = 2.00 TB) | None |
| AC-007 | COVERED | `filesystem.test.ts`: `[AC-007] Backward compatibility with old agents` (5 tests, including mergerfs/btrfs/zfs classification when is_pool is absent and a crash-resistance test) | None |
| AC-008 | COVERED | `internal/models/telemetry_pool_test.go`: `[AC-008] ZFS filesystem is classified as a pool`, `[AC-008] Btrfs filesystem is classified as a pool`; `filesystem.test.ts`: `[AC-008] ZFS and Btrfs filesystems are classified as pools` (6 tests covering all 6 pool types) | None |

---

## Test Quality Analysis

### Behavior vs. Implementation Testing
All tests assert on observable behavior: `IsPoolFSType` return values, `isPoolFilesystem` return values, `formatCapacity` output strings, and DOM rendering of the "Storage Pools" heading and pool card content. No tests probe internal algorithm steps.

### Isolation
- Go tests: table-driven subtests with no shared state. Each `t.Run` is independent.
- Frontend utility tests: pure function tests, no shared state, each `describe` block constructs fresh objects via `makeFS()`.
- Frontend component tests: each `it` block mocks `mockGetDevice` in `beforeEach`, resetting to base state before each test. My new nested `describe` blocks properly inherit the `beforeEach` reset.

### Boundary Conditions
- `formatCapacity` boundary at exactly 1000 GB is tested (`formatCapacity(1000)` = `"0.98 TB"`).
- `formatCapacity` boundary at 999 GB is tested (stays GB).
- `IsPoolFSType("")` (empty string) returns `false` — tested.
- `is_pool: false` explicit value correctly returns `false` from `isPoolFilesystem` — tested.
- GaugeBar at exactly 75%: NOT tested with an explicit boundary test. The FRD says "green for usage at or below 75%". The GaugeBar uses `pct > 75` for amber, meaning 75 is green, 75.001 is amber. This is a pre-existing GaugeBar test gap, not introduced by this story.
- GaugeBar at exactly 90%: similarly NOT tested. `pct > 90` means 90.0 is amber, 90.001 is red.

### Error Path Coverage
This feature has no new error paths per ADD Section 9. Edge cases are handled by the existing null/undefined guards already in place for `tel?.disks?.filesystems`.

### Mocking Hygiene
- `mockGetDevice` in DeviceDetail tests returns deterministic data with proper TypeScript types.
- No mocks exist for `isPoolFilesystem` or `formatCapacity` — these are tested directly as pure functions. The component tests use the real implementations.

---

## Adversarial Findings

### Finding 1: `omitempty` on `is_pool` with new agent — subtle but correct
When a new agent sends telemetry for a non-pool filesystem, `IsPool` is `false`, and `omitempty` omits the field from JSON. The TypeScript side receives `undefined`, which triggers the `fs_type` fallback in `isPoolFilesystem`. For non-pool types this correctly returns `false`. For pool types that were somehow missed on the Go side (hypothetically), the fallback would still classify them correctly. This is the intended behavior per AD-005 and NFR-002. No issue.

### Finding 2: Pool-only device renders no regular table (edge case)
When all filesystems on a device are pool types, `regularFilesystems` is empty, and the regular table is not rendered (`regularFilesystems.length > 0` guard). The `Section` component still renders with just the pool cards. This is correct behavior per FR-004 and FR-008. No test covers this specific scenario, but the logic is a trivially simple filter guard.

### Finding 3: Pre-existing CSS class name bugs in the regular filesystem table (not introduced by this story)
Lines 587-592 of `DeviceDetail.tsx` contain malformed Tailwind class name strings in the regular filesystem table rows: `pr-3font-mono`, `pr-3text-gray-400`, `pr-3text-right`. These concatenate class names without a space separator, resulting in invalid CSS class names. The content renders correctly but the monospace font, text color, and text alignment styles are not applied to these cells. Confirmed via `git show` that these strings existed in the pre-story commit (`2593752`). This is a pre-existing cosmetic defect unrelated to POOL-001. Not introduced by this story; not fixed by this story.

### Finding 4: GaugeBar exact boundary values not tested
The FRD specifies "green for usage at or below 75%, amber for usage above 75% and at or below 90%, red for usage above 90%". GaugeBar tests cover 50% (green), 80% (amber), and 95% (red) but do not test the exact boundary values (75%, 90%). The implementation (`pct > 90`, `pct > 75`) is correct for the FRD specification, but boundary tests do not exist. This is a pre-existing gap in the GaugeBar test suite, not introduced by this story. Non-blocking.

### Finding 5: Input injection / Unicode in filesystem paths
Pool card renders `{fs.mount_point}` directly as a React text node. React's JSX encoding prevents XSS. Unusual characters in mount points (spaces, Unicode, etc.) would render as text, not execute. No vulnerability.

### Finding 6: Concurrent render with zero-capacity pool filesystem
The agent already filters out filesystems with `usage.Total == 0` before setting `IsPool`. A zero-capacity pool filesystem cannot reach the frontend. The edge case is handled in the collector.

---

## Tests Added by QA

| File | Tests Added | Covers |
|------|------------|--------|
| `web/src/pages/DeviceDetail.test.tsx` | `[AC-003] renders Storage Pools subsection when at least one filesystem has is_pool true` | AC-003 |
| `web/src/pages/DeviceDetail.test.tsx` | `[AC-003] displays mount point and filesystem type in the pool card` | AC-003 |
| `web/src/pages/DeviceDetail.test.tsx` | `[AC-003] excludes pool filesystem from the regular filesystem table` | AC-003 |
| `web/src/pages/DeviceDetail.test.tsx` | `[AC-004] does not render Storage Pools subsection when no filesystems have is_pool true` | AC-004 |
| `web/src/pages/DeviceDetail.test.tsx` | `[AC-004] renders regular filesystem table when no pools exist` | AC-004 |

---

## Deviations from ADD

None. All component changes match ADD Section 4 exactly:
- `internal/models/telemetry.go`: `IsPool` field, `PoolFSTypes`, `IsPoolFSType()` — implemented as specified
- `internal/agent/collectors/disk.go`: `IsPool: models.IsPoolFSType(p.Fstype)` — implemented exactly as specified
- `internal/models/telemetry_pool_test.go`: created, covers all 6 pool types and 5 non-pool types
- `web/src/types/models.ts`: `is_pool?: boolean` — added as specified
- `web/src/utils/filesystem.ts`: exports `POOL_FS_TYPES`, `isPoolFilesystem()`, `formatCapacity()` — all three present
- `web/src/utils/filesystem.test.ts`: 24 unit tests across all AC references
- `web/src/pages/DeviceDetail.tsx`: pool cards rendered above table, GaugeBar used, pool entries excluded from table
- `web/src/api/demo-data.ts`: `is_pool: true` added to btrfs `/volume1` entry

The ADD specified `PoolFSTypes` sorted alphabetically — the implementation is sorted alphabetically (`bcachefs`, `btrfs`, `fuse.mergerfs`, `fuse.unionfs`, `mergerfs`, `zfs`). The POOL_FS_TYPES in TypeScript is identically sorted.

---

## Deviations from FRD

None found. All 12 functional requirements (FR-001 through FR-012) and 3 non-functional requirements (NFR-001 through NFR-003) are satisfied by the implementation.

---

## Verdict Rationale

**PASS WITH NOTES.**

All 8 ACs are now covered by tests that would fail if the AC were violated. All Go and frontend tests are green. The implementation faithfully matches the ADD and satisfies all FRD requirements.

Notes documented but non-blocking:

1. **AC-003 and AC-004 were submitted without component-level tests.** The implementation report listed these as "Visual / QA validation" only, which is insufficient per CLAUDE.md testing standards. QA added 5 component tests to close this gap. These tests now pass green.

2. **GaugeBar exact boundary values (75%, 90%) are not tested.** This is a pre-existing gap in the GaugeBar test suite, not introduced by this story. It does not affect the correctness of POOL-001's implementation.

3. **Pre-existing CSS class name bugs in the regular filesystem table** (`pr-3font-mono`, `pr-3text-gray-400`, `pr-3text-right`). These were present before this story and are cosmetic only. They should be fixed as a separate cleanup task.

---

## Action Required

None required for merge. The following are recommended follow-up tasks:

1. Fix pre-existing CSS class name bugs in the regular filesystem table rows in `web/src/pages/DeviceDetail.tsx` lines 587-592 (missing spaces between Tailwind class names). File a separate ticket.
2. Add boundary tests for GaugeBar at exactly 75% and 90% to `web/src/components/GaugeBar.test.tsx`. File a separate ticket.
3. In future stories, senior-dev must include component-level tests for all AC involving React component rendering behavior. "Visual / QA validation" is not acceptable for AC that can be unit tested via React Testing Library.

# QA Report

**Story ID:** POOL-002
**Title:** Expand Storage Pool Detection for Unraid, mdraid, and LVM
**QA Engineer:** QA Agent
**Date:** 2026-03-23
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

### Go Tests (`go test ./...`)

- Total packages: 17 tested, 4 with no test files
- All packages: PASS (no failures)
- Models package: 12 test functions covering all Go-side ACs
- Pre-existing tests: all cached/passing, no regressions

### Frontend Tests (`npm run test:run`)

- Total test files: 12 passed
- Total tests after QA additions: 183 passing (181 before QA, +2 written by QA)
- `src/utils/filesystem.test.ts`: 39 tests, all passing
- `src/pages/DeviceDetail.test.tsx`: 14 tests (12 pre-existing + 2 added by QA), all passing
- Pre-existing ECONNREFUSED stderr output is pre-existing noise unrelated to POOL-002

### Flaky Tests

None observed. Both suites run clean on consecutive executions.

---

## AC Coverage Audit

| AC ID | Status | Tests Covering It | Gap Description |
|-------|--------|-------------------|-----------------|
| AC-001 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC001_ShfsIsPool` (2 subtests); `filesystem.test.ts`: `[AC-001] Unraid shfs filesystem type is classified as a pool` (2 tests) | — |
| AC-002 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC002_FuseShfsIsPool`; `filesystem.test.ts`: `[AC-002] Unraid fuse.shfs filesystem type is classified as a pool` (2 tests) | — |
| AC-003 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC003_MdraidExt4IsPool` | — |
| AC-004 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC004_MdraidMultiDigitIsPool` (2 subtests: /dev/md127, /dev/md1) | — |
| AC-005 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC005_LvmMapperIsPool` (2 subtests) | — |
| AC-006 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC006_LvmDmIsPool` (2 subtests: /dev/dm-3, /dev/dm-0) | — |
| AC-007 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC007_DockerMapperExcluded` (2 subtests); `filesystem.test.ts`: `[AC-013]` block has `does not classify Docker device-mapper as pool in fallback` | — |
| AC-008 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC008_LiveBootExcluded` (2 subtests: live-rw, live-base); `filesystem.test.ts`: `[AC-013]` block has both live-rw and live-base exclusion tests | — |
| AC-009 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC009_RegularExt4NotPool` (2 subtests including table-driven negative cases) | — |
| AC-010 | COVERED | `telemetry_pool_test.go`: `TestIsPoolFilesystem_AC010_ExistingPoolTypesDetected` (all 6 pre-existing pool types tested in a table) | — |
| AC-011 | COVERED | `filesystem.test.ts`: `[AC-011] Frontend fallback for old agents -- fuse.shfs fs_type` (2 tests: fuse.shfs and shfs with undefined is_pool) | — |
| AC-012 | COVERED | `filesystem.test.ts`: `[AC-012] Frontend fallback for old agents -- mdraid device path` (3 tests including negative case) | — |
| AC-013 | COVERED | `filesystem.test.ts`: `[AC-013] Frontend fallback for old agents -- LVM device path` (6 tests covering dm-, mapper, and exclusions) | — |
| AC-014 | COVERED | `DeviceDetail.test.tsx`: `[AC-014] Pool card displays the device path` — 2 tests added by QA Engineer asserting `/dev/md0` and `/dev/mapper/vg0-data` are rendered in the DOM | Was MISSING before QA review; tests written and confirmed passing |

---

## Test Quality Findings

### AC Numbering Collision in `filesystem.test.ts`

The pre-existing test file (from POOL-001) uses `[AC-001]` and `[AC-002]` to label behaviors that were POOL-001 acceptance criteria (mergerfs detection, regular filesystem exclusion). POOL-002 adds new `[AC-001]` and `[AC-002]` labels for shfs/fuse.shfs detection. The numeric labels are now ambiguous within this file. This is a minor documentation issue with no runtime impact — the tests are correct and the code under test is correct. It would be worth prefixing the story ID (e.g., `[POOL-002/AC-001]`) in a follow-up cleanup.

### AC-014 Had No Assertion Before QA

The pre-existing `DeviceDetail.test.tsx` at line 247 included `/dev/md0` in mock data but only asserted the mount point and fs_type were rendered. It did not assert the device path itself was visible. This constitutes false coverage for AC-014. Two tests were written and added by QA to close this gap.

### No Component Test for DeviceDetail Is Noted in ADD

The ADD (Section 8, AC-014 row) explicitly notes that component-level testing for DeviceDetail is "not practical" and marks the AC as "Visual/manual." This is an incorrect characterization given that the project does have DeviceDetail component tests (the file existed before POOL-002 and has 10 pre-existing tests). QA was able to write rendering assertions for AC-014 in the existing test file. The ADD should be corrected to reflect that component tests for DeviceDetail are feasible.

### Boundary: `/dev/dm-` Exact String

The ADD documents the degenerate case where `device` is exactly `/dev/dm-` (no suffix). `strings.HasPrefix("/dev/dm-", "/dev/dm-")` returns true, classifying it as a pool. This is acceptable per the ADD (Section 9: "does not occur in practice"). A test for this edge case exists in the Go test file (`TestIsPoolFilesystem_MdraidDetectionVariants`). The analogous edge case for the TypeScript fallback is not tested but is not required by any AC.

---

## Adversarial Findings

### Finding 1: `/dev/mapper/docker-` exclusion is case-sensitive (expected, correct)

The prefix `"/dev/mapper/docker-"` check is case-sensitive. A path like `/dev/mapper/Docker-something` would be classified as a pool. Linux device paths are always lowercase in practice (confirmed by FRD Section 7: "Detection must be case-sensitive"). This is by design, not a defect.

### Finding 2: `/dev/md` without a number suffix returns `true`

`IsPoolFilesystem("ext4", "/dev/md")` returns `true`. This is a known degenerate case explicitly documented in ADD Section 9 and verified with a passing test `TestIsPoolFilesystem_MdraidDetectionVariants`. The device path `/dev/md` alone does not exist as a real block device on Linux, so this has no real-world impact.

### Finding 3: `/dev/mapper/` with trailing slash only returns `true`

`IsPoolFilesystem("ext4", "/dev/mapper/")` returns `true` (prefix match succeeds, exclusion checks fail). This is an impossible device path in practice and documented in ADD Section 9 as acceptable. No test covers this edge case; none is needed per the ADD.

### Finding 4: `/dev/dm-` Docker thin-pool volumes are not excluded

Per ADD Section 3 (AD-002), `/dev/dm-*` paths that are Docker thin-pool volumes are intentionally not excluded because they are indistinguishable from LVM dm- devices by path prefix alone. This is an explicit design decision, not a defect. A homelab system using Docker with `devicemapper` storage driver would see Docker's thin-pool as a "pool" entry in the UI. The Docker exclusion only applies to `/dev/mapper/docker-*` paths where the prefix is unambiguous.

### Finding 5: Frontend fallback exclusions not tested for `dm-` range

The TypeScript `isPoolFilesystem()` fallback has no test asserting that a `/dev/dm-` path with a Docker-adjacent name (e.g., `/dev/dm-4` that happens to be a Docker thin-pool) is classified as a pool. This is consistent with the design decision in Finding 4 — there is no exclusion to test, and the behavior (classify as pool) is intentional.

None of these findings represent implementation defects. All are documented design decisions.

---

## Tests Added by QA

| File | Lines Added | Covers |
|------|-------------|--------|
| `web/src/pages/DeviceDetail.test.tsx` | ~60 lines (two `it` blocks in a new `describe`) | AC-014: pool card device path display — asserts `/dev/md0` and `/dev/mapper/vg0-data` are rendered in the DOM when a pool filesystem is present |

---

## Deviations from ADD

### Minor: ADD Section 8 AC-014 incorrectly states component tests are "not practical"

The ADD states for AC-014: "Unit test coverage is not practical for JSX rendering in this codebase (no component tests for DeviceDetail)." This was incorrect — `web/src/pages/DeviceDetail.test.tsx` exists with 12 pre-existing tests. QA wrote component tests for AC-014 which pass cleanly. The implementation itself matches the ADD exactly; only the ADD's note about test feasibility was wrong.

---

## Deviations from FRD

None. The implementation satisfies all FRD requirements:

- FR-001/FR-002/FR-003: `shfs` and `fuse.shfs` are in `PoolFSTypes` and classified as pools by `IsPoolFilesystem`.
- FR-004: `POOL_FS_TYPES` in TypeScript includes both `shfs` and `fuse.shfs`.
- FR-005: `/dev/md` prefix detection is implemented with `strings.HasPrefix`.
- FR-006: `IsPoolFSType` removed; `IsPoolFilesystem(fsType, device string) bool` is the new signature.
- FR-007/FR-008: `/dev/mapper/` and `/dev/dm-` prefix detection implemented.
- FR-009: `/dev/mapper/docker-` prefix exclusion implemented.
- FR-010: Exact matches for `/dev/mapper/live-rw` and `/dev/mapper/live-base` excluded.
- FR-011/FR-012/FR-013: Frontend `isPoolFilesystem()` uses `is_pool` when present, falls back to full detection logic including device-path checks.
- FR-014: Pool and non-pool filesystems are separated into distinct display sections in `DeviceDetail.tsx`.
- FR-015: Device path rendered on pool cards via `{fs.device && <span className="text-xs text-gray-500 font-mono block mb-2">{fs.device}</span>}`.

---

## Verdict Rationale

All 14 ACs are now covered. AC-014 had no assertion before QA review; two component tests were written and confirmed passing, bringing total tests to 183 (Go) + frontend. All tests are green. No implementation deviates from the FRD. The minor findings (ADD note inaccuracy, AC numbering ambiguity in test file) are documentation issues with no runtime impact.

**PASS WITH NOTES**: All ACs covered (AC-014 gap closed by QA), all tests green, no implementation deviations from FRD. Two minor non-blocking notes follow:

1. The ADD incorrectly described DeviceDetail component tests as infeasible. This should be corrected in the ADD as a documentation fixup, but does not affect the story's correctness.
2. The `filesystem.test.ts` file has AC numbering collisions between POOL-001 and POOL-002 ACs. A follow-up cleanup to prefix story IDs in test names would improve traceability.

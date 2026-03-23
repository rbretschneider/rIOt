# Architecture Decision Document

- **Story ID:** POOL-002
- **FRD Reference:** docs/requirements/POOL-002-frd.md
- **Author:** Architect Agent
- **Date:** 2026-03-23
- **Status:** FINAL

---

## 1. Summary

Extend the storage pool detection system to recognize Unraid's `shfs`/`fuse.shfs` union filesystem by type, and mdraid (`/dev/md*`) and LVM (`/dev/mapper/*`, `/dev/dm-*`) volumes by device path. The existing `IsPoolFSType()` function is replaced by a broader `IsPoolFilesystem(fsType, device string) bool` that combines filesystem-type and device-path checks with exclusion rules for Docker device-mapper and live-boot paths. The frontend fallback mirror is updated to match, ensuring backward compatibility with pre-POOL-002 agents.

---

## 2. Technical Context

### Current State

- **Go models (`internal/models/telemetry.go`):** `PoolFSTypes` is a `[]string` containing 6 entries (bcachefs, btrfs, fuse.mergerfs, fuse.unionfs, mergerfs, zfs). `IsPoolFSType(fsType string) bool` performs a linear scan of this slice. The `Filesystem` struct has `Device string` and `IsPool bool` fields already present.
- **Agent (`internal/agent/collectors/disk.go`):** Line 62 calls `models.IsPoolFSType(p.Fstype)` to set `IsPool`. The device path (`p.Device`) is already available in scope but not used for pool classification.
- **Frontend (`web/src/utils/filesystem.ts`):** `POOL_FS_TYPES` mirrors the Go list (6 entries). `isPoolFilesystem()` checks `fs.is_pool` first, falls back to `POOL_FS_TYPES.includes(fs.fs_type)`. No device-path check exists.
- **Frontend rendering (`web/src/pages/DeviceDetail.tsx`):** Pool cards show mount point, fs_type badge, GaugeBar, and capacity text. The device path is **not** currently displayed on pool cards (it is shown in the regular filesystems table only).
- **TypeScript model (`web/src/types/models.ts`):** `Filesystem` interface has `device: string` already.
- **Existing tests:** `internal/models/telemetry_pool_test.go` tests `IsPoolFSType()` for all 6 pool types and several non-pool types. `web/src/utils/filesystem.test.ts` tests `isPoolFilesystem()` and `formatCapacity()`.
- **Demo data (`web/src/api/demo-data.ts`):** The nas-synology device has a btrfs filesystem at `/volume1` with device `/dev/md0` and `is_pool: true`.

### What Needs to Change

1. Add `shfs` and `fuse.shfs` to the `PoolFSTypes` list.
2. Replace `IsPoolFSType(fsType string) bool` with `IsPoolFilesystem(fsType, device string) bool` that checks both fs type and device path, including exclusions.
3. Keep `IsPoolFSType()` as a deprecated thin wrapper (or remove and update the single call site in `disk.go`).
4. Update `disk.go` to call the new function with both arguments.
5. Add `shfs` and `fuse.shfs` to the frontend `POOL_FS_TYPES` list.
6. Add device-path-based fallback detection to the frontend `isPoolFilesystem()`.
7. Add device path display to the pool card in DeviceDetail.tsx.
8. Update all tests to cover new types, device-path detection, and exclusions.

---

## 3. Architecture Decisions

### AD-001: Replace `IsPoolFSType` with `IsPoolFilesystem`

**Decision:** Rename and expand the detection function. The new signature is `IsPoolFilesystem(fsType, device string) bool`. The old `IsPoolFSType()` function is removed entirely. The single call site in `disk.go` is updated to pass both `p.Fstype` and `p.Device`.

**Rationale:** FR-006 requires the function to accept both filesystem type and device path. Since there is exactly one call site (`disk.go` line 62), a clean rename is simpler than maintaining two functions. The function name `IsPoolFilesystem` better describes what it does now that detection is multi-criteria.

**Alternatives Considered:**
- Adding a second function `IsPoolDevice(device string) bool` and OR-ing the results in the collector: Splits the detection logic across two functions and pushes the combination logic into the collector, violating NFR-004 (centralized detection).
- Keeping `IsPoolFSType` and adding `IsPoolFilesystem` that calls it: Adds an unnecessary layer of indirection for no benefit.

**Consequences:** The test file must be updated to test the new function signature. The old function name disappears from the codebase.

### AD-002: Device Path Detection via Prefix Matching with Exclusion List

**Decision:** Inside `IsPoolFilesystem`, after checking the fs type list, check device path prefixes in this order:
1. If device starts with `/dev/md` -- return `true` (mdraid).
2. If device starts with `/dev/mapper/` -- check exclusions first:
   - If device starts with `/dev/mapper/docker-` -- return `false`.
   - If device equals `/dev/mapper/live-rw` or `/dev/mapper/live-base` -- return `false`.
   - Otherwise -- return `true` (LVM/device-mapper).
3. If device starts with `/dev/dm-` -- return `true` (device-mapper kernel name).

The exclusion check for `/dev/dm-` Docker thin-pool volumes is intentionally not implemented. The FRD (FR-009) mentions `/dev/dm-` entries that are Docker thin-pool volumes, but these are indistinguishable from legitimate LVM dm- devices by path prefix alone. The Docker exclusion is applied only to `/dev/mapper/docker-*` paths where the prefix is unambiguous. This is consistent with BR-003's "unless it matches an exclusion pattern" language -- the exclusion patterns are defined by FR-009 and FR-010 as specific prefix/exact matches.

**Rationale:** Prefix matching is O(1) per check, uses only data already collected (`p.Device` from gopsutil), and requires no additional system calls (NFR-003). The exclusion list is short and specific.

**Alternatives Considered:**
- Regex matching: More powerful but slower and harder to read for simple prefix checks. Not warranted.
- Reading `/proc/mdstat` or running `lvs`: Violates NFR-003 (no additional system calls).

**Consequences:** Any future `/dev/mapper/` device not in the exclusion list will be classified as a pool. This is the correct default for homelab NAS systems per Assumption 3 in the FRD.

### AD-003: Frontend Fallback Mirrors Both Detection Methods

**Decision:** The frontend `isPoolFilesystem()` function is updated to mirror the full detection logic when `is_pool` is undefined:
1. Check `fs.fs_type` against `POOL_FS_TYPES` (now including `shfs`, `fuse.shfs`).
2. Check `fs.device` for mdraid prefix (`/dev/md`).
3. Check `fs.device` for LVM prefixes (`/dev/mapper/`, `/dev/dm-`) with the same exclusions.

When `is_pool` is defined (from an updated agent), use it directly as before.

**Rationale:** FR-012 and FR-013 require the frontend to detect mdraid/LVM pools even from old agents that do not set `is_pool` for these. The `Filesystem.device` field is already populated in stored telemetry. The fallback logic must be a complete mirror of the Go detection to avoid classification discrepancies.

**Alternatives Considered:**
- Server-side enrichment on telemetry retrieval: Would require modifying the telemetry handler and re-computing pool status on every API call. Unnecessary complexity for a display-only classification.
- Only checking fs_type in fallback: Would miss mdraid/LVM pools from old agents, violating FR-013.

**Consequences:** The detection logic exists in three locations: Go `IsPoolFilesystem()` (authoritative), TypeScript `isPoolFilesystem()` (fallback), and the `POOL_FS_TYPES`/exclusion constants in both. All three must be kept in sync. This is documented in Implementation Notes.

### AD-004: Pool Card Displays Device Path

**Decision:** Add a line showing the device path on each pool card, between the mount point / fs_type header row and the GaugeBar. Display format: the device path in monospace gray text (e.g., `/dev/md0`).

**Rationale:** FR-015 requires the device path to be visible on pool cards so users can identify the underlying array or logical volume. For device-path-detected pools (mdraid, LVM), the device path is the primary identifier. For fs-type-detected pools (ZFS, btrfs), it provides useful context (e.g., `tank/data`, `/dev/sda1`).

**Alternatives Considered:**
- Showing device path only for device-path-detected pools: Would require the frontend to know why a pool was classified, adding complexity. Showing it unconditionally is simpler and always useful.

**Consequences:** Pool cards become slightly taller. Minimal visual impact given the information value.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `internal/models/telemetry.go` | Add `shfs`, `fuse.shfs` to `PoolFSTypes`; replace `IsPoolFSType()` with `IsPoolFilesystem(fsType, device string) bool` containing device-path detection and exclusion logic |
| MODIFY | `internal/agent/collectors/disk.go` | Update call site: `models.IsPoolFSType(p.Fstype)` becomes `models.IsPoolFilesystem(p.Fstype, p.Device)` |
| MODIFY | `internal/models/telemetry_pool_test.go` | Rewrite tests for `IsPoolFilesystem()`: add shfs/fuse.shfs type tests, mdraid device tests, LVM device tests, Docker exclusion tests, live-boot exclusion tests, regular device negative tests |
| MODIFY | `web/src/utils/filesystem.ts` | Add `shfs`, `fuse.shfs` to `POOL_FS_TYPES`; add device-path detection with exclusions to `isPoolFilesystem()` fallback |
| MODIFY | `web/src/utils/filesystem.test.ts` | Add tests for shfs/fuse.shfs, mdraid device fallback, LVM device fallback, Docker exclusion, live-boot exclusion, regular device negative case |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | Add device path display line to pool card |

---

## 5. Data Model Changes

### `PoolFSTypes` -- Before

```go
var PoolFSTypes = []string{
    "bcachefs",
    "btrfs",
    "fuse.mergerfs",
    "fuse.unionfs",
    "mergerfs",
    "zfs",
}
```

### `PoolFSTypes` -- After

```go
var PoolFSTypes = []string{
    "bcachefs",
    "btrfs",
    "fuse.mergerfs",
    "fuse.shfs",
    "fuse.unionfs",
    "mergerfs",
    "shfs",
    "zfs",
}
```

### `IsPoolFSType` -- Removed

```go
// REMOVED
func IsPoolFSType(fsType string) bool { ... }
```

### `IsPoolFilesystem` -- New

```go
// IsPoolFilesystem returns true if the filesystem should be classified as a
// storage pool based on its filesystem type or device path.
//
// Detection is additive: a match on any rule returns true.
// Exclusions apply only to device-path detection (Docker device-mapper,
// live-boot overlays).
func IsPoolFilesystem(fsType, device string) bool {
    // 1. Filesystem type check
    for _, t := range PoolFSTypes {
        if fsType == t {
            return true
        }
    }

    // 2. mdraid: /dev/md*
    if strings.HasPrefix(device, "/dev/md") {
        return true
    }

    // 3. Device-mapper: /dev/mapper/*
    if strings.HasPrefix(device, "/dev/mapper/") {
        // Exclude Docker device-mapper
        if strings.HasPrefix(device, "/dev/mapper/docker-") {
            return false
        }
        // Exclude live-boot overlays
        if device == "/dev/mapper/live-rw" || device == "/dev/mapper/live-base" {
            return false
        }
        return true
    }

    // 4. Device-mapper kernel name: /dev/dm-*
    if strings.HasPrefix(device, "/dev/dm-") {
        return true
    }

    return false
}
```

### `Filesystem` Struct -- No Changes

The `Filesystem` struct already has `Device string` and `IsPool bool` fields. No modification needed.

### `Filesystem` TypeScript Interface -- No Changes

The interface already has `device: string` and `is_pool?: boolean`. No modification needed.

### `POOL_FS_TYPES` (TypeScript) -- Before

```typescript
export const POOL_FS_TYPES: readonly string[] = [
  'bcachefs', 'btrfs', 'fuse.mergerfs', 'fuse.unionfs', 'mergerfs', 'zfs',
]
```

### `POOL_FS_TYPES` (TypeScript) -- After

```typescript
export const POOL_FS_TYPES: readonly string[] = [
  'bcachefs', 'btrfs', 'fuse.mergerfs', 'fuse.shfs', 'fuse.unionfs',
  'mergerfs', 'shfs', 'zfs',
]
```

### No Database Migration

No schema changes. The `is_pool` field is part of the JSONB telemetry payload and was added in POOL-001.

---

## 6. API / Interface Contract

No API changes. No new endpoints. No changes to request/response shapes. The `is_pool` boolean within the telemetry JSONB payload was introduced in POOL-001 and is unchanged. The only difference is that updated agents will now set `is_pool: true` for additional filesystem types (shfs, fuse.shfs) and device paths (mdraid, LVM).

---

## 7. Sequence / Flow

### Agent-Side Flow (Updated from POOL-001)

1. `DiskCollector.Collect()` iterates over `gopsutil` partitions.
2. For each partition with `usage.Total > 0`, it builds a `models.Filesystem`.
3. It calls `models.IsPoolFilesystem(p.Fstype, p.Device)` and assigns the result to `IsPool`.
   - If `p.Fstype` is `"shfs"`, returns `true` (Unraid).
   - If `p.Device` is `"/dev/md0"`, returns `true` (mdraid), regardless of `p.Fstype`.
   - If `p.Device` is `"/dev/mapper/vg0-data"`, returns `true` (LVM).
   - If `p.Device` is `"/dev/mapper/docker-253:0-..."`, returns `false` (Docker exclusion).
   - If `p.Device` is `"/dev/mapper/live-rw"`, returns `false` (live-boot exclusion).
   - If `p.Fstype` is `"ext4"` and `p.Device` is `"/dev/sda1"`, returns `false` (regular disk).
4. The telemetry payload is serialized and pushed to the server as before.

### Frontend Fallback Flow (for Old Agents)

1. Frontend receives a filesystem where `is_pool` is `undefined`.
2. `isPoolFilesystem()` detects `is_pool` is undefined, enters fallback path.
3. Checks `fs.fs_type` against `POOL_FS_TYPES` (now including shfs, fuse.shfs).
4. If no fs_type match, checks `fs.device` for `/dev/md` prefix.
5. If no mdraid match, checks `fs.device` for `/dev/mapper/` prefix with exclusions.
6. If no mapper match, checks `fs.device` for `/dev/dm-` prefix.
7. Returns the result.

### Pool Card Rendering (Updated)

1. Pool cards now display: mount point + fs_type badge (row 1), device path (row 2), GaugeBar (row 3), capacity text (row 4).
2. For mdraid/LVM pools, the device path (e.g., `/dev/md0`, `/dev/mapper/vg0-data`) provides essential context since the fs_type (ext4, xfs) does not indicate pool membership.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | `models.IsPoolFilesystem("shfs", "/dev/shm")` returns `true` | Unit: `telemetry_pool_test.go` -- test `IsPoolFilesystem` with fsType `"shfs"` and arbitrary device |
| AC-002 | `models.IsPoolFilesystem("fuse.shfs", "/dev/sda")` returns `true` | Unit: `telemetry_pool_test.go` -- test `IsPoolFilesystem` with fsType `"fuse.shfs"` and arbitrary device |
| AC-003 | `models.IsPoolFilesystem("ext4", "/dev/md0")` returns `true` | Unit: `telemetry_pool_test.go` -- test with ext4 fsType and `/dev/md0` device |
| AC-004 | `models.IsPoolFilesystem("xfs", "/dev/md127")` returns `true` | Unit: `telemetry_pool_test.go` -- test with xfs fsType and `/dev/md127` device |
| AC-005 | `models.IsPoolFilesystem("ext4", "/dev/mapper/vg0-data")` returns `true` | Unit: `telemetry_pool_test.go` -- test with ext4 fsType and `/dev/mapper/vg0-data` device |
| AC-006 | `models.IsPoolFilesystem("xfs", "/dev/dm-3")` returns `true` | Unit: `telemetry_pool_test.go` -- test with xfs fsType and `/dev/dm-3` device |
| AC-007 | `models.IsPoolFilesystem("ext4", "/dev/mapper/docker-253:0-1234-abcdef")` returns `false` | Unit: `telemetry_pool_test.go` -- test Docker device-mapper exclusion |
| AC-008 | `models.IsPoolFilesystem("ext4", "/dev/mapper/live-rw")` returns `false` | Unit: `telemetry_pool_test.go` -- test live-boot exclusion for both `live-rw` and `live-base` |
| AC-009 | `models.IsPoolFilesystem("ext4", "/dev/sda1")` returns `false` | Unit: `telemetry_pool_test.go` -- test regular ext4 is not a pool |
| AC-010 | `models.IsPoolFilesystem("zfs", "tank/data")` returns `true` | Unit: `telemetry_pool_test.go` -- test existing pool types still detected (zfs, btrfs, mergerfs, etc.) |
| AC-011 | Frontend `isPoolFilesystem()` with `is_pool: undefined` and `fs_type: "fuse.shfs"` returns `true` | Unit: `filesystem.test.ts` -- test fallback for fuse.shfs |
| AC-012 | Frontend `isPoolFilesystem()` with `is_pool: undefined`, `fs_type: "ext4"`, `device: "/dev/md0"` returns `true` | Unit: `filesystem.test.ts` -- test device-path fallback for mdraid |
| AC-013 | Frontend `isPoolFilesystem()` with `is_pool: undefined`, `fs_type: "ext4"`, `device: "/dev/mapper/vg-data"` returns `true` | Unit: `filesystem.test.ts` -- test device-path fallback for LVM |
| AC-014 | Pool card in `DeviceDetail.tsx` renders `fs.device` in a visible element | Visual/manual: verify device path appears on pool card. Unit test coverage is not practical for JSX rendering in this codebase (no component tests for DeviceDetail). |

---

## 9. Error Handling

No new error paths. Pool detection is a pure function on already-collected data. Edge cases:

| Scenario | Handling |
|----------|----------|
| `device` is empty string | No prefix matches; falls through to `return false`. Correct behavior -- an empty device is not a pool. |
| `device` is exactly `/dev/md` (no number suffix) | `strings.HasPrefix("/dev/md", "/dev/md")` returns `true`. This is acceptable -- `/dev/md` without a number is not a real device path in practice. |
| `device` is `/dev/mapper/` (trailing slash, no name) | Prefix match succeeds, exclusion checks fail, returns `true`. This is a degenerate case that does not occur in practice. |
| Old agent sends telemetry without `is_pool` | Frontend fallback handles it (Section 7). |

---

## 10. Security Considerations

None. This story adds classification logic to already-collected, already-transmitted data. No new inputs, endpoints, authentication changes, or data exposure vectors.

---

## 11. Performance Considerations

- **Agent:** `IsPoolFilesystem` adds at most 3 `strings.HasPrefix` calls (each O(1)) per filesystem beyond the existing linear scan of an 8-element slice. Negligible.
- **Frontend:** The fallback adds at most 4 prefix checks per filesystem in addition to the existing `Array.includes` call. Negligible for typical filesystem counts (3-15).
- **No new database queries, indexes, or caching required.**

---

## 12. Implementation Notes for Engineers

### Go Side

1. **`internal/models/telemetry.go`:**
   - Add `"shfs"` and `"fuse.shfs"` to `PoolFSTypes` in alphabetical order.
   - Add `import "strings"` to the file (needed for `strings.HasPrefix`).
   - Remove `IsPoolFSType()` entirely.
   - Add `IsPoolFilesystem(fsType, device string) bool` with the logic shown in Section 5. The function checks fs type first (fast path for existing pool types), then device path prefixes with exclusions.
   - Add a doc comment on `IsPoolFilesystem` explaining the detection hierarchy.

2. **`internal/agent/collectors/disk.go`:**
   - Line 62: Change `models.IsPoolFSType(p.Fstype)` to `models.IsPoolFilesystem(p.Fstype, p.Device)`.
   - This is the only call site. No other files reference `IsPoolFSType`.

3. **`internal/models/telemetry_pool_test.go`:**
   - Rewrite the test file. All existing tests reference `IsPoolFSType` which no longer exists.
   - New tests must cover: all 8 pool fs types (including shfs, fuse.shfs), mdraid device paths (/dev/md0, /dev/md127), LVM device paths (/dev/mapper/vg0-data, /dev/dm-3), Docker exclusion (/dev/mapper/docker-*), live-boot exclusion (/dev/mapper/live-rw, /dev/mapper/live-base), regular device negative case (/dev/sda1 with ext4), empty strings.
   - Test names must reference AC IDs per project convention.

### Frontend Side

4. **`web/src/utils/filesystem.ts`:**
   - Add `'fuse.shfs'` and `'shfs'` to `POOL_FS_TYPES` in alphabetical order.
   - Update `isPoolFilesystem()` fallback logic. When `is_pool` is undefined, after checking `POOL_FS_TYPES`, add device-path checks:
     ```
     // Device-path-based detection (mirrors Go IsPoolFilesystem)
     const device = fs.device || ''
     if (device.startsWith('/dev/md')) return true
     if (device.startsWith('/dev/mapper/')) {
       if (device.startsWith('/dev/mapper/docker-')) return false
       if (device === '/dev/mapper/live-rw' || device === '/dev/mapper/live-base') return false
       return true
     }
     if (device.startsWith('/dev/dm-')) return true
     ```
   - Update the doc comment on `POOL_FS_TYPES` and `isPoolFilesystem()` to mention device-path detection.

5. **`web/src/utils/filesystem.test.ts`:**
   - Add new test blocks for AC-001/AC-002 (shfs/fuse.shfs), AC-011 (frontend fallback for fuse.shfs), AC-012 (mdraid device fallback), AC-013 (LVM device fallback).
   - Add negative tests for Docker exclusion and live-boot exclusion in the fallback path.
   - Update the `POOL_FS_TYPES covers all expected pool filesystem types` test to include shfs and fuse.shfs.
   - Existing tests that pass will continue to pass since we are only adding to the detection logic.

6. **`web/src/pages/DeviceDetail.tsx`:**
   - In the pool card JSX (around line 557-566), add a device path line between the header row and the GaugeBar:
     ```
     <span className="text-xs text-gray-500 font-mono">{fs.device}</span>
     ```
   - Place it after the closing `</div>` of the flex header row (line 561) and before the `<GaugeBar>` (line 562).

### Three-Location List Maintenance

The pool detection logic now exists in three conceptual locations:
- **Go `IsPoolFilesystem()`** in `internal/models/telemetry.go` -- authoritative, used by the agent.
- **TypeScript `isPoolFilesystem()`** in `web/src/utils/filesystem.ts` -- fallback for old agents, must mirror Go logic.
- **`PoolFSTypes` (Go) / `POOL_FS_TYPES` (TS)** -- the fs-type list, manually synced.

When adding a new pool detection rule, all three must be updated. The existing cross-reference comments in both files should be updated to reflect this.

### Verify No Other Call Sites

Before implementing, confirm `IsPoolFSType` is only called from `disk.go`. Run:
```
grep -r "IsPoolFSType" --include="*.go"
```
Expected: only `internal/agent/collectors/disk.go` and `internal/models/telemetry.go` (definition). If other call sites exist, they must also be updated to `IsPoolFilesystem`.

---

## 13. Definition of Done

- [ ] `shfs` and `fuse.shfs` added to `PoolFSTypes` in `internal/models/telemetry.go`
- [ ] `IsPoolFSType()` removed from `internal/models/telemetry.go`
- [ ] `IsPoolFilesystem(fsType, device string) bool` added to `internal/models/telemetry.go` with fs-type check, mdraid prefix check, LVM/device-mapper prefix check, Docker exclusion, and live-boot exclusion
- [ ] `disk.go` updated to call `models.IsPoolFilesystem(p.Fstype, p.Device)`
- [ ] `POOL_FS_TYPES` in `web/src/utils/filesystem.ts` updated with `shfs` and `fuse.shfs`
- [ ] `isPoolFilesystem()` in `web/src/utils/filesystem.ts` updated with device-path fallback detection matching Go logic
- [ ] Pool card in `DeviceDetail.tsx` displays the device path
- [ ] `telemetry_pool_test.go` rewritten with tests for all 14 ACs (Go-side ones)
- [ ] `filesystem.test.ts` updated with tests for AC-011, AC-012, AC-013, and exclusion cases
- [ ] `go test ./...` passes with no new failures
- [ ] `cd web && npm run test:run` passes with no new failures
- [ ] No new linting errors introduced
- [ ] All component changes in Section 4 implemented
- [ ] All AC mappings in Section 8 have corresponding tests with AC references

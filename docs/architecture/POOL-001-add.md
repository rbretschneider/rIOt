# Architecture Decision Document

- **Story ID:** POOL-001
- **FRD Reference:** docs/requirements/POOL-001-frd.md
- **Author:** Architect Agent
- **Date:** 2026-03-23
- **Status:** FINAL

---

## 1. Summary

Add a boolean `is_pool` field to the `Filesystem` model (Go struct and TypeScript interface) that the agent sets based on a centralized list of pool filesystem types. On the frontend, split the Filesystems section into a visually prominent "Storage Pools" card area above the existing table, with each pool filesystem rendered as a standalone card featuring a GaugeBar capacity indicator. Non-pool filesystems continue to render in the existing table unchanged. No database migration is required because telemetry is stored as JSONB.

---

## 2. Technical Context

### Current State

- **Agent (`internal/agent/collectors/disk.go`):** The `DiskCollector.Collect()` method iterates over `gopsutil` partitions and builds `models.Filesystem` structs. It already classifies network mounts via a local `netFS` string slice. mergerfs, ZFS, btrfs, and bcachefs filesystems are already collected with correct capacity values.
- **Model (`internal/models/telemetry.go`):** The `Filesystem` struct has fields for mount point, device, fs_type, capacity values, mount options, and `IsNetworkMount`. No pool classification exists.
- **TypeScript model (`web/src/types/models.ts`):** The `Filesystem` interface mirrors the Go struct. No `is_pool` field.
- **Frontend (`web/src/pages/DeviceDetail.tsx`):** Lines 545-579 render all filesystems in a single flat `<table>` inside a `<Section title="Filesystems">` wrapper. Capacity formatting already handles GB/TB conversion inline (threshold at 1000 GB, divides by 1024, 2 decimal places for TB, 1 for GB).
- **GaugeBar (`web/src/components/GaugeBar.tsx`):** Existing component with color thresholds at >75% (amber) and >90% (red). Accepts `label`, `value`, `max`, `unit` props.
- **Demo data (`web/src/api/demo-data.ts`):** Contains a btrfs filesystem at `/volume1` with 14400 GB total -- this will automatically be classified as a pool in demo mode.
- **No database migration needed:** Telemetry is stored as a JSONB column. Adding `is_pool` to the Filesystem struct flows through JSON serialization automatically. Old telemetry rows without `is_pool` will deserialize with the zero value (`false`), which is correct.

### What Needs to Change

1. A shared constant list of pool filesystem types (Go side, single source of truth).
2. The `Filesystem` Go struct gains an `IsPool bool` field.
3. The disk collector sets `IsPool` based on the pool type list.
4. The TypeScript `Filesystem` interface gains an optional `is_pool` field.
5. The frontend splits filesystems into pools vs non-pools, rendering pools in a new visual subsection.
6. A client-side fallback classification function for backward compatibility with old agents.

---

## 3. Architecture Decisions

### AD-001: Pool Type List Lives in the Go Models Package

**Decision:** Define the pool filesystem type list as a package-level variable `PoolFSTypes` (type `[]string`) in `internal/models/telemetry.go`, alongside the `Filesystem` struct. Provide a helper function `IsPoolFSType(fsType string) bool` in the same file.

**Rationale:** FR-003 requires a single maintainable location. Placing it in the models package means both the agent collector and any future server-side logic can import it. The collector already imports `internal/models`. The list is data, not business logic, so it belongs with the struct it annotates.

**Alternatives Considered:**
- Defining it in the collector: Would be inaccessible to the server if needed later.
- A separate `internal/fsutil` package: Over-engineering for a 6-element string slice.

**Consequences:** Adding a new pool FS type requires editing only `internal/models/telemetry.go` and updating the frontend fallback list. Two locations (Go + TS) is unavoidable given the backward-compat fallback requirement, but the Go list is authoritative and the TS list is explicitly documented as a fallback mirror.

### AD-002: Frontend Fallback Classification for Old Agents

**Decision:** Define a `POOL_FS_TYPES` constant array in a new file `web/src/utils/filesystem.ts`. The DeviceDetail page uses a helper function `isPoolFilesystem(fs: Filesystem): boolean` that returns `fs.is_pool` if the field is present (truthy or explicitly `true`), otherwise falls back to checking `fs.fs_type` against the `POOL_FS_TYPES` list.

**Rationale:** NFR-002 mandates backward compatibility. Old agents will not send `is_pool`, so the JSON field will be `undefined` (not `false`). The fallback ensures pool detection works regardless of agent version. A utility file keeps this logic testable and out of the component.

**Alternatives Considered:**
- Server-side enrichment on telemetry ingest: Would require modifying the telemetry handler, adding unnecessary complexity for a display-only feature.
- Always classify client-side, ignoring `is_pool`: Would make the agent-side field useless and prevent future server-side use.

**Consequences:** Two copies of the pool type list exist (Go authoritative, TS fallback). When the list changes, both must be updated. This is documented in Implementation Notes.

### AD-003: Pool Filesystems Rendered as Cards with GaugeBar

**Decision:** Pool filesystems render as individual cards in a horizontal flex layout within the existing `<Section title="Filesystems">` container, above the regular table. Each card contains: a mount point title, filesystem type badge, a GaugeBar for usage, and a text line showing "X.XX TB used of Y.YY TB (Z.ZZ TB free)" (or GB equivalents). The existing `GaugeBar` component is reused without modification.

**Rationale:** The GaugeBar already implements the exact color thresholds specified in FR-007 (>75% amber, >90% red). Cards provide visual prominence without introducing a new component library. The horizontal flex layout handles 1-3 pools gracefully and wraps on narrow screens.

**Alternatives Considered:**
- A dedicated `StoragePoolCard` component in its own file: Considered, but the card is simple enough (15-20 lines of JSX) that extracting it adds navigational overhead without reuse benefit. If pool display becomes more complex in the future, it can be extracted then.
- A separate `<Section title="Storage Pools">` above the `<Section title="Filesystems">`: Would create two visually separate sections. Keeping them in one section with a visual divider maintains the grouping that "Storage Pools" are part of the disk/filesystem story.

**Consequences:** The Filesystems section becomes slightly more complex but remains a single cohesive block. The card layout is responsive via Tailwind flex-wrap.

### AD-004: Keep Pools and Table in One Section Block

**Decision:** The "Storage Pools" subsection and the "Filesystems" table both live inside a single `<Section title="Filesystems">`. The pools area has its own subheading ("Storage Pools") rendered as a smaller heading element. A visual separator (border or margin) divides the two areas.

**Rationale:** FR-004 says "above the regular Filesystems table within the Filesystems section." A single section avoids layout fragmentation. The user sees one expandable area for all disk-related filesystem info.

**Alternatives Considered:**
- Two separate `<Section>` blocks: Creates visual separation that suggests they are unrelated.

**Consequences:** The conditional rendering logic inside the Section becomes more involved but remains readable.

### AD-005: `is_pool` Field Uses `omitempty` in Go JSON Tag

**Decision:** The `IsPool` field in the Go `Filesystem` struct uses the JSON tag `json:"is_pool,omitempty"`. This means agents that have not been updated produce JSON without the field, and updated agents only include it when `true`.

**Rationale:** Minimizes telemetry payload size. Most filesystems are not pools, so `is_pool: false` on every ext4, tmpfs, etc. is noise. The frontend fallback (AD-002) handles the absence of the field.

**Alternatives Considered:**
- Always serializing: Would add `"is_pool":false` to every non-pool filesystem entry, inflating payloads slightly for no benefit.

**Consequences:** The TypeScript interface must declare `is_pool` as optional (`is_pool?: boolean`) since it may be absent from JSON.

### AD-006: No Heuristic for Single-Disk Btrfs/ZFS

**Decision:** Classify btrfs and zfs as pool filesystems based purely on `fs_type`, with no attempt to detect whether they span multiple disks.

**Rationale:** BR-003 explicitly states this is acceptable. Detecting multi-disk pooling would require parsing `btrfs filesystem show` or `zpool status` output, which is complex, OS-specific, and fragile. The classification is a visual hint, not a guarantee. The FRD explicitly calls this out of scope.

**Alternatives Considered:**
- Parsing btrfs/zpool commands: Complex, platform-specific, error-prone, and explicitly not required.

**Consequences:** A single-disk btrfs root partition will appear in the Storage Pools section. This is an acceptable trade-off per the FRD.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `internal/models/telemetry.go` | Add `IsPool` field to `Filesystem` struct; add `PoolFSTypes` var and `IsPoolFSType()` helper |
| MODIFY | `internal/agent/collectors/disk.go` | Set `IsPool` on each filesystem using `models.IsPoolFSType()` |
| CREATE | `internal/models/telemetry_pool_test.go` | Unit tests for `IsPoolFSType()` |
| MODIFY | `web/src/types/models.ts` | Add optional `is_pool` field to `Filesystem` interface |
| CREATE | `web/src/utils/filesystem.ts` | `POOL_FS_TYPES` constant, `isPoolFilesystem()` helper, `formatCapacity()` helper |
| CREATE | `web/src/utils/filesystem.test.ts` | Unit tests for `isPoolFilesystem()` and `formatCapacity()` |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | Split filesystem rendering: pool cards above, regular table below |
| MODIFY | `web/src/api/demo-data.ts` | Add `is_pool: true` to the btrfs demo filesystem for consistency |

---

## 5. Data Model Changes

### `Filesystem` Struct (Go) -- Before

```go
type Filesystem struct {
    MountPoint     string  `json:"mount_point"`
    Device         string  `json:"device"`
    FSType         string  `json:"fs_type"`
    TotalGB        float64 `json:"total_gb"`
    UsedGB         float64 `json:"used_gb"`
    FreeGB         float64 `json:"free_gb"`
    UsagePercent   float64 `json:"usage_percent"`
    MountOptions   string  `json:"mount_options,omitempty"`
    IsNetworkMount bool    `json:"is_network_mount"`
}
```

### `Filesystem` Struct (Go) -- After

```go
type Filesystem struct {
    MountPoint     string  `json:"mount_point"`
    Device         string  `json:"device"`
    FSType         string  `json:"fs_type"`
    TotalGB        float64 `json:"total_gb"`
    UsedGB         float64 `json:"used_gb"`
    FreeGB         float64 `json:"free_gb"`
    UsagePercent   float64 `json:"usage_percent"`
    MountOptions   string  `json:"mount_options,omitempty"`
    IsNetworkMount bool    `json:"is_network_mount"`
    IsPool         bool    `json:"is_pool,omitempty"`
}
```

### New Package-Level Additions (Go, in `internal/models/telemetry.go`)

```go
// PoolFSTypes is the authoritative list of filesystem types classified as storage pools.
var PoolFSTypes = []string{
    "fuse.mergerfs",
    "mergerfs",
    "fuse.unionfs",
    "zfs",
    "bcachefs",
    "btrfs",
}

// IsPoolFSType returns true if the given filesystem type is a known pool/union filesystem.
func IsPoolFSType(fsType string) bool {
    for _, t := range PoolFSTypes {
        if fsType == t {
            return true
        }
    }
    return false
}
```

### `Filesystem` Interface (TypeScript) -- After

```typescript
export interface Filesystem {
  mount_point: string
  device: string
  fs_type: string
  total_gb: number
  used_gb: number
  free_gb: number
  usage_percent: number
  mount_options?: string
  is_network_mount: boolean
  is_pool?: boolean  // Optional: absent from old agents
}
```

### No Database Migration

Telemetry is stored as JSONB. The new field flows through JSON serialization automatically. Existing rows without `is_pool` deserialize with the zero value (`false`/`undefined`), which is correct behavior.

---

## 6. API / Interface Contract

No API changes. The telemetry ingest endpoint (`POST /api/v1/telemetry`) and the telemetry retrieval endpoint already handle the `Filesystem` struct as part of the JSONB `data` column. The new `is_pool` field is included in the JSON automatically. No endpoint signatures, request shapes, or response shapes change.

The only wire-format change is that updated agents will include `"is_pool": true` on pool filesystem entries within the telemetry JSON payload. This is additive and backward-compatible.

---

## 7. Sequence / Flow

### Agent-Side Flow (Telemetry Collection)

1. `DiskCollector.Collect()` iterates over `gopsutil` partitions.
2. For each partition with `usage.Total > 0`, it builds a `models.Filesystem`.
3. **NEW:** After setting all existing fields, it calls `models.IsPoolFSType(p.Fstype)` and assigns the result to `IsPool`.
4. The filesystem is appended to `DiskInfo.Filesystems` as before.
5. The telemetry payload is serialized to JSON and pushed to the server. `is_pool` is included when `true`, omitted when `false` (due to `omitempty`).

### Frontend Flow (Device Detail Rendering)

1. `DeviceDetail.tsx` receives telemetry containing `disks.filesystems[]`.
2. **NEW:** The component imports `isPoolFilesystem` from `utils/filesystem.ts`.
3. **NEW:** It partitions the filesystems array:
   - `poolFilesystems = filesystems.filter(isPoolFilesystem)`
   - `regularFilesystems = filesystems.filter(fs => !isPoolFilesystem(fs))`
4. **NEW:** If `poolFilesystems.length > 0`, render the "Storage Pools" subsection with pool cards, each containing a GaugeBar and capacity text.
5. If `regularFilesystems.length > 0`, render the existing table with only non-pool filesystems.
6. If there are no pool filesystems, the subsection is not rendered and the table shows all filesystems (identical to current behavior).

### Backward Compatibility Flow

1. Old agent sends telemetry without `is_pool` field on any filesystem.
2. Server stores the JSON as-is in JSONB column.
3. Frontend receives filesystems where `is_pool` is `undefined`.
4. `isPoolFilesystem()` detects `is_pool` is `undefined` (not explicitly `true`), falls back to checking `fs_type` against `POOL_FS_TYPES`.
5. mergerfs/ZFS/btrfs/bcachefs filesystems are correctly identified as pools via fallback.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | `models.IsPoolFSType("fuse.mergerfs")` returns `true`; `disk.go` sets `IsPool` field | Unit: `telemetry_pool_test.go` -- verify `IsPoolFSType` returns `true` for `fuse.mergerfs`; verify all filesystem fields remain populated |
| AC-002 | `models.IsPoolFSType("ext4")` returns `false` | Unit: `telemetry_pool_test.go` -- verify `IsPoolFSType` returns `false` for `ext4`, `tmpfs`, `vfat`, etc. |
| AC-003 | `DeviceDetail.tsx` partitions filesystems and renders pool cards with GaugeBar above table; pool entries excluded from table | Unit: `filesystem.test.ts` -- verify `isPoolFilesystem` returns `true` for pool types. Component rendering is validated visually / by QA. |
| AC-004 | Conditional render: pool subsection only rendered when `poolFilesystems.length > 0` | Unit: `filesystem.test.ts` -- verify empty pool list when no pool FS types present. Visual validation by QA. |
| AC-005 | GaugeBar component reuse: thresholds are >75% amber, >90% red (already implemented in `GaugeBar.tsx`) | Existing `GaugeBar.test.tsx` covers this. No new test needed for color thresholds. |
| AC-006 | `formatCapacity()` in `utils/filesystem.ts`: values >= 1000 GB displayed as TB (divided by 1024, 2 decimal places); values < 1000 GB displayed as GB (1 decimal place) | Unit: `filesystem.test.ts` -- verify `formatCapacity(14000)` returns `"13.67 TB"`, `formatCapacity(500)` returns `"500.0 GB"` |
| AC-007 | `isPoolFilesystem()` fallback: when `is_pool` is `undefined`, checks `fs_type` against `POOL_FS_TYPES` | Unit: `filesystem.test.ts` -- verify classification works when `is_pool` is absent from the object |
| AC-008 | `models.IsPoolFSType("zfs")` and `models.IsPoolFSType("btrfs")` both return `true` | Unit: `telemetry_pool_test.go` -- verify both types return `true` |

---

## 9. Error Handling

This feature has no new error paths. The classification is a pure function on already-collected data. Specific non-error edge cases:

| Scenario | Handling |
|----------|----------|
| `is_pool` field missing from JSON (old agent) | Frontend fallback classifies by `fs_type`. Go struct zero-value is `false`. |
| `disks` or `filesystems` is `null`/`undefined` | Existing guard (`tel?.disks?.filesystems && tel.disks.filesystems.length > 0`) already handles this. No change. |
| Pool filesystem has 0 total capacity | Already filtered out by collector (`usage.Total == 0` skip). Cannot reach the frontend. |
| Unknown filesystem type (not in pool list) | `IsPoolFSType` returns `false`. Filesystem renders in the regular table. |

---

## 10. Security Considerations

None. This feature adds a derived boolean classification field to telemetry data that is already collected and transmitted. No new inputs, no new endpoints, no authentication changes, no data exposure changes.

---

## 11. Performance Considerations

- **Agent:** `IsPoolFSType` performs a linear scan of a 6-element slice per filesystem. This is negligible (nanoseconds).
- **Frontend:** `Array.filter()` called twice on the filesystems array (typically 3-10 elements). Negligible.
- **No new database queries, indexes, or caching required.**

---

## 12. Implementation Notes for Engineers

### Go Side

1. **Add `PoolFSTypes` and `IsPoolFSType()` to `internal/models/telemetry.go`**, immediately below the `Filesystem` struct definition. Keep the list sorted alphabetically for readability.

2. **Add `IsPool` field to `Filesystem` struct** with JSON tag `json:"is_pool,omitempty"`. Place it after `IsNetworkMount` for logical grouping.

3. **In `disk.go`**, after setting `IsNetworkMount`, add:
   ```
   IsPool: models.IsPoolFSType(p.Fstype),
   ```
   Follow the exact same pattern as `isNetwork` but using the models helper instead of an inline loop.

4. **Test file `internal/models/telemetry_pool_test.go`**: Test all 6 pool types return `true`. Test at least 4 non-pool types (`ext4`, `tmpfs`, `vfat`, `xfs`) return `false`. Test empty string returns `false`.

### Frontend Side

5. **`web/src/utils/filesystem.ts`** must export:
   - `POOL_FS_TYPES: readonly string[]` -- mirror of Go list: `["fuse.mergerfs", "mergerfs", "fuse.unionfs", "zfs", "bcachefs", "btrfs"]`
   - `isPoolFilesystem(fs: Filesystem): boolean` -- returns `fs.is_pool === true` if the field is present, otherwise falls back to `POOL_FS_TYPES.includes(fs.fs_type)`
   - `formatCapacity(gb: number): string` -- returns `"X.X GB"` for values < 1000, `"X.XX TB"` for values >= 1000 (divides by 1024). This extracts the inline formatting logic already in DeviceDetail.tsx lines 566-567.

6. **In `DeviceDetail.tsx`**, the Filesystems section (lines 545-579) must be refactored:
   - Import `isPoolFilesystem` and `formatCapacity` from `../utils/filesystem`.
   - Compute `poolFilesystems` and `regularFilesystems` using `.filter()`.
   - Render pool cards in a `flex flex-wrap gap-4` container before the table.
   - Each pool card: dark background (`bg-gray-800/50 border border-gray-700 rounded-lg p-4`), mount point as title, fs_type as a small badge, GaugeBar, and a capacity summary line.
   - The capacity summary line format: `"X.XX TB used of Y.YY TB (Z.ZZ TB free)"` or GB equivalents.
   - Pool cards should be `min-w-[280px] flex-1` so they fill available width and wrap on smaller screens.
   - The existing table renders `regularFilesystems` instead of all filesystems.
   - Use `formatCapacity()` in both the pool cards and the existing table cells (replacing the inline ternaries on lines 566-567).

7. **TypeScript `is_pool` field**: Declare as `is_pool?: boolean` (optional). This correctly handles both old agents (field absent = `undefined`) and new agents (field present as `true`).

8. **Demo data**: Add `is_pool: true` to the btrfs entry in `web/src/api/demo-data.ts` line 320. This ensures demo mode shows the Storage Pools subsection.

### Two-Location List Maintenance

The pool filesystem type list exists in two places:
- **Authoritative:** `internal/models/telemetry.go` -- `PoolFSTypes`
- **Fallback mirror:** `web/src/utils/filesystem.ts` -- `POOL_FS_TYPES`

When adding a new pool type, both must be updated. Add a code comment in both locations referencing the other.

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented
- [ ] `IsPool` field added to Go `Filesystem` struct with `json:"is_pool,omitempty"` tag
- [ ] `PoolFSTypes` and `IsPoolFSType()` added to `internal/models/telemetry.go`
- [ ] `disk.go` sets `IsPool` using `models.IsPoolFSType()`
- [ ] `is_pool?: boolean` added to TypeScript `Filesystem` interface
- [ ] `web/src/utils/filesystem.ts` created with `POOL_FS_TYPES`, `isPoolFilesystem()`, `formatCapacity()`
- [ ] Pool filesystem cards render above the regular table with GaugeBar
- [ ] Pool filesystems excluded from the regular table
- [ ] No Storage Pools subsection when no pools exist
- [ ] Backward compatibility: old agent telemetry (no `is_pool` field) still classifies pools correctly
- [ ] All AC mappings in Section 8 have corresponding tests with AC references
- [ ] `go test ./...` passes with no new failures
- [ ] `cd web && npm run test:run` passes with no new failures
- [ ] No new linting errors introduced
- [ ] Demo data updated with `is_pool: true` on btrfs entry

# Formal Requirements Document

- **Story ID:** POOL-002
- **Title:** Expand Storage Pool Detection for Unraid, mdraid, and LVM
- **Author:** Business Developer Agent
- **Date:** 2026-03-23
- **Status:** FINAL

---

## 1. Executive Summary

Expand the storage pool detection system to recognize additional NAS and storage configurations commonly found in homelab environments. Currently, pools are identified solely by filesystem type (btrfs, zfs, bcachefs, mergerfs, unionfs). This story adds detection for Unraid's union filesystem (`shfs`/`fuse.shfs`), Linux software RAID arrays (mdraid), and Logical Volume Manager (LVM) volumes -- covering the majority of storage pool configurations in Synology, QNAP, Unraid, and custom Linux server builds.

---

## 2. Background & Context

The POOL-001 story introduced pool detection based on filesystem type matching. This works for filesystems that inherently represent pooled storage (ZFS, btrfs, mergerfs, etc.) but misses two major categories:

1. **Unraid** uses `shfs` / `fuse.shfs` as its union array mount. This follows the same pattern as mergerfs -- it is a distinct filesystem type that always represents a storage pool. It was simply omitted from the original list.

2. **mdraid and LVM** present RAID arrays and logical volumes as block devices (`/dev/md*`, `/dev/mapper/*`, `/dev/dm-*`) but layer standard filesystems (ext4, xfs) on top. The filesystem type alone cannot distinguish these from a regular single-disk ext4 partition. Detection must examine the device path.

The `Filesystem` struct already contains a `Device` field populated with the block device path (e.g., `/dev/md0`, `/dev/mapper/vg0-data`). This field is available on both the agent (for detection) and in stored telemetry (for frontend fallback logic).

Hardware RAID controllers and SAN LUNs present as normal block devices with no distinguishing path prefix and are explicitly out of scope.

---

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Agent | Collects filesystem telemetry on monitored devices, sets `is_pool` flag | Reads local filesystem metadata |
| Server | Receives and stores telemetry, serves it via API | Read/write telemetry data |
| Dashboard | Displays filesystems, separating pools from regular filesystems | Read-only via API |
| Old Agent (pre-POOL-002) | Agents not yet updated; do not set `is_pool` for mdraid/LVM | N/A -- frontend must handle gracefully |

---

## 4. Functional Requirements

### Unraid Detection (filesystem-type-based)

**FR-001:** The agent must classify filesystems with type `shfs` as storage pools (`is_pool = true`).

**FR-002:** The agent must classify filesystems with type `fuse.shfs` as storage pools (`is_pool = true`).

**FR-003:** The `PoolFSTypes` list in `internal/models/telemetry.go` must include `shfs` and `fuse.shfs`.

**FR-004:** The `POOL_FS_TYPES` list in `web/src/utils/filesystem.ts` must include `shfs` and `fuse.shfs`.

### mdraid Detection (device-path-based)

**FR-005:** The agent must classify a filesystem as a storage pool (`is_pool = true`) when its device path starts with `/dev/md` (matching mdraid arrays such as `/dev/md0`, `/dev/md1`, `/dev/md127`).

**FR-006:** The `IsPoolFSType` function name and signature must be updated or supplemented with a new function that accepts both the filesystem type and the device path, since pool detection is no longer based solely on filesystem type.

### LVM Detection (device-path-based)

**FR-007:** The agent must classify a filesystem as a storage pool (`is_pool = true`) when its device path starts with `/dev/mapper/` (matching LVM logical volumes and device-mapper devices).

**FR-008:** The agent must classify a filesystem as a storage pool (`is_pool = true`) when its device path starts with `/dev/dm-` (matching device-mapper devices by their kernel name).

### Exclusions from Device-Path Detection

**FR-009:** The agent must not classify device-mapper devices used by Docker's storage driver (paths matching `/dev/mapper/docker-` or `/dev/dm-` entries that are Docker thin-pool volumes) as storage pools. These are internal container infrastructure, not user-facing storage pools.

**FR-010:** The agent must not classify the device `/dev/mapper/live-rw` or `/dev/mapper/live-base` as storage pools (these are live-boot overlay devices, not persistent storage).

### Frontend Fallback (backward compatibility)

**FR-011:** The frontend `isPoolFilesystem` function must continue to use the `is_pool` field from the agent when present (`is_pool !== undefined`).

**FR-012:** When `is_pool` is not present (old agents), the frontend must fall back to checking the filesystem type against `POOL_FS_TYPES` (existing behavior, now including `shfs` and `fuse.shfs`).

**FR-013:** When `is_pool` is not present (old agents), the frontend must additionally check the device path for mdraid prefixes (`/dev/md`) and LVM prefixes (`/dev/mapper/`, `/dev/dm-`) to classify those filesystems as pools.

### Display

**FR-014:** Filesystems classified as pools by any detection method (filesystem type or device path) must appear in the "Storage Pools" subsection of the Filesystems section on the device detail page, not in the regular filesystems table.

**FR-015:** The pool card must display the device path in addition to the mount point and filesystem type, so users can identify the underlying array or logical volume.

---

## 5. Non-Functional Requirements

**NFR-001: [Backward Compatibility]** Updated agents must continue to work with older servers. The `is_pool` field is already `omitempty` in JSON. No wire format changes are required.

**NFR-002: [Backward Compatibility]** Updated servers and dashboards must handle telemetry from agents that predate POOL-002 (agents that do not perform device-path-based detection). The frontend fallback (FR-012, FR-013) addresses this.

**NFR-003: [Performance]** Pool detection must add no measurable latency to the disk collector. All detection must be based on data already collected (filesystem type and device path from the partition list) -- no additional system calls, no reading `/proc/mdstat`, no running `lvs` or `mdadm`.

**NFR-004: [Maintainability]** The pool detection logic (both fs-type-based and device-path-based) must be centralized in the models package so that any component determining pool status calls the same function. The disk collector must not contain its own inline detection logic.

---

## 6. Business Rules

**BR-001:** A filesystem is a storage pool if its filesystem type is in the pool filesystem type list (btrfs, zfs, bcachefs, mergerfs, fuse.mergerfs, unionfs, fuse.unionfs, shfs, fuse.shfs).

**BR-002:** A filesystem is a storage pool if its device path starts with `/dev/md` (mdraid), regardless of filesystem type.

**BR-003:** A filesystem is a storage pool if its device path starts with `/dev/mapper/` or `/dev/dm-` (LVM / device-mapper), regardless of filesystem type, unless it matches an exclusion pattern (FR-009, FR-010).

**BR-004:** Detection methods are additive -- a filesystem matching any one rule is classified as a pool. A filesystem matching none is not a pool.

**BR-005:** The existing pool types (btrfs, zfs, bcachefs, mergerfs, fuse.mergerfs, unionfs, fuse.unionfs) must continue to be detected. This story must not regress existing detection.

---

## 7. Data Requirements

### Entities Involved

- **Filesystem** (`internal/models/telemetry.go`): No struct changes. The existing `Device` (string) and `IsPool` (bool) fields are sufficient.
- **PoolFSTypes** (`internal/models/telemetry.go`): Add `shfs` and `fuse.shfs` to the list.
- **POOL_FS_TYPES** (`web/src/utils/filesystem.ts`): Add `shfs` and `fuse.shfs` to the list.

### Validation Rules

- Device path prefix matching must be case-sensitive (Linux paths are case-sensitive).
- Device path matching must use prefix comparison, not substring or regex, for the primary check.
- Docker device-mapper exclusion (FR-009) must match the prefix `/dev/mapper/docker-` specifically.

### State Transitions

No state transitions apply. Pool classification is stateless -- it is computed at collection time and at frontend render time. There is no persisted "pool" entity.

---

## 8. Acceptance Criteria

**AC-001: Unraid shfs detection** [Maps to FR-001, FR-002, FR-003, BR-001]
```
Given: A filesystem with fs_type "shfs" and device "/dev/shm" (or any device path)
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be true
```

**AC-002: Unraid fuse.shfs detection** [Maps to FR-001, FR-002, FR-003, BR-001]
```
Given: A filesystem with fs_type "fuse.shfs" and any device path
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be true
```

**AC-003: mdraid detection** [Maps to FR-005, BR-002]
```
Given: A filesystem with fs_type "ext4" and device "/dev/md0"
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be true
```

**AC-004: mdraid multi-digit detection** [Maps to FR-005, BR-002]
```
Given: A filesystem with fs_type "xfs" and device "/dev/md127"
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be true
```

**AC-005: LVM mapper detection** [Maps to FR-007, BR-003]
```
Given: A filesystem with fs_type "ext4" and device "/dev/mapper/vg0-data"
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be true
```

**AC-006: LVM dm detection** [Maps to FR-008, BR-003]
```
Given: A filesystem with fs_type "xfs" and device "/dev/dm-3"
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be true
```

**AC-007: Docker device-mapper exclusion** [Maps to FR-009, BR-003]
```
Given: A filesystem with device "/dev/mapper/docker-253:0-1234-abcdef"
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be false
```

**AC-008: Live-boot overlay exclusion** [Maps to FR-010]
```
Given: A filesystem with device "/dev/mapper/live-rw"
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be false
```

**AC-009: Regular ext4 not classified as pool** [Maps to BR-004]
```
Given: A filesystem with fs_type "ext4" and device "/dev/sda1"
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be false
```

**AC-010: Existing pool types still detected** [Maps to BR-005]
```
Given: A filesystem with fs_type "zfs" and device "tank/data"
When:  The agent evaluates whether the filesystem is a pool
Then:  is_pool must be true
```

**AC-011: Frontend fallback for old agents -- fs_type** [Maps to FR-012, FR-004]
```
Given: A filesystem from an old agent where is_pool is undefined, with fs_type "fuse.shfs"
When:  The frontend evaluates isPoolFilesystem()
Then:  The function must return true
```

**AC-012: Frontend fallback for old agents -- device path** [Maps to FR-013]
```
Given: A filesystem from an old agent where is_pool is undefined, with fs_type "ext4" and device "/dev/md0"
When:  The frontend evaluates isPoolFilesystem()
Then:  The function must return true
```

**AC-013: Frontend fallback for old agents -- LVM device path** [Maps to FR-013]
```
Given: A filesystem from an old agent where is_pool is undefined, with fs_type "ext4" and device "/dev/mapper/vg-data"
When:  The frontend evaluates isPoolFilesystem()
Then:  The function must return true
```

**AC-014: Pool card shows device path** [Maps to FR-015]
```
Given: A filesystem classified as a pool with device "/dev/md0" and mount_point "/mnt/storage"
When:  The pool card is rendered in the Storage Pools subsection
Then:  The device path "/dev/md0" must be visible on the card
```

---

## 9. Out of Scope

- **Hardware RAID detection**: Hardware RAID controllers (Adaptec, LSI/Broadcom, HP SmartArray) present volumes as `/dev/sd*` or `/dev/nvme*` with no distinguishing path prefix. Detecting these would require running controller-specific CLI tools. Out of scope.
- **SAN/iSCSI LUN detection**: Remote block devices also present as standard device paths. Out of scope.
- **Pool health/status reporting**: Reporting degraded mdraid arrays, LVM thin-pool usage, or ZFS scrub status. Separate story.
- **Pool composition details**: Showing which physical disks make up an mdraid or LVM pool. Separate story.
- **Database schema changes**: No schema changes. The `is_pool` boolean on the Filesystem struct within the telemetry JSON payload is sufficient.
- **New API endpoints**: No new endpoints. Pool data flows through existing telemetry collection and retrieval.

---

## 10. Assumptions

1. The `Filesystem.Device` field is reliably populated by `gopsutil` on Linux for mdraid and LVM devices. (Confirmed: `gopsutil`'s `disk.Partitions()` reads `/proc/mounts` which includes the device path.)
2. Unraid's `shfs` / `fuse.shfs` is the only filesystem type needed for Unraid pool detection. Unraid's per-disk mounts (individual `/dev/sd*` drives at `/mnt/disk*`) are intentionally not classified as pools.
3. All `/dev/mapper/*` paths that are not in the exclusion list (Docker, live-boot) represent user-facing storage volumes worth classifying as pools. This is a reasonable default for homelab NAS systems where LVM is used for data volumes.
4. The frontend `Filesystem` TypeScript type already includes the `device` field (it is used in the regular filesystems table).

---

## 11. Open Questions

None. All ambiguities have been resolved based on the provided context and codebase analysis.

---

## 12. Dependencies

- **POOL-001** (completed): Established the `is_pool` field, `PoolFSTypes` list, `IsPoolFSType()` function, and `isPoolFilesystem()` frontend function. This story extends all of those.
- **gopsutil v3**: The `disk.Partitions()` function must populate the `Device` field for mdraid and LVM paths. This is a read from `/proc/mounts` and is confirmed to work.

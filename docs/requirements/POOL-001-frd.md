# Formal Requirements Document

- **Story ID:** POOL-001
- **Title:** Storage Pool Filesystem Visual Identification and Presentation
- **Author:** Business Developer Agent
- **Date:** 2026-03-23
- **Status:** DRAFT

---

## 1. Executive Summary

Devices running union filesystems such as mergerfs present combined storage pools that aggregate multiple physical drives into a single mount point. The rIOt agent already collects these filesystems (they appear as `fuse.mergerfs` in the `fs_type` field), but the dashboard treats them identically to regular partitions. This story adds visual identification and presentation enhancements so that pool-type filesystems are immediately recognizable in the device detail view, showing their used/total capacity prominently.

---

## 2. Background & Context

A user managing a Debian NAS with OpenMediaVault uses mergerfs to pool multiple drives into a single logical volume. When viewing the device in rIOt, the mergerfs mount appears in the flat Filesystems table alongside boot partitions, tmpfs mounts, and other system filesystems with no visual distinction. For NAS/storage-oriented devices, the pool filesystem is the most important piece of storage information, but it currently requires the user to scan through the table and recognize the `fuse.mergerfs` type manually.

**Current state:**
- The agent's disk collector collects all mounted filesystems via `gopsutil`, including FUSE-based mounts. mergerfs mounts (fstype `fuse.mergerfs`) are already collected with correct total/used/free values representing the combined pool.
- The `Filesystem` model includes `fs_type` as a string field, which already carries the value `fuse.mergerfs` for mergerfs mounts.
- The `IsNetworkMount` boolean is the only filesystem classification that exists today. There is no concept of a "pool" or "union" filesystem.
- The dashboard renders all filesystems in a single flat table with columns: Mount, Device, Type, Used, Total, Usage. No visual grouping or highlighting exists.

---

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Dashboard User | Any authenticated user viewing a device's detail page | Read access to device telemetry |
| rIOt Agent | Daemon running on the monitored device | Collects and pushes telemetry (no changes needed for this story) |

---

## 4. Functional Requirements

### Data Classification

**FR-001:** The system must classify a filesystem as a "pool filesystem" when its `fs_type` value matches any of the following known pool/union filesystem types: `fuse.mergerfs`, `mergerfs`, `fuse.unionfs`, `bcachefs`, `zfs`, `btrfs`.

**FR-002:** The `Filesystem` data model must include a boolean field `is_pool` that indicates whether the filesystem has been classified as a pool filesystem per FR-001.

**FR-003:** The list of filesystem types that qualify as pool filesystems must be defined in a single, maintainable location (not duplicated between agent and frontend).

### Dashboard Presentation

**FR-004:** When a device has one or more pool filesystems, the dashboard must render a visually distinct "Storage Pools" subsection above the regular Filesystems table within the Filesystems section of the device detail page.

**FR-005:** Each pool filesystem in the Storage Pools subsection must display the following information: mount point, filesystem type, used capacity, total capacity, free capacity, and usage percentage.

**FR-006:** Each pool filesystem in the Storage Pools subsection must display a visual capacity gauge (progress bar or similar) showing the used/total ratio, consistent with the gauge style used elsewhere in the dashboard (e.g., the GaugeBar component pattern).

**FR-007:** The capacity gauge for a pool filesystem must use color-coded thresholds consistent with existing filesystem usage coloring: green for usage at or below 75%, amber for usage above 75% and at or below 90%, red for usage above 90%.

**FR-008:** Pool filesystems that appear in the Storage Pools subsection must not also appear in the regular Filesystems table below it. They must be shown in exactly one location.

**FR-009:** When a device has zero pool filesystems, the Storage Pools subsection must not be rendered. The Filesystems table must render as it does today with no changes.

**FR-010:** The Storage Pools subsection must display used and total capacity values in human-readable units (GB for values below 1000 GB, TB for values at or above 1000 GB), consistent with the existing formatting in the Filesystems table.

### Agent Behavior

**FR-011:** The agent must set the `is_pool` field on each filesystem entry based on the classification rule in FR-001 before transmitting telemetry to the server.

**FR-012:** The agent must not filter out, alter, or treat pool filesystems differently in any way other than setting the `is_pool` classification field. All existing data (mount point, device, fs_type, total, used, free, usage percent, mount options) must continue to be collected and transmitted as-is.

---

## 5. Non-Functional Requirements

**NFR-001:** [Performance] The pool classification logic must add no measurable latency to telemetry collection or dashboard rendering. It is a simple string comparison against a short list.

**NFR-002:** [Backward Compatibility] Agents that have not been updated (and therefore do not send the `is_pool` field) must not break the dashboard. The frontend must fall back to classifying filesystems by `fs_type` client-side if `is_pool` is not present in the telemetry payload.

**NFR-003:** [Backward Compatibility] The `is_pool` field must default to `false` when omitted from JSON deserialization (standard boolean zero-value behavior), ensuring existing stored telemetry remains valid.

---

## 6. Business Rules

**BR-001:** The classification of "pool filesystem" is purely informational and visual. It must not affect alerting thresholds, retention policies, or any other system behavior beyond dashboard presentation.

**BR-002:** The list of recognized pool filesystem types (FR-001) must be extensible. Adding a new pool filesystem type in the future must require changing only the single list defined per FR-003, with no other code changes.

**BR-003:** ZFS and Btrfs are included in the pool filesystem list because they commonly serve as pooled storage, even though they can also be used as single-disk filesystems. The classification is based on filesystem type, not on whether multiple disks are actually pooled.

---

## 7. Data Requirements

### Entities Modified

**Filesystem (Go model and TypeScript interface):**

| Field | Type | Change |
|-------|------|--------|
| `is_pool` | `bool` / `boolean` | New field. Indicates pool filesystem classification. JSON key: `is_pool`. Defaults to `false`. |

### Validation Rules

- `is_pool` requires no user input or validation. It is derived from `fs_type` by the agent.

### State Transitions

None. This feature adds a derived classification field with no state machine.

---

## 8. Acceptance Criteria

**AC-001: mergerfs filesystem is classified as a pool** [Maps to FR-001, FR-002, FR-011]
```
Given: A device has a mounted filesystem with fs_type "fuse.mergerfs"
When: The agent collects disk telemetry
Then: The filesystem entry must have is_pool set to true
And: All other filesystem fields (mount_point, device, total_gb, used_gb, free_gb, usage_percent) must be populated with correct values
```

**AC-002: Regular filesystem is not classified as a pool** [Maps to FR-001, FR-002, FR-011]
```
Given: A device has a mounted filesystem with fs_type "ext4"
When: The agent collects disk telemetry
Then: The filesystem entry must have is_pool set to false
```

**AC-003: Pool filesystems render in a distinct subsection** [Maps to FR-004, FR-005, FR-006, FR-008]
```
Given: A device's telemetry contains at least one filesystem with is_pool true
When: The user views the device detail page
Then: A "Storage Pools" subsection must be visible above the regular Filesystems table
And: Each pool filesystem must display mount point, filesystem type, used capacity, total capacity, free capacity, and usage percentage
And: Each pool filesystem must display a visual capacity gauge
And: The pool filesystem must not appear in the regular Filesystems table
```

**AC-004: No pool subsection when no pools exist** [Maps to FR-009]
```
Given: A device's telemetry contains zero filesystems with is_pool true
When: The user views the device detail page
Then: No "Storage Pools" subsection must be visible
And: The Filesystems table must render all filesystems as it does today
```

**AC-005: Capacity gauge uses correct color thresholds** [Maps to FR-007]
```
Given: A pool filesystem with usage_percent of 50%
When: The user views the Storage Pools subsection
Then: The capacity gauge must display in green

Given: A pool filesystem with usage_percent of 80%
When: The user views the Storage Pools subsection
Then: The capacity gauge must display in amber

Given: A pool filesystem with usage_percent of 95%
When: The user views the Storage Pools subsection
Then: The capacity gauge must display in red
```

**AC-006: Large capacity values display in TB** [Maps to FR-010]
```
Given: A pool filesystem with total_gb of 14000
When: The user views the Storage Pools subsection
Then: The total capacity must display as "13.67 TB" (14000 / 1024, to 2 decimal places)
And: The used and free capacity values must also use TB when at or above 1000 GB
```

**AC-007: Backward compatibility with old agents** [Maps to NFR-002, NFR-003]
```
Given: Telemetry from an agent that does not include the is_pool field
When: The dashboard processes the telemetry
Then: The frontend must classify filesystems as pool or non-pool based on fs_type client-side
And: The dashboard must not error or crash
```

**AC-008: ZFS and Btrfs filesystems are classified as pools** [Maps to FR-001, BR-003]
```
Given: A device has mounted filesystems with fs_type "zfs" and "btrfs"
When: The agent collects disk telemetry
Then: Both filesystem entries must have is_pool set to true
```

---

## 9. Out of Scope

- **SnapRAID status or parity information.** SnapRAID is a backup/parity tool, not a filesystem. Monitoring SnapRAID sync status, parity health, or scrub results is a separate feature.
- **Pool member drive identification.** Identifying which individual drives are members of a mergerfs pool requires parsing mergerfs configuration files or mount options. This story does not attempt to show individual member drives.
- **Pool creation, modification, or management.** rIOt is a monitoring tool. No write operations against pool configurations.
- **Historical pool capacity trending.** Showing pool usage over time (charts/graphs) is not included. Only the latest telemetry snapshot is displayed.
- **Alerting on pool-specific thresholds.** Existing disk usage alerts already cover pool filesystems by mount point. No pool-specific alert rules are added.
- **New collector or agent configuration changes.** The existing `disk` collector already collects mergerfs data. No new collector is needed. No changes to `collector.enabled` or `agent.yaml` are required.

---

## 10. Assumptions

- **A-001:** The `gopsutil` library's `disk.PartitionsWithContext(ctx, false)` call returns `fuse.mergerfs` mounts on Linux systems where mergerfs is installed and active. This has been confirmed by the fact that the current code does not filter them out and mergerfs mounts appear with `Total > 0`.
- **A-002:** The user's primary interest is visual identification and prominent display of pool filesystem capacity. The phrase "show me pools" refers to seeing the combined used/total for the mergerfs mount, which is already a single filesystem entry in the data.
- **A-003:** Including ZFS and Btrfs in the pool classification list is acceptable even though they may be used on single disks. The classification is a visual hint, not a guarantee of multi-disk pooling.
- **A-004:** The `bcachefs` filesystem type is included proactively as it is an emerging pooled filesystem that homelab users may adopt.

---

## 11. Open Questions

None. All ambiguities have been resolved through code analysis and documented assumptions.

---

## 12. Dependencies

- **Existing disk collector:** This feature depends on the disk collector already collecting mergerfs/ZFS/Btrfs filesystems correctly. Code analysis confirms this is the case.
- **Existing Filesystem model:** The `is_pool` field is added to the existing `Filesystem` struct (Go) and `Filesystem` interface (TypeScript). No new models required.
- **No database changes:** Telemetry is stored as a JSON blob. Adding a field to the Filesystem object within that JSON requires no schema migration.
- **No new dependencies:** No new libraries, services, or external systems are required.

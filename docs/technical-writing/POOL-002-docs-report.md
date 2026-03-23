# Documentation Report

**Story ID:** POOL-002
**Title:** Expand Storage Pool Detection for Unraid, mdraid, and LVM
**Author:** Technical Writer Agent
**Date:** 2026-03-23

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `CHANGELOG.md` | Updated | Added two [POOL-002] entries under [Unreleased] > Added (Unraid/mdraid/LVM pool detection, device path on pool cards) and a new [Unreleased] > Changed section with two entries (IsPoolFSType removal, PoolFSTypes/POOL_FS_TYPES expansion) |
| `README.md` | Updated | Extended the `disk` collector row in the Available Collectors table to cover all pool detection methods: filesystem-type-based (including Unraid shfs/fuse.shfs) and device-path-based (mdraid, LVM/device-mapper, with exclusion note) |

---

## Artifacts Reviewed and Not Changed

| File | Reason |
|------|--------|
| `internal/models/telemetry.go` | Inline comments are accurate and complete. The `PoolFSTypes` var carries a cross-reference to the TS mirror and a note that device-path detection lives in `IsPoolFilesystem`. The `IsPoolFilesystem` function doc comment lists the detection hierarchy, the two exclusion rules, and a pointer to the TS mirror. No stale comments found. |
| `internal/agent/collectors/disk.go` | Call site change is a one-liner with no comment needed. The pattern is self-evident. |
| `web/src/utils/filesystem.ts` | The module-level JSDoc was updated by the engineer to name all three sync locations. `isPoolFilesystem` JSDoc accurately describes the is_pool-first path and the full device-path fallback. No further changes needed. |
| `web/src/pages/DeviceDetail.tsx` | The device path line (`{fs.device && <span ...>{fs.device}</span>}`) is self-explanatory. No comment needed. |

---

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 18 | Version banner reads "README last updated for v2.38.0" — will continue to drift. Already flagged in POOL-001 docs report. | Remove or automate as part of the release workflow. |
| `web/src/pages/DeviceDetail.tsx` lines 590–592 | Pre-existing CSS class name bugs (`pr-3font-mono`, `pr-3text-gray-400`, `pr-3text-right`) in the regular filesystems table. Already flagged in POOL-001 docs report. | Fix in a separate cleanup ticket. |
| `CHANGELOG.md` | The [Unreleased] section now contains a `Changed` subsection introduced by POOL-002. A `Changed` header was absent before this story. Ensure future stories that modify existing behavior also use this subsection. | No immediate action needed; pattern is now established. |

---

## Accuracy Flags

| Item | Verified |
|------|---------|
| `PoolFSTypes` contains exactly 8 entries in alphabetical order: `bcachefs`, `btrfs`, `fuse.mergerfs`, `fuse.shfs`, `fuse.unionfs`, `mergerfs`, `shfs`, `zfs` | Yes — code at `internal/models/telemetry.go` lines 154–163 |
| `POOL_FS_TYPES` in TypeScript is alphabetically ordered and matches Go list exactly | Yes — `web/src/utils/filesystem.ts` lines 19–27 |
| `IsPoolFilesystem` detection order: fs-type list → `/dev/md` prefix → `/dev/mapper/` prefix (with Docker and live-boot exclusions) → `/dev/dm-` prefix | Yes — `internal/models/telemetry.go` lines 178–210 |
| TypeScript fallback order in `isPoolFilesystem()` mirrors Go exactly | Yes — `web/src/utils/filesystem.ts` lines 39–76 |
| Pool card device path is rendered conditionally (`{fs.device && ...}`) — no empty span when device is absent | Yes — `web/src/pages/DeviceDetail.tsx` lines 562–564 |
| `IsPoolFSType` is fully removed; no remaining call sites exist | Yes — confirmed by ADD Section 12 and implementation report |
| ADD Section 8 incorrectly states DeviceDetail component tests are "not practical" — QA wrote them and they passed | Flagged. Documented as-is (code is correct; ADD note was wrong). Not a documentation concern for user-facing or developer-facing docs. |

No discrepancies between the FRD, the ADD, and the implemented code were found.

---

## Notes for Future Writers

**Three-location detection logic.** With POOL-002, pool detection logic lives in three places that must stay in sync: `internal/models/telemetry.go` (`PoolFSTypes` list + `IsPoolFilesystem` function), `web/src/utils/filesystem.ts` (`POOL_FS_TYPES` list + `isPoolFilesystem` fallback). All three files carry cross-reference comments. When documenting any story that adds a detection rule, the CHANGELOG entry must mention the change under both Added (if a new type is visible to users) and Changed (if an existing exported symbol is modified).

**Breaking change for Go callers.** `IsPoolFSType` is gone. Any downstream code (forks, integrations) calling it must migrate to `IsPoolFilesystem(fsType, device string) bool`. The CHANGELOG `Changed` entry documents this explicitly. If this project publishes a Go module for external consumption, this warrants a major version bump. Currently it does not.

**No agent config change required.** The `disk` collector is already in every device's whitelist. No `agent.yaml` change is needed for any of the new detection methods. Do not document this feature in any "update your config" context.

**Exclusion semantics are intentionally narrow.** The Docker device-mapper exclusion applies only to `/dev/mapper/docker-*`. `/dev/dm-*` paths are not excluded even if they happen to be Docker thin-pool volumes, because those paths are indistinguishable from LVM by prefix alone. A homelab system using Docker with the `devicemapper` storage driver will see Docker's thin-pool listed as a storage pool. This is a documented design decision (ADD Section 3, AD-002), not a bug, and should not be "fixed" in documentation as if it were.

**Demo device.** The `nas-synology` demo device already uses `/dev/md0` as the device for its btrfs pool (`web/src/api/demo-data.ts`). The device path now renders on that pool card in the live demo, providing a visible confirmation of AC-014 without any change to demo data.

**AC numbering collision in `filesystem.test.ts`.** POOL-001 and POOL-002 both use `[AC-001]` and `[AC-002]` labels within the same test file. This is a test traceability issue, not a documentation issue. Noted here so a future writer is not confused if they search the test file for a specific AC. The QA report recommends prefixing story IDs (e.g., `[POOL-002/AC-001]`) in a follow-up cleanup.

# Documentation Report

**Story ID:** POOL-001
**Title:** Storage Pool Filesystem Visual Identification and Presentation
**Author:** Technical Writer Agent
**Date:** 2026-03-23

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `CHANGELOG.md` | Updated | Added three [POOL-001] entries under [Unreleased] > Added: the Storage Pools UI section, the `is_pool` telemetry field, and the exported Go/TS utility symbols |
| `README.md` | Updated | Extended the `disk` collector row in the Available Collectors table to describe pool filesystem detection and the Storage Pools card section |

---

## Artifacts Reviewed and Not Changed

| File | Reason |
|------|--------|
| `internal/models/telemetry.go` | Inline comments are accurate. The cross-reference comment on `PoolFSTypes` pointing to `web/src/utils/filesystem.ts` is present and correct. No stale comments found. |
| `internal/agent/collectors/disk.go` | No comments needed. The `IsPool` assignment follows the identical pattern as `IsNetworkMount` and is self-evident from the helper call. |
| `web/src/utils/filesystem.ts` | JSDoc comments on all three exports accurately describe behavior, fallback logic, and the two-list maintenance requirement. The cross-reference comment to `internal/models/telemetry.go` is present. |
| `web/src/pages/DeviceDetail.tsx` | The Storage Pools rendering block requires no inline comments — the logic is a straightforward filter and conditional render. |

---

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 18 | States "README last updated for v2.38.0" — this banner will drift with every story. It is unclear what version corresponds to the current HEAD. | Remove the version banner or automate it as part of the release workflow. |
| `web/src/pages/DeviceDetail.tsx` lines 587-592 | Pre-existing CSS class name bugs: `pr-3font-mono`, `pr-3text-gray-400`, `pr-3text-right` — missing space separators between Tailwind classes. Flagged by QA as Finding 3. The regular filesystem table renders without monospace font, text color, and text alignment styles on those cells. | Fix in a separate cleanup ticket. |

---

## Accuracy Flags

No discrepancies found between the ADD, FRD, and the implemented code. Specific verifications:

| Item | Verified |
|------|---------|
| `PoolFSTypes` contains exactly the 6 types listed in FR-001 (`bcachefs`, `btrfs`, `fuse.mergerfs`, `fuse.unionfs`, `mergerfs`, `zfs`), sorted alphabetically | Yes — matches ADD Section 5 and the code |
| `IsPool` field uses `json:"is_pool,omitempty"` as specified in AD-005 | Yes |
| `isPoolFilesystem()` checks `fs.is_pool !== undefined` before falling back to `fs_type` — correct for the `omitempty` behavior where the field is absent (not `false`) on old agents | Yes — code at `web/src/utils/filesystem.ts` line 29 |
| `formatCapacity()` threshold is `>= 1000` GB → TB (divided by 1024, 2 decimal places); `< 1000` GB → GB (1 decimal place) | Yes — matches FR-010 and AC-006 |
| Pool cards use `GaugeBar` with `value={fs.usage_percent}` and `max={100}` — color thresholds (>75% amber, >90% red) come from the existing `GaugeBar` component without modification | Yes |
| Pool filesystems are excluded from the regular table via `regularFilesystems = filesystems.filter(fs => !isPoolFilesystem(fs))` | Yes — satisfies FR-008 |
| The `Section title="Filesystems"` wrapper contains both the pool cards and the regular table as a single collapsible block | Yes — satisfies AD-004 |

---

## Notes for Future Writers

**Two-location pool type list.** The pool filesystem type list exists in two places that must stay in sync: `internal/models/telemetry.go` (`PoolFSTypes`) is authoritative; `web/src/utils/filesystem.ts` (`POOL_FS_TYPES`) is a fallback mirror required for backward compatibility with old agents. Both files carry cross-reference comments. When documenting any story that adds a new pool filesystem type, ensure both locations are updated and note the change in the CHANGELOG under `Changed`.

**`omitempty` on boolean fields.** The `is_pool` field uses `omitempty`, which means it is absent from JSON when `false`. The TypeScript interface declares it as `is_pool?: boolean` (optional), not `is_pool: boolean | false`. This is intentional and documented in AD-005. Any future boolean classification fields that follow the same pattern should use the same documentation approach.

**No config change for pool detection.** Pool filesystem classification is automatic — it is derived from `fs_type` by the agent. There is no `collector.enabled` entry, no `agent.yaml` key, and no server environment variable involved. Do not document this feature in any "configuration required" context.

**Demo device for visual validation.** The `nas-synology` demo device has a btrfs filesystem at `/volume1` with `is_pool: true` set in `web/src/api/demo-data.ts`. This is the canonical entry point for visually verifying the Storage Pools section in the live demo at `rbretschneider.github.io/rIOt/`.

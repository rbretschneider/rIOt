# DIETPI-PATH-001 — Architecture Decision Document (bugfix)

**FRD:** `docs/requirements/DIETPI-PATH-001-frd.md`
**Amends:** `docs/architecture/EXTRA-UPD-001-add.md`: AD-005 check 5, AD-007 (launcher) and D-10, plus EXTRA-UPD-001 FR-038 and FR-039
**Date:** 2026-10-26
**Status:** Draft for security review

## 1. Summary

| Problem | Decision |
|---|---|
| The trust walk includes `/run/dietpi`, which stock DietPi makes `0777` (F-1) | Remove `/run/dietpi` from `TrustRoots` (AD-1) |
| The trust walk descends `/var/tmp/dietpi`, which holds a mirror of `/var/log` (F-6) | Replace the walk with exact `lstat` checks of the three paths root interprets (AD-1) |
| Root writes into a world-writable, non-sticky `/run/dietpi` follow symlinks (F-3) | The launcher mounts a private tmpfs on `/run/dietpi` for the step (AD-2) |
| Host flags go stale because the step's writes are now private (F-5) | After a successful step, the worker removes the two flags DietPi would have removed, without following symlinks (AD-3) |
| A local user can forge flags in `/run/dietpi` | Detection believes a flag only when the trusted owner wrote it (AD-4) |
| A refused step fails a good apt run (F-8), and the badge says "Needs you" for something Ryan cannot act on | A skip before start is `success`, plus a warning event. The badge gets a new `blocked` kind for safety and temporary refusals, and "Needs you" only for refusals Ryan can fix (AD-5) |

No migration. No sudoers change. No change to the request or record protocol fields. One new skip reason, `rundir_not_private`.

## 2. Decisions

### AD-1 — What the trust walk covers

- **`extraupd.ApplySpec`:**
  - `TrustRoots` becomes `{"/boot/dietpi", "/boot/dietpi.txt"}`. These are walked as before: tree, depth 4, 4096 entries, no vfat special case.
  - New field `ExactRoots []ExactRoot`, where `ExactRoot{Path string; Kind ExactKind}` and `Kind` is `ExactDir` or `ExactFile`. For DietPi:
    - `{"/var/tmp/dietpi", ExactDir}`
    - `{"/var/tmp/dietpi/logs", ExactDir}`
    - `{"/var/tmp/dietpi/dietpi-update_reinstalls", ExactFile}`
  - `RequiredRoots` is unchanged: `/var/tmp/dietpi` is required while the installed version is below 10.4.
- **`maintenance.trustWalk`:** after the tree roots, check each `ExactRoot`:
  - its ancestors pass `checkAncestor` (root-owned; group or world write only with the sticky bit);
  - `lstat` of the path:
    - absent is OK unless required;
    - it must be uid 0, not a symlink, and of the right kind;
    - mode `& 0o022 == 0`;
    - for `ExactFile`, nlink must be 1. `trustStat` gains `Nlink uint64`; the production `trustFS` fills it from `Stat_t`.
  - No `ReadDir`. Nothing below the exact paths is checked.
- **`trustParents`** stays `{"/", "/boot", "/run", "/var", "/var/tmp"}`. `/run` is still required root-owned 0755.
- **Why this is safe:** per F-2 and F-7, root executes nothing from `/run/dietpi` or `/var/tmp/dietpi`. The one root-interpreted file is `dietpi-update_reinstalls`. It and its directory chain are proved root-only. Below `logs/`, entries are copied as data with `cp -a`: no dereference, ownership preserved, never executed. A local user who owns a mirrored log directory can already write there; the copy grants nothing new.

### AD-2 — Private `/run/dietpi` in the launcher

The steps are added after the existing step 8 (the `/tmp` proof) and before `chdir`/exec in `launcher.go`.

1. If `ops.StatID("/run/dietpi")` reports not-exist, skip to exec.
   - Nothing can create it meanwhile: `/run` is a root-only directory, proved by the trust walk.
   - DietPi then creates it itself as root with umask 0022.
2. `/run` must be its own mount point (`topEntry(self, "/run")`). Otherwise fail with `rundir_not_private`.
3. `ops.Mount("", "/run", "", MS_REC|MS_PRIVATE, "")`. This changes only this namespace's copy. `/`, `/boot` and `/boot/firmware` keep their propagation, and that was already proved in step 3.
4. `ops.Mount("tmpfs", "/run/dietpi", "tmpfs", MS_NOSUID|MS_NODEV|MS_NOEXEC, "mode=0755,size=1m,nr_inodes=64")`.
5. Proof (`proveRunDirIsolation`):
   - `StatfsType == tmpfsMagic`;
   - `StatID` uid 0;
   - mode `0755`. `nsOps.StatID` gains `Mode`;
   - `EntryCount == 0`;
   - dev/ino differ from `/proc/1/root/run/dietpi`;
   - the own mountinfo `topEntry("/run/dietpi")` is not shared.
6. Any failure in steps 2 to 5 sends `err rundir_not_private <detail>` to the status pipe. The launcher does not exec. The worker maps it to `skipped (rundir_not_private)` exactly like `tmp_not_private`.

**What DietPi sees:** an empty `/run/dietpi`.

- It writes `.update_available` and `.apt_updates` there, and removes flags there. All of that dies with the namespace.
- F-2: it never reads them.
- The rest of `/run` is the host's: the systemd sockets and `/run/lock` are the same mount, now with private propagation in this namespace only. `dietpi-services` talks to systemd as before.

The symlink-clobber exposure from DietPi's own daily `dietpi-update 2` (F-4) remains. It is an upstream issue, documented as a known limit with an upstream recommendation: tmpfiles mode `1777`.

### AD-3 — Host flag reconciliation after a successful step

The new `maintenance.reconcileDietPiFlags(dir string) (note string)` uses an injected `flagOps` interface; production is linux-only.

- Open `/run/dietpi` with `O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC`, then `fstat`:
  - uid must be 0. If not, return the note `/run/dietpi not root-owned; flags left`.
  - Mode `0777` is expected and accepted.
- `unlinkat(fd, name, 0)` for `.update_available` and `.live_patches`:
  - `ENOENT` is ignored;
  - other errors are collected into one note (bounded to 200 bytes);
  - a symlink is unlinked itself and never followed.
- It is called in `executeExtras`:
  - only for a run with outcome `success` and spec `ReconcileFlags: true` (DietPi);
  - before the post-run `Detect` (FR-009);
  - the note is appended to `UpdaterResult.Note` and never changes the outcome.
- Non-linux builds return `""`.

**Why:** F-5 says a successful `dietpi-update 1` removes exactly these two. Without this, the host keeps:

- a stale `.update_available`. Detection already ignores this one, because the flag's version is not newer than the installed version.
- a stale `.live_patches`. This one would read as pending until reboot and keep re-triggering auto-patch.

`.apt_updates` is left alone. rIOt's apt step already triggered DietPi's `DPkg::Pre-Install-Pkgs` hook, which removes it whenever packages changed, and DietPi's daily check rewrites it.

### AD-4 — Flag trust

- `dirFS` gains `owner uint32`:
  - `OSRoot()` and `DirFS(root)` use 0, which is the production behaviour;
  - the new `DirFSOwnedBy(root string, uid uint32) FS` is for test fixtures only. Its doc comment says so, and no production caller may use it (guarded by a test that greps non-test code).
- New method on the FS interface: `Owner() uint32`.
- New helper `trustedFlag(fsys, name) bool`:
  - `Lstat` regular;
  - mode `& 0o022 == 0`;
  - the owner from `Sys().(*syscall.Stat_t).Uid` equals `fsys.Owner()`. On non-linux the owner check is skipped, because DietPi is linux-only and this keeps Windows unit tests working.
- In `detect()`, the flag and live-patch reads happen only when `trustedFlag` is true. Otherwise the flag is absent. A flag that is present but untrusted and unreadable is still absent; it never yields `detection_failed`.
- Existing tests in `extraupd`, `agent` and `maintenance` that build fixtures with `DirFS` switch to `DirFSOwnedBy(root, uint32(os.Getuid()))` through a shared helper. This also makes them pass under a non-root CI user.

### AD-5 — Outcome, event and badge

**Worker** (`protocol.go`):
- `UpdaterResult.Failing()` returns true only for an updater that started and did not succeed: `failed`, `timed_out` or `interrupted`. Every `skipped` outcome returns false.
- New `UpdaterResult.Refused() bool`, defined as `Outcome == skipped && !NeutralSkip(Reason) && Reason != apt_not_successful`.
- `Record.Validate` keeps its rule, which now uses the new `Failing()`.
- `finalizeV2` is unchanged in structure.
- `summaryV2` is unchanged: the first line already names the skip.

**Agent:**
- Any agent-side skip that today fails the command follows the same rule. Example: `insufficient_time` when the run has no stamp (EXTRA-UPD-001 AC-022 "enabled, no stamp").
- `reportExtraProgress` sends one `extra_updater_finished` event for a result where `Refused()` is true. The event has outcome `skipped`, the new optional `AgentEvent.Reason`, severity warning, and the same durable once-marker key `<id>:end`.
- No start event is sent for a refused step.

**Server:**
- `agent_event_extra.go` accepts `Reason` only when it is in `maintenance.SkipReasons()`; otherwise it is dropped.
- The message is `DietPi update skipped on <host>: <reason words> (command <id>, origin <o>)`.
- Dedupe is unchanged.

**Badge** (amended per FRD FR-015 to FR-017: "Needs you" only when Ryan can act):

Parse the last run once. `parseRefusals(LastRun)` reads at most the first 2048 bytes of `ResultMsg`, split into lines, plus at most 8 updater entries.
- Line 1 must match `^apt: succeeded(; [a-z][a-z0-9_-]{0,31}: [a-z_ ()0-9]+)+$`.
- Every `<id>: skipped (<reason>)` with `<reason>` in the known non-neutral set is a refusal.
- The detail for that id is the first later line starting `<id>: skipped (<reason>); `. The remainder is control-character-stripped and bounded to 160 bytes.
- Anything that does not match is ignored: no refusal, and the old behaviour applies.

The parse applies when `LastRun.Status` is success. It also applies when the status is failed and line 1 says `apt: succeeded` and every non-success part of line 1 is a refusal (FR-016, an old worker). In that case NY-9 (`last_run_failed`) is not raised for that run.

For each refusal whose updater is still `Pending()` and `Enabled()`:
- **Blocked class** (`unsafe_dietpi_path`, `tmp_not_private`, `mount_propagation_unverified`, `rundir_not_private`, `dietpi_busy`, `min_interval`, `dpkg_locked`):
  - add a reason with the new code `extra_blocked` to a new `Blocked` list in the explanation;
  - text: `<Label> update blocked: <plain words>`, plus `. <detail>` when present;
  - action text: `Nothing to do; rIOt retries on the next patch run.`;
  - the pending-extra info item (`extraPendingText`, "will be applied ...") is suppressed for that updater.
- **Action class** (`non_default_update_source`, `insufficient_time`): an action item `extra_refused`, with a per-reason action:
  - "Set DEV_GITOWNER back to MichaIng in /boot/dietpi.txt.";
  - "Lengthen this host's patch window to at least 90 minutes.".

When the updater is no longer pending, the refusal becomes one info line, "The last patch run skipped <Label>: <words>.".

**Plain words** (server table):

| Reason | Words |
|---|---|
| `unsafe_dietpi_path` | `<path> permissions`, taking the path from the detail's first token when it starts with `/`, else "a DietPi path failed rIOt's safety check" |
| `tmp_not_private` | "rIOt could not isolate /tmp" |
| `mount_propagation_unverified` | "mount propagation could not be verified" |
| `rundir_not_private` | "rIOt could not isolate /run/dietpi" |
| `dietpi_busy` | "another DietPi process was running" |
| `min_interval` | "DietPi ran less than an hour earlier" |
| `dpkg_locked` | "the package lock was held" |

**Kind precedence:**
- any action item → `needs_you`;
- else any Blocked item → the new kind `blocked`;
- else as today.

`blocked` is not counted by `needs_you` filters. `GET /api/v1/fleet/patch-status` returns `patch_badge.kind: "blocked"` and `explanation.blocked: []`; the list is always present and may be empty. A client that matches kinds exactly must accept the new kind (CHANGELOG).

**Web:**
- `PatchBadge.tsx`: amber "Blocked" style, label and tooltip list.
- `FleetOverview.tsx`: the `needsYouIds` filter excludes `blocked`, and a small "N blocked" chip or filter is added beside it, following the existing pattern.
- Types: `blocked` kind and `explanation.blocked`.
- Words for `rundir_not_private` and the reason code.
- Demo data: one Blocked host.

### AD-6 — Compatibility and rollout

- **Bundling:** the worker (`riot-maintenance`) and the agent ship together. `install.sh` installs both at one version.
  - A new agent with an old worker: the old worker marks refusals `error`, the new agent accepts that, and the command shows `failed` as today.
  - A new worker with an old agent: the old agent's `Validate` would reject a `success` record carrying a refused skip. This pairing does not arise: `install.sh` installs both, and the agent self-update never changes the worker. The ops docs say to re-run `install.sh`, never to copy the worker alone.
- **Reaching the hosts:** the fix reaches PiCUPS and PiVPN only through one more root `install.sh` re-run per Pi. The agent self-update alone does not change the worker.
  - The badge and event changes need only the server and the dashboard.
  - The new outcome rule needs the new agent and the new worker together.
- **Server compatibility:** an old server ignores `Reason`. A new server tolerates its absence.

## 3. Bounds (no unbounded growth)

- **Exact checks:** 3 `lstat` calls.
- **Tmpfs:** 1 MiB and 64 inodes, gone with the namespace.
- **Reconciliation:** 2 `unlinkat` calls; the note is at most 200 bytes.
- **Event:** at most one per command and updater, with the existing durable marker.
- **Badge:** the regex reads at most 512 bytes of the stored result.

## 4. Files

| File | Change |
|---|---|
| `internal/extraupd/extraupd.go`, `dietpi.go`, `fs.go`, `fs_linux.go`, `fs_other.go` | `ExactRoots`, `ReconcileFlags`, `TrustRoots`; `Owner()`, `DirFSOwnedBy` and `trustedFlag` |
| `internal/maintenance/extra_trust.go`, `io_linux.go` (production `trustFS`) | exact checks, `Nlink` |
| `internal/maintenance/launcher.go`, `nsops*.go` | AD-2 steps, `StatID.Mode`, `rundir_not_private` |
| `internal/maintenance/extra.go`, new `flags_linux.go` and `flags_other.go` | AD-3 |
| `internal/maintenance/protocol.go` | `Failing`, `Refused`, `SkipRunDirNotPrivate`, `SkipReasons()` |
| `internal/agent/extra_updaters.go`, readiness and outcome code | AD-5 agent part |
| `internal/models/events.go` (`AgentEvent.Reason`), `internal/server/handlers/agent_event_extra.go`, `patch_badge.go` | AD-5 server part |
| `web/src/utils/extraUpdaters.ts`, badge reason words | words |
| tests in every package above | per §5 |
| `docs/ops/os-maintenance.md`, `README.md`, `CHANGELOG.md` | technical writer |

## 5. Test map

| AC | Test (name pattern `_DP_ACnnn_`) |
|---|---|
| AC-001 to AC-003 | `TestTrustWalk_DP_AC001_StockDietPiPasses`, `_AC002_BootTreeStillStrict`, `_AC003_VarTmpExactPaths` (in-memory `trustFS`) |
| AC-004 to AC-006 | `TestLauncher_DP_AC004_PrivateRunDir`, `_AC005_RunDirFailsClosed`, `_AC006_NoRunDirNoMount` (fake `nsOps`); the opt-in `RIOT_NS_FIXTURE` real-namespace test is extended: a file written to `/run/dietpi` inside stays invisible outside, and a mount under `/boot` still propagates |
| AC-007, AC-008 | `TestReconcileFlags_DP_AC007_RemovesOnlyTwoNeverFollows`, `_AC008_UnsafeDirUntouched` (linux, real temp dir) |
| AC-009 | `TestDetect_DP_AC009_UntrustedFlagIsAbsent`; guard test: no production use of `DirFSOwnedBy` |
| AC-010 | `TestExtras_DP_AC010_RefusalKeepsSuccess` (table over every non-neutral reason) plus the existing failed, timed-out and interrupted tests |
| AC-011 | `TestExtraEvents_DP_AC011_OneRefusalEvent`, server `TestAgentEventExtra_DP_AC011_UnknownReasonDropped` |
| AC-012, AC-014 to AC-016 | `TestPatchBadge_DP_AC012_RefusalIsBlocked`, `_AC014_ReasonClasses`, `_AC015_OldWorkerFailedRefusalIsBlocked`, `_AC016_ActionWinsOverBlocked`, `TestParseRefusals_DP_Bounds` (garbage, oversize, control characters) |
| AC-017 | `PatchBadge.blocked.test.tsx`, `FleetOverview` filter test |
| AC-013 | `TestExtras_DP_AC013_PiCUPSLayoutRuns` (worker rig with the PiCUPS layout) |

EXTRA-UPD-001 tests that assert the old FR-038 behaviour (refusal leads to `error`) are updated to FR-012. Each change is listed in the implementation report.

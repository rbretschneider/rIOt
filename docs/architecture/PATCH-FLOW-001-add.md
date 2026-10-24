# Architecture Decision Document

- **Story ID:** PATCH-FLOW-001
- **FRD Reference:** `docs/requirements/PATCH-FLOW-001-frd.md`
- **Amends:** PATCH-GATE FR-023 / BR-005 / AD-009 item 4 (manual dispatch stripping), and the PATCH-BADGE-001 per-host badge decision rule NY-10 and Reboot line (see AD-001, OQ-009 below)
- **Author:** Architect Agent
- **Date:** 2026-10-23
- **Status:** FINAL (revision 2: AD-012 reboot-intent pruning added per coordinator ruling; pending security review, FRD D-010)

**Product-owner rulings adopted (Ryan, 2026-10-23):** OQ-001 to OQ-008 take the FRD's recommended defaults:
failed run never reboots; a failed post-run refresh is a note, not a failure; the checkbox applies to dnf with the same
semantics; no new automatic list refresh while stale; security score out of scope; the server rejects an authorization
it cannot honor; only `sources.list`, `sources.list.d/` and `preferences.d/` count for staleness; stale devices are left
out of fleet aggregate package totals.

**OQ-009 resolution.** The badge decision table (NY-1 to NY-10, `manual_reboot_needed`, the Reboot line) was introduced
by story **PATCH-BADGE-001** (CHANGELOG `[Unreleased] / Added`, code `internal/server/handlers/patch_badge.go`). No
`docs/requirements/PATCH-BADGE-001-frd.md` or `docs/architecture/PATCH-BADGE-001-add.md` exists in the repository (grep
for `NY-10`, `computePatchBadge`, `needs_you`, `manual_reboot_needed` under `docs/` finds only this story's FRD). The
amendment is therefore recorded here (AD-001) and in the `computePatchBadge` doc comment. FR-003 to FR-006 and FR-036
replace the NY-10 trigger, its cause list, and the Reboot line wording.

**Revision 2 (coordinator ruling on hand-off flags).** Flags 1 to 6 and 8 are accepted as written. Flag 7 (the
non-pruning reboot-intent store) is resolved inside this story by **AD-012**. The intent file prunes intents older than
30 days on every claim. The 256 cap becomes an oldest-first eviction backstop that never evicts an intent younger than
72 h. At-most-once is preserved for every command that could still be redelivered.

---

## 1. Summary

The badge and every "reboot needed" surface now key only on `reboot_required`. The agent widens that fact with an
installed-versus-running kernel comparison, and a pending kernel/GPU update is described as a future reboot. A manual
`os_update` gains one boolean param, `reboot_if_required`. The server validates it at the API boundary against the
host's last-reported `allow_reboot` and a new capability fact, and only then derives `include_reboot_class = true`. The
agent reboots after a successful authorized run if and only if a reboot is required afterwards and
`commands.allow_reboot` is true. It records the decision in the result before rebooting, and the `claimPatchReboot`
intent store keeps it to at most one reboot per command. That store now prunes itself (AD-012).

Stale apt lists are detected agent-side from directory and file mtimes (no new privilege) and reported as
`lists_freshness`. The UI and badge then show "Package lists out of date" instead of a count. The privileged helper
(new build, reaches a device only via `install.sh`) refreshes once more when the run changed apt configuration, and
stamps the lists directory after every successful refresh. The helper request/record protocol is unchanged, so old
helpers keep working with new agents. The agent pushes updates telemetry right after every `os_update` result. No
database migration.

## 2. Technical Context

Verified on `feat/patch-flow-001` (base `c4ae209`):

- **Badge.** `computePatchBadge` (`patch_badge.go:120`) computes
  `rebootExpected := RebootRequired || PendingRebootClassCount > 0` (line 139). That one value drives NY-10 (line 217)
  and the Reboot line (lines 250–275). The NY-10 cause list adds "no kernel or GPU update is pending" whenever
  `PendingRebootClassCount == 0` (line 218). This is defect D1. The badge is pure and display-only, and is called only
  from `PatchStatus` (`fleet.go:121`).
- **Manual dispatch.** `SendCommand` (`commands.go:58`) deletes `include_reboot_class` for every manual `os_update`.
  Errors use `dockerError(w, status, code)`, which returns `{"error","code"}`. `CommandSent` (`events/generator.go:230`)
  builds a message that carries no params.
- **Automated dispatch.** `checkAutoPatch` (`auto_update.go:545`) sends `{"mode":"full"}`, plus
  `include_reboot_class: true` under the `gated` policy. It never sees freshness. This story leaves it **unchanged**.
- **Agent `os_update`.** `executeAction` (`agent/commands.go:168`) attaches
  `postResult = maybeRebootAfterPatch`. The apt path goes through `runDetachedMaintenance` (`maintenance.go:141`), a
  request to the root helper. The dnf path runs inline (`runOSUpdate`).
  - The reboot rule today is "reboot-class packages actually upgraded (`RebootClassApplied`) and `allow_reboot`".
  - The reboot is at most once through `claimPatchReboot` (`maintenance.go:398`): a JSON `map[id]bool` in
    `patch-reboot-intents.json`. It has **no timestamps and no pruning**, and returns false forever once 256 entries
    exist.
  - It also takes a host-lock reservation, and the reboot runs after a 2 s delay from `postResult`. `postResult` runs
    after `out.Add` (durable) and `inflight.Remove` (`command_lifecycle.go:552–567`), after `runAction` has released
    the command's own locks.
  - Recovered maintenance results never reboot (`observeMaintenance`, `recovered=true`).
- **Redelivery windows** (they bound AD-012): `QueuedTTL(os_update)` 30 min and `ExecDeadline` 40 min
  (`command_policy.go`); `SeenMaxAge` 48 h and `OutboxMaxAge` 24 h.
- **Helper.** `Worker.Execute` (`maintenance/worker.go:52`) runs, in order:
  1. hold snapshot and release
  2. configure → verify, with one repair attempt
  3. (`repair` mode stops here)
  4. `refresh` (`apt-get -o Acquire::Retries=0 update`)
  5. `upgrade`
  6. verify
  7. deferred hold restore and `Changes`

  The success summary is the fixed string "Package state verified; maintenance completed". Output is bounded by
  `appendBounded` (`MaxOutputBytes` = 256 KiB) and the budget is `WorkerBudget` (30 min). The deferred finisher turns
  any `ctx.Err()` into `interrupted`, and `Record.Validate` rejects a success that has `Interrupted=true`.
  `CommandAllowed` (`runner_linux.go:26`) already permits the exact refresh argv.
- **Helper protocol.** `maintenance.ParseRequest` (`protocol.go:69`) requires exactly four fields and rejects unknown
  fields. `parseRecord` uses `DisallowUnknownFields`. Any new request or record field would break an old helper or an
  old reader. **This design adds none** (AD-008).
- **Updates collector.** `collectAPT` (`collectors/updates.go:75`) parses `apt list --upgradable` as the unprivileged
  `riot` user. Reboot detection on apt is the marker file only (`detectRebootRequiredAPT`, line 228). There is **no
  kernel comparison** today (FRD A-005: the gap is real). dnf uses `dnf needs-restarting -r`. The collector runs every
  telemetry cycle (`collectAll`, `telemetry.go:156`). `telemetryLoop` (`agent.go:389`) sends once immediately at start,
  then every `poll_interval` (default 60 s), and also on `telemetryNow`, a 1-slot channel that coalesces triggers.
- **Agent facts.** `telemetry.go:181–188` stamps `AllowReboot`, `AllowPatching` and `HoldRebootClass` onto
  `UpdateInfo`.
- **Server telemetry reads.** `GetAllLatestSummaries` decodes the whole `updates` JSONB object into
  `models.UpdateInfo`, so new fields flow through with no SQL change. `GetLatestSnapshot` restores dropped sections.
- **Web.**
  - `DeviceDetail.tsx:263` shows `Patch (N)` when `pending_updates > 0`. The single `ConfirmDialog` (line 531) sends
    `{mode:'full'}`, and `ConfirmDialog` accepts `children`. Reboot is in the Power menu (line 359).
  - `FleetOverview.tsx` has the "Pending OS Patches" panel `PatchReviewModal` (line 415) with aggregate `totalPkgs`
    and `totalSecurity`, the device-table count cell (line 322), and the `pendingPatchCount` stat (line 42).
  - No web surface renders a reboot-required indicator from `reboot_class_count`. The REBOOT-ETA tooltip was replaced
    by the server badge.
- **Conventions.** chi, pgx, slog, `go:embed` migrations, test seams as nil-means-production `Agent` fields, vitest +
  Testing Library, Tailwind, and the `scrollbar-thin` rule.

**Missing:** kernel comparison; list freshness; the reboot-authorization param and capability; the post-run refresh;
the post-run telemetry trigger on non-reboot paths; the badge rewording; and pruning for the reboot-intent store.

## 3. Architecture Decisions

### AD-001: Badge keys "reboot needed" on `reboot_required` only (FR-001, FR-003–FR-007, FR-036; amends PATCH-BADGE-001 NY-10)

**Decision.** In `computePatchBadge`, replace `rebootExpected` with three values:

```
requiredNow   := u != nil && u.RebootRequired
stale         := u != nil && u.Freshness() == models.ListsStale
pendingRC     := u != nil && !stale && u.PendingRebootClassCount > 0
autoRebootSetUp (unchanged formula, but its `PendingRebootClassCount > 0` term becomes `pendingRC`)
autoCovers    := (requiredNow || pendingRC) && autoRebootSetUp
```

**NY-10 (`manual_reboot_needed`)** is raised iff `canPatch && requiredNow && !autoCovers`.

- **Text:** `"A reboot is required now and nothing will reboot this host automatically: " + join(causes, "; ") + "."`
- **Causes**, in this order, each only when true:
  1. `!pendingRC`: "rIOt reboots automatically only after an automatic patch run installs a kernel or GPU update"
  2. `!gated`: "the reboot_class policy is not gated"
  3. `allow_reboot` reported false: "allow_reboot is off on the agent"
  4. `hold_reboot_class` reported false: "hold_reboot_class is off on the agent"
  5. enforcement `unsupported` or `no_privilege`: "hold enforcement is not working on the agent"
  6. `!autoPatchSetUp`: "automatic patching is not set up"

  The string "no kernel or GPU update is pending" is removed from the code (FR-006, AC-004). The list is never empty:
  if `pendingRC` is true and `autoRebootSetUp` is false, one of causes 2 to 6 holds by the formula.
- **Action:** `"Reboot this host now."`. When `allow_reboot` is reported false:
  `"Reboot this host now, outside rIOt (commands.allow_reboot is off on the agent)."`

**Reboot line** (exact strings; `<D>` is `nextStart.Format("2006-01-02 15:04")`; `<why>` is today's "date Unknown"
cause list; `K` is the prefix `"The pending patch includes a kernel/GPU update; a reboot will be needed after it installs."`):

| Condition | Reboot line |
|---|---|
| `!requiredNow && !pendingRC` (includes stale) | `Not needed.` |
| `!requiredNow && pendingRC && !autoRebootSetUp` | `K + " It will not happen automatically."` |
| `!requiredNow && pendingRC && autoRebootSetUp`, reasons > 0 | `K + " It will happen automatically once the items below are resolved (date not shown)."` |
| `!requiredNow && pendingRC && autoRebootSetUp`, no reasons, window + `allow_reboot` true | `K + " It will happen automatically on <D> UTC (estimate)."` |
| `!requiredNow && pendingRC && autoRebootSetUp`, no reasons, otherwise | `K + " It will happen automatically, date Unknown: <why>."` |
| `requiredNow && !autoCovers` | `Required now: reboot this host now; nothing will reboot it automatically.` |
| `requiredNow && autoCovers`, reasons > 0 | `Required now. Automatic once the items below are resolved (date not shown).` |
| `requiredNow && autoCovers`, no reasons, window + `allow_reboot` true | `Required now. Automatic on <D> UTC (estimate).` |
| `requiredNow && autoCovers`, no reasons, otherwise | `Required now. Automatic, date Unknown: <why>.` |

**Kind:**

- reasons > 0 → `needs_you`
- else `autoCovers` → `auto_reboot_date` (window mode and `allow_reboot` true; label and `RebootAt` as today) or
  `auto_reboot_unknown`
- else → `auto`

A pending reboot-class update with no automatic path and no other reason is `auto` with the "will not happen
automatically" line. Patching is automatic and no reboot is required yet.

**Stale note (FR-036).** When `stale`, append to `Notes`: `"Package lists are out of date on this host. Pending updates
will be recounted after the next refresh; running Patch refreshes the package lists first."` No reason is derived from
the pending list while stale, because `pendingRC` is false. NY-6 (from `HeldPackages`), NY-8 (release upgrade), NY-9
(last run) and NY-10 (from `reboot_required`) are not pending-list reasons and stay active.

**Rationale.** One boolean (`requiredNow`) owns "reboot now" (BR-001). The pending signal only feeds the future-reboot
wording and the automatic-cover computation. Gating `pendingRC` on freshness gives FR-036 by construction.

**Alternatives considered.**
- *Keep `rebootExpected` and special-case the text.* Rejected: the needs-you reason would still fire for pending-only
  hosts (D1).
- *A separate "pending kernel" needs-you reason.* Rejected: FR-003 forbids a needs-you item for pending-only state.

**Consequences.** Several PATCH-BADGE-001 tests assert the old wording or rely on pending-only NY-10 (§4). REBOOT-ETA
and PATCH-GATE FR-032 indicators already key on `reboot_required` (FR-007): no change, only regression tests.

---

### AD-002: Widened `reboot_required` and one shared evaluator (FR-002, FR-019)

**Decision.** New file `internal/agent/collectors/kernelreboot.go` with:

```go
// RebootRequiredNow evaluates FR-002 for pm ("apt" | "dnf"). Sources that cannot be evaluated count as false.
func RebootRequiredNow(ctx context.Context, pm string) bool
func kernelNewerThanRunning(root string) (newer bool, installed, running string, ok bool)
func compareDebVersion(a, b string) int // dpkg verrevcmp semantics, pure Go
```

- **Running kernel:** the trimmed content of `<root>/proc/sys/kernel/osrelease`, read with a 256-byte cap.
- **Installed kernels:** names `vmlinuz-<release>` in `<root>/boot`. The read examines at most 256 entries.
  Directories are ignored. Plain `vmlinuz` and `vmlinuz.old` are excluded by the prefix rule.
- **Flavor:** the token after the last `-` in a release, when that token contains a letter (`amd64`, `generic`,
  `rpi-v8` → `v8`). Only releases whose flavor equals the running kernel's flavor are compared.
- **Comparison:** `compareDebVersion` over the releases with the flavor suffix removed (`7.2.9+deb13` against
  `6.12.48+deb13`; `6.8.0-47` against `6.8.0-45`).
- `newer` is true when some installed same-flavor release compares greater than the running one.
- `ok` is false when the running release cannot be read, `/boot` cannot be listed, or the 256-entry bound is reached.
  Not ok counts as "not required" (FR-002, last sentence).
- Release strings must match `^[A-Za-z0-9.+~_-]{1,64}$`. Others are skipped.

`UpdatesCollector.detectRebootRequiredAPT` becomes "marker OR kernel-newer". When the kernel source fires, it appends
one reason `kernel <installed> installed; running <running>`, at most 160 bytes, to `RebootRequiredReasons`.
`RebootRequiredNow("apt")` = marker exists OR kernel-newer. `RebootRequiredNow("dnf")` = `dnf needs-restarting -r`
exit 1 under a 20 s timeout. Any other `pm` → false.

The agent's post-run evaluation (AD-005) calls the same function through a seam. So the collector and the reboot
decision can never disagree.

**Test seams.** `UpdatesCollector.kernelRoot string` and `RebootRequiredNow`'s internal `root` (both "" = "/"). Tests
build a temp tree and run on Windows and Linux.

**Rationale.** `/boot/vmlinuz-*` disappears when a kernel package is purged, while `/lib/modules/*` can keep leftovers
(DKMS output). That is why `/boot` is the installed-set source. Flavor scoping avoids `generic` vs `lowlatency` false
positives. A pure comparator avoids a process spawn per cycle.

**Alternatives considered.**
- *`dpkg --compare-versions` exec.* Rejected: a spawn per cycle, and per post-run decision, for a 40-line pure
  function.
- *`/lib/modules` listing.* Rejected for leftovers.
- *`needrestart -k`.* Rejected: not installed everywhere, and it needs root for some checks.

**Consequences.**
- A host deliberately booted into an older kernel reports `reboot_required=true` until it boots the newest one. That
  is correct by FR-002, and README must say so.
- A GPU-driver-only upgrade on a host that does not write the reboot-required marker does **not** count as "reboot
  required". So an authorized run does not reboot for it. The automated path still reboots on applied reboot-class
  packages (FR-026). Accepted by Ryan (flag 4).

---

### AD-003: Package-list freshness, agent side (FR-029–FR-033, FR-037–FR-039, NFR-004)

**Decision.** New shared enumerator `internal/maintenance/aptsources.go` (no build tag; the helper and collectors
already import `maintenance`):

```go
type AptSourceEntry struct { Path string; ModTime time.Time; Size int64 }
// AptSourceEntries lists the apt-effective files under root's /etc/apt/sources.list,
// sources.list.d and preferences.d, examining at most budget directory entries.
func AptSourceEntries(root string, budget int) ([]AptSourceEntry, error) // ErrAptBudget when exceeded
func AptSourcesFingerprint(entries []AptSourceEntry) string            // canonical "path|mtime-ns|size\n" joined, sorted
```

**Apt-effective files** (the files apt actually reads; this interprets FR-029/OQ-007, accepted by Ryan, flag 3):

- `sources.list`, if it is a regular file
- `sources.list.d/*.list` and `sources.list.d/*.sources`
- `preferences.d/<name>` where `<name>` matches `^[A-Za-z0-9_.-]+$` and either contains no `.` or ends in `.pref`

Each entry is checked with `os.Stat`, which follows symlinks. Subdirectories, and backups such as `.dpkg-old`,
`.dpkg-dist`, `.save` and `~`, are skipped. A missing directory or a missing `sources.list` is not an error.

New file `internal/agent/collectors/listsfresh.go`:
`func listsFreshness(root string, now time.Time, deadline time.Time) (state string, reason string)`. The algorithm runs
these steps in order, examines at most **1,000 entries in total**, and stops at a **2 s** deadline. It checks the
deadline between `ReadDir(128)` chunks.

1. **Refreshed-at time** = max(mtime of `<root>/var/lib/apt/lists`, mtime of
   `<root>/var/lib/apt/periodic/update-success-stamp` if present).
   - If the lists dir is missing (`ENOENT`) → `stale` (FR-030).
   - Any other stat error → `unknown`.
2. **Lists exist.** Scan `<root>/var/lib/apt/lists` for a regular file whose name contains `_Packages`. Stop at the
   first hit.
   - None after a complete scan → `stale` (FR-030).
   - Budget or deadline reached first → `unknown`.
3. **Config entries.** `AptSourceEntries(root, remaining budget)`.
   - Error or budget reached → `unknown`.
   - An entry whose mtime is more than 5 min after `now` → `unknown` (clock-skewed file: never a permanent false
     stale).
4. **Decide.** `stale` if any entry's mtime is after the refreshed-at time, else `fresh`.

`collectAPT` sets `info.ListsFreshness` after `apt list --upgradable` and still reports the raw count and list
(FR-032).

- **Logging:** exactly one `slog.Debug("updates: package-list freshness unknown", "reason", reason)`, only when the
  result is `unknown`. Nothing else is logged.
- **No files are created.**
- **dnf, pacman, apk and non-Linux** leave the field empty (absent), which reads as `unknown` (FR-038).
- **Seams:** `UpdatesCollector.aptRoot string` ("" = "/") and `listsNow func() time.Time`.

**Server normalization (FR-033).** `models.ListsFreshness` is a named string with a lenient `UnmarshalJSON`:

- `"fresh"`, `"stale"` and `"unknown"` are kept.
- Any other JSON string, number, bool, array or object becomes `"unknown"`.
- `null` or an absent field leaves `""`.
- It never returns an error, so the ingest stays `2xx` (NFR-009).
- `(*UpdateInfo).Freshness()` returns `fresh|stale|unknown`, with `""` → `unknown`.

**Display-only (FR-039).** No dispatch path reads `ListsFreshness`. `checkAutoPatch` and `BulkPatchDevices` are
untouched.

**Rationale.** The agent is unprivileged and cannot run `apt-get update`. The mtime of `/var/lib/apt/lists` changes
whenever `apt-get update` renames, adds or removes a list file. Individual list-file mtimes are useless: apt sets them
to the server's `Last-Modified`. Ubuntu's `update-success-stamp` hook (`15update-stamp`) covers no-change refreshes
there. The new helper stamps the directory after every successful refresh (AD-007), which covers no-change refreshes
on every apt host it runs on.

**Alternatives considered.**
- *List-file mtimes.* Rejected: they are server times.
- *`pkgcache.bin`/`srcpkgcache.bin` mtime.* Rejected: root apt commands rebuild them after a dpkg status change. In
  the vermont case, the helper's own `apt-get check` would have rebuilt them after the postinst rewrite, a false
  `fresh`.
- *Content-based check (`apt-get indextargets` against existing files).* Rejected: deviates from FR-029's
  time-based definition and needs an exec per cycle.
- *An `APT::Update::Post-Invoke-Success` hook installed by `install.sh`.* Rejected: a new root-owned config file in
  `/etc/apt`, which only reaches devices on a re-install anyway.

**Consequences.** On an apt host with an **old helper**, a refresh that downloads nothing after an identical-content
config rewrite leaves `stale` until the next refresh that changes any list file. Debian archives publish several times
a day. Residual R-3.

---

### AD-004: Reboot authorization at the API boundary (FR-014–FR-018, NFR-003, NFR-010, BR-002/BR-003/BR-006)

**Decision.**

- **Param** on `os_update`: `reboot_if_required` (boolean; absent = false).
- **Capability fact** on `UpdateInfo`: `supports_reboot_if_required` (`models.LenientBool`). Only JSON `true` decodes
  to true; absent or anything else is false, and decoding never fails. Every agent from this release sets it to true
  in `telemetry.go`, next to `AllowReboot`.
- **Server gate:** the capability fact, not the agent version string. Version strings are not ordered reliably (`dev`
  builds, forks), and the fact names the exact behavior.

New file `internal/server/handlers/os_update_auth.go`:

```go
// applyManualOSUpdateAuth enforces FR-015..FR-018 on a manual os_update's params (mutating them).
// It returns ("", 0, nil) to proceed, or an error code and HTTP status to reject with.
func (h *Handlers) applyManualOSUpdateAuth(ctx context.Context, deviceID string, params map[string]interface{}) (code string, status int, err error)
```

Order inside `SendCommand`, for `Action == "os_update"` only:

1. `delete(params, "include_reboot_class")`. Unchanged, and still unconditional (FR-016).
2. `v, present := params["reboot_if_required"]`
   - absent → proceed (FR-025: byte-identical to today)
   - present and not a JSON boolean (`bool` after decode) → `400 invalid_request`, no command (FR-017)
   - `false` → `delete(params, "reboot_if_required")` and proceed. Stored params equal today's (AC-013).
3. `true`:
   1. `params["mode"]` equal to `"repair"` → `400 invalid_request`. The helper rejects
      `repair + include_reboot_class` (`ValidateRequest`), and repair never upgrades.
   2. `h.telemetry.GetLatestSnapshot(ctx, deviceID)`
      - `pgx.ErrNoRows`, a nil snapshot or a nil `Data.Updates` → `400 reboot_authorization_unsupported`
      - any other error → `500 {"error":"failed to create command"}` and `slog.Error`
   3. `Updates.AllowReboot != nil && !*Updates.AllowReboot` → `400 reboot_not_allowed`
   4. `Updates.AllowReboot == nil || !Updates.SupportsRebootIfRequired` → `400 reboot_authorization_unsupported`
   5. otherwise `params["include_reboot_class"] = true` (FR-015), and keep `reboot_if_required: true`

The checks run before `Dispatch`, so a rejection creates no command and no event. Rejections use
`writeJSON(w, 400, {"error": <sentence>, "code": <code>})`. New constants live in `models` (§6).

**Audit (NFR-010).** The stored command params carry `reboot_if_required: true` and `include_reboot_class: true`.
`CommandSent` appends `" (reboot authorized if required)"` to the event message when `action == os_update` and
`params["reboot_if_required"] == true`.

**Never widened by agent data (NFR-003).** Telemetry can only produce a rejection. The `true` path still requires the
explicit operator param. `checkAutoPatch` and `BulkPatchDevices` never set `reboot_if_required`.

**Rationale.** A rejection that names its reason follows OQ-006. Two distinct codes let the UI and logs tell "turn on
allow_reboot" from "update the agent". The repair-mode rejection prevents a guaranteed agent precheck failure.

**Alternatives considered.**
- *Gate on `devices.agent_version >= vX`.* Rejected (above).
- *Silent downgrade.* Rejected by OQ-006.
- *Reuse `include_reboot_class` from the client.* Rejected by FR-016.

**Consequences.**
- One extra latest-snapshot read, only on ticked dispatches.
- A device whose last telemetry dropped the `updates` section is served by `restoreOmittedSections`.
- An agent downgraded after reporting the capability could receive `include_reboot_class` without understanding
  `reboot_if_required`. It would then behave by PATCH-GATE rules: release holds, and reboot only if reboot-class
  packages applied and `allow_reboot` is true. Residual R-5.

---

### AD-005: Agent post-run reboot decision for authorized runs (FR-019–FR-025, NFR-001, NFR-006, BR-004)

**Decision.** New file `internal/agent/reboot_auth.go`.

**Param parse** (in `executeAction`'s `os_update` case, before `handleOSUpdateWithOutput`):
- `authorized, _ := payload.Params["reboot_if_required"].(bool)`
- present but not a bool → `precheckFailure("reboot_if_required must be a boolean")`

**Path split** (in `executeAction`):

```
r := a.handleOSUpdateWithOutput(ctx, payload)        // sets r.PackageManager ("apt" | "dnf" | "")
if authorized && !r.Detached {
    a.decideAuthorizedReboot(cr.ctx, payload.CommandID, &r)   // phase A: before the result is queued
    r.postResult = func() { a.runAuthorizedReboot(r) }        // phase B
} else if !authorized {
    r.RebootCommandID = payload.CommandID
    r.postResult = func() { a.maybeRebootAfterPatch(r) }      // PATCH-GATE path, unchanged reboot rule
}
```

**Phase A, `decideAuthorizedReboot`.** It fills `r.RebootAuth = &rebootAuthOutcome{Required, Started bool; Reason, Detail string}`
and appends the FR-023 suffix to `r.Message` (AD-006):

1. `Required = a.rebootRequired(ctx, r.PackageManager)`. The seam `Agent.rebootRequiredCheck`, nil →
   `collectors.RebootRequiredNow`, is evaluated for every outcome.
2. The reason is decided in this order:
   1. `r.Status != success` → `run_not_successful` (FR-021). This covers failed, timed out, interrupted, restarted and
      precheck results.
   2. `!Required` → `not_required` (FR-020).
   3. `!a.config.Commands.AllowReboot` → `allow_reboot_off` (FR-022, NFR-001).
   4. `a.claimPatchReboot(id) != claimOK` → `suppressed`, with the detail mapped from the claim result (AD-012).
   5. otherwise `Started = true`.
3. For the dnf inline path, `runOSUpdate` gets `plan.RebootAuthorized`. When it is set, the PATCH-GATE suffix
   (`reboot initiated` / `reboot required but not permitted`) is **not** appended; only
   `(reboot-class applied: …)` stays. The decision text comes from AD-006 alone.

**Phase B, `runAuthorizedReboot`** (runs from `completeCommand` after `out.Add` and `inflight.Remove`; if `out.Add`
failed, `completeCommand` already returns before `postResult` for `os_update`, so there is no reboot):

1. `!Started` → `a.triggerTelemetry()`; return.
2. `TryLockAll({host}, "patch-reboot:"+id, reboot)`. On conflict, or if `maintenanceBusy` → Warn log, one agent event
   (`EventCommandCompleted`, warning, `"authorized reboot for command <id> was not started: host busy"`),
   `triggerTelemetry()`; return.
3. A goroutine holding the reservation:
   1. Wait until `!a.out.Has(id)` (the result was delivered and acked), polling every 500 ms, for at most
      `rebootReportWait` (seam; default 30 s), and never less than `rebootDelay` (existing; default 2 s). It honors
      `commandContext()` cancellation (abort, no reboot).
   2. Re-check `shuttingDown` and `maintenanceBusy`; abort with the same event and telemetry trigger.
   3. `execReboot()`. This is the only call site besides `handleReboot` and `maybeRebootAfterPatch`. It is gated by
      `AllowReboot` in phase A and re-checked here: `if !a.config.Commands.AllowReboot { abort }`. On error: Warn,
      event, `triggerTelemetry()`.
   4. If the wait in step 1 ends without delivery (server unreachable), the reboot proceeds. The result is durable in
      the outbox and is delivered after boot (Warn log `"result not yet delivered; rebooting with result queued"`).
      Accepted by Ryan (flag 5).

**Detached and recovered.**
- A detached authorized run gets no phase A now.
- `inflightMarker` gains `RebootIfRequired bool \`json:"reboot_if_required,omitempty"\``, set at intake for an
  `os_update` whose param is true.
- `recoverMaintenanceCommand` uses it to append the suffix with reason `suppressed`, detail
  `result recovered after agent restart`. `Required` is evaluated now. It never reboots, matching the existing
  recovered rule.

**Unauthorized path.** `maybeRebootAfterPatch` keeps its reboot rule byte-for-byte (FR-025, FR-026). Its first branch
(`len(RebootClassApplied) == 0`) now calls `a.triggerTelemetry()` before returning (FR-046; not a reboot behavior
change). Its claim check becomes `a.claimPatchReboot(id) != claimOK`, the same "no reboot" outcome as today's `false`.

**Rationale.**
- Deciding and claiming **before** the result is queued makes the reported facts true when they are sent (FR-023),
  and the claim makes the decision at-most-once even across redelivery or re-execution after the seen-ID store
  expires (FR-024).
- The host lock is taken in phase B, because the command's own locks are released by `runAction` before
  `completeCommand`.
- Waiting for the outbox ack satisfies "reported before reboot" in the connected case. It is bounded, so a down server
  cannot hold a required reboot forever.

**Alternatives considered.**
- *Reuse `RebootClassApplied` as the trigger.* Rejected: FR-019 mandates the FR-002 evaluation (BR-004).
- *Claim in phase B.* Rejected: the result would already say "started" when the claim might fail.
- *Never reboot unless the result is acked.* Rejected: the result already says "started", and a server outage would
  turn an authorized reboot into a silent no-op.

**Consequences.**
- There is a microsecond window between `runAction` unlocking and phase B locking in which another host command could
  take the host key. Phase B then does not reboot, emits the warning event and triggers telemetry. The result text says
  "started" in that rare race. Residual R-2.
- The intent store is shared with the automated path. Its growth is made self-healing by AD-012.

---

### AD-006: Result facts carried in the result message (FR-023, NFR-008, NFR-010)

**Decision.** There is no wire-format or schema change: `models.CommandResult`, the `commands` table and the result
size limits are unchanged. Authorized runs append exactly one suffix to `Message`, separated by `" | "`:

```
Reboot authorized; reboot required after run: <yes|no>; reboot started: yes
Reboot authorized; reboot required after run: <yes|no>; reboot not started: <reason>
  <reason> ∈ { "not required", "allow_reboot off", "run not successful", "suppressed (<detail>)" }
  <detail> ∈ { "reboot already claimed for this command", "reboot intent could not be recorded",
               "reboot intent store full", "result recovered after agent restart" }
```

`completeCommand` truncates with `TruncateTail`, so the suffix at the end survives any truncation. Unauthorized runs get
no suffix.

**FRD delta (accepted by Ryan, flag 2).** FR-023 names three reasons. `suppressed` is added for the at-most-once and
recovery safety cases, which FR-023 does not enumerate. It never means "rebooted".

**Rationale.** A new structured result field would need a server column, which means a migration (NFR-008 forbids
that). The message is already what the command record shows.

**Alternatives considered.** *A new `CommandResult.Reboot` object.* Rejected because it needs a migration.

---

### AD-007: Helper post-run refresh and lists stamp (FR-040–FR-045, NFR-005)

**Decision.** `maintenance.Worker` gains two optional seams. Nil disables each feature, so the existing tests are
unchanged:

```go
Sources    func() (string, error) // returns AptSourcesFingerprint of the apt-effective files; error = treat as changed
TouchLists func() error           // sets /var/lib/apt/lists mtime to now; nil = no stamp
```

`cmd/riot-maintenance/main_linux.go` wires the production values:
- `Sources`: `AptSourceEntries("/", 1000)`, then the fingerprint.
- `TouchLists`: `os.Lstat("/var/lib/apt/lists")`, which must be a real directory (not a symlink); then
  `os.Chtimes(path, now, now)`.

New constant `PostRefreshBudget = 5 * time.Minute`.

Inside `Execute`, for modes `full` and `security` only. `repair` returns before step 1 (FR-043).

1. Immediately before the `refresh` step: `before, beforeErr := w.Sources()` (if `Sources` is set).
2. `refresh` (unchanged). On success: `w.touchLists("refresh")`. A touch error appends
   `"\n[lists_stamp]\ncould not update /var/lib/apt/lists timestamp: <err>\n"` through `appendBounded` and is otherwise
   ignored.
3. `upgrade`, then `verify` (unchanged). Any failure returns as today. **No** post-run refresh after a failed upgrade or
   verify (FR-044: the outcome is decided by upgrade and verify).
4. `changed := beforeErr != nil || afterErr != nil || before != after`, with `after, afterErr := w.Sources()`.
5. If `changed`, run **at most one** post-run refresh (FR-041, FR-042):
   - If the remaining budget (`deadline(ctx) - now`) is less than
     `PostRefreshBudget + TerminationGrace + 30s` (7 min) → skip, and note `post-run package list refresh skipped:
     worker time budget nearly exhausted`.
   - Otherwise:
     1. Set `rec.Phase = "post_refresh"` and `Store.Save`. A save error → note `post-run package list refresh
        skipped: state not persisted`.
     2. `sub, cancel := context.WithTimeout(ctx, PostRefreshBudget)`.
     3. `out, err := w.Runner.Run(sub, "/usr/bin/apt-get", "-o", "Acquire::Retries=0", "update")`.
     4. Append `"\n[post_refresh]\n"+out.Output` through `appendBounded` (FR-045).
     5. **Do not** touch `rec.Interrupted` or `rec.ExitCode`.
     6. `err == nil` → `w.touchLists("post_refresh")`. Otherwise, note
        `post-run package list refresh failed: <boundedText(err, 200)>`.
6. `return rec, nil`. The deferred finisher sets the success summary to
   `"Package state verified; maintenance completed"`, plus `"; " + note` when a note was set (FR-044, AC-034).
   `Execute` keeps the note in a local variable that the defer reads. The summary stays at most 4,096 bytes.

The threshold guarantees the parent `ctx` cannot expire during the extra refresh, including the 90 s
SIGTERM→SIGKILL grace. So a successful run can never become `interrupted` because of the extra refresh, and
`Record.Validate` (which rejects `success && Interrupted`) is never violated. The argv is already in `CommandAllowed`,
and `install.sh` and sudoers are unchanged.

**Rationale.**
- A fingerprint (path, mtime-ns, size) catches a postinst rewrite even when the content is identical. One extra
  refresh is harmless, so errors fail toward refreshing.
- The stamp makes the agent's mtime rule (AD-003) correct after no-change refreshes.
- Running the extra refresh through `Runner.Run` rather than `step()` keeps runner interruption flags off the record.

**Alternatives considered.**
- *Post-refresh unconditionally.* Rejected by FR-043.
- *A new `Record.PostRefresh` field.* Rejected: `parseRecord` uses `DisallowUnknownFields`, so an older reader would
  reject new records (AD-008). The summary text carries the note.
- *A stamp file in `StateDir`.* Rejected: it couples to the strictly validated root store layout. The lists-directory
  mtime is not used by apt.

**Consequences.** Hosts get the post-run refresh, and freshness after no-change refreshes, only after `install.sh` is
re-run (D-005). See AD-008.

---

### AD-008: Compatibility matrix: no helper protocol change; exact old-helper behavior

**Decision.** `maintenance.ProtocolVersion` stays `1`. `Request` (4 fields) and `Record` are unchanged. The agent
writes the same request (`include_reboot_class` is already a field). The post-run refresh and the stamp are internal to
the new helper. No negotiation is needed because nothing new crosses the boundary.

| Agent \ Helper | Old helper (install.sh not re-run) | New helper |
|---|---|---|
| **New agent** | Everything works **except** AC-032, AC-033, AC-034 and the "refreshes the lists once more" clause of AC-038. A config rewrite during the run leaves lists `stale` until the next refresh that changes a list file. Freshness after a no-change refresh relies on the lists-dir mtime or the Ubuntu stamp only. Reboot authorization, holds release (A-002), the post-run evaluation, intent pruning and telemetry all work. | All ACs |
| **Old agent** | Today's behavior. The server rejects ticked dispatches (no capability); the UI disables the checkbox. | Same as left. The helper's extra refresh and stamp are harmless; the old agent ignores freshness. |

**dnf** has no helper. All dnf behavior ships with the agent. FR-040 to FR-045 do not apply to dnf (no stale state,
FR-038).

---

### AD-009: Fresh telemetry after a run and at startup (FR-046–FR-048, NFR-007)

**Decision.**
- **After a run:** every terminal `os_update` path ends with exactly one `a.triggerTelemetry()`, unless phase B is
  starting a reboot:
  - `maybeRebootAfterPatch` (all non-reboot branches; the empty-applied branch is new)
  - `runAuthorizedReboot` (all non-reboot branches)
  - `recoverMaintenanceCommand` (after `completeCommand`, when `!res.Detached`)
- **At startup:** `telemetryLoop` already sends immediately on start (`agent.go:399`). No change; this is locked by a
  test.
- **Coalescing:** `telemetryNow` has capacity 1 and `triggerTelemetry` is non-blocking, so N triggers while one is
  pending produce one collection. `telemetryLoop` is a single goroutine, so collections never overlap (FR-048).

**Consequences.** FR-047's 60 s bound holds from the moment `telemetryLoop` starts. `Run` starts it after
`recoverInterruptedCommands`, which is bounded by `StartupRecoveryBudget` (2 min) and only slow when an interrupted
Docker update must be reconciled. Moving the loop earlier would let hold reconciliation inside `Collect` race startup
recovery (PATCH-GATE AD-007 ordering). Accepted by Ryan (flag 6).

---

### AD-010: Device page and fleet panel (FR-008–FR-013, FR-027, FR-028, FR-034, FR-035, OQ-008, NFR-011)

**Decision.**

New pure module `web/src/utils/patchFlow.ts`. It has no React and no fetches; all inputs come from data the page
already loads (NFR-011).

```ts
export type ListsFreshness = 'fresh' | 'stale' | 'unknown'
export function normalizeFreshness(v: unknown): ListsFreshness            // anything else → 'unknown'
export type PatchPrimary = 'none' | 'patch' | 'reboot'
export interface DevicePatchState {
  freshness: ListsFreshness; actionablePending: number; rebootRequired: boolean
  primary: PatchPrimary; patchLabel: string | null; showPatch: boolean
  rebootDisabledReason: string | null
}
export function devicePatchState(u: UpdateInfo | undefined): DevicePatchState  // FRD §7 table S1..S6
export interface RebootOption { enabled: boolean; disabledReason: string | null }
export function rebootOption(u: UpdateInfo | undefined): RebootOption           // FR-010 / FR-011
export function patchDialogStatements(u: UpdateInfo | undefined, ticked: boolean): string[] // FR-012, FR-013, stale note
```

**`devicePatchState`:**

| State | Condition | Result |
|---|---|---|
| S6 | `freshness === 'stale'` | `showPatch = true`, `patchLabel = "Package lists out of date — refresh pending"`, `primary = 'patch'` |
| S4 | not stale, `pending_updates === 0`, `reboot_required` | `primary = 'reboot'`, `showPatch = false` |
| S2, S3, S5 | not stale, `pending > 0` | `primary = 'patch'`, `patchLabel = "Patch (N)"` |
| S1 | otherwise | `primary = 'none'` |

`rebootDisabledReason` (S4 with `allow_reboot === false`) is `"Reboots are turned off on this agent (commands.allow_reboot: false). Reboot this host outside rIOt."`

**`rebootOption`:**
- `allow_reboot === false` → disabled, `"Reboots are turned off on this agent (commands.allow_reboot: false)."`
- else `allow_reboot === undefined || supports_reboot_if_required !== true` → disabled,
  `"The agent must be updated to use this option."`
- else enabled.

**`patchDialogStatements`** (raw `pending_reboot_class_count > 0` is `rc`; holds are active when
`hold_enforcement === 'active' && held_packages.length > 0`):

| Condition | Statement |
|---|---|
| `reboot_required` | `"A reboot is already required on this host. With the box ticked, the host reboots after the run."` |
| `rc && ticked` | (b) `"Kernel and GPU driver updates will be installed. The host reboots after the run if a reboot is required."` |
| `rc && !ticked && holdsActive` | (a) `"Kernel and GPU driver updates are held by rIOt and will not be installed by this run."` |
| `rc && !ticked && !holdsActive` | (c) `"Kernel and GPU driver updates will be installed. A reboot will be needed afterwards, and rIOt will not reboot the host."` |
| stale | `"Package lists are out of date. Running Patch refreshes them first, so the packages installed may differ from the pending list."` |

The order is: reboot required, then the rc statement, then stale. `rc === false` shows no (a), (b) or (c).

**New component `web/src/components/PatchConfirmOptions.tsx`:**
- Props `{ updates, checked, onChange }`.
- Renders a native `<input type="checkbox">` with `<label>` text **exactly**
  `Reboot automatically if the patch requires it`, `disabled` per `rebootOption`.
- Shows the disabled reason in visible text linked by `aria-describedby`.
- Lists the statements.
- It is passed as `children` of the existing `ConfirmDialog` only when `confirmAction === 'os_update'`.

**`DeviceDetail.tsx`:**
- State `rebootIfRequired` (default `false`). It is set to `false` in the Patch button's `onClick` before opening, and
  again in `onClose` (FR-009). There is no persistence.
- `onConfirm` for `os_update` sends `{ mode: 'full', reboot_if_required: true }` only when the box is checked **and**
  `rebootOption(...).enabled`; otherwise `{ mode: 'full' }`.
- The header Patch button follows `devicePatchState`. A stale label gets
  `title="Running Patch refreshes the package lists first."`.
- In S4 the header shows a primary `Reboot now` button opening the existing `setConfirmAction('reboot')`
  confirmation. When `rebootDisabledReason` is set, the button is `disabled` and the reason is rendered as visible
  text next to it (FR-028). The Power menu is unchanged.
- A 400 with code `reboot_not_allowed` or `reboot_authorization_unsupported` is shown inline by `ConfirmDialog` from
  the server's `error` text (no special casing).

**`FleetOverview.tsx`:**
- `DevicePatchInfo.lists_freshness` comes from the server (AD-011).
- In `PatchReviewModal`, a stale row replaces `"{n} pkgs"` with `Package lists out of date — refresh pending`; the
  security and reboot-class counts are hidden for that row.
- `totalPkgs` and `totalSecurity` exclude stale rows. When `k > 0` stale rows exist, the description appends
  `" · {k} device{s} with out-of-date package lists not counted"` (OQ-008).
- The device-table "updates" cell (line 322) shows `Lists out of date`, with the full label as `title`, instead of the
  count for a stale row (BR-005; the same misinformation in another place).
- `pendingPatchCount` (a device count, not a package total) keeps counting stale devices. A Patch run is what
  refreshes them, so they stay reachable from "Review Patches".

**Rationale.** A pure helper makes the S1–S6 table and FR-012 mechanically testable. The native checkbox in
`children` reuses the dialog's focus and pending handling (SEC-001 initial focus stays on Cancel).

---

### AD-011: Patch-status response carries normalized freshness

**Decision.** In `PatchStatus`, `devicePatchInfo` gains `ListsFreshness string \`json:"lists_freshness"\``, always set:
`updates.Freshness()` when updates exist, else `"unknown"`. The `all=false` row filter is unchanged, so stale devices
with a raw count still appear and remain patchable.

---

### AD-012: Self-pruning reboot-intent store (bounded growth and self-heal; NFR-006, BR-008)

**Problem.** `claimPatchReboot` stores `map[commandID]bool` in `patch-reboot-intents.json` and returns false once the
map holds 256 entries. Nothing ever removes an entry. After 256 patch reboots on a device, every automatic and
authorized post-patch reboot is suppressed forever. This story claims intents more often, and the project rule
requires every bounded store to self-heal.

**Decision.** Move the store out of `maintenance.go` into a new file, `internal/agent/reboot_intents.go`:

```go
const (
    rebootIntentMaxAge  = 30 * 24 * time.Hour // age prune
    rebootIntentMinKeep = 72 * time.Hour      // never evicted younger than this, not even by the cap
    rebootIntentCap     = 256                 // hard backstop
    rebootIntentSkew    = 5 * time.Minute
)
type rebootClaim int // claimOK | claimAlreadyClaimed | claimPersistFailed | claimStoreFull
func (a *Agent) claimPatchReboot(id string) rebootClaim // was bool; both callers updated
func pruneRebootIntents(m map[string]time.Time, now time.Time) (out map[string]time.Time, removed int) // pure
func evictForCap(m map[string]time.Time, now time.Time) (out map[string]time.Time, ok bool)          // pure
```

The clock comes from `a.clock()` (existing seam). Writes go through the existing `atomicJSONFile` and `stateFS`. The
path resolution is unchanged.

**File format, v2.** `{"version":2,"intents":{"<uuid>":"<RFC3339 UTC claim time>"}}`.

**Legacy load.** A legacy v1 file is the bare `{"<uuid>":true,...}` object.
- Every legacy entry gets claim time = `now` at first load, and the file is rewritten as v2 on the next save.
- Using `now` is conservative: a legacy intent is kept for a further 30 days and is never pruned early.
- Any entry whose key is not a canonical UUID, or whose value is neither `true` (v1) nor a valid RFC3339 string (v2),
  is dropped. Such an entry can never match a real claim, because `claimPatchReboot` rejects non-canonical IDs.

**Claim algorithm**, under `rebootIntentMu`, on every call:
1. Non-canonical `id` → `claimPersistFailed` (unchanged rejection).
2. Load the file. A missing file means an empty store. A read or parse error → `claimPersistFailed` with no reboot
   (today's fail-closed behavior; see residual R-6).
3. **Prune.** Drop entries with claim time earlier than `now - rebootIntentMaxAge`.
   - Clock guard: if any entry's claim time is more than `rebootIntentSkew` after `now`, the clock has moved
     backwards. That call prunes and evicts nothing, and the entries are kept.
4. `id` present → `claimAlreadyClaimed` (at-most-once). The pruned map is still saved when step 3 removed entries.
5. **Cap backstop.** If the store holds `rebootIntentCap` or more entries, evict oldest-first (ties broken by ID),
   but only entries with claim time earlier than `now - rebootIntentMinKeep`, until there is room.
   - If room cannot be made → `claimStoreFull`, no reboot (fail-closed).
   - `claimStoreFull` needs more than 256 patch reboots within 72 hours on one host.
6. Add `id → now` and save.
   - Save failure → `claimPersistFailed` (as today).
   - Success → `claimOK`.

**Why 30 days and 72 hours.** A command ID can reach the agent's reboot decision again only in these cases:
- **Server redelivery while the command is non-terminal.** The bound is `QueuedTTL(os_update)` 30 min plus
  `ExecDeadline` 40 min ≈ 70 min. After that the server marks the command expired or timed out and never sends it
  again.
- **Agent-side replay.** The seen-ID dedupe store keeps an ID for up to `SeenMaxAge` = 48 h. The outbox keeps an
  undelivered result for up to `OutboxMaxAge` = 24 h. The `inflight` marker is removed before `postResult`, and
  recovered results never reboot.

The longest of these windows is 48 h.
- `rebootIntentMinKeep` = 72 h is 48 h plus a 24 h margin, so **even the cap** can never evict an intent whose command
  could still be redelivered or replayed.
- `rebootIntentMaxAge` = 30 days is 15 times the longest window. It also leaves room for agent downtime, and for
  forward clock corrections on hosts without an RTC (fake-hwclock restores the last saved time, so a forward jump is
  about the real elapsed time). The store stays tiny: realistic use is a few entries per month per host.

So at-most-once holds for every command that can still arrive, and every entry outside that window self-heals with no
operator action.

**Phase A mapping (AD-005 step 2.4).**

| Claim result | Phase A reason |
|---|---|
| `claimAlreadyClaimed` | `suppressed (reboot already claimed for this command)` |
| `claimPersistFailed` | `suppressed (reboot intent could not be recorded)` |
| `claimStoreFull` | `suppressed (reboot intent store full)` |

`maybeRebootAfterPatch` treats anything other than `claimOK` as today's `false`. Its reboot rule is otherwise
unchanged (FR-026).

**Logging.**
- At most one `slog.Info("reboot intents pruned", "removed", n, "remaining", m)` per claim, and only when entries were
  removed or evicted.
- `claimStoreFull` and a parse error each log one Warn per claim attempt. Claims happen at most once per command.

**Alternatives considered.**
- *Count-only eviction (keep the newest 256).* Rejected: with no age floor, a burst could evict an intent that is still
  inside the redelivery window.
- *Prune on a timer.* Rejected: a second cadence. Pruning on claim is enough, because entries are only added there.
- *Reset the file on a parse error.* Rejected: that drops at-most-once records and could cause a second reboot. Kept
  fail-closed (residual R-6).
- *A shorter age such as 7 days.* Rejected: it buys nothing, given realistic sizes of a few entries, and narrows the
  margin over clock faults.

**Consequences.** The agent rewrites the file in v2 format on its first claim after the upgrade. An agent downgraded
afterwards would fail to parse v2 and fail closed (no post-patch reboots) until the file is removed. Downgrade is not a
supported path, and the failure direction is safe.

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `internal/models/telemetry.go` | `UpdateInfo` gains `ListsFreshness ListsFreshness \`json:"lists_freshness,omitempty"\`` and `SupportsRebootIfRequired LenientBool \`json:"supports_reboot_if_required,omitempty"\``. New types `ListsFreshness` (lenient `UnmarshalJSON`) and `LenientBool` (lenient `UnmarshalJSON`), constants `ListsFresh`/`ListsStale`/`ListsUnknown`, method `(*UpdateInfo) Freshness() string` |
| CREATE | `internal/models/telemetry_patchflow_test.go` | Lenient decode tests (AC-029, AC-016 capability parsing) |
| MODIFY | `internal/models/command_policy.go` | Constants `ParamRebootIfRequired = "reboot_if_required"`, `ParamIncludeRebootClass = "include_reboot_class"`; API codes `CodeRebootNotAllowed = "reboot_not_allowed"`, `CodeRebootAuthUnsupported = "reboot_authorization_unsupported"` |
| CREATE | `internal/server/handlers/os_update_auth.go` | `applyManualOSUpdateAuth` (AD-004) |
| MODIFY | `internal/server/handlers/commands.go` | `SendCommand` calls `applyManualOSUpdateAuth` for `os_update` before `Dispatch`; writes rejections; comment updated (PATCH-GATE FR-023 amended by BR-003) |
| MODIFY | `internal/server/handlers/commands_test.go` | AC-012 to AC-016, AC-037 (server half); the existing `[AC-013]` strip test stays green |
| MODIFY | `internal/server/handlers/patch_badge.go` | AD-001 rewrite of NY-10, the Reboot line, kind and stale note; doc comment records the PATCH-FLOW-001 amendment of PATCH-BADGE-001 NY-10 |
| MODIFY | `internal/server/handlers/patch_badge_test.go` | New PF tests (§8). Update the PATCH-BADGE-001 tests that encode the old rule: `DecisionTable_RebootDateInWindowMode` (new Reboot line string), `ManualRebootNeeded_NoRebootClassUpdatePending` (new cause text), and `ManualRebootNeeded_AllowRebootOff…`, `…PolicyNotGated…`, `…AgentHoldRebootClassOff…`, `…HoldRebootClassNotReported…`, `…AutoPatchOffCitesSetup` (set `RebootRequired = true`; they test causes, not the trigger) |
| MODIFY | `internal/server/handlers/fleet.go` | `devicePatchInfo.ListsFreshness` (AD-011) |
| MODIFY | `internal/server/handlers/fleet_patch_badge_test.go` | AC-027 (server half), AC-029 (row shows `unknown`) |
| MODIFY | `internal/server/handlers/auto_update_test.go` | AC-022, AC-031 regression tests (no production change to `auto_update.go`) |
| CREATE | `internal/server/handlers/telemetry_patchflow_test.go` | AC-029 ingest: absent and `"bogus"` freshness, non-bool capability → 2xx, no Warn records (capture handler) |
| MODIFY | `internal/server/events/generator.go` | `CommandSent` appends `" (reboot authorized if required)"` for an authorized `os_update` |
| MODIFY | `internal/server/events/generator_test.go` | AC-037 (event half), AC-020 event regression |
| CREATE | `internal/agent/collectors/kernelreboot.go` | `RebootRequiredNow`, `kernelNewerThanRunning`, `compareDebVersion` (AD-002) |
| CREATE | `internal/agent/collectors/kernelreboot_test.go` | AC-005, comparator table, flavor scoping, bounds |
| CREATE | `internal/agent/collectors/listsfresh.go` | `listsFreshness` (AD-003) |
| CREATE | `internal/agent/collectors/listsfresh_test.go` | AC-025, AC-026 |
| MODIFY | `internal/agent/collectors/updates.go` | `collectAPT` sets `ListsFreshness`; `detectRebootRequiredAPT` ORs the kernel source and adds the reason; seams `aptRoot`, `kernelRoot`, `listsNow` |
| MODIFY | `internal/agent/collectors/updates_test.go` | AC-005 (collector wiring), AC-030 (dnf omits freshness) |
| MODIFY | `internal/agent/telemetry.go` | Stamp `SupportsRebootIfRequired = true` next to `AllowReboot` |
| MODIFY | `internal/agent/telemetry_allow_reboot_test.go` | AC-009/AC-016 agent half (capability reported) |
| CREATE | `internal/agent/reboot_auth.go` | `rebootAuthOutcome`, `decideAuthorizedReboot`, `runAuthorizedReboot`, `rebootAuthSuffix`, `rebootRequired` seam wrapper (AD-005, AD-006) |
| CREATE | `internal/agent/reboot_auth_test.go` | AC-017 to AC-021, AC-035 (authorized half), AC-038 (agent half), the store-full suffix |
| CREATE | `internal/agent/reboot_intents.go` | AD-012 store: `claimPatchReboot` (moved here, returns `rebootClaim`), v2 format, legacy v1 load, age prune, 72 h floor, oldest-first cap eviction, clock-backwards guard |
| CREATE | `internal/agent/reboot_intents_test.go` | AD-012 bound tests (§8, the NFR-006/BR-008 row) |
| MODIFY | `internal/agent/commands.go` | `commandExecResult` gains `PackageManager string`, `RebootAuth *rebootAuthOutcome`; the `os_update` case parses `reboot_if_required` and splits paths; `handleOSUpdateWithOutput` sets `PackageManager`; `osUpdatePlan.RebootAuthorized` suppresses the PATCH-GATE suffix; `maybeRebootAfterPatch` triggers telemetry on the empty-applied branch and checks `claimPatchReboot(...) != claimOK` |
| MODIFY | `internal/agent/commands_test.go` | AC-013 (agent half: unauthorized never reboots), AC-022 (agent half: automated rule unchanged), AC-035 (unauthorized half); adapt existing reboot-intent assertions to `rebootClaim` |
| MODIFY | `internal/agent/maintenance.go` | Remove `claimPatchReboot` (moved to `reboot_intents.go`); `recoverMaintenanceCommand` appends the recovered suffix for authorized markers and calls `triggerTelemetry` |
| MODIFY | `internal/agent/maintenance_test.go` | AC-021 (recovered result never reboots, suffix present); move or adapt existing `claimPatchReboot` tests to `reboot_intents_test.go` |
| MODIFY | `internal/agent/cmdstate.go` | `inflightMarker.RebootIfRequired bool` (`omitempty`) |
| MODIFY | `internal/agent/command_lifecycle.go` | Set `RebootIfRequired` on the marker at intake for an authorized `os_update` |
| MODIFY | `internal/agent/agent.go` | Seams `rebootRequiredCheck func(context.Context, string) bool`, `rebootReportWait time.Duration` |
| CREATE | `internal/agent/telemetry_startup_test.go` | AC-036: the first `sendTelemetry` happens with no tick |
| CREATE | `internal/maintenance/aptsources.go` | `AptSourceEntry`, `AptSourceEntries`, `AptSourcesFingerprint`, `ErrAptBudget` (shared by helper and agent) |
| CREATE | `internal/maintenance/aptsources_test.go` | apt-effective filename rules, budget, symlinks, missing dirs |
| MODIFY | `internal/maintenance/worker.go` | `Worker.Sources`, `Worker.TouchLists`, `PostRefreshBudget`, post-run refresh, stamp, summary note (AD-007) |
| MODIFY | `internal/maintenance/worker_test.go` | AC-032, AC-033, AC-034, budget skip, `Interrupted` isolation |
| MODIFY | `cmd/riot-maintenance/main_linux.go` | Wire `Sources` and `TouchLists` |
| MODIFY | `cmd/riot-maintenance/main_linux_test.go` | `TouchLists` refuses a symlinked lists path (temp dir) |
| MODIFY | `web/src/types/models.ts` | `UpdateInfo.lists_freshness?: 'fresh' \| 'stale' \| 'unknown'`, `supports_reboot_if_required?: boolean`, and the missing `allow_patching?`, `hold_reboot_class?` already sent by the server |
| MODIFY | `web/src/api/client.ts` | `DevicePatchInfo.lists_freshness?: 'fresh' \| 'stale' \| 'unknown'` |
| CREATE | `web/src/utils/patchFlow.ts` | Pure decision helpers (AD-010) |
| CREATE | `web/src/utils/patchFlow.test.ts` | S1–S6, FR-010/011, FR-012/013 tables |
| CREATE | `web/src/components/PatchConfirmOptions.tsx` | Checkbox, disabled reason and statements |
| CREATE | `web/src/components/PatchConfirmOptions.test.tsx` | AC-007 to AC-011 at component level |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | Patch label and visibility, Reboot primary (S4), dialog children, params, reset on open and close |
| CREATE | `web/src/pages/DeviceDetail.patchflow.test.tsx` | AC-006 (device), AC-007, AC-012, AC-013 (client params), AC-023, AC-024, AC-027 (device), AC-029 (device) |
| MODIFY | `web/src/pages/FleetOverview.tsx` | Stale row label, aggregates exclude stale, table cell |
| CREATE | `web/src/pages/FleetOverview.patchflow.test.tsx` | AC-006 (fleet), AC-027 (panel), OQ-008 totals |

**Explicitly unchanged:**
- `internal/maintenance/protocol.go` (no protocol change)
- `internal/server/handlers/auto_update.go`
- `internal/agent/doctor.go` (no collector added or renamed; MEMORY rule satisfied)
- `scripts/install.sh` and sudoers
- `web/src/components/PatchBadge.tsx` (it renders server text)
- all migrations

README, CHANGELOG and the ops guide belong to the technical writer (§12, note 9).

## 5. Data Model Changes

**Postgres:** none. No migration and no new table, column or index (NFR-008). Telemetry rides the existing JSONB blob,
and command params ride `commands.params` JSONB.

**`models.UpdateInfo` (agent → server telemetry), additive:**

```diff
 type UpdateInfo struct {
   ...
   RebootRequired        bool     `json:"reboot_required,omitempty"`   // meaning widened: marker OR newer installed kernel (apt); dnf unchanged
   ...
   ReleaseUpgrade *ReleaseUpgrade `json:"release_upgrade,omitempty"`
+  // ListsFreshness is apt package-list freshness: "fresh" | "stale" | "unknown".
+  // Absent (older agent, dnf, other) reads as "unknown". Display-only (FR-039).
+  ListsFreshness ListsFreshness `json:"lists_freshness,omitempty"`
+  // SupportsRebootIfRequired: the agent understands os_update's reboot_if_required.
+  // Only JSON true counts; absent or any other value means not supported.
+  SupportsRebootIfRequired LenientBool `json:"supports_reboot_if_required,omitempty"`
 }
```

**`os_update` params:**

| Param | Type | Set by | Notes |
|---|---|---|---|
| `mode` | string | client, auto, bulk | unchanged |
| `reboot_if_required` | boolean | operator (manual only) | absent = false; `false` is deleted by the server before storage |
| `include_reboot_class` | boolean | server only | manual: derived from a validated `reboot_if_required` (AD-004); auto: unchanged `gated` rule |

**Agent `inflightMarker` (`/var/lib/riot` in-flight file):** `+ RebootIfRequired bool \`json:"reboot_if_required,omitempty"\``.

**Agent `patch-reboot-intents.json` (AD-012):**

```diff
- {"<uuid>": true, ...}                                               // v1: no age, never pruned, stops at 256
+ {"version": 2, "intents": {"<uuid>": "2026-10-23T22:51:04Z", ...}}  // v2: claim time per intent; 30-day prune; 256 cap evicts oldest (>72 h)
```

A v1 file is read once, with every entry's claim time set to load time, and rewritten as v2 on the next save.

**Command result:** `Message` suffix grammar (AD-006). No struct change.

**Helper:** `Request` and `Record` unchanged. New on-disk side effect: the mtime of `/var/lib/apt/lists`
(directory metadata only).

**Bounded growth (every new failure path):**

| Path | Bound |
|---|---|
| Freshness check | ≤ 1,000 entries and ≤ 2 s per collection; 0 files; ≤ 1 debug line per collection |
| Kernel check | ≤ 256 `/boot` entries and one 256-byte read per collection; 0 files; no log |
| Reboot reasons | +1 entry of ≤ 160 bytes |
| Post-run refresh | ≤ 1 per run; ≤ 5 min; skipped below 7 min of remaining budget; output within the existing 256 KiB bound; summary ≤ 4,096 bytes |
| Lists stamp | ≤ 2 `Chtimes` calls per run; 0 files |
| Authorized reboots | ≤ 1 per command; recovered results never reboot |
| Reboot-intent store | Entries older than 30 days are pruned on every claim. Hard cap of 256 entries (≈ 20 KiB), evicting oldest-first but only entries older than 72 h; otherwise the claim fails closed. ≤ 1 Info line per pruning claim. Self-heals with no operator action |
| Phase B abort | ≤ 1 agent event per command; ≤ 1 telemetry trigger |
| Telemetry triggers | ≤ 1 extra collection per run and per agent start (1-slot coalescing channel, single loop) |
| Server | +1 latest-snapshot read per ticked dispatch only; 0 new records; event message +36 bytes |

## 6. API / Interface Contract

### POST `/api/v1/devices/{id}/commands` (existing route; `os_update` behavior extended)

Request headers and auth are unchanged (dashboard session). Body limit: 128 KiB (unchanged).

```json
{ "action": "os_update", "params": { "mode": "full", "reboot_if_required": true }, "confirm_host": "" }
```

| Field | Type | Rules |
|---|---|---|
| `params.mode` | string, optional | unchanged; `"repair"` together with `reboot_if_required: true` → 400 `invalid_request` |
| `params.reboot_if_required` | boolean, optional | not a JSON boolean → 400 `invalid_request`; `false` is stored as absent |
| `params.include_reboot_class` | any | always removed; the server sets it only via AD-004 |

**Responses:**

| Status | Body | When |
|---|---|---|
| 201 | the created `Command`; `params` contains `reboot_if_required: true` and `include_reboot_class: true` for an authorized dispatch | success |
| 400 | `{"error":"invalid request","code":"invalid_request"}` | non-boolean authorization; repair + authorization |
| 400 | `{"error":"reboots are turned off on this agent (commands.allow_reboot: false)","code":"reboot_not_allowed"}` | latest telemetry `allow_reboot == false` |
| 400 | `{"error":"the agent must be updated to authorize a reboot with a patch","code":"reboot_authorization_unsupported"}` | no telemetry or no `updates`; `allow_reboot` absent; capability absent or not true |
| 409 / 429 / 404 / 500 | unchanged | unchanged |

### GET `/api/v1/fleet/patch-status` (additive)

Each row gains `"lists_freshness": "fresh" | "stale" | "unknown"`, always present.

### Agent telemetry (POST, existing)

`updates.lists_freshness` and `updates.supports_reboot_if_required` are optional. Any value is accepted with 2xx:
unknown strings normalize to `unknown`, and a non-`true` capability counts as not supported.

### Agent command result (existing)

`message` may end with the AD-006 suffix. No other change.

### Helper (root, internal)

Unchanged request file. A new `phase` value `post_refresh` may appear in records. The terminal summary may end with
`; post-run package list refresh failed: …` or `; post-run package list refresh skipped: …`.

## 7. Sequence / Flow

### 7.1 Authorized manual run (apt, new agent, new helper): the vermont case

1. The operator opens Patch. `devicePatchState` gives S3; `rebootOption` is enabled; statement (a) shows (holds
   active). The operator ticks the box: statement (b). Confirm.
2. The UI sends POST `{action:"os_update", params:{mode:"full", reboot_if_required:true}}`.
3. `SendCommand`: strip `include_reboot_class` → validate the bool → mode is not repair → `GetLatestSnapshot`.
   `allow_reboot = true` and the capability is true, so set `include_reboot_class = true` → `Dispatch` → `CommandSent`
   ("… (reboot authorized if required)") → 201.
4. Agent intake: the marker gets `reboot_if_required: true`. `executeAction` parses `authorized = true`.
   `handleOSUpdateWithOutput` → `runDetachedMaintenance(include = holdMgr.Enabled)` → helper request (4 fields, as
   today).
5. Helper:
   1. snapshot, release holds
   2. configure, verify
   3. `Sources()` = F1
   4. refresh → touch lists
   5. upgrade (a postinst rewrites `omvextras.sources`)
   6. verify
   7. `Sources()` = F2 ≠ F1 → `post_refresh` → touch lists
   8. return, then the deferred hold restore, `Changes`, and a success record
6. The agent observes the terminal record → success result. Phase A: `RebootRequiredNow("apt")` (kernel 7.2.9 is
   installed and 6.12.x is running) → required. `allow_reboot` is true. `claimPatchReboot(id)` prunes intents older
   than 30 days, then records `id → now` and returns `claimOK`; Started. The message gets
   `" | Reboot authorized; reboot required after run: yes; reboot started: yes"`.
7. `completeCommand`: `out.Add` (durable) → `inflight.Remove` → phase B: host reservation → wait for the outbox ack
   (≤ 30 s) → `execReboot`.
8. After boot: `telemetryLoop` sends at once. `reboot_required = false` (the running kernel is the newest; `/run` was
   cleared), pending = 0, and `lists_freshness = fresh` (lists dir mtime is later than the config mtime).

### 7.2 Authorized run, failures

- **Validation fails** → 400 with the code; no command.
- **The run fails, times out or is interrupted** → phase A gives `run_not_successful`. No claim, no reboot, holds
  restored by the helper defer (NFR-002), one telemetry trigger.
- **Success, not required** → `not_required`, telemetry trigger.
- **Success, required, `allow_reboot` false at the agent** → `allow_reboot_off`, telemetry trigger. The server sees
  `reboot_required = true` and fires the PATCH-GATE `reboot_required` event.
- **Redelivered or re-executed command** (within 30 days, and always within the 72 h floor) → `claimAlreadyClaimed` →
  `suppressed (reboot already claimed…)`.

### 7.3 Freshness collection (every telemetry cycle, apt)

`collectAPT` → `apt list --upgradable` (raw count) → `listsFreshness(aptRoot, now, now+2s)` → set the field. The server
ingests (lenient) and serves the raw count plus normalized freshness. The UI shows the stale label, and the badge drops
pending-derived wording and adds the stale note.

### 7.4 Old helper

Steps 5.3 and 5.7 do not happen. If the postinst changed the config, the next collection reports `stale`, and the UI
shows "Package lists out of date — refresh pending". The next Patch run refreshes first. Everything else matches 7.1.

## 8. Acceptance Criteria Mapping

**Naming.** Go test names include `PF_ACnnn` (or `PF_Bound` for the NFR row). Vitest `describe` blocks start with
`[PATCH-FLOW-001 AC-nnn]`. Tests are co-located, following the existing convention.

| AC | Fulfilled By | Tests |
|---|---|---|
| AC-001 | AD-001 NY-10 trigger | `TestPatchBadge_PF_AC001_PendingKernelRaisesNoManualReboot`; `TestPatchBadge_PF_AC001_NoBadgeTextTellsToRebootWhenNotRequired` (scans every text field for "reboot this host" / "Reboot this host") |
| AC-002 | AD-001 Reboot line | `TestPatchBadge_PF_AC002_PendingKernelDescribedAsFutureManualReboot` (exact string) |
| AC-003 | AD-001 Reboot line | `TestPatchBadge_PF_AC003_PendingKernelWithGatedAutoDescribedAsAutomatic` (window: dated string; anytime: "date Unknown") |
| AC-004 | AD-001 NY-10 causes and action | `TestPatchBadge_PF_AC004_RequiredNowRaisesManualRebootWithRebootNowAction`; `TestPatchBadge_PF_AC004_CausesNeverClaimNoKernelPending`; `TestPatchBadge_PF_AC004_CauseListNeverEmpty` (table over the cause combinations) |
| AC-005 | AD-002 | `TestKernelReboot_PF_AC005_NewerInstalledKernelIsRequired`; `…_RunningIsNewestNotRequired`; `…_OtherFlavorIgnored`; `…_UnreadableSourcesCountAsNotRequired`; `…_EntryBoundReachedNotOK`; `TestCompareDebVersion_PF_AC005_Table` (tilde, epochs, `+deb13`, Ubuntu ABI); `TestUpdatesCollector_PF_AC005_KernelSourceSetsRebootRequiredAndReason` |
| AC-006 | FR-007 regression | `TestPatchBadge_PF_AC006_PendingOnlyIsNotRebootRequired` (`devicePatchInfo.reboot_required` false; badge kind is not needs-you from NY-10); vitest `[PATCH-FLOW-001 AC-006] fleet shows no reboot-required indicator for pending-only hosts` and `… device page shows no Reboot now for pending-only hosts` |
| AC-007 | AD-010 | vitest `[PATCH-FLOW-001 AC-007] checkbox label is exact and unchecked on every open` (open, tick, cancel, reopen) |
| AC-008 | `rebootOption` | vitest `[PATCH-FLOW-001 AC-008] checkbox disabled with allow_reboot reason`; `patchFlow.test.ts` `rebootOption allow_reboot false` |
| AC-009 | `rebootOption` | vitest `[PATCH-FLOW-001 AC-009] checkbox disabled for an older agent` (no `allow_reboot`, no capability; and `allow_reboot` true without capability) |
| AC-010 | `patchDialogStatements` | vitest `[PATCH-FLOW-001 AC-010]` table: (a), (b), (c), and none when `rc = 0` |
| AC-011 | `patchDialogStatements` | vitest `[PATCH-FLOW-001 AC-011] dialog states an already-required reboot` |
| AC-012 | AD-004, AD-010 | `TestSendCommand_PF_AC012_AuthorizedDispatchSetsIncludeRebootClass`; vitest `[PATCH-FLOW-001 AC-012] ticked confirm sends reboot_if_required true` |
| AC-013 | AD-004 step 2; AD-005 unauthorized path | `TestSendCommand_PF_AC013_FalseAuthorizationStoredAsAbsent`; `TestSendCommand_PF_AC013_AbsentAuthorizationUnchanged`; `TestOSUpdate_PF_AC013_UnauthorizedRunNeverRebootsWhenRequired` (a required reboot with nothing applied → no `execReboot`); vitest `[PATCH-FLOW-001 AC-013] unticked confirm sends mode only` |
| AC-014 | AD-004 step 1 | existing `[AC-013]` strip test (kept); `TestSendCommand_PF_AC014_ClientIncludeRebootClassStrippedWithoutAuthorization` |
| AC-015 | AD-004 step 2 | `TestSendCommand_PF_AC015_NonBooleanAuthorizationRejected` (table: `"true"`, `1`, `null`, `{}`; asserts no `Create` call on the mock repo); `TestSendCommand_PF_AC015_RepairWithAuthorizationRejected` |
| AC-016 | AD-004 step 3 | `TestSendCommand_PF_AC016_AllowRebootFalseRejectedWithRebootNotAllowed`; `…_MissingCapabilityRejectedWithUnsupported`; `…_NoTelemetryRejectedWithUnsupported`; `…_TelemetryErrorIs500NoCommand`; `TestUpdateInfo_PF_AC016_CapabilityOnlyTrueCounts`; `TestTelemetry_PF_AC016_AgentReportsCapability` |
| AC-017 | AD-005, AD-006 | `TestRebootAuth_PF_AC017_RequiredAfterSuccessRebootsOnceAfterResultQueued` (asserts `out.Add` before the `execReboot` runner call, and the suffix "reboot started: yes"); `…_WaitsForDeliveryAck`; `…_DeliveryWaitBoundedThenReboots` |
| AC-018 | AD-005 | `TestRebootAuth_PF_AC018_NotRequiredNoReboot` (suffix "not required") |
| AC-019 | AD-005; helper defer | `TestRebootAuth_PF_AC019_FailedRunNoReboot`; `…_TimedOutRunNoReboot`; `…_InterruptedRunNoReboot` (suffix "run not successful"); `TestWorker_PF_AC019_HoldsRestoredOnFailedAuthorizedRun` |
| AC-020 | AD-005 step A.3, B.3 | `TestRebootAuth_PF_AC020_AgentVetoBeatsAuthorization` (suffix "allow_reboot off", telemetry triggered); `TestPatchGateEvent_PF_AC020_RebootRequiredEventFires` (generator: `reboot_required` transition fires once) |
| AC-021 | AD-005 claim; AD-012; recovery | `TestRebootAuth_PF_AC021_SecondExecutionSameIDSuppressed`; `TestRebootIntents_PF_AC021_ClaimedIDInsideWindowNeverPruned` (claim, advance the clock 71 h with 255 other old entries, re-claim → `claimAlreadyClaimed`); `TestRecoverMaintenance_PF_AC021_RecoveredAuthorizedResultNeverReboots` |
| AC-022 | FR-026 | `TestAutoPatch_PF_AC022_DispatchParamsUnchanged` (gated, off, out-of-window); `TestOSUpdate_PF_AC022_AutomatedRebootRuleUnchanged` (applied reboot-class → reboot; none applied → no reboot) |
| AC-023 | `devicePatchState` S4 | vitest `[PATCH-FLOW-001 AC-023] Reboot now is primary and opens the reboot confirmation` |
| AC-024 | `rebootDisabledReason` | vitest `[PATCH-FLOW-001 AC-024] Reboot now disabled with visible outside-rIOt reason` |
| AC-025 | AD-003 | `TestListsFreshness_PF_AC025_ConfigNewerThanListsIsStale` (`sources.list.d` and `preferences.d` cases); `…_RawCountStillReported` (collector with a fake `apt list`); `…_ConfigOlderIsFresh`; `…_BackupFilesIgnored` (`.dpkg-old`, `.save`); `…_UbuntuStampCountsAsRefresh` |
| AC-026 | AD-003 | `TestListsFreshness_PF_AC026_NoListsIsStale` (missing dir; dir without `_Packages`); `…_StatErrorIsUnknown`; `…_EntryBudgetIsUnknown`; `…_DeadlineIsUnknown`; `…_FutureMtimeIsUnknown`; `…_NoFilesCreated` (dir snapshot before and after); `…_AtMostOneDebugLine` (capture handler) |
| AC-027 | AD-010, AD-011 | vitest `[PATCH-FLOW-001 AC-027] stale replaces Patch (N) and stays usable`; `[PATCH-FLOW-001 AC-027] fleet panel row shows stale label instead of count`; `TestPatchStatus_PF_AC027_RowCarriesStaleFreshness` |
| AC-028 | AD-001 stale | `TestPatchBadge_PF_AC028_StaleDropsPendingKernelWordingAndAddsNote`; `…_StaleRaisesNoPendingDerivedReason` (same reasons with and without `PendingRebootClassCount`) |
| AC-029 | AD-003 normalization | `TestUpdateInfo_PF_AC029_FreshnessLenientDecode` (absent, `bogus`, `42`, `null`); `TestTelemetryIngest_PF_AC029_OldAndBogusAccepted2xxNoWarn`; `TestPatchStatus_PF_AC029_AbsentAndBogusServeUnknown`; vitest `[PATCH-FLOW-001 AC-029] unknown freshness keeps Patch (N)` |
| AC-030 | AD-003 | `TestUpdatesCollector_PF_AC030_DnfNeverReportsStale` (field empty) |
| AC-031 | FR-039 | `TestAutoPatch_PF_AC031_FreshnessDoesNotChangeDispatch` (`stale` vs `fresh` → identical commands) |
| AC-032 | AD-007 | `TestWorker_PF_AC032_ConfigChangedDuringUpgradeRefreshesOnce` (exactly one extra refresh argv, after verify; `[post_refresh]` in output; `TouchLists` called twice); `…_OutputStaysWithinBound`; `TestListsFreshness_PF_AC032_StampAfterRewriteReportsFresh` |
| AC-033 | AD-007 | `TestWorker_PF_AC033_NoChangeNoExtraRefresh`; `…_RepairModeNoRefreshAtAll`; `…_FailedUpgradeNoExtraRefresh` |
| AC-034 | AD-007 | `TestWorker_PF_AC034_FailedPostRefreshKeepsSuccessWithNote`; `…_InterruptedPostRefreshKeepsSuccessNotInterrupted`; `…_LowBudgetSkipsWithNote` |
| AC-035 | AD-009 | `TestOSUpdate_PF_AC035_EveryNonRebootOutcomeTriggersOneTelemetry` (success, error, precheck, unauthorized empty-applied, authorized not-started); `…_TriggersCoalesce` (two triggers → one queued signal); `TestRebootAuth_PF_AC035_NoTriggerWhenRebootStarts` |
| AC-036 | AD-009 | `TestTelemetryLoop_PF_AC036_FirstSendImmediateOnStart` (fake client; no ticker advance) |
| AC-037 | AD-004 audit, AD-006 | `TestSendCommand_PF_AC037_StoredParamsShowAuthorizationAndIncludeRebootClass`; `TestCommandSent_PF_AC037_EventMentionsAuthorization`; `TestRebootAuth_PF_AC037_SuffixGrammarAllReasons` (table, including `reboot intent store full`) |
| AC-038 | composite | `TestPatchBadge_PF_AC038_VermontBeforeRunNeverSaysRebootByHand`; `TestRebootAuth_PF_AC038_VermontRunReportsThenRebootsOnce` (fake maintenance client success, required true); `TestWorker_PF_AC038_PostinstRewriteTriggersOneRefresh`; plus the manual host check in DoD |
| NFR-006 / BR-008 (intent-store bound and self-heal) | AD-012 | `TestRebootIntents_PF_Bound_PrunesOlderThan30Days`; `…_KeepsEntriesYoungerThan30Days`; `…_PrunedMapSavedEvenWhenAlreadyClaimed`; `…_CapEvictsOldestFirstOnlyBeyond72h` (256 entries, oldest > 72 h → evicted, claim OK, size stays 256); `…_CapNeverEvictsYoungerThan72hFailsClosed` (256 entries all < 72 h → `claimStoreFull`, file unchanged); `…_ClockBackwardsSkipsPruneAndEvict`; `…_LegacyV1LoadedAsNowAndRewrittenV2`; `…_LegacyV1EntryNotPrunedBefore30Days`; `…_InvalidKeysAndValuesDropped`; `…_ParseErrorFailsClosed`; `…_InfoLogOnlyWhenPruned`; `…_RebootsResumeAfter256OldIntents` (the regression of today's permanent suppression); `TestRebootAuth_PF_Bound_StoreFullSuppressesWithDetail` |

## 9. Error Handling

| Failure | Behavior | Log |
|---|---|---|
| Non-boolean `reboot_if_required`, or repair + authorization | 400 `invalid_request`, no command | none |
| Latest telemetry missing, no `updates`, no capability, `allow_reboot` absent | 400 `reboot_authorization_unsupported` | none |
| `allow_reboot` false | 400 `reboot_not_allowed` | none |
| Telemetry read error | 500 `failed to create command` | `slog.Error("os_update authorization: latest telemetry", device_id, error)` |
| Agent: non-bool param | precheck failure `reboot_if_required must be a boolean` | existing command-failed Warn |
| Intent store: save failure | no reboot; suffix `suppressed (reboot intent could not be recorded)` | existing Warn |
| Intent store: parse error | no reboot (fail-closed); suffix `suppressed (reboot intent could not be recorded)` | Warn (one per claim) |
| Intent store: full within 72 h | no reboot; suffix `suppressed (reboot intent store full)` | Warn (one per claim) |
| Intent store: entries pruned or evicted | claim proceeds | one Info |
| Phase B host busy, shutdown, maintenance busy, `execReboot` error | no reboot; one warning agent event; telemetry trigger | Warn |
| Phase B delivery wait times out | reboot proceeds; result stays in the outbox | Warn `result not yet delivered; rebooting with result queued` |
| Kernel or marker source unreadable | counts as not required | none (marker: existing debug) |
| Freshness check error, bound or skew | `unknown` (today's display) | one Debug line |
| Helper fingerprint error | treated as changed → one extra refresh | none (summary unaffected unless the refresh fails) |
| Helper extra refresh fails or times out | outcome unchanged (success); summary note | journal (runner output) |
| Helper extra refresh skipped (budget or persistence) | outcome unchanged; summary note | none |
| Helper stamp error | ignored; one bounded output line | output |
| Lenient telemetry decode of a bad value | normalized; 2xx | none (NFR-009) |

## 10. Security Considerations

- **Widened hold-release path (BR-003).**
  - Released only on an explicit, per-command operator param, validated server-side at the API boundary (AD-004).
  - Agent telemetry can only narrow (NFR-003).
  - `include_reboot_class` stays server-derived (client value always deleted).
  - The agent veto `allow_reboot` is re-checked in phase A and again immediately before `execReboot` (NFR-001).
  - `allow_patching` still gates the run (`handleOSUpdateWithOutput`).
  - Holds are restored on every exit by the existing helper defer and the dnf `defer` (NFR-002).
- **Reboot loops (NFR-006).**
  - At most one reboot per command ID through the durable intent store, decided before the result is queued.
  - Recovered results never reboot.
  - Pruning cannot reopen a command that can still arrive: entries younger than 72 h, a window that covers every
    redelivery and replay path, are never pruned or evicted (AD-012).
  - A clock moving backwards disables pruning for that call.
  - No new reboot trigger exists outside an authorized command. Nothing re-dispatches automatically.
- **Untrusted agent data.**
  - Freshness and the capability use lenient decoders, so a hostile value cannot fail ingest or widen anything.
  - Kernel release strings are charset- and length-filtered before reaching `RebootRequiredReasons`, which the UI
    renders as text.
  - The badge already sanitizes agent text.
- **Root helper.**
  - No new command argv; the extra refresh uses the exact allowlisted refresh.
  - `TouchLists` touches one fixed path, refuses a symlink or non-directory, and changes only timestamps.
  - The fingerprint only stats files (no content reads), bounded to 1,000 entries.
  - The request and record protocol is unchanged, so there is no new parsing surface.
- **CSRF / auth.** Same route and same session middleware as today. No new endpoint.
- **Residual risks:**
  - **R-1:** The time-of-check gap between the server's telemetry check and execution. Mitigated by the agent veto
    (AC-020).
  - **R-2:** The phase B lock race can produce "reboot started: yes" followed by an abort. Visible as a warning event
    and continued `reboot_required`.
  - **R-3:** Old-helper false `stale` after an identical-content rewrite with a no-change refresh. Display-only, and
    self-heals on the next list change.
  - **R-4:** Delivery-wait timeout reboots with the result still queued. It is durable and delivered after boot.
  - **R-5:** An agent downgraded after reporting the capability falls back to PATCH-GATE semantics. It also cannot
    parse the v2 intent file and fails closed: no post-patch reboots.
  - **R-6:** A corrupt intent file still fails closed, so post-patch reboots are suppressed until the file is fixed. It
    is never reset automatically, because that would break at-most-once. Each claim attempt logs a Warn. Growth and the
    256-entry exhaustion are fixed by AD-012.

## 11. Performance Considerations

- **Agent per cycle (apt):**
  - ≤ 1,000 `stat`/`readdir` entries for freshness (typically < 100)
  - one `/boot` listing and one 256-byte read for the kernel check
  - no new process spawns
- **Agent per authorized run:** one extra `RebootRequiredNow` evaluation; ≤ 60 outbox polls in phase B.
- **Agent per claim:** one ≤ 20 KiB JSON read and write; the prune and evict passes are O(n log n) over ≤ 256 entries.
- **dnf:** the existing `needs-restarting` per cycle, plus one per authorized run (20 s timeout).
- **Helper per run:** two fingerprints (≤ 1,000 stats each) and at most one extra `apt-get update` (≤ 5 min, inside
  `WorkerBudget`; `RuntimeMaxSec=35min` unaffected).
- **Server:** one `GetLatestSnapshot` per ticked manual dispatch (rare, human-driven). `PatchStatus` adds no query
  (freshness comes from the already-decoded `updates`). No index, no cache, no rate-limit change; the existing command
  creation limiter applies.
- **Web:** no new requests (NFR-011); pure helpers over loaded data.

## 12. Implementation Notes for Engineers

1. **Commits:** `[PATCH-FLOW-001] <type>: <what>`, with types feat, fix, test, refactor, config. Suggested order:
   models → collectors → helper → agent (intent store first) → server → web. One logical change per commit.
2. **Do not touch `internal/maintenance/protocol.go`.** If a change seems to need a new request or record field, stop
   and file a blocker. Old helpers reject unknown request fields, and records are decoded with
   `DisallowUnknownFields`.
3. **Post-run refresh must not use `step()`.** Call `w.Runner.Run` directly so `out.Interrupted` never reaches
   `rec.Interrupted` (`Validate` rejects `success && Interrupted`). Keep the 7-minute skip threshold.
4. **Phase A runs before `completeCommand`.** Compute the suffix and claim inside `executeAction`. Phase B only locks,
   waits and executes. Re-check `a.config.Commands.AllowReboot` immediately before `execReboot`.
5. **Unauthorized path:** leave `maybeRebootAfterPatch`'s reboot conditions byte-identical. Only add the telemetry
   trigger in the empty-applied branch and adapt the claim check to `!= claimOK`. `TestOSUpdate_PF_AC022_*` locks
   this.
6. **Badge strings are exact** (AD-001 table). Update the listed PATCH-BADGE-001 tests; do not weaken
   `TestPatchBadge_EveryReasonCodeHasATest`.
7. **Clocks:**
   - `listsFreshness` takes `now`; the collector gets it from the `listsNow` seam.
   - The intent store uses `a.clock()`.
   - Phase B uses the existing `a.pause` seam and the new `rebootReportWait`.
   - No `time.Now()` in testable decision code (the Worker uses `w.now()`).
   - `pruneRebootIntents` and `evictForCap` are pure and take `now`.
8. **Filesystem tests** use `t.TempDir()` with `os.Chtimes`. Roots are passed as parameters. Tests must pass on Windows
   (`go test ./...`) and Linux CI (`-race`).
9. **Technical writer** (post-QA) must document:
   - The checkbox semantics.
   - That the post-run refresh and lists stamp need `install.sh` re-run per device, while everything else ships with
     the normal agent update (AD-008).
   - The widened `reboot_required` (kernel comparison, and older-kernel-booted hosts).
   - The stale label and its self-healing.
   - The reboot-intent retention: 30 days, with a 72-hour floor, and the corrupt-file fail-closed behavior (AD-012).
10. **Verify on a real Debian 13 host** (vermont is ideal) and record the results in the implementation report:
    1. A no-change `sudo apt-get update` leaves the `/var/lib/apt/lists` mtime unchanged. This confirms why the helper
       stamp exists.
    2. `/boot` is listable by `riot`, and `/proc/sys/kernel/osrelease` matches a `vmlinuz-*` name.
    3. An authorized Patch that installs a kernel produces exactly one reboot, the result arrives before the host goes
       down, and after boot `reboot_required=false`, `lists_freshness=fresh`, pending is correct.
    4. Touching `/etc/apt/preferences.d/<file>` flips the host to `stale` within one cycle.
    5. An existing v1 `patch-reboot-intents.json` on that host is rewritten as v2 after the first claim, with no reboot
       side effects.
11. **Do not add a dependency.** `compareDebVersion` is a port of dpkg's `verrevcmp` (public algorithm); cite it in a
    comment.
12. **Web:** the checkbox state resets in the open handler and in `onClose`, never via effects on `open`. Render the
    disabled reasons as visible text (not only `title`). Use `scrollbar-thin` on any new overflow container.

## 13. Definition of Done

- [ ] Every Section 4 change implemented; every "explicitly unchanged" file untouched (`git diff --stat` reviewed)
- [ ] Every test named in Section 8 exists, carries its AC (or `PF_Bound`) reference, and passes
- [ ] `go test ./...` (Windows) and `go test -race ./...` (Linux CI) green; `cd web && npm run test:run` green
- [ ] `go vet ./...` clean; `npm run lint` and `tsc --noEmit` clean with no new errors
- [ ] No migration added; `make migrate-up` / `make migrate-down` unaffected
- [ ] `grep -n "no kernel or GPU update is pending" internal/` returns nothing
- [ ] `grep -n "include_reboot_class\"\] = true" internal/server` shows exactly two sites (`checkAutoPatch`, `applyManualOSUpdateAuth`)
- [ ] `execReboot` call sites: `handleReboot`, `maybeRebootAfterPatch`, `runAuthorizedReboot`; each is preceded by an `AllowReboot` check
- [ ] `claimPatchReboot` is defined only in `reboot_intents.go`; no path suppresses a claim for size alone while entries older than 72 h exist
- [ ] `maintenance.ProtocolVersion == 1` and `protocol.go` unchanged
- [ ] Section 12 note 10 host verification recorded in `docs/implementation/PATCH-FLOW-001-impl-report.md`
- [ ] Security review (`docs/security/PATCH-FLOW-001-security-review.md`) completed before engineering starts (FRD D-010)

# Architecture Decision Document

- **Story ID:** DIETPI-REFRESH-001
- **FRD Reference:** `docs/requirements/DIETPI-REFRESH-001-frd.md`
- **Amends:** PATCH-FLOW-001 ADD AD-007 (version 2 runs only); EXTRA-UPD-001 FR-025 / AC-020 ordering clause "post-run refresh" (see AD-006)
- **Author:** Architect Agent
- **Date:** 2026-10-27
- **Status:** FINAL

---

## 1. Summary

For a version 2 (extra-updater) run, the helper stops making its post-run apt configuration check at the end of the apt part. Instead, `executeExtras` makes it once, after every present updater's step has ended and before `finalizeV2` builds the record. It uses the baseline fingerprint the apt part took just before its own refresh. The refresh mechanics (phase persist, `Runner.Run` with a 5-minute sub-context, `[post_refresh]` output, list stamp, note) are factored into one helper that the unchanged version 1 path and the new version 2 path share. Version 1 runs keep the exact same call order and bytes. Version 2 runs gain eligibility rules (own refresh succeeded; no interruption; service not stopping) and a budget check against the 80-minute extended budget, measured on both the injected clock and the real context deadline.

## 2. Technical Context

### 2.1 Current state (code read on branch `fix/dietpi-post-refresh`)

| Location | What it does today |
|---|---|
| `internal/maintenance/worker.go` `Execute` | Builds `runCtx` (80 min for v2, 30 min for v1) and, for v2, `aptCtx` (30 min) under it. Runs `executeApt(aptCtx, …)`. v1 then calls `finalizeV1`; v2 calls `executeExtras(ctx /*svc*/, runCtx, aptCtx, …, started)`. |
| `worker.go` `executeApt` | Recovery, then `sourcesBefore, sourcesBeforeErr := w.fingerprintSources()` immediately before the `refresh` step, `stampLists`, `upgrade`, `verify`, then `st.postRefreshNote = w.postRunRefresh(ctx, rec, sourcesBefore, sourcesBeforeErr)` (only reached when upgrade and verify succeeded). Its deferred cleanup restores holds and reads version changes **after** the post-run refresh. Baseline values are locals; nothing records whether the own refresh succeeded. |
| `worker.go` `postRunRefresh` | `Sources == nil` → `""`. Second fingerprint; equal and error-free → `""`. Real-clock `time.Until(ctx.Deadline()) < postRefreshMinRemaining` (7 min) → budget note. Persist phase `post_refresh` (fail → note). `Runner.Run(WithTimeout(ctx, PostRefreshBudget), "/usr/bin/apt-get", aptRefreshArgs()...)`. Append `"\n[post_refresh]\n"+out` via `appendBounded`. Error → `failed:` note with reason `boundedText(err, 200)`; success → `stampLists`. Never touches `ExitCode`/`Interrupted`. |
| `internal/maintenance/extra.go` `executeExtras` | Sets `rec.AptOutcome`, computes `deadline := started.Add(ExtendedWorkerBudget)` on the injected clock, runs `runExtra` per present updater (flag reconciliation and post-step detection are inside `runExtra`), upserts results, calls `finalizeV2`. |
| `extra.go` `finalizeV2` / `summaryV2` | Outcome from `AptOutcome`, updater outcomes and `aptErr`. Summary line 2 ("apt result line"): success → `SuccessSummary` plus `"; "+st.postRefreshNote` if set; interrupted → interruption text; error → `boundedText(aptErr.Error(), 1500)` (no note today, because a v2 note could only arise on apt success). Whole summary `boundedText(…, 4096)`. |
| `extra.go` `runExtra` check 7 | Budget check on the injected clock: `deadline.Sub(w.now()) < ExtraStartThreshold`. |
| `internal/maintenance/protocol.go` | `ExtendedWorkerBudget = 80m`, `ExtraStartThreshold = 47m30s` (pinned by `TestBounds_EU_AC025_WorkerSide`), `TerminationGrace = 90s`, `MaxOutputBytes = 256 KiB`, `SuccessSummary`. `Record.Validate` imposes no phase ordering; `post_refresh` after `extra_run:<id>` validates as long as no updater entry is `running` (true: entries are final when `runExtra` returns). |
| `internal/maintenance/aptsources.go` | `AptSourceEntries` covers `sources.list`, `sources.list.d/*.list|*.sources`, `preferences.d/<no-dot>|*.pref`. `dietpi-raspberrypi` is included. Fingerprint = sorted `path|mtime-ns|size`. Unchanged. |
| `cmd/riot-maintenance/main_linux.go` | Wires `Sources: aptSourcesFingerprint` and `TouchLists: touchAptLists` for every run, v1 and v2. Unchanged. |
| `internal/agent/extra_updaters.go` `extraProgressStep` | Maps any phase that is not `extra_run:<id>` (including `post_refresh`) to `StepVerify`. Unchanged; see AD-007. |

### 2.2 D-004: server classification (answered)

The only server code that reads `SuccessSummary` is `internal/server/handlers/patch_badge_refusals.go`:

- `parseRefusals` first requires line 1 to match `refusalFirstLine` (`^apt: succeeded(…)$`). A run whose apt part failed has line 1 `apt: failed; …`, so it is ignored before line 2 is read. A note appended to the apt failure text therefore cannot change anything.
- For a run with `apt: succeeded`, `successLine` accepts line 2 when it equals `maintenance.SuccessSummary` **or** has the prefix `SuccessSummary + "; "`. A post-run note appended with `"; "` keeps that true. An existing test already pins it (`[DP-SEC-004] the apt note after the success summary is allowed`).
- `onlyDetailLines(lines[2:])` reads lines 3 and later. The note sits on line 2, so it does not affect that check.

**Confirmed:** classification is prefix-based on line 2 and gated on line 1. An appended note never changes it. FRD assumption A-007 holds. No other Go or web code parses the summary (the agent passes `rec.Summary` through verbatim as `commandExecResult.Message`). AC-023 adds a server-side test that pins this.

### 2.3 FRD review notes (none blocking)

1. **EXTRA-UPD-001 FR-025 / AC-020** says each updater runs after "upgrade, verification, post-run refresh". For v2 that clause is superseded by this FRD's FR-002/FR-003. NFR-005 already permits the one test that asserts this order to change. This is recorded in AD-006, and the technical writer updates `docs/ops/os-maintenance.md`.
2. **FR-015 clock:** "remaining extended budget measured from the run's start" can mean the injected clock (as `runExtra` check 7 uses) or the real context deadline (as v1 `postRunRefresh` uses). AC-019 needs an exact boundary, which only the injected clock gives. NFR-002 needs a real-deadline guarantee. Resolved in AD-003: both are checked.
3. **FR-014 "service told to stop":** this cannot be isolated end-to-end, because every way to cancel the service context during a run also marks the apt part or an updater as interrupted. It is tested by calling the v2 method directly (AC-017 second test).

## 3. Architecture Decisions

### AD-001: Move the v2 check into `executeExtras`, between the updater loop and `finalizeV2`

**Decision.**
- `executeApt` still takes the baseline fingerprint immediately before the `refresh` step (FR-005). It now stores the baseline in `aptState`, together with a flag that is set only when the `refresh` step returned without error.
- For a version 1 request, `executeApt` still calls `postRunRefresh` at exactly the same point with the same arguments.
- For a version 2 request, `executeApt` makes no post-run check at all (FR-002).
- `executeExtras` calls a new `postRunRefreshV2` after the `for … Registry()` loop has upserted the last result, and before `finalizeV2` (FR-003, FR-004). This happens even when no updater is present or the loop broke on `FS == nil`. The returned note goes into `st.postRefreshNote`, which `summaryV2` already reads.

**Rationale.**
- At that point every `runExtra` has fully returned, including `ReconcileFlags` and the post-step `Detect` (FR-003).
- Holds are already restored, because `executeApt`'s defer ran before `executeExtras`.
- The record has not been finished yet, so the note and output still land in the one terminal `Finish`.
- `aptState` already exists to carry exactly this kind of state from the apt part to the finaliser.

**Alternatives considered.**
- *Second check at the end of `executeExtras` while keeping the apt-part check.* Rejected. It allows two refreshes (violates FR-009), and the AC-004 order rule demands no refresh during the apt part.
- *Check inside `finalizeV2`.* Rejected. `finalizeV2` sets `Phase = "finished"` and `FinishedAt` first, and mixing the refresh I/O into the outcome computation makes outcome isolation (FR-019..FR-021) harder to see.
- *Check after each updater.* Rejected. It allows N refreshes and contradicts FR-003's "exactly once".

**Consequences.** In a v2 record, `post_refresh` now follows `extra_wait:<id>`/`extra_run:<id>`, and `[post_refresh]` output follows `[extra:<id>]` (NFR-004). A worker killed during the v2 post-run refresh is recovered by the existing `RecoverInterrupted` + `markExtraInterrupted` path (no running updater entry exists then, and `AptOutcome` is already set). That is out of scope per FRD §9.

### AD-002: One shared refresh mechanic; v1 composition and call order unchanged

**Decision.** Split today's `postRunRefresh` body into two private helpers. Rebuild `postRunRefresh` (v1) from them in the identical order. Build `postRunRefreshV2` from the same helpers.

| Helper | Responsibility (moved verbatim from today's `postRunRefresh`) |
|---|---|
| `sourcesChanged(before, beforeErr)` | `Sources == nil` → false. Otherwise call `Sources()` once. Return `beforeErr != nil \|\| afterErr != nil \|\| before != after` (FR-006, FR-007). |
| `runPostRefresh(ctx, rec)` | Persist phase `post_refresh` (fail → `skipped: state not persisted` note). `WithTimeout(ctx, PostRefreshBudget)`. `Runner.Run` with `"/usr/bin/apt-get", aptRefreshArgs()...`. `appendBounded` of `"\n[post_refresh]\n"+out.Output` with `out.Truncated`. Error → `failed: `+`boundedText(err.Error(), 200)`. Success → `stampLists(rec)`, return `""`. Never writes `ExitCode`, `Interrupted`, `AptOutcome` or any `ExtraUpdaters` field. |

v1 `postRunRefresh` = `sourcesChanged` → real-clock budget check on its `ctx` (unchanged text and threshold) → `runPostRefresh`.

**Rationale.**
- FR-031 (same argv) and FR-022..FR-025 (same notes, output label, stamp) hold by construction when both versions run the same code.
- v1 byte identity holds because the v1 sequence of observable effects (`Sources` call, deadline read, `Save`, `Run`, append, stamp) is unchanged.

**Alternatives considered.**
- *Duplicate the body for v2.* Rejected: two copies of the note strings and argv would drift.
- *Add a version parameter to `postRunRefresh`.* Rejected: it puts v2 eligibility branches into the v1 path, which `golden_v1_test.go` protects.

**Consequences.** `postRunRefresh` keeps its signature, so no existing test changes. Engineers must not change the strings, the argv helper (`aptRefreshArgs`) or the `appendBounded` call while moving them.

### AD-003: v2 budget check uses both the injected clock and the real deadline

**Decision.** `postRunRefreshV2` skips with `post-run package list refresh skipped: worker time budget nearly exhausted` when **either** of these holds:
- `started.Add(ExtendedWorkerBudget).Sub(w.now()) < postRefreshMinRemaining` (injected clock, the same basis as `runExtra` check 7); or
- `runCtx` has a deadline and `time.Until(deadline) < postRefreshMinRemaining` (real clock, the same basis as v1).

Exactly 7 minutes remaining is enough; the comparison is strict `<` (AC-019). The refresh sub-context is derived from `runCtx`, never from `aptCtx` (the 30-minute apt context may already have expired) and never from `context.Background()`.

**Rationale.**
- The injected clock makes FR-015 and AC-019 testable to the second with `fakeClock`.
- The real deadline is what actually kills the process, so it alone guarantees NFR-002: with at least 7 minutes left, the 5-minute refresh plus the 90-second SIGTERM→SIGKILL grace ends inside `runCtx`.
- In production both clocks are the same wall clock, so the double check costs nothing.

**Alternatives considered.**
- *Real clock only (as v1).* Rejected: an exact 7m00s / 6m59s boundary is not testable without sleeping.
- *Injected clock only.* Rejected: a test or production `Now` that drifts from the real deadline could start a refresh that `runCtx` then kills. That would be harmless to the outcome (AD-004), but it breaks the FR-017 promise.

**Consequences.** `postRunRefreshV2` takes `started time.Time`; `executeExtras` already has it.

### AD-004: v2 eligibility rules, evaluated before any fingerprint read

**Decision.** `postRunRefreshV2` returns `""` (no refresh, no note, and `Sources` is not called) in this order:
1. `st.listsRefreshed` is false. The own refresh never ran (recovery or verification failure before it; `repair` mode, which is refused for v2 anyway) or it failed (FR-010, FR-011, BR-005).
2. `rec.AptOutcome == aptOutcomeInterrupt` (FR-014).
3. Any `rec.ExtraUpdaters[i].Outcome == UpdaterInterrupted` (FR-014).
4. `svc.Err() != nil`, meaning the service context is ending (FR-014, BR-004).

Then: if `sourcesChanged(st.sourcesBefore, st.sourcesBeforeErr)` is false → `""` (FR-030). Then the AD-003 budget check. Then `runPostRefresh(runCtx, rec)`.

Nothing else is consulted:
- **Apt outcome `error` after a successful own refresh stays eligible** (FR-012). Examples: upgrade or verification failure, hold restoration failure, `verify package versions` failure, a persistence failure in a later phase.
- **Updater outcomes `success`, `failed`, `timed_out` and `skipped` (any reason, including `apt_not_successful` and launcher refusals) stay eligible** (FR-013, BR-003).

**Rationale.**
- The listed guards are exactly the FRD's ineligibility set.
- Checking them before the fingerprint read means an interrupted run does no further I/O, not even a stat walk (BR-004).
- After an apt failure the record already says `error`. One more `apt-get update` cannot make that worse, and it keeps the freshness badge truthful.

**Consequences.**
- A v2 run whose upgrade fails after the lists were refreshed now gets a post-run refresh when the configuration changed. That is new behaviour (FR-012) and differs from v1 (OQ-001 keeps v1 as is).
- A post-run refresh that the service stop interrupts *mid-flight* behaves as in v1: `Runner.Run` returns an error, the note is `failed: …`, and the outcome is unchanged (FR-019).

### AD-005: Note on the apt result line for apt failures (v2 only)

**Decision.** `summaryV2` appends `"; " + st.postRefreshNote` to the apt result line in the **success** case (unchanged) and in the **error** case, after `boundedText(aptErr.Error(), 1500)`. It never appends it in the **interrupted** case. AD-004 guarantees there is no note then, but the switch arm must not append it either, as a second safeguard. The note itself is bounded to `"post-run package list refresh failed: "` (38 bytes) plus 200 bytes of reason. The whole summary stays under the existing `boundedText(…, 4096)`.

**Rationale.** FR-026..FR-029 and BR-006. D-004 (§2.2) shows the server ignores line 2 of an apt-failed run and prefix-matches line 2 of an apt-succeeded run.

**Consequences.**
- The apt result line grows by at most 240 bytes. Its upper bound is 1,500 + 240 = 1,740 bytes for a failure line.
- Under the 4,096-byte cap, lines that follow (updater detail lines and the 20-line tail) can lose up to 240 bytes from their end. That truncation already exists and stays bounded.

### AD-006: OQ-001 and OQ-002 decided; EXTRA-UPD-001 ordering clause amended

**Decision.**
- **OQ-001: No.** Version 1 does not gain a refresh after a failed upgrade. Ryan's constraint (v1 byte-identical) and `TestWorker_PF_AC033_FailedUpgradeNoExtraRefresh` (which pins the v1 behaviour) both rule it out here. A separate story can add it.
- **OQ-002: No.** `ExtraStartThreshold` stays `47m30s`. The code confirms the trade-off:
  - `TestBounds_EU_AC025_WorkerSide` pins the threshold to step limit + grace + lock wait + 1 minute.
  - In the worst case (step started at exactly 47m30s remaining, full 5-minute lock wait, full 40-minute step, 90-second grace), 1 minute remains, so the refresh is skipped with the FR-016 note. That is visible and accepted (A-005).
  - The typical PiCUPS run leaves more than 70 minutes.
  - Raising the threshold by 7 minutes would turn more slow apt parts into `insufficient_time` DietPi refusals, which is a worse outcome than a stale badge.
- **EXTRA-UPD-001 FR-025 / AC-020:** for v2 runs, "post-run refresh" is removed from the list of steps that precede the updater. `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore` changes as specified in §4 (permitted by NFR-005).

### AD-007: No agent, server, web, installer or schema change

**Decision.** The fix lives only in `internal/maintenance` (the `riot-maintenance` helper binary). The record format, request format, `ProtocolVersion`, `RequestVersionExtra`, `Record.Validate` and `parseRecord` stay unchanged.

While a v2 record sits in `post_refresh` after the updater step, the agent's progress step is `StepVerify` (`extraProgressStep` maps every non-`extra_run:` phase to it). Its `extra_updater_*` events are already sent by then, because `reportExtraProgress` reads finished updater results. This is acceptable, and no agent change is made (FR-033).

**Consequences.** The behaviour reaches a host only when `install.sh` is re-run as root (D-005). Older helpers keep today's behaviour, and the agent and server accept records from both (NFR-004).

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `internal/maintenance/worker.go` | (a) `aptState` gains `listsRefreshed bool`, `sourcesBefore string`, `sourcesBeforeErr error`. (b) `executeApt` stores the baseline in `st` immediately before the `refresh` step (same call position as today), and sets `st.listsRefreshed = true` right after `step("refresh", …)` returns nil (before `stampLists`). It calls `postRunRefresh` only when `req.Version == ProtocolVersion`. (c) Extract `sourcesChanged` and `runPostRefresh` from `postRunRefresh` (AD-002); `postRunRefresh` keeps its signature, behaviour and doc comment, updated to say "version 1". (d) Add `postRunRefreshV2` (AD-003, AD-004) beside `postRunRefresh`, with a doc comment citing DIETPI-REFRESH-001 FR-002..FR-018. (e) Update the `executeApt` doc comment: "and, for version 1, the post-run refresh". |
| MODIFY | `internal/maintenance/extra.go` | (a) `executeExtras`: after the updater loop and before `finalizeV2`, set `st.postRefreshNote = w.postRunRefreshV2(svc, runCtx, rec, st, started)`. (b) `summaryV2` default (apt error) arm appends `"; "+st.postRefreshNote` when non-empty (AD-005); the interrupted arm does not. (c) Update the file-header and `executeExtras` comments to state that the single post-run check follows the updater step. |
| MODIFY | `internal/maintenance/extra_test.go` | Only `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore` changes (NFR-005). Remove `"post-run refresh"` from the "comes before DietPi" map. Replace the subtest `[AC-020] DietPi is the last thing that ran` with `[AC-020] after the DietPi call only the post-run refresh runs (DIETPI-REFRESH-001)`, which asserts `g.log[step+1:]` equals exactly `["apt:"+refreshKey]`. Change `[AC-020] the post-run refresh really ran in this scenario` to assert `postRefresh > step`. No other test in the file changes. |
| CREATE | `internal/maintenance/extra_postrefresh_test.go` | All v2 tests for AC-003..AC-027 (§8). Test-only helpers, all in this file: `cfgState` (mutable fingerprint source: `bump()`, `fail(err)`, and a `sources()` method usable as `Worker.Sources`); `postRefreshRunner` (wraps `*orderRunner`, counts calls whose key is `refreshKey`, and for the N-th refresh call can return configured output, an error, `Interrupted: true`, and record `ctx.Deadline()` and `time.Now()`); `loggingTouch` (appends `"stamp"` to `g.log`, optional error on the N-th call); `withoutSources(g)` (builds an identical rig with `Sources = nil` as the "pre-story" oracle, AD-004 rationale in §12). Reuses `newExtraRig`, `fakeClock`, `validatingStore`, `fakeStep`, `orderRunner`, `sourcesSequence`, `sourcesWithErrors`, `writeFixture`, `firstLine`. |
| CREATE | `internal/maintenance/worker_postrefresh_v1_test.go` | AC-002 characterization: pinned JSON bytes, phase sequence and call list of a v1 `full` run with a fingerprint change (plain, and with `IncludeRebootClass`). **Must be written and committed against the unmodified code first** (see §12). |
| CREATE | `internal/server/handlers/patch_badge_postrefresh_test.go` | AC-023 server half: badge classification is equal with and without the post-run note on line 2, for an apt-failed run and for an apt-succeeded run with a refusal. Test only; no server code changes. |
| MODIFY (technical writer) | `docs/ops/os-maintenance.md` | §"Post-run package list refresh": v2 timing, the 80-minute budget basis, refresh after an apt failure that followed a successful refresh (v2 only), no refresh after interruption. §"What the step does" line "It runs after apt: …, the post-run refresh and …" → the post-run refresh now follows the updater step. |
| MODIFY (technical writer) | `CHANGELOG.md` | Story entry, including the D-005 `install.sh` re-run requirement. |
| UNCHANGED (must still pass) | `internal/maintenance/golden_v1_test.go`, `worker_test.go`, `worker_qa_test.go`, `extra_dp_test.go`, `extra_qa_test.go`, `qa_dietpi_test.go`, `protocol_extra_test.go`, `lifecycle_linux_test.go`, all `internal/agent` and `internal/server` tests | Regression oracles (NFR-005). |
| UNCHANGED | `internal/maintenance/aptsources.go`, `protocol.go`, `cmd/riot-maintenance/main_linux.go`, `scripts/install.sh`, sudoers, systemd unit/drop-in, all of `internal/agent`, `internal/server`, `web/`, `cmd/riot-server/migrations/` | FR-032, FR-033, NFR-003. |

## 5. Data Model Changes

**Database:** none. **Record JSON (`maintenance.Record`):** no field added, removed or retyped. **Request:** none.

**In-memory only: `aptState` (worker.go)**

| Field | Before | After |
|---|---|---|
| `postRefreshNote string` | present | unchanged (set by v1 `executeApt` or by `executeExtras` for v2) |
| `listsRefreshed bool` | — | true only after the run's own `refresh` step returned nil |
| `sourcesBefore string` | local var in `executeApt` | stored here; value taken at the same moment as today |
| `sourcesBeforeErr error` | local var in `executeApt` | stored here |

**Record content changes (v2 only):**

| Item | Before | After |
|---|---|---|
| Phase sequence, change detected, apt success, DietPi ran, holds released | `… refresh, upgrade, audit, check, post_refresh, hold_restore, extra_wait:dietpi, extra_run:dietpi, finished` | `… refresh, upgrade, audit, check, hold_restore, extra_wait:dietpi, extra_run:dietpi, post_refresh, finished` |
| Phase sequence, change only from DietPi | no `post_refresh` | `… extra_run:dietpi, post_refresh, finished` |
| Output | `[post_refresh]` before `[extra:dietpi]` | `[post_refresh]` after `[extra:dietpi]` (and after `[lists_stamp]` of the own refresh, if any) |
| Summary line 2, apt error after a successful refresh + note | apt failure text only | `boundedText(aptErr,1500) + "; " + note` |
| No change detected | — | byte-identical to today (FR-030) |

## 6. API / Interface Contract

No HTTP, WebSocket or agent protocol change. Internal Go contract (package `maintenance`, all unexported except as noted):

| Symbol | Signature | Contract |
|---|---|---|
| `(*Worker).sourcesChanged` | `(before string, beforeErr error) bool` | Calls `Sources` at most once. Nil `Sources` → false without calling anything. |
| `(*Worker).runPostRefresh` | `(ctx context.Context, rec *Record) string` | Returns `""` on success, or exactly one of `post-run package list refresh skipped: state not persisted` / `post-run package list refresh failed: <≤200 bytes>`. Performs at most one `Runner.Run`, with argv `"/usr/bin/apt-get", aptRefreshArgs()...` and a context whose deadline is ≤ `PostRefreshBudget` from the call and ≤ `ctx`'s deadline. Mutates only `rec.Phase`, `rec.UpdatedAt`, `rec.Output`, `rec.OutputTruncated`. |
| `(*Worker).postRunRefresh` | `(ctx context.Context, rec *Record, before string, beforeErr error) string` | **Unchanged** signature and behaviour; v1 only. |
| `(*Worker).postRunRefreshV2` | `(svc, runCtx context.Context, rec *Record, st *aptState, started time.Time) string` | AD-004 guards → `sourcesChanged` → AD-003 budget (note `post-run package list refresh skipped: worker time budget nearly exhausted`) → `runPostRefresh(runCtx, rec)`. Never mutates `Outcome`, `ExitCode`, `Interrupted`, `AptOutcome`, `ExtraUpdaters`, `HoldsReleased`, `VersionChanges`. |
| `summaryV2` line 2 (record `summary`) | — | success: `SuccessSummary` \| `SuccessSummary + "; " + note`. error: `boundedText(aptErr,1500)` \| that `+ "; " + note`. interrupted: unchanged, never a note. |

Exactly three note strings exist (FRD §7). The success path adds none (FR-024).

## 7. Sequence / Flow

### 7.1 Version 2 run (`full`/`security`)

1. `Execute` validates and calls `Store.Begin`. It creates `runCtx` (80 min) and `aptCtx` (30 min, child of `runCtx`).
2. `executeApt(aptCtx, …)`:
   1. hold release (if reboot class), configure/audit/check, optional repair.
   2. `st.sourcesBefore, st.sourcesBeforeErr = fingerprintSources()`.
   3. `step("refresh")`. Error → return (`listsRefreshed` stays false). Success → `st.listsRefreshed = true`, then `stampLists`.
   4. upgrade, verify. An error returns it.
   5. **v2: no post-run check.**
   6. The defer restores holds and reads version changes.
3. `executeExtras(svc, runCtx, aptCtx, …)`:
   1. `rec.AptOutcome = aptOutcome(…)`.
   2. For each present updater, `runExtra` (gates, wait, step, reconcile flags, detect) and upsert the result.
   3. `st.postRefreshNote = postRunRefreshV2(svc, runCtx, rec, st, started)`:
      1. Ineligible (AD-004 1–4) → `""`.
      2. `sourcesChanged` false → `""`.
      3. Budget (AD-003) short → budget note.
      4. `runPostRefresh(runCtx, rec)`, which persists `post_refresh`, then `Runner.Run`, appends `[post_refresh]`, then stamps or returns a note.
   4. `finalizeV2`, unchanged except that `summaryV2` places the note on line 2 (AD-005). Then `Store.Finish`.

### 7.2 Version 1 run

The sequence is identical to today. `executeApt` step 5 calls `postRunRefresh(aptCtx, rec, st.sourcesBefore, st.sourcesBeforeErr)`, which is `sourcesChanged` → real-clock 7-minute check on `aptCtx` → `runPostRefresh(aptCtx, rec)`. Then the defer (holds), then `finalizeV1`.

## 8. Acceptance Criteria Mapping

Test name prefix for new v2 tests: `TestPostRefreshV2_DR_`; file `internal/maintenance/extra_postrefresh_test.go` unless noted. "Oracle rig" = the same scenario on a rig with `Sources = nil`, which pre-story and post-story code treat identically (no post-run check at all).

| AC | Fulfilled By | Test |
|---|---|---|
| AC-001 | AD-002 (v1 call order untouched) | `TestExecute_EU_AC044_V1Golden` in `golden_v1_test.go`, **unmodified**, passes |
| AC-002 | AD-002, `executeApt` v1 branch | `TestWorker_DR_AC002_V1PostRefreshRecordPinned` (`worker_postrefresh_v1_test.go`): pinned JSON, phases `…check, post_refresh, finished` and `…check, post_refresh, hold_restore, finished`, call list with the refresh at index 7. Plus the existing `TestWorker_PF_AC032_*`, `TestWorker_PF_AC033_*`, `TestWorker_PF_AC034_*`, `TestWorker_PF_AC038_*`, `TestWorker_QA_AC032_*`, `TestWorker_QA_AC033_*`, unmodified |
| AC-003 | AD-001, `postRunRefreshV2`, `runPostRefresh` stamp | `TestPostRefreshV2_DR_AC003_DietPiRewritesPreferencesD`. Real `AptSourceEntries` over the rig root with `etc/apt/preferences.d/dietpi-raspberrypi`; the fake step's `onCall` runs `os.Chtimes` (same size). Asserts: refresh calls == 2 and the second follows `step:dietpi` in `g.log`; the last `"stamp"` in `g.log` follows `step:dietpi`; outcome `success`, exit 0; line 2 == `SuccessSummary` exactly |
| AC-004 | AD-001 (no apt-part check) | `TestPostRefreshV2_DR_AC004_AptChangeRefreshedAfterUpdater`. `cfgState.bump()` in `runner.after[upgradeKey]`. Asserts: no `refreshKey` entry between the upgrade and `step:dietpi` in `g.log`; exactly one after it; `Index(Output,"[post_refresh]") > Index(Output,"[extra:dietpi]")` |
| AC-005 | AD-001, single call site | `TestPostRefreshV2_DR_AC005_AptAndUpdaterChangeRefreshOnce`: bump in the upgrade and in the step `onCall`; total refresh calls == 2 (own + one post-run) |
| AC-006 | AD-004 `sourcesChanged` false | `TestPostRefreshV2_DR_AC006_NoChangeRecordIdenticalToOracle`: constant fingerprint; no saved record has phase `post_refresh`; `json.Marshal(rec)` equals the oracle rig's bytes (both on `fakeClock`) |
| AC-007 | `sourcesChanged` (FR-007) | `TestPostRefreshV2_DR_AC007_AfterUpdaterFingerprintErrorRefreshes`: `sourcesWithErrors("same", nil, err)`; refresh calls == 2 |
| AC-008 | baseline position (FR-005) | `TestPostRefreshV2_DR_AC008_ChangeDuringRecoveryIsBaseline`: `runner.failAt[configure]` forces repair; bump in `runner.after[configure key]`; refresh calls == 1 |
| AC-009 | AD-001 call after the loop for zero updaters | `TestPostRefreshV2_DR_AC009_NoUpdaterPresentStillChecks`: remove `boot/dietpi/.version`; bump in the upgrade; refresh calls == 2; the last saved phases are `post_refresh`, `finished`; `[post_refresh]` is the last section of the output |
| AC-010 | AD-004 rule 1 | `TestPostRefreshV2_DR_AC010_FailureBeforeOwnRefresh`, two subtests: (a) configure fails and repair fails; (b) audit keeps reporting state after repair (verification failure). Sequence fingerprint `a,b`. Refresh calls == 0; `Sources` call count == 0; summary has no `post-run` |
| AC-011 | AD-004 rule 1, no retry | `TestPostRefreshV2_DR_AC011_OwnRefreshFailsNoRetry`: `runner.failAt[refreshKey]`; refresh calls == 1; summary has no `post-run` |
| AC-012 | AD-004 (apt error eligible), outcome isolation | `TestPostRefreshV2_DR_AC012_UpgradeFailureStillRefreshes`: bump + fail on the upgrade (exit 100). Refresh calls == 2, the second after the updater skip decision (last apt call in `g.log`). Outcome `error`, `AptOutcome` `error`, exit 100. `g.result(rec)` deep-equals the oracle rig's DietPi result (`skipped`/`apt_not_successful`) |
| AC-013 | AD-004 (updater failure eligible) | `TestPostRefreshV2_DR_AC013_UpdaterFailedStillRefreshes`: the step `onCall` chtimes a `preferences.d` file; `StepOutcome{ExitCode:1, Err}`. Refresh calls == 2, the last after `step:dietpi`. Outcome `error`, exit 1. DietPi result deep-equals the oracle rig's |
| AC-014 | AD-004 (timed out eligible) | `TestPostRefreshV2_DR_AC014_UpdaterTimedOutStillRefreshes`: `step.block`, `StepLimit = 50ms`, bump in `onCall`; refresh calls == 2; DietPi outcome `timed_out` |
| AC-015 | AD-004 (refusal eligible) | `TestPostRefreshV2_DR_AC015_UpdaterRefusedStillRefreshes`: lock always held → `dpkg_locked`; bump in the upgrade. Refresh calls == 2; DietPi `skipped`/`dpkg_locked`, same note as the oracle; run outcome equals the oracle's (`success`) |
| AC-016 | AD-004 rule 2 | `TestPostRefreshV2_DR_AC016_AptInterruptedNoRefresh`, two subtests: (a) `postRefreshRunner` returns `Interrupted:true`+error on the upgrade (service alive); (b) the service ctx is cancelled in `runner.after[upgradeKey]`. Fingerprint `a,b`. Refresh calls == 1; summary has no `post-run` |
| AC-017 | AD-004 rules 3 and 4 | `TestPostRefreshV2_DR_AC017_UpdaterInterruptedNoRefresh`: `step.block`, `onCall = cancel`; refresh calls == 1; no `post-run` in the summary. Plus `TestPostRefreshV2_DR_AC017_StoppedServiceSkipsSilently`: calls `w.postRunRefreshV2` directly with a cancelled `svc`, a record with `AptOutcome` success and no updater entries, `st.listsRefreshed = true`, and a fingerprint that would differ. Returns `""`; runner calls == 0; `Sources` calls == 0 |
| AC-018 | AD-003 | `TestPostRefreshV2_DR_AC018_BudgetNearlyExhausted`: step `onCall` advances `fakeClock` so 5 min remain. No refresh after `step:dietpi`; line 2 ends with `; post-run package list refresh skipped: worker time budget nearly exhausted`; outcome, exit code and `AptOutcome` equal the same rig with a constant fingerprint. Subtest `real deadline also guards`: `Execute` with `context.WithTimeout(Background, 6*time.Minute)`, injected clock not advanced; same skip note |
| AC-019 | AD-003 strict `<` | `TestPostRefreshV2_DR_AC019_BudgetBoundary`: `onCall` advances by `ExtendedWorkerBudget - postRefreshMinRemaining` → refresh runs; advances one more second → skipped with the FR-016 note |
| AC-020 | AD-003 sub-context from `runCtx` | `TestPostRefreshV2_DR_AC020_RefreshLimit`: `postRefreshRunner` records the deadline of the 2nd refresh call. Asserts it has a deadline, `deadline − callTime ≤ PostRefreshBudget`, and `deadline ≤ runStart + ExtendedWorkerBudget` (real clock). Subtest with service ctx `WithTimeout(Background, 7m30s)`: deadline ≤ service deadline |
| AC-021 | `runPostRefresh` persist guard | `TestPostRefreshV2_DR_AC021_PhaseNotPersisted`: `store.failPhase = "post_refresh"`; no refresh after `step:dietpi`; line 2 ends with `; post-run package list refresh skipped: state not persisted`; outcome `success` |
| AC-022 | outcome isolation, AD-005 success arm | `TestPostRefreshV2_DR_AC022_RefreshFailsAfterFullSuccess`: 2nd refresh returns `Interrupted:true` and a 1 KiB error. Outcome `success`, exit 0, `Interrupted` false; line 2 == `SuccessSummary + "; post-run package list refresh failed: " + <reason>`, with reason ≤ 200 bytes; `rec.Validate()` nil |
| AC-023 | AD-005 error arm; §2.2 | `TestPostRefreshV2_DR_AC023_NoteAfterAptFailure`: the AC-012 setup with a 2 KiB upgrade error and a failing 2nd refresh. Line 2 == `boundedText(err,1500) + "; post-run package list refresh failed: …"`. Plus `TestPatchBadge_DR_AC023_PostRefreshNoteKeepsClassification` (`internal/server/handlers/patch_badge_postrefresh_test.go`): `computePatchBadge` gives an equal `Kind`, reason codes and `Blocked` for (i) `apt: failed; dietpi: skipped (apt_not_successful)` with and without the note on line 2, and (ii) `apt: succeeded; dietpi: skipped (dpkg_locked)` + `SuccessSummary` with and without the note, for both `failed` and `succeeded` statuses |
| AC-024 | `stampLists` via `runPostRefresh` | `TestPostRefreshV2_DR_AC024_StampFailureIsOutputOnly`: `loggingTouch` fails on the 2nd call. Output contains `[lists_stamp]` after `[post_refresh]`; outcome and summary equal the same rig with a succeeding stamp |
| AC-025 | `appendBounded`, AD-005 bounds | `TestPostRefreshV2_DR_AC025_OutputAndSummaryBounds`: apt upgrade output ≥ 200 KiB, a 2nd refresh with 64 KiB output and a 1 KiB error. `len(Output) ≤ MaxOutputBytes`; `OutputTruncated`; output contains the truncation marker; output ends with the tail of the `[post_refresh]` content; `len(Summary) ≤ 4096`; the note reason ≤ 200 bytes |
| AC-026 | AD-002 shared argv | `TestPostRefreshV2_DR_AC026_SameRefreshCommand`: the two `refreshKey` invocations in `runner.calls` are deep-equal (`path` and `args`) |
| AC-027 | `validatingStore` + explicit parse | `TestPostRefreshV2_DR_AC027_TerminalRecordsValidAndParse`: a table that re-runs the AC-003..AC-025 scenario setups. For each terminal record: `rec.Validate()` nil, `parseRecord(json.Marshal(rec))` nil error, `rec.Terminal()`. `validatingStore` already validates every intermediate `Save` |

**Count:** 27 ACs mapped, 29 named test functions (26 new in `extra_postrefresh_test.go`, 1 new in `worker_postrefresh_v1_test.go`, 1 new server test, 1 unmodified golden), plus the existing v1 suite as additional AC-002 evidence.

## 9. Error Handling

No HTTP surface. Every failure mode maps to a record effect; none changes `Outcome`, `ExitCode`, `Interrupted`, `AptOutcome` or any updater field.

| Failure | Handling | Record effect | Log |
|---|---|---|---|
| `Sources()` error (before or after) | Treated as changed (FR-007) | refresh attempted | none (as v1) |
| Run is ineligible (AD-004) | Silent return | none | none |
| Budget short (AD-003) | No refresh | line 2 `…; post-run package list refresh skipped: worker time budget nearly exhausted` | none |
| `Store.Save` of `post_refresh` fails | No refresh | line 2 `…; post-run package list refresh skipped: state not persisted` | none (the store error is not surfaced, as v1) |
| `Runner.Run` error / timeout / interrupted | No retry | `[post_refresh]` output + line 2 `…; post-run package list refresh failed: <≤200B>` | none |
| `TouchLists` error after a successful refresh | Ignored for outcome | `[lists_stamp]` output line (bounded 200 B) | none |
| Worker killed during the v2 refresh | Existing `RecoverInterrupted` | `interrupted`, "Previous worker stopped…" | existing |
| `Store.Finish` error | Existing `finalizeV2` handling | unchanged | unchanged |

## 10. Security Considerations

- **No new privilege (NFR-003).** The only command is `/usr/bin/apt-get -o Acquire::Retries=0 update`, the argv the worker already runs as root in the same service start, and it is already in `CommandAllowed`. No sudoers line, capability, unit setting, environment variable or argument changes. `scripts/install.sh` is untouched.
- **Run after a third-party updater.** The refresh now runs after `dietpi-update`, which may have changed apt configuration. DietPi is already trusted to run as root behind the EXTRA-UPD-001 gates (source owner `MichaIng`, trust walk, isolation checks). `apt-get update` only downloads index files and verifies them against apt's own keyring and `signed-by` rules. It installs nothing. Any source DietPi added would be fetched by the next scheduled or manual refresh anyway. The attack surface does not grow.
- **Repeated rewrites cannot amplify work.** A process that keeps touching `preferences.d` causes at most one refresh per run (FR-009; one call site), at most 5 minutes long. The fingerprint walk stays capped at 1,000 entries (`aptSourcesBudget`, unchanged).
- **Untrusted text into the summary.** The note reason is `Runner` error text, cut to 200 bytes with `boundedText` (valid UTF-8). That is the same treatment as in v1. The server reads at most 2,048 bytes of the result and matches with linear-time regexps (DP-SEC-007), so nothing changes there.
- **Interruption semantics.** No package-manager work after any interruption (BR-004, AD-004). The refresh cannot flip a run to `interrupted` (outcome fields are not written; AD-003 keeps it inside `runCtx`).

## 11. Performance Considerations

- **Time bound (NFR-002):** at most one extra refresh per run, at most `PostRefreshBudget` (5 min) plus `TerminationGrace` (90 s) before the runner returns. It starts only with at least 7 min of the 80-min extended budget left on both clocks, so a v2 run still ends inside `ExtendedWorkerBudget` and the 85-min service backstop. Typical cost on PiCUPS: one `apt-get update` (seconds), only when the fingerprint changed (every DietPi step that ran, given `dietpi-raspberrypi`).
- **Output bound (NFR-001):** the refresh output goes through `appendBounded` → whole-run output ≤ `MaxOutputBytes` (256 KiB) with the existing truncation marker. Summary: note ≤ 240 bytes (`"; "` + 38-byte prefix + 200-byte reason); summary ≤ 4,096 bytes via the existing cap. Apt failure line ≤ 1,740 bytes.
- **Growth:** no new file, field, table, retained record type, log stream or index. One extra `stat` walk (≤ 1,000 entries) per v2 run, the same count as today. The second walk moves from the apt part to after the updater step.
- No caching or indexing changes.

## 12. Implementation Notes for Engineers

1. **Write AC-002's characterization test first.** Create `worker_postrefresh_v1_test.go` against the **unmodified** code, with `fakeClock` from `extra_helpers_test.go`, `sourcesSequence("a","b")` and a succeeding `TouchLists`. Capture the JSON bytes and phase list, and commit it as `DIETPI-REFRESH-001 test: pin v1 post-run refresh record` **before** any production change. Never regenerate the bytes afterwards. `golden_v1_test.go` must not be touched.
2. **Keep v1 call order exactly the same.** `fingerprintSources()` stays immediately before `step("refresh")`. The v1 branch calls `postRunRefresh(ctx, rec, st.sourcesBefore, st.sourcesBeforeErr)` at the same point as today, before the defer. Do not move the v1 call into `Execute` or after the holds defer.
3. **Set `listsRefreshed` only on the success path** of `step("refresh")`. A persistence failure of the `refresh` phase means the step never ran, so the flag stays false.
4. **Use `runCtx`, not `aptCtx`,** for the v2 refresh and its real-deadline check. Using `aptCtx` would skip or kill every v2 refresh once the 30-min apt context has passed. Using `svc` would drop the 80-min bound.
5. **Do not route the refresh through `step()`.** `step()` writes `ExitCode` and `Interrupted`. `runPostRefresh` must keep using `Runner.Run` directly (PATCH-FLOW-001 AD-007 rationale).
6. **The "pre-story" oracle in tests.** For AC-006 and AC-012/013/015, comparing against a rig with `Sources = nil` is a valid pre-story oracle: both the old and the new code skip every post-run step when `Sources` is nil, and every other path is identical. Use `fakeClock` on both rigs so the timestamps match. Compare whole JSON (AC-006) or `UpdaterResult` with `reflect.DeepEqual` (AC-012/013/015).
7. **Clock-driven budget tests** (AC-018/019): advance `g.clock` inside `fakeStep.onCall`, never before the step. `runExtra` check 7 (`ExtraStartThreshold`) runs before the step and must still pass.
8. **Fingerprint fakes:** keep `Sources` deterministic. In v2, `Sources` is called exactly twice when eligible (before the own refresh, after the updater step) and once when ineligible after a successful refresh (the baseline only). Tests that use `sourcesSequence` must account for that. Prefer `cfgState`, which returns a value derived from a counter that hooks bump, so call counts do not matter.
9. **AC-003 uses the real fingerprint** (`AptSourceEntries(g.root, 1000)` + `AptSourcesFingerprint`) over the rig's temp root, with `os.Chtimes` to a time one hour later. It works on Windows and Linux. No real apt, dpkg or dietpi-update is ever started. Every runner, step, lock, unit, trust, store and clock is a fake (NFR-006, SC-001 tripwire still in place).
10. **No lock wait is added** to the post-run refresh. If DietPi left the lists lock held, `apt-get update` fails, and that is recorded as a `failed:` note. Adding `DPkg::Lock::Timeout` would change the argv (FR-031).
11. Test naming must carry the AC ID in the test function name and in every `t.Run` / `t.Errorf` message (e.g. `"[AC-012] …"`), following the `TestExtras_EU_AC0xx_` convention.
12. Run `go test ./internal/maintenance/... ./internal/server/handlers/... ./internal/agent/...` on Windows (no `-race`), and rely on CI for `-race` and the Linux-only files (`lifecycle_linux_test.go`, `*_linux_test.go`).
13. Commits: `DIETPI-REFRESH-001 test: …` (characterization first), then `DIETPI-REFRESH-001 refactor: extract shared post-run refresh helpers` (v1 green, golden green), then `DIETPI-REFRESH-001 feat: post-run refresh after the extra-updater step for v2 runs`, then `DIETPI-REFRESH-001 test: …` for the new v2 and server tests.

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented. No file outside Section 4 changed by engineering.
- [ ] All 27 AC mappings in Section 8 have the named tests, with AC IDs in names and messages.
- [ ] `internal/maintenance/golden_v1_test.go` is byte-for-byte unchanged (`git diff --exit-code` on it) and passes.
- [ ] `worker_postrefresh_v1_test.go` was committed before the first production change and passes unchanged afterwards.
- [ ] The only modified existing test is `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore`, changed exactly as in Section 4.
- [ ] `go test ./...` is green on Windows. CI (`make test`, `-race` on Linux) is green.
- [ ] `go vet ./...` and the existing lint pass with no new findings.
- [ ] No migration (none required). No change under `internal/agent`, `internal/server` (non-test), `web/`, `scripts/`, `cmd/`.
- [ ] Grep confirms a single call site each for `postRunRefresh` (v1, in `executeApt`) and `postRunRefreshV2` (in `executeExtras`), and a single `Runner.Run` call for the post-run refresh (in `runPostRefresh`).
- [ ] Implementation report states the bounds (≤ 1 refresh/run, ≤ 5 min + 90 s, output ≤ 256 KiB, note ≤ 240 B, summary ≤ 4,096 B) and the D-005 deploy step: Ryan re-runs `install.sh` as root on PiCUPS (and PiVPN once opted in), then performs the D-006 check.

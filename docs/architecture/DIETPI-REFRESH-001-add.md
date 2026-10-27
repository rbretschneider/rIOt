# Architecture Decision Document

- **Story ID:** DIETPI-REFRESH-001
- **FRD Reference:** `docs/requirements/DIETPI-REFRESH-001-frd.md`
- **Security review:** `docs/security/DIETPI-REFRESH-001-security-review.md` (APPROVED WITH MITIGATIONS; M-1..M-4 and C-1..C-4 are incorporated below)
- **Amends:** PATCH-FLOW-001 ADD AD-007 (version 2 runs only); EXTRA-UPD-001 FR-025 / AC-020 ordering clause "post-run refresh" (see AD-006)
- **Author:** Architect Agent
- **Date:** 2026-10-27
- **Revision:** 2
- **Status:** FINAL

## Change log

| Rev | Date | Change |
|---|---|---|
| 1 | 2026-10-27 | Initial ADD. |
| 2 | 2026-10-27 | Security review mitigations. **M-1 (DR-SEC-001):** AD-003 rewritten. The 80-minute basis applies only when `runExtra` check 6 verified the unit backstop in this run; otherwise the basis is `WorkerBudget` via `aptCtx`. `extraRun` gains `backstopVerified`, and `postRunRefreshV2`'s signature changes (§6). §11 states both bounds. Three new tests: SEC001a/b/c. **M-2 (DR-SEC-002):** §2.2, AD-005 and §10 now state that the note may span lines and why classification is unaffected; the AC-023 server test gains multi-line note cases. **M-3 (DR-SEC-003):** direct `summaryV2` tests for the interrupted and error arms. **M-4 (DR-SEC-004):** a test for a failed save of the `refresh` phase. QA conditions C-1..C-4 recorded in §8.1. Fixed the "A1-007" typo in §2.2. |

---

## 1. Summary

For a version 2 (extra-updater) run, the helper stops making its post-run apt configuration check at the end of the apt part. Instead, `executeExtras` makes it once, after every present updater's step has ended and before `finalizeV2` builds the record. It uses the baseline fingerprint the apt part took just before its own refresh.

The refresh mechanics (phase persist, `Runner.Run` with a 5-minute sub-context, `[post_refresh]` output, list stamp, note) move into one helper. The unchanged version 1 path and the new version 2 path share it. Version 1 runs keep the exact same call order and bytes.

Version 2 runs gain two things:
- **Eligibility rules:** the run's own refresh succeeded, nothing was interrupted, and the service is not stopping.
- **A budget check on both the injected clock and the real context deadline.** The time basis is the 80-minute extended budget only when this run verified the unit's backstop root-side (`runExtra` check 6). Otherwise it is the 30-minute `WorkerBudget`, which is the pre-story bound.

## 2. Technical Context

### 2.1 Current state (code read on branch `fix/dietpi-post-refresh`)

| Location | What it does today |
|---|---|
| `internal/maintenance/worker.go` `Execute` | Builds `runCtx` (80 min for v2, 30 min for v1) and, for v2, `aptCtx` under it (30 min, real deadline = start + `WorkerBudget`). Runs `executeApt(aptCtx, …)`. v1 then calls `finalizeV1`; v2 calls `executeExtras(ctx /*svc*/, runCtx, aptCtx, …, started)`. |
| `worker.go` `executeApt` | Recovery. Then `sourcesBefore, sourcesBeforeErr := w.fingerprintSources()` immediately before the `refresh` step, then `stampLists`, `upgrade`, `verify`. Then `st.postRefreshNote = w.postRunRefresh(ctx, rec, sourcesBefore, sourcesBeforeErr)`, reached only when upgrade and verify succeeded. The deferred cleanup restores holds and reads version changes **after** the post-run refresh. The baseline values are locals, and nothing records whether the own refresh succeeded. |
| `worker.go` `postRunRefresh` | 1. `Sources == nil` → `""`. 2. Take the second fingerprint; equal and error-free → `""`. 3. Real-clock `time.Until(ctx.Deadline()) < postRefreshMinRemaining` (7 min) → budget note. 4. Persist phase `post_refresh` (failure → note). 5. `Runner.Run(WithTimeout(ctx, PostRefreshBudget), "/usr/bin/apt-get", aptRefreshArgs()...)`. 6. Append `"\n[post_refresh]\n"+out` via `appendBounded`. 7. Error → `failed:` note with reason `boundedText(err, 200)`; success → `stampLists`. It never touches `ExitCode` or `Interrupted`. |
| `internal/maintenance/extra.go` `executeExtras` | Sets `rec.AptOutcome`. Computes `deadline := started.Add(ExtendedWorkerBudget)` on the injected clock. Runs `runExtra` per present updater (flag reconciliation and post-step detection happen inside `runExtra`), upserts the results, and calls `finalizeV2`. |
| `extra.go` `runExtra` check 6 | `env.Unit()` read. An error, `PrivateTmp != "no"`, a non-empty `MountFlags`, or `RuntimeMax < ExtraMinBackstop` refuses the updater. This is the only root-side verification that the unit really has the extended backstop (EXTRA-UPD-001 SEC-002). Checks 1–5 (apt not successful, not enabled, status unknown or nothing pending, source, trust) return before it. |
| `extra.go` `finalizeV2` / `summaryV2` | Outcome comes from `AptOutcome`, the updater outcomes and `aptErr`. Summary line 2 is the "apt result line": success → `SuccessSummary`, plus `"; "+st.postRefreshNote` if set; interrupted → the interruption text; error → `boundedText(aptErr.Error(), 1500)`, with no note today. The whole summary is cut with `boundedText(…, 4096)`. |
| `internal/maintenance/runner_linux.go` `runProcessSpec` | Builds its error with `errors.Join`, so a refresh-failure reason can contain `\n`, for example `package process failed: context deadline exceeded\nsignal: terminated` (DR-SEC-002). |
| `internal/maintenance/protocol.go` | `WorkerBudget = 30m`, `ExtendedWorkerBudget = 80m`, `ExtraMinBackstop = 83m30s`, `ExtraStartThreshold = 47m30s` (pinned by `TestBounds_EU_AC025_WorkerSide`), `TerminationGrace = 90s`, `MaxOutputBytes = 256 KiB`, `SuccessSummary`. `Record.Validate` imposes no phase ordering. `post_refresh` after `extra_run:<id>` validates as long as no updater entry is `running`, which holds because entries are final when `runExtra` returns. |
| `internal/maintenance/aptsources.go` | `AptSourceEntries` covers `sources.list`, `sources.list.d/*.list` and `*.sources`, and `preferences.d/<no-dot>` and `*.pref`. `dietpi-raspberrypi` is included. Unchanged. |
| `cmd/riot-maintenance/main_linux.go` | Wires `Sources` and `TouchLists` for every run. Unchanged. |
| `internal/agent/extra_updaters.go` `extraProgressStep` | Maps every phase other than `extra_run:<id>` to `StepVerify`, including `post_refresh`. Unchanged (AD-007). |

### 2.2 D-004: server classification (answered)

The only server code that reads `SuccessSummary` is `internal/server/handlers/patch_badge_refusals.go`.

- **Apt-failed runs.** `parseRefusals` first requires line 1 to match `refusalFirstLine` (`^apt: succeeded(…)$`). A run whose apt part failed has line 1 `apt: failed; …`, so it is dropped before any other line is read. In a v2 run with no present updater, line 1 is the apt text itself and also fails the regexp.
- **Apt-succeeded runs.** `successLine` accepts line 2 when it equals `maintenance.SuccessSummary` **or** starts with `SuccessSummary + "; "`. A note appended with `"; "` keeps that true. An existing test pins this (`[DP-SEC-004] the apt note after the success summary is allowed`).
- **The note can span lines (rev 2, DR-SEC-002).** The note *starts* on line 2, but its reason is runner error text built with `errors.Join`, so it can carry `\n` and push continuation lines onto line 3 and beyond. That cannot change classification:
  - the reason contains no command output, only fixed literals, Go exec/wait text and context errors, so no continuation line can begin with `<registry-id>: ` or `<id>: skipped (<reason>); `;
  - apt-failed runs are dropped at line 1;
  - for an apt-succeeded run with status `succeeded`, only `refusalDetail` reads later lines, and it matches only the exact `<id>: skipped (<reason>); ` prefix;
  - for an apt-succeeded run with status `failed`, an extra line can only make `onlyDetailLines` false, which keeps the run red (the conservative direction);
  - the badge's `runMessage` collapses newlines.
  The same multi-line behaviour already exists for v1 and for the v2 success arm.
- **Confirmed:** classification is gated on line 1 and prefix-based on line 2. An appended note, including a multi-line one, never makes a run less alarming, so FRD assumption A-007 holds. No other Go or web code parses the summary: the agent passes `rec.Summary` through verbatim as `commandExecResult.Message`. AC-023 pins this with a server-side test that includes multi-line notes.

### 2.3 FRD review notes (none blocking)

1. **Ordering conflict with EXTRA-UPD-001.** EXTRA-UPD-001 FR-025 / AC-020 says each updater runs after "upgrade, verification, post-run refresh". For v2 runs this story's FR-002/FR-003 supersede that clause. NFR-005 already permits changing the one test that asserts the old order. Recorded in AD-006; the technical writer updates `docs/ops/os-maintenance.md`.
2. **Which clock FR-015 means.** "Remaining extended budget measured from the run's start" can mean the injected clock or the real context deadline. AD-003 checks both.
3. **FR-015 tightened by DR-SEC-001.** The extended basis is used only when the backstop was verified in this run. Otherwise the 30-minute basis applies, with the same FR-016 note. The FRD's intent is unchanged: the security review confirms that AC-003..AC-027 stay valid as written.
4. **Testing FR-014's "service told to stop".** It cannot be isolated end-to-end, because every way to cancel the service context during a run also marks the apt part or an updater as interrupted. It is tested by calling the v2 method directly (AC-017, second test).

## 3. Architecture Decisions

### AD-001: Move the v2 check into `executeExtras`, between the updater loop and `finalizeV2`

**Decision.**
- `executeApt` still takes the baseline fingerprint immediately before the `refresh` step (FR-005). It now stores the baseline in `aptState`, together with a flag that is set only when the `refresh` step returned without error.
- For a version 1 request (`req.Version == ProtocolVersion`, a positive test), `executeApt` still calls `postRunRefresh` at exactly the same point with the same arguments.
- For a version 2 request, `executeApt` makes no post-run check (FR-002).
- `executeExtras` calls a new `postRunRefreshV2` once, after the `for … Registry()` loop has stored the last result and before `finalizeV2` (FR-003, FR-004). This happens even when no updater is present or when the loop broke on `FS == nil`. The returned note goes into `st.postRefreshNote`, which `summaryV2` already reads.

**Rationale.**
- At that point every `runExtra` call has fully returned, including `ReconcileFlags` and the post-step `Detect` (FR-003).
- The holds are already restored, because `executeApt`'s defer ran before `executeExtras`.
- The record has not been finished yet.
- `aptState` already exists to carry state from the apt part to the finaliser.

**Alternatives considered.**
- *A second check at the end of `executeExtras`, keeping the apt-part check.* Rejected: it allows two refreshes (FR-009, AC-004).
- *The check inside `finalizeV2`.* Rejected: `finalizeV2` sets `Phase = "finished"` and `FinishedAt` first, and mixing refresh I/O into the outcome computation blurs FR-019..FR-021.
- *A check after each updater.* Rejected: it allows N refreshes, against FR-003.

**Consequences.**
- In a v2 record, `post_refresh` now follows `extra_wait:<id>` and `extra_run:<id>`, and the `[post_refresh]` output follows `[extra:<id>]` (NFR-004).
- A worker killed during the v2 post-run refresh is recovered by the existing `RecoverInterrupted` + `markExtraInterrupted` path (FRD §9). AD-003 rev 2 makes sure the refresh itself cannot be the cause of such a kill.

### AD-002: One shared refresh mechanic; v1 composition and call order unchanged

**Decision.** Split today's `postRunRefresh` body into two private helpers. `postRunRefresh` (v1) is rebuilt from them in the identical order; `postRunRefreshV2` is built from the same helpers.

| Helper | Responsibility (moved verbatim from today's `postRunRefresh`) |
|---|---|
| `sourcesChanged(before, beforeErr)` | `Sources == nil` → false. Otherwise call `Sources()` once and return `beforeErr != nil \|\| afterErr != nil \|\| before != after` (FR-006, FR-007). |
| `runPostRefresh(ctx, rec)` | 1. Persist phase `post_refresh`; a failure returns the `skipped: state not persisted` note. 2. `WithTimeout(ctx, PostRefreshBudget)`. 3. `Runner.Run` with `"/usr/bin/apt-get", aptRefreshArgs()...`. 4. `appendBounded` of `"\n[post_refresh]\n"+out.Output` with `out.Truncated`. 5. Error → `failed: `+`boundedText(err.Error(), 200)`. Success → `stampLists(rec)` and `""`. It never writes `ExitCode`, `Interrupted`, `AptOutcome` or any `ExtraUpdaters` field. |

`postRunRefresh` (v1) = `sourcesChanged` → real-clock budget check on its `ctx` (unchanged text and threshold) → `runPostRefresh`.

**Rationale.**
- FR-031 (same argv) and FR-022..FR-025 (same notes, label and stamp) hold by construction.
- The v1 sequence of observable effects is unchanged: `Sources` call, deadline read, `Save`, `Run`, append, stamp.

**Alternatives considered.**
- *Duplicate the body for v2.* Rejected: two copies would drift.
- *Add a version parameter to `postRunRefresh`.* Rejected: it would put v2 branches into the path that `golden_v1_test.go` protects.

**Consequences.** `postRunRefresh` keeps its signature, so no existing test changes. Engineers must not alter the strings, `aptRefreshArgs`, or the `appendBounded` call while moving them.

### AD-003 (rev 2): v2 time basis depends on root-side backstop verification; both clocks checked

**Decision.**

1. **Verification flag.** `extraRun` gains `backstopVerified bool`. `runExtra` sets `run.backstopVerified = true` at one point only: immediately after the last condition of check 6 (`unit.RuntimeMax >= ExtraMinBackstop`) passes, before check 7.
   - Because the flag is set on `run`, every later return carries it: check 7 onwards, including the `skip` and `fail` closures and the step result.
   - Every return before that point leaves it false: checks 1–5, a `Unit` read error, a `PrivateTmp` or `MountFlags` refusal, or a backstop that is too short.
   - `executeExtras` computes `verified` as true when any returned `extraRun` has `backstopVerified`. The unit is the same for every updater.
2. **Basis selection** in `executeExtras`, used only for the post-run refresh:
   - `verified` → basis context `runCtx`, basis budget `ExtendedWorkerBudget` (80 min).
   - not `verified` → basis context `aptCtx`, basis budget `WorkerBudget` (30 min). `aptCtx` is the existing child of `runCtx`, created in `Execute` with a real deadline of run start + `WorkerBudget`, so it is bounded by both.
3. **Budget check.** `postRunRefreshV2` skips with `post-run package list refresh skipped: worker time budget nearly exhausted` when **either** of these holds:
   - injected clock: `started.Add(basisBudget).Sub(w.now()) < postRefreshMinRemaining`;
   - real clock: the basis context has a deadline and `time.Until(deadline) < postRefreshMinRemaining`.
   Exactly 7 minutes remaining is enough; the comparison is strictly `<` (AC-019).
4. **Refresh context.** The refresh sub-context is `WithTimeout(basisCtx, PostRefreshBudget)`. Its deadline is therefore never later than the basis deadline or `runCtx`'s deadline. It is never derived from `svc` or `context.Background()`.

**Rationale.**
- **DR-SEC-001.** EXTRA-UPD-001 SEC-002 forbids relying on the 80-minute budget unless the worker itself has verified, root-side and in this run, an effective backstop of at least `ExtraMinBackstop`. The agent's v2 dispatch cannot be trusted for that. Without the verification, a host whose effective backstop is 35 minutes could start a refresh at about minute 29. systemd would then kill the run mid-refresh, and the next start would turn a clean run into `interrupted`.
- **Why reuse check 6 instead of a fresh `Unit()` read.**
  - It adds no I/O and no code path.
  - It keeps NFR-003 trivially true.
  - It ties the extended basis to exactly the runs in which the extended time was actually used. Only a run that passed check 6 can have spent time in the lock wait or the step, and those are the only runs that need more than 30 minutes before the refresh.
  - A run that never reached check 6 (no updater present, or skipped at checks 1–5) used no extended time, so the pre-story 30-minute basis costs it nothing. This is also what the review's test (c) requires.
- **Why both clocks.** The injected clock makes the boundaries testable to the second. The real deadline is what actually kills the process, so it alone guarantees NFR-002. In production the two clocks are the same wall clock.

**Alternatives considered.**
- *A fresh `Unit()` read in `postRunRefreshV2`, with an error counting as unverified.* Rejected:
  - it adds a `systemctl show` per v2 run;
  - it would grant the 80-minute basis to runs that spent no extended time;
  - it contradicts the review's test (c) expectation that `nothing_pending` behaves like an unverified host.
- *Always use the 30-minute basis.* Rejected: a typical DietPi run that ends after minute 23 of the run would always skip the refresh. That defeats the story for PiCUPS-type runs with a long apt part.
- *Always use the 80-minute basis (rev 1).* Rejected by DR-SEC-001.
- *Real clock only, or injected clock only.* Rejected, as in rev 1: the boundary would not be testable, or the kill deadline would not be guaranteed.

**Consequences.**
- `postRunRefreshV2` takes the basis context and basis budget from `executeExtras` (§6).
- AC-018, AC-019 and the AC-020 main case use a DietPi step that passes check 6 with the 85-minute fake unit, so they exercise the verified basis unchanged.
- AC-009, AC-012 and AC-023 (no updater present, or `apt_not_successful`) now exercise the 30-minute basis. Their fake clock is not advanced, so they still refresh.

### AD-004: v2 eligibility rules, evaluated before any fingerprint read

**Decision.** `postRunRefreshV2` returns `""` (no refresh, no note, and `Sources` not called), checking in this order:
1. `st.listsRefreshed` is false (FR-010, FR-011, BR-005). This happens when:
   - the own refresh never ran (recovery or verification failure before it, or a failed save of the `refresh` phase);
   - the own refresh failed.
2. `rec.AptOutcome == aptOutcomeInterrupt` (FR-014).
3. Any `rec.ExtraUpdaters[i].Outcome == UpdaterInterrupted` (FR-014).
4. `svc.Err() != nil`: the service is stopping (FR-014, BR-004).

Then: `sourcesChanged(st.sourcesBefore, st.sourcesBeforeErr)` false → `""` (FR-030). Then the AD-003 budget check. Then `runPostRefresh(basisCtx, rec)`.

Nothing else is consulted:
- An apt outcome of `error` after a successful own refresh stays eligible (FR-012). Examples: upgrade or verification failure, hold restoration failure, `verify package versions` failure, or a persistence failure in a later phase.
- Every updater outcome other than interrupted stays eligible: `success`, `failed`, `timed_out`, and `skipped` for any reason (FR-013, BR-003).

**Rationale.**
- These are exactly the FRD's ineligibility conditions.
- Checking them before the fingerprint read means an interrupted run does no further I/O (BR-004).

**Consequences.**
- A v2 run whose upgrade fails after the lists were refreshed now refreshes when the configuration changed (FR-012). v1 does not (OQ-001).
- A refresh cut short mid-flight by a service stop behaves as in v1: a `failed: …` note, with the outcome unchanged.
- In the narrow window between rule 4 and `Runner.Run`, `runProcessSpec` checks `ctx.Err()` first. The result is a `failed: … context canceled` note and no process starts. This residual is accepted (review DR-SEC-006).

### AD-005: Note on the apt result line for apt failures (v2 only)

**Decision.** `summaryV2` appends `"; " + st.postRefreshNote` to the apt result line in two arms:
- **success** (unchanged);
- **error**, after `boundedText(aptErr.Error(), 1500)`.

It never appends it in the **interrupted** arm. AD-004 guarantees there is no note then, but the switch arm must not append it either; that is the second safeguard, pinned by a direct test (M-3).

The note is bounded to `"post-run package list refresh failed: "` (38 bytes) plus a 200-byte reason. The reason is the runner's error text and **may contain line breaks** (`errors.Join`, DR-SEC-002). It is not reformatted, so v1 bytes stay unchanged (FR-001) and v1 and v2 keep one formatter. §2.2 explains why line breaks cannot change server classification. The whole summary stays under the existing `boundedText(…, 4096)`.

**Rationale.** FR-026..FR-029 and BR-006 (§2.2).

**Alternatives considered.** *Flatten the note to a single line for v2 only.* Rejected:
- there is no consumer that needs it (§2.2);
- it would split the shared helper's output between versions.
The review lists this option as optional.

**Consequences.**
- The apt result line grows by at most 240 bytes, so an apt failure line is at most 1,740 bytes.
- Under the 4,096-byte cap, the later lines can lose up to 240 bytes. That truncation already exists and stays bounded.
- A future parser must not assume that line 3 is the first updater detail line. `docs/ops/os-maintenance.md` will say so (technical writer).

### AD-006: OQ-001 and OQ-002 decided; EXTRA-UPD-001 ordering clause amended

**Decision.**
- **OQ-001: no.** Version 1 does not gain a refresh after a failed upgrade. Ryan's byte-identical constraint and `TestWorker_PF_AC033_FailedUpgradeNoExtraRefresh` rule it out. It can be a separate story.
- **OQ-002: no.** `ExtraStartThreshold` stays at `47m30s`.
  - `TestBounds_EU_AC025_WorkerSide` pins it to step limit + grace + lock wait + 1 minute.
  - In the worst case 1 minute is left, so the refresh is skipped with the FR-016 note. That is visible and accepted (A-005).
  - A typical PiCUPS run leaves more than 70 minutes.
  - Raising the threshold would turn slow apt parts into `insufficient_time` refusals.
- **EXTRA-UPD-001 FR-025 / AC-020.** For v2 runs, "post-run refresh" is removed from the steps that come before the updater. `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore` changes as specified in §4 (NFR-005).

### AD-007: No agent, server, web, installer or schema change

**Decision.** The fix lives only in `internal/maintenance` (the `riot-maintenance` binary). The record format, the request format, `ProtocolVersion`, `RequestVersionExtra`, `Record.Validate` and `parseRecord` are unchanged.

While a v2 record is in `post_refresh` after the updater step, the agent shows `StepVerify`. Its `extra_updater_*` events have already been sent by then. No agent change is made (FR-033).

**Consequences.** The change reaches a host only through a root `install.sh` re-run (D-005). Older helpers keep today's behaviour (NFR-004).

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `internal/maintenance/worker.go` | (a) `aptState` gains `listsRefreshed bool`, `sourcesBefore string` and `sourcesBeforeErr error`. (b) `executeApt` stores the baseline in `st` immediately before the `refresh` step, in the same call position as today. It sets `st.listsRefreshed = true` only after `step("refresh", …)` returns nil, before `stampLists`. It calls `postRunRefresh` only when `req.Version == ProtocolVersion` (a positive v1 test, C-2). (c) Extract `sourcesChanged` and `runPostRefresh` from `postRunRefresh` (AD-002). `postRunRefresh` keeps its signature and behaviour; its doc comment now says "version 1". (d) Add `postRunRefreshV2` (AD-003 rev 2, AD-004) beside `postRunRefresh`, with a doc comment citing DIETPI-REFRESH-001 FR-002..FR-018 and DR-SEC-001. (e) The `executeApt` doc comment says "and, for version 1, the post-run refresh". |
| MODIFY | `internal/maintenance/extra.go` | (a) `extraRun` gains `backstopVerified bool`. (b) `runExtra` sets `run.backstopVerified = true` immediately after check 6's `RuntimeMax >= ExtraMinBackstop` condition passes (AD-003 rev 2 step 1). (c) `executeExtras`, after the updater loop and before `finalizeV2`: compute `verified` over `runs`, pick `(basisCtx, basisBudget)` = `(runCtx, ExtendedWorkerBudget)` or `(aptCtx, WorkerBudget)`, and set `st.postRefreshNote = w.postRunRefreshV2(svc, basisCtx, basisBudget, rec, st, started)`. (d) In `summaryV2`, the error arm appends `"; "+st.postRefreshNote` when non-empty (AD-005); the interrupted arm does not. (e) Update the file-header and `executeExtras` comments: the single post-run check follows the updater step, with the time basis per AD-003. |
| MODIFY | `internal/maintenance/extra_test.go` | Only `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore` changes (NFR-005). (1) Remove `"post-run refresh"` from the "comes before DietPi" map. (2) Replace subtest `[AC-020] DietPi is the last thing that ran` with `[AC-020] after the DietPi call only the post-run refresh runs (DIETPI-REFRESH-001)`, which asserts that `g.log[step+1:]` is exactly `["apt:"+refreshKey]`. (3) `[AC-020] the post-run refresh really ran in this scenario` now asserts `postRefresh > step`. Nothing else in the file changes. |
| CREATE | `internal/maintenance/extra_postrefresh_test.go` | All v2 tests in §8 except the server test and the v1 characterization test. Test-only helpers, all in this file:<br>• `cfgState`: a mutable fingerprint source with `bump()`, `fail(err)`, a `calls` counter, and a `sources()` method usable as `Worker.Sources`.<br>• `postRefreshRunner`: wraps `*orderRunner` and counts calls whose key is `refreshKey`. For the N-th refresh call it can return configured output, an error or `Interrupted: true`. It records `ctx.Deadline()` and `time.Now()`, and can return `Interrupted: true` with an error for the upgrade key.<br>• `loggingTouch`: appends `"stamp"` to `g.log`, with an optional error on the N-th call.<br>• `countingUnit(g)`: wraps `g.w.Extras.Unit` with a call counter.<br>• `oracleRig(t, setup)`: builds the same scenario with `Sources = nil`.<br>It reuses `newExtraRig`, `fakeClock`, `validatingStore`, `fakeStep`, `orderRunner`, `sourcesSequence`, `sourcesWithErrors`, `writeFixture`, `firstLine` and `runWithFixedStatus`. |
| CREATE | `internal/maintenance/worker_postrefresh_v1_test.go` | AC-002 characterization: pinned JSON bytes, phase sequence and call list for a v1 `full` run with a fingerprint change, both plain and with `IncludeRebootClass`. **Write it and commit it against the unmodified code first** (§12, C-2). |
| CREATE | `internal/server/handlers/patch_badge_postrefresh_test.go` | The server half of AC-023, including the multi-line note cases (M-2). Test only; no server code changes. |
| MODIFY (technical writer) | `docs/ops/os-maintenance.md` | Section "Post-run package list refresh": v2 timing; the 80-minute basis only with a verified backstop, otherwise 30 minutes; a refresh after an apt failure that followed a successful refresh (v2 only); no refresh after an interruption; the note may span lines. Section "What the step does": the line "It runs after apt: …, the post-run refresh and …" changes so the post-run refresh follows the updater step. |
| MODIFY (technical writer) | `CHANGELOG.md` | Story entry, including the D-005 `install.sh` re-run. |
| UNCHANGED (must still pass) | `internal/maintenance/golden_v1_test.go`, `worker_test.go`, `worker_qa_test.go`, `extra_dp_test.go`, `extra_qa_test.go`, `qa_dietpi_test.go`, `protocol_extra_test.go`, `lifecycle_linux_test.go`, all `internal/agent` and `internal/server` tests | Regression oracles (NFR-005). |
| UNCHANGED | `internal/maintenance/aptsources.go`, `protocol.go`, `runner_linux.go`, `cmd/riot-maintenance/main_linux.go`, `scripts/install.sh`, `scripts/riot-maintenance.service`, sudoers, the drop-in, all of `internal/agent`, `internal/server` (non-test), `web/`, `cmd/riot-server/migrations/` | FR-032, FR-033, NFR-003, C-1. |

## 5. Data Model Changes

There are no database changes. **Record JSON (`maintenance.Record`):** no field is added, removed or retyped. **Request:** no change.

**In-memory only:**

| Struct.Field | Before | After |
|---|---|---|
| `aptState.postRefreshNote string` | present | unchanged. Set by v1 `executeApt`, or by `executeExtras` for v2. |
| `aptState.listsRefreshed bool` | — | true only after the run's own `refresh` step returned nil |
| `aptState.sourcesBefore string` | a local in `executeApt` | stored here, taken at the same moment as today |
| `aptState.sourcesBeforeErr error` | a local in `executeApt` | stored here |
| `extraRun.backstopVerified bool` | — | true only when this updater's check 6 passed completely in this run |

**Record content changes (v2 only):**

| Item | Before | After |
|---|---|---|
| Phase sequence: change detected, apt success, DietPi ran, holds released | `… refresh, upgrade, audit, check, post_refresh, hold_restore, extra_wait:dietpi, extra_run:dietpi, finished` | `… refresh, upgrade, audit, check, hold_restore, extra_wait:dietpi, extra_run:dietpi, post_refresh, finished` |
| Phase sequence: change only from DietPi | no `post_refresh` | `… extra_run:dietpi, post_refresh, finished` |
| Output | `[post_refresh]` before `[extra:dietpi]` | `[post_refresh]` after `[extra:dietpi]` |
| Summary line 2: apt error after a successful refresh, with a note | apt failure text only | `boundedText(aptErr,1500) + "; " + note`. The note may continue on following lines. |
| No change detected | — | byte-identical to today (FR-030) |

## 6. API / Interface Contract

There is no HTTP, WebSocket or agent protocol change. The internal Go contract (package `maintenance`, all unexported):

| Symbol | Signature | Contract |
|---|---|---|
| `(*Worker).sourcesChanged` | `(before string, beforeErr error) bool` | Calls `Sources` at most once. With a nil `Sources` it returns false and calls nothing. |
| `(*Worker).runPostRefresh` | `(ctx context.Context, rec *Record) string` | Returns `""` on success, or exactly one of `post-run package list refresh skipped: state not persisted` and `post-run package list refresh failed: <≤200 bytes, may contain \n>`. At most one `Runner.Run`, with argv `"/usr/bin/apt-get", aptRefreshArgs()...`, under a context whose deadline is at most `PostRefreshBudget` away and no later than `ctx`'s deadline. It changes only `rec.Phase`, `rec.UpdatedAt`, `rec.Output` and `rec.OutputTruncated`. |
| `(*Worker).postRunRefresh` | `(ctx context.Context, rec *Record, before string, beforeErr error) string` | **Unchanged** signature and behaviour. v1 only. |
| `(*Worker).postRunRefreshV2` (rev 2) | `(svc, basisCtx context.Context, basisBudget time.Duration, rec *Record, st *aptState, started time.Time) string` | In order: the AD-004 guards; `sourcesChanged`; the AD-003 budget check against `started+basisBudget` (injected clock) and `basisCtx`'s deadline (real clock), with note `post-run package list refresh skipped: worker time budget nearly exhausted`; then `runPostRefresh(basisCtx, rec)`. The caller passes exactly `(runCtx, ExtendedWorkerBudget)` when check 6 was verified in this run, else `(aptCtx, WorkerBudget)`. It never changes `Outcome`, `ExitCode`, `Interrupted`, `AptOutcome`, `ExtraUpdaters`, `HoldsReleased` or `VersionChanges`. |
| `extraRun.backstopVerified` | `bool` | Set only in `runExtra`, immediately after check 6 passes completely. |
| `summaryV2` line 2 (record `summary`) | — | Success: `SuccessSummary`, or `SuccessSummary + "; " + note`. Error: `boundedText(aptErr,1500)`, or that `+ "; " + note`. Interrupted: unchanged, never a note. A note may span lines. |

There are exactly three note strings (FRD §7). The success path adds none (FR-024).

## 7. Sequence / Flow

### 7.1 Version 2 run (`full`/`security`)

1. `Execute` validates, calls `Store.Begin`, and creates `runCtx` (80 min) and `aptCtx` (30 min, a child of `runCtx`).
2. `executeApt(aptCtx, …)`:
   1. Hold release (if reboot class); configure, audit and check; repair if needed.
   2. `st.sourcesBefore, st.sourcesBeforeErr = fingerprintSources()`.
   3. `step("refresh")`. On error (runner failure or phase persist failure), return; `listsRefreshed` stays false. On success, set `st.listsRefreshed = true`, then `stampLists`.
   4. Upgrade and verify; an error is returned.
   5. **v2: no post-run check.**
   6. The defer restores the holds and reads the version changes.
3. `executeExtras(svc, runCtx, aptCtx, …)`:
   1. `rec.AptOutcome = aptOutcome(…)`.
   2. For each present updater, `runExtra` runs the gates; when check 6 passes, `backstopVerified = true`. Then wait, step, reconcile flags, detect, and store the result.
   3. `verified` = any `run.backstopVerified`. Pick `(basisCtx, basisBudget)`.
   4. `st.postRefreshNote = postRunRefreshV2(svc, basisCtx, basisBudget, rec, st, started)`:
      1. Ineligible (AD-004 rules 1–4) → `""`.
      2. `sourcesChanged` is false → `""`.
      3. Too little budget on either clock (AD-003) → budget note.
      4. Otherwise `runPostRefresh(basisCtx, rec)`: persist `post_refresh`, `Runner.Run`, append `[post_refresh]`, then stamp or return a note.
   5. `finalizeV2`, unchanged except that `summaryV2` places the note (AD-005); then `Store.Finish`.

### 7.2 Version 1 run

The sequence is identical to today. In `executeApt` step 5 the v1 path calls `postRunRefresh(aptCtx, rec, st.sourcesBefore, st.sourcesBeforeErr)`, which runs `sourcesChanged`, then the real-clock 7-minute check on `aptCtx`, then `runPostRefresh(aptCtx, rec)`. After that the defer restores the holds and `finalizeV1` runs.

## 8. Acceptance Criteria Mapping

New v2 tests are named with the prefix `TestPostRefreshV2_DR_` and live in `internal/maintenance/extra_postrefresh_test.go` unless noted otherwise.

The "oracle rig" is the same scenario with `Sources = nil`. Pre-story and post-story code treat that rig identically, because neither makes a post-run check.

"Verified" and "unverified" refer to the AD-003 basis.

| AC / Mitigation | Fulfilled By | Test |
|---|---|---|
| AC-001 | AD-002 (the v1 call order is untouched) | `TestExecute_EU_AC044_V1Golden` in `golden_v1_test.go`, **unmodified**, passes. |
| AC-002 | AD-002; v1 branch of `executeApt` | `TestWorker_DR_AC002_V1PostRefreshRecordPinned` (`worker_postrefresh_v1_test.go`): pinned JSON; phases `…check, post_refresh, finished` and `…check, post_refresh, hold_restore, finished`; the refresh is call index 7. The existing `TestWorker_PF_AC032_*`, `_AC033_*`, `_AC034_*`, `_AC038_*` and `TestWorker_QA_AC032_*`, `_AC033_*` pass unmodified. |
| AC-003 | AD-001; `postRunRefreshV2` and the stamp in `runPostRefresh` (verified) | `…_AC003_DietPiRewritesPreferencesD`: real `AptSourceEntries` over the rig root with `etc/apt/preferences.d/dietpi-raspberrypi`; the step `onCall` runs `os.Chtimes` (same size). Asserts: 2 refresh calls, the second after `step:dietpi`; the last `"stamp"` comes after `step:dietpi`; outcome `success` with exit 0; line 2 is exactly `SuccessSummary`. |
| AC-004 | AD-001 | `…_AC004_AptChangeRefreshedAfterUpdater`: `cfgState.bump()` in `runner.after[upgradeKey]`. Asserts: no `refreshKey` call between the upgrade and `step:dietpi`; exactly one after it; `[post_refresh]` appears after `[extra:dietpi]` in the output. |
| AC-005 | AD-001 (single call site) | `…_AC005_AptAndUpdaterChangeRefreshOnce`: bump in the upgrade and in the step `onCall`. Asserts 2 refresh calls in total. |
| AC-006 | AD-004 (`sourcesChanged` false) | `…_AC006_NoChangeRecordIdenticalToOracle`: constant fingerprint. Asserts: no saved `post_refresh` phase; `json.Marshal(rec)` equals the oracle rig's bytes (both on `fakeClock`). |
| AC-007 | `sourcesChanged` (FR-007) | `…_AC007_AfterUpdaterFingerprintErrorRefreshes`: `sourcesWithErrors("same", nil, err)`. Asserts 2 refresh calls. |
| AC-008 | Baseline position (FR-005) | `…_AC008_ChangeDuringRecoveryIsBaseline`: configure fails, which forces repair; bump in the configure hook. Asserts 1 refresh call. |
| AC-009 | AD-001 (zero updaters, unverified basis) | `…_AC009_NoUpdaterPresentStillChecks`: remove `boot/dietpi/.version`; bump in the upgrade. Asserts: 2 refresh calls; the last saved phases are `post_refresh`, `finished`; `[post_refresh]` is the last output section. |
| AC-010 | AD-004 rule 1 | `…_AC010_FailureBeforeOwnRefresh`, with subtests (a) configure and repair fail and (b) verification fails after repair. Fingerprint source is `cfgState`. Asserts: 0 refresh calls; `cfgState.calls == 0` (C-4); no `post-run` in the summary. |
| AC-011 | AD-004 rule 1 (no retry) | `…_AC011_OwnRefreshFailsNoRetry`: `runner.failAt[refreshKey]`. Asserts 1 refresh call and no `post-run`. **M-4:** `…_AC011_RefreshPhaseNotPersisted`: `store.failPhase = "refresh"` with a fingerprint change. Asserts: 0 refresh calls; `cfgState.calls <= 1` (the baseline only); no `post-run` in the summary. |
| AC-012 | AD-004 (apt error stays eligible; unverified basis); outcome isolation | `…_AC012_UpgradeFailureStillRefreshes`: bump the fingerprint and fail the upgrade with exit 100. Asserts: 2 refresh calls, the second being the last apt call; outcome `error`; `AptOutcome` `error`; exit 100; `g.result(rec)` deep-equals the oracle rig's (`skipped`/`apt_not_successful`). |
| AC-013 | AD-004 (updater failure stays eligible) | `…_AC013_UpdaterFailedStillRefreshes`: the step `onCall` runs chtimes on a `preferences.d` file; `StepOutcome{ExitCode:1, Err}`. Asserts: 2 refresh calls, the last after `step:dietpi`; outcome `error` with exit 1; the DietPi result deep-equals the oracle's. |
| AC-014 | AD-004 (timed out stays eligible) | `…_AC014_UpdaterTimedOutStillRefreshes`: `step.block`, `StepLimit = 50ms`, bump in `onCall`. Asserts 2 refresh calls and DietPi outcome `timed_out`. |
| AC-015 | AD-004 (a refusal stays eligible; verified, since lock refusals come after check 6) | `…_AC015_UpdaterRefusedStillRefreshes`: lock always held, so the result is `dpkg_locked`; bump in the upgrade. Asserts: 2 refresh calls; DietPi `skipped`/`dpkg_locked` with the same note as the oracle; the run outcome equals the oracle's. |
| AC-016 | AD-004 rule 2; AD-005 interrupted arm | `…_AC016_AptInterruptedNoRefresh`: (a) `postRefreshRunner` returns `Interrupted:true` plus an error on the upgrade; (b) the service context is cancelled in the upgrade hook. Asserts 1 refresh call, `cfgState.calls <= 1`, and no `post-run`. **M-3:** `TestSummaryV2_DR_AC016_InterruptedArmNeverCarriesNote` calls `w.summaryV2` directly with `rec.AptOutcome = aptOutcomeInterrupt`, an `aptErr`, `st.postRefreshNote = "post-run package list refresh failed: x"`, and runs both empty and with one updater. Asserts the apt line starts with `Maintenance interrupted;` and the summary contains no `post-run`. |
| AC-017 | AD-004 rules 3 and 4 | `…_AC017_UpdaterInterruptedNoRefresh`: `step.block`, `onCall = cancel`. Asserts 1 refresh call, `cfgState.calls == 1` (the baseline only, C-4), and no `post-run`. `…_AC017_StoppedServiceSkipsSilently` calls `w.postRunRefreshV2` directly with a cancelled `svc`, `AptOutcome` success, no updater entries, `listsRefreshed = true` and a fingerprint that would differ. Asserts the result is `""`, 0 runner calls and `cfgState.calls == 0`. |
| AC-018 | AD-003 (verified basis) | `…_AC018_BudgetNearlyExhausted`: the step `onCall` advances `fakeClock` so 5 minutes of the 80-minute basis remain. Asserts: no refresh after `step:dietpi`; line 2 ends with `; post-run package list refresh skipped: worker time budget nearly exhausted`; outcome, exit code and `AptOutcome` equal those of the constant-fingerprint rig. Subtest `real deadline also guards`: service context `WithTimeout(Background, 6*time.Minute)`, injected clock not advanced; the same note is expected. |
| AC-019 | AD-003 (strict `<`, verified basis) | `…_AC019_BudgetBoundary`: `onCall` advances the clock by `ExtendedWorkerBudget - postRefreshMinRemaining`, and the refresh runs. Advancing one second more skips it with the FR-016 note. |
| AC-020 | AD-003 (sub-context from the basis) | `…_AC020_RefreshLimit` (verified; real clock, C-3): `postRefreshRunner` records the second refresh's deadline. Asserts: a deadline exists; `deadline − callTime ≤ PostRefreshBudget`; `deadline ≤ realStart + ExtendedWorkerBudget`. Subtest with service context `WithTimeout(Background, 7m30s)`: deadline ≤ the service deadline. The unverified bound is asserted in SEC001a. |
| AC-021 | `runPostRefresh` persist guard | `…_AC021_PhaseNotPersisted`: `store.failPhase = "post_refresh"`. Asserts: no refresh after `step:dietpi`; line 2 ends with `; post-run package list refresh skipped: state not persisted`; outcome `success`. |
| AC-022 | Outcome isolation; AD-005 success arm | `…_AC022_RefreshFailsAfterFullSuccess`: the second refresh returns `Interrupted:true` and a 1 KiB error. Asserts: outcome `success`, exit 0, `Interrupted` false; line 2 is `SuccessSummary + "; post-run package list refresh failed: " + <reason>` with the reason ≤ 200 bytes; `rec.Validate()` returns nil. |
| AC-023 | AD-005 error arm; §2.2 | `…_AC023_NoteAfterAptFailure`: the AC-012 setup with a 2 KiB upgrade error and a failing second refresh. Asserts: line 2 starts with `boundedText(err,1500) + "; post-run package list refresh failed: "`.<br>**M-3 mirror:** `TestSummaryV2_DR_AC023_ErrorArmAppendsNoteOnce` calls `summaryV2` directly with `AptOutcome` error and a note set. Asserts the note appears exactly once, right after the 1,500-byte-bounded failure text.<br>**Server (M-2):** `TestPatchBadge_DR_AC023_PostRefreshNoteKeepsClassification` (`internal/server/handlers/patch_badge_postrefresh_test.go`) compares `computePatchBadge` with and without a note on line 2 and asserts equal `Kind`, reason codes, `Blocked`, and refusal `Detail` (via `parseRefusals`). Inputs: (i) `apt: failed; dietpi: skipped (apt_not_successful)` and (ii) `apt: succeeded; dietpi: skipped (dpkg_locked)` + `SuccessSummary`, each under statuses `failed` and `succeeded`. Each uses two notes: single-line `post-run package list refresh failed: exit status 100` and multi-line `post-run package list refresh failed: package process failed: context deadline exceeded\nsignal: terminated`. |
| AC-024 | `stampLists` via `runPostRefresh` | `…_AC024_StampFailureIsOutputOnly`: `loggingTouch` fails on its second call. Asserts: `[lists_stamp]` appears after `[post_refresh]`; outcome and summary equal those of the rig with a working stamp. |
| AC-025 | `appendBounded`; AD-005 bounds | `…_AC025_OutputAndSummaryBounds`: upgrade output ≥ 200 KiB; the second refresh returns 64 KiB of output and a 1 KiB error. Asserts: output length ≤ `MaxOutputBytes`; `OutputTruncated` is set; the truncation marker is present; the output ends with the tail of the `[post_refresh]` content; summary length ≤ 4,096; reason ≤ 200 bytes. |
| AC-026 | AD-002 (shared argv) | `…_AC026_SameRefreshCommand`: the two `refreshKey` invocations are deep-equal. |
| AC-027 | `validatingStore` plus an explicit parse | `…_AC027_TerminalRecordsValidAndParse`: a table re-running the AC-003..AC-025 and SEC001a–c setups. Asserts `rec.Validate()` returns nil, `parseRecord(json.Marshal(rec))` succeeds, and `rec.Terminal()`. |
| M-1 (a) (DR-SEC-001; FR-015, FR-017, NFR-002) | AD-003 rev 2 (check 6 refused for a short backstop, so unverified) | `…_SEC001a_UnitLimit35MinUsesWorkerBudget`: `g.unit.RuntimeMax = 35*time.Minute`, so DietPi is `skipped`/`insufficient_time` at check 6; bump in the upgrade. Subtest `start+24m`: the upgrade hook advances the clock 24 minutes; asserts no post-run refresh and line 2 ends with the FR-016 note. Subtest `start+20m`: asserts the refresh runs and its recorded deadline is ≤ `realStart + WorkerBudget` and ≤ `PostRefreshBudget` from the call. |
| M-1 (b) | AD-003 rev 2 (a `Unit` error means unverified) | `…_SEC001b_UnitErrorUsesWorkerBudget`: `g.unitErr = errors.New("systemctl failed")`; otherwise the same two subtests as (a). |
| M-1 (c) | AD-003 rev 2 (check 6 never reached) | `…_SEC001c_PreCheck6SkipUsesWorkerBudget`, run with the default 85-minute fake unit and a `countingUnit` wrapper.<br>• Subtest `status_unknown via Execute`: remove `run/dietpi/.update_available`, so detection yields `status_unknown`. This is the same check 3 path as `nothing_pending`, whose `up_to_date` row cannot occur in production (AF-1). The same `start+24m` and `start+20m` assertions as (a) apply, and `Unit` is called 0 times.<br>• Subtest `nothing_pending via runExtra`: `runWithFixedStatus(t, g, "up_to_date")`, then assert `backstopVerified == false` on the returned `extraRun` and 0 `Unit` calls. To read the flag, the subtest calls `g.w.runExtra` directly, the same way `runWithFixedStatus` does. |

**Count:** 27 ACs and 4 mitigations mapped to 35 named test functions:
- 32 new in `extra_postrefresh_test.go`: 26 from rev 1, plus SEC001a/b/c, `TestSummaryV2_DR_AC016_*`, `TestSummaryV2_DR_AC023_*` and `…_AC011_RefreshPhaseNotPersisted`;
- 1 new in `worker_postrefresh_v1_test.go`;
- 1 new server test;
- the unmodified golden test.
The existing v1 suite is further evidence for AC-002.

### 8.1 QA conditions (from the security review; QA verifies them in addition to M-1..M-4)

- **C-1:** `git diff main -- cmd/ scripts/ internal/agent internal/server/handlers/*.go` shows no non-test change. `CommandAllowed` and `aptRefreshArgs` are byte-unchanged. A grep shows exactly one `Runner.Run` for the post-run refresh (in `runPostRefresh`), one call site of `postRunRefresh`, and one call site of `postRunRefreshV2`.
- **C-2:** The v1 branch is gated positively on `req.Version == ProtocolVersion`, not on "not v2". `golden_v1_test.go` is byte-unchanged and passes. `worker_postrefresh_v1_test.go` was committed before the first production change.
- **C-3:** The deadline assertions in AC-020 use the real clock. The subtest with a short service deadline shows the sub-context deadline is no later than the service deadline. SEC001a shows the unverified deadline is ≤ `realStart + WorkerBudget`.
- **C-4:** AC-010 and AC-017 assert that `Sources` (`cfgState.calls`) is called zero times when the run is ineligible before the baseline would matter, and only once (the baseline) otherwise. This shows the guards run before any I/O.

## 9. Error Handling

There is no HTTP surface. Every failure mode maps to an effect on the record. None of them changes `Outcome`, `ExitCode`, `Interrupted`, `AptOutcome` or any updater field.

| Failure | Handling | Record effect | Log |
|---|---|---|---|
| `Sources()` error (before or after) | Treated as changed (FR-007) | refresh attempted | none (as v1) |
| Run ineligible (AD-004), including a failed save of the `refresh` phase | Silent return | none | none |
| Budget short on the chosen basis, either clock (AD-003); this includes an unverified backstop past about minute 23 | No refresh | line 2 `…; post-run package list refresh skipped: worker time budget nearly exhausted` | none |
| `Store.Save` of `post_refresh` fails | No refresh | line 2 `…; post-run package list refresh skipped: state not persisted` | none |
| `Runner.Run` error, timeout or interruption | No retry | `[post_refresh]` output, plus line 2 `…; post-run package list refresh failed: <≤200 B, may span lines>` | none |
| `TouchLists` error after a successful refresh | Ignored for the outcome | `[lists_stamp]` output line (bounded to 200 B) | none |
| Worker killed during the v2 refresh | Existing `RecoverInterrupted` | `interrupted`, "Previous worker stopped…" | existing |
| `Store.Finish` error | Existing `finalizeV2` handling | unchanged | unchanged |

## 10. Security Considerations

- **No new privilege (NFR-003, C-1).** The only command is `/usr/bin/apt-get -o Acquire::Retries=0 update`. That is the argv already in `CommandAllowed`, run as root in the same service start. The backstop verification reuses the check 6 result already obtained, so no extra `systemctl` call is added. `install.sh`, the unit, the drop-in and sudoers are untouched.
- **Backstop invariant (DR-SEC-001, M-1).** EXTRA-UPD-001 SEC-002 is preserved: the worker relies on the 80-minute budget only after verifying the unit's effective backstop root-side in this run. Otherwise the refresh ends within 30 minutes of the run start, under the base 35-minute backstop. Two consequences:
  - A drifted drop-in cannot make the refresh push a clean run into a systemd kill and a false `interrupted`.
  - A compromised `riot` account that sends v2 requests to an unverified host cannot do so either.
- **Running after a third-party updater (DR-SEC-005).** This places no new trust in updater output:
  - DietPi already has root over `/etc/apt`, and the nightly apt run reads the same files.
  - `apt-get update` installs nothing, and index files are verified against apt's keyrings and `signed-by`.
  - The decision reads only a root-side stat fingerprint and outcomes the worker computed itself.
- **Repeated rewrites cannot amplify work.** The refresh runs at most once per run (FR-009), for at most 5 minutes. The fingerprint walk is capped at 1,000 entries.
- **Untrusted text in the summary (DR-SEC-002, M-2).** The note reason is runner error text, cut to 200 bytes of valid UTF-8, and it **may span lines** (`errors.Join`). It contains no command output. §2.2 shows why a continuation line cannot change server classification or refusal `Detail`. The server reads at most 2,048 bytes using linear-time regexps (DP-SEC-007).
- **Interruption semantics.** There is no package-manager work after any interruption (BR-004, AD-004). The refresh never writes outcome fields, and AD-003 keeps it inside the verified kill deadline.
- **Accepted residuals (DR-SEC-006).**
  - The fingerprint walk has no deadline (a symlink into a hung mount; root only).
  - A lists lock held by a stray DietPi descendant gives a `failed:` note.
  - `apt-get update` can exit 0 with warnings.
  - A stop between rule 4 and `Runner.Run` gives a `failed: … context canceled` note.

## 11. Performance Considerations

- **Time bound (NFR-002), per case (rev 2):**
  - **Backstop verified in this run** (a present updater passed check 6, `RuntimeMax ≥ 83m30s`). The refresh starts only when at least 7 minutes of the 80-minute `ExtendedWorkerBudget` remain on both clocks. Its sub-context ends no later than `runCtx`. The worst case (5 min, then SIGTERM, 90 s grace, SIGKILL, 5 s drain) ends about 25 seconds inside 80 minutes. That is below `ExtraMinBackstop` (83.5 min) and the verified effective backstop.
  - **Not verified** (no present updater; updater skipped at checks 1–5; `Unit` error; mount refusal; backstop below 83m30s). The refresh starts only when at least 7 minutes of the 30-minute `WorkerBudget`, measured from run start, remain on both clocks. Its sub-context is derived from `aptCtx`, so it ends by minute 30. That is under the base 35-minute backstop, as before this story.
  - In both cases there is at most one extra refresh per run. Typical cost on PiCUPS is one `apt-get update` lasting seconds, and only when the fingerprint changed.
- **Output bound (NFR-001).** The refresh output goes through `appendBounded`, so the output stays at most 256 KiB with the existing marker. The note is at most 240 bytes. The apt failure line is at most 1,740 bytes. The summary is at most 4,096 bytes.
- **Growth.** No new file, field, table, retained record type, log stream or index. Still two fingerprint walks of at most 1,000 entries each per run. No extra `systemctl` call.
- There are no caching or indexing changes.

## 12. Implementation Notes for Engineers

1. **Write the AC-002 characterization test first.** Create `worker_postrefresh_v1_test.go` against the **unmodified** code. Use `fakeClock`, `sourcesSequence("a","b")` and a working `TouchLists`. Capture the bytes and the phases. Commit it as `DIETPI-REFRESH-001 test: pin v1 post-run refresh record` **before** any production change. Never regenerate the bytes, and do not touch `golden_v1_test.go` (C-2).
2. **Keep the v1 call order exactly as it is.** `fingerprintSources()` stays immediately before `step("refresh")`. The v1 branch (`req.Version == ProtocolVersion`) calls `postRunRefresh` at the same point as today, before the defer.
3. **Set `listsRefreshed` only on the success path of `step("refresh")`.** A persistence failure of the `refresh` phase means the step never ran, so the flag stays false (M-4 test).
4. **Basis contexts (AD-003 rev 2).** Use `runCtx` only when the backstop was verified; otherwise use `aptCtx`. Never use `svc` or `context.Background()`. `aptCtx`'s deadline is the real run start + `WorkerBudget`. When the apt part used up `aptCtx`, `AptOutcome` is `interrupted` and AD-004 returns first. In every other case, an expired `aptCtx` simply fails the 7-minute check and produces the budget note.
5. **`backstopVerified` placement.** Set it immediately after the `unit.RuntimeMax < ExtraMinBackstop` refusal is passed, on `run`, so the later `skip` and `fail` closures and the normal return carry it. Do not set it anywhere else, and do not add a new `Unit()` call.
6. **Do not route the refresh through `step()`.** `runPostRefresh` must use `Runner.Run` directly.
7. **The "pre-story" oracle.** A rig with `Sources = nil` is a valid pre-story oracle for AC-006, AC-012, AC-013 and AC-015. Use `fakeClock` on both rigs.
8. **Clock-driven tests.**
   - AC-018 and AC-019 (verified): advance `g.clock` inside `fakeStep.onCall`, never before the step, so that check 7 still passes.
   - SEC001a/b/c (unverified): advance it in `runner.after[upgradeKey]`. Check 7 is never reached there.
9. **Count fingerprint calls with `cfgState`.** In v2, `Sources` is called twice when the run is eligible, once when it is ineligible after the baseline was taken, and zero times when it is ineligible before the refresh step (C-4).
10. **AC-003 uses the real fingerprint** over the rig's temp root, with `os.Chtimes`. It works on Windows and Linux. No real apt, dpkg or dietpi-update ever starts (NFR-006). The SC-001 tripwire remains.
11. **No lock wait is added to the post-run refresh.** Adding one would change the argv (FR-031).
12. **Test names** must carry the AC or mitigation ID in the function name and in every `t.Run` and `t.Errorf` message, for example `"[AC-012] …"` or `"[DR-SEC-001] …"`.
13. **Running tests.** Run `go test ./internal/maintenance/... ./internal/server/handlers/... ./internal/agent/...` on Windows (no `-race`). CI covers `-race` and the Linux-only files.
14. **Commits:**
    1. `DIETPI-REFRESH-001 test: …` (characterization first).
    2. `DIETPI-REFRESH-001 refactor: extract shared post-run refresh helpers`, with v1 and the golden test green.
    3. `DIETPI-REFRESH-001 feat: post-run refresh after the extra-updater step for v2 runs`.
    4. `DIETPI-REFRESH-001 test: …` for the v2, summary and server tests.

## 13. Definition of Done

- [ ] All component changes in Section 4 are implemented. Engineering changed no file outside Section 4.
- [ ] All 27 ACs and the mitigations M-1..M-4 in Section 8 have their named tests, with IDs in the names and messages.
- [ ] `internal/maintenance/golden_v1_test.go` is byte-for-byte unchanged and passes.
- [ ] `worker_postrefresh_v1_test.go` was committed before the first production change and passes unchanged afterwards.
- [ ] The only modified existing test is `TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore`, changed exactly as in Section 4.
- [ ] `go test ./...` is green on Windows, and CI (`make test` with `-race` on Linux) is green.
- [ ] `go vet ./...` and the existing lint report no new findings.
- [ ] No migration is needed. There is no change under `internal/agent`, `internal/server` (non-test), `web/`, `scripts/` or `cmd/`.
- [ ] QA conditions C-1..C-4 (§8.1) hold.
- [ ] The implementation report states the bounds:
  - at most 1 refresh per run, lasting at most 5 minutes plus 90 seconds;
  - the run ends within 80 minutes when the backstop was verified, otherwise within 30 minutes;
  - output at most 256 KiB, note at most 240 bytes, summary at most 4,096 bytes.
- [ ] The implementation report states the D-005 deploy step: Ryan re-runs `install.sh` as root on PiCUPS (and on PiVPN once opted in), then performs the D-006 check.

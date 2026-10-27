# Security Review Report

**Story ID:** DIETPI-REFRESH-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-27
**Inputs:** `docs/requirements/DIETPI-REFRESH-001-frd.md` (784f3e2), `docs/architecture/DIETPI-REFRESH-001-add.md` (6695dfb), the PATCH-FLOW-001, EXTRA-UPD-001 and DIETPI-PATH-001 security reviews, and the code on `fix/dietpi-post-refresh`: `internal/maintenance/worker.go`, `extra.go`, `aptsources.go`, `runner_linux.go`, `extra_linux.go` (`EffectiveUnit`), `protocol.go`, `cmd/riot-maintenance/main_linux.go`, `scripts/riot-maintenance.service`, `scripts/install.sh` (drop-in), `internal/agent/extra_updaters.go` and `maintenance.go` (v2 dispatch and result mapping), and `internal/server/handlers/patch_badge_refusals.go` and `patch_badge.go`.
**Verdict:** APPROVED WITH MITIGATIONS

There are no CRITICAL or HIGH findings. The change adds no command, argument, environment variable or privilege. One MEDIUM finding (DR-SEC-001) needs a small amendment to AD-003, and it must land in this story. The remaining items are LOW or informational conditions for implementation and QA.

---

## Threat Model Summary

### Assets

- **Integrity of the root maintenance helper's behaviour.** `riot-maintenance` runs unsandboxed as root. Its fixed command plan (`CommandAllowed`) is the privilege boundary.
- **Integrity of the run record.** This covers outcome, exit code, interrupted flag, apt outcome and updater results. The agent, the badge, EXPECT and auto-reboot gating all act on it. A false `interrupted` or a false `success` misleads Ryan and the reboot logic.
- **Truthfulness of the freshness badge** (PATCH-FLOW-001 FR-029). This story exists to protect it.
- **Host availability.** The run must end inside the systemd backstop (`RuntimeMaxSec`: 35 min base, 85 min with the opt-in drop-in), so systemd never kills the helper mid-write.

### Threat Actors

- **Compromised `riot` agent account.** Established in EXTRA-UPD-001: it can write any well-formed request (v1 or v2) to the inbox and start the unit through sudoers. It cannot change the root policy, the drop-in or `/etc/apt`.
- **Third-party updater (DietPi `dietpi-update`) running as root.** It is trusted behind the EXTRA-UPD-001 gates (trusted owner, trust walk, isolation proofs), but it writes host apt configuration.
- **Network attacker on the mirror path.** Can stall or tamper with `apt-get update` traffic. Signatures limit tampering to denial and delay.
- **Local non-root account** (for example CUPS `lp` on PiCUPS). It cannot write `/etc/apt` or `/var/lib/apt`, so the fingerprint and the stamp are out of its reach (PATCH-FLOW-001 INFO-1).

### Attack Surface Introduced

- No new endpoint, command, argv, file, sudoers line, unit setting or record field.
- **Moved, not added:** the second apt-configuration fingerprint (a stat walk of at most 1,000 entries) and the one bounded `apt-get -o Acquire::Retries=0 update` now run after the extra-updater step, against configuration that a root third-party updater may just have written.
- **Changed time basis:** in a v2 run, the refresh's remaining-time check now measures against the 80-min extended budget (`runCtx`). Before this story it measured against the 30-min apt context.
- **New summary placement:** a refresh note can now follow an apt failure text on line 2 of a v2 summary. Before, it could follow only the success text.

---

## Findings

### CRITICAL

None.

### HIGH

None.

### MEDIUM

#### DR-SEC-001: The relocated v2 refresh is timed against the 80-minute budget without a verified 85-minute backstop

**Severity:** MEDIUM
**Domain:** Business logic (time bound), record integrity
**Location:** ADD AD-003, §6 (`postRunRefreshV2` contract), §7.1 step 3.3, §11 ("ends inside … the 85-min service backstop"); `worker.go` `Execute` (`runCtx` is 80 min for every v2 request)

**Description:**
EXTRA-UPD-001 SEC-002 established that the worker must never rely on `ExtendedWorkerBudget` unless it has verified, root-side, that the unit's effective backstop is at least `ExtraMinBackstop`. Only the agent checks this before it sends v2, and the agent is the actor these reviews assume may be compromised. The fix put that verification into `runExtra` check 6, and it guards only the updater step.

Today the v2 post-run refresh sits inside the apt part. Its real-clock check runs on `aptCtx` (30 min), so it ends by about minute 30. Hold restoration adds at most 2 min, so the run stays inside even the base 35-min backstop.

AD-003 moves the check to `runCtx` (80 min) and to the injected 80-min deadline. On a host whose effective backstop is 35 min, the updater is refused at check 6 (`insufficient_time`) or skipped earlier, yet the post-run refresh still sees about 48 minutes "remaining" and starts. When the apt part ran long, the refresh crosses minute 35. The refresh can take up to 5 min plus 90 s grace plus 5 s drain, about 6.6 min, so this happens whenever the apt part ends after about minute 28.4.

systemd then stops the unit (SIGTERM, then SIGKILL after `TimeoutStopSec=90s`). If the SIGKILL wins, the record is left in `post_refresh`. The next start's `RecoverInterrupted` then rewrites it as `interrupted` with "package state requires manual review". That happens even when apt, and any updater, succeeded. The agent then reports `ReasonRestarted`, and `extraRebootEntries` gating is lost for that run.

The ADD §11 claim that "a v2 run still ends inside … the 85-min service backstop" holds only when the drop-in is actually in force. No part of the post-run path verifies that.

**Attack Scenario:**
1. A host's effective backstop is 35 min, while the agent's cached facts (10-min TTL) still say 85. This happens after Ryan revokes the opt-in, after an installer run interrupted before `daemon-reload`, or after root-side drift. A compromised `riot` reaches the same state on purpose, because it can send v2 regardless of the facts.
2. A v2 `full` run starts on a slow Pi with a large dist-upgrade. The apt part ends at about minute 29, and an apt maintainer script touched `sources.list.d`. A network attacker can also stall the mirror so that `apt-get update` uses its full 5-min limit.
3. DietPi is skipped (`insufficient_time` at check 6, or earlier). The post-run refresh passes both 80-min checks and starts at about minute 29.
4. At minute 35 systemd stops the helper during the refresh or the `Finish` write. The persisted record is non-terminal. On the next start it becomes `interrupted` with a manual-review summary. A clean run is reported as a possible package-state failure, and the false alarm repeats while the drift persists.

Impact is limited to record integrity and availability. `apt-get update` installs nothing, and holds were already restored. That is why this is MEDIUM and not HIGH. It is still a regression of the SEC-002 invariant, and it is new in this story.

**Required Resolution (mitigation M-1, ADD AD-003, §6, §8, §11):**
- The v2 post-run refresh may use the extended budget as its time basis only when the worker has verified, root-side and in this run, that the effective unit backstop is at least `ExtraMinBackstop`. This is the same rule as `runExtra` check 6.
- When that verification did not happen in this run, or failed, the refresh must end inside the pre-story bound: `WorkerBudget` measured from the run start, on both the injected clock and the real deadline. Otherwise it is skipped with the existing FR-016 note. Verification did not happen when no updater reached check 6 (for example `nothing_pending`, `apt_not_successful` or `not_enabled`). It failed when the unit read errored or the backstop was too short.
- The refresh sub-context must still be bounded by `runCtx`, and its deadline must be no later than the basis in use.
- The architect chooses how to satisfy this. Reusing the check 6 result keeps NFR-003 trivially true. A fresh `Unit()` read is the same fixed `systemctl show` the worker already runs in v2, so it is acceptable, provided an error counts as "not verified".
- ADD §11 must state the bound per case: extended basis only with a verified backstop, otherwise the 30-min basis.
- **New tests (§8):**
  - (a) v2 run, fake `Unit` reporting `RuntimeMax` 35 min, and DietPi skipped at check 6. With a configuration change and the clock at start + 24 min, the refresh is skipped with the FR-016 note. At start + 20 min it runs, and its deadline is no later than start + `WorkerBudget`.
  - (b) The same with `Unit` returning an error.
  - (c) DietPi `nothing_pending` (check 6 never reached) behaves like (a).
- The existing AC-018 and AC-019 rigs reach check 6 with the 85-min fake unit, so they stay valid unchanged.

**Blocks:** Not implementation. It must land in this story, and QA must verify it.

### LOW / INFORMATIONAL

#### DR-SEC-002: The refresh-failure reason can span several lines, so "the note sits on line 2" is not exact

**Severity:** LOW
**Domain:** Output integrity / parser robustness (DP-SEC-004)
**Location:** ADD §2.2 (third bullet), AD-005, §10 ("Untrusted text into the summary"); `runner_linux.go` `runProcessSpec`; `worker.go` `boundedText`

**Description:**
`runProcessSpec` builds its error with `errors.Join`, which separates parts with `\n`. A realistic post-run failure therefore produces this reason:

```
package process failed: context deadline exceeded
signal: terminated
```

A stray-process kill or a forced kill adds further lines. `boundedText` keeps newlines, so the note can push continuation lines onto line 3 and beyond of the summary.

I traced every consumer:
- **No crafted content is possible.** The reason contains no apt output. Command output goes to `Output`, and the reason is built only from fixed literals, Go `exec`/wait text and context errors. No continuation line can begin with `<registry-id>: `, and none can begin with `<id>: skipped (<reason>); `.
- **Apt-failed runs.** `parseRefusals` drops line 1 `apt: failed; …` before it reads any other line. In runs with no updater, line 1 is the apt text itself and also fails the regexp.
- **Apt-succeeded runs with status succeeded.** These never reach `successLine` or `onlyDetailLines`. `refusalDetail` matches only the exact `<id>: skipped (<reason>); ` prefix.
- **Apt-succeeded runs with status failed.** An extra line can only make `onlyDetailLines` false, which keeps the run red. That is the conservative direction, and the current worker never produces such a run for a refusal anyway.
- **The badge's `runMessage`.** It collapses newlines.

Classification cannot become less alarming. The behaviour already exists for v1 and for the v2 success arm. The ADD's stated reason ("the note sits on line 2") is still wrong, and a future parser change could rely on it.

**Attack Scenario:**
No exploit exists today, because the content is not attacker-controlled. The risk is a later parser that trusts line 3 as the first updater detail line. With a multi-line note there, such a parser would misread the summary.

**Required Resolution (mitigation M-2, ADD §2.2, AD-005, §10, §8 AC-023):**
- §2.2 and §10 must state that the note may contain line breaks, and why that cannot change classification (the reasoning above).
- The AC-023 server test must add a case whose note reason contains a newline (the realistic string above). It must cover both the apt-failed and the apt-succeeded-with-refusal summaries, under both statuses, and assert equal `Kind`, reason codes, `Blocked` and refusal `Detail`.
- Making the v2 note single-line is optional. If chosen, it must not change v1 bytes (FR-001), so it would have to apply to the v2 path only.

#### DR-SEC-003: The `summaryV2` interrupted arm's second safeguard is not pinned by a test

**Severity:** LOW
**Domain:** Outcome isolation
**Location:** ADD AD-005, §8

**Description:**
AD-005 rightly requires that the interrupted arm never append the note, even when `st.postRefreshNote` is set ("second safeguard"). Every AC reaches that arm only with an empty note, because AD-004 rule 2 returns before any note exists. The safeguard is therefore untested. A later refactor that hoists `"; "+note` out of the switch would compile and pass every AC.

**Attack Scenario:**
A future change sets the note on a path that skips AD-004. An interrupted run's line 2 then reads "Maintenance interrupted …; post-run package list refresh failed: …". The text implies that housekeeping ran after the interruption, which contradicts BR-004.

**Required Resolution (mitigation M-3, ADD §8):**
- Add a direct unit test of `summaryV2`. Give it `AptOutcome` interrupted, an `aptErr`, and a non-empty `st.postRefreshNote`, and assert that line 2 contains no `post-run`.
- Add the mirror-image case for the error arm: the note is appended exactly once.

#### DR-SEC-004: The `listsRefreshed` flag needs a persistence-failure test

**Severity:** LOW
**Domain:** Business logic (eligibility)
**Location:** ADD AD-004 rule 1, §12 note 3, §8

**Description:**
§12 note 3 correctly says that a persistence failure of the `refresh` phase leaves the flag false. No AC exercises that path. AC-011 covers only a runner failure. If the flag were set before `step` returns, or on a non-nil error, a run whose own refresh never ran would get a post-run refresh. That is BR-005's forbidden "retry".

**Attack Scenario:**
A disk-full host fails `Store.Save` at phase `refresh`. A mis-placed flag then lets the post-run path run `apt-get update` on a record that says the refresh never happened. The record and the host diverge.

**Required Resolution (mitigation M-4, ADD §8):**
- Add a test with `store.failPhase = "refresh"` on a v2 run with a fingerprint change. Assert that the refresh command runs zero times, that `Sources` is called at most once (the baseline), and that the summary contains no `post-run`.

#### DR-SEC-005: Trust in apt configuration written by the extra updater (focus question)

**Severity:** INFORMATIONAL
**Domain:** Privilege / supply chain
**Location:** ADD §10 second bullet

The question was whether running `apt-get update` after DietPi is worse than the nightly apt run reading the same files. It is not. No new trust is placed in updater output.

- **DietPi already has root on the host's apt configuration.** The launcher's only isolation is a new mount namespace with private `/tmp` and `/run/dietpi` (`launcher.go` `Unshare(CLONE_NEWNS)`). DietPi drops no capability and writes `/etc/apt` on the host, as the incident shows.
- **Hooks and sources add no privilege.** Any `apt.conf.d` `APT::Update::{Pre,Post}-Invoke` hook or source that DietPi plants runs with no privilege DietPi lacked. `apt-daily.service` and the next run's own refresh would run it anyway, as unconfined root.
- **The post-run refresh is the more contained run.** It runs under the fixed environment, the 5-min limit, a process-group kill and the unit's `KillMode=control-group`.
- **`apt-get update` installs nothing.** Index files are verified against apt's keyrings and `signed-by`. A malicious source can at most feed indices that a later upgrade would act on, and that is true with or without this story.
- **Updater output is never parsed for the decision.** The decision reads only a root-side stat fingerprint, plus updater outcomes that the worker computed itself. Those outcomes are used only to suppress the refresh after an interruption (AD-004 rule 3). The argv is fixed, and `ExecRunner` re-checks it against `CommandAllowed`, so a coding error fails closed with "command is outside the fixed maintenance plan".
- **Untrusted actors cannot use it.** A local non-root user cannot touch `/etc/apt`, so it cannot drive the decision. A process that keeps rewriting `preferences.d` causes at most one refresh per run (FR-009).

No action is required.

#### DR-SEC-006: Residuals accepted for this story

**Severity:** INFORMATIONAL

- **Fingerprint walk without a deadline.** In production, `AptSourceEntries("/", 1000)` passes no expiry callback. A symlink in `sources.list.d` or `preferences.d` that points into a hung mount would block the second `stat` until the backstop kills the helper, and the record would then be recovered as `interrupted`. Only root can plant such a link. The walk now happens after DietPi, but DietPi is already trusted root. This is the same residual as PATCH-FLOW-001 SEC-006 and INFO-1.
- **A refresh next to a timed-out or failed updater.** A DietPi descendant that escaped the step's process group could still hold apt's lists lock. The refresh then fails fast, with no lock wait because the argv is unchanged (§12 note 10), and the outcome is a `failed:` note. `apt-get update` does not touch the dpkg database, so running it after a half-finished DietPi cannot worsen the package state.
- **Exit 0 with warnings.** `apt-get update` can exit 0 when some sources only warn, and the stamp then marks the lists fresh. This already holds for the run's own refresh and for v1. It is not changed here.
- **A service stop between the AD-004 rule 4 check and `Runner.Run`.** `runProcessSpec` checks `ctx.Err()` before it starts anything. In that narrow window the run therefore records a `failed: … context canceled` note instead of no note, and no process starts. The outcome is unchanged. This is acceptable.

---

## Answers to the review focus

| Focus | Result |
|---|---|
| **Privilege** | No new command, argv, environment or privilege. The post-run call is `Runner.Run(sub, "/usr/bin/apt-get", aptRefreshArgs()...)`, the exact `CommandAllowed` entry `-o Acquire::Retries=0 update` that the run's own refresh uses (FR-031, AD-002 shared helper). AD-007 and §4 leave `main_linux.go`, `install.sh`, the unit, the drop-in and sudoers unchanged. QA condition C-1 verifies this. |
| **Extra-updater trust** | Not worse than the nightly apt run, and no trust is placed in updater output (DR-SEC-005). |
| **Budget and timing** | The dual check in AD-003 is sound. The injected clock (`w.now()` is UTC wall time, and `UTC()` strips the monotonic reading) gives exact test boundaries. `time.Until(runCtx.Deadline())` uses the monotonic reading, so a wall-clock jump on an RTC-less Pi cannot defeat it: a forward jump only skips spuriously, and a backward jump is caught by the real check. With at least 7 min left, the runner's worst case (5 min, then SIGTERM, 90 s grace, SIGKILL, 5 s drain) ends about 25 s inside `runCtx`, and `ExtendedWorkerBudget` (80 min) stays inside `ExtraMinBackstop` (83.5 min). The sub-context comes from `runCtx`, never from `aptCtx` or `Background`. **Gap:** the 80-min basis is used without a verified backstop (DR-SEC-001, M-1). |
| **Outcome isolation** | Holds by construction. `runPostRefresh` bypasses `step()` and writes only `Phase`, `UpdatedAt`, `Output` and `OutputTruncated`. The note reaches only line 2 of `summaryV2`. `finalizeV2` computes the outcome from `AptOutcome`, the updater entries and `aptErr`, and the refresh writes none of them. The AC-012, AC-013, AC-015 and AC-022 oracle comparisons pin this. Server classification (DP-SEC-004) is unaffected for apt-failed runs (line 1 gate) and for apt-succeeded runs (`SuccessSummary + "; "` prefix). The multi-line caveat is DR-SEC-002 (M-2). |
| **Bounded growth** | At most one refresh per run (one call site, FR-009). Output goes through `appendBounded`, so it stays at or below `MaxOutputBytes` with the marker. The note is at most 240 B (`"; "` + 38 + 200), the apt failure line at most 1,740 B, and the summary at most 4,096 B (`boundedText`). The server reads at most 2,048 B with linear-time regexps (DP-SEC-007). The agent-side bound is `MaxResultMsgBytes` (64 KiB). Still only one fingerprint walk of at most 1,000 entries per run, plus the baseline. No new file, field, table or log stream. |
| **Interrupted and stopping paths** | AD-004 rules 2 to 4 cover apt interrupted, any updater interrupted and `svc.Err()`. They are evaluated before any `Sources` call, so an interrupted run does no further I/O (BR-004). A crash during the refresh goes through the existing `RecoverInterrupted` path (out of scope). DR-SEC-001 is the one path on which the refresh itself can cause such a crash. The `summaryV2` second safeguard needs a test (DR-SEC-003). |

---

## Required mitigations

| # | Finding | ADD section | Property that must hold |
|---|---|---|---|
| M-1 | DR-SEC-001 (MEDIUM) | AD-003, §6, §8, §11 | The v2 post-run refresh uses the 80-min basis only after a root-side verification, in this run, of an effective backstop of at least `ExtraMinBackstop`. Otherwise it must end within `WorkerBudget` of the run start, on both clocks, or skip with the FR-016 note. New tests (a) to (c) as listed in the finding. |
| M-2 | DR-SEC-002 (LOW) | §2.2, AD-005, §10, §8 AC-023 | Correct the "line 2" claim. The AC-023 server test covers a note whose reason contains a newline, with classification and refusal `Detail` unchanged. |
| M-3 | DR-SEC-003 (LOW) | §8 (AD-005) | A direct `summaryV2` test shows that the interrupted arm never carries the note, even when one is set. |
| M-4 | DR-SEC-004 (LOW) | §8 (AD-004 rule 1, §12 note 3) | A test where the `refresh` phase fails to persist: zero refresh commands and no note. |

## Conditions for QA (verify, in addition to M-1 to M-4)

- **C-1:** `git diff main -- cmd/ scripts/ internal/agent internal/server/handlers/*.go` shows no non-test change. `CommandAllowed` and `aptRefreshArgs` are byte-unchanged. A grep shows exactly one `Runner.Run` for the post-run refresh (in `runPostRefresh`), one call site of `postRunRefresh` and one of `postRunRefreshV2`.
- **C-2:** The v1 branch is gated positively on `req.Version == ProtocolVersion`, not on "not v2". `golden_v1_test.go` is byte-unchanged and green, and `worker_postrefresh_v1_test.go` was committed before the first production change.
- **C-3:** AC-020's deadline assertions run against the real clock, and the subtest with a short service deadline shows the sub-context deadline is no later than the service deadline.
- **C-4:** AC-010 and AC-017 assert that `Sources` is called zero times when the run is ineligible (guards before I/O).

---

## Positive Observations

- **No new privilege.** The design reuses the exact allowlisted refresh through one shared helper (AD-002). Drift between v1 and v2 argv or note strings is impossible by construction.
- **Outcome isolation is structural.** The refresh bypasses `step()` and has a stated write set (§6), and the oracle-rig comparisons (`Sources = nil`) prove that updater results are byte-equal.
- **AD-003's dual clock is the right call.** The injected clock makes the boundary testable, and the monotonic real deadline closes the RTC-less-Pi clock-jump hole that a wall-clock-only check would open.
- **Eligibility fails toward doing nothing.** No refresh after a failed own refresh (no retry, BR-005), none after any interruption, and the guards run before any filesystem I/O.
- **D-004 was verified in code, not assumed.** §2.2 traced the only consumer of `SuccessSummary` and its line-1 gate.
- **A test is required for each property,** and the v1 characterization is pinned before any code moves.

## Verdict Rationale

**APPROVED WITH MITIGATIONS.** There is no CRITICAL or HIGH finding.

- **DR-SEC-001 (MEDIUM)** reopens the EXTRA-UPD-001 SEC-002 invariant ("never rely on the 80-min budget without a verified backstop") for the relocated refresh. It is a small, non-architectural tightening of AD-003, and it does not change the FRD's intent. FR-015 and FR-016 still apply, with one more skip condition that uses the same note. AC-003 to AC-027 stay valid as written.
- **M-1 must land in this story.** The architect should amend AD-003, §6, §8 and §11 before senior-dev starts. Alternatively, senior-dev implements it with the ADD amendment in the same PR.
- **M-2 to M-4** are test and documentation conditions.
- **QA** verifies M-1 to M-4 and C-1 to C-4.

# Formal Requirements Document

- **Story ID:** DIETPI-REFRESH-001
- **Title:** Post-run package list refresh covers apt configuration changes made by extra updaters (DietPi)
- **Author:** Business Developer Agent
- **Date:** 2026-10-27
- **Status:** FINAL (all product decisions made by Ryan; the open questions in Section 11 are non-blocking and carry recommended defaults)
- **Amends:** PATCH-FLOW-001 FR-040 and FR-041, for version 2 (extra-updater) runs only. PATCH-FLOW-001 FR-029 (the freshness rule) is unchanged. Version 1 runs are unchanged.

---

## 1. Executive Summary

A DietPi patch run leaves both DietPi hosts showing "Lists out of date" until someone runs `apt-get update` by hand. The cause is the order of steps. The maintenance helper decides whether to run its single post-run package list refresh at the end of the apt part. The DietPi extra updater runs after that, and at its start it rewrites a file in `/etc/apt/preferences.d/`. The agent's freshness rule then rightly reports that the lists are older than the apt configuration. This story moves the apt configuration change check, and the single bounded post-run refresh it may trigger, to after the extra-updater step for version 2 runs. Changes made by apt or by any extra updater are then covered, and a DietPi patch leaves the lists fresh. Version 1 runs behave exactly as they do today. The refresh never changes any outcome, runs at most once per run, needs no new privileges, and reaches a device only when the root installer is re-run there.

## 2. Background & Context

### Incident evidence (PiCUPS, DietPi on Debian trixie, 2026-10-27, UTC)

| Time | Event | Effect |
|------|-------|--------|
| 13:05:17 | The run's own refresh (`apt-get update`, apt part of the run) completes. `/var/lib/apt/lists` is stamped. | Lists fresh. |
| (apt part end) | The helper compares the apt configuration fingerprint taken before the refresh with one taken after upgrade and verification. They are equal, so no post-run refresh runs. | Correct for what apt did. |
| 13:05:56 | The DietPi extra-updater step starts. | — |
| 13:05:57 | DietPi rewrites `/etc/apt/preferences.d/dietpi-raspberrypi`, as it does at the start of every run. | The file is now newer than the lists. |
| 13:08:02 | The DietPi step ends. The run finishes. Nothing checks the apt configuration again. | — |
| afterwards | The agent's freshness check (PATCH-FLOW-001 FR-029) sees a `preferences.d` file newer than the last refresh and reports `stale`. The dashboard shows "Lists out of date" instead of a pending count. | **Defect.** Every DietPi patch leaves the badge stale. It clears only after a manual `apt-get update`, and the next DietPi patch brings it back. |

Both DietPi hosts (PiCUPS and PiVPN) are affected whenever they run a version 2 patch.

### Current state

- PATCH-FLOW-001 FR-040..FR-045 define one post-run refresh per run. The baseline fingerprint of the FR-029 locations is taken immediately before the run's own refresh step. A second fingerprint is taken after upgrade and verification. If the two differ, or if either cannot be taken, one more bounded refresh runs (5-minute limit). It is skipped with a note when too little of the worker budget remains. It never changes the run outcome.
- The fingerprint covers each file's path, modification time and size. A rewrite with identical content is therefore detected as a change. This is the same signal the agent's freshness rule uses.
- EXTRA-UPD-001 added version 2 runs. The apt part runs first, under the 30-minute worker budget. The extra-updater step then runs inside the 80-minute extended budget (`ExtendedWorkerBudget`). The post-run refresh check still sits at the end of the apt part, so the extra-updater step is outside its view.

### Ryan's decisions

- Fix it so that a DietPi patch leaves the lists fresh.
- FR-029 stays as written. `preferences.d` still counts toward freshness.
- Version 1 runs stay exactly as they are. The v1 golden test stays byte-identical.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Ryan (operator) | Dispatches manual and scheduled patches from the dashboard and reads the run result and the freshness badge. | Unchanged. |
| Maintenance helper (`riot-maintenance` worker) | Privileged service that runs the apt part, the extra-updater step, and the post-run refresh. Updated on a device only when `install.sh` is re-run as root there. | Unchanged. It runs only the package-list refresh command it already runs. No new privileges. |
| Extra updater (DietPi `dietpi-update`) | Third-party updater started by the helper in a version 2 run. It may rewrite files in the FR-029 locations. | Unchanged. |
| rIOt Agent | Requests runs, reports the run result, and reports package-list freshness (FR-029). | Unchanged. No agent change is required. |
| rIOt Server | Stores run results and classifies a run from the apt result line of the summary (DIETPI-PATH-001 DP-SEC-004). Renders the freshness badge. | Unchanged. |
| Older helper | A helper installed before this story (installer not re-run). | Keeps today's behavior: the check runs at the end of the apt part. |

## 4. Functional Requirements

### A. Which runs change

- **FR-001:** For a version 1 run, the post-run refresh behavior (when the check is made, whether a refresh runs, its budget, its output, its summary note, and the resulting record) must be identical to the behavior before this story, in every scenario.
- **FR-002:** For a version 2 run, the apt part must not perform a post-run refresh check, and must not perform a post-run refresh.
- **FR-003:** For a version 2 run, the helper must make the post-run apt configuration change check exactly once. It must make it after the extra-updater step has ended for every present updater. For an updater whose step ran, that includes the step's flag reconciliation and its post-step detection.
- **FR-004:** FR-003 applies to every version 2 run, including one in which no extra updater is present and one in which every present updater was skipped.
- **FR-005:** The baseline for the check must be the same as today: the apt configuration observed immediately before the run's own refresh step starts.
- **FR-006:** The check must treat the configuration as changed when, between the baseline and the check, any file in the FR-029 locations was added or removed, or changed its modification time or size.
- **FR-007:** The check must treat the configuration as changed when either the baseline or the check observation could not be made.
- **FR-008:** When the check finds a change and the run is eligible (FR-010..FR-013), the helper must run one post-run package list refresh.
- **FR-009:** A version 2 run must perform at most one post-run refresh in total, whether the change came from apt, from an extra updater, or from both.

### B. Eligibility for version 2 runs

- **FR-010:** A version 2 run is eligible for the post-run refresh only if the run's own refresh step completed successfully.
- **FR-011:** A version 2 run whose apt part ended before or at its own refresh step (recovery, repair or verification failure before the refresh, or a failed refresh) must not perform a post-run refresh and must not add a post-run refresh note. The helper must not retry a failed refresh.
- **FR-012:** A version 2 run whose own refresh succeeded but whose apt part later ended with apt outcome `error` must stay eligible. Examples: upgrade failure, verification failure, hold restoration failure, version-change check failure.
- **FR-013:** Eligibility must not depend on any updater's outcome. A run stays eligible when an updater succeeded, failed, failed launcher setup, timed out, or was skipped for any reason (including refusals and `apt_not_successful`), and when no updater is present.
- **FR-014:** A version 2 run must not perform a post-run refresh, and must not add a post-run refresh note, when any of the following holds:
  - the apt outcome is `interrupted`;
  - any updater outcome is `interrupted`;
  - the helper service has been told to stop before the refresh would start.

### C. Time budget

- **FR-015:** Before starting the post-run refresh of a version 2 run, the helper must confirm that the remaining extended worker budget (`ExtendedWorkerBudget`, measured from the run's start) is at least the existing minimum. That minimum is the refresh limit (5 minutes) plus the termination grace (90 seconds) plus a 30-second margin, 7 minutes in total.
- **FR-016:** When less than that minimum remains, the helper must not start the refresh. It must record the note `post-run package list refresh skipped: worker time budget nearly exhausted`.
- **FR-017:** The post-run refresh must be limited to 5 minutes, and must end before the extended worker budget ends.
- **FR-018:** Before starting the refresh, the helper must persist the record in the `post_refresh` phase. When that fails, it must not start the refresh. It must record the note `post-run package list refresh skipped: state not persisted`.

### D. Outcome isolation

- **FR-019:** The post-run refresh, its skip, and its failure must not change the run's overall outcome, exit code or interrupted flag.
- **FR-020:** They must not change the apt outcome.
- **FR-021:** They must not change any field of any updater result: outcome, reason, exit code, start and finish times, versions, source, reboot-required flag, or note.
- **FR-022:** When the refresh fails, the helper must record the note `post-run package list refresh failed: <reason>`, with the reason cut to 200 bytes.
- **FR-023:** When the refresh succeeds, the helper must stamp the package lists as refreshed (as today). A failure to stamp must be added to the run output as a line and must not affect any outcome or the summary.
- **FR-024:** When the refresh succeeds, the helper must not add a summary note (as today).

### E. Recording

- **FR-025:** The refresh output must be appended to the run output under the `[post_refresh]` label, after the output of the last updater step, within the existing whole-run output bound.
- **FR-026:** A post-run refresh note must be appended to the apt result line of the summary, separated by `; `.
- **FR-027:** When the apt part succeeded, the apt result line must still begin with the exact success summary text (`Package state verified; maintenance completed`), as it does today when a note is present.
- **FR-028:** When the apt part failed, the note must follow the apt failure text already on that line, which stays cut to its existing 1,500-byte limit.
- **FR-029:** The whole summary must stay within its existing 4,096-byte limit.
- **FR-030:** For a version 2 run in which no configuration change is detected, the record must be identical to the record the same run produces today.

### F. Scope guards

- **FR-031:** The post-run refresh must use the same package-list refresh command, with the same options, as the run's own refresh step.
- **FR-032:** The freshness rule (PATCH-FLOW-001 FR-029) and its FR-029 locations, including `preferences.d`, must not change.
- **FR-033:** No change is permitted to the agent, server, dashboard, database schema, sudoers grants, systemd unit or drop-in, or root-owned updater policy.

## 5. Non-Functional Requirements

- **NFR-001 [Bounded growth]:**
  - At most one post-run refresh per run, version 1 and version 2 alike.
  - Each refresh is limited to 5 minutes.
  - Its output goes through the existing bounded append, so the run output stays within `MaxOutputBytes` (256 KiB) with the existing truncation marker.
  - At most one note per run, cut as in FR-022.
  - No new file, record field, table, retained record type or log stream.
- **NFR-002 [Time]:**
  - A version 2 run must not exceed `ExtendedWorkerBudget` (80 minutes) because of the post-run refresh.
  - The refresh must never be the reason a run ends as `interrupted` through budget expiry (guaranteed by FR-015).
- **NFR-003 [Security / privilege]:**
  - No new sudoers line, capability, privileged command, argument or environment variable.
  - The refresh is the command the helper already runs as root in the same run (FR-031).
- **NFR-004 [Compatibility]:**
  - An older helper keeps today's behavior. Agent and server need no change and accept records from either helper version.
  - The record format is unchanged. In a version 2 record, the `post_refresh` phase now comes after the updater phases instead of before them.
- **NFR-005 [Regression safety]:**
  - `internal/maintenance/golden_v1_test.go` must pass unmodified, and its expected output must stay byte-identical.
  - All other existing version 1 tests must pass unmodified.
  - An existing version 2 test may change only where it asserts the position of the `[post_refresh]` output or the timing of the post-run check.
- **NFR-006 [Testability]:**
  - Tests must never run real `apt-get`, `dpkg`, `dietpi-update`, or any other package-manager or updater process.
  - Command execution, updater steps, the configuration fingerprint, the list stamp and the record store must all be replaceable in tests.
- **NFR-007 [Observability]:** Every run in which a configuration change was detected must show one of these in its record:
  - a `[post_refresh]` output section (refresh attempted), or
  - a skip note (FR-016, FR-018).

  The only exception is an ineligible run under FR-011 or FR-014.

## 6. Business Rules

- **BR-001:** A patch run must leave package lists that match the apt configuration as it stands when the run ends, whatever changed that configuration during the run (apt, a package maintainer script, or an extra updater). The only exceptions are the budget, persistence, interruption and failure cases in this FRD, and each of those is visible in the record.
- **BR-002:** The post-run refresh is housekeeping. It never makes a run succeed, fail or count as interrupted, and never alters what an updater reported.
- **BR-003:** A failed or refused updater may still have rewritten apt configuration before it stopped. An updater's outcome is therefore no reason to skip the check.
- **BR-004:** After an interruption, the helper does no further package-manager work. The record already tells Ryan that the package state requires manual review.
- **BR-005:** The helper never retries the run's own refresh. A post-run refresh exists only to catch configuration that changed after a successful refresh.
- **BR-006:** The server classifies runs from the apt result line. A post-run refresh note must never change that classification (FR-027, FR-028).

## 7. Data Requirements

### Entities involved

| Entity | Change |
|--------|--------|
| Maintenance record (version 2) | No new fields. The `post_refresh` phase may now be persisted after the `extra_wait:<id>` / `extra_run:<id>` phases. `summary` may carry the post-run refresh note on the apt result line when the apt part failed (new for version 2). `output` may carry the `[post_refresh]` section after the updater sections. |
| Maintenance record (version 1) | None. |
| Apt configuration fingerprint | Unchanged definition (path, modification time, size of each file in the FR-029 locations). Only the moment of the second observation changes for version 2. |
| Package list stamp (`/var/lib/apt/lists` modification time) | Unchanged. Set after a successful post-run refresh. |

### Validation rules

- The note text is one of the three fixed strings in FR-016, FR-018 and FR-022. The FR-022 reason is cut to 200 bytes.
- The summary is at most 4,096 bytes. The output is at most `MaxOutputBytes`.
- The existing record validation must accept every record this story produces.

### Phase sequence (version 2 run)

| From | To | Condition |
|------|----|-----------|
| apt part phases (`accepted` … `check`) | `hold_restore` (if holds were released) | as today |
| apt part end | `extra_wait:<id>` / `extra_run:<id>` per present updater | as today (EXTRA-UPD-001) |
| last updater phase, or apt part end when no updater ran | `post_refresh` | configuration changed (FR-006, FR-007) **and** eligible (FR-010..FR-014) **and** budget sufficient (FR-015) **and** phase persisted (FR-018) |
| last updater phase, or apt part end when no updater ran | `finished` | any other case |
| `post_refresh` | `finished` | refresh ended (success or failure) |

Version 1 phase sequence: unchanged. `post_refresh` occurs, when it occurs, after `check` and before `hold_restore` or `finished`.

## 8. Acceptance Criteria

All criteria are verified with fake command runners, fake updater steps, fake fingerprints, a fake list stamp, a fake store and a controlled clock (NFR-006).

**AC-001: [Maps to FR-001, NFR-005] Version 1 golden output is byte-identical**
- Given: the unmodified `internal/maintenance/golden_v1_test.go`
- When: the test suite runs
- Then: the golden test passes without any change to the test or its expected bytes

**AC-002: [Maps to FR-001, NFR-005] Version 1 post-run refresh unchanged**
- Given: a version 1 `full` run whose fake fingerprint changes during the upgrade
- When: the run executes
- Then: exactly one post-run refresh runs after verification, in the same position as before this story
- And: the record (phase sequence, output, summary, outcome) equals the pre-story record for the same inputs

**AC-003: [Maps to FR-003, FR-006, FR-008, FR-023, FR-024] DietPi rewrites preferences.d (PiCUPS case)**
- Given: a version 2 `full` run with DietPi requested and permitted, apt making no configuration change, and a fake DietPi step that changes the modification time of `/etc/apt/preferences.d/dietpi-raspberrypi` (same size)
- When: the run executes
- Then: exactly one post-run refresh runs, and it starts after the DietPi step has ended
- And: the list stamp is set after the DietPi step ended
- And: the outcome is `success` with exit code 0
- And: the apt result line of the summary equals `Package state verified; maintenance completed` with no note

**AC-004: [Maps to FR-002, FR-003, FR-009] Apt changes configuration in a version 2 run**
- Given: a version 2 run in which the fake fingerprint changes during the upgrade, and the updater step changes nothing
- When: the run executes
- Then: no post-run refresh runs during the apt part
- And: exactly one post-run refresh runs after the updater step
- And: the `[post_refresh]` output section follows the updater's output section

**AC-005: [Maps to FR-009] Both apt and the updater change configuration**
- Given: a version 2 run in which the fingerprint changes during the upgrade and again during the updater step
- When: the run executes
- Then: exactly one post-run refresh runs in the whole run

**AC-006: [Maps to FR-006, FR-030] No change, no refresh**
- Given: a version 2 run whose fingerprint never changes
- When: the run executes
- Then: no post-run refresh runs and the `post_refresh` phase is never persisted
- And: the record equals the record the pre-story helper produces for the same inputs

**AC-007: [Maps to FR-007] Unreadable configuration counts as changed**
- Given: a version 2 run whose fingerprint observation after the updater step returns an error
- When: the run executes
- Then: one post-run refresh runs

**AC-008: [Maps to FR-005] Change before the run's own refresh is not a post-run change**
- Given: a version 2 run in which a configuration file changes during recovery, before the run's own refresh step, and nothing changes afterwards
- When: the run executes
- Then: no post-run refresh runs

**AC-009: [Maps to FR-004, FR-013] No updater present**
- Given: a version 2 run with no present updater, and a fingerprint that changes during the upgrade
- When: the run executes
- Then: exactly one post-run refresh runs after the (empty) updater step

**AC-010: [Maps to FR-010, FR-011] Apt part fails before its refresh**
- Given: a version 2 run whose recovery or verification fails before the run's own refresh step, and a fingerprint that differs at the end
- When: the run executes
- Then: no post-run refresh runs and the summary carries no post-run refresh note

**AC-011: [Maps to FR-011] The run's own refresh fails**
- Given: a version 2 run whose own refresh step fails
- When: the run executes
- Then: the refresh command is run exactly once in the whole run (no retry, no post-run refresh)
- And: the summary carries no post-run refresh note

**AC-012: [Maps to FR-012, FR-013, FR-019, FR-020, FR-021] Upgrade fails after a successful refresh**
- Given: a version 2 run whose own refresh succeeds, whose upgrade fails with exit 100 after the fingerprint changed, and whose updater is therefore skipped with `apt_not_successful`
- When: the run executes
- Then: exactly one post-run refresh runs after the updater step
- And: the outcome is `error`, the apt outcome is `error`, and the exit code is 100
- And: the updater result equals the result the pre-story helper records for the same inputs

**AC-013: [Maps to FR-013, FR-021] Updater fails**
- Given: a version 2 run with apt success, and a fake DietPi step that changes a `preferences.d` file and exits 1
- When: the run executes
- Then: one post-run refresh runs after the step
- And: the outcome is `error` with exit code 1
- And: the DietPi result (outcome `failed`, exit code 1, versions, note) equals the result without this story

**AC-014: [Maps to FR-013] Updater times out**
- Given: a version 2 run whose fake DietPi step changes a configuration file and then exceeds its step limit
- When: the run executes
- Then: one post-run refresh runs, provided the FR-015 minimum remains
- And: the DietPi outcome stays `timed_out`

**AC-015: [Maps to FR-013, FR-021] Updater refused**
- Given: a version 2 run with apt success, a fingerprint change during the upgrade, and DietPi skipped with a refusal reason (for example `dpkg_locked`)
- When: the run executes
- Then: one post-run refresh runs
- And: the DietPi result keeps outcome `skipped` and the same reason
- And: the run outcome is the outcome the pre-story helper records for the same inputs

**AC-016: [Maps to FR-014] Apt part interrupted**
- Given: a version 2 run whose apt outcome is `interrupted`, with a fingerprint that differs at the end
- When: the run executes
- Then: no post-run refresh runs and the summary carries no post-run refresh note

**AC-017: [Maps to FR-014] Updater interrupted by a service stop**
- Given: a version 2 run whose service context ends during the DietPi step, so the DietPi outcome is `interrupted`
- When: the run finalizes
- Then: no post-run refresh runs and the summary carries no post-run refresh note

**AC-018: [Maps to FR-015, FR-016, FR-019] Budget nearly exhausted**
- Given: a version 2 run with a configuration change, and less than 7 minutes of the extended budget left when the updater step ends
- When: the run finalizes
- Then: no refresh command runs after the updater step
- And: the summary's apt result line ends with `; post-run package list refresh skipped: worker time budget nearly exhausted`
- And: outcome, exit code and apt outcome equal those of the same run without a configuration change

**AC-019: [Maps to FR-015] Budget boundary**
- Given: two otherwise identical version 2 runs with a configuration change, one with exactly 7 minutes of extended budget left after the updater step and one with 6 minutes 59 seconds
- When: each run finalizes
- Then: the first runs the post-run refresh
- And: the second skips it with the FR-016 note

**AC-020: [Maps to FR-017] Refresh limit**
- Given: a version 2 run that performs a post-run refresh
- When: the fake runner receives the refresh command
- Then: the time limit it is given is at most 5 minutes and ends before the extended budget ends

**AC-021: [Maps to FR-018] Phase not persisted**
- Given: a version 2 run with a configuration change whose store fails when persisting the `post_refresh` phase
- When: the run finalizes
- Then: no refresh command runs after the updater step
- And: the apt result line ends with `; post-run package list refresh skipped: state not persisted`

**AC-022: [Maps to FR-019, FR-022, FR-026, FR-027] Refresh fails after full success**
- Given: a version 2 run with apt success, DietPi success, a configuration change, and a fake post-run refresh that fails (and reports itself interrupted)
- When: the run finalizes
- Then: the outcome is `success`, the exit code is 0, and the interrupted flag is false
- And: the apt result line is `Package state verified; maintenance completed; post-run package list refresh failed: <reason>`, with the reason at most 200 bytes

**AC-023: [Maps to FR-026, FR-028, BR-006] Note after an apt failure**
- Given: the run of AC-012 with a post-run refresh that fails
- When: the run finalizes
- Then: the apt result line is the apt failure text, cut to 1,500 bytes, followed by `; post-run package list refresh failed: <reason>`
- And: the server's run classification for this record equals its classification for the same record without the note

**AC-024: [Maps to FR-023] Stamp failure is output only**
- Given: a version 2 run whose post-run refresh succeeds and whose fake list stamp returns an error
- When: the run finalizes
- Then: the output contains a `[lists_stamp]` line
- And: the outcome and summary equal those of the same run with a successful stamp

**AC-025: [Maps to FR-025, FR-029, NFR-001] Output and summary bounds**
- Given: a version 2 run whose output is already near `MaxOutputBytes`, whose post-run refresh produces 64 KiB of output, and whose refresh fails with a 1 KiB error
- When: the run finalizes
- Then: the output is at most `MaxOutputBytes`, carries the truncation marker, and ends with the `[post_refresh]` content
- And: the summary is at most 4,096 bytes, and the note's reason is at most 200 bytes

**AC-026: [Maps to FR-031, NFR-003] Same refresh command**
- Given: a version 2 run that performs a post-run refresh
- When: the fake runner records its calls
- Then: the post-run refresh call has the same program and arguments as the run's own refresh call

**AC-027: [Maps to FR-021, FR-030, NFR-004] Record remains valid**
- Given: each terminal record produced in AC-003 through AC-025
- When: the record is validated and parsed with the existing record rules
- Then: validation succeeds

## 9. Out of Scope

- Any change to version 1 runs, including refreshing after a failed upgrade in a version 1 run (see OQ-001).
- Any change to the freshness rule (PATCH-FLOW-001 FR-029), its locations, or the badge text.
- Stopping DietPi from rewriting `preferences.d`, or treating DietPi-managed files specially.
- Any change to `ExtendedWorkerBudget`, `ExtraStartThreshold`, step limits, lock waits or the service backstop (see OQ-002).
- Retrying a failed run's own refresh, or retrying a failed post-run refresh.
- Agent, server, dashboard, database, sudoers or systemd changes.
- Delivering the new helper without a root installer re-run (for example, through agent self-update).
- Changing how a worker killed during the post-run refresh is recovered. The existing recovery path marks the record `interrupted`, as it does today.

## 10. Assumptions

- **A-001:** The check runs after the extra-updater step even when that step failed, timed out or was refused, because the updater may have rewritten sources before it stopped (Ryan's stated likely answer, adopted as BR-003).
- **A-002:** "Succeeded far enough to have refreshed" means the run's own refresh step completed successfully (FR-010). An apt failure after that point keeps the run eligible (FR-012).
- **A-003:** After any interruption (apt or updater, or a service stop), no post-run refresh runs and no note is added. The interrupted summary already directs Ryan to review the host by hand (BR-004).
- **A-004:** A successful post-run refresh adds no summary note, as today. The `[post_refresh]` output section is the visible trace.
- **A-005:** In the worst case of a version 2 run (apt part near its 30-minute budget, DietPi using its full lock wait and 40-minute step limit), less than 7 minutes may remain, and the refresh is skipped with the FR-016 note. The badge then shows "Lists out of date" until the next refresh. This is accepted, and budgets are not changed.
- **A-006:** The `riot-maintenance` helper reaches a device only through a root re-run of `install.sh` on that device. Agent self-update does not deliver it.
- **A-007:** The server's run classification reads the apt result line by its leading success text, so a note appended after it does not change the classification. Today's success-with-note summaries already rely on this. The architect confirms it (Dependency D-004).

## 11. Open Questions

Non-blocking. Each carries Clod's recommended default.

- **OQ-001:** Should version 1 runs also do a post-run refresh after an upgrade that fails once the refresh has succeeded, matching FR-012? *Default: no. Ryan asked for version 1 to stay exactly as it is. Raise this as a separate story if wanted.*
- **OQ-002:** Should `ExtraStartThreshold` reserve the 7 minutes the post-run refresh needs, so that the worst case in A-005 cannot skip it? *Default: no. Typical DietPi runs take minutes (PiCUPS: about 2 minutes), the skip is visible in the record, and changing the threshold makes DietPi refusals more likely.*

## 12. Dependencies

- **D-001:** PATCH-FLOW-001 FR-029..FR-031 (freshness rule) and FR-040..FR-045 (post-run refresh), as implemented. FR-040 and FR-041 are amended for version 2 runs only.
- **D-002:** EXTRA-UPD-001 (version 2 runs, extra-updater step, extended budget, record version 2 fields, summary layout) and DIETPI-PATH-001 (skip, refusal and outcome rules).
- **D-003:** `internal/maintenance/golden_v1_test.go` as the version 1 regression oracle.
- **D-004:** Server-side run classification that reads `SuccessSummary` from the apt result line (DIETPI-PATH-001 DP-SEC-004). The architect confirms it matches by leading text and is unaffected by an appended note.
- **D-005 [Deployment requirement]:** The fix ships in the `riot-maintenance` helper, which is installed by `install.sh`. After the release, Ryan must re-run `install.sh` as root on each DietPi host that runs version 2 patches (PiCUPS, and PiVPN once it is opted in). Until then, that host keeps today's behavior. Agent, server and dashboard need no deploy beyond the normal release.
- **D-006 [Post-deploy verification, not a unit-test criterion]:** After the installer re-run on PiCUPS, the next DietPi patch must leave the record showing a `[post_refresh]` section after the DietPi section. The agent's next updates report must show package lists `fresh`, not "Lists out of date".

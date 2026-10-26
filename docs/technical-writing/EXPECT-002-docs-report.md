# Documentation Report

**Story ID:** EXPECT-002 (agent self-update as expected activity)
**Author:** Technical Writer Agent (Sonnet 5.5)
**Date:** 2026-10-26
**Branch:** `feat/expect-002`, reviewed at `0fb544a`, base `c55e4bd`

## Scope

Both audiences are affected.

- **Users and administrators:** the Devices page label, the command history record, the alert behaviour, the abuse budget and the residuals.
- **Developers and integrators:** the new events endpoint, the new `agent_to_version` field, the new `agent_self_update` command origin, the three event types, migration `000034`, the rollback effect and the test commands.

The senior_dev had already written the README section, the API reference rows and the CHANGELOG entries (commit `50f51b4`) and the AC-030 test. This pass reviewed them against the final code and ADD rev 2, then corrected and completed them. Nothing was duplicated.

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | See "README changes" below |
| `CHANGELOG.md` | Updated | Restructured the `[EXPECT-002]` entries; see "CHANGELOG changes" below |
| `docs/technical-writing/EXPECT-002-docs-report.md` | Created | This report |
| Inline comments | Reviewed, no change | Every comment added by the story states a why (an AD, FR or C reference, a security property or a bound). None restates the code. No TODO, datestamp or commented-out code in the diff. No source file was touched |
| `internal/server/expect2_docs_test.go` | Not edited | Passes against the final README and CHANGELOG |

### README changes

All edits are in the "Agent self-updates" section unless stated.

| Area | Change | Verified against |
|---|---|---|
| Devices-page table row | The label clears "when the window ends" (registration on the new version, failure or the 10-minute bound). It said "when the device registers again", which is wrong for a failure or a timeout | `expect/selfupdate.go` `evaluateSelf` |
| Expected Activity intro | Added a pointer: one window does not come from a command | n/a |
| Recognition | Replaced the unverifiable "every release since 2026-03-06" with "every agent since v2.10.0". The start message form is present in tag v2.10.0 (`git tag --contains 41a20bc`) | `git log -S`, `internal/agent/updater.go:200` |
| Command history | Added what the read-only record shows: versions, actor phrase, the five outcome labels, result text, linked events, the 50-agent-event limit with the "N more agent events not shown" count | `web/src/components/SelfUpdateDetails.tsx`, AD-009 |
| Self-update or fleet command | New subsection. A command in flight or finished at most 10 minutes earlier owns the start and keeps its actor. A queued or never-sent command does not capture a start. A command dispatched while a record is open does not conflict (no target keys). The latest dispatch sets the label | `db/self_update_repo.go` step 1, AD-002, FR-028 |
| Bounds | New subsection: one open record per device, 6 per 24 hours, the cap warning (at most once per device per 24 hours, with its text), the stuck record at bound plus 1 hour (event, log warning, text, retired by the next valid start), one outcome event and one escalation per record | `db/self_update_repo.go`, `expect/selfupdate.go` `writeSelfUpdateStuck`, `models/self_update.go` message renderers |
| How the window ends | Added that a completion after the registration records `succeeded` and does not extend the window | `commands/self_update.go` `noteOutcomeEvent` |
| Outcomes | Added the `Returned, confirming` state and when it becomes `succeeded` | `selfUpdateResolution` |
| Escalation | Made it a separate paragraph. Added the failed-variant label `Agent update <from> → <to> failed: offline`, the failed-variant message with a full example, and the shared rule and limit | `expect/escalate.go`, AD-009, AD-012 |
| Dashboard | "until the window ends" instead of "until the registration" | as above |
| Residuals | Added QA N7 (a second start within 10 minutes of the first record is linked to it, not suppressed, so back-to-back releases alert on the second restart, even after the device returned). Added the R-3 residual (a fleet command's completed or failed event within a record's 10 minutes is attributed to the record). Added the fail-open behaviour for store and resolve failures (R-2). INFO-6 was already present and was kept | QA report N7; ADD residuals R-2, R-3, R-6; `db/self_update_repo.go` step 3 |
| Server only, any agent | Describes what migration `000034` adds and what the down migration does, including that the `agent_self_update_*` events keep a dangling `command_id` | ADD 5.2 |
| New fields and endpoints | Split into a list. Added the three event types (not routed through alert rules, never sent to a channel) and the `outcome` value of `command_attr` | `expect/selfupdate.go` `recordSelfUpdateEvent` (`rule: nil`) |
| Behaviour changes for every device | New subsection for X-20 (a re-registration refreshes `last_heartbeat`, why, and that it suppresses nothing) and the AD-015 severity clamp | `handlers/handlers.go` `RegisterDevice`, `models.ClampAgentUpdateSeverity` |
| Expected Activity "Behaviour changes" and "Upgrade notes" | One bullet each, linking to the section | n/a |
| Known limits | The agent-own-window bullet now points to the self-update budget | n/a |
| Testing | Added `go test ./... -run TestEX2_ -count=1` and the web command `cd web && npx vitest run -t "EXPECT-002"`, and one sentence on what the safety scan guarantees. Both commands were run and pass | run on this host |

Already correct and kept: the recognition grammar (256 bytes, the arrow, the version form), the outcomes table and its message examples (checked against the renderers in `models/self_update.go`), the abuse-budget paragraph (57 minutes, 6 outages under about 10 minutes, `info` events, escalation at the bound, `warning` without a push, cap of 6 and the 5-a-day reasoning), the API rows, the "Command events" reference (checked against `handlers/command_events.go` and `models.CommandEvents`), the `agent_to_version` row and the `origin` row.

### CHANGELOG changes

| Change | Reason |
|---|---|
| The re-registration entry (X-20) and the severity-clamp entry moved from `### Added` to `### Changed`, with X-20 marked as a behaviour change for all devices | They change existing behaviour. They are not new capabilities |
| The Added entry gained the N7 sentence and says the label lasts "until the window ends" | Accuracy |
| The event-types entry says the events are never sent to a channel, and when `agent_self_update_paused` is stored (cap and stuck record) | Completeness |
| The single upgrade note became three: server-only (no agent upgrade, no agent configuration change, no `install.sh` re-run, works with already-deployed agents v2.78.0 to v2.80.0 and every agent since v2.10.0), migration `000034` (what it adds, validates instantly), and **rollback deletes the self-update history** (records, their markers, dangling `command_id` on the events) | FR-033, ADD R-5 |

The AC-030 test asserts these phrases and they are all kept.

## Verification

| Check | Result |
|---|---|
| `go test -count=1 ./internal/server/` with `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp` | `ok` (includes `TestEX2_AC030_ReadmeAndChangelog`, `TestEX_AC048_*`) |
| `go test ./... -run TestEX2_ -count=1` (the command now in the README) | all packages `ok` (PostgreSQL tests skip without `RIOT_TEST_PG_URL`) |
| `cd web && npx vitest run -t "EXPECT-002"` (the command now in the README) | 6 files, 136 tests passed |
| First run after my edits | `TestEX2_AC030` failed on two phrases I had reworded ("never sent to an agent", "deletes the self-update records"). I restored both phrases and it passed. No test was edited |

Not run: the PostgreSQL-gated tests and `-race` (not needed for documentation). QA ran both.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 18 | `README last updated for v2.38.0`. Tags exist up to v2.81.0 | Update in isolation, or remove the line and rely on the CHANGELOG |
| `README.md` line 12 | "relatively stable at version 2.24 ... If you installed prior to v2.24" in the header note | Review whether the warning still applies at v2.81 |
| `README.md` "Testing" / development section | "Requires Go 1.24+ and Node.js 20+". The files pin `go 1.24.0` (`go.mod`) and `node:20-alpine`, `golang:1.24-alpine` (`Dockerfile`). There is no `.nvmrc` or `.tool-versions` | State the exact pins, or add an `.nvmrc` |
| `CHANGELOG.md` | The file has one heading, `## [Unreleased]`, 367 lines long. Tags v2.79.0, v2.80.0 and v2.81.0 shipped, and entries for EXPECT-001 and EXTRA-UPD-001 (both released) still sit under `[Unreleased]`. The file says versions correspond to git tags, but no version headings exist | Cut version sections from the tags. This needs the release owner to say which entry shipped in which tag |
| `CONTRIBUTING.md` | Does not exist. Development setup, the test commands, the `GOCACHE` / `GOTMPDIR` build environment used on Windows and the tag-per-code-change release rule are scattered across README sections and the maintainer's notes | Create it |
| `requirements.md` | `Version: 0.1.0-draft`, `Status: Phase 1 Scope`, `Last Updated: 2026-03-03`. It predates most of the product | Mark it historical, or refresh it |
| `README.md` top level | No single "Project Structure" section. The key directories (`internal/server/expect`, `internal/server/commands`, `cmd/riot-server/migrations`, `web/src`) are not mapped | Add a short structure map |

## Accuracy Flags

Discrepancies between the ADD, the FRD, the QA report and what the code does.

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The senior_dev README said label "clears when the device registers again". The code clears it when the window ends, including at a failure and at the bound | `README.md` Devices-page table, `expect/selfupdate.go` `evaluateSelf` | "when the window ends" |
| The senior_dev README said agents send the start message in this form "since 2026-03-06". The date is the commit date, not a release. The form is in tag v2.10.0 | `README.md`, `CHANGELOG.md` | "every agent since v2.10.0" |
| ADD AD-013 and `db/self_update_repo.go` line 25 state that rows leave with "90 d retention". The only `90` found is in the tuple of the command purge in `db/docker_retention.go:29`. I did not confirm whether it is a default or a fixed value | ADD AD-013, code comment | Not stated in the README. Needs a developer to confirm before anyone documents a retention period |
| ADD AD-008 says the handler passes `SafeName(reg.Hostname)` to `NoteSelfUpdateRegistration`. The handler passes `reg.Hostname` and the message renderer applies `SafeName` (`models.selfUpdateHost`) | `handlers/handlers.go`, `models/self_update.go` | No user-visible difference. Not documented |
| ADD does not mention that the rate-cap lookup also requires `command_id = ''`, and that the stuck-record event is written when any step fails one hour after the bound, not only steps 2 and 3 (impl report deviations 4 and 5) | `db/self_update_repo.go`, `expect/selfupdate.go` | README states the observable behaviour: at most one cap warning per device per 24 hours, and a stuck record is any record that cannot be completed |
| `docs/qa/EXPECT-002-qa-report.md` N8: no test asserts that `server.Start` wires `SelfUpdates` and `SetSelfUpdateResolver`. Without them every record gets the stuck-record warning an hour after its bound and no outcome event is written | `internal/server/server.go:253-261` | Not documentation. Recorded here so a developer can add the one-line assertion |
| QA N2: `internal/server/events/generator_test.go` is not in the AC-029 Go scan. Commit `0fb544a` has since added it | n/a | Closed |

## Notes for Future Writers

- **One source of truth per topic.** The README holds the user-facing description under `### Agent self-updates` (inside `## Expected Activity`) and the API reference under `### Command events` and the activity and command field tables. The CHANGELOG entries link to the README section. Do not copy message texts into other documents.
- **The AC-030 test pins phrases.** `internal/server/expect2_docs_test.go` and `expect_docs_test.go` assert exact substrings of the README section and the `[Unreleased]` CHANGELOG entries. Rewording a sentence can fail a test. Run `go test -count=1 ./internal/server/` after every README or CHANGELOG edit. The phrase groups are listed in the test, and the failure message names the missing phrase.
- **The CHANGELOG is one `[Unreleased]` section.** Entries use `### Added`, `### Changed`, `### Upgrade notes`, `### Fixed` and others. Story IDs prefix every entry.
- **Message texts come from `internal/models/self_update.go`.** Copy examples from the renderers, not from the FRD. The ADD (AD-012) lists the templates and they match.
- **Wording that must stay consistent.** `self-update record` (never "self-update command"), `by agent auto-update`, `Updating agent <from> → <to>`, and "window" for the expected-activity period.
- **Arrows.** The `→` (U+2192) appears in the agent's real message. A Bash heredoc on this host can mangle it. Use the Edit and Write tools for documentation text.
- **The abuse budget is a decision made on Ryan's behalf** (security review SEC-005, ADD AD-010 and "Open points"). The README states it in the terms the review fixed: about 57 minutes per rolling 24 hours, as at most 6 outages of under about 10 minutes each. Keep those terms if the cap or the bound changes, and change the test with them.

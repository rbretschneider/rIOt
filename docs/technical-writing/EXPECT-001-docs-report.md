# Documentation Report
**Story ID:** EXPECT-001 (expected-activity windows)
**Author:** Technical Writer Agent
**Date:** 2026-10-26
**QA verdict at start:** PASS WITH NOTES

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | New `## Expected Activity` section: how a window works, the 12 actions with settle periods and bounds, what is suppressed and recorded, what always alerts, escalation rules and caps, the push ceiling and the operator display name, the powered-off state, dashboard labels and the "Expected offline" count, restart grace and series bound, fail-open and bounds, known limits (NFR-011, ADD R-2 to R-6, INFO-1), behaviour changes (QA-8), upgrade and rollback notes |
| `README.md` | Updated | New `### Activity endpoint` reference (`GET /api/v1/activity`: auth, fields, errors, example); API table row; `activity_update` row in the WebSocket table |
| `README.md` | Updated | Features bullet; pointer under Alerting; Command Lifecycle cross-link; "Real-time Container Events" no longer says only OOM kills alert (container deaths alert too, except an expected death) |
| `README.md` | Updated | Testing: single-test commands, passing output, and the `RIOT_TEST_PG_URL` / `RIOT_TEST_PG_ALLOW` rules for PostgreSQL-gated tests (they existed, but were undocumented) |
| `CHANGELOG.md` | Updated | `[Unreleased]`: Added (windows, escalation, powered-off state, labels, endpoint and WebSocket message, restart grace, two event types, fleet actor), Changed (counter removed, new suppressed actions, old 2-minute rule removed, online-event text, `warning` devices now go offline, 2-minute grace, two retired tests), Upgrade notes (server only: no agent upgrade, no `install.sh` re-run; migration 000033; rollback note) |
| `internal/server/expect_docs_test.go` | Created | AC-048: `TestEX_AC048_ReadmeDescribesExpectedActivity`, `TestEX_AC048_ChangelogSaysNoAgentUpgrade`, `TestEX_AC048_ChangelogEntriesUseTheExpectedHeadings`. The action list comes from `models.ExpectActions()`, so a new action cannot ship without a README entry. Mutation check: removing "105 seconds" from the README made the first test fail |
| `docs/technical-writing/EXPECT-001-docs-report.md` | Created | This report |

Not changed, on purpose:

- `web/src/pages/LearnMore.tsx` is a security-hardening teaching page. It says nothing about alerts, offline behaviour or expected activity, so this story makes none of it stale. Its tests are untouched.
- Inline code comments. The new code already carries the why: FR, AD and SEC references on every bound, the INFO-5 clock note at `offlineAt` in `generator.go`, and the lock rules in `tracker.go`. A grep of the story's diff found no TODO, FIXME, commented-out code or date-stamped comment. No comment was added, deleted or edited, and no behaviour changed.
- `CONTRIBUTING.md` does not exist (see below). The dev workflow did not change in a way that needs one: no new environment variable, tool or CI step.

## Audience

- **Developers** (extending or debugging windows, deploying): the README section, the activity endpoint reference, the Testing additions, and the CHANGELOG upgrade note.
- **Users and operators** (reading the dashboard, tuning alert rules): the README section's dashboard, escalation and known-limits parts. They are written in outcome terms and name no internal type.

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md`, Command Lifecycle > "Queued TTLs and execution deadlines" table | The "Other" row (15 minutes) lists `docker_stop` and `docker_restart` and implies every other action is 15 minutes. `docker_stack_*` actions have a 35-minute execution deadline and `docker_remove_items` has 10 minutes (`ExecDeadline` in `internal/models/command_policy.go`). Neither appears in the table | Add rows for the stack actions and `docker_remove_items`. The new Expected Activity section quotes the stack deadline, so the two pages now disagree until this is fixed |
| `README.md` (whole file) | No `Requirements`, `Getting Started`, `Project Structure` or single `Environment Variables` section in the shape the documentation standard asks for. Versions appear only as "Requires Go 1.24+ and Node.js 20+" under Building from Source. The Dockerfile pins `node:20-alpine` and `golang:1.24-alpine`, and `go.mod` says `go 1.24.0`; there is no `.nvmrc` or `.tool-versions` | Restructure in its own story and pin the versions |
| Repository root | No `CONTRIBUTING.md`. The project has a documented release and test flow only in `CLAUDE.md` and README sections | Create one when a second contributor joins |
| `README.md`, Command Lifecycle > "Where you see this" | "Container tiles. While an update is in flight, stop and die events do not flip the tile to `exited`." This is a browser-side display rule from CMD-LIFECYCLE-001. It is separate from the server-side suppression of this story, and it is not verified here. EXPECT-005 will change per-container state in the Docker views | Re-check against the web code when EXPECT-005 lands |
| `README.md` line count and structure | The file is about 3,200 lines. Several features sit in one file with long single-line paragraphs | Consider splitting reference material into `docs/` and keeping the README as the entry point |
| `docs/implementation/EXPECT-001-impl-report.md` and the QA report | They quote the AC-047 scan as 33 and 34 files. The scan computes its set from `TestEX_`, so `expect_docs_test.go` makes it one larger. The figure is not a defect | Read the current figure from the test's `-v` output |

## Accuracy Flags

Discrepancies between the ADD or FRD and what the code does. Documented as the code behaves.

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| ADD AD-014 says labels use the browser locale. The web code uses the app's UTC/Local preference. Event messages use the server's local time | `web/src/utils/activity.ts` (`formatChartTick`), `internal/models/expect.go` message templates (`time.Local`); impl report W1 | README: labels follow the UTC/local switch; event message times use the server's local zone, so they can differ |
| FRD FR-035 says "Rebooting (waiting for dpkg lock)" appears while the command waits. No released agent reports the `wait_dpkg_lock` step, so the label never appears | `models.StepWaitDpkgLock` is defined and read by the server; nothing in `internal/agent` sets it (grep) | README and CHANGELOG say the label appears only when an agent reports that step, which current agents do not. Reboots show `Rebooting` |
| FRD FR-038 says the dashboard shows an "Expected offline" count. The card shows only when the count is above zero | `FleetOverview.tsx` line 195 (impl report W4) | README: "The card appears only when its count is above zero" |
| FRD FR-018 has no fallback for a session with no display name. The code says `by an operator` | ADD X-2, `models.ActorPhrase` | README: "A command from a session without a display name reads `by an operator`" |
| ADD AD-012 says suppressed events replace today's warnings "1:1". Today a rule cooldown stores at most one warning per cooldown, and a suppressed death reads no cooldown, so every suppressed death that reaches the generator stores one info event | QA-3, `events/generator.go` covered branch | README "Safety and bounds": volume is limited upstream by one Docker event per container and action per minute per device |
| ADD AD-009 says one tick completes in at most 8 seconds. One `finishWindow` can chain several 5-second database calls, so a tick on a hung database can run longer. The effect is fail-open readiness, which is the safe direction | QA-2, `expect/tick.go`, `expect/finish.go` | README states the fail-open rule (no completed cycle in 15 seconds means alerts as before) and makes no 8-second claim |
| ADD AD-006 and S7: a `warning` device now goes offline, and a `warning` device that registers again raises no `came online` event | `internal/server/offline.go`, `handlers.go` `RegisterDevice` | README "Behaviour changes" and CHANGELOG Changed (QA-8) |
| QA-1 (a millisecond race between the offline detector and a reconcile can briefly clear a powered-off row or an escalation label; self-heals within 30 seconds; direction is safe) | `expect/tracker.go` `mergeLocked` | Not user-facing. Left to the follow-up the QA report names. Not in the README |
| `NewGenerator` keeps an unused `commandRepo` field and parameter (S1, QA-6) | `events/generator.go` | Not documented. Dead state, no behaviour. Follow-up cleanup |
| `idx_events_command` and the 24-hour lapse are described in the ADD only | ADD AD-009, AD-012 | README documents the 24-hour deferral bound and the `expect_backstop_skipped` warning; the index is an implementation detail |

## Verification

| Check | Result |
|-------|--------|
| `go test ./internal/server/ -run EX_AC048 -v -count=1` | 3 tests PASS |
| Mutation: removed "105 seconds" from the README | `TestEX_AC048_ReadmeDescribesExpectedActivity` FAIL with the missing term named. The README was then restored |
| README anchors `#expected-activity`, `#activity-endpoint`, `#time-display`, `#command-lifecycle` | Each heading exists |
| README facts against code | Policy table and bounds from `internal/models/expect.go`; deadlines from `ExecDeadline`; messages from `models/expect.go` and `expect/messages.go`; activity fields from `models.DeviceActivity` JSON tags; the 503 body from `handlers/activity.go`; the 401 for a device key from `TestEX_Activity_RouteRequiresAnAdminSession` |
| Full suites | See the final report of the run (`go test ./...`, `cd web && npx vitest run`) |

## Notes for Future Writers

- The README is one long file, and the working tree uses CRLF (`core.autocrlf=true`). A shell `sed -i` on Windows rewrites the working-tree file to LF. Git normalises it, so `git diff` stays small, but check `git diff --stat` after a scripted edit.
- Doc tests live next to the code they guard: `scripts/shutdown_grant_docs_test.go` (installer docs), `internal/server/docs_test.go` and `internal/server/expect_docs_test.go` (server docs). They reuse `readRepoFile`, `readmePath` and `changelogPath` from `docs_test.go`. A README or CHANGELOG rewording can break them on purpose: read the failure message, which names the missing term.
- The AC-047 safety scan reads every `*_test.go` file that contains `TestEX_`. Doc tests that carry that prefix are scanned too. A string literal there must not contain the power-command words or a `riot-agent` path.
- CHANGELOG entries are long single-line bullets under `[Unreleased]`, tagged `[STORY-ID]`. Keep that format. Put server-only upgrade facts under `### Upgrade notes`.
- The `README.md` section for a feature that spans server, web and API is one `##` section with `###` parts. Cross-link it from Features, Alerting and any feature it changes.
- Event texts in the README (`Container plex died during Docker update by ryan (expected)`) are copied from the templates in `models/expect.go` and `expect/messages.go`. If a template changes, change the README and the doc test terms together.

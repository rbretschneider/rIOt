# Documentation Report
**Story ID:** EXPECT-007 (agent-side activity reporting)
**Author:** Technical Writer Agent
**Date:** 2026-10-27
**QA verdict:** PASS WITH NOTES
**Branch:** `claude/expect-007-473e29` (HEAD `434fada` plus this documentation commit)

## Audience inventory

| Audience | Affected | Where documented |
|---|---|---|
| Users (Ryan, operators) | Yes. The dashboard now shows `Rebooting (waiting for dpkg lock)` and `Shutting down (waiting for dpkg lock)`, and the command history names two new steps. | README "Agent-side activity reporting", "What you see" in "Reboots and the dpkg lock", the dashboard-labels list under "Expected Activity" |
| Integrators and agent authors | Yes. The agent event route gained four optional fields and a `duplicate` reply. | README "Agent events" (new), "Agent" API table (new row), CHANGELOG |
| Developers extending or debugging | Yes. New agent files, server linking rules, retry bounds. | Inline comments, README "Agent-side activity reporting", the story's ADD |
| Deployers | Release and upgrade facts only. No migration, no `install.sh` re-run, no config. | README "Upgrade", CHANGELOG |

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| README.md | Updated | Release number set to "the first release after v2.88.0". Added a paragraph that states an `installer`-origin row is never a valid command reference, never matches an attempt ID and is never picked by timing, and a cross-reference from "Installer reinstalls". Qualified "the only restarts rIOt starts" so it no longer contradicts the installer section. Added the rule for an attempt ID no record owns, and that only a start's versions are used. Corrected "dispatched and is running" to "dispatched and has not finished". Said that `agent_update_available` is still sent once. Reworded the `Failed at wait_dpkg_lock` and `power_action` step descriptions to match the code. |
| README.md | Stale text fixed | "Current agents do not report it yet" (Expected Activity, dashboard list). `waiting_dpkg_lock` "Always `false` with current agents" (Activity endpoint). "the agent sends it once, with no retry" and the fleet-command misattribution residual (Agent self-updates, residuals). `command_attr` values (Agent self-updates, new fields). Added a pointer from the self-update timing rule to the reference. |
| README.md | Created section | "Agent events": `POST /api/v1/devices/:id/events` request fields, validation, every status code and body, the `duplicate` reply, rate and size limits, and a worked request and reply. The route was not in the API tables before. Added its row to the "Agent" table. |
| README.md | Cross-reference | "Dashboard label" paragraph in "Reboots and the dpkg lock", "What you see". |
| CHANGELOG.md | Updated | Two "Changed" entries (event route fields and `duplicate` reply; `waiting_dpkg_lock` and `command_attr` `agent`). Clarified that EXPECT-007 itself adds no migration, because `[Unreleased]` also holds EXPECT-008's migration 000035. |
| internal/server/expect7_docs_test.go | Test text | The expected phrase changed from "the first release after v2.85.0" to "the first release after v2.88.0" so the test keeps matching the README. No logic change, no forbidden literal added. |
| internal/models/events.go | Inline comment | `AgentEvent` doc said `CommandID` is set only on the extra-updater types. It is now also the update-event reference. Listed which fields go with which types. |
| internal/models/expect.go | Inline comment | Why `DeviceActivity.WaitingDpkgLock` requires an online device and a non-terminal command. |
| internal/agent/updater.go | Inline comment | `reportUpdateEvent` now serves only `agent_update_available`. Said where the other three events go. |
| internal/agent/commands.go | Inline comment | Two `rep.leave()` comments said "after the power action started". The call runs after the exec call returns, success or not. Comment now says so (see Accuracy Flags). |
| internal/server/handlers/handlers.go | Inline comment | `ReceiveAgentEvent` doc now covers the reinstall notice and points to the README. |
| internal/server/commands/self_update.go | Inline comment | The EXPECT-002 "Bounds" paragraph now counts the one read that validates a reference. |
| docs/technical-writing/EXPECT-007-docs-report.md | Created | This report. |

No behaviour changed. The Go edits are comments only, and the one test edit is a string.

## Inline comment review

The new files (`update_events.go`, `power_wait_step.go`, the `powerguard.go` additions) and the new server code (`linkCommandRef`, `startVersions`, `noteOutcomeEvent`, `storeAgentUpdateEvent`, the attempt-match query in `self_update_repo.go`) already explain why, with FR, AD and security-condition references and the bound. Nothing in them restates the code, so nothing was deleted. The gaps were the stale or missing comments listed above.

## Verification

Run from the worktree with `GOCACHE='D:\rIOt_gocache'` and `GOTMPDIR='D:\rIOt_fuzz_tmp'`:

- `go build ./...` passes.
- `go vet ./...` passes.
- `go test ./internal/... -run 'TestEX7_|TestEX2_|TestEX8_' -count=1` passes (agent, models, server, server/commands, server/db, server/expect, server/handlers). `go test ./internal/server/ -count=1` passes in full, including the README and CHANGELOG checks of both stories.
- Formatting: the working tree uses CRLF, so a plain `gofmt -l` lists every file. With the carriage returns stripped (`tr -d '\r' < file | gofmt -l`) none of the seven touched Go files is reported.

Commands in the README's new "Agent events" section were not run against a live server. The status codes, bodies and limits were read from `router.go`, `ingest/limits.go`, `middleware/auth.go` and `handlers.go`.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| README.md L17 | "README last updated for v2.38.0". The project is at v2.88.0. | Remove the line or update it as part of each release. |
| README.md L12 | Notice about stability "at version 2.24" and installs before v2.24. | Review whether it still applies and rewrite it. |
| README.md "Agent" API table | Lists no row for `POST /api/v1/devices/:id/logs` or `/probe-results`. Both routes exist (`router.go`). | Add the rows in a documentation pass. |
| README.md "Building from Source" (L2370) | "Go 1.24+ and Node.js 20+". The exact pinned versions are `go 1.24.0` (`go.mod`), `golang:1.24-alpine` and `node:20-alpine` (`Dockerfile`), `postgres:16-alpine` (`docker-compose.yml`). There is no `.nvmrc`. | State the pinned versions and add a Requirements section. |
| README.md | No Requirements, Environment Variables, Running Tests or Project Structure sections in the standard order. The content exists but is spread across "Server Environment Variables", "Testing" and "Building from Source". | Add a Project Structure section and a short Requirements section. |
| CONTRIBUTING.md | Does not exist. The project has development workflow rules (tag every code push, `make test`, mock location) that live only in maintainers' notes. | Create it. |
| CHANGELOG.md | `[Unreleased]` holds entries for many shipped versions (for example EXPECT-001, EXPECT-002, EXTRA-UPD-001, DIETPI-PATH-001, TIME-12H-001) that were tagged long ago. The release notes cannot tell a reader which version contains what. | A release pass should move shipped entries under their version headings. |
| docs/ops/os-maintenance.md | Not changed by this story and not re-read in full. It has no section on the dashboard labels for a reboot that waits for the lock. | Check at the next operator-guide pass. |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The implementer's comments and the README said the step `power_action` is queued "after the power action started". `rep.leave()` runs after `execReboot` or `execShutdown` returns, including when it returns an error. The error result that follows ends the command, so the label does not stay stale. | `internal/agent/commands.go` L342 and L426 | README: "queued only after the agent has run the power command, never before". Comments updated to match. Not a bug. |
| The implementer's README said a command reference is valid for a command that "was dispatched and is running or finished at most 10 minutes earlier". The code and the FRD accept any dispatched command that is not terminal, including one that is dispatched but has not yet reported running. | `models.ValidAgentUpdateCommandRef` | README: "dispatched and either has not finished or finished at most 10 minutes earlier". |
| `Failed at wait_dpkg_lock` was described as "when the wait timed out". The code sets the step on any skip after the wait began (timeout, state unknown, guard failure, agent shutting down). | `internal/agent/commands.go`, `handleReboot` and `handleShutdown` | README: "when the command ends without a power action after the wait began (for example when the wait timed out)". |
| The agent sends `from_version` and `to_version` on `agent_update_completed` and `_failed`, but the server reads them on `agent_update_started` only. The earlier README and CHANGELOG wording implied all three were used. | `commands.NoteAgentUpdateEvent`, `noteOutcomeEvent` | README states the server uses the versions of a start only. |
| "An update started as a command never waits" is true (`deliverAsync`). Note also that the command path's signed hand-off completion sends no update event: the command result carries the outcome (`finishSignedHandoff`, command branch). | `signed_update_handoff.go` `finishSignedHandoff` | Not repeated in the README. Noted here so a later writer does not add a "completed event" claim for the command path. |

## Notes for Future Writers

- The README is one long file, and a docs test pins phrases in it. `internal/server/expect7_docs_test.go` and `expect8_docs_test.go` fail the build when a pinned phrase is removed. Before rewording the "Agent-side activity reporting" or "Installer reinstalls" sections, read those tests. Tests that scan for forbidden literals (the EX2 scan) force these tests to assemble words such as the package-lock name from fragments. Do not write those words as plain literals in any test file.
- The release number in the README is written as "the first release after vX.Y.Z" because the coordinating session assigns the tag. When the tag is known, replace the phrase in the README (five places: the dashboard list, the `waiting_dpkg_lock` row, the self-update timing bullet, the residuals bullet, and the "Agent-side activity reporting" section) and in `expect7_docs_test.go` in the same commit.
- The working tree has CRLF line endings under `core.autocrlf`. `gofmt -l` reports every file for that reason. Strip carriage returns before checking format, and let git normalise on commit.
- Terminology: a **record** is a `commands` row with origin `agent_self_update` or `installer`; a **command** is a row that Ryan or a schedule dispatched. The README keeps that split, and the reference rules depend on it.
- API reference now lives in README "API". The route `POST /api/v1/devices/:id/events` is documented once, in "Agent events". Link to it instead of repeating the field list.

# Documentation Report

**Story ID:** CMD-LIFECYCLE-001
**Title:** Reliable command lifecycle and Docker update execution
**Author:** Technical Writer Agent
**Date:** 2026-10-18

Inputs read: FRD, ADD (rev 2), security review, the four implementation reports, the QA report (PASS WITH NOTES), the changed files in `git diff 7ca5042..HEAD`, and the follow-up fix 348137c (the report endpoint now fails closed). Behavior was documented from the code. Where the code and the upstream documents differ, the difference is listed under "Accuracy Flags".

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | New **Command Lifecycle** section: state table with dashboard labels, queued TTL and execution deadline table, one-command-per-target rule, cancel, auto-update cooldown and backoff table, result outbox and HTTP fallback, agent state files, and **Upgrade notes** (rollout order, version skew, migration 000023, FR-008 reset, migration number collisions, consumer-visible changes). **Container Updates** rewritten: detection by running image ID, reasons and badge titles, pinned and failed checks, rename-aside rollback sequence, network dependents, separate pull and `up` limits with SIGTERM grace, restart reconciliation, shutdown behavior. **API** section: new rows in the agent, dashboard, and WebSocket tables; new **Command endpoints** subsection (command object, create and 409 body, bulk update, list filters including `active=true&target_key=`, get, cancel, agent report with every status code); new **WebSocket messages** subsection (`command_update` replacing `command_result`, `command_ack`, `command_progress`, `command_receipt`, `collected_at` and `received_at`). Two Features bullets updated |
| `CHANGELOG.md` | Updated | `[Unreleased]`: 14 Added entries, 15 Fixed entries (one per audit defect, each with its user-visible symptom, plus the fixed security hardening items and the fail-closed report endpoint), 10 Changed entries (two marked **BREAKING**, one upgrade-notes entry), 4 Known Limitations entries. The existing duplicate `### Changed` headings were left alone |
| `internal/agent/docker_update.go` | Inline comment | Added a "why" comment on `networkConfigFrom` (which fields are copied and which are left to Docker) |
| `internal/agent/outbox.go` | Inline comment | Added a "why" comment on `classifyReply` (why a 409 or 404 needs a rIOt error code, why 400 and 413 drop the entry) |
| `docs/requirements/CMD-LIFECYCLE-001-frd.md` | Redaction | See "Redactions" |
| `docs/architecture/CMD-LIFECYCLE-001-add.md` | Redaction | See "Redactions" |
| `docs/security/CMD-LIFECYCLE-001-security-review.md` | Redaction | See "Redactions" |
| `docs/implementation/CMD-LIFECYCLE-001-impl-report-agent.md`, `-server.md`, `-web.md` | Redaction | See "Redactions" |
| `docs/qa/CMD-LIFECYCLE-001-qa-report.md` | Redaction | See "Redactions" |
| `docs/technical-writing/CMD-LIFECYCLE-001-docs-report.md` | Created | This report |

No other inline comments were added. The new code already carries "why" comments with FR, AD, and SEC references. A scan of the new non-test Go files found undocumented functions only where the name states the purpose (accessors, interface adapters, small helpers), so the pass stopped at the two functions above to avoid churn.

No `CONTRIBUTING.md` exists and none was created. See the stale content list. No OpenAPI or GraphQL file exists in the repository, so the README API section is the single source for endpoint documentation.

## Redactions (public repository rules)

The story's own documents were scanned for personal or network specifics, and for descriptive detail about the two deferred security review items (SEC-012 and SEC-015). Technical content about fixed defects was not changed. Each redaction below replaced the text with generic wording.

| File | What changed |
|------|--------------|
| `docs/requirements/CMD-LIFECYCLE-001-frd.md` | Actor table: removed a personal first name from the operator role. Section 11 heading: replaced the personal name with "the product owner" |
| `docs/architecture/CMD-LIFECYCLE-001-add.md` | Section 10 item 11: removed the one-line description of each deferred item (including a first-person agent name); now reads "two deferred hardening items (SEC-012 and SEC-015) are tracked separately as follow-up work". Section 11: a personal host name was replaced with "deployments that already show table bloat". Section 15.2: the two table rows describing the deferred items were merged into one row, "Deferred. Hardening items tracked separately as follow-up work". Section 16: a personal host name in the migration-check advice became "the target database's `schema_migrations` table"; the deferred-items risk bullet is now generic; the personal name in "Open questions" became "the product owner" |
| `docs/security/CMD-LIFECYCLE-001-security-review.md` | SEC-012 finding: the full body (domain, location, mechanism, impact, recommendation) was replaced with a stub that says details are not reproduced in this public document. SEC-015 finding: same treatment (the location, platform behavior, and recommendation were removed). Recommended-items list: the parenthetical descriptions of both items were removed. Asset table: a sentence naming a personal host became a generic statement about deployments with existing Postgres bloat. SEC-013 text: a personal host name removed |
| `docs/implementation/CMD-LIFECYCLE-001-impl-report-agent.md` | QA note about SEC-015 now reads "Deferred hardening items (SEC-012, SEC-015) are tracked separately and are not covered by these probes" |
| `docs/implementation/CMD-LIFECYCLE-001-impl-report-server.md` | A personal host name in the migration-check note became "the target database's `schema_migrations` table" |
| `docs/implementation/CMD-LIFECYCLE-001-impl-report-web.md` | Local worktree path in a vitest log line replaced with `<worktree>/web` |
| `docs/qa/CMD-LIFECYCLE-001-qa-report.md` | Action item 3: removed a personal name and a local drive path for the QA scratch file |

Checked and left unchanged: the Go module path `github.com/DesyncTheThird/rIOt` (the public module name), `alice` and `/opt/alice-app` in the SEC-005 scenario (a hypothetical example), the throwaway Postgres credentials `riot_test` and `pw` on `localhost` (test fixtures), and the word "homelab" (generic).

A final scan of the story documents, `README.md`, and `CHANGELOG.md` found no LAN IPs, hostnames, home directory paths, or personal domains in anything this pass wrote. Two pre-existing hits in `README.md` are listed under stale content.

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` L941 (OIDC section) | Names a personal host in the authentik registration step. The repository is public. This breaks the public-repo rule, but the text pre-dates this story | Replace with a generic phrase in an isolated fix. Treat as a priority because the repository is public |
| `README.md` L1897 (reboot ETA example JSON) | The example `"hostname"` is a real personal host name | Replace with a neutral example such as `web01`, in an isolated fix. Same priority |
| `CHANGELOG.md` (`[Unreleased]`) | Contains `### Changed` twice (the first group and a second group about 60 lines below it). Not touched, per instruction. This story's entries went into the first group | Merge the two groups in an isolated pass |
| `CHANGELOG.md` (`[Unreleased]`) | Entries for shipped work (AGENT-HARDEN-001 shipped in v2.64.0; tags up to v2.65.0 exist) still sit under `[Unreleased]`. The file has no version headings | Cut version sections for the shipped tags and move entries under them |
| `CHANGELOG.md` | The `### Known Limitations (v1)` heading is not part of the Keep a Changelog groups used elsewhere in the file | Decide whether to keep it, and apply the same rule to every limitation entry |
| `README.md` API tables | Existing routes are missing: `PUT /devices/:id/location`, `PUT /devices/:id/tags`, `GET /devices/:id/alert-rules`, `GET /devices/:id/heartbeats`, `GET /devices/:id/logs`, `GET/PUT /devices/:id/auto-updates`, `DELETE /devices/:id/auto-updates/:target`, `PUT /devices/:id/auto-patch`, `GET /devices/:id/security-score`, `GET/PUT /settings/features`, `GET/PUT /settings/server-error-alert`, `GET/PUT /settings/tls/sans`, `PUT /settings/automation` detail | Audit the router against the tables and add the missing rows |
| `README.md` L1118 | Lists `GET/POST /api/v1/settings/logs`. The router registers `GET` and `DELETE` | Correct the method list |
| `README.md` Requirements | Says "Go 1.24+ and Node.js 20+". `go.mod` pins `go 1.24.0`, and the `Dockerfile` uses `golang:1.24-alpine` and `node:20-alpine`. There is no `.nvmrc`. The README has no Requirements, Getting Started, or Project Structure section in the form the documentation standard expects (it uses Server Installation and Building from Source) | Add exact version pins and a Project Structure map in a dedicated pass |
| `README.md` Environment variables | The Server Environment Variables table was not re-verified against code in this pass. This story added no variables to the server or agent (test-only `RIOT_TEST_PG_URL` and `RIOT_TEST_PG_ALLOW` are documented in the CHANGELOG) | Verify the table against `.env.example` and the config code |
| `CONTRIBUTING.md` | The file does not exist. CI now runs a Postgres service, and the PG-gated tests need `RIOT_TEST_PG_URL`, so contributors need a documented test procedure | Create it with setup, `make test`, the PG-gated test command, and the tag-per-code-change release rule |
| `requirements.md` (repository root) | Dated 2026-03-03 and marked "Phase 1 Scope". It predates the command system and says nothing about commands | Decide whether to archive or refresh it |
| `README.md` "Agent Config Reference" | Does not list the agent data directory or the new state files. This story documented them in the Command Lifecycle section only | Cross-reference from the config reference when that section is next edited |
| `web/src/components/ContainerGroup.tsx` | Impl report (web) records that `ContainerGroup.tsx` still has the old 5-second-reset mutation and is not imported anywhere | Delete the dead component or update it, in a cleanup story |

## Accuracy Flags

Discrepancies between the FRD or ADD and what the code does. The code was documented.

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| FRD FR-057 and NFR-008 require seen IDs for "at least 24 h"; the code (`models.SeenMaxAge`) and ADD 5.4 use 48 h | `internal/models/command_policy.go` | 48 hours |
| ADD section 9 says a disallowed agent report gets a WebSocket `invalid_transition` receipt for any report kind. ADD 6.4, the server implementation report (deviation 6), and `terminal.go` send a receipt for `command_result` only | `internal/server/handlers/terminal.go` | Receipt sent after each `command_result` only. Ack and progress get none |
| ADD AD-008 and section 6.3 do not state what `status` holds in a `coalesced` 200 reply. The code returns the current state (`running`) | `internal/server/commands/service.go` | Documented the result value only |
| The QA report (A-1) describes the report endpoint as trusting the URL `{id}` when the request context has no device ID (QA A-1). Commit 348137c removed that fallback; `authenticatedDeviceID` now returns empty and the handler answers 401 | `internal/server/handlers/commands.go` | Documented as fail-closed 401. The QA report was not edited (it is an upstream document), so it still describes the pre-fix state |
| FRD FR-095 gives one failed label, "Failed at <step>: <message>". The code also produces "Failed at <step>", "Failed: <message>", and "Failed" when a field is empty | `web/src/utils/commandLifecycle.ts` | State table shows the full form only |
| ADD AD-014 says the compose verify step requires services to be "running and not restarting". The code checks `State == "running"`, which excludes a restarting container | `internal/agent/docker_update.go` | "running again" |
| The ADD calls the list-filter response for `active=true` a list of "every non-terminal command" without stating order or that `limit`, `offset`, and `action` are ignored. The code ignores them and orders oldest first | `internal/server/handlers/commands.go`, `db/command_repo.go` | Documented both |

## Notes on open QA items

- **A-1 (fixed in 348137c):** documented as fail-closed 401. The README report-endpoint table lists the 401 case.
- **A-2 (open):** the telemetry staleness read and the snapshot insert are not one transaction. Documented as a known limitation in the CHANGELOG, with no user-facing README text because the effect is small and internal.
- **A-3 (info):** a lost heartbeat response leaves a claimed command `dispatched` until the deadline, then `timed_out`. Documented in the README state notes and in the CHANGELOG limitations.
- **N-2 (info, open):** `TestCL_AC028_HeartbeatNeverDeliversExpiredReboot` passes through the expire step first and does not isolate the claim predicate. The predicate is covered by `TestCL_SEC_ClaimSkipsExpiredEvenIfExpireStepFails` and its PG variant. Not a documentation item. Recorded here so the next reader keeps the SEC tests.
- The two security review items deferred by the ADD are mentioned only generically ("deferred hardening items tracked separately") in the CHANGELOG and in this report.

## Notes for Future Writers

- **One section owns the feature.** The README **Command Lifecycle** section is the user-facing home for states, TTLs, cancel, outbox, and upgrade notes. **Container Updates** owns Docker update behavior. **Command endpoints** owns the wire contracts. Add new command-related facts to those three places, not to the Features list.
- **Constants live in `internal/models/command_policy.go`.** The TTL, deadline, step limit, outbox, and seen-store numbers in the README come from that file. Re-check the README tables when any constant changes.
- **State and label strings are mirrored in three places:** `internal/models/command_state.go`, `web/src/utils/commandLifecycle.ts` (labels), and the README state table. Update all three together.
- **Public repository.** This repository is public. Do not write host names, LAN addresses, home paths, or personal domains in any document or example. Do not describe how an unfixed security item can be exploited. Refer to it as a deferred hardening item tracked separately.
- **Line endings.** Working-tree files are CRLF (git `autocrlf`), and the index stores LF. Git prints harmless warnings on commit.
- **Pipeline documents are upstream.** The writer redacts them only for the public-repo rules. Technical corrections go back to the owning role.

# Documentation Report

**Story ID:** AUTOUPD-HARDEN-001
**Author:** Technical Writer Agent
**Date:** 2026-10-19

QA verdict at hand-off: PASS WITH NOTES (round 2). All documentation describes the final behavior, not the original ADD.

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` > Agent Docker management > "Compose directory trust" | Rewritten and extended | Kept the section the senior developer added and made it operator-actionable. Added: what is covered (stack updates, single-service updates, dependent recreate, leftover cleanup); an "After upgrading an agent" runbook (`sudo -u riot riot-agent doctor`, how to read the output, why not root); the requirement list corrected against the code; a **refusal rule table with all 26 rules** (meaning and fix, with generic example paths); a `docker.compose_roots` example; the Windows requirement; a "Multiple working directories" paragraph that states the server never skips a stack; a compatibility table (new agent with old server, old agent with new server, dashboard); the common new refusals after an upgrade; and a shortened residual-risk list. |
| `README.md` > Agent configuration table | Updated | `docker.compose_roots` row: example value, fail-closed behavior for an empty or relative entry, restart requirement, link to the section. |
| `README.md` > Agent Diagnostics | Updated | Added a paragraph: `doctor` checks every Compose project directory and must run as the service user. |
| `CHANGELOG.md` | Updated | Added six `[AUTOUPD-HARDEN-001]` entries under the existing `[Unreleased]` heading, at the top of each subsection, in the file's one-bullet-per-entry style: one **Security** (SEC-002), three **Added** (`docker.compose_roots`, `doctor` compose check, `compose_project` parameter), two **Changed** (the **BREAKING (upgrade note)** entry with the new refusals and the dropped `COMPOSE_*` inheritance; the server-side "never skips a stack" dispatch rule). No version number was invented. CRLF line endings preserved. |
| `internal/agent/docker_update.go` | Inline comments | `updateTarget` doc now says a stack is named by project (or, from a legacy server, by directory); it said "by directory" only. Reflowed the `composeArgs` doc and replaced the unclear "one `=` token" wording. |
| `internal/agent/doctor.go` | Inline comment | `checkComposeTrust` said running as root "is harmless". That contradicts the QA-D2 ruling and `reportPrivilegeMismatch`: root can read paths the agent user cannot, so a pass as root can hide a refusal. Corrected. |
| `internal/agent/proclimit.go` | Inline comment | Reflowed the `newExecProcess` doc and stated what a nil `env` means. |
| `internal/agent/config.go`, `scripts/install.sh` | Sample config comment | The `compose_roots` line now reads identically in the generated `agent.yaml` and in the installer's Docker section: `Directories compose updates may run in, e.g. [/opt/stacks] (required on Windows)`. The `config_test.go` check on `# compose_roots: []` still holds. |
| `docs/technical-writing/AUTOUPD-HARDEN-001-docs-report.md` | Created | This report. |

No logic changed. `go build ./...` and `go vet ./...` pass. `go test ./internal/agent/... ./internal/server/...` passes.

## Artifacts Reviewed, Not Changed

| File | Reason |
|------|--------|
| `internal/agent/compose_trust.go`, `compose_snapshot.go`, `compose_trust_unix.go`, `compose_trust_acl_*.go`, `compose_trust_windows.go` | Read in full. Comments explain why, cite ADD/SEC/QA-D2 identifiers and match the code. The `resolveFallbacks` comment lists the QA-D2 conditions as the code implements them. No stale skip or event wording. |
| `internal/server/handlers/auto_update.go`, `internal/server/commands/targets.go` | The comments describe the final project-only dispatch and the held target keys. No mention of the removed skip path. A search finds no `stack_update_skipped`, `skipLimiter` or `noteStackSkipped` in Go, web or README. |
| `internal/agent/docker_deps.go`, `docker_recovery.go`, `compose_sanitize.go` | Comments accurate. |
| `CONTRIBUTING.md`, OpenAPI / GraphQL schema | None exist. No contributor workflow changed. The README's command documentation did not list `docker_update` parameters, so there was no parameter table to update. |
| `.env.example` | No new server or agent environment variable. |

---

## Operator-Facing Changes (summary)

1. **Compose updates are validated.** The agent no longer runs compose in a directory only because a label says so. Each refusal is a failed `precheck` with `compose directory refused (<rule>)`; nothing is stopped.
2. **Agents must be upgraded to be protected.** An old agent stays unhardened. Old servers work with new agents.
3. **Run the doctor after upgrading, as the service user.** `sudo -u riot riot-agent doctor`.
4. **New refusals are likely on some hosts.** Directory owned by an ordinary user, stack under another container's read-write mount, unreadable `.env`.
5. **Windows needs `docker.compose_roots`.**
6. **The agent's `COMPOSE_*` and `DOCKER_CONTEXT` environment no longer reaches compose.** Put such settings in the project's `.env`.

---

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `docs/architecture/AUTOUPD-HARDEN-001-add.md` AD-011, file table (L543), AC-014 test list (L642), residual risk R-1 (L690) | Still describe the ambiguity skip ("skip an ambiguous project", `..._AmbiguousProjectNotDispatched`, "AD-011's ambiguity skip"). The skip, its event and its tests were removed after QA round 1. The implementation report and QA report round 2 record the change; the ADD does not. | Architect to add an ADD amendment for AD-011, or mark the sections superseded. Not edited: the ADD is the architect's document. |
| `docs/qa/AUTOUPD-HARDEN-001-qa-report.md` round 1 text (L174, L235) | Describes the `stack_update_skipped` event as live. Round 2 supersedes it. This is correct as history. | None, unless the team wants a superseded marker. |
| `CHANGELOG.md` | `[Unreleased]` holds a second `### Changed` (around L199) and a second `### Removed` (around L215) after `### Fixed`, plus a `### Known Limitations (v1)` heading. Older entries are mixed into `[Unreleased]`. | Dedicated changelog pass at the next release: cut a version heading, merge the duplicate subsections. Not touched: reordering history is outside this story. |
| `README.md` line 12 | "relatively stable at version 2.24" banner is old. Carried forward from earlier reports. | Review wording at the next release. |
| `README.md` line 18 | `README last updated for v2.38.0` marker is far behind. Carried forward. | Update at the next release or remove the marker. |
| `docs/technical-writing/AGENT-HARDEN-001-docs-report.md` | Its open items (uninstall residue, `Makefile` target for the verifier, Sudoers Rules table `poweroff` row) were not re-verified in this pass. | See that report. |
| Working tree | `gofmt -l` lists most files in the checkout because `core.autocrlf=true` converts them to CRLF. The index is LF. | None. Run `gofmt` from a Linux checkout. `internal/agent/doctor.go` has one extra blank line before `maxDoctorLine` (cosmetic, comments-only pass). |

---

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The ADD says the server skips an ambiguous stack and raises an event. The final code never skips: with more than one working directory it dispatches `compose_project` only, and the event type was removed. | ADD AD-011 vs `internal/server/handlers/auto_update.go` (`projectsWithSeveralWorkDirs`) | README "Multiple working directories" and two CHANGELOG entries describe the dispatch; no event is mentioned. |
| The ADD and the FRD compatibility table say "new server, old agent: stack updates keep working". That holds for a single-directory stack. For a project whose containers report several working directories, the server sends no directory and an old agent fails the command at `precheck` ("container, container_id or compose_work_dir is required"). | FRD section 5 vs `targets.go` and `auto_update.go` | README compatibility table and CHANGELOG state the exception. |
| The README (before this pass) listed trusted owners as "root or an account that already has Docker rights" without the agent's own user. The code adds the agent user's effective UID to the trusted set. | README vs `compose_trust.go` `trustSet` | Corrected in the README. |
| The `env_unreadable` hint in the earlier README said "mode `0640` owned by an operator". The code needs the owner to pass the ownership rules too (root or an account with Docker rights). | README vs `compose_trust.go` `checkFiles` / `fileRule` | The rule table gives `chgrp docker` plus `chmod 0640` and the ownership rule appears under `owner`. |
| The doctor comment claimed root is harmless. Superseded by the QA-D2 ruling. | `doctor.go` | Comment corrected. |

---

## Notes for Future Writers

**Where the trust documentation lives.** There is no separate document. The operator material is the README section "Compose directory trust" (heading anchor `#compose-directory-trust` is linked from the config table, Agent Diagnostics and the CHANGELOG; keep it). The refusal-rule table is the only place that pairs each rule with its fix. When a rule is added to `compose_trust.go` (the `rule*` constants), add a row.

**Rule names are a closed set.** They are the constants at the top of `internal/agent/compose_trust.go`. The table has all 26, in groups: configuration, where the directory comes from, placement, ownership, files. Some rules are raised outside `Check`: `invalid_project` and `project_not_found` from `resolveProject`, and `daemon_unavailable`, `work_dir_mismatch`, `not_absolute` and `dir_changed` from `docker_update.go`.

**Say "service user", not "root", for the doctor.** The doctor deliberately warns when it runs as another user. Any runbook that says `sudo riot-agent doctor` is wrong for this check.

**Old agents.** Never describe old agents as protected, and never say the fleet is protected until every agent is upgraded. The server cannot gate on agent version (orchestrator decision).

**Public repo rule.** Examples use `/opt/stacks`, `/srv/media` and the `riot` user only. No hostnames, addresses or home paths belong in this repository.

**Line endings.** `CHANGELOG.md` is CRLF in this checkout; `README.md` and the Go files are LF. `sed -i` on Windows strips the carriage returns, so the CHANGELOG was edited through a scratch copy and written back with CRLF. `git diff` shows only the inserted lines.

**Verified.** `go build ./...`, `go vet ./...` and `go test ./internal/agent/... ./internal/server/...` ran after the comment edits. The README commands (`sudo -u riot riot-agent doctor`, `chgrp`, `chmod`, `setfacl`) were not executed: the checkout is Windows with no Docker host. Their syntax is standard; the doctor invocation matches the hint the code prints in `reportPrivilegeMismatch`. The rule list, doctor output format, trusted-account set, `.env` parsing and compatibility statements were checked against the code, not the ADD.

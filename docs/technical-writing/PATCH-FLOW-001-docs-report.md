# Documentation Report
**Story ID:** PATCH-FLOW-001
**Author:** Technical Writer Agent
**Date:** 2026-10-24
**Branch:** `feat/patch-flow-001` (not pushed). QA verdict: PASS WITH NOTES. Last code commit read: `a0baad6`.

## Audience Inventory

| Audience | Affected by this story | Where documented |
|---|---|---|
| Operators using the dashboard | Yes: Patch dialog checkbox, "Reboot now", stale label, badge wording | README "Manual patch and reboot", "Patch badge" |
| API integrators | Yes: `os_update` param `reboot_if_required`, three `400` codes, `lists_freshness` | README "Command endpoints", API tables |
| Engineers extending or debugging | Yes: kernel rule, freshness rule, reboot decision order, reboot-intent file, result sentence | README "Manual patch and reboot", inline comments |
| Platform engineers deploying | Yes: the helper change reaches a device only through `install.sh` | README "What ships where", CHANGELOG "Maintenance deployment", `docs/ops/os-maintenance.md` |

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | New section "Manual patch and reboot" (what "reboot required" means, the Patch dialog, "Reboot now", the result sentence, stale package lists, the post-run refresh, what ships where, the reboot-intent file). New `os_update` parameter and error-code table under "Command endpoints". `lists_freshness` added to the patch-status table and API row. Telemetry row, `updates` collector row, `commands.allow_reboot` row, feature list, state-files table and patch-badge text updated. Two stale statements in "Reboot-Class Package Gating" corrected. |
| `docs/ops/os-maintenance.md` | Updated | New section "Post-run package list refresh". Corrected the statement that the server always removes manual `include_reboot_class`. Added the authorized-reboot path. Mode table notes the conditional refresh. |
| `CHANGELOG.md` | Updated | Entries under `[Unreleased]`: five Added, four Changed, three Fixed, one Maintenance deployment (names v2.76.0 and the `install.sh` requirement). |
| `internal/agent/collectors/listsfresh.go` | Inline comments | Added the "why" for the clock-skew rule in `decideFreshness` and for using the lists directory mtime in `listsRefreshedAt`. |
| `internal/agent/reboot_auth.go` | Inline comments | Added the "why" for the second `allow_reboot` check and for rebooting after the delivery wait times out. |
| `docs/technical-writing/PATCH-FLOW-001-docs-report.md` | Created | This report. |

Comment-only edits in the two Go files. `go build ./internal/agent/...` passes. No logic, test or other implementation file changed.

No OpenAPI, Swagger or GraphQL file exists in the repository. The README API tables are the single API reference, so the new fields went there.

## Deploy Note (as documented)

- **Server, agent, dashboard:** ship with the normal server update and the normal agent update. Deploy the server first.
- **Privileged helper (`riot-maintenance`):** the post-run package-list refresh and the `/var/lib/apt/lists` stamp reach a device only when the matching release's `install.sh` is re-run as root on it. Agent-only updates do not refresh the helper.
- **Without the re-run:** everything else works. A package that rewrites apt configuration during a run leaves the host reporting `stale` until a refresh changes a package list file.
- No environment variable, collector, sudoers rule, migration or config change is needed.
- **`internal/agent/doctor.go` was not changed.** No collector was added or renamed, so the collector-whitelist and doctor-sync rules do not apply.
- Release target v2.76.0, as given by Ryan. The CHANGELOG still uses `[Unreleased]`, as the file does today.

## QA Notes: Status

| Note | Status | Documentation effect |
|---|---|---|
| N-1: `rt` and `cloud` kernels counted as the same flavor | **Resolved by `a0baad6`** (post-QA). `splitKernelFlavor` now takes the run of trailing `-` tokens that contain a letter and no `.` or `+`. | README documents the new rule with `rt-amd64` and `cloud-amd64` examples. The QA "pre-deploy check" (`ls /boot \| grep -E '^vmlinuz-.*-(rt\|cloud)-'`) is no longer required. The CHANGELOG states the flavor rule. |
| N-2: dialog states kernel/GPU facts from a stale count | Open | README lists the stale statement and the kernel/GPU statements as separate rows, as the code shows them. |
| N-3: web `confirmParams` guard has no test | Open | None. |
| N-4: `/etc/apt/preferences` single file not scanned | Open (by spec, OQ-007) | README states it is not checked. |
| N-5: refused-before-run result has no FR-023 sentence | Open | README "What the result says" states it. |
| N-6: FR-047 start-up telemetry waits for startup recovery | Open (accepted in ADD) | README states the 2-minute bound. |
| N-7: claim stays consumed if `out.Add` fails | Open (safe direction) | None. |

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 18 | Banner reads "README last updated for v2.38.0". The latest tag is v2.75.2. | Remove the banner or automate it. |
| `README.md` line 14 | The note says "relatively stable at version 2.24" and advises reinstalling before v2.24. | Rewrite or remove. |
| `README.md` (whole file) | No "Requirements" section with pinned versions and no single "Getting Started" and "Environment Variables" layout as the writer standard requires. The content exists but is spread across "Server Installation", "Server Environment Variables" and "Building from Source". | Restructure in its own pass. Verify Go, Node and Postgres versions against `go.mod`, `Dockerfile` and `web/package.json`. |
| `docs/ops/os-maintenance.md` lines 5 and 124 | Says the worker is "unreleased" and "v2.72.0 does not include the worker", and that vermont has not got it. The maintenance worker appears in later releases (AGENT-HARDEN-001 shipped v2.64.0, and the latest tag is v2.75.2). | Confirm which release first shipped the worker and update the status text. |
| `README.md` "Upgrading existing agents" and `docs/ops/os-maintenance.md` "Root migration" | The README says "Once a release containing MAINT-RECOVERY-001 is published". The guide says "No release version is prescribed here until the worker assets exist". | Same pass as the item above. |
| `CHANGELOG.md` `[Unreleased]` | It holds entries for stories that earlier tags already shipped (for example PATCH-BADGE-001, MAINT-RECOVERY-001, DOCKER-UX-001). The file has no versioned headings. | Split `[Unreleased]` into version headings from the git tags. |
| `docs/qa/PATCH-FLOW-001-qa-report.md` (upstream, not owned) | N-1 and post-deploy step 0 read as open. | Record N-1 as resolved by `a0baad6`. |
| `docs/architecture/PATCH-FLOW-001-add.md` AD-002 (upstream, not owned) | The flavor rule reads "the token after the last `-`". The code now takes the trailing run of letter tokens without `.` or `+`. | Architect to amend. |
| `CONTRIBUTING.md` | The file does not exist. The repository has `CLAUDE.md` with build, test and release commands. | Create `CONTRIBUTING.md` if a second contributor joins. |
| `requirements.md` (repo root) | "Version 0.1.0-draft, Last Updated 2026-03-03, Phase 1 Scope". Not read in full. It probably does not describe the current product. | Review, then archive or update. |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|---|---|---|
| The ADD says the flavor is the token after the last `-`. The code (`a0baad6`) takes the trailing run of letter tokens with no `.` or `+`. | ADD AD-002, `kernelreboot.go` `splitKernelFlavor` | The code's rule. |
| The ADD hides the stale row's security count. Ryan's ruling SEC-007 keeps it visible, marked "(lists out of date)". | ADD AD-010, `FleetOverview.tsx`, `DeviceDetail.tsx` | Visible and marked. |
| FR-023 says the result must state the reboot decision. A request refused before the run (host lock conflict or other maintenance) carries no sentence. | FRD FR-023, QA N-5 | Stated in README "What the result says". |
| A recovered authorized result reads `suppressed (result recovered after agent restart)`, even when the run failed. | ADD AD-005, `reboot_auth.go` | The code's text, in the reasons table. |
| The ADD describes `TouchLists` as `Chtimes`. The code uses `futimes` on an open, verified descriptor. | ADD AD-007, `cmd/riot-maintenance/main_linux.go` | The code's behavior (`O_NOFOLLOW`, root-owned directory, same descriptor). |

## Verification Notes

- Every error string, code, label and dialog statement in the README was copied from `os_update_auth.go`, `patchFlow.ts`, `patch_badge.go`, `reboot_auth.go`, `worker.go`, `DeviceDetail.tsx` and `FleetOverview.tsx`.
- Numeric limits (256 `/boot` entries, 1,000 freshness entries, 2 s, 5 min skew, 5 min post-refresh limit, 7 min skip threshold, 30 s delivery wait, 2 s minimum delay, 30-day and 72-hour intent rules, 64 KiB bound) were read from the code constants.
- No new command was documented, so there is nothing to run. The repository was built (`go build ./internal/agent/...`) after the comment edits. The test suites were not re-run, because only comments changed.
- The real-host steps in the QA report ("Post-Deploy Verification Steps") are untested by documentation and by QA. They remain Ryan's deploy gate. Steps 3 and 4 need a write on vermont that QA did not run.

## Notes for Future Writers

- The README is one large file. Feature sections follow `## Feature name` with `###` subsections and a `---` rule between them. New feature documentation went before "Fleet Dashboard". Keep that pattern.
- Existing files use CRLF line endings and `core.autocrlf=true`. Edit with single-line anchors so git normalizes line endings on commit.
- Patch and reboot behavior now has three owners: the server (validation and badge text), the agent (reboot decision, freshness, intents) and the helper (post-run refresh). "Manual patch and reboot" in the README is the one place that ties them together. Update it first when any of them changes.
- The pipeline documents (FRD, ADD, security review, QA report) are not owned by the writer. Corrections to them are listed above for their owners.
- The CHANGELOG uses one `[Unreleased]` heading. Put new bullets at the top of each subsection.

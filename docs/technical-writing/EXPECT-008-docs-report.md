# Documentation Report
**Story ID:** EXPECT-008
**Author:** Technical Writer Agent
**Date:** 2026-10-27
**Branch / HEAD at start:** feat/expect-008 @ b8b3513 (includes main up to PR #47)

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| README.md | Updated | New section "Installer reinstalls" under Expected Activity: when the notice is sent and not sent, the one status line and a table of every "not notified" reason, the skipped trust settings, what the server accepts, the window and its bound, outcomes, rate cap, dashboard label and record, the C-8 budget sentence, residuals (NFR-010 and the ADD residuals), rollout, mixed-version table, migration 000035 and the rollback order. Cross-links added from "Agent Installation", the Alerting pointer, "How it works", "Behaviour changes from earlier versions" and "Upgrade notes" |
| CHANGELOG.md | Updated | `[EXPECT-008]` entries under Added (feature, status line, trust parity, accepted budget, event types, CI/fixture note), Changed (behaviour change for installer re-runs) and Upgrade notes (the five rollout steps from ADD section 14, migration 000035, "migrate down to schema 34 before rolling the server image back") |
| internal/server/expect8_docs_test.go | Created | D8, `TestEX8_AC027_ReadmeAndChangelog`: asserts the FR-030 items, the C-8 budget sentence word for word (README and CHANGELOG), the five rollout steps and the rollback order. Reuses `exDocsSection`, `exUnreleased`, `normalised` and `mentions` from the sibling docs tests |
| scripts/install.sh | Inline comments | Two "why" comments, no behaviour change: the 8 second curl budget inside the FR-004 10 second bound, and why curl exit codes 23 and 141 are accepted (C-2 response cap closes the pipe). `bash -n` passes |
| docs/technical-writing/EXPECT-008-docs-report.md | Created | This report |

The new Go code (`commands/reinstall.go`, `handlers/agent_event_reinstall.go`, `expect/reinstall.go`, `db/reinstall_repo.go`, `models/reinstall.go`) already states its business-rule references and bounds in comments. No change was needed there.

## Verification

Run in the worktree from Git Bash with `GOCACHE=/d/rIOt_gocache GOTMPDIR=/d/rIOt_fuzz_tmp`:

| Run | Result |
|---|---|
| `go test -count=1 ./internal/server/ -run 'D8\|EX8\|Docs'` | ok |
| `go vet ./internal/server/` | clean |
| `go test -count=1 ./internal/server/` (includes `expect_docs_test`, `expect2_docs_test`, `time12h_docs_test`) | ok |
| `bash -n scripts/install.sh` | ok |
| Mutation: README budget "60" changed to "90" | D8 failed; restored, D8 passes |

QA merge gate status: D8 exists and passes; the README text and the C-8 sentence are present; the CHANGELOG carries the rollout steps and "migrate down to schema 34". The remaining gate item, the PR body carrying ADD section 14, is Clod's step (ADD section 13) and is not a file in this repo.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| README.md (Requirements) | There is no Requirements section with pinned runtime versions. The only statement is "Requires Go 1.24+ and Node.js 20+" in the build section (about line 2275). `go.mod` says `go 1.24.0`; the Node pin is not stated. PostgreSQL 16 is in CLAUDE.md but not in a Requirements section | Add a Requirements section with exact pinned versions, in isolation |
| README.md (Upgrading existing agents, about line 1546) | "Once a release containing MAINT-RECOVERY-001 is published" is written as a future event, but that work is released and the Agent Installation text already treats it as current | Reword to the present tense and name the first release |
| README.md (same passage) | Recommends "a reviewed tagged installer over a moving `main` URL during migration". Every other install instruction uses `main`. This is consistent only for that migration | Confirm the intent and state it once |
| CONTRIBUTING.md | Does not exist. The project has CI, a Windows no-`-race` rule, a fixture container rule (`RIOT_INSTALLER_FIXTURE=1`, never on a workstation or hawaii) and a release-by-tag rule that only live in CLAUDE.md and agent memory | Create CONTRIBUTING.md with setup, test commands, the installer fixture rule and the release process |
| docs/architecture/EXPECT-008-add.md AD-015, AD-016 | Still say `--pass ''`; the code uses `--pass riot-no-passphrase` (QA ruling, follow-up 1) | Architect amends, as QA already noted |
| docs/architecture/EXPECT-008-add.md AD-011, AD-013, section 14 | AD-011 says times are `15:04` in `time.Local`; since TIME-12H-001 (PR #47) the server writes 12-hour times in the display zone with a zone label. AD-013 cites `install.sh` line numbers that moved. Section 14 says "v2.84.0 or later" but the latest tag is v2.86.0 | Architect refreshes. The README and CHANGELOG use the current 12-hour form and say "the next release" |
| CHANGELOG.md | The `[Unreleased]` section already holds entries from several shipped stories (EXPECT-001, EXPECT-002 and others; the latest tag is v2.86.0). Entries are not grouped by release | Cut the released entries into version headings in a dedicated pass |
| README.md (line endings) | The working copy is CRLF while the repository stores LF (`core.autocrlf=true`). Docs tests normalise this; any new docs test must too | Use `normalised()` in new docs tests, as D8 does |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| ADD says `--pass ''`; code passes `--pass riot-no-passphrase` | `scripts/install.sh` (`notice_attempt`), ADD AD-015 and AD-016 | The code's value, in the CHANGELOG developer note |
| ADD message template `back by <HH:MM>` with `time.Local`; the server now writes the display zone in 12-hour form | ADD AD-011, FRD FR-017 | README example `back by 2:32 PM EDT`, which matches the shared offline template after TIME-12H-001 |
| ADD section 14 lists the notice as sent for "every registered device with a running agent"; the code also needs Linux, `curl`, a readable and plain `agent.yaml`, and trust settings that the installer can apply | `scripts/install.sh` | README lists the conditions and every skip reason. The CHANGELOG states "every registered device with a running agent qualifies" only for the rollout (no per-device action), and the README clarifies the skips |
| FRD NFR-010 residual "time spent at an interactive prompt after the stop counts against the bound" | FRD, ADD Q-3 | The ADD verified there is no prompt between stop and start. The README states that and does not carry the prompt residual |

## Notes for Future Writers

- Docs tests are the contract. `expect_docs_test.go`, `expect2_docs_test.go` and `expect8_docs_test.go` assert exact README and CHANGELOG strings. Edit those strings only together with the test, and run `go test -count=1 ./internal/server/` after every README or CHANGELOG change.
- Expected-activity features follow one pattern: a `### <Feature>` section inside `## Expected Activity`, one `[STORY-ID]` group of CHANGELOG lines under Added, one under Changed, and the rollout and migration notes under Upgrade notes. `exDocsSection` ends a section at the next heading of the same level, so keep sub-structure as bold lead-ins and tables, not `###` headings.
- The README reinstall table of reasons mirrors `notice_reason` in `scripts/install.sh`. When a reason is added there, add it to the table and to D8.
- The budget sentence is asserted word for word in both README and CHANGELOG (security condition C-8). Do not paraphrase it.
- The installer is served from `main`, so a README change about installer behaviour can describe code that devices already run before the server is deployed. State the mixed-version table whenever the installer and server must change together.
- When a collector is added, the project memory rules apply (doctor.go sync, README list). This story added none.

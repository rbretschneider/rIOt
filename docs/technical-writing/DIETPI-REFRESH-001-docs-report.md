# Documentation Report
**Story ID:** DIETPI-REFRESH-001
**Author:** Technical Writer Agent
**Date:** 2026-10-27

QA verdict: PASS WITH NOTES. Inputs read: FRD, ADD (revision 2), security review, implementation report, QA report, and the changed code (`internal/maintenance/worker.go` `postRunRefreshV2`, `runPostRefresh`, `sourcesChanged`; `internal/maintenance/extra.go` `executeExtras`, `summaryV2`, `extraRun.backstopVerified`). The behaviors, note strings, the 7-minute threshold, the basis selection and the apt-result-line placement in the new text were checked against that code. The success summary text (`Package state verified; maintenance completed`) was copied from `protocol.go`.

## Audience

| Audience | Affected | Artifact |
|---|---|---|
| Operators (Ryan) running DietPi hosts | Yes: no more "Lists out of date" after a DietPi patch; a root `install.sh` re-run is needed; a post-deploy check | `docs/ops/os-maintenance.md`, README, CHANGELOG |
| Support staff reading run results | Yes: notes on the apt result line, notes can span lines, `post_refresh` now follows the updater | `docs/ops/os-maintenance.md` |
| Developers extending or parsing the result | Yes: line 3 is not always the first updater detail line | `docs/ops/os-maintenance.md`, CHANGELOG |
| API integrators | No endpoint, request or record format change | none |

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `docs/ops/os-maintenance.md` | Updated | Section "Post-run package list refresh": the old table moved under a new subsection "Runs without extra updaters" (v1, unchanged). New subsection "Runs with extra updaters": the check and single refresh follow the extra-updater step; eligibility table; time basis table (80 minutes only when the backstop was verified in the run, otherwise 30 minutes; both clocks; strict 7-minute threshold); where the notes go (success and apt-failure lines, never the interrupted line); the note may span lines and line 3 is not always the first updater detail line (QA N-4, AD-005); bounds; deploy and the post-deploy check. The stamp paragraph gained the heading "The lists stamp and deployment". Line 19 (modes table), line 64 (the step ordering) and the results section were corrected. |
| `CHANGELOG.md` | Updated | `[Unreleased]` Fixed: the user-visible effect and the details a result reader needs. Maintenance deployment: the root `install.sh` re-run and the check on the next DietPi patch. |
| `README.md` | Updated (one paragraph) | In "Post-run package list refresh": the DietPi case, the basis rule in one sentence, a link to the operator guide, and the `install.sh` re-run. |
| `docs/requirements/EXTRA-UPD-001-frd.md` | One-line notes | A superseded-for-v2-runs note after FR-025 and after AC-020. The text of the old requirement was not rewritten. |
| `docs/technical-writing/DIETPI-REFRESH-001-docs-report.md` | Created | This report |

No code or test file was touched. No inline comments changed: the story's code already carries FR, AD and DR-SEC references in its comments, and the ones read match the behavior.

**EXTRA-UPD-001 ADD.** It has no statement of the old ordering, only a test name at L662, so it needed no note.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `docs/ops/os-maintenance.md` L5 | Says the feature is unreleased and that "v2.72.0 does not include the worker". Tags go up to v2.86.0, and MAINT-RECOVERY-001 has long shipped | Rewrite the status paragraph in a separate pass |
| `README.md` L18 | "README last updated for v2.38.0". The README documents much later work (also logged in the SHUTDOWN-DPKG-001 report) | Update or remove the marker |
| `docs/architecture/PATCH-FLOW-001-add.md` AD-007 (L501 area) and `docs/requirements/PATCH-FLOW-001-frd.md` FR-040 to FR-045 | Describe the post-run refresh at the end of the apt part for every run. For version 2 runs it now follows the updater step | Historical. Add a superseded-by note pointing to DIETPI-REFRESH-001 AD-001, or leave as history |
| `README.md` L3112 | "skips it when less than 7 minutes remain in its 30-minute budget" is exact for runs without extra updaters only. The paragraph added by this story covers the other case | Fold into one statement the next time the section is edited |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| None between the ADD (revision 2) and the code. `backstopVerified` is set once in `runExtra`, `executeExtras` defaults to `(aptCtx, WorkerBudget)`, and `summaryV2` appends the note in the success and error arms only | `extra.go`, `worker.go` | As the ADD states |
| The ADD says the 80-minute basis follows "check 6". The operator guide numbers the gates 1 to 9, and its gate 6 is the same check (the 85-minute backstop and no mount sandbox) | `docs/ops/os-maintenance.md` | The table cites "gate 6 in What the step does" |

## Notes for Future Writers

- The deploy check is a manual one. Unit tests cannot cover a real host. On the next DietPi patch of a re-installed host, a `[post_refresh]` section after the DietPi section appears only when DietPi changed the apt configuration in that run. A run with no change correctly has none. Do not write the check as "always present".
- The operator guide's summary description is shared by three stories (EXTRA-UPD-001, DIETPI-PATH-001, DIETPI-REFRESH-001). Keep the anchors `#runs-with-extra-updaters`, `#post-run-package-list-refresh` and `#results-and-truthful-outcomes`: the README, the CHANGELOG and this report link to them.
- Pipeline documents are history. Add a one-line "superseded" note; do not rewrite them.

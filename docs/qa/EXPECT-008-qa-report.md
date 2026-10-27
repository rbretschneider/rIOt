# QA Report
**Story ID:** EXPECT-008
**QA Engineer:** QA Agent (Sonnet 5.5)
**Date:** 2026-10-27
**Branch / HEAD:** feat/expect-008 @ ed9a4ce (includes the merge of origin/main PR #45 and #46 at 8d198b3)
**Verdict:** PASS WITH NOTES (conditional: AC-027 and security condition C-8 are open until Part D lands; see "Verdict Rationale")

---

## Test Run Summary

All runs were made by QA from the worktree. Nothing ran against hawaii or a real device. Every container was `--rm` or removed afterwards (`riot-qa-pg` stopped, removed by `--rm`).

| # | Run | Result |
|---|---|---|
| 1 | `go build ./... && go vet ./... && go test -count=1 ./...` (Git Bash, Windows, no PG) | build and vet clean; 31 packages `ok`, 0 failing. Neither known flake (`TestCL_AC100`, `TestDUX_AC165`) fired. The 12 PG-gated EX8 tests skip here |
| 2 | PostgreSQL 16 (`postgres:16`, throwaway, `RIOT_TEST_PG_URL=postgres://postgres:...@localhost:55432/riot_test`), `go test -count=1 ./internal/server/...` | All server packages `ok` except `internal/server/db`. First attempt hit the default 10 min `go test` timeout (the db package takes about 6 min alone; QA was running a race build, mutations and a web run in parallel, and `TestEX8_PG_CreateShapeAndSecondNoticeAbsorbed` stalled inside `migrate.Up`). Re-run with nothing else running: all 12 EX8 PG tests PASS (`-run EX8`, 184 s), and the full db package finishes in 358 s with exactly one failure, `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB` (`cannot scan NULL into *string`, `partition_conversion` empty). That test is **pre-existing and unrelated**: it fails identically on a `git archive` of origin/main e120031 in the same PG container, and fails alone on the branch. Not attributable to EXPECT-008 |
| 3 | Race, Linux: `docker run --rm --network none`, repo and module cache mounted read-only, `golang:1.24`, `go test -race -count=1 ./internal/server/...` | 19 packages `ok`; one failure, the known flake `quota TestDUX_AC165_QuotaStdoutSample` (time.Now based). Re-ran `./internal/server/quota` alone with `-race`: `ok`. (A first attempt with `GOFLAGS=-mod=mod` failed only because it tried to write go.mod on a read-only mount; CI does not use that flag) |
| 4 | Web: `cd web && npx tsc --noEmit && npm run test:run` | tsc exit 0; vitest 121 files, 2143 tests, all passed. WZ8 `[EXPECT-008 AC-026]` scans 13 files |
| 5 | Installer fixtures, CI command, LF copy of `scripts/`, mounted read-only | `debian:bookworm-slim` (curl 7.88.1, OpenSSL 3.0.22, mawk 1.3.4-20200120): `Ran 98 tests ... OK` (129 s). `debian:trixie-slim` (curl 8.14.1, OpenSSL 3.5.7, mawk 1.3.4-20250131): `Ran 98 tests ... OK` (107 s). All 30 `test_EX8_*` pass on both, including the C-1 pair (`system_store_ca_not_trusted_with_trust_file` and `system_store_positive_control`) |

- Flaky tests found: none in the story's tests. Known flakes: `quota TestDUX_AC165` failed once under heavy parallel load (race run) and passed on re-run; not related.
- Coverage percentage: not collected (no coverage tooling in the pipeline for this story).

### QA-run probes and mutation checks (no implementation file was changed)

All mutations were made in a scratch copy under the session scratchpad, never in the worktree. Each was **killed** by the named tests:

| Mutation | Killed by |
|---|---|
| expect `finishReinstall`: backstop escalation disabled | `AC012_*` (4), `AC019_BoundDuringDowntime..`, `AC019_DeadDuringDowntime..`, `AC020_ResolverUnsetFailsSafe`, `AC021_*` |
| expect: coverage of an unresolved record extended 1 min past the bound | `AC020_ResolverUnsetFailsSafe` (only this one; acceptable, see notes) |
| commands: registration before the start closes the record / registration at or after the bound closes it | `RegistrationAfterBoundIgnored` (both); the second also `AC020_ResolverUnsetFailsSafe` |
| handler: identity re-check removed | `AC014_HandlerWithoutAuthenticationRecordsNothing` |
| PG repo: cap `>=` to `>` | `AC016_PG_ConcurrentNoticesOneRecordOneCapEvent` |
| PG repo: device-live check removed (DEC-4) | `AC015_PG_OfflineDeviceNoRow` |
| PG: AD-017 attribution predicate removed | `AC023_PG_SelfUpdateAttributionIgnoresReinstall`, `MockGuardParity` |
| PG: server-side `origin <> 'installer'` guard removed | `PG_Guards`, `MockGuardParity` |
| PG: stale-retire threshold 1 h to 0 | `AC020_PG_StaleRetireInTx` |

Real-curl installer probes, run in throwaway `debian:bookworm-slim` and `debian:trixie-slim` containers against loopback Python servers, sourcing only the `reinstall-notice` block from `install.sh` with a fake `STAGE` and config dir (no agent, systemctl, apt or reboot touched). Same result on both images, stderr empty, `$STAGE` empty afterwards:

| Server behaviour | Time | Installer line |
|---|---|---|
| 302 redirect | 0.0 s | "not notified (unexpected server response)" |
| 200 with 1 byte per second (slow drip, Content-Length 100) | 7.0 s | "not notified (timed out)" |
| 200 declaring Content-Length 1,000,000,000 | 0.0 s (curl rc 63) | "not notified (unexpected server response)" |
| 200 `text/html` (an old server's SPA fall-through) | 0.0 s | "not notified (unexpected server response)" |
| 200 with `"reinstall":"created_evil"` | 0.0 s | "not notified (unexpected server response)" |
| 200 body that carries ANSI escapes around a valid `created` | 0.0 s | the fixed success line only; no body byte reaches the terminal |
| 400 `unsupported event type` (old server) | 0.0 s | "not notified (server does not support reinstall notices)" |
| 429 | 0.0 s | "not notified (server busy)" |

## AC Coverage Audit

Test names are the ADD section 8 names. "Mutation" notes that QA verified the test fails when the behaviour is broken.

| AC | Status | Tests covering it | Gap |
|---|---|---|---|
| AC-001 | COVERED | I: `test_EX8_AC001_one_notice_after_validation_before_stop` (real curl, pinned cert, trace order sudoers < NOTICE < stop, header sha256), `..._success_line_once_and_exit_status_unchanged`, `..._piped_script_runs_to_completion` | none |
| AC-002 | COVERED | I: `test_EX8_AC002_no_notice_without_identity_or_running_agent` (fresh, `--bootstrap-key`, no device_id, no api_key, inactive unit, each compared with the same run without the notice) | none |
| AC-003 | COVERED | I: `test_EX8_AC003_abort_before_stop_sends_no_notice` (4 abort causes) | none |
| AC-004 | COVERED | I: `..._server_failures_never_block_install`, `..._curl_runs_under_the_time_bound`, `..._real_hang_ends_within_10s` (real curl, <= 10.5 s), `..._oversize_undeclared_body_is_bounded`, `..._internal_error_prints_one_line`, `..._every_reason_is_one_line_and_empty_stderr`; plus the QA probes above (drip, redirect, declared length) | refused and unresolvable host are emulated (rc 7, rc 6) in the fixture, not real; the real-curl closed port is exercised in the AC-006 run |
| AC-005 | COVERED | I: `..._untrusted_certificate_no_key_sent`, `..._system_store_ca_not_trusted_with_trust_file` + `..._positive_control`, `..._trust_file_rows_pass_empty_capath`, `..._pinned_certificate_mismatch_no_key_sent`, `..._mtls_presents_client_certificate`, `..._tls_verify_off_skipped`, `..._pin_without_pinned_certificate_skipped`, `..._paths_outside_etc_riot_skipped` | none (mTLS uses `--pass riot-no-passphrase`, see ruling) |
| AC-006 | COVERED | I: `..._key_never_in_argv_output_or_files` (argv recorders on 10 tools, `bash -x`, accept and closed-port runs, filesystem grep), `..._key_reaches_only_the_printf_builtin`, `..._environment_cannot_widen_trust_or_log_keys` (with control) | none |
| AC-007 | COVERED | I: `..._reader_agrees_on_every_awk` (23 cases, mawk and gawk), `..._full_runs_use_mawk`; CI step installs curl and gawk and pins mawk. Passed on both images | busybox awk case only if present (not present in the images) |
| AC-008 | COVERED | H8 `AC008_TimelineNoNotification` (exact FR-017 and FR-018 texts, info severity, link, no alert, label gone), `AC008_LabelFromNoticeUntilRegistration`; HT8 `AC008_NoticeCreatesRecordStoresNoEvent`, `AC008_RegistrationClosesRecordAtFirstRegistration` | none |
| AC-009 | COVERED | H8 `AC009_TwoDevicesBackToBackZeroPushes` | none |
| AC-010 | COVERED | H8 `AC010_ContainerDeathDuringReinstallAlerts` (asserts `Suppressed=false`, alert Notified=true) | none |
| AC-011 | COVERED | H8 `AC011_OfflineAfterCloseAlerts` (offline at 10:05, before the bound, after the close: alerts and notifies) | none |
| AC-012 | COVERED | H8 `AC012_NotBackEscalatesOnce` (exact text, one push, nothing at 09:59, none after +1 h), `..._EscalationIgnoresUnrelatedCooldown`, `..._NoSecondEscalationAfterRestart` (3 restarts, 1 marker), `..._EscalatedLabelAndOfflineCount`; web `[AC-012]` in activity.test.ts, FleetOverview and DeviceDetail tests. Mutation: backstop removed fails 8 tests | none |
| AC-013 | COVERED | H8 `AC013_NotConfirmedWhileOnlineWarningNoPush` (exact message, one event after +1 h, no alerts, later offline alerts) | none |
| AC-014 | COVERED | HT8 `AC014_RejectedNoticesCreateNothing` (401, 401, 401, 403; no row, no event, repo never called), `AC014_HandlerWithoutAuthenticationRecordsNothing`; H8 `AC014_OfflineAfterRejectedNoticeAlerts`; C-7 `ForeignKeyForbiddenWithMTLS` | none |
| AC-015 | COVERED | S8 `AC015_DeviceOfflineNoRecord`; R8 `AC015_PG_OfflineDeviceNoRow` (mutation killed) | none |
| AC-016 | COVERED | S8 `AC016_FiftyNoticesOneRecord` (deadline unchanged, no event, debug-only logs), `AC016_RateCapSixPerDayOneWarning` (6 rows, 1 cap event, rolling 24 h); R8 `AC016_PG_ConcurrentNoticesOneRecordOneCapEvent` (mutation killed); H8 `AC016_CappedNoticeOfflineAlertsNoLogAboveDebug`, `AC016_RepeatedNoticeDoesNotExtendWindow` | the FRD's "10 notices over 20 hours" is tested as 10 notices 2 h apart (18 h); same behaviour |
| AC-017 | COVERED | HT8 `AC017_UntrustedFieldsIgnored` (> 10 KB, controls, bidi, invalid UTF-8, extra keys; row equals the plain-notice row) | none |
| AC-018 | COVERED | S8 `AC018_StoreFailureFailsOpen` (one throttled Warn, then bounded), H8 `AC018_StoreFailureFailsOpen`, HT8 `AC018_RepoErrorAnswersUnavailable` | none |
| AC-019 | COVERED | H8 `AC019_RestartMidWindowSucceeds`, `AC019_BoundDuringDowntimeEscalatesOnce`, `AC019_DeadDuringDowntimeEscalatesNotUnconfirmed` | none |
| AC-020 | COVERED | H8 `AC020_ResolverUnsetFailsSafe`; S8 `AC020_StaleOpenRecordRetiredByNextNotice` (boundary: exactly bound + 1 h still absorbs, +1 s retires); R8 `AC020_PG_StaleRetireInTx` (mutation killed) | none |
| AC-021 | COVERED | H8 `AC021_BoundedEventsLogsMemory`; I `test_EX8_AC021_no_file_left_behind`; QA probe confirms `$STAGE` empty after each notice | none |
| AC-022 | COVERED | Web: FleetOverview.reinstall (one-line label, actor and start in title, Expected offline vs Offline counts, offline dot, new browser, live clear on `activity_update` null), DeviceDetail.reinstall, SelfUpdateDetails.reinstall (read-only, no controls), activity.test `[AC-022]`; Go: S8 `AC022_NeverClaimedOrDelivered`, `AC022_AgentReportNotFoundHTTPAndWS`, `AC022_CancelNotCancellable`, HT8 `AC022_AgentReportNotFoundAndCancelConflictOverHTTP` | label clear latency (5 s) is asserted by the live-follow web test and the 1 s tracker tick, not by a wall-clock test |
| AC-023 | COVERED | H8 `AC023_ReinstallDuringSelfUpdate`; S8 `AC023_SelfUpdateStartNotAttributedToReinstall`; R8 `AC023_PG_SelfUpdateAttributionIgnoresReinstall` (mutation killed); HT8 `C7_MockGuardParityIncludesAttribution` | the H8 test's push assertion is loose (`<= 2`); it matches the documented R-8 residual |
| AC-024 | COVERED | I `test_EX8_AC024_new_installer_against_old_server` (400 and 404, real curl); H8 `AC024_ReinstallWithoutNoticeAlertsAsToday` | none |
| AC-025 | COVERED | Whole existing suite unmodified and green (Go 31 packages, web 2143, installer 68 existing x 2 images); H8 `AC025_SelfUpdateAndCommandWindowsUnchanged`. `harness_test.go` was extended additively (impl report deviation 4) and no existing test file was edited except `labels.test.ts` (54 to 56, forced by the two new event types) | none |
| AC-026 | COVERED | Z8 `TestEX8_AC026_NoPowerUpdateOrAgentExecution` + `ScannerCatchesEachForbiddenForm` (163 files; all 32 Go files of the diff are in the printed list); WZ8 (43 tests; the 13-file list contains every web source of the diff); I `test_EX8_AC026_no_power_package_or_agent_execution` plus tripwire lines asserted absent in every EX8 trace | none |
| AC-027 | **NOT YET COVERED (Part D pending)** | ADD section 14 (rollout list) exists and answers FR-029. **Absent:** `internal/server/expect8_docs_test.go` (D8, `TestEX8_AC027_ReadmeAndChangelog`), README expected-activity text (FR-030), CHANGELOG entry with the rollout and "migrate down to 34" steps, PR body list | By the pipeline order (technical-writer runs after QA) these are the technical writer's deliverables and the ADD lists Part D "after G, I, W". QA did not add D8: it would be red until the docs exist, and QA may not write the docs. See Verdict Rationale |

### Edge-case probes requested (all verified)

| Probe | Where verified |
|---|---|
| Forged notice naming another device's {id} | 403 from `DeviceAuth`, also behind `MTLSDeviceAuth`; no row, no event; handler re-check answers `unavailable` (HT8 AC014, C7 mTLS). Mutation on the re-check killed |
| Replay past 6 per 24 h | S8, PG, HT8 (`C7_NoticeStoresNoEventOnEveryDisposition`): 7th and later get `capped`, exactly one cap warning, no push, offline after them alerts normally (H8 AC016) |
| Notice while already offline (DEC-4) | S8, PG (mutation killed), HT8 disposition `device_offline`, installer prints "device already offline on server" |
| No return in 10 min escalates exactly once | H8 AC012 (once, across 3 restarts, unrelated cooldown), AC019 (downtime) |
| EXPECT-002 self-update just after a reinstall (AD-017) | S8, PG (mutation killed), HT8, H8: the start is linked to its own record, never to the reinstall row |
| Reinstall inside another agent window (DEC-8) | H8 `AC023`: latest start wins the label, each record ends at its own bound |
| Old server answering 400 | real curl fixture `test_EX8_AC024...` and probe: one "not supported" line, exit 0, install unchanged |
| Wrong key format | `test_EX8_reader_rejects_values_that_could_inject_curl_config`, `..._reader_agrees_on_every_awk`: no curl started, "agent.yaml not readable" |
| Server unreachable, hung, slow | fixture (rc 7, 28, real hang <= 10.5 s) and QA drip probe (7.0 s) |
| Response size cap | `test_EX8_AC004_oversize_undeclared_body_is_bounded` (watcher never sees > 4096 bytes) and QA declared-length probe |

## Security conditions and QA gates

| Item | Status | Evidence |
|---|---|---|
| C-1 exact trust anchor | PASS | `system_store_ca_not_trusted_with_trust_file`, `system_store_positive_control`, `trust_file_rows_pass_empty_capath`, `pinned_certificate_mismatch_no_key_sent` green on bookworm (curl 7.88.1) and trixie (curl 8.14.1) |
| C-2 response byte bound | PASS | `oversize_undeclared_body_is_bounded` green on both; QA declared-length probe |
| C-3 I/O isolation, one line | PASS | `piped_script_runs_to_completion`, `internal_error_prints_one_line`, `every_reason_is_one_line_and_empty_stderr` green on both; QA probe: stderr always empty |
| C-4 clean environment | PASS | `environment_cannot_widen_trust_or_log_keys` (with a control that the real curl does write a key log), `curl_runs_under_the_time_bound` (shows `env -i`) |
| C-5 production key format | PASS | reader tests; every EX8 key is `prod_key()` |
| C-6 snapshot race | PASS | `snapshot_rejects_swapped_inode` (symlink, FIFO, two links, swap between open and lstat) |
| C-7 four handler tests | PASS | all four exist in `agent_event_reinstall_test.go` and pass; QA gate list: transitions never touch the progress gate or result columns (S8 `TransitionsLeaveResultColumnsAndGateUntouched`, `NoCompletionEventForReinstall`), outcome events never dispatched or ceilinged (H8 `OutcomeEventsNeverDispatchedNorCeilinged`), no events row for the notice (HT8), agent reports 404/not_found over HTTP and WS (S8, HT8), key never on argv/output/disk (AC-006), Z8/WZ8 lists complete |
| C-8 budget sentence in README | OPEN | README and D8 absent (Part D) |

## Ruling: `--pass riot-no-passphrase` instead of the ADD's `--pass ''`

**Accepted. The ADD wording should be amended.**
- It is not a secret: a fixed public string in the installer source, visible in `ps` for the lifetime of a 10 s curl, protecting nothing and granting nothing.
- It cannot unlock anything the agent could use. The agent loads the pair with `tls.LoadX509KeyPair` (`internal/agent/http_client.go` lines 211 and 246), which cannot decrypt a passphrase-protected PEM, so a key the agent can actually use is always unencrypted and the passphrase is ignored by OpenSSL. An encrypted key makes the agent log "failed to load mTLS client certificate" and run without a client certificate; the installer then fails safe ("connection failed" or "TLS verification failed") and the device pages as today.
- The only key it could newly open is one encrypted with exactly that string. Only the `riot` user (or a compromise of it) can place a file at `/etc/riot/client.key`, and that actor can already put an unencrypted key there (ADD residual R-1). No new authority.
- Reason it is needed: curl 8.14.1 on trixie (hawaii's OS) rejects an empty `--pass` (exit 2), which would make every mTLS device print "connection failed". Alternatively the pass could be sent through the curl config pipe; not required.
- Action for the architect, non-blocking: change AD-016 step 2 and the mTLS row of AD-015 to the actual argument and record this ruling.

## Test Quality Findings

1. Behaviour tests, not implementation tests: Go EX8 tests assert observable outcomes (events, pushes, rows, HTTP bodies, activity DTOs) through the real Generator, Tracker, Service and router with an injected clock. No sleeps. The installer tests run the real curl and real TLS against loopback, with a positive control for the system-store test.
2. Mutation evidence above shows the tests detect the key invariants (cap, DEC-4, bound, backstop, attribution, guards, identity, stale retirement).
3. The only mutation that survived most tests is "coverage of an unresolved record extends 1 min past the bound" (killed only by `AC020_ResolverUnsetFailsSafe`). In normal operation `finishReinstall` resolves the record at the bound, so the exposure is limited to a resolver failure, which that test covers. Non-blocking.
4. The `AC023` push assertion is `<= 2`; precise enough for the documented R-8 residual, but it would not catch a third push. Non-blocking.
5. Isolation: the new PG tests each create their own env; one stalled for 3 min under heavy machine load (see run 2) but passes alone. The db package already takes about 6 min on this workstation, close to the default 10 min `go test` timeout; local runs of the whole package should pass `-timeout`.
6. Mocks: `MockCommandRepo` mirrors the PG guards and CHECK; `TestEX8_MockGuardParity` (PG) compares mock and SQL over actor x origin x status, and the AD-017 predicate is covered in both. When `Devices` is not linked the mock treats every device as live (impl report deviation 3); every EX8 test links it.
7. Installer fixture hygiene: the tests install a test CA in the container store and remove it in cleanup, replace system tools by argv recorders for one test, and bind loopback only. No test executes the agent binary (tripwire lines asserted absent).

## Adversarial Findings

No defect found. Documentation-only observations:
- A hostile or on-path server on `http://` can answer a valid-looking `created` and make the installer print the success line (ADD R-2, R-9, accepted; the line is a fixed string and the server cannot make it print anything else).
- A `wget`-only host with a registered device prints "curl not available" (the curl check precedes the identity check; impl report deviation 3). One extra line on an unusual host; harmless.
- Fail-open and bounded-growth properties hold (below).

### Bounded growth (Ryan's standing rule), confirmed

| Thing | Bound and where verified |
|---|---|
| Rows | 1 running per device (unique partial index, `PG_UniqueOpenRecord`), 6 per rolling 24 h (`>=` cap, mutation killed), so at most 540 per device in the 90-day retention; CHECK caps deadline at start + 10 min, 600 s exec, no params, no target keys (`PG_CheckShape`) |
| Events | Notice stores none on any disposition (`C7_NoticeStoresNoEventOnEveryDisposition`); at most one cap event per device per 24 h; per record one suppressed-offline info per transition, one online note, one outcome event or one escalation, one stuck warning (`AC021_BoundedEventsLogsMemory`) |
| Pushes | At most one per record (the escalation, through `device_offline` under the EXPECT-001 ceiling); outcome, cap and stuck events are broadcast only (`OutcomeEventsNeverDispatchedNorCeilinged`) |
| Logs | Debug only per notice; Warn only through the per-device throttle (`AC018`, `AC016_Capped...NoLogAboveDebug`); diff contains no new Info/Warn/Error outside `warnThrottled` |
| Memory | One window per device; `closedRecent` drop within 5 min (AC-021) |
| Installer | Response <= 4096 bytes stored, status <= 16 bytes, `$STAGE/notice` removed at the end, nothing left (AC-021 test and QA probes); total time <= 10 s (real hang test, QA 7.0 s drip) |

## Tests Added by QA

None. The ADD maps no test to a gap that QA could fill with a green test: every mapped Go, web and installer test exists and passes. The one mapped test that is absent, `TestEX8_AC027_ReadmeAndChangelog` (D8), is gated on documentation that does not exist yet; adding it now would only turn the branch red. QA made no change to implementation, test or doc files; the only file QA adds is this report.

## Deviations from ADD

| # | Deviation | QA ruling |
|---|---|---|
| 1 | `--pass riot-no-passphrase` instead of `--pass ''` | Accepted (above); ADD wording to be amended |
| 2 | `TestEX8_MockGuardParity` in `db/reinstall_parity_pg_test.go` (import cycle, same as EXPECT-002) plus no-DB companion in `commands` | Accepted |
| 3 | Unlinked `MockCommandRepo.Devices` treats every device as live | Accepted (all EX8 tests link it) |
| 4 | Additive edits to `expect/harness_test.go` (`registerOn` also calls `NoteReinstallRegistration`, `withoutReinstallResolver`, device link); `labels.test.ts` count 54 to 56 | Accepted; no existing assertion weakened |
| 5 | `notice_attempt` checks `curl` before the identity (wget-only hosts) | Accepted, as specified in AD-016 step 1 |
| 6 | Part D (README, CHANGELOG, D8, PR body) not yet implemented | See AC-027 |

The merge conflict resolution in `web/src/utils/activity.ts` (8d198b3) is correct: the reinstall label (`isReinstallActivity` returns "Reinstalling agent") is evaluated first in the `agent_update` case, then PR #45's `returning` line (`Agent restarted, settling` when online, otherwise `Restarting agent...`) is kept intact. The reinstall window phase is `in_progress`, so it never reaches the `returning` branch. `window.go` carries PR #46's change next to the `p.Reinstall` delegation without overlap. All tests from #45 and #46 pass on the merged tree.

## Deviations from FRD

None in implemented behaviour. FR-029 (PR body steps) and FR-030 (README) and the CHANGELOG half of FR-029 are outstanding with Part D. ADD section 14 already carries the FR-029 list, ready to copy.

## Verdict Rationale

**PASS WITH NOTES**, conditional.

- 26 of 27 ACs are COVERED by tests that QA confirmed assert the behaviour and fail when it is broken (mutation checks). All security conditions C-1 to C-7 are met; all runs are green on both Debian images, on Linux with `-race`, with PostgreSQL 16, and in the web suite. No implementation defect was found.
- By the letter of the verdict rules, AC-027 (and condition C-8, the README budget sentence) is MISSING, which would be FAIL. QA rules it as pending rather than failed because it is the documentation deliverable, the pipeline places the technical writer after QA, and the ADD schedules Part D last. This is a judgement for the caller to confirm. If the caller does not accept it, the verdict is FAIL on AC-027 alone.
- **Merge gate:** the story must not merge until (1) `internal/server/expect8_docs_test.go` exists and passes (README per FR-030 including the C-8 budget sentence, CHANGELOG with the five rollout steps and "migrate down to schema 34 before rolling the server image back"), (2) the PR body carries ADD section 14, and (3) the `expect2_docs_test.go` strings still pass.

## Notes for follow-up (non-blocking)

1. Amend AD-015 and AD-016 for `--pass riot-no-passphrase`.
2. `internal/server/db` PG suite: pre-existing `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB` fails on origin/main e120031 in a fresh PG 16 container; triage separately (not this story).
3. The db package needs `-timeout` above 10 min locally.
4. After merge: Clod deploys the server on hawaii and verifies schema 35 and the image digest before relying on silence; the first re-run after that is the live check (ADD section 14).

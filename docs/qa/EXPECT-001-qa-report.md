# QA Report

**Story ID:** EXPECT-001 (expected-activity windows)
**QA Engineer:** QA Agent
**Date:** 2026-10-26
**Branch:** `feat/expected-activity-001` (head at review: `8111c4e`, plus the QA test commit)
**Verdict:** PASS WITH NOTES

---

## Test Run Summary

Windows host, `GOCACHE=D:\rIOt_gocache`, `GOTMPDIR=D:\rIOt_fuzz_tmp`. Every number below is from QA's own runs.

| Run | Result |
|---|---|
| `go build ./...` | exit 0, no output |
| `go vet ./...` and `GOOS=linux go vet ./...` | exit 0, no output |
| `go test ./... -count=1` (no database) | 30 packages `ok`, 0 FAIL |
| `go test ./... -count=1` with `RIOT_TEST_PG_URL` (throwaway `postgres:17-alpine`, db `riot_test`) | 30 packages `ok`, 0 FAIL (includes `db` and `storage` PG tests and migration 000033 up/down/up) |
| `go test ./... -run EX_ -v -count=1`, no database, before QA tests | 195 top-level `TestEX_` tests: 184 PASS, 11 SKIP (the PG-gated ones), 0 FAIL |
| same, with database, after QA tests | 203 top-level `TestEX_` tests: 203 PASS, 0 SKIP, 0 FAIL |
| `go test -run EX_ -count=5` on `expect`, `server`, `events`, `handlers` | `ok`, no flake |
| `go test -race -count=1` on `server`, `expect`, `events`, `handlers`, `websocket`, `commands`, `models` (`golang:1.24` container, `--rm`) | all `ok` |
| `go test -race -count=3` on `expect`, `server` (same container, after QA tests were added) | both `ok` |
| `cd web && npx tsc --noEmit` | exit 0, no output |
| `cd web && npx vitest run` | `Test Files 101 passed (101)`, `Tests 1813 passed (1813)` |

Output tails:

```
ok  	github.com/DesyncTheThird/rIOt/internal/server	3.528s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.771s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	13.662s   (PG-gated EX tests, with database)
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.739s
ok  	github.com/DesyncTheThird/rIOt/internal/server/expect	1.184s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	10.029s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	0.923s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	2.005s
ok  	github.com/DesyncTheThird/rIOt/scripts	51.801s
--- PASS: TestEX_Repo_PG_MigrationUpDownUp (1.22s)
--- PASS: TestEX_SEC001_PG_PurgeInvalidPoweredOff (1.35s)
--- PASS: TestEX_SEC002_PG_RetentionSkipsPendingWindows (1.35s)
--- PASS: TestEX_SEC002_PG_BudgetSkipsPendingWindows (0.08s)
 Test Files  101 passed (101)
      Tests  1813 passed (1813)
```

- Coverage percentage: not produced (no coverage run requested).
- Flaky tests: none found.
- Containers: QA started `qa-expect001-pg` (removed afterwards) and two `golang:1.24` race containers (`--rm`). No other container was touched.

## AC Coverage Audit

Status is judged by the critical question: if the AC were broken, would the named test fail? Tests were read, not just listed. Line numbers are the `func` lines.

| AC | Status | Tests covering it | Gap / note |
|---|---|---|---|
| AC-001 | COVERED | `expect/container_test.go:21` | asserts info severity, command link, message text, no notification |
| AC-002 | COVERED | `container_test.go:37`; `models/expect_test.go:78` | |
| AC-003 | COVERED | `container_test.go:53` | all six actions of the AC |
| AC-004 | COVERED | `window_test.go:44`; `container_test.go:85` | |
| AC-005 | COVERED (QA-extended) | `container_test.go:98`; QA `qa_test.go` `TestEX_QA_AC005_EveryContainerActionCoversOnlyItsTargets` | original covered `docker_update` only; QA added all eight container-class actions incl. an unrelated compose project |
| AC-006 | COVERED | `container_test.go:110`, `:122`; `generator_expect_test.go` FR014 | |
| AC-007 | COVERED | `window_test.go:57`; `container_test.go:137` | 10:01:50 vs 10:02:10 both asserted |
| AC-008 | COVERED (QA-extended) | `container_test.go:161`; `models/expect_test.go:16,49,57`; QA `TestEX_QA_AC008_ContainerWindowsNeverCoverOffline` | QA added: no container-class window ever covers `device_offline` |
| AC-009 | COVERED (QA-extended) | `container_test.go:183`; `generator_expect_test.go:189`; QA `TestEX_QA_AC009_OOMDuringHostWindowStillAlerts` | QA added reboot, os_update, shutdown windows |
| AC-010 | COVERED | `container_test.go:199` | |
| AC-011 | COVERED | `expect/host_test.go:13` | |
| AC-012 | COVERED | `host_test.go:44` | |
| AC-013 | COVERED | `host_test.go:61` | |
| AC-014 | COVERED | `host_test.go:80` | |
| AC-015 | COVERED | `host_test.go:103` | exactly-once asserted |
| AC-016 | COVERED | `host_test.go:128` | |
| AC-017 | COVERED | `host_test.go:147` | |
| AC-018 | COVERED | `host_test.go:162` | |
| AC-019 | COVERED | `host_test.go:187` | |
| AC-020 | COVERED | `host_test.go:204` | |
| AC-021 | COVERED | `escalate_test.go:30`, `:56` | 20 + 1 summary, and across restart |
| AC-022 | COVERED | `host_test.go:221`; `generator_expect_test.go:403` | |
| AC-023 | COVERED | `generator_expect_test.go:432`, `:450` | |
| AC-024 | COVERED | `fleet_actor_test.go:36,51,64`; `activity_test.go:14`; web `[AC-024]` | |
| AC-025 | COVERED | `agent_shutdown_test.go:13`; `activity_test.go:44` | |
| AC-026 | COVERED | `agent_shutdown_test.go:38`; `activity_test.go:62` | |
| AC-027 | COVERED | `agent_shutdown_test.go:60` | |
| AC-028 | COVERED | `agent_shutdown_test.go:85`; `activity_test.go:84`; web `[AC-028]` | 72 h advance is one clock jump (see QA-9) |
| AC-029 | COVERED | `agent_shutdown_test.go:114` | |
| AC-030 | COVERED | `agent_shutdown_test.go:137` | |
| AC-031 | COVERED | `expect/durability_test.go:16` | |
| AC-032 | COVERED | `durability_test.go:41`, `:67` | |
| AC-033 | COVERED (QA-extended) | `window_test.go:72`; `durability_test.go:87` (12 actions, 1 s before / at the bound); QA `TestEX_QA_AC033_StuckActiveCommandOnOnlineDeviceLeavesNothingExpected` | C-8 gate item |
| AC-034 | COVERED | `durability_test.go:142`; `activity_test.go:101` | |
| AC-035 | COVERED | `offline_test.go:90,103,131,161,184,208` | |
| AC-036 | COVERED (QA-extended) | `durability_test.go:163`, `:188`; `SEC003` `:203`; QA `TestEX_QA_AC036_OfflineFailsOpenWhenStaleOrStalledAndSelfHeals` | original stale case covered the container path only; QA added the offline path for stale and stalled, plus self-heal. C-8 gate item |
| AC-037 | COVERED | `durability_test.go:217` | counts 13 + 1 + 1, log levels, memory at 5 min |
| AC-038 | COVERED | `activity_test.go:115`; web `[AC-038]` | |
| AC-039 | COVERED (web) | `FleetOverview.activity.test.tsx:189` `[AC-039]`: offline 1, expected 2, online 1, total 4 | |
| AC-040 | COVERED | `escalate_test.go:77`; web `[AC-040]`; `hub_test.go` envelope tests | |
| AC-041 | COVERED | `activity_test.go:208`, `:224` | |
| AC-042 | COVERED (web) | `DeviceDetail.activity.test.tsx`, `CommandProgress.activity.test.tsx` | |
| AC-043 | COVERED | no PatchBadge, RebootClassBadge or bulk-button test file is in the diff; the full web suite passes | verified with `git diff --name-only 08cd071..HEAD`; only `labels.test.ts` (49 to 51 event types) and `useDevices.test.ts` (additive) were edited |
| AC-044 | COVERED | `container_test.go:216`; `docker_attribution_keys_test.go:39-79` | |
| AC-045 | COVERED (QA-extended) | `container_test.go:230`, `:244`; QA `TestEX_QA_AC045_PoweredOffIsPerDevice`, `TestEX_QA_AC045_EscalationIsPerDevice` | C-8 gate item incl. `HostWindowOnOtherDeviceIgnored` (INFO-2) |
| AC-046 | COVERED | `models/expect_test.go:98` (bidi U+202E, zero-width, 300-byte with controls and invalid UTF-8); `container_test.go:259` (129-byte shared prefix gives two events and two markers) | C-8 gate item |
| AC-047 | COVERED (QA-extended) | `safety_test.go:170`, `:209`; QA `TestEX_QA_AC047_HelpersAndEditedFilesAreScannedToo`; web `[AC-047]` | scanner self-test proves each forbidden form is caught. See QA-4 |
| AC-048 | DEFERRED | none | technical writer runs after QA (README, CHANGELOG, `internal/server/expect_docs_test.go`). Not a failure |

Zero MISSING. Zero PARTIAL after the QA additions.

## Security conditions (C-1 to C-8)

| Condition | Implementation checked | Tests (all pass) |
|---|---|---|
| C-1 powered-off never covers a new transition; durable invalidation; no re-stamp | `tracker.go:471-479` (stale row deleted, transition alerts), `:556-618` (`NoteOnline` deletes even when not ready), `:624-647` (`NoteRegistration`), `expect_repo.go:233-279` (upsert `WHERE command_id <> ... AND shutdown_at >`, purge predicate, `ListPoweredOff` negation), `handlers.go` registration hook before `DeviceOnline` | all nine `TestEX_SEC001_*` in `sec001_test.go`; PG `expect_repo_pg_test.go:274,319`; handler tests `activity_test.go:141-200` |
| C-2 no window ends without its backstop; pending windows exempt from deletion | `finish.go:132-227` (single routine, eight reasons), `docker_retention.go:11-17,29`, `command_budget.go` via `PendingWindowExemptionSQL`, `tracker.go:881-1059` (overflow priority, deleted rows, lapse) | `finish_test.go:27-221` (table over all eight reasons), PG retention and budget tests |
| C-3 loop stall | `publisher.go` (queue 64, 2 workers, 30 s, drop on full), `tick.go:40-70` (3 s budget), `tracker.go:203-213` (readiness c), reconcile on its own goroutine `:1067-1082` | `publisher_test.go:36,69`, `durability_test.go:203`, `misc_test.go:44`, QA stalled-loop offline case |
| C-4 liveness predicate | `models.DeviceIsLive`; used in `offline.go:67`, `observeLocked`, `RegisterDevice` | `offline_test.go:221` (online and warning), `durability_test.go:262`, `expect_test.go:155` |
| C-5 push ceiling | `publisher.go:168-233`, state map pruned in `flushHeld` | `publisher_test.go:121,142,163` |
| C-6 names | `models/expect.go` `SafeName` (Cc, Cf, Zl, Zp), `ContainerNameKey` (hash suffix), `hexShortID`, SHA-256 marker target with a CHECK <= 64 | `expect_test.go` SEC006 tests, `container_test.go:259,295` |
| C-7 restart series | `offline.go:137-186` | `offline_test.go` SEC007 tests (5 starts 90 s apart gives at most 4 m, config error gives no grace) |
| C-8 QA gate | AC-033, AC-036, AC-045 (both), AC-046, AC-047, `TestEX_LateResultAfterTimeoutDoesNotReopen` (`durability_test.go:275`), every SEC001 to SEC004 test | **all pass**, including the four PG-gated ones against a real PostgreSQL 17 |

AC-047 scan coverage (C-8 last line): the scan printed 34 files. QA diffed the list against `git diff --name-only 08cd071..HEAD -- '*.go'` plus QA's file: **every new `*_test.go` carrying `TestEX_` is in the scan**; `harness_test.go` has no marker but sits under `internal/server/expect/` and is scanned. Files outside the automatic set (the shared double `internal/testutil/mocks_expect.go`, four pre-existing test files edited by the story, and the story's production files) were grepped by QA for `os/exec`, `syscall`, `exec.Command`, `systemctl`, `poweroff`, `reboot -`, `shutdown -`, `halt`, `riot-agent`, `StartProcess`: clean (the only hit is the pre-existing `syscall.SIGTERM` signal handling in `server.go:335`). See QA-4.

## Test Quality Findings

- Tests assert observable behaviour through a real `commands.Service`, a real `events.Generator` and a real `Tracker` over mocks, with an injected clock. Mocks enforce the marker primary key, the upsert rule and the purge predicate, and the PG tests prove the same rules on PostgreSQL. This is real coverage, not exercised-only code.
- The impl report's mutation checks (generator ignoring the tracker fails 55 tests; dropping `NoteRegistration` fails 3; no actor in the fleet dispatch fails one) are consistent with what QA read. QA did not mutate implementation files (not permitted).
- Isolation: no test depends on order (`-count=5` stable). `harness_test.go:301` starts `go hub.Run()` in every harness and never stops it (goroutine left per test; test hygiene only).
- Boundaries: AC-007 (110 s and 130 s), AC-033 (bound minus 1 s and at the bound), the 129-byte name pair, the 200-window cap, 20 + 1 escalation cap, and the 24 h lapse are tested at the boundary.
- Real-time use: only `require.Eventually` in the async publisher tests (impl report note), no sleeps in logic tests.

## Adversarial Findings

Probed with code reading and the added tests. Nothing found that hides a real outage beyond the design's stated residuals.

| ID | Severity | Location | Finding | Action |
|---|---|---|---|---|
| QA-1 | Low | `expect/tracker.go:881-899, 681-686` (`mergeLocked` -> `observeLocked`) | A reconcile reads the device list (`readAll`, `:792`) and merges it later under the lock. If the offline detector flips a device to offline and `CoverOffline` records it between those two points, the stale list says "live" while the tracker says "offline", so the merge treats it as a return: `online=true`, `returnedAt=now`, and it durably deletes the powered-off row and clears the escalation label. The next reconcile or detector pass (at most 30 s) restores the state. Direction is safe (labels and suppression shrink, nothing becomes suppressed); the window is milliseconds. Not demonstrated by a test (needs an interleaving hook). | Document; optional follow-up: ignore a "return" whose device `last_heartbeat` is not newer than the tracker's `offlineSince` |
| QA-2 | Low | `expect/tick.go:49-61` with `finish.go:176-214`, `escalate.go` | ADD AD-009 says a tick completes in at most 8 s (3 s budget plus a 5 s call). The budget is checked between plan items, and one `finishWindow` can make several sequential 5 s DB calls (suppressed deaths, starts, snapshot, targets, marker, close). With a hung database one tick can exceed 15 s. The only consequence is readiness (c) failing, i.e. fail-open (alerts as today), which is the safe direction. | Document; ADD claim is loose, behaviour is safe |
| QA-3 | Low | `events/generator.go` covered `die` branch; `handlers/docker_rate.go:28` | A suppressed death reads and writes no rule cooldown, so inside a window every death that reaches the generator stores an info event. Today a rule with a cooldown stores at most one warning per cooldown. The ADD AD-012 statement "replaces today's warning events 1:1" is exact only for installs without a cooldown. Volume is bounded upstream: the handler admits one event per container and action per minute per device (512 keys), and a window is bounded by its hard bound (at most about 6 h). 90 d event retention applies. | Document; no change needed |
| QA-4 | Info | `expect/safety_test.go:122-168` | The automatic AC-047 scan set (ADD 12 note 9) omits `internal/testutil/mocks_expect.go` (a helper this story created) and the pre-existing test files the story edited. QA verified them by grep and added `TestEX_QA_AC047_HelpersAndEditedFilesAreScannedToo` (test written and passing). | Resolved by QA test |
| QA-5 | Info | `expect/finish.go:141-147` | Reason `deleted` (the command row vanished: device deletion cascade or manual SQL) cannot raise an escalation (its markers cascade away) and writes only an `expect_backstop_skipped` warning event, not a notification. By ADD AD-009 design. Retention cannot delete pending windows (PG-tested), so this needs manual SQL. | Note |
| QA-6 | Info | `events/generator.go` `NewGenerator` | `commandRepo` field and parameter are now unused (impl report S1). Dead state, no behaviour. | Follow-up cleanup |
| QA-7 | Info | `db/docker_retention.go:29`, `server.go:68-73` | New gofmt deviations from this story: `+pendingWindowExemption+` lacks spaces; the `Expect`/`Dispatcher`/`UpdateChecker` struct fields are misaligned. (`server.go` and `generator_integration_test.go` were already not gofmt-clean at base; the working tree is CRLF so `gofmt -l` is run on LF-normalised text.) | Follow-up, cosmetic |
| QA-8 | Info | `handlers/handlers.go` `RegisterDevice` (impl report S7), `offline.go:67` | Behaviour changes to carry into docs: a `warning` device now goes offline when its heartbeat dies (C-4), and a re-registering `warning` device raises no "came online" event. Per the ADD addendum; the technical writer must state them. | For technical writer |
| QA-9 | Info | `agent_shutdown_test.go:100` | AC-028's "3 days" is one `advance(72h)` (a single reconcile and tick). It proves no alert and an intact powered-off state across the jump, and `PurgeInvalidPoweredOff` is PG-tested separately, but it does not simulate 8640 reconciles. Acceptable: the state is derived, not accumulated. | Note |

Probed and found sound:

- **Stuck "expected" state.** Every window has a hard bound `D + E + P` evaluated from the command row, not from reports (`window.go:106-215`). An active command with no reports closes and escalates if the device is offline (`TestEX_SEC002_HardBoundWithoutSweeperEscalates`) and leaves nothing behind if it is online (QA test). Terminal rows are closed by the settle or return bound; unprocessed ones lapse after 24 h with the full backstop; deferred container re-checks lapse after 24 h with a warning event. A restart loop has no grace after 4 min (C-7).
- **Fail-open.** `readyLocked` fails open when not loaded, when the last reconcile is older than 90 s, and when no tick completed within 15 s, on both the container and offline paths (QA added the offline path and the self-heal after one good reconcile).
- **Scope.** Container-class windows cover only their targets (`CoverContainer` intersects keys), never offline; host windows cover every container but never the OOM alert; agent windows cover no container; other devices are never searched.
- **Unbounded growth.** Tracker maps are bounded: windows 200 per device, devices by the device list, `closedRecent` 5 min, `warned` by a fixed set of kinds, push state pruned in `flushHeld`, publisher queue 64 with drop-on-full that keeps the event, activity cache one per device. DB: markers at most 22 per command with cascade, powered-off at most one row per device, one `admin_config` row. WS: activity messages only on change (`reflect.DeepEqual`), no time-varying fields. Logs: suppression at Debug, one Info per escalation, throttled Warn.
- **Injection.** Repository values are bind parameters; the only SQL literals are typed-constant action lists.
- **Auth.** `GET /api/v1/activity` sits in the admin session group (`TestEX_Activity_RouteRequiresAnAdminSession`); 503 without a tracker.
- **Web.** No `any`, `dangerouslySetInnerHTML`, `child_process` or `toLocale*Time` in the new files; the link is `encodeURIComponent`-built.

## Safety (no real power action, no agent execution)

- The AC-047 scan, the helper scan and QA's grep found no import of `os/exec` or `syscall`, no `exec.Command`, no power-command literal and no `riot-agent` path in any file of the story. Power and agent behaviour in the tests is simulated by `FakeAgents`, `MockDispatcher`, `fakeEscalator` and `fakeBroadcaster`.
- The only process QA started was `go test`, `docker run` for a PostgreSQL and a Go container, and `npx`.

## Code standards (CLAUDE.md)

- No `fmt.Print*` or `println` in non-test Go code of the story; no new TODO, FIXME or HACK (grep of every changed file).
- No new `any` in TypeScript (the three `any` hits are pre-existing lines, absent from the diff).
- Clocks injected: the only new `time.Now()` in non-test code are the production defaults at `expect/tracker.go:167`, `server.go` restart series and detector clock. `generator.go` uses `g.now`.
- No silenced errors found: repository failures are logged (throttled) and retried or fail open; `PrepareEscalation` returns an error rather than building a rule-less warning (impl report S2).

## Tests Added by QA

| File | Lines | Covers |
|---|---|---|
| `internal/server/expect/qa_test.go` | `TestEX_QA_AC005_EveryContainerActionCoversOnlyItsTargets` | AC-005 (8 container-class actions) |
| `internal/server/expect/qa_test.go` | `TestEX_QA_AC008_ContainerWindowsNeverCoverOffline` | AC-008, FR-002 |
| `internal/server/expect/qa_test.go` | `TestEX_QA_AC009_OOMDuringHostWindowStillAlerts` | AC-009, FR-011 |
| `internal/server/expect/qa_test.go` | `TestEX_QA_AC045_PoweredOffIsPerDevice`, `TestEX_QA_AC045_EscalationIsPerDevice` | AC-045 |
| `internal/server/expect/qa_test.go` | `TestEX_QA_AC036_OfflineFailsOpenWhenStaleOrStalledAndSelfHeals` | AC-036, SEC-003 |
| `internal/server/expect/qa_test.go` | `TestEX_QA_AC033_StuckActiveCommandOnOnlineDeviceLeavesNothingExpected` | AC-033 |
| `internal/server/expect/qa_test.go` | `TestEX_QA_AC047_HelpersAndEditedFilesAreScannedToo` | AC-047, C-8 |

All carry the `TestEX_` prefix, so the AC-047 scan picks the file up. All pass (`-count=5`, and `-race -count=3` in the Linux container).

## Deviations from ADD

None that change behaviour. Documented deviations 1 to 13, W1 to W7 and S1 to S15 in the impl report were read; QA agrees none breaks an AC or a condition. Two statements in the ADD are looser than the code (QA-2 tick bound, QA-3 "1:1" events); neither affects safety.

## Deviations from FRD

None. AC-048 is deferred to the technical writer.

## Verdict Rationale

**PASS WITH NOTES.** Zero MISSING ACs (AC-048 is deferred by design), all tests green on Windows and under `-race` on Linux, PG-gated tests green on a real PostgreSQL 17, the C-8 gate items all pass, and the AC-047 scan covers every new Go test file. No critical or high finding. QA-1 to QA-9 are low or informational and none blocks shipping.

## Notes for the technical writer (AC-048)

- Table 4.1 actions, settle periods and bounds; suppressed vs still-alerting cases (OOM and non-target containers always alert; agent update hides no container death).
- Powered-off state; restart grace (2 min per start, 4 min per rapid series); NFR-011 and ADD R-2 to R-6 residuals.
- C-5: escalation pushes carry the operator's display name. C-4 and S7 behaviour changes (QA-8). CHANGELOG: no agent upgrade or `install.sh` re-run required.
- Rollback note (INFO-3): after migration 000033 down then up, markers of commands finished in the last hour are gone and may escalate again.

## Action Required

None for the verdict. Optional follow-ups (QA-1, QA-6, QA-7) can be filed separately.

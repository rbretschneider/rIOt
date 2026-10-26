# Implementation Report, part 1 (WP-1, WP-3, WP-4)

- **Story ID:** EXPECT-001 (expected-activity windows)
- **Engineer:** Senior Dev Agent (Go backend and database)
- **Date:** 2026-10-26
- **Scope of this part:** ADD work packages WP-1 (models, migration 000033, repository, mocks, deletion exemptions), WP-3 (commands observer hook) and WP-4 (`internal/server/expect`). WP-2 (generator), WP-5 (server wiring, offline detector, hub, router), WP-6 (handlers) and WP-8 (safety tests) follow in the same worktree. WP-7 (web) was done by another engineer in parallel.

## Detected stack

Go 1.24 (`go.mod`), module `github.com/DesyncTheThird/rIOt`; chi v5, pgx v5, golang-migrate, `log/slog`; PostgreSQL migrations in `cmd/riot-server/migrations`; tests with `testing` plus testify. The frontend (React, TypeScript, Vitest) was not touched. No new dependency.

## Completed components

| File | Action | Notes |
|---|---|---|
| `cmd/riot-server/migrations/000033_expected_activity.up.sql` / `.down.sql` | created | ADD 5.1/5.2 verbatim, plus bound comments. Down→up rollback note below |
| `internal/models/expect.go` | created | Policy table (Table 4.1), constants, `DeviceIsLive`, `SafeName`, `ContainerNameKey`, `ActorPhrase`, escalation, coverage and activity types, message templates of the generator-side events |
| `internal/models/commands.go` | modified | `Command.WindowClosedAt` |
| `internal/models/events.go` | modified | `DockerEvent.TargetKeys`, `CommandAttrExpected`, `CommandAttrEscalation`, `EventExpectBackstopSkipped`, `EventExpectNotificationsHeld` |
| `internal/server/db/expect_repo.go` | created | `ExpectRepo` (all 15 methods of ADD 6.3), `PendingWindowExemptionSQL` |
| `internal/server/db/interfaces.go` | modified | `ExpectedActivityRepository` and the compile-time assertion |
| `internal/server/db/command_repo.go` | modified | `window_closed_at` in `commandColumns` and `scanCommand` |
| `internal/server/db/docker_retention.go`, `internal/server/storage/command_budget.go` | modified | Pending windows are exempt from both `commands` DELETEs (C-2 b) |
| `internal/testutil/mocks_expect.go` | created | `MockExpectedActivityRepo`; `copyCommand` in `mocks.go` copies `WindowClosedAt` (see deviations: separate file) |
| `internal/server/commands/service.go` | modified | `CommandObserver`, `Options.Observer`, call in `broadcast` (WP-3) |
| `internal/server/expect/{tracker,window,finish,escalate,publisher,activity,messages}.go` + `tick.go` | created | WP-4 |
| tests | created | See the test table |

### Exported API for WP-2, WP-5 and WP-6

`expect.New(expect.Options) *Tracker`. Options: `Repo` (`*db.ExpectRepo`), `Devices` (`db.DeviceRepository`), `Policies` (`db.AutoUpdateRepository`), `Telemetry` (`db.TelemetryRepository`), `Commands` (`db.CommandRepository`, used only to label an escalation after a restart), `Escalator` (`*events.Generator`), `Broadcaster` (the hub), `NewestAgentVersion func() string`, `Now`, `GraceEnd`, `Logger`; tuning `QueueSize`, `Workers`, `PushTimeout`, `InlinePublish` (tests only).

| Method | Contract |
|---|---|
| `Load(ctx) error`, `Reconcile(ctx) error` | Until `Load` succeeds once, every `Cover*` returns `ErrNotReady` |
| `Run(ctx)` | Publisher workers, 30 s reconcile goroutine, 1 s loop (wakes on `CommandChanged`). Returns on cancel. `StartPublisher(ctx)` starts only the workers; `Tick(ctx)` is exported for tests |
| `CommandChanged(models.Command)` | `commands.CommandObserver`. Never blocks, no I/O |
| `CoverContainer(deviceID string, keys []string, at time.Time) (models.Coverage, bool, error)` | `keys` = the event's `TargetKeys`. Memory only. `Coverage.DeathMessage(key)` and `OOMMessage(key)` render the texts |
| `CoverOffline(ctx, deviceID string, offlineAt time.Time) (models.OfflineCoverage, bool, error)` | Also records the transition. `Kind` is `window` or `powered_off`; `OfflineCoverage.Message(host)` renders the text. `offlineAt = min(now, lastHeartbeat+45s)` is the caller's job |
| `NoteOnline(ctx, deviceID, at) *models.OnlineNote` | Always clears the powered-off row and escalations durably. `OnlineNote.Message(host)`. Call it before the 5-minute cooldown of `DeviceOnline` |
| `NoteRegistration(deviceID, at)` | Every re-registration of an existing device, including `wasOnline=true`. Call it **before** `DeviceOnline`/`NoteOnline` (see deviations 9) |
| `ObserveDevices([]models.Device, at)` | Offline worker, after each pass |
| `Activities() models.ActivityResponse` | `GET /api/v1/activity` body. The generated activity of one device also goes out as `Broadcaster.BroadcastActivity(deviceID, *DeviceActivity)` (nil clears) |
| `Ready() error`, `WindowCount() int`, `Stats()` | Diagnostics |

`expect.Escalator` (implemented by the generator in WP-2):
- `PrepareEscalation(ctx, models.EscalationRequest) (models.PreparedEscalation, error)`: find the enabled rule for `req.EventType()` (`device_offline` for device kinds, `container_died` for container kinds and the summary); build the event with the rule's severity or `warning`; return `Skip=true` when the container is excluded. `req.Message` is already rendered (AD-015). It stores and sends nothing.
- `PublishEscalation(ctx, evt models.Event, rule *models.AlertRule, hostname string) error`: dispatch the notification only (no cooldown read or write, X-8). The hub broadcast goes through `expect.Broadcaster.BroadcastEvent`, so the generator must not broadcast it twice.

`expect.Broadcaster`: `BroadcastEvent(*models.Event)` (exists on the hub) and `BroadcastActivity(deviceID string, a *models.DeviceActivity)` (WP-5 adds it; WS message `{"type":"activity_update","device_id":…,"data":…|null}`).

## Test summary

AC and SEC tests carry the ADD names. `H` tests are in package `expect_test`, `W` in package `expect`.

| Reference | Test file | Tests |
|---|---|---|
| AC-001 to AC-010, AC-044 to AC-046, SEC-006 (H) | `internal/server/expect/container_test.go` | `TestEX_AC001_…` to `TestEX_AC010_…`, `TestEX_AC044_…`, `TestEX_AC045_CrossDeviceCommandIDIgnored`, `TestEX_AC045_HostWindowOnOtherDeviceIgnored`, `TestEX_AC046_SuppressedAndEscalatedMessagesSanitized`, `TestEX_SEC006_EventFieldsSanitized` |
| AC-011 to AC-020, AC-022 (H) | `host_test.go` | one per AC |
| AC-025 to AC-030 (H) | `agent_shutdown_test.go` | `AgentRestartOfflineInfo`, `AgentNotBackEscalates`, `AgentUpdateDoesNotHideDeaths`, `ShutdownPoweredOffNoAlertsThreeDays`, `PoweredOffEndsOnOnline`, `FailedShutdownNoPoweredOff` |
| AC-031 to AC-037, SEC-003 (c), SEC-004 (H), late result, cap | `durability_test.go` | `WindowsSurviveRestart`, `DowntimeEscalationOnceAcrossRestarts`, `SilentCommandsCloseAndAlertingResumes` (12 actions), `QueuedRebootDoesNotSuppress`, `FailOpenWhenNotLoaded`, `FailOpenWhenStale`, `RebootWindowEventCountsLogsMemory`, `CoverFailsOpenWhenLoopStalled`, `TrackerUsesSameLivenessPredicate`, `TestEX_LateResultAfterTimeoutDoesNotReopen`, `TestEX_Tracker_CapPerDevice` |
| SEC-001 (H) | `sec001_test.go` | nine tests, `SecondOfflineAlerts` runs six scenarios |
| SEC-002 (F) | `finish_test.go` | eight tests, `AnyEndWithDeviceOfflineEscalatesOnce` runs all eight end reasons |
| SEC-003, SEC-005 (P) | `publisher_test.go` | `BlockingPublisherDoesNotStallLoop`, `PublishQueueFullDropsPushKeepsEvent`, `PushCeilingHoldsAndSummarizes`, `HeldEscalationsStillStoredExactlyOnce`, `SummaryAtMostOncePerHour` |
| AC-021, AC-040 (E) | `escalate_test.go` | `TwentyPlusSummary`, `CapHoldsAcrossRestart`, `DispatchAndCloseBroadcastWithinOneTick` |
| AC-024, AC-025, AC-026, AC-028, AC-034, AC-038, AC-041 (A) | `activity_test.go` | one per ADD name, plus escalation after a restart |
| AC-004, AC-007, AC-033 (W) | `window_test.go` | `BulkWindowCoversThreeHours`, `ContainerSettleBoundary` (strict at the boundary), `EveryWindowHasHardBound` (all 12 actions), plus early close, non-success, shutdown, lapse |
| SEC-003 (F), FR-007, FR-015, SEC-004 | `misc_test.go` | `TickBudgetCarriesOver`, `ExactTargetBeatsHostWindow`, `LatestDispatchWinsAmongEquals`, excluded container, observe cases |
| AC-002, AC-008, AC-046, SEC-004, SEC-006 | `internal/models/expect_test.go` | policy table exact, migration predicate equals `ExpectActions()` (no database needed), actor phrase, `SafeName`, `ContainerNameKey`, marker target, message templates |
| Observer | `internal/server/commands/observer_test.go` | `TestEX_Observer_FiresOnAcceptedTransitionsOnly` |
| PG: SEC-001, SEC-002, `TestEX_Repo_PG_*` | `internal/server/db/expect_repo_pg_test.go`, `internal/server/storage/command_budget_expect_pg_test.go` | migration up, down, up; backfill; lists; exactly-once under 12 concurrent callers; cascades; purge predicate; no re-stamp; retention and budget exemptions |

Not in this part by assignment: AC-023 and the generator tests (WP-2); AC-035, SEC-004 (O), SEC-007 (WP-5); AC-024 (handlers), AC-044 (keys) in handlers (WP-6); AC-047 (WP-8); AC-048 (docs). AC-038, AC-039, AC-040, AC-041, AC-042 web tests are WP-7.

Mutation checks (temporary edits, reverted): an inclusive window end, a covering stale powered-off row, no `closedRecent` guard, no device backstop, no container backstop, no escalation cap and no push ceiling each make at least one named test fail.

### Test runs

Local PostgreSQL for the PG-gated tests: a throwaway `postgres:17-alpine` container (the ADD names PostgreSQL 16; nothing used differs between the two). It was removed afterwards.

New tests only (`go test ./internal/models/ ./internal/server/expect/ ./internal/server/commands/ -run EX_ -v`): 110 passed, 0 failed. Tail:

```
--- PASS: TestEX_Observer_FiresOnAcceptedTransitionsOnly (0.00s)
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.926s
ok  	github.com/DesyncTheThird/rIOt/internal/server/expect	0.666s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	0.434s
```

Full suite, no database (`go test ./... -count=1`, Windows, no `-race`): 30 packages ok, no FAIL. Tail:

```
ok  	github.com/DesyncTheThird/rIOt/internal/server/expect	1.059s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	10.697s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	2.014s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	1.630s
ok  	github.com/DesyncTheThird/rIOt/scripts	53.108s
```

PG-gated (`RIOT_TEST_PG_URL` set): `db` 108 s, `storage`, `commands`, `handlers` and `server` ok; all 11 `TestEX_*PG*` tests pass, and every existing PG test still passes against migration 000033.

Race detector (not available on this Windows host, no C compiler): run in a `golang:1.24` container, `go test -race ./internal/server/expect/ ./internal/server/commands/ ./internal/models/`: all ok. 15 repeated runs of the `expect` package on Windows: no flake.

Static checks: `go build ./...` and `go vet ./...` clean; `gofmt -l` clean on every file of this part. The repository has no other linter configured.

One unrelated failure was seen once in a full parallel run: `internal/agent` `TestShutdown_SD_AC030_LateNonZeroExitLogsOneWarn` (a timing test; no agent file is touched by this story). It passed on every re-run, alone (3 times) and in the next two full runs.

## Deviations from the ADD (architect review)

1. **Mocks file.** `MockExpectedActivityRepo` is in `internal/testutil/mocks_expect.go`, not appended to `mocks.go` (same package; only the `copyCommand` change is in `mocks.go`). Reason: `mocks.go` is 2,500 lines and an append through the shell was not reliable.
2. **Where the message templates live.** The templates of events the generator stores (suppressed death, OOM-in-window, suppressed offline, powered off, online after a window) are methods on `models.Coverage`, `OfflineCoverage` and `OnlineNote` in `models/expect.go`, so the generator renders them without importing `expect`. `expect/messages.go` holds the escalation, summary and warning texts.
3. **`evaluate` is unexported** (the ADD names `Evaluate`); nothing outside the package needs it. Tick, plan and the powered-off loop entry are in `expect/tick.go` (extra file).
4. **`Escalator` and collaborator signatures** were open in the ADD. `PrepareEscalation` returns `models.PreparedEscalation{Event, Rule, Skip}`; `PublishEscalation` dispatches only; `Broadcaster` has `BroadcastEvent` and `BroadcastActivity`. `Options.Commands` (a `db.CommandRepository`) is an extra collaborator: it labels an open escalation (action, actor) after a restart, when its command is no longer pending. `TelemetryReader` needs `LatestSnapshotTime` and `GetLatestSnapshot` (both on `db.TelemetryRepository`).
5. **Existing PG test helpers edited** for the new column: `command_repo_pg_test.go` `repo()` migrates to the latest version (it migrated to 32, which would now drop the column), and the stub `commands` table of `command_budget_pg_test.go` gets `action` and `window_closed_at`. No assertion changed.
6. **`SafeName` also strips line and paragraph separators** (Zl, Zp) beyond Cc and Cf. They render as line breaks in a notification.
7. **Before the end of the restart grace, `Tick` finishes no window at all** (not only the online-state decisions). Everything the ADD lists for the grace (no escalation, no early close, no loop powered-off entry) follows, plus the container checks; coverage lookups work throughout.
8. **A container check that falls due right after a restart waits 3 minutes.** When the device came back after the window's close time and has been back for under 3 minutes (this includes every device at the first minutes after a start), finishing defers the container check like AD-011's deferral. Reason: a restart sets `onlineSince` to the start time, and checking containers seconds after a server start would escalate containers that have not been observed yet. It bounds a restart-delayed escalation at 3 minutes.
9. **`NoteRegistration` keeps the powered-off record it ended** (`endedPO`, dropped after 5 minutes or consumed by `NoteOnline`). The handler calls `NoteRegistration` before `DeviceOnline`; without this, the row was already gone when `NoteOnline` ran and FR-030's "came online after Shutdown by ryan (powered off 72h)" could not be produced. Memory bound: one record per device.
10. **The loop's powered-off entry stores no info event** (restart case only; the normal entry stores it through the generator). A transition that alerted because the powered-off row could not be stored sets an in-memory `noPoweredOff` flag on the window so the loop never turns it into a silent powered-off state afterwards.
11. **H tests use a stand-in for the generator** (`harness_test.go`: `containerDied`, `containerOOM`, `deviceOffline`, `register`). The ADD names `events.Generator` there, but WP-2 does not exist yet. The stand-in does what AD-005 says (covered: info event linked, never notified; not covered or error: alert). Those four methods are the single seam: re-point them at `*events.Generator` when WP-2 lands. Tests that only pin the generator side of the contract: AC-009 (OOM message), AC-010 (exclusion first), the `Err` cases of AC-036. AC-023 is a generator test (G) and is left to WP-2.
12. **Names:** the H test for AC-033 is `TestEX_AC033_SilentCommandsCloseAndAlertingResumes` (the ADD's H name); the W test keeps `TestEX_AC033_EveryWindowHasHardBound`.
13. **Not done in this part:** the two retired generator tests (X-13) and the generator changes (WP-2); the README note on the operator name, the series bound and the INFO-1 wording (technical writer).

### Rollback note (INFO-3)

After `000033` down then up, the escalation markers of commands that finished in the last hour are gone, so those commands can escalate a second time. The down migration drops only the new state; nothing else is lost. The migration was run up, down, up on PostgreSQL by `TestEX_Repo_PG_MigrationUpDownUp`.

## Notes for the next engineer (WP-2, WP-5, WP-6) and for QA

- **Never assign a nil `*expect.Tracker` to an interface** (`commands.Options.Observer`, `events.ExpectedActivity`, `handlers.ActivitySource`).
- **WP-2:** `events.ExpectedActivity` is satisfied by `*expect.Tracker` as the ADD gives it. Use `models.ContainerNameKey(evt.ContainerName)` for `Event.ContainerName`, and `[0-9a-f]`-only `ContainerID`. A covered death is an `info` event with `CommandID`, `CommandAttr = models.CommandAttrExpected`, message `cov.DeathMessage(key)`, no cooldown read or write. A covered OOM is never suppressed: use `cov.OOMMessage(key)`. `DeviceOfflineAt` must pass `offlineAt = min(now, lastHeartbeat+45s)`; a `powered_off` kind stores the info event with `cov.Message(host)` too. `DeviceOnline` calls `NoteOnline` before its cooldown check and uses `note.Message(host)`. `PublishEscalation` must not broadcast to the hub (the publisher does).
- **WP-5:** `tracker.Load(ctx)` after `expect.New` and before `go tracker.Run(ctx)`. The tracker needs `GraceEnd` from the restart-series computation. Call `ObserveDevices` after each detector pass with updated statuses. The hub's `BroadcastActivity` must not block the tracker (the publisher already isolates it, 2 workers, queue 64).
- **WP-6:** `ReceiveDockerEvent` must fill `DockerEvent.TargetKeys` (the field exists, `json:"-"`). `RegisterDevice` calls `NoteRegistration` on every re-registration, before `DeviceOnline`.
- **QA:** the AC-047 scan will find the new test files by `TestEX_`; none contains `os/exec`, `syscall`, `systemctl`, `poweroff` or `riot-agent` (checked with a grep). The fake agents and escalator never execute anything.
- The windows use the tracker's injected clock only; the one real-time dependence is `context.WithTimeout` (5 s per database call, 30 s per push), and the async publisher tests wait with `require.Eventually`.

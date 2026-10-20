# PART-001 Implementation Report

**Story:** PART-001 — Time-Range Partitioning with Drop-Partition Retention for High-Churn Tables
**Status:** Implementation complete; unit + integration tests green (integration tests verified locally against a live PostgreSQL 16 container); ready for QA review.
**Engineer:** Senior Dev Agent
**Date:** 2026-09-10

## Detected Stack

- **Backend:** Go 1.24, chi v5, pgx v5.8.0, `log/slog`, `golang-migrate/migrate/v4` (postgres + iofs + file source drivers, all already vendored — no new dependency).
- **DB:** PostgreSQL 16 (companion container). Migrations embedded via `go:embed` in `cmd/riot-server/main.go`, applied by `DB.RunMigrations` (`internal/server/db/db.go`) before the server serves traffic.
- **Frontend:** Not touched by this story (schema + background-job only, per FRD/ADD). `npm run test:run` and `tsc --noEmit` re-verified green as a regression check only.

## What was implemented

Converted the six high-churn tables (`heartbeats`, `telemetry_snapshots`, `device_logs`, `container_metrics`, `container_logs`, `probe_results`) from plain heaps purged by `DELETE` to `PARTITION BY RANGE` tables retained by dropping whole partitions, per the approved ADD:

1. **Migrations `000024`–`000028`** (5 files × up/down = 10 files): bookkeeping table + data-derived `cutoff` (AD-003), pre-built composite PK index (AD-002 #2), bound `VALIDATE` (AD-002 #3), the metadata swap — rename → composite-PK parent → attach legacy bounded partition → attach forward `DEFAULT` catch → adopt every FR-003 index → initial forward daily partitions (AD-001/AD-002 #4/AD-004/AD-008) — and FK addition (AD-002 #5, **adapted** — see Deviations).
2. **`internal/server/partition/`** — new package: `plan.go` (pure date-math/name-parsing/spec-building, no DB), `manager.go` (executor-abstracted scheduler: `EnsureAll`, `DropAged`, `Run`, `Health`, benign-error classification, health/stall escalation), `plan_test.go` + `manager_test.go` (DB-free unit tests via a fake executor and a slog-capturing handler), `integration_test.go` (build-tag `integration`, live-PG tests, skip cleanly with no DB configured).
3. **`internal/server/server.go`** — wires `partition.Manager`, calls `EnsureAll` synchronously post-migration/pre-serving, starts `Run(ctx)` alongside the other background workers, and trims `runRetention` down to the five out-of-scope purges (refactored into a package-level, interface-driven, unit-testable function).
4. **Repo/interface/mock cleanup** — removed the six orphaned `Purge*` methods from `TelemetryRepo`, `ProbeRepo`, `ContainerLogRepo`, `ContainerMetricRepo`, `DeviceLogRepo` and their interfaces/mocks; ingest paths in all five repos are byte-for-byte unchanged.
5. **CI** — added a `test-go-integration` job (Postgres 16 service) to `.github/workflows/ci.yml` so the AC-019/AC-022 etc. gates actually run (residual R1 — see Deviations).

## Files Changed

### Created (migrations)

| File | Purpose |
|---|---|
| `cmd/riot-server/migrations/000024_partition_bookkeeping.{up,down}.sql` | `partition_conversion` table; per-table data-derived, self-healing `cutoff`; `NOT VALID` bound `CHECK` (AD-002 #1, AD-003) |
| `cmd/riot-server/migrations/000025_partition_pk_index.{up,down}.sql` | Pre-build `<t>_pk_new` composite unique index per table (AD-002 #2) |
| `cmd/riot-server/migrations/000026_partition_validate_bound.{up,down}.sql` | `VALIDATE CONSTRAINT <t>_pbound` (AD-002 #3) — down is a documented no-op (cannot un-validate) |
| `cmd/riot-server/migrations/000027_partition_swap.{up,down}.sql` | The metadata swap (AD-001, AD-002 #4, AD-004, AD-008) |
| `cmd/riot-server/migrations/000028_partition_validate_fk.{up,down}.sql` | Add the five outgoing FKs (adapted for PG16 — see Deviations) |

### Created (Go)

| File | Purpose |
|---|---|
| `internal/server/partition/plan.go` | Pure functions: `PartitionName`, `ParsePartitionDay`, `PartitionsToCreate`, `PartitionsToDrop`, `BuildSpecs`, plus `TableSpec`/`PartitionInfo`/`Health`/`TableHealth` types |
| `internal/server/partition/plan_test.go` | 24 unit tests, no DB |
| `internal/server/partition/manager.go` | `Manager`, executor interface + `pgxExecutor`, benign-error classification, health/stall escalation, `%I`/`%L`-equivalent quoting (`sqlIdent`/`sqlLiteral`) |
| `internal/server/partition/manager_test.go` | 15 unit tests using a fake `executor` + a slog-capturing handler, no DB |
| `internal/server/partition/integration_test.go` | 13 tests, `//go:build integration`, live PostgreSQL 16 required, skip cleanly otherwise |
| `internal/server/server_test.go` | New file — retention wiring tests (AC-014, AC-015), no DB |

### Modified

| File | Purpose |
|---|---|
| `internal/server/server.go` | Import `partition`; `PartitionManager` field; construct + `EnsureAll` synchronously post-migration/pre-serving; `go s.PartitionManager.Run(ctx)`; `runRetention` trimmed to the five out-of-scope repos and refactored into a testable package-level function |
| `internal/server/db/telemetry_repo.go` | Removed `PurgeHeartbeats`, `PurgeSnapshots` |
| `internal/server/db/probe_repo.go` | Removed `PurgeResults` (probe_results) |
| `internal/server/db/container_log_repo.go` | Removed `Purge`; insert path untouched |
| `internal/server/db/container_metric_repo.go` | Removed `Purge`; insert path untouched |
| `internal/server/db/device_log_repo.go` | Removed `Purge`; dropped now-unused `time` import; insert path untouched |
| `internal/server/db/interfaces.go` | Removed the six methods from `TelemetryRepository`, `ProbeRepository`, `DeviceLogRepository`, `ContainerLogRepository`, `ContainerMetricRepository` |
| `internal/testutil/mocks.go` | Removed the six corresponding mock methods; added spy fields (`PurgeCalled` etc.) to the five out-of-scope mocks for AC-015; **added `MockLogRepo`** (did not exist before — required for `db.LogRepository`, see Deviations) |
| `.github/workflows/ci.yml` | **Added** `test-go-integration` job (see Deviations) |

> Not changed, as specified: every insert/read repo query, `internal/server/config.go` (no new env var), the agent, the ingest API, migrations `000001`–`000023`. `DeviceProbeRepo.PurgeResults` is kept (different table, out of scope).

## AC-to-Test Mapping

**Unit** = pure Go function or mock-injected, no DB, runs in default `go test ./...`. **Integration** = `//go:build integration`, requires a live PostgreSQL 16 (`RIOT_TEST_DB_URL`); skips cleanly otherwise; verified locally against a throwaway `postgres:16-alpine` container and now wired into CI (residual R1).

| AC | Strategy | Test(s) |
|---|---|---|
| AC-001 | Integration | `TestIntegration_AC001_AC002_TablesArePartitionedOnCorrectKey`; Unit (spec-level) `Test_AC001_BuildSpecs_other_five_tables_key_on_timestamp` |
| AC-002 | Integration + Unit | same test above; `Test_AC002_BuildSpecs_probe_results_keys_on_created_at` |
| AC-003 | Integration | `TestIntegration_AC003_CompositePrimaryKeyIncludesPartitionKey` |
| AC-004 | Integration | `TestIntegration_AC004_AllPreExistingIndexesPreserved` |
| AC-005 | Integration | `TestIntegration_AC005_ForeignKeysPreservedContainerLogsFKFree` |
| AC-006 | Integration | `TestIntegration_AC006_AC016_AC019_SwapIsMetadataOnly_NoRewrite` (tiny + large fixture, relfilenode-based) |
| AC-007 | Integration | `TestIntegration_AC007_ConversionIsIdempotentOnReRun`, `TestIntegration_AC007_AC018_FullUpDownUpCycle_AllSixTables` |
| AC-008 | Integration | `TestIntegration_AC008_FreshEmptyDatabase_ConvertsCleanly`; also verified by booting the real compiled `riot-server` binary against a fresh DB (see Verification Beyond Unit Tests) |
| AC-009 | Integration | `TestIntegration_AC009_ExistingQueriesContinueToWork` (plain INSERT, `RETURNING id`, `CopyFrom`, staged `INSERT…SELECT…ON CONFLICT` dedup) |
| AC-010 | Unit + Integration | `Test_AC010_*` (7 tests: naming, UTC-day floor, window math, near-midnight boundary); Integration coverage folded into AC-008/AC-016 (partitions created and reachable) |
| AC-011 | Unit | `Test_AC011_PartitionsToDrop_*` (5 tests incl. exact-boundary/1s-within-window), `Test_AC011_isDroppableName_*` |
| AC-012 | Unit | `Test_AC012_PartitionsToDrop_drops_legacy_once_cutoff_is_older_than_window`, `Test_AC012_PartitionsToDrop_retains_legacy_while_cutoff_still_within_window`, `Test_AC012_listPartitions_excludes_legacy_when_cutoff_unreadable` |
| AC-013 | Unit | `Test_AC013_BuildSpecs_*` (4 tests) |
| AC-014 | Unit | `TestPartitionRetentionCutover_AC014_AC015/[AC-014]_...` (compile-enforced: the six methods no longer exist on the repo interfaces) |
| AC-015 | Unit | `TestPartitionRetentionCutover_AC014_AC015/[AC-015]_...` (2 subtests: all five still called; one failure doesn't block the others) |
| AC-016 | Integration | Folded into `TestIntegration_AC006_AC016_AC019_...` (large fixture) and `TestIntegration_AC009`; also verified by booting the real binary against a 20,000-row populated legacy-style DB (see below) |
| AC-017 | Unit | `Test_AC017_*` (4 tests: INFO on create/drop, ERROR + per-table isolation + retry-next-cycle on genuine failure, WARN not ERROR on a missing bookkeeping row) |
| AC-018 | Integration | `TestIntegration_AC018_DownMigrationBestEffortReversal`, `TestIntegration_AC007_AC018_FullUpDownUpCycle_AllSixTables` |
| AC-019 | Integration (hard gate) | `TestIntegration_AC006_AC016_AC019_SwapIsMetadataOnly_NoRewrite` — captures `pg_relation_filenode` of the legacy heap and every legacy child index before/after `000027` on both a 10-row and a 5,000-row fixture; asserts unchanged. **This is a real assertion, not illustrative — a relfilenode change fails the test.** |
| AC-020 | Unit | `Test_AC020_*` (9 tests: `LastError`/non-advancing `LastSuccessAt`, single escalated ERROR after N=3, reset-after-success, non-zero `DefaultRowCount` WARN + `Health()` value, low-coverage WARN, `Health()` returns an independent copy, and — added in the post-QA fix — per-operation stall escalation: a persistent single-operation failure (`EnsureAll` or `DropAged`) escalates independently even while the other operation keeps succeeding every cycle) |
| AC-021 | Unit (021a) + Integration (021b) | `Test_AC021a_PartitionsToCreate_*` (2 tests: no day below cutoff, empty-when-cutoff-beyond-window); `TestIntegration_AC021b_CopyFromWithFutureSkewedRowSucceedsViaDefault`; benign-classification unit slice `Test_AC021b_classifyBenign_*` |
| AC-022 | Integration | `TestIntegration_AC022a_CutoffDerivedFromMaxKey_ValidatesWithFutureDatedRow`, `TestIntegration_AC022b_EmptyTableCutoffFallsBackToNow`, `TestIntegration_AC022c_DirtyPreSwapReRunRecomputesCutoff` |

Residual R6 (benign set doesn't swallow disk-full/permission): `Test_R6_classifyBenign_does_not_swallow_disk_full_or_permission_errors`.

## Test Run Output

### Go — default suite (`go test ./...`, no `-race` per Windows dev-host instruction)

```
go build ./...   → clean (exit 0)
go vet ./...     → clean (exit 0)
go test ./...    → all packages pass (exit 0)

ok  github.com/DesyncTheThird/rIOt/internal/agent                 0.749s
ok  github.com/DesyncTheThird/rIOt/internal/agent/collectors      0.966s
ok  github.com/DesyncTheThird/rIOt/internal/models                0.371s
ok  github.com/DesyncTheThird/rIOt/internal/resilient              1.008s
ok  github.com/DesyncTheThird/rIOt/internal/server                0.860s
ok  github.com/DesyncTheThird/rIOt/internal/server/auth            0.623s
ok  github.com/DesyncTheThird/rIOt/internal/server/ca              0.602s
ok  github.com/DesyncTheThird/rIOt/internal/server/events          0.697s
ok  github.com/DesyncTheThird/rIOt/internal/server/handlers        1.933s
ok  github.com/DesyncTheThird/rIOt/internal/server/middleware      0.730s
ok  github.com/DesyncTheThird/rIOt/internal/server/notify          0.920s
ok  github.com/DesyncTheThird/rIOt/internal/server/oidc            2.150s
ok  github.com/DesyncTheThird/rIOt/internal/server/partition       0.593s
ok  github.com/DesyncTheThird/rIOt/internal/server/probes          0.974s
ok  github.com/DesyncTheThird/rIOt/internal/server/scoring         0.598s
ok  github.com/DesyncTheThird/rIOt/internal/server/summary         0.757s
ok  github.com/DesyncTheThird/rIOt/internal/server/updates         0.623s
ok  github.com/DesyncTheThird/rIOt/internal/server/websocket       0.658s

706 subtests passed, 0 failed (go test ./... -v | grep -c "^--- PASS/FAIL")
```

`internal/server/partition` unit suite alone: **39 test functions/subtests, all PASS** (24 in `plan_test.go`, 15 in `manager_test.go`). `internal/server` gained `TestPartitionRetentionCutover_AC014_AC015` (3 subtests, all PASS).

### Go — integration suite (`go test -tags=integration ./internal/server/partition/...`)

Run locally against a throwaway `postgres:16-alpine` Docker container (`RIOT_TEST_DB_URL=postgres://riot:riot@localhost:55432/riot?sslmode=disable`), since this Windows dev host has no local Postgres:

```
=== RUN   TestIntegration_AC008_FreshEmptyDatabase_ConvertsCleanly              --- PASS
=== RUN   TestIntegration_AC001_AC002_TablesArePartitionedOnCorrectKey          --- PASS
=== RUN   TestIntegration_AC003_CompositePrimaryKeyIncludesPartitionKey         --- PASS
=== RUN   TestIntegration_AC004_AllPreExistingIndexesPreserved                  --- PASS
=== RUN   TestIntegration_AC005_ForeignKeysPreservedContainerLogsFKFree         --- PASS
=== RUN   TestIntegration_AC006_AC016_AC019_SwapIsMetadataOnly_NoRewrite        --- PASS
    --- PASS: .../tiny_fixture (10 rows)
    --- PASS: .../large_fixture (5000 rows)
=== RUN   TestIntegration_AC007_ConversionIsIdempotentOnReRun                   --- PASS
=== RUN   TestIntegration_AC009_ExistingQueriesContinueToWork                   --- PASS
=== RUN   TestIntegration_AC018_DownMigrationBestEffortReversal                 --- PASS
=== RUN   TestIntegration_AC021b_CopyFromWithFutureSkewedRowSucceedsViaDefault  --- PASS
=== RUN   TestIntegration_AC022a_CutoffDerivedFromMaxKey_ValidatesWithFutureDatedRow --- PASS
=== RUN   TestIntegration_AC022b_EmptyTableCutoffFallsBackToNow                 --- PASS
=== RUN   TestIntegration_AC022c_DirtyPreSwapReRunRecomputesCutoff              --- PASS
=== RUN   TestIntegration_AC007_AC018_FullUpDownUpCycle_AllSixTables            --- PASS

PASS
ok  github.com/DesyncTheThird/rIOt/internal/server/partition  ~50s
```

53 total `--- PASS`, 0 `--- FAIL` (unit + integration combined, full package run with `RIOT_TEST_DB_URL` set).

When `RIOT_TEST_DB_URL`/`RIOT_DB_URL` is **not** set, every integration test calls `t.Skip(...)` and the run exits 0 — verified explicitly before the Docker container was available.

### Frontend (regression check only — no frontend files changed)

```
npm run test:run   → 26 files, 359 tests passed
npx tsc --noEmit   → clean (exit 0)
```

## Verification Beyond Unit Tests

Given the severity of getting a partitioning migration wrong on a production-scale database, I additionally:

1. **Started a real `postgres:16-alpine` Docker container** and ran the full integration suite against it (see above) — this is not simulated; every SQL statement in migrations `000024`–`000028` executed for real, including the metadata swap, `VALIDATE`, FK addition, and both up/down directions.
2. **Compiled the real `riot-server` binary** (`go build ./cmd/riot-server`) and booted it against:
   - a **fresh, empty** database — confirmed clean boot, all six tables converted, initial daily partitions created, server reaches `"server starting"` and serves HTTP (AC-008).
   - an **already-populated, pre-`000024` ("legacy-style") database** seeded with 20,000 `container_logs` rows on migration version 23 — confirmed the boot completes in ~1.3 seconds total (connect + migrate), all 20,000 rows survive in `container_logs_legacy`, `container_logs` is `relkind='p'` (partitioned), and the server proceeds to serve traffic (AC-016, NFR-001).
3. Both Docker artifacts (test container, seeded database) were torn down after verification; no test infrastructure was left running.

This surfaced and fixed two real defects that a review of the ADD's SQL alone would not have caught (see Deviations below) — I would not have caught either without executing against a real PostgreSQL 16 instance.

## Deviations from ADD

These are implementation-necessary corrections discovered by running the migrations against a live PostgreSQL 16, not architectural changes — the resulting schema/behavior matches the ADD's intent (AD-001/AD-002/AD-004) in every case. Flagged here per the "document deviations" rule; none of them change the file list, the AC set, or NFR-001/NFR-002's guarantees.

1. **Composite-PK / lookup-index "adoption" required two extra steps not spelled out in the ADD's illustrative SQL (file `000027`).**
   - The ADD's walkthrough renames the legacy heap and pre-built composite index, then attaches. Empirically (verified via `pg_relation_filenode` before/after on a live PG16), this **alone does not adopt the pre-built index** — PostgreSQL requires the legacy table's original single-column `PRIMARY KEY` (`<t>_pkey`) to be **dropped first** and the pre-built composite unique index **promoted to a real `PRIMARY KEY` constraint** via `ADD CONSTRAINT ... PRIMARY KEY USING INDEX <t>_legacy_pk`, before `ATTACH`. Without this, PostgreSQL silently builds a **brand-new** local PK index under the swap's held `ACCESS EXCLUSIVE` lock — a real, unrelfilenode-stable, un-adopted rebuild that quietly violates NFR-001 while still "looking like" success (no error is raised).
   - Similarly, index **names are schema-scoped in PostgreSQL, not table-scoped**. The legacy child keeps its original lookup-index names (e.g. `idx_container_logs_lookup`) after the rename, so the parent-level `CREATE INDEX idx_container_logs_lookup ON container_logs (...)` in the ADD's literal SQL **fails outright** with `relation "idx_container_logs_lookup" already exists`. The fix: rename the legacy child's lookup/dedup indexes out of the way (`<t>_legacy_<suffix>`) immediately after the table rename, before creating the parent-level indexes with the canonical FR-003 names. Adoption matches by index **definition** (columns/expression/opclass), not name, so this is purely cosmetic on the legacy side and was verified via `pg_inherits` parent/child linkage and unchanged `pg_relation_filenode` for both the heap and every index, on a 5,000-row fixture (`TestIntegration_AC006_AC016_AC019_SwapIsMetadataOnly_NoRewrite`).
   - Both fixes are documented inline in `000027_partition_swap.up.sql`'s header comment and preserved in the corresponding `.down.sql` (which restores the original single-column PK and original index names).

2. **PostgreSQL 16 does not support `ADD CONSTRAINT ... FOREIGN KEY ... NOT VALID` on a partitioned table at all** (`ERROR: cannot add NOT VALID foreign key on partitioned table ... This feature is not yet supported on partitioned tables.` — lifted in PG 17, confirmed empirically against a live PG 16.13 instance). AD-002's file `000027`/`000028` split (add `NOT VALID` in the swap, `VALIDATE` separately) is **impossible** for FKs on the target PostgreSQL version. Fix: FK addition is removed entirely from `000027` and file `000028` now performs a single, **validating** (non-`NOT VALID`) `ADD CONSTRAINT ... FOREIGN KEY` for the five FK'd tables, in its own transaction — critically, **separate from the swap's transaction**, so the FK's O(rows) validation scan runs under `SHARE ROW EXCLUSIVE` (blocks writes, not reads — never `ACCESS EXCLUSIVE`) rather than while the swap's `RENAME`-induced `ACCESS EXCLUSIVE` is still held. This is the same lock-character trade the ADD already accepts for file `000025`'s index build, applied for the same reason (pre-serving boot, single-instance target). Fully documented in `000028_partition_validate_fk.up.sql`'s header comment.

3. **`internal/testutil/mocks.go` gained one new mock type, `MockLogRepo`.** No mock existed for `db.LogRepository` prior to this story; AC-015's unit test (assert the five out-of-scope purges still run) needs one to exercise `runRetention` without a live DB. This is an addition within a file already listed in ADD Section 4 for modification, not a new file.

4. **`.github/workflows/ci.yml` gained a new job, `test-go-integration`.** Not listed in ADD Section 4, but required to satisfy security re-review **residual R1** ("the AC-019 gate must actually run on a populated fixture in the PG-backed CI job and must fail the build ... confirm it is not skipped when PG is unavailable"). Without this, the integration-gated tests — including the AC-019 hard DoD gate — would never execute automatically (the existing `test-go` job has no Postgres service and does not pass `-tags=integration`). The new job spins up a disposable `postgres:16-alpine` service and runs `go test -tags=integration ./internal/server/partition/...`; it does not modify the existing `test-go`/`test-web`/`lint` jobs.

## How Residuals R1–R6 Were Handled

- **R1 (SEC-004, the AC-019 gate must actually run in CI, not be skipped):** Implemented `TestIntegration_AC006_AC016_AC019_SwapIsMetadataOnly_NoRewrite` as a genuine, non-illustrative assertion (relfilenode capture + comparison, tiny + large fixture) and added the `test-go-integration` CI job (see Deviations #4) so it runs on every PR against a real Postgres 16 service, not just locally. Verified locally end-to-end against a live container before committing.
- **R2 (SEC-001, AC-022a/b/c future-row/empty-fallback/dirty-recompute):** All three implemented and passing against a live PG16 — `TestIntegration_AC022a_*` seeds a `now+60d` row and confirms `VALIDATE CONSTRAINT` passes with `cutoff` strictly greater; `AC022b_*` confirms the empty-table `GREATEST(now(), NULL)` fallback; `AC022c_*` directly injects a stale `partition_conversion.cutoff` value (representing exactly the pre-revision `DO NOTHING` bug) and confirms the guarded `DO UPDATE` block overwrites it with a freshly-recomputed, live-data-derived value after `migrate force` + re-run — the precise recovery procedure documented in the ADD §12.
- **R3 (SEC-002/003, AC-021b future-skew batch succeeds via DEFAULT, logs WARN not ERROR):** `TestIntegration_AC021b_CopyFromWithFutureSkewedRowSucceedsViaDefault` sends a `pgx.CopyFrom` batch containing a `now+10d` row (beyond the 3-day margin) directly through the real ingest mechanics (not a repo mock) and confirms the whole batch inserts, with the skewed row landing in `<t>_default`. The manager-side WARN-not-ERROR classification is proven both at the unit level (`Test_AC021b_classifyBenign_treats_duplicate_check_and_overlap_as_benign` — exact SQLSTATEs `42P07`/`23514`/`42P17`) and exercised end-to-end in `Test_AC017_EnsureAll_no_partition_conversion_row_logs_WARN_not_ERROR`-style manager tests.
- **R4 (SEC-002, DefaultRowCount WARN + documented manual remediation):** `Manager.ensureTable` surfaces `DefaultRowCount` in `Health()` and logs WARN when non-zero (`Test_AC020_nonzero_DefaultRowCount_is_surfaced_via_health_and_WARN`). The manual remediation (`DELETE FROM <t>_default WHERE <key> < now() - <window>`) is documented in `manager.go`'s `ensureTable` comment and is staged for the technical writer per the ADD's DoD checklist; not automated, as specified. **Post-QA fix:** QA found (and left an attributed red test proving) that the N=3 stall-escalation counter this residual also depends on was shared across `EnsureAll` and `DropAged`, so a success from either operation reset it — a persistently-failing single operation (e.g. `DropAged` stuck while `EnsureAll` kept succeeding) never reached three *consecutive* failures and never escalated. Fixed by giving `EnsureAll` and `DropAged` their own independent `opFailureTracker` (`manager.go`: `Manager.ensureFailures` / `Manager.dropFailures`), each with its own `consecutiveFailures`/`stalled` state, so a persistent failure in either operation escalates the "partition manager stalled" ERROR after 3 consecutive failures of *that* operation regardless of the other operation's outcome. The QA-authored red test (renamed `Test_AC020_stall_escalation_is_not_masked_by_an_interleaved_operation_succeeding` per the QA report's own recommendation) now passes, and a mirror test for the `DropAged`-stuck direction (`Test_AC020_DropAged_persistent_failure_escalates_independently_of_EnsureAll_succeeding`) and an independence test (`Test_AC020_EnsureAll_and_DropAged_failure_trackers_are_independent`) were added.
- **R5 (SEC-001, rolling-deploy single-instance assumption):** Documented verbatim as an awareness note in `000026_partition_validate_bound.up.sql`'s header comment (no code change required, per the ADD) — the single-instance assumption and the self-healing `force`+reboot recovery are called out explicitly for the operator/technical writer.
- **R6 (SEC-003, benign set doesn't swallow disk-full/permission):** `Test_R6_classifyBenign_does_not_swallow_disk_full_or_permission_errors` asserts `53100` (disk full) and `42501` (insufficient privilege) are classified as genuine (non-benign) — logged at ERROR, isolated per-table, retried, and counted toward the N=3 stall escalation.

## Notable Design Decisions

1. **Executor-abstracted `Manager`.** `manager.go` defines a narrow `executor` interface (`createPartition`/`dropPartition`/`readCutoff`/`countDefaultRows`/`listPartitionNames`) with a real `pgxExecutor` implementation and an unexported `newManager(exec, ...)` constructor. This is what makes AC-017/AC-020's scheduling, classification, and health-escalation logic unit-testable with a fake executor and a slog-capturing handler — no live database needed for the manager's *logic*, only for the DDL it eventually issues.
2. **`runRetention` split into a thin `Server` method + a testable package-level function.** `Server.runRetention` now just forwards `s.EventRepo`, `s.NotifyRepo`, `s.LogRepo`, `s.DeviceProbeRepo`, `s.CARepo` (already interface-satisfying, per the existing compile-time checks in `interfaces.go`) to a package-level `runRetention(ctx, cfg, ...)` that server_test.go calls directly with mocks. This is the same "concrete repo already implements the interface" pattern the codebase already uses in `handlers.HandlerDeps`.
3. **`%I`/`%L`-equivalent quoting in Go (`sqlIdent`/`sqlLiteral`).** Mirrors the SQL-side `format('%I', …)`/`format('%L', …)` convention (SEC-PART-001-006) as defense-in-depth, even though every identifier the manager builds originates from the fixed in-code `TableSpec` list and the server's own wall clock.

## Notes for QA

- **Environment requirement:** the integration suite needs `RIOT_TEST_DB_URL` (or `RIOT_DB_URL`) pointing at a real PostgreSQL 16. Recommend QA re-run `go test -tags=integration -v ./internal/server/partition/...` against the CI-provisioned service and/or a Hawaii-representative staging box, and separately confirm the new `test-go-integration` CI job is green on the PR.
- **Edge cases already covered that are worth spot-checking manually:** (a) a device with a badly-skewed future clock (`now()+10d`) — its rows land in `<t>_default` and are visible via `Health().Tables[t].DefaultRowCount`; (b) a `migrate force` + reboot recovery after a simulated crash — no manual SQL is required, per AC-022c; (c) `migrate down` past `000027` on a table with legacy-range data — data survives, forward/DEFAULT data does not (documented, intentional).
- **Known, accepted limitation (BR-008, unchanged from ADD):** retention is "at least the configured window" — up to one extra day may be retained beyond the exact window, since a partition is only dropped once its *entire* range has aged out.
- **DEFAULT-row manual remediation** (`DELETE FROM <t>_default WHERE <key> < now() - <window>`) is documented but not automated — flag this for the technical writer to add to the operator-facing docs alongside the dirty-recovery procedure.
- Docker Desktop was used locally to stand up a throwaway `postgres:16-alpine` container for verification; it was fully torn down (`docker rm -f`) after testing — no leftover test infrastructure.

## Remaining Pipeline Stages

- QA validation (`docs/qa/PART-001-qa-report.md`)
- Technical writing: README/CHANGELOG updates, operator runbook for dirty-recovery and DEFAULT-row remediation, `docs/ops/postgres-bloat.md` cross-reference (this story supersedes it as the permanent fix per D-006).

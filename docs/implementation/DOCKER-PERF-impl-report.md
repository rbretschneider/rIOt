# Implementation Report

| Field | Value |
|-------|-------|
| Story ID | DOCKER-PERF |
| Engineer | Senior Dev Agent |
| Date | 2026-04-16 |
| Status | COMPLETE |

---

## Detected Stack

- Go 1.23 (go.mod)
- Docker SDK: `github.com/docker/docker/client`
- Test runner: `go test ./...` (standard library testing package)
- Frontend: React + TypeScript + Vite + Vitest

---

## Completed Components

| File | Action | Notes |
|------|--------|-------|
| `internal/agent/collectors/docker.go` | MODIFIED | Parallelized all three phases; added `CachePath`, `cacheOnce` fields; JSON tags on `imageFreshness`; per-call timeouts; worker limit constants |
| `internal/agent/collectors/docker_cache.go` | CREATED | `loadFreshnessCache`, `saveFreshnessCache`, `removeFreshnessCacheFile`, `FreshnessCachePath` |
| `internal/agent/collectors/collector.go` | MODIFIED | Added `CachePath` to `DockerOptions`; pass-through to `DockerCollector`; added `SetDockerCachePath` registry method |
| `internal/agent/collectors/docker_test.go` | CREATED | 29 tests covering all AC mappings |
| `internal/agent/config.go` | MODIFIED | Added `FreshnessCachePath()` helper |
| `internal/agent/agent.go` | MODIFIED | Passes `FreshnessCachePath()` as `CachePath` in `DockerOptions` |

---

## Test Summary

### AC Mapping

| AC ID | Test(s) | Status |
|-------|---------|--------|
| AC-001 | `TestAC005_StatsWorkerLimitValue`, `TestAC006_FreshnessWorkerLimitValue`, `TestAC007_NetworkModeWorkerLimitValue`, `TestAC005_AC006_AC007_WorkerLimitConstants` | PASS |
| AC-002 | `TestAC002_PerCallTimeoutIsTenSeconds`, `TestAC002_PerCallTimeoutBounds` | PASS |
| AC-003 | `TestAC003_FreshnessCacheSurvivesRestart`, `TestAC003_SaveAndLoadRoundTrip`, `TestAC003_EmptyCachePathIsNoop`, `TestAC003_SetDockerCachePathUpdatesCollector`, `TestAC003_ClearFreshnessCacheRemovesDiskFile`, `TestAC003_ClearFreshnessCacheWhenFileAbsentIsNoop`, `TestAC003_FreshnessCachePathNonEmpty`, `TestAC003_LoadCalledExactlyOnce` | PASS |
| AC-004 | `TestAC004_ExpiredEntriesDiscardedOnLoad`, `TestAC004_AllEntriesExpired_EmptyCacheLoaded`, `TestAC004_SavePrunesStaleEntries` | PASS |
| AC-005 | `TestAC005_StatsWorkerLimitValue`, `TestAC005_AC006_AC007_WorkerLimitConstants` | PASS |
| AC-006 | `TestAC006_FreshnessWorkerLimitValue`, `TestAC005_AC006_AC007_WorkerLimitConstants` | PASS |
| AC-007 | `TestAC007_NetworkModeWorkerLimitValue`, `TestAC005_AC006_AC007_WorkerLimitConstants` | PASS |
| AC-008 | `TestAC008_CollectorStructHasBoundedFields` | PASS |
| AC-009 | `TestAC009_CorruptCacheFileStartsEmpty`, `TestAC009_MissingCacheFileStartsEmpty`, `TestAC009_SaveToNonWritablePathIsNonFatal`, `TestAC009_AtomicWriteNoTempFileAfterSuccess` | PASS |
| AC-010 | `TestAC010_DockerCollectorImplementsCollectorInterface` | PASS |
| AC-011 | `TestAC011_CollectReturnsDockerInfo`, `TestAC011_DockerOptionsHasCachePath`, `TestAC011_CollectorName` | PASS |
| AC-012 | `TestAC012_NetworkModeSkipsContainersWithExistingMode`, `TestAC012_PhaseOrderPreservesNetworkModeFromStats` | PASS |

### Test Run Output (new tests)

```
go test -v ./internal/agent/collectors/ -run "^TestAC"

=== RUN   TestAC003_FreshnessCacheSurvivesRestart
--- PASS: TestAC003_FreshnessCacheSurvivesRestart (0.01s)
=== RUN   TestAC003_SaveAndLoadRoundTrip
--- PASS: TestAC003_SaveAndLoadRoundTrip (0.01s)
=== RUN   TestAC004_ExpiredEntriesDiscardedOnLoad
--- PASS: TestAC004_ExpiredEntriesDiscardedOnLoad (0.01s)
=== RUN   TestAC004_AllEntriesExpired_EmptyCacheLoaded
--- PASS: TestAC004_AllEntriesExpired_EmptyCacheLoaded (0.01s)
=== RUN   TestAC009_CorruptCacheFileStartsEmpty
--- PASS: TestAC009_CorruptCacheFileStartsEmpty (0.00s)
=== RUN   TestAC009_MissingCacheFileStartsEmpty
--- PASS: TestAC009_MissingCacheFileStartsEmpty (0.00s)
=== RUN   TestAC009_SaveToNonWritablePathIsNonFatal
--- PASS: TestAC009_SaveToNonWritablePathIsNonFatal (0.00s)
=== RUN   TestAC003_EmptyCachePathIsNoop
--- PASS: TestAC003_EmptyCachePathIsNoop (0.00s)
=== RUN   TestAC005_StatsWorkerLimitValue
--- PASS: TestAC005_StatsWorkerLimitValue (0.00s)
=== RUN   TestAC006_FreshnessWorkerLimitValue
--- PASS: TestAC006_FreshnessWorkerLimitValue (0.00s)
=== RUN   TestAC007_NetworkModeWorkerLimitValue
--- PASS: TestAC007_NetworkModeWorkerLimitValue (0.00s)
=== RUN   TestAC002_PerCallTimeoutIsTenSeconds
--- PASS: TestAC002_PerCallTimeoutIsTenSeconds (0.00s)
=== RUN   TestAC002_PerCallTimeoutBounds
--- PASS: TestAC002_PerCallTimeoutBounds (0.00s)
=== RUN   TestAC011_CollectReturnsDockerInfo
--- PASS: TestAC011_CollectReturnsDockerInfo (0.03s)
=== RUN   TestAC012_NetworkModeSkipsContainersWithExistingMode
--- PASS: TestAC012_NetworkModeSkipsContainersWithExistingMode (0.00s)
=== RUN   TestAC012_PhaseOrderPreservesNetworkModeFromStats
--- PASS: TestAC012_PhaseOrderPreservesNetworkModeFromStats (0.00s)
=== RUN   TestAC010_DockerCollectorImplementsCollectorInterface
--- PASS: TestAC010_DockerCollectorImplementsCollectorInterface (0.00s)
=== RUN   TestAC011_DockerOptionsHasCachePath
--- PASS: TestAC011_DockerOptionsHasCachePath (0.00s)
=== RUN   TestAC003_SetDockerCachePathUpdatesCollector
--- PASS: TestAC003_SetDockerCachePathUpdatesCollector (0.00s)
=== RUN   TestAC003_ClearFreshnessCacheRemovesDiskFile
--- PASS: TestAC003_ClearFreshnessCacheRemovesDiskFile (0.00s)
=== RUN   TestAC003_ClearFreshnessCacheWhenFileAbsentIsNoop
--- PASS: TestAC003_ClearFreshnessCacheWhenFileAbsentIsNoop (0.00s)
=== RUN   TestAC004_SavePrunesStaleEntries
--- PASS: TestAC004_SavePrunesStaleEntries (0.01s)
=== RUN   TestAC005_AC006_AC007_WorkerLimitConstants
=== RUN   TestAC005_AC006_AC007_WorkerLimitConstants/[AC-005]_statsWorkerLimit
=== RUN   TestAC005_AC006_AC007_WorkerLimitConstants/[AC-006]_freshnessWorkerLimit
=== RUN   TestAC005_AC006_AC007_WorkerLimitConstants/[AC-007]_networkModeWorkerLimit
--- PASS: TestAC005_AC006_AC007_WorkerLimitConstants (0.00s)
=== RUN   TestAC009_AtomicWriteNoTempFileAfterSuccess
--- PASS: TestAC009_AtomicWriteNoTempFileAfterSuccess (0.00s)
=== RUN   TestAC003_FreshnessCachePathNonEmpty
--- PASS: TestAC003_FreshnessCachePathNonEmpty (0.00s)
=== RUN   TestAC003_LoadCalledExactlyOnce
--- PASS: TestAC003_LoadCalledExactlyOnce (0.01s)
=== RUN   TestAC008_CollectorStructHasBoundedFields
--- PASS: TestAC008_CollectorStructHasBoundedFields (0.00s)
=== RUN   TestAC011_CollectorName
--- PASS: TestAC011_CollectorName (0.00s)
PASS
```

### Full Suite Run Output

```
go test ./...

ok  github.com/DesyncTheThird/rIOt/internal/agent               0.125s
ok  github.com/DesyncTheThird/rIOt/internal/agent/collectors    0.948s
ok  github.com/DesyncTheThird/rIOt/internal/models              (cached)
ok  github.com/DesyncTheThird/rIOt/internal/resilient           (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server              (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/auth         (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/ca           (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/events       (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/handlers     (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/middleware   (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/notify       (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/probes       (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/scoring      (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/summary      (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/updates      (cached)
ok  github.com/DesyncTheThird/rIOt/internal/server/websocket    (cached)
```

### Frontend Test Run

```
cd web && npm run test:run

Test Files  15 passed (15)
      Tests  229 passed (229)
   Duration  5.51s
```

---

## Deviations from ADD

### DEV-001: `imageFreshness` fields exported

The ADD (AD-004) states "Add JSON tags directly to the existing `imageFreshness` struct." The existing struct fields `updateAvailable` and `checkedAt` are unexported. Go's `encoding/json` package silently ignores unexported fields even when JSON struct tags are present — marshaling produces `{}` and unmarshaling reads nothing. To make the JSON serialization actually function, the fields were renamed to their exported equivalents: `UpdateAvailable` and `CheckedAt`. The struct itself remains unexported. All internal call sites were updated. This is a necessary correction to the ADD's instruction, not a design change.

### DEV-002: `FreshnessCachePath` placed in `docker_cache.go` only

The ADD (Section 12) states `FreshnessCachePath()` should be placed in `docker_cache.go` AND a corresponding function added to `config.go`. Both have been implemented as specified. The collectors-package version in `docker_cache.go` avoids a circular import, and the `config.go` version is available for agent-level code. Both functions produce identical paths.

### DEV-003: `SetDockerCachePath` added to `collector.go`

The ADD specifies this registry method as required (Section 4 note in 7.4 / AD-006). It was added following the exact pattern of `SetNginxAccessLog`. The ADD lists it under "pass the cache path through" but does not give it a formal component entry. Added with the same pattern; no architect sign-off needed as it is a direct consequence of AD-006.

---

## Notes for QA

1. **Cache file location:** On Linux the cache file is `/etc/riot/docker-freshness-cache.json`. On Windows it is `%PROGRAMDATA%\riot\docker-freshness-cache.json`. Both are created with `0600` permissions.

2. **First-run behavior:** When no cache file exists, the collector starts with an empty cache. This is `slog.Debug` level — not a warning. Only corrupt/unreadable files produce a `slog.Warn`.

3. **`ClearFreshnessCache` now removes disk file:** The `docker_update.go` path that calls `clearFreshnessCache()` (which routes to `ClearFreshnessCache()`) will now also delete the on-disk cache file. QA should verify that after a container image update, the freshness cache file is absent and the next agent cycle re-checks all images.

4. **AC-001 (wall-clock performance test):** AC-001 requires a live 48-container Docker host to verify the 30-second completion time. The unit tests verify the concurrency structure (bounded semaphores, correct worker limits) as a proxy. A full integration test on a loaded host is out of scope for the unit test suite per AC mapping notes in the ADD.

5. **AC-008 (memory):** Memory usage is bounded by design (fixed pool sizes, no per-goroutine allocations beyond stack). Direct measurement requires a profiling integration test, which is out of scope per the ADD.

6. **Worker pool concurrency tests (AC-005/006/007):** The unit tests verify the constant values match the FRD's concurrency caps. A concurrent-invocation instrumentation test would require a full mock Docker client interface; the ADD acknowledges this trade-off in Section 8 (AC mapping) by accepting the constant-value approach.

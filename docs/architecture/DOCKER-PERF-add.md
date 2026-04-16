# Architecture Decision Document

| Field          | Value                                         |
|----------------|-----------------------------------------------|
| Story ID       | DOCKER-PERF                                   |
| FRD Reference  | docs/requirements/DOCKER-PERF-frd.md          |
| Author         | Architect Agent                               |
| Date           | 2026-04-16                                    |
| Status         | FINAL                                         |

---

## 1. Summary

Parallelize the three collection phases of the Docker collector (`collectStats`, `checkImageUpdates`, `collectNetworkModes`) using bounded semaphore-channel worker pools, and persist the image freshness cache to a JSON file on disk using atomic write-and-rename. All changes are confined to a single file (`docker.go`) plus a new cache persistence file (`docker_cache.go`), a new path helper in `config.go`, and updates to the registration path to pass the cache file path through.

---

## 2. Technical Context

### Current State

- **`internal/agent/collectors/docker.go`** contains the `DockerCollector` struct and all three collection phases (`collectStats`, `checkImageUpdates`, `collectNetworkModes`). All three iterate sequentially over the container list.
- The `imageFreshness` cache is in-memory only (`map[string]*imageFreshness`), protected by `sync.RWMutex`. It is lost on every agent restart.
- The `DockerCollector` is instantiated in `RegisterDefaultsWithDocker()` in `collector.go` with three fields: `CollectStats`, `SocketPath`, `CheckUpdates`.
- No `docker_test.go` exists.

### Existing Patterns for Disk Persistence

Two patterns exist in the codebase:

1. **Nginx offset file** (`webservers_nginx_access.go`): writes a single integer to a file via atomic temp-file-then-rename. Path derived from `runtime.GOOS` inline in the collectors package.
2. **DNS cache** (`internal/resilient/dns_cache.go`): JSON marshal of a `map[string]*Entry` to a `.json` file via atomic write. Path provided via constructor argument. Cache loaded in constructor.

The DNS cache pattern is the better fit -- JSON file, map structure, path injected from outside. We will follow this pattern.

### What Needs to Change

1. The three sequential loops in `collectStats`, `checkImageUpdates`, and `collectNetworkModes` must be replaced with bounded-concurrency parallel loops.
2. The `DockerCollector` struct must gain a `CachePath` field and methods to load/save the digest cache to disk.
3. The `DockerOptions` struct and `RegisterDefaultsWithDocker` must pass the cache path through.
4. A new `FreshnessCachePath()` helper must be added to `config.go` following the existing path helper pattern.

---

## 3. Architecture Decisions

### AD-001: Semaphore Channel for Worker Pool

**Decision:** Use `sync.WaitGroup` + buffered channel semaphore (`sem := make(chan struct{}, N)`) for bounded concurrency in all three collection phases.

**Rationale:** This is idiomatic Go, requires zero dependencies, and is the simplest correct pattern for "run N items with max M concurrency." The codebase already imports `sync` in this file. A more structured worker pool (e.g., `errgroup` with `SetLimit`) would also work but adds no benefit here since we don't aggregate errors -- individual failures are silently skipped per existing behavior.

**Alternatives Considered:**
- `golang.org/x/sync/errgroup` with `SetLimit()`: Would require adding an external dependency (the `x/sync` module). Rejected per dependency rules -- not currently in `go.mod` and not justified for this use case.
- Custom worker pool struct: Over-engineered for three simple parallel loops.

**Consequences:** Each parallel phase spawns up to N goroutines (where N = number of items to process), but only M run concurrently. Goroutines that cannot acquire the semaphore block until a slot opens. Total goroutine count per phase is bounded by the number of items, not the semaphore size, but only M are active simultaneously.

### AD-002: Per-Call Timeout via context.WithTimeout

**Decision:** Wrap each individual Docker API call (ContainerStats, ContainerInspect, DistributionInspect) and each `docker manifest inspect` subprocess in a `context.WithTimeout(ctx, 10*time.Second)` derived from the parent context.

**Rationale:** The parent context from the collector has its own timeout (the agent's poll interval), but one slow container could consume the entire budget. Per-call timeouts ensure that a single unresponsive container/registry is abandoned after 10 seconds. Using the parent context as the base means that if the parent is cancelled (agent shutdown), all in-flight calls are also cancelled.

**Alternatives Considered:**
- Relying solely on the parent context timeout: Rejected because one slow call blocks the semaphore slot and delays all remaining work.

**Consequences:** A container whose Docker API call takes >10s will have its data skipped (stats zeroed, freshness unknown, network mode empty). This is acceptable degraded behavior per FR-003/FR-007/FR-011.

### AD-003: Cache Persistence as JSON File with Atomic Write

**Decision:** Persist the freshness cache as a JSON file at `FreshnessCachePath()` (Linux: `/etc/riot/docker-freshness-cache.json`, Windows: `%PROGRAMDATA%\riot\docker-freshness-cache.json`). Use atomic write (temp file in same directory, then `os.Rename`). Load on first `Collect()` call or when `CachePath` is set. Save after each `checkImageUpdates` pass completes.

**Rationale:** Follows the exact pattern established by the DNS cache (`internal/resilient/dns_cache.go`) and nginx offset file. JSON is human-readable for debugging. Atomic write prevents corruption on crash. Placing the file in `/etc/riot/` matches every other persistent agent file.

**Alternatives Considered:**
- SQLite / bolt DB: Massively over-engineered for a small key-value map.
- Gob encoding: Not human-readable, no debugging advantage.
- Writing on every individual image check: Too many disk writes. Writing once per `checkImageUpdates` pass is sufficient.

**Consequences:** The cache file is small (typically <50 KB for 48 containers). Pruning stale entries on both load and save prevents unbounded growth (NFR-006).

### AD-004: Serializable Cache Entry Type

**Decision:** Introduce a new `freshnessCacheFile` type for JSON serialization that is separate from the internal `imageFreshness` type. The on-disk format is:

```json
{
  "nginx:latest": {
    "update_available": true,
    "checked_at": "2026-04-16T12:00:00Z"
  },
  "postgres:16": {
    "update_available": null,
    "checked_at": "2026-04-16T11:45:00Z"
  }
}
```

**Rationale:** The internal `imageFreshness` struct uses `*bool` for `updateAvailable`, which JSON naturally serializes as `true`, `false`, or `null`. Using `json:"update_available"` and `json:"checked_at"` provides clean snake_case keys matching the project's JSON convention. A separate exported type for serialization keeps the internal type unexported and avoids leaking implementation details.

**Alternatives Considered:**
- Reusing `imageFreshness` directly with JSON tags: Would require exporting the type or adding tags to an unexported type. Adding JSON tags to the internal type is acceptable but conflates serialization concerns. We will add JSON tags directly to `imageFreshness` since it is package-private anyway and creating a parallel type adds unnecessary mapping code.

**Final decision:** Add JSON tags directly to the existing `imageFreshness` struct. It is unexported so there is no API leakage concern.

### AD-005: Thread Safety for Parallel Writes to containers Slice

**Decision:** Each goroutine in `collectStats` and `collectNetworkModes` writes only to `containers[i]` where `i` is captured by the goroutine. No two goroutines write to the same index. This is safe in Go without a mutex because each goroutine operates on a distinct element of the slice. The slice itself is not resized.

For `checkImageUpdates`, the same index-based approach applies for writing `containers[i].UpdateAvailable`. The digest cache map (`c.digestCache`) is already protected by `c.cacheMu` -- the existing lock/unlock pattern in the sequential code is correct for concurrent access as-is.

**Rationale:** Go's memory model guarantees that writes to distinct array/slice elements from different goroutines do not race. This is explicitly safe and avoids mutex overhead on the hot path.

**Alternatives Considered:**
- Adding a mutex for the containers slice: Unnecessary and would serialize the work, defeating the purpose.
- Collecting results into a channel and merging: More complex with no safety benefit.

**Consequences:** The `WaitGroup.Wait()` call at the end of each phase acts as a happens-before barrier, ensuring all writes are visible to subsequent code.

### AD-006: Cache Path Injection via Struct Field

**Decision:** Add a `CachePath string` field to `DockerCollector` and pass it through `DockerOptions`. The `FreshnessCachePath()` helper in `config.go` provides the default. The cache is loaded lazily on first `Collect()` call (via a `sync.Once`).

**Rationale:** Follows the pattern of `SocketPath` -- a simple struct field set at registration time. Lazy loading via `sync.Once` avoids file I/O during registration and ensures the cache is loaded exactly once even under concurrent access (though `Collect` is called sequentially by the agent loop).

**Alternatives Considered:**
- Loading in a separate `Init()` method: Would require changing the `Collector` interface or adding a special case. Rejected.
- Loading in the constructor: There is no constructor -- the struct is initialized with a literal. `sync.Once` is the idiomatic equivalent.

**Consequences:** The `ClearFreshnessCache()` method (called by `docker_update.go`) must also delete the cache file from disk so that a restart after a container update does not reload stale cache entries.

### AD-007: Per-Call Timeout Constant

**Decision:** Define `const perCallTimeout = 10 * time.Second` as a package-level constant in `docker.go`.

**Rationale:** Single definition, referenced by all three phases. Matches FR-019. Not configurable per FRD Section 9 (out of scope).

### AD-008: Concurrency Constants

**Decision:** Define three package-level constants:
- `const statsWorkerLimit = 10` (FR-002)
- `const freshnessWorkerLimit = 5` (FR-006)
- `const networkModeWorkerLimit = 10` (FR-010)

**Rationale:** Named constants are self-documenting and easy to find. Not configurable per FRD Section 9.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `internal/agent/collectors/docker.go` | Parallelize `collectStats`, `checkImageUpdates`, `collectNetworkModes` with semaphore worker pools; add per-call timeouts; add `CachePath` field; add `sync.Once` for cache loading; add JSON tags to `imageFreshness` |
| CREATE | `internal/agent/collectors/docker_cache.go` | Cache persistence: `loadFreshnessCache()`, `saveFreshnessCache()`, `FreshnessCachePath()` (OS-aware path helper) |
| MODIFY | `internal/agent/collectors/collector.go` | Add `CachePath string` to `DockerOptions`; pass through to `DockerCollector` |
| CREATE | `internal/agent/collectors/docker_test.go` | Unit tests for parallelization, timeouts, cache persistence, and concurrency bounds |
| MODIFY | `internal/agent/config.go` | Add `FreshnessCachePath()` helper function |
| MODIFY | `internal/agent/agent.go` | Pass `agent.FreshnessCachePath()` as `CachePath` in `DockerOptions` |

---

## 5. Data Model Changes

### No Database Changes

This story has zero database impact. All changes are agent-side in-memory and local file.

### Modified Struct: `imageFreshness` (unexported, `docker.go`)

**Before:**
```go
type imageFreshness struct {
    updateAvailable *bool
    checkedAt       time.Time
}
```

**After:**
```go
type imageFreshness struct {
    updateAvailable *bool     `json:"update_available"`
    checkedAt       time.Time `json:"checked_at"`
}
```

### Modified Struct: `DockerCollector` (exported, `docker.go`)

**Before:**
```go
type DockerCollector struct {
    CollectStats bool
    SocketPath   string
    CheckUpdates bool

    cacheMu     sync.RWMutex
    digestCache map[string]*imageFreshness
}
```

**After:**
```go
type DockerCollector struct {
    CollectStats bool
    SocketPath   string
    CheckUpdates bool
    CachePath    string

    cacheMu     sync.RWMutex
    digestCache map[string]*imageFreshness
    cacheOnce   sync.Once
}
```

### Modified Struct: `DockerOptions` (exported, `collector.go`)

**Before:**
```go
type DockerOptions struct {
    CollectStats bool
    SocketPath   string
    CheckUpdates bool
}
```

**After:**
```go
type DockerOptions struct {
    CollectStats bool
    SocketPath   string
    CheckUpdates bool
    CachePath    string
}
```

### New File on Disk: `docker-freshness-cache.json`

```json
{
  "nginx:latest": {
    "update_available": false,
    "checked_at": "2026-04-16T12:00:00Z"
  },
  "ghcr.io/org/app:main": {
    "update_available": null,
    "checked_at": "2026-04-16T11:50:00Z"
  }
}
```

- **Location:** `/etc/riot/docker-freshness-cache.json` (Linux), `%PROGRAMDATA%\riot\docker-freshness-cache.json` (Windows)
- **Max size:** Bounded by number of unique images. 48 containers with ~100 bytes per entry = ~5 KB typical.
- **Pruned:** On both load and save, entries with `checked_at` older than 30 minutes are discarded.

---

## 6. API / Interface Contract

No API changes. The `Collector` interface is unchanged:

```go
type Collector interface {
    Name() string
    Collect(ctx context.Context) (interface{}, error)
}
```

`Collect()` continues to return `(*models.DockerInfo, nil)` wrapped as `(interface{}, error)`. No new endpoints, no server-side changes.

---

## 7. Sequence / Flow

### 7.1 Collect() -- Main Flow (unchanged structure, parallelized internals)

1. Create Docker client, ping daemon, get system info and version.
2. `ContainerList(ctx, All: true)` -- single call, sequential (out of scope per FRD).
3. Build `info.Containers` slice from API response.
4. **Phase 1: `collectStats(ctx, cli, info.Containers)`** -- parallel, max 10 concurrent.
5. **Phase 2: `checkImageUpdates(ctx, cli, info.Containers)`** -- parallel, max 5 concurrent.
6. **Phase 3: `collectNetworkModes(ctx, cli, info.Containers)`** -- parallel, max 10 concurrent.
7. Return `info`.

### 7.2 collectStats -- Parallel Flow

1. Create semaphore: `sem := make(chan struct{}, 10)`.
2. Create `sync.WaitGroup`.
3. For each container where `State == "running"`:
   a. `wg.Add(1)`, launch goroutine.
   b. Goroutine acquires semaphore (`sem <- struct{}{}`).
   c. `callCtx, cancel := context.WithTimeout(ctx, 10s)`.
   d. Call `cli.ContainerStats(callCtx, id, false)`. On error or timeout, skip.
   e. Parse stats JSON, compute CPU/mem.
   f. Call `cli.ContainerInspect(callCtx, id)`. On error, skip inspect fields only.
   g. Write results to `containers[i]` (goroutine's own index).
   h. `cancel()`, release semaphore (`<-sem`), `wg.Done()`.
4. `wg.Wait()`.

### 7.3 checkImageUpdates -- Parallel Flow

1. `c.cacheOnce.Do(c.loadFreshnessCache)` -- load from disk on first call.
2. Create semaphore: `sem := make(chan struct{}, 5)`.
3. Create `sync.WaitGroup`.
4. For each container where `State == "running"` and `Image != ""`:
   a. Check in-memory cache under `cacheMu.RLock()`. If hit and within TTL, set `containers[i].UpdateAvailable` and skip.
   b. Otherwise, `wg.Add(1)`, launch goroutine.
   c. Goroutine acquires semaphore.
   d. `callCtx, cancel := context.WithTimeout(ctx, 10s)`.
   e. Call `c.checkSingleImage(callCtx, cli, imageRef)`.
   f. Lock `cacheMu`, write result to `c.digestCache[imageRef]`, unlock.
   g. Write `containers[i].UpdateAvailable`.
   h. `cancel()`, release semaphore, `wg.Done()`.
5. `wg.Wait()`.
6. `c.saveFreshnessCache()` -- persist updated cache to disk.

**Important note on cache check placement:** The cache check (step 4a) happens in the main goroutine *before* launching the worker goroutine. This ensures cached images never consume a semaphore slot and provides maximum throughput for uncached images. Only cache misses spawn goroutines.

### 7.4 collectNetworkModes -- Parallel Flow

1. Create semaphore: `sem := make(chan struct{}, 10)`.
2. Create `sync.WaitGroup`.
3. For each container where `NetworkMode == ""`:
   a. `wg.Add(1)`, launch goroutine.
   b. Goroutine acquires semaphore.
   c. `callCtx, cancel := context.WithTimeout(ctx, 10s)`.
   d. Call `cli.ContainerInspect(callCtx, id)`.
   e. Write `containers[i].NetworkMode` and `containers[i].RestartPolicy`.
   f. `cancel()`, release semaphore, `wg.Done()`.
4. `wg.Wait()`.

### 7.5 Cache Load (on first Collect)

1. If `CachePath` is empty, skip.
2. Read file. If not found or unreadable, log warning, return (empty cache).
3. `json.Unmarshal` into `map[string]*imageFreshness`.
4. Iterate entries: discard any where `time.Since(checkedAt) >= 30min`.
5. Set `c.digestCache` to the pruned map.
6. Log info with count of loaded entries.

### 7.6 Cache Save (after checkImageUpdates)

1. If `CachePath` is empty, skip.
2. Lock `cacheMu` (read lock).
3. Build pruned copy: only entries where `time.Since(checkedAt) < 30min`.
4. `json.Marshal` the pruned map.
5. Unlock `cacheMu`.
6. Write to temp file in same directory as `CachePath`.
7. `os.Rename(tmp, CachePath)`.
8. On any error, log warning and continue (cache persistence is best-effort).

### 7.7 ClearFreshnessCache (called by docker_update.go)

1. Lock `cacheMu`, set `digestCache = nil`, unlock.
2. If `CachePath != ""`, remove the file from disk (`os.Remove`). Log warning on error but do not fail.

---

## 8. Acceptance Criteria Mapping

| AC ID | Fulfilled By | Test Strategy |
|-------|-------------|---------------|
| AC-001 | Parallel `collectStats` (10 workers), `checkImageUpdates` (5 workers), `collectNetworkModes` (10 workers) with per-call timeouts | Unit: mock Docker client with configurable delays; verify 48-container collection completes in <30s wall-clock |
| AC-002 | `context.WithTimeout(ctx, 10s)` wrapping every Docker API call and `exec.CommandContext` | Unit: mock one container to block forever; verify it is skipped after timeout and others succeed |
| AC-003 | `saveFreshnessCache()` after `checkImageUpdates`; `loadFreshnessCache()` on first `Collect()` | Unit: write cache file, create new collector with same path, verify cache is loaded and registry calls are skipped for cached images |
| AC-004 | TTL check in `loadFreshnessCache()`: discard entries where `time.Since(checkedAt) >= 30min` | Unit: write cache file with mix of fresh and stale entries; load and verify only fresh entries are present |
| AC-005 | `sem := make(chan struct{}, statsWorkerLimit)` in `collectStats` | Unit: instrument semaphore acquisition with atomic counter; verify peak never exceeds 10 |
| AC-006 | `sem := make(chan struct{}, freshnessWorkerLimit)` in `checkImageUpdates` | Unit: instrument semaphore acquisition with atomic counter; verify peak never exceeds 5 |
| AC-007 | `sem := make(chan struct{}, networkModeWorkerLimit)` in `collectNetworkModes` | Unit: instrument semaphore acquisition with atomic counter; verify peak never exceeds 10 |
| AC-008 | Bounded worker pools prevent goroutine explosion; no per-goroutine allocations beyond stack | Unit: not directly testable as unit test (memory measurement is integration); verify bounded goroutine count as proxy |
| AC-009 | `loadFreshnessCache()` catches JSON unmarshal errors, logs warning, returns empty cache | Unit: write garbage to cache file; verify collector starts with empty cache and collection succeeds |
| AC-010 | No existing `docker_test.go` exists. Broader agent test suite must remain green. | Run `go test ./...` before and after changes |
| AC-011 | `Collect()` signature unchanged; return type unchanged; all parallelism internal | Unit: verify `Collect()` returns `(*models.DockerInfo, nil)` with expected fields populated |
| AC-012 | `collectStats` called before `collectNetworkModes` (sequential phase ordering preserved in `Collect()`); `collectNetworkModes` skips containers where `NetworkMode != ""` | Unit: mock containers with mixed states; verify network mode from stats phase is preserved and not overwritten |

---

## 9. Error Handling

| Failure Mode | Handling | HTTP Status | Logging |
|-------------|----------|-------------|---------|
| Individual `ContainerStats` timeout | Skip container's CPU/mem data; leave at zero values | N/A (agent-side) | `slog.Debug` with container ID and error |
| Individual `ContainerInspect` timeout | Skip container's CPU limit / network mode | N/A | `slog.Debug` with container ID and error |
| Individual `DistributionInspect` timeout | Set `UpdateAvailable = nil` (unknown) | N/A | `slog.Debug` with image ref and error |
| Individual `docker manifest inspect` timeout | Fall through to `nil` return (unknown) | N/A | No additional logging (already handled by `checkSingleImage`) |
| Cache file does not exist on load | Start with empty cache | N/A | `slog.Debug` (normal first-run behavior) |
| Cache file is corrupt | Start with empty cache | N/A | `slog.Warn` with file path and parse error |
| Cache file write fails | Continue without persistence; next restart will re-check all images | N/A | `slog.Warn` with file path and error |
| Cache file rename fails | Remove temp file, continue | N/A | `slog.Warn` with file path and error |
| Parent context cancelled (agent shutdown) | All in-flight goroutines receive cancellation via derived contexts; `wg.Wait()` returns once all goroutines exit | N/A | No special logging needed |

---

## 10. Security Considerations

**Attack surface:** Minimal. This story does not introduce new network listeners, authentication mechanisms, or user input handling.

- **Cache file permissions:** Write with `0600` (owner-only read/write), matching the nginx offset file and DNS cache patterns.
- **Cache file location:** Fixed to the agent's data directory (`/etc/riot/`). Not user-configurable via the YAML config -- only the code-level `FreshnessCachePath()` determines it. This prevents path traversal.
- **Temp file location:** Created in the same directory as the cache file (`filepath.Dir(cachePath)`) to ensure atomic rename stays on the same filesystem.
- **No new subprocess calls:** `docker manifest inspect` already exists in the codebase. Per-call timeout via `exec.CommandContext` already applies.
- **Registry credentials:** No change. The existing empty-auth pattern for `DistributionInspect` is preserved. No credentials are stored in the cache file.

---

## 11. Performance Considerations

### Expected Performance Improvement

With 48 containers (40 running, 8 stopped):

| Phase | Sequential (current) | Parallel (new) | Speedup |
|-------|---------------------|----------------|---------|
| Stats (40 running) | ~40 calls x ~0.5s = 20s | 40 calls / 10 workers x ~0.5s = 2s | 10x |
| Freshness (40 running, cold cache) | ~40 calls x ~2s = 80s | 40 calls / 5 workers x ~2s = 16s | 5x |
| Network modes (8 stopped) | ~8 calls x ~0.1s = 0.8s | 8 calls / 10 workers x ~0.1s = 0.1s | 8x |
| **Total** | **~100s** | **~18s** | **~5.5x** |

This comfortably meets the 30-second target (NFR-001) even in the worst case.

### Indexing / Caching

- No database indexes affected (no DB changes).
- Cache file is tiny (<10 KB typical). No read buffering needed.
- Cache is loaded once via `sync.Once`, not on every `Collect()` call.

### Rate Limiting

- Registry concurrency capped at 5 (BR-001) to respect Docker Hub / GHCR rate limits.
- Docker daemon concurrency capped at 10 for stats/inspect (BR-002).

---

## 12. Implementation Notes for Engineers

### Goroutine Index Capture

When launching goroutines in a `for i := range` loop, the index `i` must be captured correctly. In Go 1.22+ (which this project uses per `go.mod`), loop variables are per-iteration, so `go func() { ... containers[i] ... }()` is safe. However, for clarity and to avoid subtle bugs if the Go version constraint changes, explicitly pass `i` as a parameter:

```go
go func(idx int) {
    defer wg.Done()
    // use containers[idx]
}(i)
```

### Per-Call Timeout Pattern

The timeout context must be created inside the goroutine, not in the launching loop. The parent context is passed to the goroutine; the goroutine derives its own timeout:

```go
go func(idx int) {
    defer wg.Done()
    sem <- struct{}{}
    defer func() { <-sem }()

    callCtx, cancel := context.WithTimeout(ctx, perCallTimeout)
    defer cancel()

    // use callCtx for all API calls
}(i)
```

### Cache Save Timing

`saveFreshnessCache()` is called once after the entire `checkImageUpdates` phase completes (after `wg.Wait()`), not after each individual image check. This minimizes disk I/O.

### ClearFreshnessCache Must Also Delete the File

The existing `ClearFreshnessCache()` method is called by `docker_update.go` after a container image is updated. It must now also remove the cache file from disk. Otherwise, the next agent restart would reload the stale "up to date" entry for the just-updated image.

### File Organization

Cache persistence logic (`loadFreshnessCache`, `saveFreshnessCache`, `FreshnessCachePath`) goes in a new file `docker_cache.go` in the same package. This keeps `docker.go` focused on collection logic and follows the codebase's pattern of splitting related functionality across files (e.g., `docker_labels.go`).

The `FreshnessCachePath()` function is placed in `docker_cache.go` (not `config.go`) because it follows the same pattern as `offsetFilePath()` in `webservers_nginx_access.go` -- the collectors package defines its own path helpers to avoid importing the `agent` package. However, a corresponding `FreshnessCachePath()` must also be added to `config.go` for use by the agent package when constructing `DockerOptions`.

### Existing Test Patterns

The codebase has no `docker_test.go`. Tests for this collector will need to mock the Docker client. The Docker SDK's `client.Client` is a concrete type, not an interface. To enable unit testing, the engineer should:

1. Define a minimal interface in `docker_test.go` covering only the methods called by the collector (`ContainerStats`, `ContainerInspect`, `DistributionInspect`, `ImageInspectWithRaw`, `Ping`, `Info`, `ServerVersion`, `ContainerList`, `Close`).
2. Refactor `collectStats`, `checkImageUpdates`, and `collectNetworkModes` to accept this interface instead of `*client.Client`.
3. The `Collect()` method continues to create the real `*client.Client` and pass it to the internal methods.

This is the standard approach in Go for testing code that uses concrete SDK clients. The interface is defined in the test file or alongside the consumer, not the provider.

Alternatively, if refactoring the method signatures is deemed too invasive, the engineer may use a test-only Docker API interface extracted from the existing methods. The architect recommends the interface approach as it is cleaner and makes the dependency explicit.

### No Changes to Models

`DockerInfo` and `ContainerInfo` in `internal/models/telemetry.go` are unchanged. No migration needed.

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented
- [ ] `collectStats` uses bounded worker pool with max concurrency 10
- [ ] `checkImageUpdates` uses bounded worker pool with max concurrency 5
- [ ] `collectNetworkModes` uses bounded worker pool with max concurrency 10
- [ ] Per-call timeout of 10 seconds on all Docker API calls and subprocesses
- [ ] Freshness cache persists to disk as JSON with atomic write
- [ ] Freshness cache loads from disk on first `Collect()` with TTL pruning
- [ ] `ClearFreshnessCache()` removes the disk cache file
- [ ] Corrupt/missing cache file does not prevent collection
- [ ] All AC mappings in Section 8 have corresponding tests with AC references
- [ ] Full test suite is green (`go test ./...` and `cd web && npm run test:run`)
- [ ] No new linting errors introduced
- [ ] Phase execution order preserved: stats -> freshness -> network modes
- [ ] `Collect()` return type and behavior unchanged for callers

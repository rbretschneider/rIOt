# Formal Requirements Document

| Field   | Value                                              |
|---------|----------------------------------------------------|
| Story ID | DOCKER-PERF                                       |
| Title   | Docker Collector Parallelization and Cache Persistence |
| Author  | Business Developer Agent                           |
| Date    | 2026-04-16                                         |
| Status  | FINAL                                              |

---

## 1. Executive Summary

The Docker collector in the rIOt agent makes approximately 144 sequential API calls (Docker daemon + container registries) when monitoring a host with 48 containers, resulting in telemetry collection times of 2-5 minutes. This story parallelizes the three collection phases (stats, image freshness, network modes) using bounded worker pools and persists the image freshness cache to disk so that agent restarts do not trigger redundant registry checks.

---

## 2. Background & Context

The rIOt agent's Docker collector gathers three categories of data for each container:

1. **Container stats** (`collectStats`) -- CPU and memory usage via the Docker `ContainerStats` API, plus CPU limits and network mode via `ContainerInspect`. Called only for running containers.
2. **Image freshness** (`checkImageUpdates`) -- Compares local image digests against remote registry digests via `DistributionInspect` and `docker manifest inspect`. Called only for running containers. Results are cached in-memory with a 30-minute TTL.
3. **Network modes** (`collectNetworkModes`) -- Reads `HostConfig.NetworkMode` and `RestartPolicy` via `ContainerInspect`. Called only for containers that did not already get network mode populated during stats collection (i.e., non-running containers).

All three phases iterate sequentially over the container list. On a host with 48 containers, this produces approximately 144 sequential API calls. Registry API calls for image freshness are the slowest (1-5 seconds each), making the total collection time 2-5 minutes.

**Current impact:**
- After a server restart, the dashboard shows stale Docker data for 5+ minutes.
- After `triggerTelemetry()` fires, actual telemetry does not arrive for 5+ minutes.
- The agent cannot maintain its configured 60-second telemetry interval because a single Docker collection cycle exceeds that interval.
- The in-memory freshness cache is lost on every agent restart, forcing a full re-check of all container images against their registries.

---

## 3. Actors

| Actor          | Description                                                  | Permissions                        |
|----------------|--------------------------------------------------------------|------------------------------------|
| rIOt Agent     | Daemon running on monitored hosts; executes the Docker collector | Read access to the Docker socket; read/write access to its own cache directory |
| Docker Daemon  | Local Docker engine exposing the API via Unix/named pipe socket | Responds to container list, stats, inspect, and distribution inspect calls |
| Container Registries | Remote image registries (Docker Hub, GHCR, etc.)       | Public or authenticated read access for manifest/digest queries |

---

## 4. Functional Requirements

### Stats Collection

**FR-001**: The `collectStats` function must collect CPU/memory stats from multiple running containers concurrently using a bounded worker pool.

**FR-002**: The worker pool for stats collection must have a maximum concurrency of no more than 10 simultaneous Docker API calls.

**FR-003**: Each individual `ContainerStats` API call must be subject to a per-call timeout. If the timeout is exceeded, that container's stats must be skipped and the remaining containers must continue processing.

**FR-004**: Each individual `ContainerInspect` API call (within stats collection) must be subject to the same per-call timeout as FR-003.

### Image Freshness Checks

**FR-005**: The `checkImageUpdates` function must check multiple container images concurrently using a bounded worker pool.

**FR-006**: The worker pool for image freshness checks must have a maximum concurrency of no more than 5 simultaneous registry API calls.

**FR-007**: Each individual `DistributionInspect` API call must be subject to a per-call timeout. If the timeout is exceeded, that image's freshness result must be set to unknown (nil) and the remaining images must continue processing.

**FR-008**: Each individual `docker manifest inspect` subprocess must be subject to the same per-call timeout as FR-007.

### Network Mode Collection

**FR-009**: The `collectNetworkModes` function must inspect multiple containers concurrently using a bounded worker pool.

**FR-010**: The worker pool for network mode collection must have a maximum concurrency of no more than 10 simultaneous Docker API calls.

**FR-011**: Each individual `ContainerInspect` API call (within network mode collection) must be subject to a per-call timeout. If the timeout is exceeded, that container's network mode must be left empty and the remaining containers must continue processing.

### Cache Persistence

**FR-012**: The image freshness cache must be persisted to disk so that cache entries survive agent restarts.

**FR-013**: When the agent starts, it must load the persisted freshness cache from disk if the cache file exists.

**FR-014**: When loading the persisted cache, the agent must discard any entries whose `checkedAt` timestamp is older than the 30-minute TTL. Only entries within the TTL must be loaded into memory.

**FR-015**: The cache file must be written atomically (write to temporary file, then rename) to prevent corruption if the agent is terminated during a write.

**FR-016**: If the cache file does not exist, is unreadable, or is corrupt, the agent must start with an empty cache and log a warning. This must not prevent the Docker collector from functioning.

### General

**FR-017**: The `Collect()` method must continue to return a single `DockerInfo` value. All parallelism must be internal to the collector -- callers must not be aware of or affected by the change.

**FR-018**: Errors in individual container processing (stats, freshness, network mode) must not cause the entire collection to fail. Each container must be processed independently -- a failure for one container must not affect others.

**FR-019**: The per-call timeout for individual Docker API calls must be 10 seconds.

---

## 5. Non-Functional Requirements

**NFR-001** [Performance]: With 48 containers, a full telemetry collection cycle (stats + freshness + network modes) must complete in under 30 seconds, assuming registry responses within normal latency ranges (under 5 seconds per call).

**NFR-002** [Resource Usage]: Memory usage of the Docker collector must not increase by more than 10% compared to the sequential implementation when processing 48 containers.

**NFR-003** [Resource Usage]: The bounded worker pools must prevent unbounded goroutine creation. The total number of concurrent goroutines spawned by the Docker collector must never exceed the sum of the configured pool sizes across all three phases.

**NFR-004** [Reliability]: A single slow or unresponsive container must not block the collection of data from other containers.

**NFR-005** [Compatibility]: The collector must continue to function correctly on all supported platforms (Linux amd64/arm64, Windows).

**NFR-006** [Disk Usage]: The persisted freshness cache file must not grow unboundedly. Stale entries (older than TTL) must be pruned on both load and save.

---

## 6. Business Rules

**BR-001**: Registry rate limits must be respected. The image freshness worker pool concurrency (5) is intentionally lower than the stats pool (10) to avoid triggering rate limits on Docker Hub, GHCR, and similar registries.

**BR-002**: The Docker daemon must not be overwhelmed by concurrent requests. The bounded worker pool sizes represent the maximum acceptable concurrent load on a single Docker daemon.

**BR-003**: The 30-minute TTL for the freshness cache must remain unchanged. This value balances timely update detection against registry rate limit consumption.

**BR-004**: The three collection phases (stats, freshness, network modes) must execute in the same order as the current implementation: stats first, then freshness, then network modes. This is because `collectStats` populates `NetworkMode` for running containers, and `collectNetworkModes` skips containers that already have a network mode set.

---

## 7. Data Requirements

### Entities

| Entity | Description | Changes |
|--------|-------------|---------|
| `DockerCollector` | Collector struct | Must gain the ability to persist and load its `digestCache` |
| `imageFreshness` | Cache entry with `updateAvailable` and `checkedAt` | Must be serializable to/from disk |
| `DockerInfo` | Collection result | No structural changes |
| `ContainerInfo` | Per-container data | No structural changes |

### Cache File Format

The persisted cache file must contain:
- Image reference (string key)
- Update available status (boolean or null/unknown)
- Timestamp of last check (`checkedAt`)

### State Transitions

| State | Trigger | Next State |
|-------|---------|------------|
| No cache file on disk | Agent starts | Empty in-memory cache; collector proceeds normally |
| Valid cache file on disk | Agent starts | Load entries within TTL into memory; discard expired entries |
| Corrupt/unreadable cache file | Agent starts | Empty in-memory cache; log warning |
| In-memory cache updated | Freshness check completes for an image | Persist updated cache to disk |
| Cache entry older than 30 minutes | Cache read during collection | Entry treated as expired; re-check registry |

### Validation Rules

- Cache entries with `checkedAt` older than 30 minutes must not be used and must be discarded on load.
- The cache file path must be within the agent's data directory.

---

## 8. Acceptance Criteria

**AC-001: Full collection completes within 30 seconds for 48 containers** [Maps to FR-001, FR-005, FR-009, NFR-001]
```
Given: A Docker host with 48 containers (40 running, 8 stopped)
And: The image freshness cache is empty (worst case)
When: The Docker collector executes a full Collect() cycle
Then: The collection must complete in under 30 seconds
```

**AC-002: Individual API call timeout does not block collection** [Maps to FR-003, FR-004, FR-007, FR-008, FR-011, FR-019, NFR-004]
```
Given: A Docker host with multiple running containers
And: One container's Docker API call hangs indefinitely
When: The per-call timeout (10 seconds) elapses
Then: That container's data must be skipped
And: All other containers must continue to be processed
And: The overall collection must complete successfully
```

**AC-003: Freshness cache survives agent restart** [Maps to FR-012, FR-013, FR-014]
```
Given: The Docker collector has completed a freshness check cycle
And: The freshness cache contains entries checked less than 30 minutes ago
When: The agent process is restarted
And: The Docker collector runs its next Collect() cycle
Then: Images with valid (non-expired) cache entries must not trigger registry API calls
And: The UpdateAvailable field must reflect the cached values
```

**AC-004: Expired cache entries are discarded on load** [Maps to FR-014]
```
Given: A persisted cache file exists on disk
And: Some entries have checkedAt timestamps older than 30 minutes
When: The agent starts and loads the cache
Then: Only entries within the 30-minute TTL must be loaded into memory
And: Expired entries must not be present in the in-memory cache
```

**AC-005: Stats worker pool concurrency is bounded** [Maps to FR-002, NFR-003]
```
Given: A Docker host with 48 running containers
When: collectStats executes
Then: No more than 10 concurrent ContainerStats API calls must be in flight at any time
```

**AC-006: Registry worker pool concurrency is bounded** [Maps to FR-006, NFR-003]
```
Given: A Docker host with 48 running containers
And: The freshness cache is empty
When: checkImageUpdates executes
Then: No more than 5 concurrent registry API calls must be in flight at any time
```

**AC-007: Network mode worker pool concurrency is bounded** [Maps to FR-010, NFR-003]
```
Given: A Docker host with 48 containers (some without network mode already set)
When: collectNetworkModes executes
Then: No more than 10 concurrent ContainerInspect API calls must be in flight at any time
```

**AC-008: Memory usage remains stable** [Maps to FR-002, FR-006, FR-010, NFR-002]
```
Given: A Docker host with 48 containers
When: The Docker collector executes with parallel worker pools
Then: Peak memory usage must not exceed 110% of the sequential implementation's memory usage for the same workload
```

**AC-009: Corrupt cache file does not prevent collection** [Maps to FR-016]
```
Given: The persisted cache file exists but contains invalid data
When: The agent starts
Then: The agent must log a warning
And: The Docker collector must start with an empty cache
And: Collection must proceed normally (re-checking all images against registries)
```

**AC-010: Existing Docker collector tests continue to pass** [Maps to FR-017, FR-018]
```
Given: The existing test suite for the Docker collector
When: All tests are executed after the parallelization changes
Then: All pre-existing tests must pass without modification
```

**AC-011: Collect() API contract is preserved** [Maps to FR-017]
```
Given: Any caller of DockerCollector.Collect()
When: Collect() is invoked
Then: It must return a single DockerInfo value and an error
And: The return type and behavior must be identical to the pre-change implementation
```

**AC-012: Collection phases execute in correct order** [Maps to BR-004]
```
Given: A Docker host with both running and stopped containers
When: Collect() executes
Then: collectStats must complete before collectNetworkModes begins
And: Containers that received NetworkMode from collectStats must be skipped by collectNetworkModes
```

---

## 9. Out of Scope

- Changing the 30-minute freshness cache TTL value.
- Changing the telemetry interval or push mechanism.
- Adding new data fields to `DockerInfo` or `ContainerInfo`.
- Modifying the server-side handling of Docker telemetry.
- Adding configuration options for worker pool sizes (they are fixed constants in this story).
- Parallelizing the initial `ContainerList` call (it is a single API call, not per-container).
- Registry authentication changes.
- Changes to the `container_logs` collector.

---

## 10. Assumptions

1. The Docker daemon can handle 10 concurrent `ContainerStats`/`ContainerInspect` calls without degradation on typical homelab hardware.
2. 5 concurrent registry API calls will not trigger rate limiting on Docker Hub or GHCR under normal usage patterns.
3. The agent has a writable data directory on disk where the cache file can be stored (same location pattern as the existing nginx access log offset file).
4. The 10-second per-call timeout is sufficient for normal Docker API responses while being short enough to unblock collection when a call hangs.
5. The three collection phases do not need to run concurrently with each other -- only the individual API calls within each phase are parallelized. Phases remain sequential per BR-004.
6. No existing Docker collector tests exist (none found at `docker_test.go`), so AC-010 applies to the broader agent test suite.

---

## 11. Open Questions

None. All requirements are sufficiently specified for implementation.

---

## 12. Dependencies

| Dependency | Description |
|------------|-------------|
| Docker SDK | The existing `github.com/docker/docker/client` package is already in use. No new dependencies required for parallelization. |
| Agent data directory | The agent must have a known, writable directory for cache file persistence. Must align with the existing pattern used by other collectors (e.g., nginx access log offset). |
| Existing collector interface | The `Collect(ctx context.Context) (interface{}, error)` signature must not change. |

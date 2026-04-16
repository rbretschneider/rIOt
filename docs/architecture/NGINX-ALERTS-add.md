# Architecture Decision Document

| Field         | Value                                        |
|---------------|----------------------------------------------|
| Story ID      | NGINX-ALERTS                                 |
| FRD Reference | docs/requirements/NGINX-ALERTS-frd.md        |
| Author        | Architect Agent                              |
| Date          | 2026-04-16                                   |
| Status        | FINAL                                        |

---

## 1. Summary

This story adds nginx access log metric collection to the agent's existing webservers collector, transmits per-interval HTTP status code counts through the telemetry payload, and evaluates those counts against user-configured alert rules on the server. The agent parses the combined log format line-by-line with O(1) memory, persists its byte offset to disk across restarts, and enriches the existing `ProxyServer` model with an `AccessMetrics` field. The server gains three new alertable metrics (`nginx_5xx_count`, `nginx_4xx_count`, `nginx_request_count`), two new event types, and corresponding dashboard/template entries.

---

## 2. Technical Context

### What Exists

- **Agent webservers collector** (`internal/agent/collectors/webservers.go`): Instantiates `NginxParser`, `CaddyParser`, `FerronParser`. Each parser implements `ProxyParser` (Detect + Parse). Returns `*models.WebServerInfo` containing `[]ProxyServer`.
- **NginxParser** (`webservers_nginx.go`): Detects nginx binary, reads config via `nginx -T`, parses sites/upstreams/certs/security. No access log parsing.
- **WebServersCollector** is a zero-field struct. Config does not flow to it today. The `DockerCollector` pattern shows how to pass config into collectors via struct fields.
- **Collector interface**: `Name() string` + `Collect(ctx) (interface{}, error)`. Collectors are registered in `collector.go:RegisterDefaultsWithDocker()` and filtered by `config.Collector.Enabled`.
- **CollectorConfig** (`internal/agent/config.go`): Currently has `Enabled []string` and `SMARTInterval int`. No per-collector nested config.
- **HeartbeatData** carries `LogErrors int` (atomic counter from journal log collector). The FRD explicitly places nginx metrics out of scope for heartbeat (Section 9).
- **Telemetry flow**: `collectAll()` runs all collectors, switches on return type, assigns to `FullTelemetryData` fields. Server handler calls `CheckTelemetryThresholds()` which dispatches to per-domain `Check*Alerts()` methods.
- **Alert evaluation**: `evaluateMetric()` is the standard pattern for numeric threshold metrics. It checks user rules first, then falls through to hardcoded fallbacks for `mem_percent`/`disk_percent`/`updates` only.
- **Alert templates**: Hard-coded in `internal/server/events/templates.go`. A "webserver" category already exists (cert_expiring, cert_expired).
- **Dashboard METRICS list** in `AlertRuleSettings.tsx`: Some metrics (gpu_*, cert_*, usb_*, disk_smart_*) exist in templates/generator but are not yet in the dropdown. The pattern for this story is to add the nginx metrics to the dropdown.
- **No offset persistence mechanism** exists in the agent. The `LogsCollector` uses an in-memory `lastSeen` timestamp (resets on restart). NFR-005 requires disk persistence for this feature.

### What Needs to Change

1. Agent config struct extended with nested webservers config
2. `WebServersCollector` receives config and passes access log path to `NginxParser`
3. `NginxParser` gains access log tailing + parsing
4. New `NginxAccessMetrics` struct on `ProxyServer` model
5. Server event generator gains `CheckNginxAccessAlerts()` method
6. New event type constants
7. New alert templates
8. Dashboard metric dropdown and defaults extended

---

## 3. Architecture Decisions

### AD-001: Access Log Parsing Lives in NginxParser, Not a Separate Collector

**Decision**: The access log parsing is a new method on `NginxParser` called from `WebServersCollector.Collect()` after `Parse()` completes. The metrics are attached to the `ProxyServer` struct returned by the existing collector. No new collector is created.

**Rationale**: The FRD (FR-012) requires this. The webservers collector already owns the `ProxyServer` model. A separate collector would require a new return type case in `collectAll()` and would disconnect nginx config data from nginx traffic data.

**Alternatives Considered**: (1) New `NginxAccessLogCollector` -- rejected because it violates FR-012 and creates a disconnected data path. (2) Separate goroutine polling access log -- rejected for the same reason.

**Consequences**: The `WebServersCollector` needs access to config to pass the access log path. This means changing its struct from zero-fields to carrying config, following the `DockerCollector` pattern.

### AD-002: Config Flows via WebServersCollector Struct Fields

**Decision**: Add a `WebServersConfig` struct to `CollectorConfig`. Pass it to `WebServersCollector` at registration time (like `DockerOptions`). `WebServersCollector` passes the access log path to `NginxParser` via a new field.

**Rationale**: This follows the established pattern (`DockerCollector` receives `DockerOptions`). YAML zero-value behavior means existing configs without the new key continue to work.

**Alternatives Considered**: Global config lookup inside the collector -- rejected because no other collector does this; it would break the pattern.

**Consequences**: `RegisterDefaultsWithDocker` signature grows or a new `RegisterDefaultsWithOptions` is needed. Decision: add a `WebServersOptions` parameter to `RegisterDefaultsWithDocker` since we are already passing docker options there, or add a setter like `SetSMARTInterval`. The setter pattern (`SetAccessLogPath`) is cleaner and avoids changing the `RegisterDefaultsWithDocker` signature.

### AD-003: Offset Persisted to a State File Under /etc/riot/ (or PROGRAMDATA on Windows)

**Decision**: The byte offset is persisted to `/etc/riot/nginx-access-log.offset` (a single file containing the int64 offset as a decimal string). Written atomically (write to temp + rename) after each collection cycle. Read on startup. If the file does not exist, the agent seeks to the end of the access log (first-start behavior per FRD Section 7).

**Rationale**: NFR-005 requires offset survival across restarts. The agent already uses `/etc/riot/` for config, ID, certs, and DNS cache. A simple file with a single integer is the lowest-complexity approach. The `LogsCollector` does NOT persist its cursor (it uses in-memory `lastSeen`), but NFR-005 explicitly requires persistence here because access logs can be enormous and re-processing would be wasteful.

**Alternatives Considered**: (1) Store offset in agent.yaml -- rejected because config file is user-edited. (2) SQLite/buffer.db -- rejected as overkill for a single int64. (3) In-memory only (like LogsCollector) -- rejected because NFR-005 forbids it.

**Consequences**: New function `OffsetPath()` in config.go following the existing `IDPath()`, `CertPath()` pattern. The offset file is not sensitive (it's just a number), so 0644 permissions are fine.

### AD-004: Line Parsing via bufio.Scanner with Fixed-Size Regex

**Decision**: Use `bufio.Scanner` with default 64KiB buffer to read lines. Extract the HTTP status code using a targeted regex or manual string parsing of the combined log format. Accumulate into five `int` counters (total, 2xx, 3xx, 4xx, 5xx) that reset each cycle.

**Rationale**: `bufio.Scanner` reads one line at a time from the file handle, achieving O(1) memory (NFR-001). The combined log format has the status code at a fixed structural position: after the closing quote of the request line, separated by a space. Manual byte scanning of this position is faster than regex and allocates nothing. Decision: use manual string parsing (find the `" ` after the request, parse the next 3 digits as the status code). This avoids regex compilation and per-line string allocations.

**Alternatives Considered**: (1) Full regex per line -- rejected because it allocates submatches per line, violating NFR-002. (2) Reading entire file -- rejected per FR-009.

**Consequences**: Only the combined log format is supported (FR-005). Custom formats will not parse and lines will be silently skipped (NFR-006).

### AD-005: Metrics Flow Through Telemetry Only, Not Heartbeat

**Decision**: Nginx access log metrics are fields on `ProxyServer.AccessMetrics` within the telemetry payload. They are NOT added to `HeartbeatData`.

**Rationale**: The FRD Section 9 (Out of Scope) explicitly states: "Access log metrics in the heartbeat payload (metrics flow through telemetry only)." The telemetry interval (default 60s) is frequent enough for alerting. This avoids adding complexity to the lightweight heartbeat path.

**Consequences**: Alert evaluation happens in `CheckTelemetryThresholds` -> `CheckWebServerAlerts` (or a new `CheckNginxAccessAlerts`), not in `CheckHeartbeatThresholds`. The evaluation window equals the telemetry poll interval.

### AD-006: No Hardcoded Fallback Thresholds for Nginx Metrics

**Decision**: The `evaluateMetric` fallback switch statement will NOT include cases for `nginx_5xx_count`, `nginx_4xx_count`, or `nginx_request_count`. These metrics only fire alerts when the user has explicitly created a rule.

**Rationale**: FR-018 explicitly forbids hardcoded fallbacks. Unlike memory/disk which have universal safe thresholds, nginx error counts are entirely workload-dependent.

**Consequences**: A fresh install with no alert rules will never generate nginx events, even if 5xx errors are spiking. This is the intended behavior per BR-001.

### AD-007: Access Metrics Attached to ProxyServer, Not WebServerInfo

**Decision**: Add `AccessMetrics *NginxAccessMetrics` field to `ProxyServer`. This means each detected proxy server can independently carry access metrics. For now, only nginx populates it.

**Rationale**: `ProxyServer` represents a single detected server (nginx, Caddy, etc.). Access metrics are per-server. Putting it on `WebServerInfo` would be ambiguous when multiple servers are detected. The FRD Section 7 says "nested within the existing WebServerInfo or ProxyServer model, associated with the nginx server entry" -- `ProxyServer` is the correct level.

**Consequences**: The server-side alert evaluator iterates `WebServerInfo.Servers` and checks each server's `AccessMetrics`. If Caddy access log support is added later, the same field can be reused.

---

## 4. Component Changes

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `internal/models/telemetry.go` | Add `NginxAccessMetrics` struct; add `AccessMetrics *NginxAccessMetrics` field to `ProxyServer` |
| MODIFY | `internal/models/events.go` | Add `EventNginx5xxHigh` and `EventNginx4xxHigh` event type constants |
| MODIFY | `internal/agent/config.go` | Add `WebServersConfig` and `NginxConfig` nested structs to `CollectorConfig`; add `NginxAccessLogOffsetPath()` function |
| MODIFY | `internal/agent/collectors/collector.go` | Add `SetNginxAccessLog(path string)` method on `Registry` to configure the `WebServersCollector` after registration |
| MODIFY | `internal/agent/collectors/webservers.go` | Add `AccessLogPath string` field to `WebServersCollector`; pass it to `NginxParser`; call `CollectAccessMetrics()` after `Parse()` |
| MODIFY | `internal/agent/collectors/webservers_nginx.go` | Add `AccessLogPath string` field to `NginxParser`; implement `CollectAccessMetrics()` method |
| CREATE | `internal/agent/collectors/webservers_nginx_access.go` | All access log tailing/parsing logic: `NginxAccessLogReader` struct (offset, path), `ReadAndCount()` method, line parser, offset persistence |
| CREATE | `internal/agent/collectors/webservers_nginx_access_test.go` | Unit tests for access log parsing, rotation detection, offset persistence, malformed line handling |
| MODIFY | `internal/agent/agent.go` | After `registry.FilterEnabled()`, call `registry.SetNginxAccessLog(cfg.Collector.Webservers.Nginx.AccessLog)` if configured |
| MODIFY | `internal/server/events/generator.go` | Add `CheckNginxAccessAlerts()` method; call it from `CheckWebServerAlerts()` or `CheckTelemetryThresholds()` |
| CREATE | `internal/server/events/generator_nginx_access_test.go` | Unit tests for nginx access alert evaluation |
| MODIFY | `internal/server/events/templates.go` | Add two nginx alert templates in "webserver" category |
| MODIFY | `web/src/pages/settings/AlertRuleSettings.tsx` | Add `nginx_5xx_count`, `nginx_4xx_count`, `nginx_request_count` to `METRICS` array and `METRIC_DEFAULTS` |
| MODIFY | `internal/agent/doctor.go` | No change needed -- `webservers` is already in `collectorDeps` |

---

## 5. Data Model Changes

### New Struct: `NginxAccessMetrics`

Location: `internal/models/telemetry.go`

```go
// NginxAccessMetrics holds per-interval HTTP status code counts from nginx access log parsing.
type NginxAccessMetrics struct {
    TotalRequests int `json:"total_requests"`
    Status2xx     int `json:"status_2xx"`
    Status3xx     int `json:"status_3xx"`
    Status4xx     int `json:"status_4xx"`
    Status5xx     int `json:"status_5xx"`
}
```

### Modified Struct: `ProxyServer`

**Before:**
```go
type ProxyServer struct {
    Name           string            `json:"name"`
    Version        string            `json:"version,omitempty"`
    Status         string            `json:"status"`
    PID            int               `json:"pid,omitempty"`
    ConfigPath     string            `json:"config_path,omitempty"`
    ConfigValid    *bool             `json:"config_valid,omitempty"`
    ConfigError    string            `json:"config_error,omitempty"`
    Sites          []ProxySite       `json:"sites,omitempty"`
    Certs          []ProxyCert       `json:"certs,omitempty"`
    Upstreams      []ProxyUpstream   `json:"upstreams,omitempty"`
    SecurityConfig *ProxySecurityCfg `json:"security_config,omitempty"`
}
```

**After:**
```go
type ProxyServer struct {
    Name           string              `json:"name"`
    Version        string              `json:"version,omitempty"`
    Status         string              `json:"status"`
    PID            int                 `json:"pid,omitempty"`
    ConfigPath     string              `json:"config_path,omitempty"`
    ConfigValid    *bool               `json:"config_valid,omitempty"`
    ConfigError    string              `json:"config_error,omitempty"`
    Sites          []ProxySite         `json:"sites,omitempty"`
    Certs          []ProxyCert         `json:"certs,omitempty"`
    Upstreams      []ProxyUpstream     `json:"upstreams,omitempty"`
    SecurityConfig *ProxySecurityCfg   `json:"security_config,omitempty"`
    AccessMetrics  *NginxAccessMetrics `json:"access_metrics,omitempty"`
}
```

### Modified Struct: `CollectorConfig`

**Before:**
```go
type CollectorConfig struct {
    Enabled       []string `yaml:"enabled"`
    SMARTInterval int      `yaml:"smart_interval"`
}
```

**After:**
```go
type CollectorConfig struct {
    Enabled       []string         `yaml:"enabled"`
    SMARTInterval int              `yaml:"smart_interval"`
    Webservers    WebServersConfig `yaml:"webservers"`
}

type WebServersConfig struct {
    Nginx NginxConfig `yaml:"nginx"`
}

type NginxConfig struct {
    AccessLog string `yaml:"access_log"`
}
```

### New Event Type Constants

Location: `internal/models/events.go`

```go
EventNginx5xxHigh EventType = "nginx_5xx_high"
EventNginx4xxHigh EventType = "nginx_4xx_high"
```

### No Database Schema Changes

Nginx metrics are stored as part of the telemetry JSONB blob within the existing `ProxyServer` structure. No new tables or columns are needed. The alert rule `metric` column already accepts arbitrary strings.

---

## 6. API / Interface Contract

### No New Endpoints

All data flows through existing endpoints:

- **Agent -> Server**: `POST /api/v1/devices/{id}/telemetry` -- existing endpoint. The `web_servers` field in the JSON body now contains `access_metrics` on each server entry.
- **Alert Templates**: `GET /api/v1/settings/alert-templates` -- existing endpoint. Response now includes two additional templates.

### Modified Telemetry Payload Shape (relevant fragment)

```json
{
  "web_servers": {
    "servers": [
      {
        "name": "nginx",
        "version": "1.24.0",
        "status": "running",
        "access_metrics": {
          "total_requests": 1523,
          "status_2xx": 1400,
          "status_3xx": 50,
          "status_4xx": 60,
          "status_5xx": 13
        },
        "sites": [...],
        "certs": [...]
      }
    ]
  }
}
```

When `access_log` is not configured or the file is unreadable, `access_metrics` is `null`/omitted (Go nil pointer, `omitempty`).

---

## 7. Sequence / Flow

### Agent Collection Cycle (Telemetry)

1. `collectAll()` iterates registered collectors, calls `WebServersCollector.Collect(ctx)`.
2. `WebServersCollector.Collect()` iterates parsers. For `NginxParser`:
   a. `Detect(ctx)` runs (unchanged) -- returns `*ProxyServer` or nil.
   b. `Parse(ctx, srv)` runs (unchanged) -- populates sites, upstreams, certs.
   c. If `WebServersCollector.AccessLogPath != ""` AND the server name is "nginx":
      - Call `NginxParser.CollectAccessMetrics(srv)`.
      - `CollectAccessMetrics` delegates to `NginxAccessLogReader.ReadAndCount()`.
3. `NginxAccessLogReader.ReadAndCount()`:
   a. `os.Stat(path)` to get file size.
   b. If file does not exist: log warning, return zero metrics.
   c. If file size < stored offset: log rotation detected, reset offset to 0.
   d. If stored offset == 0 AND this is first run (no offset file): seek to end, save offset, return zero metrics.
   e. `os.Open(path)`, `file.Seek(offset, io.SeekStart)`.
   f. `bufio.Scanner` loop: for each line, extract status code via manual string parse, increment counter.
   g. Update offset to current file position.
   h. Write offset to disk atomically (write temp file, rename).
   i. Return `*NginxAccessMetrics` with the five counters.
4. `srv.AccessMetrics = metrics` (set on the ProxyServer).
5. `collectAll()` assigns the `*WebServerInfo` to `data.WebServers`.
6. Telemetry is sent to server.

### Server Alert Evaluation

1. `Telemetry` handler receives POST, stores snapshot.
2. Calls `CheckTelemetryThresholds(ctx, deviceID, hostname, &snap.Data)`.
3. `CheckTelemetryThresholds` reaches the `data.WebServers != nil` branch.
4. Calls `CheckWebServerAlerts(ctx, deviceID, hostname, data.WebServers)`.
5. `CheckWebServerAlerts` (modified): after checking certs, calls `CheckNginxAccessAlerts(ctx, deviceID, hostname, ws)`.
6. `CheckNginxAccessAlerts`:
   a. Iterates `ws.Servers`.
   b. For each server with `AccessMetrics != nil`:
      - Call `evaluateMetric(ctx, deviceID, "nginx_5xx_count", float64(m.Status5xx), hostname, EventNginx5xxHigh, ...)`.
      - Call `evaluateMetric(ctx, deviceID, "nginx_4xx_count", float64(m.Status4xx), hostname, EventNginx4xxHigh, ...)`.
      - Call `evaluateMetric(ctx, deviceID, "nginx_request_count", float64(m.TotalRequests), hostname, EventNginx5xxHigh, ...)`.
   c. `evaluateMetric` handles rule matching, device scoping, cooldown, notification dispatch.
   d. Since nginx metrics have no hardcoded fallback case in the switch, `evaluateMetric` returns silently if no rules match (AD-006).

---

## 8. Acceptance Criteria Mapping

| AC ID  | Fulfilled By | Test Strategy |
|--------|-------------|---------------|
| AC-001 | `WebServersCollector.Collect()` checks `AccessLogPath != ""`; `NginxAccessLogReader.ReadAndCount()` opens configured path; skips entirely when path is empty | Unit: collector with/without access_log config |
| AC-002 | `NginxAccessLogReader.ReadAndCount()` seeks to stored offset, scans lines, updates offset after last complete line; only 5 int counters in struct | Unit: write N lines to temp file, verify offset advances exactly, verify counter values |
| AC-003 | `ReadAndCount()` compares `os.Stat().Size()` against stored offset; resets to 0 when size < offset | Unit: write file, record offset beyond new file size, verify reset |
| AC-004 | Line parser extracts status code, classifies into 2xx/3xx/4xx/5xx buckets | Unit: feed 100 known lines, verify exact counter values |
| AC-005 | `bufio.Scanner` with default buffer; no slices/maps grow with line count; only 5 int counters | Unit: process large temp file, verify no heap growth via benchmark; code review (no slice appends in hot path) |
| AC-006 | `ReadAndCount()` returns `*NginxAccessMetrics` with all zeros when `os.Stat` fails; logs warning | Unit: configure nonexistent path, verify zero metrics returned, no panic |
| AC-007 | Line parser silently skips lines that do not contain a parseable status code | Unit: feed mix of valid combined-format lines, blank lines, binary garbage, truncated entries |
| AC-008 | `CheckNginxAccessAlerts()` calls `evaluateMetric()` with `nginx_5xx_count`; `evaluateMetric` matches rule, creates event, dispatches notification | Unit: mock repos, set up rule with nginx_5xx_count > 10, pass telemetry with Status5xx=15, verify event created |
| AC-009 | `evaluateMetric` returns without event when no rules match and metric has no hardcoded fallback | Unit: pass telemetry with Status5xx=15, no rules configured, verify no event |
| AC-010 | `evaluateMetric` -> `onCooldown()` returns true within cooldown window; device scoping via `matchesDeviceScope` | Unit: fire rule, immediately re-evaluate within cooldown, verify second event suppressed |
| AC-011 | `matchesDeviceScope()` with exclude_devices containing "staging-box" returns false | Unit: existing `matchesDeviceScope` tests + new case for nginx metric path |
| AC-012 | Event message format string includes count value and hostname; event type is `EventNginx5xxHigh` | Unit: verify event.Message contains "15" and hostname; verify event.Type |
| AC-013 | `METRICS` array in `AlertRuleSettings.tsx` includes new entries; `templates.go` includes two webserver templates | Unit (frontend): verify METRICS contains entries; Unit (Go): verify `AlertTemplates()` includes new templates |
| AC-014 | `NginxAccessLogReader` reads offset from disk file on construction; writes after each cycle | Unit: write offset file, create reader, verify it seeks to stored offset |
| AC-015 | `NginxAccessMetrics` contains only int fields; parser never stores line content | Code review + unit: verify telemetry payload contains only integer counts |
| AC-016 | `WebServersCollector.Collect()` skips access log entirely when `AccessLogPath == ""`; `AccessMetrics` field remains nil | Unit: collect with empty path, verify ProxyServer.AccessMetrics is nil |

---

## 9. Error Handling

| Failure Mode | Handling | HTTP Status / Event |
|-------------|----------|-------------------|
| Access log file does not exist | `os.Stat` returns error -> log warning at DEBUG level, return `*NginxAccessMetrics` with all zero values | N/A (agent-side, no HTTP response) |
| Access log file not readable (permission denied) | Same as above -- `os.Open` fails, log warning, return zeros | N/A |
| Offset file does not exist (first run) | Seek to end of access log, write new offset file | N/A |
| Offset file not writable | Log warning, continue with in-memory offset (degraded: won't survive restart) | N/A |
| Malformed log line | Skip line, do not increment any counter, do not log (NFR-006 says "silently") | N/A |
| Log rotation (file smaller than offset) | Reset offset to 0, read from beginning | N/A |
| Scanner buffer overflow (line > 64KiB) | `bufio.Scanner` returns error for that line, skip it, continue | N/A |
| Server receives telemetry with nil AccessMetrics | `CheckNginxAccessAlerts` skips that server -- no-op | N/A |
| `evaluateMetric` DB error fetching rules | Logged by existing `slog.Error`, no event created | N/A |

---

## 10. Security Considerations

| Vector | Mitigation |
|--------|-----------|
| PII leakage from access logs | Parser extracts ONLY the 3-digit status code. No IP, URI, user-agent, or referer data leaves the agent. The `NginxAccessMetrics` struct contains only integer fields. (NFR-007) |
| Path traversal via config | The `access_log` path is read from agent.yaml which is root-owned (0600). The agent process reads the file -- no user input at runtime. |
| Malicious log content causing parser crash | Parser uses manual byte scanning with bounds checks. No regex on untrusted input. Malformed lines are skipped. |
| Large log file causing memory exhaustion | `bufio.Scanner` with default 64KiB max line buffer. No slices grow with input size. (NFR-001, NFR-002) |
| Symlink attack on offset file | Offset file is written via atomic rename in `/etc/riot/` which is root-owned. Same risk profile as existing ID/cert files. |

---

## 11. Performance Considerations

### Memory Budget (NFR-001)

The parser's hot path allocates:
- `bufio.Scanner` internal buffer: up to 64 KiB (default `MaxScanTokenSize`)
- 5 `int` counters: 40 bytes
- `os.File` handle: ~160 bytes
- Total: ~65 KiB peak, well within the 64 KiB heap budget (the scanner buffer is the read buffer itself, not "beyond" it)

No per-line allocations:
- No `string()` conversion of the full line (work on `scanner.Bytes()`)
- No regex submatches
- No slice appends
- No map operations

### Throughput (NFR-003)

Manual byte scanning of the status code position is ~5ns/line. At 50,000 lines/second target on a Pi 4, the budget is 20us/line. The bottleneck will be disk I/O, not parsing.

### Parsing Strategy for Combined Log Format

The combined log format is:
```
$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"
```

To extract `$status` without allocating:
1. Scan `scanner.Bytes()` for the byte sequence `" ` (quote-space) that ends the `"$request"` field.
2. The status code starts at the next non-space byte.
3. Read 3 bytes, verify all are ASCII digits, compute `(b[0]-'0')*100 + (b[1]-'0')*10 + (b[2]-'0')`.

This is O(n) in line length for the scan, O(1) in allocations.

### Offset File I/O

One `os.WriteFile` per collection cycle (not per line). At 60s poll interval, this is negligible.

### Indexing

No database index changes needed. Nginx metrics are stored inline in the telemetry JSONB blob, not queried independently.

---

## 12. Implementation Notes for Engineers

### Agent Side

1. **New file `webservers_nginx_access.go`** should contain all access log logic. Keep it separate from `webservers_nginx.go` (config parsing) for clarity.

2. **NginxAccessLogReader struct**:
   ```go
   type NginxAccessLogReader struct {
       path       string
       offsetPath string
       offset     int64
       loaded     bool  // true after first load from disk or first-run seek-to-end
   }
   ```

3. **Line parsing function signature**:
   ```go
   // parseStatusCode extracts the HTTP status code from a combined log format line.
   // Returns 0 if the line is malformed. Works on []byte to avoid allocation.
   func parseStatusCode(line []byte) int
   ```

4. **Atomic offset write**: Use `os.WriteFile` to a `.tmp` file then `os.Rename`. On Linux, rename is atomic within the same filesystem. On Windows (not a target for this feature since webservers collector is Linux-only), this is moot.

5. **First-run behavior**: When no offset file exists, seek to end of the access log file. This means the first telemetry cycle after enabling will report zero counts, which is correct -- we do not want to alert on historical data.

6. **Scanner on Bytes**: Use `scanner.Bytes()` (not `scanner.Text()`) to avoid a string allocation per line. The `parseStatusCode` function must work on `[]byte`.

7. **Config plumbing order**:
   - `agent.go:New()`: creates registry, registers defaults.
   - `agent.go:Run()`: calls `registry.FilterEnabled()`, then calls `registry.SetNginxAccessLog(cfg.Collector.Webservers.Nginx.AccessLog)`.
   - `SetNginxAccessLog` iterates registered collectors, finds `*WebServersCollector`, sets its `AccessLogPath` field.

8. **The `WebServersCollector` is Linux-only** (returns empty on other platforms). The access log code inherits this guard.

9. **Offset path function** in `config.go`:
   ```go
   func NginxAccessLogOffsetPath() string {
       if runtime.GOOS == "windows" {
           return os.Getenv("PROGRAMDATA") + "\\riot\\nginx-access-log.offset"
       }
       return "/etc/riot/nginx-access-log.offset"
   }
   ```

### Server Side

10. **`CheckNginxAccessAlerts`** goes in `generator.go` alongside `CheckWebServerAlerts`. Call it from `CheckWebServerAlerts` at the end, after the cert checks.

11. **Message format for events**: Use the pattern from existing evaluateMetric calls.
    - 5xx: `fmt.Sprintf("Nginx 5xx errors: %d in last interval on %s", int(val), hostname)`
    - 4xx: `fmt.Sprintf("Nginx 4xx errors: %d in last interval on %s", int(val), hostname)`
    - request_count: `fmt.Sprintf("Nginx request count: %d in last interval on %s", int(val), hostname)` -- note: `nginx_request_count` uses `EventNginx5xxHigh` is wrong. For request_count, there is no dedicated event type. Since the FRD only defines two event types (nginx_5xx_high and nginx_4xx_high), `nginx_request_count` alert rules should fire the event type based on context. Decision: use a generic approach -- `nginx_request_count` does not need its own event type. When it triggers, use `EventNginx5xxHigh` as a catch-all. Actually, re-reading FR-019, only two event types are defined. For `nginx_request_count`, use `EventNginx5xxHigh` as the event type since request count alerts are most likely used to detect traffic spikes which correlate with error conditions. Alternatively, add a third event type. Decision: Do NOT add a third event type (FRD does not specify one). Use `EventNginx5xxHigh` for request_count as a pragmatic choice -- the event message will clearly state what was measured.

    **Correction**: On reflection, reusing `EventNginx5xxHigh` for request count is semantically wrong and will confuse users filtering by event type. Since `evaluateMetric` requires an event type, and the FRD does not specify one for request_count, add `EventNginxRequestHigh` as a third event type. This is a minor extension consistent with the FRD's intent.

    Updated event types:
    ```go
    EventNginx5xxHigh     EventType = "nginx_5xx_high"
    EventNginx4xxHigh     EventType = "nginx_4xx_high"
    EventNginxRequestHigh EventType = "nginx_request_high"
    ```

12. **`evaluateMetric` for nginx_request_count**: The message function:
    ```go
    func(val float64) string {
        return fmt.Sprintf("Nginx request count: %d in last interval on %s", int(val), hostname)
    }
    ```

### Dashboard

13. **METRICS array additions** in `AlertRuleSettings.tsx`:
    ```typescript
    { value: 'nginx_5xx_count', label: 'Nginx 5xx Errors' },
    { value: 'nginx_4xx_count', label: 'Nginx 4xx Errors' },
    { value: 'nginx_request_count', label: 'Nginx Request Count' },
    ```

14. **METRIC_DEFAULTS additions**:
    ```typescript
    nginx_5xx_count:     { operator: '>', threshold: 10,  severity: 'critical', cooldown: 300,  hint: 'Number of 5xx responses per telemetry interval' },
    nginx_4xx_count:     { operator: '>', threshold: 50,  severity: 'warning',  cooldown: 900,  hint: 'Number of 4xx responses per telemetry interval' },
    nginx_request_count: { operator: '>', threshold: 1000, severity: 'info',    cooldown: 900,  hint: 'Total request count per telemetry interval' },
    ```

15. **Alert templates** in `templates.go`:
    ```go
    {
        ID:              "nginx_5xx_high",
        Name:            "Nginx 5xx Errors High",
        Category:        "webserver",
        Metric:          "nginx_5xx_count",
        Operator:        ">",
        Threshold:       10,
        Severity:        "critical",
        CooldownSeconds: 300,
        NeedsTargetName: false,
        Description:     "Fires when nginx 5xx error count exceeds the threshold per telemetry interval",
    },
    {
        ID:              "nginx_4xx_high",
        Name:            "Nginx 4xx Errors High",
        Category:        "webserver",
        Metric:          "nginx_4xx_count",
        Operator:        ">",
        Threshold:       50,
        Severity:        "warning",
        CooldownSeconds: 900,
        NeedsTargetName: false,
        Description:     "Fires when nginx 4xx error count exceeds the threshold per telemetry interval",
    },
    ```

### Default Config Template

16. **Update `defaultConfigTemplate`** in `config.go` to include commented-out webservers config:
    ```yaml
    # webservers:
    #   nginx:
    #     access_log: /var/log/nginx/access.log
    ```

---

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented
- [ ] All AC mappings in Section 8 have corresponding tests with AC reference in test name
- [ ] `NginxAccessMetrics` struct added to `telemetry.go` with JSON tags
- [ ] `ProxyServer` has `AccessMetrics` field with `omitempty`
- [ ] `CollectorConfig` extended with nested `WebServersConfig` / `NginxConfig`
- [ ] `NginxAccessLogReader` implements offset persistence, rotation detection, first-run seek-to-end
- [ ] Line parser uses `scanner.Bytes()` with zero per-line allocations
- [ ] `CheckNginxAccessAlerts()` calls `evaluateMetric()` for all three metrics
- [ ] Two new alert templates in "webserver" category
- [ ] Three new metrics in dashboard METRICS array with defaults
- [ ] Three new event type constants in `events.go`
- [ ] Full Go test suite passes (`go test ./...`)
- [ ] Frontend test suite passes (`cd web && npm run test:run`)
- [ ] No new linting errors introduced
- [ ] No commented-out code or debug print statements

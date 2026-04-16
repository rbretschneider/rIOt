# Formal Requirements Document

| Field   | Value                                      |
|---------|--------------------------------------------|
| Story ID | NGINX-ALERTS                              |
| Title   | Nginx Access Log Error Rate Alerting        |
| Author  | Business Developer Agent                    |
| Date    | 2026-04-16                                  |
| Status  | FINAL                                       |

---

## 1. Executive Summary

Enable rIOt to monitor nginx access logs for elevated HTTP error rates (4xx and 5xx) and alert homelab operators through the existing notification system when configurable thresholds are breached. The feature extends the existing webservers collector on the agent side and the alert rule evaluation engine on the server side, allowing users to create threshold-based alert rules for nginx error metrics through the existing Settings > Alert Rules UI. The primary constraint is an extremely low memory footprint on resource-constrained devices (Raspberry Pi and similar).

---

## 2. Background & Context

rIOt already monitors nginx at the configuration and service level through the webservers collector: it detects the nginx binary, validates config, parses virtual hosts, upstreams, SSL certificates, and security directives. However, it does not monitor real traffic. Operators currently have no visibility into HTTP error spikes from within rIOt and must rely on external tools or manual log inspection.

The existing logs collector provides a reference pattern: it tails journald entries since the last collection, counts errors, and includes aggregate counts in heartbeat data. The alert rule engine (`generator.go`) already supports threshold-based evaluation with cooldown, device scoping, and notification dispatch via ntfy, webhook, and SMTP. This story extends both systems to cover nginx access log metrics.

The existing heartbeat already carries a `log_errors` count from journald. Nginx access log metrics will be new fields on the telemetry payload, evaluated by the same alert rule engine.

---

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Homelab Operator | The rIOt dashboard user who creates alert rules and receives notifications | Full access to Settings > Alert Rules UI; creates, edits, deletes alert rules |
| rIOt Agent | Daemon running on monitored devices; collects telemetry and pushes to server | Reads nginx access log file; pushes telemetry via authenticated HTTP |
| rIOt Server | Receives telemetry, evaluates alert rules, dispatches notifications | Evaluates metrics against alert rules; creates events; dispatches notifications |
| Notification Channels | ntfy, webhook, SMTP endpoints configured by the operator | Receive alert payloads from the server dispatcher |

---

## 4. Functional Requirements

### Agent — Access Log Collection

**FR-001**: The agent must read the nginx access log file path from the agent YAML configuration under the `collectors.webservers.nginx.access_log` key.

**FR-002**: If the `access_log` key is not configured, the agent must not attempt to parse any nginx access log. There must be no default path assumption.

**FR-003**: The agent must track the byte offset of the last-read position in the access log file across collection intervals. On each collection cycle, it must read only new lines appended since the last-read position (tail-forward pattern).

**FR-004**: The agent must handle log file rotation (the file being replaced or truncated). When the file size is smaller than the stored byte offset, the agent must reset the offset to zero and read from the beginning of the new file.

**FR-005**: The agent must parse each line individually using the nginx "combined" log format: `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent"`. The agent must extract only the HTTP status code from each line.

**FR-006**: The agent must classify each parsed status code into one of four counters:
- 2xx (200-299)
- 3xx (300-399)
- 4xx (400-499)
- 5xx (500-599)

**FR-007**: The agent must maintain a fixed-size set of counters (total requests, 2xx count, 3xx count, 4xx count, 5xx count) for the current collection interval. These counters must be reset to zero at the start of each collection cycle.

**FR-008**: The agent must not store individual parsed log lines, buffer log content beyond the current line being parsed, or accumulate unbounded data structures. The memory footprint of the access log parser must be O(1) regardless of log volume.

**FR-009**: The agent must not load the entire access log file into memory. It must read and parse line-by-line using a streaming reader (scanning one line at a time from the file).

**FR-010**: The agent must include nginx access log metrics in the telemetry payload sent to the server. The metrics must include:
- Total request count for the interval
- 4xx error count for the interval
- 5xx error count for the interval
- 2xx count for the interval
- 3xx count for the interval

**FR-011**: If the configured access log file does not exist or is not readable, the agent must log a warning and report zero counts. It must not crash, panic, or stop collecting other telemetry.

**FR-012**: The access log parsing must execute within the existing webservers collector collection cycle. It must not introduce a separate goroutine, timer, or independent polling loop.

### Server — Telemetry Reception and Storage

**FR-013**: The server must accept nginx access log metrics as part of the existing telemetry payload within the `web_servers` field.

**FR-014**: The server must store nginx access log metrics as part of the telemetry snapshot (same retention policy as existing telemetry: 30 days default, configurable).

### Server — Alert Rule Evaluation

**FR-015**: The server must support the following new metric types for alert rules:
- `nginx_5xx_count`: the raw 5xx error count for the telemetry interval
- `nginx_4xx_count`: the raw 4xx error count for the telemetry interval
- `nginx_request_count`: the total request count for the telemetry interval

**FR-016**: The alert rule engine must evaluate nginx access log metrics using the same `evaluateMetric` pattern used for existing threshold metrics (mem_percent, disk_percent, etc.), including operator comparison, device scoping, cooldown, and notification dispatch.

**FR-017**: Nginx access log alert rules must support all existing alert rule fields: name, enabled, metric, operator, threshold, severity, include_devices, exclude_devices, cooldown_seconds, notify, and template_id.

**FR-018**: The server must not apply any hardcoded fallback thresholds for nginx metrics. Alerts must only fire when the user has explicitly created an alert rule. (Unlike mem_percent and disk_percent which have hardcoded 90% fallbacks, nginx metrics must have no fallback behavior.)

### Server — Event Types

**FR-019**: The server must define new event types for nginx access log alerts:
- `nginx_5xx_high`: triggered when 5xx count exceeds the user-configured threshold
- `nginx_4xx_high`: triggered when 4xx count exceeds the user-configured threshold

**FR-020**: Nginx access log events must include the actual metric value in the event message (e.g., "Nginx 5xx errors: 15 in last interval on <hostname>").

### Dashboard — Alert Rule Configuration

**FR-021**: The Settings > Alert Rules UI must include the new nginx metric types (`nginx_5xx_count`, `nginx_4xx_count`, `nginx_request_count`) in the metric dropdown when creating or editing alert rules.

**FR-022**: The dashboard must include alert templates for common nginx alert scenarios:
- "Nginx 5xx errors high" (metric: `nginx_5xx_count`, operator: `>`, suggested threshold: 10, category: webserver)
- "Nginx 4xx errors high" (metric: `nginx_4xx_count`, operator: `>`, suggested threshold: 50, category: webserver)

**FR-023**: The alert templates must appear in a "webserver" category in the template selector, consistent with the existing category taxonomy (system, service, network, process, container).

### Agent Configuration

**FR-024**: The agent configuration must support the following structure for nginx access log monitoring:
```yaml
collectors:
  enabled: [system, cpu, memory, disk, network, webservers]
  webservers:
    nginx:
      access_log: /var/log/nginx/access.log
```

**FR-025**: The `webservers` collector must remain in the agent's enabled collectors list for nginx access log monitoring to function. If `webservers` is not in the enabled list, the nginx access log configuration must be ignored.

**FR-026**: The `CollectorConfig` struct must be extended to support the nested `webservers.nginx.access_log` configuration key.

---

## 5. Non-Functional Requirements

**NFR-001** [Memory]: The agent's nginx access log parser must use no more than 64 KiB of heap memory beyond the read buffer during normal operation, regardless of log file size or request volume. This must be achievable with a line-scanning approach and fixed integer counters.

**NFR-002** [Memory]: The agent must not allocate slices, maps, or string buffers that grow proportionally to the number of log lines processed in a collection interval.

**NFR-003** [Performance]: The agent must process at least 50,000 log lines per second on a Raspberry Pi 4 (ARM Cortex-A72 @ 1.8GHz). Log parsing must not cause the agent to miss its poll interval deadline.

**NFR-004** [Performance]: If the access log contains more new lines than can be processed within a single collection cycle, the agent must process as many lines as possible, update the byte offset to the last fully-read line, and continue from that position on the next cycle. It must not skip lines.

**NFR-005** [Reliability]: The stored byte offset must survive agent restarts. The offset must be persisted to disk (or derived from the file state) so the agent does not re-process the entire log on restart.

**NFR-006** [Reliability]: The agent must tolerate malformed log lines gracefully. A line that does not match the expected combined format must be silently skipped (counted in neither success nor error counters). It must not cause a panic or stop processing subsequent lines.

**NFR-007** [Security]: The agent must not expose log file contents in telemetry payloads. Only aggregate integer counts must leave the agent. No IP addresses, URIs, user agents, or other PII from access logs must be transmitted.

**NFR-008** [Availability]: Failure to read or parse the nginx access log must not affect the collection or transmission of any other telemetry (CPU, memory, disk, services, etc.).

**NFR-009** [Observability]: The agent must log (via slog) the number of lines processed and any file rotation events at DEBUG level during each collection cycle.

---

## 6. Business Rules

**BR-001**: Nginx access log monitoring is strictly opt-in. It requires both: (a) the `webservers` collector enabled in the agent config, and (b) the `access_log` path explicitly configured. No automatic discovery of access log paths.

**BR-002**: Alert rules for nginx metrics are created through the same UI and stored in the same database table as all other alert rules. There is no separate configuration mechanism.

**BR-003**: Nginx access log metrics are interval-based counts (how many 5xx errors occurred since the last telemetry push), not rates. The threshold in the alert rule is compared against the raw count per interval. The interval length is determined by the agent's `poll_interval` setting (default 60 seconds).

**BR-004**: All existing alert rule features apply to nginx metrics without exception: device scoping (include/exclude), cooldown periods, severity levels, notification channel dispatch, and enable/disable toggle.

**BR-005**: If a device does not have nginx installed or the webservers collector reports no nginx access log metrics, the alert rule engine must silently skip nginx metric evaluation for that device. No error events must be generated for devices that do not report nginx metrics.

---

## 7. Data Requirements

### Entities

**New telemetry sub-structure — NginxAccessMetrics**

| Field | Type | Description |
|-------|------|-------------|
| total_requests | int | Total lines parsed in the interval |
| status_2xx | int | Count of 2xx responses |
| status_3xx | int | Count of 3xx responses |
| status_4xx | int | Count of 4xx responses |
| status_5xx | int | Count of 5xx responses |

This structure must be nested within the existing `WebServerInfo` or `ProxyServer` model, associated with the nginx server entry.

**Existing entities modified:**

| Entity | Change |
|--------|--------|
| `CollectorConfig` | Add nested `Webservers` field to support `webservers.nginx.access_log` |
| `ProxyServer` or `WebServerInfo` | Add `NginxAccessMetrics` field |
| Alert rule metric enum (UI) | Add `nginx_5xx_count`, `nginx_4xx_count`, `nginx_request_count` |
| Event type constants | Add `nginx_5xx_high`, `nginx_4xx_high` |
| Alert templates | Add two new templates in "webserver" category |

### Validation Rules

- `access_log` path must be a non-empty string when provided; the agent must verify the file exists and is readable at startup (log warning if not)
- Metric values sent to the server must be non-negative integers
- Alert rule thresholds for nginx metrics must be non-negative numbers (enforced by existing alert rule validation)

### State Transitions — Agent Log File Tracking

| State | Trigger | Next State |
|-------|---------|------------|
| No offset stored | Agent starts, access_log configured | Read from end of file (do not process historical data) |
| Offset stored | Collection cycle begins | Seek to offset, read new lines |
| Offset > file size | Log rotation detected | Reset offset to 0, read from beginning |
| File missing | access_log path not found | Log warning, report zero counts, retry next cycle |
| File returns | Previously missing file reappears | Start reading from beginning (offset 0) |

---

## 8. Acceptance Criteria

**AC-001** [Maps to FR-001, FR-002, FR-024, FR-025, FR-026]
```
Given: an agent with webservers enabled and access_log configured as "/var/log/nginx/access.log"
When: the agent starts a collection cycle
Then: the agent reads new lines from /var/log/nginx/access.log
And: the agent does not read any file when access_log is not configured
```

**AC-002** [Maps to FR-003, FR-007, FR-009]
```
Given: an nginx access log with 1000 new lines since the last collection
When: the agent collects telemetry
Then: the agent reads exactly those 1000 lines from the stored byte offset
And: the agent updates the stored byte offset to the end of the last line read
And: the agent maintains only 5 integer counters during processing (no line storage)
```

**AC-003** [Maps to FR-004]
```
Given: the stored byte offset is 50000 and the access log file size is 2000 (rotation occurred)
When: the agent begins a collection cycle
Then: the agent resets the offset to 0 and reads from the beginning of the new file
```

**AC-004** [Maps to FR-005, FR-006, FR-010]
```
Given: 100 new access log lines containing 80 "200" responses, 5 "301" responses, 10 "404" responses, and 5 "503" responses
When: the agent parses and aggregates
Then: the telemetry payload contains total_requests=100, status_2xx=80, status_3xx=5, status_4xx=10, status_5xx=5
```

**AC-005** [Maps to FR-008, NFR-001, NFR-002]
```
Given: an nginx access log producing 100,000 new lines per collection interval
When: the agent processes the log
Then: heap memory used by the parser does not exceed 64 KiB beyond the read buffer
And: no slices or maps grow proportionally to line count
```

**AC-006** [Maps to FR-011, NFR-008]
```
Given: the configured access_log path does not exist
When: the agent runs a collection cycle
Then: the agent logs a warning at DEBUG level
And: the agent reports zero counts for all nginx metrics
And: all other collectors (CPU, memory, disk, etc.) complete normally
```

**AC-007** [Maps to NFR-006]
```
Given: an access log containing a mix of valid combined-format lines and malformed lines (e.g., blank lines, binary garbage, truncated entries)
When: the agent parses the log
Then: valid lines are counted correctly
And: malformed lines are silently skipped without panic or error propagation
```

**AC-008** [Maps to FR-015, FR-016, FR-018]
```
Given: a device reporting nginx telemetry with status_5xx=15
And: an enabled alert rule with metric=nginx_5xx_count, operator=">", threshold=10
When: the server evaluates telemetry thresholds
Then: the server creates an event of type "nginx_5xx_high"
And: the server dispatches a notification if the rule has notify=true
```

**AC-009** [Maps to FR-018, BR-005]
```
Given: a device reporting nginx telemetry with status_5xx=15
And: no alert rules configured for nginx metrics
When: the server evaluates telemetry thresholds
Then: no event is created and no notification is sent
```

**AC-010** [Maps to FR-016, FR-017, BR-004]
```
Given: an alert rule for nginx_5xx_count with include_devices="webserver-01" and cooldown_seconds=300
And: device "webserver-01" reports status_5xx=20 (above threshold)
When: the server evaluates and fires the alert
And: 60 seconds later device "webserver-01" reports status_5xx=25
Then: the second evaluation does not create a new event (cooldown active)
```

**AC-011** [Maps to FR-016, BR-004]
```
Given: an alert rule for nginx_5xx_count with exclude_devices="staging-box"
And: device "staging-box" reports status_5xx=100
When: the server evaluates telemetry thresholds
Then: no event is created for "staging-box"
```

**AC-012** [Maps to FR-019, FR-020]
```
Given: a device triggers an nginx_5xx_count alert
When: the event is created
Then: the event type is "nginx_5xx_high"
And: the event message contains the actual count value and the device hostname
```

**AC-013** [Maps to FR-021, FR-022, FR-023]
```
Given: a user navigates to Settings > Alert Rules and clicks "New Rule"
When: the user opens the metric dropdown
Then: the options include "nginx_5xx_count", "nginx_4xx_count", and "nginx_request_count"
And: alert templates for "Nginx 5xx errors high" and "Nginx 4xx errors high" are available in a "webserver" category
```

**AC-014** [Maps to NFR-005]
```
Given: the agent has processed 5000 lines and stored the byte offset
When: the agent process is restarted
Then: the agent resumes reading from the stored byte offset
And: does not re-process the 5000 previously read lines
```

**AC-015** [Maps to NFR-007]
```
Given: the access log contains lines with client IP addresses, URIs, and user agents
When: the agent sends telemetry to the server
Then: the telemetry payload contains only integer counts (total, 2xx, 3xx, 4xx, 5xx)
And: no IP addresses, URIs, user agents, or other PII from the access log are present in the payload
```

**AC-016** [Maps to BR-001]
```
Given: an agent with webservers enabled but no access_log path configured
When: the agent collects webserver telemetry
Then: the agent does not attempt to open, scan, or parse any nginx access log file
And: the telemetry payload contains no nginx access metrics (field is nil/omitted)
```

---

## 9. Out of Scope

- Custom nginx log format parsing (only the default "combined" format is supported)
- Per-URI or per-endpoint error breakdown
- Per-upstream or per-site breakdown of error counts
- Historical rate graphs or time-series visualization of nginx metrics
- Parsing of nginx error.log (only access.log)
- Automatic discovery of access log file paths (must be explicitly configured)
- Rate calculation (e.g., errors per minute) — only raw counts per interval are provided; the user configures thresholds accordingly based on their poll interval
- Apache, Caddy, or other web server access log parsing (nginx only for v1)
- Access log metrics in the heartbeat payload (metrics flow through telemetry only)

---

## 10. Assumptions

- The nginx access log uses the default "combined" format. Users with custom log formats must not enable this feature until custom format support is added in a future story.
- The agent process has read permission on the configured access log file. If it does not, the agent logs a warning and reports zeros (FR-011).
- Log rotation is handled by external tools (logrotate). The agent detects rotation by comparing file size to stored offset (FR-004) but does not manage rotation itself.
- The agent's poll_interval (default 60 seconds) determines the effective evaluation window. A threshold of "10" on nginx_5xx_count means "more than 10 5xx errors per poll interval."
- The `CollectorConfig` struct can be extended with a nested map or struct for per-collector settings without breaking existing configs (existing YAML configs without the new key will continue to work due to Go's `yaml.Unmarshal` zero-value behavior).
- On first start with an existing (potentially large) access log, the agent must seek to the end of the file and begin tracking from that point forward, not process historical lines (State Transition: "No offset stored").

---

## 11. Open Questions

None. All requirements have been clarified by the user story and codebase analysis.

---

## 12. Dependencies

| Dependency | Type | Description |
|------------|------|-------------|
| Webservers collector | Existing code | Access log parsing runs within the existing webservers collector cycle |
| Alert rule engine (`generator.go`) | Existing code | New metric types evaluated by `evaluateMetric` and `CheckTelemetryThresholds` |
| Alert rule database table | Existing schema | No schema changes needed; new metric values stored in existing `metric` column |
| Notification dispatcher | Existing code | ntfy, webhook, SMTP dispatch for triggered alerts |
| Dashboard Alert Rules UI | Existing code | Metric dropdown and template list must be extended |
| Agent YAML config | Existing code | `CollectorConfig` struct must be extended for nested webserver settings |
| `internal/models/telemetry.go` | Existing code | `WebServerInfo` or `ProxyServer` model extended with access metrics |
| `internal/models/events.go` | Existing code | New event type constants added |
| `internal/agent/doctor.go` | Existing code | Must be updated if collector name/config changes (per project memory) |

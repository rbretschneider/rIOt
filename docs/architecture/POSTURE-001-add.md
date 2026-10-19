# Architecture Decision Document

**Story ID:** POSTURE-001
**FRD Reference:** docs/requirements/POSTURE-001-frd.md
**Author:** Architect Agent
**Date:** 2026-10-17
**Status:** DRAFT (non-interactive run; pending security review and the operator's sign-off on Section 15)

---

## 0. FRD review outcome

The FRD is complete and internally consistent enough to design against. The
product owner was not available, so every open question was resolved with the
FRD's recommended default (listed below). Architecture-level decisions that only
The operator can make are collected in **Section 15**, each with a recommendation.

### 0.1 Open questions resolved by default

| OQ | Default taken |
|----|---------------|
| OQ-001 | No editable query text. Fixed curated catalog in Go (Section 3, AD-004; catalog in Section 6.6). |
| OQ-002 | "Posture" is a new top-level nav entry placed immediately before "Security". `/security` stays as is. |
| OQ-003 | Keep the agent nginx access-log parser unchanged. netmon is the only source for Posture Threat KPIs. A runtime duplicate note is shown (AD-022). |
| OQ-004 | The structural VPN check ships as the v1 guarantee. The runtime check ships in code but defaults to "not configured" (AD-016). House WAN IP source in v1 is **manual entry only**, labelled "manually set". The mktxp WAN-address metric is not used because its existence is UNVERIFIED. |
| OQ-005 | No individual attacker IPs. Counts plus top 5 ASN organizations only. |
| OQ-006 | Assumed LAN/VPN-only (A-016). The operator must confirm (Section 15). |
| OQ-007 | pprof is disabled unless `RIOT_PPROF_ENABLED=true`, and when enabled it sits behind admin auth (AD-021). |
| OQ-008 | No auto-created rules. Templates plus the Egress-tile prompt. |
| OQ-009 | Flat 7-day mean baseline. |
| OQ-010 | riot-server joins the external `netmon` network, and a scrape job is added. Both are specified as a checklist for the operator in Section 16 and are **not executed**. |
| OQ-011 | Resolved by the architect: fleet-level events use the synthetic `device_id` `external:netmon` (precedent: `probe:<id>`, enabled by migration 000010, which dropped the events FK). `vpn_leak` events are attributed to the configured VPN host device (AD-018). |
| OQ-012 | Section 7.3 and 7.5 defaults accepted as written. All are editable in Settings. |

### 0.2 Technical ambiguities and how this ADD resolves them

These are interpretations, not rejections. No FRD requirement is rejected.

1. **FR-008 / AC-008 "loading state until the 10-second timeout".** The Posture API
   never calls an external source during a request (AD-002). A KPI that has not yet
   been fetched since startup or since a config change is returned with
   `pending: true`, and the UI renders the tile's loading skeleton. When the
   poller's first attempt times out at 10 s, the KPI becomes `unknown` with reason
   "timeout". AC-008 holds as written.
2. **FR-048 floor vs FR-049 insufficient history.** Both can apply at once. This ADD
   applies FR-049 first (insufficient history means `unknown`), then the floor, then
   the ratio (AD-013). This keeps AC-033 literal. As a consequence, Egress is
   `unknown` for the first 24 h after netmon starts collecting WAN data.
3. **FR-066 TTL vs 7-day baseline cost.** Each curated query has a refresh interval.
   It is 60 s for all threat and "current" queries and 10 min for the three
   7-day-baseline queries and the history queries. This is stricter than NFR-004
   ("no more than one execution per query per 60 s"), not looser. Baseline values
   count as fresh for 20 min 15 s (AD-005).
4. **FR-043 / AC-029 Explore prefill for Loki KPIs.** The Explore link prefills the
   KPI's **log-stream form** (the stream selector plus filters, without the
   `count_over_time`/`sum` wrapper), so the operator lands on the matching raw lines.
   This follows BR-001, which makes Grafana the place for raw logs. Prometheus KPIs
   prefill the exact metric query.
5. **FR-085 "legacy `RIOT_API_KEY`".** In code, `RIOT_API_KEY` maps to
   `Config.RegistrationKey` (`internal/server/config.go:62-64`). It gates device
   registration and authenticates no endpoint. AC-052 still asserts that presenting it
   to `/metrics` returns 401.
6. **FR-001 "same login redirect".** The SPA has no URL redirect today. `App.tsx`
   renders `<Login>` for any route when unauthenticated. `/posture` inherits exactly
   that behavior, and the API returns 401 via `middleware.AdminAuth`.
7. **A-011 (seed the VPN-required list from the VPN-egress service notes).** Not implemented as a default.
   Shipping the operator's container names as a product default would be wrong for other
   installs. Instead, Settings offers a **"Suggest from VPN host"** helper. It lists
   containers on the VPN host whose `network_mode` references the gateway, so the operator
   adds the exact names with one click. This replaces an assumption, not a
   requirement.
8. **FR-081 VPN structural gauge.** `riot_posture_vpn_structural_violation` is 1 when
   the structural check is `critical` and 0 when it was evaluated and passed. The
   series is absent when the check is not configured or `unknown`.

---

## 1. Summary

A new read-only **external-signals** component (`internal/server/extsignals`) runs a
fixed, curated set of PromQL/LogQL instant queries against netmon's Prometheus and Loki
from a **background poller**. Results live only in an in-memory cache, with per-query
timeouts, a 1 MiB response cap, a source-health state machine, and fail-soft staleness.
Page views, alert evaluation and `/metrics` read that cache and never fan out to netmon.

A new **posture** domain package (`internal/server/posture`) holds a 60-second **fleet
score cache**. The cache is fed by one projected telemetry query and the unchanged
`scoring.Score`. The package builds the five tiles with pure, clock-injected functions,
and one admin-only endpoint `GET /api/v1/posture` serves them.

External-signal **alert rules** reuse the existing `alert_rules` table and are told
apart by a metric-name registry, so **no migration** is needed. A 60-second worker
evaluates them through the existing `events.Generator` and `notify.Dispatcher`.

rIOt exports its own posture on a hand-rolled Prometheus text endpoint `GET /metrics`,
authenticated by a dedicated SHA-256-hashed bearer token that works on that endpoint
only. `/debug/pprof` becomes opt-in and admin-gated.

---

## 2. Technical Context

### 2.1 What exists (verified in code at `fbe2b9b`)

- **Router** (`internal/server/router.go`): chi v5. Admin routes are grouped under
  `middleware.AdminAuth(s.JWTSecret)` (`riot_session` JWT cookie). Device routes use
  `middleware.DeviceAuth` (`X-rIOt-Key`) plus optional `MTLSDeviceAuth`. The **SPA
  catch-all** `serveFrontend` answers every unmatched path with `index.html` and HTTP
  200, so routes that must return 401/404 have to be **registered explicitly**. This is
  the same lesson as OIDC-001 AD-011.
- **pprof** is mounted with no auth at `router.go:203-205`
  (`r.Mount("/debug/pprof", pprofHandler())`).
- **SetupGuard** (`middleware/setup.go`) blocks only `/api/*` and `/ws*` during
  setup. `/metrics` and `/debug/pprof` fall outside those prefixes.
- **Scoring** (`internal/server/scoring/engine.go`): `Score(*models.FullTelemetryData,
  ...ScoreOptions) *models.SecurityScore`. It is pure apart from
  `EvaluatedAt: time.Now()`. Categories: access_control, patching, network, docker,
  system. Finding IDs used by this story exist verbatim: `fw-active`,
  `no-risky-ports`, `minimal-open-ports`, `tls-certs-valid`, `proxy-config-valid`,
  `security-headers`, `rate-limiting`. `grade()` bands: A ≥ 90, B ≥ 75, C ≥ 60,
  D ≥ 40, F below that. Scores are computed per request (`handlers/security.go:148-161`)
  and never persisted.
- **Telemetry repo** (`internal/server/db/telemetry_repo.go`):
  `GetAllLatestSnapshots` decodes full blobs (heavy). `GetAllLatestSummaries` already
  uses the JSONB-projection pattern this ADD extends.
- **Device model** (`models.Device`) carries `Hostname`, `Status`, `AutoPatch`,
  `LastTelemetry`. `handlers.IsAgentConnected(id)` exists (`handlers.go:534`).
- **Alert rules** (`models.AlertRule`, table `alert_rules`, include/exclude columns
  from migrations 000014/000020). `events.Generator` evaluates per device on ingest by
  **metric-name match** (`findMatchingRuleForContainer`, `evaluateMetric`). Its
  enabled-rules cache has a 5 s TTL and uses singleflight. `onCooldown` keeps an
  in-memory `lastSent` map, and `pruneStaleEntries` drops entries older than 1 h.
  Rules whose metric name no device code path emits are never matched.
- **Events**: the `events.device_id` FK was dropped by migration 000010. The probes
  runner already writes `device_id = "probe:<id>"` (`probes/runner.go:171`).
  `Alerts.tsx` renders an unknown device as `device_id.slice(0, 8)`.
- **Notifications** (`internal/server/notify/dispatcher.go`): `Dispatch(ctx, Alert)`
  fans out to all enabled channels and is used by alert rules. `SendToChannel` is used
  by the server-error worker. ntfy renders `Rule.Name` as the title and
  `Event.Message` as the body. The webhook posts `json.Marshal(models.Alert)`.
- **Periodic-worker precedent**: `server_error_alert_worker.go` (1-minute ticker,
  config JSON in `admin_config`, in-memory cooldown).
- **Settings storage**: `admin_config(key TEXT PK, value TEXT, updated_at)` via
  `AdminRepository.GetConfig/SetConfig/GetConfigMap`.
- **Key hashing precedent**: `internal/server/auth/keyhash.go` `HashAPIKey` =
  SHA-256 hex. It is used for device API keys.
- **Dependencies**: `golang.org/x/sync` (singleflight) is already in `go.mod`. There
  is no Prometheus client library.
- **Frontend**: `App.tsx` nav (Fleet, Dashboard, Alert Rules, Probes, Security,
  Settings), `SettingsLayout.tsx` tabs, `AlertRuleSettings.tsx` (`METRICS`,
  `CONTAINER_METRICS`, `?edit=` deep link), `SecurityScoreModal` (props `score`,
  `hostname`, `onRunCommand`, `canCommand`), `api.getSecurityScore(id)`,
  `api.sendCommand(id, action, params)`. The existing fix action for pending security
  updates is `os_update` with `{mode: "security"}`. Demo mode aliases
  `api/demo-client.ts` and `api/demo-settings.ts`.
- **UI-FOUNDATION-001 is NOT in this worktree.** `web/src/components/ui/` does not
  exist. There is no `ConfirmDialog`, toast system or `<RelativeTime>`. Only the older
  `ConfirmModal` and `utils/format.ts` exist.

### 2.2 netmon (verified from `<netmon-repo>`, read-only)

- Network `netmon` (`name: netmon`). Prometheus v3.0.1 at `prometheus:9090` with
  `--web.enable-lifecycle` and no auth. Loki 3.3.0 at `loki:3100`. Neither is
  published.
- Datasource UIDs are `prometheus` and `loki`. The dashboard `netmon-security` panel
  IDs and queries match the grounding brief (panels 1-9, 24-28). Panel links already
  use numeric `viewPanel=<id>`, which proves the numeric form works on Grafana 11.3.
- Alloy streams use labels `job="nginx"`, `host="<netmon-host>"`, indexed `country_code` and
  `status`. Structured metadata includes `path`, `ip` and `asn_org`, among others. The
  Prometheus counter is `loki_process_custom_nginx_http_requests{country_code,status}`.
- Global scrape interval is 30 s. cAdvisor uses `--docker_only` and
  `--store_container_labels=false`, and its `name` label is the container name.

### 2.3 What is missing

The Posture page and API. A fleet score cache. Any outbound integration with
Prometheus or Loki. Fleet-scoped (non-device) alert evaluation. Prometheus exposition.
Service-token auth. pprof gating. A Settings page for all of the above.

### 2.4 What must not change (NFR-011, FR-078, FR-089)

The agent and its telemetry. The existing generator code paths and their tests. The
`nginx_*` metrics and templates. `GET /api/v1/security/*` and `/devices/{id}/security-score`.
All existing API shapes.

---

## 3. Architecture Decisions

### AD-001: No log ingestion. External data is memory-only, aggregate-only, admin-only

**Decision:** rIOt stores no netmon-derived value in Postgres, on disk or in browser
storage. Loki and Prometheus remain the only stores. Every external reading lives in
the `extsignals` in-memory cache and in in-flight API responses. The cache is
discarded on restart and when the integration is disabled or a source URL changes.

**Rationale:** BR-001, BR-006, NFR-014, and the PART-001 bloat context. The curated
catalog only asks netmon for aggregates (counts, rates, `topk(5)` ASN names), so
attacker IPs never cross the wire to rIOt in the first place. That is a stronger
privacy guarantee than filtering after the fact.

**Alternatives considered:**
- Persist KPI time series in a new table to show trends in rIOt. Rejected: it
  duplicates Prometheus, grows with time (NFR-014), and Grafana already provides history.
- Ingest Alloy's log stream. Rejected by product direction.

**Consequences:** After a restart, the Threat and Egress tiles are `pending` until the
first poll (about 5-15 s). External-signal history in rIOt is limited to the events
created by fired rules.

### AD-002: A background poller is the only upstream caller. All consumers read the cache

**Decision:** `extsignals.Poller` is a server goroutine that wakes every 15 s and runs
the curated queries that are **due**. A query is due when `lastAttempt +
RefreshEvery <= now` and the integration is enabled with that source configured. Each
execution goes through `singleflight.Group` keyed by query ID, so at most one execution
per query is ever in flight (FR-066). Each query ID also has a hard **minimum interval
of 60 s** between executions. Config changes and manual refreshes cannot bypass it
(NFR-004, AC-007).

The Posture API, the external-signal alert evaluator and `/metrics` call only
`extsignals.Service.Snapshot()`, which never performs I/O. A config save "kicks" the
poller through a non-blocking channel send. The kick starts a pass immediately, and
that pass still respects the per-ID minimum interval. The first pass runs 5 s after
startup.

**Rationale:** FR-007, FR-008, FR-066, FR-070, NFR-002, NFR-004. With this design, no
number of browser tabs, scrapes or rules can amplify load on netmon, and a hung netmon
can never delay a rIOt response.

**Alternatives considered:**
- **Read-through cache with singleflight on request** (the first viewer after TTL
  expiry triggers the fetch). Rejected: a request then blocks for up to 10 s on a hung
  source, which breaks FR-008 and NFR-002 for any tile assembled in the same response.
  Alert evaluation would also depend on page views (FR-070).
- **Query only while someone views the page.** Rejected: alerting must run without
  viewers (FR-070).

**Consequences:** When the integration is enabled, netmon sees a constant load of
about 11 queries per minute plus 5 baseline/history queries every 10 minutes, whether
or not anyone is looking. This is negligible for a single-node Prometheus/Loki.
Freshness of displayed values is at most about 75 s (60 s refresh plus the 15 s
wake-up granularity).

### AD-003: A read-only HTTP client with a fixed endpoint allowlist, hard limits and classified errors

**Decision:** `extsignals.Client` is the only type that talks to external sources. It
exposes only three operations:
- `Query(ctx, source, promqlOrLogql, at time.Time)`, which issues
  `GET <base>/api/v1/query` (Prometheus) or `GET <base>/loki/api/v1/query` (Loki).
- `Probe(ctx, source)`, which issues `GET <base>/api/v1/status/buildinfo`
  (Prometheus), `GET <base>/ready` (Loki), or `GET <base>/v1/publicip/ip`
  (VPN runtime).
- `PublicIP(ctx)`, which issues `GET <base>/v1/publicip/ip`.

Paths are compile-time constants appended to the validated base URL. Only the GET
method exists. There is no `query_range` call. Every instant query is enough because
range aggregation happens inside PromQL/LogQL (`[6h]`, subqueries).

**Limits:**

| Limit | Value |
|-------|-------|
| Per-request timeout (context) | **10 s** (`QueryTimeout`, FR-067). Prometheus also gets the `timeout=10s` parameter so it aborts server-side. |
| Response body cap | **1 MiB** of decoded bytes (NFR-005). Read through `io.LimitReader(body, cap+1)`. If more than the cap is read, the response is an **invalid response (oversize)**. The cap applies after Go's transparent gzip decoding, which blocks decompression bombs. |
| Series cap per vector result | 250. Anything larger is an invalid response. |
| Label value length | Truncated to 128 runes on parse. |
| Transport | Dedicated `http.Transport`: `Proxy: nil` (ignores `HTTP_PROXY` so destinations are exactly the configured hosts), dial timeout 5 s, TLS handshake timeout 5 s, `ResponseHeaderTimeout` 10 s, `MaxIdleConnsPerHost` 4, `IdleConnTimeout` 90 s. |
| Redirects | **Not followed.** `CheckRedirect` returns `http.ErrUseLastResponse`, and a 3xx counts as an HTTP error. |
| Concurrency | At most 4 in-flight requests per source (semaphore). |

**Error classes** (`extsignals.FailureClass`; used by FR-013 and FR-064):

| Class | Detected by |
|-------|-------------|
| `dns_failure` | `errors.As(err, *net.DNSError)` |
| `connection_refused` | `errors.Is(err, syscall.ECONNREFUSED)` |
| `unreachable` | Other dial/network errors (FR-013 "unreachable") |
| `timeout` | `context.DeadlineExceeded` or `net.Error.Timeout()` |
| `http_error` | Non-2xx status (keep the status code) |
| `invalid_response` | JSON decode error, `status != "success"`, unexpected `resultType`, non-numeric sample, oversize, or too many series |

The UI shows `dns_failure` and `connection_refused` as "unreachable (DNS failure)" and
"unreachable (connection refused)", so both FR-013 and FR-064 wording hold.

**Rationale:** FR-062, FR-063, FR-067, NFR-005, NFR-007. Making it structurally
impossible to issue a non-allowlisted request is easier to test and review than
checking at call sites.

**Alternatives considered:**
- Official `prometheus/client_golang/api` and Loki `logcli` clients. Rejected: large
  dependency trees for two GET calls, and they expose admin APIs that this design wants
  to be unreachable.
- Following redirects. Rejected: a redirect could send rIOt to a host other than the
  configured one (SSRF).

**Consequences:** Raw error strings, which embed the full request URL including the
query, are **never** logged or returned. Only `{source, query_id, class, http_status}`
leave the client (FR-095, AC-057).

### AD-004: A curated query catalog in Go. Only the window and the WAN interface are ever substituted

**Decision:** `extsignals/catalog.go` defines `var Catalog = []QueryDef{...}`. Each
entry has `ID`, `Source`, a `Render(p Params) string` built from a fixed template,
`RefreshEvery`, `EmptyMeans` (`zero` | `unknown`), and a `Parse` kind (`scalar` |
`asnTop` | `byName`). The exact text is in **Section 6.6**. The only variable inputs
are:
- `Window`, from the enum `{1h, 6h, 24h}` (a Go type with three constants; never a
  free string).
- `WANInterface`, validated against `^[A-Za-z0-9._-]{1,32}$` and inserted only inside
  an **exact-match** `name="..."` matcher. It cannot contain `"` or `\`, and `.` is
  literal in an `=` matcher.

**Container names never enter a query.** Per-container queries return all containers
(bounded by `topk`), and the join with rIOt telemetry happens in Go. The VPN-required
list is used only against rIOt telemetry. There is no operator-editable query text
anywhere in the UI or API. `PUT /settings/external-signals` decodes with
`DisallowUnknownFields`, so a `query` field is rejected (AC-041).

**Rationale:** OQ-001 default, FR-062, AC-041. Keeping container names out of queries
removes the only input that would need regex escaping (Docker names allow `.`).

**Alternatives considered:**
- Per-container `name=~"a|b|c"` filtering. Rejected: it needs regex escaping and grows
  the query with the list.
- `text/template` over arbitrary config. Rejected: more surface for no gain.

**Consequences:** A netmon panel or metric rename means a code change in rIOt. This is
accepted and documented in the README.

### AD-005: Freshness, staleness and the source-health model

**Decision:**
- **Per-query cache entry:** `{lastAttemptAt, lastResult (value, series), lastSuccessAt,
  lastSuccessResult, lastFailure (class, httpStatus, at)}`.
- **Fresh:** the last attempt succeeded and `now - lastSuccessAt <= FreshFor`.
  `FreshFor = 2 × RefreshEvery + 15 s`, which is 135 s for 60-second queries and
  20 min 15 s for 10-minute queries. The poller wakes every 15 s, so a baseline entry is
  re-checked within 15 s of becoming stale.
- **Pending:** no attempt has completed since startup or since the last relevant
  config change. Status is `unknown`, `pending: true`, reason "Awaiting first query".
- **Failed:** the last attempt failed. Status is `unknown`, and the reason names the
  source and class (FR-013). If `now - lastSuccessAt <= 15 min`, the response also
  carries `stale: {value, fetched_at}` (FR-014). Otherwise no value is returned.
- **Expired:** the last attempt succeeded, but `now - lastSuccessAt > FreshFor` (for
  example, the poller is wedged). Status is `unknown`, reason "Data is stale". A stale
  value may be returned under the same 15-minute rule.
- **Source health** (per source: `prometheus`, `loki`, `vpn_runtime`) follows FRD 7.6.
  States are `not_configured`, `probing`, `healthy` and `failing`.
  - A source moves to **failing** when **every** query to it in the latest pass
    failed.
  - It moves back to **healthy** when any query succeeds.
  - `failing_since` is set on the first move to failing and cleared on recovery.
  - Disabling the integration or clearing the URL moves the source to
    `not_configured` and discards its cache entries (FR-096).
  - `external_source_unavailable` (minutes) is `floor((now - failing_since)/1 min)`,
    and 0 when healthy.
- **Logging (NFR-010, AC-060):** the poller keeps, per source, the set of failing
  query IDs.
  - When the set goes from empty to non-empty, or changes, it logs **one** WARN line
    `external signals: source degraded` with `source`, `failing_queries` (IDs), and
    `class` (most common class).
  - When the set becomes empty, it logs one INFO line `external signals: source
    recovered`.
  - Unchanged failures produce no line, or at most one DEBUG line per source per pass,
    which is suppressed at the default INFO level.
  - No line ever contains a URL, a response body, an IP address or `err.Error()`.

**Rationale:** FR-013, FR-014, FR-015, FR-065, FR-068, NFR-010. Per-source tracking
satisfies FR-015 by construction, because Prometheus failures only touch
Prometheus-sourced query IDs.

**Alternatives considered:** Per-query WARN logging. Rejected: it exceeds the NFR-010
budget.

**Consequences:** All time math uses an injected `clock func() time.Time` (NFR-013).

### AD-006: Config in `admin_config` with seed-only env defaults, a separate secrets key, and an optional destination allowlist

**Decision:**
- `admin_config` key **`external_signals_config`** holds the JSON in Section 5.2.1. It
  is read at startup into an `atomic.Pointer[Config]` and swapped on save (FR-060:
  effective within one pass, with no restart).
- `admin_config` key **`external_signals_secrets`** holds
  `{"vpn_runtime_api_key": "..."}`. The GET endpoint never returns it and returns only
  `has_vpn_runtime_api_key`. Keeping it in a separate key means no serializer of the
  config struct can leak it.
- **Env vars seed defaults only.** `RIOT_NETMON_PROMETHEUS_URL`,
  `RIOT_NETMON_LOKI_URL` and `RIOT_NETMON_GRAFANA_URL` pre-fill the three URLs **only
  while the `external_signals_config` key does not exist**. After the first save, the
  database value wins. `enabled` always defaults to `false` (FR-059).
- **`RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS`** is optional, comma-separated `host[:port]`.
  When set, a Prometheus, Loki or VPN-runtime URL whose `host:port` is not listed is
  rejected at save time (field error) **and** at request time (the client refuses).
  When unset, any host is allowed. The final policy belongs to the security review
  (NFR-007). The hook exists so the review can mandate it without a redesign.
- **Validation (FR-061, 7.2, AC-040)** happens in `extsignals.Validate(cfg, deviceLookup)`
  and returns `map[field]message`. The rules are in Section 6.4.

**Rationale:** If env overrode the database (the TLS pattern in `loadDBConfig`), the
Settings UI would silently not apply edits, which breaks FR-060. Seed-only env still
allows a compose-driven first deploy.

**Alternatives considered:**
- Env-only config. Rejected: FR-059/060 require Settings.
- A new table. Rejected: `admin_config` is the established store, and nothing here
  grows.

**Consequences:** No migration. The config JSON is versioned with
`"schema_version": 1` for future evolution.

### AD-007: A fleet score cache built from one projected query, a 60-second worker, and singleflight

**Decision:** `posture.FleetCache` holds the latest `FleetState`:

```
FleetState{
  ComputedAt
  Devices []DeviceState{
    Device (models.Device)
    SnapshotAt *time.Time
    Data *models.FullTelemetryData (projected)
    Score *models.SecurityScore
  }
  Err
}
```

- **Refresh:** a worker refreshes every 60 s. On the first read, `Get(ctx)` computes
  synchronously through `singleflight` when the state is missing or older than 60 s.
- **Data source:** one call each to `DeviceRepo.List` and a **new**
  `TelemetryRepo.GetAllLatestScoringInputs(ctx)`. That method is a JSONB projection of
  only `security`, `updates`, `web_servers`, `docker`, `services`, `os` and `network`,
  plus `device_id` and `timestamp` (Section 5.3).
- **Scoring:** `scoring.Score(data, ScoreOptions{AutoPatch: device.AutoPatch})` is
  called once per device per refresh. The scorer is injectable for AC-061 counting.
- **Memory trimming:** after scoring, `ContainerInfo.Env` and `.Labels` are nil'ed in
  the cached copy. Env can hold secrets, and the fields are unused downstream.
- **Failure handling:** on a refresh failure, the last good state is kept and `Err` is
  set. Native tiles then add the note "Telemetry refresh failed; showing data from
  <RelativeTime>". If there is no state at all, native tiles are `unknown` with reason
  "rIOt telemetry store unavailable", and the API still returns 200.
- **Consumers:** the Posture API, `/metrics`, the VPN structural check and the
  shared-namespace map all read this cache. Existing `/security/*` and
  `/devices/{id}/security-score` handlers are **unchanged** (NFR-011).

**Rationale:** NFR-003 requires at most one recompute per device per 60 s regardless of
scrapes and views. Scoring on ingest could exceed that when agents push faster than
every 60 s, and it would add work to the ingest hot path, which already carries
DB-contention history. The projection avoids decoding processes, logs, USB and hardware
blobs.

**Alternatives considered:**
- Score on telemetry ingest and keep a per-device map. Rejected: it has hot-path cost,
  can recompute more often than every 60 s, and its first read after startup has no
  data for silent devices.
- Persist scores in a table. Rejected: NFR-014 spirit, and recomputing is cheap.
- Reuse `GetAllLatestSnapshots`. Rejected: it decodes multi-MB blobs.

**Consequences:** Native tiles lag telemetry by at most 60 s. An operator who just
patched a device sees the change on the next refresh. The `Posture` response exposes
`evaluated_at` per tile so this lag is visible.

### AD-008: A pure posture domain package with an FR-009 aggregator and an injected clock

**Decision:** `internal/server/posture` contains only pure functions for status logic:
- `Aggregate([]SubSignal) (Status, reason)` implements FR-009 and FRD 7.7 exactly.
  Sub-signals with `Configured=false` are excluded. If any configured sub-signal is
  `unknown`, the result is `unknown` unless a `critical` or `warn` sub-signal exists.
  The reason names the dominant contributor, or the unknown sub-signals (FR-011).
- One tile builder per tile (`BuildExposure`, `BuildHardening`, `BuildPatching`,
  `BuildThreat`, `BuildEgress`). Each takes `(FleetState, extsignals.Snapshot,
  extsignals.Config, Alerting, now time.Time)` and returns `models.PostureTile`.
- `capItems(items, 5)` sorts worst-first (critical, warn, unknown, ok) with a stable
  tile-specific secondary key, then sets `HiddenCount` (FR-005).
- `clampReason(s)` limits the reason to 160 runes, ending in "…" when truncated
  (FR-004b).
- `Service.Build(ctx)` is the composition root. It reads the caches and alert-rule
  coverage, calls the builders in the fixed order, and computes `Summary`.

**Rationale:** NFR-013 asks for testable status derivation with no wall-clock reads.
Building the tiles once lets `/metrics` and the API share one definition.

**Alternatives considered:** Computing tiles in the frontend from raw data. Rejected:
it duplicates logic between `/metrics` (FR-081) and the UI, and it ships more data to
the browser.

**Consequences:** `posture` imports `extsignals`, `scoring`, `models` and `db`
interfaces. `handlers` and `metricsexport` import `posture`. There are no cycles
(`events` imports none of these; see AD-018).

### AD-009: One aggregated, admin-only Posture endpoint

**Decision:** `GET /api/v1/posture` is registered inside the existing `adminAuth`
group and returns all five tiles (contract in Section 6.1). It takes no query
parameters, and any that are sent are ignored (FR-019, AC-015). It sets
`Cache-Control: no-store`. It performs no external I/O, only cache reads plus one
cached rules lookup. It never returns 5xx because of external sources (NFR-006).

**Rationale:** Under AD-002 every part of the response is served from memory, so
splitting per tile buys no independence and costs four extra round trips. Per-tile
independence (FR-008) comes from per-tile `pending` and status, not from separate HTTP
calls.

**Alternatives considered:**
- Five endpoints. Rejected: the same data source, more requests, and more surface.
- WebSocket push. Rejected: the 60 s refresh is adequate, and the WS hub currently
  carries device data.

**Consequences:** The UI polls every 60 s while the tab is visible, and every 5 s while
any tile is `pending` (AD-024).

### AD-010: Grafana deep links built server-side from a fixed mapping

**Decision:** `posture/deeplinks.go` builds absolute URLs from the validated
`grafana_base_url` (scheme://host[:port][/path], trimmed of a trailing `/`) and
compile-time constants. The full table is in Section 6.7.
- **Dashboard link:**
  `{base}/d/netmon-security?orgId=1&viewPanel={panel}&from=now-{W}&to=now`.
- **Explore link:**
  `{base}/explore?orgId=1&schemaVersion=1&panes={url-encoded JSON}`. The JSON is built
  with `encoding/json`, and its `datasource` is the constant `loki` or `prometheus`.
- All parameters are added with `url.Values`.
- When `grafana_base_url` is empty, no `grafana` action is emitted. Each affected tile
  instead gets the link action "Set Grafana base URL in Settings to enable drill-down"
  → `/settings/external-signals#grafana` (FR-020).
- **Rendering:** the frontend renders `kind: "grafana"` as
  `<a href target="_blank" rel="noopener noreferrer" referrerPolicy="no-referrer">`
  (FR-018).

**Rationale:** FR-018, FR-019, FR-043, FR-045. Server-side construction means the
browser never assembles URLs from request state.

**Alternatives considered:** Building links in the frontend. Rejected: it duplicates
the mapping, and the notifications (FR-073) need the same links server-side.

**Consequences:** If netmon renumbers a panel, links land on the wrong panel. This is
listed as a dependency in the README.

### AD-011: Exposure, Hardening and Patching are rules over `scoring.Score` output

**Decision:**
- **Exposure:** for each device with a snapshot, read the finding results by ID from
  the cached score.
  - **Critical set:** `fw-active`, `no-risky-ports`, `tls-certs-valid`.
  - **Warn set:** `minimal-open-ports`, `proxy-config-valid`, `security-headers`,
    `rate-limiting` (FR-022).
  - **Facts:** devices with at least one open port, and total open ports (from
    `SecurityInfo.OpenPorts`).
  - **Item per device:** status, open ports (first 10, ascending, plus
    `more_ports = N - 10`), and nginx posture.
    - `security_headers_count` is the maximum `len(SecurityConfig.SecurityHeaders)`
      across servers.
    - `has_rate_limit` is true when any server has a non-empty
      `SecurityConfig.RateLimiting`.
    - `soonest_cert_days` is the minimum `DaysLeft` across non-CA certs, or null.
  - **Links:** each item links to `/devices/{id}`, plus `/learn/{findingId}` for each
    failed finding (FR-025).
  - **Unknown:** `unknown` when no device has `Security != nil || WebServers != nil`
    (FR-026).
- **Hardening:** status comes from the **worst** `OverallScore`: ≥ 75 is `ok`,
  60–74 is `warn`, below 60 is `critical` (FR-028).
  - **Items:** the 5 lowest devices, each with score, grade and lowest-scoring
    category. The lowest category is the lowest `Score/MaxScore` ratio, with ties
    broken by category order.
  - **Facts:** per-category minimum `Score/MaxScore` as a percentage for the five
    categories, or `"no data"` when no device reports that category (FR-030).
  - **Link:** "Open Security" → `/security` (FR-031).
  - **Item click:** the UI calls the existing `api.getSecurityScore(deviceId)` and
    opens `SecurityScoreModal` (FR-029).
  - **Unknown:** when no device has at least one category, the tile is `unknown`
    (FR-032).
- **Patching:** `auto = device.AutoPatch || Updates.UnattendedUpgrades`.
  - A device is critical when `PendingSecurityCount > 0 && !auto`.
  - A device is warn when `PendingSecurityCount > 0`, or when `RebootRequired` is
    true (FR-034).
  - **Facts:** total pending security updates, and devices with at least one.
  - **Item:** device, count, `auto_patch`, `reboot_required`, and a link to the device.
    When `PendingSecurityCount > 0` and the device is `online` and
    `IsAgentConnected(id)`, the item also carries action
    `{kind: "command", command: "os_update", params: {mode: "security"}}`. This is the
    action `SecurityScoreModal` already uses (FR-016, FR-035).
  - **Unknown:** when no device reports `Updates`, the tile is `unknown` (FR-036).
- **Freshness (FR-037, FR-038):** devices with `Status == offline` are included, and
  their items carry `stale: true` and `telemetry_at`. Devices with no snapshot are
  excluded and counted in `devices_without_data`.
- **Command allowlist:** the server's posture action allowlist is exactly
  `{os_update{mode: security}}`. A unit test fails if any builder emits another command.

**Rationale:** FR-021 forbids a divergent definition, and reading finding IDs is the
only way to stay in lockstep with `scoring`.

**Alternatives considered:** Re-deriving checks from raw telemetry. Rejected by FR-021.

**Consequences:** Whether the agent permits `allow_patching` is not visible to the
server. If a command is refused by the agent, the toast reports the failure. This is
accepted, and the item text says "requires `commands.allow_patching` on the agent".

### AD-012: Threat activity tile composition

**Decision:** The Threat tile reads catalog queries T1–T8 (Section 6.6).
- **Contributing KPIs:** T1 (5xx), T2 (401) and T3 (answered probes). Each one's status
  is `critical` when `value >= critical`, `warn` when `value >= warn`, otherwise `ok`.
  A null threshold never triggers (FR-040).
- **Informational KPIs:** T4–T8 have `contributes_to_status: false` and never affect
  the status (FR-041).
- **Rounding:** Prometheus `increase()` results (T1) are rounded with `math.Round`
  before display and comparison, because `increase` extrapolates fractional values.
  Loki counts are already integers.
- **Top ASNs:** T7 returns at most 5 series. The parser reads **only** the `asn_org`
  label and discards every other label (FR-042, FR-092).
- **Host label (FR-044):** "Source: netmon, host <hostname of netmon_host_device_id>",
  or "host not set", which links to Settings.
- **Duplicate note (FR-091):** when the netmon host's latest snapshot has any
  `ProxyServer.AccessMetrics != nil`, the tile adds a note naming the host and stating
  that the agent parser duplicates Alloy there. The note does not affect status.
- **No agent data (FR-090):** the tile never reads `AccessMetrics` values.
- **Integration off (FR-012):** when the integration is disabled or both URLs are
  empty, the tile is `unknown`, reason "netmon integration not configured", with a
  Settings action.
- **Partial failure:** a KPI whose source is failing is `unknown` (FR-013), so the tile
  becomes `unknown` unless another KPI is `warn` or `critical` (FR-009).

**Rationale:** This follows FRD 7.3 one to one.

**Consequences:** If Loki is down while Prometheus is healthy, 5xx still evaluates
(AC-012).

### AD-013: WAN baseline math and precedence

**Decision:**
- **current** = Q-E1 = mean tx bit rate over the last 1 h, from `rate(...[1h])`.
- **baseline** = Q-E2 = `avg_over_time` of 5-minute rates over the 7-day subquery
  window ending 1 h ago. This is the mean over the history that exists, so it is
  robust to counter resets and to less than 7 days of data.
- **history_hours** = Q-E3 step count × 5 / 60.

Evaluation order:
1. If current or baseline is failed or pending, the result is `unknown`, with the
   reason naming Prometheus and the class.
2. If `history_hours < 24` (Q-E3 empty means 0), the result is `unknown`, reason
   "Insufficient baseline history" (FR-049).
3. If `current < floor_bps` (default 1 000 000), the result is `ok` (FR-048).
4. Otherwise `ratio = current / baseline`. When `baseline <= 0`, the ratio is +Inf and
   the reason is "no baseline traffic". The result is `critical` when
   `ratio >= critical`, `warn` when `ratio >= warn`, otherwise `ok`.

The response carries `current_bps`, `baseline_bps`, `ratio` and `history_hours`. The UI
formats them as bit/s (FR-047).

**Rationale:** A naive `rate(x[7d] offset 1h)` divides by the full 7 days even when
history is shorter, which understates the baseline on a fresh netmon. The subquery
average divides only by the steps that exist.

**Alternatives considered:**
- `increase(x[7d] offset 1h)` divided by the span computed in Go. Workable, but it
  needs an extra first-sample-timestamp query.
- Seasonal baseline. Rejected (OQ-009).

**Consequences:** The subquery covers 2016 steps over one series every 10 minutes,
which is trivial for Prometheus.

### AD-014: Per-container transmit anomaly and shared-namespace attribution

**Decision:** Q-E4, Q-E5 and Q-E6 return per-`name` current tx, baseline tx and history
steps. The netmon host's latest rIOt snapshot supplies `name → NetworkMode` and
`name/ID` maps.
- **Shared namespace (FR-051):** a container whose `NetworkMode` starts with
  `container:` (Docker writes `service:x` from compose as `container:<full id>`) is a
  **member**. The **owner** is the container in the same snapshot whose `ID` equals the
  referenced ID or starts with it (12+ chars), or whose `Name` equals the reference.
- **Owner item:** shows the owner's series as namespace traffic, `shared_with: [members]`,
  and the caveat "shared network namespace: traffic is not attributable per container".
- **Members:** members get no tx figures and are listed only under the owner.
- **Eligibility:** a container is evaluated only if it is not a member, has a current
  value, has `history_steps × 5 / 60 >= 24`, and has `baseline > 0`. Ineligible
  containers are listed as informational items ("insufficient history" or "no
  baseline"). They are **excluded from aggregation** rather than `unknown`, so a new
  container does not make the tile `unknown`.
- **Status (FR-050):** `warn` when `ratio >= warn (3.0) && current >= floor (1 Mbit/s)`.
  `critical` only if the critical threshold is non-null and the same floor holds.
  The default critical threshold is null, so the default status is never critical.
- **Sub-signal status:** the worst eligible container. It is `unknown` only when Q-E4
  itself is failed or pending. When Q-E5 or Q-E6 fail, the sub-signal is `unknown` with
  reason "baseline query failed".
- **No netmon host:** when no netmon host device is set, shared namespaces cannot be
  detected. Every container is treated as non-shared, and the note "Set the netmon host
  device to group shared network namespaces" is added.
- **Alert metric (FR-068):** `ext_container_tx_ratio` is the maximum ratio over
  eligible containers with `current >= floor`. It is 0 if none qualify, and `unknown`
  if the sub-signal is `unknown`.

**Rationale:** Applying the floor to the alert metric too prevents "50× of nothing"
alerts.

**Consequences:** cAdvisor reports identical counters for every container in a shared
namespace. Dropping member series avoids double counting.

### AD-015: VPN structural check (rIOt-native, works with the integration disabled)

**Decision:** Read the VPN host device's latest cached snapshot `Docker.Containers`.
1. Resolve the **gateway** (default `gluetun`) by exact `Name` (leading `/` trimmed).
2. For each required name:
   - **absent** → "not reporting", sub-signal `warn` (FR-054).
   - **present and `State != "running"`** → "not running", no status effect.
   - **running** → compliant if `NetworkMode` is `container:<gatewayID>`,
     `container:<gateway ID prefix of 12+ chars>`, `container:<gatewayName>` or
     `service:<gatewayName>`. Otherwise `critical` with the observed mode (FR-053).
3. **Gateway absent from telemetry:** the sub-signal is `warn`, reason "VPN gateway
   container <name> not reporting". Containers referencing an unresolvable ID show
   "cannot verify".

Other rules:
- **Not configured:** when the list is empty or no VPN host is set (FR-055).
- **Offline VPN host:** items carry `stale` and the age of the last telemetry.
- **Item links:** `/devices/{vpnHostId}/containers/{shortId}`, or `/devices/{vpnHostId}`
  when the container is absent (FR-058).
- **Independence:** the check never consults `extsignals` (BR-002).

**Rationale:** FR-052 to FR-055 and BR-004. Matching by gateway ID is required because
Docker resolves `service:gluetun` to `container:<id>`.

**Consequences:** The Docker collector must be enabled on the VPN host. If it is not,
every container is "not reporting", which gives `warn` (honest, BR-003).

### AD-016: VPN runtime check is optional. gluetun control server versus a manually entered house IP

**Decision:** Settings fields are `vpn_runtime.gluetun_control_url` (validated like
other URLs), an optional write-only API key sent as `X-API-Key`, and
`vpn_runtime.house_wan_ip` (an IP literal, labelled "manually set").
- **Configured:** only when both the URL and the house IP are set. Otherwise the
  sub-signal is "not configured", and the tile notes that only the structural check is
  active (FR-057).
- **Polling:** the poller calls `GET {url}/v1/publicip/ip` every 60 s as source
  `vpn_runtime`, parses only `public_ip`, and validates it with `net.ParseIP`.
- **Comparison:** equal to the house IP means `critical`, different means `ok`, and a
  failure means `unknown` (FR-056).
- **Privacy of these IPs:** neither IP is logged, exported on `/metrics`, or put into a
  notification. The vpn_leak message says only "VPN egress IP equals house WAN IP".
  Both appear in the admin-only Posture response.

**Rationale:** OQ-004 default. A mktxp WAN-address metric is UNVERIFIED, and an
internet IP-echo call from riot-server would add an unwanted egress path.

**Alternatives considered:**
- Agent probe inside the namespace. Rejected: it needs an agent change (NFR-011).
- Derive the house IP from mktxp. Deferred until a metric is verified.

**Consequences:** A dynamic ISP IP makes a manually entered house IP go stale. This is
documented in the UI help text. Reachability of the gluetun control server needs a
deployment change that the operator must approve (Section 15, D-2). Whether gluetun's control
server requires auth is UNVERIFIED, hence the optional API key.

### AD-017: External-signal rules live in `alert_rules`, told apart by a metric registry, with no migration

**Decision:** `events/external_metrics.go` defines the registry:

| Metric | Label | Netmon-derived | Unit |
|--------|-------|---------------|------|
| `ext_nginx_5xx` | 5xx responses (window) | yes | count |
| `ext_nginx_401` | 401 responses (window) | yes | count |
| `ext_answered_probes` | Answered probe paths (window) | yes | count |
| `ext_wan_tx_ratio` | WAN transmit ÷ 7-day baseline | yes | ratio |
| `ext_container_tx_ratio` | Max per-container transmit ratio | yes | ratio |
| `vpn_leak` | VPN leak (structural or runtime) | **no** | 0/1 |
| `external_source_unavailable` | Minutes a configured source has been failing | yes | minutes |

Exported helpers are `IsExternalMetric(m)` and `IsNetmonDerived(m)`.
`CreateAlertRule` and `UpdateAlertRule` (`handlers/settings.go`) reject an external
metric whose `include_devices`, `exclude_devices`, `include_containers`,
`exclude_containers`, `target_name` or `target_state` is non-empty after trimming. The
response is `400 {"error":"validation failed","fields":{"include_devices":"device and container scope is not supported for external-signal metrics"}}`
(FR-069, AC-044). The operator must be one of the existing six.

**Rationale:** The device evaluator matches rules strictly by metric name, and none of
its code paths emits these names. External rules are therefore invisible to it, so
FR-078 and AC-049 hold with zero change to evaluation code. The validation makes the
"fleet-scoped" property explicit.

**Alternatives considered:**
- **Migration 000023 adding `alert_rules.scope`.** Rejected: the metric name already
  determines scope, so a column would be a second, divergent source of truth needing a
  consistency check. It also adds a migration to a story that otherwise has none
  (NFR-014 spirit).
- **A separate `external_alert_rules` table.** Rejected: FR-069 requires the existing
  rule fields, editor and notification routing.

**Consequences:** The list endpoint is unchanged. The UI marks netmon-derived rules
"inactive: integration disabled" by combining the rule metric with
`GET /settings/external-signals` (FR-077). There is no API change to rules.

### AD-018: A 60-second external evaluator that reuses Generator infrastructure, with its own cooldown map and fleet-level attribution

**Decision:**
- **Worker:** new file `internal/server/external_signal_alert_worker.go`, modelled on
  `server_error_alert_worker.go`. It ticks every 60 s, with the first tick at +45 s,
  after the poller's first pass.
- **Inputs:** each tick builds `events.ExternalSignalValues`. That is a plain struct of
  `map[metric]ExternalValue{Known bool, Value float64, Window string, Detail string,
  DeepLink string, Source string, Subject string}` built from `extsignals.Snapshot` and
  the posture Egress and VPN computations. The worker then calls
  `EventGen.EvaluateExternalSignals(ctx, values, integrationEnabled)`. The `events`
  package does **not** import `extsignals` or `posture`.
- **Evaluator** (`events/external.go`, methods on `*Generator`):
  - It uses `listEnabledRules` (the existing 5 s cache).
  - It skips rules whose metric is not external.
  - It skips netmon-derived metrics when `integrationEnabled == false` (FR-077).
  - It skips values that are not `Known` (FR-071).
  - It compares with the existing `compareValue`.
  - **Cooldown key:** `rule:<id>`, or `rule:<id>:<source>` for
    `external_source_unavailable`, so one source going down does not mute the other.
    The keys live in a **separate** map `extLastFired`. That map is pruned only when an
    entry is older than its own rule cooldown, because the generic 1-hour
    `pruneStaleEntries` would break cooldowns longer than 1 h (FR-072).
  - **On fire:** it calls the existing `createEventAndNotify(ctx, e, rule, hostname,
    value)`, which means `EventRepo.Create`, a hub broadcast, and `Dispatcher.Dispatch`
    to all enabled channels. That is the same routing as every alert rule (FR-072,
    A-014).
- **Event attribution (OQ-011):**
  - `vpn_leak` uses `DeviceID = vpnHostDeviceID`, `ContainerName = first violating
    container`, and `Type = EventVPNLeak`.
  - `external_source_unavailable` uses `DeviceID = "external:netmon"` and
    `Type = EventExternalSourceUnavailable`.
  - KPI metrics use `DeviceID = "external:netmon"` and `Type = EventExternalSignal`.
  - `Alert.Hostname` is `"netmon"`, or the VPN host's hostname for `vpn_leak`.
- **Message (FR-073):** one line plus an optional link. Format:
  `"<KPI label>: <value><unit> over <window> (rule \"<name>\": <op> <threshold>, severity <sev>)"`,
  followed by `" Grafana: <deeplink>"` when a base URL is configured. The rule name and
  severity also travel in `Alert.Rule` for webhook consumers. Messages never contain
  attacker IPs or geo, because no such data exists in the inputs (FR-095).
- **Coverage helper:** exported `(*Generator).HasEnabledRuleForMetric(ctx, metric) bool`
  supports FR-076.

**Rationale:** This reuses the battle-tested cache, event storage, WS broadcast and
dispatcher. Keeping the evaluator in `events` gives access to unexported helpers
without exporting them.

**Alternatives considered:**
- A standalone worker that calls `SendToChannel` like the server-error worker.
  Rejected: FR-072 requires the alert-rule routing (fan-out), and FR-074 requires
  events.
- Reusing `lastSent`. Rejected because of the prune issue above.

**Consequences:** Cooldowns are in memory (A-013), so a restart can repeat one
notification. The `Alerts.tsx` label shows "netmon (external)" for the
`external:` prefix. The new event types are added to `eventCategory.ts`
`SECURITY_TYPES`.

### AD-019: `/metrics` uses a hand-rolled Prometheus text exposition, with no new dependency

**Decision:** `internal/server/metricsexport/exposition.go` implements a minimal writer
for text format 0.0.4. It writes `# HELP` and `# TYPE` once per family, gauges only,
sorted label sets, and label-value escaping of `\`, `"` and newline per the spec.
Values are formatted with `strconv.FormatFloat(v, 'g', -1, 64)`. The response has
`Content-Type: text/plain; version=0.0.4; charset=utf-8` and `Cache-Control: no-store`.
Metric families are listed in Section 6.5.

**Cardinality bounds:**
- At most **1000 devices** are exported. Beyond that, the list is truncated
  deterministically by device ID and a WARN is logged once per process.
- Categories are a fixed 5. Severities are a fixed 3 (`critical`, `warning`, `info`).
- Tiles are 5. Sources are at most 3.
- At 100 devices this is about 3,000 series.

Devices without a snapshot are omitted. Netmon KPI values, IPs, ASNs, geo, finding
descriptions and remediation text are never emitted (FR-082). Label values come only
from device ID, hostname, and fixed enums.

**Rationale:** About 12 gauge families, all computed from caches at scrape time. A
writer of roughly 150 lines, plus a grammar test, is smaller and easier to audit than
`prometheus/client_golang`. That library would pull in `client_model`, `common`,
`procfs`, `protobuf` and `xxhash` updates, and its registry and collector lifecycle are
not needed here (NFR-012).

**Alternatives considered:**
- `github.com/prometheus/client_golang`. Rejected (above). If a future story needs
  histograms or default Go runtime metrics, reconsider then.
- OpenMetrics format. Rejected: text 0.0.4 is the universal baseline that Prometheus
  v3.0.1 parses.

**Consequences:** AC-050 "parses with promtool" is verified in two ways:
- A Go test with a strict line-grammar validator (no new test dependency).
- A QA step running `promtool check metrics` from the `prom/prometheus:v3.0.1` image.

Hostnames are device-reported strings. They are escaped, never interpreted.

### AD-020: The scrape credential is a bearer token, SHA-256 at rest, compared in constant time, and valid only on `/metrics`

**Decision:**
- **Token format:** `riot_scrape_` followed by base64url (no padding) of 32
  `crypto/rand` bytes, about 55 characters. The public `id` is the first 8 characters
  after the prefix. It is shown in Settings and logs and does not reveal the secret.
- **Storage:** `admin_config` key **`metrics_scrape_token`** =
  `{"id","sha256","created_at","rotated_at"}`. `sha256` is the hex SHA-256 of the full
  token, using the existing `auth.HashAPIKey`. Revocation deletes the value, stored as
  an empty string (FR-086, FR-088).
- **In-memory copy:** `metricsexport.TokenStore` caches the record in an
  `atomic.Pointer`, loaded at startup and swapped synchronously on
  generate/rotate/revoke, so the old secret is rejected on the very next request
  (FR-086, AC-053). There is one process, so there is no cross-node invalidation.
- **Auth (`middleware.ScrapeAuth(store)`):** mounted **only** on `GET /metrics`.
  1. It accepts only `Authorization: Bearer <token>`. Query parameters and cookies are
     ignored.
  2. If no record exists, the header is missing, or the prefix is not `riot_scrape_`,
     it returns 401.
  3. Otherwise it compares `subtle.ConstantTimeCompare(sha256hex(presented),
     stored.sha256)`.
  4. On failure it returns 401 with an **empty body** and
     `WWW-Authenticate: Bearer realm="riot-metrics"`.
- **Rate limit:** a dedicated `middleware.NewRateLimiter(60, 60)` per client IP on
  `/metrics` (60/min). Normal scraping is 1-2/min.
- **Failure logging (FR-087):** at most one WARN per 60 s,
  `metrics scrape auth failures`, with `count` and `remote` (the client IP from the
  existing RealIP resolver). Never the presented value.
- **Lifecycle logs:** INFO `metrics scrape token generated|rotated|revoked id=<id>`.
- **Isolation (FR-085):**
  - `AdminAuth` reads only the `riot_session` cookie.
  - `DeviceAuth` looks keys up in `api_keys`, where scrape tokens never exist.
  - `ScrapeAuth` never consults device keys, mTLS identities or the registration key.
  - Therefore the token works nowhere else, and other credentials do not work on
    `/metrics`. AC-052 tests both directions.

**Rationale:** NFR-008. For a 256-bit random secret, SHA-256 is sufficient against
offline recovery. A slow KDF (bcrypt) would add about 50 ms of CPU per scrape for no
security gain. This mirrors the existing device-key hashing.

**Alternatives considered:**
- Reuse a device API key. Rejected by FR-085.
- Basic auth. Rejected: it is just a bearer token with extra encoding.
- mTLS for Prometheus. Rejected: it needs certificate issuance and rotation in netmon
  for little gain on a LAN bridge.
- HMAC-SHA256 with a server pepper. Offered to the security review as an optional
  hardening step. It does not change the API.

**Consequences:** Rotation breaks scraping until the operator updates the token file in netmon.
The Settings UI says so next to the rotate button.

### AD-021: pprof is disabled by default, admin-gated when enabled, and answers an explicit 404 when off

> **Superseded (2026-10-18, v2.65.0).** HARDEN-002 shipped a different fix for FR-097.
> The main router never serves pprof and always answers 404 for `/debug/pprof` and
> `/debug/pprof/*`. Profiling is available only on an opt-in loopback listener
> (`RIOT_PPROF_ADDR`). AC-058 is satisfied by that shipped behavior. Do **not**
> implement `RIOT_PPROF_ENABLED` or the admin-gated variant below. Phase 0 is dropped.
> The text is kept for traceability.

**Decision:** New config `Config.PprofEnabled` from `RIOT_PPROF_ENABLED` (`true`/`1`),
default false.
- **Enabled:** `r.Route("/debug/pprof", func(r) { r.Use(adminAuth); r.Mount("/", pprofHandler()) })`.
- **Disabled:** `r.Handle("/debug/pprof", http.NotFoundHandler())` and
  `r.Handle("/debug/pprof/*", http.NotFoundHandler())`, registered before the SPA
  catch-all. Without these, the catch-all would return `index.html` with HTTP 200.
- **Startup log:** INFO `pprof enabled (admin auth required)` when enabled.

This ships in **Phase 0**, before riot-server joins `netmon` (FR-097, AC-058).

**Rationale:** OQ-007 default. Defense in depth: off unless asked, and authenticated
even when on.

**Alternatives considered:**
- Admin auth only. It is weaker, because the endpoint stays live and a stolen session
  can pull heap dumps.
- Bind pprof to a separate localhost listener. It is the cleanest isolation, but inside
  Docker "localhost" is the container itself, so operators would need `docker exec`.
  It is also a larger change than this story warrants.

**Consequences:** **Behavior change for the operator.** Unauthenticated `curl` of pprof from the
host stops working. Profiling then needs `RIOT_PPROF_ENABLED=true` plus a session
cookie, for example `curl -b "riot_session=<cookie>" https://…/debug/pprof/heap`. This
is flagged in Section 15 (D-1) and in the CHANGELOG.

### AD-022: The nginx access-log single source of truth (OQ-003)

**Decision:** No change to the agent parser, `AccessMetrics`, `nginx_*` metrics or
templates (FR-089). Posture Threat KPIs come only from netmon (FR-090). The
FR-091 duplicate note is shown at runtime (AD-012). The README gains a short "Which
nginx numbers to trust" section:
- netmon/Alloy is the source of truth for **The netmon host** edge traffic on the Posture page.
- The agent parser remains the source for `nginx_*` device rules on any nginx host.
- If the duplicate note appears for a host, the procedure is to review rules scoped to
  that host that reference `nginx_*` (BR-007) before removing `webservers.access_log`
  from that host's agent YAML.

**Rationale:** OQ-003 default. The parser serves non-the netmon host hosts at negligible cost.

**Consequences:** The netmon host may carry both pipelines until the operator's ops step (Section 16,
step 11).

### AD-023: Network path and destinations on the netmon host (deploy prerequisite, not executed)

**Decision:** riot-server joins the external docker network `netmon`, and reaches
`http://prometheus:9090` and `http://loki:3100` by service name. Prometheus and Loki
stay unpublished. The compose diff and the scrape job are in Section 16. No code
depends on Docker networking. URLs are plain config.

**Rationale:** OQ-010 default. This is the least exposure: nothing new is published to
the host or LAN.

**Alternatives considered:**
- Publish Prometheus/Loki on `<host-lan-ip>`. Rejected: it exposes unauthenticated
  Prometheus lifecycle endpoints to the LAN.
- A reverse proxy with auth in front of them. Deferred: it is out of scope (FRD 9).

**Consequences:**
- riot-server gains reachability to every netmon container, including Prometheus
  `--web.enable-lifecycle` (`POST /-/quit`, `/-/reload`). rIOt's client cannot issue
  POST or non-allowlisted paths (AD-003), but a compromised riot-server could.
  Section 15, D-3 recommends dropping `--web.enable-lifecycle`.
- Every netmon container can reach riot-server, hence AD-021 first.

### AD-024: Frontend uses UI-FOUNDATION-001 primitives, has an explicit merge order, no new npm dependencies, and demo stubs

**Decision:**
- **Page:** a new page `pages/Posture.tsx` and components under
  `components/posture/`. All are built only from UI-FOUNDATION-001 primitives:
  - `Card` for tiles.
  - `Badge`, with the shared enum→label map extended by
    `ok→"OK"`, `warn→"Warning"`, `critical→"Critical"`, `unknown→"Unknown"`.
  - `Button`, `<QueryState>`, `ConfirmDialog`, toasts, and `<RelativeTime>` (FR-098).
- **Status display:** always the text label plus color (FR-099).
- **Scrolling:** scroll regions use `scrollbar-thin` (FR-100).
- **Layout:** `grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4`. Tailwind `md` is
  768 px (FR-101).
- **Data fetching:** react-query with `queryKey ['posture']`, `refetchInterval` 60 s
  (5 s while any tile is `pending`), and `refetchIntervalInBackground: false`, so
  polling stops when the tab is hidden (FR-007). Manual refresh calls `refetch()`.
- **Storage:** no persister, no localStorage, no sessionStorage (FR-093).
- **Settings:** sub-page `pages/settings/ExternalSignalsSettings.tsx` at
  `/settings/external-signals` (tab "External signals"). It contains sections with
  anchors `#netmon`, `#grafana`, `#thresholds`, `#vpn`, `#vpn-runtime` and
  `#metrics-token`, plus a "Test connection" button with inline results and a toast.
- **Alert Rules editor:**
  - It gains an "External signals" metric group.
  - It hides device and container scope fields for external metrics.
  - It shows "inactive: integration disabled" chips.
  - It supports a `?template=<id>` deep link that opens a new-rule editor pre-filled
    from that template (FR-076).
- **Demo mode:** `demo-client.ts` `getPosture()` returns a fixture with all five tiles
  in mixed statuses, using made-up ASN names and no real IPs. `demo-settings.ts` stubs
  the external-signals config and status, test connection, and token operations (fake
  token `riot_scrape_DEMO…`).
- **Dependencies:** no new npm dependencies (NFR-012).

**Merge order (hard dependency):** UI-FOUNDATION-001 **must be merged to `main` before
the Phase 1 frontend PR is opened**. Phase 0 (pprof) and the backend parts of Phases 1-4
do not depend on it. If UI-FOUNDATION-001 slips, the Phase 1 backend may merge alone,
with the Posture route hidden from navigation until the frontend lands. Primitives must
**not** be re-implemented locally in this story.

**Rationale:** FR-098 and repository policy.

**Consequences:** The `Badge` map file belongs to UI-FOUNDATION-001. Its exact path is
to be confirmed at merge time (expected `web/src/components/ui/Badge.tsx` or a sibling
label-map module).

### AD-025: Phased delivery in five tagged releases

**Decision:** Deliver in the order of FRD §2's delivery note, preceded by a security
gate. Section 14 lists each phase's scope, its ACs, and its independent DoD.

**Rationale:** The full scope touches about 60 files. Smaller PRs are reviewable, and
the security gate (pprof) must be live before the network join.

**Consequences:** Each phase is a separate PR and tag (repo rule: tag every code push).

---

## 4. Component Changes

### 4.1 Server: new files

| Action | File Path | Purpose |
|--------|-----------|---------|
| CREATE | `internal/server/extsignals/config.go` | `Config`, `Thresholds`, `Window` enum, `VPNRuntime`, `Defaults()`, `LoadConfig(ctx, AdminRepository, env)`, `SaveConfig`, `LoadSecrets/SaveSecrets`. Keys `external_signals_config`, `external_signals_secrets`. |
| CREATE | `internal/server/extsignals/validate.go` | `Validate(cfg, deviceExists func(id) bool, allowedHosts []string) map[string]string` (Section 6.4) |
| CREATE | `internal/server/extsignals/catalog.go` | `QueryDef`, `Catalog`, `Params`, exact query templates (Section 6.6) |
| CREATE | `internal/server/extsignals/client.go` | Read-only client: endpoint allowlist, limits, `FailureClass`, `classify(err)` (AD-003) |
| CREATE | `internal/server/extsignals/parse.go` | Prometheus/Loki vector/scalar parsing, `asn_org`-only label whitelist, series cap, 128-rune truncation |
| CREATE | `internal/server/extsignals/cache.go` | Per-query entries, freshness/staleness/pending (AD-005), source health, `Snapshot` (immutable copy), `Discard()` |
| CREATE | `internal/server/extsignals/poller.go` | 15 s wake-up, due-query selection, per-ID 60 s minimum, singleflight, per-source semaphore, kick channel, transition logging |
| CREATE | `internal/server/extsignals/service.go` | `Service{cfg atomic.Pointer, cache, poller, client, clock}`: `Start(ctx)`, `ApplyConfig`, `Snapshot()`, `Config()`, `Status()`, `TestConnection(ctx, cfg)` |
| CREATE | `internal/server/extsignals/vpnruntime.go` | gluetun `/v1/publicip/ip` call and parse |
| CREATE | `internal/server/extsignals/testconn.go` | Per-source probe returning `{source, ok, class, http_status, latency_ms}` |
| CREATE | `internal/server/extsignals/*_test.go` + `testdata/*.json` | Recorded response fixtures: success, empty, `status:"error"`, matrix-instead-of-vector, non-numeric, 2 MiB oversize, 251 series, Loki top-ASN with extra labels (NFR-013) |
| CREATE | `internal/server/posture/fleet.go` | `FleetCache`, `FleetState`, 60 s worker, singleflight, injectable scorer, Env/Labels trimming (AD-007) |
| CREATE | `internal/server/posture/aggregate.go` | `Aggregate`, `capItems`, `clampReason`, status ordering (AD-008) |
| CREATE | `internal/server/posture/exposure.go` | `BuildExposure` |
| CREATE | `internal/server/posture/hardening.go` | `BuildHardening` |
| CREATE | `internal/server/posture/patching.go` | `BuildPatching` (command allowlist) |
| CREATE | `internal/server/posture/threat.go` | `BuildThreat` (AD-012) |
| CREATE | `internal/server/posture/egress.go` | `BuildEgress`: WAN (AD-013), containers (AD-014), VPN structural (AD-015), runtime (AD-016) |
| CREATE | `internal/server/posture/deeplinks.go` | Grafana dashboard/Explore URL builder (AD-010, Section 6.7) |
| CREATE | `internal/server/posture/service.go` | `Service.Build(ctx) (*models.PostureResponse, error)`; `ExternalAlertValues(ctx) events.ExternalSignalValues` |
| CREATE | `internal/server/posture/*_test.go` | Table tests for every tile rule and AC (names reference AC IDs) |
| CREATE | `internal/server/metricsexport/exposition.go` | Text-format writer and escaping (AD-019) |
| CREATE | `internal/server/metricsexport/handler.go` | `Handler(posture *posture.Service, ext *extsignals.Service) http.HandlerFunc` |
| CREATE | `internal/server/metricsexport/token_store.go` | `TokenStore`: `Load`, `Generate`, `Revoke`, `Verify(presented) bool` (AD-020) |
| CREATE | `internal/server/metricsexport/*_test.go` | Grammar validator, privacy assertions, token lifecycle |
| CREATE | `internal/server/auth/scrapetoken.go` | `NewScrapeToken() (token, id string, err error)`, `ScrapeTokenPrefix` |
| CREATE | `internal/server/middleware/scrape_auth.go` | `ScrapeAuth(verifier)`: bearer parse, constant time, empty-body 401, throttled failure log |
| CREATE | `internal/server/middleware/scrape_auth_test.go` | AC-052 / AC-053 cases |
| CREATE | `internal/server/handlers/posture.go` | `GetPosture` |
| CREATE | `internal/server/handlers/external_signals.go` | `GetExternalSignals`, `PutExternalSignals`, `TestExternalSignals` |
| CREATE | `internal/server/handlers/metrics_token.go` | `GetMetricsToken`, `CreateMetricsToken` (generate or rotate), `DeleteMetricsToken` |
| CREATE | `internal/server/handlers/posture_test.go`, `external_signals_test.go`, `metrics_token_test.go` | Handler tests |
| CREATE | `internal/server/events/external_metrics.go` | Metric registry, `IsExternalMetric`, `IsNetmonDerived`, `ExternalSignalValues`, `ExternalValue` |
| CREATE | `internal/server/events/external.go` | `EvaluateExternalSignals`, `HasEnabledRuleForMetric`, ext cooldown map and prune |
| CREATE | `internal/server/events/external_test.go` | AC-045 to AC-048 |
| CREATE | `internal/server/external_signal_alert_worker.go` | 60 s ticker (first run +45 s) that bridges posture/extsignals to `EventGen.EvaluateExternalSignals` |
| CREATE | `internal/server/router_posture_test.go` | Router-level auth tests: posture 401, `/metrics` credential matrix, pprof 404/401 |
| CREATE | `internal/models/posture.go` | `PostureResponse`, `PostureTile`, `PostureSubSignal`, `PostureItem`, `PostureAction`, `PostureLink`, `PostureFact`, `PostureStale`, `PostureStatus` constants (Section 5.2.3) |

### 4.2 Server: modified files

| Action | File Path | Change |
|--------|-----------|--------|
| MODIFY | `internal/server/config.go` | Add `PprofEnabled bool` (`RIOT_PPROF_ENABLED`), `NetmonPrometheusURL`, `NetmonLokiURL`, `NetmonGrafanaURL` (seed-only), `ExternalSignalsAllowedHosts []string` |
| MODIFY | `internal/server/server.go` | Add `ExtSignals *extsignals.Service`, `Posture *posture.Service`, `FleetCache *posture.FleetCache`, `ScrapeTokens *metricsexport.TokenStore` fields. Construct them after repos and EventGen. `go ExtSignals.Start(ctx)`, `go FleetCache.Run(ctx)`, `go newExternalSignalAlertWorker(...).run(ctx)`. Startup never blocks on netmon (NFR-006). |
| MODIFY | `internal/server/router.go` | AD-021 pprof gating. `r.With(metricsLimiter.Middleware(), middleware.ScrapeAuth(s.ScrapeTokens)).Get("/metrics", metricsexport.Handler(...))` outside the admin group. In the admin group: `GET /api/v1/posture`, `GET`/`PUT /api/v1/settings/external-signals`, `POST /api/v1/settings/external-signals/test`, `GET`/`POST`/`DELETE /api/v1/settings/metrics-token`. Pass new deps into `HandlerDeps`. |
| MODIFY | `internal/server/handlers/handlers.go` | `HandlerDeps` and `Handlers` gain `Posture *posture.Service`, `ExtSignals *extsignals.Service`, `ScrapeTokens *metricsexport.TokenStore` |
| MODIFY | `internal/server/handlers/settings.go` | `CreateAlertRule` and `UpdateAlertRule` call `validateExternalRule(rule)` before persisting (AD-017). Existing behavior is unchanged for other metrics. |
| MODIFY | `internal/server/events/generator.go` | Add `extMu sync.Mutex` and `extLastFired map[string]time.Time` to `Generator` and initialise them in `NewGenerator`. **No change** to any existing method body (AC-049). |
| MODIFY | `internal/server/events/templates.go` | Append 5 templates (category `external`): `vpn_leak_detected`, `exposed_probe_answered`, `burst_401`, `wan_egress_spike`, `netmon_unavailable` (Section 6.8) |
| MODIFY | `internal/models/events.go` | Add `EventExternalSignal = "external_signal"`, `EventVPNLeak = "vpn_leak"`, `EventExternalSourceUnavailable = "external_source_unavailable"` |
| MODIFY | `internal/server/db/interfaces.go` | `TelemetryRepository` gains `GetAllLatestScoringInputs(ctx) ([]models.TelemetrySnapshot, error)` |
| MODIFY | `internal/server/db/telemetry_repo.go` | Implement the projected query (Section 5.3) |
| MODIFY | `internal/testutil/mocks.go` | `MockTelemetryRepo.GetAllLatestScoringInputs` (returns stored snapshots with non-scoring fields nil'ed). Existing mocks otherwise unchanged. |

### 4.3 Frontend

| Action | File Path | Purpose |
|--------|-----------|---------|
| CREATE | `web/src/types/posture.ts` | TS mirror of Section 5.2.3 / 6.1 |
| CREATE | `web/src/pages/Posture.tsx` | Page: summary line, 5-tile grid, refresh control, react-query polling |
| CREATE | `web/src/components/posture/PostureTile.tsx` | `Card` with Badge, reason, `RelativeTime`, source label, facts, sub-signals, items, actions, notes, pending skeleton via `QueryState` |
| CREATE | `web/src/components/posture/PostureSubSignalList.tsx` | KPI and sub-signal rows: value, stale label with age, not-configured row with Settings link, deep link |
| CREATE | `web/src/components/posture/PostureItemList.tsx` | ≤5 items, "+N more", per-item Badge, stale marker, tile-specific detail (ports, nginx, score, patch, container, VPN) |
| CREATE | `web/src/components/posture/PostureActionButton.tsx` | `link` → router `Link`; `grafana` → external anchor (AD-010); `command` → `ConfirmDialog` → `api.sendCommand` → toast; `rule_template` → navigate `/alert-rules?template=<id>` |
| CREATE | `web/src/components/posture/PostureSummary.tsx` | "1 critical, 1 warn, 2 ok, 1 unknown" (no blended score) |
| CREATE | `web/src/utils/posture.ts` | `formatBitRate`, `summaryText`, `statusOrder`, `isPending` |
| CREATE | `web/src/utils/posture.test.ts` | Unit tests |
| CREATE | `web/src/pages/Posture.test.tsx` | AC-002/003/004/005/006/008/014/015/016/021/029/047/055/059 UI assertions |
| CREATE | `web/src/pages/settings/ExternalSignalsSettings.tsx` | Settings form, field errors, Test connection (inline plus toast), per-source health with `RelativeTime`, VPN list editor plus "Suggest from VPN host", metrics token card |
| CREATE | `web/src/components/settings/MetricsTokenCard.tsx` | Generate/rotate/revoke with `ConfirmDialog`; one-time secret display with copy; rotation warning |
| CREATE | `web/src/pages/settings/ExternalSignalsSettings.test.tsx` | AC-040/042/053 UI assertions |
| MODIFY | `web/src/App.tsx` | Import `Posture`; `<Route path="/posture">`; `NavLink to="/posture"` immediately before Security in **both** desktop and mobile navs |
| MODIFY | `web/src/pages/SettingsLayout.tsx` | Add the tab `{ to: '/settings/external-signals', label: 'External signals' }` |
| MODIFY | `web/src/api/client.ts` | `getPosture: () => fetchJSON<PostureResponse>(`${BASE}/posture`)` |
| MODIFY | `web/src/api/settings.ts` | `getExternalSignals`, `saveExternalSignals` (surfaces `fields` on 400), `testExternalSignals`, `getMetricsToken`, `createMetricsToken`, `revokeMetricsToken` |
| MODIFY | `web/src/api/demo-client.ts` | `getPosture` stub |
| MODIFY | `web/src/api/demo-settings.ts` | Stubs for the six settings functions |
| MODIFY | `web/src/api/demo-data.ts` | `getPostureFixture()` (no real IPs, made-up ASN orgs) |
| MODIFY | `web/src/pages/settings/AlertRuleSettings.tsx` | `EXTERNAL_METRICS` group in `METRICS`; `METRIC_DEFAULTS` hints; hide scope fields for external metrics; "inactive: integration disabled" chip; `?template=` prefill |
| MODIFY | `web/src/pages/Alerts.tsx` | Render `device_id` with prefix `external:` as "netmon (external)" |
| MODIFY | `web/src/utils/eventCategory.ts` | Add `external_signal`, `vpn_leak`, `external_source_unavailable` to `SECURITY_TYPES` |
| MODIFY | `web/src/types/models.ts` | Add the new event type literals if `Event.type` is a union (otherwise no change) |
| MODIFY | UI-FOUNDATION-001 Badge label map (path confirmed at merge) | Add `ok`, `warn`, `critical`, `unknown` → "OK", "Warning", "Critical", "Unknown" |

### 4.4 Configuration and documentation

| Action | File Path | Purpose |
|--------|-----------|---------|
| MODIFY | `docker-compose.prod.yml` | Commented, optional `RIOT_PPROF_ENABLED`, `RIOT_NETMON_*_URL`, `RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS` with one-line explanations |
| MODIFY | `.env.example` | The same variables, documented |
| MODIFY | `README.md` | Posture page; integration settings; external-signal metrics and templates; `/metrics` families, types, help and labels (FR-083); scrape credential and Prometheus job example; privacy rules (FR-092 to FR-096); "Which nginx numbers to trust" (AD-022); pprof change |
| MODIFY | `CHANGELOG.md` | Per-phase entries; **breaking:** pprof now opt-in plus admin-only |
| NO CHANGE | `cmd/riot-server/migrations/` | No migration (Section 5.1) |
| NO CHANGE | `internal/agent/**` | Agents unchanged (NFR-011). No new collector, so there is no collector-whitelist action. |

---

## 5. Data Model Changes

### 5.1 Database: no schema change

No migration is created. The latest migration stays `000022`. Rationale: AD-006,
AD-017, AD-020. Every new persisted item is a bounded `admin_config` row:

| `admin_config.key` | Value | Bounded? |
|--------------------|-------|----------|
| `external_signals_config` | JSON (5.2.1) | One row, about 2 KB |
| `external_signals_secrets` | `{"vpn_runtime_api_key": "<string>"}` | One row |
| `metrics_scrape_token` | `{"id","sha256","created_at","rotated_at"}`, or `""` when revoked | One row |

External-signal **events** go into the existing `events` table. They are
rate-limited by rule cooldowns and purged at 90 days by the existing `runRetention`.
They never contain attacker data (FR-093, NFR-014).

### 5.2 New Go types

#### 5.2.1 `extsignals.Config` (persisted JSON shape)

```json
{
  "schema_version": 1,
  "enabled": false,
  "prometheus_url": "http://prometheus:9090",
  "loki_url": "http://loki:3100",
  "grafana_base_url": "<grafana-url>",
  "threat_window": "6h",
  "wan_interface": "ether1",
  "netmon_host_device_id": "",
  "vpn_host_device_id": "",
  "vpn_gateway_container": "gluetun",
  "vpn_required_containers": [],
  "vpn_runtime": { "gluetun_control_url": "", "house_wan_ip": "" },
  "thresholds": {
    "nginx_5xx":          { "warn": 1,   "critical": 25 },
    "nginx_401":          { "warn": 50,  "critical": 300 },
    "answered_probes":    { "warn": 1,   "critical": null },
    "wan_tx_ratio":       { "warn": 2.0, "critical": 4.0,  "floor_bps": 1000000 },
    "container_tx_ratio": { "warn": 3.0, "critical": null, "floor_bps": 1000000 }
  }
}
```

The URL values shown are what the operator would enter on the netmon host. **Product defaults** are
empty strings, or the seed env values (AD-006). `null` means "none" (never triggers).
Go types: `Threshold{Warn *float64; Critical *float64; FloorBps *float64}`, and
`Window string` restricted to the constants `"1h"`, `"6h"`, `"24h"`.

#### 5.2.2 `extsignals` in-memory types (never persisted)

```
QueryResult  { Value *float64; Series []NamedValue }          // NamedValue{Name string; Value float64}
Failure      { Class FailureClass; HTTPStatus int; At time.Time }
Reading      { QueryID; Source; Pending bool; Fresh bool; Result *QueryResult; FetchedAt *time.Time;
               Failure *Failure; Stale *StaleResult }           // StaleResult{Result; FetchedAt}
SourceHealth { Source; State ("not_configured"|"probing"|"healthy"|"failing");
               LastSuccessAt *time.Time; LastFailure *Failure; FailingSince *time.Time }
Snapshot     { TakenAt; Enabled bool; Readings map[QueryID]Reading; Health map[Source]SourceHealth;
               VPNPublicIP *Reading }
```

#### 5.2.3 `models` Posture API types (`internal/models/posture.go`)

```
PostureStatus = "ok" | "warn" | "critical" | "unknown"

PostureResponse { EvaluatedAt time.Time; Summary PostureSummary; Tiles []PostureTile;
                  Alerting PostureAlerting; Integration PostureIntegration }
PostureSummary  { Critical, Warn, OK, Unknown int }             // json: critical, warn, ok, unknown
PostureAlerting { VPNLeakRuleEnabled bool }                      // json: vpn_leak_rule_enabled
PostureIntegration { Enabled, GrafanaConfigured bool; NetmonHostLabel string }

PostureTile {
  ID string            // exposure|hardening|patching|threat|egress
  Title string
  Status PostureStatus
  Reason string        // ≤160 runes
  Sources []string     // "riot" and/or "netmon" (and "vpn_runtime" for Egress when configured)
  EvaluatedAt time.Time
  Pending bool
  Facts []PostureFact                  // headline numbers
  SubSignals []PostureSubSignal        // KPIs / sub-signals (Threat, Egress)
  Items []PostureItem                  // ≤5
  HiddenCount int
  NotConfigured []PostureNotConfigured
  Actions []PostureAction              // ≥1 always
  Notes []string
  DevicesWithoutData int
}
PostureFact { Label string; Value string; Status *PostureStatus }
PostureSubSignal {
  ID, Label string; Status PostureStatus; Configured bool; ContributesToStatus bool;
  Source string            // riot|prometheus|loki|vpn_runtime
  Value *float64; DisplayValue string; Unit string   // unit: count|per_min|bps|ratio|ip
  Reason string; Pending bool; FetchedAt *time.Time; Stale *PostureStale;
  Entries []PostureEntry   // top ASNs: {label, count}
  Links []PostureLink
  Extra map[string]float64 // e.g. current_bps, baseline_bps, ratio, history_hours
}
PostureStale { Value *float64; DisplayValue string; FetchedAt time.Time }
PostureNotConfigured { ID, Label, SettingsPath string }
PostureItem {
  Key, Label string; Status PostureStatus; Detail string; Href string; Links []PostureLink;
  TelemetryAt *time.Time; Stale bool; Action *PostureAction;
  // optional, tile-specific (omitempty):
  OpenPorts []int; MorePorts int; Nginx *PostureNginx;          // exposure
  Score *int; Grade string; LowestCategory string;              // hardening
  PendingSecurity *int; AutoPatch *bool; RebootRequired *bool;  // patching
  TxBps, BaselineBps, Ratio *float64; SharedWith []string; Caveat string; // egress container
  NetworkMode, ContainerState string                            // egress vpn
}
PostureNginx { SecurityHeadersCount int; HasRateLimit bool; SoonestCertDays *int }
PostureAction { Kind string /* link|grafana|command|rule_template */; Label string; Href string;
                DeviceID string; Command string; Params map[string]any; TemplateID string; Confirm string }
PostureLink { Label, Href string; External bool }
```

### 5.3 New repository query

`TelemetryRepo.GetAllLatestScoringInputs`:

```sql
SELECT device_id, timestamp,
       jsonb_build_object(
         'security',    data->'security',
         'updates',     data->'updates',
         'web_servers', data->'web_servers',
         'docker',      data->'docker',
         'services',    data->'services',
         'os',          data->'os',
         'network',     data->'network') AS data
FROM (
  SELECT DISTINCT ON (device_id) device_id, timestamp, data
  FROM telemetry_snapshots
  ORDER BY device_id, timestamp DESC
) latest
```

The result decodes into `models.TelemetrySnapshot{DeviceID, Timestamp, Data}`. JSON
`null` members decode to nil pointers. It uses the existing index
`idx_telemetry_device_time`. It runs once per 60 s, plus at most one coalesced cold
read.

---

## 6. API / Interface Contract

The common error shape follows the existing convention: `{"error": "<message>"}`.
Validation errors add `"fields": {"<json_field>": "<message>"}`. All endpoints below
except `/metrics` require `riot_session` (`AdminAuth`). Without it they return
`401 {"error":"unauthorized"}`.

### 6.1 `GET /api/v1/posture`

- **Request:** no body, no query parameters (any parameters are ignored).
- **200** `application/json`, `Cache-Control: no-store`:

```json
{
  "evaluated_at": "2026-10-17T12:00:00Z",
  "summary": { "critical": 1, "warn": 1, "ok": 2, "unknown": 1 },
  "integration": { "enabled": true, "grafana_configured": true, "netmon_host_label": "<netmon-host>" },
  "alerting": { "vpn_leak_rule_enabled": false },
  "tiles": [
    {
      "id": "exposure", "title": "Exposure", "status": "critical",
      "reason": "<netmon-host>: firewall is not active (fw-active).",
      "sources": ["riot"], "evaluated_at": "2026-10-17T11:59:30Z", "pending": false,
      "facts": [
        { "label": "Devices with open ports", "value": "4" },
        { "label": "Open ports (total)", "value": "37" }
      ],
      "sub_signals": [],
      "items": [
        { "key": "dev-1", "label": "<netmon-host>", "status": "critical", "detail": "Firewall not active",
          "href": "/devices/dev-1",
          "links": [ { "label": "fw-active", "href": "/learn/fw-active", "external": false } ],
          "telemetry_at": "2026-10-17T11:59:10Z", "stale": false,
          "open_ports": [22, 80, 443, 7331, 8080, 8443, 9000, 9100, 9443], "more_ports": 4,
          "nginx": { "security_headers_count": 2, "has_rate_limit": false, "soonest_cert_days": 12 } }
      ],
      "hidden_count": 0, "not_configured": [],
      "actions": [ { "kind": "link", "label": "Open Security", "href": "/security" } ],
      "notes": [], "devices_without_data": 1
    },
    {
      "id": "threat", "title": "Threat activity", "status": "warn",
      "reason": "401 responses: 120 in the last 6h (warn at 50).",
      "sources": ["netmon"], "evaluated_at": "2026-10-17T11:59:45Z", "pending": false,
      "facts": [ { "label": "Source", "value": "netmon, host the netmon host" } ],
      "sub_signals": [
        { "id": "nginx_401", "label": "401 responses", "status": "warn", "configured": true,
          "contributes_to_status": true, "source": "loki", "value": 120, "display_value": "120",
          "unit": "count", "reason": "", "pending": false, "fetched_at": "2026-10-17T11:59:44Z",
          "links": [ { "label": "Open in Grafana",
            "href": "<grafana-url>/d/netmon-security?orgId=1&viewPanel=4&from=now-6h&to=now",
            "external": true } ] },
        { "id": "top_asns", "label": "Top ASNs (blocked)", "status": "ok", "configured": true,
          "contributes_to_status": false, "source": "loki",
          "entries": [ { "label": "Example Hosting Ltd", "count": 812 } ] }
      ],
      "items": [], "hidden_count": 0, "not_configured": [],
      "actions": [ { "kind": "grafana", "label": "Open Security Overview",
        "href": "<grafana-url>/d/netmon-security?orgId=1&from=now-6h&to=now" } ],
      "notes": ["<netmon-host> also reports agent nginx access metrics; the agent parser duplicates Alloy on this host."],
      "devices_without_data": 0
    }
  ]
}
```

Tiles are always five, in the order `exposure, hardening, patching, threat, egress`.
Item `status` is always present. `actions` always has at least one entry. The response
**never** contains an IP address other than the two VPN-runtime IPs in the Egress tile
(the admin's own house IP and the VPN exit IP, `unit: "ip"`), and never a geo field,
user agent, path or referer.

- **401** `{"error":"unauthorized"}`.
- **No 5xx** due to external sources. A failure to read the fleet cache with no
  previous state is represented inside the tiles as `unknown`, and the response is
  still 200.

### 6.2 `GET /api/v1/settings/external-signals`

**200** returns the 5.2.1 config, plus:

```json
{
  "config": { "...": "5.2.1 shape" },
  "has_vpn_runtime_api_key": false,
  "allowed_hosts_enforced": false,
  "status": {
    "prometheus":  { "state": "healthy", "last_success_at": "…", "last_error": null, "failing_since": null },
    "loki":        { "state": "failing", "last_success_at": "…",
                     "last_error": { "class": "http_error", "http_status": 502, "at": "…" },
                     "failing_since": "…" },
    "vpn_runtime": { "state": "not_configured", "last_success_at": null, "last_error": null, "failing_since": null }
  }
}
```

### 6.3 `PUT /api/v1/settings/external-signals`

- **Request body:**

```json
{ "config": { "...5.2.1": "..." }, "vpn_runtime_api_key": "optional; omit = unchanged, \"\" = clear" }
```

- The decoder uses `DisallowUnknownFields`.
- **200** returns the same shape as 6.2, after `ApplyConfig`. The poller is kicked, and
  the cache is discarded when `enabled` became false or a source URL changed.
- **400** `{"error":"validation failed","fields":{"prometheus_url":"must use http or https","wan_interface":"only letters, digits, '.', '_' and '-' (max 32)"}}`.
  Nothing is persisted when any field fails (AC-040).
- **400** `{"error":"invalid request body"}` for malformed JSON or unknown fields.

### 6.4 Validation rules (`extsignals.Validate`)

| Field | Rule | Message key |
|-------|------|-------------|
| `prometheus_url`, `loki_url`, `grafana_base_url`, `vpn_runtime.gluetun_control_url` | Empty allowed. Otherwise `url.Parse` OK; scheme `http`/`https`; `User == nil`; host non-empty; no query or fragment; length ≤ 2048. Trailing `/` trimmed on save. When the allowlist is set, `host:port` must be in it (the Grafana URL is exempt because rIOt never calls it). | "must use http or https" / "must not contain credentials" / "must not contain a query or fragment" / "host not in RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS" |
| `enabled` | If true, at least one of `prometheus_url` and `loki_url` must be non-empty | "set a Prometheus or Loki URL to enable" |
| `threat_window` | One of `1h`, `6h`, `24h` | "must be 1h, 6h or 24h" |
| `wan_interface` | `^[A-Za-z0-9._-]{1,32}$` | "only letters, digits, '.', '_' and '-' (max 32)" |
| `vpn_gateway_container`, each `vpn_required_containers[i]` | `^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$` (Docker name charset) | "invalid container name" |
| `vpn_required_containers` | 0 to 50 entries, no duplicates (case-sensitive) | "at most 50" / "duplicate: <name>" |
| `netmon_host_device_id`, `vpn_host_device_id` | Empty, or `DeviceRepo.GetByID` finds the device | "unknown device" |
| `vpn_runtime.house_wan_ip` | Empty, or `net.ParseIP != nil` | "must be an IP address" |
| `thresholds.*.warn`, `.critical`, `.floor_bps` | null, or a finite number ≥ 0 | "must be a number ≥ 0" |
| `thresholds.*` | If warn and critical are both non-null, `warn <= critical` | "warn must not exceed critical" |

### 6.5 `GET /metrics`

- **Auth:** `Authorization: Bearer riot_scrape_…` only (AD-020).
- **200** `text/plain; version=0.0.4; charset=utf-8`. All families are gauges:

| Metric | Labels | Help |
|--------|--------|------|
| `riot_device_security_score` | `device_id`, `hostname` | Overall security score 0–100 |
| `riot_device_security_category_score` | `device_id`, `hostname`, `category` | Points earned in category |
| `riot_device_security_category_max_score` | `device_id`, `hostname`, `category` | Max points in category |
| `riot_device_security_failed_findings` | `device_id`, `hostname`, `category`, `severity` | Failed findings by category and severity (all three severities emitted, zeros included, for each present category) |
| `riot_device_pending_security_updates` | `device_id`, `hostname` | Pending security updates |
| `riot_device_pending_updates` | `device_id`, `hostname` | Pending updates |
| `riot_device_reboot_required` | `device_id`, `hostname` | 1 if reboot required |
| `riot_device_telemetry_age_seconds` | `device_id`, `hostname` | Age of latest telemetry snapshot |
| `riot_posture_tile_status` | `tile` | 0 ok, 1 warn, 2 critical, 3 unknown |
| `riot_posture_vpn_structural_violation` | none | 1 if a VPN-required container bypasses the gateway; absent if not configured or unknown |
| `riot_external_source_up` | `source` | 1 healthy, 0 failing or probing; only configured sources |

Example lines:

```
# HELP riot_device_security_score Overall security score 0-100.
# TYPE riot_device_security_score gauge
riot_device_security_score{device_id="dev-1",hostname="<netmon-host>"} 71
```

- **401** with an empty body (missing, wrong, revoked or non-scrape credential; any
  config) and `WWW-Authenticate: Bearer realm="riot-metrics"`.
- **429** from the per-IP limiter (empty body).
- Any method other than GET gets **405** (chi default).

### 6.5.1 Scrape-token endpoints

| Method / Path | Response |
|---------------|----------|
| `GET /api/v1/settings/metrics-token` | 200 `{"configured": true, "id": "AbC12xYz", "created_at": "…", "rotated_at": "…"}`, or `{"configured": false}`. **Never** the secret. |
| `POST /api/v1/settings/metrics-token` | 201 `{"token": "riot_scrape_…", "id": "…", "created_at": "…", "rotated": false}` on first generation, `"rotated": true` on rotation. The old token is invalid immediately. The secret is returned in this response only. |
| `DELETE /api/v1/settings/metrics-token` | 200 `{"status": "revoked"}`. Idempotent. |

### 6.5.2 `POST /api/v1/settings/external-signals/test`

- **Request:** the same body as PUT (validated with the same rules). It is not
  persisted. Each source is probed at most once per 5 s server-side, to absorb button
  spam.
- **200:**

```json
{ "results": [
  { "source": "prometheus", "configured": true, "ok": true,  "class": "",            "http_status": 200, "latency_ms": 12 },
  { "source": "loki",       "configured": true, "ok": false, "class": "dns_failure", "http_status": 0,   "latency_ms": 3 },
  { "source": "vpn_runtime","configured": false }
] }
```

- **400** for validation failure (same shape as 6.3).

### 6.6 Curated query catalog (exact text as sent; `W` ∈ {1h,6h,24h}, `I` = validated WAN interface)

Every query is an **instant** query at `time = now` (injected clock). "Empty" means an
empty vector, mapped per `EmptyMeans`.

| ID | Source | Query (exact) | Refresh | Empty means | Parse |
|----|--------|---------------|---------|-------------|-------|
| `threat.nginx_5xx` (T1) | Prometheus | `sum(increase(loki_process_custom_nginx_http_requests{status=~"5.."}[W]))` | 60 s | 0 | scalar, `math.Round` |
| `threat.nginx_401` (T2) | Loki | `sum(count_over_time({job="nginx", status="401"}[W]))` | 60 s | 0 | scalar |
| `threat.answered_probes` (T3) | Loki | `sum(count_over_time({job="nginx", status=~"2.."} \| path=~"(?i).*(\\.env\|\\.git\|/\\.aws\|/\\.ssh\|wp-login\|wp-admin\|xmlrpc\\.php\|phpmyadmin\|/actuator\|/vendor/\|/config\\.(json\|php\|ya?ml)\|/backup).*" [W]))` | 60 s | 0 | scalar |
| `threat.blocked_per_min` (T4) | Prometheus | `60 * sum(rate(loki_process_custom_nginx_http_requests{status="444"}[5m]))` | 60 s | 0 | scalar |
| `threat.cred_probes` (T5) | Loki | `sum(count_over_time({job="nginx"} \|~ "(?i)(\\.env\|\\.git\|/\\.aws\|/\\.ssh\|wp-login\|wp-admin\|xmlrpc\\.php\|phpmyadmin\|/actuator\|/vendor/\|/config\\.(json\|php\|ya?ml)\|/backup)" [W]))` | 60 s | 0 | scalar |
| `threat.distinct_attacker_ips` (T6) | Loki | `count(sum by (ip) (count_over_time({job="nginx", status="444"} \| ip != "" [W])))` | 60 s | 0 | scalar (Loki returns the count only; no IP leaves Loki) |
| `threat.top_asns` (T7) | Loki | `topk(5, sum by (asn_org) (count_over_time({job="nginx", status="444"} \| asn_org != "" [W])))` | 60 s | empty list | asnTop: read only `asn_org`, max 5, 128-rune cap |
| `threat.router_blocked_bps` (T8) | Prometheus | `sum(rate(mktxp_firewall_filter_total{name=~".*(geo\|drop from wan\|cameras blocked).*"}[5m])) * 8` | 60 s | 0 | scalar |
| `egress.wan_tx_current_bps` (E1) | Prometheus | `sum(rate(mktxp_interface_tx_byte_total{name="I"}[1h])) * 8` | 60 s | unknown ("no series for interface I") | scalar |
| `egress.wan_tx_baseline_bps` (E2) | Prometheus | `avg_over_time((sum(rate(mktxp_interface_tx_byte_total{name="I"}[5m])))[7d:5m] offset 1h) * 8` | 10 min | unknown | scalar |
| `egress.wan_tx_history_steps` (E3) | Prometheus | `count_over_time((sum(rate(mktxp_interface_tx_byte_total{name="I"}[5m])))[7d:5m] offset 1h)` | 10 min | 0 (insufficient) | scalar; hours = value × 5 / 60 |
| `egress.container_tx_current_bps` (E4) | Prometheus | `topk(50, sum by (name) (rate(container_network_transmit_bytes_total{name!=""}[1h])) * 8)` | 60 s | empty list | byName |
| `egress.container_tx_baseline_bps` (E5) | Prometheus | `topk(200, avg_over_time((sum by (name) (rate(container_network_transmit_bytes_total{name!=""}[5m])))[7d:5m] offset 1h) * 8)` | 10 min | empty list | byName |
| `egress.container_tx_history_steps` (E6) | Prometheus | `topk(200, count_over_time((sum by (name) (rate(container_network_transmit_bytes_total{name!=""}[5m])))[7d:5m] offset 1h))` | 10 min | empty list | byName |

Notes:
- In the table, `\|` is the Markdown escape for `|`. The actual query has a bare `|`.
  The LogQL strings contain `\\.`, which LogQL unescapes to regex `\.`. This is
  identical to netmon panel 3.
- T5 reproduces panel 3 byte for byte, so its numbers match Grafana. T3 deliberately
  filters the **`path` structured-metadata field** rather than the whole line. This
  avoids false positives from referers or user agents that contain `.env`. It is also
  why T3 can be lower than the T5 subset with 2xx.
- T1, T2, T4 and T6 mirror panels 1, 4, 2 and 5. `$__range` is replaced by `W`, and
  `$__rate_interval` by `5m` (FRD 7.3, "last 5 min rate").
- **Explore prefill text (AD-010, 0.2 item 4):**
  - T3: `{job="nginx", status=~"2.."} | path=~"<same regex>"`
  - E4: `topk(50, sum by (name) (rate(container_network_transmit_bytes_total{name!=""}[1h])) * 8)`
- **Upstream request shape:**
  - Prometheus: `GET {prometheus_url}/api/v1/query?query=<urlenc>&time=<unix seconds, 3 decimals>&timeout=10s`.
  - Loki: `GET {loki_url}/loki/api/v1/query?query=<urlenc>&time=<unix nanoseconds>`.
  - Accepted `data.resultType`: `vector`, or `scalar` for Prometheus. Anything else is
    `invalid_response`.
- **Connection-test endpoints:** Prometheus `GET /api/v1/status/buildinfo`, which must
  return JSON with `status == "success"`. Loki `GET /ready`, which must return 200.
  VPN runtime `GET /v1/publicip/ip`, which must return a parsable `public_ip`.

### 6.7 Grafana deep-link mapping (constants)

`B` = `grafana_base_url`. Every link adds `orgId=1`.

| KPI / target | URL |
|--------------|-----|
| 5xx (T1) | `B/d/netmon-security?orgId=1&viewPanel=1&from=now-W&to=now` |
| Blocked probes/min (T4) | `…viewPanel=2&from=now-W&to=now` |
| Credential/path probes (T5) | `…viewPanel=3&from=now-W&to=now` |
| 401 (T2) | `…viewPanel=4&from=now-W&to=now` |
| Distinct attacker IPs (T6) | `…viewPanel=5&from=now-W&to=now` |
| Top ASNs (T7) | `…viewPanel=27&from=now-W&to=now` |
| Router blocked (T8) | `…viewPanel=6&from=now-W&to=now` (a second link "drops by rule" → `viewPanel=24`) |
| WAN vs baseline (E1–E3) | `…viewPanel=8&from=now-7d&to=now` |
| Answered probes (T3) | `B/explore?orgId=1&schemaVersion=1&panes=<enc({"pp":{"datasource":"loki","queries":[{"refId":"A","expr":"<T3 log form>","queryType":"range","datasource":{"type":"loki","uid":"loki"}}],"range":{"from":"now-W","to":"now"}}})>` |
| Per-container transmit (E4) | `B/explore?…panes=<enc({"pp":{"datasource":"prometheus","queries":[{"refId":"A","expr":"<E4>","datasource":{"type":"prometheus","uid":"prometheus"}}],"range":{"from":"now-7d","to":"now"}}})>` |
| Tile-level "Open Security Overview" | `B/d/netmon-security?orgId=1&from=now-W&to=now` |
| VPN checks | No Grafana link. rIOt device or container page (FR-058). |

### 6.8 Alert templates added (FR-075)

| `id` | Name | Metric | Op | Threshold | Severity | Cooldown | Category |
|------|------|--------|----|-----------|----------|----------|----------|
| `vpn_leak_detected` | VPN leak detected | `vpn_leak` | `>=` | 1 | critical | 1800 | external |
| `exposed_probe_answered` | Exposed probe path answered | `ext_answered_probes` | `>=` | 1 | warning | 3600 | external |
| `burst_401` | 401 burst | `ext_nginx_401` | `>=` | 300 | warning | 1800 | external |
| `wan_egress_spike` | WAN egress spike | `ext_wan_tx_ratio` | `>=` | 4.0 | warning | 1800 | external |
| `netmon_unavailable` | netmon unavailable | `external_source_unavailable` | `>=` | 5 | warning | 3600 | external |

`NeedsTargetName` is false for all. No rule is seeded (`seedDefaultAlertRules` is
unchanged, BR-005).

### 6.9 Internal Go interfaces (contract only)

```go
// extsignals
func NewService(admin db.AdminRepository, devices db.DeviceRepository, env EnvDefaults, clock func() time.Time) *Service
func (s *Service) Start(ctx context.Context)                 // non-blocking; launches poller
func (s *Service) ApplyConfig(ctx context.Context, cfg Config, secrets *Secrets) error // persist + swap + kick
func (s *Service) Config() Config
func (s *Service) Snapshot() Snapshot                        // no I/O
func (s *Service) Status() map[Source]SourceHealth
func (s *Service) TestConnection(ctx context.Context, cfg Config, secrets *Secrets) []TestResult

// posture
func NewFleetCache(devices db.DeviceRepository, tel db.TelemetryRepository, scorer Scorer, clock func() time.Time) *FleetCache
func (c *FleetCache) Run(ctx context.Context)
func (c *FleetCache) Get(ctx context.Context) (*FleetState, error) // cached or singleflight compute
func NewService(fc *FleetCache, ext *extsignals.Service, rules RuleCoverage, clock func() time.Time) *Service
func (s *Service) Build(ctx context.Context) (*models.PostureResponse, error)
func (s *Service) ExternalAlertValues(ctx context.Context) (events.ExternalSignalValues, bool /*integrationEnabled*/)

// events
func (g *Generator) EvaluateExternalSignals(ctx context.Context, v ExternalSignalValues, integrationEnabled bool)
func (g *Generator) HasEnabledRuleForMetric(ctx context.Context, metric string) bool

// metricsexport
func NewTokenStore(admin db.AdminRepository, clock func() time.Time) *TokenStore
func (t *TokenStore) Generate(ctx context.Context) (token string, rec TokenRecord, rotated bool, err error)
func (t *TokenStore) Revoke(ctx context.Context) error
func (t *TokenStore) Verify(presented string) bool          // constant-time; no I/O
```

---

## 7. Sequence / Flow

### 7.1 Startup

1. Repos are created, then EventGen and Dispatcher, as today.
2. `ExtSignals = extsignals.NewService(...)`. It loads `external_signals_config`. If the
   key is absent, it uses defaults plus the seed env. It then loads the secrets.
3. `ScrapeTokens.Load(ctx)`. A missing record means `/metrics` returns 401.
4. `FleetCache` is created and `go FleetCache.Run(ctx)` starts. The first refresh is
   immediate.
5. `go ExtSignals.Start(ctx)`. The first poll pass runs at +5 s. All sources are
   `probing`, or `not_configured`.
6. `go externalSignalAlertWorker.run(ctx)`. The first evaluation runs at +45 s.
7. The router is built (AD-021 pprof branch, `/metrics`, new admin routes), and the
   server listens. No step blocks on netmon (NFR-006).

### 7.2 Poll pass (every 15 s)

1. If the integration is disabled: no queries run, and VPN runtime does not run either
   (it is part of the integration settings).
2. For each `QueryDef`:
   - The source must be configured.
   - It must be due: `now - lastAttempt >= RefreshEvery` and `>= 60s`.
   - If so, `singleflight.Do(id)`, acquire the source semaphore, `Client.Query`
     (10 s), then parse.
3. Record success or failure in the cache with `clock()`.
4. Recompute source health. On a transition, log (AD-005).
5. If VPN runtime is configured and due, `PublicIP`, and record it.

### 7.3 `GET /api/v1/posture`

1. AdminAuth runs.
2. `Posture.Build`:
   - `FleetCache.Get` (memory, or a singleflight DB read if cold).
   - `ExtSignals.Snapshot()` (memory).
   - `HasEnabledRuleForMetric("vpn_leak")` (5 s rules cache).
3. Tile builders run in fixed order with `now = clock()`. Then the summary is built.
4. Write JSON with `no-store`.

### 7.4 External alert evaluation (every 60 s)

1. The worker calls `Posture.ExternalAlertValues(ctx)`, which builds values from
   `Snapshot` and the egress/VPN computations, with `Known` false where they are
   unknown.
2. `EventGen.EvaluateExternalSignals(ctx, values, enabled)`. For each enabled rule with
   an external metric, the following checks run **in order**:
   1. If netmon-derived and disabled: skip.
   2. If not `Known`: skip.
   3. If `!compareValue`: skip.
   4. If in cooldown: skip.
   5. Otherwise `createEventAndNotify`, which writes the event, broadcasts it, and calls
      `Dispatch` to all enabled channels.

### 7.5 Settings save

1. PUT decodes with strict unknown-field rejection, then `Validate`. On failure: 400
   with `fields`, and nothing is written.
2. `ApplyConfig`:
   - `SetConfig(external_signals_config)`, and secrets if provided.
   - Swap the atomic config.
   - If `enabled` turned false or a URL changed: `cache.Discard(source)` and set
     health to `not_configured` or `probing`.
   - Kick the poller.
3. Respond with the 6.2 shape.

### 7.6 Scrape

1. Prometheus sends `GET https://riot-server:7331/metrics` with
   `Authorization: Bearer …`.
2. The rate limiter runs, then `ScrapeAuth` (`TokenStore.Verify`, constant time).
3. Handler: `FleetCache.Get` and `Posture.Build` (for the tile gauges) and
   `ExtSignals.Status`, then write the exposition.

### 7.7 Token rotation

1. The operator clicks "Rotate". `ConfirmDialog` warns that scraping breaks until the netmon
   token file is updated.
2. `POST /settings/metrics-token`. A new token is generated, `SetConfig` runs, the
   atomic pointer is swapped, an INFO log is written (id only), and the token is shown
   once.
3. The next scrape with the old token gets 401.

---

## 8. Acceptance Criteria Mapping

Test names must include the AC ID (NFR-013), for example `TestBuildExposure_AC017_FirewallCritical`
or `it('[AC-005] caps items at 5')`.

| AC | Fulfilled by | Test strategy |
|----|--------------|---------------|
| AC-001 | `GET /api/v1/posture` in the `adminAuth` group. SPA renders `<Login>` when unauthenticated. | Go router: no cookie gives 401, valid JWT gives 200. Vitest: authenticated `/posture` renders. Unauthenticated App renders Login (existing pattern). |
| AC-002 | `App.tsx` NavLink "Posture" before "Security" (desktop and mobile) | Vitest: nav link order in both menus |
| AC-003 | `Service.Build` fixed tile order. Grid classes `grid-cols-1 md:grid-cols-2`. | Go: tile ID order. Vitest: order and classes. QA manual at 767 and 1024 px. |
| AC-004 | `PostureTile` (Badge label, reason, `RelativeTime`, source). `clampReason`. At least one action per builder. | Go: for every builder fixture, reason ≤ 160 runes, actions ≥ 1, sources non-empty. Vitest: Badge text, RelativeTime, source label. |
| AC-005 | `capItems` and `HiddenCount`. `PostureItemList` "+N more". | Go: 8 contributing devices give 5 items and hidden 3. Vitest: "+3 more". |
| AC-006 | Server `Summary`, `PostureSummary` component. No overall score field. | Go: counts. Vitest: text "1 critical, 1 warn, 2 ok, 1 unknown", no "overall" or score element. |
| AC-007 | AD-002 poller as sole executor, per-ID 60 s minimum, singleflight. Handler reads the cache only. | Go integration: counting httptest stub, fake clock stepping 5 min. 3 goroutines calling the handler every 60 s plus 10 manual calls. Assert ≤ 1 execution per query ID per 60 s window. |
| AC-008 | AD-002/AD-005 `pending`. Native tiles from FleetCache. | Go: hanging stub (accepts, never responds). The handler returns in < 2 s with native tiles final and threat `pending`. After the poll's 10 s timeout (injected short timeout in test; the constant is asserted to be 10 s), the KPIs are `unknown` with reason containing "timeout". Vitest: a pending tile renders the skeleton. |
| AC-009 | `posture.Aggregate` | Go table: (ok, unknown) gives unknown with the name in the reason. (warn, unknown) gives warn. (critical, warn, ok) gives critical. |
| AC-010 | `BuildEgress` not-configured handling | Go: runtime not configured and others ok gives `ok`, the not-configured entry with `settings_path`, and the "only structural" note |
| AC-011 | `BuildThreat` disabled branch. VPN structural independent of extsignals. | Go |
| AC-012 | Client `http_error`, per-source health, `BuildThreat` | Go: Loki stub 502, Prometheus stub OK. Loki KPIs unknown "Loki … HTTP error". T1, T4, T8 have values. |
| AC-013 | AD-005 stale window | Go: fake clock. Last success at −10 min shows `stale` with fetched_at. At −16 min there is no value. Vitest: "stale" label plus RelativeTime. |
| AC-014 | Patching `command` action (allowlist `os_update` security). `PostureActionButton` uses ConfirmDialog, then `api.sendCommand`, then a toast. | Vitest: no call before confirm; call with `('id','os_update',{mode:'security'})` after confirm; success and failure toasts. Go: allowlist test. Review: no new command in `internal/agent`. |
| AC-015 | `deeplinks.go` and the anchor attributes | Go: exact URL for the 401 KPI with W=6h. Handler ignores request query parameters. Vitest: `target=_blank`, `rel` contains `noopener` and `noreferrer`, `referrerpolicy=no-referrer`. |
| AC-016 | No-base-URL branch | Go: no `grafana` actions or links; Settings link present. Vitest: text and href `/settings/external-signals#grafana`. |
| AC-017 | `BuildExposure` rule sets | Go: three cases |
| AC-018 | Exposure item fields | Go: 14 ports give 10 plus `more_ports` 4; nginx 2 headers, no rate limit, 12 days; links. Vitest: rendering "+4 more", "2 security headers", "no rate limit", "cert: 12 days". |
| AC-019 | Exposure unknown | Go |
| AC-020 | Hardening bands | Go: 74 warn, 75 ok, 59 critical |
| AC-021 | Category minimum "no data". Item opens `SecurityScoreModal`. `/security` link. | Go: docker shows "no data". Vitest: click calls `api.getSecurityScore` and shows the modal. Link present. |
| AC-022 | Hardening unknown | Go |
| AC-023 | `BuildPatching` | Go: the four cases |
| AC-024 | Patching unknown | Go |
| AC-025 | FleetState offline marking and `devices_without_data` | Go: B offline 3 h is stale and counted. C excluded, so the note says 1 on each native tile. Vitest: "3 hours ago" via RelativeTime. |
| AC-026 | `BuildThreat` thresholds and contributing flags | Go: 401=120 gives warn, reason names 401s; informational KPIs don't change status; 401=300 gives critical |
| AC-027 | T3 query and threshold | Go: stub returns 1 for the T3 query text (asserted exactly); KPI warn; tile at least warn. QA: end-to-end against netmon with a `/.env` 200 test line (optional). |
| AC-028 | `parse.asnTop` (whitelist `asn_org`, max 5) | Go: 10-series fixture with an extra `ip` label gives 5 entries and no IP. Response-JSON IPv4/IPv6 regex scan (excluding `unit: ip` sub-signals). |
| AC-029 | Explore builder | Go: decode `panes` JSON; datasource `loki`, expr is T3 log form, range now-W..now. Vitest: href. |
| AC-030 | Threat facts "Source: netmon, host the netmon host" | Go |
| AC-031 | Window enum, config apply, re-rendered queries | Go: save 24h, next pass renders `[24h]`, links `from=now-24h`. `12h` gives a 400 field error. |
| AC-032 | AD-013 | Go: 22/5 gives 4.4 critical; 12/5 gives warn; 0.9/0.2 below floor gives ok |
| AC-033 | AD-013 step 2 | Go: E3 = 240 steps (20 h) gives unknown "Insufficient baseline history" |
| AC-034 | AD-014 | Go: X 4 vs 1 Mbit/s gives warn. vpn-app-a `container:<gluetunID>` is grouped under gluetun with the caveat and no tx. Defaults never critical. |
| AC-035 | AD-015 | Go: vpn-app-b `bridge` gives critical; reason names vpn-app-b and "bridge"; link to the container page |
| AC-036 | AD-015 | Go: vpn-app-c absent gives not reporting and warn; vpn-app-d exited gives "not running" with no effect |
| AC-037 | AD-015 independence | Go: integration disabled still evaluates; empty list gives not configured |
| AC-038 | AD-016 | Go: gluetun stub 198.51.100.7 vs house 203.0.113.9 gives ok; equal gives critical; stub 500 gives unknown |
| AC-039 | `ApplyConfig` and `admin_config` persistence | Go: PUT then a new Service from the same MockAdminRepo keeps the values; a URL change is used by the next pass ≤ 60 s (fake clock) |
| AC-040 | `extsignals.Validate` and the PUT handler | Go table: 4 inputs give 4 field errors; MockAdminRepo unchanged |
| AC-041 | AD-003 allowlist, AD-004 catalog, `DisallowUnknownFields` | Go: recording stub over 10 simulated minutes (poller plus test conn); every path is in the allowlist, method GET; every `query` parameter equals a rendered catalog entry. PUT with `"query"` gives 400. |
| AC-042 | `testconn.go` and Settings UI | Go: Loki URL `http://nonexistent.invalid:3100` gives `dns_failure`; Prometheus stub OK. Vitest: toast plus inline results; RelativeTime for last success and error. |
| AC-043 | Client 10 s timeout | Go: stub sleeps beyond the timeout (test injects 100 ms; the constant is asserted == 10 s) and returns class `timeout`; the KPI reason says "timeout" |
| AC-044 | AD-017 validation, AlertRuleSettings | Go: POST `vpn_leak` with `include_devices` gives 400 with field. Vitest: scope fields hidden for `vpn_leak`; metric list contains the seven metrics (FR-068 set) |
| AC-045 | AD-018 worker and evaluator | Go: rule 401 ≥ 300, value 350, webhook httptest channel. One POST within the window; payload contains name, severity, KPI, 350, ">= 300", "6h", Grafana link. One event with `device_id` `external:netmon`. No second POST within 1800 s (fake clock). |
| AC-046 | FR-071 skip, per-source cooldown | Go: Loki failing 6 min. 401 rule not fired; unavailable rule fired once. |
| AC-047 | Templates, no seeding, `vpn_leak_rule_enabled`, `rule_template` action | Go: `AlertTemplates()` contains the 5 IDs; `seedDefaultAlertRules` creates no external rule; posture flag false gives the Egress notice and action. Vitest: navigates to `/alert-rules?template=vpn_leak_detected`, and the editor is pre-filled. |
| AC-048 | Evaluator `integrationEnabled=false`; UI chip | Go: 401 rule skipped; `vpn_leak` fires on structural critical. Vitest: "inactive: integration disabled". |
| AC-049 | No change to existing generator methods or tests | CI: the existing `events/*_test.go` (incl. `generator_nginx_access_test.go`) pass **unmodified**. Diff review: no edits to existing method bodies, agent, or nginx templates. |
| AC-050 | AD-019 exposition | Go: 3 seeded devices; grammar validator passes; all families present for 3 devices; 5 tile gauges, VPN gauge, source gauges. QA: `docker run --rm -i --entrypoint promtool prom/prometheus:v3.0.1 check metrics < out.txt`. README review. |
| AC-051 | AD-019 privacy | Go: with a cached ASN/attacker fixture, output has no IP regex match, no `asn`, no geo key, no finding description or remediation substring, no KPI metric names |
| AC-052 | AD-020 `ScrapeAuth`, isolation | Go router: no token gives 401 with empty body; wrong token; device `X-rIOt-Key`; legacy `RIOT_API_KEY` value as Bearer; mTLS client cert (httptest TLS) all give 401. A valid scrape token on `/api/v1/devices` gives 401. |
| AC-053 | `TokenStore`, handlers, logging | Go: GET never contains the token; rotate makes the old 401 and the new 200 immediately; captured slog output has the id and not the secret. Vitest: secret shown once, hidden after close and refetch. |
| AC-054 | AD-012 (no `AccessMetrics` use, duplicate note) | Go: AccessMetrics 5xx=40, netmon 5xx=2. KPI 2, note present, status unchanged. |
| AC-055 | AD-001 | Go: migrations directory has no file > 000022 (schema test). After 10 simulated minutes, every MockAdminRepo value written has no IP or ASN, and EventRepo rows contain no IP. A restarted Service gives the first Build all-pending external KPIs. Vitest: after rendering Posture, `localStorage` and `sessionStorage` are empty. QA: IndexedDB empty (devtools). |
| AC-056 | Admin group placement | Go router: `X-rIOt-Key` or no session gives 401 and a body without aggregates |
| AC-057 | AD-003/AD-005 logging discipline, AD-018 messages | Go: forced Loki parse error plus a fired rule. Captured logs and the webhook payload have no IPv4/IPv6 or lat/long pattern. |
| AC-058 | AD-021 | Go router: disabled gives 404 on `/debug/pprof/` and `/debug/pprof/heap`; enabled without a session gives 401; enabled with a session gives 200 |
| AC-059 | AD-024 | Vitest: tiles render `Card`; Badge labels OK/Warning/Critical/Unknown; Buttons; QueryState; RelativeTime; every `overflow-*` element has `scrollbar-thin`; status text present without CSS |
| AC-060 | AD-002/AD-005/NFR-006 | Go: both URLs pointing at a closed port. `Start` returns immediately; the handler returns 200 with native tiles; over 30 simulated minutes, captured WARN count = 1 per source and no more than 1 failure line per source per pass |
| AC-061 | AD-007 cache, AD-003 cap | Go: 100 seeded devices, warm caches, 200 sequential requests each to posture and `/metrics`; p95 ≤ 500 ms and ≤ 1 s. The scorer counter is ≤ 1 per device per 60 s of fake time. A 2 MiB stub gives `invalid_response`. |

---

## 9. Error Handling

| Failure | Detection | Behavior | HTTP | Logging |
|---------|-----------|----------|------|---------|
| Source DNS failure, refused, unreachable | `classify` | Affected KPIs `unknown`, stale value if within 15 min; health `failing` after a full failed pass | Posture 200 | WARN once on transition (AD-005) |
| Source timeout (10 s) | ctx deadline | Same as above, class `timeout` | 200 | Same |
| Non-2xx from source | status code | Class `http_error` with status | 200 | Same |
| Invalid, oversize or too many series | parse, LimitReader | Class `invalid_response` | 200 | Same |
| Redirect from source | `ErrUseLastResponse` with 3xx | `http_error` | 200 | Same |
| Integration disabled | config | Threat `unknown` "netmon integration not configured"; Egress netmon sub-signals not configured; VPN structural still runs | 200 | none |
| Fleet cache refresh failure | repo error | Keep the last state plus a note; with no state, native tiles `unknown` "rIOt telemetry store unavailable" | 200 | ERROR `posture: fleet refresh failed` (max once per minute) |
| Settings validation | `Validate` | Nothing persisted | 400 with `fields` | none |
| Settings persistence error | `SetConfig` error | Config not swapped | 500 `{"error":"failed to save external signals config"}` | ERROR |
| Test connection failure | per source | Reported in results | 200 | none (ad-hoc) |
| Token store DB error on generate/revoke | repo error | In-memory pointer not swapped | 500 `{"error":"failed to update metrics token"}` | ERROR (no secret) |
| `/metrics` bad or missing credential | `ScrapeAuth` | Empty body | 401 | WARN at most once per minute with count and remote |
| `/metrics` flood | limiter | Empty body | 429 | existing limiter behavior |
| pprof disabled | route | `NotFoundHandler` | 404 | none |
| pprof enabled, no session | AdminAuth | Unauthorized JSON | 401 | none |
| External rule with scope | handler validation | Not persisted | 400 with `fields` | none |
| External evaluator dispatch failure | Dispatcher | Logged per channel by the existing Dispatcher; notification_log `failed`; event still stored | n/a | existing ERROR |
| VPN host has no docker telemetry | builder | Required containers "not reporting", `warn` | 200 | none |
| Panic in a tile builder | `chimw.Recoverer` (existing) | Response 500. Builders must be total; a fuzz-style table test covers nil telemetry fields. | 500 | existing |

Error messages returned to the browser for source failures contain only the source,
class and HTTP status. They never contain a URL, a body or `err.Error()`.

---

## 10. Security Considerations

To be confirmed and extended by the security review. The FRD hands it OQ-001, NFR-007,
NFR-008, FR-084 to FR-088, FR-097, and the lifecycle-endpoint exposure.

1. **SSRF via admin-configured URLs (NFR-007).**
   - **Mitigations designed in:**
     - Only http/https, no userinfo, no query or fragment.
     - GET only, to fixed paths (AD-003). Redirects are not followed.
     - `Proxy: nil`.
     - A 1 MiB cap, and responses are never reflected to the browser (only parsed
       numbers or ASN strings are).
     - Error bodies are never returned.
     - The optional `RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS` server-side allowlist, which
       the admin UI cannot change.
   - **Residual risk:** an admin can point rIOt at any internal host:port and learn
     reachability and timing (blind SSRF). Admins are already full-trust (A-002). The
     review decides whether the allowlist becomes mandatory or is recommended.
2. **Query injection / heavy-query DoS (OQ-001).** No operator query text. The only
   substitutions are an enum and a strict regex in an exact matcher (AD-004).
   Per-query timeout plus Prometheus `timeout=` parameter. Bounded `topk`. Baseline
   queries every 10 min.
3. **netmon lifecycle endpoints.** After the network join, riot-server can reach
   `POST /-/quit` and `/-/reload`. rIOt cannot issue them through its client (no POST,
   allowlisted paths). The recommendation to remove `--web.enable-lifecycle` is Section
   15, D-3.
4. **pprof (FR-097).** Off by default, admin-only when on, explicit 404 (AD-021).
   Phase 0 ships it before the network join.
5. **Scrape credential (FR-084 to FR-088, NFR-008).** 256-bit random token, SHA-256 at
   rest, constant-time compare, bearer header only (never a query string, so it is not
   in access logs; `middleware.Logger` logs only the path), shown once, immediate
   rotation, per-IP limiter, throttled failure logs. It is usable only on `/metrics`.
   The review may add an HMAC pepper.
6. **Attacker data (FR-092 to FR-096, BR-006).**
   - Queries return only aggregates. The ASN parser whitelists one label.
   - Memory only. Admin-only endpoint. `Cache-Control: no-store`. No browser
     persistence.
   - Logs carry only IDs and classes. Notifications carry only counts.
   - `/metrics` carries none of it.
   - These are automated tests (NFR-009: AC-028, AC-051, AC-055, AC-057).
7. **VPN IPs.** The house IP and VPN exit IP are shown only in the admin Posture
   response. Never logged, exported or notified (AD-016). The gluetun API key is
   write-only and stored in a separate config key (like notification channel tokens,
   plaintext in the database). The review decides whether that is acceptable.
8. **Posture actions.** Only `os_update{mode: security}` via the existing,
   admin-authenticated command endpoint, with ConfirmDialog. No new device-side action
   (FR-016).
9. **Deep links.** Built server-side from constants and the validated base URL. No
   request input is reflected. They open with `noopener noreferrer` and
   `referrerPolicy=no-referrer`, so Grafana gets no referrer revealing rIOt URLs
   (FR-018, FR-019).
10. **Exposure of rIOt (OQ-006).** If rIOt is reachable from outside the LAN, Threat
    aggregates become internet-visible to any session holder. The operator must confirm
    LAN/VPN-only (Section 15, D-6).
11. **Network join blast radius.** Every netmon container can reach riot-server:7331.
    With pprof gated, the exposed surface is the same as LAN exposure today (TLS,
    auth). `/metrics` requires the token.

---

## 11. Performance Considerations

- **Fleet scoring:** one projected DB query plus N `Score` calls every 60 s. At 100
  devices, `Score` costs microseconds per device. The projection avoids decoding
  processes, logs, USB and hardware data. Memory is about the size of the projected
  JSON for 100 devices (expected low single-digit MB), with container Env and Labels
  dropped.
- **Posture API (NFR-001):** pure in-memory compute, O(devices + KPIs). The rules
  lookup uses the existing 5 s cache. The target is well under 50 ms warm, against a
  budget of 500 ms p95.
- **`/metrics` (NFR-003):** in-memory, about 3k lines at 100 devices, under 1 MB of
  text. The target is under 100 ms, against a budget of 1 s. The scrape interval should
  be 60 s, matching the cache cadence.
- **netmon load (NFR-004):** 11 queries per 60 s, plus 5 per 10 min, plus 1 VPN runtime
  call per 60 s when configured. None of this scales with users, scrapes or rules. The
  three 7-day subqueries read about 20k samples per series every 10 min.
- **Resource bounds (NFR-005):** 1 MiB per response, 250 series, 4 concurrent per
  source, 10 s timeouts, 128-rune labels.
- **No indexing or migration changes.** The existing `idx_telemetry_device_time` serves
  the projection. This is compatible with PART-001 partitioning, which keeps the same
  table name and the same `(device_id, timestamp)` access path (assumption; verify when
  PART-001 lands).
- **Frontend:** one request per 60 s per visible tab, 5 s only while a tile is pending.
  No polling in hidden tabs.
- **Logging volume:** `middleware.Logger` emits one INFO line per scrape (stdout only;
  the logstore stores WARN and above). That is acceptable, and no change is needed.

---

## 12. Implementation Notes for Engineers

1. **No `time.Now()` in testable code.** `extsignals`, `posture`, `metricsexport` and
   `events/external.go` take a `clock func() time.Time`. Only `server.go` passes
   `time.Now`. `scoring.Score` stamps `EvaluatedAt` itself, and that is fine because
   tile `evaluated_at` comes from `FleetState.ComputedAt`.
2. **Register routes before the SPA catch-all.** `/metrics`, the pprof 404 handlers
   and every new API route must be registered in `setupRouter` before
   `serveFrontend`. Otherwise unmatched paths return `index.html` with HTTP 200.
3. **pprof route shape.** Use `r.Route("/debug/pprof", ...)` with `r.Use(adminAuth)`
   when enabled. When disabled, register **both** `/debug/pprof` and `/debug/pprof/*`
   to `http.NotFoundHandler()`.
4. **Never log `err.Error()` from the external client.** It embeds the full URL and
   query. Log `source`, `query_id`, `class` and `http_status`.
5. **Docker `service:` vs `container:`.** Compose `network_mode: service:gluetun` is
   reported by the agent as `container:<64-hex id>`. Match by ID, by a 12+-char prefix,
   or by name (AD-015). Fixtures must include the full-ID form.
6. **`increase()` rounding.** Round T1 before thresholds (AD-012). Do not round ratios.
7. **Loki label filter on structured metadata.** T3 relies on Loki 3.x structured
   metadata being available to label filters (`| path=~"…"`). The same mechanism
   already powers panels 5 and 27 (`| ip != ""`, `| asn_org != ""`). Label regex
   matchers are fully anchored, hence the `.*…*.` wrapper.
8. **Prometheus empty vector vs scalar.** `sum(...)` over no series returns an empty
   vector, not 0. Apply `EmptyMeans` per query, and never guess.
9. **Reason strings.** Build them with `fmt`, then `clampReason`. The dominant
   contributor goes first: worst status, then the highest-weight item.
10. **Command allowlist.** Use a single const set in `posture/patching.go`, with a test
    that walks all builder outputs.
11. **Generator edits are additive only.** Add fields to `Generator` and initialise
    them in `NewGenerator`. Do not touch existing methods (AC-049). The external
    evaluator lives in the new `events/external.go`.
12. **Mocks.** Update `internal/testutil/mocks.go` for the new `TelemetryRepository`
    method (signature changes break mocks). Build and run `go test ./...` and
    `cd web && npm run test:run` locally before pushing. On Windows, skip `-race`.
13. **Strict decoding.** Use `json.Decoder.DisallowUnknownFields()` for PUT
    `/settings/external-signals` only. Do not change other handlers.
14. **Exposition escaping.** Label values escape `\` → `\\`, `"` → `\"`, newline →
    `\n`. Metric and label names are constants. Sort series by `(device_id, category,
    severity)` for deterministic output (test golden files).
15. **No new dependencies.** Go uses only stdlib plus the existing `golang.org/x/sync`.
    No new npm packages. **Any new import outside these must be raised with the
    architect first.**
16. **UI-FOUNDATION-001.** Import the primitives from `web/src/components/ui/`. Do not
    fork `ConfirmModal`. Wait for UI-FOUNDATION-001 to merge (AD-024).
17. **Demo fixtures.** Use RFC 5737 and RFC 3849 documentation IPs for the VPN runtime
    demo values (`203.0.113.x`, `2001:db8::`), made-up ASN names, and no real hostnames
    beyond existing demo data.
18. **Release.** Tag each phase (repo rule). The CHANGELOG notes the pprof behavior
    change in Phase 0.

---

## 13. Definition of Done

Applies to the final phase cumulatively. Each phase satisfies the subset in Section 14.

- [ ] All component changes in Section 4 are implemented. There is no migration, and
      the directory still ends at 000022.
- [ ] Every AC in Section 8 has a passing test whose name contains its AC ID, or a
      documented QA manual step (AC-003 layout, AC-050 promtool, AC-055 IndexedDB).
- [ ] Existing Go and Vitest suites pass **unmodified**, especially `internal/server/events/*_test.go` (AC-049).
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` and `cd web && npm run test:run`
      are green. There are no new lint errors and no `any` in new TS.
- [ ] No new Go module in `go.mod` (`git diff go.mod` is empty, or only `go mod tidy`
      noise). `package.json` dependencies are unchanged.
- [ ] Recorded fixtures cover success, empty, error, timeout, oversize and too-many-series
      for every catalog query (NFR-013).
- [ ] The privacy tests (AC-028, AC-051, AC-055, AC-057) pass.
- [ ] `promtool check metrics` (Prometheus v3.0.1 image) passes on a captured `/metrics`
      body.
- [ ] pprof returns 404 by default, and 401 without a session when enabled.
- [ ] README and CHANGELOG are updated (FR-083, NFR-015). The Infrastructure docs change is
      **drafted only**, pending the operator's approval.
- [ ] Demo build (`VITE_DEMO=true`) renders the Posture page and the External signals
      settings page with no network calls.
- [ ] The security review conditions (`docs/security/POSTURE-001-security-review.md`)
      are addressed.

---

## 14. Phased delivery (AD-025)

> **Update (2026-10-18).** Phase 0 is done: HARDEN-002 shipped it in v2.65.0 (see AD-021).
> UI-FOUNDATION-001 has merged to main, so Phase 1 is unblocked (D-9). With REG-HARDEN-001
> shipped, Phase 4 no longer waits on security condition C1.

| Phase | Scope | ACs closed | Depends on |
|-------|-------|-----------|------------|
| **0: Security gate** (small PR) | AD-021 pprof gating, config flag, router 404s, tests, CHANGELOG breaking note | AC-058 | none. **Must be deployed to the netmon host before Section 16 step 3.** |
| **1: Posture page, native tiles** | `posture` package (FleetCache, aggregate, exposure, hardening, patching; egress with VPN structural only, netmon sub-signals "not configured"); repo projection and mocks; `GET /api/v1/posture`; `models/posture.go`; Posture UI; nav; demo stubs; minimal `extsignals/config.go` and `validate.go` and the Settings page sections for VPN host, list and gateway | AC-001 to AC-006, AC-009, AC-010, AC-014, AC-017 to AC-025, AC-035 to AC-037, AC-059; partial AC-011 | UI-FOUNDATION-001 merged (frontend) |
| **2: External signals and the Threat and Egress tiles** | Rest of `extsignals` (client, catalog, parse, cache, poller, testconn, vpnruntime), Settings for URLs, thresholds and Test connection, `BuildThreat`, WAN and per-container egress, deep links | AC-007, AC-008, AC-011 to AC-013, AC-015, AC-016, AC-026 to AC-034, AC-038 to AC-043, AC-054 to AC-057 (Posture parts), AC-060, AC-061 (posture parts) | Phase 1 |
| **3: External-signal alerting** | Registry, validation, templates, `events/external.go`, worker, AlertRuleSettings changes, Alerts label, event types, Egress VPN-rule notice | AC-044 to AC-049, AC-057 (notification parts) | Phase 2 |
| **4: `/metrics` export** | `metricsexport`, token store, `ScrapeAuth`, token endpoints and UI card, README metrics table | AC-050 to AC-053, AC-061 (`/metrics` parts) | Phase 1 (tile gauges); Phase 2 for source gauges |

Deployment steps (Section 16) are executed by the operator **after** Phase 2 for the
network join and **after** Phase 4 for the scrape job.

---

## 15. Decisions requiring the product owner (the operator)

| # | Decision | Recommendation | Default the code ships with |
|---|----------|----------------|-----------------------------|
| D-1 | **Resolved by HARDEN-002 (v2.65.0), loopback listener; see AD-021.** Original: **pprof becomes opt-in and admin-only** (OQ-007, AD-021). This breaks today's unauthenticated `curl` profiling. pprof is used for memory profiling. | Accept. When profiling, set `RIOT_PPROF_ENABLED=true` temporarily and use the session cookie. | Disabled |
| D-2 | **VPN runtime check reachability** (OQ-004, AD-016). riot-server must reach gluetun's control server (`:8000`, auth mode UNVERIFIED). That requires joining riot-server to the VPN stack's network or another path, which gives riot-server a route into the namespace hosting the VPN-routed application UIs. House IP is manual (dynamic-IP risk). | Ship structural-only on the netmon host for v1. Leave runtime "not configured". Revisit if the operator wants a runtime guarantee. If so, prefer a gluetun API-key-protected control server and a dedicated small network shared only by riot-server and gluetun. | Not configured |
| D-3 | **netmon Prometheus `--web.enable-lifecycle`.** It becomes reachable from riot-server after the join (and already from every netmon container). | Remove the flag in `<netmon-dir>/docker-compose.yml`, and reload via `docker compose restart prometheus`. This is a deployment change for approval, separate from this story's code. | No change (deployment-side) |
| D-4 | **Scrape TLS verification.** Pin rIOt's self-signed server cert in Prometheus (`ca_file` + `server_name: <host-lan-ip>`, if that IP SAN exists) vs `insecure_skip_verify` on the internal bridge. | Pin. **Do not regenerate the rIOt TLS cert** to add a SAN, because agents pin its fingerprint (TOFU). Use `insecure_skip_verify` only if the IP SAN is absent. | n/a (netmon config) |
| D-5 | **Destination allowlist** `RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS` (NFR-007). | Set it on the netmon host to `prometheus:9090,loki:3100` (plus the gluetun host if D-2 is accepted). The security review may make it mandatory. | Unset (any host) |
| D-6 | **Confirm rIOt is LAN/VPN-only** (OQ-006, A-016). The repo publishes 7331 on all interfaces. | Confirm. If rIOt is internet-reachable, open a follow-up to gate Threat aggregates. | Assumed LAN-only |
| D-7 | **Default thresholds** (OQ-012), including the analyst defaults 5xx critical 25 and egress ratios 2/4 and 3, plus the architect refinement of 10-minute baseline refresh (AD-013, 0.2 item 3). | Accept. Tune in Settings after a week. | As in 5.2.1 |
| D-8 | **VPN-required list is not pre-seeded** (A-011 replaced, 0.2 item 7). | Accept. Use "Suggest from VPN host" to add the exact container names on the VPN host. | Empty |
| D-9 | **Merge order:** UI-FOUNDATION-001 before the Phase 1 frontend (AD-024). | **Resolved:** UI-FOUNDATION-001 merged to main (PR #6). | n/a |
| D-10 | **Nav placement** (OQ-002). | Posture immediately before Security, as in the FRD default. | Before Security |

---

## 16. Deployment prerequisites. A checklist for the operator, NOT executed by this story

All of these steps are deployment changes and require the operator's approval (BR-008). Nothing
is deleted. Read the deployment's compose-location notes first.

1. [x] **Done (v2.65.0, HARDEN-002): the main port returns 404 for pprof.** Original step: **Phase 0 release is running on the netmon host.** Verify by digest (`docker inspect`).
   Confirm `curl -sk https://<riot-host>:7331/debug/pprof/` returns **404**.
2. [ ] **Locate riot-server's compose directory.** If it is not already
   documented, find it with:
   `docker inspect riot-server --format '{{ index .Config.Labels "com.docker.compose.project.working_dir" }}'`.
   Record it in the deployment notes.
3. [ ] **Join riot-server to `netmon`** (after Phase 2 is deployed). Compose diff for
   the riot-server service. Keep `default` explicitly: declaring `networks:` removes
   the implicit default network, and without it riot-server loses `riot-db`.

   ```yaml
   services:
     riot-server:
       networks:
         - default
         - netmon
   networks:
     netmon:
       external: true
   ```

   Then run `docker compose up -d riot-server` from that directory. Verify with
   `docker exec riot-server wget -qO- http://prometheus:9090/api/v1/status/buildinfo`
   (only if the image has wget; otherwise use rIOt's "Test connection").
   **Image tag unchanged (`:latest`).**
4. [ ] *(D-3, optional, recommended)* Remove `--web.enable-lifecycle` from netmon
   Prometheus, then `docker compose up -d prometheus` in `<netmon-dir>/`.
5. [ ] *(D-5, recommended)* Add `RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS=prometheus:9090,loki:3100`
   to riot-server's environment, then recreate the container.
6. [ ] **rIOt Settings → External signals:**
   - Enable the integration.
   - Prometheus `http://prometheus:9090`, Loki `http://loki:3100`, Grafana
     `<grafana-url>`.
   - Window `6h`, WAN interface `ether1`.
   - netmon host = `<netmon-host>`, VPN host = `<vpn-host>`, gateway = `gluetun`.
   - Build the VPN-required list with "Suggest from VPN host". Cross-check with
     `docker ps --format '{{.Names}}'` in `<vpn-stack-dir>/`.
   - Click **Test connection**, and confirm both sources succeed.
7. [ ] **Create alert rules from templates.** At least "VPN leak detected", and
   optionally the other four.
8. [ ] **Generate the scrape token** (after Phase 4) in Settings → External signals →
   Metrics export. Write it to `<netmon-dir>/prometheus/riot_scrape_token`.
   - The Prometheus image runs as `nobody` (65534): `chown 65534:65534` and `chmod 0400`.
   - Add the file and the pinned cert to `.gitignore` in `<netmon-repo>`.
9. [ ] **Pin rIOt's server cert (D-4).**
   - Fetch `cert_pem` from `GET https://<riot-host>:7331/api/v1/server-cert`. The JSON
     field `cert_pem` exists in self-signed mode.
   - Save it as `<netmon-dir>/prometheus/riot-server.pem`.
   - Check that its SANs include IP `<host-lan-ip>`. Use
     `openssl x509 -noout -ext subjectAltName`.
10. [ ] **Add the scrape job and mounts** to netmon:
    - Add to `prometheus.yml`:

      ```yaml
        # rIOt posture export (POSTURE-001). Bearer token file is gitignored.
        - job_name: riot
          scheme: https
          metrics_path: /metrics
          scrape_interval: 60s
          scrape_timeout: 10s
          authorization:
            type: Bearer
            credentials_file: /etc/prometheus/riot_scrape_token
          tls_config:
            ca_file: /etc/prometheus/riot-server.pem
            server_name: <host-lan-ip>      # or insecure_skip_verify: true if no IP SAN (D-4)
          static_configs:
            - targets: ["riot-server:7331"]
              labels:
                instance: riot
      ```

    - Add to the `prometheus` service volumes in netmon `docker-compose.yml`:

      ```yaml
            - ./prometheus/riot_scrape_token:/etc/prometheus/riot_scrape_token:ro
            - ./prometheus/riot-server.pem:/etc/prometheus/riot-server.pem:ro
      ```

    - Run `docker compose up -d prometheus`. New mounts need a recreate; a reload is
      not enough.
    - Verify in Grafana Explore that `up{job="riot"} == 1` and that
      `riot_posture_tile_status` is present.
11. [ ] **If the Threat tile shows the duplicate-parser note for the netmon host (FR-091):**
    1. List rules that reference `nginx_5xx_count`, `nginx_4xx_count` or
       `nginx_request_count` and are scoped to the netmon host, or global (BR-007).
    2. Decide whether to migrate them.
    3. Only then remove `webservers.access_log` from the netmon host's `/etc/riot/agent.yaml`
       and restart the agent.
12. [ ] **Infrastructure docs.** Update `docs/netmon.md` (riot job, network member) and
    the compose-location notes (riot-server compose path, `netmon` network membership), with the operator's
    approval.
13. [ ] *(D-2, only if accepted)* Make the gluetun control server reachable for the
    runtime VPN check, then configure `vpn_runtime` in Settings.

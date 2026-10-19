# Formal Requirements Document

**Story ID:** POSTURE-001
**Title:** Single-pane security Posture page, read-only netmon signals, external-signal alerting, and a `/metrics` posture export
**Author:** Business Developer Agent
**Date:** 2026-10-17
**Status:** DRAFT

> Non-interactive run. The product owner (the operator) was not available for a live
> interview. The analyst answered every clarification question from the
> grounding brief and the codebase, and recorded each answer as an assumption
> (Section 10). Anything only the operator can decide is in Section 11, with a
> recommended default so the ADD and security stages can proceed.

---

## 1. Executive Summary

rIOt becomes the single place the operator looks to answer two questions: "what is my
security posture right now?" and "what do I do about it?". A new **Posture**
page shows five tiles: **Exposure, Hardening, Patching, Threat activity,
Egress**. Each tile shows a status (`ok` / `warn` / `critical` / `unknown`),
a one-line human reason, and either a rIOt action, a link to the rIOt page
that fixes the problem, or a Grafana deep link that opens the matching netmon
panel with the time range already filled in. Exposure, Hardening, and Patching
come from data rIOt already has. Threat activity and Egress come from a new,
read-only, cached **external-signals integration**: the rIOt server runs a
fixed, curated set of queries against netmon's Prometheus and Loki. rIOt also
becomes the **alerting path** for those signals (netmon has no alerting) by
evaluating them in rIOt alert rules that use the existing ntfy and webhook
channels. rIOt then **exports its own posture** on an authenticated Prometheus
`/metrics` endpoint so Grafana can trend it. rIOt does not ingest logs. Raw
logs, history, attacker IPs, and geo stay in Grafana. The Egress tile includes
a VPN-leak check for containers that must egress via gluetun, which is a
VPN-egress requirement of a service that requires VPN egress.

## 2. Background & Context

- **Current state, rIOt.** rIOt computes a per-device hardening score on demand
  (`scoring.Score`). The score covers the categories access_control, patching,
  network, docker, and system. It is shown on the `/security` page and in
  `SecurityScoreModal`. Open ports, firewall state, nginx security headers,
  nginx rate-limit rules, certificate expiry, and pending security updates are
  already collected. Scores are not persisted. Alert rules are evaluated per
  device when telemetry is ingested. There is a 1-minute periodic-worker
  precedent (`server_error_alert_worker`) that dispatches to a notification
  channel. rIOt has no Prometheus exposition and no service-token
  authentication today.
- **Current state, netmon.** netmon is a separate stack on the netmon host at
  `<netmon-dir>/` on docker network `netmon`. It runs Prometheus 15d,
  Loki 14d, Alloy, mktxp, node-exporter, and cAdvisor. Its Grafana listens
  LAN-only on `<grafana-url>` behind OIDC SSO. The netmon "Security
  Overview" dashboard (`netmon-security`) already defines the threat KPI
  queries this story curates. netmon has no alerting provisioned, so nobody
  is notified when attack or egress signals spike. Prometheus and Loki are not
  published to the host and have no authentication.
- **Problem.** Posture is split across two UIs. Neither one says what to do
  next. Attack and egress signals generate no notifications. A VPN leak by a
  VPN-routed container would expose the house IP to the service that requires VPN egress and would go
  unnoticed. The agent's nginx access-log parser (NGINX-ALERTS) and Alloy both
  tail the netmon host's nginx access log, so the same traffic has two competing
  sources of truth.
- **Direction (from the operator).** rIOt shows current state and next actions. Grafana
  is the drill-down for history and raw logs. rIOt pulls a curated set of KPIs
  and pushes its own posture out on `/metrics`. rIOt does not ingest logs,
  because Postgres bloat is already an active concern (PART-001).
- **Delivery note.** This FRD covers one story with six requirement areas.
  Without changing any requirement, the architect may slice delivery into
  increments in this order: (1) Posture page with the rIOt-native tiles,
  (2) external-signals integration with the Threat and Egress tiles,
  (3) external-signal alerting, (4) `/metrics` export. Area 5 (nginx parser
  decision) and area 6 (privacy) apply across all increments.

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Operator (the operator / any authenticated rIOt dashboard user) | Uses the Posture page, Settings, and Alert Rules | Admin session (`riot_session` via password or OIDC). Reads the Posture page and attacker aggregates, configures the integration, creates and edits rules, generates and rotates the scrape credential, and runs existing rIOt actions. rIOt has no finer role model (A-002). |
| rIOt server | Go server on port 7331 | Reads device telemetry and scores. Runs curated read-only queries against netmon. Holds results in memory only. Evaluates external-signal rules. Serves `/metrics`. Dispatches notifications. |
| rIOt agent | Daemon on each fleet device | Unchanged by this story. Continues to report the telemetry the native tiles use, including container `NetworkMode` and the optional nginx `AccessMetrics`. |
| netmon Prometheus (`prometheus:9090`) | Time-series source for nginx status counters, mktxp router and WAN series, and cAdvisor per-container network series | Queried read-only by rIOt. Scrapes rIOt `/metrics` using the scrape credential (scrape config is a deployment-side change, see Dependencies). |
| netmon Loki (`loki:3100`) | Log store for the netmon host nginx access logs with geo and ASN metadata | Queried read-only by rIOt, using aggregate queries only |
| netmon Grafana (`<grafana-url>`) | Drill-down UI | Opened in the operator's browser through deep links. rIOt makes no server-side calls to Grafana. |
| Notification channels (ntfy, webhook) | Existing rIOt channels | Receive external-signal alert notifications |
| VPN runtime egress source (optional, e.g. gluetun control server) | Reports the public IP seen from inside the VPN namespace | Queried read-only by rIOt **only if** The operator configures it (OQ-004) |

## 4. Functional Requirements

Every requirement uses "must" or "must not" and can be verified on its own.
"Netmon-derived" means the value comes from the external-signals integration.
"rIOt-native" means the value comes from rIOt telemetry and scoring.

### 4.1 Posture page and navigation

- **FR-001:** The system must provide a Posture page at route `/posture` that
  is reachable only with an authenticated admin session. An unauthenticated
  request must get the same login redirect as every other admin page.
- **FR-002:** The system must add a "Posture" entry to the primary navigation.
  Its placement relative to the existing "Security" entry is governed by
  OQ-002 (default: immediately before "Security").
- **FR-003:** The Posture page must render exactly five tiles, always in this
  order: Exposure, Hardening, Patching, Threat activity, Egress.
- **FR-004:** Every tile must display all of the following: (a) a status
  that is exactly one of `ok`, `warn`, `critical`, `unknown`; (b) a reason of
  one human-readable sentence of at most 160 characters that names the
  dominant contributor to the status; (c) at least one next step, which is a
  rIOt action, a link to a rIOt page, or a Grafana deep link; (d) the time the
  tile was evaluated, rendered with `<RelativeTime>`; (e) a source label of
  "rIOt", "netmon", or both.
- **FR-005:** Every tile must show a detail list of the top contributing items
  for its status, capped at 5 items. Examples are the worst devices, the
  failing findings, and the KPIs breaching thresholds. Each item must show
  its own status. When more than 5 items contribute, the tile must show a
  count of the hidden items.
- **FR-006:** The page must show a summary line with the number of tiles in
  each status (for example "1 critical, 1 warn, 2 ok, 1 unknown"). The page
  must not compute or display a single blended "overall score" across the
  five tiles.
- **FR-007:** The page must refresh tile data automatically every 60 seconds
  while the browser tab is visible. It must also provide a manual refresh
  control. Neither the automatic nor the manual refresh must cause any
  external query to run more than once per cache TTL (FR-066).
- **FR-008:** Each tile must load and render independently. A slow, failing,
  or timed-out external source must not delay or block the Exposure,
  Hardening, or Patching tiles, or the rIOt-native sub-signals of the Egress
  tile.

### 4.2 Tile contract: status aggregation, degradation, actions, deep links

- **FR-009:** A tile's status must be derived from its sub-signals using this
  precedence, applied in order: (1) any sub-signal `critical` gives
  `critical`; (2) otherwise any sub-signal `warn` gives `warn`; (3) otherwise
  any configured sub-signal that is `unknown` (unavailable or stale) gives
  `unknown`; (4) otherwise, if at least one sub-signal was evaluated and all
  evaluated sub-signals are `ok`, the tile is `ok`; (5) if no sub-signal is
  configured or evaluable, the tile is `unknown`.
- **FR-010:** Sub-signals that are **not configured** (integration disabled,
  endpoint URL empty, or an optional check not set up) must be excluded from
  aggregation. They must be listed in the tile as "not configured" with a link
  to the Settings section that configures them. They must not by themselves
  make a tile `unknown` when another configured sub-signal of that tile was
  evaluated.
- **FR-011:** A tile must never display `ok` while any of its configured
  sub-signals is `unknown`. In that case FR-009 rule 3 applies, and the reason
  must name the unavailable sub-signal(s).
- **FR-012:** When the external-signals integration is disabled or has no
  endpoint configured, the Threat activity tile must show status `unknown`,
  reason "netmon integration not configured", and an action linking to the
  integration's Settings section.
- **FR-013:** When an external source is configured but the last query for a
  sub-signal failed (connection error, timeout, non-success response, or
  unparseable response), that sub-signal must be `unknown`. The reason must
  name the failing source (Prometheus or Loki) and the failure class
  (unreachable, timeout, HTTP error, invalid response).
- **FR-014:** When a sub-signal is `unknown` because of a failure and a value
  was fetched successfully within the last 15 minutes, the tile must show that
  value labelled "stale", with its age rendered by `<RelativeTime>`, and the
  sub-signal status must stay `unknown`. A value older than 15 minutes must
  not be displayed.
- **FR-015:** A failure of Prometheus must affect only Prometheus-backed
  sub-signals, and a failure of Loki must affect only Loki-backed
  sub-signals.
- **FR-016:** Every action button on the Posture page must invoke an
  **existing** rIOt action. This story must not introduce any new device-side
  action type.
- **FR-017:** Every action that changes state on a device or in rIOt must
  require confirmation through `ConfirmDialog` before it runs. It must report
  success or failure through a toast.
- **FR-018:** Every Grafana deep link must open the dashboard and panel listed
  for that KPI in the deep-link mapping (Section 7.4). It must use the time
  range listed there (`from` and `to` filled in) and must open in a new
  browser tab without passing an opener reference or a referrer to Grafana.
- **FR-019:** Grafana deep links must be built only from the configured
  Grafana base URL plus the fixed mapping in Section 7.4. The system must not
  reflect any user-supplied query-string value into a deep link.
- **FR-020:** When no Grafana base URL is configured, the system must not
  render Grafana deep links. Each affected tile must instead show the
  rIOt-side next step, or the text "Set Grafana base URL in Settings to enable
  drill-down" linking to that setting. The system must never render a broken
  or relative link.

### 4.3 Exposure tile (rIOt-native)

- **FR-021:** The Exposure tile must be computed from the following existing
  scoring findings, evaluated per device from each device's latest telemetry:
  `fw-active`, `no-risky-ports`, `minimal-open-ports`, `tls-certs-valid`,
  `proxy-config-valid`, `security-headers`, `rate-limiting`. It must not
  introduce a second, divergent definition of any of these checks.
- **FR-022:** The Exposure tile status must be `critical` when any device has
  a failed `fw-active`, `no-risky-ports`, or `tls-certs-valid` finding. It
  must be `warn` when no critical condition exists and any device has a failed
  `minimal-open-ports`, `proxy-config-valid`, `security-headers`, or
  `rate-limiting` finding. Otherwise it must be `ok`.
- **FR-023:** The Exposure tile must show the fleet-wide count of devices
  reporting open ports and the total number of open ports. For each listed
  device it must show the device's open port numbers, up to 10 ports, with a
  "+N more" indicator beyond that.
- **FR-024:** For each device with web-server telemetry, the Exposure tile
  detail must show its nginx posture: number of security headers configured,
  whether any rate-limit rule exists, and the soonest certificate expiry in
  days.
- **FR-025:** Each Exposure detail item must link to the device page. Each
  failed finding must also link to its `/learn/:findingId` page.
- **FR-026:** When no device reports security or web-server telemetry, the
  Exposure tile must be `unknown` with reason "No device reports security
  telemetry".

### 4.4 Hardening tile (rIOt-native)

- **FR-027:** The Hardening tile must use each device's existing overall
  security score and grade, computed by the existing scoring engine.
- **FR-028:** The Hardening tile status must follow the existing grade bands
  of the **worst-scoring** device: grade A or B (score of 75 or more) gives
  `ok`, grade C (60 to 74) gives `warn`, grade D or F (below 60) gives
  `critical`.
- **FR-029:** The Hardening tile detail must list the lowest-scoring devices
  (up to 5) with overall score, grade, and the lowest-scoring category. Each
  item must open that device's existing security-score detail
  (`SecurityScoreModal` or the device's security view).
- **FR-030:** The Hardening tile must show the fleet's per-category minimum
  score for access_control, patching, network, docker, and system. A
  category that no device reports must be shown as "no data", not as 0.
- **FR-031:** The Hardening tile must provide a link to the existing
  `/security` page.
- **FR-032:** When no device has a computable score, the Hardening tile must
  be `unknown` with reason "No device has scorable telemetry".

### 4.5 Patching tile (rIOt-native)

- **FR-033:** The Patching tile must show the fleet-wide total of pending
  security updates and the number of devices with at least one pending
  security update (`UpdateInfo.PendingSecurityCount`).
- **FR-034:** The Patching tile status must be `critical` when any device has
  one or more pending security updates **and** has neither rIOt auto-patch
  nor OS unattended-upgrades enabled. It must be `warn` when no critical
  condition exists and either (a) any device has one or more pending security
  updates, or (b) any device reports `reboot_required == true`. Otherwise it
  must be `ok`.
- **FR-035:** Each Patching detail item must show the device, its pending
  security count, whether automatic patching is enabled, and whether a reboot
  is required. It must link to the device page. Where an existing rIOt OS-patch
  action is available for that device, it must offer that action, subject to
  FR-016 and FR-017.
- **FR-036:** When no device reports update telemetry, the Patching tile must
  be `unknown` with reason "No device reports update telemetry".

### 4.6 Device freshness for rIOt-native tiles

- **FR-037:** rIOt-native tiles must evaluate each device from its latest
  stored telemetry snapshot. A device that rIOt currently marks offline must
  still be included, and its detail items must carry a "stale" marker with
  the age of its last telemetry.
- **FR-038:** A device with no telemetry snapshot at all must be excluded from
  rIOt-native tile computation and counted in a per-tile "N devices without
  data" note.

### 4.7 Threat activity tile (netmon-derived)

- **FR-039:** The Threat activity tile must show exactly the KPIs in the
  Threat KPI table (Section 7.3), each computed by the curated query defined
  for it, over the configured threat window (FR-045).
- **FR-040:** KPIs marked "contributes to status" in Section 7.3 must determine
  the tile status, and all other KPIs must not affect it. Each contributing
  KPI's status must be `critical` when its value is at or above its critical
  threshold. It must be `warn` when its value is at or above its warn
  threshold and below its critical threshold. Otherwise it must be `ok`. A
  threshold set to "none" must never trigger.
- **FR-041:** Informational KPIs (blocked probes/min, credential/path probes,
  distinct attacker IPs, top ASNs, router blocked bit rate) must be displayed
  with their values but must not change the tile status.
- **FR-042:** The "top ASNs" KPI must display at most 5 ASN organization names,
  each with its count of blocked (444) requests in the window.
- **FR-043:** Each Threat KPI that has a mapped Grafana panel (Section 7.4)
  must render a deep link to it per FR-018. Each KPI without a mapped panel
  must deep link to Grafana Explore on the matching datasource with that KPI's
  query and time range filled in.
- **FR-044:** The Threat activity tile must state that its data covers the
  configured netmon host only (label: "Source: netmon, host <netmon host
  label>"), so that it is not read as fleet-wide.
- **FR-045:** The threat window must be a Settings value with allowed values
  `1h`, `6h`, `24h` and default `6h`. The same value must be used for KPI
  computation and for the deep-link time range (`from=now-<window>`,
  `to=now`).

### 4.8 Egress tile (netmon-derived plus rIOt-native VPN structural check)

- **FR-046:** The Egress tile must comprise four sub-signals: (a) WAN transmit
  vs 7-day baseline, (b) per-container transmit anomalies, (c) VPN structural
  check, (d) VPN runtime egress check. Sub-signal (c) is rIOt-native. Sub-
  signals (a) and (b) are netmon-derived. Sub-signal (d) uses the optional
  runtime source.
- **FR-047 (WAN baseline):** WAN transmit **current** must be defined as the
  mean transmit bit rate of the configured WAN interface (default `ether1`)
  over the last 1 hour. WAN transmit **baseline** must be defined as the mean
  transmit bit rate of the same interface over the 7 days ending at the start
  of that 1-hour window. The displayed ratio must be current ÷ baseline,
  shown alongside both absolute values in bit/s units.
- **FR-048:** The WAN sub-signal must be `critical` when the ratio is at or
  above the critical threshold (default 4.0). It must be `warn` when the ratio
  is at or above the warn threshold (default 2.0) and below critical.
  Otherwise it must be `ok`. Whenever current is below the absolute floor
  (default 1 Mbit/s), the sub-signal must be `ok` regardless of ratio.
- **FR-049:** When Prometheus holds less than 24 hours of WAN history before
  the current window, the WAN sub-signal must be `unknown` with reason
  "Insufficient baseline history".
- **FR-050 (per-container):** For each running container on the netmon host
  that has cAdvisor network series, the system must compare that container's
  1-hour mean transmit bit rate to its own 7-day mean. A container must be
  `warn` when the ratio is at or above the per-container warn threshold
  (default 3.0) **and** its current rate is at or above the per-container
  floor (default 1 Mbit/s). By default the per-container sub-signal must never
  produce `critical`. Its critical threshold defaults to "none".
- **FR-051:** Containers whose rIOt-reported `NetworkMode` references another
  container (shared network namespace, e.g. `service:gluetun` or
  `container:<id>`) must not be shown with individually attributed transmit
  figures. They must be grouped under the namespace-owning container, with
  the caveat "shared network namespace: traffic is not attributable per
  container".
- **FR-052 (VPN structural):** For every container in the configured
  VPN-required list, on the configured VPN host device, the system must check
  that the container's rIOt-reported `NetworkMode` references the configured
  VPN gateway container (default name `gluetun`).
- **FR-053:** The VPN structural sub-signal must be `critical` when any
  VPN-required container is running with a `NetworkMode` that does not
  reference the VPN gateway container. The reason must name the container(s)
  and the observed network mode.
- **FR-054:** A VPN-required container that is absent from the VPN host
  device's latest telemetry must be shown as "not reporting" and must make the
  structural sub-signal `warn`. A VPN-required container that is present but
  not running must be shown as "not running" and must not, on its own, change
  the sub-signal status.
- **FR-055:** When the VPN-required list is empty or no VPN host device is
  configured, the VPN structural sub-signal must be "not configured" (FR-010).
  The VPN structural check must work when the external-signals integration is
  disabled.
- **FR-056 (VPN runtime):** When a runtime egress source and a house WAN IP
  source are both configured (OQ-004), the system must compare the public IP
  observed from inside the VPN namespace with the house WAN IP. The sub-signal
  must be `critical` when they are equal, `ok` when they differ, and `unknown`
  when either value cannot be obtained.
- **FR-057:** When either runtime source is not configured, the VPN runtime
  sub-signal must be "not configured" (FR-010). The tile must state that only
  the structural check is active.
- **FR-058:** VPN leak detail items must link to the affected container on the
  VPN host device's page in rIOt.

### 4.9 External-signals integration (read-only, curated, cached)

- **FR-059:** Settings must provide an "External signals (netmon)" section with
  the following settings: enable/disable (default disabled), Prometheus base
  URL, Loki base URL, Grafana base URL, threat window (FR-045), WAN interface
  name (default `ether1`), netmon host device, VPN host device, VPN gateway
  container name, VPN-required container list, optional VPN runtime sources
  (OQ-004), and per-KPI warn/critical thresholds and floors (Sections 7.3 and
  7.5).
- **FR-060:** Integration settings must be persisted server-side, must survive
  a server restart, and must take effect within one cache TTL of saving
  without a server restart.
- **FR-061:** Settings validation must reject the following: a URL whose
  scheme is not `http` or `https`; a URL containing user-info credentials; a
  WAN interface name not matching `^[A-Za-z0-9._-]{1,32}$`; a container name
  not matching Docker's container-name character set; a threshold that is
  negative or not a number; a warn threshold above its critical threshold
  (where both are set); a threat window outside `1h|6h|24h`. Each rejection
  must return a field-level error message.
- **FR-062:** The system must execute only the curated query set defined in
  Sections 7.3 and 7.5. The only variable inputs to a curated query must be
  the validated parameters listed in FR-061 (window, WAN interface name,
  container names) and fixed constants. Raw query text must not be editable
  by the operator in this story (see OQ-001).
- **FR-063:** The system must call only the instant-query and range-query APIs
  of Prometheus and Loki, plus one read-only readiness or build-info endpoint
  per source for the connection test. It must never call any administrative,
  lifecycle, write, or deletion endpoint, including Prometheus `/-/quit`,
  `/-/reload`, and `/api/v1/admin/*`, and any Loki push or delete API.
- **FR-064:** Settings must provide a "Test connection" control. It must report
  each configured source separately: success, or failure class (DNS failure,
  connection refused, timeout, HTTP status, unexpected response). The result
  must be delivered as a toast and must also be shown inline.
- **FR-065:** Settings must show, for each source, the time of the last
  successful query and the last error with its time, rendered with
  `<RelativeTime>`.
- **FR-066:** Each curated query result must be cached in server memory for a
  TTL of 60 seconds. While a cached result is fresh, concurrent requests for
  the same query (from page views, alert evaluation, or other consumers) must
  be served from the cache. No more than one upstream execution per query
  must be in flight at any time.
- **FR-067:** Each upstream query must be bounded by a timeout of 10 seconds.
  When the timeout is reached the query must count as failed (FR-013).

### 4.10 External-signal alerting

- **FR-068:** The Alert Rules UI must offer external-signal metrics: one for
  each status-contributing Threat KPI (Section 7.3), the WAN baseline ratio,
  the per-container transmit anomaly (maximum ratio among non-shared
  containers), `vpn_leak` (1 when the VPN structural or runtime check is
  `critical`, otherwise 0), and `external_source_unavailable` (minutes that a
  configured source has been continuously failing).
- **FR-069:** External-signal rules must support the existing rule fields
  name, enabled, metric, operator, threshold, severity, cooldown, notify, and
  template. They must not accept device or container include/exclude scoping.
  Saving such a rule with any device or container scope must be rejected with
  a validation error, and the UI must not present those scope fields for
  these metrics.
- **FR-070:** External-signal rules must be evaluated every 60 seconds by the
  server, independent of page views and of device telemetry ingest.
- **FR-071:** An external-signal rule must not fire when its metric's current
  value is `unknown`. The only rule that represents unavailability is the
  `external_source_unavailable` metric.
- **FR-072:** A triggered external-signal rule must dispatch through the same
  notification channel routing used by existing alert rules (ntfy, webhook)
  and must respect the rule's cooldown.
- **FR-073:** Each notification must include the rule name, severity, KPI
  name, observed value, threshold and operator, and window. When a Grafana
  base URL is configured, it must also include the KPI's Grafana deep link.
- **FR-074:** A triggered external-signal rule must be recorded in rIOt's event
  history, so that the operator can see it after the fact in the existing
  events view. Event attribution (fleet-level, or the configured netmon host
  device) is decided by the architect per OQ-011.
- **FR-075:** The system must provide alert templates for at least: "VPN leak
  detected" (`vpn_leak` ≥ 1, critical), "Exposed probe path answered" (answered
  probes ≥ 1, warning), "401 burst" (401s ≥ 300 in window, warning), "WAN
  egress spike" (WAN ratio ≥ 4.0, warning), and "netmon unavailable"
  (`external_source_unavailable` ≥ 5, warning).
- **FR-076:** The system must not create or enable any external-signal rule
  automatically (see OQ-008). When no enabled rule covers `vpn_leak`, the
  Egress tile must show the notice "No alert rule covers VPN leaks", with an
  action that opens the Alert Rules editor pre-filled from the "VPN leak
  detected" template.
- **FR-077:** When the integration is disabled, external-signal rules for
  netmon-derived metrics must not be evaluated. The Alert Rules list must mark
  them "inactive: integration disabled". `vpn_leak` rules must continue to
  evaluate the VPN structural check.
- **FR-078:** Existing device-scoped alert rules, including `nginx_5xx_count`,
  `nginx_4xx_count`, and `nginx_request_count`, must keep evaluating exactly as
  before this story.

### 4.11 Prometheus `/metrics` posture export

- **FR-079:** The server must serve `GET /metrics` in Prometheus text
  exposition format, parseable by Prometheus v3.0.1.
- **FR-080:** `/metrics` must expose, per device (labels: device ID and
  hostname): overall security score (0 to 100); per-category score and
  per-category maximum (label: category); count of failed findings by category
  and severity; pending security update count; pending update count;
  reboot-required (0/1); and age in seconds of the latest telemetry snapshot.
- **FR-081:** `/metrics` must expose, fleet-wide: each Posture tile's status as
  a numeric gauge (label: tile; 0 = ok, 1 = warn, 2 = critical, 3 = unknown);
  the VPN structural check result (0/1); and, per external source (label:
  source), a 0/1 health gauge.
- **FR-082:** `/metrics` must not re-export netmon-derived KPI values, because
  Prometheus already holds them. It must not expose any IP address, ASN, geo,
  finding description text, or remediation text as a value or label.
- **FR-083:** The metric names, types, help text, and labels must be
  documented in the README.
- **FR-084:** `/metrics` must reject every request that lacks a valid scrape
  credential with HTTP 401 and an empty body. Anonymous access must not be
  possible under any configuration.
- **FR-085:** The scrape credential must authorize `GET /metrics` only. Every
  other endpoint must reject it. Device API keys (`X-rIOt-Key`), device mTLS
  identities, and the legacy `RIOT_API_KEY` must not be accepted by
  `/metrics`.
- **FR-086:** Settings must let the operator generate, rotate, and revoke the
  scrape credential. The secret must be displayed exactly once, at
  generation. It must not be retrievable afterward through the UI or API.
  Rotation must invalidate the previous credential immediately.
- **FR-087:** Generation, rotation, revocation, and repeated authentication
  failures on `/metrics` must be written to the server log without the secret
  value.
- **FR-088:** When no scrape credential exists, `/metrics` must return 401 for
  every request.

### 4.12 Single source of truth for nginx access-log metrics

- **FR-089:** This story must not remove, rename, or change the behavior of the
  agent nginx access-log parser, the `AccessMetrics` telemetry field, the alert
  metrics `nginx_5xx_count` / `nginx_4xx_count` / `nginx_request_count`, or the
  NGINX-ALERTS templates.
- **FR-090:** The Posture page Threat activity tile must take its nginx traffic
  figures only from netmon. It must not display, sum, or blend agent
  `AccessMetrics` values.
- **FR-091:** When the configured netmon host device also reports agent nginx
  `AccessMetrics`, the Threat activity tile must show an informational note
  naming the device and stating that the agent parser duplicates Alloy on that
  host. The note must not change the tile status.

### 4.13 Privacy of attacker data

- **FR-092:** Attacker-derived data shown in rIOt must be limited to aggregate
  counts, the distinct-attacker-IP count, and the top ASN organization names
  with counts. The Posture page must not display individual attacker IP
  addresses, city, country, latitude, longitude, user agents, request paths,
  or referers (see OQ-005).
- **FR-093:** Attacker-derived data must not be written to Postgres, to any
  file, or to browser persistent storage (localStorage, sessionStorage,
  IndexedDB). It must exist only in the server's memory cache and in
  in-flight API responses.
- **FR-094:** Attacker-derived data must be served only on endpoints that
  require an authenticated admin session.
- **FR-095:** Notifications and server log lines must not contain individual
  attacker IP addresses or geo coordinates. Attacker-derived content in
  notifications must be limited to aggregate counts and ASN organization
  names.
- **FR-096:** All cached external data must be discarded on server restart and
  when the integration is disabled.

### 4.14 Pre-existing exposure surfaced by this story

- **FR-097:** `/debug/pprof` must not be reachable without an authenticated
  admin session (or must be disabled unless explicitly enabled by server
  configuration; see OQ-007). This must be in place before riot-server is
  attached to the `netmon` network.

### 4.15 UI foundation

- **FR-098:** The Posture page and the new Settings and Alert Rules elements
  must use the UI-FOUNDATION-001 primitives: `Card` for tiles, `Badge` for
  statuses (with `ok`, `warn`, `critical`, `unknown` added to the shared
  enum→label map as "OK", "Warning", "Critical", "Unknown"), `Button` for
  actions, `<QueryState>` for loading, error-with-retry, and empty states,
  `ConfirmDialog` for mutating actions, toasts for action and test results,
  and `<RelativeTime>` for every timestamp and age.
- **FR-099:** Every status must be shown as a text label as well as a color.
  Status must never be conveyed by color or icon alone.
- **FR-100:** Every scrollable region on the Posture page must use the
  `scrollbar-thin` class.
- **FR-101:** The five tiles must lay out as a single column below 768 px of
  viewport width and must use the multi-column grid at 768 px and above.

## 5. Non-Functional Requirements

- **NFR-001 [Performance]:** With a warm cache and a fleet of up to 100
  devices, the Posture API response(s) must complete in 500 ms or less at
  p95, measured at the server.
- **NFR-002 [Performance]:** With an external source hung until timeout, the
  rIOt-native tiles must still render within 2 seconds of page load.
- **NFR-003 [Performance]:** `GET /metrics` must respond in 1 second or less
  at p95 for up to 100 devices. Device scores used by `/metrics` and by the
  Posture page must each be recomputed at most once per device per 60
  seconds, no matter how many scrapes or page views occur.
- **NFR-004 [Load on netmon]:** In steady state the rIOt server must issue no
  more than one upstream execution per curated query per 60 seconds,
  independent of the number of browser sessions, scrapes, and rules.
- **NFR-005 [Resource bounds]:** The server must cap each upstream response it
  reads at 1 MiB and must process at most the series bounded by the curated
  queries (`topk` limits, single-series aggregates). A response that exceeds
  the cap must be treated as an invalid response (FR-013).
- **NFR-006 [Availability]:** Unavailability of any external source must never
  cause a rIOt page or API to return a 5xx error, crash the server, or prevent
  server startup.
- **NFR-007 [Security]:** The integration must be read-only toward netmon
  (FR-063). Outbound requests must go only to the configured base URLs. The
  security review must assess server-side request forgery risk from
  admin-configured URLs and define any additional destination restrictions.
- **NFR-008 [Security]:** The scrape credential must be compared in constant
  time. Its storage must not allow recovery of the plaintext from the
  database (exact mechanism per security review).
- **NFR-009 [Privacy]:** FR-092 to FR-096 must be verifiable by automated test
  (no attacker fields in persisted rows, logs, notifications, or `/metrics`
  output).
- **NFR-010 [Observability]:** Source failures must be logged at WARN level
  once per transition from healthy to failing, and recovery once at INFO
  level. Per-query failure logging must not exceed one line per source per
  minute.
- **NFR-011 [Compatibility]:** All new API fields and endpoints must be
  additive. No existing API contract, alert rule, or telemetry field must
  change incompatibly. Agents must need no change and no re-install.
- **NFR-012 [Dependencies]:** No new npm dependencies (UI-FOUNDATION-001
  policy). Any new Go dependency must be justified in the ADD.
- **NFR-013 [Testability]:** Every curated query's result parsing and status
  derivation must have unit tests against recorded Prometheus/Loki response
  fixtures, including empty-result, error, timeout, and oversize cases. Time
  must be injected (no direct wall-clock reads in testable code). Test names
  must reference AC IDs.
- **NFR-014 [Storage]:** This story must add no table, column, or row volume
  that grows with log volume or with time. No external-signal time series
  must be persisted in Postgres.
- **NFR-015 [Documentation]:** README must document the Posture page, the
  integration settings, the external-signal metrics, `/metrics` and its
  credential, and the privacy rules. CHANGELOG must record the release.
  the infrastructure repo docs must describe the deploy prerequisites (Section 12),
  changed only with the operator's approval.

## 6. Business Rules

- **BR-001:** rIOt shows current state and next actions. Grafana is the
  drill-down for history and raw logs. rIOt must not ingest, store, or search
  raw log lines.
- **BR-002:** The rIOt-native tiles (Exposure, Hardening, Patching) and the
  VPN structural check must be fully functional with the external-signals
  integration disabled or unreachable.
- **BR-003:** The Posture page must never show a status it cannot justify from
  current data. Missing or failed data shows as `unknown` with a reason,
  never as `ok`.
- **BR-004:** VPN-egress compliance: every container in the VPN-required list must
  egress via the VPN gateway, never the house IP. A structural or runtime
  violation is always `critical`.
- **BR-005:** Alerts fire only from rules the operator has explicitly created
  or enabled. There are no hardcoded fallback thresholds for external-signal
  alerts (consistent with NGINX-ALERTS FR-018).
- **BR-006:** Attacker IP, ASN, and geo data are sensitive. The netmon
  LAN-only stance applies: in rIOt this data is aggregate-only, live-only,
  memory-only, and admin-only.
- **BR-007:** Any future deprecation of the agent nginx access-log parser or
  its alert metrics must first list every alert rule that references
  `nginx_5xx_count`, `nginx_4xx_count`, or `nginx_request_count`, show that
  list to the operator, and require explicit migration or acknowledgment. Any
  silent breakage of an existing rule is forbidden.
- **BR-008:** Deployment infrastructure changes (attaching riot-server to the
  `netmon` network, a Prometheus scrape job for rIOt, gluetun control-server
  reachability) are deploy prerequisites that the operator must approve. They are not
  part of the code deliverable.

## 7. Data Requirements

### 7.1 Entities

| Entity | Persisted? | Contents |
|--------|-----------|----------|
| External-signals config | Yes (server-side settings) | enabled, prometheus_url, loki_url, grafana_base_url, threat_window, wan_interface, netmon_host_device_id, vpn_host_device_id, vpn_gateway_container, vpn_required_containers[], vpn_runtime_source (OQ-004), house_wan_ip_source (OQ-004), thresholds{kpi: warn, critical, floor} |
| Scrape credential | Yes (non-recoverable form) | identifier, created_at, rotated_at, revoked flag. The secret is never stored in plaintext. |
| External KPI reading | **No** (memory only) | kpi_id, value, window, status, source, fetched_at, last_error, last_error_at |
| Source health | **No** (memory only) | source, state, last_success_at, last_error, failing_since |
| Posture tile (API view) | No (computed) | tile_id, status, reason, actions[], deep_links[], evaluated_at, sources[], items[≤5], hidden_count, not_configured[], stale[] |
| External-signal alert rule | Yes (existing alert-rule storage) | existing rule fields. Scoping fields must be empty. |
| External-signal event | Yes (existing event history) | rule, KPI, value, threshold, window, severity, timestamp. No attacker IPs or geo. |

### 7.2 Validation rules

See FR-061. In addition: VPN-required containers form a list of 0 to 50
names with no duplicates. Netmon host device and VPN host device must
reference existing devices or be empty. Thresholds are numbers ≥ 0 or
"none".

### 7.3 Threat KPI table (curated; defaults pending the operator's sign-off, OQ-012)

| KPI | Source | Query basis | Window | Contributes to status | Default warn | Default critical |
|-----|--------|-------------|--------|----------------------|--------------|------------------|
| 5xx responses | Prometheus | netmon-security panel 1 (`increase` of `loki_process_custom_nginx_http_requests{status=~"5.."}`) | threat window | Yes | ≥ 1 | ≥ 25 |
| 401 responses | Loki | panel 4 (`count_over_time({job="nginx", status="401"})`) | threat window | Yes | ≥ 50 | ≥ 300 |
| Answered probes (probe-path regex of panel 3 with a 2xx status) | Loki | new curated query (A-009) | threat window | Yes | ≥ 1 | none |
| Blocked probes/min (444) | Prometheus | panel 2 | last 5 min rate | No | n/a | n/a |
| Credential/path probes | Loki | panel 3 | threat window | No | n/a | n/a |
| Distinct attacker IPs (444) | Loki | panel 5 (count only) | threat window | No | n/a | n/a |
| Top ASNs (444) | Loki | panel 27, limited to top 5 | threat window | No | n/a | n/a |
| Router blocked bit rate | Prometheus | panel 6 | last 5 min rate | No | n/a | n/a |

### 7.4 Grafana deep-link mapping

| KPI | Dashboard UID | Panel | Time range |
|-----|---------------|-------|-----------|
| 5xx responses | `netmon-security` | 1 | now-<threat window> → now |
| Blocked probes/min | `netmon-security` | 2 | now-<threat window> → now |
| Credential/path probes | `netmon-security` | 3 | now-<threat window> → now |
| 401 responses | `netmon-security` | 4 | now-<threat window> → now |
| Distinct attacker IPs | `netmon-security` | 5 | now-<threat window> → now |
| Top ASNs | `netmon-security` | 27 | now-<threat window> → now |
| Router blocked bit rate | `netmon-security` | 6 (router drops by rule: panel 24) | now-<threat window> → now |
| WAN transmit vs baseline | `netmon-security` | 8 (WAN up) | now-7d → now |
| Answered probes | Grafana Explore, datasource `loki` | query prefilled | now-<threat window> → now |
| Per-container transmit | Grafana Explore, datasource `prometheus` | query prefilled | now-7d → now |
| VPN checks | none (rIOt device/container page) | n/a | n/a |

### 7.5 Egress thresholds (defaults)

| Sub-signal | Warn | Critical | Floor |
|-----------|------|----------|-------|
| WAN transmit ratio | ≥ 2.0 | ≥ 4.0 | current ≥ 1 Mbit/s |
| Per-container transmit ratio | ≥ 3.0 | none | current ≥ 1 Mbit/s |
| VPN structural | n/a (not reporting gives warn) | mismatch | n/a |
| VPN runtime | n/a | VPN IP equals house IP | n/a |

### 7.6 Source health state transitions

| From | Event | To |
|------|-------|----|
| not_configured | URL saved and integration enabled | probing |
| probing | query succeeds | healthy |
| probing / healthy | query fails or times out | failing (failing_since set if empty) |
| failing | query succeeds | healthy (failing_since cleared) |
| any | integration disabled or URL cleared | not_configured (cache discarded) |

### 7.7 Tile status decision table (generic, FR-009)

| Any critical | Any warn | Any configured unknown | ≥1 evaluated | Result |
|---|---|---|---|---|
| yes | – | – | – | critical |
| no | yes | – | – | warn |
| no | no | yes | – | unknown |
| no | no | no | yes | ok |
| no | no | no | no | unknown ("not configured") |

## 8. Acceptance Criteria

**AC-001 — Posture page requires auth [FR-001]**
Given: no valid `riot_session`
When: the browser requests `/posture` or the Posture API
Then: the page redirects to login and the API returns 401
And: with a valid admin session, the page renders.

**AC-002 — Navigation entry [FR-002]**
Given: an authenticated operator
When: the primary navigation renders
Then: a "Posture" entry is present, positioned per the resolved OQ-002 (default: immediately before "Security").

**AC-003 — Five tiles in fixed order [FR-003, FR-101]**
Given: the Posture page loads
Then: exactly five tiles render in order Exposure, Hardening, Patching, Threat activity, Egress
And: at a 767 px viewport they render in one column, and at 1024 px in more than one column.

**AC-004 — Tile anatomy [FR-004, FR-098, FR-099]**
Given: any tile in any status
Then: it shows a Badge with a text status label, a reason of 160 characters or fewer, at least one next step, an evaluated-at `<RelativeTime>`, and a source label.

**AC-005 — Detail list cap [FR-005]**
Given: 8 devices contribute to the Hardening tile status
Then: 5 items are shown, each with its own status
And: a "+3 more" indicator is shown.

**AC-006 — Summary without blended score [FR-006]**
Given: tiles in statuses critical, warn, ok, ok, unknown
Then: the summary line reads 1 critical, 1 warn, 2 ok, 1 unknown
And: no single overall posture score is displayed.

**AC-007 — Refresh respects cache [FR-007, FR-066, NFR-004]**
Given: the integration is enabled and the Posture page is open in 3 browser tabs for 5 minutes, with manual refresh clicked 10 times
When: upstream calls are counted at a stub Prometheus/Loki
Then: each curated query was executed no more than once per 60-second interval.

**AC-008 — Native tiles independent of netmon [FR-008, NFR-002]**
Given: a stub Prometheus that accepts connections but never responds
When: the Posture page loads
Then: Exposure, Hardening, and Patching render with final statuses within 2 seconds
And: Threat activity shows a loading state until the 10-second timeout, then `unknown`.

**AC-009 — Aggregation precedence [FR-009, FR-011]**
Given: a tile with sub-signals (ok, unknown-configured)
Then: the tile status is `unknown` and the reason names the unknown sub-signal
And: given (warn, unknown-configured), the tile status is `warn`
And: given (critical, warn, ok), the tile status is `critical`.

**AC-010 — Not-configured sub-signals excluded [FR-010, FR-057]**
Given: VPN runtime sources are not configured, and WAN, per-container, and VPN structural sub-signals are all `ok`
Then: the Egress tile is `ok`
And: the VPN runtime sub-signal is listed as "not configured" with a link to Settings
And: the tile states that only the structural VPN check is active.

**AC-011 — Integration not configured [FR-012, BR-002]**
Given: the integration is disabled
Then: Threat activity is `unknown` with reason "netmon integration not configured" and a link to Settings
And: Exposure, Hardening, and Patching show computed statuses
And: the Egress VPN structural sub-signal is still evaluated.

**AC-012 — Source failure classification [FR-013, FR-015]**
Given: Loki returns HTTP 502 and Prometheus is healthy
Then: Loki-backed KPIs (401s, answered probes, credential probes, attacker IPs, top ASNs) are `unknown` with reason naming Loki and "HTTP error"
And: Prometheus-backed KPIs (5xx, blocked probes/min, router blocked) still show values and statuses.

**AC-013 — Stale value window [FR-014]**
Given: a KPI last fetched successfully 10 minutes ago and the source now failing
Then: the last value is shown labelled "stale" with age "10 minutes ago" and the KPI status is `unknown`
And: at 16 minutes since the last success, no value is shown.

**AC-014 — Actions use existing operations with confirmation [FR-016, FR-017, FR-035]**
Given: a Patching item for a device where an existing OS-patch action is available
When: the operator clicks the action
Then: a ConfirmDialog appears, and only on confirmation is the existing action invoked
And: a success or failure toast is shown
And: the change set introduces no new device-side action type.

**AC-015 — Deep link correctness [FR-018, FR-019, FR-045]**
Given: Grafana base URL `<grafana-url>` and threat window `6h`
When: the operator clicks the 401 KPI deep link
Then: a new tab opens dashboard `netmon-security`, panel 4, with `from=now-6h` and `to=now`
And: the link carries no opener reference and sends no referrer
And: adding arbitrary query parameters to the Posture page URL does not change the link.

**AC-016 — No Grafana base URL [FR-020]**
Given: the Grafana base URL is empty and the integration is otherwise healthy
Then: no Grafana links are rendered
And: affected tiles show "Set Grafana base URL in Settings to enable drill-down" linking to that setting.

**AC-017 — Exposure critical on firewall/risky port/cert [FR-021, FR-022]**
Given: device A fails `fw-active`, and all other devices pass every exposure finding
Then: Exposure is `critical` and the reason names device A and the firewall
And: given instead that a device only fails `security-headers`, Exposure is `warn`
And: given all devices pass all seven findings, Exposure is `ok`.

**AC-018 — Exposure detail [FR-023, FR-024, FR-025]**
Given: a device with 14 open ports and an nginx proxy with 2 security headers, no rate limit, and a certificate with 12 days left
Then: the item shows 10 port numbers and "+4 more", "2 security headers", "no rate limit", and "cert: 12 days"
And: it links to the device page, and the failing findings link to `/learn/:findingId`.

**AC-019 — Exposure unknown [FR-026]**
Given: no device reports security or web-server telemetry
Then: Exposure is `unknown` with reason "No device reports security telemetry".

**AC-020 — Hardening grade bands [FR-027, FR-028]**
Given: worst device scores 74
Then: Hardening is `warn`
And: for worst scores of 75 and 59, Hardening is `ok` and `critical` respectively.

**AC-021 — Hardening detail and links [FR-029, FR-030, FR-031]**
Given: a fleet where no device reports docker data
Then: the docker category minimum shows "no data" (not 0)
And: each listed device opens its security-score detail
And: a link to `/security` is present.

**AC-022 — Hardening unknown [FR-032]**
Given: no device has scorable telemetry
Then: Hardening is `unknown` with reason "No device has scorable telemetry".

**AC-023 — Patching rules [FR-033, FR-034]**
Given: device A has 3 pending security updates and neither auto-patch nor unattended-upgrades
Then: Patching is `critical`, with total 3 and 1 affected device shown
And: if device A had auto-patch enabled, Patching would be `warn`
And: given zero pending security updates fleet-wide but one device with `reboot_required == true`, Patching is `warn`
And: given zero pending and no reboot required, Patching is `ok`.

**AC-024 — Patching unknown [FR-036]**
Given: no device reports update telemetry
Then: Patching is `unknown` with reason "No device reports update telemetry".

**AC-025 — Stale and no-data devices [FR-037, FR-038]**
Given: device B is offline with last telemetry 3 hours old, and device C has never reported
Then: device B's items carry "stale" with age "3 hours ago" and B counts toward statuses
And: device C is excluded and each native tile shows "1 device without data".

**AC-026 — Threat KPI statuses [FR-039, FR-040, FR-041]**
Given: stub values 401s = 120, 5xx = 0, answered probes = 0, blocked probes/min = 900, distinct attacker IPs = 400
Then: the 401 KPI is `warn`, and Threat activity is `warn` with reason naming 401s
And: blocked probes/min and attacker IPs are shown but do not change the status
And: with 401s = 300, the tile is `critical`.

**AC-027 — Answered probe [FR-039, FR-040]**
Given: one access-log entry for `/.env` with status 200 in the window
Then: the answered-probes KPI is 1 with status `warn`, and Threat activity is at least `warn`.

**AC-028 — Top ASNs capped [FR-042, FR-092]**
Given: Loki returns 10 ASN organizations
Then: at most 5 ASN organization names with counts are displayed
And: no IP address, city, country, or coordinates appear anywhere on the page or in the API response.

**AC-029 — Explore link for unmapped KPI [FR-043]**
Given: a Grafana base URL is configured
When: the operator clicks the answered-probes link
Then: Grafana Explore opens on datasource `loki` with the answered-probes query and the threat-window time range prefilled.

**AC-030 — Host scope label [FR-044]**
Given: netmon host device "<netmon-host>"
Then: Threat activity displays "Source: netmon, host the netmon host".

**AC-031 — Threat window setting [FR-045, FR-061]**
Given: the threat window is changed from 6h to 24h and saved
Then: within 60 seconds KPI values reflect a 24h window, and deep links use `from=now-24h`
And: saving `12h` is rejected with a field-level error.

**AC-032 — WAN baseline math [FR-046, FR-047, FR-048]**
Given: a stub 7-day mean WAN tx of 5 Mbit/s and a 1-hour mean of 22 Mbit/s
Then: the ratio is 4.4, the WAN sub-signal is `critical`, and both absolute values are shown
And: with a 1-hour mean of 12 Mbit/s, it is `warn`
And: with baseline 0.2 Mbit/s and current 0.9 Mbit/s (ratio 4.5, below the floor), it is `ok`.

**AC-033 — Insufficient baseline [FR-049]**
Given: Prometheus holds 20 hours of WAN history
Then: the WAN sub-signal is `unknown` with reason "Insufficient baseline history".

**AC-034 — Per-container anomaly and shared namespace [FR-050, FR-051]**
Given: container X (own namespace) at 4 Mbit/s vs a 7-day mean of 1 Mbit/s, and vpn-app-a with `NetworkMode` referencing gluetun
Then: X is `warn`, and the per-container sub-signal never shows `critical` under defaults
And: vpn-app-a shows no individual transmit figure and appears grouped under gluetun with the shared-namespace caveat.

**AC-035 — VPN structural leak [FR-052, FR-053, BR-004]**
Given: the VPN-required list contains `vpn-app-b`, and vpn-app-b is running on the VPN host with `NetworkMode` `bridge`
Then: the VPN structural sub-signal and the Egress tile are `critical`, and the reason names vpn-app-b and "bridge"
And: the item links to vpn-app-b on the VPN host's device page.

**AC-036 — VPN structural not reporting / not running [FR-054]**
Given: `vpn-app-c` is in the list but absent from telemetry, and `vpn-app-d` is present but stopped, and all others pass
Then: vpn-app-c shows "not reporting" and the sub-signal is `warn`
And: vpn-app-d shows "not running" and does not by itself change the status.

**AC-037 — VPN structural without netmon [FR-055, BR-002]**
Given: the integration is disabled, and the VPN list and host are configured
Then: the VPN structural sub-signal is still evaluated
And: with an empty list, it shows "not configured".

**AC-038 — VPN runtime check [FR-056, FR-057]**
Given: both runtime sources are configured, the VPN namespace public IP is 198.51.100.7, and the house WAN IP differs
Then: the VPN runtime sub-signal is `ok`
And: when both IPs are equal, it is `critical`
And: when the runtime source errors, it is `unknown`.

**AC-039 — Settings persistence and live apply [FR-059, FR-060]**
Given: the operator saves Prometheus and Loki URLs and enables the integration
When: the server restarts
Then: the settings are retained
And: without a restart, a URL change takes effect within 60 seconds.

**AC-040 — Settings validation [FR-061]**
Given: inputs `ftp://prometheus:9090`, `http://user:pw@loki:3100`, WAN interface `ether1"}`, warn 300 > critical 50
Then: each is rejected with a field-level error and nothing is saved.

**AC-041 — Curated, read-only querying [FR-062, FR-063]**
Given: a recording stub at the Prometheus and Loki URLs
When: the Posture page, alert evaluator, and connection test run for 10 minutes
Then: every recorded request targets only the instant/range query APIs or the single readiness/build-info endpoint
And: no request targets `/-/quit`, `/-/reload`, `/api/v1/admin/*`, or any Loki push or delete path
And: every query string matches a curated template with only validated parameters substituted
And: no UI or API field accepts raw query text.

**AC-042 — Test connection [FR-064, FR-065]**
Given: Prometheus reachable and the Loki host unresolvable
When: the operator clicks "Test connection"
Then: a toast and inline result show Prometheus success and Loki "DNS failure"
And: Settings shows per-source last success and last error with relative times.

**AC-043 — Query timeout [FR-067]**
Given: a stub that delays 15 seconds
Then: the query is abandoned at 10 seconds and the KPI becomes `unknown` with reason "timeout".

**AC-044 — External rule metrics and no scoping [FR-068, FR-069]**
Given: the Alert Rules editor
When: the operator selects metric `vpn_leak`
Then: device and container scope fields are not shown
And: an API request saving a `vpn_leak` rule with `include_devices` set is rejected with a validation error
And: the metric list contains each status-contributing Threat KPI, the WAN ratio, the per-container ratio, `vpn_leak`, and `external_source_unavailable`.

**AC-045 — Periodic evaluation and dispatch [FR-070, FR-072, FR-073, FR-074]**
Given: an enabled rule "401s ≥ 300" with notify on, cooldown 1800 s, a configured webhook channel, and stub 401s = 350
When: no browser is open and no device telemetry arrives for 2 minutes
Then: exactly one webhook notification is sent within 120 seconds, containing rule name, severity, KPI, 350, "≥ 300", window, and the Grafana deep link
And: an event is recorded in rIOt event history
And: no second notification is sent within the 1800 s cooldown.

**AC-046 — Unknown does not fire; unavailability rule does [FR-071, FR-068]**
Given: a rule "401s ≥ 300" and a rule "`external_source_unavailable` ≥ 5", and Loki down for 6 minutes
Then: the 401 rule does not fire
And: the unavailability rule fires once.

**AC-047 — Templates and no auto-rules [FR-075, FR-076, BR-005]**
Given: a fresh install with the integration enabled
Then: no external-signal rule exists or is enabled
And: the five templates of FR-075 are available
And: the Egress tile shows "No alert rule covers VPN leaks", and its action opens the editor pre-filled from "VPN leak detected".

**AC-048 — Integration disabled rule behavior [FR-077]**
Given: an enabled "401s" rule and an enabled `vpn_leak` rule, with the integration disabled
Then: the 401 rule is marked "inactive: integration disabled" and is not evaluated
And: the `vpn_leak` rule fires when the VPN structural check is `critical`.

**AC-049 — Existing rules unaffected [FR-078, FR-089, NFR-011]**
Given: existing `nginx_5xx_count` and `mem_percent` rules and agent-reported `AccessMetrics`
When: the story is deployed
Then: those rules evaluate and fire exactly as before (existing generator tests pass unmodified)
And: the agent parser, `AccessMetrics`, the three nginx metrics, and the NGINX-ALERTS templates are unchanged.

**AC-050 — /metrics content [FR-079, FR-080, FR-081, FR-083]**
Given: 3 devices with telemetry and a valid scrape credential
When: `GET /metrics` is requested with the credential
Then: the body parses as Prometheus text exposition (validated with promtool or Prometheus v3.0.1)
And: it contains per-device score, per-category score and max, failed-finding counts by category and severity, pending security, pending total, reboot-required, and telemetry age for all 3 devices
And: it contains five tile-status gauges, the VPN structural gauge, and per-source health gauges
And: every metric is documented in the README.

**AC-051 — /metrics excludes sensitive and duplicated data [FR-082, FR-095]**
Given: the integration is enabled with attacker data cached
When: `/metrics` is scraped
Then: no label or value contains an IP address, ASN, geo field, finding description, or remediation text
And: no netmon-derived KPI value (401s, 5xx, WAN rate, etc.) is present.

**AC-052 — /metrics auth [FR-084, FR-085, FR-088]**
Given: no credential configured
Then: `/metrics` returns 401 with an empty body
And: with a credential configured, requests with no credential, a wrong credential, a device `X-rIOt-Key`, the legacy `RIOT_API_KEY`, or a device mTLS identity each return 401
And: the valid scrape credential presented to any other endpoint (e.g. `/api/v1/devices`) is rejected.

**AC-053 — Credential lifecycle [FR-086, FR-087, NFR-008]**
Given: the operator generates a credential
Then: the secret is shown once, and no later UI view or API response returns it
When: the operator rotates it
Then: the old secret returns 401 immediately and the new one returns 200
And: server logs record generation and rotation without the secret value.

**AC-054 — Netmon is sole Posture source; duplicate note [FR-090, FR-091]**
Given: netmon host device "<netmon-host>" reports agent `AccessMetrics` with `status_5xx = 40`, and netmon 5xx = 2
Then: the Threat 5xx KPI shows 2 (no blending with 40)
And: an informational note names the netmon host as a duplicate source, and the tile status is unaffected by the note.

**AC-055 — No persistence of attacker data [FR-093, FR-096, NFR-009, NFR-014]**
Given: the integration has been enabled and queried for 10 minutes
Then: no Postgres table contains attacker IPs, ASNs, or geo values (verified by schema/row inspection test)
And: browser storage contains no attacker data after a Posture page visit
And: after server restart, the first Posture response shows no cached external values until a new fetch completes.

**AC-056 — Admin-only attacker data [FR-094]**
Given: a request to the Posture/Threat API with a device API key or no session
Then: it returns 401 and no attacker aggregates.

**AC-057 — No IPs in notifications or logs [FR-095]**
Given: a triggered external-signal rule and a forced Loki parse error
Then: the notification payload and server log lines contain no IPv4/IPv6 address from attacker data and no coordinates.

**AC-058 — pprof gated [FR-097]**
Given: a request to `/debug/pprof/` without an admin session
Then: it returns 401/404 (per OQ-007 resolution), not profile data.

**AC-059 — UI primitives and accessibility [FR-098, FR-099, FR-100]**
Given: the Posture page source
Then: tiles use `Card`, statuses use `Badge` with labels OK/Warning/Critical/Unknown, actions use `Button`, data states use `<QueryState>`, and timestamps use `<RelativeTime>`
And: every scrollable element has `scrollbar-thin`
And: with CSS colors removed, each status remains identifiable by text.

**AC-060 — Netmon outage never breaks rIOt [NFR-006, NFR-010]**
Given: both sources unreachable at server start and for 30 minutes
Then: the server starts, all rIOt pages return non-5xx responses, and native tiles render
And: the server log contains one WARN per source at failure onset, and no more than one failure line per source per minute.

**AC-061 — Performance budgets [NFR-001, NFR-003, NFR-005]**
Given: 100 seeded devices with warm cache
Then: Posture API p95 ≤ 500 ms and `/metrics` p95 ≤ 1 s over 200 requests
And: device scoring runs at most once per device per 60 s during that load
And: a stub response of 2 MiB is rejected as an invalid response.

## 9. Out of Scope

- Ingesting, storing, searching, or displaying raw log lines in rIOt. Any
  per-request drill-down stays in Grafana.
- Displaying individual attacker IPs, city/country/coordinates, user agents,
  or request paths in rIOt (pending OQ-005).
- Operator-editable PromQL/LogQL query text (pending OQ-001).
- Conntrack, WAN download, and other netmon panels not listed in Section 7.3.
- Seasonal or time-of-day baselines for egress (pending OQ-009). v1 uses a
  flat 7-day mean.
- Per-finding pass/fail series on `/metrics`. Counts by category and severity
  only.
- Authentication to Prometheus, Loki, or Grafana (they have none today).
- Grafana alerting, Grafana provisioning, or any change to netmon dashboards.
- Removing or deprecating the agent nginx access-log parser or its metrics.
- Resolved/recovery notifications, beyond whatever the existing alert system
  already does.
- Merging or removing the existing `/security` page (pending OQ-002).
- Executing any deployment-side change: network attachment, Prometheus scrape
  job, gluetun control-server exposure, or agent YAML edits. These are
  deploy prerequisites for the operator's approval.
- New device-side action types.
- Multi-host netmon (more than one Alloy/Loki host).

## 10. Assumptions

- **A-001:** All grounding-brief facts are authoritative. Items tagged
  UNVERIFIED there remain open (OQ-004, A-012).
- **A-002:** rIOt has a single admin role. "Who can see attacker data" is
  answered as "any authenticated rIOt session", which today means the operator and
  anyone the operator has given a rIOt login or IdP access to rIOt.
- **A-003:** The Exposure tile reuses existing scoring findings rather than
  introducing an open-port allowlist. Overlap with the Hardening network
  category is intentional (different lenses on the same checks).
- **A-004:** Hardening status reuses the existing grade bands (A/B ok, C warn,
  D/F critical) of the worst device.
- **A-005:** Default threat thresholds come from the netmon dashboard where
  defined (401s 50/300). The 5xx critical threshold of 25 and the egress
  ratios are analyst defaults pending OQ-012.
- **A-006:** The netmon dashboard's default range (now-6h) is the default
  threat window.
- **A-007:** Cache TTL 60 s, query timeout 10 s, staleness cap 15 min, and
  evaluation interval 60 s mirror the 1-minute `server_error_alert_worker`
  precedent.
- **A-008:** "Netmon host device" and "VPN host device" are both the netmon host for
  the operator's deployment, but are separate settings so that the design does not
  hardcode a host.
- **A-009:** "Answered probes" (probe-path pattern answered with 2xx) is added
  as a gap fill, because a successful probe is the actionable threat signal.
  Blocked (444) probes are informational. Default severity is warn, because
  some matching paths can be legitimate.
- **A-010:** cAdvisor's container `name` label equals the Docker container
  name that rIOt reports, which lets rIOt join cAdvisor series with rIOt's
  `NetworkMode`.
- **A-011:** The default VPN-required list seeds from the VPN-egress service notes (the list of
  containers documented as sharing the gluetun namespace).
  Exact container names on the netmon host are unverified, so the operator confirms them
  in Settings.
- **A-012:** Whether the netmon host's agent has `webservers.access_log` set is
  unverified. FR-091 detects it at runtime instead of assuming.
- **A-013:** External-signal rule cooldown state is held in memory, as in the
  existing worker precedent. A server restart can therefore produce one
  repeat notification for a still-breaching signal.
- **A-014:** The existing notification routing (which channels a rule
  notifies) applies unchanged to external-signal rules.
- **A-015:** An existing rIOt OS-patch action exists for at least some
  devices. Where it does not, the Patching tile falls back to a device-page
  link (FR-035).
- **A-016:** rIOt is reachable only from the LAN/VPN, not the public internet.
  OQ-006 asks the operator to confirm this.

## 11. Open Questions / Decisions for Product Owner

Each question has a recommended default. Downstream stages proceed on the
default unless the operator overrides it.

- **OQ-001 — Editable query text?** Should operators be able to edit
  PromQL/LogQL in Settings? **Recommend: no.** Ship a fixed curated query set
  in code. Expose only validated parameters (window, WAN interface name,
  container names, thresholds, URLs). This removes the query-injection and
  heavy-query-DoS surface. The security stage weighs in.
- **OQ-002 — Posture vs `/security`?** **Recommend:** Posture is a new
  top-level page placed immediately before "Security". `/security` stays as
  the per-device hardening drill-down, and the Hardening and Exposure tiles
  link into it. Revisit a merge after a few weeks of use.
- **OQ-003 — Fate of the agent nginx access-log parser?** **Recommend: keep it,
  with no deprecation.** It serves non-the netmon host nginx devices and the existing
  `nginx_*` rules at negligible cost. netmon is the sole source for Posture
  Threat KPIs (FR-090). If the FR-091 note appears for the netmon host, the operator removes
  `access_log` from the netmon host's agent YAML as an ops step, after checking that no
  rule scoped to the netmon host depends on `nginx_*` metrics. Any future deprecation
  follows BR-007.
- **OQ-004 — VPN runtime leak source and house WAN IP source?** **Recommend:**
  ship the structural check (rIOt-native) as the v1 guarantee. Make the runtime
  check optional and default to "not configured". Preferred runtime source:
  gluetun's control server `/v1/publicip/ip`, if the operator confirms it is enabled
  and approves rIOt reachability. Preferred house-IP source: a mktxp WAN
  address metric, if one exists (verify), otherwise a manually entered value
  with a visible "manually set" label.
- **OQ-005 — Show individual attacker IPs in rIOt?** **Recommend: no.** Show
  counts plus the top 5 ASN organizations only. IPs and geo stay in LAN-only
  Grafana through deep links.
- **OQ-006 — Is rIOt reachable from outside the LAN?** The repo publishes
  `7331` on all interfaces. **Recommend:** The operator confirms rIOt is LAN/VPN-only
  (A-016). If it is internet-reachable, a follow-up must decide whether Threat
  aggregates require an extra gate.
- **OQ-007 — `/debug/pprof` handling?** **Recommend:** put it behind admin auth
  and also disable it unless a server env flag enables it. Include this in
  this story (FR-097) as a gate before the netmon network join.
- **OQ-008 — Auto-enable a VPN-leak alert?** **Recommend: no auto-created
  rules** (BR-005). Ship the template, and show the FR-076 prompt on the
  Egress tile so that the operator enables it with one click.
- **OQ-009 — Egress baseline method?** **Recommend:** a flat 7-day mean for v1.
  Consider a same-hour-of-day baseline later if false positives appear.
- **OQ-010 — Deploy prerequisite approval.** riot-server must be able to reach
  `prometheus:9090` and `loki:3100`. **Recommend:** attach riot-server to the
  `netmon` docker network as an external network, add a netmon Prometheus
  scrape job for rIOt `/metrics` that uses the scrape credential, and
  document both in the infrastructure repo. Needs the operator's approval. Note: once
  attached, netmon's unauthenticated Prometheus lifecycle endpoints
  (`/-/quit`, `/-/reload`) become reachable from riot-server, and pprof is
  reachable from every netmon container until FR-097 ships.
- **OQ-011 — Event attribution for fleet-level alerts (routed to the
  architect).** **Recommend:** record the event as fleet-level if the event
  model allows it. Otherwise attribute it to the configured netmon host device,
  marking the source as "external signal".
- **OQ-012 — Default thresholds sign-off.** **Recommend:** accept the Section
  7.3 and 7.5 defaults as written. All are editable in Settings.

## 12. Dependencies

| Dependency | Type | Notes |
|------------|------|-------|
| UI-FOUNDATION-001 (`web/src/components/ui/`, `utils/format.ts`, `<RelativeTime>`, toast system) | In-flight story, other worktree | Must merge first, or this story's UI waits for it. The Badge enum map gains the posture statuses. |
| Scoring engine (`internal/server/scoring/engine.go`) | Existing code | Source for Exposure findings, Hardening scores, and `/metrics`. Needs short-lived caching (NFR-003). |
| Security handlers / `/security` page / `SecurityScoreModal` / `/learn/:findingId` | Existing code | Link targets |
| Telemetry (`SecurityInfo`, `UpdateInfo`, `WebServerInfo`/`ProxyServer`, `ContainerInfo.NetworkMode`, patch-status `reboot_required`) | Existing data | Inputs to the native tiles and the VPN structural check |
| Alert rules, generator, `notify.Dispatcher`, `server_error_alert_worker` precedent | Existing code | External-signal rules plug in beside the device-scoped evaluator |
| `admin_config` settings storage / Settings UI | Existing code | Integration settings and credential metadata |
| Auth middleware (`AdminAuth`, device key/mTLS) | Existing code | New scrape-credential auth path, plus the pprof gate |
| netmon Prometheus v3.0.1, Loki 3.3.0, Grafana 11.3.0 dashboard `netmon-security` panel IDs | External system (netmon) | Query semantics and deep-link mapping. A panel-ID change in netmon breaks deep links. |
| Deployment: riot-server attached to the `netmon` network; Prometheus scrape job for rIOt; optional gluetun control-server reachability | Deploy prerequisite, the operator's approval | Not part of the code deliverable (BR-008, OQ-010) |
| PART-001 (Postgres partitioning) | Related constraint | Reason for NFR-014. No dependency on its completion. |
| ADD (architect) and security review | Pipeline | Security must cover OQ-001, NFR-007 (SSRF), NFR-008, FR-084 to FR-088, FR-097, and the netmon lifecycle-endpoint exposure |

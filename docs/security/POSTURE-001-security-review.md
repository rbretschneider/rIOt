# Security Review Report

**Story ID:** POSTURE-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-17
**Verdict:** APPROVED WITH CONDITIONS

One HIGH finding (SEC-POSTURE-004) and six MEDIUM findings. No CRITICAL. The HIGH
finding has a clear, bounded resolution that does not need the architecture reworked.
It blocks **Phase 4 (`/metrics`) and the Prometheus scrape job only**, not Phases 0-3.
All MEDIUM findings have bounded resolutions and are listed as numbered conditions at the
end.

The core design is careful and mostly right: the curated catalog, the poller as the only
upstream caller, the endpoint allowlist, the aggregate-only queries and the ASN-only label
whitelist. Most findings fall into two groups. The first is **false-green failure modes**:
places where "no data" or "bad data" turns into `ok` instead of `unknown`, which conflicts
with BR-003 and BR-004. The second is **trust leaking across the new rIOt/netmon
boundary**, in both directions.

Documents and code read in full: `docs/requirements/POSTURE-001-frd.md`,
`docs/architecture/POSTURE-001-add.md`, the POSTURE-001 grounding brief,
`docs/security/{OIDC-001,NGINX-ALERTS,REBOOT-ETA-001}-security-review.md` (format),
`internal/server/router.go`, `internal/server/middleware/{admin_auth,auth,clientip,ratelimit,cors,logger,setup}.go`,
`internal/server/config.go` (env surface), `internal/server/handlers/settings.go`,
`internal/server/handlers/handlers.go` (`Health`, `RegisterDevice`, `Telemetry`, `writeJSON`),
`internal/server/handlers/auth.go` (cookie attributes), `internal/server/db/device_repo.go`
(API-key storage), `internal/server/auth/keyhash.go`, `internal/server/notify/{ntfy,webhook,dispatcher}.go`
(and the list of backends), `internal/server/events/generator.go` (`createEventAndNotify`,
`compareValue`), `internal/models/{alerts,events,device}.go`, `internal/agent/{register,http_client}.go`,
`docker-compose.prod.yml`, `<netmon-repo>/{docker-compose.yml,prometheus/prometheus.yml,loki/config.yml,alloy/config.alloy}`,
the netmon stack's operational notes, the deployment security notes, the VPN-egress service notes and the IdP app-binding notes.
The deployment host was not contacted. Everything about the netmon host's live state below is tagged
UNVERIFIED unless the repo shows it.

---

## Threat Model Summary

### Assets

| Asset | Where it lives after this story | Why it matters |
|---|---|---|
| Attacker aggregates (counts, distinct-IP count, top-5 ASN org names) | rIOt memory cache, admin `GET /api/v1/posture` responses | BR-006: netmon is LAN-only *because* of this data class |
| VPN exit IP and manually entered house WAN IP | `admin_config` (house IP), memory, admin Posture response | House IP is identifying. VPN exit IP is the identity seen by the service that requires VPN egress |
| Posture integrity (the five tile statuses, `vpn_leak`) | Computed from agent telemetry and netmon responses | The operator's single pane. A false `ok` is the worst failure mode (BR-003). The VPN check is a VPN-egress compliance control (BR-004) |
| Alerting path for external signals | `alert_rules`, the 60 s worker, `notify.Dispatcher` | The *only* alerting netmon has. Silent suppression means attacks go unnoticed |
| Per-device posture data (scores, failed-finding counts, pending security updates, hostnames, device UUIDs) | New: exported on `/metrics` into netmon Prometheus (no auth), visible in Grafana | A "weakest host" map. Device UUIDs turn out to be an authentication input (SEC-POSTURE-004) |
| Scrape token | SHA-256 in `admin_config`, plaintext file in `<netmon-dir>/prometheus/` | Grants `/metrics` read |
| gluetun control API key (optional) | `admin_config.external_signals_secrets`, plaintext | Control over the VPN namespace that serves the VPN-routed applications |
| Fleet control surface reached by any admin session (unchanged) | Existing routes (`/ws/terminal`, `/devices/{id}/commands`, bulk-patch) | An admin session already means RCE across the fleet, so admin-only SSRF and config abuse add little *on top of* a stolen session |

### Threat Actors

1. **Internet attacker against the netmon host's nginx edge.** Controls the request volume, paths,
   user agents and referers that Alloy turns into Loki streams. Can inflate counts at will
   and can try to make queries slow. Cannot reach rIOt directly (assumption A-016: rIOt is reachable only from the LAN or VPN;
   this must be confirmed for each deployment).
2. **Code running in any `netmon` container.** The candidates are an `mktxp` image
   (third-party, `:latest`, holds router credentials), `cadvisor` (privileged, mounts
   `/var/run` and so the Docker socket, which means host root), and Alloy (root, reads
   nginx logs). Today such code can already reach rIOt on `<riot-host>:7331` through the
   host IP. After the join it reaches `riot-server:7331` directly. It can also query and
   push to Prometheus and Loki with no authentication.
3. **Grafana SSO-group member.** Every `<grafana-sso-group>` member is a Grafana
   **Admin** (`netmon/docker-compose.yml` role mapping). rIOt's IdP app is bound
   only to `<riot-admins-group>`, which contains only the operator. So Grafana's
   audience is wider than rIOt's.
4. **Unauthenticated LAN or VLAN host, or a compromised fleet device.** Can reach every
   public rIOt route (`/api/v1/devices/register`, `/health`, `/api/v1/server-cert`,
   pprof before Phase 0).
5. **Holder of a stolen rIOt admin session, or a same-site web origin that can drive the
   admin's browser** (CSRF through a sibling service on host `<host-lan-ip>`).
6. **Third-party notification providers** (ntfy.sh by default, Discord, Slack, Telegram,
   Pushover, SMTP relays). They receive whatever the alert message carries.
7. **Supply chain.** The design adds no new Go or npm modules (positive). The netmon
   images are the operator's existing risk.

### Attack Surface Introduced

- **New outbound HTTP** from riot-server to admin-configured URLs (Prometheus, Loki,
  gluetun), driven by a background poller and by an on-demand "Test connection"
  (`POST /api/v1/settings/external-signals/test`), which takes an **unsaved** config.
- **New inbound unauthenticated-path route** `GET /metrics`, protected by a new
  bearer-token scheme (`ScrapeAuth`). This is the first service-token auth in rIOt.
- **New admin routes:** `GET /api/v1/posture`; `GET`/`PUT /api/v1/settings/external-signals`;
  `POST .../test`; `GET`/`POST`/`DELETE /api/v1/settings/metrics-token`.
- **New network adjacency (deployment ops):** riot-server joins the `netmon` bridge. rIOt
  gains reach to unauthenticated Prometheus (lifecycle API on), Loki (push/delete),
  Alloy, cAdvisor and mktxp. Those containers gain a direct path to riot-server.
- **New data egress:** rIOt posture data flows into Prometheus and Grafana (wider
  audience, no auth). External-signal alerts flow to every enabled notification channel.
- **New trust input:** values and label strings parsed from Prometheus and Loki
  responses, which rIOt does not authenticate, now drive tile statuses and alerts.
- **Changed surface:** `/debug/pprof` becomes 404 by default and admin-gated when
  enabled. This is a reduction.

---

## Severity index

| ID | Sev | Section | Title | Disposition |
|---|---|---|---|---|
| SEC-POSTURE-001 | MEDIUM | A | `/metrics` moves the per-device weakness map into an unauthenticated store with a wider audience | The operator accepts; docs condition C12 |
| SEC-POSTURE-002 | LOW | A | External-signal alerts fan out to every channel, including third-party SaaS | Ops item; docs condition C12 |
| SEC-POSTURE-003 | INFO | A | rIOt reachability beyond the LAN (OQ-006 / D-6) | Ops item |
| SEC-POSTURE-004 | **HIGH** | B | Device UUIDs exported via `/metrics` combine with pre-existing unauthenticated key minting on re-registration, so telemetry for the VPN host can be forged | **C1 (blocks Phase 4)** plus ops item |
| SEC-POSTURE-005 | MEDIUM | B | Non-finite floats (`+Inf` by design, NaN or Inf from upstream) break the Posture JSON and webhook delivery, and NaN evaluates as `ok` | C5 |
| SEC-POSTURE-006 | LOW | B | SSRF via admin-configured URLs: residual blind SSRF and port-scan oracle | C7 plus ops item |
| SEC-POSTURE-007 | LOW | B | Untrusted strings from Loki and agents reach the UI, notifications and `/metrics` | C8 |
| SEC-POSTURE-008 | LOW | B | Network join: lateral reach both ways (Loki push/delete, Prometheus lifecycle, spoofable plain-HTTP answers) | Ops items |
| SEC-POSTURE-009 | LOW | B | Scrape-token details: public id is a substring of the secret, TLS pinning, token file | C9 plus ops item |
| SEC-POSTURE-010 | INFO | B | gluetun control-server reachability (D-2) | Ops item |
| SEC-POSTURE-011 | INFO | C | Query catalog injection claim verified; guardrails for any future editable queries | None now |
| SEC-POSTURE-012 | MEDIUM | D | "Empty result = 0" lets a dead ingest pipeline show `ok` | C2 |
| SEC-POSTURE-013 | MEDIUM | D | A partial source failure silences KPI rules without tripping `external_source_unavailable` | C3 |
| SEC-POSTURE-014 | MEDIUM | D | VPN structural check: stale or future-dated telemetry shows `ok`; "cannot verify" has no status | C4 |
| SEC-POSTURE-015 | MEDIUM | D | CSRF: `SameSite=Lax` is the only defense on the new POST endpoints | C6; follow-up for existing routes |
| SEC-POSTURE-016 | LOW | D | No audit trail for config changes that reduce detection | C10 |
| SEC-POSTURE-017 | LOW | D | External-rule cooldown is unbounded below (0 via PUT), so a 60 s notification storm is possible | C11 |
| SEC-POSTURE-018 | LOW | D | pprof is unauthenticated today and already LAN-reachable. AD-021 fixes it | C13 plus ops item |
| SEC-POSTURE-019 | LOW | D | Pre-existing: the dispatcher persists `err.Error()`, which embeds webhook URLs and their tokens | Follow-up |
| SEC-POSTURE-020 | INFO | D | The answered-probe regex matches raw paths, so percent-encoding evades it | Docs condition C12 |
| SEC-POSTURE-021 | INFO | D | Pseudo-device id, event content, demo fixtures | C12 (demo part) |

---

## Findings

## Section A: Attacker IP, geo and ASN data inside rIOt

### Verified properties (no finding)

Each of the following was checked against the ADD and the code:

- **What crosses the wire.** The catalog (ADD §6.6) asks Loki only for aggregates.
  - T6 returns `count(sum by (ip) ...)`, a single scalar, so no IP leaves Loki.
  - T7 returns `topk(5, sum by (asn_org) ...)`, so only org names and counts leave Loki.
  - The `asnTop` parser reads only `asn_org` and discards every other label. A
    forged extra `ip` label (AC-028 fixture) therefore cannot get through.
  - This is source-side minimization, stronger than filtering after the fact.
    Positive.
- **Who sees it.** `GET /api/v1/posture` sits in the `AdminAuth` group (cookie
  `riot_session`).
  - rIOt's IdP application is bound to `<riot-admins-group>`, which is the operator only. The
    other admin path is the rIOt password.
  - So in practice the audience for attacker aggregates in rIOt is **narrower** than in
    Grafana, where every member of the Grafana SSO group is Admin. BR-006 holds.
- **Persistence.**
  - Memory only (AD-001), with `Cache-Control: no-store` and no react-query persister.
  - The events table receives KPI counts only. ASNs are not an alert metric, so no ASN
    reaches events or notifications.
  - Server logs (`WARN+` is persisted to `server_logs`, `server.go:115`) receive only
    source, query id and failure class (AD-005).
  - Demo fixtures use made-up ASNs and RFC 5737 / RFC 3849 IPs.
  - QA must still prove all of this by test (AC-028, AC-051, AC-055, AC-057).
- **`/metrics` boundary.** FR-082 and AD-019 export no netmon KPI, IP, ASN or geo value.
  Label values are only device id, hostname and fixed enums. The attacker-data side of
  the boundary is correct. The rIOt-data side is not (SEC-POSTURE-001, SEC-POSTURE-004).
- **VPN IPs.** The house IP and the VPN exit IP appear only in the admin Posture
  response. They are not logged, exported or notified (AD-016). Acceptable.

#### SEC-POSTURE-001: `/metrics` moves the per-device weakness map into an unauthenticated store with a wider audience
**Severity:** MEDIUM
**Domain:** Data Exposure / Access Control boundary
**Location:** ADD AD-019, §6.5; FRD FR-080; netmon `docker-compose.yml` (Grafana role mapping), `prometheus.yml` (no auth)

**Description.** Today, rIOt's per-device posture data is admin-only, and admin means
the operator alone, through the IdP group binding.
- The data includes: overall and per-category security score, failed findings by
  severity, pending security updates, reboot-required, telemetry age, hostname and full
  device UUID.
- Phase 4 copies all of it into netmon Prometheus. Prometheus has no authentication.
  Anything on the `netmon` network can read it with
  `GET prometheus:9090/api/v1/query?query=riot_device_pending_security_updates`.
- It also becomes visible to every `<grafana-sso-group>` member, because Grafana maps them all to
  Admin and Explore is unrestricted.
- The data is a ranked list of which hosts are least hardened and which are behind on
  security patches. That is reconnaissance input for an attacker already inside the
  VLAN or inside a netmon container.

**Attack scenario.**
1. A compromised `mktxp` image runs on the `netmon` network.
2. It issues
   `GET http://prometheus:9090/api/v1/query?query=sort_desc(riot_device_pending_security_updates)`.
3. It now has every fleet host's name, how many unpatched security updates each one has,
   and which hosts score D or F. It uses that list to pick its next target.

**Required resolution.**
1. The operator must explicitly accept that `/metrics` data inherits the access control of
   netmon Prometheus and Grafana, not of rIOt. This is a product-owner decision. The
   architect should add it to ADD §15.
2. The README `/metrics` section must state this plainly (C12).
3. SEC-POSTURE-004 must be resolved before the export ships. The device UUID in this
   export is what turns reconnaissance into identity takeover.

No further code change is required if the operator accepts.

**Blocks:** Phase 4 is gated only through C1 (SEC-POSTURE-004). Otherwise this is a the operator decision plus docs.

---

#### SEC-POSTURE-002: External-signal alerts fan out to every enabled channel, including third-party SaaS
**Severity:** LOW
**Domain:** Data Exposure (notifications)
**Location:** ADD AD-018 (message format, `Dispatch` to all channels); `notify/dispatcher.go:79-121` (iterates `ListEnabledChannels`); `notify/ntfy.go` (default `https://ntfy.sh`); backends `discord`, `slack`, `telegram`, `pushover`, `smtp`, `gotify`, `webhook`

**Description.**
- AD-018 reuses `Dispatch`, which sends to **every** enabled channel. There is no
  per-rule channel selection.
- The message carries attack-volume information ("401 responses: 350 over 6h") and the
  internal Grafana deep link (`<grafana-url>/d/netmon-security?...`).
- The ntfy default server is public `ntfy.sh`. On a public server the topic name is the
  only access control, so anyone who knows or guesses the topic can subscribe.
- No attacker IP or geo reaches notifications. The content is within FR-095. What is
  new is the class of signal (live attack telemetry plus internal topology) going to
  third parties.

**Attack scenario.**
1. The operator uses a guessable ntfy.sh topic.
2. A stranger subscribes to it.
3. The stranger learns when the netmon host is being probed, that 401 bursts are tolerated up to
   the threshold, and the LAN-only Grafana address.

**Required resolution.**
- README must state that external-signal alerts go to all enabled channels and what they
  contain (C12).
- Ops item for the operator: confirm each enabled channel is self-hosted or access-controlled
  (an ntfy token or a private server), or accept the exposure.

**Blocks:** Nothing.

---

#### SEC-POSTURE-003: rIOt reachability beyond the LAN (OQ-006 / D-6)
**Severity:** INFO
**Domain:** Infrastructure exposure
**Location:** `docker-compose.prod.yml` (`"7331:7331"`, all interfaces); ADD §15 D-6

**Description.** The repo compose publishes 7331 on `0.0.0.0`. Whether a deployment's
perimeter exposes 7331 is a property of that deployment; A-016 assumes rIOt is
reachable only from the LAN or VPN.

Two things are UNVERIFIED:
- whether the router forwards 7331, and
- whether any nginx vhost proxies rIOt publicly.

If either is true, the Threat aggregates sit one password away from the internet. The
same is true of everything else in rIOt, including terminal RCE, so this story does not
change the conclusion. It only adds a reason to check.

**Attack scenario.** Not applicable while A-016 holds.

**Required resolution.** Ops item for the operator:
- Confirm that no router forward and no public nginx vhost exist for 7331.
- Consider binding the published port to the LAN IP, as netmon does for Grafana
  (`<host-lan-ip>:7331:7331`). Agents on other VLANs still reach that address through the
  router.

**Blocks:** Nothing.

---

## Section B: Trust and auth path to Prometheus and Loki

#### SEC-POSTURE-004: Device UUIDs exported via `/metrics`, combined with pre-existing unauthenticated key minting on re-registration, allow forged telemetry for the VPN host
**Severity:** HIGH
**Domain:** Authentication / Business-logic integrity
**Location:** ADD AD-019 and §6.5 (`device_id` label on every per-device family); ADD AD-015 and AD-007 (VPN check and tiles trust the "latest" snapshot); **pre-existing** `internal/server/handlers/handlers.go:155-211` (`RegisterDevice` re-registration branch), `db/device_repo.go:205-210` (`StoreAPIKey` inserts an *additional* key), `handlers.go:341-342` (snapshot timestamp is agent-supplied)

**Description.** Three facts combine. One is new in this story and two are pre-existing.

1. **Pre-existing: unauthenticated key minting.**
   - `POST /api/v1/devices/register` is public and rate-limited to 10/min per IP.
   - When the `registration_key` admin config is empty, it requires no credential.
   - A body that names an existing `device_id` hits the re-registration branch. That
     branch overwrites the device's hostname. When the request has **no** `X-rIOt-Key`
     header, the branch also generates and stores a **new API key bound to that device**,
     returns it, and keeps all existing keys valid.
   - The branch exists for mTLS-enrolled devices that have no key, but nothing checks
     that the caller is that device.
   - The new key passes `DeviceAuth` on `/api/v1/devices/{id}/telemetry` and
     `/heartbeat`, unless mTLS is enabled, in which case a client cert is also required.
2. **Pre-existing: agent-chosen snapshot timestamps.**
   - The telemetry handler keeps the agent's `Timestamp` and only defaults it when zero.
   - The ADD's projection picks "latest" by `ORDER BY timestamp DESC`.
   - A snapshot dated in the future therefore stays "latest" indefinitely. The real
     agent's later posts never displace it.
3. **New: device UUIDs published.**
   - Until now, device UUIDs were visible only to rIOt admins and to each device itself.
   - Phase 4 publishes **every** device UUID with its hostname to netmon Prometheus,
     where any netmon container can read it unauthenticated (SEC-POSTURE-001), and to
     every Grafana SSO-group Admin.

**This story** also makes a VPN-egress compliance control (BR-004, the VPN structural check) and
every native tile's `ok` depend on the latest snapshot of a named device.

**Attack scenario.** Preconditions are both UNVERIFIED on the netmon host: `registration_key`
empty and mTLS disabled.
1. Code in any netmon container (for example a compromised `mktxp:latest`) queries
   `prometheus:9090/api/v1/query?query=riot_device_security_score{hostname="<netmon-host>"}`.
   It reads the netmon host's `device_id` from the label.
2. It sends `POST https://<riot-host>:7331/api/v1/devices/register` with
   `{"device_id":"<netmon-host-uuid>","hostname":"<netmon-host>"}` and no `X-rIOt-Key`. The response
   contains a fresh `api_key` for the netmon host. The real agent's key keeps working, so nothing
   visibly breaks.
3. It posts `/api/v1/devices/<netmon-host-uuid>/telemetry` with
   `timestamp: 2099-01-01T00:00:00Z`. The snapshot lists every VPN-required container as
   `running` with `NetworkMode: container:<gluetun id>`, the firewall as active, and zero
   pending security updates.
4. From then on the FleetCache always reads the forged snapshot.
   - The VPN structural check reports `ok`, and `vpn_leak` stays 0, whatever the real
     containers do.
   - Exposure and Patching report the netmon host as clean.
   - FR-037's staleness marker never triggers, because the snapshot's age is negative.
5. A real leak follows. Example: a compose edit drops `network_mode: service:gluetun`
   from vpn-app-a. Its traffic leaves from the house IP, the service that requires VPN egress sees it, and no alert
   fires.
6. Separately, the attacker can poll `/heartbeat` as the netmon host. Per the agent's
   `HeartbeatResponse.PendingCommands` contract, it can then drain queued commands (for
   example the `os_update` the Patching tile offers), so the real host never patches.

**Required resolution.** The architect must amend the ADD so that both properties below
hold before Phase 4 ships and before the netmon scrape job is added.

- **(a) Identity.** No value exported on `/metrics`, or otherwise written into the
  netmon stack, may be enough on its own to obtain a credential for a device at any
  unauthenticated rIOt endpoint.
  - The preferred and systemic fix is: *re-registration of an existing device must
    require proof of a credential already bound to that device* (its current API key, or
    a valid mTLS identity for that device). An unauthenticated caller must never receive
    a key for an existing device.
  - If the registration fix is deferred to a tracked follow-up story, the export must
    not carry the full device UUID until it lands.
- **(b) Recency.** For posture evaluation (FleetCache, VPN structural check, staleness
  markers, `riot_device_telemetry_age_seconds`), a snapshot whose agent-supplied
  timestamp is in the future beyond a small clock-skew bound must not count as the
  latest state. Age must never be negative.

**Ops item for the operator (interim, before the network join):** check whether `registration_key`
is set and whether mTLS is enabled on the netmon host. The operator must not change either without first
checking how existing agents re-register (`internal/agent/register.go` sends the device
key as `X-rIOt-Key`).

**Blocks:** Phase 4 and the Section 16 scrape-job steps. Phases 0-3 may proceed.

---

#### SEC-POSTURE-005: Non-finite floats (`+Inf` by design, NaN or Inf from upstream) break the Posture JSON and webhook delivery, and NaN evaluates as `ok`
**Severity:** MEDIUM
**Domain:** Input validation / Availability / Fail-open
**Location:** ADD AD-013 step 4 ("When `baseline <= 0`, the ratio is +Inf"), §5.2.3 (`Ratio *float64`, `Extra map[string]float64`), AD-003 parse rules; `handlers/handlers.go:924-928` (`writeJSON` writes the 200 status before encoding); `notify/webhook.go:47-50` (`json.Marshal(alert)`); `events/generator.go:1403` (`compareValue`)

**Description.**
- Go's `encoding/json` refuses `NaN` and `±Inf`. `writeJSON` has already written
  `200 OK` when `Encode` fails, so the client receives an empty 200 body.
- AD-013 deliberately produces `ratio = +Inf` and returns it in `Extra`, and the alert
  value is that same `+Inf`. Two things break:
  - The **whole** `GET /api/v1/posture` response fails. The page breaks and NFR-006 is
    violated.
  - Webhook delivery of the `ext_wan_tx_ratio` alert fails at marshal time
    (`models.Alert.Value` is a float64). That is exactly the "egress out of nothing"
    case the alert exists for.
- In the other direction, Prometheus legitimately returns the sample strings `"NaN"`,
  `"+Inf"` and `"-Inf"`, and `strconv.ParseFloat` accepts them. "Non-numeric sample"
  (AD-003) does not exclude them.
- A NaN value fails every `>=` comparison, so a KPI or ratio that is NaN evaluates to
  **`ok`** (fail-open). It also never fires a `>=` rule.

**Attack scenario.**
1. An attacker with a foothold on the netmon bridge (see SEC-POSTURE-008: spoofable
   plain HTTP) answers rIOt's T1/T2/T3 queries with `{"resultType":"vector","result":[{"value":[t,"NaN"]}]}`.
2. The Threat tile shows `ok` with NaN, and no 401 or answered-probe rule fires.
3. Separately, and benignly: the operator sets the WAN interface to an idle port. The first time
   current traffic crosses the floor, the Posture page goes blank and the egress webhook
   silently fails.

**Required resolution.**
- **(a)** A non-finite sample from any upstream must be classified `invalid_response`,
  which makes the KPI `unknown`.
- **(b)** No server-computed value placed in an API response, an event, or a
  notification payload may be non-finite. "No baseline" must be represented explicitly,
  not as `+Inf`, and the status rule for that case must be specified.
- **(c)** A status derivation that receives a non-finite input must yield `unknown`,
  never `ok`.
- **(d)** Tests: NaN, `+Inf` and `-Inf` fixtures for every scalar query, and a
  baseline-zero WAN case that round-trips through the Posture handler and the webhook
  backend.

**Blocks:** Implementation of Phase 2 and Phase 3 (condition C5).

---

#### SEC-POSTURE-006: SSRF via admin-configured URLs: residual blind SSRF and port-scan oracle
**Severity:** LOW
**Domain:** SSRF
**Location:** ADD AD-003, AD-006, §6.4, §6.5.2, §10 item 1

**Description.** The designed controls are good.
- http and https only; no userinfo, query or fragment.
- GET only, to compile-time paths.
- No redirects (`ErrUseLastResponse`). `Proxy: nil`.
- 1 MiB cap. Bodies are never reflected; only parsed numbers or ASN strings come back.
- The optional `RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS` allowlist.

The residual risks are these:
- **(a) Port-scan oracle.** `POST .../test` accepts an *unsaved* config and returns
  `class` (`dns_failure` / `connection_refused` / `timeout`), `http_status` and
  `latency_ms` per source. That is a reachability and port-scan oracle for any host
  riot-server can reach. After the join, that includes every netmon container.
- **(b) Path prefix.** The base URL may carry a path. Validation forbids query and
  fragment but not dot-segments or `%2F`. An admin-chosen prefix plus the fixed suffix
  reaches GET endpoints under arbitrary path prefixes.
- **(c) Allowlist normalization.** The allowlist compares `host:port` strings. Without
  normalization, it can be bypassed (an implicit default port, a trailing dot, case) or
  can reject legitimate URLs.
- **(d) Shared state.** If test-connection results ever wrote to the shared cache or
  source-health state, a test against a hostile endpoint would poison live tiles.
- DNS rebinding and cloud metadata (169.254.169.254) are not meaningful here. Only an
  admin sets hostnames, which resolve through Docker DNS, and the netmon host is not a cloud VM.
- The realistic way to trigger (a) is not a malicious admin, who already has fleet RCE.
  It is CSRF from a same-site origin (SEC-POSTURE-015) or a stolen session.

**Attack scenario.**
1. A same-site page rendered in the operator's browser (see SEC-POSTURE-015) form-POSTs a
   text/plain JSON body to `/api/v1/settings/external-signals/test` with
   `prometheus_url: http://<router-ip>:8728`.
2. rIOt dials the router's API port. The response can't be read cross-origin, but it
   maps out internal reachability.

**Required resolution.**
- **(a)** When the allowlist is set, it must apply to test-connection with exactly the
  same check as save time and request time. Comparison must use normalized
  `host:port`: lowercase host, trailing dot stripped, IPv6 brackets normalized, default
  port made explicit from the scheme.
- **(b)** Base-URL paths must not contain dot-segments or encoded path separators.
- **(c)** Test-connection must not write to the shared cache or to source-health state.
- **(d)** When the allowlist is unset, the Settings page must say clearly that any
  destination is allowed. `allowed_hosts_enforced` already exists for this.
- **Ops item (D-5):** The operator sets
  `RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS=prometheus:9090,loki:3100` on the netmon host. This review
  makes it **required for the netmon host deployment** and recommended, not mandatory, as the
  product default. A mandatory allowlist adds little against a full-trust admin but
  closes the CSRF and stolen-session paths.

**Blocks:** Nothing at design level (condition C7).

---

#### SEC-POSTURE-007: Untrusted strings from Loki and agents reach the UI, notifications and `/metrics`
**Severity:** LOW
**Domain:** Output encoding / UI spoofing
**Location:** ADD AD-012 (ASN labels), AD-015 and AD-018 (container name and `NetworkMode` in reasons, events and notifications), AD-019 (hostname labels), AD-010 (`kind:"grafana"` hrefs), AD-015 (item links built from agent-reported container short IDs)

**Description.** Several string inputs are attacker-influenced:
- `asn_org` label values. Loki has no auth and accepts `/loki/api/v1/push` from anything
  on `netmon`, so a forged stream `{job="nginx",status="444"}` with arbitrary `asn_org`
  structured metadata ends up in T7.
- Hostnames, container names, container IDs and `NetworkMode` strings, which are
  agent-reported and fully attacker-controlled under SEC-POSTURE-004.

Current state of the defenses:
- React escapes text, and the repo has no `dangerouslySetInnerHTML` outside one test.
  XSS is therefore not present by default, but nothing in the ADD *requires* text-only
  rendering.
- The 128-rune truncation exists only for Prometheus and Loki labels.
- Bidi and control characters (for example U+202E) allow visual spoofing of reasons such
  as "VPN OK".
- Container IDs are interpolated into SPA paths.

**Attack scenario.**
1. A netmon foothold pushes 10,000 lines with
   `asn_org="All clear - no action needed\u202e"`.
2. The Threat tile's top-ASN list shows that text as the leading entry.

**Required resolution.** Every externally sourced or agent-sourced string shown in
Posture, written into an event or notification, or emitted as a `/metrics` label must:
- be rendered as text only (no HTML sink);
- have control and bidi-override characters stripped;
- be length-clamped (labels 128 runes, hostnames 253);
- be URL-path-escaped when interpolated into an href.

The frontend must render a `kind:"grafana"` or external link only when its scheme is
`http` or `https`.

**Blocks:** Nothing at design level (condition C8).

---

#### SEC-POSTURE-008: Network join gives lateral reach both ways (Loki push/delete, Prometheus lifecycle, spoofable plain-HTTP answers)
**Severity:** LOW
**Domain:** Infrastructure / Trust boundary
**Location:** ADD AD-023, §15 D-3, §16 step 3; netmon `docker-compose.yml`, `loki/config.yml`

**Description.** Each direction is listed separately.

**rIOt to netmon** (impact if riot-server is compromised):
- Prometheus has `--web.enable-lifecycle`, so `POST /-/quit` and `/-/reload` are open.
  - Impact is low. `restart: unless-stopped` brings Prometheus back, and the config
    mount is read-only.
  - `--web.enable-admin-api` and the remote-write receiver are **not** enabled. That is
    good: there is no TSDB delete and no metric injection into Prometheus.
- Loki runs with `auth_enabled: false`.
  - `/loki/api/v1/push` is open. Forged log lines can fabricate or drown signals.
  - The compactor is configured with a delete-request store. Whether the
    log-deletion API is active depends on the deletion mode (UNVERIFIED). If active, it
    allows **evidence deletion** of nginx access history.
- Alloy (`:12345`), cAdvisor (`:8080`, read-only API) and mktxp (`:49090`, metrics only)
  are reachable.
- The incremental risk is low for one reason. A compromised riot-server already holds
  `/ws/terminal` into containers on every enrolled device, including the netmon host's netmon
  containers if the netmon host agent permits terminals. So the join adds little that rIOt
  compromise does not already grant.

**netmon to rIOt:**
- Every netmon container can already reach `<riot-host>:7331` through the host IP,
  because the repo publishes on all interfaces. The join changes the path, not the
  reachable set.
- The surface is the public routes: `/health`, `/api/v1/server-cert`, `/api/v1/auth/*`
  (5/min per IP), `/api/v1/devices/register` (SEC-POSTURE-004), `/api/v1/update/check`,
  `/ws/agent`, and pprof until Phase 0.
- What is genuinely new is that **rIOt now trusts answers** arriving over plain HTTP on
  a Docker bridge. Containers have `NET_RAW` by default, so a container on `netmon` can
  ARP-spoof the bridge and answer rIOt's queries (feeding SEC-POSTURE-005 and
  SEC-POSTURE-012). cAdvisor is privileged with the Docker socket visible, so a cAdvisor
  compromise is host root anyway. mktxp is the plausible unprivileged foothold.

**Attack scenario.**
1. A compromised mktxp container ARP-spoofs `prometheus` on the bridge.
2. It answers every rIOt query with empty vectors.
3. Threat KPIs read 0 and show `ok` (SEC-POSTURE-012), the egress queries go `unknown`,
   and nothing fires.

**Required resolution** (ops items for the operator; outside rIOt code):
1. Remove `--web.enable-lifecycle` (D-3). This review recommends it, with low priority.
2. Confirm Loki's deletion mode, and disable log deletion if it is not used.
3. Track a follow-up for putting an authenticating, read-only proxy in front of
   Prometheus and Loki. The proxy would allow only the query, buildinfo and ready paths
   for rIOt, and push only from Alloy.

On the rIOt side, C2 and C5 limit what spoofed answers can do (they become `unknown`,
never false `ok`).

**Blocks:** Nothing.

---

#### SEC-POSTURE-009: Scrape-token details: public id is a substring of the secret, TLS pinning, token file
**Severity:** LOW
**Domain:** Cryptography & Secrets
**Location:** ADD AD-020, §15 D-4, §16 steps 8-10

**Description.** The core design is sound.
- 256-bit `crypto/rand` token with a distinct prefix.
- SHA-256 at rest. This is acceptable for a 256-bit random secret; a KDF adds nothing,
  and an HMAC pepper is optional and not required.
- Constant-time compare. Bearer header only, never query or cookie.
- `middleware.Logger` logs the path, not the query or headers, so the token never
  reaches logs.
- An empty 401. Valid only on `GET /metrics`. `AdminAuth` reads only the cookie and
  `DeviceAuth` only `api_keys`, so the isolation is structural (verified in the code).
- Immediate in-memory swap on rotation. Per-IP 60/min limiter before auth (key comes
  from `ClientIP`, not spoofable without a trusted proxy).

Gaps:
- **(a)** The public `id` is defined as "the first 8 characters after the prefix". That
  is 48 bits **of the secret itself**, printed in Settings and in INFO logs. The
  remaining 208 bits are still ample, but logging a secret prefix is a pattern that
  should not exist.
- **(b)** With `insecure_skip_verify` (D-4 fallback), a bridge-level MITM
  (SEC-POSTURE-008) captures the token. Impact today is `/metrics` read, which is
  SEC-POSTURE-001 and SEC-POSTURE-004 territory.
- **(c)** The token file in `<netmon-dir>/prometheus/` travels wherever that
  directory is backed up (including any off-box backups).
- **(d)** `POST /settings/metrics-token` (rotate) can be triggered by CSRF
  (SEC-POSTURE-015). Scraping silently breaks until the operator updates the file.

**Attack scenario** (for (a)).
1. A server-log export or screenshot of Settings leaks the id.
2. The attacker's brute-force space drops by 48 bits. This is not practically
   exploitable, which is why the finding is LOW.

**Required resolution.**
- **(a)** The public token id must not be derived from the secret. It must be either
  independent random data or a truncation of the stored hash (C9).
- **(b)** The rotate and revoke endpoints are covered by C6.
- **Ops items for the operator:**
  - Pin rIOt's cert in the scrape job (D-4). Use `insecure_skip_verify` only if the IP
    SAN is absent, and record that acceptance.
  - Create the token file `0400` owned by `65534`, add it to netmon `.gitignore`, and
    decide whether off-box backups of `<netmon-dir>` may carry it.

**Blocks:** Nothing (condition C9).

---

#### SEC-POSTURE-010: gluetun control-server reachability (D-2)
**Severity:** INFO
**Domain:** Infrastructure / Least privilege
**Location:** ADD AD-016, §15 D-2, §16 step 13

**Description.**
- The runtime VPN check needs riot-server to reach gluetun's control server.
- That server also exposes state-changing routes: VPN start and stop, updater, and
  port-forward state. Whether auth is enforced is UNVERIFIED.
- The gluetun namespace serves several VPN-routed applications, some of which may run with
  auth disabled or subnet-bypassed.
- The ADD already recommends shipping structural-only. This review agrees.

**Attack scenario.**
1. D-2 is accepted without scoped auth.
2. A compromised riot-server (or another member of the shared network) calls the VPN
   stop route.
3. The result is disruption of the VPN-routed applications. Depending on kill-switch behavior, it
   may also be a VPN-egress-relevant egress event.

**Required resolution** (ops item, only if D-2 is ever accepted):
- The control server must enforce auth with a role that allows **only**
  `GET /v1/publicip/ip` for rIOt's key.
- The network path must be a dedicated network shared only by riot-server and gluetun.
- The key stored in `external_signals_secrets` (plaintext, consistent with channel
  tokens) must be that scoped key. This review accepts plaintext storage of that scoped
  key.

**Blocks:** Nothing.

---

## Section C: PromQL and LogQL injection surface

#### SEC-POSTURE-011: Query catalog injection claim verified; guardrails for any future editable queries
**Severity:** INFO
**Domain:** Injection
**Location:** ADD AD-004, §6.6, §6.7, AD-010, AD-014

**Verification of the ADD's claim, against the catalog text in §6.6:**

| Substitution point | Where | Verdict |
|---|---|---|
| `W` (threat window) | T1-T3, T5-T7 range selectors `[W]`; deep links `from=now-W` | Go enum of three constants, never a free string. Safe. |
| `I` (WAN interface) | E1-E3 inside `name="I"` (exact-match PromQL matcher) | Regex `^[A-Za-z0-9._-]{1,32}$` excludes `"`, `\`, `}`, whitespace and newline. `.` is literal in `=`. No escape is possible. Safe. |
| Thresholds and floors | Never in queries; compared in Go | Safe. |
| Container names and VPN list | Never in queries; E4-E6 return all names (bounded by `topk`), joined in Go | Safe. Removing the only regex-escaping need was the right call. |
| ASN filters | Constant `asn_org != ""` | Safe. |
| T3 and T5 regex | Constants, byte-identical to netmon panel 3 (T5) | Safe. |
| Explore prefill (`panes`) | Constant T3 log form and constant E4, JSON built with `encoding/json`, `url.Values` encoding, constant datasource UIDs | Safe. |
| Grafana base URL | Validated http/https, no userinfo, query or fragment. `javascript:` and `data:` are impossible. rIOt never redirects, so there is no open redirect. Links open with `noopener noreferrer` and `referrerPolicy=no-referrer` | Safe. SEC-POSTURE-007 adds a frontend scheme check as defense in depth. |
| `PUT` body | `DisallowUnknownFields`, so a smuggled `query` field gives 400 (AC-041) | Safe. |

**Residual observations:**
- **ReDoS.** Prometheus, Loki and Go's `regexp` are all RE2-based and run in linear
  time. The probe-path regexes cannot backtrack catastrophically, even against
  attacker-controlled 8 KB paths.
- **Server-side query timeout.**
  - Prometheus gets `timeout=10s`. Loki gets no server-side bound in the request shape.
  - An abandoned Loki query keeps running until Loki's own query timeout, so a
    pathological slowdown can overlap a few executions.
  - Load is small (the netmon host logs about 1.7 MB/day, and Loki has `max_query_series: 5000`).
    the operator may set an explicit Loki `query_timeout`.
- **Attacker-driven slowness.** This is covered by SEC-POSTURE-013. Request floods can
  push the heavy line-filter queries (T3, T5 at 24h) toward the 10 s timeout, and a large
  set of distinct attacker IPs can make T6 exceed `max_query_series`. Both cases become
  `unknown`. That is honest, but it must be alertable.

**Guardrails if OQ-001 is ever reopened (editable queries).** These are recorded for the
future ADD and are not conditions now:
- Editable query text must stay admin-only and must never be accepted on any endpoint
  outside `AdminAuth`.
- The per-query parser must keep a declared label whitelist. Free-form queries would
  otherwise re-import `ip`, `city` and lat/long and defeat FR-092 at the source.
- Upstream cost limits must be in place before the feature exists:
  - Prometheus `--query.timeout` and `--query.max-samples`;
  - Loki `query_timeout`, `max_query_length` (bounded to at most 7d), `max_query_series`
    and `max_entries_limit_per_query`;
  - rIOt's own 10 s, 1 MiB and 250-series caps must stay.
- Only instant queries; log-returning (stream) queries rejected. Only metric queries, so
  no raw lines enter rIOt (BR-001).
- Any value that is still parameterized must go through a language-correct quoting
  function for the target language. String concatenation into a matcher is not
  acceptable.
- Per-query minimum interval and singleflight must stay. A user-defined query must never
  be executed per page view.

**Blocks:** Nothing.

---

## Section D: Other design-level issues

#### SEC-POSTURE-012: "Empty result = 0" lets a dead ingest pipeline show `ok`
**Severity:** MEDIUM
**Domain:** Business logic / Detection integrity (fail-open)
**Location:** ADD §6.6 `EmptyMeans` column (T1-T8 = `0`), AD-012; FRD BR-003

**Description.**
- T1-T8 map an empty vector to `0`, which yields `ok`.
- An empty vector is ambiguous: it means either "no matching lines" or "nothing is being
  ingested".
- If Alloy stops, its tail breaks, the nginx log path changes, or Loki ingest stalls,
  then T2, T3 and T5-T7 return empty, which reads as 0. T1 goes empty once the
  Alloy-derived counter series goes stale. The Threat tile then shows **`ok`** with
  "0 401s, 0 answered probes", and no rule fires.
- That is exactly the unjustified status BR-003 forbids.
- An internet-facing nginx edge typically receives continuous 444 probe traffic. So
  "zero lines of any kind for N minutes" is a strong, cheap signal that the pipeline is
  broken.

**Attack scenario.**
1. An attacker with a netmon foothold stops or wedges `netmon-alloy`. The same thing can
   happen benignly, for example when log rotation changes the file Alloy is reading.
2. The attacker then probes `/.env` until something answers 200.
3. rIOt shows Threat `ok` throughout, and the "Exposed probe path answered" rule never
   fires.

**Required resolution.**
- The design must distinguish "no matching events" from "no ingest". When the ingest
  pipeline for a source shows no data for a bounded period, every KPI whose `EmptyMeans`
  is `zero` must become `unknown`, with a reason naming the stale pipeline.
- Ingest liveness must itself be derived from a curated, constant query (for example the
  freshness of the `{job="nginx"}` stream or of the Alloy-derived counter). Its exact
  form is the architect's choice.
- The bound must be documented. AC tests must cover "Alloy down, Loki up".

**Blocks:** Implementation of Phase 2 (condition C2).

---

#### SEC-POSTURE-013: A partial source failure silences KPI rules without tripping `external_source_unavailable`
**Severity:** MEDIUM
**Domain:** Business logic / Detection integrity
**Location:** ADD AD-005 ("A source moves to failing when **every** query to it in the latest pass failed"), AD-018 step 2 (skip when not `Known`), FR-071

**Description.**
- FR-071 correctly stops a rule from firing on `unknown`. The only alertable
  unavailability is `external_source_unavailable`.
- But that metric counts minutes since the source went **failing**, and a source only
  goes failing when *all* of its queries fail in a pass.
- The heavy Loki queries (T3 and T5, line filter over 24h) can time out while the cheap
  label-only T2 still succeeds. In that state Loki stays "healthy",
  `external_source_unavailable` stays 0, and the answered-probes rule is blind.
- The Posture tile honestly shows `unknown`, but nobody is notified, and the alerting
  path is the reason netmon integration exists.

**Attack scenario.**
1. An internet attacker sustains a high request rate against the nginx edge. The goal is
   not DoS of nginx. The goal is to grow the 24h Loki working set until T3 and T5 exceed
   10 s.
2. T2 still returns, so Loki is "healthy".
3. While the answered-probe KPI is `unknown`, the attacker runs the real probe. Neither
   the answered-probe rule nor the unavailability rule fires.

**Required resolution.**
- It must be possible to raise a notification when any configured **status-contributing**
  KPI (T1-T3, WAN ratio, container ratio, `vpn_leak`) has been continuously `unknown`
  for at least N minutes, whatever the aggregate source state.
- The mechanism is the architect's choice. Options include a per-KPI unknown-duration
  metric, or redefining `external_source_unavailable` per query.
- The "netmon unavailable" template must cover this case.

**Blocks:** Implementation of Phase 3 (condition C3).

---

#### SEC-POSTURE-014: VPN structural check: stale or future-dated telemetry shows `ok`; "cannot verify" has no status
**Severity:** MEDIUM
**Domain:** Business logic (BR-004 VPN-egress control)
**Location:** ADD AD-015 ("Offline VPN host: items carry `stale`"; "Containers referencing an unresolvable ID show 'cannot verify'"), AD-011 freshness, AD-018 (`vpn_leak` Known)

**Description.**
- **(a) Stale data.** AD-015 evaluates the VPN host's *latest* snapshot even when the host
  is offline, and marks items stale while keeping their status.
  - If the netmon host agent dies, the check keeps reporting the last-known `ok`
    indefinitely. `vpn_leak` stays `Known=true, 0`, so the VPN-leak rule can never fire.
  - FR-037 permits stale devices for the *native tiles*. For a compliance control that
    is a false green.
- **(b) Future dates.** A future-dated snapshot (SEC-POSTURE-004 (b)) pins any state and
  defeats staleness entirely.
- **(c) "Cannot verify".** This state has no specified status. If the implementation
  treats it as compliant, a container whose namespace owner can't be resolved passes.
- **(d) Matching semantics.** These are specified in prose only. A careless
  `HasPrefix(ref, gatewayID)` (wrong direction), a prefix match on names (so
  `gluetun-old` matches `gluetun`), or a short-ID match under 12 characters would all
  produce false `ok`.

**Attack scenario.** This is mostly a benign-failure scenario with consequences for the service that requires VPN egress.
1. The netmon host agent crashes.
2. A week later, the operator edits the VPN stack compose file, and vpn-app-b loses
   `network_mode: service:gluetun`.
3. The Posture page still shows Egress `ok (stale)`, and the VPN-leak rule is silent.
4. The service that requires VPN egress sees traffic from the house IP.

**Required resolution.**
- **(a)** The VPN structural sub-signal must be `unknown` when the VPN host's latest
  telemetry is older than a documented bound (or future-dated beyond skew). `vpn_leak`
  must then be not `Known`, so it falls under C3's alertable-unknown.
- **(b)** "Cannot verify" must contribute `warn` or `unknown`, never `ok`.
- **(c)** Matching must be exact. The reference is either the full gateway ID, a
  reference of at least 12 characters that is a **prefix of** the gateway ID, or an exact
  name match after trimming the leading `/`.
- **(d)** Tests must cover `container:gluetun2`, `service:gluetun-backup`, an 11-character
  ID prefix, the gateway ID being a prefix of the reference, an offline host past the
  bound, and a future-dated snapshot.

**Blocks:** Implementation of Phase 1 (condition C4).

---

#### SEC-POSTURE-015: CSRF: `SameSite=Lax` is the only defense on the new POST endpoints
**Severity:** MEDIUM
**Domain:** Session management / CSRF
**Location:** `handlers/auth.go:42-45` (`SameSite=Lax`, `Secure` per scheme); no Origin, `Sec-Fetch-Site` or Content-Type check anywhere in `internal/server` (verified by search; only `cors.go` and `wsorigin.go` read `Origin`); new routes `POST .../external-signals/test`, `POST`/`DELETE .../metrics-token`, `PUT .../external-signals`

**Description.**
- `SameSite=Lax` blocks the cookie on cross-**site** subrequests, but not on
  **same-site** ones.
- For an IP-addressed deployment, the site is the host. Every web service on
  `<host-lan-ip>`, on any port, is same-site with rIOt:
  - all https services there;
  - also the http ones (for example Grafana and other LAN web UIs) in
    browsers that do not enforce schemeful same-site.
- An XSS or a hostile page in any of them can submit an HTML form to rIOt **with**
  the operator's session.
- Handlers decode JSON without checking Content-Type. A `text/plain` form whose field
  name carries the JSON and whose value closes it decodes cleanly. `DisallowUnknownFields`
  does not stop this, because the attacker can terminate the payload inside the known
  `vpn_runtime_api_key` field.
- Forms can only send GET and POST, so `PUT` is safe, but the new POSTs are not:
  - `.../test` gives the SSRF oracle (SEC-POSTURE-006).
  - `.../metrics-token` rotates the token, which breaks scraping silently.
- **Pre-existing and more severe:** `POST /api/v1/devices/{id}/commands`, `/fleet/bulk-patch`
  and `/fleet/bulk-update` are CSRF-able the same way, through reboots and OS updates.
  That is out of this story's scope, but the systemic issue is flagged here.

**Attack scenario.**
1. A stored XSS in a same-host LAN UI (for example a LAN app with auth bypassed) runs
   in the operator's browser.
2. It auto-submits
   `<form method=POST enctype=text/plain action="https://<riot-host>:7331/api/v1/settings/metrics-token">`.
3. The scrape token rotates. `up{job="riot"}` drops, and the posture gauges vanish from
   Grafana until the operator notices.

**Required resolution.**
- Every **new** state-changing endpoint in this story must reject requests that a
  cross-origin HTML form can produce. Either of these properties is acceptable:
  - the request must carry `Content-Type: application/json`, which forces a CORS
    preflight that `cors.go` denies; or
  - the `Origin` or `Sec-Fetch-Site` header must show the request is same-origin.
- Follow-up story (not a condition here): apply the same property to every existing
  mutating admin route, with `/devices/{id}/commands` and the bulk fleet routes first.

**Blocks:** Implementation, for the new endpoints only (condition C6).

---

#### SEC-POSTURE-016: No audit trail for config changes that reduce detection
**Severity:** LOW
**Domain:** Logging & monitoring
**Location:** ADD §7.5 (settings save), §9 (no logging on save), AD-020 (token lifecycle logged at INFO only)

**Description.** The following changes silently remove detection coverage:
- disabling the integration;
- repointing a source URL;
- emptying the VPN-required list, or changing the VPN host or gateway;
- setting a contributing threshold to `null`;
- revoking the scrape token.

None of them is logged at a level that persists. Only WARN and above reach `server_logs`;
INFO goes to stdout only. A stolen session or a mistaken edit therefore leaves no trace
that the operator can review in rIOt.

**Attack scenario.**
1. A stolen session clears the VPN-required list.
2. The VPN check becomes "not configured", and the Egress tile stays `ok` (FR-010).
3. Nothing in rIOt records when or why.

**Required resolution.**
- Every save that disables the integration, changes a source URL, changes the VPN host,
  gateway or required list, sets a contributing threshold to `null` or raises it, or
  generates, rotates or revokes the scrape token must emit one log line at WARN (which
  persists). The line names the changed fields as old→new summaries.
- The line must contain no secret, token, URL credential or IP address. The house IP
  appears as "changed", not as its value (C10).

**Blocks:** Nothing at design level (condition C10).

---

#### SEC-POSTURE-017: External-rule cooldown is unbounded below, so a 60 s notification storm is possible
**Severity:** LOW
**Domain:** Abuse prevention
**Location:** ADD AD-017 (validation adds the scope check only); `handlers/settings.go:60-81` (`UpdateAlertRule` applies no default and no minimum to `cooldown_seconds`)

**Description.**
- `CreateAlertRule` defaults a 0 cooldown to 900. `UpdateAlertRule` does not.
- An external rule with cooldown 0 fires on every 60 s tick, to **every** enabled channel
  (SEC-POSTURE-002).
- An internet attacker controls the 401 count at will, just by sending failed auth to
  any vhost. Paired with a low cooldown, that becomes a sustained notification flood.
  The effects are third-party rate-limit bans (ntfy.sh, Discord), Pushover quota cost,
  and alert fatigue that hides the real alert.

**Attack scenario.**
1. A rule "401 burst" is saved with cooldown 0.
2. The attacker keeps 401s above 300 per 6h, which costs about one request a minute.
3. rIOt sends 1,440 notifications a day per channel.

**Required resolution.** External-signal rules must enforce a minimum cooldown, on both
create and update. The minimum must be no less than the evaluation interval, and the
architect should pick a value that keeps notification volume sane (C11).

**Blocks:** Nothing at design level (condition C11).

---

#### SEC-POSTURE-018: pprof is unauthenticated today and already LAN-reachable. AD-021 fixes it
**Severity:** LOW (residual after AD-021. The pre-existing exposure was MEDIUM.)
**Domain:** Information disclosure / DoS
**Location:** `router.go:203-205` (pre-existing); ADD AD-021, Phase 0

**Description.**
- The pre-existing `r.Mount("/debug/pprof", pprofHandler())` has no auth. Its comment
  assumes "not exposed beyond the host/network".
- In fact, the repo compose publishes 7331 on all interfaces. pprof is therefore
  reachable **today** from the whole LAN, from VPN clients, and from
  every container on the netmon host through the host IP, including the netmon containers. The
  join does not create that exposure.
- The exposure gives goroutine stacks, `cmdline`, and an unauthenticated CPU-burn DoS
  through `/debug/pprof/profile?seconds=N` and `/trace?seconds=N`.
- AD-021 is the right fix: off by default, explicit 404s registered before the SPA
  catch-all, admin-gated when on. One residual: when enabled, `profile` and `trace` are
  GETs, so `SameSite=Lax` lets a cross-site top-level navigation trigger a long profile.
  That is acceptable because enablement is temporary.

**Attack scenario** (current production).
1. Any VLAN host sends `curl -k https://<riot-host>:7331/debug/pprof/trace?seconds=600`
   in a loop.

**Required resolution.** QA must verify the following (C13):
- With pprof disabled, `/debug/pprof`, `/debug/pprof/`, `/debug/pprof/heap` and
  `/debug/pprof/profile` all return 404, not `index.html`.
- With it enabled, each returns 401 without a session.
- **Ops item:** The operator deploys Phase 0 as soon as it is tagged, independent of the netmon
  join, and confirms the 404 per ADD §16 step 1.

**Blocks:** Nothing (the fix is designed in).

---

#### SEC-POSTURE-019: Pre-existing: the dispatcher persists `err.Error()`, which embeds webhook URLs and their tokens
**Severity:** LOW (pre-existing; amplified by more dispatches)
**Domain:** Secrets in logs
**Location:** `notify/dispatcher.go` `Dispatch` (`logEntry.ErrorMsg = err.Error()` and `slog.Error(..., "error", err.Error())`)

**Description.**
- Go's `*url.Error` text includes the full request URL.
- Discord and Slack webhook URLs, and some ntfy and Gotify configurations, carry their
  secret in the URL.
- A failed send therefore writes the secret into `notification_log.error_msg`, which is
  returned by `GET /settings/notifications/log`, and into `server_logs`, because the
  line is ERROR and persists.
- This story adds a new, periodic dispatch source, so it raises the chance of this
  happening. The ADD itself correctly forbids `err.Error()` from its own client
  (AD-003).

**Attack scenario.**
1. Discord has a transient outage.
2. The ERROR row in `server_logs` contains the full webhook URL, including its token.
3. That row reaches anyone who later reads or exports server logs.

**Required resolution.** Follow-up story (not a condition): dispatcher error logging must
redact URLs and credentials.

**Blocks:** Nothing.

---

#### SEC-POSTURE-020: The answered-probe regex matches raw paths, so percent-encoding evades it
**Severity:** INFO
**Domain:** Detection coverage
**Location:** ADD §6.6 T3; Alloy `path` is the raw request-line token

**Description.**
- Alloy stores the request path exactly as received, without decoding.
- `GET /%2Eenv` or `/.%65nv` is served by many backends as `/.env` but does not match
  `\.env` in T3 (or T5).
- So an `ok` answered-probes KPI is evidence, not proof.

**Attack scenario.**
1. The attacker requests `/%2egit/config`.
2. It answers 200, and T3 stays 0.

**Required resolution.** README and UI help text must state that the KPI covers literal
paths only (C12). Decoding at ingest is a netmon follow-up.

**Blocks:** Nothing.

---

#### SEC-POSTURE-021: Pseudo-device id, event content, demo fixtures
**Severity:** INFO
**Domain:** Business logic / Data hygiene
**Location:** ADD AD-018, AD-024; `handlers.go:221-222`

**Description.**
- **Pseudo-device id.** `external:netmon` cannot collide with a real device. Device IDs
  are server-generated `uuid.New()`. Re-registration looks up the supplied id with
  `GetByID`, and if none is found it allocates a new UUID. So no agent can claim
  `external:netmon`.
- **Registry overlap.** Registry metric names do not overlap any device-emitted metric
  today. A test should keep it that way, so a future device metric named
  `ext_*` or `vpn_leak` cannot cross-trigger external rules.
- **Event content.** Messages carry only KPI labels, values, window, rule name, severity
  and the deep link. For `vpn_leak`, they carry container name and observed mode, which
  are agent-controlled and therefore fall under C8.
- **Demo mode.** AD-024 specifies made-up ASNs and documentation IPs. The ADD's §6.1
  example JSON contains the operator's real Grafana address and hostname, and those must not be
  copied into demo fixtures.

**Attack scenario.** None.

**Required resolution.**
- Add the registry-disjointness test.
- Demo fixtures must use no real hostname, LAN address or ASN taken from the operator's
  environment (C12).

**Blocks:** Nothing.

---

## Positive Observations

- **The poller is the only upstream caller (AD-002).** No browser tab, scrape or rule
  can amplify load on netmon, and a hung netmon can't delay a rIOt response. This is
  structurally the right shape for DoS resistance in both directions.
- **Structural allowlisting of upstream calls (AD-003).**
  - Only GET exists, paths are compile-time constants, and redirects are refused.
  - `Proxy: nil`, a 1 MiB cap applied after decompression, a series cap, and
    per-source concurrency.
  - Raw error strings, which carry the URL and query, never leave the client. This is
    stronger than reviewing call sites.
- **Container names never enter a query (AD-004).** Removing the only input that would
  have needed regex escaping eliminates the injection class instead of mitigating it.
- **Attacker data minimized at the source.** Aggregate-only queries and a one-label
  parser whitelist mean IPs never reach rIOt. This is better than filtering on display.
- **Scrape-token isolation is structural.** Each middleware reads exactly one credential
  type, so FR-085 holds by construction. The empty-body 401, constant-time compare,
  bearer-only token and per-IP limiter are all correct.
- **The FleetCache trims container `Env` and `Labels`** (AD-007). Secrets in container
  environments never reach the Posture API or `/metrics`.
- **Write-only secrets in a separate config key** (AD-006). No serializer of the config
  struct can leak the gluetun key.
- **pprof gating ships first** (AD-021, Phase 0), with explicit 404s that respect the
  SPA catch-all lesson from OIDC-001.
- **Honest degradation in the tile algebra** (FR-009/FR-011, AD-008). `unknown` beats
  `ok` everywhere the design considered. The MEDIUM findings above extend that same
  principle to the places it missed.
- **No new dependencies, no migration, and no change to existing generator code paths.**
  The added attack surface stays small and reviewable.

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.**
- There are no CRITICAL findings.
- The single HIGH finding (SEC-POSTURE-004) has a clear resolution. It needs a
  registration-path property and a recency property, plus a small ADD amendment to
  §6.5 and AD-007/AD-015. It needs no redesign. It gates **Phase 4 and the scrape job
  only**.
- The six MEDIUM findings are bounded properties for the phase that introduces each
  surface. Four are false-green or fail-open gaps (SEC-POSTURE-005, -012, -013, -014).
  The other two are a CSRF property (SEC-POSTURE-015) and a data-classification
  decision for the operator (SEC-POSTURE-001).
- The architect should fold C1-C5 into the ADD text before senior_dev starts the
  relevant phase, because those conditions change specified behavior (status rules, the
  `EmptyMeans` semantics, the alert metric set, and the export label set). The rest are
  implementation and test properties.

### Conditions for implementation (senior_dev must satisfy; QA must verify)

1. **C1 (SEC-POSTURE-004, HIGH; blocks Phase 4 and the scrape job).**
   - (a) No value exported on `/metrics`, or otherwise written into netmon, may be
     enough on its own to obtain a credential for a device at any unauthenticated rIOt
     endpoint. The preferred fix: re-registration of an existing device requires proof
     of a credential already bound to that device, and an unauthenticated caller never
     receives a key for an existing device.
   - (b) For posture evaluation, a snapshot whose agent-supplied timestamp is in the
     future beyond a small skew must not count as latest state, and telemetry age must
     never be negative.
   - Tests must cover both.
2. **C2 (SEC-POSTURE-012; Phase 2).** "No ingest" must be distinguished from "no
   matching events". When a source's ingest is stale beyond a documented bound, every
   `EmptyMeans=zero` KPI is `unknown` with a reason naming the stale pipeline. The
   liveness check is itself a curated constant query. Add a test for "Alloy down, Loki
   up".
3. **C3 (SEC-POSTURE-013; Phase 3).** A notification must be possible when any
   status-contributing KPI (T1-T3, WAN ratio, container ratio, `vpn_leak`) has been
   continuously `unknown` for at least N minutes, independent of aggregate source
   health. The "netmon unavailable" template covers it.
4. **C4 (SEC-POSTURE-014; Phase 1).**
   - The VPN structural sub-signal is `unknown` (and `vpn_leak` not `Known`) when VPN
     host telemetry is older than a documented bound or future-dated.
   - "Cannot verify" is `warn` or `unknown`, never `ok`.
   - Matching is exact as specified in the finding.
   - The edge-case tests listed in the finding are present.
5. **C5 (SEC-POSTURE-005; Phases 2-3).**
   - Non-finite upstream samples are `invalid_response`.
   - No non-finite value appears in any API response, event or notification payload.
   - "No baseline" is represented explicitly with a specified status.
   - Non-finite input to a status derivation yields `unknown`.
   - Tests cover NaN, `+Inf` and `-Inf` fixtures plus a baseline-zero round trip through
     the Posture handler and the webhook backend.
6. **C6 (SEC-POSTURE-015).** `PUT /settings/external-signals`,
   `POST /settings/external-signals/test`, `POST /settings/metrics-token` and
   `DELETE /settings/metrics-token` must reject requests that a cross-origin HTML form
   can produce. That means either requiring `Content-Type: application/json`, or
   verifying `Origin` or `Sec-Fetch-Site`. Add a test with a `text/plain` POST carrying
   a valid session cookie.
7. **C7 (SEC-POSTURE-006).**
   - The allowlist, when set, is enforced identically at save time, at request time and
     in test-connection, against normalized `host:port`.
   - Base-URL paths reject dot-segments and encoded separators.
   - Test-connection never writes to the shared cache or source-health state.
   - Settings clearly indicates when no allowlist is enforced.
8. **C8 (SEC-POSTURE-007).** ASN labels, hostnames, container names and IDs, and
   `NetworkMode` strings must be:
   - rendered as text only;
   - stripped of control and bidi-override characters;
   - length-clamped in the Posture API, events, notifications and `/metrics` labels;
   - URL-path-escaped when placed in hrefs.
   External links render only for `http` or `https` schemes.
9. **C9 (SEC-POSTURE-009).** The public scrape-token id must not be derived from the
   secret. The scrape token, the gluetun API key, upstream URLs and response bodies, and
   any `err.Error()` from the extsignals client must never appear in logs, API
   responses or notifications (AC-053 and AC-057 extended to cover these).
10. **C10 (SEC-POSTURE-016).** Detection-reducing config changes and token lifecycle
    events emit one persisted WARN line each, with field-level old→new summaries and no
    secrets, credentials or IP values.
11. **C11 (SEC-POSTURE-017).** External-signal rules enforce a minimum cooldown, at
    least the evaluation interval, on both create and update.
12. **C12 (docs and fixtures; SEC-POSTURE-001, -002, -020, -021).** The README must
    state that:
    - `/metrics` data inherits netmon Prometheus and Grafana access control (Grafana SSO group
      Grafana Admins and every netmon container can read it);
    - external-signal alerts go to all enabled channels, and what they contain;
    - the answered-probe KPI covers literal (undecoded) paths only;
    - the manually entered house IP goes stale.
    Demo fixtures contain no real hostname, LAN address or ASN.
    Add a registry-disjointness test for external metric names.
13. **C13 (SEC-POSTURE-018; QA verification).** With pprof disabled, `/debug/pprof`,
    `/debug/pprof/`, `/debug/pprof/heap` and `/debug/pprof/profile` all return 404, not
    `index.html`. With it enabled, each returns 401 without a session and 200 with one.

### Deployment ops items for the operator (deploy time, outside rIOt code; nothing here was executed)

1. **Deploy Phase 0 (pprof gate) as soon as it is tagged.** pprof is LAN-reachable today
   through `0.0.0.0:7331`, independent of the netmon join. Verify the 404 by digest and
   with curl (SEC-POSTURE-018).
2. **Before the join and before any scrape job:** check whether `registration_key` is set
   and whether mTLS is enabled on the netmon host (SEC-POSTURE-004 interim mitigation). Review
   agent re-registration behavior before changing either. **Do not add the scrape job
   until C1 has shipped.**
3. **Set `RIOT_EXTERNAL_SIGNALS_ALLOWED_HOSTS=prometheus:9090,loki:3100`** on
   riot-server. Required for the netmon host (D-5, SEC-POSTURE-006).
4. **Confirm 7331 is not router-forwarded and not proxied by a public nginx vhost.**
   Consider binding the published port to `<host-lan-ip>` (D-6, SEC-POSTURE-003).
5. **Remove `--web.enable-lifecycle`** from netmon Prometheus (D-3; low priority). **Confirm
   Loki's deletion mode** and disable log deletion if unused. Consider a follow-up
   read-only, authenticating proxy in front of Prometheus and Loki (SEC-POSTURE-008).
6. **Scrape TLS:** pin rIOt's cert (D-4). Create the token file `0400` owned by `65534`,
   add it to netmon `.gitignore`, and decide whether off-box backups of
   `<netmon-dir>` may carry it (SEC-POSTURE-009).
7. **Accept, or decline, the `/metrics` audience widening:** Grafana SSO-group Admins and
   all netmon containers would see per-device scores and patch backlog
   (SEC-POSTURE-001). Product-owner decision; add to ADD §15.
8. **Review enabled notification channels.** External-signal alerts reach all of them.
   Ensure ntfy is self-hosted or token-protected, or accept the exposure
   (SEC-POSTURE-002).
9. **Keep the VPN runtime check "not configured" (D-2).** If it is ever enabled, scope
   the gluetun control-server auth to `GET /v1/publicip/ip` only, on a dedicated
   riot-server and gluetun network (SEC-POSTURE-010).

### Follow-up stories (recorded, not conditions)

- CSRF property (C6) applied to all existing mutating admin routes, with
  `/devices/{id}/commands` and the bulk fleet routes first (SEC-POSTURE-015).
- Dispatcher error redaction for webhook URLs and credentials (SEC-POSTURE-019).
- Read-only authenticating proxy for netmon Prometheus and Loki (SEC-POSTURE-008).
- If not taken in C1: the systemic re-registration fix (SEC-POSTURE-004).

**Routing:** invoke `senior_dev` with this report alongside the ADD. The architect should
first fold C1-C5 into the ADD (§6.5 label set, AD-007/AD-015 recency and VPN status
rules, §6.6 `EmptyMeans` semantics plus the liveness query, AD-018 alertable-unknown,
AD-013 "no baseline" representation). QA verifies C1-C13. The operator owns the nine deployment ops
items.

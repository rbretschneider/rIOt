# Security Review Report

**Story ID:** DOCKER-UX-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-18
**Verdict:** APPROVED WITH CONDITIONS (final, design phase closed at ADD rev 3.3; see "Closing check (rev 3.3)" for the consolidated conditions C-1..C-59)

There are no CRITICAL findings. The design gets the large decisions right:

- redaction runs on the host, before data leaves it, and fails closed;
- reads, live logs and stack actions name resources, never paths;
- prune never calls Docker's `*Prune` APIs and removes only listed items;
- every new agent-to-server message is scoped to the authenticated device's socket;
- live logs are relayed without storage, with non-blocking fan-out;
- no new privilege, sudoers rule or dependency is introduced.

There are three HIGH findings. Each has a clear resolution that does not redesign the
story, but each must be written into the ADD before the phase it gates starts:

- **SEC-001 (gates P1).** The new fleet-wide destructive POST endpoints can be driven by
  same-site cross-site request forgery (CSRF) and same-site framing. The server-side typed
  confirmations (AD-009) do not help here, because an attacker can supply every expected
  value. A "same-site" foothold is any other web app on the same host IP (any port) or under
  the same parent domain. That is the normal homelab layout, and rIOt's own managed
  containers are those apps.
- **SEC-002 (gates P3).** Compose stack identity is built from container labels, and those
  labels can be forged. The agent stack registry and `docker_stacks` both take the last
  observation that arrives. A standalone container from a hostile image can therefore
  redirect a real stack's Up, Redeploy or Update, and today's stack auto-update, to a compose
  file the attacker can write. The result is root on the host, with rIOt as the confused
  deputy.
- **SEC-003 (gates P1, P2, P3).** The redaction rules have concrete bypasses in common
  formats:
  - the URL-credential regex misses `redis://:pw@h`, any password containing `/`, and splits
    on an `@` inside the password;
  - D-27 shows non-sensitive compose `environment:` values, so `DATABASE_URL` leaks whenever
    the regex misses;
  - camelCase keys (`clientKey`, `signingKey`) and `*_PW` keys pass;
  - compose `command:` and inspect `Healthcheck`/`LogConfig` are not covered;
  - multi-line `.env` values can surface as visible *keys*.

There are also seven MEDIUM, seven LOW and three INFO findings. All MEDIUM findings and the
LOW findings marked "mandatory" are implementation conditions, listed in "Mandatory
conditions".

**Read in full:**
- `docs/requirements/DOCKER-UX-001-frd.md`
- `docs/architecture/DOCKER-UX-001-add.md` (every AD, §8, §11, §12 Security, §13.3, §16, §17,
  §18)
- the orchestrator grounding brief
- prerequisite copies:
  - CMD-LIFECYCLE-001 ADD and security review;
  - UI-FOUNDATION-001 security review;
  - AGENT-PRIV ADD (manifest scope);
  - PART-001 ADD and bloat notes
- existing reviews for format and history: CMD-LIFECYCLE, FLEET-DASH, OIDC-001 (SameSite and
  `CheckWSOrigin` analysis), SYS-EXPORT-001

**Code inspected at `main@a7b9f9e` (verified, not taken from the ADD):**
- `internal/server/router.go` (route groups, `deviceAuth`, admin group, pprof)
- `internal/server/middleware/admin_auth.go`, `wsorigin.go`, `cors.go`, `clientip.go`
- `internal/server/handlers/auth.go` (`issueSessionCookie`: claims and cookie attributes),
  `oidc.go` (`OIDCCallback`: any IdP subject is granted admin), `internal/server/oidc/identity.go`,
  `service.go` (claims)
- `internal/server/handlers/terminal.go` (agent WS read loop, browser relay, audit)
- `internal/server/handlers/handlers.go`:
  - `Telemetry` and the post-work;
  - `GetDevice`, `GetDeviceHistory`, `GetDeviceContainers`, `GetContainerDetail`,
    `GetContainerLogs`;
  - `hasRiotServerContainer`, `stripHeavyTelemetry`, `extractPrimaryIP`, `DeviceSummary`
- `internal/server/handlers/auto_update.go` (stack auto-update work directory source)
- `cmd/riot-server/migrations/000001_init.up.sql` (`terminal_sessions`: FK cascade)
- `internal/agent/ws_client.go`, `terminal.go` (including the `_host` shell), `docker_update.go`
- `internal/agent/collectors/docker.go` (labels and ports), `docker_labels.go`
  (`ParseRiotLabels`, `InferRepoURL`)
- `scripts/install.sh`:
  - service user and docker group;
  - sudoers drop-in;
  - systemd unit (`ProtectSystem`, `ReadWritePaths`, no `ProtectHome`)
- `web/src` URL sinks (`href={c.repo_url}`), and `react-dom@19.2.4` `sanitizeURL`
  (`javascript:` hrefs are neutralised; verified in the installed build)

---

## Threat Model Summary

### Assets

| Asset | Where | Why it matters |
|---|---|---|
| Host integrity on every Docker host | docker socket (agent user in `docker` group = root-equivalent); `docker compose` invocations by the agent | Any compose `up` on attacker-chosen YAML is host root (privileged, `/:/host`). This story adds Up, Redeploy and Down, and a stack registry |
| Container secrets | inspect `Config.Env`, labels, compose files, `.env`, `Cmd`/`Healthcheck`/`LogConfig`, log lines, step output | BR-004 promises they never reach the browser. Labels are stored and broadcast verbatim today |
| Workload availability and data | containers, stacks, images, volumes, networks | Bulk Stop (100 targets, many hosts), stack Down, prune and volume removal are new single-request blast radii |
| Admin session | `riot_session` HS256 JWT, `HttpOnly`, `SameSite=Lax`, 24 h, no server-side revocation | A session, or a request ridden on it, drives fleet-wide destructive actions and the `_host` terminal |
| Audit and attribution | `commands` (+actor), `terminal_sessions` (generalised), `events` | This is the only record of who stopped, pruned or opened a shell. The story adds the first retention to it |
| Server availability | Postgres (`docker_stacks`, `docker_inventory`, `commands`, `command_output`, `terminal_sessions`), relay memory, browser tab | The PART-001 bloat history: one device credential must not grow storage without bound |
| Agent host files | anything readable by the agent user outside the agent's own config and state | Compose and `.env` reads turn the agent into a file reader steered by labels |

### Threat Actors

1. **Same-site web foothold (primary for SEC-001).** Script running on any origin that is
   same-site with the rIOt dashboard while the operator is signed in. Examples: another
   container's web UI at `http://192.0.2.10:8080` when rIOt is `http://192.0.2.10:<port>`
   (SameSite ignores ports), or `app.example.internal` when rIOt is `riot.example.internal`.
   It may be an XSS in a third-party app, a malicious image's own UI, or a page the operator
   opens through this story's new port links. Under `SameSite=Lax` the cookie rides this
   actor's POSTs.
2. **Hostile image author or supply chain (primary for SEC-002, SEC-003, SEC-005, SEC-011).**
   Controls image `LABEL`s, which containers inherit. That includes
   `com.docker.compose.*` keys on containers that compose did not create,
   `org.opencontainers.image.source`, and `riot.*`. Also controls container log output and
   anything the container writes to its writable bind mounts. Its code runs inside a
   container, not on the host.
3. **Compromised enrolled agent (valid `X-rIOt-Key`).** Can send any telemetry, capability
   token, inventory, event, `logs_*` or `docker_read_result` on its own socket. It already
   owns its host. It wants to degrade the server or other hosts, or to mislead the operator.
4. **Chatty or broken container (non-malicious).** Produces log volume, very long lines or
   no-newline output that stresses the agent, relay and browser.
5. **Authenticated operator (trusted).** In scope only for mistakes (self-takedown, wrong
   prune) and for attribution among several OIDC identities. Any subject the IdP admits is
   granted admin (`OIDCCallback`).
6. **Unauthenticated network attacker.** Every new route sits in the admin group. Relevant
   only through the routes inventoried in "Attack Surface Introduced".

### Attack Surface Introduced

- **New admin HTTP routes:**
  - `GET /docker/hosts`;
  - `POST /docker/bulk-actions`;
  - `GET .../inspect`, `.../compose`, `.../compose/files/{index}`, `.../env` (agent
    round-trips plus audit writes);
  - `GET`/`POST` stacks list, detail, `actions` and `forget`;
  - inventory `GET`, `refresh`, `prune/preview`, `prune/execute`;
  - `GET /docker/activity` and its detail route.
- **Extended routes:**
  - `GET /fleet/containers`;
  - `POST /devices/{id}/commands` (`confirm_host`).
- **New browser WebSocket:** `GET /ws/docker/logs/{deviceId}`.
- **New agent WS message types:** `logs_start/stop/data/event` and
  `docker_read`/`docker_read_result`; `command_result` gains `target_results` and `steps`.
- **New agent actions:**
  - `docker_stack_pull/redeploy/restart/down/up`;
  - `docker_remove_items`;
  - `docker_inventory_refresh`;
  - container locks on start/stop/restart.
- **New agent file reads** (compose, `.env`) and a persisted agent registry
  (`/var/lib/riot/docker-stacks.json`).
- **New stored data:**
  - `docker_stacks`, `docker_inventory`;
  - `commands.actor`, `actor_kind`, `steps`, `target_results`;
  - `command_output` `steps` rows;
  - `events.command_id`;
  - generalised `terminal_sessions`.
- **New JWT claims:** `riot_auth`, `riot_actor`.
- **New retention purges:** `commands`, `command_output`, `terminal_sessions`.
- **New agent-controlled values rendered as links:** port host and IP, primary IP, repository
  URL in the new ⋯ menu.

---

## Coverage of the mandated review areas

Each area below lists its findings, or states why there is none.

### (a) Live log streaming authorization

- **Browser WS authentication.** Verified: the route will sit in the `r.Group(adminAuth)`
  block, as `/ws/terminal` does (`router.go:207-213`). `AdminAuth` rejects a missing or
  invalid cookie before the upgrade. No finding.
- **Origin.** `CheckWSOrigin` (`wsorigin.go:10-20`) compares `Origin` host:port with `Host`.
  That correctly rejects same-site siblings on other ports or subdomains, which matters
  because SEC-001's actor is same-site. `Origin == ""` returns true. Browsers always send
  `Origin` on WS handshakes, so the passthrough is reachable only by non-browser clients
  that already hold the cookie (as in OIDC-001 SEC-014). For a new endpoint there is no
  compatibility reason to keep it: **SEC-012 (LOW, mandatory)**, Q-S2 = yes.
- **Container belongs to device.** AD-016 step 2 validates every name against the latest
  snapshot before any agent send, and the agent resolves names only on its own daemon. This
  closes the terminal handler's gap (`HandleTerminalWS` never checks membership). No
  finding.
- **Session ownership and hijack.** IDs are 128-bit `crypto/rand`, server-generated, never
  accepted from the browser. There is one session per browser socket. Agent frames are
  routed only when `session.deviceID` equals the socket's authenticated device. That is
  correct, and it fixes CMD-LIFECYCLE SEC-014 for the new types. No finding.
- **Caps and DoS:**
  - The agent reads Docker's follow stream without a stated per-line memory bound.
  - The masking-versus-limiter order is ambiguous.
  - The server does not re-enforce the batch, rate or byte bounds on agent frames.
  - The browser ring is bounded by lines only (10,000 × 16 KiB ≈ 160 MB of text, roughly
    320 MB as UTF-16).
  - Agent-flap reconnects create one audit row each.

  These are **SEC-006 (MEDIUM)** and SEC-010 item 5.
- **Audit.** Start and end rows are written with actor and remote address (AD-024).
  Identity quality is **SEC-008**; retention is **SEC-009**.
- **"Never stored".** The relay touches no repository (AC-049), and the ADD §14.3 forbids
  logging content on the server. The agent side is not covered: any agent log line that
  contains log content lands in journald, which the `logs` collector ships into
  `device_logs`. That is **SEC-016 (LOW, mandatory)**.

### (b) Secret redaction

- **Agent side versus server side.** The split is correct: the agent is binding and the
  server is defence in depth. Two server behaviours weaken the server layer:
  - it *skips* masking for agents that advertise `logs_live_v1`;
  - several pre-existing output paths are not on AD-006's list.

  These are **SEC-004** and **SEC-014**.
- **Rule completeness (OQ-06) and bypasses:**
  - base64;
  - JSON inside a value;
  - URLs with credentials;
  - camelCase keys;
  - multi-line values;
  - compose `command:` and healthcheck strings;
  - inspect `Healthcheck` and `LogConfig`.

  These are **SEC-003 (HIGH)**, together with the accepted and added key list.
- **Already-stored label data (D-26).** No scrub migration is the right call, because a scrub
  rewrites every `telemetry_snapshots` row. But AD-006's output-path list misses
  `GET /devices/{id}/history`, which returns up to `limit` raw snapshots and leaks the
  verbatim labels for 30 days. That is **SEC-004**.
- **Download and export.**
  - The live-log download is built from the masked buffer. No finding.
  - Inspect copy uses the redacted document. No finding.
  - `GET /devices/{id}/summary` already excludes labels and env (`summary.go:220`). No
    finding.
- **WS broadcasts.** `stripHeavyTelemetry` gains `redact.Labels`. No finding there.
  `command_update` and the existing command list and output endpoints carry agent text
  (`target_results` messages, progress `message`, `stderr_tail`, `result_msg`, output)
  without masking. That is **SEC-004**.

### (c) Prune and bulk destructive actions

- **Preview token binding.**
  - Device: yes. Item set: `selected ⊆ preview.items`. Expiry: 10 minutes. Single use: yes.
  - Entropy and unguessability are not specified, and that matters because SEC-001's actor
    cannot read the preview response.
  - Consume-on-conflict semantics are unspecified.

  These are **SEC-007**.
- **TOCTOU between preview and execute.** Docker's own non-forced removals refuse in-use
  images, volumes and networks at the moment of removal. That is the right atomic guard (see
  Positive Observations). Two identity gaps remain:
  - a tag can move to a *different* image between preview and execute, and untag-by-name
    then deletes the unlisted image;
  - a volume can be removed and recreated under the same name.

  These are **SEC-007 (MEDIUM)**.
- **Host-wide lock.** Enforced in SQL under the per-device advisory lock and again on the
  agent (`docker:host`, `HeldWithPrefix`). No finding.
- **Typed confirmations, server-enforced.** Implemented (AD-009). They are an anti-mistake
  control, not an anti-forgery control: every expected value (count, host name, project name)
  can be known or guessed by an attacker. **SEC-001** asks the ADD to state this, and
  supplies the actual forgery control.
- **Self-protection (OQ-01) spoofing.** Labels and image names can only add friction (a
  spoofed `riot.role` asks for more typing). AD-009 computes `selfTargets(snapshot of
  serverHostID)`. `serverHostID` is a single, last-writer-wins value that any agent sets by
  reporting an image containing `riot-server` (`handlers.go:388-390`). A spoofing agent can
  therefore *move* it and strip rule (2), the database-in-the-same-project protection, from
  the real server host. That is **SEC-013 (LOW)**.
- **CSRF on the new POST endpoints given `SameSite=Lax`.** This is real. See **SEC-001
  (HIGH)**.

### (d) Compose-file read access

- **Path confinement with untrusted labels.**
  - `..` is handled by `Clean`, then `EvalSymlinks`, then `Rel`.
  - Symlinks: the final component is opened with `O_NOFOLLOW`.
  - Absolute paths, `/proc` and the agent config are on the deny list.
  - File size caps are set.

  These are correctly specified *relative to the working directory*. But the working
  directory itself comes from the same untrusted label, so a label of
  `working_dir=/` confines nothing. On top of that:
  - the deny list omits `/etc` (Q-S3), `/root`, user credential directories and Docker
    state;
  - there is a check-then-open race on intermediate path components;
  - FIFOs and special files can hang the reader.

  These are **SEC-005 (MEDIUM)**. `/etc/shadow` via `config_files`: refused by extension and
  unreadable by the agent user. Not a finding on its own.
- **`.env` exposure.** Values follow rule 1. Keys are shown. Multi-line values are
  **SEC-003** item 6.
- **Actions, not reads, are the dangerous half.** The same untrusted labels choose which
  compose file `up` executes. That is **SEC-002 (HIGH)**.
- **D-1 (compose editing) if the product owner chooses it later.** See **SEC-019 (INFO)**: it would
  require a new review and is CRITICAL-gated by construction.

### (e) Agent privilege

- Verified in `install.sh:172-187, 457-565`:
  - the agent runs as a system user with `SupplementaryGroups=docker` (root-equivalent);
  - sudoers holds only the package, reboot, update, nginx and smartctl rules;
  - the unit has no `ProtectHome`.
- Every new capability uses the socket or plain reads as the agent user. Nothing needs sudo.
  The AGENT-PRIV manifest covers sudoers rules only, so "no mapping" is correct.
- Capability tokens can be spoofed by a compromised agent. That is acceptable only if the
  server treats them as UX gating, never as authorization, and never as a reason to skip its
  own bounds or masking. That is **SEC-014 (LOW, mandatory)**.
- Details are in **SEC-020 (INFO)**.

### (f) Actor identity, audit integrity and retention

- **AD-022 claims are integrity-protected.** The JWT is HS256 with the server secret, and the
  OIDC transaction key is HKDF-separated (`oidc/transaction.go:50`). No extra HMAC is needed
  (Q-S4 = no).
- **What is recorded.** `preferred_username` is mutable and non-unique, and can be changed by
  the IdP user in many IdPs. The auth method (`riot_auth`) and stable subject are not
  persisted, and an OIDC user named `admin` is indistinguishable from the local admin. That
  is **SEC-008 (MEDIUM)**.
- **Retention.** AD-025 and D-21 reduce terminal audit, including `_host` shell sessions,
  from "kept forever" to 90 days. That is short for incident forensics, and command history
  is purged on the same clock. Separately, `terminal_sessions` has
  `REFERENCES devices ON DELETE CASCADE` (`000001_init.up.sql:77`), so deleting a device
  erases its audit trail. These are **SEC-009 (MEDIUM)**.
- **Attribution of observed events** (`events.command_id`, agent-supplied) is **SEC-015
  (LOW)**.

### (g) Command palette, fleet table, URL state

- **Rendering as text.** Container names, labels, image names, actor names and step output
  render as React text, highlighting builds text nodes plus `<mark>`, and the QA grep forbids
  `dangerouslySetInnerHTML`. No finding.
- **`javascript:` links.** `react-dom@19.2.4` replaces `javascript:` hrefs (verified in
  `sanitizeURL`), so the existing `href={c.repo_url}` (`ContainerDetail.tsx:423`) is not a
  script sink.
- **Attacker-chosen hosts.**
  - The repository URL is a raw image label (`InferRepoURL` returns
    `org.opencontainers.image.source` verbatim), and the new ⋯ "Open source repository" item
    hides the destination.
  - The port-link host comes from agent-reported `host_ip` or `primary_ip`. Neither is
    validated as an IP: `extractPrimaryIP` only strips a CIDR suffix.

  These are **SEC-011 (LOW, mandatory)**.
- **Open redirects.** None found. The legacy redirects navigate only to fixed same-origin
  prefixes, after the device loads. Unencoded path segments are **SEC-017 (LOW)**.
- **URL state.** Parse-and-drop with allowlisted enums. It never triggers an action
  (NFR-012). No finding.

### (h) DB growth as a DoS vector

- These bounds are correct and enforced on the server:
  - `commands.steps`: at most 64 entries;
  - step output: 64 KiB per command, written once;
  - `target_results`: at most 100 entries of 1 KiB;
  - `command_output`: bounded by CMD-LIFECYCLE (256 KiB);
  - new retention purges batched at 5,000 rows.
- These are bounded by assumption, not by enforcement:
  - `docker_stacks` (rows are never deleted, and a hostile agent can rotate project names);
  - `config_files` array and string length;
  - inventory per-item inner arrays and strings;
  - inventory write cadence (the server does not enforce the 15-minute interval);
  - live-log audit rows on agent flaps.

  These are **SEC-010 (MEDIUM)**.
- The telemetry endpoint itself has no per-device rate limit or body cap (pre-existing,
  FLEET-DASH SEC-004). It is noted in SEC-010 as systemic.

### (i) Public-repo hygiene of the FRD and ADD

Scanned both documents for IPv4 literals outside RFC 5737, email patterns, home or user
paths, drive letters, private domains and suffixes, and the operator's machine names. No LAN
IP, real hostname, home path, domain or email was found. All addresses are `192.0.2.x` and
all paths are product paths (`/etc/riot`, `/var/lib/riot`) or `/srv/stacks/<name>`. The
product owner's first name appears 14 times (FRD 4, ADD 10). That is **SEC-018 (INFO)**.
This report uses placeholders only.

---

## Findings

### CRITICAL

None.

---

### HIGH

#### SEC-001: Same-site request forgery and framing can drive the new fleet-wide destructive endpoints; server-side typed confirmations are not a forgery control

**Severity:** HIGH
**Domain:** CSRF / Clickjacking / Authorization
**Affected:**
- AD-008 (`POST /docker/bulk-actions`)
- AD-009 (typed confirmations "a URL alone can never act")
- AD-027 (`POST .../stacks/{project}/actions`, `.../forget`)
- AD-029 (`prune/preview`, `prune/execute`, `inventory/refresh`)
- AD-019 (GET reads that trigger agent RPCs and audit writes)
- §8.4, §8.6-§8.8
- NFR-012, BR-006

**Location:**
- `handlers/auth.go:27-47` (`SameSite=Lax`; `Secure` only on https)
- `middleware/cors.go` (no Origin check on non-preflighted requests)
- every handler decodes JSON with `json.NewDecoder(r.Body)` regardless of `Content-Type`
- no `frame-ancestors` or `X-Frame-Options` anywhere in `internal/server`

**Description.**
`SameSite=Lax` withholds the cookie from *cross-site* subresource requests. It sends it on
*same-site* ones. "Site" is scheme plus registrable domain, and for an IP host it is the IP;
**ports are ignored**. In the deployments this product targets, many origins are same-site
with the dashboard:

- every other web UI published on the same host IP (the containers rIOt itself manages,
  which this story turns into one-click port links);
- every sibling behind the same reverse-proxy parent domain.

A page on such an origin can send `fetch(url, {method:'POST', credentials:'include',
mode:'no-cors', headers:{'Content-Type':'text/plain'}, body: JSON})`. That is a "simple"
request, so there is no preflight. The cookie is attached, and the handler parses the body
as JSON because it never checks `Content-Type`.

The attacker cannot read the response. It does not need to:

- `bulk-actions` with `action:"stop"`, 100 targets from the fleet, and the right
  `confirm_count` (the attacker sends whatever count it needs, and a mismatch costs one
  retry);
- `stacks/media/actions` with `{"action":"down","confirm":"media"}` (project names are
  guessable);
- `confirm_host` for a self target (the hostname is guessable).

The typed-confirmation design in AD-009 is presented as "defence in depth, and a URL alone
can never act". It defends against operator slips, not forgery.

The same actor can also frame the dashboard. A same-site iframe loads *authenticated*,
unlike the cross-site case analysed in UI-FOUNDATION SEC-009. It can then overlay decoys on
the non-typed confirmations: bulk Stop of 5 or fewer on one host, stack Restart and
Redeploy, Pull, Up, single Stop.

GET reads (inspect, compose, env) can be forced with `<img src>`. Each one costs an agent
round-trip, writes an audit row, and consumes the 30-per-minute per-host budget, which
denies Inspect to the real operator.

This is partly pre-existing (`POST /devices/{id}/commands` with `reboot`). This story
multiplies the blast radius to fleet-wide in one request, and adds data-destroying actions.

**Attack Scenario.**
1. The rIOt server runs on `host-a` at `http://192.0.2.10:<rIOt-port>`. A media container on
   the same host serves its UI at `http://192.0.2.10:8096` and has a stored XSS. Alternatively,
   an operator-installed image's UI is malicious after a supply-chain update.
2. The operator, signed in to rIOt, opens that UI (for example from the new port link).
3. Its script reads nothing, but POSTs `text/plain` JSON to
   `/api/v1/docker/bulk-actions` with `{"action":"stop","targets":[…up to 100 guessed or
   common names across hosts…],"confirm_count":N}`, then retries with the count from a
   400. It also posts `stack down` for common project names, with the name as `confirm`.
4. Containers across the fleet stop, and stacks go Down. With P4 it can also run
   `prune/preview` blind, but it cannot read `preview_id`. SEC-007 requires that the ID be
   unguessable.

**Required Resolution.**
The ADD must specify, as a cross-cutting control on every route this story adds or modifies
(and is recommended for all existing state-changing admin routes, D-30), that these
properties hold:

1. **State-changing requests are same-origin only.** POST, PUT and DELETE are rejected
   (403, no side effect, no audit row) unless:
   - the request carries `Content-Type: application/json`; **and**
   - either `Sec-Fetch-Site` is `same-origin`, or `Origin` equals the server's own origin
     (scheme, host and port, compared like `CheckWSOrigin`).

   When neither header is present, the request is rejected. Non-browser clients must send
   `Origin`. A same-site but cross-origin request must be rejected.
2. **Side-effecting GETs get the same protection.** GETs that cause an agent round-trip, an
   audit write, or rate-limit consumption (inspect, compose list, compose file, env) must
   carry a non-simple request header (for example a fixed custom header), which forces a
   preflight that CORS will refuse, or must pass the same origin test.
3. **Framing is forbidden.** Every dashboard response must forbid framing
   (`frame-ancestors 'none'` or equivalent).
4. **The ADD reclassifies AD-009.** It states that typed confirmations are anti-mistake
   controls only, and that properties 1 to 3 are the anti-forgery control. NFR-012's "a URL
   alone must never start an action" is met by property 1, not by AD-009.
5. **Prune preview IDs** meet SEC-007 item 1.

**Blocks:** Implementation of P1 (the bulk endpoint is the first new destructive POST) until
the ADD states properties 1 to 4. It is a condition for P2 to P4 routes.

---

#### SEC-002: Stack identity comes from forgeable labels with last-observation-wins semantics, so a hostile standalone container can redirect a real stack's Up, Redeploy or Update (and today's auto-update) to an attacker-writable compose file

**Severity:** HIGH
**Domain:** Business logic / Path trust / Privilege escalation (container to host)
**Affected:**
- AD-021 (agent stack registry "from labels the agent itself observed")
- AD-026 (`docker_stacks.Observe` "for every compose project in the snapshot")
- AD-027 (Up, Redeploy, Update, Pull through `-f <config_files>` and `--project-directory`)
- AD-007 (stack member keys)
- FR-112, FR-116, FR-118, FR-123, NFR-013, A-11, R-06

**Pre-existing:** `handlers/auto_update.go:382-392` dispatches the stack update with the
`working_dir` label of the **first** container seen in a project that has an update.

**Description.**
Image `LABEL`s are inherited by containers. Compose overwrites the `com.docker.compose.*`
keys on containers *it* creates, but a container started without compose
(`docker run`, another orchestrator, a sidecar a tool launched) keeps whatever the image
author wrote, including:

- `com.docker.compose.project=<real project>`;
- `com.docker.compose.project.working_dir=<path>`;
- `com.docker.compose.project.config_files=<path>`.

The ADD accepts that labels are untrusted (A-11), and applies `stackfs` to *reads*. But
`stackfs` confines a file *to the label-chosen working directory*, so it does not decide
whether that directory is the real stack's. Neither the agent registry nor `docker_stacks`
says what happens when two containers of one project report different paths. Both use one
record per project, so in effect the last write wins.

A hostile container needs only one writable bind mount (common:
`-v /srv/appdata/<x>:/config`). It writes `evil.yaml` there, and labels itself into the real
project with that directory and file. From then on:

- the operator's **Up**, **Redeploy** or **Update** on that stack runs
  `docker compose -p <project> --project-directory <attacker dir> -f <attacker file> up -d`;
- **Up** of a Down stack uses the persisted record with no live member to contradict it;
- the auto-update dispatcher can do the same with **no operator action**, if the hostile
  container advertises an update (its author controls its registry tags).

Compose `up` on attacker YAML is `privileged: true` with `/:/host`, which is root on the
host. rIOt, holding the docker socket, is the confused deputy. Pull and Up need no
confirmation (FRD §8.11), so nothing shows the operator the paths that will execute.

**Attack Scenario.**
1. `host-a` runs stack `media` from `/srv/stacks/media` (compose-created containers, correct
   labels). It also runs a standalone container `helper` from a third-party image, with
   `-v /srv/appdata/helper:/data`.
2. A malicious update of that image ships
   `LABEL com.docker.compose.project=media`,
   `com.docker.compose.project.working_dir=/srv/appdata/helper`,
   `com.docker.compose.project.config_files=/srv/appdata/helper/compose.yaml`.
   At start it writes `/data/compose.yaml` containing a privileged service with `/:/host`.
3. The next collection records these paths for `media` in the agent registry and in
   `docker_stacks`, overwriting the real ones.
4. The operator clicks Update on `media` (or auto-update fires, or the operator takes `media`
   Down and then Up). The agent precheck passes: the working directory matches the registry,
   the file is inside it with a `.yaml` extension, and no denied root applies. Compose then
   starts the attacker's service. The attacker has host root.

**Required Resolution.** These properties must hold, be written into AD-021, AD-026 and
AD-027, and be tested:

1. **Agreement or conflict.** A project's `working_dir` and `config_files` are accepted only
   when every observed member container agrees. On disagreement the stack is `conflicted`:
   - every stack action (including the existing stack Update and the auto-update
     dispatcher), compose read and `.env` read is refused with a specific reason;
   - the UI shows the conflict and lists the disagreeing containers by name.
2. **No silent path change.** A change to a known stack's `working_dir` or `config_files`
   (agent registry or `docker_stacks`) does not take effect for actions until the operator
   acknowledges it. Until then the stack is treated as `conflicted`. The change is recorded in
   Activity and audit.
3. **No execution of container-writable compose files.** The agent refuses to *execute*
   compose (Up, Redeploy, Update, and `docker_update{compose_work_dir}`) when the resolved
   working directory or any resolved compose file lies under the host source of a
   **read-write** bind mount of any container on that host.
   - An explicit, host-local operator allowlist (agent configuration, not settable from the
     server) may exempt stack roots for tools that manage compose files from inside a
     container (D-29).
   - Reads may stay permitted, but they are marked.
4. **One source of truth for auto-update.** The auto-update stack path takes its working
   directory from the same agreed record as the stack actions, never from "the first
   container seen". This closes the pre-existing hole.
5. **Paths are shown before execution.** The Redeploy, Update and Down confirmations, and the
   stack page header (for Pull and Up, which have no dialog), show the agent-resolved working
   directory and the compose file list that will execute.

**Blocks:** Implementation of P3 (WP-3A/3B) until the ADD states properties 1 to 5.
Property 4 is recommended for P1, because it is a pre-existing host-takeover path.

---

#### SEC-003: Redaction rules have concrete bypasses in common secret formats

**Severity:** HIGH
**Domain:** Data exposure / Secrets
**Affected:**
- AD-005 (`rules.go`, `URLCreds` regex, `SensitiveKey`)
- AD-006 (labels, P1)
- AD-020 (inspect, P2)
- AD-021 (compose and `.env`, P3)
- D-27
- FRD §8.6 and §8.7, OQ-06, BR-004, NFR-010

**Description.** BR-004 is the story's central promise. These inputs pass it today:

1. **`URLCreds` regex** `([a-z][a-z0-9+.-]*://)([^/\s:@]+):([^@\s/]+)@`:
   - **empty user:** `redis://:s3cr3t@cache:6379` does not match, because the user group
     needs one or more characters. Password-only Redis URLs are the default form;
   - **`/` in the password:** `postgres://app:Ab/9+x==@db/app` does not match, because the
     password class excludes `/`. Generated base64 passwords routinely contain `/`;
   - **raw `@` in the password:** `postgres://app:p@ss@db` matches only up to the first
     `@`, leaving `ss@db` visible;
   - credentials in a query string (`?password=`, `?token=`, `?sig=`) are not covered at all.
2. **D-27.** Compose `environment:` values with non-sensitive keys are shown. Combined with
   item 1, `DATABASE_URL`, `REDIS_URL`, `AMQP_URL` and `MONGO_URI` leak in the Compose tab,
   although the same variable is `<redacted>` in Inspect. That inconsistency is noted in the
   ADD but not resolved.
3. **`SensitiveKey` misses common key spellings:**
   - camelCase and kebab-case: `clientKey`, `signingKey`, `encryptionKey`, `masterKey`,
     `privKey`. `KEY` is a whole segment only after a `_ . -` split, and `PRIVKEY` contains
     neither `PRIVATE` nor a `KEY` segment;
   - abbreviations: `*_PW`, `*_PSK`, `OTP`, `HMAC`, `SIGNING`, `ENCRYPT`, `LICENSE`, `SAS`,
     `CONNSTR`;
   - the architect's proposed `BEARER`, `JWT`, `WEBHOOK`.
4. **Secret-shaped values under non-sensitive keys.** Labels, compose scalars and inspect
   strings get only rule 3, so these pass:
   - JSON in a label value: `com.example.cfg={"password":"x"}`;
   - a PEM block under `tls.pem: |`;
   - `Authorization: Bearer …` inside a healthcheck string;
   - a JWT.
5. **Argument and command coverage:**
   - rule 4 applies only to inspect `Cmd`, `Entrypoint` and `Args`, element by element;
   - shell-form elements (`["sh","-c","app --token=abc"]`) and attached short flags
     (`mysql -psecret`) are missed;
   - inspect `Config.Healthcheck.Test` is not covered (`curl -u admin:pw`, `-H
     'Authorization: …'`);
   - compose `command:`, `entrypoint:` and `healthcheck.test` get no argv rule at all.
6. **Multi-line `.env` values.** `redact.DotEnv` parses "`KEY=VALUE` lines". A double-quoted
   multi-line value (a PEM key, which compose supports) yields continuation lines. A
   line-oriented parser either shows them as *keys* (keys are never redacted) or drops them.
   The first case shows private key material in the key column.

**Attack Scenario.**
The operator opens the Compose tab of stack `app` while sharing a screen, or a copy of the
redacted output goes into a support ticket. The file holds
`environment: { DATABASE_URL: "postgres://app:Q2/x9+Lm==@db/app" }` and `.env` holds
`TLS_BUNDLE="-----BEGIN PRIVATE KEY-----\nMIIE…\n-----END PRIVATE KEY-----"`.

1. `DATABASE_URL` is not a sensitive key, and the URL regex fails on `/`, so the full password
   is shown.
2. The `.env` continuation lines appear as keys.

No attacker action is needed: these are leaks under ordinary use, of exactly what the
feature promises to hide.

**Required Resolution.** These properties must hold, as data in `rules.go` with table tests
on Go and TS (NFR-020):

1. **D-27 is resolved to rule 1.** Every compose `environment:` value (map and list form,
   including values reached through anchors, aliases and merge keys) is redacted except
   for safe-list keys, exactly as for inspect `Config.Env`. The FRD permits the review to
   tighten (§8.6).
2. **URL credentials.** Rule 3 redacts the password component for:
   - an empty user;
   - passwords containing `/`, `:`, `+`, `=` or `%`-escapes;
   - passwords containing a raw `@` (everything up to the *last* `@` before the host).

   It also redacts the values of query parameters whose names match rule 2.
3. **Sensitive-key matching** also matches:
   - `KEY` at a camelCase boundary and as a suffix of a segment;
   - the additions `BEARER`, `JWT`, `WEBHOOK`, `SIGNING`, `ENCRYPT`, `HMAC`, `PRIVKEY`,
     `PASSPHRASE`, `PSK`, `LICENSE`, `CONNSTR`, `CONN_STR`;
   - `PW`, `OTP` and `SAS` as whole segments.

   False positives (for example `MONKEY`) are accepted: the rule fails toward redaction.
4. **Masking as a second net.** The §8.7 masking patterns (Authorization values, JWT form,
   PEM blocks, `"key":"value"` and `key=value` with rule-2 keys) apply to every label value
   and to every string in the inspect and compose documents, not only to log lines.
5. **Argv rule.** Rule 4 applies to whitespace-split tokens inside every argv element, to
   attached short flags for `-p` and `-P`, and to inspect `Healthcheck.Test` and compose
   `command`, `entrypoint` and `healthcheck.test` (string or list form).
6. **`.env` parsing.** `.env` follows compose's dotenv semantics, including multi-line quoted
   values. Any line that does not parse to a key matching `^[A-Za-z_][A-Za-z0-9_.-]*$` makes
   the whole read fail closed (`redaction_failed`). Values are never shown as keys.
7. **Residual risk is documented.** The README and the Logs, Inspect and Compose notices state
   that base64-encoded or otherwise obfuscated secrets under non-sensitive keys can still
   appear outside `environment:` and env.

**Blocks:**
- P1: items 2 to 4 for labels, because label redaction ships in WP-1A/1B.
- P2: items 2 to 5 for inspect and masking.
- P3: items 1 to 6 for compose and `.env`.

Each must be in AD-005 or AD-020/AD-021 before its phase starts.

---

### MEDIUM

#### SEC-004: Redaction and masking do not cover every output path, and the server skips masking based on an agent claim

**Severity:** MEDIUM
**Domain:** Data exposure
**Affected:** AD-006 (output-path list), AD-016 ("does not mask… done at the source by agents
with `logs_live_v1`"), AD-023 (step output), FR-089, D-26, NFR-010

**Description.**
1. **History endpoint.** `GET /devices/{id}/history` (`handlers.go:559-575`, `limit`
   unbounded) returns full snapshots with verbatim labels, and with the `env` field. It is
   not on AD-006's redacted list. Under D-26 (no scrub) it is a 30-day window onto every
   label secret already stored, and the server-side pass is meant to close it.
2. **Unmasked command text.** FR-089 masks "Activity output text" on the Activity endpoints.
   The same agent text is also returned unmasked by:
   - the existing `GET /devices/{id}/commands` and `GET …/commands/{commandId}/output`;
   - the `command_update` WS broadcast (progress `message`, `stderr_tail`, `result_msg`,
     `target_results` messages).

   A compose pull failure routinely echoes registry URLs with credentials, or
   `Authorization` headers.
3. **Masking skipped on an agent claim.** The live-log relay skips server masking whenever
   the agent advertises `logs_live_v1`. A buggy or older-but-spoofing agent then sends
   unmasked lines straight to the browser. Masking is idempotent (AD-005), and its CPU cost
   is already bounded by the byte caps.

**Attack Scenario.** A failed stack Pull reports
`Error: pull access denied … https://ci:glpat-XXXX@registry.example.internal/…`. Activity
shows it masked, but the dashboard's existing command panel, which reads
`/commands/{id}/output`, and every open dashboard socket (`command_update`) show it
unmasked.

**Required Resolution.**
1. AD-006's list must enumerate *every* server output path that returns snapshot container
   data, and apply label redaction (and drop `env`) on each. That includes `/history`.
2. Agent-originated free text (`result_msg`, `stderr_tail`, progress message, output,
   `target_results[].message`, step output) must be masked **at write time**, before storage
   and broadcast, so that every reader, existing or new, gets masked text.
3. The server applies `MaskLine` to relayed live-log lines regardless of capability tokens
   (SEC-014).
4. Add a test that enumerates the routes (an extension of the router auth matrix) and asserts
   that no listed route returns a fixture's raw label value.

**Blocks:** No. Condition C-4. Items 1 and 4 are P1, items 2 and 3 are P2.

---

#### SEC-005: Compose and `.env` reads: a label-chosen working directory confines nothing, the deny list is incomplete, and the path is checked before the open, not on the opened file

**Severity:** MEDIUM
**Domain:** Path traversal / File handling (CWE-22, CWE-59, CWE-367)
**Affected:** AD-021 (`stackfs.CheckComposePath`, denied roots), FR-123, NFR-013, Q-S3

**Description.**
1. **No outer bound.** Confinement is "inside `wd`", but `wd` is itself a label value. A
   label `working_dir=/` (or `/home/<user>`) plus a `config_files` entry turns the agent
   into a reader of any `.yml`, `.yaml` or `.json` file the agent user can read. The unit has
   no `ProtectHome`, and AGENT-PRIV does not restrict reads. Examples:
   - IdP and home-automation `secrets.yaml` files, whose keys are arbitrary (`mqtt_pw`);
   - `~/.docker/config.json`;
   - k3s `k3s.yaml`;
   - netplan files.

   The reader is the operator, not the attacker, but the content leaves the host into the
   browser and its captures, contrary to the intent of NFR-013.
2. **Deny list gaps.** The list covers agent config, agent state, certificates, `/proc`,
   `/sys`, `/dev` and `/boot`. It omits `/etc` (Q-S3), `/root`, `/run`, `/var/run`,
   `/var/lib/docker`, and the per-user credential directories (`.ssh`, `.docker`, `.kube`,
   `.gnupg`, `.config`).
3. **Check-then-open race.** `EvalSymlinks` then `Lstat` then `open(rp, O_NOFOLLOW)`.
   `O_NOFOLLOW` protects only the final component. An intermediate directory under `wd` that
   a container can write (SEC-002's bind mount) can be swapped for a symlink between the
   check and the open.
4. **Special files.** `Lstat` checks for a regular file, then the open blocks if the path is
   swapped for a FIFO. The goroutine hangs past the server's 10 s timeout, and repeating it
   exhausts the 4 concurrent reads per host.

**Attack Scenario.** A hostile image sets `working_dir=/home/<user>` and
`config_files=/home/<user>/.config/<app>/secrets.json`. The Stacks list shows a stack, and
the operator opens its Compose tab. The file is world-readable, inside `wd`, has a `.json`
extension and is not denied. Its keys are not on the sensitive list, so the content is shown.

**Required Resolution.**
1. **Outer bound on `wd`.** `wd` must not be a filesystem root, must not equal or be an
   ancestor of any denied root, and must not be inside a denied root.
2. **Deny list.** The deny list adds the paths in item 2 above, and all of `/etc` (Q-S3 =
   yes). The denied roots are themselves symlink-resolved before comparison.
3. **Confinement on the opened file.** The opened descriptor must be verified, not only the
   path checked before it: the file is a regular file with the right size, and its resolved
   location is still inside the resolved `wd`. Opening must never block on non-regular files.
4. **Paths shown.** The Compose tab shows the agent-resolved `wd` and file paths, so that an
   odd directory is visible to the operator (SEC-002 item 5).
5. **Test cases** in `stackfs_test.go`:
   - `wd=/`;
   - `wd` as an ancestor of `/etc/riot`;
   - a component swapped to a symlink after the check;
   - a FIFO.

**Blocks:** P3 (WP-3A) until the ADD amends AD-021 with items 1 to 3. Items 4 and 5 are
conditions.

---

#### SEC-006: Live-log resource bounds are not enforced end to end (agent line memory, mask-before-drop, server re-checks, browser bytes, merged stream)

**Severity:** MEDIUM
**Domain:** Denial of service
**Affected:** AD-015, AD-016, AD-017, AD-018, FR-080, FR-084, NFR-007, D-13, D-21

**Description.**
1. **Agent line memory.** AD-017 cuts lines at 16 KiB *after* reading them. A container that
   writes without newlines (accidental binary output, or a hostile process) makes a naive
   line reader buffer without limit, and the agent is OOM-killed. That takes monitoring and
   commands down on that host. The TTY raw-reader path has the same exposure.
2. **Mask before drop.** AD-017 lists the limiter and "MaskLine runs on each line before
   batching" without an order. If masking runs before the drop decision, a container at
   100 MB/s forces 100 MB/s of regex work on the host, and the 5 % CPU estimate in §13.1
   assumes the cap.
3. **Server re-checks.** The server re-applies only the 16 KiB line cap. A compromised agent
   can send 4 MiB frames (the WS read limit) with any number of lines, at link speed, for
   each of its 8 sessions. The non-blocking `out` channel protects the agent read loop, but
   decode cost and per-frame memory (64 frames × 4 MiB = 256 MiB per session) are unbounded
   by policy.
4. **Browser memory.** The ring is 10,000 lines, and each line can be 16 KiB. That is
   about 160 MB of text (about 320 MB as UTF-16) per view, reached in minutes by a container
   that logs large JSON lines within the 512 KiB/s cap. A merged stack view sits on one ring,
   and several tabs multiply it.
5. **Audit rows on flaps.** An agent that reconnects every few seconds makes every open view
   start a new session, which means a new audit row each time (see SEC-010).

**Attack Scenario.** A compromised container on `host-b` prints 1 GiB with no newline. The
operator opens Live logs, and the agent on `host-b` grows until it is OOM-killed. Commands to
`host-b` stall until systemd restarts the agent, and the next Live view repeats it.

**Required Resolution.**
1. The agent's log reader holds at most `LiveLogLineMaxBytes` plus a small constant per
   stream. A longer run is emitted as a truncated line, and the rest is discarded up to the
   next newline (test with a no-newline stream).
2. The rate and byte decision is made **before** masking, so dropped lines are never masked.
   This is stated in AD-017.
3. The server enforces, per agent frame:
   - lines ≤ `LiveLogBatchMaxLines`, bytes ≤ `LiveLogBatchMaxBytes`, each line ≤ 16 KiB;
   - per-session and per-agent byte rates (the AD-017 values plus headroom).

   Excess is dropped and counted. A frame for an unknown session is dropped with a throttled
   log.
4. The browser buffer is bounded by bytes as well as lines (for example 32 MiB of text per
   view). It drops the oldest lines when either bound is reached.
5. Reconnects of the same browser socket after an agent flap reuse one audit row (or
   coalesce within a window) instead of writing one row each time.

**Blocks:** No. Condition C-6 (P2, and P3 for merged streams).

---

#### SEC-007: Prune execute can remove unlisted items via tag-to-image drift or volume-name reuse; the preview token's entropy and consume semantics are unspecified

**Severity:** MEDIUM
**Domain:** Business logic (TOCTOU, CWE-367) / Data loss
**Affected:** AD-029, §14.1 item 8 ("Remove the listed tags with `ImageRemove(<repo:tag>)`
first, then the ID"), FR-142, BR-008, D-5

**Description.**
1. **Preview IDs.** `preview_id` has no stated entropy, format, or binding beyond the device.
   SEC-001's actor cannot read it, which is what makes execute forgery-resistant, but only if
   it is unguessable. The ADD also does not say whether a conflict (409) or a stale-inventory
   response consumes the preview.
2. **Tag drift.** The agent untags by **reference** (`repo:tag`). If, between preview and
   execute, `app:latest` is re-pulled and now names unused image Y, then:
   - `ImageRemove("app:latest")` deletes Y, which has no container and no other tag, so a
     non-forced remove succeeds. Y was never listed or confirmed;
   - removing X by ID then removes the listed image.

   The "unexpected new tag" check covers tags *added to X*, not tags *moved off X*.
3. **Volume name reuse.** Volumes are listed by name. A volume removed and recreated under
   the same name (stack down and up, or a re-run with a fixed name) between preview and
   execute is a different volume with possibly new data. If it is unused at that moment it is
   removed.

**Attack Scenario (operator, no attacker).**
1. The operator previews "Prune images (all unused)". It lists `app:latest` = X (old,
   unused).
2. Auto-update pulls a new `app:latest` = Y for a container that is recreated a minute later.
3. The operator confirms within the 15-minute freshness window. Y is deleted before the
   recreate. The update fails at create, and a locally built Y (not in any registry) is gone.

**Required Resolution.**
1. **Preview IDs** carry at least 128 bits of CSPRNG entropy. They are bound to device, kind,
   item set and expiry, and are consumed on first use whatever the outcome. A failed execute
   requires a new preview.
2. **Images.** Every untag-by-reference first verifies that the reference still resolves to
   the listed image ID. Otherwise the item is `Skipped: changed`.
3. **Volumes.** The preview records each volume's creation time (or another immutable
   attribute). The agent skips a volume whose current attribute differs.
4. **Test:** `TestDUX_SEC007_TagMovedToOtherImageNotRemoved`, plus the volume equivalent.

**Blocks:** No. Condition C-7 (P4). Item 1 must be in AD-029 before WP-4B starts.

---

#### SEC-008: The actor identity recorded for audit is a mutable, non-unique display name, with no auth method and no stable subject

**Severity:** MEDIUM
**Domain:** Authentication / Audit integrity
**Affected:** AD-022 (claims; `commands.actor_kind`, `actor`), AD-024 (audit `actor`), FR-093,
BR-011, NFR-011, Q-S4

**Description.**
`OIDCCallback` grants admin to any subject the IdP authenticates (`oidc.go:192-205`: "new SSO
identity granted admin"). Several people can therefore be operators. AD-022 records
`preferred_username` (or the subject) as `actor`, and AD-024 records the same value.
`preferred_username` is display data:

- it is not unique across users or issuers;
- many IdPs let users edit it.

`riot_auth` is put in the JWT but is not persisted. The result:

- an OIDC user whose username is `admin` is indistinguishable from the local password admin;
- a user who renames themselves to a colleague's name forges attribution for every later
  command and audit row.

Legacy tokens default to `admin` for 24 h after upgrade, which mis-attributes OIDC sessions
in that window.

**Attack Scenario.** User `operator-2` changes their IdP username to `operator-1`, runs a
bulk Stop and a volume prune, and changes it back. Activity and audit show `operator-1`. No
stored field distinguishes them.

**Required Resolution.**
1. The session claims carry, and commands and audit rows persist:
   - the auth method (`password` or `oidc`);
   - for OIDC, a stable identifier derived from `(issuer, subject)`;
   - the display name separately.
2. Display may use the name, but filters and the audit record key on the stable identifier.
   A local admin and an OIDC user with the same display name must render distinguishably
   (for example, the auth method shown).
3. Tokens without the new claims are recorded as `unknown (legacy session)`, not as `admin`.
4. Q-S4: no additional HMAC binding is needed. The HS256 session already
   integrity-protects the claims. What was missing is persisting the subject.

**Blocks:** No. Condition C-8 (P2). It must be in AD-022 before WP-2B starts.

---

#### SEC-009: New retention deletes security-relevant audit too early, and audit rows are erased with their device

**Severity:** MEDIUM
**Domain:** Logging and audit / Repudiation
**Affected:** AD-024, AD-025, D-20, D-21, NFR-011

**Location:** `000001_init.up.sql:77` (`terminal_sessions.device_id … ON DELETE CASCADE`);
`commands` (FK cascade on device, per 000001)

**Description.**
1. **Retention length.** D-21 reduces terminal audit from unbounded to 90 days for every
   kind. That includes `_host` sessions: an interactive host shell
   (`agent/terminal.go:41-44`). D-20 deletes command history, including reboot, uninstall and
   now prune and stack Down, after 90 days. Intrusions are commonly found after more than
   90 days.
2. **Volume.** These records are tiny: about 30 audit rows and 20 commands per host per day
   (§13.3). Volume is not the constraint.
3. **Cascade on device delete.** `DELETE /devices/{id}` cascades away every audit and command
   row for that device. An operator, or anyone riding a session, can erase the trail of what
   was done on a host by deleting and re-enrolling it.

**Attack Scenario.** An attacker with a stolen session opens a `_host` terminal on `host-a`,
prunes volumes, then deletes `host-a` from rIOt. The agent re-enrols on its next restart. No
audit row of the shell or the prune remains.

**Required Resolution.**
1. Audit retention is set per kind. `terminal` (including `_host`) and `command` rows for
   destructive or privileged actions (reboot, shutdown, update, uninstall, stack Down, bulk
   Stop, prune and remove) are kept at least 365 days. `logs`, `inspect` and `compose` reads
   may stay at 90 days. This revises D-21 (see the Decisions table).
2. Audit rows survive device deletion (no cascade; the device ID is kept as text), and the
   device deletion itself is recorded as an audit row with actor and remote address.
3. The purge never deletes a row whose session is still open (`ended_at IS NULL`, within
   the session maximum).

**Blocks:** No. Condition C-9 (P2). The retention numbers depend on the product owner's D-21 decision.

---

#### SEC-010: Agent-supplied Docker data that the server stores has bounds the server does not enforce

**Severity:** MEDIUM
**Domain:** DoS / DB growth
**Affected:**
- AD-026 (`docker_stacks`: "at most 5 rows per host" is an assumption; rows are never
  deleted)
- AD-028 (inventory: only item counts are truncated server-side; cadence is agent-enforced)
- AD-021 (`config_files` array)
- AD-023 (`events.command_id`)
- AD-024 (live-log audit rows)
- NFR-003, R-07

**Description.** A compromised agent (actor 3) controls every one of these values:

1. **`docker_stacks`.** It reports N new project names per telemetry push (it can push at any
   rate: the telemetry endpoint has no per-device limit and no body cap, pre-existing
   FLEET-DASH SEC-004). Each name becomes a permanent row that only device deletion removes.
   Long `working_dir` and `config_files` values make each row large.
2. **`docker_inventory`.** Only the item counts are capped. Per item it can send unbounded
   `Tags`, `Digests`, `UsedBy`, `Containers` and `Subnets`, and unbounded string lengths, up
   to the 20 MB snapshot guard. It can also ignore the 15-minute cadence and change `hash` on
   every push. Each push then replaces a TOAST value of up to tens of MB, which is high churn
   on a table sized for low churn.
3. **`events.command_id`.** Free text with no stated bound (see SEC-015).
4. **Audit rows on flaps.** Live-log audit rows on agent flaps (SEC-006 item 5).

**Attack Scenario.** A compromised agent posts telemetry every 100 ms, with 1,000 fresh
project names and a 10 MB inventory that changes hash every time. `docker_stacks` grows by
millions of permanent rows per day, and `docker_inventory` rewrites a 10 MB TOAST value ten
times a second. Postgres disk fills and fleet monitoring stops.

**Required Resolution.** The server enforces these on ingest, whatever the agent claims:

1. `docker_stacks`:
   - at most 500 rows per device; new projects beyond that are ignored with a throttled Warn;
   - `project`, `working_dir` and each `config_files` entry are length- and charset-bounded;
   - at most 10 files per project (the agent bound).

   Rows not seen for a bounded period may be marked forgotten (BR-015 keeps them listed, but
   rows past the bound may be evicted, oldest first).
2. `docker_inventory`:
   - the per-item inner arrays and strings are bounded, with the truncation flag set;
   - the serialized inventory is capped (for example 4 MiB);
   - a *changed* payload is accepted at most once per `InventoryMinInterval` minus slack per
     device, except within 2 minutes after a server-issued `docker_inventory_refresh`.

   Other pushes update nothing.
3. `events.command_id`: SEC-015.
4. Audit coalescing: SEC-006 item 5.
5. **Systemic, recommended.** A per-device rate limit and body cap on the telemetry and
   docker-events endpoints, reusing the CMD-LIFECYCLE per-device limiter. This is not
   required for this story.

**Blocks:** No. Condition C-10 (P3 for `docker_stacks`, P4 for inventory).

---

### LOW / INFORMATIONAL

#### SEC-011 (LOW, mandatory): Agent-controlled URLs rendered as links must be scheme- and host-validated

**Domain:** Open redirect / Phishing
**Affected:** FRD §8.4 (port links), §8.5 ⋯ "Open source repository", AD-003 (`primary_ip`),
`InferRepoURL` (returns label values verbatim), `extractPrimaryIP` (no IP parse)

`react-dom@19.2.4` neutralises `javascript:` hrefs (verified), so these are not script sinks.
What remains is attacker-chosen destinations:

- a hostile image's `org.opencontainers.image.source=https://riot-login.example.internal/…`
  behind a menu item that hides the URL;
- an agent that reports a non-IP "address" (`evil.example.internal`), so that port links
  point at a credential-phishing page styled as rIOt's login.

**Attack Scenario:** an operator clicks "Open source repository" and lands on a fake "session
expired" page that harvests the admin password.

**Required properties:**
- link hrefs from agent data are only `http:` or `https:`;
- port-link hosts are IP literals (parsed, not pattern-matched), with ports as integers in
  1 to 65535;
- `primary_ip` is IP-parsed on ingest or rejected;
- menu items that open agent-supplied URLs show the destination host (for example in the item
  text or a tooltip);
- `riot.url` follows the same rules if it is ever rendered as a link.

**Blocks:** No (condition C-11, P1).

#### SEC-012 (LOW, mandatory): Require `Origin` on `/ws/docker/logs`

**Domain:** CSWSH (defence in depth)
**Affected:** AD-016, §12.1, Q-S2

The new endpoint has no legacy non-browser client. It must reject an upgrade whose `Origin`
is absent, and compare a present `Origin` as `CheckWSOrigin` does. Pre-existing routes may
keep the current behaviour.

**Attack Scenario:** a tool or script that holds a stolen cookie connects without `Origin`.
That is negligible from a browser, and is listed for completeness.

**Test:** `TestDUX_SEC012_LogsWSMissingOrigin403`.

**Blocks:** No (condition C-12, P2).

#### SEC-013 (LOW): Self-target detection must not depend on the last-writer `serverHostID`

**Domain:** Business logic (safety interlock integrity)
**Affected:** AD-009 (`selfTargets(snapshot of serverHostID)`), `handlers.go:388-390`, D-25

Any agent that reports an image containing `riot-server` overwrites `serverHostID`. If rule
(2), "the rest of the server's compose project", is computed only from that host's snapshot,
a spoofing agent on `host-b` removes the database-container warning on the real server host
`host-a`.

**Attack Scenario:** a compromised `host-b` reports a fake `riot-server` container. An
operator then takes the real rIOt database stack on `host-a` Down without the FR-147 warning.

**Required property:** `selfTargets` is computed independently per host, across all hosts,
from each host's own latest snapshot, and never from a single global value. Spoofing may
*add* self targets but must never remove one. Labels and image heuristics only ever add
friction (fail-safe direction), and that is acceptable.

**Blocks:** No (condition C-13, P1).

#### SEC-014 (LOW, mandatory): Capability tokens are agent assertions: use them for UX gating only

**Domain:** Trust boundaries
**Affected:** AD-001, AD-016 (mask skip), §4.1

The tokens decide which requests the server *sends*. They must never:

- relax a server-side bound, validation or masking (SEC-004 item 3, SEC-006 item 3);
- be read as authorization;
- feed any decision about another device.

**Attack Scenario:** a compromised agent advertises every token and sends unmasked,
oversized frames, relying on the server to trust its claims.

**Test:** `TestDUX_SEC014_ServerBoundsIndependentOfCapabilities` sends oversized and unmasked
frames from an agent that advertises every token, and asserts that they are bounded and
masked.

**Blocks:** No (condition C-14, P2).

#### SEC-015 (LOW): Validate agent-supplied `events.command_id`

**Domain:** Audit integrity / Input validation
**Affected:** AD-023 item 4

Events that carry a non-empty `command_id` disappear from the observed list. The server must
accept `command_id` only if:

- it is a canonical UUID;
- it is a command of the *same device*;
- that command was active, or finished within the correlation window, for the same
  container key.

Otherwise the server clears it, so the event stays "Agent (observed)". It must store no
unvalidated text.

A compromised agent can already withhold events, so the residual risk is misleading
attribution, not concealment. The server-side fallback (attribute to any active command on
the container) can also mis-attribute a concurrent manual `docker restart` to rIOt. The UI
should label server-inferred attribution as "inferred".

**Attack Scenario:** a compromised agent tags an external `docker stop` with a real
`command_id` from another host, so the stop appears inside an unrelated operator command.

**Blocks:** No (condition C-15, P2).

#### SEC-016 (LOW, mandatory): "Never stored" must also hold for agent logs

**Domain:** Data exposure
**Affected:** BR-005, AD-017, AD-019, AD-021, §14.1 item 5

The server rules forbid logging content. The agent is not covered. Any agent `slog` record
that carries a log line, inspect fragment, compose or `.env` content, or a parse error that
quotes input, reaches journald, and the agent's own `logs` collector ships it into
`device_logs`, which is stored.

**Attack Scenario:** a redaction parse error logs the offending YAML line (`password: …`) at
Warn. The `logs` collector ships it, and it is stored for the device-log retention period.

**Required property:** agent code in the new paths logs only lengths, counts, enums and
names, never content.

**Test:** `TestDUX_SEC016_AgentLogsNoContent` uses log capture over the inspect, compose,
`.env` and live-log paths with secret fixtures.

**Blocks:** No (condition C-16, P2/P3).

#### SEC-017 (LOW): Encode route path segments built from data

**Domain:** Input handling (client)
**Affected:** AD-011 (`LegacyContainersRedirect`, `LegacyContainerRedirect`, canonical page
links)

Container names reach the client from agent data. A hostile agent can report a name with
`/`, `..` or `?`. Building `/docker/containers/<id>/<name>` by concatenation lets such a name
traverse to other in-app routes. These are same-origin only, and no route acts on navigation
(NFR-012), so the impact is confusion, not action.

**Attack Scenario:** a compromised agent reports a container named `../../settings`. The
legacy redirect for that container lands the operator on an unrelated in-app page instead of
the container.

**Required properties:**
- path segments and the `host` query value are URI-encoded;
- names that fail `ContainerNamePattern` are not linked, and are shown as text.

**Blocks:** No.

#### SEC-018 (INFO): Public-repo hygiene of the FRD and ADD

**Domain:** Information disclosure (public repository)

No LAN IPs, real hostnames, home paths, domains or emails were found. Addresses are RFC 5737
and paths are placeholders or product paths.

The product owner's first name appears 14 times (FRD: §2a, §8.6, §13 heading and body; ADD:
status line, §0, §4.2, §6 footer, §14.1, §15, §17, D-24, D-28). It is already public through
commit author metadata, and it appears in one previously committed FRD, so this is not a new
disclosure. Replacing it with "the product owner" in public docs is optional (D-31).

**Attack Scenario:** none beyond linking the repository to a first name, which commit
metadata already does.

**Blocks:** No.

#### SEC-019 (INFO): Security implications if D-1 (compose editing) is ever chosen

**Domain:** Design note
**Affected:** D-1, BR-010

Option (b) or (c) would make the dashboard able to write files that the docker-group agent
then executes. Any session compromise, same-site forgery (SEC-001) or stored XSS would then
become host root on every host. The minimum properties for that future story are:

- its own FRD, ADD and security review, CRITICAL-gated;
- SEC-001's controls on the write endpoint, plus a re-authentication step for each save;
- writes only to agreed, non-container-writable stack files (SEC-002 items 1 to 3),
  exclusive-create temp files, no symlink following, mode preserved;
- optimistic concurrency on a content hash, and refusal of git-managed directories with
  uncommitted changes;
- a backup copy in agent-owned state;
- audit rows holding the before and after hashes and the actor's stable identifier;
- secrets: the editor must never receive redacted values and write `<redacted>` back.

Keeping (a) is endorsed.

**Attack Scenario:** none today, because no write path exists.

**Blocks:** No.

#### SEC-020 (INFO): Privilege statement verified

**Domain:** Least privilege
**Affected:** NFR-014, §12.9, AGENT-PRIV

Verified in `install.sh`:

- docker access is supplementary-group membership, which is root-equivalent and already
  required by the `docker` collector;
- no new sudoers line is needed;
- the AGENT-PRIV manifest models sudoers capabilities only, so "no mapping" is correct.

The agent can read world-readable files anywhere (no `ProtectHome`), which is why SEC-005
needs an outer bound.

Operational note, not security: compose actions run as the agent user, so stacks whose
`.env` is `0600 root` fail at `up` as well as at read. The release notes should say so next
to R-6.

Optional: AGENT-PRIV's drift report could list docker-group membership as an informational
capability, so the operator can see it.

**Attack Scenario:** none; this records verified posture.

**Blocks:** No.

---

## Positive Observations

- **Redaction at the source, failing closed.** Parse errors return no content, there is no
  reveal path, responses carry `no-store`, and one shared rule package is used by both
  binaries.
- **Names, never paths, at the API.** `DisallowUnknownFields` rejects path fields (FR-118,
  AC-082). The server-held `working_dir` is only a consistency hint that the agent checks.
  SEC-002 strengthens the agent's own source but keeps this structure.
- **Prune design.** Docker's `*Prune` APIs are never called. Removals are explicit and per
  item. Docker's own non-forced remove is the atomic in-use guard. `Force:false` is used
  everywhere. `down` never passes `-v` or `--rmi`, and an argv test proves it. The host-wide
  exclusive lock is enforced both in SQL under the existing advisory lock and on the agent.
- **Device-scoped agent message routing** for `logs_*` and `docker_read_result` (fixing the
  terminal SEC-014 pattern for new types), name validation before any agent contact, and one
  session per browser socket with server-generated 128-bit IDs.
- **Non-blocking relay.** The agent read loop, which carries command reports, can never be
  blocked by a slow browser. Byte caps were added where the FRD had only line caps (D-21).
- **Bounded command storage.** Step output is written once, capped on both sides, and stored
  in an existing table. No new high-churn DELETE-retained table is added. Inventory is kept
  out of `telemetry_snapshots`.
- **Capability tokens instead of version comparison.** A missing capability is refused with
  `409` before any agent contact.
- **No new dependency** in either binary. `axe-core` (D-24) is dev-only and has no runtime
  surface; it is approved from a security standpoint.
- **Self-protection** is enforced on the server, not only in the UI.

---

## Answers to the architect's open questions

| Question | Answer |
|---|---|
| Q-S1 / OQ-06: add `BEARER`, `JWT`, `WEBHOOK`? | **Yes**, plus the additions and matching changes in SEC-003 item 3. No safe-list additions. |
| Q-S2: require `Origin` on `/ws/docker/logs`? | **Yes** (SEC-012). |
| Q-S3: deny all of `/etc`? | **Yes**, together with an outer bound on `wd` and the extra roots in SEC-005. |
| Q-S4: HMAC-bind `riot_actor`? | **No.** The JWT already integrity-protects claims. Persist the auth method and the stable `(issuer, subject)` identifier instead (SEC-008). |
| D-27: compose `environment:` rule | **Tightened to rule 1** (SEC-003 item 1). The FRD permits the review to tighten. |
| D-26: no scrub of stored labels | **Agreed**, provided that SEC-004 item 1 covers every output path, including `/history`. |

---

## Verdict Rationale

**APPROVED WITH CONDITIONS, phase-gated.**

There are no CRITICAL findings. Each HIGH finding has a resolution that keeps the ADD's
structure:

- SEC-001 is a cross-cutting request-origin control plus anti-framing.
- SEC-002 adds an agreement and conflict rule, a no-execute-from-container-writable-paths
  rule, and a single path source for auto-update.
- SEC-003 is rule-data and parser changes in `internal/redact`.

They are, however, design-level statements the ADD must contain *before* the phase they gate
starts. They are not left for implementation to discover. `senior_dev` may start a phase when
that phase's "ADD amendment required" column below is satisfied, with this report passed
alongside the ADD. The QA engineer verifies every condition C-1 to C-16.

### Mandatory conditions

Test names follow the ADD's `TestDUX_` convention, with `SEC` in place of `AC`.

**C-1 (SEC-001): request-origin control and framing.**
- `TestDUX_SEC001_TextPlainPostRejected`: `text/plain` JSON to bulk-actions, stack actions and
  prune execute gives 403, with no command and no audit row.
- `TestDUX_SEC001_SameSiteCrossOriginRejected`: `Origin: http://192.0.2.10:8080` against
  server `http://192.0.2.10:<port>`, and `Sec-Fetch-Site: same-site`, give 403.
- `TestDUX_SEC001_SameOriginAccepted`.
- `TestDUX_SEC001_SideEffectGETsRequireHeader`: inspect, compose and env without the
  required header or origin give 403, with no agent RPC and no audit row.
- `TestDUX_SEC001_FrameAncestorsNone`: every HTML and API response.
- The router matrix (`router_test.go`) asserts that the middleware sits on every route this
  story adds.

**C-2 (SEC-002): stack identity.**
- `TestDUX_SEC002_DisagreeingMembersConflicted`: two containers of project `media` with
  different `working_dir` make every stack action, the auto-update dispatch, compose read
  and env read refuse with `conflicted`.
- `TestDUX_SEC002_PathChangeRequiresAck`.
- `TestDUX_SEC002_ComposeUnderRWBindMountNotExecuted`: Linux. It covers Up, Redeploy, Update
  and `docker_update{compose_work_dir}`, and the allowlist exemption.
- `TestDUX_SEC002_AutoUpdateUsesAgreedRecord`: the hostile first container's label is never
  dispatched.
- UI: the Redeploy, Update and Down dialogs and the stack header show the resolved paths.

**C-3 (SEC-003): redaction tables** (Go `redact` and TS `logMask`, shared fixture). These
cases must be redacted:
- `redis://:pw@h`;
- `postgres://u:a/b+c==@h/db`;
- `postgres://u:p@ss@h` (no `ss` left visible);
- `?password=` and `?token=` query values;
- `clientKey`, `signingKey`, `PRIVKEY`, `MQTT_PW`;
- a JSON-in-label value;
- a PEM block under a non-sensitive key;
- a Bearer value in `Healthcheck.Test`;
- `sh -c "app --token=abc"`, `mysql -psecret`;
- compose `command:` with `--password x`;
- compose `environment: { DATABASE_URL: … , LOG_LEVEL: info }`: both values redacted
  (rule 1);
- `.env` with a multi-line quoted PEM: either parsed with the value redacted, or the read
  fails closed. Key text never contains PEM content.

**C-4 (SEC-004):**
- `TestDUX_SEC004_HistoryEndpointRedactsLabels`;
- `TestDUX_SEC004_CommandTextMaskedAtWrite`: covers `/commands`, `/commands/{id}/output`,
  `command_update` and Activity;
- `TestDUX_SEC004_RelayMasksRegardlessOfCapability`;
- the route-enumeration test.

**C-5 (SEC-005):** `stackfs_test.go` cases `wd=/`, `wd` an ancestor of a denied root, `/etc`
denied, a symlink swap after the check (Linux), and a FIFO that does not block. The opened file
is verified (regular, size, location).

**C-6 (SEC-006):**
- `TestDUX_SEC006_NoNewlineStreamBoundedMemory`;
- `TestDUX_SEC006_DropBeforeMask`: the mask call count equals the delivered lines;
- `TestDUX_SEC006_ServerEnforcesBatchAndRate`: a hostile 4 MiB frame is bounded;
- `logBuffer.test.ts` byte bound;
- audit coalescing on reconnect.

**C-7 (SEC-007):**
- `TestDUX_SEC007_PreviewIDEntropyAndSingleUse`: consumed on 409 and on stale too;
- `TestDUX_SEC007_TagMovedToOtherImageNotRemoved`;
- `TestDUX_SEC007_RecreatedVolumeNotRemoved`.

**C-8 (SEC-008):**
- `TestDUX_SEC008_AuditStoresAuthMethodAndStableSubject`;
- `TestDUX_SEC008_OIDCUserNamedAdminDistinguishable`;
- `TestDUX_SEC008_LegacyTokenRecordedUnknown`.

**C-9 (SEC-009):**
- `TestDUX_SEC009_RetentionPerKind` (PG);
- `TestDUX_SEC009_AuditSurvivesDeviceDelete` (PG);
- `TestDUX_SEC009_DeviceDeleteAudited`;
- `TestDUX_SEC009_OpenSessionNotPurged`.

**C-10 (SEC-010):**
- `TestDUX_SEC010_StacksPerDeviceCapped`;
- `TestDUX_SEC010_InventoryFieldBoundsAndSizeCap`;
- `TestDUX_SEC010_InventoryChangedPayloadRateLimited` (fake clock);
- refresh-window exemption.

**C-11 (SEC-011):** `portLinks.test.ts` and a repository-link test. Non-IP hosts, non-numeric
ports and non-http(s) schemes render as text. The menu item shows the destination host.

**C-12 to C-16:** the tests named in SEC-012, SEC-013 (`TestDUX_SEC013_SpoofedServerHostDoesNotRemoveSelfTargets`),
SEC-014, SEC-015 (`TestDUX_SEC015_ForeignOrMalformedCommandIDCleared`) and SEC-016.

**Verify as designed (cannot be confirmed from the ADD alone):**
- every new route is in the admin group, which is the existing router-matrix extension;
- AC-049 and AC-064 no-storage assertions, including `server_logs` (Warn records persisted);
- `Cache-Control: no-store` on reads;
- `down` argv contains no `-v`, `--volumes`, `--rmi` or `--remove-orphans`;
- `ImageRemove` and `VolumeRemove` never use force;
- `ContainerRemove` in update never sets `RemoveVolumes`;
- a QA grep finds no `dangerouslySetInnerHTML`, and no agent string bound to `src`; `href`
  bindings only through the SEC-011 validators;
- typed-confirmation errors never echo the expected host or name.

**Recommended, not mandatory:** SEC-010 item 5 (per-device telemetry limiter), SEC-017,
applying C-1 to all pre-existing state-changing routes (D-30).

---

## Per-phase gate

"ADD amendment required" means that the architect must amend the ADD with the stated
properties before that phase's WP-nA/B/C starts. "Implementation conditions" are verified by
QA before that phase is signed off.

| Phase | ADD amendment required before start | Implementation conditions (QA) |
|---|---|---|
| **WP-0 / P1** (WP-1A, 1B, 1C) | **SEC-001** items 1-4 (cross-cutting origin and content-type control, framing, AD-009 reclassified); **SEC-003** items 2-4 (URL credentials, key matching, masking net, for labels); **SEC-004** item 1 (complete label output-path list, including `/history`) | C-1, C-3 (label cases), C-4 (history, route enumeration), C-11, C-13; SEC-002 item 4 recommended here (pre-existing auto-update hijack) |
| **P2** (WP-2A, 2B, 2C) | **SEC-003** items 2-5 (inspect argv, Healthcheck, LogConfig, masking net); **SEC-008** items 1-3 (persist auth method and stable subject); **SEC-009** (per-kind retention, no cascade), after the product owner's D-21 decision | C-3 (inspect and mask cases), C-4 (write-time masking, relay), C-6, C-8, C-9, C-12, C-14, C-15, C-16 |
| **WP-2D** (I/O history) | none new | none new |
| **P3** (WP-3A, 3B, 3C) | **SEC-002** items 1-5 (agreement and conflict, acknowledged path change, no execution from container-writable paths with a host-local allowlist, single source for auto-update, paths shown); **SEC-005** items 1-3 (outer `wd` bound, deny list including `/etc`, verification on the opened file); **SEC-003** items 1 and 6 (compose env rule 1, dotenv parsing) | C-2, C-3 (compose and `.env` cases), C-5, C-6 (merged stream), C-10 (`docker_stacks`), C-16 |
| **P4** (WP-4A, 4B, 4C) | **SEC-007** item 1 (preview ID entropy and consume semantics); **SEC-010** item 2 (inventory server bounds and cadence) | C-7, C-10 (inventory) |

---

## Additions to the Decisions for the product owner list

| ID | Decision | Options | Security recommendation | Consequence |
|---|---|---|---|---|
| D-21 (revised) | Audit retention | (a) 90 d for every kind (ADD). (b) Per kind: ≥365 d for terminal (including `_host`) and destructive or privileged command audit; 90 d for reads. (c) Keep terminal unbounded, as today | **(b)** | About 30 rows per host per day, so negligible size. Avoids losing shell and prune forensics at 90 days (SEC-009) |
| D-27 (resolved by review) | Compose `environment:` values | (a) Rule 1, as for inspect env (applied by this review). (b) Sensitive-key rule (ADD default) | **(a)**. The product owner may revert only by explicitly accepting SEC-003's `DATABASE_URL`-class leak | Compose tab shows `LOG_LEVEL=<redacted>` too. Consistent with Inspect |
| **D-29 (new)** | Stacks whose compose files are writable from a container (tools that manage `/srv/stacks` from inside a container) | (a) Refuse compose execution unless the stack root is on a host-local agent allowlist. (b) Warn and require typed confirmation instead of refusing. (c) No check | **(a)** | Operators of such tools add one line to the agent config per stack root. Without it, SEC-002's container-to-host path stays open |
| **D-30 (new)** | Scope of the request-origin control (SEC-001) | (a) Only routes this story adds or modifies. (b) All state-changing admin routes, including existing `/commands`, settings and device delete | **(b)** | Existing reboot and uninstall endpoints share the same same-site exposure. Non-browser scripts that POST with the cookie must send `Origin` and JSON |
| **D-31 (new)** | Product owner's first name in public docs | (a) Keep (already public through commit metadata). (b) Use "the product owner" in docs from now on | Either; **(b)** is tidier | Cosmetic (SEC-018) |
| D-24 (endorsed) | `axe-core` devDependency | approve or decline | **Approve** from a security standpoint | Dev-only. No runtime or bundle surface |

---

## Revision 2 addendum

**Date:** 2026-10-19
**Scope:** delta review of FRD rev 2 (`5d76411`) and ADD rev 2 (`5a76227`), compared with
the revision 1 basis this report was written against (`2f87bcf`). Clod read the whole of
ADD rev 2: §0, §2.4 and §2.5, every revised AD, AD-034 to AD-039, §7.2, §8.10 to §8.13, §9,
§12, §13.3, §17, §18 and §19. Clod also read the FRD rev 2 P0 and live-log requirements
(FR-148 to FR-165, NFR-024 to NFR-027).

The following were checked against real code and the prerequisite design:

- `internal/server/db/container_log_repo.go`: `InsertBatch` copies `stream`,
  `container_name` and `timestamp` verbatim from the agent, with a 20,000-row outer cap;
- `internal/server/handlers/handlers.go` (`Telemetry`: no per-device rate limit);
- the PART-001 ADD (AD-005 and AD-008: a forward DEFAULT partition that is **never
  dropped**, and benign classification of DEFAULT-capture errors when a day partition is
  created).

**Updated verdict: APPROVED WITH CONDITIONS (phase-gated, now P0 to P4).**

- Every revision 1 HIGH (SEC-001, SEC-002, SEC-003) is closed in the ADD.
- Every MEDIUM is closed, except SEC-009, which is partially closed.
- Two new HIGH findings gate P0:
  - **SEC-021:** container-log timestamps are not bounded on ingest, so one agent defeats
    partitioned rotation fleet-wide.
  - **SEC-022:** the prescribed `migrate force 22` reconciliation keys on an ambiguous
    version number and permits a destructive "drop first".

Both have non-architectural resolutions, but they must be written into the ADD (AD-035,
§2.4 and R-3) before WP-P0B or WP-P0C starts, and before any deployed database is
reconciled.

---

### R2.1 Status of the revision 1 findings

| Finding | Status | Where (ADD rev 2) | Note |
|---|---|---|---|
| SEC-001 | **Closed** | AD-034, AD-009, AD-019 | `SameOriginJSON` runs before decode on every new or modified state change, including `paths/acknowledge`. `RequireRiotHeader` covers the side-effecting GETs, and `NoFraming` applies globally. Typed confirmations are reclassified as mistake prevention. Residual: pre-existing routes stay exposed until D-30 (b) (R-12). |
| SEC-002 | **Closed in P3; partially closed in the P1 interim** | AD-038, AD-021, AD-026, AD-027 | Agreement, acknowledgement, the writable-path refusal, a single source of truth and paths shown are all present. Two gaps remain: the P1 auto-update uses agreement only, and the writable check misses bind-backed named volumes. Both are **SEC-024**. |
| SEC-003 | **Closed** | AD-005, AD-020, AD-021 | URL-credential scanner (last `@`), camelCase, suffix and abbreviation keys, masking net on documents, argv tokens and healthchecks, compose environment under rule 1, dotenv parsing that fails closed. |
| SEC-004 | **Closed** | AD-006, AD-023, AD-016, AD-035 | `/history` (capped at 100) and `/summary` are on the list. A route-walk test is specified. Command text is masked at write time. The relay always masks. For P0, see **SEC-028**. |
| SEC-005 | **Closed** | AD-021 | Outer `wd` bound; deny list including `/etc` and credential directories; component-wise `Openat` with `O_NOFOLLOW`, then `O_NONBLOCK` and `Fstat`. Windows is unsupported and fails closed (D-39). |
| SEC-006 | **Closed** | AD-017, AD-016, AD-036 | Bounded reader; drop before mask; per-frame and per-rate server re-checks; buffer bounded at 10,000 lines and 32 MiB; audit coalescing. New fan-out issues: **SEC-025** and **SEC-026**. |
| SEC-007 | **Closed** | AD-029 | 128-bit preview IDs bound to device, kind, items, actor and expiry, consumed on any outcome; `ImageInspect(ref)==id` before each untag; volume `CreatedAt` check. |
| SEC-008 | **Closed** | AD-022, AD-024 | `riot_auth` and `riot_sub` (a hashed issuer and subject) are persisted. The display name and method are shown. Legacy sessions are recorded as unknown, and filters use `actor_sub`. |
| SEC-009 | **Partially closed** | AD-024, AD-025, §7.2 | Per-kind retention, no FK cascade, device deletion audited, open rows kept. Gaps: no `command` audit row is written for non-Docker privileged actions, and the P2 down migration deletes audit rows (**SEC-029**). |
| SEC-010 | **Closed** | AD-026, AD-028, AD-023, AD-016 | Stack rows and fields bounded, with eviction; inventory per-item bounds, a 4 MiB cap and cadence. The stored-log ceiling is a new surface: **SEC-023**. |
| SEC-011 | **Closed** | AD-011 (`safeUrl.ts`, `ip.ts`), AD-003 | http(s) only; strict IP grammar; ports from 1 to 65535; `primary_ip` parsed at ingest; destination shown. |
| SEC-012 | **Closed** | AD-016, §8.10 | `Origin` is required and compared. For token expiry on long-lived sockets, see **SEC-027**. |
| SEC-013 | **Closed** | AD-009 | Self targets are computed per host, never from `serverHostID`, with a test. |
| SEC-014 | **Closed** | §4.1, AD-016 | Tokens gate UX only. The server's bounds and masking apply regardless of tokens. |
| SEC-015 | **Closed** | AD-023 | UUID, same device, active or finished within 10 s, key match. Otherwise the ID is cleared. `command_attr` labels inferred attributions. |
| SEC-016 | **Closed** | AD-017, §14.1 | Agent logs carry no content. `redact.ErrParse` never quotes input. A capture test is specified. |
| SEC-017 | **Closed** | AD-002, AD-011 | Segments go through `encodeURIComponent`. A non-matching name is shown as text. |
| SEC-018 | **Open (INFO)** | D-31 | The owner's first name still appears 14 times in the ADD and 11 times in the FRD. This awaits the product owner's D-31 choice. No other private data was found in either rev 2 document. |
| SEC-019 | **Closed (INFO)** | D-1 | Read-only compose is kept. |
| SEC-020 | **Closed (INFO)** | §12.9 | `compose_trusted_roots` is host-local and grants no privilege. |

---

### R2.2 Review of the new surface

**Shared-stream fan-out (AD-016, AD-037).**

- **Can viewer A's filters or pause affect viewer B?** No. The text filter, stream toggle,
  Pause and Follow are client-side view state over each browser's own buffer (AD-036). The
  server holds no per-viewer filter, and the agent always streams both streams. Backfill
  frames go only to the requesting viewer. A slow viewer's full queue drops frames and
  detaches only that viewer. No finding.
- **Does detach or teardown leak a stream?**
  - Detach is socket-scoped: a viewer can only detach itself.
  - Last viewer gone means a 2 s linger, then `logs_unfollow`.
  - Agent disconnect and `replace` eviction both call `AgentGone`.

  Two leak paths remain:
  - the linger timer can race a new attach;
  - when the server forgets a stream (a lost `logs_unfollow`, or a hub restart), the agent
    keeps following, because frames for unknown streams are only dropped.

  These are **SEC-025 (LOW, mandatory)**.
- **Does an unauthenticated viewer ever join?** No, at upgrade time. The route sits in the
  admin group, and `Origin` is required and compared. Attach messages are accepted only on
  an upgraded socket. Afterwards, however, the session is never re-validated: a socket
  opened shortly before the token expires (or before logout, which only clears the cookie)
  can keep attaching new viewers indefinitely, because the 4 h limit is per viewer, not per
  socket. That is **SEC-027 (LOW, mandatory)**.
- **Isolation across hosts.** Agent frames are routed only to streams of the socket's own
  device, and stream IDs are server-generated 128-bit values. No finding.

**Backfill reads as an agent DoS vector (AD-015, AD-017).** Per call, a backfill is bounded:
5,000 lines, a 20,000-line scan for `skipped`, at most 2 concurrent and at most 16 queued per
host, all within the agent-wide byte budget. Nothing bounds the *rate*, though. Re-attach is
automatic (a 2 s reconnect loop, hidden-tab resume, Live toggles, several tabs), every attach
queues a fresh backfill, and backfill shares the 2 MiB/s agent budget with live streams. A
reconnect storm can keep both backfill slots and the agent budget busy, starving live viewers
of every container on that host. The browser-supplied `boundary` is also unvalidated. That is
**SEC-026 (LOW, mandatory)**.

**Gap-marker rows as an injection or spoofing channel (AD-035 §5).** Gap rows are ordinary
`container_logs` rows with `stream IN ('gap_agent','gap_server')`, and `line` holds the
count. On `main`, however, `InsertBatch` copies the agent's `stream`, `container_name` and
`timestamp` verbatim, and the table has no CHECK on `stream`. So:

- a compromised agent can forge `gap_server` rows, the server's own attestation;
- it can send non-numeric "counts";
- it can create arbitrary stream values;
- it can send an unbounded number of `container_log_gaps` entries.

Gap and line timestamps are agent-chosen. That interacts with partitioning: see SEC-021 and
SEC-023. Rendering is safe, because gap entries are structured and text is rendered as text.

**Write-time masking when an older agent sends raw lines (AD-035 §6).** The server masks every
stored line at ingest whatever the agent version, and history reads re-mask older rows. That
part is correct. The ordering across phases is not: masking lands in WP-1B (P1), while P0
widens what is stored (output of stopped and crashing containers, TTY containers) and can
ship first. That is **SEC-028 (LOW, mandatory)**.

**The retention setting (FR-149, AD-035 §2, §8.13).** No finding.

- `parseContainerLogRetention` accepts only integers from 1 to 90. Unset gives 7. Zero,
  negative, non-numeric or above-90 values give 7, with a startup Warn. Zero is therefore
  impossible, and the largest value is bounded.
- The effective value stays 7 until the partition manager owns `container_logs`, so the
  setting can never widen the row-DELETE path (NFR-024).
- One consequence to note in the docs: lowering the value drops whole days at the next hourly
  run, and cannot be undone. The setting is a host environment variable, so only someone with
  host access can change it.
- The 90-day worst case (§13.3, about 160 GiB per host at the default caps) holds only if
  SEC-021 and SEC-023 are fixed. Without them the bound is not real.

**PART-001 renumbering and `migrate force 22` (§2.4, R-3).** Renumbering PART-001 above
`000023_command_lifecycle` is correct, and keeping its internal order is correct. The
prescribed reconciliation is not safe as written. That is **SEC-022 (HIGH)**.

**Do live logs ever touch storage?** By design, no.

- The hub writes only the coalesced audit row (AD-016).
- AC-049 also covers `server_logs`.
- Backfill is an agent read relayed over WS.
- The Download is client-side.
- Live lines never enter telemetry.

One property is not stated: the agent must never route `logs_data` or `logs_event` through
its persistent paths (the CMD-LIFECYCLE command outbox under `/var/lib/riot`, or the SQLite
telemetry buffer `buffer.go`) while disconnected. Streams cancel on disconnect (AD-017), which
implies this, but it must be asserted (verification item V-1 below).

---

### R2.3 New findings

#### HIGH

##### SEC-021: Container-log timestamps are not bounded on ingest, so one agent (or a skewed clock) defeats partitioned rotation fleet-wide and suppresses new lines in the Logs view

**Severity:** HIGH
**Domain:** DoS / Data retention integrity
**Affected:** AD-035 §1 and §5 (`applyCeiling`), AD-036 (buffer acceptance rule,
`newest_stored_at`), FR-148, FR-150, NFR-025; PART-001 AD-005 and AD-008

**Location:** `container_log_repo.go::InsertBatch` (the agent's `timestamp` is copied
verbatim); the PART-001 manager (the DEFAULT partition is "never dropped"; a DEFAULT-capture
error when creating a day partition is classified benign and skipped)

**Description.** `container_logs` is partitioned by line `timestamp`, and the agent chooses
that timestamp. PART-001 routes any row with no daily partition into a forward DEFAULT
partition, which is **never dropped**. When the manager later tries to create the day
partition for a day that already has rows in DEFAULT, PostgreSQL refuses (`23514`). PART-001
logs a WARN and skips that day. That day's partition never exists, so **every** device's rows
for that day land in DEFAULT and are never rotated. Rows dated before the oldest partition
also fall into DEFAULT once the legacy partition is dropped.

PART-001 accepted DEFAULT accumulation for "rare clock-broken devices". P0 now makes
`container_logs` the table whose bounded retention is the story's headline guarantee
(NFR-025), and adds a new ingest function (`applyCeiling`) that PART-001's "no ingest-path
change" constraint does not cover.

There is a second effect. The browser buffer accepts a line only if its timestamp is at or
after the container's `newestTs` (AD-036). One stored line dated in the future makes
`newest_stored_at` and `newestTs` lie in the future, so every later real line, both refresh
and live, is silently rejected for that container.

**Attack Scenario.**
1. A compromised agent on `host-b` sends one container-log row per day for the next 365 days
   (or its clock is set a year ahead).
2. The rows go into `container_logs_default`. On each following day, the manager's create for
   that day fails with `23514` and is skipped.
3. Every device's container logs for those days accumulate in DEFAULT. Nothing is dropped, and
   storage grows without bound: the PART-001 bloat incident again.
4. Meanwhile the operator's Logs tab for that container shows nothing new: refreshed and live
   lines are older than the forged newest line.

**Required Resolution.** The ADD (AD-035 §5) must state, and tests must cover:

1. **Timestamp window.** At ingest, before the ceiling and before `CopyFrom`, every
   container-log row and every gap entry must have a timestamp within
   `[received_at − effective_retention, received_at + tolerance]`. The tolerance should match
   the merged `TelemetryFutureTolerance` (5 m). Out-of-window rows are either dropped and
   counted in one `gap_server` row stamped `received_at`, or clamped. The ADD picks one
   (D-42). A clamped row must not be stored with its original timestamp.
2. **History API.** It never returns rows dated after `now + tolerance`.
3. **Buffer.** The buffer never adopts a `newestTs` later than `now + tolerance`.
4. **Fleet-wide DEFAULT alarm.** A non-zero `DefaultRowCount` for `container_logs` raises an
   operator-visible warning. PART-001's health surface already counts DEFAULT rows; P0 must
   wire that count to an operator-visible event.
5. **Tests:**
   - `TestDUX_SEC021_FutureAndPastDatedLogRowsNotStoredOutOfWindow`;
   - `TestDUX_SEC021_GapEntryTimestampWindow`;
   - a PG test proving that a forged future row cannot prevent the next day's partition from
     being created;
   - `logBuffer.test.ts`, where a future-dated row does not suppress later lines.

**Blocks:** Implementation of WP-P0B and WP-P0C until AD-035 states items 1 to 4.

##### SEC-022: The prescribed `migrate force 22` reconciliation keys on an ambiguous version number and permits a destructive "drop first"

**Severity:** HIGH
**Domain:** Data integrity / Operational safety (production database)
**Affected:** ADD §2.4 rule 2, R-3, D-35, D-37, AD-032

**Description.** golang-migrate's `schema_migrations` stores one integer and a dirty flag.
Version 23 on a deployed database can mean any of three things:

- main's `000023_command_lifecycle` (CMD-LIFECYCLE is merged and may already be deployed);
- PART-001's branch `000023_autovacuum_tuning`;
- `fix/pg-autovacuum-bloat`'s `000023`.

Versions 24 to 28 can mean branch PART-001 files. §2.4 says to "read
`schema_migrations.version`" and, "if any database shows a branch-applied version", to run
`migrate force 22` after "confirming that the branch migration's objects either can be
re-applied idempotently **or are dropped first**". As written:

1. **The check cannot tell the cases apart.** The version alone cannot distinguish main's
   23 from a branch's 23.
   - An operator who reads 23 and concludes "main" on a branch-23 database never applies
     command_lifecycle.
   - An operator who forces a main-23 database back to 22 re-runs `000023_command_lifecycle`.
     That migration is not idempotent: it adds columns, rewrites historical statuses,
     backfills expiry, and resets `last_triggered_at`. Re-running it either fails and leaves
     the database **dirty**, or re-applies the status rewrite and the auto-update re-arm
     against live data.
2. **"Dropped first" is unbounded.** On a database that ran PART-001's branch conversion,
   the "objects" include the converted parent tables and their partitions (`container_logs`,
   `telemetry_snapshots` and four others) and `partition_conversion`. Dropping them is total
   loss of telemetry and logs.
3. **No backup step, and no dirty-state rule.** The procedure takes no backup and says nothing
   about a database whose version is dirty (a half-applied conversion). Forcing a lower
   version over a dirty mid-swap state then re-running files is undefined.
4. **Renumbered re-runs are unverified.** PART-001 states that its phases no-op on an
   already-converted table. That claim is not tested *under the new numbers*, after a force,
   on a database that ran the old numbers.

**Attack Scenario (operator error, no attacker).** A production instance was upgraded to the
CMD-LIFECYCLE release (version 23 is main's). Before the PART-001 merge, the operator follows
§2.4, sees "23", treats it as possibly branch-applied, and runs `migrate force 22`. The next
start re-runs `000023_command_lifecycle`. Either it fails and the server refuses to start with
a dirty schema, or it rewrites command history again and re-arms every auto-update policy, so
containers fleet-wide are recreated in one telemetry cycle. If the operator instead follows
"dropped first" on a PART-001 branch database, the telemetry and log tables are dropped.

**Required Resolution.** §2.4 and R-3 must be rewritten as a runbook with these properties:

1. **Backup first.** A verified logical backup (or snapshot) of the database is taken and
   restorable before any `force`. The runbook names the command and the restore test.
2. **Identify by schema fingerprint, not by number.** Before any action, the runbook checks
   which migration produced the current version by inspecting objects:
   - main 23: for example, `commands.deadline_at` exists;
   - PART-001: for example, `partition_conversion` exists and `container_logs` has
     `relkind = 'p'`;
   - the autovacuum branch: the reloptions are present.

   The procedure branches on the fingerprint. A database whose fingerprint shows main's 23 is
   **never** forced below 23.
3. **Never drop data.** "Dropped first" is limited to objects that hold no data: reloptions,
   indexes, and empty bookkeeping rows. The runbook forbids dropping or truncating any table
   that holds telemetry, logs, commands or audit.
4. **Dirty databases stop.** A dirty database is not forced to 22. It follows PART-001's own
   dirty-recovery procedure (force the last good version, re-run) under its ADD, or stops for
   a human decision.
5. **Idempotence under the new numbers.** A PG test runs the renumbered PART-001 files on a
   database already converted under the old numbers, after the prescribed `force`, and asserts
   both a no-op and no data change. A second test covers each fingerprint branch.
6. **Recorded results.** The implementation report records, per database, the fingerprint
   observed, the action taken and the version afterwards. No reconciliation is run without
   the explicit approval of that database's operator (D-43).

**Blocks:** The WP-P0C merge and any deploy of the PART-001 merge. It also blocks running the
§2.4 procedure on any deployed database until the ADD carries properties 1 to 6.

#### MEDIUM

##### SEC-023: Stored-log ingest trusts agent-supplied `stream` and container names, and the per-container ceiling can be bypassed by rotating container names

**Severity:** MEDIUM
**Domain:** Integrity (spoofing) / DoS (DB growth, server CPU)
**Affected:** AD-035 §3 and §5 (`applyCeiling`, gap rows), §7.2 ("`stream` has no CHECK
constraint, so no schema change is needed"), AD-036 (gap parsing), NFR-025, FR-153, FR-159

**Description.**
1. **`stream` is copied verbatim** (`InsertBatch`). An agent can send ordinary entries with
   `stream = 'gap_server'` and `line = '999999+'`. The history API then renders them as a
   *server* attestation ("server cap dropped N lines"), and the agent and server gap kinds
   become indistinguishable. Arbitrary `stream` strings, and non-numeric `line` text in gap
   rows, reach the history mapper.
2. **`container_log_gaps` is unbounded.** It has no bound on entries per push, no bound on
   `count`, no validation of `container_name` or `container_id`, and no `reason` enum
   (`reason` is used later).
3. **The rolling limit is per `(device, container)`, with no per-device bound.** The
   telemetry endpoint has no per-device rate limit (pre-existing). An agent that rotates
   container names gets 1,000 lines and 256 KiB per new name, up to the 20,000-row outer cap
   per push, at any push rate. NFR-025's "per-host daily maximum implied by the caps" does not
   hold. Server `MaskLine` at ingest runs on up to 20,000 × 16 KiB per push, unbounded per
   device per second.

**Attack Scenario.** A compromised agent posts telemetry ten times a second. Each push carries
20,000 lines under 20 freshly invented container names, plus forged `gap_server` rows. Within
retention, the partitions grow by gigabytes per hour, and server CPU goes to masking. The
operator sees "server cap" markers the server never produced.

**Required Resolution.**
1. **Stream allowlist.** The server sets `stream` itself: ordinary entries must be `stdout` or
   `stderr`, otherwise the entry is dropped. Gap rows are created only by server code, from
   validated `container_log_gaps`, never from entries. The ADD should also add a CHECK
   constraint on the partitioned parent (in WP-P0C's migration), or state why not.
2. **Gap entry validation.** At most one entry per container per push; container name matching
   `ContainerNamePattern`; `count` a non-negative bounded integer; `reason` in the enum;
   timestamps per SEC-021. Invalid entries are dropped.
3. **Per-device bounds.** A per-device rolling ceiling on rows and bytes per 60 s, sized from
   the configured caps × the device's container count in its latest snapshot (or a fixed
   maximum). Container names not present in the device's latest snapshot are refused, or
   counted against a small per-device "unknown" budget. Masking runs only after the ceiling.
4. **Tests:**
   - `TestDUX_SEC023_ForgedGapStreamNotStored`;
   - `TestDUX_SEC023_GapEntriesValidatedAndBounded`;
   - `TestDUX_SEC023_NameRotationHeldByDeviceCeiling`;
   - `TestDUX_SEC023_MaskAfterCeiling`.

**Blocks:** No. Condition C-23. It must be stated in AD-035 before WP-P0B starts (see the gate
table).

##### SEC-024: Stack path trust: the writable-path check misses bind-backed named volumes, and the P1 interim auto-update relies on agreement alone

**Severity:** MEDIUM
**Domain:** Privilege escalation (container to host); residual of SEC-002
**Affected:** AD-038 §1 and §3, AD-027 precheck, `auto_update.go` (P1)

**Description.**
1. **Bind-backed named volumes.** AD-038 §3 collects host sources from `Type=bind, RW=true`
   mounts only. A named volume created with the `local` driver and the options
   `type=none,o=bind,device=<host path>` is a read-write host path, but inspect shows it as
   `Type=volume` with source `/var/lib/docker/volumes/<name>/_data`. A hostile container can
   therefore write `compose.yaml` into a stack directory through such a volume, without
   tripping the check.
2. **P1 interim.** In P1, auto-update dispatches when "the members agree". There is not yet a
   `docker_stacks` memory, and no agent writable-path check (both arrive in P3). If the real
   stack's containers are absent (Down, or removed) and a hostile standalone container
   labelled into that project is the only member reporting an update, it "agrees with itself".
   The pre-existing hijack survives in that window.

**Attack Scenario.** Stack `media` is Down. A standalone `helper` container from a
supply-chain image carries `com.docker.compose.project=media` with working dir and config
files pointing into its own bind mount, and advertises an update. The P1 auto-update agrees
on the only member, and dispatches `docker_update{compose_work_dir}` into the attacker's
directory.

**Required Resolution.**
1. AD-038 §3 also treats, as host sources, every mount whose volume uses the `local` driver
   with bind options (`o` contains `bind`, plus a `device` path). The agent resolves those
   from `VolumeInspect(...).Options`. Any other volume driver that exposes a host path should
   be handled the same way, or documented.
2. Move the agent-side writable-path refusal (AD-038 §3) for `docker_update{compose_work_dir}`
   into WP-1A, so that it applies from P1. Until P3, the P1 auto-update dispatches a stack
   update only when at least one member was created by compose itself. The ADD defines the
   test, for example a container carrying `com.docker.compose.config-hash` plus
   `com.docker.compose.container-number`. Otherwise it skips with a conflict Warn. Note that
   these labels are forgeable too, so item 2's writable check is the binding control.
3. **Tests:**
   - `TestDUX_SEC024_BindBackedVolumeCountsAsWritable`;
   - `TestDUX_SEC024_P1AutoUpdateRefusesContainerWritableWorkdir`.

**Blocks:** No. Condition C-24. Item 2 should land in WP-1A, and item 1 must be in AD-038
before WP-3A starts.

#### LOW / INFORMATIONAL

##### SEC-025 (LOW, mandatory): Shared-stream lifecycle races and orphaned agent follows

**Affected:** AD-016 ("Detach and stop", "A frame for an unknown stream is dropped"), AD-017

**Required properties:**

- The linger timer and attach are serialized. An attach during the linger cancels the pending
  unfollow. Once `logs_unfollow` has been sent, a new attach opens a **new** stream with a new
  `stream_id`, and frames for the old ID are discarded.
- A frame from the agent for an unknown `stream_id` makes the server send `logs_unfollow` for
  that ID (rate-limited), so the agent never keeps following a stream the server has
  forgotten.
- The agent cancels any stream that has had no server-originated message for a bounded idle
  period. That period should be longer than the keepalive, for example 5 minutes, unless the
  server re-asserts the stream.
- **Tests:**
  - `TestDUX_SEC025_AttachDuringLingerKeepsStream`;
  - `TestDUX_SEC025_UnknownStreamFrameTriggersUnfollow`;
  - `TestDUX_SEC025_AgentReapsOrphanedFollow`.

**Attack Scenario:** a lost `logs_unfollow` (write error) leaves the agent following a
container until it reconnects. That uses one of its 8 stream slots and part of the byte
budget. Repeated, the host reports `too_many_streams` to every operator.

##### SEC-026 (LOW, mandatory): Backfill has no rate limit and shares the live budget

**Affected:** AD-015, AD-016 step 3, AD-017 "Backfill", AD-037

**Required properties:**

- Backfills are rate-limited per (socket, container), for example at most 1 per 10 s.
- Concurrent backfills for the same (device, container, boundary) are coalesced into one agent
  read, fanned out to the requesters.
- Backfill draws from its own share of the agent byte budget (for example at most 50 %), so
  that live streams always keep a share.
- `boundary` must parse as RFC3339 with microseconds and lie within
  `[now − effective retention, now + tolerance]`. Otherwise the request is treated as "no
  boundary" (last 100 lines).
- **Tests:**
  - `TestDUX_SEC026_ReattachStormBounded`;
  - `TestDUX_SEC026_LiveKeepsBudgetDuringBackfill`.

**Attack Scenario:** a client bug or a flapping network makes 8 tabs re-attach every 2 s. Both
backfill slots stay busy, and the shared budget starves live tails of every container on that
host.

##### SEC-027 (LOW, mandatory): The live-log socket outlives the session it was authorized with

**Affected:** AD-016 (auth checked only at upgrade; the 4 h limit is per viewer, not per
socket), §8.10

**Required properties:**

- The server records the JWT `exp` at upgrade, closes the socket at that time with
  `session_expired`, and refuses `attach` after it.
- A socket's total lifetime is capped (for example at 4 h, like a viewer). The client then
  reconnects through a fresh upgrade, which re-checks the cookie.
- **Test:** `TestDUX_SEC027_SocketClosedAtTokenExpiry`.

The same gap exists on `/ws` and `/ws/terminal`. Fixing those is recommended, not required by
this story.

**Attack Scenario:** an operator logs out at a shared machine. A tab left open keeps a live
socket that can attach to any container's logs for as long as it stays open.

##### SEC-028 (LOW, mandatory): P0 widens stored capture before write-time masking exists

**Affected:** AD-035 §4 and §6, §4.2 (masking is in WP-1B; capture of stopped and TTY
containers is in WP-P0A)

FR-154 and FR-155 newly store crash output and TTY output: stack traces and configuration
dumps printed on failure. In P0 these are stored unmasked, and stay so for the retention
period. History reads re-mask from P1 onward, but the raw rows sit in the database until they
rotate.

**Required property:** `redact.MaskLine` at ingest ships in WP-P0B (move the masking subset of
`internal/redact` into P0), **or** the WP-P0A collection of stopped and TTY containers is not
released before WP-1B's write-time masking.

**Test:** `TestDUX_SEC028_StoredLinesMaskedFromP0`.

**Attack Scenario:** a crash-looping container prints its DSN with password on every exit. P0
stores each copy unmasked for the full retention period.

##### SEC-029 (LOW): Audit gaps left in the SEC-009 resolution

**Affected:** AD-024 (`command` audit rows are written only for "each operator-created Docker
command"; `commands` rows still cascade on device delete), §7.2 P2 down migration ("re-adding
the FK requires deleting orphan rows first")

**Required properties:**

- A `command` audit row (long class) is written for **every** operator-created command in the
  AD-024 long-retention action list, including `reboot`, `shutdown`, `os_update`,
  `agent_update`, `agent_uninstall` and `sync_privileges`, not only for Docker actions.
- The P2 down migration must not delete audit rows. It re-adds the FK as `NOT VALID`, or omits
  it.

**Attack Scenario:** a stolen session reboots `host-a` and then deletes the device. The
command row cascades away, and no audit row records the reboot.

##### Verification items for QA (designed correctly; cannot be confirmed from the ADD)

- **V-1:** `logs_data`, `logs_event` and backfill frames never enter the agent's command
  outbox, the SQLite telemetry buffer, or any file. Test:
  `TestDUX_V1_LiveFramesNeverPersistedOnAgent`, with the agent disconnected mid-stream.
- **V-2:** viewer isolation. Two viewers with different filters, Pause and stream toggles
  receive identical frame sequences from the server. A backfill reaches only its requester.
- **V-3:** the AC-049 extension holds with several viewers, reconnects and backfills (no
  `container_logs` call; no `server_logs` record containing a fixture line).
- **V-4:** gap markers render from structured entries only. A stored line whose text imitates
  a marker renders as an ordinary line.
- **V-5:** `parseContainerLogRetention` table: `0`, `-1`, `91`, `7.5`, `1e2`, empty, and a
  string give 7 plus a Warn; `1` and `90` are accepted.

---

### R2.4 Updated mandatory conditions (additions)

These are added to C-1 to C-16:

| ID | Finding | Tests |
|---|---|---|
| C-21 | SEC-021 | the timestamp-window tests, the PG partition-creation test and the buffer test listed in SEC-021 |
| C-22 | SEC-022 | the fingerprint-branch PG tests and the renumbered re-run idempotence test. The runbook is reviewed by the product owner before use on any deployed database |
| C-23 | SEC-023 | the forged-stream, gap-validation, name-rotation and mask-after-ceiling tests |
| C-24 | SEC-024 | the bind-backed volume and P1 auto-update tests |
| C-25 to C-29 | SEC-025 to SEC-029 | the tests named in each finding |
| V-1 to V-5 | verification items | as listed |

---

### R2.5 Updated per-phase gate (revision 2)

"ADD amendment required" means that the architect must amend the ADD before that phase's work
packages start. "Implementation conditions" are verified by QA before the phase is signed off.
The revision 1 gates for P1 to P4 are satisfied by ADD rev 2, except where noted.

| Phase | ADD amendment required before start | Implementation conditions (QA) |
|---|---|---|
| **P0** (WP-P0A, P0B, P0C) | **SEC-021** items 1-4 (timestamp window at ingest, history and buffer future bound, DEFAULT alarm). **SEC-023** items 1-3 (stream allowlist, gap validation, per-device ceiling; mask after the ceiling). **SEC-028** (masking at ingest in P0, or hold stopped and TTY capture). **SEC-022** items 1-6: the reconciliation runbook. It gates the **WP-P0C merge, any PART-001 deploy, and any use of `migrate force` on a deployed database** | C-21, C-22, C-23, C-28; V-5 |
| **P1** (WP-0, WP-1A, 1B, 1C) | none outstanding (SEC-001, SEC-003 label items and SEC-004 item 1 are closed). **SEC-024 item 2** is strongly recommended in WP-1A | C-1, C-3 (label cases), C-4, C-11, C-13; C-24 item 2 if adopted |
| **P2** (WP-2A, 2B, 2C) | **SEC-025**, **SEC-026**, **SEC-027** (one-paragraph additions to AD-016 and AD-017); **SEC-029** (AD-024 audit coverage; non-destructive down migration) | C-3 (inspect and mask), C-4, C-6, C-8, C-9, C-12, C-14, C-15, C-16, C-25, C-26, C-27, C-29; V-1 to V-4 |
| **WP-2D** (I/O history) | none new | none new |
| **P3** (WP-3A, 3B, 3C) | **SEC-024 item 1** (bind-backed volumes as writable sources). Revision 1 items (SEC-002, SEC-005, SEC-003 items 1 and 6) are closed | C-2, C-3 (compose and `.env`), C-5, C-6 (merged view), C-10, C-16, C-24 |
| **P4** (WP-4A, 4B, 4C) | none outstanding (SEC-007 item 1 and SEC-010 item 2 are closed) | C-7, C-10 |

---

### R2.6 Decisions for the product owner (additions and changes)

| ID | Decision | Security recommendation |
|---|---|---|
| D-30 | Scope of the origin control | Unchanged: **(b)**. ADD rev 2 implements (a), so `POST /devices/{id}/commands` (modified) is covered, but other existing state changes (device delete, settings, fleet bulk patch) stay exposed to same-site forgery (R-12). |
| D-37 | PART-001 merge scope | **Merge whole**, endorsed. It is conditional on SEC-022's runbook, and on SEC-021's ingest window being in place before `container_logs` is routed to partitions. |
| D-34 | Retention range 1..90 | **Endorsed.** Note in the docs that lowering the value is irreversible at the next hourly run. |
| D-41 | Lower-bound gap counts | **Endorsed.** Bounded agent work matters more than exact counts. |
| **D-42 (new)** | Out-of-window container-log timestamps (SEC-021): drop and count, or clamp to `received_at` | **Drop and count** (one `gap_server` row). Clamping stores lines at a false time, and could duplicate lines at the history and live boundary. |
| **D-43 (new)** | Whether the reconciliation runbook (SEC-022) may be run on a deployed database without a per-database sign-off | **No:** each database needs the explicit approval of its operator, after the fingerprint and backup steps are recorded. |

---

## Revision 3 addendum

**Date:** 2026-10-19
**Scope:** delta review of FRD rev 3 (`7ea602e`) and ADD rev 3 (`6d3b268`), compared with
the revision 2 basis (`30dd0f7`). Commit `b47c751` only replaced personal names in this
report, and Clod checked that diff.

Clod read these parts of ADD rev 3: §0.0, §2.2, §2.4, §2.4.1, §2.4.2, AD-016, AD-017,
AD-018, AD-024, AD-025, AD-035 §1a, §5 and §6, AD-036, AD-038, §6.1 and §6.2, §7.2, §17,
§18 and §19. Clod also read FRD rev 3, FR-166 to FR-175 and §8.13.

The following were checked against real code:

- `cmd/riot-server/main.go`: there is no subcommand dispatch today. The server builds itself
  and calls `Start`, which runs migrations (`server.go:90`).
- `internal/server/db/db.go` `RunMigrations`: it uses golang-migrate's
  `NewWithSourceInstance`.
- `internal/server/handlers/setup.go:95`: the JWT secret is stored in `admin_config`, so a
  database dump contains it.

The product owner settled these decisions: D-1 (compose read-only), D-21 (180 days for the
long audit class, 90 days for the short class), D-31 (no personal names), D-35 with D-37
(PART-001 merged whole and renumbered, as P0's first step), D-42 (**re-stamp**, against this
review's drop recommendation), and D-43 (sign-off per database). This review accepts each one,
and assesses D-42 and D-21 below.

**Updated verdict: APPROVED WITH CONDITIONS (phase-gated).**

- Every finding from SEC-021 to SEC-029 is closed in the ADD.
- No ADD amendment is outstanding for any phase.
- This revision adds no new CRITICAL, HIGH or MEDIUM finding.
- It adds four LOW findings (SEC-030, SEC-031, SEC-033 mandatory; SEC-034 recommended) and
  one INFO (SEC-032). They are implementation conditions, verified by QA.
- One operational gate remains for P0: the §2.4.1 runbook result and a verified backup for
  each database before the PART-001 deploy.

---

### R3.1 Status of the revision 2 findings

| Finding | Status | Where (ADD rev 3) | Note |
|---|---|---|---|
| SEC-021 | **Closed** | AD-035 §1a; AD-036; AD-016; WP-P0C | The window `[receivedAt − effective retention, receivedAt + 5 min]` uses the server clock and runs before the ceiling, masking and `CopyFrom`. Out-of-window rows get `timestamp = receivedAt`, with `agent_ts` kept, so an agent clock can never reach the DEFAULT partition. History never returns rows later than `now() + 5 min`. The buffer never adopts a future `newestTs`. The DEFAULT-row event is kept. Live lines are handled the same way. Residuals: SEC-031, SEC-032. |
| SEC-022 | **Closed** | §2.4.1, §2.4.2, R-3 | Verified `pg_dump` first; classification by fingerprint (F-LC, F-AV, F-PT); never force below 23 when F-LC is present; stop on dirty or on any unknown combination; no drops or truncates; sign-off per database; a no-op test for the renumbered files. Read-only enforcement of `db-fingerprint` and backup handling are **SEC-030**. |
| SEC-023 | **Closed** | AD-035 §5 | The server owns `stream`; gap entries are validated and must name a container in the push; the unknown budget is 200 lines and 64 KiB per 60 s; the per-device ceiling is capped at 20,000 lines and 5 MiB per 60 s; masking runs after the ceiling. Not adding a CHECK constraint is justified. |
| SEC-024 | **Closed** | AD-038 §1 and §3; WP-1A; D-44 | Bind-backed `local` volumes and host-path volume drivers count as writable sources. The agent refusal covers `docker_update{compose_work_dir}` from P1. The P1 auto-update needs a member created by compose. `doctor` lists the affected stacks. |
| SEC-025 | **Closed** | AD-016, AD-015, AD-017 | Attach, detach and linger are serialized per stream mutex; retired IDs are never reused; a frame for an unknown ID triggers `logs_unfollow`; `logs_keep` runs every 60 s; the agent reaps orphaned follows after 5 min. |
| SEC-026 | **Closed** | AD-016 step 3, AD-017 | At most one backfill per 10 s per (socket, container), deferred rather than refused; coalesced per (device, container, boundary); boundary validated against server time; its own 1 MiB/s bucket inside the 2 MiB/s agent budget. |
| SEC-027 | **Closed** | AD-016, §8.10 | The JWT `exp` is recorded at upgrade, and the socket is closed with `session_expired` at that time; attach is refused afterwards; socket lifetime is capped at 4 h. `/ws` and `/ws/terminal` are noted as R-17, not fixed. |
| SEC-028 | **Closed** | AD-035 §6, §2.2, WP-P0B | Write-time `MaskLine` is in WP-P0B. A release rule ensures that no release contains WP-P0A or WP-P0C without WP-P0B. |
| SEC-029 | **Closed** | AD-024, AD-025, §7.2 | `Dispatch` writes a `command` audit row for every operator command, Docker or not. The P2 down migration re-adds the FK `NOT VALID` and never deletes audit rows. Observation: commands initiated by the system (auto-update, auto-patch with reboot) are recorded only in `commands` (90 days). Auditing changes to auto-update and auto-patch *policies* would close that gap. It is recommended, not required. |

---

### R3.2 The re-stamp design (D-42)

The product owner chose to re-stamp out-of-window lines instead of dropping them. This review
**accepts** that choice: no log data is lost, and SEC-021's property still holds. No row is
ever stored at an out-of-window partition time. Each question in turn:

- **Is the window computed from server time only?**
  - At ingest (`applyWindow`) and in the live hub, yes. `receivedAt` is the server clock, and
    the effective retention and the 5-minute tolerance are server constants.
  - The agent's time never decides the partition.
  - **One exception:** AD-036's browser buffer bounds `newestTs` by "the browser's current
    time plus 5 minutes". An operator browser whose clock is wrong is not under rIOt's
    control. That is **SEC-031** item 2.
- **Can a hostile agent flood re-stamped rows?** No more than ordinary rows. The window runs
  before the ceilings, so re-stamped rows count against the per-container ceiling, the
  per-device ceiling (capped at 20,000 lines and 5 MiB per 60 s) and the unknown budget,
  like any other row. Re-sending an old backlog to keep it "one more retention period" is
  bounded by the same ceilings. Each device raises at most one `clock_skew` event per
  transition, and the event text carries only the skew in seconds. No finding.
- **Can it forge `agent_ts` to mislead an investigator?** Yes, but this adds nothing a
  compromised agent cannot already do. Every stored log timestamp, in-window ones included,
  is agent-asserted. A compromised agent can backdate fabricated lines to any time within
  the retention period, and they look exactly like genuine history. `agent_ts` only records
  the claim on rows the server did *not* believe. This is a pre-existing evidentiary limit,
  and it must be stated to operators: **SEC-032 (INFO)**.
- **Can forged old lines be made to look current?** An out-of-window old line is placed at
  receive time, but FR-174 requires the skew marker, with *both* times as text and not by
  colour alone. It is therefore visibly a re-stamped line. A compromised agent that wants a
  forged line to look current simply sends it with a current time, and no design can prevent
  that (SEC-032). No new finding.
- **Ordering confusion in the UI and in downloads.**
  - In the UI, the view orders by `t` (receive time), and each re-stamped line within an
    ingest gets a monotonic microsecond offset. A re-stamped line can therefore appear after
    in-window lines that the device actually produced later. R-18 documents this, and the
    marker shows both times. Acceptable.
  - The **Download** (AD-036) is specified only as "all buffered lines … masked". It does not
    say it keeps `agent_t`, the skew flag or the gap markers. A downloaded file, which is what
    an investigator keeps, would present re-stamped lines as if they happened at receive time,
    with no trace of the skew. That is **SEC-031** item 1.
- **Is the UI's distinction clear?** Yes in the view: FR-174 and FR-175, AD-036 `LogLines`,
  both times as text, not colour only. No in the download: SEC-031.

---

### R3.3 The §2.4.1 runbook and `riot-server db-fingerprint`

The runbook is now safe in substance:

- **Backup first and verified.** `pg_dump --format=custom`, `pg_restore --list` covering the
  four tables, an optional restore into a scratch database, and no later step without it.
- **Fingerprints decide, not version numbers.** F-LC requires both `commands.deadline_at` and
  `commands_origin_check`. F-PT detects the conversion. Overlapping version ranges
  (24..28 under branch numbering, and 24 or higher under main numbering) are separated by F-LC.
- **Never force below 23 with F-LC.**
- **The only write is `force`,** and it needs sign-off.
- **No drop, truncate or delete.**
- **Stop on dirty, and stop on any unlisted combination.**
- **Re-verify after the upgrade,** and restore only with sign-off.
- **Production** is recorded as "23, F-LC only", needing a backup and no force.

These are the gaps, all LOW, collected in **SEC-030**:

1. **"It performs no writes" is a claim, not an enforced property.** Two specific traps:
   - golang-migrate's postgres driver, when instantiated (`WithInstance`), runs
     `CREATE TABLE IF NOT EXISTS schema_migrations` and takes an advisory lock. The
     subcommand must read `schema_migrations` with plain SQL.
   - Today `main.go` has no subcommand dispatch, and the normal path calls `Start`, which
     runs `RunMigrations`. If the dispatch is added after `server.New` or `Start`, a
     "fingerprint" run could migrate the database.
2. **The output and error text are unspecified.** The runbook says the fingerprint, version
   and backup evidence go into the implementation report, and that report is a public-repo
   document. The subcommand must never print the database URL, password, host, port, user
   or database name, and must sanitize driver errors.
3. **Backup handling.** The dump contains `admin_config` (including the stored JWT signing
   secret), device API key hashes, notification channel settings and every log. A leaked
   dump allows forged sessions. The runbook does not say where the dump lives, what mode it
   has, or when it is deleted. The `pg_dump … "<connection>"` form also puts the password on
   the command line and into shell history.
4. **Writes during the procedure.** The runbook does not require the server to be stopped
   between the backup and the upgrade. Rows written after the backup are lost if step 5's
   restore is ever used.
5. **F-AV is not listed in the post-upgrade fingerprint row.** After the renumbered tuning
   file runs, or if the reloptions were applied by hand, a main-numbered database shows
   F-LC + F-PT + F-AV. That combination is not in the step 4 table, so a later run of the
   runbook would "stop". This fails safe, but it is a false alarm the table should encode.
6. **The renumbered no-op test checks seeded rows and DDL,** but not the bookkeeping row in
   `partition_conversion`. Re-running the bookkeeping file could recompute `cutoff` from the
   current data and shift legacy-partition handling.

---

### R3.4 D-21 (180 days long class, 90 days short): audit assessment

This is **acceptable**, with one condition.

- **Why 180 days is acceptable.** This review recommended at least 365 days, so 180 is
  shorter. It still covers the typical window in which intrusions are detected, for a
  single-tenant homelab product, and it keeps the long-class rows (`terminal` including
  `_host` shells, destructive or privileged commands, `stack_paths` and `device_delete`)
  twice as long as the 90-day short class and the `commands` table.
- **What is preserved.** The trail now survives device deletion and downgrade (SEC-029), so
  the main erasure paths are closed.
- **The condition.** The first purge after the P2 upgrade irreversibly deletes `terminal`
  rows older than 180 days. Today those rows are kept forever. That historical forensic
  record cannot be recovered after the upgrade. The release notes already mention the
  deletion. Operators who want that history need an explicit, documented step before
  upgrading: **SEC-033 (LOW, mandatory)**, and decision D-45.
- **Note.** Audit rows live in the database that the admin session can influence (device
  delete is now audited, but there is no tamper-evident or off-host copy). That is
  unchanged, acceptable for this product, and recorded only for awareness.

---

### R3.5 Public-repo hygiene (FRD rev 3, ADD rev 3, this report)

Scanned for:

- personal names, including the product owner's first and last name;
- the operator's machine names;
- IPv4 literals outside RFC 5737;
- email patterns;
- home and user paths, drive letters;
- private DNS suffixes.

**Result: clean.** No personal name remains in any of the three documents (D-31 met,
**SEC-018 closed**). The only address-like and email-like hits are placeholders:
`192.0.2.x`, `example.internal`, and a `…@registry.example.internal` credential example in
this report. §2.4.2 records the production deployment by release version only, with no host
or address. Continuing obligation: the per-database runbook records (SEC-030 item 2) and any
`db-fingerprint` output pasted into implementation reports must follow the same rule.

---

### R3.6 New findings

#### SEC-030 (LOW, mandatory): `db-fingerprint` must be read-only by construction and secret-free; runbook backup handling

**Domain:** Operational safety / Secrets
**Affected:** §2.4.1, §6.2 (`cmd/riot-server/main.go`, `db/fingerprint.go`), D-43

**Attack Scenario (operator error):** the operator runs `riot-server db-fingerprint` on a
database that is not yet classified, and the subcommand is wired after `Start`. It applies
pending migrations, which is exactly the action the runbook exists to gate. Separately, the
command prints its connection error, including the database URL, and the operator pastes it
into a public implementation report.

**Required properties:**

1. **Dispatch first.** The subcommand is dispatched in `main` before `server.New` and before
   any code path that can reach `RunMigrations`. It never instantiates golang-migrate. It
   reads `schema_migrations` and the catalog with plain SQL.
2. **Read-only by construction.** It connects with `default_transaction_read_only=on` (a
   runtime parameter) and runs inside a `READ ONLY` transaction, so an accidental write
   fails. The runbook recommends running it, and steps 2 and 3, under a read-only database
   role.
3. **No identifying output.** It prints only: version, dirty, the F-LC, F-AV and F-PT
   booleans, the classification, and the action class. It never prints the connection
   string, password, host, port, user or database name. Connection errors are reduced to a
   fixed message plus an error class. Its exit code is non-zero for every "stop" class.
4. **Dump handling.**
   - Dumps are written with mode `0600`, outside any repository or worktree, and are deleted
     or moved to the operator's backup store after the upgrade is verified.
   - The connection password is supplied through `PGPASSFILE` or the environment, not on the
     command line.
   - The runbook states that the dump contains the session-signing secret and must be handled
     like a credential.
5. **No writes after the backup.** The runbook requires the server to be stopped (or
   write-quiesced) from the backup until step 5 succeeds.
6. **Fingerprint table and tests.**
   - The step 4 table includes "F-LC + F-PT + F-AV at main numbering → None", and
     "F-LC + F-AV at 23 (reloptions applied by hand) → None, no force".
   - `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB` also asserts that
     `partition_conversion` is unchanged.

**Tests:**
- `TestDUX_SEC030_FingerprintCreatesNothing`: against a schema without `schema_migrations`,
  no table is created and no advisory lock is taken.
- `TestDUX_SEC030_FingerprintReadOnlySession`.
- `TestDUX_SEC030_FingerprintOutputHasNoDSN`: covers both a bad password and an unreachable
  host.

**Blocks:** No. Condition C-30 (P0, WP-P0C). It must hold before the subcommand or the
runbook is used on any deployed database.

#### SEC-031 (LOW, mandatory): Downloads must keep the skew evidence; the buffer's future bound must use server time

**Domain:** Integrity of exported evidence
**Affected:** AD-036 (Download; buffer "browser's current time plus 5 minutes"), FR-174,
FR-175

**Attack Scenario:** a host with a dead RTC battery (or a compromised agent) produces
re-stamped lines. The operator downloads the log for an incident ticket. The file shows those
lines at receive time with no skew marker, so the investigator builds the timeline on the
wrong times. Separately, an operator browser whose clock is 2 hours slow treats every new
line as "future". The view's `newestTs` stops advancing, and refresh and de-duplication
behave erratically.

**Required properties:**

1. **The Download keeps the evidence.** For every re-stamped line it emits both the receive
   time and the agent-reported time, and a textual skew marker, for example
   `<t> [clock-skew agent=<agent_t>] <line>`. Gap markers are emitted as text lines in place.
   A header line states that times are server receive times for marked lines.
2. **The future bound uses server time.** It is derived from a server-supplied time: a
   `server_now` field in the history response and in the WS session message. The bound is
   that time plus the elapsed monotonic time since it was received. It is never the browser's
   wall clock.

**Tests:**
- `logDownload.test.ts`: a re-stamped line and a gap appear with both times and the marker.
- `logBuffer.test.ts`: a browser clock offset of ±2 h does not change acceptance.

**Blocks:** No. Condition C-31 (P1 for the history buffer and Download; P2 for live).

#### SEC-032 (INFO): Stored container-log times are agent-asserted

**Domain:** Evidentiary limits
**Affected:** AD-035 §1a, FRD §8.13, R-18

Within the window, `timestamp` is whatever the agent claims, and the window reaches back the
full retention period. A compromised agent can therefore insert fabricated lines into any day
of retained history, indistinguishable from genuine ones. Re-stamping does not change this,
and it is not a regression.

The Logs tab notice, or the README, should say that stored log times are reported by the host
and are not tamper-evident. If evidentiary use matters later, a server receive-time column for
every row would let investigators bound when each line actually arrived. That would cost about
8 bytes per row on a high-churn table, so it is recorded here for awareness only.

**Attack Scenario:** a compromised agent backdates fabricated lines by three days, and an
investigator later reads them as genuine history.

**Blocks:** No.

#### SEC-033 (LOW, mandatory): Preserve the historical terminal audit before the first 180-day purge

**Domain:** Audit retention
**Affected:** AD-024 "Upgrade effect", AD-025, D-21 (settled), D-45 (new)

**Attack Scenario:** after the P2 upgrade, an operator needs to know who opened a `_host`
shell eight months earlier. The first purge has already deleted that row, and nothing kept a
copy.

**Required properties:**

- The P2 release notes and upgrade steps say, *before* the upgrade, that the first purge
  irreversibly deletes `terminal` audit rows older than 180 days.
- They give a documented export step for operators who want to keep that history (for
  example a `COPY (SELECT … FROM terminal_sessions WHERE started_at < now() − 180 days)` to
  a `0600` file outside the repository).
- The first purge after upgrade logs the number of rows it deleted, with no row content.

**Test:** `TestDUX_SEC033_FirstPurgeLogsDeletedCount`.

**Blocks:** No. Condition C-33 (P2).

#### SEC-034 (LOW, recommended): Audit changes to auto-update and auto-patch policies

**Domain:** Audit coverage
**Affected:** AD-024; `PUT …/auto-updates`, `DELETE …/auto-updates/{target}`,
`PUT …/auto-patch`

Destructive commands created by the system (auto-update recreates, auto-patch with reboot)
are attributed to `system` and are kept only in `commands` (90 days). The operator action
that caused them, enabling the policy, is not audited.

**Attack Scenario:** a stolen session enables auto-patch with reboot on every host. Every
resulting reboot is attributed to "System", and nothing records who enabled the policy.

**Recommended:** write a long-class audit row (kind `command`, action `policy_change`, or a
new kind) for each policy change.

**Blocks:** No.

---

### R3.7 Updated mandatory conditions (additions)

| ID | Finding | Tests |
|---|---|---|
| C-30 | SEC-030 | `TestDUX_SEC030_FingerprintCreatesNothing`, `TestDUX_SEC030_FingerprintReadOnlySession`, `TestDUX_SEC030_FingerprintOutputHasNoDSN`; the extended `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB`. QA reviews the runbook text for items 4 to 6 |
| C-31 | SEC-031 | `logDownload.test.ts`, `logBuffer.test.ts` (clock offset) |
| C-33 | SEC-033 | `TestDUX_SEC033_FirstPurgeLogsDeletedCount`. QA checks the release notes |

---

### R3.8 Updated per-phase gate (revision 3)

"ADD amendment required" means that the architect must amend the ADD before that phase's work
packages start. "Implementation conditions" are verified by QA before the phase is signed off.

| Phase | ADD amendment required before start | Implementation conditions (QA) | Operational gate |
|---|---|---|---|
| **P0** (WP-P0A, P0B, P0C) | **None outstanding** (SEC-021, SEC-022, SEC-023 and SEC-028 are closed) | C-21, C-22, C-23, C-28, **C-30**; V-5; the release rule in §2.2 (no WP-P0A or WP-P0C without WP-P0B) | For **each** database: a verified backup and the §2.4.1 classification recorded, with operator sign-off for any `force` (D-43). This happens before the PART-001 deploy. Production: backup plus a re-run fingerprint; no force. The server is stopped from backup to verification (SEC-030 item 5) |
| **P1** (WP-0, WP-1A, 1B, 1C) | None outstanding | C-1, C-3 (labels), C-4, C-11, C-13, C-24 item 2 (D-44), **C-31** (history buffer and Download) | P1 release notes list the stacks that stop auto-updating (D-44) |
| **P2** (WP-2A, 2B, 2C) | None outstanding | C-3 (inspect and mask), C-4, C-6, C-8, C-9, C-12, C-14, C-15, C-16, C-25, C-26, C-27, C-29, **C-31** (live), **C-33**; V-1 to V-4 | Before upgrade: the release notes give the terminal-audit export step (SEC-033, D-45) |
| **WP-2D** | None | None new | — |
| **P3** (WP-3A, 3B, 3C) | None outstanding | C-2, C-3 (compose and `.env`), C-5, C-6 (merged view), C-10, C-16, C-24 | — |
| **P4** (WP-4A, 4B, 4C) | None outstanding | C-7, C-10 | — |

---

### R3.9 Decisions for the product owner (additions)

| ID | Decision | Security recommendation |
|---|---|---|
| D-42 (settled: re-stamp) | Out-of-window log timestamps | **Accepted.** SEC-021 is met. The conditions are SEC-031 (downloads keep the skew evidence, server-time bound), plus SEC-032's notice. |
| D-21 (settled: 180/90) | Audit retention | **Accepted**, subject to SEC-033. |
| D-44 | Writable-path refusal from P1 | **Endorsed (from P1).** It closes the pre-existing auto-update hijack early. |
| **D-45 (new)** | Whether to archive `terminal` audit rows older than 180 days before the P2 upgrade deletes them | **Yes for any operator who wants that history:** export to a `0600` file kept offline (not in any repository). The release notes state that otherwise the rows are gone for good. |

---

## Revision 3.1 addendum

**Date:** 2026-10-19
**Scope:** delta review of ADD rev 3.1 (`13b1fdb`, compared with `445730f`). This revision
answers the product owner's standing requirement: everything this story writes heals itself,
and no exception may let the server fill up with data, logs, images or anything else.

Clod read §13A (B-01 to B-37), AD-035 §1b (the partition janitor), the AD-025 self-healing
rules, §14.1 items 29 to 31, AC-134 to AC-143, D-46 to D-48 and R-19. The following were
checked against real code:

- **Ingest handlers.** `router.go` (the agent ingest routes carry only `deviceAuth`, with no
  limiter) and `handlers.go` (`Telemetry`, `Heartbeat`, `ReceiveDockerEvent` and
  `ReceiveAgentEvent` decode bodies with no `MaxBytesReader`; heartbeats are inserted on
  every call).
- **Storage repositories.** `db/device_log_repo.go` and `db/container_metric_repo.go` (a
  per-push cap at most, with no per-device rate).
- **Dashboard hub.** `websocket/hub.go` (a 256-slot broadcast channel and 256 slots per
  client).
- **Shipped compose files.** Neither compose file sets a `logging:` option for the server or
  database container.

**Updated verdict: APPROVED WITH CONDITIONS (phase-gated).**

- §13A is a strong normative design: concrete constants, real choke points and named tests
  for most rows.
- But the dominant ingest paths (snapshots, heartbeats, metrics, device logs, events) have no
  per-device rate or body cap, so the table's worst-case and R-19 figures are not real bounds
  (**SEC-035, MEDIUM**).
- Several self-healing claims fail under a full disk and under lock contention
  (**SEC-037, MEDIUM**).

Both need ADD amendments before P0 starts. Rev 3 had no ADD amendment outstanding, so the
verdict line changes.

---

### R31.1 B-row assessment

Each row is rated for a concrete cap, a choke point that cannot be bypassed, and an adequate
named test.

| Row | Rating | Note and fix |
|---|---|---|
| B-01 stored logs | Testable | Per-container and per-device ceilings in `applyCeiling`; the agent cannot bypass them. AC-111, AC-134, `TestDUX_SEC023_NameRotationHeldByDeviceCeiling` |
| B-02 gap rows | Testable | 200 per 60 s per device; validated entries only |
| B-03 re-stamped rows | Testable | Counted inside B-01 (AC-122, AC-123) |
| B-04 DEFAULT partition | Testable | AC-135 asserts DEFAULT stays at 0 |
| B-05 partition count | Testable | At most 96; AC-134 |
| B-06 container limiter | Testable | LRU of 512 per device; AC-143 |
| B-07 device state | Testable | Keyed by the authenticated device; idle prune |
| B-08 live streams | Testable | 8 per host |
| B-09 viewers and queues | Testable | 64 viewers, 1 MiB per queue; AC-143 |
| B-10 backfill queue | Testable | 2 running and 16 queued per host |
| B-11 agent RPC | Testable | 4 per device and 16 server-wide; AC-143 |
| B-12 preview store | **Weak** | The named tests (AC-092, SEC-007) do not prove the 50-preview cap, the eviction or the 60 s sweep. Fix: `TestDUX_BND_PreviewStoreCapAndSweep` (the 51st preview evicts the oldest; expired previews are swept with an injected clock) |
| B-13 typed confirmations | Testable (n/a) | Stateless |
| B-14 rate limiters | **Weak** | The only named test belongs to another story (`TestCL_SEC001_…`), and the new limiters' idle eviction is untested. The command and terminal-open limiters are keyed by the device ID in the URL path. If the bucket is created before the device is checked, random IDs create unbounded buckets. Fix: create buckets only after the device is validated as existing (and, for terminals, connected). Add `TestDUX_BND_LimitersKeyOnlyKnownDevices` and `TestDUX_BND_LimiterIdleEviction` |
| B-15 agent registry | Testable | One entry per device |
| B-16 audit rows | **Weak** | No named constant bounds the rate of `logs` audit rows. Coalescing is per (actor, device, container, remote) over 2 min, so an attach storm across many containers writes up to C × 720 rows per day. The figure of 46,080 is an assumption, and no test asserts a write rate. Fix: add `AuditLogAttachRowsPerDevicePerMinute` (for example 32) with an overflow summary row, and `TestDUX_BND_AuditRowsUnderAttachStorm` |
| B-17 commands | **Weak** | Commands created by the system are "assumed ≤ 100 per day". One per policy per cooldown is not a cap: the policy count is unbounded, and the cooldown is an operator setting that can be very short. A failing update completes quickly and re-fires on the next telemetry push. Fix: add a hard `SystemCommandsPerDevicePerHour` (for example 30) in `Dispatch` for origins other than operator, plus a minimum cooldown enforced by the server, and `TestDUX_BND_SystemCommandCap` |
| B-18 command output | Testable | 256 KiB and 64 KiB caps in `sanitizeReport`; AC-068 |
| B-19 events | **Weak** | (a) The 200 B row size is assumed: agent-controlled text that reaches `events` (container and image names, messages, and the agent `/events` message) has no length cap, and the request bodies have no size limit. (b) The per-container throttle counters are keyed by agent-supplied container names, so that map is unbounded. Fix: cap every stored event text field (for example 1 KiB, truncated at a rune boundary); bound the per-container throttle map (an LRU of 512 per device, like B-06, with overflow counted against the device bucket); extend AC-139 with a 10,000-random-names case and an oversized-message case |
| B-20 inventory | Testable | 4 MiB cap and cadence; named tests |
| B-21 stacks | Testable | 500 rows and a write budget; named tests |
| B-22 prune writes | Testable | `TestDUX_BND_PruneResultsTruncated` |
| B-23 agent buffer | Testable | AC-142 |
| B-24 agent outbox | Testable | CMD-LIFECYCLE tests |
| B-25 agent stack registry | **Weak** | The test cell names a file, not a cap assertion. Fix: `TestDUX_BND_StackRegistryCapAndPrune` (the 501st project evicts the oldest unseen; entries unseen for 90 days are pruned; the file stays at or below its size bound) |
| B-26 agent follows | Testable | Named memory tests |
| B-27 agent backfill | **Weak** | The named test checks the byte budget, not the memory claim ("streamed … the skip scan only counts"). Fix: `TestDUX_BND_BackfillStreamsWithoutBuffering`: 5,000 lines of 16 KiB plus a 20,000-line skip scan must keep peak heap growth under a stated bound |
| B-28 collector state | Testable | `TestDUX_BND_CollectorMapsPruned` |
| B-29 event bus | **Weak** | 64 per subscriber, but the number of subscribers is not bounded or tested. Every stream watcher subscribes, and a missed unsubscribe leaks a channel and a goroutine. Fix: cap subscribers (for example `LiveStreamsPerHost + 2`), unsubscribe in the cancel path, and add `TestDUX_BND_EventBusUnsubscribeOnCancel` |
| B-30 server logs | Testable | AC-141. It bounds only this story's Warn and above; see SEC-038 for Info and stdout |
| B-31 agent logs | Testable | AC-141. It covers only the new agent paths |
| B-32 browser buffer | Testable | `logBuffer.test.ts` |
| B-33 browser storage | Testable | AC-046 |
| B-34 snapshot fields | **Untestable as written** | "existing / existing / existing". The figure of 43 MB per day assumes one push per minute, but nothing enforces any push rate, and snapshot bodies are only checked against 20 MB *after* the full decode. No test can prove the figure. Fix: SEC-035 (a per-device ingest rate and a body cap), then a row B-38 with `TestDUX_BND_TelemetryIngestRate` |
| B-35 I/O columns | Testable | WP-2D PG test (its volume inherits SEC-035) |
| B-36 projections | Testable | Nothing cached |
| B-37 janitor and purge state | Testable | AC-134, AC-136, AC-138 |

**Counts: 28 testable, 8 weak (B-12, B-14, B-16, B-17, B-19, B-25, B-27, B-29),
1 untestable (B-34).**

---

### R31.2 Growth paths missing from §13A

1. **Agent ingest rate and body size.** This is the largest gap: see SEC-035. It covers:
   - `POST /telemetry`, which stores a snapshot of up to 20 MB on every call;
   - `POST /heartbeat`, one `heartbeats` row per call;
   - container metrics, one row per running container per push;
   - `device_logs`, with a per-push cap only;
   - `POST /docker-events` and `/events`;
   - `/probe-results`.

   None of them has a per-device rate limit, and none caps the request body before decoding.
   A compromised agent can grow `telemetry_snapshots`, `heartbeats`, `container_metrics` and
   `device_logs` by gigabytes per minute. PART-001's retention bounds only *how long* rows
   are kept, not how much is written per day. The paths are pre-existing, but the standing
   requirement ("no matter what exception happens") makes them in scope, and P0 changes the
   `Telemetry` handler.
2. **In-memory state keyed by agent-chosen identifiers** (SEC-036):
   - AD-016's set of retired stream IDs, and its "at most once per second per ID" unfollow
     throttle. A hostile agent sending random `stream_id`s grows both without bound.
   - D-47's per-container event-throttle counters (B-19).
   - The event-bus subscriber list (B-29).
3. **Disk on the server host from process logs** (SEC-038):
   - Neither shipped compose file sets a `logging:` option for the server or database
     containers. With Docker's default `json-file` driver and no daemon-level rotation, these
     files grow without bound.
   - The request logger writes one Info line per agent push (telemetry and heartbeat).
   - `dockerlog.Budget` covers only this story's Warn-and-above records, not Info or stdout.
4. **Dashboard broadcast amplification.** Each Docker event and each `command_update`
   becomes a hub broadcast to every dashboard socket. Memory is bounded by the existing
   256-slot channels (oldest dropped). No storage. Covered by the existing hub; recorded so
   the table is complete. No finding.
5. **Docker images on managed hosts.** rIOt-initiated stack Pull, Update and Up leave the
   superseded images on each host, and only the manual P4 prune reclaims them. That disk is
   the *hosts'*, not the server's, but it is "images" as named in the requirement. Product
   decision **D-52**.
6. **Browser.** The Activity "load more" pages accumulate without limit in a long-lived tab.
   This affects the browser only and has no server impact. Recommendation: cap retained pages
   (for example 40 pages, about 1,000 entries) and drop the oldest. LOW, not gated.

---

### R31.3 Self-healing assessment

Several paths converge correctly:

- **In-memory state** rebuilds from empty after a crash, and fails safe because the device
  ceilings still apply.
- **The janitor's catch-up is bounded.** The backlog can never exceed the partitions that
  exist (at most 96), because the server creates none while it is down. So 24 drops per run
  clears any outage, or a retention cut from 90 days to 1 day, in at most 4 hourly runs. The
  "at most 24 drops per run" rule does not leave an unbounded tail.
- **Purges are idempotent** and resume where they stopped.
- **Stale events** surface stuck work.

These claims fail as written (**SEC-037**):

1. **Disk full.** §13A says "Janitor drops and purges keep running, so space comes back".
   - Row-DELETE purges free no disk space. They *add* WAL and dead tuples, and VACUUM needs
     working space too.
   - Only partition drops return space. They also need some WAL, so on a completely full
     volume PostgreSQL may refuse them or stop.
   - Nothing acts *before* the disk is full: there is no storage budget or headroom guard.
     With N hostile or runaway devices, the R-19 footprint scales as N × 72 GiB, with no
     fleet-wide ceiling.
2. **Purge livelock.**
   - `CommandRepo.PurgeTerminal` deletes `commands` in batches of 5,000 with
     `statement_timeout = 10 s`, and `command_output` follows by FK cascade. One batch can
     cascade into 5,000 × (256 + 64) KiB of TOAST, about 1.5 GiB.
   - If that batch times out on every run, it is retried unchanged forever. The purge never
     progresses, and `retention_stale` fires but nothing converges.
   - Fix: adaptive batch size (halve on timeout down to 1, grow back on success), and purge
     `command_output` for the batch first in its own bounded statements.
3. **Lock behaviour of partition DDL.**
   - `DROP TABLE <partition>` takes `ACCESS EXCLUSIVE` on the partitioned parent as well.
     While it waits (up to `lock_timeout` 5 s), every new reader and every `COPY` on
     `container_logs` queues behind it.
   - 24 drops per run is up to about 2 minutes of repeated fleet-wide ingest stalls under
     contention.
   - Fix: drop with `ALTER TABLE … DETACH PARTITION … CONCURRENTLY` (PostgreSQL 14 and later;
     the stack runs 16), then `DROP TABLE` of the detached table. Use a short lock timeout
     (≤ 1 s) and a per-run time budget, and stop the run early on the first lock timeout.
4. **Hot-path DDL after an outage.**
   - After a multi-day server outage, agents flush buffered snapshots whose lines fall inside
     the window but on days for which no partition exists. The janitor creates only
     today − 1 to today + 3.
   - Each such day then triggers an on-demand `CREATE TABLE … PARTITION OF` from the ingest
     path. That is a strong lock on the parent, and must scan the DEFAULT partition, on the
     hot path, for every backlog day, right when every agent reconnects.
   - The "one attempt per day per 60 s" rule must be **server-wide** (singleflight across
     ingest goroutines). As written, it can be read as per caller.
   - Fix: at startup, before serving, the janitor also creates every missing day in
     `[today − effective retention, today + 3]`. That is at most 94 cheap creates on empty
     tables, while nothing else holds locks. On-demand DDL then becomes a rare fallback,
     limited by a server-wide singleflight.
5. **Re-stamp fallback lands in an older day.** When the on-demand create fails, the rows go
   into the "newest existing in-window partition", which can be yesterday's. They then
   rotate one day early, and display out of order. This is acceptable as a degraded mode,
   because the rows carry the skew marker, but AC-135 should assert it.

---

### R31.4 R-19 recommendation

**The 72 GiB per hostile device is not acceptable as the only bound under the standing
requirement, for three reasons:**

- it is per device, so the fleet-wide worst case grows with the number of compromised or
  runaway hosts;
- it ignores the snapshot, heartbeat, metric and device-log paths (SEC-035);
- its only remedy is an operator action (revoking the key), and the requirement asks for
  self-healing without one.

**Recommendation:**

1. **D-49 (required to meet the requirement): a fleet-wide storage budget with self-healing
   eviction.** Add `RIOT_CONTAINER_LOG_MAX_GB`, and a matching budget for `command_output`.
   The default should be a fixed size (for example 20 GiB), optionally lowered automatically
   to a percentage of free database disk.
   - The janitor measures the `container_logs` partition sizes on each run. When the total
     exceeds the budget, it drops the oldest day partitions early, which shortens the
     effective retention.
   - It reports the shortened value through `retention_days`, and raises one
     `log_budget_eviction` event per transition.
   - This is the only mechanism here that frees space *before* the disk is full, and it uses
     the cheap partition drop.
2. **D-50 (recommended): a per-device daily stored-bytes quota** for container logs, for
   example 512 MiB per day, and for command output, for example 64 MiB per day. Over the
   quota, lines become `gap_server` counts and output is truncated. One device can then
   never consume the fleet budget by itself, and its footprint at the default retentions
   falls to about 6 GiB.
3. **D-51 (required, SEC-035): per-device ingest rate and body caps** on every agent endpoint,
   so that B-34 and the PART-001 tables have real per-day bounds.
4. **A disk-headroom warning.** When database free space drops below a threshold, raise one
   operator-visible event, and let the D-49 eviction take over.

---

### R31.5 New findings

#### SEC-035 (MEDIUM): Agent ingest endpoints have no per-device rate or body cap, so the largest growth paths are unbounded per day

**Domain:** DoS / DB growth
**Affected:** §13A (missing row; B-34 untestable), R-19; `router.go` agent routes;
`handlers.go` `Telemetry`, `Heartbeat`, `ReceiveDockerEvent`, `ReceiveAgentEvent`,
`ReceiveDeviceLogs`, and probe results

**Attack Scenario:** a compromised agent posts a 19 MB telemetry body and a heartbeat in a
tight loop. Each telemetry call stores a snapshot, along with its container metrics and its
device logs. Within an hour the database volume fills. The partitioned retention would drop
those rows only days later.

**Required properties:**

- **Per-device rate.** A token bucket per device, keyed by the authenticated device and
  shared across HTTP and WS where both exist:
  - telemetry: at most 1 stored snapshot per 15 s, with a burst of 100, so that the agent's
    buffer flush after an outage (B-23) is accepted;
  - heartbeats: 1 per 10 s;
  - Docker and agent events: 120 per minute.

  Excess gets `429` (the agent buffers and retries), or is accepted for live broadcast but
  not stored. The ADD chooses which, and records it.
- **Body caps before decoding** (`http.MaxBytesReader`): telemetry 25 MiB, other agent
  endpoints 1 MiB.
- **Row B-38** in §13A, with constants and the tests `TestDUX_BND_TelemetryIngestRate`,
  `TestDUX_BND_HeartbeatIngestRate` and `TestDUX_BND_AgentBodyCaps`.

**Blocks:** **an ADD amendment is required before P0 starts.** P0 changes the `Telemetry`
handler, and the standing requirement makes the path in scope. Values: **D-51**.

#### SEC-036 (LOW, mandatory): In-memory state keyed by agent-chosen identifiers needs a bound

**Affected:** AD-016 (retired stream IDs; the per-ID unfollow throttle), D-47 (per-container
event-throttle counters), AD-017 event bus (subscribers)

**Attack Scenario:** a compromised agent sends 10,000 `logs_data` frames per second, each with
a fresh random `stream_id`, and Docker events naming random containers. The server's
retired-ID set, its per-ID throttle map and its per-container throttle map grow until the
process runs out of memory.

**Required properties:**

- Retired IDs are kept for at most 10 minutes, with an LRU of at most 256 per device.
- The "unknown stream" unfollow is throttled by a **per-device** token bucket, not by per-ID
  state.
- The per-container throttle map has an LRU of 512 per device (as in B-06).
- Event-bus subscribers are capped and removed on cancel.

**Tests:**
- `TestDUX_BND_RetiredStreamIDsBounded`, with 100,000 random IDs;
- `TestDUX_BND_EventThrottleMapBounded`;
- `TestDUX_BND_EventBusUnsubscribeOnCancel`.

**Blocks:** no. Condition C-36 (P2).

#### SEC-037 (MEDIUM): Self-healing gaps under a full disk, purge timeouts and partition DDL locking

**Affected:** §13A recovery summary ("Server disk full"), AD-025 self-healing, AD-035 §1b
rules 1, 2 and 4, R-19

**Attack Scenario:** after a 5-day server outage, 25 agents reconnect at once.
- Their buffered lines hit five missing day partitions. On-demand creates take strong locks on
  `container_logs` while every ingest goroutine queues behind them.
- Meanwhile the first purge run's 5,000-row `commands` batch cascades into gigabytes of
  `command_output` TOAST, times out, and repeats every hour.
- The disk keeps filling, because DELETE purges free nothing. Nothing evicts proactively until
  inserts start failing.

**Required properties:**

1. **Proactive budget eviction** (D-49) and a disk-headroom event. Correct the §13A statement:
   only partition drops free space, and DELETE purges do not.
2. **Adaptive purge batches.** A batch halves on `statement_timeout` and grows back on
   success. Cascaded `command_output` rows are deleted first, in their own bounded batches.
   Test: `TestDUX_BND_PurgeBatchAdaptsOnTimeout`.
3. **Partition drops via `DETACH PARTITION … CONCURRENTLY` then `DROP`,** with
   `lock_timeout` ≤ 1 s, a per-run time budget, and the run ending at the first lock timeout.
   Test: a PG test where a long reader holds the parent, and the drop neither blocks a
   concurrent `COPY` for more than 1 s nor retries in a tight loop.
4. **At startup, before serving, ensure every missing day in
   `[today − effective retention, today + 3]`.** On-demand creation from ingest becomes a rare
   fallback under a **server-wide** singleflight: at most one attempt per day per 60 s across
   all goroutines.
5. **AC-135 also asserts the yesterday-fallback case**, including its early rotation.

**Blocks:** **an ADD amendment is required before P0 starts**, for items 1 to 4.

#### SEC-038 (LOW, mandatory): Server-host process logs are unbounded

**Affected:** `docker-compose.yml`, `docker-compose.prod.yml` (no `logging:` for the server or
the database), the request logging middleware (Info for every agent push), §14.1 item 30

**Attack Scenario:** a fleet of 25 agents, each pushing telemetry and heartbeats every minute,
writes about 72,000 request log lines a day into the server container's `json-file` log.
Nothing rotates it, so over months it fills the server host's disk, outside every database
bound.

**Required properties:**

- The shipped compose files set log rotation for the server and database containers (for
  example the `local` driver, or `json-file` with `max-size` and `max-file`).
- The README states the same for custom deployments.
- Per-request Info logging for agent ingest routes is sampled or budgeted, for example one
  summary line per device per 10 minutes.

**Test:** a static check that both compose files declare `logging` options for every service.

**Blocks:** no. Condition C-38. It must ship with P0's release.

---

### R31.6 Updated mandatory conditions (additions)

| ID | Finding | Tests |
|---|---|---|
| C-35 | SEC-035 | `TestDUX_BND_TelemetryIngestRate`, `TestDUX_BND_HeartbeatIngestRate`, `TestDUX_BND_AgentBodyCaps` |
| C-36 | SEC-036 | `TestDUX_BND_RetiredStreamIDsBounded`, `TestDUX_BND_EventThrottleMapBounded`, `TestDUX_BND_EventBusUnsubscribeOnCancel` |
| C-37 | SEC-037 | `TestDUX_BND_PurgeBatchAdaptsOnTimeout`; the detach-concurrently PG test; a startup-ensure test over the backlog range; the extended AC-135; a budget-eviction test if D-49 is adopted |
| C-38 | SEC-038 | static check of the compose `logging` options |
| C-B | Weak rows | the fixes in R31.1 for B-12, B-14, B-16, B-17, B-19, B-25, B-27 and B-29 |

---

### R31.7 Updated per-phase gate (revision 3.1)

"ADD amendment required" means that the architect must amend the ADD before that phase's work
packages start. "Implementation conditions" are verified by QA before the phase is signed off.

| Phase | ADD amendment required before start | Implementation conditions (QA) |
|---|---|---|
| **P0** | **SEC-035** (row B-38: ingest rate and body caps; D-51). **SEC-037** items 1-4 (budget eviction or headroom, D-49; adaptive purges; `DETACH … CONCURRENTLY`; startup backlog ensure plus a server-wide singleflight) | All rev 3 P0 conditions, plus C-35, C-37, C-38; the B-34 fix |
| **P1** | none | All rev 3 P1 conditions, plus the B-14 and B-17 fixes (the D-48 limiters keyed on known devices; the system-command cap) |
| **P2** | none | All rev 3 P2 conditions, plus C-36 and the B-16, B-19, B-27 and B-29 fixes |
| **WP-2D** | none | inherits C-35 |
| **P3** | none | All rev 3 P3 conditions, plus the B-25 fix |
| **P4** | none | All rev 3 P4 conditions, plus the B-12 fix |

The rev 3 P0 operational gate (the §2.4.1 runbook record and a verified backup per database)
still applies.

---

### R31.8 Public-repo hygiene

ADD rev 3.1, FRD rev 3 and this report were scanned for personal names, the operator's machine
names, IPv4 literals outside RFC 5737, emails, home and user paths, and private DNS suffixes.
**Clean.** The only hits are rule literals (`127.0.0.1`, `0.0.0.0` in the port-link rules)
and `example.internal` placeholders.

---

### R31.9 Decisions for the product owner (additions)

| ID | Decision | Security recommendation |
|---|---|---|
| D-46 | Per-device stored-log ceiling 5,000 lines and 2 MiB per minute | **Endorsed** |
| D-47 | Observed-event throttle 60 per container and 600 per device per hour | **Endorsed**, with the B-19 fixes |
| D-48 | Operator command and terminal-open limits | **Endorsed**, with the B-14 keying fix. Also add the B-17 cap on system-origin commands |
| **D-49 (new)** | Fleet-wide storage budget for `container_logs` and `command_output`, with self-healing eviction of the oldest days | **Adopt**: for example a 20 GiB default, configurable, optionally capped to a share of free database disk. This is the only mechanism that keeps the server off a full disk whatever the number of misbehaving devices (R-19, SEC-037) |
| **D-50 (new)** | Per-device daily stored-bytes quota (container logs about 512 MiB per day; command output about 64 MiB per day) | **Adopt.** One device can never exhaust the fleet budget, and the per-device worst case falls from about 72 GiB to about 6 GiB |
| **D-51 (new)** | Per-device agent ingest rate and body caps (SEC-035): telemetry 1 per 15 s with a burst of 100; heartbeat 1 per 10 s; events 120 per minute; bodies 25 MiB for telemetry and 1 MiB otherwise. Over the limit: `429` with agent retry, or accept for live broadcast but do not store | **Adopt.** Prefer `429` for telemetry, since the agent already buffers with a bounded size (B-23), and accept-but-don't-store for heartbeats |
| **D-52 (new)** | Images left on hosts by rIOt-initiated pulls and updates | **Optional.** After a successful update, remove only the superseded image ID, if it is unused, through the per-item path (never a `*Prune` API), behind an opt-in setting. Otherwise show the reclaimable size and leave it to P4 prune |

---

## Revision 3.2 addendum

**Date:** 2026-10-19
**Scope:** final delta check of ADD rev 3.2 (`fd7bb1d`, compared with `ca8a559`). Clod read
AD-040, AD-041, the rewritten AD-035 §1b, AD-025 (adaptive batches), §13A (B-12, B-14,
B-16, B-17, B-19, B-25, B-27, B-29, B-34, and B-38 to B-48 with the worst-case tables), §17
D-49 to D-53, and the §19 rows for SEC-030 to SEC-038.

The following were checked against the repository:

- `Dockerfile`: the final `alpine` stage sets no `USER`, so the server runs as root.
- `docker-compose.prod.yml`: the database uses the named volume
  `riot-data:/var/lib/postgresql/data`.
- §2.4.1 step 1 (the runbook body is unchanged from rev 3).

**Updated verdict: APPROVED WITH CONDITIONS (phase-gated).**

- SEC-035 to SEC-038, the eight weak rows and B-34 are closed. That includes AD-040's
  correction that the merged agent socket sets no WS read limit.
- Two items still need small ADD amendments before P0:
  - **SEC-039 (MEDIUM, new):** the D-50 quotas and the headroom thresholds degrade by
    *stopping*, which harms honest hosts. That is the data-loss concern the product owner
    raised.
  - **SEC-030:** its required properties appear only in the §19 mapping row. They are not in
    the runbook text that operators will follow.
- No other phase is gated on the ADD.

---

### R32.1 Status

| Item | Status | Note |
|---|---|---|
| SEC-035 | **Closed** | AD-040: `MaxBytesReader` before decoding on every agent route; per-device buckets created only after `DeviceAuth`; telemetry `429` with `Retry-After`, and the agent flush deletes each item after its own success; heartbeat excess gets `200 throttled` with no write; events, device logs and probe results limited; agent WS read limit of 4 MiB plus a frame bucket. Tests AC-144 to AC-146 |
| SEC-036 | **Closed** | B-43: retired-ID LRU of 256 with a 10 min TTL; a per-device unfollow bucket with no per-ID state; event-throttle LRU of 512; at most 10 event-bus subscribers, unsubscribed on cancel (AC-153) |
| SEC-037 | **Closed, with residuals** | Budget eviction and headroom (AD-041); adaptive purges with children deleted first (AC-150); startup ensure over the whole window plus a server-wide singleflight (AC-152); drops with a 1 s lock and stop at the first timeout (AC-151); the yesterday fallback asserted. Residuals: **SEC-041** (a long-lived lock holder stalls every eviction path) and **SEC-039** item 3 (the headroom thresholds) |
| SEC-038 | **Closed** | `json-file` 10m × 5 for the server and the database in both compose files, with a static test (AC-154); agent-ingest request lines summarized per device every 10 min |
| B-12 | **Closed** | AC-155 |
| B-14 | **Closed** | Buckets only for validated devices (and connected ones for terminals); AC-156 |
| B-16 | **Closed** | `AuditLogAttachRowsPerDevicePerMinute` = 32 plus an overflow summary row; AC-157 |
| B-17 | **Closed** | `SystemCommandsPerDevicePerHour` = 30, and `SystemPolicyMinCooldown` = 15 min enforced by the server; AC-158 |
| B-19 | **Closed** | Text caps of 1 KiB per field and 3 KiB per row in `EventRepo.Create`; one device bucket for every agent-originated kind; a 512-entry LRU; AC-139 extended |
| B-25 | **Closed** | AC-159 |
| B-27 | **Closed** | AC-160: peak heap growth under 4 MiB per backfill |
| B-29 | **Closed** | Subscriber cap and unsubscribe on cancel; AC-153 |
| B-34 | **Closed** | It now sits inside B-38: rate, body cap and quota; AC-144 and AC-147 |
| SEC-030 (§19 mapping, "to be confirmed") | **Confirmed in substance, but only partially closed** | The §19 row restates SEC-030 correctly. The normative text is still missing where operators and engineers read it. §2.4.1 step 1 still shows `pg_dump … "<connection>"`: the password is on the command line, and there is no `0600` rule, no "outside any repository" rule, no credential-handling note, and no "stop the server from backup to step 5" step. The step 4 table lacks the F-LC + F-PT + F-AV and F-LC + F-AV rows. §6.2 and §10 do not list `TestDUX_SEC030_FingerprintCreatesNothing`, `TestDUX_SEC030_FingerprintReadOnlySession` or `TestDUX_SEC030_FingerprintOutputHasNoDSN`. **Correction:** move these properties into the §2.4.1 body, the §6.2 file rows and §10; add the recommendation to use a read-only database role for steps 2 and 3. This is an ADD amendment required before WP-P0C, or before any use of the runbook |
| SEC-031..SEC-034 (§19 rows) | **Confirmed** | The §19 rows match the rev 3 addendum |

---

### R32.2 Are AD-040 and AD-041 sound?

**AD-040: sound.**

- Keying after authentication means unauthenticated callers create no state.
- Telemetry 429 plus the agent's per-item flush loses nothing that fits in the bounded buffer.
- Heartbeat throttling still delivers pending commands on the accepted heartbeats.
- The WS frame bucket of 200 per second comfortably exceeds legitimate traffic: 8 streams
  × 4 batches per second, plus reports.
- No finding.

**AD-041: sound as a bound, with four concerns.**

1. **Per-device fairness under the fleet budget.** Eviction drops whole *day* partitions, so
   it hits every device equally. One device alone cannot force eviction at the defaults: at
   its daily quota, 8 days is about 4.4 GiB, against a 16 GiB budget. But when the operator
   raises retention, or the fleet is large, the budget already binds, and then one heavy
   host, honest or hostile, at 0.55 GiB per day can roughly halve every other host's history
   depth. `retention_limited_by` makes this visible, but there is no fairness: the cost of one
   device's volume falls on everyone's oldest data. That is **SEC-042 (LOW, recommended)**,
   decision **D-56**.
2. **Honest-host harm from the D-50 quota cliff.** The quotas reset at UTC midnight and, once
   exhausted, *stop* storing:
   - A busy honest host reaches 512 MiB of stored logs after about 7 h, and then stores
     **no new lines** for up to 17 h. During an incident the newest lines matter most, and
     those are exactly the ones lost (only gap counts remain).
   - The telemetry quota has a worse version of the same effect. Once 1 GiB of snapshots is
     stored, no snapshot is stored until midnight. Live broadcast continues, but every REST
     view that reads the latest *stored* snapshot shows data that keeps getting older. That
     covers the fleet container table after a reload, `GET /devices/{id}`, container detail,
     self-target computation and the stack state.

   The product owner explicitly worried about losing important log data. The quota should
   degrade instead of stopping. That is **SEC-039**.
3. **The headroom thresholds on large shared disks.** "Low" is below 15 % **or** 4 GiB free,
   and "emergency" is below 5 % **or** 1 GiB. On the very common homelab layout, Docker's
   volumes sit on a large, mostly full media disk (for example 8 TB at 96 % used, 320 GB
   free). Such a host is permanently in emergency: rIOt drops every container-log day down to
   2, drops other tables to their 2-day floors, deletes events older than 7 days, and stops
   storing snapshots and logs, with hundreds of GB still free. The percentage should never
   fire when the absolute free space is ample. That is **SEC-039** item 3.
4. **The read-only database volume mounted into the server container.**
   - AD-041 §3 says the server "cannot read" PostgreSQL's files because they are mode 0700.
     That is false for this image: the server runs as **root** (no `USER` in the
     `Dockerfile`), and root's `CAP_DAC_OVERRIDE` and `CAP_DAC_READ_SEARCH` bypass 0700. The
     mount gives the server process read access to every database file, including the stored
     session-signing secret, password hashes and all logs.
   - The incremental risk is limited, because a compromised server already holds
     `RIOT_DB_URL`. But it turns any future arbitrary-file-read bug in the server into a
     full database read, and the ADD statement is wrong.
   - `statfs` reports *filesystem* free space, so the mount is unnecessary. Any path on the
     same filesystem gives the same numbers: for example a dedicated, empty named volume,
     which by default sits under the same Docker root as `riot-data`.

   That is **SEC-040 (LOW, mandatory)**.

**The D-53 trade-off.** This review **endorses the working default:** keep the DEFAULT
partition, and drop with `DROP TABLE` under a 1 s lock timeout, stopping at the first timeout.

- The stall is bounded at about 1 s per hourly run, plus milliseconds per successful drop.
- Rev 3.2's ingest guard keeps DEFAULT empty, so the scan that DEFAULT forces on each create
  is trivial.
- Removing DEFAULT would buy `DETACH … CONCURRENTLY`, but it adds a failure mode: one
  unroutable row fails the whole `COPY` batch. It would also diverge from PART-001's uniform
  design.
- Revisit only if lock-timeout stale events are observed in practice.

The default has one dependency, **SEC-041**: a session that holds a lock on `container_logs`
indefinitely (idle in transaction, or a stuck query) makes *every* drop time out. That blocks
retention drops, budget eviction and emergency eviction alike, because they all share the
method.

---

### R32.3 Remaining unbounded paths and self-healing gaps

- **No unbounded growth path remains in §13A.** B-01 to B-48, together with the D-49 fleet
  budget, the D-50 quotas and the headroom guard, bound every path this review has
  identified. When no free-space source is configured, the guard is off and the bound is the
  sum of the per-device bounds plus the budgets. That is documented and acceptable.
- **Audit rows are deliberately never evicted,** even in an emergency. They are admin-driven
  and rate-limited (B-16, D-48). Acceptable, and recorded.
- **Self-healing gaps:**
  - **SEC-041:** a long-lived lock holder blocks every drop path. There is no
    `idle_in_transaction_session_timeout` and no statement timeout for the server's own
    sessions, and nothing identifies the blocker.
  - **SEC-039:** quota cliffs and false emergencies are oscillation and data-loss modes that
    an honest fleet would hit. They are not convergence failures, but they work against the
    product owner's intent.

---

### R32.4 New findings

#### SEC-039 (MEDIUM): Quota and headroom limits degrade by stopping, which loses the newest data on honest hosts

**Domain:** Availability and integrity of monitoring data
**Affected:** AD-041 §1 (D-50 classes `logs` and `telemetry`) and §3 (thresholds), D-50, D-49

**Attack Scenario (honest host, no attacker):**
- A build server with 50 chatty containers reaches the 512 MiB log quota at 07:00 UTC. When
  its deploy fails at 15:00, none of the failure's log lines are stored, only gap counts.
- Its large snapshots reach the telemetry quota at 17:00. After a page reload, the fleet table
  shows its containers as they were at 17:00.
- Separately, a NAS-hosted deployment with 300 GB free on a 96 %-full disk is in permanent
  emergency, and has stopped storing logs and snapshots altogether.

**Required properties:**

1. **Logs quota:**
   - **Continuous refill.** The daily total stays the same, but it refills continuously
     instead of resetting at midnight: a token bucket with capacity, for example, 25 % of the
     daily quota, refilling at quota ÷ 24 h. A heavy host then stores a steady share all day,
     instead of 7 hours on and 17 hours off.
   - **Priority reserve.** Once a device has used more than 75 % of its allowance, stdout
     lines are sampled, for example the first K per container per minute. stderr lines, and
     lines matching the error-severity patterns, are still stored from a reserved slice (for
     example 25 %).
   - **Per-container share.** One container cannot use more than its share of the device's
     allowance while others are starved.
   - **Visibility.** Gap rows record the sampling, and the Logs tab says "Stored history is
     sampled (storage quota); Live shows every line".
2. **Telemetry quota:**
   - Over 75 %, the server downsamples (stores at most one snapshot per
     `TelemetryQuotaDownsampleInterval`, for example 15 min) instead of stopping.
   - The *latest* state that the REST views read never goes stale beyond that interval.
3. **Headroom thresholds:**
   - The thresholds are absolute byte values, configurable.
   - A percentage applies only as a cap on a small volume. For example: low when free
     < min(15 % of total, 50 GiB) **and** free < 4 GiB-or-more as configured; emergency when
     free < max(1 GiB, min(5 % of total, 10 GiB)).
   - A volume with tens of GiB free never enters emergency.
4. **Tests:**
   - `TestDUX_BND_QuotaContinuousRefill`;
   - `TestDUX_BND_QuotaStderrReserve`;
   - `TestDUX_BND_TelemetryQuotaDownsamples`;
   - `TestDUX_BND_HeadroomLargeDiskNoFalseEmergency` (8 TB at 96 % used: no emergency).

**Blocks:** **an ADD amendment is required before P0 starts** (AD-041 §1 and §3). Values:
**D-54** and **D-55**.

#### SEC-040 (LOW, mandatory): The database-volume mount exposes database files to a root server process

**Affected:** AD-041 §3 (`RIOT_DB_VOLUME_PATH`, the read-only mount at `/riot-db-volume`),
§6 (compose files), `Dockerfile` (no `USER`)

**Attack Scenario:** a future path-traversal or file-read bug in the server, which runs as
root with the database volume mounted read-only, lets an attacker read the PostgreSQL data
files directly. That includes the session-signing secret, with no SQL credentials needed.

**Required properties:**

- The compose files do **not** mount the database volume into the server. For `statfs`, they
  mount a dedicated empty volume or directory on the same filesystem. The README explains
  that, for custom layouts, the probe path must sit on the database volume's filesystem; the
  alternative is `RIOT_DB_MAX_GB`.
- The incorrect "cannot read them" statement is removed.
- Recommended: run the server image as a non-root user.

**Test:** a static check that no compose service other than the database mounts `riot-data`.

**Blocks:** no. Condition C-40. It must ship with WP-P0C.

#### SEC-041 (LOW, mandatory): A long-lived lock holder stalls every eviction path

**Affected:** AD-035 §1b rule 2 (stop at the first lock timeout), AD-041 §2 and §3 (budget
and emergency eviction use the same method), D-53

**Attack Scenario (fault):** a session that is idle in a transaction holds `AccessShare` on
`container_logs`. Every hourly drop times out after 1 s. Retention drops, budget eviction and
emergency eviction all stop, and the disk fills until the guard sheds ingest. Nothing points
the operator at the cause.

**Required properties:**

- The server's database sessions set `idle_in_transaction_session_timeout` (for example 60 s)
  and a `statement_timeout` for read paths (for example 30 s for history and Activity
  queries).
- When a drop times out, the janitor records the blocking backends from `pg_blocking_pids`,
  as PID, application name and state only, in the stale event and the budgeted log.
- In emergency mode only, the guard may `pg_cancel_backend` blocking backends that belong to
  rIOt's own role, and must never touch other roles.

**Test:** `TestDUX_BND_IdleTxBlockerDoesNotStallEviction` (PG).

**Blocks:** no. Condition C-41 (P0).

#### SEC-042 (LOW, recommended): Fleet-budget eviction has no per-device fairness

**Affected:** AD-041 §2, D-49, D-50

When the budget binds, a heavy device shortens every device's retention, because eviction is
by whole day.

**Attack Scenario:** with retention raised to 30 days, one host at its daily quota roughly
halves every other host's stored history.

**Recommended:** when the budget binds, reduce the daily allowance of devices above a fair
share, before shortening the fleet's retention. For example,
`allowance = min(D-50 quota, 0.9 × budget ÷ (effective retention days × active devices))`
for devices above the median. The heavy device then gets sampled or gap rows instead of
everyone losing days. Decision **D-56**.

**Blocks:** no.

---

### R32.5 Updated mandatory conditions (additions)

| ID | Finding | Tests |
|---|---|---|
| C-30 (restated) | SEC-030 | the three `TestDUX_SEC030_*` tests, listed in §6.2 and §10; the runbook body updated |
| C-39 | SEC-039 | `TestDUX_BND_QuotaContinuousRefill`, `TestDUX_BND_QuotaStderrReserve`, `TestDUX_BND_TelemetryQuotaDownsamples`, `TestDUX_BND_HeadroomLargeDiskNoFalseEmergency` |
| C-40 | SEC-040 | a compose static check that only the database mounts `riot-data` |
| C-41 | SEC-041 | `TestDUX_BND_IdleTxBlockerDoesNotStallEviction` |

---

### R32.6 Updated per-phase gate (revision 3.2)

"ADD amendment required" means that the architect must amend the ADD before that phase's work
packages start. "Implementation conditions" are verified by QA before the phase is signed off.

| Phase | ADD amendment required before start | Implementation conditions (QA) |
|---|---|---|
| **P0** | **SEC-039** (degrading quotas, telemetry downsampling, absolute-first headroom thresholds; D-54, D-55). **SEC-030** (move the properties into the §2.4.1 body, and list the tests in §6.2 and §10) | All earlier P0 conditions, plus C-30, C-35, C-37, C-38, C-39, C-40, C-41 and AC-144 to AC-154. Operational gate: the §2.4.1 record and a verified backup per database |
| **P1** | none | Earlier P1 conditions, plus AC-155 to AC-158 |
| **P2** | none | Earlier P2 conditions, plus C-36, AC-153, AC-157 and AC-162; the commands quota stub (AC-147) |
| **WP-2D** | none | inherits C-35 and C-39 |
| **P3** | none | Earlier P3 conditions, plus AC-159 |
| **P4** | none | Earlier P4 conditions, plus AC-155 and AC-161 (D-52) |

---

### R32.7 Public-repo hygiene

The rev 3.2 additions to the ADD, together with the FRD and this report, were scanned for:

- personal names, including the owner's name and account handle;
- the operator's machine names;
- private IPv4 ranges;
- emails;
- home and user paths.

**Clean.** This addendum also cites no registry path that would include an account handle.

---

### R32.8 Decisions for the product owner (additions)

| ID | Decision | Security recommendation |
|---|---|---|
| D-49, D-50, D-51 (adopted by default) | Fleet budget, daily quotas, ingest caps | **Endorsed as bounds.** D-50's *shape* changes per SEC-039 and D-54 |
| D-52 (opt-in, off) | Superseded-image cleanup on hosts | **Endorsed** |
| D-53 | Drop method while the DEFAULT partition exists | **Endorse the working default** (keep DEFAULT, 1 s lock, stop at the first timeout), conditional on SEC-041 |
| **D-54 (new)** | Shape of the daily quotas | **Adopt:** continuous refill instead of a midnight reset; a reserve for stderr and error lines with stdout sampling past 75 %; a per-container share; telemetry downsampled (1 per 15 min) instead of stopped, so the latest state never goes stale. The daily totals stay as in D-50 |
| **D-55 (new)** | Disk-headroom thresholds | **Adopt absolute-first thresholds:** emergency below max(1 GiB, min(5 % of total, 10 GiB)); low below max(4 GiB, min(15 % of total, 50 GiB)); both configurable. Large, mostly full shared disks must not sit in emergency |
| **D-56 (new)** | Per-device fairness when the fleet budget binds | **Recommended:** lower the allowance of devices above the fair share before shortening every device's retention (SEC-042) |

---

## Closing check (rev 3.3)

**Date:** 2026-10-19
**Scope:** closing security check of ADD rev 3.3 (`e86de24`, compared with `5d6a164`), the
last architecture revision for this design phase. Everything still open is expressed below as
an **implementation condition** attached to a phase, a **follow-up**, or a **product
decision**. No further ADD revision is requested.

### Final verdict: APPROVED WITH CONDITIONS

Implementation may proceed phase by phase, as long as these hold:

- Each phase satisfies its conditions in the consolidated table (CC.6).
- **Blocking conditions** (marked **B**) are met before that phase's pull request merges or,
  for operational conditions, before its deploy.
- All other conditions are verified by QA before the phase is signed off.

---

### CC.1 Status of the rev 3.2 findings

| Finding | Status | Where (ADD rev 3.3) |
|---|---|---|
| SEC-030 | **Closed** | §2.4.1 body: server stopped from step 1 to step 5; `riot-server migrate` (step 4b); `PGPASSFILE`; dump written with `umask 077`, handled as a credential and deleted afterwards; read-only role recommended; F-AV rows. §6.2 (`main.go`, `fingerprint.go`); AC-163. Residual: the `migrate` error output (C-43) |
| SEC-039 | **Closed** | AD-041 §1: refilling buckets, degraded modes, priority first, per-container max-min share, `gap_quota`, telemetry downsampling. AD-041 §3: absolute-first thresholds and a minimal mode that never stores nothing. AC-147, AC-149, AC-164 to AC-168. Residuals: the semantics of trimmed snapshots (C-42), and fairness inside the priority lane (C-46) |
| SEC-040 | **Closed** | AD-041 §3 and §6.1: `riot-data` is mounted only in the database; the empty `riot-fsprobe` volume serves `statfs`; the same-filesystem sanity check; the false statement removed; AC-170. Residuals: mount the probe read-only, and the one-directional mismatch check (C-44) |
| SEC-041 | **Closed** | AD-041 §3, `db.go`: `application_name=riot-server`, a 60 s idle-in-transaction timeout, `ReadTx` with a 30 s statement timeout, the blocker report, and emergency cancellation of the server's own active blockers only; AC-171. Residual: maintenance subcommands must be uncancellable (C-45) |
| SEC-042 | **Closed** | AD-041 §2, D-56: fairness before eviction; early drops only above 110 % or under disk pressure; AC-169. Residual: the device-count lever (follow-up F-1 and D-58) |

---

### CC.2 Review of the rev 3.3 changes

**Sound, no finding:**

- **Continuous-refill buckets.** At most 1.25 × Q in any 24 h window, and at most Q per day
  on average. There is no midnight cliff, and the level survives a restart without an early
  refill.
- **`gap_quota` rows,** and the Logs-tab notice when history is sampled.
- **Absolute-first thresholds.** The 8 TB-at-96 % case stays in normal mode; there are
  overrides.
- **The emergency order** evicts oldest first, and minimal mode has bounded inflow.
- **`riot-fsprobe`** keeps database files out of the server container.
- **Pool session limits** and the blocker report (no query text).
- **Cancellation** is limited to the server's own active backends, with a transaction older
  than 30 s. It never terminates a backend.
- **`riot-server migrate`** keeps the server from serving while the database is being
  upgraded.

**New findings.** None is CRITICAL or HIGH. Each becomes a condition (CC.6).

#### SEC-043 (MEDIUM): Trimmed downsampled snapshots must not read as "nothing to report"

**Affected:** AD-041 §1 `telemetry` downsampled mode (`TelemetryDownsampleKeepSections`;
largest sections dropped); every REST consumer of the latest stored snapshot.

**Description.** Consumers include the device page, `security.go` (security overview and
device scores), fleet patch status and update indicators, the fleet tables and self targets.
A host in downsampled mode stores snapshots *without* sections such as `updates`, `security`,
`services` and `processes`. A consumer that treats a missing section as empty would show zero
pending updates, or no security findings. That is a misleading posture view.

The ADD also does not say what happens when the kept sections alone exceed the 2 MiB trim
target (for example, a large `docker` section on a host with many containers).

**Attack Scenario:** a host with pending security updates enters downsampled mode, because of
large snapshots or a compromised agent padding them. The fleet patch view then shows "up to
date" for it.

**Condition C-42.**

#### SEC-044 (LOW): `riot-server migrate` must share `db-fingerprint`'s output hygiene and stop rules

**Affected:** §2.4.1 step 4b. Only `TestDUX_SEC030_MigrateSubcommandDoesNotServe` is
specified.

**Description.** golang-migrate and driver errors can echo connection details. The subcommand
must also refuse a dirty database explicitly, and exit non-zero.

**Attack Scenario:** a failed `migrate` prints the connection details in its error, and the
operator pastes the output into a public implementation report.

**Condition C-43.**

#### SEC-045 (LOW): Probe-path hardening

**Affected:** AD-041 §3, compose rows, R-26.

**Description.**
- The `riot-fsprobe` mount should be read-only. The server never writes to it.
- The same-filesystem check catches only a probe that is *smaller* than the database. A probe
  on a different, larger and fuller filesystem passes, and could drive a false emergency that
  drops rIOt's data because of another filesystem's pressure, or a false "normal".

**Attack Scenario:** in a custom layout, the probe path sits on the host's root filesystem
while the database sits on a separate disk. A large download fills the root filesystem, the
guard enters emergency, and rIOt drops its log partitions to the floor although its own disk
has plenty of space.

**Condition C-44.**

#### SEC-046 (LOW): Maintenance sessions must be uncancellable by the emergency guard

**Affected:** AD-041 §3 cancellation rule (role plus `application_name=riot-server`);
`db-fingerprint`; `migrate`; startup `RunMigrations`.

**Description.** If the maintenance subcommands, or the migration connection, use
`application_name=riot-server`, an emergency guard on another server instance could cancel a
running migration and leave the schema dirty.

**Attack Scenario:** during a disk emergency on one server instance, an operator runs
`riot-server migrate`. The guard sees an active `riot-server` backend with a transaction older
than 30 s and cancels it. The migration aborts and the schema is left dirty.

**Condition C-45.**

#### SEC-047 (LOW): Fairness must hold inside the priority lane

**Affected:** AD-035 §5 rule 6 and AD-041 §1 sampled mode.

**Description.** "Priority lines first" plus "max-min fair share" does not say that the share
also applies to priority lines. A container that writes everything to stderr, or matches
`ErrorLinePatterns`, could take all refill tokens. Other containers' error lines would then be
lost, which is exactly the data the product owner wants kept.

**Attack Scenario:** one crash-looping container floods stderr. Its lines take every refill
token, and another container's single fatal error line is never stored.

**Condition C-46.**

---

### CC.3 Self-healing and bounds at close

- Every growth path in §13A (B-01 to B-51) has a named constant, a choke point and a named
  test.
- Fleet-wide data this story owns is bounded at 21.6 GiB.
- Other agent-driven data is bounded per device and, when a free-space source exists, by the
  headroom guard.
- After a crash, a multi-day outage, a failed janitor or purge run, DB errors or disk
  pressure, every path converges without operator action:
  - bounded catch-up, adaptive purges and the startup partition sweep;
  - a server-wide singleflight, and 1 s drop locks with blocker reporting;
  - state restored from `device_storage_usage`.
- **One residual remains:** the number of registered devices is not capped. Each device brings
  its own quotas, so a leaked registration key multiplies the per-device bounds, and the fleet
  budget and headroom guard become the only fleet bounds. The registration path is
  pre-existing and outside this story: follow-up F-1, decision D-58.

---

### CC.4 Follow-ups (not gating any phase)

| ID | Follow-up | Origin |
|---|---|---|
| F-1 | Cap the number of registered devices, and the registrations per day (configurable) | CC.3; D-58 |
| F-2 | Run the server image as a non-root user | SEC-040, R-27 |
| F-3 | Close `/ws` and `/ws/terminal` at session expiry, as the live-log socket does | SEC-027, R-17 |
| F-4 | Audit changes to auto-update and auto-patch policies (long class) | SEC-034 |
| F-5 | Extend the AD-034 origin control to every pre-existing state-changing route | SEC-001, D-30 (b) |
| F-6 | Tell operators that stored log times are host-reported and not tamper-evident (Logs tab notice or README); consider a per-row server receive time later | SEC-032 |

---

### CC.5 Product decisions for the product owner

1. **D-57 (new): behaviour at critical free space** (below 512 MiB). Today, minimal mode
   continues: priority lines, 1 stdout line per container per minute, and one snapshot per
   15 min. **Recommend:** at critical, store priority lines only, plus one snapshot per device
   per hour, until free space is back above emergency. That slows the approach to a full disk
   without ever reaching zero.
2. **D-58 (new): a cap on registered devices** (F-1), for example a configurable maximum of
   500, plus a daily registration limit. **Recommend:** adopt.
3. **D-30: scope of the origin control.** **Recommend (b):** every state-changing route,
   including the existing reboot, uninstall and settings routes.
4. **D-44: writable-path refusal from P1.** **Recommend:** keep it. In-container stack managers
   must add `docker.compose_trusted_roots`.
5. **D-45: archive terminal audit before the P2 purge.** **Recommend:** yes, for operators who
   want the history. Export to a `0600` file kept offline.
6. **D-53: drop method while the DEFAULT partition exists.** **Recommend:** keep the working
   default (keep DEFAULT; 1 s lock; stop at the first timeout).
7. **D-21 (ii): live-log bounds** (512 KiB/s per stream, 2 MiB/s per agent, 64 viewers, 4 h).
   **Recommend:** accept.
8. **D-46 to D-48 values:**
   - stored-log ceiling 5,000 lines and 2 MiB per minute per host;
   - events 60 per container and 600 per device per hour;
   - commands burst 120 then 60 per hour; terminal opens 30 per minute.

   **Recommend:** accept.
9. **D-54 to D-56 values** (adopted by default): bucket capacity 25 %, sampling at 75 %,
   5 stdout lines per minute, 15-min trimmed snapshots, absolute-first thresholds, fairness
   floor 32 MiB per day. **Recommend:** accept, and tune after observing real fleets.
10. **D-24: `axe-core` as a devDependency.** **Recommend:** approve.

---

### CC.6 Consolidated implementation conditions (C-1 to C-59)

Every condition from the original review and all addenda is listed here, with its phase, a
one-line requirement, and the test that proves it. **B** marks a blocking condition: the
phase's pull request must not merge, or for operational conditions the phase must not deploy,
until it holds. C-17 to C-20, C-32 and C-34 are unused.

| C | Phase | B | Requirement (one line) | Proving test |
|---|---|---|---|---|
| C-1 | P1 (and every later route) | **B** | Same-origin JSON on state changes; header plus origin on side-effecting GETs; framing forbidden (AD-034) | `TestDUX_SEC001_TextPlainPostRejected`, `_SameSiteCrossOriginRejected`, `_SameOriginAccepted`, `_SideEffectGETsRequireHeader`, `_FrameAncestorsNone`; router matrix |
| C-2 | P3 | **B** | Stack path agreement or conflict; acknowledged changes; no compose execution from container-writable paths; one agreed source; paths shown (AD-038) | `TestDUX_SEC002_DisagreeingMembersConflicted`, `_PathChangeRequiresAck`, `_ComposeUnderRWBindMountNotExecuted`, `_AutoUpdateUsesAgreedRecord` |
| C-3 | P1 labels; P2 inspect and mask; P3 compose and `.env` | **B** | Widened redaction rules: URL scanner, key matching, masking net, argv and healthcheck, compose env under rule 1, dotenv fail-closed (AD-005, AD-020, AD-021) | Shared fixture `internal/redact/testdata/cases.json` in Go and TS tables; AC-062, AC-063, AC-083 |
| C-4 | P1, P2 | | Label redaction on every output path (including `/history`); command text masked at write; relay always masks | `TestDUX_SEC004_NoRouteReturnsRawLabel`, `_CommandTextMaskedAtWrite`, `_RelayMasksRegardlessOfCapability` |
| C-5 | P3 | | Outer `wd` bound, deny list including `/etc`, component-wise no-race open, FIFO-safe | `stackfs_test.go` (`wd=/`, ancestor of a denied root, `/etc`, symlink swap, FIFO); AC-084 |
| C-6 | P2 (P3 merged view) | | Bounded agent reader; drop before mask; server per-frame and rate checks; browser byte bound | `TestDUX_SEC006_NoNewlineStreamBoundedMemory`, `_DropBeforeMask`, `_ServerEnforcesBatchAndRate`; `logBuffer.test.ts` |
| C-7 | P4 | | 128-bit single-use previews; tag-to-ID check; volume `CreatedAt` check | `TestDUX_SEC007_PreviewIDEntropyAndSingleUse`, `_TagMovedToOtherImageNotRemoved`, `_RecreatedVolumeNotRemoved` |
| C-8 | P2 | | Auth method and stable subject persisted; legacy recorded as unknown | `TestDUX_SEC008_AuditStoresAuthMethodAndStableSubject`, `_OIDCUserNamedAdminDistinguishable`, `_LegacyTokenRecordedUnknown` |
| C-9 | P2 | | Per-kind audit retention; no cascade; device delete audited; open rows kept | `TestDUX_SEC009_RetentionPerKind`, `_AuditSurvivesDeviceDelete`, `_DeviceDeleteAudited`, `_OpenSessionNotPurged` |
| C-10 | P3 stacks; P4 inventory | | Server bounds on stack rows and fields, inventory fields and size, and inventory cadence | `TestDUX_SEC010_StacksPerDeviceCapped`, `_InventoryFieldBoundsAndSizeCap`, `_InventoryChangedPayloadRateLimited` |
| C-11 | P1 | | Agent URLs: http(s) only, IP literals parsed, ports checked, destination shown | `portLinks.test.ts`; `safeUrl` tests |
| C-12 | P2 | | `Origin` required on `/ws/docker/logs` | `TestDUX_SEC012_LogsWSMissingOrigin403` |
| C-13 | P1 | | Self targets computed per host; spoofing only adds targets | `TestDUX_SEC013_SpoofedServerHostDoesNotRemoveSelfTargets` |
| C-14 | P2 | | Capability tokens never relax a server bound or masking | `TestDUX_SEC014_ServerBoundsIndependentOfCapabilities` |
| C-15 | P2 | | Agent `command_id` validated (UUID, same device, window, key); inferred attribution labelled | `TestDUX_SEC015_ForeignOrMalformedCommandIDCleared` |
| C-16 | P2, P3 | | No content in agent logs | `TestDUX_SEC016_AgentLogsNoContent` |
| C-21 | P0 | **B** | Ingest timestamp window with re-stamp; no row reaches DEFAULT; history and buffer future bound; DEFAULT alarm | `TestDUX_SEC021_FutureAndPastDatedLogRowsNotStoredOutOfWindow`, `_GapEntryTimestampWindow`; `container_log_default_pg_test.go`; `logBuffer.test.ts` |
| C-22 | P0 | **B** | Migration classification by fingerprint; never force below 23 with F-LC; stop on dirty; renumbered PART-001 is a no-op | `TestDUX_SEC022_FingerprintClassification`, `_RenumberedPart001NoOpOnConvertedDB`, `_DirtyDatabaseRefused` |
| C-23 | P0 | | Server-owned `stream`; validated gap entries; unknown-container budget; per-device ceiling; mask after the ceiling | `TestDUX_SEC023_ForgedGapStreamNotStored`, `_GapEntriesValidatedAndBounded`, `_NameRotationHeldByDeviceCeiling`, `_MaskAfterCeiling` |
| C-24 | P1 (item 2); P3 (item 1) | | Writable-path refusal for `docker_update{compose_work_dir}` from P1; bind-backed and host-path volumes count as writable | `TestDUX_SEC024_P1AutoUpdateRefusesContainerWritableWorkdir`; `TestDUX_SEC024_BindBackedVolumeCountsAsWritable` |
| C-25 | P2 | | Linger and attach serialized; retired IDs; unknown-stream unfollow; `logs_keep`; agent reaping | `TestDUX_SEC025_AttachDuringLingerKeepsStream`, `_UnknownStreamFrameTriggersUnfollow`, `_AgentReapsOrphanedFollow` |
| C-26 | P2 | | Backfill rate limit, coalescing, boundary validation, own byte share | `TestDUX_SEC026_ReattachStormBounded`, `_LiveKeepsBudgetDuringBackfill` |
| C-27 | P2 | | Live-log socket closed at JWT `exp`, and capped at 4 h | `TestDUX_SEC027_SocketClosedAtTokenExpiry` |
| C-28 | P0 | | Write-time masking of stored logs from P0 | `TestDUX_SEC028_StoredLinesMaskedFromP0` |
| C-29 | P2 | | A `command` audit row for every operator command (Docker or not); the down migration deletes no audit rows | `TestDUX_SEC029_EveryOperatorCommandAudited`, `TestDUX_SEC029_DownMigrationKeepsAudit` |
| C-30 | P0 | | `db-fingerprint` is read-only by construction and secret-free; `migrate` does not serve; runbook hygiene | AC-163: `TestDUX_SEC030_FingerprintCreatesNothing`, `_FingerprintReadOnlySession`, `_FingerprintOutputHasNoDSN`, `_MigrateSubcommandDoesNotServe` |
| C-31 | P1 (history, Download); P2 (live) | | Download keeps both times, the skew marker and gaps; the buffer's future bound uses server time | `logDownload.test.ts`; `logBuffer.test.ts` (clock offset) |
| C-33 | P2 | | The release notes give the terminal-audit export step before upgrade; the first purge logs its count | `TestDUX_SEC033_FirstPurgeLogsDeletedCount`; release-notes review |
| C-35 | P0 (inherited by WP-2D) | | Body caps before decoding and per-device rates on every agent endpoint; agent WS read limit and frame bucket | AC-144 to AC-146: `TestDUX_BND_TelemetryIngestRate`, `_HeartbeatIngestRate`, `_AgentBodyCaps`, `_AgentWSReadLimitAndFrameRate`, `_ProbeResultBounds`; `TestDUX_AC144_AgentHonoursRetryAfter` |
| C-36 | P2 | | Bounded state keyed by agent-chosen identifiers | AC-153: `TestDUX_BND_RetiredStreamIDsBounded`, `_EventThrottleMapBounded`, `_EventBusUnsubscribeOnCancel` |
| C-37 | P0 | | Budget eviction; adaptive purges; 1 s drop locks stopping at the first timeout; startup sweep; server-wide singleflight; yesterday fallback | AC-135, AC-148, AC-150, AC-151, AC-152 |
| C-38 | P0 | | Compose log rotation on the server and the database; agent-ingest request logs summarized | `TestDUX_SEC038_ComposeLoggingOptions` (AC-154) |
| C-39 | P0 (inherited by WP-2D) | | Quotas degrade (refill, priority, sampling, fair share, downsampling); absolute-first thresholds; minimal mode | AC-147, AC-149, AC-164 to AC-168 |
| C-40 | P0 | | `riot-data` mounted only in the database; probe volume; mismatch fallback | `TestDUX_SEC040_OnlyDatabaseMountsDataVolume`, `_ProbePathMismatchFallsBack` (AC-170) |
| C-41 | P0 | | Pool session limits; blocker report; cancel only the server's own blockers in emergency | `TestDUX_BND_IdleTxBlockerDoesNotStallEviction`, `TestDUX_SEC041_EmergencyCancelsOnlyOwnBlockers` (AC-171) |
| C-42 | P0 | | **SEC-043.** Trimmed snapshots carry `trimmed:true` and the list of dropped sections. Every consumer treats a dropped section as "unknown", or uses the newest snapshot that contained it, never as empty or zero. When the kept sections alone exceed 2 MiB, the stored snapshot is still capped (for example by dropping heavy per-container fields), and the cut is counted | `TestDUX_SEC043_TrimmedSnapshotSectionsReadAsUnknown` (patch status, security overview, device page); `TestDUX_SEC043_DownsampleCapWhenKeptSectionsTooLarge` |
| C-43 | P0 | | **SEC-044.** `riot-server migrate` prints only the secret-free summary. Errors are reduced to a fixed message and class. It refuses a dirty database, and exits non-zero on every failure | `TestDUX_SEC044_MigrateOutputHasNoDSN` (bad password, unreachable host); `TestDUX_SEC044_MigrateRefusesDirty` |
| C-44 | P0 | | **SEC-045.** `riot-fsprobe` is mounted read-only. The mismatch check also compares growth: if the database grows by ΔD while the probe's used bytes do not grow by at least about ΔD over a window, the source is `mismatch`. Emergency drop steps never run on a source flagged `mismatch` | `TestDUX_SEC045_ProbeVolumeReadOnly` (compose static check); `TestDUX_SEC045_ProbeMismatchByGrowthDelta` |
| C-45 | P0 | | **SEC-046.** `db-fingerprint`, `migrate` and the startup migration connection use distinct `application_name` values (for example `riot-server-migrate`), and the emergency guard never cancels them | `TestDUX_SEC046_MaintenanceSessionsNotCancelled` |
| C-46 | P0 | | **SEC-047.** In sampled mode, the max-min per-container share also applies inside the priority lane. One container's stderr or error lines cannot take every refill token | `TestDUX_SEC047_PriorityLaneFairShare` |
| C-47 | P1 | | Weak-row fixes: D-48 limiters keyed only on known devices, with idle eviction (B-14); system-command cap and minimum cooldown (B-17) | AC-156 (`TestDUX_BND_LimitersKeyOnlyKnownDevices`, `_LimiterIdleEviction`); AC-158 (`TestDUX_BND_SystemCommandCap`) |
| C-48 | P2 | | Weak-row fixes: audit attach-row cap (B-16); event text caps and LRU (B-19); backfill memory (B-27); event-bus subscribers (B-29) | AC-157; AC-139 (extended), `TestDUX_BND_EventTextCapped`; AC-160; AC-153 |
| C-49 | P3 | | Weak-row fix: stack registry cap and prune (B-25) | AC-159 (`TestDUX_BND_StackRegistryCapAndPrune`) |
| C-50 | P4 | | Weak-row fix: preview store cap and sweep (B-12) | AC-155 (`TestDUX_BND_PreviewStoreCapAndSweep`) |
| C-51 | All phases | | Every §13A cap and every exported constant in `docker_policy.go` has a test; every new collection has a §13A row | `TestDUX_AC137_EveryBoundHasATest` |
| C-52 | P0 deploy (operational) | **B** | For **each** database: a verified backup, the §2.4.1 classification recorded, the server stopped from the backup through step 5, and operator sign-off for any `force` (D-43) | QA reviews the implementation-report record (fingerprint, action, version and dirty flag afterwards, backup verification), against C-30 and C-22 |
| C-53 | P0 release | **B** | No release contains WP-P0A or WP-P0C without WP-P0B (window, ceilings, masking and quotas at ingest) | QA reviews the release manifest; C-21, C-23, C-28 and C-39 tests pass in the same release |
| C-54 | P2 | | V-1: live-log frames never enter the agent outbox, the buffer or any file | `TestDUX_V1_LiveFramesNeverPersistedOnAgent` |
| C-55 | P2 | | V-2: viewer isolation. Filters and Pause are client-only; a backfill reaches only its requester | `TestDUX_V2_ViewerIsolation` |
| C-56 | P2 | | V-3: no storage from live logs, with several viewers, reconnects and backfills (including `server_logs`) | `TestDUX_AC049_LiveSessionNoStorage` (extended) |
| C-57 | P1 | | V-4: gap markers render only from structured entries; text imitating a marker renders as an ordinary line | `LogView.test.tsx` (marker-imitation case) |
| C-58 | P0 | | V-5: the retention parser accepts 1 to 90 only, and falls back to 7 with a Warn | `parseContainerLogRetention` table test |
| C-59 | All phases | | Verify as designed: route auth matrix; `no-store` on reads; `down` argv without `-v`, `--rmi` or `--remove-orphans`; no forced `ImageRemove` or `VolumeRemove`; no `RemoveVolumes`; no `dangerouslySetInnerHTML`; typed-confirmation errors never echo the expected value | `TestDUX_SEC_RouteAuthMatrix`; `TestDUX_AC079_DownArgvKeepsVolumes`; `TestDUX_AC096_NoVolumeSideEffects`; `TestDUX_AC064_InspectNotStored`; static guards (AC-099) |

**Conditions per phase** (a condition that spans phases is counted in each):

| Phase | Conditions | Blocking |
|---|---|---|
| P0 | 21: C-21, C-22, C-23, C-28, C-30, C-35, C-37 to C-46, C-51, C-52, C-53, C-58, C-59 | 4: C-21, C-22, C-52, C-53 |
| P1 | 11: C-1, C-3, C-4, C-11, C-13, C-24, C-31, C-47, C-51, C-57, C-59 | 2: C-1, C-3 |
| P2 | 22: C-3, C-4, C-6, C-8, C-9, C-12, C-14, C-15, C-16, C-25, C-26, C-27, C-29, C-31, C-33, C-36, C-48, C-51, C-54, C-55, C-56, C-59 | 1: C-3 |
| WP-2D | 3: C-35, C-39, C-51 (inherited) | 0 |
| P3 | 10: C-2, C-3, C-5, C-6, C-10, C-16, C-24, C-49, C-51, C-59 | 2: C-2, C-3 |
| P4 | 5: C-7, C-10, C-50, C-51, C-59 | 0 |

### CC.7 Public-repo hygiene at close

ADD rev 3.3, FRD rev 3 and this report contain no personal names, real hostnames, private IP
addresses, home paths or email addresses. Every example uses placeholders (`host-a`,
`192.0.2.x`, `example.internal`, `/srv/stacks/<name>`, `<db-host>`). Any runbook record or
`db-fingerprint` / `migrate` output pasted into implementation reports must follow the same
rule (C-30, C-43, C-52).

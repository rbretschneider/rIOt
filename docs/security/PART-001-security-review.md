# Security Review Report

- **Story ID:** PART-001
- **Reviewer:** Security Researcher Agent
- **Date:** 2026-09-10
- **Verdict:** REVISE (ARCHITECT)

---

## Scope note

This story's dominant risk class is **data-integrity / availability / migration-safety**,
not classic injection/authz. Per the review mandate these are weighted equally
with security-classic concerns. The two blocking findings below (SEC-PART-001-001,
SEC-PART-001-002) are correctness/availability defects in the migration and insert
path that would first manifest on the ~34 GB `Hawaii` production boot. The
classic-security surface (SEC-PART-001-006) is genuinely low and is confirmed safe.

---

## Threat Model Summary

### Assets

- **Hawaii production database (~34 GB live).** The conversion runs automatically,
  pre-serving, on the real box. A failed migration marks the golang-migrate schema
  **dirty** and `RunMigrations` returns an error, which aborts `Server.Start`
  (`server.go:88-93`) — i.e. a failed conversion is a **full monitoring-platform
  outage** until an operator manually intervenes. Availability of the boot path is
  the primary asset.
- **Existing telemetry/log rows (integrity).** ~28 GB of `container_logs` plus five
  other tables must survive the metadata swap uncopied and unlost. A mis-bounded
  legacy partition or a bad down-migration destroys or strands this data.
- **Ingest continuity (NFR-002).** The agent insert path (`INSERT`, `COPY`,
  `INSERT … SELECT … ON CONFLICT`) must never fail because a target partition is
  missing. A COPY batch that hits a missing partition fails the **entire batch**.
- **Business-logic integrity of retention.** Drop-partition retention must not drop
  in-window data and must not leave data un-retained (unbounded growth).

### Threat Actors

- **Automated systems / misconfigured devices (primary, non-malicious).** Agents
  push agent-supplied timestamps (`device_logs.timestamp` from the journal,
  `container_logs.timestamp`/`container_metrics.timestamp` from Docker,
  `heartbeats`/`telemetry_snapshots` payloads). A device with a bad clock (no NTP,
  wrong year, skew) is the realistic adversary here — it can produce future-dated
  rows that break both the conversion (VALIDATE) and the insert path (missing
  forward partition). This is the highest-probability failure driver.
- **Authenticated agent (valid `X-rIOt-Key`).** Same trust class as existing
  telemetry; can influence row *timestamps* and *package/container names*, but not
  DDL identifiers.
- **Operator.** Runs the boot; recovers a dirty schema; sets `RIOT_RETENTION_DAYS`.
- **PostgreSQL 16.** Enforces partition bounds, routes inserts.

### Attack Surface Introduced

- Five new migration files (`000024`–`000028`) that perform DDL on the six live
  tables, automatically, pre-serving, on every boot until converged.
- A new `partition_conversion` bookkeeping table that is the sole source of truth
  for the legacy partition bound (`cutoff`).
- A new Go partition manager (`internal/server/partition/`) that issues dynamically
  built `CREATE TABLE … PARTITION OF` / `DROP TABLE` DDL on a periodic loop.
- Removal of six `DELETE` retention calls; drop-partition becomes the sole retention.
- No new HTTP endpoint, auth path, external service, or dependency.

---

## Findings

### CRITICAL

None.

---

### HIGH

#### SEC-PART-001-001: Legacy-partition `cutoff` is a fixed `now + 2 days`, not derived from the data — a single future-dated row fails `VALIDATE` and dirties the Hawaii boot; the documented recovery is ineffective

- **Severity:** HIGH
- **Domain:** Data Integrity / Migration Safety / Availability
- **Location:** ADD AD-003 (`cutoff = date_trunc('day', now()) + INTERVAL '2 days'`),
  ADD §5 file `000024`/`000026`, ADD §9 (VALIDATE-fails row), ADD §12
  (dirty-schema recovery); FRD §7 validation rule ("upper bound strictly greater
  than max existing key"), NFR-001.
- **Description:** The bound that the legacy partition is attached under
  (`FOR VALUES FROM (MINVALUE) TO (cutoff)`, backed by
  `CHECK (<key> < cutoff) NOT VALID` then `VALIDATE`) is chosen as a **fixed offset
  from wall-clock now**, not as a function of the actual maximum key in each table.
  `VALIDATE CONSTRAINT` in file `000026` scans the legacy heap and **fails if any
  single existing row has `<key> >= cutoff`**. Whether that holds is entirely
  dependent on data the design does not inspect:
  - `device_logs.timestamp`, `container_logs.timestamp`, `container_metrics.timestamp`
    are **agent/Docker/journal-supplied**, not `DEFAULT NOW()` server stamps
    (confirmed: `device_log_repo.go:61`, `container_metric_repo.go:27` copy
    `m.Timestamp` straight through). A device whose clock is set ahead by more than
    ~2 days — or to the wrong year entirely — has already written future-dated rows.
  - On a 34 GB / 28 GB-per-table production history, the probability that **zero**
    rows across six high-churn tables are future-dated by >2 days is not something
    the design can assert. FRD §7 explicitly requires the bound be "strictly greater
    than max existing key"; AD-003 does not guarantee this — it guarantees only
    "≥ ~24 h in the future."
  - Failure mode: `VALIDATE` errors in `000026` → golang-migrate marks the schema
    **dirty** → `RunMigrations` returns error → `Server.Start` aborts → **Hawaii does
    not boot** and the monitoring platform is offline until manual recovery.
- **Attack / Failure Scenario:** A homelab device runs without NTP and its RTC reads
  2026-11 while real time is 2026-09. Its `container_logs`/`device_logs` rows carry
  `timestamp` ~2 months in the future. The operator upgrades the server; on boot,
  `000026 VALIDATE CONSTRAINT container_logs_pbound` scans the 28 GB legacy heap,
  finds those rows `>= cutoff`, and errors. The server exits; the whole fleet goes
  dark.
- **Compounding defect — the documented recovery does not work.** ADD §9/§12
  prescribe "widen `cutoff` (bump the `+2 days` margin) and re-run." But `cutoff` is
  **persisted** in `partition_conversion` by file `000024` with
  `ON CONFLICT DO NOTHING` (AD-003). Every later phase reads the stored value, and
  file `000024`'s `DO` block is guarded to skip a table already present. So editing
  the `+2 days` literal and re-running **does not change the stored `cutoff`** — the
  old row wins on conflict. Real recovery additionally requires a manual
  `UPDATE partition_conversion SET cutoff = … WHERE table_name = …` (and re-adding
  the `_pbound` CHECK with the new literal), which is **not documented anywhere**.
  The operator following the ADD's runbook will re-run, hit the same dirty state,
  and be stuck.
- **Required Resolution (blocks; ADD revision):**
  1. Derive `cutoff` per table from the data so the bound is *provably* satisfiable
     before `VALIDATE` runs — e.g. day-aligned
     `GREATEST(date_trunc('day', now()) + margin, date_trunc('day', (SELECT max(<key>) FROM <t>)) + INTERVAL '1 day')`
     computed and persisted once in `000024`. State in AD-003 that the invariant is
     `cutoff > max(existing <key>)` (FRD §7), and that an empty table falls back to
     the now-based value (FR-007). Amend AC-006/AC-016 to seed a future-dated row and
     assert `VALIDATE` still passes.
  2. Make the recovery real: document (ADD §12) that widening the margin requires
     updating the persisted `partition_conversion.cutoff` row (and the `_pbound`
     CHECK), not just the SQL literal — or, preferably, remove the need entirely by
     (1). Note that `date_trunc('day', now())` in `000024` is itself non-deterministic
     across a crash/next-day reboot; because it is persisted this is handled, but the
     `_pbound` CHECK literal added in the *same* file must be read back from the row,
     not recomputed (implementation note §12 already says this — keep it, and test it).
- **Blocks:** Implementation. This is the single most likely cause of a failed
  Hawaii production boot and the prescribed recovery is broken.

#### SEC-PART-001-002: NFR-002 ("inserts never fail") is not actually guaranteed — a future-dated row beyond the 3-day forward margin hits a missing partition and fails the whole COPY batch

- **Severity:** HIGH
- **Domain:** Availability / Data Integrity (ingest loss)
- **Location:** ADD AD-005 (create-ahead `[today-1 .. today+3]`), AD-001 (DEFAULT
  partition rejected), ADD §9 ("Should be impossible given EnsureAll + 3-day
  margin"), NFR-002; `container_metric_repo.go:29` (`CopyFrom`),
  `container_log_repo.go` (`CopyFrom`), `device_log_repo.go:63-77`
  (`CopyFrom` stage + `INSERT … SELECT`).
- **Description:** The manager pre-creates forward partitions only out to `today+3`
  (server wall clock). Any insert whose key is `>= today+4 00:00Z` routes to a
  partition **that does not exist**, and PostgreSQL raises
  `no partition of relation "<t>" found for row`. Because the design bounds forward
  partitions by the **server** clock (correctly — this is why a year-2099 device does
  **not** cause runaway partition creation; see Positive Observations), a device
  whose own clock is ahead by more than 3 days produces exactly such rows. The ADD
  dismisses this as "should be impossible," but device clock skew of >3 days is a
  routine homelab reality (dead RTC battery, no NTP, wrong timezone/year), and the
  insert path makes the blast radius **an entire batch**, not one row:
  - `container_logs` and `container_metrics` ingest via `pgx.CopyFrom`
    (`container_metric_repo.go:29`). A single out-of-range row aborts the whole
    `COPY` — every row in that agent push is rejected, including the well-dated ones.
  - `device_logs` ingests via `COPY` into a temp stage then one
    `INSERT … SELECT … ON CONFLICT` (`device_log_repo.go:72-77`); the `INSERT`
    statement fails as a unit if any staged row is out of range.
  This directly violates NFR-002 ("an insert can never fail because its target
  partition is missing") and causes silent telemetry/log loss for the affected pushes
  for as long as the device clock stays skewed.
- **Attack / Failure Scenario:** A device's clock jumps forward 10 days. Its next
  telemetry push carries `container_metrics` rows dated `today+10`. The `CopyFrom`
  fails with `no partition … found`; the ingest handler returns an error; **all**
  container metrics in that push are dropped. This repeats every cycle until the
  clock is fixed — the operator sees ingest failures for a device that is otherwise
  healthy, and loses that device's metrics/logs the whole time.
- **Required Resolution (blocks; ADD revision):** Provide a forward catch mechanism so
  an out-of-range future row cannot fail the batch. AD-001's rejection of a DEFAULT
  partition conflated two different cases: the *legacy* 28 GB blob (where a DEFAULT
  would be rescanned on every daily attach — correctly rejected) versus a small
  *forward* catch. Acceptable options for the architect to choose among and document:
  (a) a bounded/again-attachable forward catch partition that holds only the rare
  future row and is cheap to rescan because it is near-empty; (b) on-demand partition
  creation in the insert error path (catch `no partition found`, create, retry) — but
  this reintroduces dynamic DDL on the hot path and must be bounded (see
  SEC-PART-001-005's runaway concern); or (c) validate/clamp agent-supplied timestamps
  at ingest to a sane `[now-window, now+margin]` band and route out-of-band rows to a
  defined bucket rather than failing the batch. Whichever is chosen, NFR-002 must be
  restated honestly: the current design guarantees it only under the unstated
  assumption "no agent clock is ever >3 days fast," which is false for the target
  environment. Update AC-010 to cover a future-skewed insert.
- **Blocks:** Implementation.

---

### MEDIUM

#### SEC-PART-001-003: Manager create-ahead overlaps the legacy partition for the first ~2–3 days after conversion, producing a per-cycle DDL error storm that can trip the server-error-rate alert worker

- **Severity:** MEDIUM
- **Domain:** Availability / Observability (false-positive alerting)
- **Location:** ADD AD-005 (`PartitionsToCreate` returns `[today-1 .. today+3]`),
  AD-003 (`cutoff = today+2`), ADD §5 initial-partitions step; interaction with
  `newServerErrorAlertWorker` (`server.go:189`).
- **Description:** Immediately after conversion, the legacy partition owns
  `[MINVALUE, cutoff)` with `cutoff = date_trunc('day', now()) + 2 days`. `EnsureAll`
  then tries to create daily partitions for `today-1`, `today`, `today+1` — **all of
  which fall inside `[MINVALUE, cutoff)`** and therefore *overlap the legacy
  partition*. `CREATE TABLE … PARTITION OF` for an overlapping range does **not**
  raise a duplicate-object error (which AD-005 says is caught as success); it raises a
  distinct "partition would overlap" error. So for the ~2–3 days until the wall clock
  passes `cutoff`, every manager cycle logs three `slog.Error` records per table (up
  to ~18/cycle) for a benign, expected condition. Inserts themselves do **not** fail
  (rows `< cutoff` route to the still-present legacy partition, rows `>= cutoff` route
  to `today+2`/`today+3` which *are* creatable), so this is not an outage — but the
  rIOt server runs a **server-error-rate alert worker** (`server.go:189`) that
  "notifies the user when the server itself starts logging too many errors in a short
  window." A predictable post-upgrade ERROR storm is exactly the input that worker
  watches for, so a clean conversion can page the operator with a false alarm.
- **Failure Scenario:** Operator upgrades on Monday; conversion sets
  `cutoff = Wednesday 00:00Z`; the manager logs overlap errors every hour Monday and
  Tuesday; the server-error-rate worker fires a "server is logging errors" notification
  despite nothing being wrong.
- **Required Resolution:** The pure planner must not emit days whose range is fully
  below `cutoff` (i.e. create only for `day >= date_trunc('day', cutoff)`), so no
  create overlaps the legacy partition; alternatively, classify the overlap error as
  benign (like duplicate-object) so it does not log at ERROR. Reference AD-005 (planner)
  and AD-003 (cutoff). Add a unit test asserting `PartitionsToCreate` skips
  legacy-covered days for a freshly-converted table.
- **Blocks:** No — implement/test during impl phase.

#### SEC-PART-001-004: File `000027`'s O(1) `ACCESS EXCLUSIVE` claim is contingent on exact index-definition matching; a mismatch turns the swap into an O(rows) index build while the RENAME's AEL is held in the same transaction (NFR-001 violation)

- **Severity:** MEDIUM
- **Domain:** Migration Safety / Performance (lock duration)
- **Location:** ADD AD-002 (file `000027` is "the only ACCESS EXCLUSIVE step and is
  O(1)"), AD-004 ("adopt, don't rebuild"), ADD §5 SQL (`CREATE INDEX
  idx_container_logs_lookup ON container_logs …` after `ATTACH`), ADD §11 + §12
  implementation note ("A definition mismatch … forces a full rebuild"), NFR-001.
- **Description:** File `000027` runs as a single implicit transaction that begins with
  `ALTER TABLE … RENAME` (takes `ACCESS EXCLUSIVE`) and, later in the same
  transaction, issues `CREATE INDEX` on the partitioned parent for each FR-003 lookup
  index. The design's O(1) guarantee depends on PostgreSQL *attaching* the already-built
  legacy child index rather than *building* a new one — which only happens if the
  parent index definition matches the legacy index **exactly** (column order, `DESC`,
  opclass). The ADD itself warns (§12) that a mismatch "forces a full rebuild." The
  danger is that such a rebuild is O(rows) on the 28 GB legacy child **and the
  `ACCESS EXCLUSIVE` lock taken by the RENAME at the top of the same transaction is
  held until the transaction commits** — so a single wrong index definition converts
  the "brief metadata swap" into an `ACCESS EXCLUSIVE` lock held for the entire
  index-build duration, which is precisely the NFR-001 violation the five-file split
  exists to avoid. Because migrations run pre-serving this "blocks nothing" in the
  single-instance case, but NFR-001 is stated as a bound on lock *duration* / no
  rewrite, and the design's compliance is silently conditional on getting every index
  DDL byte-perfect.
- **Failure Scenario:** An engineer writes `idx_container_logs_lookup` without the
  trailing `DESC` on `timestamp`; on the Hawaii boot, file `000027` rebuilds the 28 GB
  index under the held AEL; the boot hangs for the build duration and any concurrent
  old-instance writer (rolling deploy) is locked out — the exact NFR-001 hazard.
- **Required Resolution:** Make adoption a verified invariant, not an assumption. The
  ADD should require (and AC-006 should assert) that after `000027` no heap or index
  `relfilenode` for a legacy child changed (no rebuild) and that the AEL hold time is
  row-count-independent (lock capture, already listed in AC-006 — make it a hard DoD
  gate, not illustrative). Consider isolating the lookup-index creation so an
  accidental rebuild cannot occur under the RENAME's AEL (e.g. verify each parent index
  definition against §2's exact list in code/review before merge). Reference AD-002,
  AD-004, §11.
- **Blocks:** No — but QA must verify the relfilenode/lock-capture assertions actually
  run and pass on a populated fixture.

---

### LOW / INFORMATIONAL

#### SEC-PART-001-005: Manager silent-stall — if partition creation fails persistently while the server keeps serving, inserts begin failing after the forward margin with only ERROR logs as signal

- **Severity:** LOW
- **Domain:** Availability / Observability
- **Location:** ADD AD-005 (loop "never dies," per-table failure isolation), ADD §9
  (manager panic/recover), FR-021, NFR-006.
- **Description:** The design's resilience story is "log ERROR and continue," with a
  3-day forward margin as the safety buffer. If `CREATE` fails for a *persistent*
  reason (disk full, a permission change, or the overlap condition of
  SEC-PART-001-003 masking a real failure), the manager logs ERROR every hour but
  never escalates; once the newest pre-created partition ages into the past
  (>3 days of continuous failure while serving), inserts start failing per
  SEC-PART-001-002. The only signal is log volume — there is no health endpoint,
  metric, or startup self-check that surfaces "forward partition coverage is below N
  days." For a self-hosted platform whose operator may not be tailing logs, this can
  degrade silently until ingest breaks.
- **Required Resolution (defense-in-depth):** Surface forward-coverage health (a
  metric, a device-page/status signal, or a WARN when the furthest future partition is
  within, say, 1 day of now) so a stalled manager is visible before ingest fails. Not
  a blocker; recommended alongside the SEC-PART-001-002 fix.
- **Blocks:** No.

#### SEC-PART-001-006: Dynamic DDL injection surface is minimal and confirmed safe; ensure identifiers are quoted with `%I`, not just the `cutoff` literal with `%L`

- **Severity:** INFORMATIONAL
- **Domain:** SQL Injection / Dynamic DDL
- **Location:** ADD §10, AD-005 (`PartitionName`, drop pattern match), ADD §12
  (`format('… TO (%L)', cutoff)`).
- **Description:** I confirmed no attacker-controlled value reaches a DDL **identifier**:
  table names come from a fixed in-code spec list; partition names are derived from the
  **server** wall clock via `PartitionName`; the `cutoff` literal is read from
  `partition_conversion` and baked via `format('%L', …)`; and `DROP` targets are matched
  against the fixed `<t>_p%` / `<t>_legacy` patterns before execution (ADD §10). Agent
  telemetry (timestamps, container/package names) never becomes an identifier. This is a
  genuinely low surface. One hardening note: table/partition identifiers built in the
  Go manager and in the `DO` blocks should be emitted with `format('%I', …)` (identifier
  quoting), consistent with the `%L` used for the literal, so the pattern is
  defense-in-depth-correct even if a spec entry were ever changed. No exploitable path
  today.
- **Required Resolution:** None required; use `%I` for identifiers as a convention.
- **Blocks:** No.

#### SEC-PART-001-007: `000023` per-table autovacuum reloptions are not carried onto the new parent or new daily partitions by `CREATE TABLE (LIKE …)` — immaterial

- **Severity:** INFORMATIONAL
- **Domain:** Migration Fidelity
- **Location:** `000023_autovacuum_tuning.up.sql` (sets
  `autovacuum_vacuum_scale_factor`/`threshold` on the six tables), ADD §5
  (`CREATE TABLE (LIKE <t>_legacy INCLUDING DEFAULTS INCLUDING GENERATED …)`).
- **Description:** `CREATE TABLE … (LIKE …)` does not copy storage/reloptions
  (autovacuum settings) even with `INCLUDING DEFAULTS INCLUDING GENERATED`; the new
  partitioned parent and each new daily partition will use PostgreSQL defaults. This is
  immaterial: the parent stores no rows, and daily partitions are short-lived and
  dropped (drop-partition retention generates zero dead tuples — the whole point of the
  story, NFR-004), so the interim autovacuum tuning is simply no longer needed for the
  forward partitions. The legacy partition keeps its `000023` reloptions (they travel
  with the renamed relation). No action needed; noted so the technical writer does not
  claim the `000023` tuning still applies to new partitions.
- **Blocks:** No.

---

## Migration-safety verdict (explicit, as requested)

- **Crash resumability between the five files — ADEQUATE in structure, with one broken
  recovery path.** Per-file atomicity (each golang-migrate file = one implicit
  transaction) genuinely prevents a *committed* half-converted table: a failure in any
  file rolls that file back entirely, and critically the rename→create-parent→attach are
  **all in one file (`000027`)**, so there is no committed/serving state in which the
  live table name has been renamed away but the partitioned parent does not yet exist —
  that window is intra-transaction only. golang-migrate version tracking handles
  idempotent re-runs on a normal reboot. **However**, recovery from the `dirty` state is
  the weak point: (a) the ADD's own most-likely dirty trigger (VALIDATE vs. a
  future-dated row) is real and under-mitigated (SEC-PART-001-001), and (b) the
  documented remedy ("widen the margin and re-run") **does not work** because `cutoff`
  is persisted with `ON CONFLICT DO NOTHING` and re-reads the stale value — a manual
  `UPDATE` is required and undocumented. Resumability of the *mechanism* is sound;
  resumability of the *documented operator procedure* is not. This must be fixed.

- **Cutoff-vs-data correctness — INADEQUATE.** The legacy bound is a fixed
  `now + 2 days`, not derived from `max(<key>)`, so FRD §7's "strictly greater than max
  existing key" invariant is not guaranteed. On agent-supplied timestamp columns over a
  34 GB history this is a realistic path to a failed production boot. This is the primary
  reason for the REVISE verdict and must be resolved in the ADD (SEC-PART-001-001).

---

## Positive Observations

- **Per-file transaction atomicity is used correctly.** Splitting the O(rows) prep
  (index build under `SHARE`, bound `VALIDATE` under `SHARE UPDATE EXCLUSIVE`) into
  their own pre-`ATTACH` files so the only `ACCESS EXCLUSIVE` file is O(1) metadata is
  the right shape for NFR-001, and keeping rename+create+attach in a single transaction
  eliminates any committed "stranded table name" window.
- **Dropping sole-`id` uniqueness is verified safe.** No foreign key references any of
  the six tables' `id` (grep of all migrations: none). The only `WHERE id =` sites in
  the codebase are on `ca_config` and `events` — both out of scope. `probe_results` uses
  `INSERT … RETURNING id` (`probe_repo.go:97-100`) only to populate the model; no read,
  update, or delete keys on `id` alone. AD-004's claim that nothing relies on
  single-column `id` uniqueness holds.
- **`device_logs` dedup survives partitioning.** The `ON CONFLICT
  (device_id, timestamp, priority, unit, (md5(message)))` arbiter
  (`device_log_repo.go:75`) matches a UNIQUE index whose key already includes the
  partition column `timestamp`, satisfying PG's "unique key must contain the partition
  key" rule — the dedup insert path keeps working on the parent.
- **Retention drop-eligibility is conservative.** Dropping by *upper bound ≤ now-window*
  over-retains by at most one interval (BR-008) and never drops in-window data; the
  legacy partition is only dropped once its `cutoff` (its newest possible row) is older
  than the window, so it over-retains rather than under-retains. No un-retained gap and
  no double retention after the six `DELETE` calls are removed.
- **Runaway partition creation is structurally prevented.** Forward partitions are
  bounded by the *server* clock (`PartitionsToCreate(now, …)`), not by observed device
  timestamps, so a device reporting year 2099 cannot make the manager pre-create
  thousands of partitions. (The trade-off is SEC-PART-001-002 — the same design choice
  means such a device's inserts have nowhere to go.)
- **Injection surface is minimal and confirmed.** No agent-controlled data reaches a DDL
  identifier; drop targets are pattern-gated to `<t>_p%`/`<t>_legacy`.

---

## Verdict Rationale

**REVISE (ARCHITECT).** No CRITICAL findings, and the core mechanical design is sound —
the five-file split honors the NFR-001 lock bounds, per-file atomicity prevents a
committed half-converted state, the sole-`id`-uniqueness drop is verified safe, dedup and
reads work transparently on the parent, and retention drop math is conservative.

However, two HIGH findings are architectural and block implementation:

- **SEC-PART-001-001** — the legacy `cutoff` is not derived from the data, so a single
  future-dated row (routine with agent clock skew on the six agent-timestamped tables)
  fails `VALIDATE`, dirties the schema, and prevents the ~34 GB Hawaii server from
  booting — and the documented recovery is ineffective because the cutoff is persisted
  and sticky. FRD §7 already mandates the correct invariant (`cutoff > max(existing
  key)`); the ADD must implement it and fix the recovery procedure.
- **SEC-PART-001-002** — NFR-002's "inserts never fail" is guaranteed only under the
  unstated, false assumption that no agent clock is ever >3 days fast; a future-dated row
  beyond the forward margin fails the entire `COPY`/staged-`INSERT` batch, silently
  dropping telemetry. AD-001's blanket rejection of a DEFAULT/catch partition conflated
  the legacy-blob case with a cheap forward catch and must be revisited.

Both require changes to AD-001/AD-003/AD-005 (and §9/§12 documentation) before the
engineering team writes the migrations. Two MEDIUM items (the post-conversion overlap
error storm / false-alert interaction, SEC-PART-001-003; and the index-adoption AEL
contingency, SEC-PART-001-004) and one LOW (manager silent-stall visibility,
SEC-PART-001-005) must be addressed/verified during implementation and QA.

---

## Routing

- **Verdict:** REVISE (ARCHITECT)
- **Next agent:** `architect` (do NOT invoke `senior-dev`). Pass this report.
- **Architect must revise the ADD for:**
  - SEC-PART-001-001: derive `cutoff` per table as `> max(existing key)` (FRD §7),
    day-aligned, empty-table fallback; fix the dirty-schema recovery to account for the
    persisted `cutoff` (AD-003, §9, §12).
  - SEC-PART-001-002: add a forward catch mechanism or ingest-timestamp band so a
    future-dated row cannot fail a COPY batch; restate NFR-002 honestly (AD-001, AD-005,
    §9).
  - SEC-PART-001-003: planner must not emit legacy-overlapping days (AD-005/AD-003), or
    treat overlap as benign, to avoid the error storm / false server-error alert.
  - SEC-PART-001-004: make no-rebuild/no-relfilenode-change/O(1)-AEL a hard, tested DoD
    gate for `000027`, not an illustrative assertion (AD-002, AD-004, §11).
- **QA must later verify:** future-dated-row conversion (VALIDATE passes), future-skew
  insert does not fail a batch, no post-conversion ERROR storm, `000027` relfilenode/lock
  capture, and drop-eligibility never drops in-window data.

---
---

# Re-review (revised ADD)

- **Reviewer:** Security Researcher Agent
- **Date:** 2026-09-10
- **Re-review verdict:** **APPROVED WITH REQUIRED MITIGATIONS**
- **Scope:** Re-verification of the revised ADD (`docs/architecture/PART-001-add.md`,
  DRAFT "revised after security review") against the seven prior findings, plus a
  fresh-issue scan of the two new mechanisms (forward DEFAULT catch partition AD-008;
  self-healing guarded `DO`-block recovery AD-003). Original findings above are kept
  verbatim for the record; nothing below rescinds them, it records their disposition.

The revision resolves both blocking HIGH findings and every MEDIUM/LOW finding. The
mechanical design is now correct: `cutoff` is data-derived and `VALIDATE`-cannot-fail
by construction, NFR-002 is a genuine guarantee via the DEFAULT catch, the overlap
storm is eliminated, and the O(1) swap is a build-failing DoD gate. No new CRITICAL or
HIGH issue was introduced. The residual items below are implementation/QA obligations
that bind `senior-dev` and `qa-engineer`; **they do not re-block the architect.**

---

## Per-finding disposition (one line each)

- **SEC-PART-001-001 (data-derived `cutoff`, working recovery):** **RESOLVED.**
- **SEC-PART-001-002 (NFR-002 via forward DEFAULT catch):** **RESOLVED** (residual R4: DEFAULT-row retention/coverage semantics — availability preserved, disk/retention-completeness trade surfaced via `DefaultRowCount`).
- **SEC-PART-001-003 (overlap storm / benign-error classification):** **RESOLVED.**
- **SEC-PART-001-004 (O(1) swap DoD gate):** **RESOLVED** (residual R1: the gate must actually run on a populated fixture in CI and must fail the build — not be skipped when PG is absent).
- **SEC-PART-001-005 (silent-stall health surface):** **RESOLVED.**
- **SEC-PART-001-006 (`%I`/`%L` identifier quoting):** **RESOLVED** (adopted as convention, section 10/12).
- **SEC-PART-001-007 (autovacuum reloptions):** **RESOLVED** (deliberate no-carry-forward decision, AD-009).
- **New-issue scan (DEFAULT partition + guarded `DO`-block recovery):** **NO NEW BLOCKING ISSUE.** Two residual awareness items (R4, R5) and one classification guard (R6) noted below.

---

## Verification detail

### SEC-PART-001-001 — cutoff / VALIDATE-cannot-fail — RESOLVED

`cutoff = date_trunc('day', GREATEST(now(), (SELECT max(<key>) FROM <t>))) + INTERVAL '1 day'`
(AD-003, section 5 file 000024).

- **(a) Strictly greater than every existing key — proven.** Let `M = max(key)` and
  `G = GREATEST(now(), M) >= M`. `D = date_trunc('day', G)` satisfies `D <= G < D + 1 day`,
  so `cutoff = D + 1 day > G >= M`. The day-boundary case (`M` exactly at `00:00Z`,
  `G = M`, `D = M`) still gives `cutoff = M + 1 day > M`. The FRD section 7 invariant
  `cutoff > max(existing key)` holds by construction.
- **(b) Empty-table fallback — correct.** `max(key)` is `NULL`; PG `GREATEST` ignores
  `NULL` arguments, so `GREATEST(now(), NULL) = now()` and `cutoff` day-aligns from
  `now()`. No special-case branch, matches FR-007/AC-008.
- **(c) No writer can race compute->VALIDATE — confirmed against code.** `server.go:88-93`
  runs `RunMigrations` before repos are constructed (`:95+`), before the router/`http.Server`
  is built (`:194-206`), and before `ListenAndServe` (`:219+`). During files 000024->000026
  there is no ingest handler and no serving path in-process, so the max key file 000026's
  `VALIDATE` can observe is exactly the one file 000024 measured. The scan is a formality.
  (One caveat becomes residual R5 for the rolling-deploy case.)
- **(d) Recovery genuinely recomputes; CHECK matches attached bound — confirmed.** File
  000024's guarded `DO` block: if already partitioned -> no-op (historical row untouched);
  else recompute from live `max(key)`, `ON CONFLICT (table_name) DO UPDATE SET cutoff = ...`
  (no longer `DO NOTHING`), then `DROP CONSTRAINT IF EXISTS <t>_pbound` and re-`ADD ... CHECK (%I < %L) NOT VALID`
  from the same `v_cutoff`. Persisted `cutoff` and the `_pbound` literal derive from one
  variable in one transaction, so they cannot diverge. File 000027 attaches
  `FOR VALUES ... TO (<cutoff>)` reading the same persisted row. golang-migrate only re-runs
  file 000024 when the dirty version is 000024 (force back to 000023); a failure at a later
  file re-runs only from the forced version and leaves the already-persisted, already-matching
  `cutoff`/CHECK intact. The prior sticky-`DO NOTHING` recovery bug is closed and no operator
  SQL hand-edit is required. AC-022c gates this.

### SEC-PART-001-002 — NFR-002 via forward DEFAULT catch — RESOLVED

- The `<t>_default` DEFAULT partition is created in file 000027 immediately after the
  legacy attach (section 5), i.e. pre-serving. Because a DEFAULT partition catches every row
  not covered by a bounded partition — future-skewed rows *above* the furthest daily
  partition, gap rows, and (after the legacy partition is eventually dropped) old rows
  *below* all remaining bounded partitions — no insert can ever hit "no partition of relation
  found." NFR-002 is now a genuine guarantee independent of agent clock skew, so no `CopyFrom`
  / staged-`INSERT` batch can fail on a missing partition (AC-021b).
- **New interaction (DEFAULT already holds a row for a day the manager tries to create):**
  handled correctly. PG raises `SQLSTATE 23514` on `CREATE ... PARTITION OF ... FOR VALUES`;
  AD-005/AD-008/section 9 classify this as benign -> WARN + skip that day. The row is **not
  lost**: it stays in the DEFAULT, and inserts for that day continue to route to the DEFAULT
  and succeed. That day simply has no dedicated bounded partition — coverage remains complete.
- DEFAULT rows are honestly documented (AD-008) as non-auto-migrating and the DEFAULT as
  never-dropped; `DefaultRowCount` per table is surfaced in `Health()` and logged WARN when
  non-zero, so a skewed device is visible. This is an accepted, documented trade
  (availability over perfectly-granular retention of anomalous rows) — see residual R4.

### SEC-PART-001-003 — overlap storm / benign classification — RESOLVED

- `PartitionsToCreate(now, cutoff, ahead, behind)` is now cutoff-aware: it emits day-starts
  in `[max(now-behind, cutoffDay) .. now+ahead]` and never a day whose range is fully below
  `cutoff`, so no `CREATE ... PARTITION OF` overlaps the legacy `[MINVALUE, cutoff)` — the
  ~18-ERROR/cycle legacy-overlap storm is gone (AC-021a unit gate).
- The benign set (`42P07` duplicate, `23514` DEFAULT-capture, `42P17` overlap) -> WARN does
  **not** swallow page-worthy errors: disk-full / permission / any other `SQLSTATE` remains
  ERROR, is per-table isolated, retried, and escalates a single ERROR after N=3 consecutive
  failures (AD-005, section 9). A genuine failure to create a needed partition therefore still
  pages. (Minor: `42P17` classification could theoretically mask a planner date-math bug at
  runtime, but AC-021a is a DoD unit gate on the planner, and a persistently non-advancing
  furthest-future partition escalates via the coverage WARN — acceptable.)

### SEC-PART-001-004 — O(1) swap DoD gate — RESOLVED

- AC-019 is now a real build-failing DoD gate (section 8, 11, 13): capture `relfilenode` of
  every legacy child index **and** the legacy heap before/after 000027 and assert
  **unchanged** (no rebuild/rewrite), on both a tiny and a large fixture, and assert AEL hold
  time is row-count-independent. Section 13 states explicitly "A relfilenode change fails the
  build." The exact per-table parent index DDL to byte-match is enumerated in AD-004. This is
  no longer illustrative. (Residual R1: QA must confirm the integration gate actually executes
  on a populated fixture in the PG-backed CI job and is not silently skipped.)

### SEC-PART-001-005 — silent-stall health surface — RESOLVED

- The manager exposes a mutex-protected `Health()` snapshot (`LastRunAt`, `LastSuccessAt`,
  `LastError`, per-table `FurthestFuturePartition`, `DefaultRowCount`). Persistent stall is
  surfaced loudly: WARN when forward coverage is within 1 day of `now`; a single escalated
  ERROR after N=3 consecutive failures, routed through the DB log handler (`server.go:114-129`)
  so it is visible without tailing stdout (and, appropriately, can trip
  `newServerErrorAlertWorker` for a genuine stall). Non-zero `DefaultRowCount` emits WARN.
  AC-020 covers it. Adequate for a self-hosted operator.

### Fresh-issue scan of the new mechanisms — NO NEW BLOCKING ISSUE

- **DEFAULT vs. retention drop / DETACH:** the DEFAULT is never dropped and never blocks a
  bounded child's `DROP TABLE`; retention continues to drop daily and legacy partitions
  normally. Creating a new bounded child triggers only a scan of the near-empty DEFAULT
  (not the 28 GB legacy blob, which is a bounded partition and never rescanned). No DETACH is
  used in steady state.
- **DEFAULT catches below-range rows after legacy drop:** once the legacy partition ages out
  and is dropped, an old-dated insert (below all remaining bounded partitions) still lands in
  the DEFAULT — NFR-002 has no post-legacy-drop gap.
- **`DO` block transactionality:** golang-migrate sends each file as one implicit
  transaction; the `DO` block and its `ALTER TABLE ... ADD/DROP CONSTRAINT` are transactional
  DDL inside that txn, so a failure rolls the whole file back atomically — no half state, no
  `CONCURRENTLY` inside a `DO` block. Recovery cannot leave the `_pbound` CHECK and the
  attached bound inconsistent (shown in SEC-001(d)).
- **Down-migration + DEFAULT:** 000027.down drops the DEFAULT (and forward partitions),
  detaches legacy, drops the parent, renames back — lossy-by-design and documented, with the
  DEFAULT-row data-loss caveat called out (AD-007, AC-018). Dev/test rollback only.

---

## Required mitigations (bind senior-dev / qa-engineer — do NOT re-block the architect)

- **R1 (SEC-004, QA):** The AC-019 relfilenode/no-rewrite + row-count-independent-AEL gate
  must actually run on a *populated* fixture in the PG-backed CI job and must **fail the
  build** on any relfilenode change. Confirm it is not skipped/no-op'd when PG is unavailable
  — a skipped gate silently reverts SEC-004 to an assumption.
- **R2 (SEC-001, QA):** AC-022a/b/c must (a) seed a legacy row dated `now+60d` and assert
  `VALIDATE CONSTRAINT` passes with `partition_conversion.cutoff` strictly greater than that
  row; (b) assert the empty-table `GREATEST(now(), NULL)` fallback yields a day-aligned
  now-based cutoff on the actual PG 16; (c) abort mid-conversion pre-swap, `migrate force` +
  re-run, and assert file 000024 recomputes `cutoff` from live `max(key)` (no stale value
  survives) and the `_pbound` literal matches the attached bound.
- **R3 (SEC-002/003, QA):** AC-021b must assert a `CopyFrom` batch containing an
  out-of-margin future row (`now+10d`) **succeeds** with the row in `<t>_default` and the
  whole batch inserted, and that the manager's subsequent DEFAULT-capture create error logs
  at **WARN, not ERROR**. Verify the exact PG 16 `SQLSTATE`s (`23514`/`42P17`/`42P07`) by
  observation and classify by code, never message text (section 12).
- **R4 (SEC-002, ops/impl awareness):** Rows in the DEFAULT are never dropped by retention,
  and a stale future-dated DEFAULT row can permanently prevent that calendar day's dedicated
  bounded partition from ever being created (perpetual `23514`), so that day's *legitimate*
  traffic then also accumulates un-retained in the DEFAULT. Availability is preserved
  throughout; this is a disk/retention-completeness residual, self-healing once the operator
  acts on the `DefaultRowCount` WARN. `senior-dev` must implement the `DefaultRowCount` WARN;
  the technical writer must document the manual remediation
  (`DELETE FROM <t>_default WHERE <key> < now() - <window>`).
- **R5 (SEC-001, ops/impl awareness):** The "no writer races compute->VALIDATE" guarantee
  holds for the single-instance target only. In a rolling deploy an old instance still serving
  could insert a row `>= cutoff` in the window between file 000024's commit and file 000025's
  `SHARE` lock, which would fail file 000026's `VALIDATE` and dirty the boot. This is the
  documented single-instance assumption; the self-healing `force`+reboot recovery covers it
  (the old instance is gone by the retry). No code change required — awareness only.
- **R6 (SEC-003, QA):** Confirm the benign-error set is exactly `42P07`/`23514`/`42P17` and
  that genuine failures (e.g. disk-full `53100`, permission `42501`) still log ERROR, isolate
  per-table, retry, and escalate after N=3 — i.e. the WARN classification does not swallow a
  page-worthy failure to create a needed partition.

---

## Re-review routing

- **Verdict:** APPROVED WITH REQUIRED MITIGATIONS
- **Next agent:** `senior-dev` (implement the ADD; carry residuals R1–R6). Pass this report
  alongside the ADD. Do **not** route back to the architect — no residual requires an
  architectural change.
- **QA must later verify:** R1 (relfilenode/AEL gate runs on a populated fixture and fails
  the build), R2 (future-dated-row VALIDATE + empty fallback + crash-recompute recovery),
  R3 (future-skew batch succeeds via DEFAULT; DEFAULT-capture logs WARN), R6 (benign set
  does not swallow disk-full/permission), and that `DefaultRowCount`/coverage WARN + N=3
  stall ERROR actually fire (R4/SEC-005).

# Architecture Decision Document

**Story ID:** DOCKER-UX-001
**FRD Reference:** docs/requirements/DOCKER-UX-001-frd.md (revision 2)
**Security Review:** docs/security/DOCKER-UX-001-security-review.md (APPROVED WITH CONDITIONS, phase-gated, including its "Revision 2", "Revision 3", "Revision 3.1" and "Revision 3.2" addenda). These are folded into this revision (see §19):
- every gating condition of the original review and of the revision 2 addendum (SEC-001..SEC-029);
- the revision 3.1 addendum's required ADD amendments (SEC-035, SEC-037) and conditions (SEC-036, SEC-038, the R31.1 weak rows);
- the revision 3.2 addendum's required ADD amendments (SEC-039, and SEC-030 written into the runbook body) and conditions (SEC-040, SEC-041, SEC-042).
**Author:** Architect Agent
**Date:** 2026-10-19
**Status:** DRAFT, revision 3.3 (pending the product owner's open decisions in §17)

## Revision history

| Rev | Date | Change | By |
|---|---|---|---|
| 3.3 | 2026-10-19 | **Inputs.** The security review's revision 3.2 addendum (SEC-039 and the SEC-030 runbook text gate P0; SEC-040, SEC-041 and SEC-042), the product owner's concern about losing important log data, and the standing bounds requirement. **Quotas degrade instead of stopping (D-54):** continuously refilling buckets replace the midnight reset; past 75 % used, stderr and error lines go first, other stdout is sampled to 5 lines per container per minute, and each container gets a max-min fair share; new `gap_quota` rows and a Logs-tab notice record the sampling; telemetry is downsampled to one trimmed snapshot per 15 min instead of stopping. **Headroom (D-55):** absolute-first thresholds; emergency ends in a minimal mode that never stops all storage; a critical step keeps today only. **Fairness (D-56):** when the fleet budget binds, the heaviest devices' allowances shrink before anyone's retention does; early drops only above 110 % or under disk pressure. **SEC-040:** the database volume is no longer mounted into the server; an empty `riot-fsprobe` volume serves `statfs`. **SEC-041:** pool session limits, blocker reports, and emergency cancellation of the server's own blockers only. **SEC-030:** written into the §2.4.1 body (server stopped through step 5 with a new `riot-server migrate` subcommand, no password on the command line, dump handled as a credential, read-only role, F-AV rows, tests). **Amended:** AD-035 §5, AD-036, AD-041 §1–§3, §2.4.1, §6.1, §6.2, §7.2, §8.3, §8.13, §8.14, §9.1, §10..§19. **New ACs:** AC-163..AC-171; AC-147 and AC-149 amended. **Decisions:** D-54..D-56 adopted by default; D-53's working default endorsed by the review. | Architect Agent |
| 3.2 | 2026-10-19 | **Inputs.** The security review's revision 3.1 addendum (SEC-035 and SEC-037 gate P0; SEC-036, SEC-038 and the R31.1 weak rows are conditions), and the product owner's standing bounds requirement. **New ADs:** AD-040, agent ingest bounds (body caps before decoding and per-device rates on every agent endpoint, the agent WS read limit, and request-log summarizing; SEC-035, SEC-038, D-51); AD-041, daily quotas, fleet storage budget and disk headroom (D-49, D-50; SEC-037 item 1). **Amended:** AD-016 (retired stream IDs and unknown-stream unfollow bounded; audit attach-row cap); AD-017 (event-bus subscriber cap, unsubscribe on cancel, backfill memory test); AD-025 (adaptive purge batches, output deleted before commands, disk statement corrected); AD-029 (preview store test, D-52 superseded images); AD-035 §1b (startup ensure over the whole retention window, `DETACH … CONCURRENTLY` or a 1 s-lock fallback, stop at the first lock timeout, server-wide singleflight, the yesterday fallback); AD-035 §2 and §5; §6; §7.2; §7.3; §8.3; §8.11..§8.14; §9.1; §11..§19. **§13A:** B-34 fixed; weak rows B-12, B-14, B-16, B-17, B-19, B-25, B-27 and B-29 fixed; new rows B-38..B-48; new per-device and fleet worst cases; full-disk statements corrected. **New ACs:** AC-144..AC-162. **Decisions:** D-49..D-52, adopted by default under the standing bounds requirement (the product owner may tune values); new open decision D-53 (partition drop method while PART-001 keeps a DEFAULT partition). **Correction:** the merged agent socket sets no WS read limit; §12 item 8 said it did. | Architect Agent |
| 3.1 | 2026-10-19 | **Input.** The product owner's standing requirement that everything this story writes is self-healing and can never bloat the server. **New:** §13A, the bounds and self-healing table: one row per growth path, the worst-case sum per device per day, and the 25-device steady state. Also new: AD-035 §1b, the partition janitor (startup ensure, bounded catch-up, backoff, an ingest partition guard so rows never reach DEFAULT, a stale-janitor event); self-healing rules for every purge (AD-025); the bounds design rule (§14.1 items 29–31); ADD-derived AC-134..AC-143. **Tightened:** the per-device stored-log ceiling (5,000 lines and 2 MiB per minute, D-46); a gap-row cap per device; an LRU cap on the per-(device, container) limiter map; a 1 MiB byte cap per viewer queue; a server-wide cap on agent RPCs; an agent pending-batch cap; a 64 MiB byte cap on the agent telemetry buffer; a write budget for `docker_stacks`; truncated prune results; server and agent log budgets. **New decisions:** D-46, D-47 (observed-event throttle), D-48 (operator command and terminal-open rate limits). | Architect Agent |
| 3 | 2026-10-19 | **Inputs.** Product-owner decisions settled in revision 3: D-1, D-21 (180 days), D-31, D-35 with D-37, D-42 (re-stamp), D-43 (per-database sign-off). Also the production migration fact, and the security review's revision 2 addendum (SEC-021..SEC-029, gated P0..P4). **New sections:** §0.0; §2.4.1 reconciliation runbook; §2.4.2 production fact; AD-035 §1a timestamp window and re-stamp. **Amended:** §2.2, §2.4, §4.2, AD-015, AD-016, AD-017, AD-018, AD-024, AD-025, AD-032, AD-035 §5 and §6, AD-036, AD-038, §6, §7.2, §7.3, §8.3, §8.10..§8.12, §9.1, §11..§19. **Moved:** write-time masking of stored logs, from WP-1B to WP-P0B (SEC-028); the agent writable-path refusal and `docker.compose_trusted_roots`, from WP-3A to WP-1A (SEC-024, D-44). **New decision:** D-44. | Architect Agent |
| 2 | 2026-10-19 | **Inputs.** Amended for FRD revision 2 (S-1..S-3, phase P0, FR-148..FR-165, NFR-024..NFR-027, AC-107..AC-121, OQ-07..OQ-10, D-33..D-36) and for the security review (SEC-001..SEC-016). **New ADs:** AD-034 (request-origin control and anti-framing), AD-035 (stored-log rotation, P0), AD-036 (one Logs tab, history API and line identity), AD-037 (shared live streams), AD-038 (stack path trust), AD-039 (stored and live logs trade-offs). **Rewritten:** AD-015..AD-018. There is now one agent follow per container plus a separate, bounded boundary backfill. The `mode` URL parameter and the separate History and Live components are removed. **Amended:** AD-001, AD-003, AD-005, AD-006, AD-007, AD-009, AD-011, AD-019..AD-029, AD-032, AD-033. **Gate:** CMD-LIFECYCLE-001 (PR #9) and UI-FOUNDATION-001 (PR #6) are both on `main`. WP-0 now means "rebase and reconcile" (§2.1), with a drift table against the merged code (§2.5). **P0:** new work packages WP-P0A, WP-P0B and WP-P0C, with migration sequencing (§2.4). Main holds `000023_command_lifecycle`, so PART-001 must renumber `000023`–`000028` when it lands. **Other:** new §19 maps each security finding to its AD. §17 lists every decision with its owner document and status. New decisions D-37..D-41. Revision 2 text made self-contained: unchanged revision 1 content restored inline. | Architect Agent |
| 1 | 2026-10-18 | Initial ADD (commit 78b777b) | Architect Agent |

**Detected stack.**

- **Server:** Go 1.24 with chi v5, pgx v5, golang-migrate v4, gorilla/websocket and slog.
- **Agent:** Go with the Docker SDK v28 (API version negotiation), `gopkg.in/yaml.v3` (already in `go.mod`), gorilla/websocket, and a SQLite telemetry buffer (`internal/agent/buffer.go`).
- **Database:** PostgreSQL 16.
- **Web:** React 19, TypeScript, Vite, Tailwind v4, react-router-dom 7, TanStack Query 5, recharts 3, `@floating-ui/react`, `@xterm/xterm`, vitest 4 with happy-dom and Testing Library.

**Dependency position.**

- This story adds **no Go dependency and no npm runtime dependency**.
- The no-race file reads (AD-021) use the standard library `syscall` package on Linux.
- One optional npm **devDependency**, `axe-core`, is still put to the product owner as D-24. The security review endorses it.

**Public repository rule.** Every example uses placeholders:

| Kind | Placeholder |
|---|---|
| Hosts | `host-a`, `host-b` |
| Device IDs | `<device-id>` |
| Addresses | `192.0.2.10` |
| Stack directories | `/srv/stacks/<name>` |
| Domains | `example.internal` |

Fixtures, demo data and tests must do the same (NFR-021, AC-105).

---

## 0. FRD review outcome

### 0.0 Revision 3 review (product-owner decisions and the security addendum)

**Settled by the product owner in revision 3.** §17.1 lists each one.

- **D-1.** Compose stays read-only.
- **D-21.** The long audit class is kept for 180 days.
- **D-31.** No personal names appear in this story's documents.
- **D-35 with D-37.** PART-001 is merged whole, as P0's first step, with its migrations renumbered above `000023_command_lifecycle`.
- **D-42.** Out-of-window log timestamps are re-stamped, never dropped.
- **D-43.** Any reconciliation on a deployed database needs that database's operator sign-off.

**Production fact.** The production deployment moved from schema 22 to 23,
not dirty, on the v2.67.0 release. So `000023_command_lifecycle` ran there,
and the skipped-migration risk is resolved for that deployment (§2.4.2, R-3).

**Security addendum** (SEC-021..SEC-029, gated P0..P4). This revision writes
in every amendment the addendum gates:

| Phase | Finding | Where |
|---|---|---|
| P0 | SEC-021 | AD-035 §1a, AD-036, AD-016 |
| P0 | SEC-022 | §2.4.1 |
| P0 | SEC-023 | AD-035 §5 |
| P0 | SEC-028 | AD-035 §6, WP-P0B |
| P1 | SEC-024 item 2 | AD-038, WP-1A, D-44 |
| P2 | SEC-025, SEC-026, SEC-027 | AD-015..AD-018 |
| P2 | SEC-029 | AD-024, §7.2 |
| P3 | SEC-024 item 1 | AD-038 §3 |

§19 maps each finding.

**One review recommendation is replaced by a product-owner decision.**

- The review recommended dropping out-of-window rows. D-42 settles on re-stamping instead: no line is ever discarded for its timestamp.
- SEC-021's requirement still holds. A row is never stored at its original out-of-window time: the partition key is the server receive time, and the agent's time is kept only in `agent_ts`.
- The review's concern about duplicates at the history/live boundary is handled by comparing line identity in the agent's clock domain (AD-036).

### 0.1 Revision 2 review (FRD rev 2 and the security review)

**Settled by the product owner and now binding:**

- **S-1:** the fleet-wide Docker section replaces the per-device Docker page.
- **S-2:** legacy URLs redirect. This was D-18.
- **S-3:** stored logs and live logs share one Logs tab.

AD-011, AD-015..AD-018 and AD-036 implement them.

**Conflicts in FRD rev 2, resolved here:**

1. **OQ-10, the Logs tab and live sessions.**
   - Revision 1 modelled one agent session per browser socket, a separate Live mode, and a `mode` URL parameter. All three are withdrawn.
   - AD-036 defines one Logs tab: stored history first, Live appended below it. Live is never taken from the URL (FR-164).
   - AD-037 defines one agent follow per container, fanned out to every viewer (D-36). Each viewer gets a separate, bounded boundary backfill (OQ-09).
2. **Stream bounds against merged stack views.**
   - FRD §8.8 allows 8 distinct live streams per host, but up to 25 containers in a merged stack view. A live merged view of more than 8 running containers on one host cannot satisfy both bounds.
   - **Resolution:** stored history merges up to 25 containers. Live follows at most the host's free stream slots, and a picker chooses which containers (D-38, AD-037).
3. **Timestamp precision for line identity.**
   - FRD §8.12 requires the timestamp "exactly as Docker reports it (full precision)". `container_logs.timestamp` is `TIMESTAMPTZ`, which holds microseconds. Docker reports nanoseconds.
   - **Resolution:** AD-036 defines identity at **microsecond** precision in both paths, and the agent truncates live timestamps to microseconds. The same line therefore compares equal whether it came from storage or from the live tail.
4. **Retention setting before partitioned rotation exists.**
   - FR-149 adds a retention setting. NFR-024 and D-35 forbid widening the row-DELETE path.
   - **Resolution:** AD-035 parses and validates the setting from WP-P0B onward. The **effective** retention stays 7 days until partitioned rotation (WP-P0C) is live. The dashboard always shows the effective value (FR-161).
5. **Gap markers and older servers.**
   - If gap markers travelled as ordinary log entries, an older server would store them as text lines.
   - **Resolution:** AD-035 sends them in a separate additive telemetry field, which older servers ignore.
6. **Exact drop counts against agent memory.**
   - FR-152 asks for the exact number of lines not stored. Counting every line of a runaway container costs unbounded agent work.
   - **Resolution:** AD-035 bounds the scan. Beyond the bound the count is a lower bound ("more than N"). See D-41.

**Security review.** Verdict: APPROVED WITH CONDITIONS, gated per phase. This revision writes into the named ADs (§19):

- every ADD amendment the review gates on P1..P4 (SEC-001..SEC-010);
- every mandatory LOW finding (SEC-011, SEC-012, SEC-014, SEC-016);
- the optional SEC-013, SEC-015 and SEC-017.

Two review resolutions change revision 1 defaults:

- **D-27** is now rule 1: every compose `environment:` value is redacted except safe-list keys.
- **D-21** audit retention is now per kind. It was settled in revision 3: 180 days for the long class, 90 days for the short class (AD-024).

### 0.2 Revision 1 review (still applies)

**Assumptions that do not hold:**

1. **A-04 is false.** On `main@a7b9f9e`, `issueSessionCookie` mints `{"sub":"admin","exp","iat"}` for both password and OIDC logins, and `AdminAuth` puts only `true` in the request context. The server therefore has no user identity when it creates a command. AD-022 (revised for SEC-008) adds identity claims.
2. **A-03 is false.** `commands`, `command_output` and `terminal_sessions` have no retention. AD-025 adds it, and AD-024 makes audit retention per kind (SEC-009).

**Requirements that cannot be met exactly as written:**

3. **FR-042, bulk Update.** One command per target would conflict and race. The design uses one `docker_bulk_update` per host, with per-target results (AD-008, D-19).
4. **FR-094 and FR-095, step output.** Step output is shown in full when the command ends. While the command runs, Activity shows the step and its progress message (AD-023, D-20).
5. **AC-100 and AC-101, browser harness.** The repository has no browser test harness. AD-033 uses vitest guards plus a recorded manual check. The automated accessibility scan needs D-24.
6. **FR-026, selection in the URL.** The selection is omitted above 200 entries **or** above 4 KiB encoded (D-23).

**Other gaps closed:**

- **OQ-05 premise.** Terminal session audit records are kept forever today. NFR-011 asks for "no longer than terminal session retention". AD-024 gives both kinds of record one store, with the per-kind retention above (D-21).
- The configured memory limit is reported, and the agent computes I/O rates (AD-004).
- The Docker navigation entry follows the existing `docker` feature toggle (D-22).
- Labels are redacted at the source and again on every output path (AD-006).

---

## 1. Summary

Docker becomes a top-level dashboard section, built only on the
CMD-LIFECYCLE-001 command service and the UI-FOUNDATION-001 primitives. Both
are now on `main`. The story ships in five phases, each independently
releasable.

**P0: stored-log rotation.**

- `container_logs` moves to whole-day partitions, dropped by age, through PART-001's conversion.
- A dedicated retention setting is added: default 7 days, range 1 to 90.
- Caps are enforced per container on the agent, with a hard ceiling on the server.
- Gap markers record every line that was not stored.

**P1 to P4: fleet UI and actions.**

- The fleet table reads the extended FLEET-DASH projection and merges live telemetry. Its windowing is hand-rolled, with no new dependency.
- Bulk, stack and prune actions go through one command service. It gains "probe keys" so that container, stack and host-wide locks compose correctly.
- Every new state-changing route sits behind a same-origin, JSON-only control and anti-framing headers (SEC-001).

**The Logs tab** is one tab.

- It opens on stored history. The history API reads by container name, so history spans recreates.
- A Live toggle appends the live tail below the history. Line identity removes duplicates at the boundary.
- Every viewer of a container shares one agent follow. Each viewer gets its own bounded boundary backfill.
- Live lines are masked on the host and again on the server. They are never stored.

**Inspect, compose and `.env` reads.**

- They are redacted on the host under widened rules (SEC-003).
- They are confined by an outer path bound, a deny list, and a component-by-component no-race open (SEC-005).

**Stack paths.**

- A stack's paths are trusted only when every member container agrees on them.
- A change to known paths needs operator acknowledgement before it takes effect.
- The agent never executes compose files that a container can write (SEC-002).

**Activity and audit.**

- Activity reads `commands`, with the actor's auth method and stable subject, the step timeline, and per-target results.
- Activity also reads `events`, with a validated `command_id`.
- Audit uses one store with per-kind retention that survives device deletion.

**Inventory and prune.**

- Inventory is opt-in, stored latest-only, and bounded by the server.
- Prune uses a single-use 128-bit preview. It removes only listed items, and only after checking each item's identity.

---

## 2. Prerequisites and merge gate

### 2.1 Gate for P1..P4: WP-0, rebase and reconcile

| Prerequisite | State | What this ADD consumes |
|---|---|---|
| UI-FOUNDATION-001 | **Merged to `main`** (PR #6) | Tokens; `components/ui/*` (Button, Card, Badge + `labels.ts`, Modal, ConfirmDialog, EmptyState, Skeleton, QueryState/ErrorPanel, Toast); `utils/format.ts`; `<RelativeTime>`/`<Timestamp>`; `TimePreferenceProvider`; `api/errors.ts`; typed demo parity; `test/renderWithProviders.tsx`; the static-guard test |
| CMD-LIFECYCLE-001 | **Merged to `main`** (PR #9, 2026-10-19). Its migration on `main` is **`000023_command_lifecycle`** | `commands.Service` (`Dispatch(ctx, DispatchRequest)`, `DispatchBulkDocker`, `HandleAgentReport`, `AllowReport`); `validate.go` (`sanitizeReport`, `validateReportIDs`); `targets.go` (`ResolveTargetKeys`); `models/command_state.go`; `models/command_policy.go` (`TargetKeyCompose`, `TargetKeyContainer`, `TargetKeyContainerID`, `TargetKeyForContainer`, `IsDockerTargetKey`, `HostTargetKeys`, `NormalizeTargetKeys`, `MaxAgentWSMessageBytes`); the agent registry (AD-009); the serialized agent writer (AD-015); `target_lock.go` (`TryLockAll`, `HeldWithPrefix`); `command_lifecycle.go`; `docker_deps.go`; `composeArgs`; `useCommandTracker`; `commandLifecycle.ts`; the `command_update` message |

**WP-0 has no merge to wait for.** It is the first task of P1. It runs once
and has four steps:

1. Rebase this story's branch onto current `main`.
2. Reconcile every DOCKER-UX contract that touches CMD-LIFECYCLE against the
   **as-merged** code, starting from the drift table in §2.5. Record the
   result in the implementation report.
3. Record the migration numbers present on `main` at rebase time (§2.4).
4. Stop and ask the architect if the reconciliation changes a contract in §8.

### 2.2 P0 is not gated (BR-001, NFR-001)

- **WP-P0A and WP-P0B can start now.** They need neither PART-001 nor WP-0.
  - WP-P0A adds no migration.
  - WP-P0B adds one metadata-only migration, `docker_ux_p0_log_agent_ts` (§7.2, AD-035 §1a). It is numbered per §2.4 rule 6.
- **WP-P0C needs PART-001 merged first, whole, and renumbered** above
  `000023_command_lifecycle`. D-35 (a) and D-37 were both settled in
  revision 3.
  - Before the merge is deployed to any database, the §2.4.1 runbook is run for that database and its classification recorded (D-43, settled).
  - The production deployment is already classified (§2.4.2). It needs a verified backup before the PART-001 deploy, and no `force`.
- **Order inside P0 (SEC-021, SEC-023, SEC-028).** WP-P0B carries the ingest
  timestamp window, the per-device ceiling and write-time masking.
  - WP-P0B ships no later than WP-P0A's capture of stopped and TTY containers.
  - WP-P0B ships before WP-P0C routes `container_logs` to day partitions.
  - No release contains WP-P0A or WP-P0C without WP-P0B.
- **Until WP-P0C ships**, the existing 7-day `ContainerLogRepo.Purge` at
  `server.go:490` stays unchanged, and no new row-DELETE path is added
  (NFR-024).

### 2.3 Conditional prerequisites

| Prerequisite | Needed by | If not merged |
|---|---|---|
| **PART-001** (local branch `part-001-partitioning`, unmerged) | WP-P0C (`container_logs` rotation); WP-2D (FR-107 I/O history) | WP-P0C waits, and the 7-day DELETE purge stays. The P1 history API reads by the container's current ID instead of its name (AD-036 fallback). WP-2D is parked. Everything else proceeds. |
| **AGENT-PRIV** | nothing functional | No capability maps to it (§12.9). |

### 2.4 Migration numbering and sequencing (D-35 and P0; risk R-3)

**Facts at the time of this revision.**

| Where | Migrations |
|---|---|
| `main` | `000001`..`000022`, then **`000023_command_lifecycle`** (CMD-LIFECYCLE-001, merged) |
| PART-001 branch | `000023_autovacuum_tuning` and `000024`..`000028` (the conversion). **These collide with `main`.** |
| `fix/pg-autovacuum-bloat` branch | `000023` |

**Rules.**

1. **PART-001 renumbers when it lands.**
   - Its files move to the next free block on `main` at merge time: `000024`..`000029`, if nothing else has landed first.
   - Their internal order stays the same: tuning, bookkeeping, PK index, validate, swap, validate FK.
   - The PART-001 ADD's file names change accordingly. Its logic does not.
   - This is recorded against P0 and D-35.
2. **Classify every deployed database before an upgrade that adds or moves
   migrations.** That means the CMD-LIFECYCLE release, the PART-001 merge,
   or any later DOCKER-UX migration on a database that ever ran a branch
   build.
   - **Why version 23 alone is ambiguous.** golang-migrate records only the highest applied version and a dirty flag. So version 23 could be main's `000023_command_lifecycle`, PART-001's branch `000023_autovacuum_tuning`, or the `000023` from `fix/pg-autovacuum-bloat` (SEC-022).
   - **Classify by schema fingerprint**, never by the version number alone, following the §2.4.1 runbook.
   - **No `migrate force` without sign-off.** It never runs on a deployed database without that database's operator sign-off (D-43, settled) and a verified backup.
   - **Record the result.** The implementation report records, per database: the fingerprint, the classification, the action taken, the version and dirty flag afterwards, and the backup evidence. This is R-3.
3. **DOCKER-UX migrations are never hard-coded.** Each is
   `<NNNNNN>_docker_ux_<phase>_<purpose>`, numbered next-free on `main` when
   its work package is implemented.
4. **WP-P0C's migration (`docker_ux_p0_log_name_index`) is numbered after
   PART-001's renumbered conversion files.** It indexes the partitioned
   parent table.
5. **The P1 migration (`docker_ux_p1_bulk_results`) may land before or after
   PART-001.** Whichever lands second takes the next free numbers. A
   DOCKER-UX file never takes a number that PART-001 has reserved in an open
   pull request.

6. **WP-P0B's `docker_ux_p0_log_agent_ts` may land before or after
   PART-001.**
   - PART-001's swap builds the partitioned parent with `LIKE container_logs_legacy`, so a column added earlier is carried over.
   - PART-001's conversion PG tests run with the column present.
   - If PART-001 lands first, the column is added to the partitioned parent and propagates to every partition. The column is nullable with no default, so this is metadata-only.

#### 2.4.1 Reconciliation runbook (SEC-022; D-43 settled)

**Scope.** Every deployed database, before deploying a release that adds or
renumbers migrations. Production is already classified (§2.4.2).

Steps 1 to 4 are read-only. Step 4's action is taken only with sign-off.

**Ground rules.**

- **Per-database sign-off (D-43).** Two things need the explicit sign-off of that database's operator: any `migrate force`, and any schema change outside a normal server start. The sign-off is recorded together with the fingerprint and backup evidence below.
- **Never drop data.** The runbook never drops, truncates or deletes from any table, and never drops a partition.
  - The only write it performs is `migrate force <version>` on the `schema_migrations` row.
  - Leftover reloptions, indexes and empty bookkeeping rows from branch builds stay in place. The renumbered files re-apply them idempotently.
- **Stop on dirty.** If `schema_migrations.dirty` is true, the database is neither forced nor upgraded by this runbook. It goes to the owners of the migration that failed (PART-001 or CMD-LIFECYCLE). Their own ADDs define the recovery.
- **Stop on anything unexpected.** Any fingerprint combination that is not in the step 4 table stops the procedure.
- **Server stopped from step 1 to step 5 (rev 3.3, SEC-030).** The server is stopped before the backup and is not started again until step 5 succeeds.
  - The upgrade itself runs through `riot-server migrate` (step 4b), which does not serve.
  - Agents buffer telemetry in the meantime (B-23). If step 5's restore is ever needed, no row written after the backup is lost.
- **Credentials (rev 3.3, SEC-030).**
  - The database password comes from a `PGPASSFILE` (mode 0600) or from the environment of a non-interactive shell. It is never placed on a command line or in a connection URI, so it never reaches shell history or the process list.
  - Records contain no connection string, host, port, user, database name or password.
- **Read-only role for steps 2 and 3 (recommended).** Run steps 2 and 3, and `db-fingerprint`, as a read-only role: for example one with `CONNECT` and `pg_read_all_data` only (PostgreSQL 14 and later).
  - Creating that role is a change outside a normal server start, so it needs the same sign-off.
  - Without the role, the subcommand's read-only session (step 3) still prevents writes.

**Step 1: stop the server, then take a verified backup (amended in rev 3.3, SEC-030).**

1. **Stop the server.** In the server's compose directory, run `docker compose stop riot-server`. It stays stopped until step 5 succeeds.
2. **Supply the password** through `PGPASSFILE` or the environment (see the ground rules). Never put it on the command line.
3. **Dump.** From a directory outside any repository or worktree, with `umask 077`, run:
   `pg_dump --format=custom --host=<db-host> --port=<port> --username=<user> --file=<backup-dir>/<db>-pre-upgrade-<UTC date>.dump <dbname>`
   Then confirm that the file's mode is 0600.
4. **The dump is a credential.**
   - It contains `admin_config`, including the stored session-signing secret, as well as device API-key hashes, notification-channel settings and every stored log. Anyone holding it can forge admin sessions.
   - Keep it out of every repository, ticket and chat.
5. **Verify the dump.** `pg_restore --list <file>` must succeed and list the tables `commands`, `container_logs`, `telemetry_snapshots` and `schema_migrations`.
6. **Recommended: test the restore.** Restore into a scratch database (`createdb <scratch> && pg_restore --dbname=<scratch> <file>`), compare the row counts of those four tables with the source, and then drop the scratch database: it holds the same secrets.
7. **Record.** No later step runs without a verified dump. Record the file name (without its directory), its size and the verification result.
8. **After step 5 succeeds,** delete the dump, or move it into the operator's backup store, which applies the same credential handling.

**Step 2: read the version.**

`SELECT version, dirty FROM schema_migrations;`

If `dirty` is true: stop.

**Step 3: fingerprint the schema.**

These are read-only queries. The read-only subcommand
`riot-server db-fingerprint` (WP-P0C) also prints them. Its read-only
property is enforced by construction (rev 3.3, SEC-030):

- **Dispatch first.** `main` dispatches the subcommand before `server.New`, and before any code path that can reach `RunMigrations`.
  - It never instantiates golang-migrate, so `schema_migrations` is never created and no advisory lock is taken.
  - It reads `schema_migrations` and the catalog with plain SQL.
- **Read-only session.** It connects with `default_transaction_read_only=on`, and runs everything inside one `READ ONLY` transaction. Any accidental write fails.
- **No identifying output.** It prints only these: version, dirty, the F-LC, F-AV and F-PT booleans, the classification and the action class.
  - It never prints the connection string, password, host, port, user or database name.
  - A connection error is reduced to a fixed message plus an error class (`auth`, `unreachable`, `timeout` or `other`).
  - It exits non-zero for every "stop" class.
- **Password.** It comes from `PGPASSFILE` or the environment, as in step 1.

| Fingerprint | True when |
|---|---|
| F-LC (main's 23) | `information_schema.columns` has `commands.deadline_at` **and** `pg_constraint` has `commands_origin_check` |
| F-AV (autovacuum branch) | the `pg_class.reloptions` of `container_logs`, or of `container_logs_legacy`, contain `autovacuum_vacuum_scale_factor` |
| F-PT (PART-001 conversion) | table `partition_conversion` exists, **or** `pg_class.relkind` of `container_logs` is `'p'` |

**Step 4: classify and act.**

| Version | Fingerprints | Meaning | Action |
|---|---|---|---|
| 22 | none | Clean `main` before 23 | None. A normal upgrade applies `000023_command_lifecycle` and the later files |
| 23 | F-LC only | `main`'s 23 is applied (the production case) | None. **Never force below 23.** A normal upgrade applies the renumbered PART-001 files and the later files |
| 23 | F-LC + F-AV, no F-PT (rev 3.3) | `main`'s 23, with the autovacuum reloptions applied by hand | None. **No force.** The renumbered tuning file re-applies the same reloptions idempotently |
| higher than 23, using `main` numbering | F-LC, or F-LC + F-AV | Normal `main` history, before the renumbered PART-001 runs | None |
| higher than 23, using `main` numbering | F-LC + F-PT + F-AV, or F-LC + F-PT (rev 3.3) | Normal `main` history, after the renumbered PART-001 tuning and conversion files ran | None |
| 23 | F-AV only, no F-LC | The branch autovacuum file sits at 23; `000023_command_lifecycle` never ran | With sign-off: `migrate force 22`, then a normal upgrade. The renumbered tuning file re-applies the same reloptions idempotently |
| 24..28, using branch numbering | F-PT (F-AV may also be present), no F-LC | The branch PART-001 conversion ran; `000023_command_lifecycle` never ran | With sign-off: `migrate force 22`, then a normal upgrade. `000023_command_lifecycle` touches only `commands` and `auto_update_policies`. The renumbered PART-001 files must do nothing on the already-converted tables (tested below) |
| any | Any other combination, including F-LC at a branch number, or F-PT at 23 | Mixed or manual state | **Stop.** Escalate. No force |

**Step 4b: upgrade without serving (rev 3.3, SEC-030).**

With the new image, run `riot-server migrate` (WP-P0C).

- `main` dispatches it the same way as `db-fingerprint`, before `server.New`.
- It applies the pending migrations with golang-migrate, prints the same secret-free summary, and exits without starting the HTTP server.
- The server itself is not started yet.

**Step 5: after the upgrade.**

1. Run steps 2 and 3 again.
2. Expect: not dirty, F-LC true, and F-PT true once PART-001 is deployed. F-AV may be true.
3. Record the result.
4. **On any difference, stop.** The server stays stopped. Restore from step 1 only with the operator's sign-off. Because the server has not served since the backup, no row is lost.
5. **Otherwise,** start the server (`docker compose up -d riot-server`), and then delete the dump or move it to the backup store (step 1 item 8).

**Tests (C-22).**

- `TestDUX_SEC022_FingerprintClassification`: PG fixtures for each row of the step 4 table. It asserts the class returned by `db.ClassifyMigrationState`.
- `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB`:
  1. Apply the branch-numbered PART-001 files (test fixtures) to a fresh schema with seeded rows.
  2. Force 22.
  3. Run `main`'s migrations, including the renumbered PART-001 files.
  4. Assert that the run succeeds, nothing is dirty, every seeded row is still present with the same checksum, and no DDL changed the converted tables.
- `TestDUX_SEC022_DirtyDatabaseRefused`: `db-fingerprint` reports "stop" for a dirty database.
- `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB` also asserts that the `partition_conversion` row is unchanged (rev 3.3, SEC-030 item 6).
- `TestDUX_SEC030_FingerprintCreatesNothing`: against a schema with no `schema_migrations`, no table is created and no advisory lock is taken.
- `TestDUX_SEC030_FingerprintReadOnlySession`: a write injected into the subcommand's session fails with a read-only error.
- `TestDUX_SEC030_FingerprintOutputHasNoDSN`: with a bad password, and with an unreachable host, stdout and stderr contain no host, port, user, database name or password, and the exit code is non-zero.
- `TestDUX_SEC030_MigrateSubcommandDoesNotServe`: `riot-server migrate` applies migrations and exits without binding a port.

#### 2.4.2 Production deployment (recorded fact)

- **What ran.** The production deployment's schema moved from version 22 to 23, not dirty, on the v2.67.0 release, which shipped CMD-LIFECYCLE-001. So `000023_command_lifecycle` ran there. Its classification is "23, F-LC only".
- **The skipped-migration risk is resolved for that deployment.**
- **Still required before PART-001 is deployed there:** step 1 (verified backup) and step 3 (fingerprint, expected unchanged). No `force` is needed.
- **Other operators' databases** are covered by §2.4.1.

### 2.5 Drift between the CMD-LIFECYCLE ADD copy and the merged code

Spot-checked against the main checkout. That checkout contains
`000023_command_lifecycle`, so it reflects the merge. WP-0 re-confirms these
items against `origin/main`.

| Item | ADD copy (rev 1 basis) | Merged code | Effect on DOCKER-UX |
|---|---|---|---|
| `commands` columns (§5.1) | `expires_at`, `exec_deadline_s`, `deadline_at`, the per-state timestamps, `step`, `progress`, `failure_step`, `stderr_tail`, `origin`, `policy_id`, `target_keys`, `pre_lifecycle`, `send_ambiguous` | Identical. Adds explicit CHECKs `commands_origin_check` and `commands_progress_check` | None. The DOCKER-UX P1 and P2 columns are additive |
| Agent registry (AD-009) | `register`, `unregister` (compare-and-delete), `get` | `register`, **`replace`**, `unregister`, `get`, `connected`. `terminalBrowserConns` holds `{conn, deviceID}` | AD-016's unregister hook attaches to `unregister`. It must also run when `replace` evicts a connection |
| Agent writer (AD-015) | `connMu`, `writeMu`, `errNotConnected`, 10 s write deadline | Identical | None |
| Report kinds | `ack`, `progress`, `result`; WS `command_ack`, `command_progress`, `command_result`, `command_receipt` | Identical (`models.ReportKind*`) | None |
| WS read limit | 4 MiB | `models.MaxAgentWSMessageBytes = 4 << 20` | None. AD-016 per-frame bounds sit below it |
| Dispatch API | `Service.Dispatch(…)` | `Dispatch(ctx, DispatchRequest{DeviceID, Action, Params, Origin, PolicyID, WindowEnd, TargetKeys})`; `DispatchBulkDocker(ctx, deviceID, ids)` | AD-007 adds `ProbeKeys`, `DockerExclusive` and `IsDocker` as **fields of `DispatchRequest`**, not a separate parameter. AD-022 adds `Initiator` the same way |
| `CreateIfNoConflict` | Under the per-device advisory lock | `CreateIfNoConflict(ctx, cmd)`. It takes the advisory lock **only when `len(keys) > 0`** | AD-007 must take the lock whenever the request has stored keys, probe keys or `IsDocker`. Otherwise a key-less Docker command could race a prune. The signature gains the conflict spec |
| Target key helpers | Generic names | `TargetKeyCompose`, `TargetKeyContainer`, `TargetKeyContainerID`, `TargetKeyForContainer`, `IsDockerTargetKey`, `HostTargetKeys`, `NormalizeTargetKeys`; prefix `TargetKeyDockerPrefix = "docker:"` | The new `TargetKeyDockerHost()` returns `docker:host`. `IsDockerTargetKey("docker:host")` is true, which correctly makes `agent_update` refuse during a prune (CMD-LIFECYCLE C-4(c)) |
| `ResolveTargetKeys` | Keys only for update, bulk update and host actions | Identical: `docker_start`, `docker_stop` and `docker_restart` resolve to no keys | AD-007's extension is required for FR-042 and FR-051 |
| `composeArgs` | `composeArgs(wd, sanitizedFile, sub, services...)` | Identical signature (`docker_update.go:290`): one `-f` file, no project flag | AD-027 adds `composeStackArgs(project, wd, files []string, sub, services...)` beside it, reusing its validation and the `--` separator. `composeArgs` itself stays as it is |
| `TelemetryFutureTolerance` | 2 m | **5 m** | None for DOCKER-UX. Noted for completeness |
| `ContainerInfo` freshness fields | `image_id`, `update_status`, `update_reason` | Present | AD-004 reuses `image_id` and does not add it again |

---

## 3. Technical Context

Line numbers refer to `main@a7b9f9e`, except where a CMD-LIFECYCLE file is
named (§2.5).

**Agent**

- **`collectors/docker.go`.**
  - It inspects every container every cycle. FR-062's values and the I/O counters therefore cost no extra API calls.
  - It copies labels verbatim.
- **`collectors/docker_logs.go` (`container_logs`).**
  - It reads running containers only.
  - It uses `Tail=50` on the first collection, then `Since=lastSeen`, with **no per-collection bound**.
  - It parses multiplexed output only, so TTY containers are mis-parsed (FR-155).
  - It breaks on frames larger than 1 MiB.
  - It drops `lastSeen` for stopped containers (FR-154).
- **`docker_events.go`** forwards container events without command attribution.
- **`terminal.go`** opens a host shell when the container ID is `_host`.
- **`buffer.go`** buffers telemetry while the server is unavailable.
- **From CMD-LIFECYCLE (merged):** the serialized writer, `target_lock.go`, `command_lifecycle.go`, `docker_deps.go`, and `composeArgs`.

**Server**

- **`handlers/terminal.go`**: the device-scoped agent registry (merged).
- **`handlers.go`**:
  - `Telemetry` extracts `Logs` and `ContainerLogs` before `StoreSnapshot`.
  - `stripHeavyTelemetry` broadcasts labels.
  - `GetDeviceHistory` returns full snapshots with no upper limit.
  - `DeviceSummary` renders labels through `summary.Render`.
  - `hasRiotServerContainer` feeds the global `serverHostID`.
  - `extractPrimaryIP` does not parse the IP.
- **`db/container_log_repo.go`**:
  - It caps each push at 20,000 rows, with no per-container ceiling.
  - It lists rows by `container_id`.
  - The table has indexes `(device_id, container_id, timestamp DESC)` and `(timestamp)`.
  - The `stream` column has no CHECK constraint.
- **`server.go:490`**: a 7-day container-log DELETE purge.
- **`config.go`**: `RIOT_RETENTION_DAYS` (default 30), which container logs do not use.
- **Request security**:
  - The session cookie is `SameSite=Lax`.
  - No POST checks `Content-Type` or `Origin`.
  - No response sets anti-framing headers.
  - `terminal_sessions.device_id` is `ON DELETE CASCADE`.
- **`auto_update.go`**: stack auto-update uses the first container's `working_dir` (SEC-002, pre-existing).

**PART-001 (unmerged)**

- It converts six tables, including `container_logs`, to `PARTITION BY RANGE (timestamp)`.
- Partitions are daily (`<t>_pYYYYMMDD`), plus a bounded legacy partition and a forward DEFAULT partition.
- A partition is dropped once its upper bound is at or before `now − window`.
- The `container_logs` window is a constant 7 days (PART-001 AD-005).
- PART-001 AD-006 removes the six DELETE purges.

**Web**

- UI-FOUNDATION is on `main`, so its primitives exist.
- The current Logs tab polls stored logs every 15 s with fixed line limits. It shows "collector must be enabled" whenever no rows come back.

---

## 4. Phased delivery and work packages

### 4.1 Capability tokens (OQ-04; SEC-014)

Each agent advertises compiled-in tokens in `DockerInfo.capabilities`.

**Tokens are agent assertions.** They are used only to choose what the server
sends and what the UI offers. A token never relaxes a server bound, a
validation or masking step, never counts as authorization, and never affects
decisions about other devices.

When a token is missing:

- the UI shows "Requires agent update";
- the server refuses with `409 agent_unsupported`, before it contacts any agent.

| Token | Work package | Gates |
|---|---|---|
| `log_caps_v1` | WP-P0A | Agent caps and gap markers, `DockerInfo.container_logs_enabled`, collection from stopped and TTY containers. The server ceiling applies whether or not the token is present. |
| `docker_meta_v1` | WP-1A | FR-062 fields |
| `bulk_results_v1` | WP-1A | Per-target bulk results |
| `container_locks_v1` | WP-1A | Locks for start, stop and restart |
| `logs_live_v1` | WP-2A | Shared follow and backfill protocol (AD-015) |
| `inspect_v1` | WP-2A | Inspect |
| `cmd_steps_v1` | WP-2A | Step output |
| `event_attribution_v1` | WP-2A | `command_id` on Docker events |
| `stack_actions_v1` | WP-3A | Stack actions with path-trust checks |
| `compose_read_v1` | WP-3A | Compose and `.env` reads (Linux agents only, D-39) |
| `inventory_v1` | WP-4A | Inventory collector present |
| `prune_v1` | WP-4A | `docker_remove_items` |

- **The server never compares agent version strings.** Agents built from a branch, or from a version-skewed release, would break version-boundary logic.
- **Minimum version per capability.** It is the first tagged release that contains its work package. The release notes name it. The server learns it **only** through the token.
- **Refresh.** Tokens refresh with every telemetry push, so an upgraded agent is recognised within one telemetry interval.
- **No `docker` collector.** If an agent has the `docker` collector disabled, it advertises nothing, and the host is not a Docker host (FR-004).

### 4.2 Work packages

- Within a phase, packages A, B and C run **in parallel**, against the frozen §8 contracts.
- P0 runs now, alongside P1.
- P1 to P4 run in order, each released separately.
- WP-2D is gated on PART-001.

| WP | Scope | Parallel with | Requires | Migration |
|---|---|---|---|---|
| **WP-P0A** agent | FR-152, FR-154, FR-155: caps, gap field, bounded scan, stopped and TTY collection, `container_logs_enabled`, `log_caps_v1`, cap config, README | WP-P0B, WP-P0C, WP-0, P1 | none (**can start now**) | none |
| **WP-P0B** server | FR-153 ceiling with server-owned `stream`, gap-entry validation, per-container and per-device rolling limits and the unknown-container budget (SEC-023). The ingest timestamp window with re-stamping, the skew counters and the `clock_skew` event (D-42, SEC-021). Write-time `MaskLine`, with the masking subset of `internal/redact` moved here (SEC-028). The FR-149 setting parse with its startup warning. The effective-retention accessor, which stays 7 until WP-P0C | WP-P0A | none (**can start now**) | `docker_ux_p0_log_agent_ts` |
| **WP-P0C** server | FR-148, FR-150, FR-151, FR-156, NFR-025: merge PART-001 whole and renumbered (§2.4; D-35 and D-37 settled); drive the `container_logs` window from the setting; name index; DEFAULT-partition event (SEC-021 item 4); NFR-022 retention log; the read-only `riot-server db-fingerprint` subcommand and the SEC-022 tests; rotation tests | WP-P0A, P1 | **PART-001 merged**, **WP-P0B shipped**, and the §2.4.1 runbook result recorded for every database it will be deployed to (production: §2.4.2) | `docker_ux_p0_log_name_index` |
| **WP-0** gate | §2.1 rebase and reconcile, §2.5 drift | none | none: both prerequisites are on `main` | none |
| **WP-1A** agent | FR-062/063 fields, rates, limit. Label redaction with the SEC-003 label rules, extending the WP-P0B `internal/redact` package. Tokens; `riot.role`; container locks; bulk `target_results`. The agent writable-path refusal for `docker_update{compose_work_dir}`, including bind-backed volumes, with `docker.compose_trusted_roots` and a doctor listing of the stacks affected (SEC-024, D-44) | WP-1B, WP-1C | WP-0 | none |
| **WP-1B** server | AD-034 origin control and framing. `internal/redact` label functions (labels, URL credentials, keys, masking net). FR-064 row; `/docker/hosts` (including the skew and ingest counters); bulk endpoint; probe keys; typed confirmations; self targets per host. Output-path redaction including `/history` and `/summary`. History API (FR-157..FR-161), with masking at read and the future bound. Auto-update path agreement plus the compose-created-member rule (SEC-002 item 4, SEC-024 item 2). `primary_ip` parse | WP-1A, WP-1C | WP-0 | `docker_ux_p1_bulk_results` |
| **WP-1C** web | FR-001..FR-073 and FR-157..FR-161: section, redirects, table, bulk, header, tabs, palette, one Logs tab (history half), new `ui` primitives, link validators | WP-1A, WP-1B | WP-0 | none |
| **WP-2A** agent | Shared follow and backfill streamer: bounded reader, drop before mask, orphan reaping and `logs_keep` (SEC-025), a separate backfill byte share (SEC-026), frames never persisted (V-1). Inspect with widened rules; steps; event attribution; log hygiene | WP-2B, WP-2C | P1 released | none |
| **WP-2B** server | `livelogs.Hub` fan-out with server bounds, masking and live re-stamping (D-42). Stream-lifecycle serialization, unknown-stream unfollow and `logs_keep` (SEC-025). Backfill rate limit, coalescing and boundary validation (SEC-026). Socket expiry (SEC-027). Agent RPC with the SEC-001 header; identity claims and stable subject; Activity API. Audit store (180/90-day retention, no cascade; a `command` audit row for every operator command; down migration without data loss: SEC-029). Write-time masking of command text; `command_id` validation | WP-2A, WP-2C | P1 released | `docker_ux_p2_activity` |
| **WP-2C** web | Live toggle in the one Logs tab (FR-074..FR-085, FR-162..FR-165), Inspect, Activity, Stats I/O | WP-2A, WP-2B | P1 released | none |
| **WP-2D** server and web | FR-107 I/O history | after WP-2B | PART-001 merged, D-7 confirmed | `docker_ux_io_history` |
| **WP-3A** agent | Stack actions with path trust. The writable-path refusal and `rwmounts.go` arrive in WP-1A; WP-3A applies them to every stack action. Compose and `.env` reads (outer bound, deny list, component-wise open, dotenv, compose rule 1); stack registry; `composeStackArgs` | WP-3B, WP-3C | P2 released | none |
| **WP-3B** server | `docker_stacks` (agreed and observed paths, acknowledgement, row bounds); stacks API; dispatch with agreed paths; compose relay | WP-3A, WP-3C | P2 released | `docker_ux_p3_stacks` |
| **WP-3C** web | FR-053, FR-108..FR-127, AC-106; path display and acknowledgement UI | WP-3A, WP-3B | P2 released | none |
| **WP-4A** agent | inventory collector, `docker_remove_items` with identity checks, refresh, doctor, README | WP-4B, WP-4C | P3 released | none |
| **WP-4B** server | inventory with server bounds and cadence; preview and execute with 128-bit single-use IDs | WP-4A, WP-4C | P3 released | `docker_ux_p4_inventory` |
| **WP-4C** web | FR-128..FR-147 | WP-4A, WP-4B | P3 released | none |

Integration happens only through the frozen contracts in §8. WP-nC codes
against §8 with mocked API and WS, and does not wait for WP-nA or WP-nB.

Note on dependencies:

- P3 depends on P2 only for merged stack Live (FR-121) and stack Activity (FR-122).
- P4 depends on P2 Activity for prune history.
- If the product owner wants P3 before P2, FR-121's Live half and FR-122 move to a P3.1 follow-up. Nothing else in P3 needs P2.

Suggested child story IDs are `DOCKER-UX-001-P0` .. `-P4`. The commit prefix
is `[DOCKER-UX-001]`.

### 4.3 Compatibility (NFR-016)

**New server, older agent.**

| Phase | Behaviour |
|---|---|
| P0 | The server ceiling and gap rows apply to every agent. Old collectors still use unbounded `Since=lastSeen`, which the ceiling now bounds. Collector state is "unknown", so it is inferred (AD-036). |
| P1 | Rows render with fallbacks. Bulk Update shows the host command's outcome. History works. |
| P2 | Live is disabled with "Live logs require an agent update", and history still renders. Inspect needs an agent update. No step output is shown. |
| P3 | Stack actions other than Update are disabled. Compose needs an agent update. Update is refused when the members disagree on paths. |
| P4 | Inventory needs an agent update. Prune is disabled. |

**New agent, older server.**

- Unknown fields are ignored, including the gap field.
- In P4, an older server stores `docker_inventory` inside the snapshot. The release notes therefore say to upgrade the server first.

**Agent configuration.**

- P0 to P3 need no change. The caps default to 50 lines and 16 KiB, and can optionally be tuned.
- P4 needs one opt-in collector line.

**Release order:** upgrade the server first, or together with the agents. For
P0, the §2.4 database check comes before the upgrade.

---

## 5. Architecture Decisions

### AD-001: Phase and work-package structure with capability tokens

**Decision:** the phases and work packages in §4.

- **Where the tokens live.** They are typed constants in `internal/models/docker_caps.go`, carried in `DockerInfo.Capabilities` (telemetry, `omitempty`). WP-P0A creates the file. Later work packages may only append to it.
- **`AllDockerCapabilities`.** The list for the current build.
- **`HasDockerCap(info *DockerInfo, c DockerCap) bool`.** It is nil-safe.
- **`requireDockerCap(w, deviceID, cap)`.** Server handlers call it. It reads the latest snapshot's `docker.capabilities`, and only short-circuits requests the agent cannot serve.
- **Web.** The web reads the same list from `GET /api/v1/docker/hosts`.
- **No relaxation (SEC-014).** Every server bound, validation and masking step runs regardless of tokens. `TestDUX_SEC014_ServerBoundsIndependentOfCapabilities` sends oversized, unmasked and malformed data from an agent that advertises every token, and asserts that the server bounds, masks or rejects it.

**Rationale:** explicit, testable, and immune to version-string formats.
Telemetry already carries per-host Docker state.

**Alternatives considered:**

- semver comparison against `devices.agent_version` (rejected: dev builds, forks, and unparseable strings);
- a WS "hello" handshake (rejected: HTTP-only agents and offline hosts would have no record).

**Consequences:** a capability is visible only after the first telemetry
push of an upgraded agent. Tokens can only be appended, never removed or
renamed.

### AD-002: Resource identity, URL schema and server-side identifier validation

**Decision.**

**Identity:**

| Resource | Identity | Canonical URL |
|---|---|---|
| Container | `(device_id, container name)` (BR-002) | `/docker/containers/<device-id>/<container-name>` |
| Stack | `(device_id, compose project)` (BR-003) | `/docker/stacks/<device-id>/<project>` |
| Image | `(device_id, image ID)` | row in `/docker/images` (no page) |
| Volume | `(device_id, volume name)` | row in `/docker/volumes` |
| Network | `(device_id, network ID)` | row in `/docker/networks` |

**Validation:**

| Value | Rule |
|---|---|
| Container name | `models.ContainerNamePattern = ^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`, Docker's own charset. Every server endpoint that takes a container name validates it |
| Compose project | `models.ComposeProjectPattern = ^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$` |
| Device ID | must parse as a UUID |

A value that fails validation returns `400 {"code":"invalid_identifier"}`,
before any DB or agent call. Malformed values are never logged; only their
length is.

**Client (SEC-017):**

- Path segments and query values go through `encodeURIComponent`.
- A name that fails the pattern is shown as plain text and is not linked.

**Rationale:** names survive recreate (FR-009). Both charsets are URL-safe.
Validation closes injection into the compose argv and file paths early
(AD-021, AD-027).

**Consequences:** two containers can never share a name on one host (Docker
guarantees it). A renamed container is a new resource.

### AD-003: Fleet container data, Docker hosts, and live merge

**Decision:**

1. **Extend** `GET /api/v1/fleet/containers` (FLEET-DASH) to the full FR-064
   row (§8.2). `GetFleetContainerLeaderboard` keeps the single JSONB
   projection. It already returns whole container objects, so the server
   decodes into a widened `FleetContainerProjectionRow`. The state filter
   adds `"removing"` (FR-011).
2. **Allowlist.** The handler copies exactly the §8.2 fields.
   - Labels contribute only `com.docker.compose.project`,
     `com.docker.compose.service`, and the derived `target_key`/`stack_key`
     (built by the CMD-LIFECYCLE target key helpers).
   - `riot.*` values come from the already parsed `Riot` struct.
   - `labels`, `env`, `mounts` and `networks` are never serialized (AD-012
     of FLEET-DASH, kept). The FLEET-DASH spy and allowlist tests are updated
     to the new allowlist.
3. **New** `GET /api/v1/docker/hosts`. One row per device, with:
   - `docker_available`, `capabilities`, `inventory_enabled`;
   - `status`, `last_seen`, `primary_ip`, `hostname`;
   - `self_role` (AD-009);
   - `container_logs: "enabled"|"disabled"|"unknown"`.

   It reads `devices` plus the latest-snapshot `docker` sub-tree through a
   second JSONB projection (`GetDockerHostProjection`). The full blob is never
   decoded.
4. **Live merge (web).** `useDockerFleet()` holds
   `['docker','containers']` and `['docker','hosts']`.
   - On each `telemetry` WS message, it projects that device's broadcast
     containers through the **same** projection function as the server
     (`projectFleetRow` in `utils/dockerFleet.ts`, mirrored by a parity test
     over shared fixture JSON).
   - It replaces that device's rows. Updates are coalesced: at most one cache
     write per 1 s (NFR-004 input-block budget).
   - On `docker_update` events it applies the CMD-LIFECYCLE suppression rule
     (`shouldSuppressExited`).
5. **`stripHeavyTelemetry`** adds every FR-062 field and `UpdateStatus`/
   `UpdateReason` (if CMD-LIFECYCLE did not already), and applies
   `redact.Labels` to the broadcast labels (AD-006).
6. **SEC-011.** `primary_ip` is parsed with `net.ParseIP` at ingest. An unparseable value is not stored, and the previous value is kept.
7. **SEC-013.** `self_role` is computed per host (AD-009).

**Rationale:** FR-064 says *extend*. One projection query serves both the
leaderboard and the table. The broadcast already reaches the same admin
browser, so the client-side projection adds no exposure.

**Alternatives considered:** a new `/docker/containers` endpoint (rejected:
two projections to keep in sync). Polling (rejected: NFR-004 and live rows
need WS).

**Consequences:** the response grows to about 600 B per container, about
1.2 MB for 2,000 containers. That is fetched once, then kept current by WS.

### AD-004: Additive collector fields, configured limit, agent-computed I/O rates

**Decision:** `ContainerInfo` gains these fields, all `omitempty`
(`models/telemetry.go`):

| JSON | Type | Source (zero extra API calls) |
|---|---|---|
| `started_at` | RFC3339 string | `inspect.State.StartedAt` (empty for never-started) |
| `finished_at` | RFC3339 string | `inspect.State.FinishedAt`, only when state is not running |
| `exit_code` | `*int` | `inspect.State.ExitCode`, only when not running |
| `mem_limit_configured` | int64 | `inspect.HostConfig.Memory` (0 = no limit) |
| `net_rx_bytes`, `net_tx_bytes`, `blk_read_bytes`, `blk_write_bytes` | `*uint64` | stats `networks[*]` summed; `blkio_stats.io_service_bytes_recursive` summed by op Read/Write (cgroup v1 and v2) |
| `net_rx_rate`, `net_tx_rate`, `blk_read_rate`, `blk_write_rate` | `*float64` B/s | agent: `(cur − prev)/(t_cur − t_prev)` |

`image_id` (`inspect.Image`) is already on `main` from CMD-LIFECYCLE (§2.5).
It is reused, not added again.

- **Rate computation.** It lives in the pure function
  `collectors/docker_iorate.go::ioRates(prev, cur sample, now) *rates`.
  - Rates are computed only when both samples have the same container ID,
    every counter is ≥ its previous value, and the interval is ≥ 1 s.
  - Otherwise the rate field is nil, so FR-103 "—" holds and a value is never
    negative.
  - The previous-sample map is pruned to live container IDs.
- **Running versus stopped.** The `*_bytes` counters are reported only for
  running containers.

**Rationale:** rates belong where the previous sample lives. The server and
the browser would otherwise need history reads. Each value is a single
counter read.

**Consequences:** about 150 B of JSON is added per running container per
snapshot (§13.3). The rate is the average over one collection interval
(A-08).

### AD-005: Shared redaction and masking package (revised for SEC-003)

**Decision.** One package, `internal/redact`, is used by both the agent and
the server.

- It has no imports beyond the standard library and `gopkg.in/yaml.v3`.
- Rule data lives in `rules.go` as exported, documented variables, so the security review can tighten it without code changes.
- One fixture, `internal/redact/testdata/cases.json`, is copied byte-for-byte to `web/src/utils/__fixtures__/redact-cases.json`. Go and TypeScript table tests both run it (C-3).

**Functions:**

| Function | Signature |
|---|---|
| Rule 1 | `EnvPair(key, val) (string, bool)` |
| Rule 2 | `SensitiveKey(key) bool` |
| Rule 3 | `URLCreds(s) (string, int)` |
| Rule 4 | `Argv([]string) ([]string, int)` |
| Labels map | `Labels(map) (map, int)` |
| Inspect document | `InspectJSON([]byte) ([]byte, int, error)` |
| Compose YAML/JSON | `ComposeFile([]byte) ([]byte, int, error)` |
| `.env` | `DotEnv([]byte) ([]EnvKey, int, error)` |
| Log masking | `MaskLine(string) (string, int)` |
| Masking net | `MaskText(s, marker)` |
| ANSI strip | `StripANSI(string) string` |

- **Return contract.** Every function returns its count. Every function that parses returns an error **instead of** partial content (NFR-022: fail closed).
- **Idempotence.** Masking is idempotent: `<masked>` and `<redacted>` match no pattern. Running it twice (agent, then server) is safe.

**Rule 1: environment values (`EnvPair`).** Every value is redacted unless
its key is on the FRD §8.6 safe list (adopted as-is, with no additions). It
applies to inspect `Config.Env` and to `.env`, and now also to compose
`environment:` (D-27 resolved; AD-021).

**Rule 2: sensitive key (`SensitiveKey`).**

The key is upper-cased and split into segments on `_ . - /`, and on camelCase
or acronym boundaries. For example, `clientKey` becomes `CLIENT|KEY`, and
`PRIVKey` becomes `PRIV|KEY`.

The key matches when any of these holds:

- (a) The key, with separators removed, contains any of: `PASSWORD`, `PASSWD`, `PASS`, `PWD`, `SECRET`, `TOKEN`, `APIKEY`, `ACCESSKEY`, `PRIVATE`, `CREDENTIAL`, `AUTH`, `COOKIE`, `SESSION`, `DSN`, `CONNECTIONSTRING`, `SALT`, `BEARER`, `JWT`, `WEBHOOK`, `SIGNING`, `SIGNATURE`, `ENCRYPT`, `HMAC`, `PRIVKEY`, `PASSPHRASE`, `PSK`, `LICENSE`, `CONNSTR`.
- (b) A segment equals `KEY`, `PW`, `OTP`, `SAS` or `SIG`.
- (c) A segment ends with `KEY`.

False positives such as `MONKEY` are accepted.

**Rule 3: URL credentials (`URLCreds`).** This is a scanner, not a regular
expression.

1. Find each `scheme://`. The token runs until whitespace, a quote, `<`, `>`, or the end of the string.
2. Within the token, take the **last** `@`.
3. If a `:` appears between `://` and that `@`, replace everything from the first such `:` up to that `@` with `:<redacted>`. This covers an empty user, `/`, `:`, `+`, `=`, `%` escapes, and a raw `@` in the password.
4. Redact the value of any query parameter whose name matches rule 2.

**Rule 4: command arguments (`Argv`).**

- Each argv element is also split on whitespace, with simple shell quoting.
- Matching `--flag=v` and `--flag v` are redacted.
- Attached short flags `-p<v>` and `-P<v>` are redacted.
- `-u` or `--user` followed by `x:y` has `y` redacted.
- The rule applies to inspect `Cmd`, `Entrypoint`, `Args` and `Healthcheck.Test`.
- It also applies to compose `command`, `entrypoint` and `healthcheck.test`, in both string and list form.

**Masking net (`MaskText(s, marker)`).** The FRD §8.7 patterns are applied
with `<masked>` to log lines, and with `<redacted>` to every label value and
every string scalar in inspect and compose documents. The patterns are:

- `Authorization` values;
- the JWT form;
- PEM blocks;
- rule-2 `key=value`, `key: value` and `"key":"value"` pairs;
- JSON embedded in a value.

**Fail closed.** Any parse error returns no content, with the reason
`redaction_failed`.

**Residual-risk notice.** The README and the Logs, Inspect and Compose tabs
state that obfuscated secrets stored under non-sensitive keys can remain.

**Where each function runs:**

| Function | Agent (primary) | Server (defence in depth) |
|---|---|---|
| `Labels` | docker collector | every output path (AD-006) |
| `MaskLine` | live lines, step output | container-log ingest (write time, AD-035), history read, live relay (always), command text at write (AD-023) |
| `InspectJSON`, `ComposeFile`, `DotEnv` | read handlers | none |
| `EnvPair`, `Argv` | inspect, compose and `.env` reads | none (never leaves the host unredacted) |
| `SensitiveKey`, `URLCreds` | every redaction path | label output |
| `StripANSI` | none | Activity text (the browser also strips at render) |

**OQ-06.** The FRD safe list is adopted with no additions. The FRD
sensitive-key list is adopted, widened by the security review (SEC-003) to
the rule 2 list above, which includes `BEARER`, `JWT` and `WEBHOOK`.

**Rationale:** one implementation, one test table (NFR-020), both binaries.
The agent's redaction is binding (NFR-010). The server's pass covers data
from older agents and stored history.

**Consequences:** the agent gains an import of `internal/redact` (in-repo,
not a new dependency). Masking costs CPU on the host, bounded by the
line-rate cap (§13.1).

### AD-006: Label redaction at the source and on every output path (revised for SEC-004)

**Agent.** `collectors/docker.go::containerFromAPI` computes
`ParseRiotLabels` and `InferRepoURL` from the raw labels first, then stores
`redact.Labels(c.Labels)`. `com.docker.compose.*` and `riot.*` pass through
unredacted. `redact.LabelPassThrough` lists them, and a test proves that no
rule matches them.

**Server.** `redactSnapshotForOutput(*models.TelemetrySnapshot)` redacts
labels and drops `env`. It is applied on every output path:

| Output path | Notes |
|---|---|
| `GET /api/v1/devices/{id}` | — |
| `GET /api/v1/devices/{id}/history` | `limit` is now capped at 100 |
| `GET /api/v1/devices/{id}/containers` | — |
| `GET /api/v1/devices/{id}/containers/{cid}` | — |
| `GET /api/v1/devices/{id}/summary` | the markdown summary; `summary.Render` receives the redacted snapshot |
| WS `telemetry` | — |
| `GET /api/v1/fleet/containers` | allowlist only |
| new Docker endpoints | — |

`TestDUX_SEC004_NoRouteReturnsRawLabel` (C-4) walks the router with
`chi.Walk` and calls every admin GET route over a fixture that contains
`hunter2-fixture` as a label value. It asserts that no response contains it.
A new route must be classified before the test passes.

**Stored data is not rewritten.** Rows already in `telemetry_snapshots`
from older agents keep raw labels until retention removes them. **D-26**
(no scrub of labels already stored) depends on this list being complete,
and the review agrees.

**Rationale:** FR-056 needs labels redacted on Overview. NFR-010 needs it
before data leaves the host. The server pass protects mixed fleets.

**Consequences:** Overview shows `<redacted>` for label values such as
`traefik.http.middlewares.auth.basicauth.users` (matches `AUTH`). That is
intended.

### AD-007: Target keys: stored keys, probe keys, host-wide Docker key (revised for §2.5)

**Problem.** CMD-LIFECYCLE gives keys only to updates and host actions.
FR-042, FR-051 and FR-113 need conflicts for start, stop and restart too.
FR-144 needs a prune that excludes every Docker command on the host, and the
reverse. One overlap test (`target_keys && $keys`) cannot express "a restart
of `web` conflicts with a stack Down of `web`'s stack, but two restarts in
the same stack do not conflict."

**Decision.** `commands.DispatchRequest` (as merged) gains three fields,
filled by `commands/targets.go::conflictSpecFor(action, params, snapshot)`:

| Field | Type | Meaning |
|---|---|---|
| `ProbeKeys` | `[]string` | Checked for conflicts, never stored |
| `DockerExclusive` | `bool` | Conflicts with any active Docker command |
| `IsDocker` | `bool` | Conflicts with an active `docker:host` holder |

`TargetKeys` stays as the stored keys. They keep CMD-LIFECYCLE's rule that a
command matches a target when `target_keys` includes the target's key. Probe
keys are checked for overlap but not stored. `CreateIfNoConflict(ctx, cmd, spec)`:

- **always takes the per-device advisory lock** when stored keys, probe keys or `IsDocker` are present. The merged code takes it only when `len(keys) > 0`.
- finds the oldest active row matching any of:
  - `target_keys && (stored ∪ probe)`;
  - `$exclusive AND EXISTS (SELECT 1 FROM unnest(target_keys) k WHERE k LIKE 'docker:%')`;
  - `$isDocker AND 'docker:host' = ANY(target_keys)`.

The query is built by a pure builder that uses only bound parameters.

**Keys per action:**

| Action | Stored keys | Probe keys |
|---|---|---|
| start, stop, restart | `TargetKeyContainer(name)` | `TargetKeyCompose(wd)` when the container belongs to a stack |
| single-service compose update | `TargetKeyCompose(wd)` + `TargetKeyContainer(name)` (adds the container key to CMD-LIFECYCLE's set) | none |
| standalone `docker_update` | `TargetKeyContainer(name)` (unchanged) | none |
| `docker_bulk_update` | union of each target's update keys | none |
| stack actions, stack Update (`docker_update` with `compose_work_dir`) | the compose key + every member's container key in the latest snapshot | none |
| prune or remove (`docker_remove_items`) | `TargetKeyDockerHost()` (new) | exclusive |
| `docker_inventory_refresh` | `TargetKeyDockerHost()` | none; not exclusive |
| every Docker action above (except refresh) | — | `IsDocker` |
| `docker_check_updates` | none (unchanged) | none |

The agent mirrors these locks (`container_locks_v1`):

- start, stop and restart call `TryLockAll` on the container key;
- each fails `precheck` (reason `conflict`) when `docker:host` or its compose key is held;
- `docker_remove_items` takes `docker:host` and fails when `HeldWithPrefix("docker:")` matches;
- every other Docker action fails `precheck` when `docker:host` is held.

**Rationale:** the stored keys keep the web matching rule (CMD-LIFECYCLE
§6.7) true and additive. The probe keys give asymmetric "member-of" checks
without storing spurious keys. The exclusive class is the FR-144 rule
expressed once, in SQL, under the lock that already serialises creation.

**Alternatives considered:**

- a `docker:any` key on every command (rejected: it makes all Docker commands conflict with each other);
- per-action SQL (rejected: scattered).

**Consequences:** bulk Restart of four containers in one stack creates four
commands with no mutual conflict. A Restart of `db` during an Update of `web`
in the same stack conflicts, which is conservative because of the
network-dependents hazard. A PG test and a mock test both cover the table
above.

### AD-008: Bulk actions

**Decision:** new endpoint `POST /api/v1/docker/bulk-actions` (§8.4), behind
AD-034.

1. The server validates at most 100 targets (FR-044). Each target's device ID
   and name are checked against AD-002, and against the latest snapshot.
   - A missing container gives `skipped/not_found`.
   - An inapplicable state gives `skipped/not_applicable` (FRD §8.5 table,
     implemented in `commands/applicability.go`).
2. **Typed confirmations** are checked server-side (AD-009).
3. **Start, Stop and Restart.** One `Dispatch` per target, with the AD-007
   `DispatchRequest` fields. A 409 becomes `conflict` with
   `existing_command_id`.
4. **Update.** Targets are grouped by host. One `docker_bulk_update` is
   dispatched per host, built on the merged `DispatchBulkDocker`, with
   `container_ids` and `containers` (names). The CMD-LIFECYCLE bulk conflict
   logic reports per-target conflicts and dispatches the remainder. Every
   target gets the host command's ID (`command_id`) in the response (D-19).
5. **Results.** Agents with `bulk_results_v1` add
   `target_results:[{container, outcome: succeeded|failed|skipped, message, step}]`
   (at most 100 entries, each message at most 1 KiB, sanitized, and masked at
   write time per AD-023) to the final `command_result`.
   - The server stores it in `commands.target_results` at the terminal
     transition (§7.2), alongside the existing result write, with no extra
     write.
   - `GET /devices/{id}/commands/{commandId}` and the `command_update` WS
     message include it when it is terminal (`target_results` absent
     otherwise).

**Progress panel (web, FR-043).** The panel tracks one `useCommandTracker`
per distinct command ID.

- A bulk-update target shows its host command's lifecycle label and step.
- At terminal, the target shows its own `target_results` outcome.
- For older agents, it shows the host command's outcome.

**Rationale:** see §0.2 item 3. This preserves CMD-LIFECYCLE's
children-first ordering and time budget for updates.

**Consequences:** FR-042 is met for Start, Stop and Restart. For Update it is
met per host, not per target (D-19).

### AD-009: Typed confirmations prevent mistakes; self-protection (revised for SEC-001 and SEC-013)

**Reclassification.** Server-checked typed confirmations (`confirm_count`,
`confirm_host`, `confirm`) are **anti-mistake controls only**: a forger can
supply them.

- The **anti-forgery control is AD-034**.
- NFR-012's "a URL alone must never start an action" is met by AD-034: same origin, JSON only, no framing.

**When a typed confirmation is required.** Typed values are checked on the
server, not only in the UI. Requests carry `confirm` fields:

| Request | Required when | Field | Must equal |
|---|---|---|---|
| bulk `stop` | more than 5 applicable targets, or more than 1 host (D-12) | `confirm_count` | number of applicable targets |
| single or bulk Stop, stack Down, Redeploy or Restart touching a self target | always | `confirm_host` | hostname of the self target's host |
| stack `down` | always | `confirm` | project name |
| `docker_remove_items`, volumes | always | `confirm` | hostname |
| `docker_remove_items`, images or networks | more than 20 items | `confirm` | item count as decimal |

A mismatch returns
`400 {"code":"confirmation_required","expected_kind":"count"|"host"|"name"}`.
The response never echoes the expected value.

**Self targets (OQ-01, revised for SEC-013).** `selfTargets(allLatest)` runs
**per host, from each host's own latest snapshot, across every host**. It
never uses the global `serverHostID`. On each host the self set is the union
of:

1. any container whose image (lower-cased) contains `riot-server`. This is the existing `hasRiotServerContainer` heuristic, made per container;
2. every container in the same compose project on that host as a container from (1). This covers the database container of the standard compose deployment;
3. any container labelled `riot.role=server` or `riot.role=database`. The value is parsed into the new `RiotLabels.Role`. This is the explicit operator marking.

The function lives in `handlers/docker_self.go`. Stacks are self stacks when
any member is a self target. Fleet rows and `/docker/hosts` expose
`self_role` (`server` | `database` | `""`).

A spoofing agent can only add self targets on its own host
(`TestDUX_SEC013_SpoofedServerHostDoesNotRemoveSelfTargets`). `serverHostID`
keeps its existing roles.

**Limits, stated as FR-147 requires.** When the server runs outside Docker,
or its database is outside the server's compose project, only labelled
containers are protected. The README documents the label. AC-098 tests (1)
and (2).

**Rationale:** the UI cannot be the only gate for destructive actions.
Identification reuses proven heuristics and adds an explicit override.

**Consequences:** an operator who renames the image and does not label it
gets no warning (D-25).

### AD-010: Action rules as pure functions, mirrored on the server

**Decision:**

- **Web.** `web/src/utils/dockerActions.ts` exports:
  - `containerPrimaryAction(c, activeCmd)`;
  - `containerSecondaryActions(c, flags)`;
  - `containerMenu(c, flags)`;
  - `bulkApplicability(action, c, activeCmd)`;
  - `stackPrimaryAction(s, activeCmd)`;
  - `stackSecondaryActions(...)`.

  Each one encodes FRD §8.5 exactly, first match wins, and is covered by
  table-driven tests.
- **Server.** `internal/server/commands/applicability.go` mirrors only
  `bulkApplicability` (state-based). The server never emphasises an action,
  so it does not need the rest.

**Consequences:** AC-033, AC-026 and AC-087 test the web tables. A Go table
test asserts the server applicability matches the same fixture JSON (shared
file `internal/server/commands/testdata/applicability.json`, copied
byte-for-byte to `web/src/utils/__fixtures__/applicability.json`, with a
parity test on each side).

### AD-011: Routes, redirects, navigation and URL state (revised)

**Routes** (`App.tsx`), nested under `DockerLayout` (sub-nav and `<Outlet/>`):

| Path | Page | Phase |
|---|---|---|
| `/docker` | `<Navigate to="/docker/containers" replace/>` | P1 |
| `/docker/containers` | `ContainersPage` | P1 |
| `/docker/containers/:deviceId/:name` | `ContainerPage` | P1 |
| `/docker/activity` | `ActivityPage` | P2 |
| `/docker/stacks`, `/docker/stacks/:deviceId/:project` | `StacksPage`, `StackPage` | P3 |
| `/docker/images`, `/docker/volumes`, `/docker/networks` | inventory pages | P4 |
| `/devices/:id/containers` | `LegacyContainersRedirect` | P1 |
| `/devices/:id/containers/:cid` | `LegacyContainerRedirect` | P1 |

**Phase gating.** `web/src/pages/docker/phases.ts` exports
`DOCKER_SHIPPED = { containers: true, activity: false, stacks: false, inventory: false }`.
Each phase's WP-nC flips its flag. The sub-navigation, tab list and routes
read it (FR-002, FR-054, AC-002, AC-037).

**Navigation.**

- The top-level "Docker" `NavLink` is added after "Dashboard" in both
  desktop and mobile navigation.
- It is shown while `isEnabled('docker')`, the existing toggle, which
  defaults to on (D-22).
- The sub-navigation is a `<nav aria-label="Docker">` list.
- The active entry has `aria-current="page"` (AC-001).

**Replacement of the per-device pages (S-1, BR-018):**

- `DeviceContainers.tsx` and `ContainerDetailPage.tsx` are **deleted**.
- The legacy paths redirect with history replacement:
  - **Containers list.** `LegacyContainersRedirect` loads `['device', id]`.
    On 404 it renders `DeviceQueryFallback` ("Device not found" with a link
    to Fleet). Otherwise it renders
    `<Navigate replace to={'/docker/containers?host=' + id}/>` (FR-006,
    AC-004).
  - **Single container.** `LegacyContainerRedirect` resolves `:cid` against
    `short_id`, then full `id`, in that device's containers. It then
    navigates with replace to the canonical page. If nothing matches, it
    renders `EmptyState` "Container not found" with a link to
    `/docker/containers?host=<id>` (FR-007, AC-005).
- **Device page.** The device page's Docker link changes to
  `/docker/containers?host=<device-id>` (FR-008).

**Canonical container page (FR-009).** It finds the container by `name` in
the host's latest data, so a recreate keeps the same URL. The
`lastContainer` ref is kept to avoid a flash during a recreate.

**URL state schema** (`web/src/utils/dockerUrlState.ts`: pure
`parse(search, ctx) → State` and `serialize(State) → string`):

| Param | Values | Default (omitted) |
|---|---|---|
| `q` | free text (trimmed, at most 200 chars) | empty |
| `status` | comma list of `running,stopped,unhealthy,updates` | none |
| `host` | comma list of device UUIDs | all |
| `stack` | comma list of `<device-uuid>:<project>` | all |
| `sort` | `<col>:<asc|desc>`, col in `status,name,stack,host,image,cpu,mem,uptime` | `name:asc` |
| `view` | `table` or `tiles` | last view chosen in this browser (`localStorage riot-docker-view`), else `table` (FR-029) |
| `sel` | comma list of `<device short_id>.<name>` | none |
| `tab` (resource pages) | `overview,logs,activity,inspect,terminal,stats` (container), `services,logs,activity,compose` (stack) | first tab |
| `range` (Logs, FR-157) | `15m,1h,6h,24h,7d` | `1h` |
| `from`, `to` (Logs, FR-157) | RFC3339; together they replace `range` | none |
| inventory pages | `q`, `status`, `host`, `sort` | as above |
| `/docker/activity` | `host`, `stack`, `container` (`<device-uuid>:<name>`), `actor` (`operator,system,agent,unknown`, or `sub:<actor_sub>`), `state` (canonical list), `type` (`command,event`), `from`, `to` (RFC3339) | none |

- **Removed (FR-164, OQ-10):** `mode`. The Logs tab is reached with `?tab=logs` only. The Live state is never read from the URL or from storage.

**Parsing and writing rules.**

- **Invalid values.** Unknown params and invalid values are dropped
  silently, and the valid remainder is applied (FR-027).
- **Selection.**
  - Selection entries are resolved against the currently visible rows.
    Unmatched entries are dropped (BR-012).
  - `sel` is written only when the selection has at most 200 entries **and**
    its encoded length is at most 4,096 characters. Otherwise it is omitted
    and the in-memory selection stays (FR-026, D-23).
- **History.** Filter changes use `navigate({search}, {replace:false})` so
  that Back and Forward step through them (FR-026). Typing in search
  debounces 300 ms before it writes the URL. Selection changes use
  `replace:true`, so the history is not flooded.

**Agent-supplied URLs (SEC-011).** Handled by `utils/safeUrl.ts`:

- Port links require an IP literal parsed by `utils/ip.ts::parseIP`, with a strict IPv4 and IPv6 grammar.
- Ports must be integers from 1 to 65535.
- The scheme must be `http` or `https`.
- `repo_url` and `riot.url` become links only when they parse as `http:` or `https:`. Otherwise they render as text.
- Menu items show the destination host.
- Links open with `rel="noopener noreferrer"`.

**Rationale:** device UUIDs are stable where hostnames are not. The short
`short_id` keeps `sel` compact.

**Consequences:** shared links carry device UUIDs. A demo build uses
HashRouter (UI-FOUNDATION), and the same parse and serialize functions apply.

### AD-012: Containers table

**Decision:**

- **Generic `components/docker/DataTable.tsx`** (also used by Stacks,
  Services and the inventory tables). It takes `columns`, `rows`, a
  `rowKey`, `sort`, `selection` and `onActivate`. It renders a
  `role="grid"` with `aria-rowcount`/`aria-colcount`, a sticky header row,
  and rows windowed by `hooks/useVirtualRows.ts`:
  - fixed row height 40 px (`ROW_HEIGHT` constant);
  - overscan of 10 rows;
  - the scroll container's height from a `ResizeObserver`, injectable for
    tests. When the observer is absent (happy-dom), it falls back to a
    `height` prop;
  - spacer elements above and below the window;
  - `aria-rowindex` on every rendered row.
- **Focus.**
  - The focused row index is roving `tabIndex`. The focused row is always
    inside the rendered window.
  - Up and Down move focus, and the window scrolls with it. Space toggles the
    row's checkbox. Enter activates the row (NFR-017).
- **Ports overflow.** "+N" opens a popover (`@floating-ui/react`, already a
  dependency), so the row height never changes.
- **Live updates.** The row component is `React.memo`, keyed by
  `<device>:<name>` and compared on a row `version` (a hash of the displayed
  fields). Telemetry merges are coalesced to at most one per second
  (AD-003).
- **Volatile-sort freeze (FR-034).**
  `utils/sortFreeze.ts::nextOrder(prevOrder, rows, sort, now, lastReorderAt, interacting)`
  re-sorts only when all of these hold:
  - the sort column is not volatile, **or**
  - at least 5 s have passed **and** `interacting` is false. `interacting` is
    true while focus is within the grid, or while the pointer is over it.

  Otherwise it keeps the previous order. New rows are appended in sorted
  position, and removed rows are dropped. The function is pure and its clock
  is injected.
- **Filtering, chips and counts.** These live in `utils/dockerChips.ts`
  (FRD §8.3), as pure functions over the projected rows. Counts ignore the
  chip selection itself.
- **Layout (NFR-023).** Columns use fixed widths that fit at 1280 px. Below
  768 px the grid container gets `overflow-x-auto scrollbar-thin`.

**Rationale:** 2,000 rows × 10 cells is about 20,000 nodes, which risks the
1 s and 100 ms budgets. Windowing renders about 40 rows. No library is needed
for fixed-height rows.

**Alternatives considered:**

- `@tanstack/react-virtual` (rejected: new dependency);
- CSS `content-visibility` for the table (rejected: it still creates every
  node and every React element).

**Consequences:** Ctrl+F browser search finds only rendered rows. The
in-page search box is the supported path.

### AD-013: New UI primitives

**Decision:** add these under `web/src/components/ui/` (generic, tokenised,
each with a co-located test). UI-FOUNDATION is on `main`, so its files are
modified in place:

| Primitive | Contract |
|---|---|
| `Tabs` | `Tabs({ tabs:{id,label,hidden?}[], selected, onSelect, idPrefix })` plus `TabPanel`. WAI-ARIA tabs: roving focus; Left/Right/Home/End; activation on arrow (automatic). `aria-controls`/`aria-labelledby` |
| `Menu` | `MenuButton({ label, items: MenuItem[] | MenuGroup[] })`. Menu-button pattern: Enter, Space or Down opens; arrows move; Escape closes and returns focus; `role="menu"`/`menuitem`; `separator` between groups; `danger` items use `text-danger-fg`. Positioned with `@floating-ui/react` |
| `ConfirmDialog` (modified) | adds an optional `confirmDisabled?: boolean`. Additive: default false |
| `TypedConfirmInput` | a labelled input that reports `matches(expected)`. Used as ConfirmDialog `children` with `confirmDisabled={!matches}` |
| `Popover` | a thin wrapper used for ports "+N" and tooltips needing interactive content |

`labels.ts` gains:

- `commandAction` labels for the new actions and step labels for the new steps (§8.11);
- categories `containerState`, `healthStatus`, `stackState`, `actorKind` and `inventoryStatus`;
- the stack path states and the actor auth methods.

That retires the deferred "container states not mapped" item from
UI-FOUNDATION 12.7 for Docker pages, so AC-099 holds.

**Rationale:** FR-050, FR-055 and NFR-017 need these patterns. UI-FOUNDATION
deliberately did not build them. Building them as primitives avoids
per-page copies.

### AD-014: Command palette

**Decision:**

- **`components/CommandPalette.tsx`.** It is mounted once in the
  authenticated shell (`App.tsx`) inside a `Modal` (`size="lg"`). It is a
  combobox with a listbox: `role="combobox"` on the input,
  `aria-activedescendant`, and options grouped with `role="group"` and
  `aria-label`.
- **Opening.**
  - A global `keydown` listener runs in the **capture** phase on `window`.
    On Ctrl+K (Windows and Linux), or Cmd+K when
    `navigator.platform`/`userAgentData` says macOS, it calls
    `preventDefault` and opens the palette.
  - The exception: when `event.target.closest('[data-palette-exempt]')`
    matches, the listener returns untouched, so the keystroke reaches the
    terminal. `components/Terminal.tsx`'s wrapper gains `data-palette-exempt`
    (FR-066, AC-044).
  - A header button labelled "Search (Ctrl+K)" opens the palette too.
- **Data source** (`hooks/usePaletteIndex.ts`). It builds from React Query
  caches:
  - `['devices']` for hosts;
  - `['docker','containers']` for containers. On first open it is fetched
    with `ensureQueryData` and `staleTime: 30 s` if absent;
  - stacks: derived from containers in P1–P2. From P3 they come from
    `['docker','stacks']`, which includes Down stacks;
  - the static list of shipped Docker pages.

  Each entry precomputes a lower-cased `name`, `host`, `stack` and `image`.
  The index is memoised on the cache data references.
- **Ranking** (`utils/paletteRank.ts`, pure). Matching tokens split the
  lower-cased query on whitespace, and every token must occur in
  `name|host|stack|image`. Within a type group, results are ranked:
  - rank 0: the name starts with the full query;
  - rank 1: the name contains the full query;
  - rank 2: any other match;
  - ties: name, then host.

  Groups are ordered Hosts, Containers, Stacks, Pages. At most 50 results in
  total, with "N more, refine your search" (FR-069).
- **Recent resources.** These are kept in `localStorage riot-palette-recent`
  as up to 8 `{type, key}` entries, newest first. They are pushed when a
  container, stack or host page mounts. Entries missing from the current
  index are filtered out at render (FR-073).
- **Offline hosts.** Containers on offline hosts carry a "Host offline"
  marker (FR-072).

**Performance:** a linear scan over 5,000 entries with at most 4 tokens of
`includes` is about 20,000 string checks, well under 100 ms (NFR-005).
`vitest` asserts that the pure ranker runs in under 50 ms on 5,000 synthetic
entries, with `performance.now`.

### AD-015: Live logs protocol: shared follow plus per-viewer backfill (rewritten for OQ-09, OQ-10 and D-36)

**Decision.** New message types are carried in the existing agent WS
envelope (`AgentWSMessage{type, session_id, container_id, data}`). JSON
only. Every `data` payload is bounded.

- Containers are identified by **name**. The agent resolves names to IDs itself, at follow time and again on restart.
- **Batching.** The agent flushes a `logs_data` batch every 250 ms, or when it is full.
- There is one agent stream per `(device, container)`. Its `stream_id` is 16 random bytes in hex, generated by the server.
- Backfills are separate, one-shot requests with their own `request_id`.

| Direction | `type` | `data` |
|---|---|---|
| server → agent | `logs_follow` | `{"stream_id","container"}`. Follows from now (`Since=now`, `Tail=0`). |
| server → agent | `logs_unfollow` | `{"stream_id"}` |
| server → agent | `logs_backfill` | `{"request_id","container","after"?:"<RFC3339 µs>","limit":5000}`. With no `after`, returns the last 100 lines. Never follows. |
| server → agent | `logs_keep` | `{"stream_ids":[...]}`. Sent every 60 s to each connected agent, listing the streams that still have viewers. The agent reaps any follow that no `logs_follow` or `logs_keep` has named for 5 minutes (SEC-025). |
| agent → server | `logs_data` | `{"stream_id"? \| "request_id"?,"lines":[{"s":"o"\|"e","t":"<RFC3339 µs>","l":"<masked>","x"?:true}]}`. At most 256 lines and 64 KiB. |
| agent → server | `logs_event` | `{"stream_id"? \| "request_id"?,"kind":"live"\|"exited"\|"restarted"\|"dropped"\|"backfill_end"\|"unavailable"\|"ended","n"?,"skipped"?,"skipped_lb"?,"code"?,"reason"?}` |

- The agent truncates timestamps to microseconds.
- `backfill_end` carries `n`, the number of lines delivered, and `skipped`, the number of lines beyond the bound. `skipped` is a lower bound when `skipped_lb` is set.
- Reason values: `not_found`, `unsupported`, `too_many_streams`, `docker_unavailable`, `internal`.

**Rationale:** it reuses the authenticated, mTLS-capable socket (FR-075) and
the terminal envelope.

### AD-016: Server fan-out hub (rewritten; SEC-004, SEC-006, SEC-012)

**Decision.** New files: `internal/server/livelogs/hub.go` and
`internal/server/handlers/docker_logs_ws.go`.

**Browser endpoint.** `GET /ws/docker/logs/{deviceId}` sits in the admin
group.

- **SEC-012.** `Origin` is required. An absent value gets 403. A present value is compared the way `CheckWSOrigin` does (NFR-009).
- **Before the upgrade,** the server checks:
  - a valid device UUID;
  - the `logs_live_v1` token, for UX only (`requireDockerCap`; else HTTP 409 `agent_unsupported`);
  - that the agent is connected in the agent registry (else 502 `agent_offline`);
  - the actor (AD-022) and the remote address (`middleware.ClientIP(r)`).

**One socket per Logs view.** Browser frames are text frames of at most
4 KiB, with a read limit set:

- `{"type":"attach","containers":[{"name","boundary"?}],"merged":bool}`;
- `{"type":"detach","containers"?:[name]}`.

**Attach, per container.**

1. The name passes AD-002 and appears in the latest snapshot. If not, the server sends `error not_found` and **sends nothing** to the agent.
2. The server looks up `hub.streams[(device, name)]`:
   - **The stream exists.** The viewer joins it, within 8 viewers per stream and 64 viewers server-wide. Otherwise the server replies `too_many_viewers`.
   - **No stream exists.** The server opens one, within 8 streams per host. Otherwise it replies `too_many_streams`. It sends `logs_follow` to the agent. Existing streams are never ended (FR-085).
3. The server queues one `logs_backfill` for this viewer, starting at its boundary (SEC-026):
   - **Boundary check.** The `boundary` must parse as RFC3339 with microseconds and lie within `[now − effective retention, now + 5 min]`. Otherwise it is treated as "no boundary", which means the last 100 lines.
     - On a host with a skewed clock, the agent-domain boundary (AD-018) can fall outside this window. The backfill then degrades to the last 100 lines, and the view shows a gap marker.
   - **Rate.** At most one backfill per (socket, container) per 10 s. A faster request is deferred to the end of that window and coalesced. It is never refused with an error.
   - **Coalescing.** Concurrent requests for the same (device, container, boundary) become one agent read, fanned out to every requester.
   - **Concurrency.** At most 2 backfills run per host. Others wait in a FIFO queue of at most 16. Beyond that, the request is refused and the viewer shows a gap marker.
4. The server writes an audit row with `kind=logs`, per viewer and container. Rows are coalesced (SEC-006 item 5): a re-attach by the same actor sub, device, container and remote address within 2 minutes of the previous row's end re-opens that row instead of adding a new one.
   - **Rate cap (rev 3.2, B-16).** Each device gets at most `AuditLogAttachRowsPerDevicePerMinute` (32) new `logs` rows per minute.
   - **Overflow.** The first attach over the cap in a minute writes one overflow summary row: `target` `container:*`, `detail{"suppressed":n}`. Later overflows in that minute update its count. The audit trail therefore still records that the attaches happened.
   - Test: `TestDUX_BND_AuditRowsUnderAttachStorm`.

**Merged views (D-38).** Live in a merged view is accepted for at most the
host's free stream slots for containers that are not already streaming.
Joining a stream that already exists is always allowed. The picker shows the
containers that were refused.

**Agent to hub (device-scoped).**

- Agent frames are routed only to streams or requests of the socket's own device.
- **Per-frame re-checks (SEC-006 item 3).** The server re-checks each frame against these limits; excess is dropped and counted:
  - at most 256 lines and 64 KiB per frame;
  - each line at most 16 KiB;
  - each stream at most 768 KiB/s;
  - each agent at most 3 MiB/s.
- **Unknown or retired streams (SEC-025; bounded in rev 3.2, SEC-036).** A frame for an unknown or retired `stream_id` is dropped.
  - **Reply.** The server answers with `logs_unfollow` for that ID, drawing from one **per-device** bucket: `LiveUnknownStreamUnfollowPerDevice`, 4 per second, burst 8.
  - **No per-ID state.** Nothing is stored for unknown IDs. Beyond the bucket, frames are dropped without a reply and counted.
  - **Retired IDs.** Each device keeps an LRU of at most `LiveRetiredIDsPerDevice` (256) retired IDs, each for at most `LiveRetiredIDTTL` (10 min).
    - An ID that has aged out is simply unknown, and gets the same treatment.
    - IDs are 128-bit random, so a retired ID is still never reused (SEC-025).
  - A throttled Warn records it.
  - Test: `TestDUX_BND_RetiredStreamIDsBounded`, with 100,000 random IDs.
- **SEC-004 item 3.** The server applies `SanitizeText` and `MaskLine` to every relayed line, whatever tokens the agent advertises.
- **Timestamp window (D-42, SEC-021).** Some relayed lines carry a timestamp outside `[now − effective retention, now + 5 min]`. Each such line is re-stamped with the server receive time and carries the agent's time as `agent_t`, exactly as stored lines are (AD-035 §1a). The browser shows the same clock-skew marker.
- **Fan-out.** The server places each frame on each viewer's queue without blocking. Backfill frames go only to the viewer that requested them.
  - **Queue cap (rev 3.1).** A viewer queue holds at most 64 frames **and** at most 1 MiB (`LiveViewerQueueBytes`), whichever limit is reached first.
  - **Total.** The 64-viewer server cap therefore bounds all queue memory to 64 MiB.
- **A full queue** drops the frame for that viewer and sends it a `dropped{n}` marker. After 10 s of continuous drops, that viewer alone is detached with `client_too_slow`.
- The agent read loop, which also carries command reports, never blocks on a viewer.
- **Browser writer.** One goroutine per socket drains its viewers' queues with a 10 s write deadline.
- **Keepalive.** Pings every 30 s, read deadline 90 s. This is the terminal pattern.
- **Maximum lifetime.** A viewer is detached after 4 h (D-21).
- **Session expiry (SEC-027).**
  - At upgrade, the server records the session JWT's `exp`.
  - At that time it closes the socket with `{"type":"error","code":"session_expired"}`, and it refuses `attach` from then on.
  - A socket's total lifetime is also capped at 4 h.
  - The client reconnects through a fresh upgrade, which re-checks the cookie. An expired cookie gets 401, which starts the dashboard's normal login flow.
  - Test: `TestDUX_SEC027_SocketClosedAtTokenExpiry`.
  - `/ws` and `/ws/terminal` have the same gap. It is noted, not fixed, by this story (R-17).

**Detach and stop (FR-165).**

- A viewer leaves on a detach message, when its socket closes, or when the hidden-tab timer fires.
- When the **last** viewer leaves, the server waits a 2 s linger and then sends `logs_unfollow`. The agent cancels within 1 s, so the follow stops within 5 s (AC-121).
- **Serialization (SEC-025).**
  - Attach, detach and the linger timer for one `(device, container)` all run under that stream's mutex.
  - An attach during the linger cancels the pending unfollow and joins the existing stream.
  - Once `logs_unfollow` has been sent, the stream ID is retired. A later attach opens a **new** stream with a new `stream_id`.
  - Frames still arriving for a retired ID are discarded, and they trigger the unfollow described above.
  - Tests: `TestDUX_SEC025_AttachDuringLingerKeepsStream`, `TestDUX_SEC025_UnknownStreamFrameTriggersUnfollow`.
- **Keep-alive (SEC-025).**
  - Every 60 s, the hub sends each connected agent a `logs_keep` message listing the stream IDs that still have viewers.
  - The agent reaps any follow that no `logs_follow` or `logs_keep` has named in the last 5 minutes (AD-017).

**Agent disconnect.**

- The registry's `unregister` hook calls `hub.AgentGone(device)`. That includes eviction through `replace` (§2.5). Each viewer receives `session reconnecting`.
- On reconnect, the hub re-follows every stream that still has viewers. Each viewer re-attaches with its boundary and receives a backfill.

**No storage.** The hub writes only the audit row. AC-049 checks that it
makes no container-log repository call, and that no `server_logs` record
contains line content.

**Rationale:** non-blocking fan-out protects the command channel. The device
scoping and the authorization checks before the agent is contacted satisfy
NFR-009.

**Consequences:** a slow browser loses lines and sees markers. It never
slows the agent or other viewers.

### AD-017: Agent log streamer (rewritten; SEC-006, SEC-016)

**Decision.** New file `internal/agent/livelogs.go`.

**Limits:**

- at most 8 streams (beyond that it replies `unavailable/too_many_streams`);
- at most 2 concurrent backfills;
- `docker` must be enabled;
- each container name is resolved with `ContainerList(All)` and an exact match on the trimmed name. An unknown name gets `unavailable/not_found`. The agent serves only its own daemon, so "own host only" holds by construction.

**Reading.** The agent inspects the container's TTY setting (`Config.Tty`)
and uses the raw reader for TTY containers, or the multiplexed parser
otherwise. The 8-byte header parse is implemented locally, as
`docker_logs.go` already does; no new import. Logs are read with
`ContainerLogs(Follow:true, Timestamps:true, ShowStdout, ShowStderr)`.
**The line reader is bounded:**

- it holds at most 16 KiB plus 4 KiB per stream;
- a run longer than that without a newline is emitted as one truncated line (`x:true`), cut at a rune boundary; the browser renders "[truncated]";
- the remainder is discarded up to the next newline.

**Per line, in this order:**

1. Read the line under the bound.
2. Decide whether to admit it. The buckets allow 1,000 lines/s and 512 KiB/s per stream, and 2 MiB/s per agent. A dropped line is counted, and is never masked.
3. Apply `MaskLine`.
4. Add the line to a batch.

The agent emits `dropped{n}` once per second when lines were dropped.
`TestDUX_SEC006_DropBeforeMask` asserts that the number of mask calls equals
the number of lines delivered.

**Backfill.**

- It reads with `Follow:false`. With `after`, it uses `Since=after` and `Tail=5001`. Without `after`, it uses `Tail=100`.
- If 5,001 lines come back, the agent drops the oldest one and computes `skipped`. It scans at most 20,000 lines for that count. Beyond the scan bound, `skipped` is a lower bound and `skipped_lb` is set.
- **Byte budget (SEC-026).** Backfill draws from its own bucket of at most 1 MiB/s, inside the 2 MiB/s agent budget. Live streams therefore always keep at least 1 MiB/s. Test: `TestDUX_SEC026_LiveKeepsBudgetDuringBackfill`.
- **Memory (rev 3.2, B-27).** Backfill is streamed. It holds one 64 KiB batch and one 16 KiB look-ahead line, and the skip scan only counts lines.
  - Test: `TestDUX_BND_BackfillStreamsWithoutBuffering`. Peak heap growth must stay under 4 MiB for 5,000 lines of 16 KiB plus a 20,000-line skip scan.

**Exit and restart.**

- At end of stream the agent inspects the container and sends `exited{code}`.
- A watcher subscribes to the agent's existing Docker event stream (`dockerEventLoop` gains an in-process fan-out, `a.dockerEvents.Subscribe()`). A `start` event for the same name triggers `restarted`, and the agent re-follows from that point (FR-082).
  - **Bounds (rev 3.2, SEC-036, B-29).** `Subscribe` returns `(ch, unsubscribe, err)`, and at most `AgentEventBusSubscribersMax` (10, that is 8 streams + 2) subscribers exist.
  - **Unsubscribe.** The watcher calls `unsubscribe` in its cancel path (`defer`). Every stream end, unfollow, reap and disconnect therefore removes it.
  - **Bus full.** `Subscribe` fails. That stream still works, but sends no `restarted` marker, and a budgeted Warn records it.
  - Test: `TestDUX_BND_EventBusUnsubscribeOnCancel`.

**Writes and shutdown.**

- Writes go through the merged serialized writer, in batches of at most 64 KiB (NFR-007). A command report waiting on `writeMu` therefore waits for at most one batch write (milliseconds on a LAN). The byte caps keep the socket far from saturation (NFR-007, AC-061).
- **Pending batches (rev 3.1).** Each stream holds at most `AgentStreamPendingBatches` (2) batches waiting for the writer.
  - When both are full, newly admitted lines are dropped. They are counted into the next `dropped{n}` marker.
  - A slow socket therefore never grows agent memory. Test: `TestDUX_BND_AgentPendingBatches`.
- On `logs_unfollow`, on disconnect (streams are bound to the connection), or on shutdown, the agent cancels within 1 s, emits `ended` when connected, and closes all readers.
- **Orphan reaping (SEC-025).**
  - The agent cancels any follow that no `logs_follow` or `logs_keep` has named for 5 minutes.
  - It logs Info with the stream ID only.
  - Test: `TestDUX_SEC025_AgentReapsOrphanedFollow`.
- **Never persisted (V-1).**
  - `logs_data` frames, `logs_event` frames and backfill frames are written only to the live WS.
  - They never enter the command outbox, the SQLite telemetry buffer, or any file. When the WS is down, they are dropped.
  - Test: `TestDUX_V1_LiveFramesNeverPersistedOnAgent`.

**Rationale:** the limits sit at the source, so neither the agent socket nor
the server sees more than the bound. Resolving names on the agent follows
recreates.

**Log hygiene (SEC-016).** Agent `slog` records in `livelogs.go`,
`docker_read.go`, `docker_stack.go`, `docker_remove.go`, `stackfs*.go`,
`docker_logs.go` and `redact/*` carry only lengths, counts, enums and names.

- Parse errors are wrapped as `redact.ErrParse{Offset, Kind}`, which never quotes input.
- `TestDUX_SEC016_AgentLogsNoContent` captures log output while running secret fixtures through these paths, and checks that no secret appears.

### AD-018: Browser live client inside the one Logs tab (rewritten)

**Decision.** The live client is the Live half of `useLogView` (AD-036). It is
not a separate component.

- **Transport.** `api/liveLogs.ts` exports `openLogsSocket(deviceId)`, which returns `{attach, detach, close}`. It wraps `new WebSocket('/ws/docker/logs/<id>')`. The browser sets `Origin` on the socket. When `import.meta.env.VITE_DEMO` is set, the module returns a simulated hub (AD-031) and makes no network calls (NFR-018, AC-103). No `vite.config` change is made.
- **Live on** (enabled per FR-074):
  1. Send `attach` with one boundary per container: the timestamp of the newest shown line, in the agent's clock domain. That is `agent_t` when the line was re-stamped (D-42), and `t` otherwise.
  2. Hold incoming live lines as *pending* until that container's `backfill_end` arrives.
  3. Merge backfill and pending lines by identity.
  4. Add a gap marker when `skipped > 0`.
  5. Pause the history refresh (FR-163).
- **Live off.** Detach, keep every line already shown, and resume history refresh from the newest one (FR-079).
- **States** follow FRD §8.9. Reconnect re-attaches every 2 s with the current boundary and adds a gap marker. After 60 s of `agent_offline`, the state becomes Unavailable.
- **Hidden tab.** `hooks/useHiddenStop.ts` runs with an injected clock. It detaches after 5 minutes hidden, and re-attaches from the boundary, with a gap marker, when the tab is visible again (FR-083).

### AD-019: On-demand reads through an agent RPC (revised for SEC-001)

**Decision:** create `internal/server/agentrpc/agentrpc.go`.

- **Call.** `Call(ctx, deviceID, kind, req any, maxResp int) (json.RawMessage, error)`.
- **Messages.** It sends `{"type":"docker_read","session_id":<request UUID>,"data":{"kind":…,…}}`
  and waits for `docker_read_result` with the same ID **from the same
  device's socket** (scoped like AD-016).
- **Pending map.** `map[requestID]{deviceID, ch}`, with at most 4 concurrent
  requests per device.
- **Timeout.** 10 s (FR-090), with an injected clock.
- **Rate limit.** A per-device token bucket allows 30 per minute, the
  CMD-LIFECYCLE `DeviceLimiter` type reused as a separate instance. Over the
  limit the server returns `429 {"code":"rate_limited","message":"Try again shortly"}`
  (NFR-008).
- **Size.** A response over `maxResp` (1 MiB for inspect and each compose
  file, 256 KiB for env) is discarded with `too_large`. The agent also
  enforces it.

**Read kinds:**

| `kind` | Request | Agent result |
|---|---|---|
| `inspect` | `{container}` | `{ok, json (redacted, indented), redactions}` or `{ok:false, reason}` |
| `compose_list` | `{project, working_dir, config_files}` (agreed paths, AD-021) | `{ok, working_dir, container_writable, files:[{index, name, size, status:"ok"|reason, container_writable}], env:{present, status}}` |
| `compose_file` | `{project, working_dir, config_files, index}` | `{ok, content (redacted), redactions}` or reason |
| `env` | `{project, working_dir, config_files}` | `{ok, keys:[{key, value}], redactions}` or reason |

**Admin HTTP endpoints** (§8.6) call `Call`. Every response is sent with
`Cache-Control: no-store`. Nothing is persisted (FR-086, FR-119). The server
records an audit row: `kind=inspect` or `kind=compose` (AD-024).

**SEC-001 item 2.** Inspect, compose list, compose file and env reads use
`RequireRiotHeader` (AD-034). It requires `X-Riot-Request: 1` and a
same-origin request. A failure returns 403 before any agent call, before any
rate-limit use, and before any audit row.

**Reason enum:** `not_found|outside_stack_directory|denied_path|bad_extension|too_large|not_readable|missing|redaction_failed|unsupported|too_many_files|conflicted|paths_changed|container_writable|not_regular`.

**Rationale:** a WS request/response with no persistence fits a transient
read. Commands would persist content and would be wrong here.

### AD-020: Inspect redaction (revised for SEC-003)

`redact.InspectJSON` decodes the inspect document with `UseNumber` and
applies:

| Field | Rule |
|---|---|
| `Config.Env` | rule 1 |
| `Config.Cmd`, `Entrypoint`, `Args`, `Config.Healthcheck.Test` | rule 4, with whitespace tokens and attached short flags |
| `Config.Labels` | rule 2 plus the masking net |
| `HostConfig.LogConfig.Config` and every other object | rule 2 on keys; every string gets rule 3 plus the masking net |

Redactions are counted. The output is re-encoded with two-space indentation
and sorted keys (`json.Marshal` of a map sorts keys). A result over 1 MiB
returns `too_large`, and any decode error returns `redaction_failed` with no
content.

On failure, the agent logs `inspect redaction failed` (container name length
only), and the server logs the NFR-022 entry on receipt.

**Consequences:** AC-062 and AC-063: `APP_MODE=prod` is redacted (not on the
safe list), `TZ` is kept, and the count is 4.

### AD-021: Compose and `.env` reads (revised for SEC-002, SEC-003 and SEC-005)

**Path sources (SEC-002).** Paths come only from the agreed record (AD-038).

- The server sends `{project, working_dir, config_files}` taken from `docker_stacks.agreed_*`.
- The agent requires its registry's last *agreed* entry to equal those paths. Otherwise the read fails with `paths_changed`.
- The agent also requires current member labels, if any, to agree. Otherwise it fails with `conflicted`.
- Requests name `(device, project)` only. Bodies and queries containing any path field are rejected: decoding uses `DisallowUnknownFields`, giving 400 `invalid_request`, and nothing is sent (FR-118, AC-082).
- **Agent stack registry** (`internal/agent/stack_registry.go`, file `/var/lib/riot/docker-stacks.json`, mode 0600, atomic writes through CMD-LIFECYCLE `atomicJSONFile`, at most 500 projects, §7.4). It is updated after each `docker` collection from labels **the agent itself observed**, and keeps the last agreed paths per project (AD-038).

**Outer bound on the working directory (SEC-005 item 1).** With
`rw := EvalSymlinks(wd)`:

- `rw` must not be `/`;
- it must not equal a denied root, be an ancestor of one, or lie inside one.

**Denied roots (SEC-005 item 2).** Each root is symlink-resolved before
comparison:

- `/etc`, `/root`, `/run`, `/var/run`, `/var/lib/docker`;
- `/proc`, `/sys`, `/dev`, `/boot`;
- the agent's config directory, `/var/lib/riot` and `/usr/local/lib/riot`;
- the directories of the client certificate, client key and CA file, and of the pinned server certificate;
- any path containing a `.ssh`, `.docker`, `.kube`, `.gnupg` or `.config` component.

**Per-file checks** (`stackfs.CheckComposePath(wd, p)` in
`internal/agent/stackfs.go`):

- Absolute, and stable under `filepath.Clean`.
- Extension `.yml`, `.yaml` or `.json`. For `.env`: exactly `wd/.env`.
- `EvalSymlinks` result inside `rw`, checked with `filepath.Rel` without `..`.
- Size limit: 1 MiB per compose file, 256 KiB for `.env`.
- At most 10 files per stack.
- Each failure has its own reason (FR-123, AC-084).

**No-race open (SEC-005 item 3; `stackfs_linux.go`).**

1. Open `/` with `O_PATH|O_DIRECTORY|O_CLOEXEC`.
2. Open each component of the resolved path, then of the file's relative path, with `syscall.Openat(dirfd, comp, O_PATH|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC)`. A component swapped for a symlink fails with `ELOOP` or `ENOTDIR`, and the read returns `outside_stack_directory`.
3. Open the final component with `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`, so a FIFO never blocks.
4. `syscall.Fstat` must report a regular file within the size limit. Otherwise the read returns `not_regular` or `too_large`.
5. Read through `io.LimitReader(limit+1)`.

The file's location is guaranteed by construction. On Windows,
`stackfs_other.go` returns `unsupported`, and the token is not advertised
(D-39).

**Compose redaction (in place, keeping line numbers).** `redact.ComposeFile(b)`
keeps the original text so line numbers stay true (FR-119).

- **Parsing.** The file is parsed with `yaml.v3` into a `Node` tree (JSON is YAML). Anchors and aliases are resolved for detection, but the anchor's own scalar is the one replaced.
- **Replacement.** Each replacement span is located in the original bytes by `Line` and `Column`. A single-line plain or quoted scalar is replaced in place with `<redacted>`. A block scalar (`|`, `>`) has every content line replaced with `<indent><redacted>`.
- **D-27 resolved to rule 1.** Every `environment:` value is redacted, in map and list form, including values reached through anchors, aliases and `<<` merges, unless its key is on the safe list.
- Other mapping keys get rule 2.
- `command`, `entrypoint` and `healthcheck.test` get rule 4.
- Every scalar gets rule 3 plus the masking net.
- Each replacement is located exactly; block scalars have every line replaced. If any location fails, the read fails with `redaction_failed`.
- A final rule 3 pass runs over the whole text, comments included.

**`.env` (`redact.DotEnv`, SEC-003 item 6).** Parsed with compose dotenv
semantics:

- `#` comments and blank lines;
- an optional `export ` prefix;
- `KEY=VALUE`, where the key must match `^[A-Za-z_][A-Za-z0-9_.-]*$`;
- single-quoted values are literal;
- double-quoted values may contain escapes and span multiple lines;
- unquoted values may end with an inline ` #` comment.

Any line that fails to parse fails the whole read (`redaction_failed`). Value
text is never shown as a key. Values get rule 1. The result is the keys with
their values, in which redacted values are `<redacted>`.

**Paths shown (SEC-005 item 4).** The Compose tab shows the agent-resolved
working directory and files. It marks any file that AD-038 finds writable by
a container (`container_writable`).

**Untrusted labels (A-11).** Container labels can be set by image authors. A
hostile `com.docker.compose.project.working_dir=/etc/riot` or a
`config_files` entry outside the directory never becomes an agreed path
without agreement and acknowledgement (AD-038), and every read and every
compose invocation (AD-027) still passes the checks above.

**Rationale:** confinement is decided on the host from the host's own view.
In-place redaction keeps line numbers true, which a re-marshal would lose
(along with comments).

**Consequences:** `.env` files that the agent user cannot read
(commonly 0600 root) return `not_readable` (A-06). The README says so.

### AD-022: Actor attribution: auth method, stable subject, display name (revised for SEC-008)

**Claims** set by `issueSessionCookie`:

| Claim | Password login | OIDC login |
|---|---|---|
| `riot_auth` | `password` | `oidc` |
| `riot_sub` | `local:admin` | `oidc:` + hex(sha256(issuer + "\n" + subject))[:32] |
| `riot_name` | `admin` | `preferred_username`, falling back to the subject; passed through `SanitizeText`, at most 128 bytes |

`oidc.Claims` gains `PreferredUsername *string`.

**Request context.** `AdminAuth` and `AdminAuthWS` put
`middleware.Actor{Auth, Sub, Name}` into the context. A token issued before
this change, without the new claims, maps to the `legacy` row below
(SEC-008 item 3).

**Stored on commands and audit rows.** `DispatchRequest` gains `Initiator`
(§2.5). The command columns are filled as follows:

| Source | `actor_kind` | `actor_auth` | `actor_sub` | `actor_name` | Display |
|---|---|---|---|---|---|
| Operator | `operator` | from the claim | from the claim | from the claim | name and method, e.g. "admin (password)" or "admin (SSO)" |
| Legacy session | `unknown` | `legacy` | empty | empty | "Unknown (legacy session)" |
| Auto-update | `system` | `system` | `system:auto-update` | `auto-update` | "System (auto-update)" |
| Auto-patch | `system` | `system` | `system:auto-patch` | `auto-patch` | "System (auto-patch)" |
| Row created before this story | `unknown` | empty | empty | empty | "Unknown (before activity tracking)" |

The type is named `models.Initiator`, so it does not collide with
CMD-LIFECYCLE's `CommandActor` transition enum. Filters match on
`actor_sub`. Display (web, `labels.ts actorKind`) shows the name together
with the auth method.

**Observed events.** They are not commands. Their actor is derived as
`agent_observed` (AD-023) and displayed as "Agent (observed)".

`riot_name` is truncated to 128 bytes at a rune boundary.

**Q-S4.** No extra HMAC is needed: HS256 already protects the claims.

**Rationale:** the identity must be captured at creation (FR-093). The JWT is
the only per-request identity carrier, and adding claims needs no session
store.

**Consequences:** the display name is operator-controlled IdP data. It is
rendered as text only, never used for authorization, and never logged with
other identity data. OIDC users without `preferred_username` show their
subject.

### AD-023: Activity, write-time masking, validated attribution (revised for SEC-004 and SEC-015)

**Decision (OQ-02):**

1. **Step timeline.** `commands.steps JSONB` (§7.2) holds at most 64 entries
   of `{seq, name, started_at, finished_at?, outcome?: "succeeded"|"failed"|"skipped"}`.
   - It is maintained **by the server** in the same guarded `Transition`
     UPDATE that already writes `step`: when an accepted report changes
     `step`, the open entry is closed and a new one appended. There is no
     extra write.
   - At terminal, the last entry is closed with the command's outcome.
   - The JSON append is done in SQL:
     `steps = CASE WHEN step IS DISTINCT FROM $step THEN … END`, built by a
     pure helper `buildStepAppend`.
2. **Step output.** Agents with `cmd_steps_v1` add
   `steps:[{name, started_at, finished_at, outcome, output}]` to the final
   `command_result`.
   - The agent applies `redact.MaskLine` to each output line, and caps the
     total at 64 KiB, cutting from the head of the longest outputs first and
     marking `truncated:true` per step.
   - The server re-sanitizes, re-masks, re-caps (64 entries, 64 KiB in
     total), and stores **one** `command_output` row with `stream='steps'`
     and the JSON `content`. It is written once at terminal, next to the
     existing `combined` row. The agent's step timestamps replace the
     server-observed ones in `commands.steps` in the same terminal UPDATE.
   - Older agents: no `steps` row. The UI shows the server-observed
     timeline and the `stderr_tail`/`result_msg` text.
3. **Retention.** `command_output` rows older than 30 days are deleted
   (D-14). The command row and its `steps` timeline follow the command
   retention, 90 days (AD-025).
4. **Observed Docker events.** The existing `events` rows of type
   `container_*` are reused. `events.command_id` (§7.2) is set as follows,
   and then validated (below):
   - **Agent side.** Agents with `event_attribution_v1` fill
     `DockerEvent.command_id` when the event's container name is in the
     agent's `activeDockerTargets` map. Every executing Docker command
     registers its container names there; stack commands register all
     members.
   - **Server side.** When no `command_id` arrives, `CheckDockerEvent`
     attributes the event to an active command, or one finished within the
     last 10 s, on that device whose `target_keys` contain
     `docker:container:<name>` (one indexed lookup in the CMD-LIFECYCLE
     active index). Such an attribution is recorded as `inferred`.
   - Events with a non-empty `command_id` are **not** listed as observed
     entries. The command's detail lists them as "Docker events during this
     command".
5. **Activity queries** (`internal/server/db/activity_repo.go`). Two keyset
   queries are merged in Go by `(created_at DESC, kind, id)`, and an opaque
   cursor is returned:
   - commands: `action LIKE 'docker_%'`, optional device, and for a container
     scope `target_keys && $keys`, with
     `$keys = [docker:container:<name>, docker:compose:<wd of its stack>]`
     (FR-091);
   - events: `type LIKE 'container_%' AND command_id = ''`, with the optional
     device and container name, using the new partial index.

   Each query is `LIMIT $limit+1`. Filters are bound parameters. State
   filters use CMD-LIFECYCLE `parseStatusFilter`. This satisfies FR-097: all
   entries within retention, not a recent-events window.
6. **Live updates.** The UI applies `command_update` WS messages (step and
   status) to visible entries. On a terminal status it refetches that entry's
   detail, which brings in step output.

**Write-time masking (SEC-004 item 2).** The merged `sanitizeReport` in
`commands/validate.go` gains `redact.MaskLine`, applied line by line,
**before storage and broadcast**, to:

- `result_msg`;
- `stderr_tail`;
- the progress `message`;
- `output`;
- `target_results[].message`;
- step output.

Rows written before this change are masked again at read time on
`/commands`, `/commands/{id}/output` and Activity. Masking is idempotent.
Test: `TestDUX_SEC004_CommandTextMaskedAtWrite`.

**Validated `command_id` (SEC-015).** A `command_id` reported by the agent is
kept only when all of these hold:

- it is a canonical UUID;
- it names a command on the **same device**;
- that command is active, or finished at most 10 s ago;
- its `target_keys` include the event container's key or its compose key.

Otherwise the server clears it.

`events.command_attr` records the source: `agent`, `inferred` (the server's
own fallback correlation), or empty. The UI labels inferred attributions.
Test: `TestDUX_SEC015_ForeignOrMalformedCommandIDCleared`.

**Rationale:**

- The step timeline is a few hundred bytes per command, written only when an existing write already happens.
- Output is written once, into an existing table, and is bounded.
- Observed events already exist, so no new table is added.

**Alternatives considered:** a `command_steps` table with one row per step
(rejected: about 10 times the row count, and update churn per step).

### AD-024: One audit store with per-kind retention that survives device deletion (revised for SEC-009)

**Columns.** `terminal_sessions` gains:

- `kind`: one of `terminal`, `logs`, `inspect`, `compose`, `command`, `stack_paths`, `device_delete`;
- `actor_auth`, `actor_sub`, `actor_name`;
- `target`;
- `detail` (JSONB);
- `retention_class`: `long` or `short`.

`db.TerminalRepository` gains
`LogAccess(ctx, models.AccessAudit) (id int64, err error)` and
`EndAccess(ctx, id)`. The existing `LogSessionStart` and `LogSessionEnd` are
kept and write `kind='terminal'`.

| `kind` | When | `container_id` | `target` | `detail` |
|---|---|---|---|---|
| `terminal` | unchanged (plus actor now) | as today | container | none |
| `logs` | each live viewer attach, per container (coalesced, AD-016); `ended_at` at detach | `""` | `container:<name>` | `{"containers":n,"end_reason":"…"}` |
| `inspect` | each Inspect read | `""` | `container:<name>` | `{"result":"ok"|reason}` |
| `compose` | each compose list, file or env read | `""` | `stack:<project>` | `{"what":"list"|"file"|"env","result":…}` |
| `command` | each operator-created command, for every action, Docker and non-Docker alike (including `reboot`, `shutdown`, `os_update`, `agent_update`, `agent_uninstall` and `sync_privileges`); each bulk target; each prune. Written by `commands.Service.Dispatch` (SEC-029) | `""` | target key, or `host` | `{"command_id","action"}` |
| `stack_paths` | a stack path change observed, and each acknowledgement (AD-038) | `""` | `stack:<project>` | old and new paths |
| `device_delete` | `DeleteDevice`, before the delete | `""` | `""` | `{hostname}` |

Each row has the actor (AD-022), `device_id`, `started_at` and
`remote_addr` (NFR-011, AC-104).

**Retention (D-21, settled in revision 3).**

| Class | Kept for | Rows |
|---|---|---|
| `long` | **180 days** | `terminal` (including `_host` host shells); `command` rows for destructive or privileged actions; `stack_paths`; `device_delete` |
| `short` | **90 days** | `logs`, `inspect`, `compose`, and all other `command` rows |

- **Configuration.** The values are code constants: `AuditRetentionLong` and `AuditRetentionShort` (§14.2).
  - The ADD has no audit-retention setting, so none is added.
  - `short` may be lowered later, but never set longer than `long`.
- **Upgrade effect.** Terminal audit rows are kept forever today. The first hourly purge after the P2 upgrade deletes `terminal` rows older than 180 days. The release notes say so.
- **Down migration (SEC-029).** The P2 down migration never deletes audit rows. It re-adds the device foreign key as `NOT VALID`, so the rows of deleted devices survive a downgrade (§7.2).

The destructive or privileged actions are `reboot`, `shutdown`, `os_update`,
`agent_update`, `agent_uninstall`, `sync_privileges`, `docker_stop`,
`docker_update`, `docker_bulk_update`, `docker_stack_down`,
`docker_stack_redeploy`, `docker_stack_restart` and `docker_remove_items`.

**The purge never deletes an open row:** `ended_at IS NULL` and the row
started less than the session maximum ago (24 h for terminals, 4 h for log
viewers).

**No cascade.**

- The foreign key from `terminal_sessions` to `devices` is dropped.
- `DeleteDevice` writes a `device_delete` audit row first, with the actor, the remote address and `detail{hostname}`.
- `commands` rows still cascade with their device. The `command` audit rows keep the trail.

**Rationale:** one store, one query, and no new table. The table's columns
already fit, and the `kind` default keeps every existing writer valid.

### AD-025: Retention where none exists (A-03)

**Decision:** `runRetention` gains three bounded, batched purges. Each loops
`DELETE … WHERE ctid IN (SELECT ctid … LIMIT $batch)` until it deletes fewer
rows than the batch, or 30 s pass. The batch size adapts (rev 3.2, see the
self-healing rules below).

| Purge | Rule |
|---|---|
| `CommandRepo.PurgeTerminal(olderThan = now − 90 d)` | rows whose status is terminal (canonical terminal plus legacy aliases, via `SQLStatusList`) **and** `COALESCE(finished_at, updated_at) < $1`. `command_output` follows by cascade |
| `CommandRepo.PurgeOutput(olderThan = now − 30 d)` | `command_output.created_at < $1` |
| `TerminalRepo.Purge` | per retention class (AD-024): `long` rows older than 180 days, `short` rows older than 90 days, never an open row |

Container logs are not handled here. They move to AD-035.

**Upgrade effect (D-21, D-20).**

- On the first run after the P2 upgrade, `TerminalRepo.Purge` deletes terminal audit rows older than 180 days. Today these rows are kept forever.
- `CommandRepo.PurgeTerminal` deletes terminal-state commands older than 90 days. The long-class `command` audit rows keep the trail for 180 days.

**Self-healing (rev 3.1).** These rules apply to every purge this story adds.

- **When purges run.** Every purge runs once at server start, delayed by `RetentionStartupDelay` (5 min) so that it does not slow boot. After that it runs hourly.
- **How a purge deletes (adaptive in rev 3.2, SEC-037 item 2).**
  - Oldest rows go first, ordered by an indexed column: `idx_commands_created`, `idx_command_output_created` or `idx_terminal_sessions_started`.
  - **Adaptive batch.** A batch starts at `PurgeBatchMax` (5,000) rows.
    - A batch that hits `statement_timeout` (`PurgeStatementTimeout`, 10 s) is retried at half the size, down to `PurgeBatchMin` (1).
    - Each successful batch doubles the size again, up to 5,000.
    - A one-row batch that still times out is logged (Error, through the budget), and that purge ends for this run.
  - **Children before parents.** `CommandRepo.PurgeTerminal` selects a batch of command IDs.
    - It deletes their `command_output` rows first, in their own adaptive batches, and then deletes the commands.
    - The FK cascade then has nothing left to delete, so no single statement cascades into gigabytes of TOAST.
  - One purge spends at most 30 s per run. Each batch runs with `lock_timeout = 5 s`.
- **Catch-up.** Purges are idempotent. Unfinished work continues on the next run.
  - The backlog after any outage is bounded by the insert caps in §13A.
  - At full batch size it drains roughly 150,000 rows or more per purge per run. Under contention it drains more slowly, but it never stalls on one oversized batch.
  - No backlog can grow without limit, and no lock storm can occur.
- **Disk (rev 3.2; corrects rev 3.1).** Row DELETEs free **no** disk space.
  - They add WAL and dead tuples. Autovacuum then makes the space reusable only inside the same table.
  - Purges stop growth. They never shrink the volume.
  - Only partition drops (AD-035 §1b, AD-041) return space to the filesystem.
- **Failure.** A failed purge is logged through the log budget (§14.1 item 30) and retried on the next run. It never blocks ingest, and never blocks the other purges.
- **Stale alarm.** If any purge from this story has not completed successfully for `RetentionStaleAfter` (24 h), the server raises one `retention_stale` warning event, once per transition.
- **Tables with no time-based purge.**
  - `docker_stacks` is bounded by its 500-row cap, by Down-stack eviction and by the device cascade (AD-026).
  - `docker_inventory` keeps only the latest row.
  - The prune preview store sweeps expired previews every 60 s and on every access.
- Test: AC-138.

**Rationale and churn:** command creation is human- or policy-driven (§13.3).
These are low-churn tables, so DELETE retention is acceptable under NFR-003.
The 90-day horizon matches `events`, so Activity's two sources age out
together (D-20).

### AD-026: Known stacks (revised for SEC-002 and SEC-010)

**Decision.** New table `docker_stacks` (§7.2), keyed by
`(device_id, project)`. It stores the agreed paths, the observed paths and a
`path_state` (AD-038).

- **Observation.** `processTelemetryPostWork` calls `StackRepo.Observe(deviceID, []StackObservation{project, working_dir, config_files})` for every compose project in the snapshot. It is a single `INSERT … ON CONFLICT (device_id, project) DO UPDATE` statement, which also clears `forgotten_at`. The AD-038 rules decide which path columns and which `path_state` it writes.
- `Observe` writes only on change (paths, path state, or `forgotten_at IS NOT NULL`), or at most once an hour otherwise (`last_seen_at < $now − 1 h`). That gives about 24 writes per stack per day, plus changes.
- **Write budget (rev 3.1).**
  - An agent that flips paths on every push could force a write on every push. A per-device budget, `StackObserveWritesPerHour` (500), caps row writes.
  - Observations over the budget are skipped for the rest of that hour. Stored state is unchanged, and the next hour picks the observation up again.
  - The P3 migration sets aggressive autovacuum reloptions on `docker_stacks` (§7.2).
  - Test: `TestDUX_BND_StackObserveBudget`.
- **Bounds (SEC-010 item 1):**
  - at most 500 rows per device. Beyond that, new projects are ignored with a throttled Warn, unless the oldest row is a Down stack unseen for more than 90 days, in which case that row is evicted;
  - `project` must match `ComposeProjectPattern`;
  - paths must be absolute, at most 1,024 bytes, with no control characters;
  - at most 10 `config_files`;
  - at most 20 `conflict_members`.
- An observation that fails these checks is treated as `conflicted`.
- **Forget.** `POST …/forget` sets `forgotten_at`. It is allowed only when the stack is Down: no running container in the latest snapshot. Otherwise it returns `409 {"code":"not_down"}`. A later observation clears it (FR-117).
- **State** (FRD §8.10) is computed at read time from the latest snapshot and the record. A known record with no containers is Down (BR-015).
- **Deletion.** Rows are removed by `ON DELETE CASCADE` when the device is deleted. Apart from that and the eviction bound above, nothing deletes them (D-11).

### AD-027: Stack actions (revised for SEC-002 and §2.5)

**Decision.** The new agent actions are listed below. Each is a lifecycle
command with class `docker_stack`: TTL 60 m, exec deadline 35 m, agent hard
max 29 m 55 s, the same budget as `docker_update`. Each is added to
`validActions`, `models.ActionClass`, the `ExecDeadline` table, and the
`labels.ts` `commandAction` map.

| Action | Agent steps | compose argv (via `composeStackArgs`, single tokens, `--` before services) |
|---|---|---|
| `docker_stack_pull` | precheck, pull | `-p=<project> --project-directory=<wd> -f=<file>… pull` |
| `docker_stack_redeploy` | precheck, stop_dependents, recreate, verify, restart_dependents | `… up -d --force-recreate --no-build --pull=never` |
| `docker_stack_restart` | precheck, stop_dependents, restart, verify, restart_dependents | `… restart` |
| `docker_stack_down` | precheck, down | `… down` (**never** `-v`, `--volumes`, `--rmi`, or `--remove-orphans`; a unit test asserts the exact argv) |
| `docker_stack_up` | precheck, up, verify | `… up -d --no-build --pull=missing` |
| stack Update | unchanged | the existing `docker_update{compose_work_dir}` (CMD-LIFECYCLE) |

- **Params.** `{project, working_dir, config_files}`. The server takes them from `docker_stacks.agreed_*`, never from the client (FR-118).
- **Dependents.** The CMD-LIFECYCLE dependents stop and restore (`docker_deps.go`) wraps Redeploy and Restart (FRD §8.11).
- **Results.** The result carries `target_results` per service and the AD-023 `steps`.
- **Server dispatch.** `POST /api/v1/devices/{id}/docker/stacks/{project}/actions` (§8.7) builds the AD-007 request with every member's container key. A conflict returns 409 with the existing command, and the page attaches to it (FR-113, AC-080).
- **Capability.** Without `stack_actions_v1`, every action except Update returns `409 agent_unsupported`, and the UI disables them (FR-125, AC-086).

**Argv (§2.5).** The new `composeStackArgs(project, wd, files []string, sub, services...)`
sits in `docker_update.go`, next to the merged `composeArgs`, and reuses its
service-name validation and the `--` separator. It emits single tokens:

- `-p=<project>`;
- `--project-directory=<wd>`;
- one `-f=<file>` per agreed file. When the agreed record has no `config_files`, no `-f` is passed and compose discovery runs in `wd`.

`composeArgs` is unchanged.

**Precheck order (SEC-002).** This also applies to stack Update and to
`docker_update{compose_work_dir}`.

1. The params carry the **agreed** paths. The server returns `409 stack_paths_unagreed` unless `path_state='agreed'`.
2. The agent checks that live members agree with the params (`conflicted`), and that its registry's last agreed entry equals them (`paths_changed`).
3. The writable-path check (AD-038) runs for every compose invocation.
4. The `stackfs` checks and the no-race open (AD-021) run for every `-f` file, and `wd` passes the outer bound and denied-roots check.
5. The AD-007 locks are taken.

**Paths shown.** The Redeploy, Update and Down dialogs, and the stack
header, show the agreed paths. The header covers Pull and Up.

### AD-028: Inventory collector (revised for SEC-010)

**Decision (OQ-03):**

- **Collector.** `collectors/docker_inventory.go`, named `docker_inventory`
  (the FRD name is kept). It is registered by `RegisterDefaultsWithDocker`.
  It only runs when listed in `collector.enabled` (opt-in, D-6, NFR-015).
- **Cadence.**
  - It holds `lastRun`. `Collect` returns `nil` (no field in the telemetry)
    unless `now − lastRun ≥ 15 m` or a forced refresh is pending.
  - A `sync.Mutex` taken with `TryLock` guarantees that two runs never
    overlap (NFR-007). A concurrent call returns nil.
  - The whole run has a 25 s context (NFR-007: within 30 s).
- **Collection** (docker socket only):
  - `ImageList(All:false)`;
  - `VolumeList`;
  - `NetworkList`;
  - `ContainerList(All)` for references;
  - `DiskUsage(Types: images, volumes, build-cache)` under a 15 s
    sub-budget. If it times out, volume sizes are omitted and
    `sizes_available=false`.
- **Shape.** `models.DockerInventory` (§7.3), with bounds of 2,000 images,
  2,000 volumes and 500 networks. Items are sorted (images by created
  descending, volumes and networks by name), the bound is applied, and
  `truncated.images` and the other flags are set (FR-130).
- **Change detection.** `hash` is the SHA-256 of the canonical JSON. When
  the hash equals the last one sent, the agent sends
  `{collected_at, hash, unchanged:true}` only.
- **Forced refresh.** The command `docker_inventory_refresh` (class
  `other`) sets the forced flag, at most once per 60 s; otherwise it fails
  `precheck` with `refresh too frequent`. It then calls `triggerTelemetry()`
  and succeeds when the next collection completes, or fails if it errors.
- **Server ingest.** `Telemetry` extracts `snap.Data.DockerInventory`
  **before** `StoreSnapshot`, as it does for `Logs`, and sets it to nil, so
  inventory never enters `telemetry_snapshots`.
  - The post-work applies the server bounds below, then calls
    `InventoryRepo.Upsert(deviceID, inv)`.
  - An unchanged payload updates `collected_at` only.
  - A changed payload replaces `data`, subject to the cadence rule below.
- **Enabled flag.** `DockerInfo.inventory_enabled` is set by the agent from
  its registry (true when the collector is in the filtered list). It is only
  meaningful with `inventory_v1` (FR-136).
- **Doctor (NFR-015, the MEMORY doctor-sync rule).**
  - `doctor.go` adds `docker_inventory` to `allCollectors`.
  - The Docker section runs when any of `docker`, `container_logs` or
    `docker_inventory` is enabled.
  - It **pings the socket** (2 s timeout) and prints pass or fail with the
    socket path.
  - It prints `docker_inventory: enabled` or
    `disabled (add "docker_inventory" to collector.enabled)`.
- **README.** Collector table, the exact config line, and the `.env`
  readability note.
- **Release notes** must give the exact line operators add:
  `collector.enabled: [..., docker_inventory]`.
- **Installer.** `install.sh`'s default whitelist is **unchanged**
  (opt-in).

**Privilege (NFR-014).** This uses docker socket group membership only, the
same access the `docker` collector already has. There is no sudo and no new
sudoers rule.

**Why not the snapshot.** Riding the snapshot would put tens of KB to 2 MB
per host per interval into `telemetry_snapshots` (§13.3), which repeats the
PART-001 bloat pattern.

**Server bounds (SEC-010)** (`inventory_ingest.go`):

| Bound | Limit |
|---|---|
| Item counts | 2,000 images, 2,000 volumes, 500 networks |
| Tags or digests per image | 32 |
| `UsedBy` per volume | 64 |
| Containers per network | 256 |
| Subnets per network | 16 |
| Any string | 512 bytes |

Truncation sets flags in the stored inventory.

- **Size cap: 4 MiB serialized.** Anything larger after truncation is rejected for that push, with a throttled Warn. The snapshot is still accepted.
- **Cadence.** A *changed* payload is accepted at most once per 14 minutes per device. The exception is the 2 minutes after a server-issued `docker_inventory_refresh`. Outside that, a changed push updates nothing.

### AD-029: Prune and remove (revised for SEC-007)

**Decision:**

- **One action, `docker_remove_items`.** Params:
  `{kind:"image"|"volume"|"network", items:[…], preview_id}`, with the item
  shapes below. Docker's `ImagesPrune`, `VolumesPrune` and `NetworksPrune`
  APIs are **never** called, because they remove unlisted items (BR-008).
- **Preview** (`POST …/docker/prune/preview`, §8.8):
  - the server computes candidates **from the latest inventory**:
    - `images` (dangling only) or `images_all` (unused);
    - `volumes` (unused);
    - `networks` (unused, never built-in);
    - `selected` (explicit items, which must currently be unused).
  - It stores the preview (bound as described below, plus
    `created_at` and `inventory_collected_at`) in an in-memory map: TTL
    10 minutes, at most 50 previews server-wide, oldest evicted.
  - It returns the items with `selected_default` (volumes: anonymous
    selected, named not selected, D-5), the estimated size, the inventory
    age, `requires_refresh` (when the age is over 15 minutes), and the
    typed-confirmation requirement.
  - Built-in networks (`bridge`, `host`, `none`, and the ingress network)
    are filtered out on the server **and** refused by the agent (FR-140).
- **Execute** (`POST …/docker/prune/execute`). It validates:
  - the preview exists, has not expired, and belongs to the device and the actor;
  - `selected ⊆ preview.items`;
  - the current inventory age is at most 15 minutes, else
    `409 inventory_stale`;
  - the typed confirmation (AD-009).

  It then `Dispatch`es `docker_remove_items` with stored key
  `docker:host` and `DockerExclusive=true` (AD-007). A conflict returns 409
  with the blocking command (FR-144, AC-095).
- **Agent** (`internal/agent/docker_remove.go`).
  1. Precheck the `docker:host` lock (AD-007).
  2. Re-check usage once: `ContainerList(All)` gives image IDs, volume mount
     names and network attachments in use.
  3. For each item, in order:
     - built-in network: `Skipped: built-in`;
     - still in use: `Skipped: in use`;
     - identity changed (below): `Skipped: changed`;
     - otherwise remove: `ImageRemove(id, Force:false, PruneChildren:false)`
       after untagging each listed tag; an unexpected new tag skips the item
       as in use. `VolumeRemove(name, force=false)`. `NetworkRemove(id)`.
  4. A Docker conflict error maps to `Skipped: in use`. Other errors map to
     `Failed: <message>`.
  5. Each item is bounded to 30 s. When the action budget (9 m) is near,
     the remaining items are `Skipped: time budget`.
- **Results (rev 3.1).**
  - `target_results` holds at most `TargetResultsMax` (100) per-item entries. One summary entry follows: `{"container":"*","outcome":"summary","message":"{\"removed\":n,\"skipped\":n,\"failed\":n,\"bytes_reclaimed\":n,\"truncated\":n}"}`.
  - The full per-item list goes to the `steps` output. That output is capped at 64 KiB and shows "Output truncated" beyond it.
  - A prune of 2,000 images therefore never writes more than these caps.
  - The result is recorded in Activity (FR-143).
  - Test: `TestDUX_BND_PruneResultsTruncated`.
- **Volume safety (FR-145, BR-007).**
  - No other action removes volumes: `down` never passes `-v` (AD-027), and
    image and network removal never touches volumes.
  - The standalone update's `ContainerRemove` (CMD-LIFECYCLE) is called
    without `RemoveVolumes`. A test asserts it.

**Preview IDs.** 16 bytes from `crypto/rand`, written as 32 hex characters.

- **Bound to:** the device, the kind, the item set with identity data, the actor's sub, and an expiry of 10 minutes.
- **Consumed on first use, whatever the outcome.**
- Test: `TestDUX_SEC007_PreviewIDEntropyAndSingleUse`.

**Images.** Each item is `{id, tags[]}`.

- Before untagging a tag, the agent calls `ImageInspect(ref)`. If the result is no longer `id`, the item is `Skipped: changed`.
- The image ID is removed only after every tag has been verified and untagged.
- Test: `TestDUX_SEC007_TagMovedToOtherImageNotRemoved`.

**Volumes.** Each item is `{name, created_at}`.

- If `VolumeInspect(name).CreatedAt` differs, the item is `Skipped: changed`.
- Test: `TestDUX_SEC007_RecreatedVolumeNotRemoved`.

**Preview store bounds (rev 3.2, B-12).**

- The store holds at most `PreviewMaxServer` (50) previews. The 51st evicts the oldest.
- A sweep every 60 s, and on every access, removes expired previews.
- Test: `TestDUX_BND_PreviewStoreCapAndSweep`, with an injected clock.

**Superseded images (D-52; WP-4A; opt-in, default off).**

- **Setting.** `docker.prune_superseded_images` in `agent.yaml`, host-local, default `false`.
  - The server has no switch, so a compromised server cannot turn it on.
- **When.** After any of these succeeds:
  - a `docker_update`, standalone or compose;
  - one target of a `docker_bulk_update`;
  - a `docker_stack_redeploy`.

  For every container whose image ID changed, the agent records the previous image ID before the update.
- **What.** After `verify`, the agent runs a `cleanup_image` step.
  1. It re-checks usage once with `ContainerList(All)`.
  2. If no container uses the previous ID and that image has no remaining tags, it calls `ImageRemove(previousID, Force:false, PruneChildren:false)`.
  3. Otherwise the outcome is `Skipped: in use` or `Skipped: tagged`. A Docker conflict also maps to `Skipped: in use`.
  - It never calls `ImagesPrune`, never forces a remove, and never touches volumes (BR-008).
- **Failure.** A failed removal never fails the update. The step outcome records it, and the P4 prune can reclaim the image later.
- **Visibility.** `riot-agent doctor` prints the setting. With the setting off, the inventory (AD-028) shows the reclaimable size.
- **Why P4.** It reuses WP-4A's per-item removal and identity checks. Shipping it in P1 would duplicate that code, without the inventory to show the effect.
- Test: AC-161.

### AD-030: I/O history (FR-107), gated on PART-001

**Decision (WP-2D only):**

- `container_metrics` gains four nullable `DOUBLE PRECISION` columns:
  `net_rx_rate`, `net_tx_rate`, `blk_read_rate`, `blk_write_rate`.
  - `ALTER TABLE … ADD COLUMN` on the partitioned parent is metadata-only
    and propagates to the partitions.
  - The ingest `CopyFrom` adds the columns.
- `GET …/containers/{cname}/metrics` returns them when non-null.
- The Stats tab renders the I/O charts only when at least one returned point
  has a non-null rate. With PART-001 unmerged, no column exists and no chart
  is drawn (AC-075).

### AD-031: Demo mode

**Decision:**

- Every new API method is added to `api/client.ts` and to `demo-client.ts`
  (UI-FOUNDATION typing enforces parity).
- `demo-data.ts` gains:
  - two hosts (`host-a`, `host-b`) with Docker data, including stacks
    `media` and `infra`, an exited container with code 137, an unhealthy
    container, and updates;
  - a generator for 2,000 containers (`demo.bigFleet()`, used by the
    performance guard);
  - inventory with dangling and unused items;
  - Activity entries with steps;
  - a redacted inspect document;
  - a redacted compose file;
  - a simulated stored history: 7 days, with gap markers and the retention
    text.
- **Simulated live hub** (returned by `api/liveLogs.ts` under
  `VITE_DEMO`, AD-018). It respects the boundary, and generates 2 lines/s,
  a stderr line every 10, and an `exited` marker after 2 minutes, with no
  network access (NFR-018, AC-103).
- Demo actions resolve with simulated commands and do no network access
  (NFR-018).

### AD-032: Migrations

**Decision.** Seven pairs, each numbered next-free under the §2.4 rules:

| Migration | Work package | Ordering constraint |
|---|---|---|
| `docker_ux_p0_log_name_index` | WP-P0C | after the renumbered PART-001 files |
| `docker_ux_p0_log_agent_ts` | WP-P0B | none: before or after PART-001 (§2.4 rule 6) |
| `docker_ux_p1_bulk_results` | WP-1B | none |
| `docker_ux_p2_activity` | WP-2B | none |
| `docker_ux_p3_stacks` | WP-3B | none |
| `docker_ux_p4_inventory` | WP-4B | none |
| `docker_ux_io_history` | WP-2D | after PART-001 |

All seven are reversible. The down migrations drop only what the up added, and never delete audit rows (SEC-029).
The implementation report records the number taken and the `main` head it
was taken against. Contents are in §7.2.

### AD-033: Test strategy

**Decision:**

- **Go test naming.** `TestDUX_AC0nn_<Summary>`, `TestDUX_NFR0nn_<Summary>` and `TestDUX_SEC0nn_<Summary>`. PG-gated tests use the CMD-LIFECYCLE `pgTestURL`.
- **Web test naming.** Describe titles `[DUX AC-0nn] …`. Use `renderWithProviders`.
- **Clocks.** Every time rule uses an injected clock (NFR-020). There is no `time.Now()` in `livelogs`, `agentrpc`, the sort freeze, the hidden-stop logic, the preview store, the inventory gate or the server ceiling's rolling limiter.
- **AC-100.**
  - (a) `web/src/utils/dockerChips.perf.test.ts` filters, counts and sorts 2,000 demo rows. It asserts under 50 ms per operation, and logs the timing.
  - (b) The implementation report records a manual production-build measurement on the demo build with 2,000 containers. It uses the browser Performance panel: type a search term, toggle a chip, record the interaction-to-paint time. The pass bar is under 200 ms.
- **AC-101.**
  - The keyboard half is automated in vitest.
  - The scan half uses `axe-core` in vitest when D-24 is approved (`web/src/test/a11y.test.tsx` runs `axe.run` on each page, `impact in [serious, critical]` gives 0).
  - Otherwise, the implementation report records a manual axe browser extension scan of each page.
- **AC-107 and AC-109:** PG tests with PART-001's manager and an injected clock.
- **AC-107, static check:** `container_log_repo.go` has no `Purge`, and no `DELETE FROM container_logs` appears outside down migrations.
- **Boundary and identity:** table tests in `logBuffer.test.ts`.
- **Static guards.** The UI-FOUNDATION guard is extended to `components/docker/**`, `pages/docker/**`, and the new `ui` primitives (AC-099). A new `docs-fixtures` guard scans the files added by this story, including the story's Go `testdata` and its docs, for private IPv4 ranges and email patterns (AC-105).

### AD-034 (new): Request-origin control and anti-framing (SEC-001)

**Decision.** New file `internal/server/middleware/sameorigin.go`.

**`SameOriginJSON`.** Applies to every POST, PUT, PATCH and DELETE that this
story adds or modifies.

1. `mime.ParseMediaType(Content-Type)` must be `application/json`, and a body is required (clients send `{}` when they have nothing else).
2. Either `Sec-Fetch-Site: same-origin`, or `Origin` equal to `RequestScheme(r) + "://" + r.Host`.
3. The request is rejected when both headers are absent, when `Sec-Fetch-Site` holds any other value, or when the origin does not match.

A rejection returns `403 {"code":"origin_refused"}` **before** the body is
decoded, so no side effect, audit row or rate-limit use happens.

**`RequireRiotHeader`.** Applies to side-effecting GETs. It requires
`X-Riot-Request: 1` and the same origin test. `cors.go` never allows
`X-Riot-Request` for foreign origins. Configured `AllowedOrigins` count as
trusted, which is documented.

**`NoFraming`.** A global middleware, first in `setupRouter`. On every
response it sets `Content-Security-Policy: frame-ancestors 'none'` and
`X-Frame-Options: DENY`.

**Scope.**

- **Mandatory:** every route in §8, including the modified `POST /devices/{id}/commands`, and the new WebSocket (Origin required).
- **D-30 (b),** which the review recommends, would extend `SameOriginJSON` to every existing state-changing admin route. That is a one-line change to a router group. The ADD implements (a) unless the product owner chooses (b).

**Client.** `mutateJSON` always sends JSON. `getWithSideEffect` adds
`X-Riot-Request: 1`. The README says that non-browser scripts must send
`Origin` and JSON.

**Tests (C-1):**

- `TestDUX_SEC001_TextPlainPostRejected`
- `TestDUX_SEC001_SameSiteCrossOriginRejected`
- `TestDUX_SEC001_SameOriginAccepted`
- `TestDUX_SEC001_SideEffectGETsRequireHeader`
- `TestDUX_SEC001_FrameAncestorsNone`
- a router matrix check that every new route has this middleware.

### AD-035 (new): Stored-log rotation (P0; OQ-07 and OQ-08)

**1. Partitioning (FR-148, FR-150, FR-156, NFR-024, NFR-025).**

- Delivered **only** by PART-001's `container_logs` conversion, renumbered per §2.4. No second scheme is introduced.
- PART-001 drops a day partition once its upper bound is at or before `now − N`. That always keeps N full days plus the current day.
- PART-001 AD-006 removes `ContainerLogRepo.Purge`.
- FR-156 is met by PART-001's O(1) metadata swap, which runs before the server serves traffic. Agents buffer telemetry during the window (`buffer.go`) and resend afterwards (AC-114).

**1a. Timestamp window and re-stamp (D-42, settled; SEC-021).**

*Where out-of-window timestamps come from.* The product owner asked how
these arise and whether log data could be lost. The realistic sources are:

- **No real-time clock.** A Raspberry Pi or another single-board computer has no RTC. It boots at the epoch, or at the last shutdown time, until NTP syncs. Its early-boot container output therefore carries 1970 or a stale time.
- **A dead CMOS or RTC battery,** on any host.
- **An offline backlog.** After a long offline period, the agent's telemetry buffer flushes lines that are older than the oldest live partition.
- **A container clock problem.** A container can have its own clock, time-zone or faketime bug. Docker stamps each log line with the daemon's clock on receipt, so such a bug changes the line text, not the stored timestamp. It matters only when the host clock itself is wrong.
- **A misbehaving or forged agent** (the security case).

*Rule.* `container_log_window.go::applyWindow(entries, gaps, receivedAt, retention)`
applies this to every stored line and every gap row at ingest. It runs
**before** the ceiling, masking and `CopyFrom`.

- **The window** is `[receivedAt − effective_retention, receivedAt + LogTimestampFutureTolerance]`. The tolerance is 5 min, the merged `models.TelemetryFutureTolerance`.
- **An in-window row** is stored as today: `timestamp` holds the agent's time, and `agent_ts` is NULL.
- **An out-of-window row is re-stamped, never dropped.**
  - `timestamp`, the partition key, becomes `receivedAt` (server clock, microsecond precision).
  - `agent_ts` keeps the agent-supplied time verbatim.
  - `agent_ts IS NOT NULL` is the clock-skew flag (§7.2).
- **Order inside one ingest.** Re-stamped rows from one ingest keep their relative order: the row's index within the ingest is added to `receivedAt` as microseconds. The per-device ceiling bounds this offset to at most 20,000 µs. History queries order by `timestamp, id`.
- **Old backlog lines.** Lines older than retention are kept, flagged, for one more retention period from the time they arrive. No line is ever discarded for its timestamp.
- **Consequence.** An agent clock can never put a container-log row into PART-001's never-dropped DEFAULT partition. A future-dated row can therefore never block creation of a day partition (SEC-021 item 1).
- **Tests:**
  - `TestDUX_SEC021_FutureAndPastDatedLogRowsNotStoredOutOfWindow` asserts the re-stamp: `timestamp` = receive time, `agent_ts` = the original time;
  - `TestDUX_SEC021_GapEntryTimestampWindow`;
  - a PG test proving that a forged future row cannot prevent the next day's partition from being created (WP-P0C).

*Skew accounting (WP-P0B).*

- **Counters.** The server keeps in-memory counters per device:
  - re-stamped lines over the last 24 h, as a rolling count in hourly buckets;
  - the last observed skew, as agent time minus receive time, in signed seconds;
  - the time of the last re-stamp.

  `GET /docker/hosts` exposes them as `log_clock_skew` (§8.3).
- **Event.** When a device has re-stamped lines in 3 consecutive ingests, the server emits one `clock_skew` event.
  - It is a warning, scoped to the device. The message gives the skew in whole seconds and contains no log content.
  - It follows the once-per-transition pattern of `CheckRebootRequired`.
  - It clears after 1 h of clean ingests, and can fire again later.
  - No alert rule is seeded. Operators can add one.
- **NFR-022.** Info `container log lines re-stamped` (device, count, skew seconds), throttled per device.

*Defence in depth (SEC-021 item 4; WP-P0C).* WP-P0C wires PART-001's health
`DefaultRowCount` for `container_logs`. A non-zero count emits a Warn, plus
one `log_partition_default_rows` warning event per transition. It stays in
place even though re-stamping should keep the DEFAULT partition empty.

**1b. Partition janitor and self-healing (rev 3.1; WP-P0C; extends PART-001's partition manager for `container_logs`).**

PART-001's manager design stays the base. These rules are additive.

1. **Ensure partitions (amended in rev 3.2, SEC-037 item 4).**
   - **At startup, before serving.** The janitor creates every missing day partition in `[today − effective retention, today + PartitionAheadDays (3)]`, within `PartitionJanitorStartupBudget` (60 s).
     - That is at most `PartitionStartupEnsureMax` (94) creates of empty tables, while nothing else holds locks.
     - Backlogs that agents flush after a server outage then land in existing partitions instead of triggering DDL on the hot path.
   - **Every hourly run** creates any missing partition in `[today − 1, today + 3]`.
   - Each create is `CREATE TABLE IF NOT EXISTS … PARTITION OF`, with `lock_timeout = 5 s`.
   - If startup fails or runs out of budget, the janitor logs it and the server serves anyway. Rule 4 covers ingest in the meantime.
2. **Drop expired partitions without stalling ingest (rewritten in rev 3.2, SEC-037 item 3).**
   - **What is dropped.** Each run lists, oldest first:
     - the partitions whose upper bound is at or before now − effective retention, including the legacy partition;
     - the partitions that AD-041 marks for early eviction.
   - **Limits.** At most `PartitionDropsPerRun` (24) drops, and at most `PartitionDropRunBudget` (20 s), per run.
   - **Method: chosen each run.** The janitor checks two conditions: the server is PostgreSQL 14 or later (`server_version_num`; the stack runs 16), and the parent has no DEFAULT partition (`pg_partitioned_table.partdefid = 0`). When both hold:
     1. It first finishes any interrupted detach. A partition with `pg_inherits.inhdetachpending` gets `ALTER TABLE … DETACH PARTITION … FINALIZE`.
     2. It runs `ALTER TABLE container_logs DETACH PARTITION <p> CONCURRENTLY`, outside any transaction block, with `lock_timeout = 1 s` (`PartitionDropLockTimeout`) and `statement_timeout = 60 s` (`PartitionDetachStatementTimeout`). This holds only `SHARE UPDATE EXCLUSIVE` on the parent, so `COPY` and reads continue.
     3. It runs `DROP TABLE <p>` on the now-standalone table. That needs no lock on the parent.
   - **Fallback.** PostgreSQL refuses `DETACH … CONCURRENTLY` while the parent has a DEFAULT partition, and PART-001 keeps one. PostgreSQL 13 and older lack the option entirely. In those cases the janitor runs `DROP TABLE IF EXISTS <p>` with `lock_timeout = 1 s`.
     - This takes `ACCESS EXCLUSIVE` on the parent, for the catalog change only.
     - While it waits, new readers and `COPY` queue behind it for at most 1 s.
     - D-53 records the choice between the two methods.
   - **Stop at the first lock timeout.**
     - The run ends there, and the rest waits for the next hourly run. There is no tight retry loop.
     - A lock timeout is a normal outcome, not a failure, so it does not trigger the rule 3 backoff.
     - If 24 consecutive runs end on a lock timeout, the stale-janitor event fires (rule 5).
   - **Idempotent.** Drops use `IF EXISTS` and `FINALIZE`.
   - **Bounded backlog.** The backlog can never exceed the partitions that exist (at most 96), so at most four runs clear any outage or any cut in retention.
3. **Retry with backoff; never block ingest.**
   - A failed run is retried after 1, 2, 4, … minutes, capped at 60 minutes.
   - The janitor runs in its own goroutine. Ingest never waits on it.
4. **Ingest partition guard.** Before `CopyFrom`, `partitionGuard.EnsureDays(days)` checks each distinct UTC day in the batch against an in-memory set of existing partitions. Every janitor run and every on-demand create refreshes that set.
   - **Missing partition (server-wide in rev 3.2).** The guard creates it on demand, with `lock_timeout = 2 s`.
     - Creates go through a **server-wide** `singleflight` keyed by day (`golang.org/x/sync/singleflight`, already a direct dependency), plus a per-day last-attempt time. Across all ingest goroutines, there is at most one attempt per day per 60 s.
     - Other callers wait for that attempt's result. Within the 60 s after a failed attempt, they go straight to the fallback.
     - The last-attempt map holds only days inside the window (at most 96 entries) and is pruned outside it (B-43).
     - After rule 1's startup ensure, this path is rare.
   - **On-demand create fails.** The affected rows are re-stamped into the newest existing in-window day partition:
     - `timestamp` becomes that partition's last microsecond, minus the row's ingest index;
     - `agent_ts` keeps the agent's time, unless it is already set;
     - the rows show the clock-skew marker.
   - **Yesterday's partition (rev 3.2).** When today's partition cannot be created, the newest existing one can be yesterday's.
     - Those rows then appear before today's rows, and rotate one day early.
     - They carry the clock-skew marker, so this degraded mode is visible. AC-135 asserts it.
   - **No in-window partition exists at all.** The rows are dropped and counted in `log_ingest.rejected_24h`, an Error is logged through the log budget, and the stale-janitor event fires.
   - **Rows never go to the DEFAULT partition from this story's ingest.**
5. **Stale-janitor event.**
   - The server raises one `partition_janitor_stale` warning event, once per transition, in either case:
     - no janitor run has succeeded for `PartitionJanitorStaleAfter` (6 h);
     - 24 consecutive runs have ended on a lock timeout.
   - The next run that completes clears it.
   - It also logs an Error, which feeds the existing server-error alert on purpose.
6. **Tests:**
   - AC-134: the startup ensure over the retention window, then bounded catch-up;
   - AC-135: the ingest guard, including the yesterday fallback;
   - AC-136: the stale event;
   - AC-151: drop lock behaviour, with both methods;
   - AC-152: the startup backlog ensure and the server-wide singleflight.

**2. Retention setting (FR-149, FR-151, D-34).**

- **`RIOT_CONTAINER_LOG_RETENTION_DAYS`,** parsed by the pure `parseContainerLogRetention(raw) (days, warn)`:
  - unset: 7;
  - an integer from 1 to 90: that value;
  - anything else: 7, plus a startup Warn naming the setting and the value used (AC-108).
- **Applying the setting.** WP-P0C replaces PART-001's constant 7 for `container_logs` with `Config.ContainerLogRetentionDays`. The manager is built in `server.Start`, so after a restart the next hourly run applies the value. Lowering it drops days. Raising it restores nothing (AC-109).
- **Effective value (OQ-08).** `Server.EffectiveContainerLogRetention()` returns the setting once the manager has `container_logs` registered, and 7 before that. The history API (AD-036) exposes it as `retention_days` and `oldest_retained_at`.
  - **Rev 3.2.** The fleet storage budget or the disk-headroom guard may drop days early (AD-041). In that case the function returns the number of whole days actually kept.
  - The history API then adds `retention_limited_by`: `storage_budget` or `disk_headroom`. It is empty otherwise.
- **Startup Info.** When the setting and the effective value differ, the server logs "container log retention setting takes effect after partitioned rotation".
- **NFR-022.** Every manager run that drops container-log days logs `container log retention run` with `days_removed` and `oldest_day_kept`.

**3. Agent caps (FR-152, D-33; WP-P0A).**

- **Configuration:**
  - `collector.container_logs.max_lines`: default 50, accepted 1 to 1,000;
  - `collector.container_logs.max_bytes`: default 16,384, accepted 1,024 to 262,144.

  An invalid value falls back to its default, with a Warn.
- **Per container, per collection,** output flows through the pure `capRing`:
  - each line is truncated to 16 KiB at a rune boundary, with ` [truncated]` appended (D-40);
  - the ring keeps the **newest** lines that fit both caps;
  - every line it evicts is counted.
- **Scan bound (D-41):** at most 20,000 lines and 8 MiB per container per collection. Beyond that, the count is a lower bound.
- **Gap field.** `FullTelemetryData.ContainerLogGaps` (`container_log_gaps`) is additive. Each entry is `{container_id, container_name, timestamp, count, lower_bound, reason:"agent_cap"}`, where `timestamp` is that of the newest dropped line.
- **Collector state.** `DockerInfo.container_logs_enabled` is set.

**4. Stopped and TTY containers (FR-154, FR-155; WP-P0A).**

- The collector now lists **all** containers. It reads running containers, plus any container whose `State.FinishedAt` is later than the previous collection, with `Since = lastSeen` (or the previous collection time) and `Until = FinishedAt + 1 s`. It then removes that container's `lastSeen` entry.
- `Config.Tty` is cached per container ID.
- TTY output goes through the raw reader and is stored as `stdout`, without stream headers.
- The frame reader is bounded to 16 KiB plus 4 KiB per line. Frames over 1 MiB no longer abort the read.

**5. Server ceiling and ingest validation (FR-153; WP-P0B; SEC-023).**

`container_log_ingest.go` runs for every agent, in this order:

1. validate;
2. window and re-stamp (§1a);
3. ceilings;
4. mask (§6);
5. `InsertBatch`.

Masking never runs before the ceilings.

1. **The server owns `stream`.**
   - An ordinary entry must have `stream` set to `stdout` or `stderr`. Anything else is dropped and counted.
   - Gap rows (`gap_agent`, `gap_server`) are created **only** by server code, from validated gap entries or ceiling counts. They are never copied from an entry.
2. **Entry validation.**
   - `container_name` must match `ContainerNamePattern`.
   - `container_id` must be 12 or 64 hex characters.
   - Each line is truncated to 16 KiB.
   - Invalid entries are dropped and counted.
3. **Gap entry validation.** Invalid gap entries are dropped.
   - At most one gap entry per container per push. Extra entries for the same container are dropped.
   - `container_name` must match `ContainerNamePattern` and must name a container in this push's `docker.containers` list.
   - `count` must be an integer from 1 to 10,000,000.
   - `reason` must be `agent_cap`.
   - The timestamp goes through §1a.
   - A push may carry at most as many gap entries as its docker list has containers.
4. **Known-container rule.**
   - Some entries and gap entries name containers that are not in this push's `docker.containers` list. The agent lists all containers, so a container that has just exited is still present.
   - Those entries share a per-device **unknown budget** of 200 lines and 64 KiB per 60 s. Beyond that budget they are dropped and counted.
5. **Per-container ceiling.**
   - Keep the newest lines within 1,000 lines and 256 KiB per container per ingest.
   - On top of that, a rolling per-(device, container) limit of the same size applies per 60 s, with an injected clock.
   - **Limiter map cap (rev 3.1).** The limiter map holds at most `StoredLogContainerLimiterMax` (512) containers per device. The least recently used entry goes first, and idle entries are evicted after 10 min.
   - An evicted container falls back to the per-device ceiling, so eviction never raises the allowance. Test: `TestDUX_BND_ContainerLimiterLRU`.
6. **Per-device ceiling.**
   - A rolling per-device limit applies per 60 s: `min(N, 20) × 1,000` lines and `min(N, 20) × 256 KiB`. N is the number of containers in this push's docker list, minimum 1.
   - **Tightened cap (rev 3.1, D-46).** The limit is capped at 5,000 lines and 2 MiB per 60 s. It was 20,000 lines and 5 MiB.
     - The default agent caps (50 lines and 16 KiB per container per minute) fit under it for up to 100 containers per host.
   - **Gap rows** have their own per-device bound, `StoredLogGapRowsPerDevice` (200 per 60 s). Drops beyond it are counted only in `log_ingest.rejected_24h`.
   - Rotating container names therefore cannot multiply the allowance (SEC-023 item 3).
   - **Device quota (rev 3.2, D-50; reshaped in rev 3.3, D-54, SEC-039).** After the ceilings, the surviving lines draw from the device's `logs` bucket (AD-041 §1).
     - The bucket's capacity is 25 % of `DeviceDailyLogBytes` (512 MiB), and it refills continuously at 512 MiB per 24 h. Each line counts its bytes plus 200 B per row.
     - It **degrades instead of stopping:**
       - **Normal** (bucket above 25 % of its capacity): every line that passed the ceilings is stored.
       - **Sampled** (bucket at or below 25 % of capacity, that is, more than 75 % of the allowance used): *priority lines* are stored first. These are every stderr line, and every stdout line that matches `ErrorLinePatterns`. Other stdout lines are sampled: the first `StdoutSampleLinesPerContainerPerMinute` (5) per container per minute.
       - **Never zero.** The bucket refills every second, so a device in sampled mode keeps storing about 364 KiB per minute at the defaults, priority lines first.
     - **Fair share.** In sampled mode, the tokens available to one ingest are split max-min fairly across its containers. No container gets less than an equal share while another takes more than its own demand.
     - **Recording.** Lines not stored are counted into one `gap_quota` row per container. `gap_quota` is a new server-owned `stream` value, and the history API reports it with reason `quota_sampled` (AD-036).
     - **Hysteresis.** Sampled mode ends when the bucket is back above 50 % of capacity.
     - Gap rows do not draw from the bucket. This rule's gap-row bound holds all three gap kinds.
     - Priority classification reads the line before masking, in memory only.
7. **Accounting of drops.**
   - Lines from a known container dropped by rules 5 and 6 are counted into one `gap_server` row per container. In that row, `line` holds the count, with a `+` suffix for a lower bound, and `timestamp` is the marker's position.
   - Drops under rules 1 to 4 have no valid container to attach a gap row to. They are counted only in the device's `log_ingest.rejected_24h` counter (§8.3) and in the NFR-022 log.
8. **Agent gaps** become `gap_agent` rows the same way.

Gap rows live in the same partitioned table and rotate with it (**OQ-07**).
The existing cap of 20,000 rows per push stays as an outer bound.

NFR-022 adds the log `container log ceiling applied`, throttled, with the
device, the container count, the lines dropped and the entries rejected.
Line content is never logged.

**Tests:**

- `TestDUX_SEC023_ForgedGapStreamNotStored`
- `TestDUX_SEC023_GapEntriesValidatedAndBounded`
- `TestDUX_SEC023_NameRotationHeldByDeviceCeiling`
- `TestDUX_SEC023_MaskAfterCeiling`
- `TestDUX_AC111_ServerCeilingAnyAgent`

**6. Write-time masking (moved to WP-P0B; SEC-028).**

- **From P0,** `redact.MaskLine` runs on every stored line at ingest, after the ceilings (§5) and before `InsertBatch`.
- **Package ownership.** WP-P0B creates the masking subset of `internal/redact`: rule 2 key matching, the rule 3 URL scanner, and the FRD §8.7 patterns, as widened in AD-005. WP-1A adds the label functions on top.
- **Release rule.** WP-P0A's capture of stopped and TTY containers is never released without WP-P0B.
- **Older rows.** History reads (P1) mask again, which covers rows stored before WP-P0B.
- Test: `TestDUX_SEC028_StoredLinesMaskedFromP0`.

### AD-036 (new): One Logs tab: history API, line identity, de-duplication (OQ-08 and OQ-10)

**History API (P1, WP-1B):**
`GET /api/v1/devices/{id}/docker/containers/{name}/logs?from&to&before&after&limit`.

| Parameter | Meaning |
|---|---|
| `from`, `to` | RFC3339. Default: the last hour |
| `before` | Opaque `(timestamp, id)` cursor for older pages |
| `after` | RFC3339 µs. Used by refresh |
| `limit` | At most 5,000. Default 5,000 |

The response:

```
200 { "entries":[ {"t":"<RFC3339 µs>","s":"o"|"e","l":"<masked text>","x"?:true}
                 | {"t","gap":{"count":n,"lower_bound":bool,"reason":"agent_cap"|"server_cap"|"quota_sampled"}} ],
      "has_more": bool, "next_before"?: "<cursor>",
      "retention_days": n, "oldest_retained_at": "<RFC3339>", "range_clamped": bool,
      "retention_limited_by": ""|"storage_budget"|"disk_headroom",
      "newest_stored_at"?: "<RFC3339 µs>", "pre_recreate_history": bool,
      "collector": "enabled"|"disabled"|"unknown_no_data"|"unknown" }
```

**Re-stamped lines (D-42, revision 3).**

- **`agent_t`.** A line entry may carry `"agent_t":"<RFC3339 µs>"`. That means the row was re-stamped at ingest (AD-035 §1a): `t` is the receive time, and `agent_t` is the agent's original time. Gap entries may carry it too.
- **Gap reasons.** A gap entry's `reason` is `agent_cap`, `server_cap` or `quota_sampled` (rev 3.3, D-54).
- **Sampled-history notice (rev 3.3).** When the visible range contains a `quota_sampled` gap, `LogView` shows "Stored history is sampled (storage quota); Live shows every line".
- **Display.** `LogLines` shows a clock-skew marker on such lines with both times, formatted through the UI-FOUNDATION formatter.
- **Gap row mapping.** A row becomes a gap entry only when both conditions hold:
  - its `stream` is `gap_agent`, `gap_server` or `gap_quota`;
  - its `line` parses as an integer from 1 to 10,000,000, with an optional `+`.

  Any other row renders as an ordinary text line (SEC-023, V-4).
- **Labels.** `labels.ts` gains the event types `clock_skew` and `log_partition_default_rows`.

**Reading by name (FR-009).**

- The query filters `device_id`, `container_name` and the `timestamp` range, uses the `(timestamp, id)` keyset, orders by `timestamp DESC, id DESC`, and uses `LIMIT $limit+1`.
- It needs WP-P0C's `idx_container_logs_name`.
- Before WP-P0C, the query filters by the current `container_id` and sets `pre_recreate_history: false`. The UI then shows a one-line note.

**Refresh.** With `after`, the query returns rows where
`timestamp >= $after` and `timestamp <= now() + 5 min`, ordered by
`timestamp, id` ascending.

- Every history query carries the same upper bound, so no row dated in the future is ever returned (SEC-021 item 2).
- Ingest re-stamping (AD-035 §1a) already keeps such rows out. The bound is defence in depth.

**Masking.** Every line goes through `MaskLine`. Gap rows come back as gap
entries.

**Stack variant.**
`GET /api/v1/devices/{id}/docker/stacks/{project}/logs?services=…` returns
the same shape, with a `container` field on each entry. It accepts at most 25
member names.

**Collector state.**

- `enabled` or `disabled` comes from `DockerInfo.container_logs_enabled`.
- For older agents, the server checks `EXISTS(SELECT 1 FROM container_logs WHERE device_id=$1)`. If no row exists, it reports `unknown_no_data`; if rows exist, `unknown`. The UI shows the collector-not-enabled text for `unknown_no_data`.
- This keeps the FR-158 states distinct.

**Range clamping (FR-157).** A range that starts before `oldest_retained_at`
is clamped, and `range_clamped` is set.

**Authorization.** The endpoint is a database read with no agent call and no
audit row. `AdminAuth` alone applies.

**Line identity (FRD §8.12, at microsecond precision).** The key is
`container | ts_µs | stream | text`.

- `ts` is the line's time in the agent's clock domain: `agent_t` when the line was re-stamped (D-42), `t` otherwise.
- `text` is the line after 16 KiB truncation and after masking.
- Both paths use the same `redact` package and Docker's own timestamps.
- The hub re-stamps live lines with the same rule. A stored copy and a live copy of one line therefore still compare equal on a host with a skewed clock.

**One buffer (`utils/logBuffer.ts`).** Per container, it keeps `newestTs` and
the set of identities seen at `newestTs`.

- A line is accepted when its timestamp is after `newestTs`, or equal to it with an identity not yet in the set.
- **Future bound (SEC-021 item 3).** Ordering uses `t`.
  - The buffer never adopts a `newestTs` later than the browser's current time plus 5 minutes.
  - A line beyond that is still shown, with the clock-skew marker, but it does not advance `newestTs`. It therefore cannot suppress later lines.
  - Test: `logBuffer.test.ts`.
- Accepted lines are inserted in timestamp order by binary search, so merged stack views stay in order.
- **Bounds:** 10,000 lines **and** 32 MiB of text (twice the UTF-16 length). When a bound is hit, the oldest lines are dropped and the view says "Older lines were dropped from view" (SEC-006 item 4).

**One component (`useLogView` with `LogView`).**

- The history range is held in the URL.
- The newest page loads first. "Load older lines" prepends earlier pages, with de-duplication.
- History refreshes every 15 s with `after = newest`.
- The view shows empty and error states, and the effective retention text.
- From P2 it also holds the Live half (AD-018).
- Subcomponents: `HistoryRangePicker`, `LogToolbar` (filter, stream, wrap, timestamps, Download, Live toggle), `LogLines` and `ConnectionIndicator`.
- The stack page uses the same component with `merged`. There is **no** separate History or Live component.

**View state and rendering.**

- Follow and Pause are view state only. Pause freezes the rendered slice, and the buffer keeps filling: `pendingCount = buffer.seq − pausedAtSeq`.
- Lines render in chunks of 200 inside elements styled with `content-visibility: auto; contain-intrinsic-size: auto 4000px`. The browser skips off-screen chunks, and variable-height wrapped lines work.
- Every line renders as a text node. ANSI is stripped at render (`utils/ansi.ts`).
- The stream toggle and the text filter are client-side over the buffer. Both streams are always fetched and streamed. Matches are highlighted by splitting the text into text nodes and `<mark>`, with no HTML injection.
- Buffer entries are `{seq, svc, stream, ts, text, truncated}`, with inline markers as entries of kind `marker`.
- **Notice.** The Logs tab shows "Known secret patterns are masked, but logs may still contain sensitive data."

**Download.** All buffered lines for the selected streams, history first and
then live, masked and unfiltered (the text filter is ignored). The file name
is `${hostname}_${container}_${utcCompact}.log`, from `formatUtcCompact` in
`utils/format.ts` (`YYYYMMDDTHHmmssZ`). Hostname and container are sanitised
to `[A-Za-z0-9._-]`.

**FR-164.** Opening the tab never sends `attach` (AC-120).

### AD-037 (new): Shared live streams: limits and costs (FR-165, NFR-027, D-36, D-38)

**Agent cost (NFR-027).**

- One Docker follow per container, whatever the number of viewers.
- One masking pass per admitted line.
- One bounded backfill per viewer join: at most 5,000 lines, at most 2 at a time per host, within the agent byte budget.

**Bounds:**

| Bound | Limit |
|---|---|
| Live streams per host | 8 |
| Viewers per stream | 8 |
| Viewers server-wide | 64 |
| Line rate per stream | 1,000 lines/s |
| Byte rate per stream | 512 KiB/s |
| Byte rate per agent | 2 MiB/s |
| Server headroom | 768 KiB/s per stream; 3 MiB/s per agent |
| Backfill | 5,000 lines |
| Viewer lifetime | 4 h |

A stream stops within 5 s of its last viewer leaving: 2 s of linger, then
unfollow, then an agent cancel within 1 s.

**Merged stack Live (D-38).** Live follows at most the host's free stream
slots. The picker pre-selects running services in name order. History is
unaffected and can merge up to 25 containers.

### AD-038 (new): Stack path trust (SEC-002)

**1. Agreement (P1 for the auto-update path; P3 for everything else).**

- Per (device, project), the server collects the distinct `(working_dir, config_files)` tuples reported by the members in the latest snapshot.
- If there is more than one, the stack is `conflicted`, and up to 20 disagreeing members are recorded.
- **P1 (WP-1B):**
  - `auto_update.go` stops using the first container it sees. It dispatches a stack update only when both of these hold:
    - (a) the members agree;
    - (b) at least one member was created by compose itself, meaning it carries both `com.docker.compose.config-hash` and `com.docker.compose.container-number` (SEC-024 item 2).

    Otherwise it skips with a throttled Warn, counted as a conflict skip rather than a failure.
  - Rule (b)'s labels can be forged too. The binding control is therefore the agent-side writable-path refusal (item 3). From P1 it applies to `docker_update{compose_work_dir}`, so it covers both manual stack Update and auto-update (D-44).
  - A manual stack Update returns `409 stack_paths_unagreed` when the members disagree.
  - Test: `TestDUX_SEC002_AutoUpdateUsesAgreedRecord`.
- **P3:** these checks switch to the agreed record in `docker_stacks`.

**2. No silent path change (P3).**

- The table keeps `agreed_*` and `observed_*`.
- On the first agreeing observation, both are set and the stack is `agreed`.
- A later agreeing observation that differs from `agreed_*`:
  - sets `pending_ack` and `path_changed_at`;
  - writes a `stack_paths_changed` event and a `stack_paths` audit row;
  - makes actions and reads refuse with `stack_paths_unagreed`.
- **Acknowledgement:**
  - `POST /api/v1/devices/{id}/docker/stacks/{project}/paths/acknowledge` takes `{working_dir, config_files}`, which must equal `observed_*`.
  - It goes through AD-034 and a confirm dialog that shows the old and new paths.
  - It copies observed into agreed, writes an audit row (long retention) and writes an Activity event.
- The agent registry keeps its last agreed paths. It accepts new paths only when a request carries exactly them **and** its own live members agree.

**3. No execution of compose files a container can write.** This applies
from P1 for `docker_update{compose_work_dir}`, including auto-update (agent
WP-1A; D-44). It applies from P3 for every stack action. D-29 default (a)
applies.

- Before any compose invocation, the agent collects the symlink-resolved host sources that any container on the host can write:
  - every `Type=bind, RW=true` mount, across **all** containers;
  - every read-write mount of a named volume using the `local` driver with bind options. Its `VolumeInspect(name).Options` has `o` containing `bind` and a `device` path; that path is the host source (SEC-024 item 1);
  - every read-write mount of a volume from any other driver whose `Mountpoint` lies outside `/var/lib/docker/volumes/`. That mountpoint is treated as a host source.

  The README documents that volumes whose driver exposes no host path are not covered.
- If the resolved working directory or any resolved compose file is at or under one of those sources, `precheck` fails with `container_writable`.
- The exception is a working directory under an entry of the host-local `docker.compose_trusted_roots` (in `agent.yaml`). The server can never set this.
- Reads are still allowed, but are marked `container_writable:true`.
- Test: `TestDUX_SEC002_ComposeUnderRWBindMountNotExecuted`, covering Up, Redeploy, Update, `docker_update{compose_work_dir}` and the exception.
- Tests:
  - `TestDUX_SEC024_BindBackedVolumeCountsAsWritable` (WP-3A, C-24);
  - `TestDUX_SEC024_P1AutoUpdateRefusesContainerWritableWorkdir` (WP-1A, C-24 item 2).
- **Doctor (WP-1A).** `riot-agent doctor` lists the stacks whose compose files are writable from a container and are not under `docker.compose_trusted_roots`. The release notes for P1 say that these stacks stop updating until they are added (D-44).

**4. A single source of truth.** Stack actions, stack Update, auto-update and
the compose and `.env` reads all use the agreed record.

**5. Paths are shown before execution.** See AD-027 and AD-021.

### AD-039 (new): Stored and live logs trade-offs (requested by the product owner)

**1. Collector interval versus live latency.**

| Path | Latency | Cost |
|---|---|---|
| Stored history | At most one telemetry interval plus 10 s (NFR-026): about 70 s at the default `poll_interval` of 60 s | Collector and storage, all the time |
| Live tail | Under 2 s at p95 (NFR-006) | Nothing stored; the agent pays only while someone watches |

Shortening `poll_interval` to 15 s gives about 25 s of history lag. The cost
is four times the snapshot rows and the container-log rows (caps apply per
collection, so the daily worst case scales as 1 ÷ interval; see §13.3), and
four times the collector's Docker reads. **The ADD keeps 60 s**, and treats
Live as the tool for immediacy (FR-160 text).

**2. De-duplication at the history/live boundary.**

- **Cost.** Each arriving line costs one set lookup at its container's newest timestamp. Memory is only the lines that share that timestamp.
- **Correctness.** It depends on both paths producing identical text: the same `redact` rules, the same 16 KiB truncation, and Docker timestamps at microsecond precision. No clock skew is involved.
- **Failure modes.** These are visible duplicates, never lost lines:
  - An agent and a server on different masking-rule versions can still produce different text. The server re-masks live lines, but when the agent masked *more*, one line can appear twice.
  - Distinct lines that share container, microsecond, stream and text collapse into one (FRD R-13).
- **Order.** Pending live lines wait until the backfill finishes, which takes under 5 s within the budget.

**3. Agent load with several viewers.**

- **With D-36 fan-out:** one follow per container, plus one bounded backfill per viewer join. Eight viewers of one container cost one reader, one masking pass and one rate budget.
- **The rejected per-viewer option:** eight readers and eight times the masking CPU. Duplicate streams would also use up the per-host cap.
- **Server cost:** a queue copy per viewer, for at most 64 viewers.
- **Worst-case agent load:** 8 streams at 1,000 lines/s, held within 2 MiB/s of admitted data in total.

### AD-040 (new, rev 3.2): Agent ingest bounds (SEC-035, SEC-038; D-51)

**Decision.** A new package, `internal/server/ingest` (WP-P0B), puts a body
cap and a per-device rate in front of every agent ingest route. `router.go`
adds `ingest.Limit(<route class>)` after `deviceAuth` on each agent route.

**Keying.**

- Buckets are keyed by the device that `DeviceAuth` (and mTLS, when enabled) authenticated, and are created only after authentication succeeds.
- A request that fails authentication never creates a bucket.
- A bucket is evicted after `IngestLimiterIdleEviction` (10 min) without use (B-14, B-43).

**Limits (D-51).**

| Route class | Body cap (before decoding) | Rate per device | Over the rate |
|---|---|---|---|
| telemetry | `TelemetryBodyMaxBytes` 25 MiB | `TelemetryIngestInterval` 1 per 15 s, `TelemetryIngestBurst` 120 | `429 rate_limited` with `Retry-After`; nothing decoded or stored |
| heartbeat | `HeartbeatBodyMaxBytes` 64 KiB | `HeartbeatIngestInterval` 1 per 10 s, burst 6 | `200 {"status":"ok","throttled":true}`; no insert, no device UPDATE, no pending-command delivery |
| events (`/docker-events` and `/events`, shared) | `AgentEventBodyMaxBytes` 1 MiB | `AgentEventsPerMinute` 120 | `429` with `Retry-After`; nothing stored or broadcast |
| device logs (`/logs`) | `DeviceLogBodyMaxBytes` 8 MiB | `DeviceLogPushesPerMinute` 6 | `429` with `Retry-After` |
| probe results | `ProbeResultBodyMaxBytes` 1 MiB | `ProbeResultPushesPerMinute` 6; at most `ProbeResultsPerPush` (64) results per push | `429` with `Retry-After`. Two kinds of result are dropped and counted: results for probes not enabled for this device, and results within `ProbeResultMinSpacing` (30 s) of the last stored result for that probe |
| agent WS frames (`/ws/agent`) | `AgentWSReadLimit` 4 MiB per frame (`conn.SetReadLimit`) | `AgentWSFramesPerSec` 200, burst 400 | Frames dropped and counted. After `AgentWSExcessCloseAfter` (3 s) of continuous excess, the socket closes with 1008 and the agent reconnects |

- **Body caps.** `http.MaxBytesReader` wraps the body before `json.NewDecoder` runs.
  - An `*http.MaxBytesError` returns `413 payload_too_large`.
  - The existing 20 MB snapshot guard stays, as a second check.
- **Why a burst of 120.** It covers the agent's buffer flush after an outage (at most 100 snapshots, B-23) plus a live push.
- **Why 429 for telemetry.** The agent already buffers with a fixed size (B-23) and resends, so a 429 loses nothing that fits in the buffer. Accepting and dropping would lose history silently.
- **Why heartbeats and events are dropped.** Heartbeats carry no history, so dropping the excess is invisible. Events are observational, and the next snapshot reconciles the state.
- **WS read limit (correction).** The merged agent socket (`HandleAgentWS`) sets no read limit today, although §12 item 8 said it did. P0 adds the limit.
  - The largest legitimate agent frame is a command report (at most 256 KiB of combined output plus steps) or an RPC response (at most 1 MiB).

**Agent behaviour (WP-P0A).**

- **429.** On a telemetry 429, the agent stores the snapshot in its buffer and sends no telemetry until `Retry-After` has passed.
- **Flush.** `flushBuffer` sends oldest first, deletes each item after its own 2xx, and stops at the first response that is not 2xx.
  - Today it clears the buffer only after the whole flush. A 429 halfway through a flush therefore makes the next flush resend snapshots that were already stored.
- **Older agents** keep today's behaviour. The server bounds hold for them, and any duplicate snapshots count against the same rate and quota (R-25).

**Events (P2, WP-2B; B-19).**

- **Text caps.** `EventRepo.Create` applies these to every writer, cutting at a rune boundary:
  - each stored text field at most `EventTextMaxBytes` (1 KiB);
  - all of a row's text together at most `EventRowTextMaxBytes` (3 KiB).
- **One device bucket.** D-47's device bucket (600 per hour) covers every agent-originated event: Docker events, `/events` agent events and probe-failure events.
- **Per-container counters.** They are an LRU of `EventThrottleContainersMax` (512) per device. An evicted or overflowing container counts against the device bucket only.

**Request logging (SEC-038).**

- **Agent ingest routes answering below 400.** `middleware.Logger` writes no per-request line. Instead, every `AgentRequestLogSummaryInterval` (10 min), it writes one `agent ingest summary` line per device, with counts per route class, throttled counts and bytes.
- **Agent ingest routes answering 400 or above** go through the log budget (§14.1 item 30).
- **`ReceiveDeviceLogs`.** Its per-push Info line joins the summary.
- **All other routes** keep today's request line.

**Server-host disk (SEC-038).**

- **Compose files.** Both shipped compose files set this for `riot-server` and for `riot-db`:
  `logging: {driver: json-file, options: {max-size: "10m", max-file: "5"}}`.
  Each container's log is therefore at most 50 MiB.
- **README.** It gives the same settings for custom deployments, and the daemon-wide alternative (`log-opts` in `daemon.json`).
- **Test.** `TestDUX_SEC038_ComposeLoggingOptions` parses both files with `yaml.v3`, and fails when any service lacks these options.

**Tests:** AC-144, AC-145, AC-146 and AC-154; the B-19 cases sit under AC-139.

### AD-041 (new, rev 3.2): Daily quotas, fleet storage budget and disk headroom (D-49, D-50; SEC-037 item 1)

**1. Per-device quotas (D-50; reshaped in rev 3.3 by D-54, SEC-039).** New package `internal/server/quota` (WP-P0B; the `commands` class is in WP-2B).

Each class is a **continuously refilling token bucket** per device, not a
counter that resets at UTC midnight. The daily totals stay as in D-50. Each
bucket's capacity is 25 % of its daily quota Q (`QuotaBucketCapacityShare`),
and it refills at Q per 24 h.

| Class | Counts | Daily quota Q | Degraded mode (bucket at or below 25 % of capacity) | Phase |
|---|---|---|---|---|
| `logs` | Stored container-log line bytes, plus 200 B per row (gap rows excluded) | `DeviceDailyLogBytes` 512 MiB | **Sampled:** priority lines first (stderr, and stdout matching `ErrorLinePatterns`); other stdout sampled to 5 lines per container per minute; a max-min fair share per container; the rest counted in `gap_quota` rows (AD-035 §5 rule 6) | P0 |
| `telemetry` | Stored snapshot bytes (after logs and inventory are extracted), plus 200 B per container-metric row | `DeviceDailyTelemetryBytes` 1 GiB | **Downsampled:** at most one snapshot per `TelemetryQuotaDownsampleInterval` (15 min), stored **even when the bucket is empty**. It is trimmed to at most `TelemetryDownsampleMaxBytes` (2 MiB) by dropping the largest sections other than those in `TelemetryDownsampleKeepSections`, using the existing `snapshotFieldSizes`. The latest stored state is therefore never more than 15 min old. Last-seen, broadcast and alerts still run on every accepted push | P0 |
| `device_logs` | Message bytes, plus 200 B per row | `DeviceDailyDeviceLogBytes` 256 MiB | **Sampled:** entries with priority ≤ 3 (error and above) first; the rest sampled to `DeviceLogSampleEntriesPerMinute` (20) per minute; the remainder counted | P0 |
| `commands` | `command_output` bytes, plus the bytes of `commands.steps` and `target_results` | `DeviceDailyCommandBytes` 64 MiB | When the bucket is empty: output is stored as a 1 KiB head-and-tail stub (`QuotaTruncatedOutputBytes`), marked "Output truncated (storage quota)", and `target_results` is reduced to the summary entry. The command itself still runs, and its status is recorded | P2 |

- **Bound.** In any 24 h window, a class stores at most capacity + refill, which is 1.25 × Q. For `telemetry`, add at most 96 trimmed snapshots of at most 2 MiB each. Over longer windows, the average is at most Q per day.
- **Never zero.** A bucket refills every second. A device in degraded mode keeps storing at the refill rate (about 364 KiB of log lines per minute at the defaults), priority first.
- **Hysteresis.** Degraded mode starts at 25 % of capacity (`QuotaDegradeBelow`) and ends above 50 % (`QuotaRecoverAbove`).
- **When bytes count.** At the moment of storage: after ceilings and validation, before `CopyFrom` or `INSERT`.
- **State survives restarts.**
  - Bucket levels and daily totals are held in memory per device. They are flushed at most every `QuotaFlushInterval` (60 s) to `device_storage_usage` (§7.2), with one upsert per dirty device.
  - At startup, each level is seeded from the latest row, plus the refill for the elapsed time, capped at capacity.
  - A crash loses at most 60 s of accounting, and a restart never refills a bucket early.
- **Usage rows.** The janitor deletes rows older than `QuotaUsageRetention`: max(effective container-log retention + 1, 7) days, at most 91. D-56 reads them.
- **Event.** One `storage_quota_exceeded` warning event per device, per class, per **transition** into degraded mode. It clears when the mode ends. It names the class and the quota, and contains no log content. `GET /docker/hosts` exposes levels and modes as `storage_quota` (§8.3).
- **Fairness override (D-56).** §2 may lower a device's `logs` allowance below Q while the fleet budget binds.
- Tests: AC-147 (amended), AC-164, AC-165, AC-166.

**2. Fleet storage budget (D-49).** New package `internal/server/storage`. Container logs are in WP-P0C; command results are in WP-2B.

- **Settings.** `RIOT_CONTAINER_LOG_MAX_GB` (default 16) and `RIOT_COMMAND_RESULTS_MAX_GB` (default 4): 20 GiB together.
  - Each must be a positive integer. Anything else falls back to the default, with a startup Warn.
- **Effective budget.** When free space is known (§3), it is the lower of:
  - the setting;
  - `StorageBudgetDiskShare` (50 %) × (the class's current usage + DB free bytes).

  When free space is unknown, it is the setting.
- **Container logs.** Each janitor run sums `pg_total_relation_size` over the `container_logs` partitions. This reads the catalog only, so it is cheap.
  - **Fairness first (rev 3.3, D-56, SEC-042).** The effective budget is a soft limit. When usage exceeds it:
    1. The janitor computes the fair share F = `FairShareFactor` (0.9) × budget ÷ (days kept × active devices). Days kept is the effective retention + 1. Active devices are those with stored log bytes in that window, taken from `device_storage_usage`.
    2. A device gets a lowered `logs` allowance when its mean daily stored log bytes over the last `FairShareWindowDays` (3) exceed both F and the fleet median.
       - The allowance is A = max(F, `FairShareMinBytesPerDay` 32 MiB). Its bucket refills at A per 24 h, with a capacity of 25 % of A.
       - That device enters sampled mode sooner, priority lines first, instead of every device losing days.
    3. Allowances are recomputed every run. They return to Q once usage has stayed at or below `FairShareRestoreBelow` (80 % of the budget) for 24 h.
    4. One `log_fairness_limited` warning event fires per device per transition, and `GET /docker/hosts` shows `storage_quota.fairness` (§8.3).
  - **Early drops** happen only in two cases: usage is above the hard limit, `StorageBudgetHardFactor` (110 %) of the effective budget; or the headroom guard is low or worse (§3).
    - The janitor then drops the oldest day partitions early, using the §1b rule 2 method and limits.
    - It stops once usage is at most `StorageBudgetLowWater` (90 %) of the budget.
  - **Floor.** It never drops today's or yesterday's partition.
  - **Reporting.** The effective retention becomes the number of whole days kept. The history API adds `retention_limited_by: "storage_budget"` (AD-035 §2, AD-036).
  - **Event.** One `log_budget_eviction` warning event per transition. It clears after 24 h without an early drop, once the reported retention is back to the setting.
  - If today and yesterday alone exceed the budget, the daily quotas still bound growth, and the event says so.
- **Command results (P2).** Each run sums `command_output.bytes` and `commands.result_bytes`, through index-only scans (§7.2).
  - Above the budget, it deletes the oldest `command_output` rows first, then the oldest terminal-state commands, in adaptive batches (AD-025). It stops at 90 % of the budget.
  - These DELETEs free no disk. The tables stop growing, and autovacuum makes the space reusable.
  - Rows written before P2 count as 0 bytes. They age out within 30 days (output) and 90 days (commands) (R-24).

**3. Disk headroom (D-49; thresholds D-55; rev 3.3, SEC-039, SEC-040, SEC-041).** `storage.Headroom` (WP-P0C).

- **Free-space source, in this order:**
  1. **`RIOT_DB_FS_PROBE_PATH`:** `statfs` of a path on the **same filesystem** as the database volume. `statfs` reports filesystem-wide free and total counts, so the path never needs the database files.
     - **Compose.** The shipped compose files mount a dedicated, empty named volume, `riot-fsprobe`, at `/riot-fsprobe` in the server container, and set this variable. Docker's local driver places it under the same data root as `riot-data`.
     - **The database volume is never mounted into the server** (SEC-040). The server image runs as root today, and root bypasses file modes, so such a mount would expose every database file.
     - **Sanity check.** Sometimes `pg_database_size(current_database())` exceeds the probed filesystem's used bytes (total − free). Then the path is not on the database's filesystem: the guard logs a Warn, reports the source as `mismatch`, and falls back to the next source.
     - **Custom layouts.** The README explains that the probe path must sit on the database volume's filesystem.
  2. **`RIOT_DB_MAX_GB`:** total = that size; free = total − `pg_database_size(current_database())`.
  3. **Neither:** headroom checks are off, and a startup Info says so. Quotas, fairness and budgets still apply.
- **Thresholds (D-55: absolute first).** With the volume's total size T:
  - **Low:** free < max(4 GiB, min(15 % × T, 50 GiB)).
  - **Emergency:** free < max(1 GiB, min(5 % × T, 10 GiB)).
  - **Critical:** free < `DBFreeCriticalBytes` (512 MiB).
  - **Overrides.** `RIOT_DB_FREE_LOW_GB` and `RIOT_DB_FREE_EMERGENCY_GB` replace the computed low and emergency thresholds.
  - **Example.** A volume with tens of GiB free never leaves normal mode. An 8 TB disk at 96 % used (about 320 GB free) is normal, because its low threshold is 50 GiB.
- **Checks** run every `DBFreeCheckInterval`: 5 min, or 60 s while low or worse.
- **Low** raises one `db_disk_low` warning event per transition, and allows the fleet budget's early drops (§2).
- **Emergency** raises one `db_disk_emergency` event and logs an Error. The guard then works through these steps in order, re-measuring after each, and stops as soon as free space is back above the low threshold. **It evicts oldest first, and never stops accepting all new data:**
  1. Drop the oldest `container_logs` day partitions, down to today and yesterday.
  2. Drop the oldest partitions of PART-001's other partitioned tables (for example `telemetry_snapshots`, `container_metrics`, `device_logs` and `heartbeats`, whichever PART-001 converts). The oldest upper bound goes first across tables, down to `EmergencyPartitionFloorDays` (2) per table.
  3. Delete command results older than `EmergencyDeleteFloorDays` (7 days), oldest first. This only stops growth.
  4. Delete events older than 7 days. This only stops growth.
  5. **Minimal mode** (replaces rev 3.2's shed), until free space is back above the low threshold:
     - **container logs:** priority lines, plus stdout sampled to 1 line per container per minute, within `EmergencyLogBytesPerDevicePerMinute` (64 KiB) per device;
     - **telemetry:** one snapshot per device per 15 min, trimmed to 2 MiB, exactly as in the `telemetry` downsampled mode. Last-seen, broadcast and alerts still run on every push;
     - **device logs:** priority ≤ 3 entries only, within `EmergencyDeviceLogBytesPerDevicePerMinute` (16 KiB) per device;
     - everything not stored is counted, with gap rows for container logs.
  6. **Critical only** (free below 512 MiB after steps 1 to 5): drop yesterday's `container_logs` partition, and drop the PART-001 partitions down to `CriticalPartitionFloorDays` (1, today). Minimal mode continues.
  - **The minimal recent window that always remains:**
    - today's partitions of every table;
    - each device's newest snapshot, at most 15 min old;
    - each device's priority log lines since the emergency began.
  - **Never touched:** audit rows, devices, keys, settings, alert rules, `docker_stacks`, `docker_inventory`, and commands that are not in a terminal state.
  - **Out of the guard's control.** If PostgreSQL itself runs out of space, inserts fail and are counted. That is why eviction starts well before.
- **Recovery.**
  - After `DBFreeRecoverAfter` (10 min) above the low threshold, the guard returns to normal. Minimal mode ends, and the events clear.
  - Retention is reported as shortened (`retention_limited_by: "disk_headroom"`) until the dropped days would have expired anyway.
- **Why absolute first.** Partition drops need a little WAL, so eviction must start while space remains. But a percentage alone would put a large, mostly full shared disk into a permanent emergency (SEC-039 item 3).
- **Lock holders (SEC-041).** Every eviction path uses the §1b drop method, so a single lock holder could stall all of them. Three rules prevent that:
  - **Session limits.**
    - The server's pool (`internal/server/db/db.go`) sets `application_name=riot-server`, and `idle_in_transaction_session_timeout` = 60 s (`DBIdleInTxTimeout`), on every connection.
    - History, Activity and inventory reads run inside a read-only transaction with `SET LOCAL statement_timeout` = 30 s (`DBReadStatementTimeout`).
    - So an idle-in-transaction session of the server itself can hold a lock for at most 60 s.
  - **Report the blocker.** When a drop hits its lock timeout, the janitor lists the backends that hold granted locks on the parent or on the partition.
    - It reads them from `pg_locks` joined to `pg_stat_activity`, plus `pg_blocking_pids` where available.
    - It reports at most `BlockerReportMax` (16) backends: PID, `application_name`, state, transaction age in seconds, and whether the role is the server's own. Never the query text.
    - The list goes into the budgeted Warn, and into the stale-janitor event when that fires.
  - **Emergency only: cancel the server's own blockers.** In emergency or critical mode, the guard may call `pg_cancel_backend` on a blocker that meets all of these:
    - its role is the server's own;
    - its `application_name` is `riot-server`;
    - it is `active`, with a transaction older than `EmergencyCancelMinTxAge` (30 s).

    It never touches other roles or other applications, for example an operator's `psql` session, even under the same role. It never terminates a backend.
- Tests: AC-148, AC-149 (amended), AC-167, AC-168, AC-170, AC-171.

**4. Creation-side caps (B-14, B-17; WP-1B).**

- **Limiter keying.**
  - The D-48 command buckets are created only after `Dispatch` has found the device. An unknown device gets 404 first.
  - The terminal-open bucket is created only after the device exists and its agent is connected.
  - Tests: `TestDUX_BND_LimitersKeyOnlyKnownDevices`, `TestDUX_BND_LimiterIdleEviction`.
- **System-origin commands.** These are commands with an origin other than operator: auto-update, auto-patch and policies.
  - **Cap.** `Dispatch` caps them at `SystemCommandsPerDevicePerHour` (30) per device. Over the cap they are not created. A budgeted Warn, plus one `system_commands_throttled` event per device per hour, records it.
  - **Minimum interval.** The server enforces at least `SystemPolicyMinCooldown` (15 min) between system commands for the same policy and target, whatever the policy settings. A failing update therefore cannot re-fire on every telemetry push.
  - Test: `TestDUX_BND_SystemCommandCap`.

---

## 6. Component Changes

Each file belongs to exactly one WP. *(on main)* marks a file that
CMD-LIFECYCLE-001 or UI-FOUNDATION-001 already put on `main`, and that this
story modifies. *(post-PART-001)* marks a file that arrives with PART-001.

### 6.0 WP-P0A (agent, P0, can start now)

| Action | File | Purpose |
|---|---|---|
| CREATE | `internal/models/docker_caps.go`, `docker_caps_test.go` | Tokens, starting with `log_caps_v1` |
| MODIFY | `internal/models/telemetry.go` | `ContainerLogGap`; `FullTelemetryData.ContainerLogGaps`; `DockerInfo.Capabilities`; `DockerInfo.ContainerLogsEnabled` |
| MODIFY | `internal/agent/collectors/docker_logs.go` | Caps, bounded scan, stopped and TTY collection, bounded frames, gaps (AD-035 §3–§4) |
| CREATE | `internal/agent/collectors/docker_logs_caps.go`, `docker_logs_caps_test.go` | `capRing` (AC-110) |
| MODIFY | `internal/agent/collectors/docker_logs_test.go` | AC-112, AC-113, frames over 1 MiB, lower bounds |
| MODIFY | `internal/agent/collectors/collector.go`, `docker.go` | Cap options; tokens and the enabled flag |
| MODIFY | `internal/agent/config.go`, `config_test.go` | `CollectorConfig.ContainerLogs{MaxLines, MaxBytes}` and validation |
| MODIFY | `internal/agent/agent.go` | Wiring |
| MODIFY | `internal/agent/doctor.go`, `doctor_test.go` | Print the effective `container_logs` caps |
| MODIFY | `README.md` | Caps and gap markers |
| MODIFY | `internal/agent/buffer.go`, `buffer_test.go` | `AgentBufferMaxBytes` (64 MiB) byte cap beside the existing 100-snapshot cap; oldest dropped (§13A B-23, AC-142). Per-item `Delete(id)`, so a flush removes each snapshot after its own success (AD-040) |
| CREATE | `internal/agent/agentlog/budget.go`, `budget_test.go` | Agent log budget for the new agent paths (§14.1 item 31, AC-141) |
| MODIFY | `internal/agent/telemetry.go`, `telemetry_test.go` | On a telemetry 429: buffer, then honour `Retry-After`. `flushBuffer` deletes each item after its own 2xx and stops at the first non-2xx (AD-040, AC-144) |
| MODIFY | `internal/agent/http_client.go`, `http_client_test.go` | Typed `RateLimitedError{RetryAfter}` for a telemetry 429 |

### 6.1 WP-P0B (server, P0, can start now)

| Action | File | Purpose |
|---|---|---|
| MODIFY | `internal/server/config.go`, `config_test.go` | `ContainerLogRetentionDays` and `parseContainerLogRetention` (AC-108) |
| CREATE | `internal/server/handlers/container_log_ingest.go`, `container_log_ingest_test.go` | `applyCeiling`, the rolling limiter, gap rows (AC-111) |
| MODIFY | `internal/server/handlers/handlers.go` | Ceiling before `InsertBatch`; gaps become rows |
| MODIFY | `internal/server/db/container_log_repo.go` | Gap rows through the same `CopyFrom` |
| MODIFY | `internal/server/server.go` | Startup warning or Info; `EffectiveContainerLogRetention()`; the purge at :490 **left unchanged** |
| CREATE | `cmd/riot-server/migrations/<NNNNNN>_docker_ux_p0_log_agent_ts.up.sql`, `.down.sql` | §7.2 P0 (D-42) |
| CREATE | `internal/redact/rules.go`, `mask.go`, `mask_test.go`, `testdata/cases.json` | Masking subset of AD-005 (rule 2 key matching, rule 3 URL scanner, FRD §8.7 patterns) for write-time masking (SEC-028) |
| CREATE | `internal/server/handlers/container_log_window.go`, `container_log_window_test.go` | `applyWindow` re-stamp (AD-035 §1a) and the per-device skew counters (`TestDUX_SEC021_*`) |
| CREATE | `internal/server/dockerlog/budget.go`, `budget_test.go` | Server Warn and Error budget (§14.1 item 30, AC-141) |
| CREATE | `internal/server/devicestate/registry.go`, `registry_test.go` | Per-device state registry with pruning; container limiter LRU; gap-row bound (§13A B-02, B-06, B-07; AC-143) |
| CREATE | `internal/models/docker_bounds_test.go` | `TestDUX_AC137_EveryBoundHasATest`: an AST scan of the constants in `docker_policy.go`, checked against the test files |
| MODIFY | `internal/server/handlers/container_log_ingest.go`, `container_log_ingest_test.go` (created by the CREATE row above) | Validation, server-owned `stream`, gap-entry bounds, unknown budget, per-device ceiling, mask after the ceiling (`TestDUX_SEC023_*`, `TestDUX_SEC028_StoredLinesMaskedFromP0`) |
| MODIFY | `internal/server/db/container_log_repo.go` | `agent_ts` in `CopyFrom`; history ordering `timestamp, id` |
| MODIFY | `internal/models/events.go`, `internal/server/events/generator.go` | `clock_skew` event type; once-per-transition `CheckClockSkew` |
| CREATE | `internal/server/ingest/limits.go`, `limits_test.go` | AD-040: per-device buckets keyed by the authenticated device, body caps through `http.MaxBytesReader`, `Retry-After`, idle eviction (AC-144..AC-146, B-14) |
| MODIFY | `internal/server/router.go` | `ingest.Limit(<route class>)` after `deviceAuth` on every agent ingest route |
| MODIFY | `internal/server/handlers/handlers.go` | `Telemetry`, `Heartbeat`, `ReceiveDeviceLogs`, `ReceiveAgentEvent`, `ReceiveDockerEvent`: body caps, heartbeat throttle (no DB write), and the D-50 `telemetry`, `device_logs` and `logs` quotas (AD-040, AD-041) |
| MODIFY | `internal/server/handlers/device_probes.go` | Probe-result bounds: 64 per push, enabled probes only, 30 s spacing (AD-040) |
| MODIFY | `internal/server/handlers/terminal.go` *(on main)* | Agent WS `SetReadLimit` (4 MiB) and a per-device frame bucket (AD-040, AC-146) |
| MODIFY | `internal/server/middleware/logger.go`, `logger_test.go` | Agent-ingest request lines summarized per device every 10 min; error responses through the log budget (SEC-038, AC-154) |
| CREATE | `internal/server/quota/daily.go`, `daily_test.go`, `daily_pg_test.go` | D-50 quotas as continuously refilling buckets with degraded modes (D-54): sampling with priority first and a per-container fair share, telemetry downsampling and trimming, persisted levels, `storage_quota_exceeded` per transition (AD-041 §1; AC-147, AC-164..AC-166) |
| CREATE | `internal/server/quota/priority.go`, `priority_test.go` | `ErrorLinePatterns` classification of lines, in memory and before masking (D-54) |
| CREATE | `internal/server/db/usage_repo.go`; MODIFY `internal/server/db/interfaces.go`, `internal/testutil/mocks.go` | `device_storage_usage` upsert, seed (with elapsed refill) and purge |
| MODIFY | `internal/server/config.go`, `config_test.go` | `RIOT_CONTAINER_LOG_MAX_GB`, `RIOT_COMMAND_RESULTS_MAX_GB`, `RIOT_DB_FS_PROBE_PATH`, `RIOT_DB_MAX_GB`, `RIOT_DB_FREE_LOW_GB`, `RIOT_DB_FREE_EMERGENCY_GB` (AD-041) |
| MODIFY | `docker-compose.yml`, `docker-compose.prod.yml` | `logging` (json-file, 10m × 5) on `riot-server` and `riot-db`; an empty named volume `riot-fsprobe` mounted at `/riot-fsprobe` in the server, with `RIOT_DB_FS_PROBE_PATH`; **no** mount of `riot-data` outside `riot-db` (SEC-038, SEC-040, AD-041 §3) |
| CREATE | `internal/server/compose_logging_test.go` | `TestDUX_SEC038_ComposeLoggingOptions` (both compose files, every service); `TestDUX_SEC040_OnlyDatabaseMountsDataVolume` |
| MODIFY | `internal/server/db/db.go`, `db_test.go` | Pool runtime parameters `application_name=riot-server` and `idle_in_transaction_session_timeout` 60 s; a `ReadTx(ctx, fn)` helper with `READ ONLY` and `SET LOCAL statement_timeout` 30 s (SEC-041) |
| MODIFY | `README.md` | Log rotation for custom deployments; ingest limits, quota modes, the storage budget and fairness, the D-55 thresholds, and the probe-path rule for custom layouts (SEC-040) |

### 6.2 WP-P0C (server, P0, needs PART-001)

| Action | File | Purpose |
|---|---|---|
| (merge) | PART-001 branch, migrations renumbered (§2.4) | D-35 (a) and D-37 |
| MODIFY | `internal/server/partition/*` *(post-PART-001; the per-table window spec)* | Take the `container_logs` window from the setting |
| CREATE | `cmd/riot-server/migrations/<NNNNNN>_docker_ux_p0_log_name_index.up.sql`, `.down.sql` | §7.2 P0 |
| CREATE | `internal/server/db/container_log_rotation_pg_test.go`, `container_log_nodelete_test.go` | AC-107, AC-109, AC-114 |
| MODIFY | `internal/server/server.go` | Effective value from the manager registration; NFR-022 log |
| CREATE | `internal/server/db/fingerprint.go`, `fingerprint_pg_test.go` | `ClassifyMigrationState` (§2.4.1), including the rev 3.3 step 4 rows (F-LC + F-AV; F-LC + F-PT + F-AV); plain SQL only, never golang-migrate; `TestDUX_SEC022_FingerprintClassification`, `TestDUX_SEC022_DirtyDatabaseRefused`, `TestDUX_SEC030_FingerprintCreatesNothing`, `TestDUX_SEC030_FingerprintReadOnlySession`, `TestDUX_SEC030_FingerprintOutputHasNoDSN` (AC-163) |
| MODIFY | `cmd/riot-server/main.go`, `main_test.go` | Subcommand dispatch **before** `server.New`: `db-fingerprint` (read-only session, secret-free output, non-zero exit on every "stop" class) and `migrate` (applies migrations and exits without serving; §2.4.1 step 4b). `TestDUX_SEC030_MigrateSubcommandDoesNotServe` (SEC-030) |
| CREATE | `internal/server/db/part001_renumber_pg_test.go`, `testdata/part001_branch/*.sql` | `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB` (branch-numbered fixtures, placeholders only), which also asserts that the `partition_conversion` row is unchanged (SEC-030 item 6) |
| MODIFY | `internal/server/events/generator.go`, `internal/models/events.go` | `log_partition_default_rows` event from PART-001's `DefaultRowCount` (SEC-021 item 4) |
| CREATE | `internal/server/db/container_log_default_pg_test.go` | A forged future row cannot block day-partition creation (SEC-021) |
| CREATE | `internal/server/partition/janitor_dux.go` *(post-PART-001; extends the manager)*, `janitor_dux_test.go`, `janitor_dux_pg_test.go` | AD-035 §1b: startup ensure over `[today − retention, today + 3]`, drops by `DETACH … CONCURRENTLY` or with a 1 s lock (D-53), stop at the first lock timeout, backoff, ingest partition guard with a server-wide singleflight, stale event (AC-134..AC-136, AC-151, AC-152) |
| CREATE | `internal/server/storage/budget.go`, `budget_test.go`, `budget_pg_test.go` | D-49 container-log budget: D-56 fairness first (soft limit), early drops above 110 % or under disk pressure, `retention_limited_by` (AD-041 §2; AC-148, AC-169) |
| CREATE | `internal/server/storage/headroom.go`, `headroom_linux.go`, `headroom_other.go`, `headroom_test.go` | Free-space source (probe path with the same-filesystem check, or `RIOT_DB_MAX_GB`); D-55 absolute-first thresholds; the emergency order with minimal mode and the critical step; cancelling only the server's own blockers in emergency (AD-041 §3; AC-149, AC-167, AC-168, AC-170, AC-171) |
| CREATE | `internal/server/storage/blockers.go`, `blockers_pg_test.go` | SEC-041 blocker report from `pg_locks`, `pg_stat_activity` and `pg_blocking_pids` (at most 16; no query text), used by the janitor and the guard (`TestDUX_BND_IdleTxBlockerDoesNotStallEviction`, `TestDUX_SEC041_EmergencyCancelsOnlyOwnBlockers`) |

### 6.3 WP-1A (agent, P1)

| Action | File | Purpose |
|---|---|---|
| MODIFY | `internal/models/telemetry.go`, `docker_caps.go` | AD-004 fields, `RiotLabels.Role`, P1 tokens |
| MODIFY | `internal/redact/rules.go`, `mask.go`, `testdata/cases.json` (created in WP-P0B); CREATE `redact.go`, `redact_test.go` | AD-005 label rules and `MaskText`, on top of the WP-P0B masking subset |
| MODIFY | `internal/agent/collectors/docker.go`, `docker_labels.go`, `docker_test.go` | Fields, rates, label redaction, tokens, role |
| CREATE | `internal/agent/collectors/docker_iorate.go`, `docker_iorate_test.go` | AD-004 |
| MODIFY | `internal/agent/commands.go`, `target_lock_test.go` *(on main)* | AD-007 agent locks |
| MODIFY | `internal/agent/docker_update.go`, `docker_update_test.go` *(on main)* | `target_results` |
| MODIFY | `internal/models/commands.go` *(on main)* | `TargetResult` and the result fields |
| CREATE | `internal/agent/rwmounts.go`, `rwmounts_test.go` | AD-038 item 3: read-write bind mounts, bind-backed volumes (`VolumeInspect` options) and host-path volume drivers as writable sources (SEC-024; C-2, C-24) |
| MODIFY | `internal/agent/config.go`, `config_test.go` | `docker.compose_trusted_roots` (host-local) |
| MODIFY | `internal/agent/docker_update.go` *(on main)* | Writable-path precheck for `compose_work_dir` (SEC-024 item 2, D-44) |
| MODIFY | `internal/agent/doctor.go`, `doctor_test.go` | List the stacks that are container-writable and not under `compose_trusted_roots` (D-44) |

### 6.4 WP-1B (server, P1)

| Action | File | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/<NNNNNN>_docker_ux_p1_bulk_results.up.sql`, `.down.sql` | §7.2 P1 |
| CREATE | `internal/server/middleware/sameorigin.go`, `sameorigin_test.go` | AD-034 (C-1) |
| MODIFY | `internal/server/router.go`, `router_test.go` *(on main)* | `NoFraming`; `registerDockerRoutes` with the middleware; matrix check |
| MODIFY | `internal/server/middleware/cors.go` | No `X-Riot-Request` for foreign origins |
| MODIFY | `internal/models/command_policy.go`, `internal/server/commands/targets.go`, `targets_test.go`, `service.go`, `service_test.go` *(on main)* | AD-007 (new `DispatchRequest` fields, `TargetKeyDockerHost`), AD-008 |
| CREATE | `internal/server/commands/applicability.go`, `applicability_test.go`, `testdata/applicability.json` | AD-010 |
| MODIFY | `internal/server/commands/service.go`, `service_test.go` *(on main)* | Operator command-creation limit per device: burst 120, 60 per hour (D-48, AC-140). Buckets are created only after the device is found. `SystemCommandsPerDevicePerHour` (30) and `SystemPolicyMinCooldown` (15 min) for non-operator origins (AD-041 §4, AC-156, AC-158) |
| MODIFY | `internal/server/handlers/terminal.go` *(on main)* | Terminal open limit of 30 per minute per device (D-48, AC-140). The bucket is created only after the device exists and its agent is connected (AC-156) |
| MODIFY | `internal/server/db/interfaces.go`, `command_repo.go`, `command_repo_pg_test.go` *(on main)*, `telemetry_repo.go`, `internal/testutil/mocks.go` *(on main)* | Conflict SQL with the lock always taken; projections; `target_results` |
| MODIFY | `internal/server/db/container_log_repo.go` | `ListRange` (by name, or by ID before WP-P0C), `ListAfter`, `AnyForDevice` |
| CREATE | `internal/server/handlers/docker_logs_history.go`, `docker_logs_history_test.go` | AD-036 history API |
| (moved) | `internal/server/handlers/container_log_ingest.go` write-time `MaskLine` | Now in WP-P0B (SEC-028). WP-1B masks again at read time in `docker_logs_history.go` |
| MODIFY | `internal/server/handlers/fleet_dashboard.go`, `fleet_dashboard_test.go` | FR-064 |
| CREATE | `internal/server/handlers/docker_hosts.go`, `docker_bulk.go`, `docker_self.go`, `docker_p1_test.go` | AD-003, AD-008, AD-009 (C-13); the `storage_quota` field (AD-041 §1) |
| CREATE | `internal/server/handlers/output_redact.go`, `output_redact_test.go` | AD-006 (C-4) |
| MODIFY | `internal/server/handlers/handlers.go`, `internal/server/summary/summary.go` | Output redaction; `/history` cap; `primary_ip` parse |
| MODIFY | `internal/server/handlers/auto_update.go`, `auto_update_test.go`, `commands.go` *(on main)* | AD-038 item 1 (agreement plus the compose-created-member rule, SEC-024 item 2); `confirm_host`; stack-update agreement |

### 6.5 WP-1C (web, P1)

| Action | File | Purpose |
|---|---|---|
| MODIFY | `web/src/App.tsx` | Docker nav entry, routes (AD-011), palette mount and shortcut |
| MODIFY | `web/src/types/models.ts` | `ContainerInfo` fields, `FleetContainerRow` (§8.2), `DockerHost`, `BulkActionRequest/Result`, `TargetResult`, history entry types |
| MODIFY | `web/src/api/client.ts` | `getDockerHosts`, `bulkDockerAction`; widened `getFleetContainers`; `mutateJSON`, `getWithSideEffect`, `getContainerLogHistory` |
| MODIFY | `web/src/api/demo-client.ts` | parity stubs |
| MODIFY | `web/src/api/demo-data.ts` | AD-031 P1 fixtures, `bigFleet()`, simulated stored history |
| CREATE | `web/src/pages/docker/phases.ts` | `DOCKER_SHIPPED` |
| CREATE | `web/src/pages/docker/DockerLayout.tsx` | sub-nav and outlet (FR-002, FR-003) |
| CREATE | `web/src/pages/docker/ContainersPage.tsx` | FR-011..FR-035 |
| CREATE | `web/src/pages/docker/ContainerPage.tsx` | FR-046..FR-061 |
| CREATE | `web/src/pages/docker/LegacyContainersRedirect.tsx` | FR-006 |
| CREATE | `web/src/pages/docker/LegacyContainerRedirect.tsx` | FR-007 |
| CREATE | `web/src/pages/docker/ContainersPage.test.tsx` | AC-003, 008..024, 031 (UI) |
| CREATE | `web/src/pages/docker/ContainerPage.test.tsx` | AC-006, 032..040 |
| CREATE | `web/src/pages/docker/redirects.test.tsx` | AC-004, 005, 007 |
| CREATE | `web/src/pages/docker/DockerLayout.test.tsx` | AC-001, 002 |
| CREATE | `web/src/components/docker/DataTable.tsx` | AD-012 grid |
| CREATE | `web/src/components/docker/DataTable.test.tsx` | windowing, keyboard (AC-101 keyboard half) |
| CREATE | `web/src/hooks/useVirtualRows.ts` | AD-012 |
| CREATE | `web/src/components/docker/ContainerCells.tsx` | Status, Name, Stack, Host, Image, Ports, CPU, Memory, Uptime cells (FRD §8.2) |
| CREATE | `web/src/components/docker/FilterChips.tsx` | FR-022 |
| CREATE | `web/src/components/docker/MultiSelectFilter.tsx` | FR-023 |
| CREATE | `web/src/components/docker/BulkActionBar.tsx` | FR-039 |
| CREATE | `web/src/components/docker/BulkConfirmDialog.tsx` | FR-040, FR-041, FR-044, FR-045, FR-147 |
| CREATE | `web/src/components/docker/BulkProgressPanel.tsx` | FR-043 |
| CREATE | `web/src/components/docker/BulkActions.test.tsx` | AC-025..AC-031 |
| CREATE | `web/src/components/docker/ResourceHeader.tsx` | FR-046..FR-052 (container); stack variant later |
| CREATE | `web/src/components/docker/ContainerTabs/OverviewTab.tsx` | FR-056 (moves the General, Network and Volumes content from `ContainerDetail.tsx`) |
| CREATE | `web/src/components/docker/ContainerTabs/StatsTab.tsx` | FR-057, FR-060 |
| CREATE | `web/src/components/docker/ContainerTabs/TerminalTab.tsx` | FR-059 wrapper |
| (dropped) | `web/src/components/docker/ContainerTabs/HistoryLogs.tsx` | Replaced by `LogView` |
| CREATE | `web/src/components/docker/logs/LogView.tsx`, `LogToolbar.tsx`, `LogLines.tsx`, `HistoryRangePicker.tsx`, `LogView.test.tsx` | AD-036 history half (FR-058, FR-157..FR-161); the sampled-history notice for `quota_sampled` gaps (rev 3.3, AC-165) |
| CREATE | `web/src/hooks/useLogView.ts`, `useLogView.test.ts` | AD-036 |
| CREATE | `web/src/utils/logBuffer.ts`, `ansi.ts`, `logMask.ts`, `safeUrl.ts`, `ip.ts` (+ tests; shared redact fixture) | Identity and buffer bounds (AC-053), ANSI strip, browser defence-in-depth mask (same table as Go), SEC-011 |
| CREATE | `web/src/components/docker/TilesView.tsx` | FR-028 (hosts `ComposeStackSection`, `NetworkClusterGroup`, `CompactContainerTile`) |
| MODIFY | `web/src/components/ContainerDetail.tsx` | tab helpers exported for reuse; `ContainerDetailContent` deleted once `ContainerPage` replaces it (verified by grep) |
| DELETE | `web/src/pages/DeviceContainers.tsx`, `web/src/pages/ContainerDetailPage.tsx` | S-1 (BR-014 through redirects) |
| MODIFY | `web/src/pages/ContainerDetailPage.test.tsx` → MOVE to `web/src/pages/docker/ContainerPage.test.tsx` | existing cases kept (test count must not drop) |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | Link to `/docker/containers?host=<id>` (FR-008) |
| MODIFY | `web/src/components/Terminal.tsx` | `data-palette-exempt` wrapper |
| CREATE | `web/src/components/CommandPalette.tsx` | AD-014 |
| CREATE | `web/src/components/CommandPalette.test.tsx` | AC-043..AC-047 |
| CREATE | `web/src/hooks/usePaletteIndex.ts`, `web/src/hooks/useRecentResources.ts` | AD-014 |
| CREATE | `web/src/utils/paletteRank.ts`, `paletteRank.test.ts` | FR-068, FR-069, NFR-005 guard |
| CREATE | `web/src/hooks/useDockerFleet.ts`, `useDockerFleet.test.ts` | AD-003 merge, coalescing |
| CREATE | `web/src/utils/dockerFleet.ts`, `dockerFleet.test.ts` | `projectFleetRow` + server-parity fixture |
| CREATE | `web/src/utils/dockerUrlState.ts`, `dockerUrlState.test.ts` | AD-011 (AC-018, AC-019, AC-120): no `mode`; `range`, `from`, `to` |
| CREATE | `web/src/utils/dockerChips.ts`, `dockerChips.test.ts`, `dockerChips.perf.test.ts` | FRD §8.3, AC-100(a) |
| CREATE | `web/src/utils/dockerActions.ts`, `dockerActions.test.ts` | AD-010 (AC-033, AC-026) |
| CREATE | `web/src/utils/__fixtures__/applicability.json` | shared fixture |
| CREATE | `web/src/utils/portLinks.ts`, `portLinks.test.ts` | FRD §8.4 (AC-011), through `safeUrl` |
| CREATE | `web/src/utils/sortFreeze.ts`, `sortFreeze.test.ts` | FR-034 (AC-024) |
| CREATE | `web/src/utils/uptime.ts`, `uptime.test.ts` | FRD §8.2 uptime/exit (AC-012) |
| CREATE | `web/src/components/ui/Tabs.tsx`, `Tabs.test.tsx` | AD-013 |
| CREATE | `web/src/components/ui/Menu.tsx`, `Menu.test.tsx` | AD-013 |
| CREATE | `web/src/components/ui/Popover.tsx`, `Popover.test.tsx` | AD-013 |
| CREATE | `web/src/components/ui/TypedConfirmInput.tsx`, `TypedConfirmInput.test.tsx` | AD-013 |
| MODIFY | `web/src/components/ui/ConfirmDialog.tsx`, `ConfirmDialog.test.tsx` *(on main)* | `confirmDisabled` (AD-013) |
| MODIFY | `web/src/components/ui/labels.ts`, `labels.test.ts` *(on main)* | new categories and actions (AD-013) |
| MODIFY | `web/src/components/ui/index.ts` *(on main)* | exports |
| MODIFY | `web/src/utils/format.ts`, `format.test.ts` *(on main)* | `formatUtcCompact` |
| MODIFY | `web/src/test/static-guards.test.ts` *(on main)* | scope extension (AC-099), fixture privacy scan (AC-105) |

### 6.6 WP-2A (agent, P2)

| Action | File | Purpose |
|---|---|---|
| CREATE | `internal/redact/inspect.go`, `inspect_test.go`; MODIFY `redact.go` | AD-020; argv widening |
| CREATE | `internal/agent/livelogs.go`, `livelogs_test.go` | AD-017, including orphan reaping, `logs_keep`, the backfill byte share, never-persisted frames and streamed backfill memory (`TestDUX_SEC025_AgentReapsOrphanedFollow`, `TestDUX_SEC026_LiveKeepsBudgetDuringBackfill`, `TestDUX_V1_LiveFramesNeverPersistedOnAgent`, `TestDUX_BND_BackfillStreamsWithoutBuffering`) |
| CREATE | `internal/agent/docker_events_bus.go`, `docker_events_bus_test.go`; MODIFY `docker_events.go` | Event bus with at most 10 subscribers and unsubscribe on cancel (`TestDUX_BND_EventBusUnsubscribeOnCancel`); `command_id` |
| CREATE | `internal/agent/docker_read.go`, `docker_read_test.go`, `loghygiene_test.go` | Inspect; SEC-016 |
| MODIFY | `internal/agent/ws_client.go`, `command_lifecycle.go`, `command_lifecycle_test.go` *(on main)* | Message routing; step recorder |
| MODIFY | `internal/models/events.go`, `commands.go`; CREATE `internal/models/docker_policy.go` | Fields and constants |

### 6.7 WP-2B (server, P2)

| Action | File | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/<NNNNNN>_docker_ux_p2_activity.up.sql`, `.down.sql` | §7.2 P2 |
| MODIFY | `internal/server/handlers/auth.go`, `oidc.go`, `internal/server/oidc/identity.go`, `service.go`, `internal/server/middleware/admin_auth.go`, `admin_auth_test.go` | AD-022 (C-8) |
| MODIFY | `internal/server/commands/validate.go`, `service.go`, `service_test.go` *(on main)* | Write-time masking; `Initiator`; steps; `command_id` validation; a `command` audit row for every operator-created command, Docker and non-Docker (SEC-029) |
| MODIFY | `internal/server/handlers/auto_update.go` *(on main)* | System initiator |
| MODIFY | `internal/server/db/command_repo.go` *(on main)*, `event_repo.go`, `terminal_repo.go`, `interfaces.go`, `internal/testutil/mocks.go` | Columns, purges with the AD-025 self-healing rules (ordered batches, timeouts, startup run, `retention_stale` event; AC-138), `LogAccess` |
| CREATE | `internal/server/db/activity_repo.go` (+ tests), `audit_retention_pg_test.go` | AD-023; C-9 |
| MODIFY | `internal/server/handlers/handlers.go` | `DeleteDevice` audits first |
| MODIFY | `internal/server/events/generator.go` | Validated attribution; observed-event throttle with an hourly summary (D-47, AC-139); per-container counters as an LRU of 512 per device; one device bucket for every agent-originated event (B-19, `TestDUX_BND_EventThrottleMapBounded`) |
| MODIFY | `internal/server/db/event_repo.go` | `EventTextMaxBytes` (1 KiB per field) and `EventRowTextMaxBytes` (3 KiB per row) on every stored event (`TestDUX_BND_EventTextCapped`) |
| MODIFY | `internal/server/handlers/handlers.go`, `device_probes.go` | Agent events and probe-failure events draw from the D-47 device bucket (B-19) |
| MODIFY | `internal/server/commands/validate.go`, `service.go` *(on main)* | D-50 `commands` quota: a 1 KiB stub over the quota and summary-only `target_results`; `bytes` and `result_bytes` written (AC-147) |
| MODIFY | `internal/server/db/command_repo.go` *(on main)*, `command_repo_pg_test.go` | Adaptive purge batches, output deleted before commands (AC-150); `SumResultBytes` and `EvictOldestResults` for the D-49 budget (AC-148) |
| CREATE | `internal/server/storage/command_budget.go`, `command_budget_test.go` | D-49 command-results budget (AD-041 §2) |
| MODIFY | `internal/server/agentrpc/agentrpc.go` | Server-wide concurrency cap `DockerReadsConcurrentServer` (16) (§13A B-11, AC-143) |
| CREATE | `internal/server/livelogs/hub.go`, `hub_test.go` | AD-016: stream serialization, retired IDs (an LRU of 256 per device, 10 min TTL), unknown-stream unfollow through a per-device bucket, `logs_keep`, backfill rate limit and coalescing, boundary validation, live re-stamp, audit attach-row cap (`TestDUX_SEC025_*`, `TestDUX_SEC026_ReattachStormBounded`, `TestDUX_BND_RetiredStreamIDsBounded`, `TestDUX_BND_AuditRowsUnderAttachStorm`) |
| CREATE | `internal/server/handlers/docker_logs_ws.go`, `docker_logs_ws_test.go` | Endpoint; SEC-012; socket expiry at the JWT `exp` and the 4 h socket cap (`TestDUX_SEC027_SocketClosedAtTokenExpiry`) |
| CREATE | `internal/server/agentrpc/agentrpc.go`, `agentrpc_test.go`; `internal/server/handlers/docker_inspect.go`, `docker_activity.go` (+ tests) | AD-019, AD-023 |
| MODIFY | `internal/server/handlers/terminal.go` *(on main)* | Unregister hook (also on `replace`); routing; terminal audit fields |
| MODIFY | `internal/server/server.go`, `router.go`, `router_test.go` | Wiring, retention, routes |

### 6.8 WP-2C (web, P2)

| Action | File | Purpose |
|---|---|---|
| CREATE | `web/src/api/liveLogs.ts`, `liveLogs.test.ts` | AD-018 |
| MODIFY | `web/src/hooks/useLogView.ts`, `LogView.tsx`, `LogToolbar.tsx` | Live half |
| CREATE | `web/src/hooks/useHiddenStop.ts`, `web/src/components/docker/logs/ConnectionIndicator.tsx` | FR-081, FR-083 |
| (dropped) | `LiveLogView.tsx`, `LiveLogView.test.tsx` | Merged into `LogView`; tests live in `LogView.test.tsx` (AC-048, 050..053, 058 UI, 059 UI) |
| (dropped) | `web/src/hooks/useLiveLogs.ts`, `useLiveLogs.test.ts` | Replaced by the Live half of `useLogView` (AC-054..AC-056 in `useLogView.test.ts`) |
| (dropped) | `web/src/components/docker/ContainerTabs/LogsTab.tsx` | No Live/History switch; the Logs tab renders `LogView` directly |
| CREATE | `web/src/components/docker/ContainerTabs/InspectTab.tsx`, `JsonTree.tsx`, `InspectTab.test.tsx` | FR-086..FR-090 (AC-062 UI, 064, 065 UI) |
| CREATE | `web/src/components/docker/activity/ActivityList.tsx`, `ActivityEntry.tsx`, `StepTimeline.tsx`, `ActivityFilters.tsx` | FR-091..FR-100; `ActivityList` keeps at most `ACTIVITY_MAX_PAGES` (40) pages through `useInfiniteQuery` `maxPages` (B-48, AC-162) |
| CREATE | `web/src/components/docker/activity/Activity.test.tsx` | AC-066..AC-072 (UI) |
| CREATE | `web/src/components/docker/ContainerTabs/ActivityTab.tsx` | container scope |
| CREATE | `web/src/pages/docker/ActivityPage.tsx`, `ActivityPage.test.tsx` | FR-098 |
| MODIFY | `web/src/components/docker/ContainerTabs/StatsTab.tsx` | FR-101..FR-106 (AC-073..AC-075 UI) |
| MODIFY | `web/src/components/ContainerMetricChart.tsx` | limit reference line (FR-105), zone axis (FR-104) |
| MODIFY | `web/src/api/client.ts`, `demo-client.ts`, `demo-data.ts`, `types/models.ts` | Activity, inspect, simulated live hub |
| MODIFY | `web/src/pages/docker/phases.ts` | `activity: true` |

### 6.9 WP-2D (server and web, needs PART-001)

| Action | File | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/<NNNNNN>_docker_ux_io_history.up.sql`, `.down.sql` | §7.2 |
| MODIFY | `internal/models/telemetry.go` | `ContainerMetric` rate fields |
| MODIFY | `internal/server/db/container_metric_repo.go` | `CopyFrom` columns, `GetHistory` select |
| MODIFY | `internal/server/handlers/handlers.go` | the post-work metric mapping includes rates |
| MODIFY | `web/src/components/docker/ContainerTabs/StatsTab.tsx` | I/O history charts (FR-107) |
| CREATE | `internal/server/db/container_metric_repo_pg_test.go` | partitioned insert and select |

### 6.10 WP-3A (agent, P3)

| Action | File | Purpose |
|---|---|---|
| CREATE | `internal/agent/stack_registry.go`, `stack_registry_test.go` | Agreed and pending paths; cap and prune (`TestDUX_BND_StackRegistryCapAndPrune`, B-25) |
| CREATE | `internal/agent/stackfs.go`, `stackfs_linux.go`, `stackfs_other.go`, `stackfs_test.go` | AD-021 (C-5) |
| MODIFY | `internal/agent/rwmounts.go`, `rwmounts_test.go` | Created in WP-1A. WP-3A applies it to every stack action (`TestDUX_SEC024_BindBackedVolumeCountsAsWritable`) |
| (moved) | `internal/agent/config.go` `docker.compose_trusted_roots` | Now in WP-1A (D-44) |
| CREATE | `internal/agent/docker_stack.go`, `docker_stack_test.go` | AD-027 |
| MODIFY | `internal/agent/docker_update.go` *(on main)* | `composeStackArgs`; path-trust precheck for `compose_work_dir` |
| MODIFY | `internal/agent/docker_read.go` | Compose and env reads (`compose_list`, `compose_file`, `env`) |
| CREATE | `internal/redact/compose.go`, `compose_test.go`, `dotenv.go`, `dotenv_test.go` | C-3, AC-083 |
| MODIFY | `internal/agent/commands.go` *(on main)* | dispatch the new actions |
| MODIFY | `internal/agent/agent.go` | update the stack registry after collection |
| MODIFY | `internal/models/command_policy.go` *(on main)* | class `docker_stack`, new step constants |

### 6.11 WP-3B (server, P3)

| Action | File | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/<NNNNNN>_docker_ux_p3_stacks.up.sql`, `.down.sql` | §7.2 P3 |
| CREATE | `internal/server/db/stack_repo.go`, `stack_repo_pg_test.go` | Observe, bounds, per-device write budget (`TestDUX_BND_StackObserveBudget`), acknowledgement (C-10) |
| MODIFY | `internal/server/db/interfaces.go`, `internal/testutil/mocks.go` | `StackRepository` |
| CREATE | `internal/server/handlers/docker_stacks.go`, `docker_stacks_test.go` | List, detail, actions, forget, acknowledge, compose relay (C-2; AC-076, 077, 080..082, 086 server) |
| MODIFY | `internal/server/handlers/auto_update.go`, `commands.go` *(on main)*, `handlers.go`, `router.go`, `router_test.go` | Agreed record, `validActions`, Observe, routes |

### 6.12 WP-3C (web, P3)

| Action | File | Purpose |
|---|---|---|
| CREATE | `web/src/pages/docker/StacksPage.tsx`, `StackPage.tsx`, tests | FR-108..FR-127 (AC-076..AC-088, AC-106) |
| CREATE | `web/src/components/docker/stack/ServicesTab.tsx`, `ComposeTab.tsx`, `EnvKeyList.tsx`, `StackLogsTab.tsx`, `StackActivityTab.tsx` | tabs; `StackLogsTab` hosts the stack `LogView` (`merged`) with the D-38 Live picker |
| CREATE | `web/src/components/docker/stack/StackPathsBanner.tsx` (+ test) | Path state banner with the acknowledge dialog (AD-038) |
| CREATE | `web/src/utils/stackState.ts`, `stackState.test.ts` | FRD §8.10 |
| MODIFY | `web/src/components/docker/ResourceHeader.tsx` | stack variant (FR-053); agreed path display in the header and in the dialogs |
| MODIFY | `web/src/components/ComposeStackSection.tsx` | heading links to the stack page (FR-115) |
| MODIFY | `web/src/components/docker/ContainerCells.tsx`, `OverviewTab.tsx` | stack links (FR-124, FR-127) |
| MODIFY | `web/src/hooks/usePaletteIndex.ts` | stacks from the API |
| MODIFY | `web/src/api/*`, `types/models.ts`, `pages/docker/phases.ts` | P3 |

### 6.13 WP-4A, WP-4B and WP-4C (P4)

**WP-4A (agent, P4)**

| Action | File | Purpose |
|---|---|---|
| CREATE | `internal/agent/collectors/docker_inventory.go`, `docker_inventory_test.go` | AD-028 (AC-090 bounds, cadence, unchanged hash) |
| MODIFY | `internal/agent/collectors/collector.go` | register; `SetInventoryForce` |
| CREATE | `internal/agent/docker_remove.go`, `docker_remove_test.go` | AD-029 with identity checks (AC-094, AC-096 image prune; SEC-007) |
| MODIFY | `internal/agent/commands.go` *(on main)* | `docker_remove_items`, `docker_inventory_refresh` |
| MODIFY | `internal/agent/telemetry.go` | `InventoryEnabled` flag |
| MODIFY | `internal/agent/doctor.go`, `doctor_test.go` | AD-028 doctor (AC-089) |
| MODIFY | `internal/models/telemetry.go`, `docker_caps.go` | `DockerInventory` types; `FullTelemetryData.DockerInventory`; P4 tokens |
| MODIFY | `README.md` | collector docs, `riot.role`, `.env` readability, the "upgrade server first" rule |
| CREATE | `internal/agent/docker_image_cleanup.go`, `docker_image_cleanup_test.go` | D-52: opt-in removal of a superseded image, per item, `Force:false`, never a `*Prune` API (AC-161) |
| MODIFY | `internal/agent/docker_update.go` *(on main)*, `docker_stack.go` (created in WP-3A), `config.go`, `doctor.go` | Record the previous image ID; the `cleanup_image` step; `docker.prune_superseded_images`; doctor prints it (D-52) |

**WP-4B (server, P4)**

| Action | File | Purpose |
|---|---|---|
| CREATE | `cmd/riot-server/migrations/<NNNNNN>_docker_ux_p4_inventory.up.sql`, `.down.sql` | §7.2 P4 |
| CREATE | `internal/server/db/inventory_repo.go`, `inventory_repo_pg_test.go` | latest-only upsert (AC-090) |
| CREATE | `internal/server/handlers/inventory_ingest.go`, `inventory_ingest_test.go` | AD-028 server bounds, size cap and cadence (C-10) |
| MODIFY | `internal/server/handlers/handlers.go` | extract before `StoreSnapshot`; upsert in the post-work |
| CREATE | `internal/server/handlers/docker_inventory.go`, `docker_inventory_test.go` | inventory GET, refresh, preview, execute (AC-091..AC-095, AC-097) |
| CREATE | `internal/server/handlers/prune_preview.go`, `prune_preview_test.go` | in-memory preview store; 128-bit IDs, identity binding and single-use handling (C-7); cap and sweep (`TestDUX_BND_PreviewStoreCapAndSweep`, B-12) |
| MODIFY | `internal/server/db/interfaces.go`, `internal/testutil/mocks.go`, `router.go`, `router_test.go` | P4 |

**WP-4C (web, P4)**

| Action | File | Purpose |
|---|---|---|
| CREATE | `web/src/pages/docker/ImagesPage.tsx`, `VolumesPage.tsx`, `NetworksPage.tsx`, tests | FR-132..FR-136 |
| CREATE | `web/src/components/docker/inventory/DiskUsageSummary.tsx`, `InventoryHostNotice.tsx`, `PruneDialog.tsx`, `PruneResult.tsx`, tests | FR-137..FR-143, FR-146 |
| MODIFY | `web/src/api/*`, `types/models.ts`, `pages/docker/phases.ts` | P4 |

**No other file changes.**

- `go.mod` and `go.sum` stay byte-identical.
- `web/package.json` and `web/package-lock.json` stay byte-identical, except for D-24 if it is approved.

---

## 7. Data Model Changes

### 7.1 Telemetry (JSON, no schema)

`ContainerInfo` additions (AD-004; `image_id` already exists on `main`):

```
started_at?, finished_at?, exit_code?, mem_limit_configured?,
net_rx_bytes?, net_tx_bytes?, blk_read_bytes?, blk_write_bytes?,
net_rx_rate?, net_tx_rate?, blk_read_rate?, blk_write_rate?
```

Other telemetry additions:

- `DockerInfo`: `capabilities?: string[]`, `inventory_enabled?: bool`, `container_logs_enabled?: bool`.
- `RiotLabels`: `role?: "server"|"database"`.
- `DockerEvent`: `command_id?`.
- `FullTelemetryData.container_log_gaps?[]` (P0, AD-035). Older servers ignore it.
- `FullTelemetryData.docker_inventory?` (P4). The server strips it before storage.

### 7.2 PostgreSQL

**P0 (WP-P0B): `<NNNNNN>_docker_ux_p0_log_agent_ts`** (D-42). It may land
before or after PART-001 (§2.4 rule 6).

```sql
-- up: nullable, no default, metadata-only on a plain or a partitioned table
ALTER TABLE container_logs ADD COLUMN agent_ts TIMESTAMPTZ;
-- down
ALTER TABLE container_logs DROP COLUMN agent_ts;
```

- `agent_ts IS NOT NULL` is the clock-skew flag: the row was re-stamped (AD-035 §1a). No separate boolean column is needed.
- No index is added. `agent_ts` is only read alongside `timestamp`.

**P0 (WP-P0B), same migration file: `device_storage_usage`** (rev 3.2,
D-50). The table is independent of PART-001.

```sql
-- up
CREATE TABLE device_storage_usage (
  device_id         TEXT        NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  day               DATE        NOT NULL,
  log_bytes         BIGINT      NOT NULL DEFAULT 0,
  telemetry_bytes   BIGINT      NOT NULL DEFAULT 0,
  device_log_bytes  BIGINT      NOT NULL DEFAULT 0,
  command_bytes     BIGINT      NOT NULL DEFAULT 0,
  bucket_levels     JSONB       NOT NULL DEFAULT '{}',   -- rev 3.3, D-54
  updated_at        TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (device_id, day)
) WITH (fillfactor = 70);
-- down
DROP TABLE device_storage_usage;
```

- **Writes.** One row per device per UTC day. It is upserted at most once per 60 s per device. `fillfactor` 70 keeps those updates HOT.
- **Daily totals** feed reporting and D-56 fairness. **`bucket_levels`** holds `{class: tokens}` as of `updated_at`. The latest row is authoritative at startup (AD-041 §1).
- **Retention.** The janitor deletes rows older than max(effective container-log retention + 1, 7) days, at most 91 (B-44).
- **Column types.** The engineer verifies that the type of `devices.id` matches.

**P0 (WP-P0C): `<NNNNNN>_docker_ux_p0_log_name_index`.** This runs after
PART-001's renumbered conversion, on the partitioned parent table.

```sql
CREATE INDEX idx_container_logs_name ON container_logs (device_id, container_name, timestamp DESC);
-- down: DROP INDEX idx_container_logs_name;
```

- `container_logs` itself changes only through PART-001.
- **`stream` values are server-owned (SEC-023).** Ordinary rows hold `stdout` or `stderr`. Gap rows hold `gap_agent` or `gap_server`, and only server code writes them.
- **No CHECK constraint is added.** Validating a CHECK on the partitioned parent would need a full scan of every partition, including PART-001's legacy partition, under `ACCESS EXCLUSIVE` at startup. A single legacy row with an unexpected value would then stop the server from starting.
  - The server allowlist is the control. `TestDUX_SEC023_ForgedGapStreamNotStored` covers it.
  - The history API maps only well-formed gap rows (AD-036).

**P1: `<NNNNNN>_docker_ux_p1_bulk_results`.**

```sql
-- up
ALTER TABLE commands ADD COLUMN target_results JSONB NOT NULL DEFAULT '[]';
-- down
ALTER TABLE commands DROP COLUMN target_results;
```

`commands`, before and after (only the DOCKER-UX columns are shown;
CMD-LIFECYCLE §5.1 is assumed):

| Column | Before | After |
|---|---|---|
| `target_results` | none | JSONB, at most 100 entries (each `{container, outcome, message ≤1 KiB, step}`), written once at terminal |

**P2: `<NNNNNN>_docker_ux_p2_activity`.**

```sql
ALTER TABLE commands ADD COLUMN actor_kind TEXT NOT NULL DEFAULT 'unknown'
  CHECK (actor_kind IN ('operator','system','unknown'));
ALTER TABLE commands ADD COLUMN actor_auth TEXT NOT NULL DEFAULT '';   -- password|oidc|system|legacy|''
ALTER TABLE commands ADD COLUMN actor_sub  TEXT NOT NULL DEFAULT '';
ALTER TABLE commands ADD COLUMN actor_name TEXT NOT NULL DEFAULT '';
ALTER TABLE commands ADD COLUMN steps JSONB NOT NULL DEFAULT '[]';
ALTER TABLE commands ADD COLUMN result_bytes INTEGER NOT NULL DEFAULT 0;   -- rev 3.2, D-49/D-50
CREATE INDEX idx_commands_created ON commands (created_at DESC) INCLUDE (result_bytes);

ALTER TABLE command_output DROP CONSTRAINT command_output_stream_check;
ALTER TABLE command_output ADD CONSTRAINT command_output_stream_check
  CHECK (stream IN ('stdout','stderr','combined','steps'));
ALTER TABLE command_output ADD COLUMN bytes INTEGER NOT NULL DEFAULT 0;     -- rev 3.2, D-49/D-50
CREATE INDEX idx_command_output_created ON command_output (created_at) INCLUDE (bytes);

ALTER TABLE events ADD COLUMN command_id   TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN command_attr TEXT NOT NULL DEFAULT ''
  CHECK (command_attr IN ('','agent','inferred'));
CREATE INDEX idx_events_device_container ON events (device_id, container_name, created_at DESC)
  WHERE container_name <> '';

ALTER TABLE terminal_sessions DROP CONSTRAINT terminal_sessions_device_id_fkey;
ALTER TABLE terminal_sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'terminal'
  CHECK (kind IN ('terminal','logs','inspect','compose','command','stack_paths','device_delete'));
ALTER TABLE terminal_sessions ADD COLUMN actor_auth TEXT NOT NULL DEFAULT '';
ALTER TABLE terminal_sessions ADD COLUMN actor_sub  TEXT NOT NULL DEFAULT '';
ALTER TABLE terminal_sessions ADD COLUMN actor_name TEXT NOT NULL DEFAULT '';
ALTER TABLE terminal_sessions ADD COLUMN target TEXT NOT NULL DEFAULT '';
ALTER TABLE terminal_sessions ADD COLUMN detail JSONB NOT NULL DEFAULT '{}';
ALTER TABLE terminal_sessions ADD COLUMN retention_class TEXT NOT NULL DEFAULT 'long'
  CHECK (retention_class IN ('long','short'));
CREATE INDEX idx_terminal_sessions_started ON terminal_sessions (started_at);
-- down: drop the additions; restore the stream CHECK after deleting 'steps' rows;
-- re-add the device FK as NOT VALID, so audit rows of deleted devices survive;
-- never delete audit rows (SEC-029).
```

Notes:

- `commands` on `main` also has `commands_origin_check` and `commands_progress_check` (§2.5). This migration does not touch them.
- **Constant defaults.** All added columns use constant defaults, so on PG 11+ they are metadata-only and the table is not rewritten.
- **Index builds.** The indexes on `events` and `commands` are built without `CONCURRENTLY` (golang-migrate runs each file in one transaction). They run at startup, before traffic. On a large `events` table the build takes seconds to minutes, and the release notes mention it.
- **Constraint names.** `command_output_stream_check` is PostgreSQL's generated name for the 000017 inline CHECK. The engineer verifies it and `terminal_sessions_device_id_fkey` against a database at the current schema, and records them in the implementation report.
- **Actor kinds.** `agent_observed` is not a command actor kind: it is derived for event entries.

**P3: `<NNNNNN>_docker_ux_p3_stacks`.**

```sql
CREATE TABLE docker_stacks (
  device_id              TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  project                TEXT NOT NULL,
  agreed_working_dir     TEXT NOT NULL DEFAULT '',
  agreed_config_files    TEXT[] NOT NULL DEFAULT '{}',
  observed_working_dir   TEXT NOT NULL DEFAULT '',
  observed_config_files  TEXT[] NOT NULL DEFAULT '{}',
  path_state             TEXT NOT NULL DEFAULT 'agreed'
                         CHECK (path_state IN ('agreed','conflicted','pending_ack')),
  conflict_members       TEXT[] NOT NULL DEFAULT '{}',
  path_changed_at        TIMESTAMPTZ,
  first_seen_at          TIMESTAMPTZ NOT NULL,
  last_seen_at           TIMESTAMPTZ NOT NULL,
  forgotten_at           TIMESTAMPTZ,
  PRIMARY KEY (device_id, project)
);
ALTER TABLE docker_stacks SET (autovacuum_vacuum_scale_factor = 0.05, autovacuum_vacuum_threshold = 50);
-- down: DROP TABLE docker_stacks;
```

The AD-026 bounds are enforced in code.

**P4: `<NNNNNN>_docker_ux_p4_inventory`.**

```sql
CREATE TABLE docker_inventory (
  device_id    TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
  collected_at TIMESTAMPTZ NOT NULL,
  received_at  TIMESTAMPTZ NOT NULL,
  hash         TEXT NOT NULL,
  data         JSONB NOT NULL
) WITH (autovacuum_vacuum_threshold = 10, autovacuum_vacuum_scale_factor = 0,
        toast.autovacuum_vacuum_threshold = 10, toast.autovacuum_vacuum_scale_factor = 0);
-- down: DROP TABLE docker_inventory;
```

**WP-2D: `<NNNNNN>_docker_ux_io_history`** (only after PART-001).

```sql
ALTER TABLE container_metrics
  ADD COLUMN net_rx_rate DOUBLE PRECISION, ADD COLUMN net_tx_rate DOUBLE PRECISION,
  ADD COLUMN blk_read_rate DOUBLE PRECISION, ADD COLUMN blk_write_rate DOUBLE PRECISION;
-- down: drop the four columns
```

### 7.3 New Go structures

| Structure | Fields |
|---|---|
| `models.TargetResult` | `Container`, `Outcome`, `Message`, `Step string` |
| `models.StepRecord` | `Seq int`, `Name string`, `StartedAt`, `FinishedAt *time.Time`, `Outcome string`, `Output string`, `Truncated bool` |
| `models.ContainerLogGap` | `ContainerID`, `ContainerName`, `Timestamp`, `Count`, `LowerBound`, `Reason` |
| `handlers.deviceLogStats` (in memory) | `RestampedHourly [24]int`, `LastSkewSeconds int64`, `LastRestampAt time.Time`, `ConsecutiveSkewIngests int`, `RejectedHourly [24]int`. One per device; reset on restart (AD-035 §1a, §5) |
| `models.Initiator` | `Kind`, `Auth`, `Sub`, `Name`. Carried on `DispatchRequest` |
| `models.AccessAudit` | `DeviceID`, `Kind`, `ActorAuth`, `ActorSub`, `ActorName`, `Target`, `RemoteAddr`, `SessionID`, `RetentionClass`, `Detail map[string]any` |
| `models.DockerInventory` | `CollectedAt time.Time`; `Hash string`; `Unchanged bool`; `Images []InvImage`; `Volumes []InvVolume`; `Networks []InvNetwork`; `DiskUsage InvDiskUsage`; `SizesAvailable bool`; `Truncated InvTruncation` |
| `InvImage` | `ID`, `Tags[]`, `Digests[]`, `Size`, `Created`, `Containers int`, `Dangling bool` |
| `InvVolume` | `Name`, `Driver`, `Mountpoint`, `Created`, `Project`, `Anonymous bool`, `UsedBy []string`, `Size *int64` |
| `InvNetwork` | `ID`, `Name`, `Driver`, `Scope`, `Internal bool`, `Subnets []{Subnet, Gateway}`, `Containers []string`, `BuiltIn bool` |
| `InvDiskUsage` | `Images`, `Volumes`, `BuildCache {Total, Reclaimable int64}` |
| `db.StackRecord` | `DeviceID`, `Project`, `AgreedWorkingDir`, `AgreedConfigFiles`, `ObservedWorkingDir`, `ObservedConfigFiles`, `PathState`, `ConflictMembers`, `PathChangedAt`, `FirstSeenAt`, `LastSeenAt time.Time`, `ForgottenAt *time.Time` |
| `PreviewItem` | `ID`, `Tags`, `Name`, `CreatedAt` |
| `commands.DispatchRequest` | Adds `ProbeKeys`, `DockerExclusive`, `IsDocker` and `Initiator` |
| `quota.DailyUsage` (in memory; persisted every 60 s) | `DeviceID`, `Day` (UTC date), `LogBytes`, `TelemetryBytes`, `DeviceLogBytes`, `CommandBytes int64`, `Exceeded map[string]bool`, `Dirty bool`. One per active device (B-44) |
| `storage.HeadroomState` | `Source` (`statfs`, `configured` or `none`), `FreeBytes`, `TotalBytes int64`, `Mode` (`normal`, `low` or `emergency`), `Since time.Time` |
| `ingest.Limits` (in memory) | One token bucket per (authenticated device, route class); idle eviction after 10 min (B-14, B-43) |

### 7.4 Agent persisted files

| File | Shape | Bound |
|---|---|---|
| `/var/lib/riot/docker-stacks.json` (0600) | `{"version":2,"stacks":{"<project>":{"agreed":{…},"pending"?:{…},"last_seen"}}}` | At most 500 projects. An entry not seen for 90 days is pruned. |

---

## 8. API / Interface Contract

These rules apply to every endpoint in this section:

- Admin endpoints use the JWT cookie (`AdminAuth`).
- **AD-034 applies to every state-changing route here.** `SameOriginJSON` returns `403 origin_refused` on failure. Side-effecting GETs (§8.6) also go through `RequireRiotHeader`.
- Errors use `{"error": "<human text>", "code": "<enum>", …}`.
- Identifiers follow AD-002.
- These contracts are **frozen for WP-nC**.

### 8.1 Common errors

| Code | HTTP | Meaning |
|---|---|---|
| `invalid_identifier`, `invalid_request`, `invalid_filter`, `too_many_targets`, `confirmation_required` | 400 | Validation failed |
| `origin_refused` | 403 | The request failed the AD-034 origin check |
| `device_not_found`, `container_not_found`, `stack_not_found`, `command_not_found` | 404 | Resource absent |
| `conflict` (with `existing_command_id`, `existing_status`, `target_key`), `agent_unsupported` (with `capability`), `not_down`, `inventory_stale`, `preview_expired` | 409 | State conflict |
| `stack_paths_unagreed` | 409 | The stack's paths are not agreed (AD-038) |
| `too_many_streams` | 409 | The host has no free live stream slot |
| `too_many_viewers` | 409 | The stream or the server has no free viewer slot |
| `read_refused` (with `reason`) | 422 | The agent refused an on-demand read (AD-019 reason enum) |
| `rate_limited` | 429 | NFR-008 |
| `agent_offline` | 502 | Host not connected (on-demand reads and the log WS pre-upgrade only) |
| `agent_timeout` | 504 | No answer within 10 s |

`read_refused` gains these reasons: `conflicted`, `paths_changed`,
`container_writable`, `not_regular`, `unsupported`.

### 8.2 `GET /api/v1/fleet/containers` (extended, P1)

`200` returns `FleetContainerRow[]`:

```
{ device_id, hostname, container_id, short_id, container_name, image, image_id?,
  state, status, health_status?, started_at?, finished_at?, exit_code?,
  ports: [{container_port, host_port?, protocol, host_ip?}],
  update_available?, update_status?, update_reason?,
  stack?, compose_service?, target_key, stack_key?,
  riot_name?, riot_icon?, riot_group?, riot_tags?: string[],
  cpu_percent, mem_usage, mem_limit, mem_limit_configured?, restart_count,
  network_parent?, self_role? }
```

- `stack` keeps its FLEET-DASH meaning (compose project).
- `network_parent` is the parent name derived from `network_mode`
  `container:<x>`/`service:<x>`. The raw mode is not exposed.
- **Never present:** `labels`, `env`, `mounts`, `networks`.

### 8.3 `GET /api/v1/docker/hosts` (P1)

`200` returns:

```
[{ device_id, hostname, status, last_seen?, primary_ip?, docker_available,
   capabilities: string[], inventory_enabled, self_role?,
   container_logs: "enabled"|"disabled"|"unknown",
   log_clock_skew?: { restamped_24h, last_skew_s, last_restamp_at },
   log_ingest?: { rejected_24h },
   storage_quota?: { day, used: { logs, telemetry, device_logs, commands },
                     quota: { logs, telemetry, device_logs, commands },
                     level: { logs, telemetry, device_logs, commands },
                     mode: { logs: "normal"|"sampled", telemetry: "normal"|"downsampled",
                             device_logs: "normal"|"sampled", commands: "normal"|"stub" },
                     fairness?: { limited: bool, logs_allowance_per_day } } }]
```

### 8.4 `POST /api/v1/docker/bulk-actions` (P1)

Request (`SameOriginJSON`):

```
{ "action": "start"|"stop"|"restart"|"update",
  "targets": [{"device_id","container"}],
  "confirm_count"?: int, "confirm_host"?: string }
```

| Status | Body |
|---|---|
| 200 | `{"results":[{"device_id","container","outcome":"created"|"conflict"|"skipped"|"error","command_id"?,"existing_command_id"?,"reason"?:"not_found"|"not_applicable"|"agent_offline_queued"|"internal"}]}`. Offline hosts still get `created` (queued, FR-045) |
| 400 | `too_many_targets` (more than 100), `confirmation_required`, `invalid_request` |
| 403 | `origin_refused` |

### 8.5 Commands (additive)

`Command` JSON (CMD-LIFECYCLE §6.1) gains:

- `target_results` (terminal commands only);
- `actor_kind`, `actor_auth` and `actor_name`;
- `steps:[{seq,name,started_at,finished_at?,outcome?}]`.

`command_update` WS messages gain:

- `actor_kind`, `actor_auth` and `actor_name`;
- `target_results` (terminal only).

`actor_sub` appears only on the Activity endpoints. All free text is masked.

`POST /devices/{id}/commands` accepts `confirm_host` in the body next to
`params` for self-target stops (AD-009).

### 8.6 On-demand reads (P2/P3, `RequireRiotHeader`)

These reads require `X-Riot-Request: 1` and a same-origin request.

| Method and path | Success | Errors |
|---|---|---|
| `GET /api/v1/devices/{id}/docker/containers/{name}/inspect` | `200 {"json": "<indented redacted JSON>", "redactions": n, "fetched_at"}` | 403 `origin_refused`; 404 `container_not_found` (the name is checked against the latest snapshot before any agent call); 409 `agent_unsupported`; 502; 504; 429; 422 `{"code":"read_refused","reason":<AD-019 enum>}` |
| `GET /api/v1/devices/{id}/docker/stacks/{project}/compose` | `200 {"working_dir","read_only":true,"path_state","container_writable","files":[{"index","name","size","status","container_writable"}],"env":{"present","status"}}` | as above, with `stack_not_found`; 409 `stack_paths_unagreed` |
| `GET …/stacks/{project}/compose/files/{index}` | `200 {"name","content","redactions","line_count"}` | as above |
| `GET …/stacks/{project}/env` | `200 {"keys":[{"key","value"}],"redactions"}` | as above |

Every one of these sends `Cache-Control: no-store`, and none accepts a body
or a path parameter.

### 8.7 Stacks (P3)

| Method and path | Request | Success | Errors |
|---|---|---|---|
| `GET /api/v1/docker/stacks` | none | `200 [{device_id, hostname, project, state:"running"|"partial"|"down", running, total, updates, auto_update, self_role?, last_seen_at, path_state, agreed_working_dir, agreed_config_files, observed_*?, conflict_members}]` (forgotten stacks are excluded) | 500 |
| `GET /api/v1/docker/stacks/{deviceId}/{project}` | none | `200 {…list fields, working_dir, config_file_count, services:[FleetContainerRow]}` | 404 |
| `POST /api/v1/devices/{id}/docker/stacks/{project}/actions` | `{"action":"pull"|"update"|"redeploy"|"restart"|"down"|"up","confirm"?,"confirm_host"?}` (`DisallowUnknownFields`) | `201 Command` | 409 `conflict` / `agent_unsupported` / `stack_paths_unagreed`; 400 `confirmation_required` / `invalid_request` |
| `POST /api/v1/devices/{id}/docker/stacks/{project}/forget` | `{}` | `204` | 409 `not_down`; 404 |

In the list and detail responses, `observed_working_dir` and
`observed_config_files` are present when the state is `pending_ack`.

**New:**
`POST /api/v1/devices/{id}/docker/stacks/{project}/paths/acknowledge`, with
body `{"working_dir","config_files":[]}`.

| Status | Meaning |
|---|---|
| 200 | `{path_state:"agreed"}` |
| 409 `not_pending` | The stack is not waiting for acknowledgement |
| 409 `paths_mismatch` | The body does not match the observed paths |

While a stack is not agreed, stack actions return
`409 stack_paths_unagreed`.

### 8.8 Inventory and prune (P4)

| Method and path | Request | Success | Errors |
|---|---|---|---|
| `GET /api/v1/docker/inventory?kind=images|volumes|networks` | none | `200 {"hosts":[{device_id, hostname, state:"ok"|"not_enabled"|"unsupported"|"never_collected", collected_at?, stale, truncated, disk_usage?}],"items":[…kind rows with device_id…]}` | 400 |
| `POST /api/v1/devices/{id}/docker/inventory/refresh` | `{}` | `201 Command` (`docker_inventory_refresh`) | 409 |
| `POST /api/v1/devices/{id}/docker/prune/preview` | `{"kind":"images"|"images_all"|"volumes"|"networks"|"selected","item_kind"?: "image"|"volume"|"network","items"?:[id or name]}` | `200 {"preview_id","item_kind","items":[{"id","name","size"?,"selected_default","tags"? (images),"created_at"? (volumes)}],"count","reclaimable_bytes","inventory_age_s","requires_refresh","confirmation":{"kind":"host"|"count"|"none","count"?}}` | 409 `agent_unsupported`; 400 |
| `POST /api/v1/devices/{id}/docker/prune/execute` | `{"preview_id","selected":[id or name],"confirm"?}` | `201 Command` (`docker_remove_items`) | 409 `inventory_stale` / `preview_expired` / `conflict`; 400 `confirmation_required` / `invalid_request` (selected not in the preview) |

- `preview_id` is 32 hex characters, consumed on any outcome.
- Per-item outcomes include `Skipped: changed` (AD-029).

### 8.9 Activity (P2)

`GET /api/v1/docker/activity` takes these query parameters:

- `device_id?`;
- `container?` (requires `device_id`);
- `stack?` (requires `device_id`);
- `actor?=operator,system,agent,unknown`, or `sub:<actor_sub>`;
- `state?=<canonical csv>`;
- `type?=command,event`;
- `from?`, `to?` (RFC3339);
- `cursor?`;
- `limit?` (default 25, maximum 100).

The response is:

```
200 {"entries":[{
   "kind":"command"|"event", "id", "device_id", "hostname", "container"?, "stack"?,
   "action", "actor_kind":"operator"|"system"|"agent_observed"|"unknown",
   "actor_auth", "actor_name", "actor_sub",
   "attribution"?: ""|"agent"|"inferred" (event entries),
   "status"?, "step"?, "failure_step"?, "message" (masked, ANSI-stripped, ≤ 2 KiB),
   "requested_at", "started_at"?, "finished_at"?, "duration_ms"?, "target_exists": bool }],
 "next_cursor"?}
```

`GET /api/v1/docker/activity/commands/{commandId}?device_id=` returns:

```
200 {"entry":{…},"steps":[{"seq","name","started_at","finished_at"?,"outcome"?,"output"?,"truncated"?}],
     "output_truncated": bool, "target_results":[…], "events":[{type, created_at, message}]}
```

It returns 404 `command_not_found`.

### 8.10 WebSocket: browser live logs (P2, rewritten)

`GET /ws/docker/logs/{deviceId}` requires the admin cookie and a present
`Origin` equal to the server's own origin. Pre-upgrade errors are HTTP 400,
401, 403, 409 and 502.

| Direction | Message |
|---|---|
| browser → server | `{"type":"attach","containers":[{"name","boundary"?}],"merged":bool}` · `{"type":"detach","containers"?:[name]}` |
| server → browser | `{"type":"session","state":"connecting"\|"live"\|"reconnecting"\|"unavailable","reason"?}` |
| server → browser | `{"type":"lines","container":name,"src":"live"\|"backfill","lines":[{"t","s","l","x"?,"agent_t"?}]}`. `agent_t` is present when the line was re-stamped (D-42) |
| server → browser | `{"type":"marker","container":name,"kind":"backfill_end"\|"dropped"\|"exited"\|"restarted","n"?,"skipped"?,"skipped_lb"?,"code"?}` |
| server → browser | `{"type":"error","container"?:name,"code":"not_found"\|"too_many_streams"\|"too_many_viewers"\|"agent_offline"\|"unsupported"\|"invalid_request"\|"client_too_slow"\|"session_expired"}`. `session_expired` closes the socket (SEC-027) |

### 8.11 WebSocket: agent and server (rewritten)

| Message | Purpose |
|---|---|
| `logs_follow`, `logs_unfollow`, `logs_keep`, `logs_backfill`, `logs_data`, `logs_event` | Live log streams, keep-alive and backfills (AD-015, SEC-025) |
| `docker_read`, `docker_read_result` | On-demand reads (`session_id` = request ID, AD-019) |
| `command_result` | Gains `target_results?` and `steps?`. Both are sanitized, masked and bounded on the server (AD-008, AD-023) |

**New command actions:**

- `docker_stack_pull`, `docker_stack_redeploy`, `docker_stack_restart`,
  `docker_stack_down`, `docker_stack_up`;
- `docker_remove_items`;
- `docker_inventory_refresh`.

**New step names** (they match `^[a-z0-9_]{1,64}$`), with their labels:

| Step | Label |
|---|---|
| `restart` | Restarting |
| `down` | Taking down |
| `up` | Starting services |
| `remove` | Removing |
| `collect` | Collecting inventory |
| `cleanup_image` | Removing superseded image (D-52) |

The CMD-LIFECYCLE step labels are unchanged.

### 8.12 Stored-log history (P1, new)

| Endpoint | Response | Errors |
|---|---|---|
| `GET /api/v1/devices/{id}/docker/containers/{name}/logs?from&to&before&after&limit` | The AD-036 shape | 400 `invalid_identifier` or `invalid_request`; 404 `container_not_found` |
| `GET /api/v1/devices/{id}/docker/stacks/{project}/logs?services&from&to&before&after&limit` | The same shape, plus `container` on each entry | as above |

The existing `GET /devices/{id}/containers/{cid}/logs` stays, for older
dashboards. From P1 its lines are masked at read time.

From P0 (WP-P0B), stored lines are also masked at write time (SEC-028).
History entries for re-stamped lines carry `agent_t` (AD-036). No history
response contains a row dated after `now + 5 min` (SEC-021).

### 8.13 Configuration (P0, new)

**Server.** `RIOT_CONTAINER_LOG_RETENTION_DAYS` sets the container-log
retention in whole days. The default is 7 and the accepted range is 1 to 90.
Any invalid value falls back to 7 with a startup warning. The effective value
appears in the history API's `retention_days` field.

**Server, rev 3.2 (AD-041).** Each setting below falls back to its default,
with a startup warning, when it is invalid.

| Setting | Default | Meaning |
|---|---|---|
| `RIOT_CONTAINER_LOG_MAX_GB` | 16 | Fleet budget for `container_logs` (D-49) |
| `RIOT_COMMAND_RESULTS_MAX_GB` | 4 | Fleet budget for command results (D-49) |
| `RIOT_DB_FS_PROBE_PATH` | unset (`/riot-fsprobe` in the shipped compose files) | A path on the **same filesystem** as the database volume, used for `statfs` (disk headroom). Never the database volume itself (SEC-040). Replaces rev 3.2's `RIOT_DB_VOLUME_PATH` |
| `RIOT_DB_FREE_LOW_GB` | unset (computed: max(4, min(15 % of total, 50)) GiB) | Overrides the low threshold (D-55) |
| `RIOT_DB_FREE_EMERGENCY_GB` | unset (computed: max(1, min(5 % of total, 10)) GiB) | Overrides the emergency threshold (D-55) |
| `RIOT_DB_MAX_GB` | unset | DB volume size, used when no path is set |

**Agent `agent.yaml`:**

```
collector:
  container_logs: { max_lines: 50, max_bytes: 16384 }
docker:
  compose_trusted_roots: []   # host-local only (AD-038)
  prune_superseded_images: false   # P4, opt-in, host-local (D-52)
```

### 8.14 Agent ingest limits (P0, rev 3.2; AD-040)

These are the existing agent routes. Only the limits and the responses to
excess are new.

| Route | Body cap | Rate per device | Over the rate | Over the body cap |
|---|---|---|---|---|
| `POST /api/v1/devices/{id}/telemetry` | 25 MiB | 1 per 15 s, burst 120 | `429 {"error":"rate limited","code":"rate_limited"}` with `Retry-After: <seconds>`; nothing stored | `413 {"error":"payload too large","code":"payload_too_large"}` |
| `POST …/heartbeat` | 64 KiB | 1 per 10 s, burst 6 | `200 {"status":"ok","throttled":true}`; nothing stored | 413 |
| `POST …/docker-events`, `POST …/events` | 1 MiB | 120 per minute, shared | 429 with `Retry-After`; nothing stored or broadcast | 413 |
| `POST …/logs` | 8 MiB | 6 per minute | 429 with `Retry-After` | 413 |
| `POST …/probe-results` | 1 MiB | 6 per minute; at most 64 results per push | 429 with `Retry-After`. Excess results, and results for unknown probes, are dropped: `200` with `"dropped": n` | 413 |
| `GET /ws/agent` frames | 4 MiB per frame | 200 frames per second, burst 400 | Frames dropped and counted. After 3 s of continuous excess, the socket closes with 1008 | Socket closed with 1009 |

**Quotas.** When a quota (AD-041 §1) samples or downsamples storage, the
agent still gets `200`, because it cannot fix the problem. Storage never
stops entirely. The operator sees the event, and `storage_quota` in
`GET /docker/hosts`.

---

## 9. Sequence / Flow

### 9.1 Stored-log ingest (P0)

1. **Agent collection,** every `poll_interval:**
   - For each running container, and each container stopped since the last collection, the agent scans at most 20,000 lines and 8 MiB from `Since=lastSeen`.
   - `capRing` keeps the newest 50 lines within 16 KiB.
   - Lines it evicts become an `agent_cap` gap.
2. The agent sends `container_logs` and `container_log_gaps` in its telemetry.
3. **Server ingest:**
   - `Telemetry` extracts both before `StoreSnapshot`.
   - The post-work applies `applyCeiling`: the per-ingest limit and the rolling limit, recording `gap_server` gaps.
   - First it validates entries and gap entries, applies the timestamp window with re-stamping (AD-035 §1a), and applies the per-device ceiling (AD-035 §5).
   - It then masks the lines (from P0, WP-P0B) and writes them with `CopyFrom` into the table, which is partitioned once WP-P0C ships.
4. **Every hour,** PART-001's manager drops each day partition whose upper bound is at or before `now − retention`, and logs the run.
5. **Quotas, budget and headroom (rev 3.2; reshaped in rev 3.3).**
   - Before `CopyFrom`, the lines draw from the device's `logs` bucket (AD-041 §1). In sampled mode, priority lines go first, other stdout is sampled, and the rest become `gap_quota` counts.
   - Each janitor run measures the container-log partitions against the fleet budget and the DB free space.
     - Over the budget, it first lowers the heaviest devices' allowances (D-56).
     - It drops the oldest days early only above 110 % of the budget, or under disk pressure (AD-041 §2, §3).

### 9.2 Logs tab: history, then Live (P1 and P2)

1. **The tab opens.** The client calls the history API for the last hour and renders the newest 5,000 lines with gaps, the retention text, and "History up to …".
2. **Refresh,** every 15 s: the client queries with `after = newest`. The buffer removes duplicates at that timestamp.
3. **Live on:**
   1. The client opens the WebSocket and sends `attach{web, boundary=B}`.
   2. The hub joins the existing stream for `web`, or opens one with `logs_follow`.
   3. The hub requests `logs_backfill(after=B)` for this viewer.
   4. Live lines arrive and are held as pending.
   5. When `backfill_end{n, skipped}` arrives, the client merges backfill and pending lines by identity, adds a gap marker if anything was skipped, and pauses history refresh.
4. **A second tab** joins the same stream and gets its own backfill. The agent still runs one follow.
5. **Live off in tab 1:** tab 1 detaches, and the stream continues for tab 2. When tab 2 also turns Live off, the hub waits its 2 s linger and sends `logs_unfollow`. The agent stops within 1 s.

### 9.3 Stack path change (P3)

1. **Change observed.** Telemetry shows every member of `media` agreeing on new paths. `Observe` sets `pending_ack`, and writes an event and an audit row.
2. **Actions blocked.** The UI shows a banner with the old and new paths. Every action returns 409.
3. **Acknowledged.** The operator chooses "Accept new paths" and confirms. The client calls `POST …/paths/acknowledge` (AD-034). The paths become agreed, and an audit row is written.
4. **Next Up.** The request carries the new paths. Before it executes, it must pass the agent registry, the live members, the writable-path check, and `stackfs`.

### 9.4 Other flows

In every flow below, the AD-034 checks run first.

#### 9.4.1 Bulk Restart across two hosts (P1)

1. The operator selects rows and opens Restart (4). The client opens
   `BulkConfirmDialog`. It lists the applicable targets by host, the skipped
   rows with reasons (from `bulkApplicability` plus active commands), the
   network dependents (rows whose `network_parent` is a target), and the
   offline-host notice.
2. On Confirm, the client sends `POST /docker/bulk-actions`.
3. The server validates and resolves each target against the latest
   snapshot, then calls `Dispatch` for `docker_restart` per target, with the
   AD-007 `DispatchRequest` fields. Each returns created or conflict.
4. The server responds 200 with the results. The client opens
   `BulkProgressPanel` with one tracker per command ID. `command_update`
   messages drive the labels until all are terminal, then a summary toast
   appears.

#### 9.4.2 Inspect (P2)

1. The browser sends `GET …/inspect` with `X-Riot-Request: 1`. The server
   checks the header and origin, validates the name, checks the capability,
   and applies the rate limit.
2. `agentrpc.Call(inspect)` sends `docker_read` to the agent. The agent runs
   `ContainerInspect`, then `redact.InspectJSON`, and replies with
   `docker_read_result`.
3. The server checks the size, audits, and responds 200 with `no-store`. If
   10 s pass, it responds 504.

#### 9.4.3 Stack Down (P3)

1. The header ⋯ menu opens Down. A danger dialog appears and shows the
   agreed paths. The operator types "media"; when the stack is a self stack,
   they also type the host name.
2. The client sends `POST …/stacks/media/actions{action:"down",confirm:"media"}`.
3. The server validates the confirmation, requires `path_state='agreed'`
   (else `409 stack_paths_unagreed`), reads the agreed paths from
   `docker_stacks`, and dispatches `docker_stack_down`. The stored keys are
   `docker:compose:<wd>` plus the members' container keys.
4. The agent's precheck checks the live members and its registry against the
   agreed paths, runs the writable-path check and `stackfs` on the files, and
   takes the locks. It then runs `compose … down` (no `-v`), reports the
   result with `steps`, and triggers telemetry.
5. The containers vanish from telemetry. The stack stays listed as Down
   (from `docker_stacks`) with Up as its primary action.

#### 9.4.4 Prune volumes (P4)

1. The operator opens Prune volumes. The client sends `POST …/prune/preview{kind:"volumes"}`.
2. The server reads the latest inventory and returns the unused volumes
   (each with `name` and `created_at`), with anonymous ones selected by
   default, plus the age and `requires_refresh`. The preview ID is 128-bit,
   single use, and bound to the device, kind, items, actor and expiry.
3. If the age is over 15 minutes, the client calls
   `POST …/inventory/refresh`, tracks the command to terminal, and then
   requests the preview again.
4. The operator types the host name and confirms. The client sends
   `POST …/prune/execute{preview_id, selected, confirm}`.
5. The server validates, consumes the preview, and dispatches
   `docker_remove_items` with `[docker:host]`, DockerExclusive. On conflict
   it returns 409 with the blocking command.
6. The agent takes the `docker:host` lock, re-checks usage and each volume's
   `CreatedAt`, and removes the items one by one. The result carries
   `target_results` per item.
7. The client shows the result. Activity lists the entry.

---

## 10. Acceptance Criteria Mapping

Test locations use the names from §6. Go tests are `TestDUX_ACnnn_*` (and
`TestDUX_SEC0nn_*` for the §19 security tests). Web tests are
`[DUX AC-nnn]` describe blocks. ACs changed in FRD revision 2 are marked
"(rev 2)"; ACs new in FRD revision 2 are in bold.

| AC | Fulfilled by | Test |
|---|---|---|
| AC-001 | `App.tsx` Docker `NavLink`; `DockerLayout` `aria-current` | `DockerLayout.test.tsx`: render, click, assert route and attribute |
| AC-002 | `phases.ts DOCKER_SHIPPED` read by `DockerLayout` | `DockerLayout.test.tsx` with the P1 flags |
| AC-003 | `ContainersPage` `QueryState`: `isEmpty` when there are no Docker hosts, error panel on 500 | `ContainersPage.test.tsx` (two cases) |
| AC-004 (rev 2) | `LegacyContainersRedirect` `Navigate replace` to `/docker/containers?host=<id>` | `redirects.test.tsx`: MemoryRouter history length and Back |
| AC-005 | `LegacyContainerRedirect` resolution and the not-found `EmptyState` | `redirects.test.tsx` |
| AC-006 | `ContainerPage` name lookup and `lastContainer` | `ContainerPage.test.tsx`: swap the ID in the cache and re-render; fresh render from the URL |
| AC-007 (rev 2) | The Docker link on `DeviceDetail` | `DeviceDetail.test.tsx` link `href` |
| AC-008 | `DataTable` rows and footer text | `ContainersPage.test.tsx` |
| AC-009 | `ContainerCells` (Status accessible text from `labels.ts`, Name, Host links), column order constant | `ContainersPage.test.tsx` |
| AC-010 | `ImageCell`, using `update_status` and `update_reason` and the CMD-LIFECYCLE indicator titles | `ContainersPage.test.tsx` |
| AC-011 | `portLinks.ts` (through `safeUrl`), `PortsCell` + `Popover` | `portLinks.test.ts` (rule table), `ContainersPage.test.tsx` (`rel`/`target`) |
| AC-012 | `uptime.ts` with `formatDuration`/`RelativeTime`; memory percentage only when `mem_limit_configured > 0` | `uptime.test.ts` with an injected clock |
| AC-013 | `dockerChips.ts::matchesSearch` | `dockerChips.test.ts` |
| AC-014 | `dockerChips.ts::chipCounts`, `filterRows` | `dockerChips.test.ts` |
| AC-015 | chip counts respect the host filter | `dockerChips.test.ts` |
| AC-016 | `MultiSelectFilter` stack options from the selected hosts; P1 stack cell sets the filter | `ContainersPage.test.tsx` |
| AC-017 | `DataTable` sort, `sortRows` tie-breakers | `dockerChips.test.ts`, `ContainersPage.test.tsx` |
| AC-018 | `dockerUrlState.ts` | `dockerUrlState.test.ts` round trip; invalid values |
| AC-019 | `serialize` caps (count and length) | `dockerUrlState.test.ts` |
| AC-020 | view toggle, `localStorage riot-docker-view`, `TilesView` | `ContainersPage.test.tsx` |
| AC-021 | "Check for updates" sends `sendCommand(docker_check_updates)` per filtered host, then a toast | `ContainersPage.test.tsx` (mock call count) |
| AC-022 | `EmptyState` "No containers match these filters" with Clear | `ContainersPage.test.tsx` |
| AC-023 | "Host offline" cell indicator with `RelativeTime` from `/docker/hosts` | `ContainersPage.test.tsx` with an injected clock |
| AC-024 | `sortFreeze.ts`, memoised rows, roving focus | `sortFreeze.test.ts`; `ContainersPage.test.tsx` (selection and focus retained) |
| AC-025 | `DataTable` selection: shift range, mixed header, prune on filter (BR-012) | `BulkActions.test.tsx` |
| AC-026 | `dockerActions.ts::bulkApplicability` and active-command exclusion; server mirror | `dockerActions.test.ts`; `applicability_test.go` |
| AC-027 | `BulkConfirmDialog` grouping, skip reasons, dependents via `network_parent` | `BulkActions.test.tsx` |
| AC-028 | `BulkConfirmDialog` + `TypedConfirmInput`; server `confirm_count` | `BulkActions.test.tsx`; `TestDUX_AC028_BulkStopRequiresTypedCount` |
| AC-029 | `docker_bulk.go` per-host bulk update with conflicts; `BulkProgressPanel` | `TestDUX_AC029_BulkUpdateConflictPartial`; `BulkActions.test.tsx` |
| AC-030 | client disables at more than 100; server 400 `too_many_targets` | `BulkActions.test.tsx`; `TestDUX_AC030_BulkMax100` |
| AC-031 | offline notice from `/docker/hosts` status | `BulkActions.test.tsx` |
| AC-032 | `ResourceHeader` breadcrumb and badges | `ContainerPage.test.tsx` |
| AC-033 | `containerPrimaryAction`, `containerSecondaryActions` | `dockerActions.test.ts` (table-driven) |
| AC-034 | `Menu` groups, divider, danger item | `ContainerPage.test.tsx`; `Menu.test.tsx` |
| AC-035 | `ResourceHeader` with `useCommandTracker` (queued shows Cancel; running shows step) | `ContainerPage.test.tsx` (WS `command_update` injected) |
| AC-036 | Stop goes through `ConfirmDialog variant="danger"`; Start is direct | `ContainerPage.test.tsx` |
| AC-037 (rev 2) | `Tabs` with `DOCKER_SHIPPED` and the feature flag, with exactly one Logs tab; `?tab=` | `ContainerPage.test.tsx` |
| AC-038 | `OverviewTab` content parity, no Environment | `ContainerPage.test.tsx` |
| AC-039 | `StatsTab` charts with the remembered range | `ContainerPage.test.tsx` |
| AC-040 (rev 2) | The history half of `LogView`; collector state; `TerminalTab` message | `LogView.test.tsx`; `TestDUX_AC040_HistoryCollectorState`; `ContainerPage.test.tsx` |
| AC-041 | collector fields; name unchanged | `TestDUX_AC041_CollectorAdditiveFields` (fake Docker API), `docker_iorate_test.go` |
| AC-042 | `fleet_dashboard.go` allowlist | `TestDUX_AC042_FleetRowAllowlist` (JSON key-set assertion) |
| AC-043 | `CommandPalette` shortcut, `Modal` focus restore | `CommandPalette.test.tsx` |
| AC-044 | `data-palette-exempt` capture-phase check | `CommandPalette.test.tsx` (keydown from a node inside an exempt wrapper: palette closed, event not prevented) |
| AC-045 | `paletteRank.ts` | `paletteRank.test.ts` |
| AC-046 | `useRecentResources` and filtering; listbox keyboard | `CommandPalette.test.tsx` |
| AC-047 | offline marker | `CommandPalette.test.tsx` |
| AC-048 (rev 2) | Live toggle enablement; history always shown | `LogView.test.tsx` |
| AC-049 | The hub writes nothing to storage | `TestDUX_AC049_LiveSessionNoStorage` |
| AC-050 | Follow and Pause | `LogView.test.tsx` |
| AC-051 (rev 2) | One buffer for history and live | `LogView.test.tsx` |
| AC-052 (rev 2) | Backfill `skipped`; gap marker | `TestDUX_AC052_BackfillSkippedCount`; `useLogView.test.ts` |
| AC-053 | Line bound in the buffer | `logBuffer.test.ts` |
| AC-054 | Re-attach from the boundary | `useLogView.test.ts` |
| AC-055 | Exit and restart per stream | `TestDUX_AC055_ExitThenRestartMarkers`; `useLogView.test.ts` |
| AC-056 (rev 2) | Hidden-tab detach and resume | `useLogView.test.ts`; `TestDUX_AC056_LastViewerHiddenStopsFollow` |
| AC-057 | Drop before mask; truncation | `TestDUX_AC057_RateCapAndTruncate` |
| AC-058 (rev 2) | Per-host stream bound; joining an existing stream allowed | `TestDUX_AC058_NinthStreamRefusedJoinAllowed`; `LogView.test.tsx` |
| AC-059 (rev 2) | Masking at ingest, on history read, on the agent and on the relay | `mask_test.go`; `logMask.test.ts`; `TestDUX_AC059_HistoryAndLiveMasked` |
| AC-060 | Name check before any send; admin group; Origin required | `TestDUX_AC060_*`; `TestDUX_SEC012_LogsWSMissingOrigin403` |
| AC-061 | Writer fairness | `TestDUX_AC061_CommandAckP95UnderLogLoad` |
| AC-062 | `redact.InspectJSON`; `InspectTab` count | `inspect_test.go`; `InspectTab.test.tsx` |
| AC-063 | agent `docker_read` output | `TestDUX_AC063_InspectPayloadHasNoSecrets` (captures the WS frame) |
| AC-064 | `InspectTab` Refresh, Copy, search; `no-store`; no repository writes | `InspectTab.test.tsx`; `TestDUX_AC064_InspectNotStored` |
| AC-065 | `agentrpc` timeout; fail-closed redaction plus log | `TestDUX_AC065_ReadTimeout10s`, `TestDUX_AC065_RedactionFailureNoContent` (slog capture); `InspectTab.test.tsx` |
| AC-066 | actor columns and claims; container-scoped Activity query across recreates; observed events | `TestDUX_AC066_ActivityActorsPG` (PG); `Activity.test.tsx` (labels) |
| AC-067 | `steps` timeline + `steps` output row; `StepTimeline` | `TestDUX_AC067_StepsRecorded`; `Activity.test.tsx` |
| AC-068 | 64 KiB cap on agent and server | `TestDUX_AC068_StepOutputCapped`; `Activity.test.tsx` "Output truncated" |
| AC-069 | `command_update` applied to the entry | `Activity.test.tsx` |
| AC-070 | keyset pagination over commands and events | `TestDUX_AC070_ActivityPagination` (PG); `Activity.test.tsx` |
| AC-071 | fleet Activity filters, `target_exists` | `TestDUX_AC071_FleetActivityFilters`; `ActivityPage.test.tsx` |
| AC-072 | `StripANSI` on the server; text-node rendering | `TestDUX_AC072_ActivityTextSanitized`; `Activity.test.tsx` (no `img` element) |
| AC-073 | agent `ioRates`; `StatsTab` live | `docker_iorate_test.go`; `ContainerPage.test.tsx` |
| AC-074 | missing capability gives "—" with tooltip | `ContainerPage.test.tsx` |
| AC-075 | limit line from `mem_limit_configured`; zone label; no I/O chart without rates | `ContainerPage.test.tsx` |
| AC-076 | `/docker/stacks` state and chips | `TestDUX_AC076_StackStates`; `StacksPage.test.tsx` |
| AC-077 | `(device, project)` key | `TestDUX_AC077_SameProjectTwoHosts` |
| AC-078 | `ServicesTab` | `StackPage.test.tsx` |
| AC-079 | Pull without dialog; Down typed; down argv without `-v`/`--rmi` | `StackPage.test.tsx`; `TestDUX_AC079_DownArgvKeepsVolumes` |
| AC-080 | AD-007 conflict, attach | `TestDUX_AC080_StackActionConflict`; `StackPage.test.tsx` |
| AC-081 | `docker_stacks` forget and reappear | `TestDUX_AC081_ForgetAndReappearPG`; `StacksPage.test.tsx` |
| AC-082 | `DisallowUnknownFields`, no agent send | `TestDUX_AC082_PathFieldRejected` |
| AC-083 | `redact.ComposeFile`, `DotEnv`; `ComposeTab` | `compose_test.go`, `dotenv_test.go`; `StackPage.test.tsx` |
| AC-084 | `stackfs.CheckComposePath` and the no-race open (`stackfs_linux.go`) | `stackfs_test.go` (the four AC cases plus denied roots; symlink cases are Linux-only, `t.Skip` on Windows) |
| AC-085 (rev 2) | Stack `LogView` with merged history and per-service Live | `StackPage.test.tsx`; `TestDUX_AC085_StackHistoryMerged` |
| AC-086 | `stack_actions_v1` gating | `TestDUX_AC086_UnsupportedStackAction409`; `StackPage.test.tsx` |
| AC-087 | `stackPrimaryAction` | `dockerActions.test.ts` (table-driven) |
| AC-088 | stack links, tile heading link and toggle | `StackPage.test.tsx`, `ContainersPage.test.tsx` |
| AC-089 | `inventory_enabled`; `InventoryHostNotice`; doctor | `ImagesPage.test.tsx`; `TestDUX_AC089_DoctorReportsInventoryDisabled` |
| AC-090 | collector bounds; latest-only upsert | `docker_inventory_test.go`; `TestDUX_AC090_InventoryLatestOnlyPG` |
| AC-091 | inventory status derivation; staleness | `ImagesPage.test.tsx`, `VolumesPage.test.tsx`, `NetworksPage.test.tsx` |
| AC-092 | preview `requires_refresh`, volume defaults, built-in filter | `TestDUX_AC092_PreviewRules`; `PruneDialog.test.tsx` |
| AC-093 | typed rules (server and UI) | `TestDUX_AC093_PruneTypedConfirm`; `PruneDialog.test.tsx` |
| AC-094 | agent per-item re-check | `TestDUX_AC094_OnlyListedStillUnused` (fake Docker) |
| AC-095 | exclusive conflict both ways | `TestDUX_AC095_PruneExclusivePG` |
| AC-096 | Down argv; `ImageRemove` without volume calls; update `ContainerRemove` without `RemoveVolumes` | `TestDUX_AC096_NoVolumeSideEffects` |
| AC-097 | `prune_v1` gating | `ImagesPage.test.tsx`; `TestDUX_AC097_PruneUnsupported409` |
| AC-098 | `selfTargets` per host (image, project, label); confirmation | `TestDUX_AC098_SelfStackDownNeedsHost`; `StackPage.test.tsx` |
| AC-099 | static guards over the Docker UI scope; `labels.ts` maps | `static-guards.test.ts` |
| AC-100 | AD-033 (a) and (b) | `dockerChips.perf.test.ts`; manual record in the implementation report |
| AC-101 (rev 2) | `DataTable`, `Tabs`, `Menu` and Logs controls keyboard operation; axe | keyboard tests in `DataTable.test.tsx`, `Tabs.test.tsx`, `Menu.test.tsx`, `LogView.test.tsx`; `a11y.test.tsx` (D-24) or a manual record |
| AC-102 (rev 2) | capability gating on every P2–P4 surface; history still shown | `TestDUX_AC102_LegacyAgentDegrades` (server 409 codes); `ContainerPage.test.tsx`, `StackPage.test.tsx`, `ImagesPage.test.tsx` legacy fixtures |
| AC-103 (rev 2) | demo stubs, demo history and demo hub | `demo-parity.test.ts` (UI-FOUNDATION) and `liveLogs.test.ts` (no `fetch`/`WebSocket` constructed under the demo flag) |
| AC-104 | AD-024 rows | `TestDUX_AC104_AuditRows` |
| AC-105 (rev 2) | fixture privacy scan (static guards) | `static-guards.test.ts` (TS/JSON fixtures) and `TestDUX_AC105_NoPrivateAddressesInTestdata` (Go `testdata`, `docs/**/DOCKER-UX-001*`) |
| AC-106 | stack Activity scope: `docker:compose:<wd>` plus the member container keys plus member observed events | `TestDUX_AC106_StackActivityScopePG`; `StackPage.test.tsx` |
| **AC-107** | Whole-day drops by the PART-001 manager; no row DELETE | `TestDUX_AC107_WholeDayRotationPG`; `TestDUX_AC107_NoContainerLogRowDelete` |
| **AC-108** | `parseContainerLogRetention` | `TestDUX_AC108_RetentionSettingParse` |
| **AC-109** | Manager window taken from the setting | `TestDUX_AC109_RetentionChangePG` |
| **AC-110** | `capRing` | `TestDUX_AC110_AgentCapsNewestWithGap` |
| **AC-111** | `applyCeiling` | `TestDUX_AC111_ServerCeilingAnyAgent` |
| **AC-112** | Collection from stopped containers | `TestDUX_AC112_ExitedContainerLinesStored` |
| **AC-113** | The TTY reader | `TestDUX_AC113_TTYLineExact` |
| **AC-114** | PART-001 conversion and agent buffer | PART-001 AC-006 and AC-019; `TestDUX_AC114_ConversionKeepsLinesPG`; the agent buffer test |
| **AC-115** | Collector freshness | `TestDUX_AC115_StoredFreshnessWithinIntervalPlus10s` |
| **AC-116** | History paging, clamping and retention text | `TestDUX_AC116_HistoryPagingAndClamp`; `LogView.test.tsx` |
| **AC-117** | Gap entries; refresh de-duplication | `TestDUX_AC117_GapEntries`; `logBuffer.test.ts`; `LogView.test.tsx` |
| **AC-118** | Boundary backfill and pending merge | `useLogView.test.ts`; `logBuffer.test.ts` |
| **AC-119** | History refresh paused while Live is on, resumed after | `useLogView.test.ts` |
| **AC-120** | No `mode` parameter; no attach on open | `LogView.test.tsx`; `dockerUrlState.test.ts` |
| **AC-121** | One follow per container; linger, then unfollow | `TestDUX_AC121_SharedFollowFanOut`; `TestDUX_AC121_AgentOneReaderPerContainer` |
| **AC-122** (rev 3) | AD-035 §1a `applyWindow` re-stamp (`timestamp` = receive time, `agent_ts` kept); DEFAULT-partition event (WP-P0C) | `TestDUX_SEC021_FutureAndPastDatedLogRowsNotStoredOutOfWindow`; `TestDUX_SEC021_GapEntryTimestampWindow`; `container_log_default_pg_test.go` (next day's partition still created) |
| **AC-123** (rev 3) | AD-035 §1a: lines older than retention are re-stamped and kept, never dropped | `TestDUX_SEC021_FutureAndPastDatedLogRowsNotStoredOutOfWindow` (`container_log_window_test.go`) |
| **AC-124** (rev 3) | AD-035 §1a defence in depth: PART-001 `DefaultRowCount` Warn and the `log_partition_default_rows` event (WP-P0C) | `container_log_default_pg_test.go`; event tests for `internal/server/events/generator.go` |
| **AC-125** (rev 3) | AD-035 §1a skew accounting: per-device counters, `clock_skew` event via `CheckClockSkew`, NFR-022 Info log | `container_log_window_test.go` (`TestDUX_SEC021_*`); event tests for `internal/server/events/generator.go` |
| **AC-126** (rev 3) | AD-035 §5 rules 1 to 3: server-owned `stream`, entry and gap-entry validation | `TestDUX_SEC023_ForgedGapStreamNotStored`; `TestDUX_SEC023_GapEntriesValidatedAndBounded` |
| **AC-127** (rev 3) | AD-035 §5 rules 4 to 7: unknown-container budget, per-device rolling ceiling, `gap_server` rows | `TestDUX_SEC023_NameRotationHeldByDeviceCeiling` |
| **AC-128** (rev 3) | AD-035 §6: write-time `MaskLine` in WP-P0B, after the ceilings | `TestDUX_SEC028_StoredLinesMaskedFromP0`; `TestDUX_SEC023_MaskAfterCeiling` |
| **AC-129** (rev 3) | §2.4.1 runbook; `db.ClassifyMigrationState`; read-only `riot-server db-fingerprint` | `TestDUX_SEC022_FingerprintClassification`; `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB`; `TestDUX_SEC022_DirtyDatabaseRefused`; runbook review (sign-off and verified backup) |
| **AC-130** (rev 3) | AD-036 re-stamped lines: `agent_t` in history entries, `LogLines` clock-skew marker, ordering by `timestamp, id` | `LogView.test.tsx`; `container_log_repo.go` history-ordering test |
| **AC-131** (rev 3) | AD-016 live re-stamp (timestamp window); AD-036 future bound on `newestTs` | `hub_test.go`; `logBuffer.test.ts` |
| **AC-132** (rev 3) | AD-024 retention by class (180 and 90 days) and upgrade effect; AD-025 device deletion audited; `command` audit row for every operator command (SEC-029) | `TerminalRepo.Purge` retention test; `TestDUX_AC104_AuditRows`; `service_test.go` (SEC-029) |
| **AC-133** (rev 3) | AD-016 session expiry: socket closed at the JWT `exp`, attach refused, 4 h socket cap | `TestDUX_SEC027_SocketClosedAtTokenExpiry` |
| **AC-134** (ADD-derived, rev 3.1; amended in rev 3.2) | AD-035 §1b catch-up. Given 3 missing future partitions, 5 missing past days inside the retention window, and 30 expired day partitions (a simulated 30-day outage), when the server starts: every partition for today − effective retention..today + 3 exists before it serves; expired partitions are then dropped at most 24 per run, and none remain after 2 runs; ingest during the runs succeeds | `TestDUX_AC134_JanitorCatchUpBoundedPG` |
| **AC-135** (ADD-derived, rev 3.1; amended in rev 3.2) | AD-035 §1b ingest guard. Given today's partition missing and the janitor stopped, a batch creates it on demand. Given on-demand creation fails (forced) and yesterday's partition is the newest one in the window, the rows land re-stamped in **yesterday's** partition, with `agent_ts` set and the clock-skew marker, and they are dropped with that partition one day early. The DEFAULT row count stays 0 throughout | `TestDUX_AC135_IngestPartitionGuardPG` |
| **AC-136** (ADD-derived, rev 3.1) | AD-035 §1b stale event. With an injected clock and no successful run for 6 h, exactly one `partition_janitor_stale` event fires. The next success clears it, and no repeat fires while it stays stale | `TestDUX_AC136_JanitorStaleEvent` |
| **AC-137** (ADD-derived, rev 3.1) | §14.1 item 29. Every exported constant in `internal/models/docker_policy.go`, and every cap named in §13A, is referenced by at least one test | `TestDUX_AC137_EveryBoundHasATest` |
| **AC-138** (ADD-derived, rev 3.1) | AD-025 self-healing. Given a seeded backlog of 400,000 expired audit, command and output rows, each run deletes in batches of at most 5,000 within 30 s per purge, the leftovers drain on later runs, and none remain afterwards. A forced DB error in one purge does not stop the others and is retried. `retention_stale` fires after 24 h without success | `TestDUX_AC138_PurgeCatchUpPG` |
| **AC-139** (ADD-derived, rev 3.1; extended in rev 3.2) | D-47. A crash-looping container emits 1,000 `die` events in one hour: at most 60 are stored for it, plus one "N events suppressed" summary. Device-wide, at most 600 per hour are stored, plus summaries; agent events and probe-failure events draw from the same device bucket. **Rev 3.2 (B-19):** 10,000 events naming random containers keep the per-container counter map at no more than 512 entries per device, with the overflow counted against the device bucket. An event with a 512 KiB message is stored with each text field at no more than 1 KiB, and the row's text at no more than 3 KiB, cut at a rune boundary | `TestDUX_AC139_ObservedEventThrottle`; `TestDUX_BND_EventThrottleMapBounded`; `TestDUX_BND_EventTextCapped` |
| **AC-140** (ADD-derived, rev 3.1) | D-48. The 121st operator command in a burst for one device gets `429 rate_limited` and no row is created. System-origin commands are not limited by it. The 31st terminal open in a minute for one device is refused | `TestDUX_AC140_CommandCreationLimit`; `TestDUX_AC140_TerminalOpenLimit` |
| **AC-141** (ADD-derived, rev 3.1) | §14.1 items 30–31. 10,000 rejected or re-stamped lines across 25 devices within one minute produce at most one Warn per (device, message key) per minute plus one summary. At most 1,000 Warn records per hour are written server-wide. The agent writes at most one record per key per minute | `TestDUX_AC141_ServerLogBudget`; `TestDUX_AC141_AgentLogBudget` |
| **AC-142** (ADD-derived, rev 3.1) | §13A B-23. With the server down and the agent buffering 2 MiB snapshots, the buffer stays at no more than 64 MiB and no more than 100 rows, and the oldest snapshots are dropped | `TestDUX_AC142_AgentBufferByteCap` |
| **AC-143** (ADD-derived, rev 3.1) | §13A B-06, B-09, B-11. With 64 stalled viewers, each queue stays at no more than 1 MiB. The 17th concurrent agent RPC server-wide is refused. Under 10,000 rotating container names, the limiter map stays at no more than 512 entries per device | `TestDUX_BND_ViewerQueueBytes`; `TestDUX_BND_AgentRPCConcurrency`; `TestDUX_BND_ContainerLimiterLRU` |
| **AC-144** (ADD-derived, rev 3.2) | D-51 telemetry rate (SEC-035). With an injected clock, one device can store 120 snapshots in a burst. The 121st gets `429 rate_limited`, with a `Retry-After` equal to the seconds until the next token, and nothing is stored. After 15 s, one more is accepted. A second device is unaffected. The agent buffers the refused snapshot, sends nothing until `Retry-After`, and its flush deletes each item after its own success, so nothing is resent | `TestDUX_BND_TelemetryIngestRate`; `TestDUX_AC144_AgentHonoursRetryAfter` |
| **AC-145** (ADD-derived, rev 3.2) | D-51 heartbeats. Ten heartbeats from one device within 10 s: the first 6 are stored. The rest get `200 {"status":"ok","throttled":true}`, with no `heartbeats` insert and no `devices` UPDATE | `TestDUX_BND_HeartbeatIngestRate` |
| **AC-146** (ADD-derived, rev 3.2) | D-51 body caps and the WS limit. A 25 MiB + 1 B telemetry body, a 64 KiB + 1 B heartbeat, 1 MiB + 1 B event and probe bodies, and an 8 MiB + 1 B device-log body each get `413 payload_too_large`, and peak heap growth shows the decoder never buffered past the cap. A 4 MiB + 1 B agent WS frame closes the socket. 1,000 frames in one second are cut to the bucket, and 3 s of continuous excess closes the socket. Probe results for probes that are not enabled for the device are dropped | `TestDUX_BND_AgentBodyCaps`; `TestDUX_BND_AgentWSReadLimitAndFrameRate`; `TestDUX_BND_ProbeResultBounds` |
| **AC-147** (ADD-derived, rev 3.2; amended in rev 3.3) | D-50 quotas as buckets (D-54). Given a device whose `logs` bucket is empty, it still stores lines at the refill rate (Q ÷ 24 h, within 5 %). Exactly one `storage_quota_exceeded` event fires per transition, and it clears when the bucket is back above 50 %. After a simulated restart, the level is restored from `device_storage_usage` plus the elapsed refill, losing at most 60 s of accounting. There is no discontinuity at UTC midnight. `commands` with an empty bucket store a 1 KiB stub and summary-only `target_results` | `TestDUX_BND_DailyQuotasPG` |
| **AC-148** (ADD-derived, rev 3.2) | D-49 budget. Given container-log partitions totalling 18 GiB and a 16 GiB budget, the next run drops the oldest days until usage is at most 14.4 GiB, and never drops today's or yesterday's. History returns `retention_days` equal to the days kept, and `retention_limited_by` = `storage_budget`. Exactly one `log_budget_eviction` event fires, and it clears after 24 h without an early drop. With command results over 4 GiB, the oldest output rows are deleted first, until usage is at most 3.6 GiB | `TestDUX_BND_StorageBudgetEvictionPG`; `TestDUX_BND_CommandResultsBudgetPG` |
| **AC-149** (ADD-derived, rev 3.2; amended in rev 3.3) | Disk headroom, with an injected free-space source on a 100 GiB volume. Below 15 GiB free, one `db_disk_low` event fires. Below 5 GiB, the AD-041 §3 order runs step by step, re-measuring after each step, and stops as soon as free space is above 15 GiB. Audit rows are untouched. Minimal mode still stores priority log lines, sampled stdout (1 per container per minute), device-log entries of priority ≤ 3, and one trimmed snapshot per device per 15 min, while last-seen still updates. After 10 min above the low threshold, emergency mode ends. With no source configured, headroom is off and a startup Info says so | `TestDUX_BND_DiskHeadroomEmergencyOrder` |
| **AC-150** (ADD-derived, rev 3.2) | AD-025 adaptive purge (SEC-037 item 2). With forced `statement_timeout` on batches over 600 rows, the batch halves from 5,000 down to 312 and then grows back after successes. `command_output` rows of a batch are deleted before their commands. The backlog converges to zero over runs | `TestDUX_BND_PurgeBatchAdaptsOnTimeout` |
| **AC-151** (ADD-derived, rev 3.2) | AD-035 §1b drop locks (SEC-037 item 3). A long reader holds `container_logs`. With a DEFAULT partition present, the drop uses `DROP TABLE` with `lock_timeout` 1 s: a concurrent `COPY` waits no more than about 1 s, the run ends at that first timeout, and nothing retries until the next run. With no DEFAULT partition, the drop uses `DETACH … CONCURRENTLY` and does not block the `COPY`. A detach left pending is finalized on the next run | `TestDUX_BND_PartitionDropLockBoundedPG` |
| **AC-152** (ADD-derived, rev 3.2) | AD-035 §1b startup and singleflight (SEC-037 item 4). With retention 7 and every past partition missing, all days from today − 7 to today + 3 exist before the server serves. 50 concurrent ingests for a missing day make exactly one `CREATE` attempt within 60 s, server-wide | `TestDUX_BND_StartupEnsureBacklogPG`; `TestDUX_BND_OnDemandCreateSingleflight` |
| **AC-153** (ADD-derived, rev 3.2) | SEC-036. 100,000 `logs_data` frames with random `stream_id`s keep the retired-ID set at no more than 256 per device, and the number of `logs_unfollow` sent within the per-device bucket. 10,000 random container names keep the event-throttle map at no more than 512 per device. Cancelling 1,000 streams leaves zero event-bus subscribers, and the 11th concurrent subscription is refused | `TestDUX_BND_RetiredStreamIDsBounded`; `TestDUX_BND_EventThrottleMapBounded`; `TestDUX_BND_EventBusUnsubscribeOnCancel` |
| **AC-154** (ADD-derived, rev 3.2) | SEC-038. Both shipped compose files declare `logging` with `max-size` and `max-file` for every service. 1,000 successful agent pushes from one device within 10 min produce one summary line and no per-request lines | `TestDUX_SEC038_ComposeLoggingOptions`; `TestDUX_SEC038_AgentRequestLogSummarized` |
| **AC-155** (ADD-derived, rev 3.2) | B-12. The 51st preview evicts the oldest. With an injected clock, expired previews are swept within 60 s and on access | `TestDUX_BND_PreviewStoreCapAndSweep` |
| **AC-156** (ADD-derived, rev 3.2) | B-14. 10,000 command and terminal requests naming random device IDs create no limiter bucket. Buckets idle for 10 min are evicted | `TestDUX_BND_LimitersKeyOnlyKnownDevices`; `TestDUX_BND_LimiterIdleEviction` |
| **AC-157** (ADD-derived, rev 3.2) | B-16. An attach storm across 500 containers on one device in one minute writes at most 32 `logs` rows plus one overflow summary row with the suppressed count | `TestDUX_BND_AuditRowsUnderAttachStorm` |
| **AC-158** (ADD-derived, rev 3.2) | B-17. Policies that try to create 100 system commands for one device within an hour create at most 30. A policy with a 1-minute cooldown re-fires no sooner than 15 min after a failure | `TestDUX_BND_SystemCommandCap` |
| **AC-159** (ADD-derived, rev 3.2) | B-25. The 501st project evicts the oldest unseen entry. Entries unseen for 90 days are pruned. The file stays within its size bound | `TestDUX_BND_StackRegistryCapAndPrune` |
| **AC-160** (ADD-derived, rev 3.2) | B-27. A backfill of 5,000 lines of 16 KiB, plus a 20,000-line skip scan, keeps peak heap growth under 4 MiB | `TestDUX_BND_BackfillStreamsWithoutBuffering` |
| **AC-161** (ADD-derived, rev 3.2) | D-52. With `docker.prune_superseded_images` false (the default), no image is removed after an update. With it true, only the superseded image ID is removed, and only if unused and untagged, through `ImageRemove(Force:false, PruneChildren:false)`; no `*Prune` API is called. An image that is in use or still tagged is skipped. A failed removal does not fail the update | `TestDUX_AC161_SupersededImageCleanupOptIn` |
| **AC-162** (ADD-derived, rev 3.2) | B-48. Loading a 41st Activity page drops the oldest page from memory, and scrolling back refetches it | `Activity.test.tsx` |
| **AC-163** (ADD-derived, rev 3.3) | SEC-030. Against a schema with no `schema_migrations`, `db-fingerprint` creates no table and takes no advisory lock. A write injected into its session fails as read-only. With a bad password, and with an unreachable host, its output contains no host, port, user, database name or password, and it exits non-zero. `riot-server migrate` applies pending migrations and exits without binding a port. The renumbered PART-001 no-op test also finds the `partition_conversion` row unchanged. The step 4 classifier returns "None" for F-LC + F-AV at 23, and for F-LC + F-PT + F-AV at `main` numbering | `TestDUX_SEC030_FingerprintCreatesNothing`; `TestDUX_SEC030_FingerprintReadOnlySession`; `TestDUX_SEC030_FingerprintOutputHasNoDSN`; `TestDUX_SEC030_MigrateSubcommandDoesNotServe`; `TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB`; `TestDUX_SEC022_FingerprintClassification` |
| **AC-164** (ADD-derived, rev 3.3) | D-54 continuous refill. A device pushing at its ceilings for 48 h (injected clock) stores at most 1.25 × Q in any 24 h window, and at most Q per 24 h on average. No ingest after the first hour stores zero lines, and there is no step change at UTC midnight | `TestDUX_BND_QuotaContinuousRefill` |
| **AC-165** (ADD-derived, rev 3.3) | D-54 sampled mode. With the `logs` bucket at 20 % of capacity: every stderr line, and every stdout line matching `ErrorLinePatterns`, is stored before any other stdout line; other stdout is stored at no more than 5 lines per container per minute; the remainder appears as `gap_quota` rows, reported with reason `quota_sampled`; and `LogView` shows the sampled-history notice. One container flooding stdout cannot reduce a quiet container's stored lines below an equal share of the ingest's tokens | `TestDUX_BND_QuotaStderrReserve`; `TestDUX_BND_QuotaPerContainerFairShare`; `LogView.test.tsx` |
| **AC-166** (ADD-derived, rev 3.3) | D-54 telemetry downsampling. With the `telemetry` bucket at 20 %, or empty, at most one snapshot per 15 min is stored, even with the bucket empty. A 10 MiB snapshot is trimmed to at most 2 MiB, and keeps `system`, `cpu`, `memory`, `disks`, `network` and `docker`. The latest stored snapshot is never more than 15 min old. Last-seen and broadcast update on every accepted push | `TestDUX_BND_TelemetryQuotaDownsamples` |
| **AC-167** (ADD-derived, rev 3.3) | D-55 thresholds. An 8 TB volume at 96 % used (about 320 GB free) is normal. A 20 GiB volume is low below 4 GiB, and in emergency below 1 GiB. A 1 TB volume is low below 50 GiB, and in emergency below 10 GiB. `RIOT_DB_FREE_LOW_GB` and `RIOT_DB_FREE_EMERGENCY_GB` override the computed values | `TestDUX_BND_HeadroomLargeDiskNoFalseEmergency`; `TestDUX_BND_HeadroomThresholds` |
| **AC-168** (ADD-derived, rev 3.3) | D-55 minimal window. Held in emergency for 1 h, with 5 devices pushing at their ceilings, every device still has stored priority log lines from that hour, sampled stdout, and a stored snapshot no older than 15 min. Total inflow stays within the minimal-mode rates. At critical (free below 512 MiB), yesterday's partitions are dropped and today's remain | `TestDUX_BND_EmergencyKeepsMinimalWindow` |
| **AC-169** (ADD-derived, rev 3.3) | D-56 (SEC-042). 10 devices, retention 30 days, a 16 GiB budget: one device stores at its quota, the others at 20 MiB per day. When usage passes the budget, only the heavy device's allowance is lowered to the fair share (at least 32 MiB per day). One `log_fairness_limited` event fires for it, `storage_quota.fairness` shows it, and no partition is dropped early while usage stays at or below 110 % of the budget. The allowance returns to the quota after 24 h at or below 80 % | `TestDUX_BND_FleetBudgetFairness` |
| **AC-170** (ADD-derived, rev 3.3) | SEC-040. In both compose files, no service other than `riot-db` mounts `riot-data`, and `riot-server` mounts `riot-fsprobe`. A probe path whose filesystem reports fewer used bytes than `pg_database_size` is reported as `mismatch`, and the guard falls back to `RIOT_DB_MAX_GB`, or turns headroom off | `TestDUX_SEC040_OnlyDatabaseMountsDataVolume`; `TestDUX_SEC040_ProbePathMismatchFallsBack` |
| **AC-171** (ADD-derived, rev 3.3) | SEC-041. A pool session left idle in a transaction, holding `AccessShare` on `container_logs`, is ended by the server's 60 s timeout, and the next drop succeeds. While it held the lock, the budgeted Warn named its PID, `application_name`, state and transaction age, without query text. In emergency, an active blocker with the server's role and `application_name` `riot-server` is cancelled. A blocker of another role, or of the same role with another `application_name`, is reported and never cancelled | `TestDUX_BND_IdleTxBlockerDoesNotStallEviction`; `TestDUX_SEC041_EmergencyCancelsOnlyOwnBlockers` |

The §19 security tests (`TestDUX_SEC0nn_*`) run alongside these, chiefly
for AC-062..AC-084, AC-086..AC-100 and AC-104.

**AC-106 [P3] [FR-122] (added by the ADD; the FRD has no AC for FR-122)**

Given: stack `media` on `host-a` with members `web` and `db`, and these
entries:

- a stack Restart of `media`;
- a container Restart of `web`;
- a bulk Update that included `db`;
- a Docker `die` event for `web` that rIOt did not trigger;
- a container Restart of `cache` in stack `infra`.

When: the `media` stack's Activity tab opens.
Then: exactly the first four entries are listed, newest first, each with the
FR-092 content.
And: the `cache` entry is not listed.
And: expanding the stack Restart shows its step timeline (FR-094).

---

## 11. Error Handling

| Failure | Handling | HTTP / WS | Log (structured, NFR-022) |
|---|---|---|---|
| Invalid identifier | no DB or agent call | 400 `invalid_identifier` | none (length only, at Debug) |
| Container or stack not in the latest data | no agent call | 404, or WS `error not_found` | Info `docker read rejected` (device, kind, reason) |
| Capability missing | no agent call | 409 `agent_unsupported` with capability | none |
| Agent not connected | none | 502 `agent_offline`; WS pre-upgrade 502 | none |
| Agent RPC timeout | pending entry removed | 504 `agent_timeout` | Warn `docker read timeout` (device, kind) |
| Agent refuses a read | reason passed through | 422 `read_refused` | Info `docker read rejected` |
| Redaction failure (agent) | no content | 422 `read_refused redaction_failed` | agent Warn; server Warn `redaction failed` (device, kind) |
| Read rate limit | none | 429 `rate_limited` "Try again shortly" | Warn through the CMD-LIFECYCLE `LogThrottle` |
| Live viewer attach, detach or limit | none | none | Info `live log session opened` / `closed` (stream, device, containers count, reason, lines, dropped) |
| Browser too slow | that viewer detached | WS `error client_too_slow` | Info |
| Agent disconnect while streaming | viewers told to reconnect | WS `session reconnecting` | Info |
| Conflict on dispatch | no row | 409 `conflict` | Info (CMD-LIFECYCLE) |
| Typed confirmation mismatch | no row | 400 `confirmation_required` | Info `docker confirmation rejected` (action, kind) |
| Bulk over 100 | none | 400 `too_many_targets` | none |
| Preview expired or stale | no command | 409 `preview_expired` / `inventory_stale` | none |
| Prune run | command | 201 | Info `docker prune dispatched` (device, kind, count); the agent result is logged by CMD-LIFECYCLE |
| Inventory payload over bounds (old or hostile agent) | truncated server-side to bounds; `truncated=true` | 200 | Warn `inventory truncated` (device) |
| Inventory upsert DB error | snapshot still stored | 200 to the agent | Error |
| Stack observe DB error | ignored for this cycle | none | Warn (throttled) |
| Step output over bounds | truncated | none | none |
| Retention purge error | Retried on the next run; the other purges continue; resumes where it stopped (AD-025 self-healing) | none | Error through the log budget; `retention_stale` event after 24 h without success |
| Partition janitor run fails | Retried with backoff (1 to 60 min); ingest unaffected | none | Error through the log budget; `partition_janitor_stale` event after 6 h without success |
| Day partition missing at ingest | Created on demand; if that fails, rows are re-stamped into the newest in-window partition; never DEFAULT | 200 | Warn through the log budget |
| No in-window partition exists | Rows dropped and counted in `log_ingest.rejected_24h` | 200 | Error through the log budget; stale-janitor event |
| Operator command or terminal-open rate exceeded (D-48) | Refused; no row | 429 `rate_limited` | Info, throttled |
| Observed-event throttle reached (D-47) | Excess events not stored; one summary per hour | none | none |
| Log budget reached (server or agent) | Records suppressed; one summary per window | none | one summary record |
| Agent telemetry buffer full (count or bytes) | Oldest snapshot dropped | none | agent Warn through the agent log budget |
| Stack observe write budget reached | Observation skipped until the next hour | none | Info, throttled |
| Agent ingest over the per-device rate (D-51) | Telemetry and events: not stored. Heartbeat: not stored | Telemetry and events: 429 with `Retry-After`. Heartbeat: 200 `throttled` | Summarized per device every 10 min (SEC-038) |
| Agent request body over its cap | Not decoded | 413 `payload_too_large` | Warn through the log budget |
| Agent WS frame over 4 MiB, or frame rate exceeded | Socket closed, or frames dropped and counted | WS close 1009 or 1008 | Warn through the log budget |
| Quota bucket low (D-50, D-54) | Degraded, never stopped. Log lines are sampled with priority lines first, and the rest become `gap_quota` counts. Telemetry is downsampled to one snapshot per 15 min, trimmed to 2 MiB. Device logs are sampled. Command output is stubbed when the bucket is empty | 200 | One `storage_quota_exceeded` event per device per class per transition; Info, budgeted |
| Fleet storage budget exceeded (D-49, D-56) | The heaviest devices' `logs` allowances are lowered first. Above 110 %, or under disk pressure, the oldest container-log days are dropped early and the oldest command results deleted | none | `log_fairness_limited` per device, and `log_budget_eviction` per transition; Info per run |
| DB free space low, emergency or critical (D-55) | A warning event; or the AD-041 §3 emergency order, ending in minimal mode (storage never drops to zero); at critical, yesterday's partitions are dropped | none | `db_disk_low` or `db_disk_emergency` event per transition; Error in emergency |
| Headroom probe path not on the database's filesystem (SEC-040) | Falls back to `RIOT_DB_MAX_GB`, or headroom off | none | Warn at startup; source reported as `mismatch` |
| Purge batch hits `statement_timeout` | Batch halved and retried; output deleted before commands | none | Warn through the log budget |
| Partition drop hits `lock_timeout` | The run ends; the next hourly run continues. The blockers are listed (PID, application, state, transaction age). In emergency, the server's own active blockers are cancelled (SEC-041) | none | Warn with the blocker list, budgeted; stale event after 24 consecutive runs |
| System-command cap reached (B-17) | Not created | none | Warn, budgeted; `system_commands_throttled` event per device per hour |
| Event-bus subscriber cap reached (agent) | That stream runs without `restarted` markers | none | Agent Warn, budgeted |
| Cross-origin or non-JSON state change | Rejected before any decode | 403 `origin_refused` | Info `request origin refused` (method, route pattern) |
| Side-effecting GET without the header | No agent call, no audit, no rate-limit use | 403 `origin_refused` | as above |
| Invalid retention setting | Falls back to 7 | none | Warn at startup |
| Server ceiling applied | Excess dropped; `gap_server` row written | 200 | Info, throttled (device, container count, lines dropped) |
| Agent gap | Stored as a `gap_agent` row | 200 | none |
| Retention run | Day partitions dropped | none | Info (days removed, oldest day kept) |
| History range starts before retention | Clamped | 200 | none |
| Per-host stream bound reached | Refused for that container | WS `too_many_streams` | Info |
| Viewer bounds reached | Refused | WS `too_many_viewers` | Info |
| Agent frame over bounds | Excess dropped and counted | none | Warn, throttled (no content) |
| Stack paths not agreed | Refused | 409 `stack_paths_unagreed` | Info |
| Compose file writable by a container | Step refused | `failed`/`precheck`/`container_writable` | Agent Warn (names only) |
| Path component swapped, or file not regular | Refused | 422 `read_refused` | Info |
| Preview reused | Refused | 409 `preview_expired` | none |
| Image tag moved, or volume recreated | Item skipped | `Skipped: changed` | none |
| Device deleted | Audit row written first | as before | Info |
| Out-of-window container-log or gap timestamp | Re-stamped with the receive time; agent time kept in `agent_ts` | 200 | Info, throttled (device, count, skew seconds); `clock_skew` event after 3 consecutive ingests |
| Entry with a forged `stream`, an invalid name or an invalid ID | Dropped and counted | 200 | Info, throttled (device, count) |
| Gap entry invalid or over the per-push bound | Dropped | 200 | Info, throttled |
| Unknown-container budget or per-device ceiling exceeded | Excess dropped; a `gap_server` row for known containers | 200 | Info, throttled |
| Rows in the `container_logs` DEFAULT partition | Kept (PART-001 never drops it) | none | Warn, plus one `log_partition_default_rows` event per transition |
| Frame for an unknown or retired stream | Dropped; `logs_unfollow` sent within a per-device bucket (4 per second, burst 8); beyond that, dropped without a reply | none | Warn, throttled |
| Agent follow not re-asserted for 5 min | The agent cancels it | none | Agent Info (stream ID only) |
| Backfill requested faster than 1 per 10 s | Deferred and coalesced | none | none |
| Session token expired on an open log socket | Socket closed | WS `error session_expired` | Info |
| Reconciliation runbook finds a dirty or unexpected state | Stop; nothing forced | n/a (operator procedure) | Recorded in the implementation report |

UI mapping:

- **Errors on a load** render `ErrorPanel` with Retry (FR-005).
- **Errors on an action** render an error toast that uses the server
  `error` text.
- **409 conflicts** attach to the existing command (FR-113).
- **`agent_unsupported`** renders "Requires agent update".

---

## 12. Security Considerations

This section is the architect's threat model, covering live-log
authorization, redaction, file reads, prune and bulk destructive actions,
and agent privilege (FRD §14). Every security-review finding is mapped to
its AD in §19.

1. **Authorization, forgery and framing (NFR-009, SEC-001, SEC-012).**
   - Every new HTTP and WS route sits in the admin-auth group, and `router_test.go` asserts the auth matrix.
   - AD-034: same-origin JSON for state changes, a custom header for side-effecting GETs, and frame-ancestors none. Typed confirmations prevent mistakes only.
   - WS upgrades use `CheckWSOrigin`. `CheckWSOrigin` returns true when `Origin` is absent, a pre-existing weakness; `/ws/docker/logs` therefore requires `Origin` (SEC-012).
   - Every read and every attach checks that the resource exists on the named host in the latest data before contacting an agent.
2. **Device scoping on the agent socket.** `logs_*` and `docker_read_result` from a socket are applied only to streams or requests of that socket's device (the SEC-014 pattern of CMD-LIFECYCLE). A compromised agent cannot inject lines into another host's stream or answer another host's read.
3. **Stream and viewer binding.** A viewer is bound to the browser socket that attached it, and is detached when that socket closes. Stream IDs are 128-bit random, are never accepted from the browser, and are server-generated.
4. **Secrets at the source (NFR-010, SEC-003, SEC-004, SEC-016).**
   - Inspect, compose, `.env` and labels are redacted on the agent. Logs and step output are masked on the agent.
   - The redaction rules are wider, and a masking net covers documents.
   - Compose `environment:` follows rule 1, and dotenv parsing fails closed.
   - Every output path is redacted, including `/history` and `/summary`.
   - Command text and stored lines are masked at write time. The relay always masks.
   - Nothing redacted is reversible. There is no reveal path (BR-004).
   - Fail closed on parse errors. `no-store` on read responses.
   - The agent never logs content.
5. **Untrusted labels and paths (A-11, R-06, NFR-013, SEC-002, SEC-005).**
   - Paths are used only when every member agrees, from the agreed record, re-verified against the agent's own registry. Changes need an acknowledgement.
   - Compose never runs from paths that containers can write, unless the path is under a host-local trusted root.
   - Reads are bounded by an outer limit and a deny list that includes `/etc` and covers agent config, state, keys and certificates.
   - Each path is symlink-resolved, confined to its working directory, and restricted by extension. Files are opened component by component with `O_NOFOLLOW`, which defeats swapped components and FIFOs, and have size caps.
   - Compose argv goes through `composeArgs` or `composeStackArgs`, with validated service and project names and `--`.
   - The server rejects client-supplied paths (FR-118).
6. **Destructive actions (NFR-012, SEC-007).**
   - Typed confirmations are enforced on the server.
   - Previews are 128-bit, single use, server held, and bound to device, kind, items, actor and expiry.
   - Removal is explicit per item with a re-check, tag-to-ID verification and a volume `CreatedAt` match. `*Prune` APIs are never called.
   - Volumes are never removed as a side effect, and argv tests prove it.
   - The host-exclusive lock is applied both in SQL and on the agent.
   - Self-protection warnings are server enforced, with self targets computed per host.
7. **Rendering and links (FR-099, CMD-LIFECYCLE C-11, SEC-011, SEC-017).**
   - Every agent-originated string renders as text: log lines, inspect JSON, compose text, step output, actor names. Highlighting builds text nodes and `<mark>` only. `dangerouslySetInnerHTML` stays absent (QA grep).
   - Links use only `http` and `https`. IP literals are parsed, ports checked, and links use `rel="noopener noreferrer"` (FRD §8.4). Path segments are encoded.
8. **Resource exhaustion (SEC-006, SEC-010).**
   - Agent caps: lines, bytes, streams, backfills, containers. The agent reader is bounded, and drops lines before masking them.
   - Server caps: streams per host, viewers per stream and server-wide, a non-blocking fan-out, per-frame re-checks, per-host read limits, pending reads at most 4 per device, at most 50 previews.
   - The browser buffer has a line and a byte bound. Audit rows are coalesced.
   - Stacks and inventory have server-side bounds and cadence. Stored logs have a rolling ceiling.
   - WS read limits: 4 KiB browser → server; 4 MiB agent → server.
     - **Correction (rev 3.2):** the merged agent socket sets no read limit today.
     - AD-040 adds `SetReadLimit(4 MiB)` and a per-device frame bucket in P0.
9. **Privilege (NFR-014, AGENT-PRIV, SEC-020).** Every new agent capability uses the docker socket (group membership) or plain file reads as the agent user: stored and live logs, inspect, compose and `.env` reads, stack actions through the `docker compose` CLI, inventory, item removal. **No new sudoers rule. No AGENT-PRIV manifest capability maps to any of them.** The registry file lives in the agent-writable `/var/lib/riot`. `compose_trusted_roots` is local configuration and grants no privilege.
10. **Identity and audit (NFR-011, SEC-008, SEC-009).**
    - AD-024 records actor, host, target, time and remote address. Remote addresses come from `middleware.RealIP` (trusted proxies only).
    - The auth method and a stable subject are recorded. Legacy sessions are recorded as unknown.
    - Retention is per kind. Open sessions are never purged. Audit rows survive device deletion, and the deletion is itself audited.
    - The display name is IdP-controlled text. It is sanitized and length-capped, never used for authorization, and never logged with other identity data.
11. **Agent claims (SEC-013, SEC-014, SEC-015).** Self targets are computed per host. Capability tokens gate UX only. `command_id` is validated.
12. **Residual risks** (see also §18).
    - Obfuscated secrets under non-sensitive keys can still appear.
    - Pre-existing routes stay exposed to same-site requests until D-30 (b) is adopted.
    - Inferred attribution can be wrong; it is labelled as inferred.
    - Duplicates at the boundary when masking versions differ (AD-039).
    - Lines from hosts with skewed clocks appear at their arrival time, marked with both times (D-42). Their original times are kept in `agent_ts`. Nothing is dropped.
    - From P1, in-container stack managers need `docker.compose_trusted_roots` (D-44).
13. **Stored-log integrity (SEC-021, SEC-023, SEC-028).**
    - Timestamps are bounded at ingest by re-stamping, so no agent can push rows into PART-001's never-dropped DEFAULT partition.
    - `stream` values are server-owned. Gap entries are validated and bounded.
    - A per-device ceiling holds name rotation in check.
    - From P0, stored lines are masked at write time.
14. **Migration safety (SEC-022).** Reconciliation follows §2.4.1:
    - a verified backup first;
    - schema fingerprints;
    - stop on dirty;
    - never drop data;
    - per-database sign-off (D-43).
15. **Storage exhaustion (rev 3.2; SEC-035, SEC-036, SEC-037, SEC-038).**
    - Every agent ingest endpoint has a body cap before decoding, and a per-device rate (AD-040).
    - Per-device daily quotas, a fleet storage budget and a disk-headroom guard keep any number of misbehaving devices from filling the database volume (AD-041).
    - Server state keyed by identifiers that agents choose is LRU-bounded (B-43).
    - rIOt's own container logs rotate (B-46).
16. **Degrade, never stop; least privilege for the probe (rev 3.3; SEC-039..SEC-042).**
    - Quotas and the headroom guard degrade by sampling and downsampling, priority first. Stored logs never stop entirely for an honest host (AD-041 §1, §3).
    - The database volume is never mounted into the server container (SEC-040). The server image still runs as root; a non-root user is a recommended follow-up (R-27).
    - The server's own sessions cannot hold locks for long. Blockers are reported, and only the server's own blockers are ever cancelled (SEC-041).
    - The fleet budget lowers the heaviest devices' allowances before it shortens anyone's history (SEC-042).

---

## 13. Performance Considerations

### 13.1 Runtime

- **Containers table (NFR-004).**
  - The data is fetched once, then merged from WS at most once per second.
  - Windowing renders about 40 rows.
  - Filter, count and sort are pure O(n log n) over 2,000 rows (the vitest guard is under 50 ms).
  - Rows are memoised by version.
- **Palette (NFR-005).** A precomputed lower-cased index, and a linear scan over 5,000 entries (the guard is under 50 ms).
- **Live logs (NFR-006, NFR-007).**
  - A 250 ms agent batch, plus server relay, plus browser append gives about 0.3–0.5 s end to end on a LAN.
  - Agent masking costs a handful of compiled regexes per line. At the 1,000 lines/s cap that is under 5 % of one core.
  - The byte caps (512 KiB/s per stream, 2 MiB/s per agent) keep the agent socket unsaturated, so command reports wait for at most one 64 KiB batch write.
  - Agent load does not depend on the number of viewers. Backfills are bounded at 5,000 lines, at most 2 per host.
- **Server fan-out.** O(1) per frame, never parsing beyond the line caps, plus a queue copy per viewer, for at most 64 viewers.
- **Masking.** At ingest, at most 1,000 lines per container per ingest. On history read, at most 5,000 lines per page.
- **History queries.** The name index, `LIMIT 5001`, and partition pruning.
- **Origin middleware.** Constant-time checks.
- **On-demand reads.** At most 30 per minute per host, at most 1 MiB each.
- **Inventory.** It runs every 15 minutes or more, within 25 s, and never overlaps itself. Unchanged payloads send about 100 bytes.
- **Collector (P1).** Zero additional Docker API calls. Rate math is O(containers).
- **Activity queries.** Device-scoped queries use `idx_commands_device` and `idx_events_device_container`. Fleet queries use `idx_commands_created` and `idx_events_device_time`. Each is `LIMIT 26`.

### 13.2 Indexing

New indexes:

- `idx_commands_created`;
- `idx_command_output_created`;
- `idx_events_device_container` (partial);
- `idx_terminal_sessions_started`;
- `idx_container_logs_name` (WP-P0C).

`docker_stacks` and `docker_inventory` are served by their primary keys.

### 13.3 Database growth (NFR-003, NFR-025)

Assumptions: 30 hosts, 50 containers per host, a 60 s telemetry interval,
20 Docker commands per host per day, 5 stacks per host.

| Element | Growth per host per day | Bound | Churn class |
|---|---|---|---|
| FR-062 fields in `telemetry_snapshots` JSON | about 150 B × 50 × 1,440 ≈ 10.8 MB before TOAST compression (about 5–8 % of a typical snapshot) | existing `RIOT_RETENTION_DAYS` (30 d). Drop-partitioned after PART-001 | existing high-churn table, no new rows |
| Redacted labels | negative or zero (values shorten to `<redacted>`) | as above | n/a |
| `commands` rows (+ actor columns, `target_results`, `steps`) | about 20 rows × (≈1 KB + steps ≈1 KB) ≈ 40 KB | **new 90-day retention** (AD-025) | low (human or policy driven) |
| `command_output` `steps` row | about 20 × 4 KB typical (64 KiB maximum) ≈ 80 KB (1.3 MB worst) | **30 days** (D-14) | low (insert once) |
| `events.command_id` and `command_attr` | about +37 B per Docker event | existing 90 days | existing |
| `container_metrics` I/O rate columns (WP-2D) | 4 × 8 B × 50 × 1,440 ≈ 2.3 MB | existing 7 d, **only after PART-001** (drop-partitioned) | existing table |
| Live log lines, inspect, compose, `.env` | **0** (never stored, AC-049, AC-064) | n/a | n/a |
| **Stored logs under the default caps** (D-33 a) | Worst case 50 × 16 KiB × 1,440 ≈ **1.1 GiB of line data**, at most 3.6 M rows. With row and index overhead, about **1.8 GiB** in total. Typical loads are far lower. | Retention × daily volume, plus at most 4 partitions created ahead. Whole days are dropped, so there are no dead rows. | High churn, retained by dropping partitions |
| **Stored logs under the server ceilings** (legacy, misconfigured or hostile agents) | Worst case 2 MiB × 1,440 ≈ **2.8 GiB** of line data per host per day, plus at most 5,000 × 1,440 = 7.2 M rows of overhead (≈ 1.3 GiB), whatever the push rate or the container names (per-device ceiling, SEC-023; tightened in rev 3.1, D-46). Re-stamped rows count against the same ceilings. See §13A | as above | as above |
| Gap rows | At most 200 per device per 60 s (rev 3.1): at most 288,000 rows, ≈ 55 MiB | same rotation | as above |
| Name index | About 60 B per row | same rotation | as above |
| `docker_stacks` | At most 500 rows per device; typically 5. At most 500 row writes per device per hour (rev 3.1) | Updated only on change, within the write budget; eviction rule | Low |
| Audit | About 30 rows of 300 B | 180 days (long) or 90 days (short), D-21; about 1.6 MB per host per 180 days | Low |
| `commands` actor columns | About 120 B per command | 90 days | Low |
| Inventory | At most 4 MiB per write, about 103 writes per day; at most 0.4 GiB of rewritten TOAST in the worst case, kilobytes in practice | Latest only | Low |

**Retention scaling.** Stored-log volume grows linearly with retention.

- **Without quotas,** 90 days at the default-cap worst case would be about 160 GiB per host.
- **Daily quota.** From rev 3.2, the D-50 quota limits stored logs to about 0.55 GiB per host per day, or about 50 GiB at 90 days.
- **Fleet budget.** The D-49 budget (16 GiB by default) limits the total, whatever the setting. When the budget binds, the oldest days go first, and the history API reports the shortened retention.

The configuration docs say so, and D-34 caps the setting at 90 days.

Fleet-wide (30 hosts), the new DELETE-retained rows total about 1,500
commands, 1,500 output rows and 900 audit rows per day: three orders of
magnitude below `container_logs` churn, which is now retained by dropping
partitions. **No new high-churn DELETE-retained table is introduced.** The
two new tables are justified as follows:

- `docker_stacks`: BR-015 needs memory of stacks that have no containers, which telemetry cannot supply.
- `docker_inventory`: the snapshot alternative grows a high-churn table (AD-028).

---

## 13A. Bounds and self-healing (rev 3.1)

**Standing requirement (product owner).** Everything this story writes heals
itself. Whatever exception happens, the server must never fill up with
unbounded data, logs, images or anything else.

This table is **normative**. It lists every growth path that this story
creates or changes.

From rev 3.2 it also covers the following, because the standing requirement
covers them:

- the pre-existing agent ingest paths (B-38..B-42);
- the server host's own logs (B-46);
- the images left on managed hosts (B-47).

- Every new collection, map, queue, buffer, table or file needs a row here before code review (§14.1 item 29).
- Every cap needs a test (AC-137).
- Constants are in §14.2.

**How to read the worst case.**

- "Worst case per device per day" assumes a hostile, buggy or runaway agent or client pushing at every cap.
- Where a figure depends on the container count C, C = 200.
- PostgreSQL row overhead is taken as about 200 B per row: the tuple header, fixed columns and three index entries.

**Columns in the table:**

- **Lives:** server DB, server memory, agent disk, agent memory, or browser.
- **On cap:** what happens when the hard cap is reached.
- **Recovery:** recovery after a failure (crash, downtime, failed run, DB error, disk full, agent offline).
- **Worst/device/day:** worst-case growth per device per day.

| # | Growth path | Lives | Hard cap (constant = value) | Enforced by | On cap | Recovery | Worst/device/day | Test |
|---|---|---|---|---|---|---|---|---|
| B-01 | Stored container-log rows | server DB (`container_logs` day partitions) | `ContainerLogRetentionDays` = 7 (1..90); per container `StoredLogServerCeilingLines` = 1,000 and `StoredLogServerCeilingBytes` = 256 KiB per ingest and per 60 s; per device `StoredLogDeviceCeilingLinesMax` = 5,000 and `StoredLogDeviceCeilingBytesMax` = 2 MiB per 60 s; per device a `logs` bucket of `DeviceDailyLogBytes` = 512 MiB per 24 h, capacity 25 %, refilled continuously, counting line bytes plus 200 B per row (D-50, D-54); in sampled mode, priority lines first, other stdout 5 per container per minute, and a max-min fair share per container (rev 3.3); fleet-wide `ContainerLogBudgetDefault` = 16 GiB soft and 110 % hard, with D-56 fairness (rev 3.3); 20,000 rows per push (outer) | `container_log_ingest.go::applyCeiling` (AD-035 §5); `quota.Daily` (AD-041 §1); janitor, fairness and budget eviction (AD-035 §1b, AD-041 §2) | Newest kept within the ceilings; the excess is counted in a `gap_server` row. With the bucket low, stdout is sampled and the rest counted in `gap_quota` rows; storage continues at the refill rate (about 364 KiB per minute), priority first. Over the fleet budget, the heaviest devices' allowances shrink first; above 110 %, the oldest days are dropped early. Whole days are dropped by age | The janitor catches up across runs. A DB error at ingest loses that batch, which is counted (Error through the budget). Bucket levels persist every 60 s (B-44), so a restart does not refill them. While the server is down, the agent buffers telemetry (B-23) | Any 24 h window: capacity + refill = 1.25 × 512 MiB → **0.625 GiB** (long-run average ≤ 0.5 GiB per day) | AC-107, AC-111, AC-127, AC-134, AC-147, AC-148, AC-164, AC-165, AC-169; `TestDUX_SEC023_NameRotationHeldByDeviceCeiling` |
| B-02 | Gap rows (`gap_agent`, `gap_server`, and from rev 3.3 `gap_quota`) | server DB (same partitions) | `StoredLogGapRowsPerDevice` = 200 per 60 s, shared by all three kinds; 1 gap entry per container per push; `StoredLogGapCountMax` = 10,000,000 | AD-035 §5 rules 3, 6 and 7 | Further drops counted only in `log_ingest.rejected_24h` | Rotate with their partitions | 200 × 1,440 = 288,000 rows × 200 B ≈ **55 MiB** | `TestDUX_SEC023_GapEntriesValidatedAndBounded`; AC-143 (`devicestate`); AC-165 |
| B-03 | Re-stamped rows (D-42) | server DB | No allowance of their own: they count against B-01 | `applyWindow` runs before the ceilings (AD-035 §1a) | As B-01 | Never in DEFAULT (§1b rule 4) | included in B-01 | AC-122, AC-123 |
| B-04 | DEFAULT partition `container_logs_default` | server DB | Expected 0 rows. PART-001 never drops it, so it must stay empty | Re-stamp (§1a) and the ingest partition guard (§1b) keep it empty; PART-001 `DefaultRowCount` health | Warn plus one `log_partition_default_rows` event per transition | Operator-visible. This story's ingest never writes to it | **0** | AC-124, AC-135 |
| B-05 | Partition count and drop locks | server DB catalog | at most retention + 1 behind + 3 ahead + legacy + default ≤ 96. Drops: `DETACH … CONCURRENTLY` when the parent has no DEFAULT partition, otherwise `DROP TABLE` with `lock_timeout` 1 s (D-53); at most 24 drops and 20 s per run; the run stops at the first lock timeout (rev 3.2) | PART-001 manager and janitor (§1b) | Oldest dropped; ahead count fixed; `COPY` waits no more than about 1 s per run | Catch-up of at most 24 drops per run, idempotent; pending detaches finalized | 0 (catalog only) | AC-134; AC-151 |
| B-06 | Per-(device, container) rolling limiter | server memory | `StoredLogContainerLimiterMax` = 512 per device (LRU); idle eviction 10 min | `devicestate` registry (AD-035 §5 rule 5) | LRU entry evicted; the evicted container falls back to the device ceiling | Rebuilt from empty after restart, which fails safe: the device ceiling still applies | 0 growth; at most 512 × ~96 B ≈ 48 KiB per device | `TestDUX_BND_ContainerLimiterLRU` |
| B-07 | Per-device state (unknown budget, device ceiling, skew and rejected counters, clock-skew state, event-throttle state, log-budget state) | server memory | One entry per registered device; pruned on device delete and when idle for 24 h (10-minute sweep) | `devicestate` registry | Evicted | Reset on restart (R-18) | 0 growth; ≈ 2 KiB per device | `TestDUX_BND_DeviceStatePruned` |
| B-08 | Live hub streams | server memory | `LiveStreamsPerHost` = 8 per host | AD-016 | Refused with `too_many_streams` | `AgentGone` clears them; retired IDs are never reused; the agent reaps orphans after 5 min (SEC-025) | 0 | AC-058; `TestDUX_SEC025_*` |
| B-09 | Viewers and per-viewer queues | server memory | `LiveViewersServer` = 64; `LiveViewersPerStream` = 8; queue 64 frames **and** `LiveViewerQueueBytes` = 1 MiB (rev 3.1) → at most 64 MiB in total | AD-016 fan-out | Frame dropped for that viewer, with `dropped{n}`; after 10 s of drops that viewer is detached | Socket close frees everything; 4 h socket cap; session expiry (SEC-027) | 0 growth; ≤ 64 MiB server-wide | AC-143; AC-058 |
| B-10 | Backfill queue and coalescing map | server memory | 2 running and 16 queued per host; 1 per 10 s per (socket, container) | AD-016 step 3 (SEC-026) | Refused, and the viewer shows a gap marker | Cleared on socket close or `AgentGone` | 0 | `TestDUX_SEC026_ReattachStormBounded` |
| B-11 | Agent RPC pending reads | server memory | 4 per device; `DockerReadsConcurrentServer` = 16 server-wide (rev 3.1); responses ≤ 1 MiB each | AD-019 `agentrpc.Call` | 429 `rate_limited` | A 10 s timeout removes the entry | 0 growth; ≤ 16 MiB in flight | AC-143 |
| B-12 | Prune preview store | server memory | `PreviewMaxServer` = 50; `PreviewTTL` = 10 min; sweep every 60 s and on access; items per preview ≤ inventory caps (2,000) | AD-029 | Oldest evicted | Lost on restart; the operator previews again | 0 growth; ≤ 50 × ~400 KiB ≈ 20 MiB | AC-155 (`TestDUX_BND_PreviewStoreCapAndSweep`, rev 3.2); AC-092; `TestDUX_SEC007_PreviewIDEntropyAndSingleUse` |
| B-13 | Typed confirmation values | none (stateless) | n/a | Compared per request (AD-009) | n/a | n/a | 0 | AC-028 |
| B-14 | Rate limiters and throttles (report limiter, read limiter, command-creation and terminal-open limiters (D-48), D-51 ingest buckets, the system-command cap, `LogThrottle`, log budget) | server memory | One bucket per device per limiter; idle eviction after 10 min, checked every 256 calls. **Buckets are created only for a validated device** (rev 3.2): for agent routes, after `DeviceAuth`; for D-48, after the device is found and, for terminals, connected. Random IDs in a URL create nothing | the CMD-LIFECYCLE AD-020 pattern; AD-040; AD-041 §4 | Evicted | Rebuilt after restart | 0 growth (KiB) | AC-156 (`TestDUX_BND_LimitersKeyOnlyKnownDevices`, `TestDUX_BND_LimiterIdleEviction`); AC-140; `TestCL_SEC001_DeviceLimiterTokenBucket` |
| B-15 | Agent connection registry | server memory | One entry per device | CMD-LIFECYCLE AD-009 (compare-and-delete, `replace`) | Replaced | n/a | 0 | CMD-LIFECYCLE registry tests |
| B-16 | Audit rows (`terminal_sessions`) | server DB | 180 days `long` / 90 days `short` (D-21). Written only by authenticated admin actions. Writes: operator commands per B-17; reads ≤ 30 per minute per host; log attaches coalesced over 2 min **and** at most `AuditLogAttachRowsPerDevicePerMinute` = 32 per device per minute, plus one overflow summary row (rev 3.2); terminal opens ≤ 30 per minute per device (D-48) | AD-016 step 4, AD-024, AD-025 | Purged per class in batches; open rows never purged; attaches over the cap counted in the summary row | Purge catches up across runs (AD-025 self-healing). Never evicted early by the budget or headroom guard: they are the security trail | (46,080 attaches + 1,440 summaries + 43,200 reads + 43,200 terminal opens + 1,560 commands) = 135,480 × 300 B ≈ 41 MB → **0.038 GiB** | AC-157 (`TestDUX_BND_AuditRowsUnderAttachStorm`); AC-132; AC-138; `TestDUX_SEC009_RetentionPerKind` |
| B-17 | `commands` rows (`steps`, `target_results`) | server DB | 90 days. Operator creation: `CommandCreatePerDeviceBurst` = 120 and `CommandCreatePerDevicePerHour` = 60 (D-48). **Non-operator origins: `SystemCommandsPerDevicePerHour` = 30, and at least `SystemPolicyMinCooldown` = 15 min between system commands for the same policy and target, enforced by the server** (rev 3.2). `steps` ≤ 64 entries; `target_results` ≤ 100 plus a summary. Result bytes count against `DeviceDailyCommandBytes` = 64 MiB per device per UTC day (D-50) and the fleet `CommandResultsBudgetDefault` = 4 GiB (D-49) | `commands.Service.Dispatch`, `sanitizeReport`, AD-023, AD-025, AD-041 | Operator: 429 `rate_limited`. System: not created, with a budgeted Warn and an hourly event. Steps and results truncated with a marker. Over the quota, results reduced to the summary entry | Adaptive batched purge with catch-up (AD-025); budget eviction (AD-041 §2) | 120 + 1,440 + 720 = 2,280 commands × ~2 KiB of fixed row ≈ 4.5 MiB, plus result bytes inside the 64 MiB quota shared with B-18 → B-17 and B-18 together **0.070 GiB** (rev 3.1: 0.66 GiB) | AC-158 (`TestDUX_BND_SystemCommandCap`); AC-068; AC-140; AC-147 |
| B-18 | `command_output` (`combined`, `steps`) | server DB | `combined` ≤ 256 KiB (CMD-LIFECYCLE); `steps` ≤ `StepOutputMaxBytes` = 64 KiB; 30 days. Counted in `DeviceDailyCommandBytes` (D-50) and the fleet budget (D-49) (rev 3.2) | `sanitizeReport`, AD-023, AD-041 | Head and tail kept, "Output truncated"; over the quota, a 1 KiB head-and-tail stub | Adaptive batched purge, output deleted before its commands (AD-025); budget eviction deletes the oldest output first | Inside B-17's 0.070 GiB (2,280 stubs × 1.2 KiB ≈ 2.7 MiB beyond the quota) | AC-068; AC-138; AC-147; AC-150 |
| B-19 | Agent-originated events (`events`): Docker events, agent events (`/events`) and probe-failure events | server DB | 90 days (existing). `EventsPerContainerPerHour` = 60; `EventsPerDevicePerHour` = 600, shared by every agent-originated kind (rev 3.2); one summary per device per hour (D-47). Per-container counters: an LRU of `EventThrottleContainersMax` = 512 per device, overflow counted against the device bucket. Every stored text field ≤ `EventTextMaxBytes` = 1 KiB, and a row's text ≤ `EventRowTextMaxBytes` = 3 KiB, cut at a rune boundary (rev 3.2) | `events/generator.go` throttle; `EventRepo.Create` text caps (AD-040) | Excess not stored; one "N events suppressed" summary | Existing purge; emergency eviction beyond 7 days (AD-041 §3) | 600 × 24 + 24 summaries = 14,424 rows × (3 KiB + 200 B) ≈ 45 MiB → **0.044 GiB** | AC-139 (extended: 10,000 random names, an oversized message); `TestDUX_BND_EventThrottleMapBounded`; `TestDUX_BND_EventTextCapped` |
| B-20 | `docker_inventory` | server DB | 1 row per device; ≤ `InventoryMaxBytes` = 4 MiB; a changed write at most every 14 min (plus refresh windows) | AD-028 | Truncated with flags; out-of-cadence pushes ignored | Latest only; cascades on device delete | live growth 0; churn ≤ 103 × 4 MiB ≈ 412 MiB of dead tuples, reclaimed by aggressive autovacuum | AC-090; `TestDUX_SEC010_InventoryFieldBoundsAndSizeCap`; `TestDUX_SEC010_InventoryChangedPayloadRateLimited` |
| B-21 | `docker_stacks` (including forgotten and Down rows) | server DB | `StacksPerDeviceMax` = 500 rows, which forgotten and Down rows count towards; field bounds; `StackObserveWritesPerHour` = 500 per device (rev 3.1) | AD-026 | New projects ignored, or a Down stack unseen for 90 days evicted; writes over budget skipped for the hour | Idempotent upsert; the next observation corrects state | live ≤ 6 MiB (not per day); churn ≤ 12,000 × 12 KiB ≈ 141 MiB of dead tuples (autovacuum reloptions) | `TestDUX_SEC010_StacksPerDeviceCapped`; `TestDUX_BND_StackObserveBudget` |
| B-22 | Prune and remove writes | server DB (`commands`, `command_output`) | `target_results` ≤ 100 plus a summary; item detail ≤ 64 KiB `steps` output; items per execute ≤ preview items (≤ 2,000); prunes ≤ D-48 | AD-029 (rev 3.1) | Truncated with a marker | As B-17 and B-18 | included in B-17 and B-18 | AC-094; `TestDUX_BND_PruneResultsTruncated` |
| B-23 | Agent telemetry buffer (`buffer.go`, SQLite) | agent disk | `maxBuffered` = 100 snapshots (existing) **and** `AgentBufferMaxBytes` = 64 MiB (rev 3.1, WP-P0A) | `Buffer.Store` | Oldest snapshot dropped | Server down for days: the newest ≤ 100 snapshots and ≤ 64 MiB are kept. Disk full: the store fails, telemetry is dropped, and a budgeted Warn is logged | 0 growth (fixed cap) | AC-142 |
| B-24 | Agent outbox, seen store, in-flight markers | agent disk | Outbox ≤ 200 entries, ≤ 1 MiB, ≤ 256 KiB per entry, 24 h; seen store ≤ 1,000 entries, 48 h; markers only for active commands | CMD-LIFECYCLE AD-010, AD-011 | Oldest dropped (Warn) | Disk failure: degraded in-memory mode with the same caps | 0 growth | CMD-LIFECYCLE outbox and state tests |
| B-25 | Agent stack registry `docker-stacks.json` | agent disk | 500 projects (≤ ~5.5 MiB); entries unseen for 90 days pruned | AD-021 | Oldest unseen entry evicted | A failed write keeps the state in memory and retries at the next collection | 0 growth | AC-159 (`TestDUX_BND_StackRegistryCapAndPrune`, rev 3.2: the 501st project evicts the oldest unseen entry; 90-day prune; file size bound) |
| B-26 | Agent live-log follows | agent memory | 8 streams × (20 KiB line buffer + `AgentStreamPendingBatches` = 2 × 64 KiB) ≈ 1.2 MiB | AD-017 (rev 3.1) | Pending full: lines dropped and counted in `dropped{n}` | Disconnect cancels all streams; idle reap after 5 min | 0 growth | `TestDUX_SEC006_NoNewlineStreamBoundedMemory`; `TestDUX_BND_AgentPendingBatches` |
| B-27 | Agent backfill reads | agent memory | 2 concurrent; streamed: one 64 KiB batch plus one 16 KiB look-ahead line; the skip scan only counts | AD-017 | Queue refusal (server side) | Cancelled on disconnect | 0 growth; peak heap growth < 4 MiB per backfill | AC-160 (`TestDUX_BND_BackfillStreamsWithoutBuffering`, rev 3.2); `TestDUX_SEC026_LiveKeepsBudgetDuringBackfill` |
| B-28 | Agent collector state (`capRing`, scan, `lastSeen`, TTY cache, `ioRates` previous samples) | agent memory | `capRing` ≤ `max_bytes` (≤ 256 KiB), one container at a time; scan ≤ 20,000 lines and 8 MiB, streamed; every map pruned to the listed containers on each collection | AD-035 §3–§4, AD-004 | Ring evicts oldest; scan stops and reports a lower bound | Rebuilt after restart | 0 growth | AC-110; `TestDUX_BND_CollectorMapsPruned` |
| B-29 | Agent event bus and `activeDockerTargets` | agent memory | Channel of 64 per subscriber, dropping when full; at most `AgentEventBusSubscribersMax` = 10 subscribers; `Subscribe` returns an unsubscribe function that the watcher calls in its cancel path (rev 3.2); a target is removed when its command ends (defer) | AD-017, AD-023 | Events dropped for that subscriber; a new subscription is refused when the bus is full, and that stream runs without `restarted` markers | Cancel unsubscribes; disconnect cancels every stream | 0 growth | AC-153 (`TestDUX_BND_EventBusUnsubscribeOnCancel`); `docker_events_bus_test.go` |
| B-30 | Server logs (`server_logs`, Warn and above persisted) | server DB | 1 record per (device, message key) per minute; `DockerWarnBudgetPerHour` = 1,000 per hour server-wide from this story's code; existing 7-day purge | `dockerlog.Budget` (§14.1 item 30) | Suppressed; one summary per window | Existing purge | ≤ 600 per hour per device × 24 × 500 B ≈ **7 MB** (server-wide ≤ 24,000 per day ≈ 12 MB) | AC-141 |
| B-31 | Agent logs → journald → `device_logs` (via the `logs` collector) | agent and server DB | 1 record per message key per minute in the new agent paths, plus a summary; inside the B-40 `device_logs` quota (rev 3.2) | `agentlog.Budget` (§14.1 item 31) | Suppressed, plus a summary | `device_logs` retention (partitioned by PART-001); D-50 quota | ≤ 20 keys × 1,440 × 300 B ≈ 9 MB, inside B-40 | AC-141 |
| B-32 | Browser log view buffer | browser | `LOG_BUFFER_MAX` = 10,000 lines and `LOG_VIEW_MAX_BYTES` = 32 MiB | `logBuffer.ts` | Oldest dropped, with a notice | Freed on reload | n/a | AC-053; `logBuffer.test.ts` |
| B-33 | Browser storage | browser (`localStorage`) | Palette recent list 8; one view-mode key; one time-preference key | AD-014, AD-011 | Oldest dropped | n/a | n/a | AC-046 |
| B-34 | FR-062 fields in `telemetry_snapshots` | server DB | Part of B-38 (fixed in rev 3.2). A snapshot is stored only within D-51's rate (1 per 15 s, burst 120) and D-50's `DeviceDailyTelemetryBytes` quota. About 150 B per container per snapshot; snapshot body ≤ 25 MiB before decoding, ≤ 20 MB after (existing guard); 30 days; partitioned after PART-001 | `ingest.Limit` and `quota.Daily` (AD-040, AD-041) | As B-38 | As B-38 | 150 B × 200 × 5,880 ≈ 176 MB, inside B-38's 1 GiB | AC-144; AC-147; AC-041 |
| B-35 | `container_metrics` I/O columns (WP-2D) | server DB | 32 B per existing row; 7-day partitions; rows stored only within B-38's rate and quota (rev 3.2) | WP-2D; AD-041 | As B-38 | PART-001 manager | Inside B-38 | WP-2D PG test; AC-147 |
| B-36 | Self targets and projections | server memory | Nothing cached: computed per request from the latest snapshot | AD-009, AD-003 | n/a | n/a | 0 | AC-098 |
| B-37 | Janitor and purge state | server memory | O(1) per purge or janitor | AD-035 §1b, AD-025 | n/a | Backoff; stale events | 0 | AC-134; AC-136; AC-138 |
| B-38 | Telemetry snapshots and container metrics (`telemetry_snapshots`, `container_metrics`) (rev 3.2, SEC-035) | server DB | `TelemetryBodyMaxBytes` = 25 MiB before decoding; existing 20 MB snapshot guard; `TelemetryIngestInterval` = 1 per 15 s per device, `TelemetryIngestBurst` = 120 (D-51); a `telemetry` bucket of `DeviceDailyTelemetryBytes` = 1 GiB per 24 h, capacity 25 %, refilled continuously, counting snapshot bytes plus 200 B per metric row (D-50, D-54); downsampled mode: at most one snapshot per 15 min, stored even with the bucket empty, trimmed to `TelemetryDownsampleMaxBytes` = 2 MiB (rev 3.3) | `ingest.Limit` (AD-040); `quota.Daily` (AD-041 §1); `Telemetry` handler | Over the rate: 429 with `Retry-After`, nothing stored, and the agent buffers (B-23). Bucket low: downsampled, so the latest stored state is never more than 15 min old; last-seen, broadcast and alerts still run; one event per transition | Bucket persisted (B-44); PART-001 partitions rotate; emergency eviction down to a 2-day floor, and 1 day at critical (AD-041 §3) | 1.25 × 1 GiB + 96 × 2 MiB → **1.44 GiB** (long-run ≤ 1.19 GiB per day; without the quota: 5,880 × 20 MB ≈ 110 GiB) | AC-144; AC-146; AC-147; AC-166 |
| B-39 | Heartbeats (`heartbeats`; `devices` last-heartbeat UPDATE) (rev 3.2) | server DB | `HeartbeatBodyMaxBytes` = 64 KiB; `HeartbeatIngestInterval` = 1 per 10 s per device, burst 6 (D-51) | `ingest.Limit` (AD-040) | Excess: `200 {"status":"ok","throttled":true}`; nothing stored, no device UPDATE, no pending-command delivery | Existing purge (`RIOT_RETENTION_DAYS`); PART-001 partitions | (8,640 + 6) × ~500 B ≈ 4.1 MiB → **0.004 GiB** | AC-145 |
| B-40 | Device logs (`device_logs`), from telemetry `logs` and from `POST /logs` (`fetch_logs`) (rev 3.2) | server DB | `DeviceLogBodyMaxBytes` = 8 MiB; `DeviceLogPushesPerMinute` = 6; existing `MaxDeviceLogBatch` = 20,000 rows per push; a `device_logs` bucket of `DeviceDailyDeviceLogBytes` = 256 MiB per 24 h, capacity 25 %, refilled continuously (D-50, D-54); sampled mode: priority ≤ 3 first, others 20 per minute (rev 3.3) | `ingest.Limit`, `quota.Daily` | 429 over the rate; bucket low: sampled, the rest counted, one event per transition | Bucket persisted; existing retention; PART-001 partitions; emergency floor of 2 days | 1.25 × 256 MiB → **0.31 GiB** (long-run ≤ 0.25 GiB) | AC-146; AC-147 |
| B-41 | Agent event and probe-result ingest (`/docker-events`, `/events`, `/probe-results` → `events`, `device_probe_results`) (rev 3.2) | server DB | Bodies 1 MiB; `AgentEventsPerMinute` = 120 per device, shared by `/docker-events` and `/events`; probe results: 6 pushes per minute, ≤ 64 results per push, enabled probes of this device only, ≤ 1 stored result per probe per 30 s | `ingest.Limit`; `ReceiveDeviceProbeResults` (AD-040) | 429 over the rate; invalid or excess results dropped and counted | Existing purges | Events in B-19. Probe results: 2,880 per probe × 20 probes × 300 B ≈ 16 MiB → **0.016 GiB** (probes are operator-configured) | AC-146 (`TestDUX_BND_ProbeResultBounds`) |
| B-42 | Agent WebSocket frames (`/ws/agent`) (rev 3.2) | server memory | `AgentWSReadLimit` = 4 MiB per frame (the merged code sets none); `AgentWSFramesPerSec` = 200 per device, burst 400; the existing report limiter and per-frame log caps | `HandleAgentWS` (AD-040) | Oversized frame: socket closed (1009). Excess frames dropped and counted; 3 s of continuous excess closes the socket (1008) | Agent reconnects; the outbox replays reports (CMD-LIFECYCLE) | 0 storage; ≤ 4 MiB read buffer per socket | AC-146 (`TestDUX_BND_AgentWSReadLimitAndFrameRate`) |
| B-43 | Server state keyed by agent-chosen identifiers: retired stream IDs, unknown-stream unfollow, event-throttle counters, D-51 ingest buckets, on-demand partition attempts (rev 3.2, SEC-036) | server memory | Retired IDs: an LRU of `LiveRetiredIDsPerDevice` = 256, TTL 10 min. Unknown-stream unfollow: one per-device bucket (4 per second, burst 8), no per-ID state. Event-throttle counters: B-19's LRU of 512. Ingest buckets: one per authenticated device, idle eviction after 10 min. Partition attempts: one entry per day inside the window (≤ 96), pruned outside it | `livelogs/hub.go`, `events/generator.go`, `ingest`, `partitionGuard` | LRU evicts; unknown frames beyond the bucket dropped without a reply | Rebuilt from empty after restart | 0 growth; ≤ 256 × ~48 B per device for retired IDs | AC-153 (`TestDUX_BND_RetiredStreamIDsBounded` with 100,000 random IDs; `TestDUX_BND_EventThrottleMapBounded`); AC-152 |
| B-44 | Usage rows and bucket levels (`device_storage_usage`, plus memory) (rev 3.2; amended in rev 3.3) | server DB and memory | One row per device per UTC day, with daily totals and the latest bucket levels; flushed at most once per 60 s per device; rows older than max(effective container-log retention + 1, 7) days, at most 91, deleted by the janitor; one in-memory entry per active device | `quota.Daily` (AD-041 §1) | n/a | Levels seeded from the latest row plus the elapsed refill; a crash loses at most 60 s of accounting | 1,440 HOT updates of one ~150 B row per day; at most 91 rows kept per device | AC-147; AC-169 |
| B-45 | Fleet storage budget, fairness and disk headroom (rev 3.2, D-49; rev 3.3, D-55, D-56) | server DB (whole volume) | `ContainerLogBudgetDefault` = 16 GiB (soft; hard at `StorageBudgetHardFactor` 110 %) and `CommandResultsBudgetDefault` = 4 GiB, each capped at 50 % of (class usage + DB free space) when free space is known. Fairness: heavy devices' `logs` allowance lowered to the fair share (at least 32 MiB per day). Thresholds, absolute first: low below max(4 GiB, min(15 %, 50 GiB)); emergency below max(1 GiB, min(5 %, 10 GiB)); critical below 512 MiB | `storage.Budget`, `storage.Headroom`, in the janitor loop (AD-041 §2, §3) | Soft limit: fairness. Hard limit or disk pressure: oldest container-log days dropped early (floor: today and yesterday); oldest command results deleted. Emergency: the AD-041 §3 order, ending in minimal mode. Critical: a 1-day floor | Automatic. Allowances return after 24 h at or below 80 %; early eviction stops at 90 % of the budget; emergency mode ends after 10 min above the low threshold | Fleet: container logs ≤ 17.6 GiB plus command results ≤ 4 GiB, whatever the device count | AC-148; AC-149; AC-167..AC-169 |
| B-46 | Server-host disk: rIOt's own container logs (server and database stdout and stderr) (rev 3.2, SEC-038) | server host disk | Both shipped compose files: `logging: {driver: json-file, options: {max-size: "10m", max-file: "5"}}` per service, so ≤ 50 MiB each. Agent-ingest request lines summarized, one per device per 10 min; error responses through the budget | Compose files; `middleware.Logger` (AD-040) | Docker rotates the files | Self-rotating. The README states the same for custom deployments | 0 growth beyond 100 MiB in total | AC-154 |
| B-47 | Docker images left on managed hosts by rIOt-initiated updates (rev 3.2, D-52) | managed host disk (not the server) | Opt-in `docker.prune_superseded_images` (default false, host-local). When on, after a successful update or redeploy, the superseded image ID is removed if it is unused and untagged, through `ImageRemove(Force:false, PruneChildren:false)`; never a `*Prune` API | `docker_image_cleanup.go` (AD-029, WP-4A) | In use or still tagged: skipped and recorded in steps | A failed removal is recorded; the P4 prune can reclaim it later | Setting off: one superseded image per update, reclaimable through the P4 prune. Setting on: 0 | AC-161 |
| B-48 | Browser Activity pages (rev 3.2) | browser | `ACTIVITY_MAX_PAGES` = 40 pages (about 1,000 entries), through TanStack `maxPages` | `ActivityList.tsx` | Oldest page dropped; scrolling back refetches | Freed on reload | n/a | AC-162 |
| B-49 | Quota and sampling state: per-device buckets (4 classes), degraded-mode flags, per-container per-minute sample counters, fairness allowances (rev 3.3, D-54, D-56) | server memory | One entry per active device in the `devicestate` registry; sample counters inside the B-06 LRU of 512 per device, reset each minute; one allowance per device | `quota.Daily`, `storage.Budget` | LRU evicts; an evicted container starts a fresh minute's sample count | Bucket levels restored from `device_storage_usage` plus the elapsed refill; allowances recomputed by the next janitor run | 0 growth; ≈ 1 KiB per device | AC-164; AC-165; AC-169 |
| B-50 | Headroom probe volume `riot-fsprobe` (rev 3.3, SEC-040) | server host disk | Empty by design: the server only calls `statfs` on it, and never writes to it | Compose files; `storage.Headroom` | n/a | n/a | 0 | AC-170 |
| B-51 | Lock-blocker diagnostics (rev 3.3, SEC-041) | server memory and logs | At most `BlockerReportMax` = 16 backends per report; one budgeted Warn per run; no query text | `storage/blockers.go`, `janitor_dux.go`, `storage.Headroom` | Truncated at 16 | n/a | 0 (inside B-30) | AC-171 |

**Worst case per device per day (rev 3.3).** This is a hostile or runaway
ceiling, not a forecast. It includes the pre-existing agent ingest paths.

- Figures are for any 24 h window: bucket capacity plus refill (D-54).
- Long-run daily averages are lower, and are shown in brackets.
- The rise from rev 3.2 is the cost of degrading instead of stopping: bucket bursts, plus the trimmed telemetry that keeps device state fresh.

| Growth path | GiB per device per day |
|---|---|
| B-01 stored logs (bucket: 1.25 × 512 MiB) | 0.625 (0.50) |
| B-02 gap rows, including `gap_quota` | 0.054 |
| B-17 + B-18 command results (bucket plus fixed rows) | 0.085 (0.070) |
| B-16 audit (admin-driven) | 0.038 |
| B-19 events (text-capped, one D-47 device bucket) | 0.044 |
| B-30 server logs | 0.007 |
| **This story's paths** | **≈ 0.85** (long-run 0.71; rev 3.2: 0.71; rev 3.1: 4.97) |
| B-38 telemetry snapshots and container metrics (bucket plus 96 trimmed snapshots), including B-34 and B-35 | 1.44 (1.19) |
| B-40 device logs (bucket), including B-31 | 0.31 (0.25) |
| B-39 heartbeats | 0.004 |
| B-41 probe results (20 probes) | 0.016 |
| **All paths** | **≈ 2.62** (long-run ≈ 2.17; rev 3.2: 1.98) |

On top of that, B-20 and B-21 churn produces 0.54 GiB of dead tuples per day
(B-44 adds a negligible amount). Autovacuum makes that space reusable, so it
does not accumulate.

**Retained worst-case footprint per device, at the default retentions (rev 3.3).**

| Growth path | Retention | Footprint |
|---|---|---|
| Stored logs and gap rows | 8 day partitions: 0.125 GiB burst + 8 × (0.5 + 0.054) GiB | ≈ 4.6 GiB |
| Command results | 90 and 30 days, capped by the 4 GiB fleet budget | ≤ 4.0 GiB |
| Audit (admin-driven) | 180 and 90 days | ≈ 4.5 GiB |
| Events | 90 days (0.044 GiB × 90) | ≈ 4.0 GiB |
| Server logs | 7 days | ≈ 0.05 GiB |
| **This story's paths** | | **≈ 17 GiB** (rev 3.1: 72 GiB) |
| Telemetry snapshots | 30 days: 0.25 GiB burst + 30 × 1.19 GiB | ≤ 36 GiB |
| Device logs | 30 days: 0.06 GiB burst + 30 × 0.25 GiB | ≈ 7.6 GiB |
| Heartbeats and probe results | 30 days | ≈ 0.6 GiB |
| **All paths** | | **≈ 61 GiB** (rev 3.2: 55 GiB) |

**Fleet worst case (D-49, D-55, D-56).**

- **Container logs and command results:** at most 17.6 GiB (the 110 % hard limit) plus 4 GiB, so 21.6 GiB in total, whatever the number of devices. Before the hard limit binds, fairness shrinks the heaviest devices first.
- **Other agent-driven data:**
  - at most about 48 GiB per hostile device at the default retentions: telemetry 36, device logs 7.6, events 4.0, and heartbeats, probe results and server logs 0.7;
  - fleet-wide, the disk-headroom eviction (AD-041 §3) keeps free space above the emergency threshold, as long as the irreducible floor fits.
  - **The irreducible floor** is 21.6 GiB + N × 3.8 GiB: the 2-day partition floor at pre-emergency rates (3.5 GiB) plus the 7-day event floor (0.31 GiB) per hostile device. For 25 hostile devices, that is about 117 GiB.
  - **At critical,** the floor falls to 21.6 GiB + N × 2.1 GiB, about 73 GiB for 25 devices.
  - **Minimal mode** adds at most about 0.3 GiB per device per day while the emergency lasts: 64 KiB of log lines per minute, 96 trimmed snapshots, and 16 KiB of device logs per minute.
- **Audit rows** are written only by authenticated admin actions, and are never evicted early, because they are the security trail. They add at most about 4.5 GiB per device's worth of runaway admin traffic.
- **Without a free-space source** (AD-041 §3), the headroom guard is off. The fleet bound is then the sum of the per-device bounds plus the budgets.

**Default-cap host.** Consider a host that is not hostile, with 50 containers
all at the D-33 caps. It would produce 1.77 GiB of stored logs per day:

- 50 × 16 KiB × 1,440 = 1.10 GiB of line data;
- 3.6 M rows × 200 B = 0.67 GiB of row overhead.

The D-50 bucket stores about 0.5 GiB of it per day (0.625 GiB in a burst
window). Past 75 % of the allowance, stderr and error lines are kept first and
other stdout is sampled to 5 lines per container per minute, so the host never
stops storing new lines. Typical hosts stay far below the quota (see the
steady state below).

**Steady-state footprint, 25-device fleet.** Typical assumptions: 20
containers per device, 2 stored lines per minute per container of 200 B, 20
commands per day, 30 audit rows per day, 5 stacks, and 50 KB of inventory.

| Item | Arithmetic | Fleet total |
|---|---|---|
| Stored logs | 20 × 2 × 1,440 = 57,600 rows per day × 400 B ≈ 23 MB per day; × 8 days = 184 MB per device | **4.6 GB** |
| Commands and output | (20 × 2 KB × 90 d) + (20 × 10 KB × 30 d) ≈ 9.6 MB per device | **0.24 GB** |
| Audit | 30 × 300 B × 180 d ≈ 1.6 MB per device | **0.04 GB** |
| FR-062 snapshot fields (existing table) | 150 B × 20 × 1,440 × 30 d ≈ 130 MB per device | **3.2 GB** |
| Inventory and stacks | | **< 0.01 GB** |
| **Total** | | **≈ 8.1 GB** |

Memory and agent disk in the steady state:

- **Server memory:** under 2 MB of per-device state. Live hub memory is 0 when idle, and at most 64 MiB at its caps.
- **Agent disk:** at most 64 MiB of buffer, 1 MiB of outbox and 5.5 MiB of registry per host. These are usually close to empty.

**Recovery summary.**

| Failure | What heals it |
|---|---|
| Crash or restart | In-memory state rebuilds from empty, which fails safe. Quota counters are restored from `device_storage_usage`, losing at most 60 s of counts. Before serving, the janitor ensures every partition in the retention window. The purges run at startup and catch up in bounded steps (AD-035 §1b, AD-025). |
| Failed run or DB error | Retried with backoff, or on the next run. Stale events fire after 6 h (janitor) or 24 h (purges). |
| Lock contention | Partition drops wait at most 1 s, and the run stops at the first lock timeout. Purge batches halve on timeout and grow back (rev 3.2). |
| DB volume filling (corrected in rev 3.2; rev 3.3) | Fairness first, then early drops above 110 % of the budget. Low (absolute-first thresholds, D-55): a warning event. Emergency: the AD-041 §3 order drops partitions, the only operation that returns disk space. Minimal mode then keeps storing priority lines, sampled stdout and one snapshot per device per 15 min. Critical: a 1-day floor. **Row-DELETE purges free no disk:** they add WAL and dead tuples, and the space becomes reusable only inside the same table. |
| Disk completely full | PostgreSQL may refuse even partition drops, because they need WAL. That is why eviction starts at the emergency threshold. Inserts fail and are counted, and the server keeps serving reads. |
| Lock holder (rev 3.3, SEC-041) | The server's own idle-in-transaction sessions end after 60 s, and its reads time out after 30 s. A drop that hits a lock timeout reports the blockers. In emergency, only the server's own active blockers are cancelled; anything else is reported to the operator. |
| Agent offline for days | The agent-side caps hold (B-23..B-25). The startup ensure covers backlog days. After reconnect, old lines are re-stamped (§1a) and never sent to DEFAULT. The burst of 120 accepts a full buffer flush. |
| Misbehaving agent | 413 and 429 at ingest; daily quotas; events throttled and text-capped; agent-keyed maps LRU-bounded. |

## 14. Implementation Notes for Engineers

### 14.1 Patterns

1. **Prerequisite contracts first.** Code against the merged CMD-LIFECYCLE
   `commands.Service`. Never write `commands` status outside `Transition`.
   New columns (`target_results`, `steps`, actor) are written in the same
   guarded UPDATE, never in a separate statement.
2. **Typed-nil trap** (CMD-LIFECYCLE note 2). It applies to the new
   `livelogs.Hub` and `agentrpc` dependencies too.
3. **No raw clocks** in `livelogs`, `agentrpc`, the prune preview store,
   the inventory gate, the agent limiter, the server ceiling's rolling
   limiter, `sortFreeze`, `useHiddenStop` or `useLogView`. Use
   composition-root defaults only.
4. **SQL.** Filters are bound with `ANY($n)`. Status lists go through
   `SQLStatusList(typed…)` only. Keep the AD-007 conflict SQL in a pure
   builder with a placeholder-only test, like CMD-LIFECYCLE
   `buildFilteredQuery`.
5. **Logging.** Never log a log line, inspect content, compose content,
   `.env` content, a typed confirmation value, or a malformed identifier.
   Log lengths, counts and enums. Rejection logs go through `LogThrottle`.
6. **Sanitize.** `models.SanitizeText` on every agent string before a DB
   write or broadcast. This includes `target_results` messages, step
   outputs, actor names, and relayed log lines (the line cap is enforced
   after sanitizing).
7. **Docker SDK.** Use the existing `newDockerClient` with API negotiation.
   No `client.WithVersion` pinning. For TTY containers, use the raw log
   reader (AD-017, AD-035).
8. **`ImageRemove` with multiple tags.** Remove the listed tags with
   `ImageRemove(<repo:tag>)` first, then the ID. Never pass `Force:true`.
9. **Compose CLI.** Use `composeArgs` or `composeStackArgs` only, with no
   shell. Stack actions use `composeStackArgs`, which adds `-p=<project>`.
   Confirm that the installed compose v2 accepts `--pull=never` and
   `--pull=missing` on `up`, and record it in the implementation report. If
   not, drop `--pull` and rely on `--no-build` (Up) or `pull` being skipped
   (Redeploy).
10. **Web.**
    - No `any`.
    - `scrollbar-thin` on every overflow element (MEMORY rule).
    - Tokens only, with no `gray-<N>` or hex.
    - Enum text through `labels.ts`.
    - Times through `RelativeTime`, `Timestamp` and `useTimeFormatters`.
    - Status is never conveyed by colour alone. Icons carry `aria-label`.
    - The service colour in merged stack logs is assigned from a fixed
      tokenised palette of 8 classes by the service's index in the view.
      Every line also carries the service name as a text prefix.
11. **Existing tests** must keep passing. Moved test files keep their cases.
    The repo test count must not drop (NFR-020). Run `go test ./...`
    (without `-race` on Windows) and `cd web && npm run test:run`.
12. **Collector rule (MEMORY).** For WP-4A, update `doctor.go` and the
    README, and give the operator the exact `collector.enabled` line to add to each
    agent's `/etc/riot/agent.yaml`. The installer's default list is
    unchanged.
13. **Release.** No tagging or pushing is part of this story (FRD §10).
    The implementation reports name the release that must carry each WP-nA
    (§4.1).
14. **AD-034 first.** Add `NoFraming` globally, and put `SameOriginJSON` and `RequireRiotHeader` on the new route groups. The router matrix check fails if any route lacks them.
15. **Agent logging (SEC-016).** Log only lengths, counts, enums and names. Parse errors never quote their input.
16. **Drop before mask,** on both the agent and the server.
17. **Identity text.** Truncate, then mask, then compare. Live timestamps go through `t.Truncate(time.Microsecond)` before they are serialized.
18. **No-race reads.** Only `stackfs_linux.go` opens stack files. Never `os.Open` a stack path.
19. **Stack paths.** Every compose invocation uses the agreed record, re-verified on the agent. Never read labels at action time.
20. **Retention wiring.** WP-P0C changes only the `container_logs` window in PART-001's spec.
21. **No DELETE path for container logs.**
    - After WP-P0C, `ContainerLogRepo` has no `Purge`.
    - Before WP-P0C, leave `server.go:490` untouched.
22. **Compose runs as the agent user.** `up` fails for stacks whose `.env` is `0600 root`, just as reads do. The release notes say so.
23. **CMD-LIFECYCLE reconciliation (§2.5).**
    - New fields go on `DispatchRequest`.
    - `CreateIfNoConflict` takes the lock whenever the request is a Docker request or has a probe.
    - The hub's unregister hook also fires on `replace`.
    - `composeStackArgs` sits beside `composeArgs`, which is left as it is.
24. **Ingest order (P0).** The steps run in this order:
    1. validate;
    2. window and re-stamp;
    3. the per-container and per-device ceilings;
    4. mask;
    5. `InsertBatch`.

    Never mask before the ceilings (SEC-023).
25. **Clock domains.**
    - Partitioning, ordering and retention use `timestamp`, which the server bounds.
    - Identity, and the boundary sent to the agent, use `agent_ts` when it is set (AD-036).
26. **No `migrate force` in code or CI,** outside the PG test schemas.
    - The reconciliation is an operator procedure (§2.4.1).
    - `riot-server db-fingerprint` is read-only.
27. **Hub stream lifecycle.**
    - All attach, detach and linger transitions for one stream run under its mutex.
    - A retired stream ID is never reused (SEC-025).
28. **Audit for every operator command.** `commands.Service.Dispatch` writes the `command` audit row for every operator-initiated action, not only Docker ones (SEC-029).
29. **Bounds before code review (rev 3.1; the product owner's standing requirement).** Every new collection, map, queue, buffer, table or file MUST have a row in the §13A Bounds table before code review. The row names its cap as a constant in §14.2, and a test proves the cap (AC-137). These are not allowed:
    - an `append` to a slice, or a send on a channel, with no length or byte bound and no drop rule;
    - `make(map…)` with no maximum size and no eviction rule (LRU, idle timeout, or pruning to a known set);
    - a retention purge without the catch-up rules of AD-025 (bounded batches, idempotent, resumes on the next run);
    - a log statement on a per-line or per-request path without the log budget (item 30).
30. **Server log budget.**
    - Warn and Error records from this story's code go through `dockerlog.Budget`.
    - It allows at most one record per (device, message key) per minute, and `DockerWarnBudgetPerHour` (1,000) records per hour server-wide.
    - Suppressed records are counted and summarized once per window.
    - `server_logs` persists every Warn and Error record, so this budget is what bounds it.
31. **Agent log budget.**
    - In the new agent paths, records go through `agentlog.Budget`: one per message key per minute, plus a summary.
    - The `logs` collector ships the agent's journald output into `device_logs`, so this budget bounds that path too.
32. **Ingest limits before decoding (rev 3.2, SEC-035).** Every agent route wraps its body in `http.MaxBytesReader` before any decoder runs, and takes a token from the device's bucket first. Buckets are keyed by the device that `DeviceAuth` authenticated, never by a URL or body value alone.
33. **Only partition drops free disk (SEC-037).**
    - Never rely on row DELETEs to recover space.
    - Drops of `container_logs` partitions always run with `lock_timeout` of at most 1 s and a per-run time budget, and stop at the first lock timeout.
    - `DETACH … CONCURRENTLY` never runs inside a transaction block.
34. **Identifiers chosen by agents (SEC-036).** No map, set or bucket is keyed by an identifier that the agent chooses (stream IDs, container names, probe IDs) unless it has an LRU cap, or a per-device bucket replaces the per-ID state.
35. **Adaptive batches.** Every batched DELETE halves its batch on `statement_timeout`, and grows back on success. Children are deleted before parents, so no single statement cascades into an unbounded amount of TOAST.
36. **Degrade, never stop (rev 3.3, D-54, D-55).** No limit on stored data may stop storing all new data from a device.
    - Every limit samples or downsamples, keeps priority data first, and records what it dropped (gap rows, counters).
    - Only a database that refuses writes stops storage.
37. **Database sessions (SEC-041).**
    - Use the pool from `internal/server/db/db.go`, which sets `application_name` and the idle-in-transaction timeout.
    - Every new read path goes through `ReadTx`: read-only, with a 30 s statement timeout.
    - Never hold a transaction open across a network call to an agent or a browser.
38. **No database files in the server (SEC-040).** No container other than the database mounts the database volume. Free space is measured with `statfs` on a probe path on the same filesystem, or derived from `RIOT_DB_MAX_GB`.

### 14.2 Constants

All server constants live in `internal/models/docker_policy.go`.

**Reads, steps, commands and bulk**

| Constant | Value |
|---|---|
| `LiveLogMaxContainers` | 25 (containers in one merged view; Live is further limited by free stream slots, D-38) |
| `LiveLogBatchMaxLines` | 256 |
| `LiveLogBatchMaxBytes` | 65536 |
| `LiveLogBatchInterval` | 250ms |
| `DockerReadTimeout` | 10s |
| `DockerReadsPerMinute` | 30 |
| `DockerReadsConcurrentPerHost` | 4 |
| `InspectMaxBytes` | 1048576 |
| `ComposeFileMaxBytes` | 1048576 |
| `ComposeMaxFiles` | 10 |
| `DotEnvMaxBytes` | 262144 |
| `StepsMax` | 64 |
| `StepOutputMaxBytes` | 65536 |
| `TargetResultsMax` | 100 |
| `TargetResultMsgMax` | 1024 |
| `CommandRetention` | 90 days |
| `CommandOutputRetention` | 30 days |
| `InventoryMinInterval` | 15m |
| `InventoryRefreshMinSpacing` | 60s |
| `InventoryRunBudget` | 25s |
| `InventoryMaxImages` | 2000 |
| `InventoryMaxVolumes` | 2000 |
| `InventoryMaxNetworks` | 500 |
| `PreviewTTL` | 10m |
| `PreviewStaleAfter` | 15m |
| `PreviewMaxServer` | 50 |
| `BulkMaxTargets` | 100 |
| `TypedStopThreshold` | 5 |
| `TypedRemoveCountThreshold` | 20 |

**Container-log retention**

| Constant | Value |
|---|---|
| `ContainerLogRetentionDefault` | 7 days |
| `ContainerLogRetentionMin` | 1 day |
| `ContainerLogRetentionMax` | 90 days |

**Stored-log collection and ceiling**

| Constant | Value |
|---|---|
| `StoredLogAgentMaxLines` | 50 (accepted 1..1000) |
| `StoredLogAgentMaxBytes` | 16384 (accepted 1024..262144) |
| `StoredLogScanMaxLines` | 20000 |
| `StoredLogScanMaxBytes` | 8 MiB |
| `LogLineMaxBytes` | 16384 |
| `StoredLogServerCeilingLines` | 1000 |
| `StoredLogServerCeilingBytes` | 262144 |
| `StoredLogRollingWindow` | 60 s |
| `HistoryPageMax` | 5000 |
| `HistoryEndpointMaxSnapshots` | 100 |
| `LogTimestampFutureTolerance` | 5 min (= `models.TelemetryFutureTolerance`) |
| `StoredLogDeviceCeilingContainers` | 20 (allowance = min(N, 20) × the per-container ceiling, then capped) |
| `StoredLogDeviceCeilingLinesMax` | 5000 per 60 s (rev 3.1, D-46) |
| `StoredLogDeviceCeilingBytesMax` | 2 MiB per 60 s (rev 3.1, D-46) |
| `StoredLogGapRowsPerDevice` | 200 per 60 s |
| `StoredLogContainerLimiterMax` | 512 per device (LRU; idle eviction 10 min) |
| `StoredLogUnknownBudgetLines` | 200 per 60 s per device |
| `StoredLogUnknownBudgetBytes` | 65536 per 60 s per device |
| `StoredLogGapCountMax` | 10000000 |
| `ClockSkewEventIngests` | 3 consecutive ingests |
| `ClockSkewClearAfter` | 1 h |

**Live streams**

| Constant | Value |
|---|---|
| `LiveStreamsPerHost` | 8 |
| `LiveViewersPerStream` | 8 |
| `LiveViewersServer` | 64 |
| `LiveStreamLinger` | 2 s |
| `LiveUnfollowAgentMax` | 1 s |
| `LiveBackfillMax` | 5000 |
| `LiveBackfillTailDefault` | 100 |
| `LiveBackfillConcurrentPerHost` | 2 |
| `LiveBackfillQueuePerHost` | 16 |
| `LiveBackfillSkipScanMax` | 20000 |
| `LiveLinesPerSec` | 1000 |
| `LiveBytesPerSecStream` | 524288 |
| `LiveBytesPerSecAgent` | 2097152 |
| `ServerLiveBytesPerSecStream` | 786432 |
| `ServerLiveBytesPerSecAgent` | 3145728 |
| `LiveViewerQueue` | 64 frames |
| `LiveViewerMax` | 4 h |
| `LiveKeepInterval` | 60 s |
| `AgentStreamIdleReap` | 5 min |
| `LiveBackfillMinInterval` | 10 s per (socket, container) |
| `LiveBackfillBytesPerSec` | 1048576 (inside the agent budget) |
| `LiveSocketMax` | 4 h |
| `LiveViewerQueueBytes` | 1048576 per viewer (rev 3.1) |
| `AgentStreamPendingBatches` | 2 per stream |
| `DockerReadsConcurrentServer` | 16 server-wide |
| `LiveUnknownStreamUnfollowPerDevice` | 4 per second, burst 8, per device (replaces the per-ID rate; rev 3.2, SEC-036) |
| `LiveRetiredIDsPerDevice` | 256 (LRU) |
| `LiveRetiredIDTTL` | 10 min |
| `AgentEventBusSubscribersMax` | 10 (= `LiveStreamsPerHost` + 2) |

**Audit**

| Constant | Value |
|---|---|
| `AuditCoalesceWindow` | 2 min |
| `AuditRetentionLong` | 180 days (D-21, settled) |
| `AuditRetentionShort` | 90 days |
| `TerminalSessionMax` | 24 h |

**Self-healing and budgets (rev 3.1)**

| Constant | Value |
|---|---|
| `PartitionAheadDays` | 3 |
| `PartitionDropsPerRun` | 24 |
| `PartitionJanitorStartupBudget` | 60 s |
| `PartitionJanitorBackoffMax` | 60 min (1, 2, 4, … min) |
| `PartitionJanitorStaleAfter` | 6 h |
| `PartitionOnDemandRetry` | 60 s per day partition, server-wide (singleflight; rev 3.2) |
| `RetentionStartupDelay` | 5 min |
| `RetentionBatchRows` | 5000 |
| `RetentionRunBudget` | 30 s per purge per run |
| `RetentionStaleAfter` | 24 h |
| `CommandCreatePerDeviceBurst` | 120 (operator-initiated, D-48) |
| `CommandCreatePerDevicePerHour` | 60 (D-48) |
| `TerminalOpensPerDevicePerMinute` | 30 (D-48) |
| `EventsPerContainerPerHour` | 60 (D-47) |
| `EventsPerDevicePerHour` | 600 (D-47) |
| `DockerWarnBudgetPerHour` | 1000 server-wide |
| `LogBudgetPerKeyPerMinute` | 1 (server, per device and key; agent, per key) |
| `AgentBufferMaxBytes` | 64 MiB |
| `StackObserveWritesPerHour` | 500 per device |
| `DeviceStateIdleEviction` | 24 h |

**Ingest, quotas, budgets and headroom (rev 3.2)**

| Constant | Value |
|---|---|
| `TelemetryIngestInterval` | 15 s per device (D-51) |
| `TelemetryIngestBurst` | 120 |
| `TelemetryBodyMaxBytes` | 25 MiB |
| `HeartbeatIngestInterval` | 10 s per device, burst 6 |
| `HeartbeatBodyMaxBytes` | 64 KiB |
| `AgentEventsPerMinute` | 120 per device (`/docker-events` and `/events`, shared) |
| `AgentEventBodyMaxBytes` | 1 MiB |
| `DeviceLogPushesPerMinute` | 6 per device |
| `DeviceLogBodyMaxBytes` | 8 MiB |
| `ProbeResultPushesPerMinute` | 6 per device |
| `ProbeResultsPerPush` | 64 |
| `ProbeResultMinSpacing` | 30 s per probe |
| `ProbeResultBodyMaxBytes` | 1 MiB |
| `AgentWSReadLimit` | 4 MiB per frame |
| `AgentWSFramesPerSec` | 200 per device, burst 400 |
| `AgentWSExcessCloseAfter` | 3 s |
| `IngestLimiterIdleEviction` | 10 min |
| `AgentRequestLogSummaryInterval` | 10 min per device |
| `EventTextMaxBytes` | 1024 per stored text field |
| `EventRowTextMaxBytes` | 3072 per event row |
| `EventThrottleContainersMax` | 512 per device (LRU) |
| `AuditLogAttachRowsPerDevicePerMinute` | 32, plus one overflow summary row |
| `SystemCommandsPerDevicePerHour` | 30 (non-operator origins) |
| `SystemPolicyMinCooldown` | 15 min per policy and target |
| `DeviceDailyLogBytes` | 512 MiB (D-50) |
| `DeviceDailyTelemetryBytes` | 1 GiB (D-50) |
| `DeviceDailyDeviceLogBytes` | 256 MiB (D-50) |
| `DeviceDailyCommandBytes` | 64 MiB (D-50) |
| `QuotaRowOverheadBytes` | 200 |
| `QuotaFlushInterval` | 60 s |
| `QuotaUsageRetention` | max(effective container-log retention + 1, 7) days, at most 91 (rev 3.3) |
| `QuotaTruncatedOutputBytes` | 1024 |
| `ContainerLogBudgetDefault` | 16 GiB (`RIOT_CONTAINER_LOG_MAX_GB`, D-49) |
| `CommandResultsBudgetDefault` | 4 GiB (`RIOT_COMMAND_RESULTS_MAX_GB`, D-49) |
| `StorageBudgetDiskShare` | 50 % of (class usage + DB free space) |
| `StorageBudgetLowWater` | 90 % of the budget |
| `DBFreeLowFloorBytes`, `DBFreeLowPercent`, `DBFreeLowCapBytes` | 4 GiB, 15 %, 50 GiB: low below max(4 GiB, min(15 % of total, 50 GiB)) (rev 3.3, D-55) |
| `DBFreeEmergencyFloorBytes`, `DBFreeEmergencyPercent`, `DBFreeEmergencyCapBytes` | 1 GiB, 5 %, 10 GiB: emergency below max(1 GiB, min(5 % of total, 10 GiB)) (rev 3.3, D-55) |
| `DBFreeCriticalBytes` | 512 MiB (rev 3.3) |
| `DBFreeCheckInterval` | 5 min (60 s while low or in emergency) |
| `DBFreeRecoverAfter` | 10 min above the low threshold |
| `EmergencyPartitionFloorDays` | 2 (PART-001 tables) |
| `EmergencyDeleteFloorDays` | 7 (command results, events) |
| `PartitionStartupEnsureMax` | 94 creates (retention + 4) |
| `PartitionDropLockTimeout` | 1 s |
| `PartitionDropRunBudget` | 20 s per run |
| `PartitionDetachStatementTimeout` | 60 s |
| `PurgeBatchMax` | 5000 (= `RetentionBatchRows`) |
| `PurgeBatchMin` | 1 |
| `PurgeStatementTimeout` | 10 s |

**Quota shape, fairness, emergency and sessions (rev 3.3)**

| Constant | Value |
|---|---|
| `QuotaBucketCapacityShare` | 25 % of the daily quota |
| `QuotaDegradeBelow` | 25 % of capacity (more than 75 % used) |
| `QuotaRecoverAbove` | 50 % of capacity |
| `StdoutSampleLinesPerContainerPerMinute` | 5 (1 in emergency minimal mode) |
| `DeviceLogSampleEntriesPerMinute` | 20 |
| `ErrorLinePatterns` | Case-insensitive whole-word match on `err`, `error`, `fatal`, `panic`, `exception`, `traceback`, `crit`, `critical`, `emerg`, `alert`, `fail`, `failed`, `failure`; plus a `level=` or JSON `"level":` value of `error`, `fatal` or `crit` |
| `TelemetryQuotaDownsampleInterval` | 15 min |
| `TelemetryDownsampleMaxBytes` | 2 MiB |
| `TelemetryDownsampleKeepSections` | `system`, `cpu`, `memory`, `disks`, `network`, `docker` (JSON names confirmed against `FullTelemetryData`) |
| `StorageBudgetHardFactor` | 110 % of the effective budget |
| `FairShareFactor` | 0.9 |
| `FairShareMinBytesPerDay` | 32 MiB |
| `FairShareWindowDays` | 3 |
| `FairShareRestoreBelow` | 80 % of the budget, for 24 h |
| `EmergencyLogBytesPerDevicePerMinute` | 64 KiB |
| `EmergencyDeviceLogBytesPerDevicePerMinute` | 16 KiB |
| `CriticalPartitionFloorDays` | 1 (today) |
| `DBIdleInTxTimeout` | 60 s |
| `DBReadStatementTimeout` | 30 s |
| `BlockerReportMax` | 16 backends |
| `EmergencyCancelMinTxAge` | 30 s |

**Stacks**

| Constant | Value |
|---|---|
| `StacksPerDeviceMax` | 500 |
| `StackPathMaxBytes` | 1024 |
| `StackConflictMembersMax` | 20 |

**Inventory and prune**

| Constant | Value |
|---|---|
| `InventoryMaxBytes` | 4 MiB |
| `InventoryChangedMinInterval` | 14 min |
| `InventoryRefreshWindow` | 2 min |
| Inventory per-item bounds | tags 32, digests 32, `UsedBy` 64, containers 256, subnets 16, strings 512 B |
| `PreviewIDBytes` | 16 |

**Web** (`web/src/pages/docker/constants.ts`)

| Constant | Value |
|---|---|
| `LOG_BUFFER_MAX` | 10000 |
| `LOG_VIEW_MAX_BYTES` | 32 MiB |
| `HISTORY_REFRESH_MS` | 15000 |
| `HISTORY_DEFAULT_RANGE` | `'1h'` |
| `HIDDEN_STOP_MS` | 300000 |
| `SEL_URL_MAX` | 200 |
| `SEL_URL_MAX_CHARS` | 4096 |
| `PALETTE_MAX` | 50 |
| `PALETTE_RECENT` | 8 |
| `REORDER_FREEZE_MS` | 5000 |
| `ROW_HEIGHT` | 40 |
| `RECONNECT_MS` | 2000 |
| `OFFLINE_UNAVAILABLE_MS` | 60000 |
| `ACTIVITY_MAX_PAGES` | 40 (rev 3.2, B-48) |

### 14.3 Things to avoid

- `docker system prune`, `image prune`, `volume prune`, `network prune`, or
  the matching SDK calls.
- `compose down -v`, `--rmi`, or `--remove-orphans`.
- Storing any live line, inspect document, compose file or `.env` content,
  including in `server_logs` attributes (Warn+ records are persisted;
  CMD-LIFECYCLE note).
- New npm or Go dependencies (D-24 is the only candidate).
- Any reveal toggle.
- Row DELETEs as a way to free disk space. They free none (SEC-037).
- `DROP TABLE` or `DETACH PARTITION` on `container_logs` without `lock_timeout`; `DETACH … CONCURRENTLY` inside a transaction.
- Decoding any agent request body before its size cap and rate check.
- Mounting the database volume into any container other than the database (SEC-040).
- Any limit that stops storing all new data from a device (D-54, D-55).
- Cancelling or terminating a backend that is not the server's own (SEC-041).

---

## 15. Definition of Done

**Per WP**

- [ ] All component changes for the WP in §6 are implemented. No other
      files changed.
- [ ] Every AC row for the WP's phase in §10 has a test with the stated name,
      and it passes.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` pass on Windows.
      CI `go test -race -count=1 ./...` passes, including PG-gated tests.
- [ ] `cd web && npx tsc --noEmit && npm run test:run` passes. The test count
      has not decreased.
- [ ] The WP's migration (if any) runs up, down and up cleanly against a PG
      test database. The number is the next free one on `main` (§2.4),
      recorded in the implementation report.
- [ ] `go.mod`, `go.sum`, `web/package.json` and `web/package-lock.json` are
      unchanged (except the D-24 devDependency, if approved).
- [ ] Static guards pass: no native dialogs, no hex, no `gray-<N>`, no raw
      enums, no `dangerouslySetInnerHTML`, no private addresses or emails in
      the story's fixtures and docs.
- [ ] No `time.Now()` or `time.Since()` in the files listed in §14.1 item 3.
- [ ] The NFR-022 log messages exist, with the fields in §11.
- [ ] **Bounds (rev 3.1).** Every new collection, map, queue, buffer, table or file in the WP has a row in §13A, its cap constant in §14.2, and a passing test for that cap. AC-137 passes. No retention is added without the catch-up rules of AD-025 and AD-035 §1b.

**Phase-specific (P1..P4)**

- [ ] **P1:**
  - the AD-007 PG conflict matrix passes;
  - the FLEET-DASH allowlist test is updated and passes;
  - the label pass-through proof passes;
  - the auth matrix includes the bulk and hosts routes.
- [ ] **P2:**
  - AC-061 fairness test passes;
  - AC-049 and AC-064 no-storage tests pass;
  - redaction and masking tables pass on Go and TS;
  - retention purges are tested against PG;
  - the session-claim tests cover password, OIDC with `preferred_username`,
    OIDC without it, and a legacy token.
- [ ] **P3:**
  - `stackfs` tests pass (the Linux symlink cases in CI);
  - the down argv test passes;
  - compose in-place redaction keeps line numbers (golden files);
  - the compose `--pull` flag check is recorded.
- [ ] **P4:**
  - the doctor test passes;
  - the README lists `docker_inventory` and the exact config line;
  - the operator is told to add the collector to each agent's `collector.enabled`;
  - the volume no-side-effect test passes;
  - the preview single-use and expiry tests pass.
- [ ] **WP-2D:** PART-001 is merged; the partitioned insert test passes.
- [ ] Manual records in the implementation report: AC-100(b), and AC-101
      if D-24 is declined.

**P0**

- [ ] **WP-P0A and WP-P0B:**
  - AC-108, AC-110..AC-113 and AC-115 pass;
  - WP-P0B's `docker_ux_p0_log_agent_ts` runs up, down and up cleanly;
  - `server.go:490` is unchanged;
  - WP-P0A is not released without WP-P0B (SEC-028).
- [ ] **WP-P0C:**
  - PART-001 is merged whole under its own DoD, **renumbered above `000023_command_lifecycle`** (D-35, D-37);
  - for every database it is deployed to, the §2.4.1 runbook result is recorded: fingerprint, classification, action, version and dirty flag afterwards, backup evidence, and operator sign-off. Production follows §2.4.2;
  - AC-107, AC-109 and AC-114 pass against PG, with `agent_ts` present;
  - there is no row DELETE against `container_logs` outside down migrations.
- [ ] **README:** caps, the retention setting with its storage guidance, and gap markers.
- [ ] **Self-healing (rev 3.1):**
  - P0: AC-134, AC-135, AC-136, AC-137, AC-141 and AC-142 pass.
  - P1: AC-140 passes.
  - P2: AC-138, AC-139 and AC-143 pass.
- [ ] **Bounds (rev 3.2):**
  - P0: AC-144..AC-149, AC-151, AC-152 and AC-154 pass. Both compose files carry `logging` for every service. The agent WS read limit is set. The agent honours `Retry-After`.
  - P1: AC-156 and AC-158 pass.
  - P2: AC-150, AC-153, AC-157, AC-160 and the extended AC-139 pass.
  - P3: AC-159 passes.
  - P4: AC-155 and AC-161 pass.
  - WP-2C: AC-162 passes.
  - The README documents the ingest limits, quotas, budget settings, headroom source and log rotation.
- [ ] **Degrade and diagnose (rev 3.3):**
  - P0: AC-163..AC-171 pass, as do AC-147 and AC-149 as amended. Both compose files mount `riot-fsprobe` in the server, and mount `riot-data` only in the database. The pool sets `application_name` and the idle-in-transaction timeout.
  - P1: the history API maps `gap_quota` rows, and `LogView` shows the sampled-history notice (AC-165).
  - The README documents the quota modes, fairness, the D-55 thresholds and their overrides, the probe-path rule for custom layouts, and the §2.4.1 credential handling.

**WP-0**

- [ ] Rebased onto `main`.
- [ ] Every row of §2.5 re-confirmed against `origin/main`, with any new drift recorded.

**Security conditions** (QA verifies C-1..C-16 of the review)

- [ ] **P1:** C-1; C-3 (label cases); C-4 (`/history`, `/summary`, route enumeration); C-11; C-13; SEC-002 item 4.
- [ ] **P2:** C-3 (inspect and masking cases); C-4 (write-time masking, relay); C-6; C-8; C-9; C-12; C-14; C-15; C-16.
- [ ] **P3:** C-2; C-3 (compose and `.env`); C-5; C-6 (merged view); C-10 (stacks); C-16.
- [ ] **P4:** C-7; C-10 (inventory).
- [ ] **P0 (revision 3):**
  - C-21: the window and re-stamp tests, the DEFAULT-partition PG test, and the buffer test;
  - C-22: the fingerprint and renumbered no-op tests, plus the runbook result recorded per database with the operator's sign-off;
  - C-23 and C-28;
  - V-5.
- [ ] **P1 (revision 3):** C-24 item 2 (`TestDUX_SEC024_P1AutoUpdateRefusesContainerWritableWorkdir`).
- [ ] **P2 (revision 3):** C-25, C-26, C-27, C-29; V-1 to V-4.
- [ ] **P3 (revision 3):** C-24 (`TestDUX_SEC024_BindBackedVolumeCountsAsWritable`).
- [ ] **Revision 3.1 addendum (R31.6, R31.7):**
  - P0: C-35, C-37 and C-38, plus the B-34 fix;
  - P1: C-B for B-14 and B-17;
  - P2: C-36, plus C-B for B-16, B-19, B-27 and B-29;
  - WP-2D: inherits C-35;
  - P3: C-B for B-25;
  - P4: C-B for B-12.
- [ ] **Revision 3.2 addendum (R32.5, R32.6):**
  - P0: C-30 (the §2.4.1 body as amended, and the `TestDUX_SEC030_*` tests in §6.2 and AC-163), C-39, C-40 and C-41;
  - WP-2D: inherits C-35 and C-39.
- [ ] **"Verify as designed" items from the review:**
  - nothing stored, including in `server_logs`;
  - `no-store` on reads;
  - the Down argv;
  - no forced removes;
  - no `RemoveVolumes`;
  - no `dangerouslySetInnerHTML`;
  - no agent-supplied value in `src`; `href` only through `safeUrl`;
  - no echo of expected confirmation values.

---

## 16. FRD deviations and Open Question resolutions

### 16.1 Deviations

| # | FRD item | Resolution | Why |
|---|---|---|---|
| 1 | FR-042 (bulk Update one per target) | One `docker_bulk_update` per host, with per-target results | CMD-LIFECYCLE compose-key conflicts and the dependents race (§0.2 item 3; D-19) |
| 2 | FR-094 and FR-095 live step output | Live: step timeline plus progress message. Full step output at terminal | CMD-LIFECYCLE report bounds (§0.2 item 4; D-20) |
| 3 | FR-026 selection in the URL | Also omitted above 4 KiB encoded | Proxy request-line limits (D-23) |
| 4 | FR-001 Docker entry on every page | Follows the existing `docker` feature toggle (default on) | Consistency with existing toggles (D-22) |
| 5 | FR-147 rIOt self-identification | Image heuristic plus same compose project plus `riot.role` label, computed per host. Unlabelled non-Docker deployments are not protected | OQ-01 (D-25, SEC-013) |
| 6 | A-03, A-04 | Retention added (AD-025); identity claims added (AD-022) | Both assumptions are false on `main` |
| 7 | NFR-011 retention "no longer than terminal" | Terminal sessions gain retention, and all audit kinds share one store with per-kind retention: 180 days (`long`: terminal, destructive or privileged commands, stack path changes, device deletes) and 90 days (`short`: reads and other commands). The upgrade deletes terminal rows older than 180 days | Terminal had none (D-21 settled, SEC-009) |
| 8 | FR-084 and NFR-007 bounds | Adds byte caps (512 KiB/s per stream, 2 MiB/s per agent), a 64-viewer server cap, and a 4-hour viewer limit | Line caps alone allow 16 MiB/s per stream at 16 KiB lines (D-21) |
| 9 | FR-101 rates | Computed by the agent and shipped with the counters | The previous sample lives there |
| 10 | AC-100, AC-101 | AD-033 split into automated and recorded-manual parts; axe needs D-24 | No browser harness without new dependencies |
| 11 | FR-078 stream toggle | Client-side over a both-streams buffer, covering history and live together | Download and AC-051 need both streams buffered |
| 12 | FR-011 container states | `removing` added to the fleet projection filter | The FLEET-DASH filter omitted it |
| 13 | FR-122 | AC-106 added | The FRD had no AC |
| 14 | §8.12, full-precision identity | Identity at microsecond precision | The database stores microseconds |
| 15 | FR-121 and §8.8: 25 merged containers against 8 streams per host | History merges up to 25. Live follows only free slots, chosen with a picker | The two bounds conflict (D-38) |
| 16 | FR-149 before partitioning | The setting is parsed. The effective value stays 7 until WP-P0C | NFR-024 |
| 17 | FR-152 exact count | The count becomes a lower bound past the scan cap | Keeps agent work bounded (D-41) |
| 18 | FR-152 byte cap against one long line | The line is truncated to 16 KiB and marked | Otherwise it could never be stored (D-40) |
| 19 | FR-153 per ingest | A rolling per-container limit is added | Bounds agents that push fast |
| 20 | FR-119 and FR-120 on Windows | Unsupported | The no-race open needs Linux (D-39) |
| 21 | FR-009 before WP-P0C | History uses the current container ID | The name index needs the partitioned table |
| 22 | FR-148, NFR-025 against agent-chosen timestamps | Out-of-window lines and gap rows are re-stamped with the receive time and keep the agent time in `agent_ts`. They are never dropped | SEC-021; D-42 settled (re-stamp, not drop) |
| 23 | FR-153 per-container ceiling | Adds a per-device rolling ceiling, a known-container rule with an unknown budget, server-owned `stream` values and validated gap entries | SEC-023 |
| 24 | P0 and P1 phase split of masking | Write-time masking of stored lines moves into P0 (WP-P0B) | SEC-028 |
| 25 | FRD §8.12 identity | Identity, and the boundary sent to the agent, use the agent-domain time (`agent_t` when re-stamped) | Keeps de-duplication working on skewed hosts (D-42) |
| 26 | FR-083, FR-165 socket lifetime | A log socket closes at session expiry, and after 4 h. The client reconnects | SEC-027 |
| 27 | P3 placement of the writable-path refusal | Moved to P1 for `docker_update{compose_work_dir}` and auto-update | SEC-024 item 2; D-44 |
| 28 | FR-153 per-device allowance (ADD-defined in rev 3) | Tightened to 5,000 lines and 2 MiB per 60 s, with a separate gap-row bound | The product owner's self-healing and no-bloat requirement (D-46) |
| 29 | FR-091, FR-097 observed events | Stored at most 60 per container and 600 per device per hour, plus an hourly "N events suppressed" summary | No-bloat requirement (D-47) |
| 30 | FR-042, FR-059 (unlimited actions) | Operator commands limited to a burst of 120 and 60 per hour per device; terminal opens to 30 per minute per device | No-bloat requirement (D-48) |
| 31 | FR-149 retention setting | The effective retention may be shortened by the fleet storage budget or the disk-headroom guard, and is reported as `retention_limited_by` | Standing bounds requirement (D-49, SEC-037) |
| 32 | FR-152 and FR-153 stored volume | A per-device daily stored-bytes quota (512 MiB of logs) applies on top of the ceilings | Standing bounds requirement (D-50) |
| 33 | Pre-existing agent API (no FR) | Ingest rate and body caps: telemetry gets 429 with `Retry-After`; excess heartbeats are not stored | SEC-035 (D-51) |
| 34 | FR-094 command output | Over the daily command-results quota, output is stored as a 1 KiB head-and-tail stub | Standing bounds requirement (D-50) |
| 35 | FR-152 and FR-153 stored volume (D-50 shape) | Quotas refill continuously and degrade by sampling (stderr and error lines first, stdout sampled, a per-container fair share), never by stopping | The product owner's data-loss concern (D-54, SEC-039) |
| 36 | FR-149 retention under the fleet budget | Heavy devices' allowances are lowered before anyone's retention is shortened | Fairness (D-56, SEC-042) |
| 37 | Telemetry history under the quota | Stored at most once per 15 min, trimmed to 2 MiB, instead of stopping, so the latest stored state is at most 15 min old | D-54, SEC-039 |

### 16.2 Open Questions

| OQ | Resolution |
|---|---|
| OQ-01 | AD-009: image contains `riot-server`, plus every container in the same compose project on that host, plus an explicit `riot.role=server|database` label. Self targets are computed per host, from each host's own snapshot (SEC-013). A server running outside Docker, or a database outside the server's project, is protected only when labelled. The README documents this (D-25) |
| OQ-02 | AD-023: the step timeline (name, times, outcome) in `commands.steps` JSONB, at most 64 entries, written with the existing step-change UPDATE. Step output as one `command_output` row (`stream='steps'`, at most 64 KiB) written once at terminal. 30-day DELETE retention on output; 90-day on commands |
| OQ-03 | AD-028: a separate latest-only `docker_inventory` table, extracted from the telemetry payload before `StoreSnapshot`, with unchanged payloads elided, and bounded by the server (SEC-010). Riding snapshots would add tens of KB to 2 MB per host per interval to the highest-churn table (§13.3) |
| OQ-04 | AD-001: no version boundary. Agents advertise capability tokens in `DockerInfo.capabilities`. The minimum version per capability is the first release containing that phase's agent work package, named in the release notes. The server checks tokens, and the UI shows "Requires agent update". Tokens gate UX only and never relax a server bound (SEC-014) |
| OQ-05 | AD-024: yes. `terminal_sessions` is generalised with `kind`, `actor_auth`, `actor_sub`, `actor_name`, `target`, `detail` and `retention_class`. Its retention today is **none** (unbounded). This story sets per-kind retention (180 or 90 days; D-21, settled), never purges open rows, and drops the device cascade so audit rows survive device deletion (SEC-009). The P2 down migration deletes no audit rows (SEC-029) |
| OQ-06 | AD-005: the FRD safe list is adopted with no additions. The FRD sensitive-key list is adopted, widened by the security review (SEC-003) to include `BEARER`, `JWT`, `WEBHOOK` and the other rule 2 terms. The rule data is isolated in `internal/redact/rules.go` |
| **OQ-07** | Gap markers are **marker rows in the same partitioned `container_logs` table**. `stream` is `gap_agent` or `gap_server`, written only by server code (SEC-023). `line` holds the count, with `+` when it is a lower bound. The timestamp is that of the newest dropped line, re-stamped with the receive time when it is out of window (D-42). They rotate with their day partition. The agent sends them in the additive `container_log_gaps` field, so older servers ignore them, and the server validates and bounds them (AD-035). |
| **OQ-08** | The effective retention is a field on the read that needs it. The history API returns `retention_days` and `oldest_retained_at`, from `EffectiveContainerLogRetention()`: the setting once partitioned rotation is live, 7 before that. No separate settings read is added (AD-035, AD-036). |
| **OQ-09** | Each viewer gets **a separate short agent read**, `logs_backfill`: one-shot, at most 5,000 lines, at most 2 at a time per host, within the agent byte budget. The agent keeps no per-stream history buffer. The client holds live lines as pending until `backfill_end`, then merges them by identity (AD-015, AD-017, AD-018). |
| **OQ-10** | AD-015 to AD-018 are rewritten: one agent follow per (device, container), fanned out by the server hub, plus a backfill per viewer. One `LogView` with `useLogView` serves history and Live. `mode` is removed, the separate components are dropped, and Live is never restored from the URL (AD-036, AD-037). |

---

## 17. Decisions: owner and status of every decision

"Working default" means the design is built on that option until the product
owner decides.

### 17.1 Settled

| ID | Decision | Outcome | Owner |
|---|---|---|---|
| S-1 | Fleet-wide section replaces the device Docker page | Settled | FRD §13.1 |
| S-2 / D-18 | Legacy URLs | Redirect. D-18 is retired. | FRD §13.1 |
| S-3 | Stored and live logs | Both, in one tab | FRD §13.1 |
| D-1 | Compose editing | (a) Read-only. rIOt never writes compose or `.env` files | Product owner, revision 3 (owner document: FRD) |
| D-21 (i) | Audit retention | 180 days for the long class (terminal including host shells, destructive or privileged commands, stack path changes, device deletes) and 90 days for the short class. The upgrade deletes terminal rows older than 180 days. Part (ii) stays open (§17.2) | Product owner, revision 3 (owner document: ADD) |
| D-31 | Personal names in public docs | None in this story's docs; "the product owner" or "the operator" is used instead | Product owner, revision 3 (owner document: security review) |
| D-35 | PART-001 not merged when P0 starts | (a): PART-001 is merged as P0's first step | Product owner, revision 3 (owner document: FRD) |
| D-37 | PART-001 merge scope | Whole, with migrations renumbered above `000023_command_lifecycle` | Product owner, revision 3 (owner document: ADD) |
| D-42 | Out-of-window container-log timestamps | Re-stamp with the receive time, keep the agent time, flag it and show it; never drop (AD-035 §1a). This replaces the review's drop recommendation | Product owner, revision 3 (owner document: security review) |
| D-43 | Reconciliation on a deployed database | Only with that database's operator sign-off, after a verified backup and a schema fingerprint (§2.4.1) | Product owner, revision 3 (owner document: security review) |

### 17.2 Open

| ID | Decision | Owner | Status | Working default and note |
|---|---|---|---|---|
| D-1 | Compose editing | FRD | **Settled** (revision 3) | See §17.1 |
| D-2 | Secret values | FRD | Open | (a) safe list, no reveal. Redaction happens on the agent; a reveal would require secrets to leave the host. |
| D-3 | Mask logs | FRD | Open | (a). Masking runs on the agent (live) and on the server. It is idempotent. Now applied at write time to stored logs and command text. |
| D-4 | Container Remove | FRD | Open | (a) out of scope. AD-029 removes only images, volumes and networks. |
| D-5 | Volume removal scope | FRD | Open | (a) named volumes unselected. Enforced through `selected_default` and typed host confirmation. |
| D-6 | Inventory enablement | FRD | Open | (a) opt-in. Cheap to collect but rarely changing; opt-in avoids a disk-usage scan on every host. |
| D-7 | I/O history | FRD | Open | (a) after PART-001. WP-2D is parked until PART-001 merges. |
| D-8 | Selection in the URL | FRD | Open | (a) store at most 200, plus D-23 |
| D-9 | Default view | FRD | Open | (a) Table |
| D-10 | Same project on two hosts | FRD | Open | (a) separate stacks. Primary key `(device_id, project)`. |
| D-11 | Down stack retention | FRD | Open | (a) until forgotten, within the SEC-010 bound. Rows are change-gated and tiny. |
| D-12 | Typed confirmation for bulk Stop | FRD | Open | (a) more than 5 targets or more than 1 host. Also enforced on the server. |
| D-13 | Live log limits (rev 2 values) | FRD | Open | (a): 1,000 lines/s per stream, 8 streams per host, 8 viewers per stream, 64 viewers in total, plus D-21's byte caps |
| D-14 | Step output retention | FRD | Open | (a) 30 days |
| D-15 | Observed events | FRD | Open | (a), with inferred attributions labelled. Reuses `events`. Attribution by agent, plus server fallback. |
| D-16 | Primary action for a healthy container | FRD | Open | (a) none |
| D-17 | Palette scope | FRD | Open | (a) app-wide |
| D-19 | Bulk Update grouping | ADD | Open | One command per host with per-target results, instead of one per target. Avoids spurious "Already in progress" inside a stack and the dependents race. Start, Stop and Restart stay one per target. |
| D-20 | Command retention and live step output | ADD | Open | Commands 90 days (matches events); step output fully visible when the command finishes, while the live view shows step and progress message. Adds the first retention on `commands`; older history disappears from Fleet's Activity log too. |
| D-21 (ii) | Extra live-log bounds | ADD | Open | 512 KiB/s per stream, 2 MiB/s per agent (backfill limited to 1 MiB/s of it, SEC-026), 64 viewers server-wide, 4 h per viewer and per socket. Part (i), audit retention, is settled (§17.1) |
| D-22 | Docker entry follows the toggle | ADD | Open | Yes: hide the Docker entry when the existing `docker` toggle is off (default on). Consistent with how other toggles hide sections. |
| D-23 | Length cap on URL selection | ADD | Open | Also omit above 4 KiB. Prevents 414 errors on reload behind proxies. |
| D-24 | `axe-core` devDependency | ADD | Open (endorsed by the review) | Approve (dev-only, no runtime or bundle impact). Without it, AC-101's scan is a manual recorded step per page. |
| D-25 | Self-protection scope | ADD | Open | Heuristic plus `riot.role` label, computed per host. Operators running the server outside Docker must label the database container to get the FR-147 warning. |
| D-26 | No scrub of stored labels | ADD | Open (the review agrees, on condition of SEC-004; met in AD-006) | No scrub migration. Rows age out with snapshot retention (30 days); every output path redacts. A one-time scrub would rewrite every `telemetry_snapshots` row: the exact bloat pattern PART-001 fixes. |
| D-27 | Compose `environment:` rule | ADD, **resolved by the review** | Resolved. The product owner may revert only by accepting the leak class described in SEC-003. | Rule 1 |
| D-28 | Phase order | ADD | Open | Keep P2 before P3. If the product owner wants stacks sooner, P3 can ship first without FR-121 and FR-122 (§4.2). |
| D-29 | Compose files writable by containers | Security review | Open | (a) refuse, except under `docker.compose_trusted_roots`. This applies from P1 for `docker_update{compose_work_dir}` and auto-update (D-44), and from P3 for every stack action |
| D-30 | Scope of the origin control | Security review | Open | The ADD implements (a). The review recommends (b): one router line. |
| D-31 | Personal names in public docs | Security review | **Settled** (revision 3) | See §17.1 |
| D-33 | Per-container stored-log cap | FRD | Open | (a) 50 lines and 16 KiB |
| D-34 | Container-log retention setting | FRD | Open | (a) default 7, range 1..90 |
| D-35 | PART-001 not merged when P0 starts | FRD | **Settled** (revision 3) | See §17.1. PART-001 renumbers above `000023_command_lifecycle` (§2.4) |
| D-36 | Several live viewers | FRD | Open | (a) fan-out |
| D-37 | PART-001 merge scope for P0 | ADD rev 2 | **Settled** (revision 3) | See §17.1. Merged whole, renumbered, under its own ADD and conditions, after the §2.4.1 runbook (SEC-022) |
| **D-38 (new)** | Merged stack Live against 8 streams per host | ADD rev 2 | Open | **Keep 8 per host**, with a picker. Raising it to 25 for stack views costs up to 3 times the agent reads. |
| **D-39 (new)** | Compose and `.env` reads on Windows agents | ADD rev 2 | Open | **Unsupported**, fail closed. Stack actions are unaffected. |
| **D-40 (new)** | Stored lines longer than 16 KiB | ADD rev 2 | Open | **Truncate and mark.** Dropping them would lose the start of stack traces. |
| **D-41 (new)** | Exact gap counts for runaway containers | ADD rev 2 | Open | **Lower bound** past 20,000 lines or 8 MiB per collection |
| **D-44 (new)** | When the agent's writable-path refusal starts to apply to stack Update and auto-update | ADD rev 3 | Open | **From P1** (WP-1A agent; SEC-024 item 2, which the review strongly recommends). Some stacks have compose files under a directory that a container mounts read-write, which is typical of in-container stack managers. Those stacks stop updating, manually and automatically, until the operator adds the stack root to `docker.compose_trusted_roots`. `riot-agent doctor` and the P1 release notes list the affected stacks. The alternative is waiting for P3, which leaves the pre-existing auto-update hijack open until then |
| **D-46 (new)** | Per-device stored-log ceiling | ADD rev 3.1 | Open | **5,000 lines and 2 MiB per minute per host** (was 20,000 and 5 MiB). The default agent caps fit under it for up to 100 containers per host. Beyond that, `gap_server` markers appear. It cuts the hostile worst case from ≈ 12 GiB to ≈ 4.2 GiB per host per day. The alternative keeps rev 3's figures, with higher ceilings and about 3 times the worst-case storage |
| **D-47 (new)** | Observed Docker events stored per hour | ADD rev 3.1 | Open | **60 per container and 600 per device per hour**, plus one hourly "N events suppressed" summary. Alert notifications keep their own rule cooldowns. A crash-looping container then shows a summary in Activity instead of thousands of rows. The alternative is unthrottled storage (as today), which a crash loop or a compromised agent can grow without limit until the 90-day purge |
| **D-48 (new)** | Rate limits on operator-created commands and terminal opens | ADD rev 3.1 | Open | **Commands: burst 120, then 60 per hour per device** (operator-initiated only; system-origin commands are bounded by their policies). **Terminal opens: 30 per minute per device.** A bulk action of 100 targets fits within the burst. Over the limit, the request gets `429 rate_limited`. The alternative is no limit, where a looping script can grow `commands`, `command_output` and audit rows without bound until their purges |
| **D-49 (new)** | Fleet storage budget for container logs and command results, with self-healing eviction, plus a disk-headroom warning and emergency eviction | ADD rev 3.2 (security review rev 3.1, R31.4) | **Adopted by default under the standing bounds requirement; product owner may tune values** | `RIOT_CONTAINER_LOG_MAX_GB` 16 plus `RIOT_COMMAND_RESULTS_MAX_GB` 4 (20 GiB), each capped at 50 % of (usage + free DB disk) when free space is known. **Rev 3.3:** the container-log budget is a soft limit that first triggers D-56 fairness; early day drops happen only above 110 % or under disk pressure. The effective retention is reported (`retention_limited_by`), one `log_budget_eviction` event fires, and recovery is automatic. The headroom thresholds are D-55's, and the emergency order ends in minimal mode, never zero storage. **Trade-off:** on a small disk, container-log retention may be shorter than the setting; the UI says so |
| **D-50 (new)** | Per-device daily stored-bytes quotas | ADD rev 3.2 (R31.4) | **Adopted by default under the standing bounds requirement; product owner may tune values** | Daily totals: logs 512 MiB, command results 64 MiB, telemetry 1 GiB, device logs 256 MiB. **Rev 3.3:** the shape is D-54's (continuous refill, degrade instead of stop), and the midnight reset is gone. One event per class per transition. **Trade-off:** an unusually chatty but honest host (for example 50 containers, each logging 16 KiB every minute) passes 75 % of its allowance after a few hours. From then on its plain stdout is sampled, while stderr and error lines are kept |
| **D-51 (new)** | Per-device agent ingest rate and body caps | ADD rev 3.2 (SEC-035) | **Adopted by default under the standing bounds requirement; product owner may tune values** | Telemetry: 1 per 15 s, burst 120, body 25 MiB, **429 with `Retry-After`** (the agent buffers). Heartbeat: 1 per 10 s, burst 6, body 64 KiB; excess gets **200, stored nothing**. Events: 120 per minute, 1 MiB, 429. Device logs: 6 per minute, 8 MiB. Probe results: 6 per minute, 64 per push. Agent WS: 4 MiB per frame, 200 frames per second. **Trade-off:** an agent configured with `poll_interval` below 15 s is throttled to one stored snapshot per 15 s |
| **D-52 (new)** | Superseded images after rIOt-initiated updates | ADD rev 3.2 (R31.2 item 5) | **Adopted by default under the standing bounds requirement (feature available, off by default); product owner may tune values** | **P4**, opt-in through host-local `docker.prune_superseded_images: false`. After a successful update or redeploy, only the superseded image ID is removed, if it is unused and untagged, through `ImageRemove(Force:false)`; never a `*Prune` API (AD-029). It is P4 because it reuses WP-4A's per-item removal and identity checks; P1 would duplicate them. On by default was rejected, because keeping the previous image allows a fast manual rollback |
| **D-53 (new)** | Partition drop method on `container_logs` while PART-001 keeps a DEFAULT partition | ADD rev 3.2 (SEC-037 item 3) | Open; **working default endorsed by the security review** (rev 3.2 addendum), conditional on SEC-041, which rev 3.3 meets | **Keep the DEFAULT partition** (defence in depth, SEC-021 item 4). Drops use `DROP TABLE` with `lock_timeout` 1 s and stop at the first timeout, so ingest stalls for at most about 1 s per hourly run. PostgreSQL refuses `DETACH PARTITION … CONCURRENTLY` while a DEFAULT partition exists. The alternative is to remove the empty DEFAULT partition, which allows `DETACH … CONCURRENTLY` but makes one unroutable row fail a whole `COPY` batch. It is revisited only if lock-timeout stale events are observed in practice. The janitor implements both methods and chooses at run time |
| **D-54 (new)** | Shape of the daily quotas | ADD rev 3.3 (SEC-039) | **Adopted by default (the product owner's data-loss concern and the standing bounds requirement); product owner may tune values** | A continuous token-bucket refill (capacity 25 % of the daily quota, refill of the quota per 24 h) replaces the midnight reset. Past 75 % used: stderr and error-pattern lines go first; other stdout is sampled to 5 per container per minute; each container gets a max-min fair share; `gap_quota` rows record what was sampled away; and the Logs tab says so. Telemetry is downsampled to one snapshot per 15 min (trimmed to 2 MiB) instead of stopped, so device state and the fleet table are never more than 15 min stale. **Cost:** any 24 h window can store 1.25 × the daily quota, and telemetry adds up to 96 trimmed snapshots per day. The per-device worst case rises from 1.98 to 2.62 GiB per day |
| **D-55 (new)** | Disk-headroom thresholds and emergency behaviour | ADD rev 3.3 (SEC-039 item 3) | **Adopted by default; product owner may tune values** | Absolute first: low below max(4 GiB, min(15 %, 50 GiB)); emergency below max(1 GiB, min(5 %, 10 GiB)); critical below 512 MiB. `RIOT_DB_FREE_LOW_GB` and `RIOT_DB_FREE_EMERGENCY_GB` override them. The emergency evicts oldest first, and never stops accepting all new data: minimal mode keeps priority log lines, stdout sampled to 1 per container per minute, and one snapshot per device per 15 min. **The minimal recent window** is today's partitions, each device's newest snapshot, and its priority lines since the emergency began. A large, mostly full shared disk (for example 8 TB at 96 % used) stays in normal mode |
| **D-56 (new)** | Per-device fairness when the fleet budget binds | ADD rev 3.3 (SEC-042) | **Adopted by default; product owner may tune values** | Above the budget (the soft limit), devices above both the fair share (0.9 × budget ÷ (days kept × active devices)) and the fleet median get a lowered `logs` allowance (at least 32 MiB per day). The heavy host is sampled, instead of every host losing days. Early drops happen only above 110 % of the budget or under disk pressure. Reported per device (`storage_quota.fairness`, `log_fairness_limited`). **Trade-off:** the budget can be exceeded by up to 10 % while fairness takes effect |

D-32 is unused.

---

## 18. Risks, open questions, and notes for the caller

**Open risks**

| ID | Risk |
|---|---|
| R-1 | Further drift on `main` after this revision. WP-0 re-confirms §2.5. |
| R-2 | AD-007, AD-008 and AD-023 extend CMD-LIFECYCLE's merged contracts additively: new `DispatchRequest` fields, the lock rule, masking in `sanitizeReport`. The CMD-LIFECYCLE owners should acknowledge them. |
| R-3 | **Migration numbering.** `main` has `000023_command_lifecycle`. PART-001's branch numbers `000023`..`000028` collide, and are renumbered when it lands (D-37). **This is resolved for the production deployment:** it moved 22 → 23, not dirty, on the v2.67.0 release, so `000023_command_lifecycle` ran (§2.4.2). Other operators' databases that ever ran a branch build are classified and reconciled only through the §2.4.1 runbook: verified backup, schema fingerprints, stop on dirty, never drop data, and per-database sign-off (D-43). |
| R-4 | `events` churn from crash-looping containers. Pre-existing. |
| R-5 | Masking is best effort. Obfuscated secrets can remain; this is documented. |
| R-6 | `.env` files at `0600 root` fail both reads and `up`. |
| R-7 | Raised retention multiplies stored-log volume (§13.3). |
| R-8 | The server-ceiling worst case (about 2.8 GiB of line data plus 1.3 GiB of row overhead per host per day, §13.3 and §13A B-01) applies to legacy, misconfigured or hostile agents until they are upgraded or removed. |
| R-9 | Duplicates at the boundary when agent and server mask with different rule versions. |
| R-10 | Tools that manage stacks from inside a container need `compose_trusted_roots`. |
| R-11 | Before WP-P0C, history does not span recreates. |
| R-12 | Under D-30 (a), pre-existing routes stay exposed to same-site forgery. |
| R-13 | Label redaction visibility. After P1 ships, labels whose keys match rule 2 (for example `PASS`, a `KEY` segment or `AUTH`) show as `<redacted>` on Overview. That is intended, but visible. |
| R-14 | Backfill pacing. A 5,000-line backfill runs within the agent byte budget and can take a few seconds. This is accepted to protect NFR-007. |
| R-15 | Hosts with skewed clocks (D-42). Re-stamped lines appear at their arrival time. A backlog flushed after a long offline period therefore appears as one block at arrival time, flagged with each line's original time. On such a host, the history/live boundary may fall back to the last 100 lines. |
| R-16 | From P1, stacks whose compose files a container can write (in-container stack managers) stop updating, manually and automatically, until their root is in `docker.compose_trusted_roots` (D-44). |
| R-17 | `/ws` and `/ws/terminal` still outlive the session they were opened with. This is pre-existing (SEC-027 note); only `/ws/docker/logs` is fixed here. |
| R-18 | The per-device skew and ingest counters are held in memory and reset when the server restarts. The `clock_skew` event persists. |
| R-19 | **Revised in rev 3.2.** Every path is bounded (§13A). The retained worst case for one hostile device, at the default retentions, is now about 17 GiB on this story's paths (rev 3.1: 72 GiB), and about 55 GiB including the pre-existing telemetry and device-log paths. Fleet-wide, container logs and command results stay within 20 GiB (D-49). Everything else is held below 95 % of the DB volume by the headroom eviction, as long as the irreducible floor (20 GiB + N × 2.8 GiB) fits. No operator action is needed. Removing the device's key remains the way to stop it entirely. The steady state for a 25-device fleet is about 8 GB. |
| R-20 | The PART-001 manager extension (AD-035 §1b) depends on PART-001's code once merged. Its function names are confirmed at WP-P0C, and any drift is recorded. |
| R-21 | The emergency eviction (AD-041 §3) drops partitions of PART-001's other tables (for example telemetry snapshots, container metrics, device logs and heartbeats), down to a 2-day floor. This is a cross-story extension of PART-001's manager. Its API is confirmed at WP-P0C. |
| R-22 | **Revised in rev 3.3.** D-50 quotas no longer pause history. On honest but unusual hosts (very chatty containers, or a short `poll_interval` with large snapshots), they sample stdout or downsample telemetry instead, keeping stderr and error lines first and the latest state at most 15 min old. The event and `storage_quota` say so, and the values are tunable. |
| R-23 | Container-log retention may be shorter than the setting when the D-49 budget or the headroom guard binds. It is shown as `retention_limited_by`. |
| R-24 | `command_output` and `commands` rows written before P2 count as 0 bytes toward the command-results budget until they age out (30 and 90 days). |
| R-25 | Agents older than WP-P0A clear their buffer only after a whole flush. After a 429 halfway through a flush, they resend snapshots that were already stored. The duplicates are bounded by D-51 and D-50, and stop once the agent is upgraded. |
| R-26 | Without `RIOT_DB_FS_PROBE_PATH` (on the database's filesystem) or `RIOT_DB_MAX_GB` (custom deployments), the disk-headroom guard is off. Only the budgets and the per-device quotas bound the volume. A probe path on the wrong filesystem is detected only when the database is larger than that filesystem's used space (AD-041 §3). |
| R-27 | The server image runs as root: the `Dockerfile` sets no `USER`. Rev 3.3 removes the database-volume mount that made this dangerous (SEC-040), but running the server as a non-root user is a recommended follow-up, outside this story's scope. |
| R-28 | Under D-56, container-log usage can exceed the budget by up to 10 % while lowered allowances take effect. |
| R-29 | Sampled stored history (D-54) is incomplete by design. Live shows every line, and gap rows record what was sampled away. |
| R-30 | D-54 raises the per-device worst case to about 2.6 GiB per day (bucket bursts plus trimmed telemetry). That is the price of never stopping storage. |
| R-31 | Emergency cancellation (SEC-041) relies on `application_name`. A process that impersonates `riot-server` under the same role could be cancelled. That only affects its own running statement. |

**Open questions**

- The security review's revision 2 addendum covered the revision 2 amendments. A short delta check should confirm the revision 3 changes:
  - AD-035 §1a and §5;
  - §2.4.1;
  - the AD-016 and AD-017 stream lifecycle;
  - AD-024 retention;
  - AD-038 §3;
  - in particular the D-42 re-stamp, which replaces the review's drop recommendation.
- A delta check of revision 3.2 by the security reviewer. It should cover AD-040, AD-041, AD-035 §1b rules 1, 2 and 4, D-53, and §13A B-38..B-48.
- A final delta check of revision 3.3 before the docs freeze. It should cover AD-041 §1 to §3 as reshaped (D-54, D-55, D-56, SEC-040, SEC-041), the §2.4.1 body (SEC-030), and §13A B-01, B-38, B-40, B-44, B-45 and B-49..B-51.

**Notes for the caller**

- No code was written. Only this ADD changed.
- §2.5 was spot-checked against the local main checkout, which contains `000023_command_lifecycle`. WP-0 confirms it against `origin/main`.

---

## 19. Security findings mapped to ADs

| Finding | Severity | Gates | Answered by | Design answer |
|---|---|---|---|---|
| SEC-001 | HIGH | P1 and all later routes | **AD-034**, AD-009, AD-019 | Same-origin JSON on state changes; header plus origin check on side-effecting GETs; frame-ancestors none and `X-Frame-Options: DENY`; typed confirmations reclassified as mistake prevention |
| SEC-002 | HIGH | P3 (item 4 in P1) | **AD-038**, AD-021, AD-026, AD-027 | Agreement or conflict; acknowledged path changes; no execution under read-write bind-mount sources unless under host-local trusted roots; auto-update uses the agreed record; paths shown |
| SEC-003 | HIGH | P1, P2, P3 | **AD-005**, AD-020, AD-021 | URL-credential scanner; camelCase, suffix and abbreviation key matching; masking net on documents; wider argv and healthcheck rules; compose env rule 1; dotenv fails closed; residual-risk notice |
| SEC-004 | MEDIUM | P1, P2 | **AD-006**, AD-023, AD-016, AD-035 | Every output path enumerated (including `/history` and `/summary`) with a route-walk test; write-time masking of command text and stored lines; relay always masks |
| SEC-005 | MEDIUM | P3 | **AD-021** | Outer `wd` bound; deny list including `/etc`; component-wise `Openat` with `O_NOFOLLOW`, plus `O_NONBLOCK` and `Fstat` on the open file; paths shown |
| SEC-006 | MEDIUM | P2, P3 | **AD-017**, AD-016, AD-036, AD-037 | Bounded reader; drop before mask; server per-frame and rate re-checks; 32 MiB browser bound; audit coalescing |
| SEC-007 | MEDIUM | P4 | **AD-029** | 128-bit CSPRNG preview bound to device, kind, items, actor and expiry, consumed on any outcome; tag-to-ID check; volume `CreatedAt` check |
| SEC-008 | MEDIUM | P2 | **AD-022**, AD-024 | Auth method, stable subject and display name persisted; legacy sessions recorded as unknown; filters by subject |
| SEC-009 | MEDIUM | P2 | **AD-024**, AD-025 | Per-kind retention of 180 or 90 days (D-21, settled); no FK cascade; device deletion audited; open sessions kept; residual gaps closed under SEC-029 |
| SEC-010 | MEDIUM | P3, P4 | **AD-026**, AD-028, AD-023, AD-016, AD-035 | Stack row and field bounds with eviction; inventory per-item bounds, size cap and cadence; `command_id` validation; audit coalescing; rolling stored-log limit |
| SEC-011 | LOW (mandatory) | P1 | **AD-011**, AD-003 | Only `http`/`https`; IPs parsed; ports checked; `primary_ip` parsed at ingest; destination host shown |
| SEC-012 | LOW (mandatory) | P2 | **AD-016** | `Origin` required on `/ws/docker/logs` |
| SEC-013 | LOW | P1 | **AD-009** | Self targets per host; spoofing can only add targets |
| SEC-014 | LOW (mandatory) | P2 | **AD-001**, AD-016 | Tokens gate UX only; tested |
| SEC-015 | LOW | P2 | **AD-023** | Validated `command_id`; inferred attribution labelled |
| SEC-016 | LOW (mandatory) | P2, P3 | **AD-017**, §14.1 | Agent never logs content; capture test |
| SEC-017 | LOW | none | **AD-002**, AD-011 | Encoded path segments; non-matching names not linked |
| SEC-018 | INFO | none | D-31 (settled) | No personal names in this story's docs |
| SEC-019 | INFO | none | D-1 (settled) | Read-only compose; rIOt never writes compose or `.env` files |
| SEC-020 | INFO | none | §12.9 | Privilege statement confirmed |
| SEC-021 | HIGH | P0 | **AD-035 §1a**, AD-036, AD-016, WP-P0C | Ingest window `[received − retention, received + 5 min]`. Out-of-window rows are re-stamped with the receive time, with the agent time kept in `agent_ts` (D-42: re-stamp, not drop). Live lines are treated the same. History never returns rows after now + 5 min. The buffer never adopts a future `newestTs`. The DEFAULT-partition event is kept |
| SEC-022 | HIGH | P0 (the WP-P0C merge, any PART-001 deploy, any `force`) | **§2.4.1**, §2.4.2, R-3 | Verified `pg_dump` first; classification by schema fingerprint; stop on dirty; never drop data; a no-op test for the renumbered PART-001 files; per-database sign-off (D-43) |
| SEC-023 | MEDIUM | P0 | **AD-035 §5**, §7.2 | Server-owned `stream`; validated and bounded gap entries; a known-container rule with an unknown budget; a per-device rolling ceiling; masking after the ceiling; no CHECK constraint (reason stated) |
| SEC-024 | MEDIUM | P1 (item 2), P3 (item 1) | **AD-038** §1 and §3, WP-1A | Bind-backed named volumes and host-path volume drivers count as writable sources. The agent's writable-path refusal applies to `docker_update{compose_work_dir}` from P1. The P1 auto-update needs a member created by compose (D-44) |
| SEC-025 | LOW (mandatory) | P2 | **AD-016**, AD-015, AD-017 | Linger and attach serialized; retired stream IDs never reused; frames for unknown streams trigger `logs_unfollow`; `logs_keep` every 60 s; agent reaps orphaned follows after 5 min |
| SEC-026 | LOW (mandatory) | P2 | **AD-016**, AD-017 | At most one backfill per 10 s per (socket, container), coalesced per (device, container, boundary); its own 1 MiB/s share of the agent budget; boundary validated |
| SEC-027 | LOW (mandatory) | P2 | **AD-016**, §8.10 | Socket closed at the JWT `exp` with `session_expired`; attach refused after that; socket lifetime capped at 4 h |
| SEC-028 | LOW (mandatory) | P0 | **AD-035 §6**, WP-P0B | Write-time masking moved into WP-P0B. WP-P0A may not ship without it |
| SEC-029 | LOW | P2 | **AD-024**, §7.2 | A `command` audit row for every operator command in the long-retention list, Docker or not. The P2 down migration re-adds the FK as `NOT VALID` and deletes no audit rows |
| SEC-030 | LOW (mandatory) | P0 (WP-P0C; before the subcommand or the runbook is used on any deployed database) | **§2.4.1** (body amended in rev 3.3), §6.2 (`cmd/riot-server/main.go`, `db/fingerprint.go`), AC-163, D-43 | **Rev 3.3: these rules are now written into the §2.4.1 body, not only here.** A new `riot-server migrate` subcommand runs the upgrade without serving, so the server stays stopped from the backup through step 5; the dump is written with `umask 077` and deleted or moved to the backup store after step 5; a read-only role is recommended for steps 2 and 3; and `TestDUX_SEC030_MigrateSubcommandDoesNotServe` joins the tests. `db-fingerprint` is dispatched in `main` before `server.New`, never instantiates golang-migrate, and reads with plain SQL in a `READ ONLY` transaction with `default_transaction_read_only=on`. Output is limited to version, dirty, the fingerprint booleans, classification and action class; it never prints connection details, and errors are reduced to a fixed message (non-zero exit on every "stop"). Dumps are `0600`, outside any repository, with the password from `PGPASSFILE` or the environment, and handled as a credential. The server is stopped from backup to step 5. The step 4 table adds F-LC + F-PT + F-AV and F-LC + F-AV rows; the no-op test also asserts `partition_conversion` is unchanged (C-30) |
| SEC-031 | LOW (mandatory) | P1 (history buffer and Download), P2 (live) | **AD-036** | The Download emits both times and a textual skew marker for re-stamped lines, gap markers as text lines, and a header line. The buffer's future bound uses a server-supplied `server_now` (history response and WS session message) plus elapsed monotonic time, never the browser clock (C-31: `logDownload.test.ts`, `logBuffer.test.ts`) |
| SEC-032 | INFO | none | **AD-035 §1a**, R-18 | Stored log times are agent-asserted and not tamper-evident; the Logs tab notice or the README says so. A per-row server receive-time column is noted for awareness only |
| SEC-033 | LOW (mandatory) | P2 | **AD-024**, AD-025, D-21, D-45 | The P2 release notes and upgrade steps warn, before the upgrade, that the first purge irreversibly deletes `terminal` audit rows older than 180 days, and give a documented export step to a `0600` file outside any repository. The first purge logs the deleted row count, with no content (C-33: `TestDUX_SEC033_FirstPurgeLogsDeletedCount`) |
| SEC-034 | LOW (recommended) | none | **AD-024** | Recommended: a long-class audit row (kind `command`, action `policy_change`, or a new kind) for each auto-update and auto-patch policy change (`PUT …/auto-updates`, `DELETE …/auto-updates/{target}`, `PUT …/auto-patch`) |
| SEC-035 | MEDIUM | P0 (ADD amendment; this revision) | **AD-040**, AD-041, §13A B-34, B-38..B-42 | Body caps before decoding on every agent endpoint. Per-device buckets keyed after `DeviceAuth`: telemetry 1 per 15 s, burst 120, 429 with `Retry-After`; heartbeats 1 per 10 s, excess gets 200 and is not stored; events 120 per minute; device logs and probe results 6 per minute. Agent WS read limit 4 MiB, plus a frame bucket. D-50 telemetry and device-log quotas. The agent honours `Retry-After` |
| SEC-036 | LOW (mandatory) | P2 | **AD-016**, AD-017, AD-040, §13A B-19, B-29, B-43 | Retired IDs: an LRU of 256 per device, 10 min TTL. Unknown-stream unfollow through a per-device bucket, with no per-ID state. Event-throttle counters: an LRU of 512 per device. Event-bus subscribers capped at 10, and unsubscribed on cancel |
| SEC-037 | MEDIUM | P0 (ADD amendment; this revision) | **AD-041**, AD-035 §1b, AD-025, §13A | Fleet budget eviction and disk headroom (D-49). Adaptive purge batches, with output deleted before commands. Drops by `DETACH … CONCURRENTLY` when no DEFAULT partition exists; otherwise `DROP` with `lock_timeout` 1 s, 20 s per run, stopping at the first timeout (D-53). Startup ensure over `[today − retention, today + 3]`. Server-wide singleflight. AC-135 asserts the yesterday fallback. Full-disk statements corrected |
| SEC-038 | LOW (mandatory) | P0 release | **AD-040**, §13A B-46 | `json-file` 10m × 5 for the server and the DB in both compose files; README for custom deployments; agent-ingest request lines summarized per device every 10 min; static compose test |
| R31.1 weak rows (C-B) | conditions | P1..P4 | §13A B-12, B-14, B-16, B-17, B-19, B-25, B-27, B-29 | The review's exact fixes: named tests, constants, LRU caps, a server-enforced minimum cooldown, and buckets for validated devices only (AC-139, AC-153, AC-155..AC-160) |
| SEC-039 | MEDIUM | P0 (ADD amendment; this revision) | **AD-041 §1, §3**, AD-035 §5, AD-036, D-54, D-55 | Quotas are continuously refilling buckets that degrade: stderr and error lines first, stdout sampled, a per-container fair share, `gap_quota` rows and a Logs-tab notice. Telemetry is downsampled to one trimmed snapshot per 15 min, so the latest state is at most 15 min old. Headroom thresholds are absolute first, and the emergency ends in minimal mode, never zero storage (AC-147, AC-149, AC-164..AC-168) |
| SEC-040 | LOW (mandatory) | P0 (WP-P0C) | **AD-041 §3**, §6.1 compose rows | The database volume is no longer mounted into the server. An empty `riot-fsprobe` volume on the same filesystem serves `statfs`, with a same-filesystem sanity check. The false "cannot read" statement is removed. A non-root server is recommended as a follow-up (R-27) (AC-170) |
| SEC-041 | LOW (mandatory) | P0 | **AD-041 §3**, AD-035 §1b, §14.1 item 37 | Pool sessions set `application_name=riot-server` and a 60 s idle-in-transaction timeout; reads use a 30 s statement timeout. Drop timeouts report the blockers (PID, application, state, transaction age; no query text). In emergency, only the server's own active blockers are cancelled (AC-171) |
| SEC-042 | LOW (recommended) | P0 (WP-P0C) | **AD-041 §2**, D-56 | Fairness first when the budget binds: heavy devices' allowances are lowered to the fair share. Early drops happen only above 110 % or under disk pressure. Reported per device (AC-169) |

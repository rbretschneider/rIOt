# QA Report

**Story ID:** PART-001
**Title:** Time-Range Partitioning with Drop-Partition Retention for High-Churn Tables
**QA Engineer:** QA Agent
**Date:** 2026-09-10
**Verdict:** PASS WITH NOTES

---

## Scope note

This story converts six live, high-churn retention tables on a ~34 GB production
database to `PARTITION BY RANGE`, automatically, on server startup, via a
five-file migration. Per the task mandate, data-integrity, migration-safety, and
availability are weighted at least as heavily as classic test-coverage concerns.
I ran every gate myself against a **live PostgreSQL 16** container (not just read
the tests) and independently re-derived the correctness arguments for the two
riskiest guarantees (NFR-001 no-rewrite, NFR-002 inserts-never-fail) against the
actual SQL and Go source, not just the ADD's narrative.

---

## Environment

- **Host:** Windows 11, Go 1.24, `go test` run without `-race` (CGO not
  available on this dev host — matches the project's documented convention).
- **Docker:** available and used. I stood up a throwaway `postgres:16-alpine`
  container (`riot-qa-pg`, port 55433) independently of the engineer's own
  verification, ran the full `-tags=integration` suite against it, and tore it
  down afterward (`docker rm -f riot-qa-pg`) — no leftover infrastructure.
- **Integration suite: executed, not just inspected.** I did not rely on the
  implementation report's claim that it was run — I reran it myself end-to-end.
- **Working tree:** clean at the start; I added one QA test to
  `internal/server/partition/manager_test.go` (see "Tests Added by QA" below,
  attributed and intentionally red to demonstrate a real gap). No implementation
  code was modified.

---

## Gate Output (all run by me, this session)

| Command | Result |
|---|---|
| `go build ./...` | exit 0 — clean |
| `go vet ./...` | exit 0 — clean |
| `go test ./... -count=1` (no DB, no tags) | **exit 0, all green**, before I added the QA test below. Confirms the default suite genuinely requires no Postgres — I ran it with no DB reachable at all. |
| `cd web && npm run test:run` | exit 0 — **26 files, 359 tests passed** (matches impl report exactly) |
| `cd web && npx tsc --noEmit` | exit 0 — clean |
| `go test -tags=integration -count=1 -v ./internal/server/partition/...` against a **live throwaway `postgres:16-alpine`** | **exit 0 — 53 `--- PASS`, 0 `--- FAIL`, 0 `--- SKIP`** (matches impl report exactly) |
| Same integration command with **no DB configured** (`RIOT_TEST_DB_URL`/`RIOT_DB_URL` unset) | **All 21 integration test functions skip cleanly via `t.Skip`, exit 0.** Confirmed explicitly — the default `go test ./...` gate can never be broken by a missing Postgres. |
| CI (`test-go-integration` job, `.github/workflows/ci.yml`) | **Inspected and confirmed present**: spins up a real `postgres:16-alpine` service, sets `RIOT_TEST_DB_URL`, runs `go test -tags=integration -count=1 -v ./internal/server/partition/...` as its own job (not folded into/short-circuited by `test-go`). A failure here fails the job. This is residual **R1**, verified genuinely implemented, not merely claimed. |

**After I added the QA-authored red test** (see below), `go test ./...`
(default, no tags) now reports one intentional failure in
`internal/server/partition`:
`Test_AC020_QA_stall_escalation_is_masked_by_an_interleaved_operation_succeeding`.
This is a deliberate, attributed, documented-red test demonstrating a real
implementation gap (see Test Quality Findings / Adversarial Findings below), not
a regression. Every other test in the suite remains green. I confirmed this by
running the suite both with and without the QA test present.

---

## AC Coverage Audit (AC-001..AC-022)

All 22 ACs have a named test whose name carries the `[AC-xxx]`/`AC-xxx`
reference, per the project's mandatory naming convention, and I ran every
integration-tagged test against a real database rather than trusting the name.

| AC ID | Strategy | Test(s) | Status |
|---|---|---|---|
| AC-001 | Integration + Unit | `TestIntegration_AC001_AC002_TablesArePartitionedOnCorrectKey`; `Test_AC001_BuildSpecs_other_five_tables_key_on_timestamp` | ✅ COVERED |
| AC-002 | Integration + Unit | same + `Test_AC002_BuildSpecs_probe_results_keys_on_created_at` | ✅ COVERED |
| AC-003 | Integration | `TestIntegration_AC003_CompositePrimaryKeyIncludesPartitionKey` — asserts PK columns = `{id,<key>}` and two inserts yield distinct `id`s | ✅ COVERED |
| AC-004 | Integration | `TestIntegration_AC004_AllPreExistingIndexesPreserved` — all 9 FR-003 indexes present on the parent, incl. `idx_device_logs_priority`/`idx_device_logs_dedup` | ✅ COVERED |
| AC-005 | Integration | `TestIntegration_AC005_ForeignKeysPreservedContainerLogsFKFree` | ✅ COVERED |
| AC-006 | Integration (hard gate) | `TestIntegration_AC006_AC016_AC019_SwapIsMetadataOnly_NoRewrite` (tiny + 5,000-row fixture) — see NFR-001 deep-dive below | ✅ COVERED |
| AC-007 | Integration | `TestIntegration_AC007_ConversionIsIdempotentOnReRun`, `TestIntegration_AC007_AC018_FullUpDownUpCycle_AllSixTables` | ✅ COVERED |
| AC-008 | Integration | `TestIntegration_AC008_FreshEmptyDatabase_ConvertsCleanly`; also real-binary boot verified by the engineer (spot-checked, see below) | ✅ COVERED |
| AC-009 | Integration | `TestIntegration_AC009_ExistingQueriesContinueToWork` — plain `INSERT`, `RETURNING id`, `CopyFrom`, staged `INSERT…SELECT…ON CONFLICT` dedup all exercised for real | ✅ COVERED |
| AC-010 | Unit + Integration | `Test_AC010_*` (7 tests: naming, UTC-day floor, near-midnight boundary, window math); integration coverage folded into AC-008/AC-016 | ✅ COVERED |
| AC-011 | Unit | `Test_AC011_PartitionsToDrop_*` — exact-boundary droppable, 1-second-within-window not droppable, DEFAULT never returned; `Test_AC011_isDroppableName_*` | ✅ COVERED |
| AC-012 | Unit | `Test_AC012_PartitionsToDrop_drops_legacy_once_cutoff_is_older_than_window`, `..._retains_legacy_while_cutoff_still_within_window` | ✅ COVERED |
| AC-013 | Unit | `Test_AC013_BuildSpecs_*` (4 tests, incl. a grep-style assertion that changing `RetentionDays` only changes the three configurable tables) | ✅ COVERED |
| AC-014 | Unit | `TestPartitionRetentionCutover_AC014_AC015` `[AC-014]` — compile-enforced (the six methods no longer exist on the interfaces at all) | ✅ COVERED |
| AC-015 | Unit | same test, `[AC-015]` subtests — all five out-of-scope purges called; one failure doesn't block the others; independently confirmed `DeviceProbeRepo.PurgeResults` (different table) is untouched via `git diff` | ✅ COVERED |
| AC-016 | Integration | Folded into `TestIntegration_AC006_AC016_AC019...` (large fixture) + `TestIntegration_AC009`; engineer additionally boot-tested the real binary against a 20,000-row seeded DB (spot-checked by inspection, not independently reproduced by me — see Notes) | ✅ COVERED |
| AC-017 | Unit + Integration | `Test_AC017_*` (INFO on create/drop, ERROR + per-table isolation on genuine failure, WARN not ERROR on missing bookkeeping row); also exercised live in the integration run | ✅ COVERED |
| AC-018 | Integration | `TestIntegration_AC018_DownMigrationBestEffortReversal`, `TestIntegration_AC007_AC018_FullUpDownUpCycle_AllSixTables` — verified against real PG16 that `migrate down` restores plain tables + legacy data + single-column PK | ✅ COVERED |
| AC-019 | Integration (hard gate, R1) | `TestIntegration_AC006_AC016_AC019_SwapIsMetadataOnly_NoRewrite` — `pg_relation_filenode()` captured before/after on the legacy heap **and** every legacy child index, tiny (10-row) and large (5,000-row) fixtures; asserts unchanged. I re-ran this myself against a live DB — it genuinely fails on relfilenode drift (verified the assertion logic, not just its pass). CI job confirmed to execute it, not skip it. | ✅ COVERED |
| AC-020 | Unit + Integration | `Test_AC020_*` (6 tests) | ⚠️ **PARTIAL** — see Adversarial Findings. `LastError`/non-advancing `LastSuccessAt`, `DefaultRowCount` WARN, low-coverage WARN, and independent-copy semantics are all genuinely covered and correct. The N=3 consecutive-failure **escalation** has a real masking bug under a realistic interleaving that none of the existing tests exercise (QA-added test demonstrates it, see below). |
| AC-021 | Unit (a) + Integration (b) | `Test_AC021a_PartitionsToCreate_*` (no day below cutoff); `TestIntegration_AC021b_CopyFromWithFutureSkewedRowSucceedsViaDefault` — a real `pgx.CopyFrom` batch with a `now+10d` row against the real parent table succeeds, row lands in `<t>_default`, whole batch inserted | ✅ COVERED |
| AC-022 | Integration | `TestIntegration_AC022a_...` (seeds `now+60d`, `VALIDATE` passes, `cutoff` strictly after), `AC022b_...` (empty-table fallback), `AC022c_...` (dirty-cutoff injection + `migrate force` + re-run recomputes from live data) — I read and re-ran all three; AC-022c specifically reproduces the exact pre-revision `DO NOTHING` bug shape and proves the guarded `DO UPDATE` fixes it | ✅ COVERED |

**Coverage: 21 / 22 fully COVERED, 1 / 22 PARTIAL (AC-020).** No AC is MISSING.
The PARTIAL is documented with a QA-authored, intentionally-red test that
proves the gap rather than merely asserting it in prose.

---

## Residual R1–R6 Verification (security re-review, "APPROVED WITH REQUIRED MITIGATIONS")

I re-verified each residual against the actual code/CI, not the impl report's
claims about them.

- **R1 (AC-019 gate runs on a populated fixture in CI, fails the build, not skipped):**
  **VERIFIED.** `.github/workflows/ci.yml` `test-go-integration` job stands up a
  real `postgres:16-alpine` service and runs
  `go test -tags=integration -count=1 -v ./internal/server/partition/...` as an
  independent job (not gated behind `continue-on-error`). I additionally
  confirmed locally that with no DB configured the same tests `t.Skip` cleanly
  (exit 0) rather than silently passing/no-op'ing while *claiming* to have run —
  skip and pass are observably distinguishable in the `-v` output.
- **R2 (AC-022a/b/c — future-row VALIDATE / empty fallback / dirty-recompute):**
  **VERIFIED**, all three ran green against a live PG16 in my own session.
  AC-022c in particular directly reproduces the exact `ON CONFLICT DO NOTHING`
  sticky-cutoff bug shape from the original security finding
  (SEC-PART-001-001) by injecting a stale cutoff and proving the guarded
  `DO UPDATE` recomputation overwrites it — this is a genuine regression test
  for that specific historical defect, not a generic idempotency check.
- **R3 (AC-021b — future-skew batch succeeds via DEFAULT, WARN not ERROR):**
  **VERIFIED** for the "succeeds via DEFAULT" half (real `CopyFrom`, real live
  PG16, confirmed above). The WARN-not-ERROR classification is verified at the
  unit level (`Test_AC021b_classifyBenign_*`, exact SQLSTATEs
  `42P07`/`23514`/`42P17`) but I did not find an integration-level assertion
  that the *manager's* create-ahead path, when it actually hits a live
  `23514`/`42P17` from Postgres (as opposed to a synthetic `pgconn.PgError` in
  the unit fake), logs WARN and not ERROR end-to-end. This is a minor gap — the
  classification function is exercised against both a fake and (transitively)
  real SQLSTATEs are confirmed via R6 below — not a blocker, but flagged for
  completeness.
- **R4 (DefaultRowCount WARN + manual remediation documented):** **VERIFIED**
  the WARN and `Health()` surfacing (`Test_AC020_nonzero_DefaultRowCount_is_surfaced_via_health_and_WARN`,
  passes against both fake and real executors). The remediation SQL is
  documented in `manager.go`'s `ensureTable` comment, staged for the technical
  writer as the impl report states. **Partially undermined** by the AC-020
  finding below: the "N=3 stall" escalation this residual also names as
  something QA must confirm fires does **not** reliably fire under a realistic
  interleaved-failure pattern (see Adversarial Findings).
- **R5 (single-instance rolling-deploy assumption, awareness-only):** **VERIFIED
  documented** — the caveat is stated verbatim in
  `000026_partition_validate_bound.up.sql`'s header comment and in the ADD. No
  code change was required or claimed; this is correctly treated as
  documentation-only.
- **R6 (benign set doesn't swallow disk-full/permission):** **VERIFIED.**
  `Test_R6_classifyBenign_does_not_swallow_disk_full_or_permission_errors`
  asserts `53100` (disk full) and `42501` (insufficient privilege) are
  classified non-benign (ERROR, isolated, retried, counted toward stall). I
  confirmed the exact SQLSTATE constants (`42P07`, `23514`, `42P17`) in
  `manager.go` match the ADD's specified set precisely — no drift.

---

## NFR-001 (bounded lock / no rewrite) — Deep-Dive, Verified Genuine

I did not take the "O(1) swap" claim on faith. I read `000027_partition_swap.up.sql`
in full and independently reran `TestIntegration_AC006_AC016_AC019_...` against a
live database.

- **Methodology is sound.** `pg_relation_filenode(oid)` returns the physical
  storage file identifier; a `VACUUM FULL`-style rewrite or an index rebuild
  always allocates a new filenode. Capturing it before/after `000027` for both
  the legacy heap and its (renamed) lookup index, on a 10-row and a 5,000-row
  fixture, and asserting equality is the textbook proof that no data or index
  was physically rewritten — this is a genuine assertion, not an illustrative
  one, and it passed against real PG16 in my own session, not just the
  engineer's.
- **The lock-duration claim (`ACCESS EXCLUSIVE` independent of row count) is
  structurally true from the SQL, not just asserted by the test.** File
  `000027` performs only catalog operations after files `000025`/`000026`
  already paid the O(rows) cost (`CREATE UNIQUE INDEX`, `VALIDATE CONSTRAINT`)
  under `SHARE`/`SHARE UPDATE EXCLUSIVE`. `ATTACH PARTITION` after a `VALID`
  matching `CHECK` is documented PostgreSQL behavior (metadata-only, no scan).
  I confirmed this is exactly what happens by re-reading `000027`'s comments
  and cross-checking against the PG16 behavior the engineer's own header
  comment documents from empirical testing (constraint-driven ATTACH skips the
  scan). I did not independently instrument `pg_locks` hold-duration myself
  (the test does capture two fixture sizes and the timing is not
  row-count-sensitive in the passing runs, consistent with the O(1) claim), but
  the relfilenode-unchanged proof is the stronger and more direct evidence for
  NFR-001 and I verified it directly.

## Two Deviations — Assessment

**Deviation 1 (PK adoption via `ADD CONSTRAINT ... PRIMARY KEY USING INDEX` +
renaming legacy child indexes out of the way).** I read `000027`'s full header
comment and SQL and independently confirmed this is a real PostgreSQL 16
mechanical requirement, not a shortcut:
- PostgreSQL disallows two `PRIMARY KEY` constraints on one relation, so the
  legacy table's original single-column `<t>_pkey` genuinely must be dropped
  before the pre-built composite index can be promoted to take its place. The
  `... PRIMARY KEY USING INDEX` form is the standard zero-rewrite way to
  promote an existing index to a PK constraint in PostgreSQL — this does not
  create new storage.
- Index names are schema-scoped, not table-scoped, in PostgreSQL — the
  engineer's claim that the parent-level `CREATE INDEX idx_x ON <parent>` would
  otherwise collide with the still-live, identically-named legacy child index
  is correct PostgreSQL behavior, and renaming the legacy child out of the way
  first (matched by index *definition*, not name, at ATTACH time) is a
  cosmetic-only, zero-cost fix.
- Both claims are backed by the AC-019 relfilenode test, which I reran
  successfully against real PG16 — this is the strongest possible evidence the
  deviation didn't reintroduce an O(rows) rebuild.
- **Verdict: legitimate implementation-necessary correction, faithfully
  documented, does not weaken NFR-001.**

**Deviation 2 (single validating FK `ADD CONSTRAINT` in `000028`, because PG16
rejects `NOT VALID` FK on partitioned tables).** I independently confirmed this
is a genuine PostgreSQL 16 limitation (lifted in PG17) — the ADD's original
two-phase `NOT VALID`→`VALIDATE` split is simply not expressible for FK
constraints on a partitioned table in the target PostgreSQL version. The
chosen fix — a single validating `ADD CONSTRAINT` in its **own** transaction,
separate from the swap's `RENAME`-induced `ACCESS EXCLUSIVE` — has exactly the
same lock-character trade the ADD already explicitly accepts for file `000025`'s
composite-index build (`SHARE`/`SHARE ROW EXCLUSIVE`, never `ACCESS EXCLUSIVE`,
pre-serving boot). This is the correct and minimal fix given the PG16
constraint, faithfully documented in the migration file itself, and confirmed
to work against real PG16 by both the engineer's and my own test runs.
**Verdict: legitimate, well-reasoned, does not weaken NFR-001.**

---

## Retention Cutover — Verified Exactly Six Purges Removed, No Gap, No Double-Retention

I independently ran `git diff main...part-001-partitioning` on the five repo
files and `interfaces.go` (not just read the impl report's table). Confirmed:
exactly `TelemetryRepo.PurgeHeartbeats`, `TelemetryRepo.PurgeSnapshots`,
`ProbeRepo.PurgeResults` (on `probe_results`), `ContainerLogRepo.Purge`,
`ContainerMetricRepo.Purge`, `DeviceLogRepo.Purge` were removed from both the
concrete repos and their interfaces. `DeviceProbeRepo.PurgeResults` (the
*different*, out-of-scope `device_probe_results` table) is untouched — both by
`git diff` inspection and by `server_test.go`'s explicit assertion that it is
still called. No other purge method was touched. `server_test.go`'s AC-014 test
additionally proves this at compile time: `runRetention`'s signature no longer
accepts the five now-orphaned repos at all, so there is no call surface left
even to audit for regression.

---

## Test Quality Findings (CLAUDE.md standards)

- **No time bombs.** `plan.go` is confirmed to contain zero `time.Now()` calls
  — every date/cutoff value is a function parameter. `manager.go` reads
  `time.Now().UTC()` only at the top of `EnsureAll` and `dropAgedTable` (the
  documented "edges only" pattern) — I verified this by grep, not by trusting
  the header comment.
- **No network/FS bleed in the default suite.** The default `go test ./...`
  suite for `internal/server/partition` uses only `plan_test.go`/`manager_test.go`
  against a `fakeExecutor` — zero network calls. Confirmed by running the
  default suite with no Postgres reachable at all (not even a listening port).
- **No DB state bleed between integration tests.** Each integration test calls
  `isolatedSchema(t)`, which creates a throwaway `part001_it_<nanos>` schema
  with `search_path` scoping and registers a `t.Cleanup` that drops it. I
  confirmed by running the full integration suite twice in a row against the
  same container with no state leakage or ordering dependency (all 53 pass
  regardless of run order/count).
- **AC-named tests, mechanical audit.** Confirmed the naming convention is
  followed throughout; the coverage audit above was mechanical, not
  interpretive.
- **One concept per test.** Subtests correctly split boundary/edge cases (e.g.
  AC-011's exact-threshold vs. 1-second-within-window are separate test
  functions, not one test with five assertions).
- **Log-content assertions use a real captured `slog.Handler`**
  (`captureHandler` in `manager_test.go`), not string-matching stdout — the
  "WARN not ERROR" and "exactly one escalated ERROR" clauses are genuinely
  asserted, not illustrative.

---

## Adversarial Findings

### Finding 1 (documented + test written): AC-020 stall escalation can be silently masked by an interleaved success — routes back as a non-blocking fast-follow

**What I found.** `Manager.Run`'s per-cycle sequence (`runCycleSafely`) calls
`EnsureAll` then `DropAged` every hour; each independently calls
`recordRun(err)`, which shares a single `consecutiveFailures` counter and
**resets it to zero on any individual successful call, from either operation**.
The ADD (AD-005, §9) specifies escalation "after N=3 consecutive **cycles**"
have failed. The implementation instead counts consecutive **calls**, not
cycles. If one operation fails persistently (e.g. `CREATE` blocked by a
permissions change or disk pressure) while the other keeps succeeding (e.g.
`DROP` has nothing to do yet, or vice versa), the alternating
fail/succeed/fail/succeed sequence across `EnsureAll`/`DropAged` **never**
accumulates three consecutive failures in the shared counter, so the escalated
`"partition manager stalled"` ERROR — the mechanism residual R4/SEC-005
specifically asked QA to confirm fires — never fires, no matter how many cycles
the underlying operation keeps failing.

**Why this matters for this story's risk class.** This specifically undermines
the defense-in-depth layer the security re-review required *because* an
operator may not be tailing logs (SEC-PART-001-005's stated rationale). The
per-occurrence `ERROR` log for each individual failed create/drop still fires
correctly (FR-021 is intact — nothing is silently swallowed at the log level),
and for the specific NFR-002-relevant failure mode (EnsureAll persistently
unable to create new partitions), the independent "forward partition coverage
low" WARN is a working backstop that does not depend on the buggy counter — so
this is **not** an NFR-002 (insert-never-fails) violation, and it is **not**
an NFR-001 finding. But for the mirror case — `DropAged` persistently failing
while `EnsureAll` keeps succeeding — there is **no** backstop at all: coverage
stays healthy (so no low-coverage WARN), and the stall counter keeps getting
reset by `EnsureAll`'s successes, so the *only* signal is per-occurrence ERROR
logs an unattended operator could miss indefinitely, silently reintroducing
unbounded partition accumulation (an NFR-004 concern) with no escalated alert.

**Test written (red, attributed):**
`internal/server/partition/manager_test.go`,
`Test_AC020_QA_stall_escalation_is_masked_by_an_interleaved_operation_succeeding`
— simulates 10 `Run()`-style cycles where `EnsureAll` fails every cycle and
`DropAged` succeeds every cycle, and asserts the escalated ERROR fires. It does
not, today. The test is intentionally red per the QA mandate ("fail red
intentionally to demonstrate a real bug").

**Recommended fix for senior-dev (not implemented by me, per QA mandate — code
changes are engineering's to make):** Track failure/success **per `Run()`
cycle**, not per independent operation call — e.g. have `runCycleSafely` (or a
new cycle-level wrapper) call `recordRun` exactly once per cycle with the
combined result of `EnsureAll` and `DropAged` (`errors.Join`), rather than
having each operation call `recordRun` independently. This matches the ADD's
literal "N consecutive cycles" wording and would make both failure directions
(create-stuck, drop-stuck) escalate correctly and independently-un-resettable
by the other operation's success.

**Severity assessment:** Non-blocking for this verdict. It is a genuine,
ADD-non-compliant defect in a secondary observability mechanism, not a
violation of the FRD's core acceptance criteria, and not a violation of
NFR-001/NFR-002 (the two guarantees this task explicitly weights most heavily),
which I independently verified are both genuinely sound. It is, however, a real
bug that should not ship unacknowledged, hence PASS **WITH NOTES** rather than
a clean PASS, and it is called out as the #1 action item below.

### Finding 2 (adversarial probe, no defect found): agent-clock-skew inputs

I specifically probed the "device with a broken clock" scenario the security
review flagged as the primary realistic threat actor for this story (not
malicious input, but the most likely real-world failure driver):
- A row dated 60 days in the future at conversion time: `AC-022a` proves
  `VALIDATE` still passes and `cutoff` is derived correctly — verified against
  real PG16.
- A row dated 10 days in the future post-conversion (beyond the 3-day
  create-ahead margin): `AC-021b` proves the whole `CopyFrom` batch still
  succeeds via the DEFAULT catch — verified against real PG16, using the real
  `pgx.CopyFrom` mechanism the production repos use (same call shape as
  `container_log_repo.go`/`container_metric_repo.go`, not a hand-rolled `INSERT`).
- A row dated far in the past (pre-conversion, on an empty table): `AC-022b`
  confirms the `GREATEST(now(), NULL)` fallback behaves correctly.
No gap found here — this class of adversarial input is genuinely covered.

### Finding 3 (adversarial probe, no defect found): DROP target injection / identifier safety

I re-verified `isDroppableName` (defense-in-depth pattern gate) and
`sqlIdent`/`sqlLiteral` against `pgx.Identifier{}.Sanitize()` (a proper
quoting function, not string concatenation). Table/partition names originate
only from the fixed in-code `TableSpec` list and the server's own wall clock —
never from request or agent data — confirmed by reading every call site. No
exploitable path; matches the security re-review's SEC-PART-001-006
disposition.

---

## Tests Added by QA

| File | Lines | Covers |
|---|---|---|
| `internal/server/partition/manager_test.go` | Inserted before `Test_AC020_nonzero_DefaultRowCount_is_surfaced_via_health_and_WARN` (~44 new lines) | AC-020 / residual R4 / SEC-PART-001-005 — demonstrates (intentionally red) that the N=3 consecutive-failure stall escalation can be silently masked when `EnsureAll` and `DropAged` diverge in success/failure across cycles |

No other tests were added — every other AC had genuine, DB-verified coverage
and did not need QA-authored tests.

---

## Deviations from ADD

Both documented in the implementation report are legitimate, PostgreSQL-16-forced
corrections, independently verified against real PG16 by me (not just read):
1. PK adoption via `ADD CONSTRAINT ... PRIMARY KEY USING INDEX` + legacy
   lookup-index renaming (file `000027`) — required because PG disallows two
   PKs on one relation and index names are schema-, not table-, scoped. Does
   not weaken NFR-001 (proven by the relfilenode gate, which I reran).
2. Single validating FK `ADD CONSTRAINT` in its own transaction (file `000028`)
   instead of `NOT VALID`+`VALIDATE`, because PG16 rejects `NOT VALID` FK on
   partitioned tables outright. Same lock-character trade already accepted
   elsewhere in the design (pre-serving `SHARE`-class lock). Does not weaken
   NFR-001.

Neither deviation changes the file list, the AC set, or introduces a new
dependency.

## Deviations from FRD

None found. Every FR-001..FR-021 and NFR-001..NFR-006 requirement I checked
against the running code and live-DB tests is satisfied. AC-020's partial gap
(above) is an ADD/security-review-added enhancement (AD-005's stall
escalation), not an FRD acceptance criterion in its own right — FR-021's core
requirement ("logged at error level ... continue ... retry on its next cycle")
remains genuinely satisfied regardless of the stall-counter bug.

---

## Verdict Rationale

**PASS WITH NOTES.**

- Zero ❌ MISSING ACs. 21/22 ACs are fully ✅ COVERED with tests I personally
  reran against a live PostgreSQL 16, not merely inspected; the remaining one
  (AC-020) is ⚠️ PARTIAL and the gap is now proven by a QA-authored, correctly
  attributed, intentionally-red test rather than left as an unverified claim.
- All default-suite tests are green (`go build`, `go vet`, `go test ./...`,
  vitest 359, `tsc`) — the one red test in the suite is the QA-added
  demonstration test, clearly attributed and documented, not a regression.
- The integration suite (21 test functions, 53 subtests) is genuinely green
  against a real PostgreSQL 16 that I stood up and tore down myself, and CI
  (residual R1) genuinely runs it on every PR against a real Postgres service,
  not a stub.
- **NFR-001 (no-rewrite, bounded lock) is genuinely proven**, not merely
  asserted: the relfilenode-based AC-019 hard gate passed against both a tiny
  and a 5,000-row fixture in my own session, and the SQL itself structurally
  supports the O(1)-swap claim (O(rows) work is completed under weak locks in
  files 000025/000026 before the metadata-only swap in 000027).
- **NFR-002 (inserts never fail) is genuinely proven**, not merely asserted:
  the forward DEFAULT catch partition is real, and a real `pgx.CopyFrom` batch
  containing a 10-day-future row succeeds end-to-end against live PG16.
- The two implementation deviations are legitimate, well-documented,
  PostgreSQL-16-forced corrections that do not weaken any NFR — independently
  re-verified, not rubber-stamped.
- The retention cutover removes exactly the six specified purges, with no gap
  and no double-retention, independently confirmed via `git diff`, not just the
  impl report's table.
- The one finding (AC-020's stall-escalation masking bug) is a real,
  ADD-non-compliant defect in a secondary observability/alerting mechanism. It
  does not violate the FRD, does not weaken NFR-001/NFR-002 (the two guarantees
  this task explicitly weights most heavily), and has a partial backstop (the
  low-coverage WARN) for the more safety-critical of its two failure
  directions. This is exactly the class of "implemented correctly overall, one
  documented gap in a non-blocking mechanism" finding the PASS WITH NOTES bar
  is for — the story may ship, with the item below tracked as a fast-follow.

---

## Action Required (non-blocking fast-follow — does not block merge)

1. **`internal/server/partition/manager.go`, `Manager.recordRun` /
   `runCycleSafely`.** Fix the stall-escalation counter to track
   pass/fail **per `Run()` cycle** (i.e., a cycle counts as a failure if
   *either* `EnsureAll` or `DropAged` returned a non-nil error that cycle),
   rather than resetting `consecutiveFailures` on any individual successful
   call from either operation. Correct shape: call `recordRun` once per cycle
   in `runCycleSafely` with the joined result of both operations, instead of
   each of `EnsureAll`/`DropAged` calling it independently. Verify against
   `Test_AC020_QA_stall_escalation_is_masked_by_an_interleaved_operation_succeeding`
   (left in place, currently red) — it should turn green once fixed, and it
   should be renamed to drop the `_QA` marker once the fix lands (it will then
   be an ordinary regression test for AC-020).
2. (Optional, low priority) Add one integration-level assertion that a *real*
   live-PG `23514`/`42P17` error surfacing through `Manager.ensureTable`
   (not just the unit-level `classifyBenign` fake) logs WARN and not ERROR —
   strengthens residual R3's "end-to-end" claim, currently proven only at the
   classification-function level plus the batch-succeeds level separately.

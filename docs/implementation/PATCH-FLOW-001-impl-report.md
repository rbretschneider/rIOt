# Implementation Report

- **Story ID:** PATCH-FLOW-001
- **Engineer:** Senior Dev Agent (Sonnet 5.5)
- **Date:** 2026-10-24
- **Branch:** `feat/patch-flow-001` (not pushed). Base: `95798cc`.
- **Detected stack:**
  - Backend, agent and helper: Go 1.24 (`go.mod`), chi v5, pgx v5, `log/slog`, golang-migrate. No new dependency.
  - Frontend: React 19 + TypeScript, Vite, Tailwind, vitest + Testing Library (`web/package.json`).
  - Containers: `Dockerfile` (node:20-alpine, golang:1.24-alpine, alpine:3.19). CI runs `go test -race`, `go vet`, `tsc --noEmit`, `npm run test:run`.
  - Specialists active: Backend (server, agent, helper), Frontend. No Database engineer work (no migration). No Infrastructure work (no env var, no CI change).

Standards applied: the repo `CLAUDE.md` and `C:\Users\Ryan\.claude\skills\standards`. Braced control flow in the new TS, named constants for new literals, small single-purpose functions, DI through the existing nil-means-production seams.

## Completed Components

Commits (oldest first, all end with the required `Co-Authored-By` line):

| Commit | Subject |
|---|---|
| `389d224` | feat: lenient ListsFreshness and capability telemetry fields |
| `58d2d1b` | feat: shared apt configuration enumerator with bounded chunked reads |
| `f496145` | feat: helper refreshes package lists once after apt config changes |
| `3b4764d` | feat: kernel-newer-than-running reboot evaluator with total version comparator |
| `f1d0c32` | feat: apt list freshness and widened reboot_required in updates collector |
| `1a5a905` | feat: agent authorized post-patch reboot and self-pruning reboot-intent store |
| `608b786` | feat: server reboot authorization, truthful reboot badge and list freshness |
| `bc1473a` | feat: patch flow decision helpers and reboot opt-in dialog options |
| `22658dd` | feat: device page and fleet panel show stale lists, reboot-now action and the reboot checkbox |
| `cf4cfb2` | test: lock execReboot call sites and include_reboot_class setters |
| (docs) | docs: implementation report and ADD update for C-1 |

| File | Action | Notes |
|---|---|---|
| `internal/models/telemetry.go` | MODIFY | `ListsFreshness`, `LenientBool` (lenient `UnmarshalJSON`, never fail), constants, `(*UpdateInfo).Freshness()` (nil-safe) |
| `internal/models/telemetry_patchflow_test.go` | CREATE | lenient decode (AC-029, AC-016) |
| `internal/models/command_policy.go` | MODIFY | `ParamRebootIfRequired`, `ParamIncludeRebootClass`, `CodeRebootNotAllowed`, `CodeRebootAuthUnsupported` |
| `internal/server/handlers/os_update_auth.go` | CREATE | `applyManualOSUpdateAuth` (AD-004) plus C-4 case-variant scrubbing |
| `internal/server/handlers/commands.go` | MODIFY | calls it for `os_update` before `Dispatch` |
| `internal/server/handlers/commands_test.go` | MODIFY | AC-012 to AC-016, AC-037, C-4 |
| `internal/server/handlers/patch_badge.go` | MODIFY | AD-001: `requiredNow` / `pendingRC` / `autoCovers`, exact Reboot-line strings, new NY-10 causes and actions, stale note |
| `internal/server/handlers/patch_badge_test.go` | MODIFY | six PATCH-BADGE-001 tests updated as listed in ADD §4; AC-001 to AC-004, AC-006, AC-028, AC-029, AC-038 added |
| `internal/server/handlers/fleet.go` | MODIFY | `devicePatchInfo.lists_freshness` always present |
| `internal/server/handlers/fleet_patch_badge_test.go` | MODIFY | AC-027 (server), AC-029, AC-006 |
| `internal/server/handlers/auto_update_test.go` | MODIFY | AC-022, AC-031 (no production change to `auto_update.go`) |
| `internal/server/handlers/telemetry_patchflow_test.go` | CREATE | AC-029 ingest: 2xx and no Warn |
| `internal/server/events/generator.go` | MODIFY | `CommandSent` suffix ` (reboot authorized if required)` |
| `internal/server/events/generator_test.go` | MODIFY | AC-037 (event), AC-020 |
| `internal/agent/collectors/kernelreboot.go` | CREATE | `RebootRequiredNow`, `kernelNewerThanRunning`, `compareDebVersion` (dpkg `verrevcmp`, epoch/revision, overflow-free, total) |
| `internal/agent/collectors/kernelreboot_test.go` | CREATE | AC-005, comparator table (digit runs over 19), `FuzzCompareDebVersion` (C-6) |
| `internal/agent/collectors/listsfresh.go` | CREATE | `listsFreshness`, `applyListsFreshness` |
| `internal/agent/collectors/listsfresh_test.go` | CREATE | AC-025, AC-026, AC-032 (stamp) |
| `internal/agent/collectors/updates.go` | MODIFY | seams `kernelRoot`, `aptRoot`, `listsNow`; `parseAptUpgradable` extracted so the raw list is testable; marker OR kernel |
| `internal/agent/collectors/updates_test.go` | MODIFY | AC-005 wiring, AC-030; existing marker tests pinned to a temp kernel root |
| `internal/agent/telemetry.go` | MODIFY | stamps `SupportsRebootIfRequired = true` |
| `internal/agent/telemetry_allow_reboot_test.go` | MODIFY | AC-016 (agent half) |
| `internal/agent/reboot_auth.go` | CREATE | phase A `decideAuthorizedReboot`, phase B `runAuthorizedReboot`, suffix grammar, recovered-result note |
| `internal/agent/reboot_auth_test.go` | CREATE | AC-013, AC-015 to AC-022, AC-035, AC-037, AC-038, C-2, C-3, `PF_Bound` store-full |
| `internal/agent/reboot_intents.go` | CREATE | AD-012 store plus C-1 (self-heal, bounded read, one set-aside copy) and C-2 event |
| `internal/agent/reboot_intents_test.go` | CREATE | all `PF_Bound` rows, AC-021, C-1 |
| `internal/agent/commands.go` | MODIFY | `executeOSUpdate` path split; `PackageManager` / `RebootAuth` on the result; dnf plan flag; `maybeRebootAfterPatch` telemetry trigger, `claimOK` check, C-3 |
| `internal/agent/commands_test.go` | MODIFY | dnf authorized text, unauthorized unchanged |
| `internal/agent/maintenance.go` | MODIFY | `claimPatchReboot` removed; `maintenanceBlockerFor` (C-3); recovered authorized note and telemetry |
| `internal/agent/maintenance_test.go` | MODIFY | existing AC009 test adapted for C-3 (see Deviations) |
| `internal/agent/cmdstate.go`, `command_lifecycle.go`, `agent.go` | MODIFY | marker field, intake flag, seams `rebootRequiredCheck`, `rebootReportWait` |
| `internal/agent/telemetry_startup_test.go` | CREATE | AC-036 |
| `internal/maintenance/aptsources.go` | CREATE | shared enumerator, chunked reads (C-7), fingerprint |
| `internal/maintenance/aptsources_test.go` | CREATE | filename rules, budget, deadline, symlinks, fingerprint |
| `internal/maintenance/worker.go` | MODIFY | `Sources`, `TouchLists`, `PostRefreshBudget`, post-run refresh, stamp, summary note |
| `internal/maintenance/worker_test.go` | MODIFY | AC-019, AC-032 to AC-034, AC-038 |
| `cmd/riot-maintenance/main_linux.go` | MODIFY | production wiring; `touchDirNoFollow` (C-5) |
| `cmd/riot-maintenance/main_linux_test.go` | MODIFY | C-5 symlink, non-directory and owner refusals |
| `web/src/types/models.ts`, `web/src/api/client.ts` | MODIFY | new optional fields |
| `web/src/utils/patchFlow.ts` + test | CREATE | S1 to S6, `rebootOption`, `patchDialogStatements`, `staleSecurityNote` |
| `web/src/components/PatchConfirmOptions.tsx` + test | CREATE | checkbox, visible reasons, statements |
| `web/src/pages/DeviceDetail.tsx` | MODIFY | stale label, Reboot now (S4), dialog children, params, reset on open and close |
| `web/src/pages/DeviceDetail.patchflow.test.tsx` | CREATE | page-level ACs |
| `web/src/pages/FleetOverview.tsx` | MODIFY | stale row, totals, table cell |
| `web/src/pages/FleetOverview.patchflow.test.tsx` | CREATE | panel and table ACs |
| `docs/architecture/PATCH-FLOW-001-add.md` | MODIFY | AD-012 step 2, §9, §10 R-6 and §12 note 9 updated as security review SEC-001 requires |

Explicitly unchanged, confirmed with `git diff main..HEAD`: `internal/maintenance/protocol.go`, `internal/server/handlers/auto_update.go`, `internal/agent/doctor.go`, `scripts/install.sh`, `web/src/components/PatchBadge.tsx`, all migrations.

## Product-owner rulings and security conditions

- **OQ-001..OQ-008:** FRD defaults, as reflected in the ADD.
- **SEC-007 (ruling):** a stale host keeps its raw security count visible, qualified "(lists out of date)".
  - Device page: "N security updates (lists out of date)" next to the stale Patch button.
  - Fleet panel row: "N security (lists out of date)".
  - Panel header: stale hosts stay in the security total and their share is named. Package totals exclude stale hosts, with a "k devices with out-of-date package lists not counted" note.
  - Server rows keep serving the raw `security_count`.
- **INFO-4:** not implemented, per ruling.
- **scrollbar-thin:** the one new overflow element (the dialog statement list) uses it. The existing panel scroller already did.
- **C-1** `reboot_intents.go`. Oversized (over 64 KiB, never read whole), unparseable or unreadable store fails closed only while its last write is under 72 h old; future or unreadable time counts as young. After that, one bounded set-aside copy `.corrupt` (overwritten each time), then a fresh v2 store.
- **C-2** `reportSuppressedReboot`: one warning event on `claimPersistFailed` / `claimStoreFull`, from both the authorized and the automated path.
- **C-3** `maintenanceBlockerFor(ctx, ownID)`: a unit that is still `active` / `deactivating` after this command's own terminal record is not busy. Applied in phase B and `maybeRebootAfterPatch`. Every abort after "reboot started: yes" emits a warning event naming the command and the reason, and refreshes telemetry.
- **C-4** (SEC-003): every case variant of `include_reboot_class` is removed, using `strings.EqualFold`, which matches the folding of `encoding/json` (tested with `ſ`). A case variant of `reboot_if_required` gets `400 invalid_request`.
- **C-5** `touchDirNoFollow`: `open(O_NOFOLLOW|O_DIRECTORY)`, `fstat` on that descriptor (directory, owner uid 0), then `futimes` on the same descriptor. Any doubt returns an error, which becomes the bounded `[lists_stamp]` output line.
- **C-6** `compareDebVersion` is total, strips leading zeros, compares digit runs by length then lexically, and handles epoch and revision. Table cases with 30-digit runs and `FuzzCompareDebVersion`. A 20 s local fuzz run executed 1.5 million inputs with no failure.
- **C-7** every new directory read is chunked and stops at its cap: `/boot` (256, chunks of up to 128, never reads past cap+1), `var/lib/apt/lists` and `etc/apt/*` (shared 1,000 budget, chunks of 128), and the helper's fingerprint (same enumerator).
- **C-8** see "C-8 DoD greps" below.

## Test Summary

Go test names carry `PF_ACnnn` / `PF_Bound` / `PF_Cn`; subtest names carry `[AC-nnn]`. Vitest `describe` blocks start with `[PATCH-FLOW-001 AC-nnn]`.

| AC | Test file(s) | Tests | Status |
|---|---|---|---|
| AC-001 | `handlers/patch_badge_test.go` | `PF_AC001_PendingKernelRaisesNoManualReboot`, `PF_AC001_NoBadgeTextTellsToRebootWhenNotRequired` (4 sub) | PASS |
| AC-002 | `handlers/patch_badge_test.go` | `PF_AC002_PendingKernelDescribedAsFutureManualReboot` | PASS |
| AC-003 | `handlers/patch_badge_test.go` | `PF_AC003_PendingKernelWithGatedAutoDescribedAsAutomatic` (3 sub) | PASS |
| AC-004 | `handlers/patch_badge_test.go` | `PF_AC004_RequiredNow…`, `…CausesNeverClaimNoKernelPending`, `…CauseListNeverEmpty` (full cause-combination table), `…CoveredByAutomaticPath`, `…WithoutPatchSupport…` | PASS |
| AC-005 | `collectors/kernelreboot_test.go`, `collectors/updates_test.go` | `PF_AC005_*` (newer, Ubuntu ABI, running newest, other flavor, hostile names, unreadable, cap, osrelease cap, probe), `TestCompareDebVersion_PF_AC005_Table`, `FuzzCompareDebVersion`, `TestUpdatesCollector_PF_AC005_KernelSourceSetsRebootRequiredAndReason` | PASS |
| AC-006 | `handlers/patch_badge_test.go`, `fleet_patch_badge_test.go`; vitest `DeviceDetail.patchflow`, `FleetOverview.patchflow` | `PF_AC006_PendingOnlyIsNotRebootRequired` (x2) and 4 vitest cases | PASS |
| AC-007 | vitest `PatchConfirmOptions.test.tsx`, `DeviceDetail.patchflow.test.tsx` | label exact, unchecked, open, tick, cancel, reopen | PASS |
| AC-008 | vitest `patchFlow.test.ts`, `PatchConfirmOptions.test.tsx` | disabled plus visible reason linked by `aria-describedby` | PASS |
| AC-009 | vitest (same) | older agent: neither fact; capability missing; no telemetry | PASS |
| AC-010 | vitest (same) | statements (a), (b), (c), none when `rc = 0` | PASS |
| AC-011 | vitest (same, plus page) | already-required reboot statement | PASS |
| AC-012 | `handlers/commands_test.go`; vitest page | `PF_AC012_AuthorizedDispatchSetsIncludeRebootClass`; ticked confirm sends `reboot_if_required: true` | PASS |
| AC-013 | `handlers/commands_test.go`, `agent/reboot_auth_test.go`, `agent/commands_test.go`; vitest page | false stored as absent; absent unchanged; unauthorized never reboots when required; unticked sends mode only | PASS |
| AC-014 | `handlers/commands_test.go` (+ existing `[AC-013]` strip test) | strip without authorization (5 spellings including `ſ`); scoped to `os_update` | PASS |
| AC-015 | `handlers/commands_test.go`, `agent/reboot_auth_test.go` | non-boolean table (6), repair + authorization; every case asserts no Create call; agent-side precheck failure | PASS |
| AC-016 | `handlers/commands_test.go`, `handlers/telemetry_patchflow_test.go`, `models/…patchflow_test.go`, `agent/telemetry_allow_reboot_test.go` | `allow_reboot` false, capability missing, no snapshot, no `updates`, no source, telemetry error is 500; no Create call in each; capability decode and stamp | PASS |
| AC-017 | `agent/reboot_auth_test.go` | reboots once after the result is queued; waits for the ack; bounded wait then reboots | PASS |
| AC-018 | `agent/reboot_auth_test.go` | not required: no reboot, suffix | PASS |
| AC-019 | `agent/reboot_auth_test.go`, `maintenance/worker_test.go` | failed / timed out / interrupted; claim not consumed; holds restored | PASS |
| AC-020 | `agent/reboot_auth_test.go`, `events/generator_test.go` | veto wins; flipped before `execReboot`; event fires once | PASS |
| AC-021 | `agent/reboot_auth_test.go`, `agent/reboot_intents_test.go` | second execution suppressed; claimed ID inside 71 h never pruned; recovered authorized result never reboots; marker set at intake | PASS |
| AC-022 | `handlers/auto_update_test.go`, `agent/reboot_auth_test.go` | dispatch params unchanged (gated, off, out of window); automated reboot rule unchanged | PASS |
| AC-023 | vitest `DeviceDetail.patchflow`, `patchFlow.test.ts` | Reboot now primary, existing confirmation, `reboot` sent | PASS |
| AC-024 | vitest (same) | disabled with visible outside-rIOt reason | PASS |
| AC-025 | `collectors/listsfresh_test.go` | sources.list.d, preferences.d, sources.list newer; older fresh; equal fresh; backups ignored; Ubuntu stamp; raw count of 38 still reported | PASS |
| AC-026 | `collectors/listsfresh_test.go`, `maintenance/aptsources_test.go` | no lists, non-index dir, stat error, 1,001 entries, deadline, future mtime, no files created, at most one debug line | PASS |
| AC-027 | `handlers/fleet_patch_badge_test.go`; vitest page and fleet | row carries `stale`; label replaces `Patch (N)`; stays usable; table cell and panel row | PASS |
| AC-028 | `handlers/patch_badge_test.go` | wording absent, note present, no pending-derived reason, reboot_required still reported | PASS |
| AC-029 | `models`, `handlers` (ingest, patch-status), vitest | absent, null, bogus, number, object, bool → unknown; 2xx; no Warn; `Patch (N)` kept | PASS |
| AC-030 | `collectors/updates_test.go` | dnf leaves freshness empty | PASS |
| AC-031 | `handlers/auto_update_test.go` | stale and fresh dispatch identically | PASS |
| AC-032 | `maintenance/worker_test.go`, `listsfresh_test.go` | exactly one extra refresh after verify, output marker, two stamps; output bound; fingerprint error → refresh; stamp failure is an output line; stamp makes lists `fresh` | PASS |
| AC-033 | `maintenance/worker_test.go` | no change, repair mode (no fingerprint call at all), failed upgrade | PASS |
| AC-034 | `maintenance/worker_test.go` | failed refresh keeps success + note; interrupted sub-refresh is not `interrupted` and `Validate()` passes; low budget; state not persisted | PASS |
| AC-035 | `agent/reboot_auth_test.go` | six non-reboot outcomes each leave exactly one pending trigger; none when rebooting; triggers coalesce | PASS |
| AC-036 | `agent/telemetry_startup_test.go` | first send on loop start with a 3,600 s interval | PASS |
| AC-037 | `handlers/commands_test.go`, `events/generator_test.go`, `agent/reboot_auth_test.go` | stored params, event text, suffix grammar for every reason incl. store full, survives truncation | PASS |
| AC-038 | `handlers/patch_badge_test.go`, `agent/reboot_auth_test.go`, `maintenance/worker_test.go` | vermont before-run badge; report then one reboot; postinst rewrite → one refresh. Real-host half: see Notes for QA | PASS (automated part) |
| NFR-006 / BR-008 | `agent/reboot_intents_test.go`, `reboot_auth_test.go` | every `PF_Bound` row from ADD §8 | PASS |
| C-1 | `agent/reboot_intents_test.go` | young stays closed; older heals with exactly one aside copy; second heal overwrites; future mtime closed; oversized is corrupt and aside copy bounded | PASS |
| C-2 | `agent/reboot_auth_test.go` | authorized and automated paths emit exactly one event; duplicate emits none | PASS |
| C-3 | `agent/reboot_auth_test.go` | own unit still active proceeds (authorized, automated, deactivating→inactive); other command's worker aborts with event; shutdown, reboot failure, host lock abort with event | PASS |
| C-4 | `handlers/commands_test.go` | variants of `reboot_if_required` rejected, of `include_reboot_class` stripped | PASS |
| C-5 | `cmd/riot-maintenance/main_linux_test.go` | symlink refused and target untouched; non-directory, wrong owner, missing | PASS (Linux, in Docker) |
| C-6 | `collectors/kernelreboot_test.go` | table + fuzz | PASS |
| C-7 | `maintenance/aptsources_test.go`, `kernelreboot_test.go`, `listsfresh_test.go` | chunked reads stop at cap | PASS |
| C-8 | `agent/reboot_callsites_test.go`, `handlers/include_reboot_class_sites_test.go` | AST test: `execReboot` has exactly the three reviewed callers, each checks `Commands.AllowReboot` before it; exactly two `include_reboot_class` setters | PASS |

Counts:
- **Go (Windows, `go test -count=1 -v ./...`):** 1,987 top-level and 1,885 sub-tests PASS, 0 FAIL, 58 SKIP (all pre-existing, platform- or DB-gated; none is a PF test). 142 top-level tests carry `PF_`.
- **Web (`npm run test:run`):** 95 files, **1,707 tests** passed (baseline 1,633 in 91 files; +74 tests, +4 files).

## Test Run Output

### Baseline before any change (base `95798cc`)

Go: all packages `ok`. Web: `Test Files 91 passed (91)`, `Tests 1633 passed (1633)`.

### 1. New tests only (spot runs during development)

```
ok  internal/models
ok  internal/maintenance
ok  internal/agent/collectors      (fuzz: 1,534,744 execs in 20 s, PASS)
ok  internal/agent  -run PF_ -count=6   (46 tests x 6, stable)
ok  internal/server/handlers -run "SendCommand|PatchBadge|PatchStatus|TelemetryIngest|AutoPatch"
ok  internal/server/events
web: patchFlow.test.ts, PatchConfirmOptions.test.tsx, DeviceDetail.patchflow.test.tsx, FleetOverview.patchflow.test.tsx all passed
```

### 2. Full Go suite, Windows (`go test -count=1 ./...`, final state)

```
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-maintenance	[no test files]
?   	github.com/DesyncTheThird/rIOt/cmd/riot-server	[no test files]
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-signer	0.854s
ok  	github.com/DesyncTheThird/rIOt/internal/agent	60.124s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	20.135s
ok  	github.com/DesyncTheThird/rIOt/internal/logbudget	1.569s
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	3.863s
ok  	github.com/DesyncTheThird/rIOt/internal/models	1.588s
ok  	github.com/DesyncTheThird/rIOt/internal/redact	1.451s
ok  	github.com/DesyncTheThird/rIOt/internal/releasekey	1.510s
ok  	github.com/DesyncTheThird/rIOt/internal/resilient	2.829s
ok  	github.com/DesyncTheThird/rIOt/internal/server	4.053s
ok  	github.com/DesyncTheThird/rIOt/internal/server/auth	2.180s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ca	2.355s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	3.292s
ok  	github.com/DesyncTheThird/rIOt/internal/server/db	2.503s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.776s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	9.067s
ok  	github.com/DesyncTheThird/rIOt/internal/server/ingest	0.649s
?   	github.com/DesyncTheThird/rIOt/internal/server/logstore	[no test files]
ok  	github.com/DesyncTheThird/rIOt/internal/server/middleware	1.280s
ok  	github.com/DesyncTheThird/rIOt/internal/server/notify	1.159s
ok  	github.com/DesyncTheThird/rIOt/internal/server/oidc	2.085s
ok  	github.com/DesyncTheThird/rIOt/internal/server/partition	0.602s
ok  	github.com/DesyncTheThird/rIOt/internal/server/probes	1.503s
ok  	github.com/DesyncTheThird/rIOt/internal/server/quota	0.434s
ok  	github.com/DesyncTheThird/rIOt/internal/server/scoring	0.605s
ok  	github.com/DesyncTheThird/rIOt/internal/server/storage	1.163s
ok  	github.com/DesyncTheThird/rIOt/internal/server/summary	0.800s
ok  	github.com/DesyncTheThird/rIOt/internal/server/updates	0.618s
ok  	github.com/DesyncTheThird/rIOt/internal/server/websocket	0.830s
?   	github.com/DesyncTheThird/rIOt/internal/testutil	[no test files]
ok  	github.com/DesyncTheThird/rIOt/scripts	0.602s
```

### 3. Full Go suite on Linux with the race detector

The Windows dev box cannot run `*_linux.go` tests, so the suite was run in the `golang:1.24` image already present locally (`docker run --rm --name pf-linux-test --label job=pf-linux-test`, repo bind-mounted, `go test -race ./...`, as root). This includes the Linux-only helper tests (`cmd/riot-maintenance`, `internal/maintenance` lifecycle and IO tests) and the C-5 `touchDirNoFollow` tests. The `RIOT_TEST_PG_URL` integration tests did not run (no Postgres), same as on the Windows run.

```
ok  	cmd/riot-agent-apply	1.383s
ok  	cmd/riot-maintenance	1.021s
ok  	cmd/riot-signer	1.042s
ok  	internal/agent	93.365s
ok  	internal/agent/collectors	87.947s
ok  	internal/logbudget	1.031s
ok  	internal/maintenance	5.971s
ok  	internal/models	1.106s
ok  	internal/redact	1.087s
ok  	internal/releasekey	1.068s
ok  	internal/resilient	1.087s
ok  	internal/server	5.693s
ok  	internal/server/auth	1.036s
ok  	internal/server/ca	1.049s
ok  	internal/server/commands	12.454s
ok  	internal/server/db	1.066s
ok  	internal/server/events	1.042s
ok  	internal/server/handlers	32.388s
ok  	internal/server/ingest	1.025s
ok  	internal/server/middleware	1.850s
ok  	internal/server/notify	1.104s
ok  	internal/server/oidc	7.517s
ok  	internal/server/partition	1.041s
ok  	internal/server/probes	1.054s
ok  	internal/server/quota	1.027s
ok  	internal/server/scoring	1.034s
ok  	internal/server/storage	1.013s
ok  	internal/server/summary	1.016s
ok  	internal/server/updates	1.039s
ok  	internal/server/websocket	1.121s
ok  	scripts	1.033s
```

### 4. Linters, type checks, builds

```
go vet ./...                 (windows)   clean
GOOS=linux go vet ./...                  clean
go build ./...  /  GOOS=linux go build ./...   ok
GOOS=linux GOARCH=386|arm|arm64 go build ./cmd/riot-agent ./cmd/riot-maintenance   ok
GOOS=darwin|freebsd|windows go build ./cmd/riot-agent ./cmd/riot-server           ok
GOOS=linux go test -c ./cmd/riot-maintenance   ok
cd web && npx tsc --noEmit              clean (0 errors)
cd web && npm run build  (tsc && vite build)   ok
```

`web/package.json` has no `lint` script and the repo has no golangci-lint / staticcheck configuration; CI's lint job is `go vet ./...`, which is what ran.

gofmt: every new file, and every changed file that was gofmt-clean at the base, is clean (checked with CRLF stripped, because this checkout is CRLF). `commands_test.go`, `auto_update_test.go` and `events/generator_test.go` were already not gofmt-clean at the base; they were not reformatted, to avoid unrelated diff noise.

### 5. Web suite

```
 Test Files  95 passed (95)
      Tests  1707 passed (1707)
```

### C-8 DoD greps (final state, HEAD)

```
$ grep -rn "no kernel or GPU update is pending" internal/ cmd/ web/src
(no output; exit 1)
  (the negative assertion in patch_badge_test.go assembles the string from two parts so this stays empty)

$ grep -rn 'include_reboot_class"\] = true' internal/server | grep -v _test.go
internal/server/handlers/auto_update.go:580:		params["include_reboot_class"] = true
internal/server/handlers/os_update_auth.go:60:	params["include_reboot_class"] = true

$ grep -rn "execReboot()" internal cmd | grep -v _test.go
internal/agent/commands.go:238:   if err := a.execReboot(); err != nil {      <- maybeRebootAfterPatch (inside `if a.config.Commands.AllowReboot`)
internal/agent/commands.go:314:   if err := a.execReboot(); err != nil {      <- handleReboot (after `if !AllowReboot { return }`)
internal/agent/commands.go:324:   func (a *Agent) execReboot() error {          <- definition
internal/agent/reboot_auth.go:234: if err := a.execReboot(); err != nil {       <- rebootAfterDelivery (after `if !AllowReboot { abort }`)

$ grep -rn "func (a \*Agent) claimPatchReboot" internal
internal/agent/reboot_intents.go:58

$ grep -n "ProtocolVersion *=" internal/maintenance/protocol.go
19:	ProtocolVersion    = 1        (protocol.go unchanged: git diff main..HEAD is empty)

$ grep -n "dangerouslySetInnerHTML\|innerHTML" web/src/utils/patchFlow.ts web/src/components/PatchConfirmOptions.tsx web/src/pages/DeviceDetail.tsx web/src/pages/FleetOverview.tsx
(no output; exit 1)
```

The first four are also enforced by tests: `TestRebootAuth_PF_C8_ExecRebootHasExactlyThreeGuardedCallSites` (Go AST) and `TestSendCommand_PF_C8_IncludeRebootClassHasExactlyTwoSetters`.

Migration check: no file under `cmd/riot-server/migrations` changed.

## Deviations from ADD

These are listed for architect review. None changes a contract in ADD §6 or a security property.

1. **`applyManualOSUpdateAuth` signature.** It returns `(*authRejection, error)` (rejection = HTTP status + code) instead of `(code string, status int, err error)`. Same three outcomes; one value instead of two for the rejection. Behavior and wire responses are exactly ADD §6.
2. **`maintenance.AptSourceEntriesUntil(root, budget, expired)`** was added next to `AptSourceEntries`. The freshness check needs a between-chunk deadline (NFR-004); `AptSourceEntries` is the nil-deadline wrapper from the ADD.
3. **`listsFreshness` keeps the ADD signature `(root, now, deadline)`.** Expiry is judged against the wall clock (`time.Now()`) between chunks, because `now` is a fixed value used only for the clock-skew guard. Tests pass a past deadline.
4. **SEC-007 (product-owner ruling) changes AD-010.** The ADD hid the stale row's security and reboot-class counts and excluded stale rows from `totalSecurity`. The ruling keeps raw security counts visible, qualified, on the device page, the panel row and the panel total. Reboot-class counts are still hidden on a stale row. Package totals exclude stale hosts, as the ADD says.
5. **ADD edited as the security review requires.** SEC-001 says to update ADD §10 R-6 and technical-writer note 9. I also updated AD-012 claim step 2 and the §9 row so the ADD does not contradict the code. No other ADD text was touched.
6. **Existing test `TestMaintenance_AC009_…` adapted.** It simulated "a fresh worker" by setting the unit active while the record still named the same, finished command. That is exactly the false "busy" that C-3 removes. The test now simulates another command's worker (record for `idB`, running). It still asserts no reboot and that the claim is not replayed.
7. **PATCH-BADGE-001 tests** updated exactly as ADD §4 lists (six tests); `TestPatchBadge_EveryReasonCodeHasATest` is untouched and passes.
8. **`PackageManager` is set in `handleOSUpdateWithOutput`** (around `runOSUpdate`) rather than inside `runOSUpdate`, so failed dnf runs also carry it and the "reboot required after run" fact is evaluated on every outcome as the ADD requires.
9. **Two extra test files** beyond ADD §4: `internal/agent/reboot_callsites_test.go` and `internal/server/handlers/include_reboot_class_sites_test.go`. They turn the DoD greps (C-8) into tests. Test-only.
10. **`parseAptUpgradable`** was extracted from `collectAPT` (behavior-identical) so the "raw count still reported while stale" AC can be tested without an `apt` binary.
11. **`reportSuppressedReboot`** lives in `reboot_intents.go` (it is the C-2 hook shared by both reboot paths), not in `reboot_auth.go`.
12. **Phase B checks `maintenanceBlockerFor` once before it takes the host reservation** (cheap early abort with the event) and again after the delivery wait. The ADD describes lock first, then the check. The guards are the same and both abort with the same event.
13. **`touchDirNoFollow` uses `futimes` (microsecond resolution)** on the verified descriptor instead of `Chtimes` (C-5 forbids following the final component).
14. **Recovered authorized results** always read "suppressed (result recovered after agent restart)", including a recovered failed run, as the ADD states. (A non-success recovered result is arguably better described as "run not successful"; kept literal.)
15. **Not done: ADD §12 note 10 / DoD item "host verification recorded".** It needs a built agent and helper on a real Debian 13 host (vermont). Not attempted: this task did not include deploying. The Docker/Linux run above covers the code paths, not the real-host facts listed there. Listed under Notes for QA.
16. **`npm run lint` does not exist** (see §4).

## Notes for QA

- **Real-host verification still open (ADD §12 note 10).** On a Debian 13 host with the new agent and helper:
  1. a no-change `sudo apt-get update` leaves the `/var/lib/apt/lists` mtime unchanged (why the helper stamp exists);
  2. `/boot` is listable by `riot` and `/proc/sys/kernel/osrelease` matches a `vmlinuz-*` name;
  3. an authorized Patch that installs a kernel reboots exactly once, the result arrives first, and after boot `reboot_required=false`, `lists_freshness=fresh`;
  4. `touch /etc/apt/preferences.d/<file>` flips the host to `stale` within one cycle;
  5. an existing v1 `patch-reboot-intents.json` is rewritten as v2 after the first claim.
- **Ships in two parts.** Everything except the post-run refresh and the lists stamp ships with the agent update. Those two need `install.sh` re-run per device (new `riot-maintenance`). With an old helper, a config rewrite during a run leaves the host `stale` until the next refresh that changes a list file (R-3).
- **A GPU-only driver update on a host that writes no `reboot-required` marker is not "reboot required"** (AD-002), so an authorized run will not reboot for it.
- **Early-return results carry no FR-023 suffix.** If `executeAction` refuses an `os_update` before running it (maintenance blocker, host lock conflict), the result is the existing conflict/precheck result without the authorization suffix. The ADD puts the suffix on results of runs that were attempted.
- **`supports_reboot_if_required` requires the `updates` collector.** With it disabled, the server answers `reboot_authorization_unsupported` ("agent must be updated"), which is misleading but safe (security review INFO-5).
- **Timing-sensitive tests** (phase B waits) use 1 to 40 ms real timers; the PF agent tests ran 6 times in a row without a failure, and under `-race` on Linux.
- **The `models.UpdateInfo` name is declared twice in `web/src/types/models.ts`** (patch telemetry at ~line 208 and the server-update info at ~line 684). TypeScript merges them; the new fields were added to the first. New web tests cast fixtures to `UpdateInfo` because of the merged required fields.
- **Test data setup:** none needed beyond temp dirs. Freshness tests build a fake apt tree with `os.Chtimes`; kernel tests build a fake `/proc` and `/boot`; the intent-store tests use the fake clock and real temp files.
- **Edge cases worth probing:** a host booted into an older kernel reports `reboot_required=true` permanently (FR-002, intended); a clock-skewed apt config file reports `unknown`, never `stale`; `repair` mode with the box ticked is rejected by the server (400 `invalid_request`).
- **No browser check was done.** UI behavior is covered by Testing Library tests only; the dev server needs a backend and was not started.
- **Environment artifacts created outside the repo:** `D:\rIOt_gocache` (Go build cache) and `D:\rIOt_fuzz_tmp` (empty temp dir for the fuzz run), because C: was nearly full at one point. The sandbox refused to delete them; both are safe for Ryan to remove.

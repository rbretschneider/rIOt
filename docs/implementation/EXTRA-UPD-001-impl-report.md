# Implementation Report

- **Story ID:** EXTRA-UPD-001
- **Title:** Generic extra-updater step (DietPi first) and the "Needs you only when work is waiting" badge rule
- **Engineer:** Senior Dev Agent
- **Date:** 2026-10-26
- **Branch:** `feat/extra-upd-001` (not pushed, not tagged, not merged)
- **Stack:** Go 1.24 module (agent, root maintenance worker, chi/pgx server), React 19 + TypeScript + Vite + vitest (web), bash installer with Python container fixtures, Postgres (no migration added).
- **Inputs:** FRD revision 2, ADD revision 3 (rev 1 text for the parts rev 3 says are "as in revision 2"), security review (SEC-001..009, OPS-001), repo `CLAUDE.md`.

---

## Hard rules, and how they were kept

| Rule | How |
|---|---|
| No test runs `dietpi-update` or any DietPi script, apt/dpkg mutation, reboot, shutdown, poweroff, real `systemctl start/stop`, or the installed `riot-agent` | Every apply path takes an injected runner, a fake `nsOps` or a fixture tree (`extraupd.DirFS` over `t.TempDir`). `TestSafety_EU_AC045_NoForbiddenProcess` scans the agent, worker and launcher tests for forbidden process names, and asserts that no test calls `RunExtra` with a `/boot/dietpi` path. Launcher tests either use the fake `nsOps` or a `/bin/sh` fixture spec. |
| Installer fixtures: any `dietpi-update` stub is a tripwire | The fake `/boot/dietpi` tree contains a tripwire `dietpi-update` (mode 0644, not executable) and every installer test asserts it was never executed. |
| Privileged real-namespace test is opt-in | `TestRunExtra_EU_OPS001_RealNamespace` runs only with `RIOT_NS_FIXTURE=1` and never by default. It was run once, by hand, in a disposable `CAP_SYS_ADMIN` container, and passed (including from a non-leader thread, SEC-009). It was not enabled in any default run below. |
| Bounded growth | Every new persisted or logged item has its bound (see "Bounds" below). |
| No new dependencies | `go.mod`, `go.sum` and `web/package.json` are unchanged (`golang.org/x/sys` was already present). |
| No migration | `git diff 08cd071..HEAD -- cmd/riot-server/migrations` is empty. See "Notes for the coordinator (PR #37)". |
| Docs prose belongs to the technical writer | README, CHANGELOG and `docs/ops/*` are untouched. Docs tests are in `scripts/extra_updaters_docs_test.go` (see below). |
| `GOCACHE=D:/rIOt_gocache`, `GOTMPDIR=D:/rIOt_fuzz_tmp` | Exported before every Go command. The web `node_modules` is a directory junction to the main checkout's (ignored by git, no install, nothing written to C:). |

---

## Commits

| Commit | Subject |
|---|---|
| `7d46398` | feat: extra-updater telemetry types, params, bounds and event fields |
| `e918a45` | feat: extra-updater contract, DietPi detector, reboot indicators and policy codec |
| `40cdd5f` | test: pin the v1 maintenance record and request bytes before restructuring Execute |
| `00d6b5e` | feat: maintenance request and record protocol v2 with strict validation |
| `36ca5c8` | feat: worker runs the extra-updater step after apt with root-side gates and truthful per-updater results |
| `bfd9e02` | feat: root-side probes, mount-namespace launcher and supervised step runner for the extra updater |
| `c5b319c` | feat: riot-maintenance --capabilities, --run-extra launcher mode and worker extra-updater wiring |
| `de5bb76` | feat: installer DietPi question, fail-closed list reader and verified root-side opt-in |
| `824b5b5` | feat: agent config, readiness, v2 requests, result lines, progress events and doctor for extra updaters |
| `b174152` | feat: server dispatch budget, group K badge, extra-updater events and scheduler trigger |
| `187b487` | feat: dashboard extra-updater list, Patch label and dialog, manual badge kind and demo data |
| `2b8ddd0` | test: documentation checks for SEC-008 and the AC-043 topics |
| (this report) | docs: implementation report |

---

## Completed Components (ADD section 4)

| Area | Files | Action |
|---|---|---|
| Models | `internal/models/extra_updaters.go` (+test), `telemetry.go`, `command_policy.go`, `events.go` | CREATE / MODIFY. `ExtraUpdaterList` lenient decoder (8 entries, id pattern, enum normalisation, string bounds); `exclude_extra_updaters`, `extra_updater_budget`, 88 min agent hard max, 95 min deadline; event type and `AgentEvent` fields. |
| Contract and detector | `internal/extraupd/{extraupd,fs,fs_linux,fs_other,dietpi,reboot,policy}.go` + tests | CREATE. `Updater` interface, `ApplySpec`, `DirFS`/`OSRoot`, decision table D1..D7, notices, 1 s detect timeout with an in-flight guard, 512 B closed JSON policy codec. |
| Worker protocol | `internal/maintenance/protocol.go` | MODIFY. `ProtocolVersion` stays 1; `RequestVersionExtra = 2`; `extra_updaters`, `requested_extra_updaters`, `apt_outcome`, `extra_updaters[]` (all omitempty, absent on v1). |
| Worker | `internal/maintenance/{worker,extra,extra_trust,extra_unit,extra_linux,runner_linux,io_linux}.go` | CREATE / MODIFY. `Execute` split into `executeApt` / `finalizeV1` (byte-identical, pinned by a golden test written first) / `executeExtras` / `finalizeV2`. Twelve ordered checks per updater. |
| Launcher | `internal/maintenance/{nsops,nsops_linux,launcher}.go`, `cmd/riot-maintenance/main_linux.go` | CREATE / MODIFY. `--capabilities`, `--run-extra <id>` (exit 125 on refusal), raw `unshare(CLONE_NEWNS)` keeping propagation, `/tmp` private + read-only + fresh tmpfs, isolation proofs, `ok` on fd 3, exec of the compiled registry spec. Mount tables read from `/proc/thread-self/mountinfo` (SEC-009) and `runtime.LockOSThread()` in `init`. |
| Installer | `scripts/install.sh`, `scripts/test_install_maintenance.py` | MODIFY. DietPi presence and question, fail-closed list reader, root-side policy file and unit drop-in (`RuntimeMaxSec=85min`, 35 min on revoke), write order (drop-in, daemon-reload, verify, policy), revoke order (policy `[]` first). No sudoers line added. |
| Agent | `internal/agent/{config,extra_updaters,extra_updaters_linux,extra_updaters_other,extra_updaters_doctor,maintenance,maintenance_linux,commands,reboot_auth,command_lifecycle,cmdstate,telemetry,doctor,agent}.go`, `collectors/{updates,kernelreboot}.go` | CREATE / MODIFY. `allow_extra_updaters`, readiness (five reasons), cached facts, v2 request, result lines, progress steps, at-most-two durable events, reboot pseudo-entry, doctor. |
| Server | `commands/service.go`, `handlers/{os_update_auth,auto_update,fleet,handlers,patch_badge,agent_event_extra}.go`, `events/generator.go`, `db/{interfaces,event_repo}.go`, `testutil/mocks.go` | MODIFY / CREATE. Budget stamp, exclusion validation, group K badge, scheduler trigger, panel rows, bulk patch, validated and deduped events. |
| Web | `types/models.ts`, `api/client.ts`, `utils/patchFlow.ts`, `utils/extraUpdaters.ts` (new), `components/{PatchBadge,PatchConfirmOptions,ExtraUpdatersList}.tsx`, `pages/{DeviceDetail,FleetOverview}.tsx`, `api/demo-data.ts` | MODIFY / CREATE. |
| Docs tests | `scripts/extra_updaters_docs_test.go` | CREATE. |

### Bounds (NFR, bounded growth)

| Item | Bound |
|---|---|
| Telemetry `extra_updaters` | 8 entries, 8 notices each, version 32, strings 64, 50 pending items, pending count 10000 (decoder and badge re-apply them) |
| Policy file | 512 B, closed JSON, 0644 root |
| Worker record | `extra_updaters` at most the 8 requested; output capped at 128 KiB per step; step limit 40 min |
| Deadlines | step 40 min < worker 80 min < backstop 85 min < agent hard max 88 min < command deadline 95 min |
| Agent events | at most two per updater per command (`ExtraEvents` at most 16 in the inflight marker); server stores one start and one end per command |
| Badge | one item per (code, updater), at most 8 extra items plus the existing codes |
| Launcher | the mount table is read once; a single tmpfs; no persistent state |
| Installer | one policy file and one drop-in; backups counted and compared in the bounded-growth test |

---

## Test Summary

Test names follow the ADD section 8 pattern (`…_EU_AC0NN_…`, `…_EU_SEC00N_…`, `…_EU_OPS001_…`; web `[EXTRA-UPD-001 AC-0NN]`; Python `test_EU_…`).

| AC / SEC / OPS | Test files | Status |
|---|---|---|
| AC-001..AC-009 detector | `internal/extraupd/dietpi_test.go` | PASS |
| AC-010, AC-011 modules missing, no new collector | `internal/agent/collectors/updates_extra_test.go`, `internal/agent/extra_updaters_test.go` | PASS |
| AC-012, AC-013 agent-side only never runs; five readiness reasons | `internal/agent/extra_updaters_test.go` | PASS |
| AC-014 unknown ids | `internal/agent/config_extra_test.go`, `extra_updaters_doctor_test.go` | PASS |
| AC-015..AC-018 installer | `scripts/test_install_maintenance.py` (`test_EU_AC015..018`), `extra_updaters_test.go` | PASS (67 tests, both images) |
| AC-019 exact command | `internal/maintenance/{extra,launcher,protocol_extra}_test.go`, `internal/agent/extra_updaters_test.go`, installer `test_EU_AC019_*` | PASS |
| AC-020..AC-030 worker | `internal/maintenance/extra_test.go`, `extra_trust_test.go`, `extra_unit_test.go` | PASS |
| AC-031..AC-034 reboot and events | `internal/agent/extra_updaters_test.go`, `internal/server/handlers/extra_updaters_server_test.go` | PASS |
| AC-035..AC-038 server | `internal/models/extra_updaters_test.go`, `handlers/extra_updaters_server_test.go`, `handlers/patch_badge_extra_test.go`, `commands/service_extra_test.go`, `events/generator_extra_test.go` | PASS |
| AC-039..AC-041 web | `ExtraUpdatersList.test.tsx`, `extraUpdaters.test.ts`, `PatchConfirmOptions.extra.test.tsx`, `DeviceDetail.extra.test.tsx`, `demo-data.extra.test.ts`, `ActivityLog.extra.test.tsx` | PASS |
| AC-042 doctor | `internal/agent/extra_updaters_doctor_test.go` | PASS |
| AC-043 docs | `scripts/extra_updaters_docs_test.go` | negative SEC-008 check PASS; positive topic checks SKIP until the docs exist (see QA notes) |
| AC-044 unchanged paths | `internal/maintenance/golden_v1_test.go`, `extra_updaters_test.go`, installer `test_EU_AC044_*` | PASS |
| AC-045 safety scan | `internal/agent/extra_safety_test.go` | PASS |
| AC-046..AC-055 group K | `handlers/patch_badge_extra_test.go`, rewritten `patch_badge_test.go` / `fleet_patch_badge_test.go`; web `PatchBadge.extra.test.tsx`, `FleetOverview.extra.test.tsx` | PASS |
| SEC-001..SEC-009, OPS-001 | `launcher_test.go`, `extra_test.go`, `extra_trust_test.go`, `extra_unit_test.go`, `nsops_test.go`, `handlers/extra_updaters_server_test.go`, `commands/service_extra_test.go`, installer `test_EU_SEC002_*`, `scripts/extra_updaters_docs_test.go` | PASS |

### Test run output

**1. Windows, `go test ./... -count=1`** (30 packages `ok`, no failures; abridged):

```
ok  	github.com/DesyncTheThird/rIOt/internal/agent	23.639s
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	12.550s
ok  	github.com/DesyncTheThird/rIOt/internal/extraupd	1.951s
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	2.481s
ok  	github.com/DesyncTheThird/rIOt/internal/models	0.904s
ok  	github.com/DesyncTheThird/rIOt/internal/server/commands	1.836s
ok  	github.com/DesyncTheThird/rIOt/internal/server/events	0.752s
ok  	github.com/DesyncTheThird/rIOt/internal/server/handlers	10.060s
ok  	github.com/DesyncTheThird/rIOt/scripts	52.494s
```

`go vet ./...` and `GOOS=linux go vet ./...`: no output, exit 0.

**2. Linux container (`golang:1.24`, `-race -count=1`, network off, source mounted read-only):**

```
ok  	github.com/DesyncTheThird/rIOt/internal/maintenance	14.156s
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-agent-apply	5.521s
ok  	github.com/DesyncTheThird/rIOt/cmd/riot-maintenance	1.031s
ok  	github.com/DesyncTheThird/rIOt/internal/extraupd	1.935s
FAIL	github.com/DesyncTheThird/rIOt/internal/agent	132.668s     (one test, below)
ok  	github.com/DesyncTheThird/rIOt/internal/agent/collectors	82.983s
ok  	github.com/DesyncTheThird/rIOt/internal/models	1.090s
ok  	github.com/DesyncTheThird/rIOt/internal/server/...  (all 20 packages ok)
```

The one failure is `TestShutdown_SD_AC030_LateNonZeroExitLogsOneWarn/a_late_refusal` (expected 1 warn line, got 0). It fails the same way, 10 of 10 runs, on the merge base `08cd071` (PR #36) extracted with `git archive` and run in the same container, so it is pre-existing and not caused by this story. It passes on Windows. Every other test in `./internal/agent` passes under `-race`.

**3. Installer fixtures (`RIOT_INSTALLER_FIXTURE=1 python3 scripts/test_install_maintenance.py -v`):**

```
=== riot-fixture:bookworm
Ran 67 tests in 61.814s
OK
=== riot-fixture:trixie
Ran 67 tests in 67.048s
OK
```

`bash -n scripts/install.sh` (LF) passes.

**4. Web:**

```
npm run test:run   Test Files 103 passed (103)   Tests 1767 passed (1767)
npm run build      tsc clean, vite build: built in 6.05s
```

Before this story's web tests there were 95 files and 1707 tests, so the story adds 8 files and 60 tests.

---

## Deviations from ADD

These trigger architect review.

1. **Fixtures are built programmatically.** The ADD lists checked-in `testdata/dietpi/<case>` trees. They are built in `t.TempDir()` by `fixture_test.go` instead, because the repo uses `core.autocrlf=true` and CRLF conversion would silently corrupt `.version` and `dietpi.txt` fixtures. The trip-wire stays mode 0644.
2. **Launcher portability.** `nsops.go` (the interface) and `launcher.go` (the logic) are portable and unit-tested on Windows with the fake `nsOps`; only `nsops_linux.go` has the real syscalls. There is no `nsops_other.go`.
3. **Extra tests are in new files** (`protocol_extra_test.go`, `config_extra_test.go`, `patch_badge_extra_test.go`, `*.extra.test.tsx`, `extraUpdaters.test.ts`) instead of editing the existing test files named in the ADD, to keep rebases small. The AC-named tests are the ones ADD section 8 lists, except where noted below.
4. **Files modified or created outside ADD section 4:** `internal/agent/agent.go` (extra-updater state field), `internal/agent/reboot_auth.go` (passes the command row into `handleOSUpdateWithRun`), `internal/server/handlers/agent_event_extra.go` (new: the validated, deduped extra-updater event receiver; `handlers.go` only routes to it), `web/src/utils/extraUpdaters.ts` (new: pure helpers so `patchFlow.ts` stays small), `web/src/components/ActivityLog.extra.test.tsx`.
5. **Existing badge tests rewritten to group K.** `patch_badge_test.go` and `fleet_patch_badge_test.go` assumed "any reason means Needs you". They now run against `waitingInput()` (a host with work waiting) and check information items separately. `TestPatchBadge_EveryReasonCodeHasATest` covers action and information-only codes. This is the intended behaviour change of group K, not a regression.
6. **Recovery test name.** `TestRecover_EU_AC029_NoSecondRequest` in the ADD map is `TestExtras_EU_AC029_RecoverNeverReruns` (and `…_PersistenceFailureStartsNothing`).
7. **Installer transparency line** prints only on DietPi hosts or when the config lists `dietpi`; a non-DietPi host that does not list it prints nothing (AC-017 requires that nothing is recorded; printing there added noise with no meaning). The agreement check is merged into `require_shutdown_agreement`, so the existing "four `restore_policy` call sites" test still holds.
8. **Seams.** The worker is wired through `ExtraEnv` and `StepRunner` seams so the launcher can be replaced by a fake in every test; the production entry admits only the registry spec.
9. **Doctor** reads helper and unit facts through injected probes, never by running an updater.
10. **Pre-existing flakes, not touched:** `TestShutdown_SD_AC030_LateNonZeroExitLogsOneWarn` (fails on the merge base in the Linux container, see above), and `TestDUX_AC165_QuotaStdoutSample` / `internal/server/quota` (rare on Windows).
11. **Docs tests.** The AC-043 positive topic checks cannot pass before the docs exist, so they skip with an explicit message until README or `os-maintenance.md` names `commands.allow_extra_updaters`.

---

## Notes for the coordinator (PR #37, EXPECT-001)

- **Migrations:** this story adds none. If #37's `000033_expected_activity` merges first there is nothing to renumber.
- **No parallel expected-activity mechanism.** The extra-updater step rides inside the `os_update` command. Its start and end reporting is the `extra_dietpi` / `extra_dietpi_<outcome>` progress steps plus the two deduped agent events (`extra_updater_started`, `extra_updater_finished`). `TestNoExpectCode_EU_AC034` asserts the build has no EXPECT-001 code.
- **Where #37's expect window should consume the extra-updater deadline.** The single source of the longer deadline is `execDeadlineSeconds` in `internal/server/commands/service.go`, called from `Dispatch`. It stores `ExecDeadlineS = 95 min` (`models.OSUpdateExtraExecDeadline`) on the command row when `extra_updater_budget` is stamped, and the default (`models.ExecDeadlineSeconds`, 40 min for `os_update`) otherwise. #37's per-command window must derive from the stored `Command.ExecDeadlineS` (and `ExpiresAt`), not from a constant, so an extended os_update gets a 95 minute window. The same applies to the auto-patch path: `checkAutoPatch` dispatches through `Dispatch`, so an auto-patch run that includes DietPi is stamped and extended the same way.
- **Overlap files, kept minimal.** In `commands/service.go` the change is one call (`prepareParams`) and one call (`execDeadlineSeconds`) plus two new functions. `db/interfaces.go` and `testutil/mocks.go` gain one method (`HasCommandEvent`). `events/generator.go` gains one constant and four lines in `CommandSent`. `handlers/fleet.go` and `handlers/handlers.go` gain additive blocks (`pendingExtraUpdaters`, the event routing and one mutex field). New logic lives in new files (`agent_event_extra.go`); `patch_badge.go` is the one large rewrite (the group K badge), and #37 is not described as touching it. Expect small textual conflicts in `interfaces.go` and `mocks.go` (both append to the same interface and mock); keep both methods.
- **`HasCommandEvent`** is a new method on `EventRepository`, `EventRepo` (pgx), `MockEventRepo` and the generator test's mock. Its query is `SELECT EXISTS (... FROM events WHERE device_id=$1 AND command_id=$2 AND type=$3)`. It uses the existing `events.command_id` column (migration 000032), so there is no schema change and no new index. It runs at most twice per command and the rows are bounded by the 90-day event retention. An index on `events(command_id)`, if #37 adds one, helps it without any change.

---

## Notes for QA

- **Docs (technical writer):** `scripts/extra_updaters_docs_test.go` holds `TestDocs_EU_AC043_OpsDocCoversEveryTopic`, `…_ReadmeStepsPerServer`, `…_ChangelogEntry` and `…_SecurityStatements`. They skip today. When the docs land they run on every build and check every FR-063 topic by plain phrases, the FR-064 per-server steps (hawaii, vermont, PiVPN, PiCUPS, the single re-run covering AGENT-HARDEN-001, PATCH-FLOW-001 and SHUTDOWN-SUDO-001, the reboot after the 9 to 10 update), the FR-065 entry, and the owner rule. `TestDocs_EU_SEC008_NoManualCopyOfVarTmpDietpi` already runs: the docs must never tell the operator to copy `/var/tmp/dietpi` content.
- **Real-namespace launcher test:** `RIOT_NS_FIXTURE=1 go test -run TestRunExtra_EU_OPS001_RealNamespace ./internal/maintenance` needs `CAP_SYS_ADMIN` and Linux; run it only in a disposable privileged container. Never on a workstation.
- **Pi hardware only:** DietPi 10's `rpi_firmware_migration` may change `/boot` to `/boot/firmware` from inside the service. With propagation kept shared (OPS-001), the host should see it. Fixtures prove the call sequence, not the real kernel on a Pi. Check on the first real PiVPN run, and confirm the reboot the migration requires is offered (the `dietpi` pseudo-entry in the reboot class).
- **Installer on real hosts:** the first re-run on each host also applies pending AGENT-HARDEN-001, PATCH-FLOW-001 and SHUTDOWN-SUDO-001 changes. Doctor reports `riot-maintenance --capabilities` and the effective unit (`PrivateTmp=no`, empty `MountFlags`, `RuntimeMaxUSec` at least 83 min 30 s).
- **Edge cases worth probing:** an `os_update` dispatched while the host's telemetry is older than a revocation (the worker re-checks everything and skips with `launcher_refused` or `not_enabled`); two DietPi events delivered twice (second is `200 duplicate`); `exclude_extra_updaters` in a case variant (400, no command); a stamped command whose agent is restarted mid-step (recovery never reruns the updater, `TestExtras_EU_AC029_RecoverNeverReruns`); a non-DietPi apt host (request, record and sudoers equal the golden bytes).
- **Group K:** vermont (auto-patch off, nothing waiting) now reads `Manual`, neutral, with `auto_patch_off` listed under "Information". Fleet "N hosts need you" counts only `needs_you`. The Automatic, Reboot and As-of lines are asserted equal to a frozen copy of the pre-story logic in `TestPatchBadge_EU_AC055_EachCodeOnceAndOtherLinesUnchanged`.
- **Windows tooling:** the worktree is CRLF on disk and LF in the index; new files were written LF and git normalises them.

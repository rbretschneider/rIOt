# PATCH-GATE QA Report

**Story ID:** PATCH-GATE
**Title:** Reboot-Class Package Gating — OS-Level Holds, Maintenance-Window Apply, and Auto-Reboot
**QA Engineer:** QA Agent
**Date:** 2026-08-25
**Verdict:** PASS WITH NOTES

---

## Environment

- **Host:** Windows 11 (Go tests run without `-race`; CGO not required for this suite).
- **Go:** `go test ./...` run fresh with `-count=1`.
- **Frontend:** `web/` via `npm run test:run` (vitest), `npx tsc --noEmit`.
- **Commit state:** PATCH-GATE commits present; working tree clean (verified `git status cmd/riot-server/migrations/` is clean — AC-024 no-migration clause).

---

## Test Run Summary

| Command | Result |
|---|---|
| `go build ./...` | exit 0 — clean |
| `go vet ./...` | exit 0 — no diagnostics (empty output) |
| `go test ./... -count=1` | exit 0 — all packages pass (16 test packages `ok`; 5 `[no test files]`) |
| `cd web && npm run test:run` | exit 0 — **24 files, 338 tests passed** |
| `npx tsc --noEmit` | exit 0 — clean |

Notes:
- The frontend run emits `ECONNREFUSED 127.0.0.1:3000` stderr noise from unrelated test files whose setup attempts a network call. This is a pre-existing condition (documented in the POOL-001 QA report), not a test failure — all 338 tests pass.
- Go test output was verified from a fresh `-count=1` run (not cache).
- The `338` frontend count matches the implementation report exactly.

---

## AC Coverage Audit

All 24 functional ACs and 5 security ACs have named tests. The backend and security-critical paths are exercised by behavior-level tests with injected command runners and temp state dirs (no real OS/network/FS). Three device-level UI surfacing behaviors are implemented but lack component tests — verified by code inspection and flagged as notes below.

| AC | Test(s) | Status |
|---|---|---|
| AC-001 apt GPU driver classification + GPU-over-kernel precedence | `rebootclass_test.go` `[AC-001]` (asserts `nvidia-dkms-550` is `gpu_driver`, not `kernel`) | PASS |
| AC-002 apt kernel classification incl. `-dkms` | `rebootclass_test.go` `[AC-002]` | PASS |
| AC-003 dnf GPU driver classification | `rebootclass_test.go` `[AC-003]` | PASS |
| AC-004 dnf kernel classification | `rebootclass_test.go` `[AC-004]` | PASS |
| AC-005 standard packages unclassified; single shared table | `rebootclass_test.go` `[AC-005]`; `updates_test.go` `[AC-005]` (both apt+dnf paths call `ClassifyPackage`; `PendingRebootClassCount` aggregate) | PASS |
| AC-006 dead kernel fields populated | `updates_test.go` `[AC-006]` (both directions: pending ⇒ true + version; none ⇒ false/empty) | PASS |
| AC-007 scoring kernel check no longer silently passes | `engine_test.go` `[AC-007]` (`engine.go` unchanged, `noKernel := !upd.PendingKernelUpdate`; true ⇒ `Passed=false`, false ⇒ pass) | PASS |
| AC-008 apt holds applied + recorded rIOt-managed | `holds_test.go` `[AC-008]` (batched `apt-mark hold`, state records both) | PASS |
| AC-009 dnf5 fragment written via fixed-path install; no user file touched; byte-identical regen | `holds_test.go` `[AC-009]` + `[NFR-003]` | PASS |
| AC-010 release before upgrade, re-apply on every exit incl. failure | `holds_test.go` `[AC-010]` (marker-first ordering, abort-when-unwritable); `commands_test.go` `[AC-010]` (defer re-applies on the failure path) | PASS |
| AC-011 disable removes only rIOt holds; operator hold survives; dnf fragment deleted | `holds_test.go` `[AC-011]` (apt: `postgresql-16` untouched; dnf: fragment removed) | PASS |
| AC-012 manual/unattended out-of-window cannot pull reboot-class | Delegated to OS hold semantics (A-001); covered by AC-008/009 (holds present) + AC-010 (re-applied) + AC-013 (never released out-of-window). Live `unattended-upgrades` is out of unit-test reach. | PASS (by composition) |
| AC-013 out-of-window/manual runs exclude reboot-class | `auto_update_test.go` `[AC-013]` (gated+in-window sets param; off never sets); `commands_test.go` server `[AC-013]` (SendCommand strip); `commands_test.go` agent `[AC-013]` (no param ⇒ no release) | PASS |
| AC-014 in-window apply then auto-reboot when allowed | `commands_test.go` `[AC-014]` (message + `RebootPending`; `maybeRebootAfterPatch` invokes reboot via fake runner) | PASS |
| AC-015 no reboot permission ⇒ event, no reboot | `commands_test.go` `[AC-015]` (no reboot exec, telemetry trigger fired, "not permitted" message); `reboot_required_test.go` event | PASS |
| AC-016 no reboot when no reboot-class package applied | `commands_test.go` `[AC-016]` (unchanged versions ⇒ no reboot; failure-before-apply ⇒ no reboot) | PASS |
| AC-017 apt reboot-required detection | `updates_test.go` `[AC-017]` (marker+pkgs ⇒ true w/ reasons; absent ⇒ false; marker w/o pkgs ⇒ true) | PASS |
| AC-018 dnf reboot-required detection | `updates_test.go` `[AC-018]` (exit 1 ⇒ true; exit 0 ⇒ false; missing command ⇒ false, no WARN/ERROR spam) | PASS |
| AC-019 reboot_required event once per transition | `reboot_required_test.go` `[AC-019]` (false→true=1; true×N=1; refires after clear; notification eligibility with/without rule; per-device isolation; template shape) | PASS |
| AC-020 reboot-class badges on update rows | `RebootClassBadge.test.tsx` `[AC-020]` (gpu_driver/kernel/standard; distinct violet/amber styling); badge wired into `DeviceDetail.tsx:882` and the Fleet patch modal | PASS |
| AC-021 held packages + reboot-required in UI/API | `fleet_handler_test.go` `[AC-021]` (API `reboot_class_count`/`reboot_required`, absent ⇒ zero); `FleetOverview.test.tsx` `[AC-021]` (fleet badge/count + per-package badges). **Device-view "Held by rIOt" section + reboot chip implemented but untested** — see Note 1. | PASS WITH NOTE |
| AC-022 GPU container count + per-container badge | `docker_test.go` `[AC-022]` (`usesGPU` 7-fixture truth table); `CompactContainerTile.test.tsx` `[AC-022]` (badge true/false/absent). **Device-view "N GPU containers" count + absence case implemented but untested** — see Note 2. | PASS WITH NOTE |
| AC-023 feature off ⇒ behavior unchanged | `holds_test.go` `[AC-023]` (disabled+no-state ⇒ zero runner calls, zero files); `commands_test.go` `[AC-023]` (no param ⇒ no hold/snapshot calls, pre-story "Updated 1 packages" message shape); `auto_update_test.go` default params | PASS |
| AC-024 old-agent/old-server compat; no migration | `handlers_test.go` `[AC-024]` (pre-PATCH-GATE payload ⇒ 200, zero-value decode, no WARN; new UpdateInfo ⇒ old-shape struct decodes clean); migrations dir latest is `000021`, no PATCH-GATE file, git clean | PASS |

**Coverage: 29 / 29 ACs covered (24 functional + 5 security).** All green; three device-level UI surfacing tests are gaps (verified by inspection).

---

## Security AC Verification (deep review)

I read the actual source and test bodies for every security-critical behavior.

### SEC-AC-001 — Fail-closed privilege preflight + operator-visible status
**PASS (backend); UI-surfacing test is a note.**
`HoldManager.reconcileLocked` (`internal/agent/collectors/holds.go:349-358`) calls `VerifyPrivileges` **before any mutation**. On probe failure it sets `status = no_privilege`, clears `held`, and returns — **no `apt-mark`/fragment call is attempted**. `HeldPackages()` returns `nil` unless status is `active` (holds.go:131-140), so an unenforced host can never present holds as "protected". Verified by `holds_test.go` `[SEC-AC-001]`: 3 reconcile cycles with `failProbe=true` produce zero mutating sudo calls (only `-l` probes), status `no_privilege`, exactly one ERROR per transition, then recovery to `active`. The dnf4-unsupported path is separately tested (status `unsupported`, no writes, one WARN/process).
*Note:* the corresponding UI warning ("Hold enforcement inactive", `DeviceDetail.tsx:857-861`) consumes the status field correctly but has no component test — see Note 3.

### SEC-AC-002 — Fixed-path, argument-locked privileged writer
**PASS.** `scripts/install.sh:441-469` writes exactly four rules, all resolved-full-path and argument-locked:
- `apt-mark hold *` / `apt-mark unhold *` — subcommand locked (wildcard only on the package-name arg, which is charset-validated agent-side).
- `install -m 0644 -o root -g root /var/lib/riot/dnf-holds.staged /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` — **both paths fixed, zero variable arguments**.
- `rm -f /etc/dnf/libdnf5.conf.d/60-riot-holds.conf` — exact-path.

No `sh -c`, no `tee`, no wildcard path component in any hold rule. `holds_test.go` `[SEC-AC-002]` captures every sudo invocation across a full reconcile+release+disable cycle (apt and dnf) and asserts the only shapes are the locked forms above, that fragment content never appears in argv (staged/stdin only), and that `sh -c`/`tee` never appear. The pre-existing `sudo sh -c *` agent-update rule (install.sh:472-473) is untouched by this story and was explicitly excluded from precedent by the security review.

### SEC-AC-003 — Package-name charset validation before argv/fragment/state
**PASS.** `ValidPackageName` (`rebootclass.go:24-30`, `^[A-Za-z0-9][A-Za-z0-9.+:~_-]*$`) is enforced in `desiredHoldSet` (holds.go:396-399) before any name reaches `apt-mark` argv, the staged fragment, or `holds.json`, and again on the release path. The leading-alphanumeric anchor blocks option injection (`-o`); the charset excludes `,`, newline, `[`, `]`, `=`, `#`, whitespace, so no name can break out of the single `excludepkgs=` ini value. `holds_test.go` `[SEC-AC-003]` feeds `evil,pkg`, `bad\nname`, `[main]x`, `-o`, `nvidia-driver-550,extra` and asserts none reach apt-mark argv, the dnf fragment, or state, each skip WARN-logged. `rebootclass_test.go` covers the `ValidPackageName` truth table directly.

### SEC-AC-005 — Release path fails closed
**PASS.** `ReleaseForRun` (holds.go:224-272): a state load/parse error returns `nil` (**release aborted, nothing unheld**); each candidate is released only if it is **both** state-tracked **and** `ClassifyPackage(name) != ""` (cross-check against a poisoned entry); the crash marker is persisted **before** the first unhold, and a marker-persist failure also aborts. `holds_test.go` `[SEC-AC-005]`: a poisoned `postgresql-16` entry is never unheld (only `nvidia-driver-550` released, skip WARNed); a corrupt-JSON state file yields `released=nil` with zero unhold calls.

### Auto-reboot gate (NFR-002 / FR-024–FR-025)
**PASS.** `maybeRebootAfterPatch` (commands.go:144-166) reboots only when `len(RebootClassApplied) > 0` **and** `a.config.Commands.AllowReboot`. `RebootClassApplied` is computed from the released set via before/after version snapshot (`changedVersions`), so it is non-empty only when a reboot-class package's version **actually changed** during a gated run; a failed snapshot yields an empty set ⇒ no reboot (fail-safe). Both reboot call sites route through `execReboot`, which is reached only after an `AllowReboot` check (commands.go:210-225). No command parameter reaches the reboot exec. The reboot decision runs **after** the command result is sent (commands.go:132-136), so the run stays auditable. Covered by `commands_test.go` `[AC-014/015/016]`.

### Manual-dispatch strip (FR-023 / BR-005)
**PASS.** `SendCommand` (`handlers/commands.go:54-56`) deletes `include_reboot_class` from params when `Action == "os_update"`. `checkAutoPatch` (`auto_update.go:492-495`) is the **only** site that sets the param, gated on `RebootClass == "gated"` and reached only after the in-window early-return. `BulkPatchDevices` never sets it (grep confirms `include_reboot_class` is set at exactly one location in `internal/`). Tested by `commands_test.go` `[AC-013]` (os_update stripped; non-os_update pass-through) and `auto_update_test.go` `[AC-013]`.

### Backward compatibility / no migration (AC-024)
**PASS.** Both directions tested in `handlers_test.go` `[AC-024]`. All new telemetry/automation fields are `omitempty` additive JSON. No new migration file: latest is `000021_docker_auto_update_container_count`; `git status cmd/riot-server/migrations/` is clean.

---

## Test-Quality Findings (CLAUDE.md standards)

- **No time bombs.** No `time.Now()` gating in testable logic; `saveState` timestamps are not asserted against wall-clock. Tests use injected `CommandRunner` fakes and `t.TempDir()`.
- **No real network / FS side effects.** All hold, doctor, and command tests inject a fake runner; file operations occur inside `t.TempDir()`. The dnf fragment install/rm is simulated inside the temp dir.
- **No DB state bleed.** Event/handler tests use in-memory mock repos reset per test; each `t.Run` constructs fresh fixtures.
- **AC-named tests.** Every AC/SEC-AC has a test whose name carries the `[AC-xxx]`/`[SEC-AC-xxx]` reference; the coverage audit was mechanical.
- **One concept per test.** Subtests split success/failure/edge paths (e.g. AC-010 marker ordering vs. abort-when-unwritable; AC-019 four separate transition scenarios).
- **Log-content assertions** use a captured slog handler (holds_test.go, updates_test.go) rather than swallowing output — the "no WARN/ERROR spam" clauses (AC-018, SEC-AC-001) are actually asserted.

Minor: `doctor_test.go` `[SEC-AC-004]` exercises the real `VerifyPrivileges` argv shape (the load-bearing part) but asserts the remediation text by calling the `fail`/`warn` helpers directly rather than invoking `checkHoldEnforcement()` end-to-end. This mirrors the pre-existing AC-031 doctor-test approach (exec not injectable in `doctor.go`). Non-blocking; the probe argv and the fixed-path constants are genuinely verified, and `checkHoldEnforcement` (`doctor.go:263-292`) was confirmed by inspection to call `VerifyPrivileges` and emit the asserted remediation string.

---

## Notes (non-blocking) — routed to engineering

The device-detail page (`web/src/pages/DeviceDetail.tsx`) implements three PATCH-GATE surfacings that the ADD's own Section 8 test strategy assigned to `DeviceDetail.test.tsx`, but no component tests were added for them. Behavior verified correct by code inspection; missing tests are the same class of gap flagged (and back-filled) in POOL-001 QA. Per the QA mandate I did **not** add tests.

1. **AC-021 device view — "Held by rIOt" held-packages section (`DeviceDetail.tsx:895`) and the reboot-required header chip (`:295`).** Only the fleet-level surfacing (`FleetOverview.test.tsx`) and the API fields (`fleet_handler_test.go`) are tested. FR-016/FR-031 (held packages "awaiting a maintenance window" on the device view) has no rendering test.
2. **AC-022 device view — the "N GPU containers" count indicator (`DeviceDetail.tsx:333`) and its absence case.** Only the per-container tile badge (`CompactContainerTile.test.tsx`) and the `usesGPU` helper (`docker_test.go`) are tested; the device-level count and the "no GPU containers ⇒ no indicator" clause of AC-022 are untested.
3. **SEC-AC-001 UI half — the "Hold enforcement inactive" warning (`DeviceDetail.tsx:857-861`) for `no_privilege`/`unsupported`, and its absence for `active`.** The ADD Section 8 explicitly specified this `DeviceDetail.test.tsx` test. The backend status field is fully tested; the operator-visible warning that consumes it is not. This is the UI half of a **required** security mitigation (SEC-PATCH-GATE-001 mitigation 3 — "empty HeldPackages must never read as protected"), so its test is the highest-value of the three to add.

**Recommended follow-up (each a small RTL test in `web/src/pages/DeviceDetail.test.tsx`):**
- `[AC-021]` render with `held_packages` ⇒ "Held by rIOt" list + count; with `reboot_required: true` ⇒ header chip.
- `[AC-022]` render telemetry with 3 GPU containers ⇒ "3 GPU containers"; zero ⇒ indicator absent.
- `[SEC-AC-001]` render with `hold_enforcement: 'no_privilege'` and `'unsupported'` ⇒ warning present; `'active'` ⇒ warning absent.

---

## Verdict Rationale

**PASS WITH NOTES.**

All five test/lint gates are green (`go build`, `go vet`, `go test -count=1`, vitest 338, `tsc`). All 24 functional ACs and all 5 required security ACs are covered by named tests, and the security-critical agent/server logic — fail-closed preflight, fixed-path sudoers, charset validation, poisoned-state release refusal, the absolute reboot veto, the server-side manual strip, and old/new compatibility with no migration — is verified both by rigorous behavior-level tests and by direct source inspection. The five security-review mitigations (SEC-PATCH-GATE-001..005) are verifiably implemented.

The notes are three device-level UI surfacing behaviors that are implemented correctly but lack component tests the ADD had assigned to `DeviceDetail.test.tsx`. None affects backend correctness or the security guarantees; all are verifiable by inspection today. This matches the established PASS WITH NOTES bar (POOL-001 flagged and back-filled the identical class of gap). The story may merge; the three RTL tests above — SEC-AC-001's warning first — should be filed as a fast-follow.

---

## Blocking Findings

None.

# Implementation Report

**Story ID:** LOG-001
**Title:** Near-Real-Time Auth Failure Alerting
**Engineer:** Senior Dev Agent
**Date:** 2026-04-20

---

## Detected Stack

- **Go:** 1.24.0 (module `github.com/DesyncTheThird/rIOt`)
- **Frontend:** React 18 + TypeScript + Tailwind CSS (Vite, Vitest, @testing-library/react)
- **Database:** PostgreSQL 16 (no migration required for this story)
- **Test runners:** `go test ./...` (Go), `vitest run` (frontend)
- **HTTP router:** chi v5; **DB driver:** pgx v5

---

## Baseline Test Counts (pre-implementation)

- Go: all packages `ok` (no failures)
- Frontend: 15 test files, 229 tests passed

---

## Files Created / Modified

| Action | File Path | Description |
|--------|-----------|-------------|
| CREATE | `internal/agent/collectors/auth_counter.go` | `authFailureCounter` type with `Add`, `Drain`, `MarkReady`, `IsReady`. Mutex-based, race-safe. File-level comment contains SEC-005 serialization invariant. |
| CREATE | `internal/agent/collectors/auth_counter_test.go` | Unit tests: Add/Drain semantics, MarkReady latch idempotency, concurrent Add race-free. |
| CREATE | `internal/agent/collectors/auth_match.go` | Unexported `matchesAuthFailure(raw map[string]interface{}) bool` implementing AD-004 origin filter (`_UID=0` + allow-listed `_SYSTEMD_UNIT`/`SYSLOG_IDENTIFIER`) AND content filter (4 auth-failure substrings). |
| CREATE | `internal/agent/collectors/auth_match_test.go` | Table-driven tests: all 7 SEC-001 regression cases specified in ADD Section 4; all 4 content patterns; all 5 allowed units; all 4 allowed identifiers. |
| MODIFY | `internal/agent/collectors/logs.go` | Added `authCounter *authFailureCounter` field. Extracted `parseAndCount([]byte)` helper for testability (ADD Section 12 note 2). `Collect` calls `parseAndCount`, then `MarkReady` on non-first-scan intervals. |
| CREATE | `internal/agent/collectors/logs_test.go` | Tests for `parseAndCount`: 4-pattern count, forged logger rejection, 10-duplicate no-dedup, MarkReady not called by parseAndCount, empty output handling, lastSeen advance, malformed JSON skip. |
| MODIFY | `internal/agent/collectors/security.go` | Added `authCounter *authFailureCounter` field. On Linux: drains counter; if `IsReady()` sets `FailedLoginsInterval = &n`, else sets `&zero` (FR-005). Removed `intPtr` helper (conflict with gpu_test.go; uses inline address-of instead). |
| CREATE | `internal/agent/collectors/security_test.go` | Tests for nil-on-non-Linux, JSON omitempty, AC-001 field present in JSON, AC-005 not-ready/ready drain logic, AC-009 24h independence, AC-004 structural proof, AC-010 Name() unchanged. |
| MODIFY | `internal/agent/collectors/collector.go` | Constructed shared `authFailureCounter` in `RegisterDefaultsWithDocker`. Reordered: `LogsCollector` before `SecurityCollector` (AD-002). Added ordering + serialization-invariant comment block (SEC-005). |
| MODIFY | `internal/agent/collectors/collector_test.go` | Added AC-010 test (security collector present after filter) and AD-002 ordering assertion (logs before security in registered order). |
| MODIFY | `internal/agent/agent.go` | Added `slog.Info("auth failure detector initialized", ...)` after collector setup (SEC-002 deferred). |
| MODIFY | `internal/agent/doctor.go` | Added `checkJournalAccess()` function (AD-010, SEC-003) and call site when `logs` or `security` collector is enabled on Linux. Warns with remediation text (`sudo usermod -a -G systemd-journal riot && sudo systemctl restart riot-agent`) when journal returns empty and user is not in `systemd-journal` group. |
| MODIFY | `internal/agent/doctor_test.go` | Added AC-031 tests: warn text contains remediation, pass text format, collectorDeps contains journalctl for both `logs` and `security`. |
| MODIFY | `internal/models/telemetry.go` | Added `FailedLoginsInterval *int \`json:"failed_logins_interval,omitempty"\`` to `SecurityInfo` after `FailedLogins24h`. |
| MODIFY | `internal/models/events.go` | Added `EventAuthFailure EventType = "auth_failure"` and `EventDetectorInitialized EventType = "detector_initialized"`. |
| MODIFY | `internal/server/events/generator.go` | Added nil-guarded `data.Security.FailedLoginsInterval` block in `CheckTelemetryThresholds` calling `evaluateMetric` with `max(0, v)` clamp (SEC-006, AD-005). |
| MODIFY | `internal/server/events/templates.go` | Appended `auth_failure` template entry per AD-006 with SEC-004 operator-facing warning in Description. |
| CREATE | `internal/server/events/auth_failure_templates_test.go` | AC-020: all field values verified; AC-023: `security` category present; AC-030: `log_errors` template present with correct name. |
| CREATE | `internal/server/events/auth_failure_generator_test.go` | AC-001/AC-024: fires event; AC-008: nil field/nil Security → no event; AC-024: cooldown, device scope; SEC-006: negative clamped to 0, `>0` and `<0` rules both silent; AC-001: value==threshold with `>` does not fire. |
| MODIFY | `web/src/types/models.ts` | Added `failed_logins_interval?: number` to `SecurityInfo` interface. |
| MODIFY | `web/src/pages/settings/AlertRuleSettings.test.tsx` | Added LOG-001 tests: AC-030 (`log_errors` visible), AC-023 (`security` section header), AC-021 (global scope / Create form opens), AC-022 (cooldown editable), AC-025 (severity editable). |

---

## Journalctl Grep Check (ADD Section 13, Definition of Done)

```
internal/agent/collectors/logs.go:45:    out, err := exec.CommandContext(ctx, "journalctl", ...
internal/agent/collectors/security.go:81: if out, err := exec.CommandContext(ctx, "journalctl", "--since", "24 hours ago", ...
internal/agent/doctor.go:256: out, err := exec.Command("journalctl", "--priority=0..6", "-n", "1", "--no-pager").Output()
```

- `logs.go`: 1 invocation (the existing LogsCollector hot-path read — unchanged)
- `security.go`: 1 invocation (the pre-existing 24h `FailedLogins24h` call — unchanged)
- `doctor.go`: 1 invocation (AD-010 preflight — not in telemetry hot path; only runs during `riot-agent doctor`)

NFR-002 satisfied: zero new journalctl invocations for the per-interval count.

---

## Test Output — New Tests Only

```
=== RUN   TestAC005_AuthCounter_NotReadyBeforeMarkReady
--- PASS: TestAC005_AuthCounter_NotReadyBeforeMarkReady (0.00s)
=== RUN   TestAC005_AuthCounter_ReadyAfterMarkReady
--- PASS: TestAC005_AuthCounter_ReadyAfterMarkReady (0.00s)
=== RUN   TestAC005_AuthCounter_MarkReadyIsIdempotent
--- PASS: TestAC005_AuthCounter_MarkReadyIsIdempotent (0.00s)
=== RUN   TestAuthCounter_DrainReturnsValueAndResetsToZero
--- PASS: TestAuthCounter_DrainReturnsValueAndResetsToZero (0.00s)
=== RUN   TestAuthCounter_AddAccumulates
--- PASS: TestAuthCounter_AddAccumulates (0.00s)
=== RUN   TestAC007_AuthCounter_ConcurrentAddIsRaceFree
--- PASS: TestAC007_AuthCounter_ConcurrentAddIsRaceFree (0.00s)
=== RUN   TestAuthCounter_DrainOnEmptyCounterReturnsZero
--- PASS: TestAuthCounter_DrainOnEmptyCounterReturnsZero (0.00s)
=== RUN   TestAC003_MatchesAuthFailure_SEC001_ForgedLoggerNonRootUID_NotCounted
--- PASS
=== RUN   TestAC002_MatchesAuthFailure_RealSSHDFailedPassword_Counted
--- PASS
=== RUN   TestAC002_MatchesAuthFailure_RealSSHDService_Counted
--- PASS
=== RUN   TestAC002_MatchesAuthFailure_RealSudoPAMFailure_Counted
--- PASS
=== RUN   TestAC002_MatchesAuthFailure_GenericPAMAuthenticationFailure_Counted
--- PASS
=== RUN   TestAC002_MatchesAuthFailure_InvalidUser_Counted
--- PASS
=== RUN   TestAC003_MatchesAuthFailure_NonMatchingMessage_NotCounted
--- PASS
=== RUN   TestAC003_MatchesAuthFailure_WrongUnit_NotCounted
--- PASS
=== RUN   TestAC003_MatchesAuthFailure_MissingUnitAndIdentifier_NotCounted
--- PASS
=== RUN   TestMatchesAuthFailure_EmptyMessage_NotCounted
--- PASS
=== RUN   TestMatchesAuthFailure_MissingMessage_NotCounted
--- PASS
=== RUN   TestAC002_MatchesAuthFailure_AllFourPatternsMatch (4 sub-tests)
--- PASS
=== RUN   TestAC002_MatchesAuthFailure_SyslogIdentifierAllowlist_Counted
--- PASS
=== RUN   TestSEC001_MatchesAuthFailure_SyslogIdentifierForge_NonRootUID_NotCounted
--- PASS
=== RUN   TestMatchesAuthFailure_AllowedUnits_AllMatch (5 sub-tests)
--- PASS
=== RUN   TestMatchesAuthFailure_AllowedIdentifiers_AllMatch (4 sub-tests)
--- PASS
[collector_test.go additions]
=== RUN   TestAC010_RegisterDefaultsWithDocker_SecurityCollectorPresent
--- PASS
=== RUN   TestAD002_RegisterDefaultsWithDocker_LogsBeforeSecurityInOrder
--- PASS
[logs_test.go]
=== RUN   TestAC002_ParseAndCount_FourAuthPatterns_CounterIsFour
--- PASS
=== RUN   TestAC003_ParseAndCount_ForgedLoggerEntry_NotCounted
--- PASS
=== RUN   TestAC003_ParseAndCount_NonMatchingMessage_NotCounted
--- PASS
=== RUN   TestAC007_ParseAndCount_TenIdenticalLines_CounterIsTen
--- PASS
=== RUN   TestAC005_ParseAndCount_DoesNotCallMarkReady
--- PASS
=== RUN   TestAC006_ParseAndCount_EmptyOutput_NoEntriesNoCount
--- PASS
=== RUN   TestParseAndCount_UpdatesLastSeen
--- PASS
=== RUN   TestParseAndCount_MalformedJSON_Skipped
--- PASS
[security_test.go]
=== RUN   TestAC008_SecurityCollector_NonLinux_FailedLoginsIntervalIsNil (skipped on Linux)
=== RUN   TestAC008_SecurityInfo_NilPointer_OmittedFromJSON
--- PASS
=== RUN   TestAC001_SecurityInfo_NonNilPointer_PresentInJSON
--- PASS
=== RUN   TestAC005_SecurityCollector_CounterNotReady_ReportsZero
--- PASS
=== RUN   TestAC005_SecurityCollector_CounterReady_ReportsDrainedValue
--- PASS
=== RUN   TestAC009_SecurityInfo_24hAndIntervalAreIndependent
--- PASS
=== RUN   TestAC004_SecurityCollector_UsesCounterNotJournalctl
--- PASS
=== RUN   TestAC010_SecurityCollector_NameIsUnchanged
--- PASS
[auth_failure_templates_test.go]
=== RUN   TestAC020_AuthFailureTemplate_PresentWithCorrectFields
--- PASS
=== RUN   TestAC020_AuthFailureTemplate_DescriptionContainsSEC004Warning
--- PASS
=== RUN   TestAC023_SecurityCategory_PresentInTemplateList
--- PASS
=== RUN   TestAC023_AuthFailureTemplate_UnderSecurityCategory
--- PASS
=== RUN   TestAC030_LogErrorsTemplate_PresentInTemplateList
--- PASS
[auth_failure_generator_test.go]
=== RUN   TestAC001_AC024_CheckTelemetryThresholds_AuthFailure_FiresWhenAboveThreshold
--- PASS
=== RUN   TestAC008_CheckTelemetryThresholds_NilFailedLoginsInterval_NoEvent
--- PASS
=== RUN   TestAC008_CheckTelemetryThresholds_NilSecurity_NoEvent
--- PASS
=== RUN   TestAC024_CheckTelemetryThresholds_AuthFailure_CooldownPreventsRepeat
--- PASS
=== RUN   TestAC024_CheckTelemetryThresholds_AuthFailure_DeviceScopeExcludes
--- PASS
=== RUN   TestSEC006_CheckTelemetryThresholds_NegativeValueClamped_RuleGTZeroDoesNotFire
--- PASS
=== RUN   TestSEC006_CheckTelemetryThresholds_NegativeValueClamped_RuleLTZeroDoesNotFire
--- PASS
=== RUN   TestAC001_CheckTelemetryThresholds_AuthFailure_ValueEqualsThreshold_NoFire
--- PASS
[doctor_test.go additions]
=== RUN   TestAC031_CheckJournalAccess_WarnTextContainsRemediation
--- PASS
=== RUN   TestAC031_CheckJournalAccess_PassTextFormat
--- PASS
=== RUN   TestAC031_CollectorDeps_LogsHasJournalctl
--- PASS
=== RUN   TestAC031_CollectorDeps_SecurityHasJournalctl
--- PASS
```

## Full Test Suite

```
go test ./...
ok  github.com/DesyncTheThird/rIOt/internal/agent
ok  github.com/DesyncTheThird/rIOt/internal/agent/collectors
ok  github.com/DesyncTheThird/rIOt/internal/models
ok  github.com/DesyncTheThird/rIOt/internal/resilient
ok  github.com/DesyncTheThird/rIOt/internal/server
ok  github.com/DesyncTheThird/rIOt/internal/server/auth
ok  github.com/DesyncTheThird/rIOt/internal/server/ca
ok  github.com/DesyncTheThird/rIOt/internal/server/events
ok  github.com/DesyncTheThird/rIOt/internal/server/handlers
ok  github.com/DesyncTheThird/rIOt/internal/server/middleware
ok  github.com/DesyncTheThird/rIOt/internal/server/notify
ok  github.com/DesyncTheThird/rIOt/internal/server/probes
ok  github.com/DesyncTheThird/rIOt/internal/server/scoring
ok  github.com/DesyncTheThird/rIOt/internal/server/summary
ok  github.com/DesyncTheThird/rIOt/internal/server/updates
ok  github.com/DesyncTheThird/rIOt/internal/server/websocket

go vet ./...  (no output — clean)

cd web && npm run test:run
Test Files: 15 passed (15)
Tests:      234 passed (234)  [baseline was 229]
```

---

## AC Coverage Table

| AC ID | Test Name(s) | File(s) |
|-------|-------------|---------|
| AC-001 | `TestAC001_SecurityInfo_NonNilPointer_PresentInJSON`, `TestAC001_AC024_CheckTelemetryThresholds_AuthFailure_FiresWhenAboveThreshold`, `TestAC001_CheckTelemetryThresholds_AuthFailure_ValueEqualsThreshold_NoFire` | `security_test.go`, `auth_failure_generator_test.go` |
| AC-002 | `TestAC002_ParseAndCount_FourAuthPatterns_CounterIsFour`, `TestAC002_MatchesAuthFailure_*` (5 tests + 2 table tests) | `logs_test.go`, `auth_match_test.go` |
| AC-003 | `TestAC003_ParseAndCount_ForgedLoggerEntry_NotCounted`, `TestAC003_ParseAndCount_NonMatchingMessage_NotCounted`, `TestAC003_MatchesAuthFailure_WrongUnit_NotCounted`, `TestAC003_MatchesAuthFailure_MissingUnitAndIdentifier_NotCounted`, `TestAC003_MatchesAuthFailure_SEC001_ForgedLoggerNonRootUID_NotCounted` | `logs_test.go`, `auth_match_test.go` |
| AC-004 | `TestAC004_SecurityCollector_UsesCounterNotJournalctl` (structural proof + grep verification in report) | `security_test.go` |
| AC-005 | `TestAC005_AuthCounter_NotReadyBeforeMarkReady`, `TestAC005_AuthCounter_ReadyAfterMarkReady`, `TestAC005_AuthCounter_MarkReadyIsIdempotent`, `TestAC005_ParseAndCount_DoesNotCallMarkReady`, `TestAC005_SecurityCollector_CounterNotReady_ReportsZero`, `TestAC005_SecurityCollector_CounterReady_ReportsDrainedValue` | `auth_counter_test.go`, `logs_test.go`, `security_test.go` |
| AC-006 | `TestAC006_ParseAndCount_EmptyOutput_NoEntriesNoCount` | `logs_test.go` |
| AC-007 | `TestAC007_AuthCounter_ConcurrentAddIsRaceFree`, `TestAC007_ParseAndCount_TenIdenticalLines_CounterIsTen` | `auth_counter_test.go`, `logs_test.go` |
| AC-008 | `TestAC008_SecurityCollector_NonLinux_FailedLoginsIntervalIsNil` (skips on Linux / runs on non-Linux), `TestAC008_SecurityInfo_NilPointer_OmittedFromJSON`, `TestAC008_CheckTelemetryThresholds_NilFailedLoginsInterval_NoEvent`, `TestAC008_CheckTelemetryThresholds_NilSecurity_NoEvent` | `security_test.go`, `auth_failure_generator_test.go` |
| AC-009 | `TestAC009_SecurityInfo_24hAndIntervalAreIndependent` | `security_test.go` |
| AC-010 | `TestAC010_RegisterDefaultsWithDocker_SecurityCollectorPresent`, `TestAC010_SecurityCollector_NameIsUnchanged` | `collector_test.go`, `security_test.go` |
| AC-020 | `TestAC020_AuthFailureTemplate_PresentWithCorrectFields`, `TestAC020_AuthFailureTemplate_DescriptionContainsSEC004Warning` | `auth_failure_templates_test.go` |
| AC-021 | `[AC-021] rule form opens with "Create Alert Rule" heading after template selection` | `AlertRuleSettings.test.tsx` |
| AC-022 | `[AC-022] cooldown input reflects template default and accepts new value` | `AlertRuleSettings.test.tsx` |
| AC-023 | `TestAC023_SecurityCategory_PresentInTemplateList`, `TestAC023_AuthFailureTemplate_UnderSecurityCategory`, `[AC-023] renders a "security" section header for auth_failure template` | `auth_failure_templates_test.go`, `AlertRuleSettings.test.tsx` |
| AC-024 | `TestAC001_AC024_CheckTelemetryThresholds_AuthFailure_FiresWhenAboveThreshold`, `TestAC024_CheckTelemetryThresholds_AuthFailure_CooldownPreventsRepeat`, `TestAC024_CheckTelemetryThresholds_AuthFailure_DeviceScopeExcludes` | `auth_failure_generator_test.go` |
| AC-025 | `[AC-025] severity select can be changed to critical` | `AlertRuleSettings.test.tsx` |
| AC-030 | `TestAC030_LogErrorsTemplate_PresentInTemplateList`, `[AC-030] renders "Log Errors Detected" button under the system section` | `auth_failure_templates_test.go`, `AlertRuleSettings.test.tsx` |
| AC-031 | `TestAC031_CheckJournalAccess_WarnTextContainsRemediation`, `TestAC031_CheckJournalAccess_PassTextFormat`, `TestAC031_CollectorDeps_LogsHasJournalctl`, `TestAC031_CollectorDeps_SecurityHasJournalctl` | `doctor_test.go` |

---

## Deviations from ADD

1. **`intPtr` helper not added to `security.go` (ADD note 4).** The ADD says to check for an existing `intPtr` and not duplicate. `gpu_test.go` already defines `intPtr` in the `collectors` package test scope. Adding it to `security.go` caused a test-build conflict. Resolution: used inline address-of expressions (`&n`, `&zero`) in `security.go` instead of a helper. Functionally equivalent; ADD note 4 explicitly provides this escape hatch ("verify location, do not duplicate").

2. **`parseAndCount` returns `([]models.LogEntry, time.Time)` not `[]models.LogEntry`.** ADD Section 12 note 2 defines the signature as `(c *LogsCollector) parseAndCount(raw []byte) []models.LogEntry`. Returning the latest timestamp as a second value avoids re-scanning the slice to advance `lastSeen` and keeps the public contract clean. This is an implementation-internal detail with no external interface impact.

3. **AC-021 test assertion adjusted.** ADD Section 8 AC-021 says "assert the created rule has `include_devices === ''`". After template selection the UI opens a modal with an `<input type="text">` for the rule name (value "Auth Failure") — the test can verify the modal opened in Create mode (heading "Create Alert Rule") and the name input contains the template name. The `include_devices` field is empty by construction (it is not set by template selection per `AlertRuleSettings.tsx:444-454` which spreads `emptyRule`). The test verifies the form opened correctly as a proxy for this; a deeper DOM assertion would require inspecting a DeviceMultiSelect custom component whose internal input is not accessible via standard role queries without additional `data-testid` attributes (not in scope per ADD Section 4).

4. **`checkJournalAccess` outputs a section header via `section()`.** The ADD does not specify whether to emit a section header. The existing doctor pattern (e.g., Docker section, Permissions section) uses `section()` for all capability groupings. The journal-read check follows the same pattern. This is a style decision consistent with the file's conventions, not a functional deviation.

---

## Notes for QA

1. **Linux-only behavior:** AC-002 through AC-007, AC-009, AC-010 (collector ordering), AC-031 (journal preflight) require a Linux host with journald to exercise the full live path. Go tests that require Linux are either gated with `t.Skip` on non-Linux (AC-008 non-Linux variant, AC-004 structural, AC-005 drain gate) or test the logic in-process via `parseAndCount` without a real exec.

2. **AC-004 grep verification:** Run `git grep -n 'exec.CommandContext.*journalctl' internal/agent/collectors/security.go` — must return exactly 1 line (the pre-existing 24h call). The `parseAndCount` path in `logs.go` has no journalctl call.

3. **AC-031 manual smoke test:** On a Linux host where `riot` is NOT in the `systemd-journal` group, run `riot-agent doctor` — the "Journal Access" section must appear with a `!` warn line mentioning `systemd-journal` group and the `sudo usermod` remediation command.

4. **SEC-006 regression test is present:** `TestSEC006_*` in `auth_failure_generator_test.go` covers both `>` and `<` operators with a negative value. Manual verification: POST a telemetry payload with `"failed_logins_interval": -5` to the server — the event table should have no new rows.

5. **Frontend AC-021 note:** The AC-021 test verifies that the "Create Alert Rule" modal opens after clicking the auth_failure template, and that the name field contains "Auth Failure". The `include_devices` field starting empty is enforced by the `emptyRule` spread in `AlertRuleSettings.tsx:444-454` — this is code-level proof, not runtime assertion. QA may verify by opening the dashboard, clicking "Create from Template", selecting "Auth Failure", and checking the "Include Devices" field is blank.

6. **No migration required:** No database schema changes were made. `go test ./...` covers the existing migration tests.

7. **Collector whitelist:** No new collector name was introduced. Devices with `collector.enabled` containing `security` will automatically report `failed_logins_interval` after agent upgrade with no config change required.

---

## Follow-up Items for Technical Writer

- README: document `failed_logins_interval` metric under the alert metrics section; note Linux-only availability and the `> 0` internet-facing SSH warning per SEC-004.
- README: document the new `auth_failure` alert template and `security` category.
- README: document `riot-agent doctor` journal-read preflight and the `sudo usermod -a -G systemd-journal riot` remediation.
- CHANGELOG: entry for LOG-001 — near-real-time auth failure alerting via per-interval `failed_logins_interval` metric.
- MEMORY.md `feedback_doctor_sync.md`: no new collector name, but the doctor check was extended (AC-031). Update if that document tracks doctor changes.

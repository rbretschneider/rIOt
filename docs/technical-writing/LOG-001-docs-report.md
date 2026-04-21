# Documentation Report — LOG-001

**Story ID:** LOG-001
**Date:** 2026-04-20

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated — Features bullet | Extended "Advanced alerting" feature bullet to mention near-real-time SSH/auth failure alerting with latency and Linux-only qualifier. |
| `README.md` | Updated — Available Collectors table | Extended `security` collector row to name `failed_logins_interval`, call out Linux-only scope, note origin-filtering, and link to the new section. |
| `README.md` | Updated — State Alerts section | Added "Auth failure" bullet referencing the new section anchor. |
| `README.md` | Updated — Agent Diagnostics section | Added a paragraph explaining the new Journal Read Access check, what it detects, and the exact remediation command. |
| `README.md` | Created — Auth Failure Alerting section | New top-level section between Nginx Access Log Monitoring and mTLS. Covers: what the metric is, Linux-only scope, origin-filter mechanism and why it matters, four matched substrings, first-interval-zero behavior and restart window, step-by-step template setup including the SEC-004 internet-facing SSH warning, metric reference table, and no-op on non-Linux / no config change required. |
| `CHANGELOG.md` | Updated — [Unreleased] / Added | Five entries covering: `failed_logins_interval` metric, origin filtering with forger-rejection rationale, Auth Failure template with SEC-004 warning, new event types, and the `doctor` journal read check with remediation command. |

---

## Files Reviewed — No Changes Made

| File | Reason |
|------|--------|
| `internal/agent/collectors/auth_counter.go` | SEC-005 serialization invariant already documented in package doc comment with full detail. `MarkReady`/`IsReady` latch comments already reference FR-005. |
| `internal/agent/collectors/auth_match.go` | `allowedUnits` and `allowedIdentifiers` var comments explain the trust model. `matchesAuthFailure` comment spells out two-gate requirement with AD-004 and SEC-001 references. |
| `internal/agent/collectors/logs.go` | `collectFromOutput` comment covers fail-open, FR-006, and MarkReady non-call on error. `parseAndCount` references AD-001 and AD-004 at the counter-increment site. |
| `internal/agent/collectors/security.go` | Lines 79-80 carry the explicit "do NOT apply SEC-001 filter here" comment on the `FailedLogins24h` path. Lines 103-106 explain the Drain/IsReady gate with FR-005 reference. |
| `internal/agent/collectors/collector.go` | Lines 64-79 carry the ordering-constraint block comment with AD-002, SEC-005, and a warning about parallel execution. |
| `internal/agent/agent.go` | SEC-002 deferred comment at the `slog.Info` call identifies the first-push-zero window. |
| `internal/agent/doctor.go` | `checkJournalAccess` function comment references AD-010 and SEC-003. |
| `internal/server/events/templates.go` | `auth_failure` template entry has all correct fields (verified against AC-020). Description field carries SEC-004 warning. |
| `internal/models/telemetry.go` | `FailedLoginsInterval` field doc comment explains pointer semantics, Linux scope, fail-open zero. |
| `internal/models/events.go` | `EventAuthFailure` and `EventDetectorInitialized` constants present with correct string values. |
| `CONTRIBUTING.md` | No project-level CONTRIBUTING.md exists. No workflow changes from this story warrant creating one. |

---

## Stale Content Found — Not Fixed (out of scope)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` (top of file) | `<h4>` stability notice ("active dev...if you installed prior to v2.24 I recommend starting over") is stale — project is substantially past v2.24. | Remove or update in a housekeeping pass. |
| `README.md` — Releasing section | Example tag shown as `v1.2.0` but `README last updated for v2.38.0`. Mismatch creates operator confusion. | Replace with placeholder or current-format example tag. |

---

## Accuracy Flags

| Item | Location | Finding |
|------|----------|---------|
| `EventDetectorInitialized` is agent-side only | `internal/models/events.go`, `internal/agent/agent.go` | ADD Section 7 described a server-side "detector initialized" event row as deferred SEC-002 behavior. Implementation emits only `slog.Info` — no server-side event row is written. Documented as QA Deviation 4 (informational). CHANGELOG uses precise phrasing: "informational agent startup log acknowledging the first-interval-zero window". |
| `FailedLoginsInterval` drain-when-not-ready | `security.go:107-113` | Code calls `Drain()` unconditionally then checks `IsReady()`. If not ready, the drained value is discarded. Correct per FR-005; the value is lost for that interval (not held for the next tick). Intentional, not a bug. |

---

## Decisions a Reviewer May Want to Revisit

1. **Section placement.** The new "Auth Failure Alerting" section was placed between "Nginx Access Log Monitoring" and "mTLS Device Authentication" — grouping it with other feature-specific monitoring sections rather than embedding it within the Alerting section. The Alerting section has a short bullet pointing to the anchor.
2. **Operator warning tone.** The SEC-004 guidance ("raise the threshold before enabling") is phrased as a recommendation matching the template Description. Could be strengthened to a callout.
3. **CONTRIBUTING.md.** Absent. The load-bearing registration-order constraint (`LogsCollector` before `SecurityCollector`) is documented inline in `collector.go` for now; a future CONTRIBUTING.md should reference it.

---

## Pipeline Completion

All six pipeline artifacts exist:

- `docs/requirements/LOG-001-frd.md`
- `docs/architecture/LOG-001-add.md` (rev 2)
- `docs/security/LOG-001-security-review.md`
- `docs/implementation/LOG-001-impl-report.md`
- `docs/qa/LOG-001-qa-report.md`
- `docs/technical-writing/LOG-001-docs-report.md`

Story is ready to merge.

# Documentation Report

**Story ID:** SYS-EXPORT-001
**Title:** Device System Summary Export
**Author:** Technical Writer Agent
**Date:** 2026-04-08
**QA Verdict:** PASS WITH NOTES

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Added "Device summary export" bullet to the Features list; added `GET /api/v1/devices/:id/summary` row to the Dashboard API table |
| `CHANGELOG.md` | Updated | Added two `[SYS-EXPORT-001]` entries under `[Unreleased] → Added`: one for the dashboard buttons/export behavior, one for the new API endpoint |
| `internal/server/summary/summary.go` | Inline comments | Added FR-002 and `html/template` avoidance explanation to `rawTemplate` constant; added explanation of why UPS battery/load fields are pre-formatted at population time rather than in the template |

---

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 18 | States "README last updated for v2.38.0" — this marker is not updated as part of stories and may be wrong after any release | Update the version marker when cutting the next release tag; consider removing the manual marker entirely since git tags are the source of truth |
| `README.md` API Dashboard table | `GET /api/v1/devices/:id/security-score` is registered in the router (`router.go` line 137) and tested, but is absent from the API table | Add the missing row in a dedicated pass |

---

## Accuracy Flags

| Discrepancy | Location | Notes |
|-------------|----------|-------|
| ADD Section 12 item 14 states the frontend should extract the filename from the `Content-Disposition` response header as its primary strategy, falling back to client-side construction. The implementation constructs the filename client-side only (using `device.hostname` and the current date from `new Date().toISOString()`) and never reads `Content-Disposition`. | `web/src/pages/DeviceDetail.tsx` lines 153–154 | The resulting filename is functionally equivalent and matches FR-007. The `Content-Disposition` header is still set correctly by the server (per FR-007 and SEC-001). Documented as-built. Not a bug. |
| ADD Section 12 item 7 describes sanitizing the hostname in the handler only. The frontend independently sanitizes the hostname when constructing the client-side filename (`device.hostname.replace(/[^a-zA-Z0-9_-]/g, '_')`). Both sanitization paths produce the same character set. | `web/src/pages/DeviceDetail.tsx` line 154 | Redundant but harmless. The server's `sanitizeFilename` remains the authoritative sanitizer for the `Content-Disposition` header. |
| The QA report for SYS-EXPORT-001 does not exist in `docs/qa/` as of the time this report was written. The task stated "QA has issued a PASS WITH NOTES verdict" but the report file has not been committed. | `docs/qa/SYS-EXPORT-001-qa-report.md` | Create and commit the QA report to complete the story pipeline. All other four pipeline documents are present. |

---

## Inline Comment Decisions

### `internal/server/summary/summary.go`

**Added — `rawTemplate` constant:**
The existing one-line comment explained that `text/template` was chosen over `html/template`. The addition explains *why* `html/template` would be wrong (HTML-escaping Markdown output) and references FR-002 so that a future engineer understands the conditional section logic at a glance rather than having to re-read the FRD.

**Added — UPS section in `buildSummaryData`:**
The implementation report notes that pre-formatting `UPSBatCharge` and `UPSLoad` as strings (rather than keeping them as `*float64` pointers) was a deliberate deviation from what the ADD implied. Without a comment, this looks like it could be changed "to use pointers for consistency." The comment explains the trade-off: avoiding template reflection helpers keeps the template readable, and references FR-002 to explain the em-dash fallback.

**Not changed — existing comments:**
The `summaryContainer` struct comment (SEC-002), the `summaryData` struct comment (AD-006), the CPU section comment (FR-003), the Storage section comment on `DiskDrives` vs `Filesystems`, the Docker section comment (SEC-002), and the `escapePipe` function comment (SEC-003) are all accurate and complete. No changes needed.

### `internal/server/handlers/handlers.go`

The `DeviceSummary` handler and `sanitizeFilename` function already have complete, accurate comments added by the engineer. No changes needed.

---

## Notes for Future Writers

- The `summary` package is entirely self-contained. The template, data struct, and all helper functions live in `internal/server/summary/summary.go`. There are no partial template files or embedded assets.
- The `summaryContainer` struct's four-field limit is a security constraint (SEC-002), not an oversight. Any future request to add container fields to the export must go through a security review before mapping new `ContainerInfo` fields into the template data.
- The API table in `README.md` uses `:id` as the path parameter notation (consistent with the existing table style). The ADD and OpenAPI-style docs use `{id}`. Both refer to the same thing — a device UUID.
- The CHANGELOG follows Keep a Changelog 1.1.0. New entries go under `## [Unreleased]`. Section order is Added → Changed → Fixed → Removed.

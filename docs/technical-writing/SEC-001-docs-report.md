# Documentation Report

**Story ID:** SEC-001
**Author:** Technical Writer Agent
**Date:** 2026-03-23

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Merged the "Security scoring" and "Security overview" feature bullets into a single "Security page" bullet that accurately describes the consolidated feature: scores on the Security page (not Fleet Overview), fleet score banner, pending security updates column, auto-updates column, cert expiry card, and sortable table. |
| `internal/server/handlers/security.go` | Inline comment | Added a "why" comment above the cert counting block in `SecurityOverview` explaining that it runs before the `sec == nil` guard intentionally, so that devices running `webservers` but not `security` still contribute their certificates to fleet totals. This was the site of CRIT-001 and the placement is non-obvious without context. |
| `CHANGELOG.md` | Created | No changelog existed. Created with an [Unreleased] entry covering all SEC-001 changes: the score column migration, the three new `security/devices` fields, the two new `security/overview` fields, the new Security page capabilities, the new utility module, and the extracted `MiniScore` component. |

---

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 12 | The stability disclaimer ("relatively stable at version 2.24", "prior to v2.24 I recommend starting over") refers to a version the project is far past. New users reading this will find it confusing. | Remove or replace with a current stability statement in a standalone documentation pass. First flagged in PROBE-SPLIT-docs-report. |
| `README.md` | The `README last updated for v2.38.0` marker at line 18 has not been updated since PROBE-SPLIT and SEC-001 changes. | Update the version marker whenever README changes are made. |
| `CHANGELOG.md` | The newly created changelog has no entries for versions prior to [Unreleased]. The project has a documented release workflow and git tags going back to at least v2.24. | Backfill entries from GitHub Release notes and git tags in a dedicated pass. |

---

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| DEV-001 (impl report): Fleet average score rendered as a banner strip above the table, not as an overview StatCard. | `Security.tsx` `SortableSecurityTable` | README and CHANGELOG describe it as "fleet score banner" — accurate to the actual implementation, not the ADD's "overview card" language. The deviation is cosmetic and QA accepted it. |
| The ADD (Section 12, item 1) names the utility function `scoreColor` but the implementation exports `gradeColor` (no `scoreColor` exists). The ADD also names `miniScoreColor`/`miniStrokeColor` as separate exports; the implementation exports them as `gradeColor`/`gradeStrokeColor` from `utils/security.ts`, not as separate named exports from `MiniScore.tsx`. | `web/src/utils/security.ts` | Documented using the actual exported function names: `gradeColor`, `gradeStrokeColor`, `gradeFromScore`. The ADD names are not used anywhere in the final code. |

---

## Notes for Future Writers

**Security page is now the score hub.** The Fleet Overview page no longer shows security scores. Any documentation or user-facing text that describes where to find device security scores must point to the Security page, not Fleet Overview. The `MiniScore` component (`web/src/components/MiniScore.tsx`) is still used on the device detail page (gated behind the `security_score` feature flag) but is no longer rendered on Fleet Overview.

**`unattended_upgrades: null` is not the same as `false`.** The API returns `null` when no update telemetry is available for a device. The frontend uses this to distinguish "auto-updates disabled" from "no data". Both the "Sec. Updates" and "Auto-Updates" columns show a dash for `null`. Any documentation describing these fields must preserve this three-value semantic: `true` (enabled), `false` (disabled), `null` (no telemetry).

**Certs Expiring card is conditional.** The card only appears when `total_certs > 0` in the overview response. A fleet with no devices running the `webservers` collector will never see this card. If documenting this feature for users, note the `webservers` collector dependency.

**Fleet score is computed client-side.** The backend does not return a fleet-wide score. The Security page fetches individual scores for each device via `GET /api/v1/devices/{id}/security-score` and computes the arithmetic mean in the browser. For a fleet of N devices the page makes N+2 requests on load; React Query caches scores for 5 minutes.

**Grade thresholds are defined server-side.** The `gradeFromScore` function in `web/src/utils/security.ts` must stay synchronized with `internal/server/scoring/engine.go`. The thresholds are: ≥90 = A, ≥75 = B, ≥60 = C, ≥40 = D, <40 = F. If the scoring engine changes these thresholds, `gradeFromScore` must be updated to match.

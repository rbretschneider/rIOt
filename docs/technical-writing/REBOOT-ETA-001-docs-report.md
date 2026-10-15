# Documentation Report
**Story ID:** REBOOT-ETA-001
**Author:** Technical Writer Agent
**Date:** 2026-10-15

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `README.md` | Updated | Added a "Reboot ETA tooltip" section (placement, tooltip-resolution matrix, computation rules, dependencies, agent-upgrade requirement, limitations, API fields with example). Added a Features bullet, extended the `commands.allow_reboot` config-table row, and extended the `GET /api/v1/fleet/patch-status` row in the API table. |
| `CHANGELOG.md` | Updated | Four `[REBOOT-ETA-001]` entries under `[Unreleased] > Added` (feature, additive API fields, agent `allow_reboot` report, internal helpers) and two entries under `Known Limitations (v1)`. |
| Inline code comments | No change | The changed code (`rebootEta.ts`, `fleet.go`, `auto_update.go`, `models/telemetry.go`, `agent/telemetry.go`) already explains the non-obvious "why" (injected clock, once-per-request compute, display-only `allow_reboot`, tri-state pointer). Nothing missing, nothing restating code. No churn. |

No new documentation structure was created. The README API table is the project's only API reference (no OpenAPI file, no separate `docs/api/`), so the new fields are documented there, as the FLEET-DASH docs report also established.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `CHANGELOG.md` (`### Changed` at ~L33 and again at ~L99) | The `[Unreleased]` section has two `### Changed` headings, with a `### Fixed` between them. The second block (PATCH-GATE, FLEET-DASH, POOL-002, SEC-001 entries) belongs under the first. | Merge the two `### Changed` blocks into one in an isolated docs pass. |
| `CHANGELOG.md` `[Unreleased]` | Entries from many shipped stories (FLEET-DASH, GPU-001, POOL-001, and others dated 2026-04) still sit under `[Unreleased]`; no version headings exist. | Cut versioned sections from the git tags in an isolated pass. |
| `README.md` (whole file) | The README does not follow the standard front-door layout: no top-level "Requirements" section with pinned versions, no consolidated environment-variable table, no "Project Structure" section. Versions are scattered ("Requires Go 1.24+ and Node.js 20+" at ~L1136; Postgres 16 at ~L116). They match `go.mod` (1.24.0) and `Dockerfile` (`golang:1.24-alpine`, `node:20-alpine`) today. | Restructure in an isolated pass. Not a regression from this story. |
| `CONTRIBUTING.md` | File does not exist. | Create it (dev setup, `make test`, release/tag rule, test-naming convention) in an isolated pass. |
| `README.md` "Testing" | Lists commands only; no single-test command, no coverage of `go test ./...` on Windows (no `-race`), no "what a passing run looks like". | Extend in an isolated pass. |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| Implementation report states Go 1.26.0 as the detected stack; `go.mod` declares `go 1.24.0` and the `Dockerfile` builds with `golang:1.24-alpine`. | `docs/implementation/REBOOT-ETA-001-impl-report.md` header vs `go.mod:3` | README keeps the existing "Go 1.24+" (matches `go.mod`/`Dockerfile`). The 1.26.0 figure is probably the local toolchain. No doc changed. |
| ADD says `formatUtc(iso): string`; code returns `string \| null` and the anytime wording drops the cooldown parenthetical when no cooldown is set. | `web/src/utils/rebootEta.ts` vs ADD Section 4 | Documented as the code behaves: README states the cooldown part is omitted when unset, and that an uncomputable start falls back to the no-time text. Both deviations were already flagged and accepted by QA. |
| The patch-status endpoint only lists devices with `pending_updates > 0` (`fleet.go:61`, pre-existing). The FRD reads as if every `reboot_required` device gets an indicator (FR-001/FR-002, AC-001..003). A device with a pending reboot but zero pending updates (the common state right after patches apply) has no row, so no indicator or tooltip appears. Neither the ADD, the impl report, nor the QA report mentions this. | `internal/server/handlers/fleet.go` `PatchStatus` vs FRD FR-001/FR-002 | Documented as a limitation in the README ("Limitations", and the API table row "Lists only devices with at least one pending update") and in CHANGELOG "Known Limitations". Flagged here for the architect/product owner: if the intent was to show the ETA for every reboot-required device, a follow-up story is needed (the endpoint filter is outside this story's scope). |
| The `Est. reboot:` tooltip for `allow_reboot: true` in `window` mode carries no "if" qualifier, even when the device's `auto_patch` is off or the reboot-class policy is not `gated` (the row does not carry those fields). This is QA note 1 and is consistent with the ADD and FRD FR-017/AC-012, but it is a residual gap against BR-002. | `web/src/utils/rebootEta.ts` | Documented in the README ("What the estimate depends on") and in CHANGELOG "Known Limitations". |

## Verification Performed

- Tooltip wording, resolution order, and `formatUtc` format (`YYYY-MM-DD HH:MM UTC`) were checked against `web/src/utils/rebootEta.ts`.
- The four JSON field names, `omitempty` behaviour, and once-per-request compute were checked against `internal/server/handlers/fleet.go`.
- Selection rule, in-window and exact-start behaviour, and the 367-day bound were checked against `nextMaintenanceWindowStart` in `internal/server/handlers/auto_update.go`.
- The agent stamp (`allow := a.config.Commands.AllowReboot; v.AllowReboot = &allow`) was checked against `internal/agent/telemetry.go`; the `*bool` field against `internal/models/telemetry.go`.
- Both placements (`↻` glyph in the Updates column, `reboot required` badge in "Pending OS Patches") were checked against `web/src/pages/FleetOverview.tsx`. The **Frequency** label was checked against `AgentManagement.tsx`.
- The README example response is illustrative (placeholder device ID and hostname); the field names and shapes match the handler.
- No commands were documented that need execution; no flags were invented. The README gained no new runnable steps.

## Notes for Future Writers

- Entries in `CHANGELOG.md` and sections in the README are grouped by story and separated by blank lines; new stories go at the top of `### Added`/`### Changed`, with `Known Limitations (v1)` at the bottom of `[Unreleased]`.
- Both `README.md` and `CHANGELOG.md` use CRLF line endings. Edit with tools that preserve them; do not rewrite whole files.
- The README API table (`## API`) is the single API reference. Add new endpoint or field notes there and link to a feature section for detail.
- Per project memory, a docs-only change needs no release tag.
- Feature sections are cross-linked by anchor (`#reboot-class-package-gating`, `#reboot-eta-tooltip`). Keep headings stable or fix the links.

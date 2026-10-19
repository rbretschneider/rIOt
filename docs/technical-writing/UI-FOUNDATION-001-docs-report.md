# Documentation Report
**Story ID:** UI-FOUNDATION-001
**Author:** Technical Writer Agent
**Date:** 2026-10-18

---

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `CHANGELOG.md` | Updated | Added `[UI-FOUNDATION-001]` entries under `[Unreleased]`: 6 Added (toasts, confirmation dialogs, honest error states, UTC/local time switch, design system, demo build), 7 Changed (24-hour header clock, hostnames as reported, enum labels, device Delete placement and gauge caption, Security stat row, duration/byte formats, shared Modal), a new `### Removed` section (dead components, native dialogs, inline banners), 3 Fixed (QA-D1 patch-all double dispatch, QA-D2 offline "Device not found", silent failures), and 2 Known Limitations. Insert-only: no existing line was modified. CRLF line endings preserved. |
| `README.md` | Updated | Extended the "Real-time dashboard" feature bullet with the clock toggle; added a short "Time Display" section (default UTC, 24-hour, click the header clock, `localStorage` key `riot-time-zone`, reboot ETA stays UTC); added a pointer to the frontend design-system guide in the Testing section. CRLF preserved. |
| `web/src/components/ui/README.md` | Created | Developer guide: tokens and their Tailwind utilities, z-index scale, each primitive with minimal usage, `useToast`, the `ConfirmDialog` pending/error contract and the "never autofocus Confirm" rule, `QueryState`, `Timestamp`/`RelativeTime` and `utils/format.ts` as the only formatting module, the `labels.ts` enum map and how to add a value, chart colors, typed API errors, the static guards that enforce the rules, and how to test a screen with `renderWithProviders`. |
| `web/src/components/ui/Button.tsx` | Inline comment | Why `type` defaults to `"button"` and why pending disables the button. |
| `web/src/components/ui/Modal.tsx` | Inline comment | Why an empty footer renders a spacer. |
| `web/src/components/ui/Toast.tsx` | Inline comment | Why the toast region ignores pointer events, why it sits above modals, the toast-vs-`aria-modal` consequence (QA documentation-only finding), and why the list is newest-first. |
| `web/src/components/ui/QueryState.tsx` | Inline comment | Why data is checked before error. |

Verification: `npx tsc --noEmit` exits 0 and `npx vitest run src/test/static-guards.test.ts src/components/ui` passes (13 files, 346 tests) after the comment edits. No code, logic, or test file was changed.

## Artifacts Not Updated (by design)

| File | Reason |
|------|--------|
| `CONTRIBUTING.md` | Does not exist. The story adds no build step, environment variable, or tool. The Testing section of the README now links to the frontend guide. See "Stale Content Found". |
| API documentation | No API endpoint was added or changed. The story is frontend-only (`web/` and `docs/`). |
| Other `docs/` files | `docs/` contains only per-story pipeline artifacts. None references changed behaviour. |
| Test files | Out of scope per instruction. |

## Inline Comment Assessment

The new primitives were already commented with the reasons that matter: `Modal` (opener capture, open-modal stack, one-time focus effect), `ConfirmDialog` (SEC-001 focus rule, focus return after error, unmounted-dialog toast fallback), `Toast` (pause/resume accounting), `useMinuteTick` (one shared timer), `TimePreferenceProvider` (whitelisted enum, FR-103), `focusRing`, and `chartColors`. Those were left unchanged. Only the five gaps above were filled. No comment was deleted: none restated its code, none was a bare TODO, and none held commented-out code.

## Stale Content Found (Not Fixed)

Items found outside the scope of this story that need a documentation pass:

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `README.md` line 19 | "README last updated for v2.38.0". The latest tag is v2.64.2. The marker was already stale before this story and is also stale after it. | Remove the marker and rely on git history, or update it at every release. |
| `README.md` line 44 | "16 individually toggleable dashboard features". `web/src/hooks/featureCatalog.ts` defines 17 (`tags` is the 17th). | Change to 17, or drop the number. |
| `README.md` "Building from Source" | States "Node.js 20+". The Dockerfile builds the frontend on `node:20-alpine`; CI (`.github/workflows/ci.yml`) runs Node 22. There is no `.nvmrc` or `.tool-versions`, so no pinned version exists. | Decide the supported Node version, pin it in a version file, and state the exact version. |
| `README.md` (whole file) | Lacks the standard front-door sections: Requirements with exact versions, a numbered Getting Started from clone to running, an Environment Variables table that points to an `.env.example`, and a Project Structure map. Server env vars exist under "Server Environment Variables" but no `.env.example` was checked. | Add Requirements and Project Structure sections in a dedicated pass. |
| `CHANGELOG.md` | Two `### Changed` headings inside the single `[Unreleased]` block (originally lines 38 and 104; now 47 and 120). Section order is also non-standard (Added, Fixed, Changed). Entries were inserted under the first `### Changed` and the first `### Fixed`, and the new `### Removed` sits before `### Known Limitations (v1)`. | Merge the two `### Changed` blocks and reorder to Added, Changed, Removed, Fixed. |
| `CHANGELOG.md` | `[Unreleased]` still holds stories that have already shipped in tagged releases (for example AGENT-HARDEN-001). The file has no versioned sections although its header says versions correspond to tags. | Cut versioned sections from the tags and keep only unreleased work under `[Unreleased]`. |
| `CONTRIBUTING.md` | Missing. The project has branch, tag, and release conventions and a test workflow that exist only in the README and the maintainers' own notes. | Create it: environment setup, `make test`, `npm run test:run`, `npx tsc --noEmit` (the repo has no ESLint), branch and PR conventions, and the tag-per-code-push release rule. |
| `web/` | No `web/README.md`. The frontend guide now lives at `web/src/components/ui/README.md`. | Add a short `web/README.md` that links to it and documents `npm run build:demo` and the demo build. |
| `web/src/utils/docker.ts` | `groupContainers` and its `ContainerGroup` type are unused since the dead-component removal (noted in the impl report). | Delete in a code cleanup story. |
| `web/src/components/Section.tsx` | The header comment says the card is "always the same gray-900 card with an uppercase header". Primitives now use the `raised` token and `Card`. The comment is accurate for the code but pre-dates the tokens, and the component is one of the un-migrated ones. | Re-evaluate when `Section` is migrated onto `Card`. |
| `SecurityScoreModal.tsx`, `SetupGuide.tsx` | Still use a literal `z-[60]` overlay (deferred as SEC-004). The new guide tells developers never to use a numeric z-index, so these two are the visible exceptions. | Migrate to `Modal` and the token scale. |

## Accuracy Flags

Discrepancies between the pipeline documents and what the code does:

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| The impl report "Known gaps" section says the `after-*.png` screenshots, the AC-134 console walkthrough, and visual verification were not done. The "Visual verification (Clod)" section later in the same report says they were. | `docs/implementation/UI-FOUNDATION-001-impl-report.md` | Treated the later section as current. Nothing user-facing depends on it. |
| The QA report lists QA-D1 and QA-D2 as open `it.fails` tests; the "Post-QA fixes" section says both were fixed and the tests are plain `it`. `FleetOverview.tsx` and `DeviceQueryFallback.tsx` agree with the fixes. | `docs/qa/UI-FOUNDATION-001-qa-report.md` | Documented as fixed, under "Fixed" in the CHANGELOG. |
| ADD DEV-11 (primary dialogs focus Confirm) and ADD Flow 7.4 (`mutateAsync` with live status for delete) differ from the code, which focuses Cancel on every dialog and snapshots `deleteScope`. Both are recorded as deviations in the impl and QA reports. | `ConfirmDialog.tsx`, `DeviceDetail.tsx` | Documented what the code does: Cancel is always first focus. |
| FR-120 requires a visible "Security score" caption. The code renders the caption "Security score" in small uppercase through CSS, so it reads "SECURITY SCORE" on screen. The accessible name is "Security score N, grade X. Open details". | `web/src/components/SecurityScoreGauge.tsx:32, 67` | CHANGELOG says a visible "Security score" caption and an accessible name including score and grade. |
| ADD said the Modal's default initial focus is the "first focusable"; the code focuses the first control in the body, then the footer, then the close button. | `web/src/components/ui/Modal.tsx` | The guide states the code's order. |

## Notes for Future Writers

- **The CHANGELOG is the project's only release record.** Entries are long, one bullet per behaviour, tagged `[STORY-ID]`. Insert new entries at the top of the matching heading. The file uses CRLF line endings: do not use a tool that rewrites them, or the diff becomes the whole file. This story inserted entries with a script that converts to LF, inserts, and converts back.
- **Two `### Changed` headings exist.** New "Changed" entries go under the first one (immediately after `### Fixed`). Remove the ambiguity when the file is reorganised.
- **Frontend documentation lives next to the code.** `web/src/components/ui/README.md` is the single guide for the design system. When a primitive's props, a token, or a static guard changes, update it in the same change. The README links to it. Do not duplicate its content in the root README.
- **The guide describes behaviour tests pin.** Before editing a sentence about focus, pending state, or toasts, read the matching `*.test.tsx`: the `[UIF AC-xxx]` describe titles map each statement to a requirement in `docs/requirements/UI-FOUNDATION-001-frd.md`.
- **Comment style.** `[UIF AC-xxx]`, `SEC-00x`, and `FR-0xx` identifiers appear in code comments and tests. Reference them in new comments when a rule comes from the FRD or the security review.
- **Public repository.** Use generic examples only (hostnames, IPs, time zones such as `America/Los_Angeles`). No real hostnames, LAN addresses, personal domains, or filesystem paths.

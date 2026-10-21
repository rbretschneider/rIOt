# Architecture — DOCKER-STACK-005

**Date:** 2026-10-20  
**Stack:** React 19, strict TypeScript 5.9, Tailwind 4, existing Button/DataTable/StackUpdateDialog, Vitest/Testing Library. No dependencies.

## Design and files

| File | Change |
|---|---|
| `web/src/pages/docker/StacksPage.tsx` | Derive positive presentation from `s.updates > 0`; compact warning `Update (N)` control and singular/plural Updates-cell pill. No behavior change. |
| `web/src/pages/docker/DockerPages.test.tsx` | AC-001/002/003 positive plural/singular, neutral zero, and same confirmation-only click regression. |

For positive counts only, use shared Button `variant="ghost"` plus Tailwind 4 important utilities `h-6 px-2! bg-warn! text-black! hover:bg-warn/85!`, ensuring local semantic colors/padding override ghost/size defaults. Existing warn is amber500, paired with dark readable text. Verify computed output; do not add a shared warning variant, new palette value or global CSS. Show `Update (${s.updates})` and a title explaining N container updates available. Retain small size, 24px height, accessible `Update <project> on <host>` label, focus ring and unchanged onClick selected device/project assignment. Action width112 minus cell padding24 leaves88px; compact px2 padding must keep realistic multi-digit labels such as `Update (15)` inside it. Updates column stays100px. Zero/nonpositive uses the existing neutral secondary Button and visible `Update` text.

Keep the existing Stack identity cell unchanged. In the existing Updates column, positive numeric counts render as a compact warning pill: `1 update` for one and `${s.updates} updates` otherwise. Nonpositive data retain the original numeric rendering; zero is `0`. The numeric data/accessor/filter model does not change. The action's own count provides a non-color cue when the Updates column is offscreen. Keep project220/actions112 column widths and 44px DataTable rows; no added column, identity badge, grouping row or query.

No change to StackUpdateDialog, shared Button/DataTable, StackPage detail, API/types, update tracking/confirmation or auth/feature routes. The count is the existing server projection and is not a freshness guarantee or permission. Positive and neutral buttons both open the current selected-target dialog; that dialog still fetches fresh detail/hosts and enforces capability/path/explicit confirmation before dispatch.

## Verification

AC-001 verifies singular/plural pill, button count/title and warning presentation; AC-002 verifies neutral zero/nonpositive/no freshness claim. AC-003 verifies no mutation on opening and the correct target plus existing dialog gates. AC-004 requires actual wide/portrait browser measurement of button24/row44/action112 widths, fitting realistic counts, unchanged project identity, computed warning colors/contrast, focus and contained horizontal scrolling. Preserve existing collection link/filter tests. Run focused Docker pages tests, full frontend tests, `npx tsc --noEmit`, production build; package.json has no lint script. Follow independent QA then technical-writing pipeline; do not issue production commands.

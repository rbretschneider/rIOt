# Formal Requirements — DOCKER-STACK-005

**Date:** 2026-10-20  
**Scope:** Make known outdated stacks apparent in the Stacks collection

## Outcome and requirements

The Stacks table currently presents the same neutral Update control for every stack, with known update counts easy to miss. For `s.updates > 0`, show a compact amber/orange `Update (N)` button and an Updates-cell warning pill reading `1 update` or `N updates`. Zero/nonpositive counts retain the neutral `Update` control and numeric Updates value; absence of emphasis must not claim Up to date or a successful freshness check.

This is presentation only in StacksPage. Preserve the pinned 220px Stack column, 112px action column, 24px button/44px virtual row height, fixed table widths, thin internal scrolling, filters and encoded links. Keep the existing project identity unchanged, accessible Update label and shared focus ring. Positive state must be conveyed by count text on the action itself as well as color, even when the Updates column is offscreen.

Clicking either styled or neutral Update continues to open the existing confirmation dialog. No automatic dispatch, new mutation, altered capability/path gate, change to confirmation/progress, dependency, route, shared Button variant or global theme is permitted.

## Acceptance criteria

| ID | Required evidence |
|---|---|
| AC-001 | A count of 1 renders `Update (1)` and Updates-cell pill `1 update`; counts above 1 render `Update (N)` and `N updates`. Positive button is amber with readable dark text; its title explains the count of available container updates. |
| AC-002 | Zero/nonpositive counts have a neutral `Update` control and the original numeric count (zero remains `0`), without warning pill or Up-to-date claim. Existing count data and filters remain unchanged. |
| AC-003 | Positive and zero-count controls preserve scoped accessible labels and open the existing correct-target dialog without dispatch. Mutation still requires its fresh capability/path gates and explicit Confirm; existing stack dialog regressions remain green. |
| AC-004 | Wide and portrait browser checks show 24px buttons/44px rows, 112px action width, fitting positive count labels even when Updates is offscreen, no project-column change/new page overflow and usable focus/contrast. Stack/host filters, canonical links and collection context remain intact. |

## Gates

Approved ADD/security, green frontend baseline, focused AC-labelled tests, full frontend suite, TypeScript/build, then independent browser QA and technical writing. No production update is part of verification.

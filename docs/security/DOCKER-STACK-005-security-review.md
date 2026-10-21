# Security Review — DOCKER-STACK-005

**Date:** 2026-10-20  
**Verdict:** APPROVED for scoped presentation change

Reviewed current StacksPage, shared Button and existing StackUpdateDialog. This change adds warning `Update (N)` styling/count text and an existing Updates-cell singular/plural pill from `updates > 0` data only. Project identity remains unchanged. No endpoint, privilege, dependency, route, global style or data-contract change.

Required invariants:

- Existing row click only selects device/project and opens confirmation. Styling/count cannot trigger commands, bypass fresh path/capability checks or remove explicit consent.
- Keep the exact scoped action label, encoded project link, escaped React text and existing feature/auth boundaries. No HTML injection or external resource added.
- Zero/unknown freshness does not become a successful Up-to-date claim. Count visibility is informational, not action authorization.
- Use existing amber warning background/dark foreground with verified computed contrast, shared focus ring and action count text so meaning is not color-only even with Updates offscreen. Preserve 24px button/44px row/112px action geometry and portrait scrolling.

AC-001/002 cover accurate positive/neutral presentation; AC-003 preserves confirmation-only dispatch and scoped target; AC-004 requires real layout/accessibility browser evidence. Existing stack dialog safety tests remain regression evidence. No unresolved security blocker; no production update is authorized for testing.

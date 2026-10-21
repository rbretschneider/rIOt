# Security Review — DOCKER-TABLE-006

**Date:** 2026-10-21  
**Verdict:** APPROVED for scoped frontend sizing change

Reviewed current ContainersPage column widths/renderers, DataTable explicit-grow/pinned-only allocation and prior compact-Name tests. CPU/Memory fixed160 and explicit Name growth use existing table behavior. This adds no endpoint, data field, dependency, privilege or interaction.

Required invariants:

- Preserve escaped identity text, encoded resource/log links, nonshrinking accessible update badge and existing verdict precedence. More space must not change freshness or update eligibility.
- Keep graph values/scales unchanged; equal track widths communicate comparable geometry, not equivalent CPU/Memory units or a new utilization calculation.
- Preserve fixed row geometry, focus, selection, filters/sorts, saved preferences, internal scroll, action confirmation and auth/feature gates. No resize behavior may dispatch work or rewrite preferences.
- Leave generic DataTable pinned-only/no-grow behavior intact; use the existing explicit grow contract locally. Confirm actual portrait overflow and badge visibility rather than relying solely on metadata tests.

AC-001/002 cover widths/growth/preferences; AC-003 covers badges/sorts/URLs; AC-004 covers browser geometry and existing safety regressions. No unresolved security blocker within this scope. No production action is authorized for verification.

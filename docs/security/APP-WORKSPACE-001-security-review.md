# Security Review — APP-WORKSPACE-001

**Date:** 2026-10-20  
**Reviewer:** Security reviewer  
**Verdict:** APPROVED FOR IMPLEMENTATION against the current FRD/ADD. No blocking design changes.

## Scope and evidence

Reviewed root `CLAUDE.md`, the complete APP-WORKSPACE-001 FRD and ADD, and the existing authenticated App boundary, `App.auth.test.tsx`, `useFeatures`, command palette, feature catalog, Docker URL/return-state helpers, container/stack pages, host terminal, shared Tabs/Modal and safe external URL helpers. Source baseline: `7b3123f`. The integrator reports a green baseline of 80 frontend files / 1,499 tests; this design review does not claim independent runtime verification of the new implementation.

This change reorganizes authenticated frontend presentation and filters copies of existing Docker host data. It adds no endpoint, authority, dependency, database change or infrastructure operation. Existing server authorization remains authoritative; hiding navigation or filtering hosts is not access control.

## Required implementation invariants

| Concern | Required behavior and verification |
|---|---|
| Authentication / prior login reload regression | Preserve `useFeatures(authenticated && !loading && !needsSetup)`. Mount AppShell, protected utility queries, command palette and authenticated WebSocket components only after the existing guards. AppShell/PageHeader must not fetch. AC-004 must exercise real App auth flow: pending auth, anonymous OIDC login and initial setup issue no protected settings/navigation requests and trigger no reload. Do not make protected APIs public to compensate. |
| Features and host scoping | Hide Docker links and Activity when Docker is disabled and preserve DockerLayout's disabled deep-link boundary. Keep terminal feature/capability rules and other feature meanings unchanged. Apply `docker_available === true` to derived Docker lists/options only; retain offline available hosts and the complete Devices/cache data. AC-008 must distinguish unavailable, offline, empty and failed-query states. |
| Navigation / URL injection | Use fixed internal route destinations, `containerPath`/`stackPath` and encoded identity/query components. Preserve the existing allowlist for collection-return router state; never navigate to an arbitrary stored URL. Render resource labels as React text, without raw HTML. Preserve `safeUrl`/published-port validation for external links and `noopener noreferrer` for new-window links. Host scope must never silently resolve a direct link to a different resource. |
| Terminals and shortcuts | Terminal shortcuts navigate only; do not mount or preconnect terminal sessions in the shell or PageHeader. Preserve `.xterm` and `[data-palette-exempt]` Ctrl/Cmd+K exclusions. Opening/searching navigation must not send shell input or infrastructure commands. |
| Actions and secret exposure | Presentation slots retain the existing typed confirmations, self-host warnings, disabled/pending state, locks and explicit mutation handlers. No navigation/render/selection side effect may run an action. Do not add raw configuration, environment secrets or unmasked inspect values to header metadata; existing masked reads and stored/live log behavior stay intact. AC-009 must retain action and log regressions. |
| Accessible operation and theme | Use named navigation/toggle controls, unique `aria-current`, `aria-expanded` and a working skip link. Closed mobile navigation must contain no focusable controls; disclosure supports keyboard closing and route selection. Reuse Modal focus trapping/restoration if an overlay is chosen. Preserve severity text/colors and visible focus; check portrait layout and long titles/actions in a real browser rather than masking overflow. |

The ADD already incorporates these invariants in sections 3, 5–8. They are implementation/QA obligations, not a request for further operator approval. Release/deployment remains separately authorized under the repository's operational rules.

## Handoff

Development may proceed. QA should report AC-003–005 and AC-008–010 evidence explicitly, including anonymous OIDC behavior, disabled Docker deep links, malicious return-state/encoded resource coverage, terminal shortcut exclusion and keyboard/portrait checks. No broader backend security audit or production rollout is implied by this approval.

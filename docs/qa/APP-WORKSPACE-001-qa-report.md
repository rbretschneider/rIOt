# QA report — APP-WORKSPACE-001

Date: 2026-10-20

Verdict: **PASS WITH NOTES**. No unresolved release-blocking findings. The verified follow-up navigation fix is committed; technical writing may proceed.

## Scope and independence

Read the complete root CLAUDE.md, FRD, ADD, security review, implementation report and captured validation output. Reviewed the frontend diff from `7b3123f` through implementation commit `4b6727b`, authenticated-navigation test strengthening `b9ca1ac`, and trailing-slash fix `7baa47d`. No implementation code was changed by QA. No infrastructure actions or production requests were performed.

Independently ran the relevant shell/auth/header/Docker/terminal tests: **6 files / 51 tests passed**. Independently ran URL/action/log buffer/masking, recent-resource, static-guard, confirmation, device-action, settings and container regressions: **10 files / 200 tests passed**. TypeScript `npx tsc --noEmit` passed. After the fixes, independently re-ran AppShell and App authentication: **2 files / 24 tests passed**, including both new trailing-slash cases.

After technical writing identified a stale contrast-test mirror, independently reviewed the five neutral `TOKEN_HEX` corrections and the new AC-010 test, which merges all root CSS declarations before comparing the effective surface aliases. Independently re-ran theme and static guards: **2 files / 103 tests passed**; a fresh TypeScript check also passed. The correction aligns test evidence with the already-rendered global palette; it does not change runtime colors or authority.

The integrator confirms the final full suite is **85 files / 1,541 tests passed**, with TypeScript passing after the contrast-mirror correction. Production and demo builds passed after the navigation fix, on the unchanged runtime source; the later correction updates test-only color constants and coverage. QA inspected the earlier full-suite/build validation output and does not claim to have independently re-run the full suite or builds. No dependency, backend/API client contract, agent, migration or deployment changes appear in the diff.

## Findings and resolution

1. **Resolved — AC-002 trailing-slash route ownership.** React Router accepts `/settings/agents/`, but the original matching logic selected both Agents and Settings. `/security/` selected neither item. The integrator normalized trailing slashes before matching and added both fixtures. Independent re-test passed all 21 AppShell cases.
2. **Resolved — stronger authentication test assertion.** The old authenticated feature test queried the removed top-level “Docker” label, which no longer proved the child links were absent. It now checks Containers after the feature result settles. Independent authentication re-test passed all three cases. The separate shell feature-gate and Docker deep-link tests also pass.
3. **Resolved — contrast-test mirror consistency.** The writer identified that `TOKEN_HEX` still contained the previous gray surface values. The effective five neutral values now match the global overrides, and an AC-010 assertion prevents future divergence. Independent theme/static-guard tests passed, including the existing 4.5:1 text contrast checks against the corrected surfaces. This stays within the ADD's explicitly allowed token-mirror scope; the QA approval remains valid.

## Acceptance and security audit

| Criteria | Result and evidence |
|---|---|
| AC-001 | PASS. One Application navigation and one main landmark; DockerLayout only retains the feature gate/outlet. No navigation-only host query remains. |
| AC-002 | PASS after fix. Unique collection ownership is tested across device, Docker, monitoring and settings routes, including the accepted trailing-slash aliases. Fixed internal destinations and segment-aware matching avoid prefix collisions. |
| AC-003 | PASS. Disclosure state, same-route click, route transition, Escape/focus restoration and skip-to-main behavior pass. The skip link prevents hash replacement, preserving HashRouter. Closed disclosure uses actual CSS display hiding below xl. Root browser checks confirm 390px keyboard/accessibility behavior and desktop persistence. |
| AC-004 | PASS. The original authentication guards precede AppShell, WebSocket/global-query and palette mounting. `useFeatures(authenticated && !loading && !needsSetup)` remains intact. Independent real-App tests confirm pending/login/setup do not request protected settings and OIDC discovery stays stable without reload. |
| AC-005 | PASS. Search dispatch still opens the existing palette; account and unread-alert controls are supplied to the utility slot on all widths. Palette open-only data queries and `.xterm`/`[data-palette-exempt]` shortcut exclusions remain intact in source. Root browser checks exercise search, account links and Escape restoration. No terminal is mounted by the shell. |
| AC-006 | PASS. PageHeader matches the specified slot contract, supplies one wrapping h1 and omits absent slot wrappers. Component tests pass. Root's 390px device-detail check reports a 348px header, one h1 and all header buttons inside the viewport. |
| AC-007 | PASS. Devices, Overview, device details/probes/terminal, monitoring, Docker and settings use PageHeader. Settings removes its competing layout h1. Existing data/mutation handlers, pending flags and confirmations remain attached to the original controls. Representative tests and the full-suite evidence cover interactions. |
| AC-008 | PASS. Host lists/options derive filtered copies using strict Docker availability and retain offline Docker hosts. Tests cover online/offline available hosts, unavailable machines, unchanged shared data, complete Devices, no-Docker empty state, no search matches and query failure. Direct resource routes are not rewritten by host filtering. |
| AC-009 | PASS. Independent DockerPages tests include stored history, capability-gated Live, backfill boundaries/deduplication, detach, typed bulk/stack/cleanup confirmation and stale-state locking. URL utility tests confirm encoded identities. Container collection-return state remains restricted to the internal collection route; existing safe external URL, masked reads and action protections remain unchanged. Device action and ConfirmDialog regressions also pass. |
| AC-010 | PASS with browser evidence limits below. Global neutral palette overrides are now on root; semantic chart/status/focus tokens and existing thin scrollbar rules remain. Independently inspected saved Devices, Overview and 390px mobile Hosts screenshots: coherent neutral surfaces, differentiated status/chart hues, readable controls and one application navigation. |
| AC-011 | PASS. Integrator confirms 85 files / 1,541 tests and TypeScript after the mirror correction; independent theme/static-guard and TypeScript checks also pass. Production and demo builds passed on unchanged runtime source. No dependency or database/API diff. |

## Notes and limits

- Visual evidence uses the local simulated-data preview, not production. Root reports browser checks at 390×844, 1080×1760, 1280×900 and 1920×1080, and a 1080px Docker table with equal scroll/client widths. QA independently inspected the saved screenshots; root retained ownership of the active browser session to avoid conflicting control.
- Destructive actions were validated with mocks and source review; no live terminal command, deletion, restart or deployment was submitted.
- The inherited fixture warning about an unmatched device-probes route is reproduced in the independent regression run. It does not fail tests. Existing Vite chunk-size and demo out-directory advisories remain non-blocking.
- Diff whitespace warnings concern intentional Markdown hard breaks and captured verbatim validation output; no implementation whitespace error was found.

The resolved findings are covered by regressions. No remaining finding requires a design change or operator approval for this frontend story.

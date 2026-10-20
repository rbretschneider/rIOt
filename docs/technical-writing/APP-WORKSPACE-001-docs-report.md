# Documentation report — APP-WORKSPACE-001

**Date:** 2026-10-20  
**Pipeline:** Technical writing after QA PASS WITH NOTES and committed navigation follow-up `7baa47d`.  
**Verdict:** Documentation complete for the implemented workspace; no API documentation change required.

## Inputs and verification boundaries

Read root `CLAUDE.md` technical-writer instructions, the story [FRD](../requirements/APP-WORKSPACE-001-frd.md), [ADD](../architecture/APP-WORKSPACE-001-add.md), [implementation report](../implementation/APP-WORKSPACE-001-impl-report.md) and [QA report](../qa/APP-WORKSPACE-001-qa-report.md). Checked current AppShell, PageHeader, global CSS, theme mirror and existing README/UI guidance against the documented behavior. Upstream documents, implementation and tests were not edited by this writing pass.

The integrator's final evidence is 85 files / 1,541 tests passing, with successful TypeScript, production and demo builds. QA independently passed focused auth/navigation/header/Docker and operational regression subsets and approved the story with notes. After the theme-mirror correction, QA independently passed 103 tests and TypeScript checking and approved the correction. The writer does not claim an independent full-suite run, live deployment, live terminal action or infrastructure operation.

## Changes

| Artifact | Update |
|---|---|
| `README.md` | Added unified-workspace feature and a navigation map with canonical Devices/Overview URLs, exact groups, desktop/mobile behavior, utility search and keyboard access. Explained complete Devices versus Docker-only hosts including offline records. Updated Docker entry and Overview navigation instructions and related Devices/Alert Rules terminology. |
| `CHANGELOG.md` | Added an Unreleased APP-WORKSPACE-001 Changed entry describing the shell, shared headers, neutral surfaces, host scoping and preservation of existing URLs/auth/logs/terminals/confirmations. Older release-history entries remain historical. |
| `web/src/components/ui/README.md` | Documented exact PageHeader slots, AppShell module/slots and ownership, one-main/one-heading rules, auth/query boundary, unique active routes, keyboard/mobile behavior, global neutral surfaces and inherited thin scrollbars. Clarified that alias/contrast tests do not compute browser CSS inheritance. |

No endpoint, environment, migration, agent or deployment contract changed, so `docs/ops/docker-api.md` and operational upgrade instructions require no update for this story. The shared UI guide already owns developer layout guidance; no additional guide was created.

## Follow-up verification owned by integration

Writing identified that the literal `TOKEN_HEX` surface values still reflected the old gray palette even though the global stylesheet now supplies neutral overrides. The integrator completed the in-scope mirror/test correction permitted by the ADD, and independent QA approved it after 103 passing tests and TypeScript checking. The issue is resolved. The UI guide documents effective CSS values and the test boundary without claiming automated browser-computed contrast verification.

## Stale content found outside scope

- README's blanket “last updated for v2.38.0” marker and introductory v2.24 stability/reinstallation guidance predate this workspace story. They were not advanced or removed because doing so would imply a full documentation or upgrade-policy audit.
- No other unrelated reference guide was changed. Production deployment remains a separately authorized step; these documents do not claim the workspace is already live.

## Completion checks

Navigation labels/routes and 1280px disclosure behavior were checked against AppShell. Header props were checked against the shared component/export. Host scoping and auth protections match the implementation/QA reports. Markdown references resolve to existing story documents; documentation diffs contain no unintended implementation or test edits. Intentional Markdown hard-break whitespace follows the existing report style.

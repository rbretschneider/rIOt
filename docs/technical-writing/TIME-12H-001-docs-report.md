# Documentation Report
**Story ID:** TIME-12H-001
**Author:** Technical Writer Agent
**Date:** 2026-10-27

QA verdict: PASS WITH NOTES. Inputs read: FRD, ADD (including AD-016), security review, the Go and web implementation reports, the QA report, and `git diff origin/main...HEAD` for `README.md`, `.env.example`, `docker-compose.yml`, `web/src/components/ui/README.md` and the production code. The new README text was checked against `internal/server/config.go`, `internal/server/handlers/time_display.go`, `internal/server/handlers/patch_badge.go`, `internal/server/expect/messages.go` and `internal/server/router_time_display_test.go`, not against the ADD. The code and the ADD agree on every user-visible point. One FRD table row is wrong (see Accuracy Flags).

## Audience

| Audience | Affected | Artifact |
|---|---|---|
| Administrators and operators (Ryan) | Yes: new default display, new `RIOT_DISPLAY_TZ`, the `TZ` fallback, changed text in events and the patch badge | README "Time Display" and environment table, CHANGELOG |
| End users of the dashboard | Yes: the header clock now opens a panel; default is Server zone, 12-hour | README "Time Display", `web/src/components/ui/README.md` (developer-facing) |
| API integrators | Yes, lightly: one new admin endpoint. API time fields are unchanged and still UTC | README API table and "Time Display" |
| Developers extending or debugging | Yes: formatter and provider contracts, `renderWithProviders` default | CHANGELOG developer entry, `web/src/components/ui/README.md`, inline comments |

## Artifacts Updated

| File | Change Type | Summary |
|------|-------------|---------|
| `CHANGELOG.md` | Updated | `[Unreleased]` (no version). **Added:** five TIME-12H-001 entries (12-hour Server-zone display and the header panel with the two `localStorage` keys; `RIOT_DISPLAY_TZ` with its fallback chain, validation and start-up logging; `GET /api/v1/server/time-display`; server-written text; a developer entry for the Go and web helpers). **Changed:** three entries (the default moved from UTC 24-hour to Server 12-hour with the one-time legacy mapping; the new form of event and badge text, with old messages unchanged; what stays UTC). **Upgrade notes:** one entry that an existing `TZ` becomes the Server zone (QA N3) |
| `README.md` | Updated | Fixed the lines this story made wrong (below). Made two points explicit: an existing valid `TZ` becomes the Server zone on upgrade, and `RIOT_DISPLAY_TZ` is preferred over `TZ`. Added a request and response example for the new endpoint in "Time Display" and expanded its API table row with the `401` body and `GET`-only routing. The engineering team's earlier rewrite (the environment table rows, "Time Display", the Expected Activity examples) was kept and verified |
| `docs/requirements/TIME-12H-001-frd.md` | Erratum note | Section 7.5: a dated note under the table says the `*-*-01 00:00:00` row should read "Monthly on day 01 at midnight" (QA N4). The row itself is left as written so the history stays visible |
| `web/src/utils/cron.ts` | Inline comment | The doc comment for `*-*-01 00:00:00` said "day 1". The code echoes the day text, so it now says "day 01" and notes that `*-*-1` gives "day 1". Comment only; no logic changed |
| `docs/technical-writing/TIME-12H-001-docs-report.md` | Created | This report |

### README lines changed (QA N1 and the sweep)

| Line (new numbering) | Was | Now |
|---|---|---|
| Features bullet, "Real-time dashboard" (L27) | "click the utility-bar clock to switch every timestamp between UTC and your browser's local time" | Every timestamp shows in the server's zone in 12-hour form by default; the clock opens a panel for zone and clock choice |
| Features bullet, "Patch badge" (L58) | "A reboot date is shown as an estimate in UTC" | "in the server's display zone", with a link to Time Display |
| Environment table, `TZ` row (L177) | Said it is a fallback | Adds: if the server already sets a valid `TZ`, that zone becomes the Server zone on upgrade (QA N3) |
| Expected Activity, activity labels bullet (L862) | "follow the UTC or local choice in the utility bar" | "follow the time zone and clock choice in the utility bar" |
| Time Display, "Server display zone" paragraph | Said `TZ` is honoured as a fallback | Adds that a server with a valid `TZ` and no `RIOT_DISPLAY_TZ` uses that `TZ` from the first start after upgrading, and to check it before deploying |
| Time Display, after the endpoint paragraph | Prose only | Adds a request and response example (cookie `riot_session`, `200`, `Cache-Control: no-store`, `{"display_zone":"America/New_York"}`) and the `401` body |
| Patch badge table, "Auto (reboot on YYYY-MM-DD)" | "at the next OS-patch window start (UTC)" | "at the next OS-patch window start. The date is the date in the server's display zone" |
| Patch badge, hover explanation paragraph | "As of (when the update data was reported, in UTC)" | In the display zone, 12-hour form with a label, with an example and a note that the browser choice does not change it |
| API table, `GET /api/v1/server/time-display` row (L1808) | Present (added by the engineering team) | Adds the `401` body, "a device API key is not accepted" and `GET` only |

The Go implementation report's l.2824 and l.2830 use the older numbering. They are the "Auto (reboot on YYYY-MM-DD)" table row and the "As of ... in UTC" hover paragraph, the same two lines as the QA report's l.2871 and l.2877. Both are fixed (the table above lists them). The line numbers in this report are for the file after this edit and shift as the README changes; search by the quoted text.

### Statements left alone because they stay true

`README.md` "What stays UTC" (L1736 onward); the `next_window_start` API field "RFC 3339 UTC" row; the `window is in UTC` sentence in the DietPi patch-window guidance (the window is stored and edited in UTC); `docs/ops/os-maintenance.md` window text; the `HH:MM in UTC` field comments in `internal/models/automation.go` and the UTC-to-local helper comment in `AgentManagement.tsx` (they describe stored window values and the editor, which this story did not change).

### Claims checked against AD-016

The README says only the held-escalation summary is pushed ("Time Display", "Text the server writes") and that the powered-off and expected-offline messages appear on the dashboard and are not pushed. The existing Expected Activity text (L889) says "nothing is notified" for a suppressed offline. No README or CHANGELOG line claims those two messages are pushed. L926 ("one extra push for a host that was powered off on purpose") is about a different, pre-existing escalation path and was not changed.

## Verification

- `go test ./internal/server/ -run 'TIME12H|Docs|docs' -count=1` with `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp`: `ok` (1.97 s). This includes the README assertions in `time12h_docs_test.go`.
- `cd web && npm run test:run -- --reporter=dot`: 126 files, 2291 tests passed.
- Not run: the full Go suite (this change touched no Go file) and any command the README documents for installation. The new `docker-compose` instruction (add `- RIOT_DISPLAY_TZ=...` to the environment list) was checked against `docker-compose.yml`, not executed on a host.

## Stale Content Found (Not Fixed)

| File | Issue | Recommended Action |
|------|-------|--------------------|
| `docker-compose.prod.yml` | Has no commented `RIOT_DISPLAY_TZ` example, while `docker-compose.yml` does. The README says "the compose files in this repository list the server's environment explicitly". That is true, but a production user has no in-file hint. QA accepted the omission (Go deviation 5) | Add the commented line in a separate pass, or say in the README that only `docker-compose.yml` carries the example |
| `CHANGELOG.md` `[Unreleased]` | Holds many shipped stories (the same finding is open from SHUTDOWN-SUDO-001 and others) | Roll shipped entries under their version headings in a separate pass. Release this story under its own heading when it is tagged |
| `README.md` L18 | "README last updated for v2.38.0" (open since SHUTDOWN-DPKG-001) | Update or remove the marker |
| `README.md` (structure) | No top-level Requirements, Getting Started or Environment Variables sections in the form the writing standard asks for (open since SHUTDOWN-SUDO-001) | Audit the README structure as its own task |
| `README.md` Features bullet "Device summary export" (L24) | Still says the file is generated on export without naming the zone. The "Time Display" section covers the `Generated` line. Not wrong, only incomplete | Optional one-clause addition |
| `docs/requirements/TIME-12H-001-frd.md` other places | The FRD still contains the R-1 assumption (FR-029, AC-064) that powered-off and expected-offline messages become pushes. ADD AD-016 corrects it. Upstream documents are not mine to edit | Add a superseded-by note pointing to ADD AD-016, or leave as history |
| `docs/architecture/TIME-12H-001-add.md` | Open-points and Definition of Done boxes were not reviewed for ticking | Tick them when the story merges |
| `web/src/pages/settings/AgentManagement.tsx` L177 and `internal/models/automation.go` L12-13 | Comments describe window times as UTC. Accurate, listed so a later "show windows in the server zone" story knows they exist | None now |

## Accuracy Flags

| Discrepancy | Location | Documented As |
|-------------|----------|---------------|
| FRD 7.5 lists `*-*-01 00:00:00` as "Monthly on day 1 at midnight". The code echoes the day text, and the unchanged test `cron.test.ts` L126 expects "day 01" (QA N4) | FRD 7.5; `web/src/utils/cron.ts` | Erratum note under the FRD table; the `cron.ts` comment now says "day 01". No user-facing README text names this form |
| FR-029 and AC-064 assumed the powered-off and expected-offline messages become pushes. They are stored events that are never dispatched (ADD R-1, AD-016) | FRD FR-029; code `events.Generator.DeviceOfflineAt` | README and CHANGELOG say only the held-escalation summary is sent to notification channels |
| Zone labels differ by surface for zones without an `en-US` short name: the server writes the database abbreviation (`IST`), the dashboard writes `GMT+5:30` (QA N2, FRD DEC-7). The README says the `GMT` plus offset form "matches what the browser shows", which holds for zones with no letter abbreviation but not for `Asia/Kolkata` | README "Text the server writes" (last paragraph) | Left as the engineering team wrote it. The by-design difference is not spelled out for end users; consider one sentence in a later pass |
| QA D-1: `renderWithProviders` pins UTC and 24-hour when `timeZone.storage` is omitted. The architect has not confirmed it | `web/src/test/renderWithProviders.tsx` | CHANGELOG developer entry states the default and how to opt out |

## Notes for Future Writers

- **The Time Display section is the single source.** README "Time Display" owns the panel, the storage keys, the legacy mapping, `RIOT_DISPLAY_TZ`, the server text list, "What stays UTC" and the endpoint example. The environment table, the Features bullets, the Expected Activity bullets, the patch badge section and the API table link to it. A change to zone rules needs edits in all of those, the CHANGELOG, `.env.example` and `docker-compose.yml`. `internal/server/time12h_docs_test.go` asserts the README, `.env.example`, `docker-compose.yml` and `web/src/components/ui/README.md`; run it after any edit.
- **Patch-badge times come from the server.** The Reboot, Automatic patching and As of lines and the `Auto (reboot on date)` label are written in `internal/server/handlers/patch_badge.go` with `models.FormatDisplay*`. `reboot_at` stays an RFC 3339 UTC instant on purpose.
- **Event message examples in the README use `EDT`.** They are examples for the `America/New_York` zone. The real label follows `RIOT_DISPLAY_TZ`.
- **Line endings.** `README.md` and `docs/` files are LF in the index. `CHANGELOG.md` is CRLF in the working tree (`core.autocrlf`); the insert kept CRLF.
- **First production check (owner: Ryan).** Before deploying to hawaii, read `TZ` in `/home/docker/riot` compose; a stray value becomes the Server zone (QA N3). `RIOT_DISPLAY_TZ` has no effect until it is added there and the container is recreated.
- **Release.** The branch contains code, so it needs a tag when released. The README, CHANGELOG, FRD note and comment edits alone are docs-only.

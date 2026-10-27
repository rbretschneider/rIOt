# Formal Requirements Document

**Story ID:** TIME-12H-001
**Title:** 12-hour time display in the server's time zone (dashboard and server-written text)
**Author:** Business Developer Agent
**Date:** 2026-10-27
**Status:** FINAL (clarifications resolved by Clod on Ryan's behalf, see Section 2a; pending Ryan's review)

---

## 1. Executive Summary

Ryan asked: "put timing in 12hour format too. Preferably server time, not utc."
Today the dashboard shows every absolute time in 24-hour form, in UTC by
default, and the server writes UTC or unlabeled times into the patch badge, the
device summary and the event messages that become ntfy pushes. This story adds
a 12-hour display and a "Server" time zone. The Server zone is the IANA zone
that the server operator configures. The new default is Server zone in 12-hour
form, for example `2026-10-27 1:05:09 PM EDT`. UTC and browser-local time stay
selectable, and so does 24-hour form. Human text that the server writes uses the
server display zone, in 12-hour form, with a zone label. Stored data, API
fields, and the partition, quota and retention boundaries stay UTC. The story
adds no stored server state and no dependency.

## 2. Background & Context

UI-FOUNDATION-001 (UIF) introduced the canonical formatters in
`web/src/utils/format.ts` and a two-value preference, `UTC` or `Local`, with UTC
as the default. The preference is toggled by clicking the header clock. Every
absolute time carries a zone label and uses 24-hour form (UIF FR-092, A-005).
UIF OQ-002 left Go-rendered text out of scope.

Current state, verified against `origin/main` 8a16f2a (v2.83.0 + PR #44):

- **Dashboard formatters.** `formatTimestamp` renders the second form
  `YYYY-MM-DD HH:mm:ss ZONE`, the minute form `YYYY-MM-DD HH:mm ZONE` and the
  time form `HH:mm:ss ZONE`, through a cached `Intl.DateTimeFormat('en-US',
  { hourCycle: 'h23', timeZoneName: 'short' })`. Chart ticks are `HH:mm` with
  no zone label. `formatUtc` has no production callers.
- **Preference.** `contexts/TimePreferenceProvider.tsx` stores
  `riot-time-zone` in localStorage. Only the exact value `Local` selects Local,
  and anything else gives UTC. The stored value is an enum, never a zone string.
- **Header clock.** `components/HeaderClock.tsx` is a button. One click toggles
  between UTC and Local.
- **Unlabeled times.** `ActivityLabel.tsx` and `AgentUpdatingVersion.tsx` render
  activity clock times ("back by", "settling until", "Powered off … at",
  "started") with the chart-tick formatter, so these times have no zone label.
- **Server text.** `handlers/patch_badge.go` writes "… UTC" times in 24-hour
  form. `summary/summary.go` writes `Generated: … UTC`.
  `models/expect.go` and `expect/messages.go` format event messages with
  `.Local()` and `15:04`. In the container, `time.Local` is UTC, so the ntfy
  pushes read "at 17:05" with no zone label.
- **Deployment.** The runtime image is `alpine:3.19` with `tzdata` installed.
  The container sets no `TZ`. The Hawaii host zone is America/New_York.
  Server configuration is env-only (`internal/server/config.go`).
- **UTC-critical code.** `partition/plan.go` and `quota/daily.go` already call
  `.UTC()` explicitly. The maintenance-window math in `handlers/auto_update.go`
  is UTC. Maintenance windows are stored as `HH:MM` UTC.

## 2a. Decisions made on Ryan's behalf

Ryan was not available for an interview. Clod made each decision below. They
are binding for this story and are **not** open questions. The PR body lists
them as decisions made on Ryan's behalf.

| ID | Question | Options considered | Decision and reason |
|----|----------|--------------------|---------------------|
| DEC-1 | Where does the server learn its display zone? | (a) a new `RIOT_DISPLAY_TZ` only; (b) standard `TZ` only; (c) mounting the host's `/etc/localtime`; (d) `RIOT_DISPLAY_TZ`, then `TZ`, then UTC | **(d).** `RIOT_DISPLAY_TZ` (an IANA name) is the documented setting and changes nothing but display. `TZ` is honored as a fallback because many Docker users already set it. Mounting `/etc/localtime` (c) gives no IANA name to pass to the browser. The server never changes `time.Local` itself. |
| DEC-2 | What happens when the zone is unset or invalid? | fail start; fall straight to UTC; try the next source | **Try the next source, then UTC.** An invalid value logs one warning that names the source and a sanitized, truncated copy of the value. The server always starts. `Local`, empty and whitespace-only values count as unset or invalid. |
| DEC-3 | Fixed 12-hour, or a separate hour-cycle preference? | (a) fixed 12-hour everywhere; (b) hour cycle tied to the zone; (c) a separate whitelisted preference with a 12-hour default | **(c).** Ryan asked for "12hour format **too**", so 24-hour stays available. The hour-cycle preference has its own storage entry with an enum value, defaulting to 12-hour. |
| DEC-4 | How does the header clock choose among 3 zones and 2 hour cycles? | (a) each click cycles through 6 combinations; (b) a Settings page; (c) a small panel opened from the clock, with two radio groups | **(c).** Cycling through 6 states is not discoverable, and a Settings page hides a one-click choice. The panel keeps the clock as the single entry point that UIF established. |
| DEC-5 | What happens to a preference stored by an older build? | honor the legacy `UTC`; reset everything; map selectively | **A legacy `UTC` becomes Server and a legacy `Local` stays Local.** The old build wrote `UTC` whenever the clock was toggled back, so the value does not prove a deliberate choice, and Ryan explicitly asked for server time rather than UTC. A UTC choice made after this story persists normally. |
| DEC-6 | Which zone and hour cycle does server-written text use? | follow the browser preference; 24-hour; 12-hour in the display zone | **12-hour in the server display zone, always with a zone label.** ntfy pushes, Markdown files and stored event messages have no browser, so they cannot follow a browser preference. This supersedes UIF BR-007 for server text: maintenance-window instants in the patch badge show the display zone, not UTC. |
| DEC-7 | What is the zone label? | IANA name; numeric offset; short abbreviation | **The short name.** On the dashboard this is the `en-US` `Intl` short name (for example `EDT`, `PDT`, `UTC`, `GMT-3`). On the server it is the zone database abbreviation (`EDT`, `IST`). When that abbreviation is numeric (for example `-03` or `+0545`), the server writes `GMT-3` or `GMT+5:45` to match the browser form. For non-US zones the two surfaces can still differ (for example `BST` in an ntfy push and `GMT+1` on the dashboard). Both are correct and unambiguous, and this difference is accepted. |
| DEC-8 | What exact 12-hour shape? | `01:05 PM`; `1:05 PM`; `1:05pm` | **`1:05 PM`:** no leading zero on the hour, a space, and uppercase `AM`/`PM`. Midnight is `12:00 AM` and noon is `12:00 PM`. Dates stay `YYYY-MM-DD`. The 24-hour output is exactly today's output (`HH` zero-padded, midnight `00`). |
| DEC-9 | Do activity clock times gain a zone label? | keep them unlabeled; add the label | **Add the label** (for example `Rebooting, back by 1:20 PM EDT`). The scope rule is "zone labels on absolute times", and the matching ntfy push now carries a label too. The fleet row already truncates to one line (PR #44), and its hover title shows the full text. Chart axis ticks stay unlabeled because the tooltip carries the label (UIF A-008). |
| DEC-10 | Is systemd calendar text ("Daily at 06:00") reformatted? | leave it; reformat by hour cycle; convert the zone | **Reformat by hour cycle, with no zone conversion and no zone label.** The text describes a schedule in the agent host's own zone, not an instant, so it cannot be converted. It can be written in 12-hour form (`Daily at 6:00 AM`). "midnight" wording stays. |
| DEC-11 | Does the maintenance-window editor (AgentManagement) follow the display zone? | follow the display zone; stay browser-local | **Stay as is (out of scope).** It is an input that converts browser-local to stored UTC, and its label already states this honestly. Changing input semantics is a separate story. |
| DEC-12 | Do the Docker activity "From (UTC)" / "To (UTC)" filters follow the zone? | follow the zone; stay UTC | **Stay UTC (out of scope).** They are labelled input fields, and changing what a typed value means is riskier than the display change. |
| DEC-13 | Which date goes in the device summary download filename? | UTC date; display-zone date | **The display-zone date.** It then matches the `Generated` line inside the file. The UTC date can be one day ahead in the evening in the Americas. |
| DEC-14 | Which date does the patch badge label "Auto (reboot on …)" use? | UTC date; display-zone date; date plus time | **The display-zone date, with no time and no zone label.** It is a date-only label on a one-line fleet row, and its explanation line gives the full labelled time. `reboot_at` in the API stays RFC 3339 UTC. |
| DEC-15 | How does the dashboard learn the server zone, and what does it show before it knows? | an unauthenticated endpoint; an authenticated endpoint; cache the zone in localStorage | **Authenticated callers only. The architect picks the endpoint (an existing one such as `auth/check` when authenticated, or a new admin endpoint).** The zone is read once per page load and not cached. Until it is known, and whenever it is invalid, Server formats as UTC with the label `UTC`. The demo build supplies `America/New_York`. |
| DEC-16 | Do time-only server messages ("powered off at", "back by", "held since") gain a date? | add the date; keep time-only and add the zone label | **Keep time-only and add the zone label.** These are short pushes about the next minutes or hours. The label settles the DST-repeated hour (`1:30 AM EDT` vs `1:30 AM EST`). |
| DEC-17 | Are event messages already stored rewritten? | rewrite them; leave them | **Leave them.** Only new events use the new text. Rewriting would touch up to 90 days of rows for no operational gain, and retention removes the old text. |
| DEC-18 | What happens to `formatUtc` (no production callers)? | delete it; change it; keep it | **Keep it unchanged.** Removing dead code is outside this story. UIF FR-094 still holds. |

### Superseded UI-FOUNDATION-001 items

| UIF item | Status after this story |
|----------|-------------------------|
| FR-092 (24-hour only) | Superseded by FR-030..FR-036. The 24-hour output is unchanged when 24-hour is selected. |
| FR-099 (`UTC`/`Local`, default UTC) | Superseded by FR-040 (Server/UTC/Local, default Server). |
| FR-100 (header clock time form) | Amended by FR-041: it follows the zone and the hour cycle. |
| FR-101 (click toggles UTC/Local) | Superseded by FR-042..FR-046 (the click opens the time-display panel). |
| FR-103 (fallback to UTC) | Superseded by FR-048 (fallback to Server, 12-hour). |
| FR-105 (chart ticks in the preferred zone) | Amended by FR-036: ticks also follow the hour cycle. |
| BR-006 (default UTC) | Superseded by BR-005. |
| BR-007 (maintenance-window times always UTC) | Superseded for server-written text by DEC-6/FR-020..FR-023. The editor is unchanged (DEC-11). |
| A-005 (24-hour canonical) | Superseded by DEC-3/DEC-8. |
| OQ-002 (Go-rendered artifacts) | Answered: they follow the server display zone (DEC-6). |
| AC-104, AC-105 | Still valid with the 24-hour cycle selected explicitly (AC-006, AC-007). |
| AC-113, AC-114, AC-115, AC-116, AC-117, AC-118 | Superseded by AC-021..AC-030 and AC-013. Their tests must be updated, not deleted (FR-072). |

## 3. Actors

| Actor | Description | Permissions |
|-------|-------------|-------------|
| Fleet operator (Ryan) | Authenticated dashboard user (password or SSO). | Reads times and chooses the time display. This story adds and removes no permission. |
| Server operator (Ryan) | Sets the server's environment (compose file). | Sets `RIOT_DISPLAY_TZ` and/or `TZ`. |
| Notification recipient (Ryan's phone) | Receives ntfy pushes built from event messages. | Read only. |
| Demo visitor | Anonymous user of the static demo build. | Reads the simulated data, including a simulated server zone. |
| rIOt server | Go server. | Resolves the display zone at startup, writes human text in it, and reports it to authenticated dashboard sessions. |
| Browser storage | localStorage. | Holds two enum preferences. It may be unavailable or tampered with. |

## 4. Functional Requirements

Paths are relative to the repository root unless stated. "Display zone" means
the zone resolved by FR-001..FR-008. "Example instant" means
`2026-10-27T17:05:09Z`.

### 4.1 Server display zone

- **FR-001:** At startup the server must resolve one display zone by trying, in
  order: `RIOT_DISPLAY_TZ`, then `TZ`, then UTC. It must use the first source
  whose value is valid under FR-002.
- **FR-002:** A candidate value is valid only when all of these hold after
  trimming surrounding whitespace: it is non-empty; it is at most 64 characters
  long; it consists of 1 to 3 `/`-separated segments; each segment starts with a
  letter and contains only ASCII letters, digits, `_`, `+` and `-`; it is not
  `Local`; and the server's zone database loads it.
- **FR-003:** An empty or whitespace-only source must be treated as unset and
  must not log a warning.
- **FR-004:** A source that is set but invalid must log exactly one warning per
  process start. The warning names the source variable and includes the
  rejected value with control characters removed, cut to 64 characters.
- **FR-005:** The server must log exactly one informational line per process
  start that states the effective display zone name and its source
  (`RIOT_DISPLAY_TZ`, `TZ` or `default`).
- **FR-006:** No zone configuration, valid or invalid, may prevent the server
  from starting or cause a panic.
- **FR-007:** The server must not change the process-wide local zone
  (`time.Local`) and must not read it to produce any human text in Section 4.3.
- **FR-008:** The effective display zone name must be the validated, trimmed
  source value (for example `America/New_York`), or `UTC` when no source is
  valid.

### 4.2 Exposing the display zone to the dashboard

- **FR-010:** An authenticated dashboard session must be able to read the
  effective display zone name (FR-008) from the API.
- **FR-011:** The display zone name must not be returned to unauthenticated
  requests or to agent-key (`X-rIOt-Key`) requests.
- **FR-012:** The addition must be backward compatible. No existing response
  field changes name, type or meaning.

### 4.3 Server-written human text

All server text in this section uses the display zone and 12-hour form, and
every time in it carries a zone label (DEC-6, DEC-7, DEC-8). The Go layouts are
equivalent to: full `2006-01-02 3:04:05 PM <ZONE>`, minute
`2006-01-02 3:04 PM <ZONE>`, clock `3:04 PM <ZONE>`, and date `2006-01-02`.

- **FR-020:** The patch badge "automatic patching" line for a window schedule
  must read `Yes. Next window starts <minute form>.` For example:
  `Yes. Next window starts 2026-10-28 2:00 AM EDT.`
- **FR-021:** The patch badge dated reboot line must end with
  `on <minute form> (estimate).` and must no longer contain the literal
  ` UTC (estimate)` unless the display zone is UTC.
- **FR-022:** The patch badge "as of" line must read
  `<minute form> (<age>)`. The age text (`just now`, `N min ago`, `N h ago`,
  `N d ago`) and its thresholds must not change.
- **FR-023:** The patch badge label for a dated automatic reboot must read
  `Auto (reboot on <date>)`, where `<date>` is the date of the reboot instant
  in the display zone (DEC-14).
- **FR-024:** The device Markdown summary `Generated` value must use the full
  form, for example `2026-10-27 1:05:09 PM EDT`.
- **FR-025:** The device summary download filename must be
  `<sanitized-host>-summary-<date>.md`, where `<date>` is the display-zone date
  of the generation instant (DEC-13). Hostname sanitizing must not change.
- **FR-026:** The powered-off event message must read
  `Device <host> powered off <actor phrase> at <clock form>`.
- **FR-027:** The suppressed-offline event message must render its "back by"
  part as `, back by <clock form>`. The rest of the message must not change.
- **FR-028:** The held-escalation summary message must read
  `<n> escalation notifications for <host> were held since <clock form>; see Alerts`.
- **FR-029:** Notifications built from the events in FR-026..FR-028 must carry
  the event message text unchanged, so the ntfy push shows the same labelled
  time.
- **FR-029a:** When the zone database gives a numeric abbreviation (it begins
  with `+` or `-`), the zone label must be `GMT`, then the sign, then the hours
  without a leading zero, then `:MM` only when the minutes are not zero. For
  example `-03` becomes `GMT-3`, `+0530` becomes `GMT+5:30` and `+0545` becomes
  `GMT+5:45`. Otherwise the label is the abbreviation unchanged (`EDT`, `EST`,
  `IST`, `UTC`).
- **FR-029b:** All API time fields (for example `reboot_at`, `updates_at` and
  event `created_at`) must stay RFC 3339 in UTC. Stored timestamps must stay
  UTC. Partition, quota, retention and maintenance-window computations must
  produce identical results whatever the display zone or process local zone.

### 4.4 Dashboard formats

The examples use the example instant with the zone America/New_York (EDT).

- **FR-030:** The dashboard must support two hour cycles, `12h` and `24h`.
- **FR-031:** In 12-hour form the absolute-timestamp formatter must render the
  second form `YYYY-MM-DD h:mm:ss AM|PM ZONE` (`2026-10-27 1:05:09 PM EDT`),
  the minute form `YYYY-MM-DD h:mm AM|PM ZONE` (`2026-10-27 1:05 PM EDT`) and
  the time form `h:mm:ss AM|PM ZONE` (`1:05:09 PM EDT`). The hour has no leading
  zero. Midnight is `12` with `AM` and noon is `12` with `PM`.
- **FR-032:** In 24-hour form the formatter output must be exactly today's
  output (`2026-10-27 13:05:09 EDT`, `2026-10-27 13:05 EDT`, `13:05:09 EDT`).
- **FR-033:** The formatter must provide a clock form with a zone label:
  `h:mm AM|PM ZONE` in 12-hour form (`1:05 PM EDT`) and `HH:mm ZONE` in
  24-hour form (`13:05 EDT`).
- **FR-034:** The zone label must be `UTC` for the UTC zone. For any other zone
  it must be the `en-US` short time-zone name for that instant (`EDT`, `EST`,
  `PDT`, `GMT-3`).
- **FR-035:** Missing or unparseable input must render `—` in every form and
  hour cycle, and the formatter must never throw.
- **FR-036:** Chart axis ticks must render `h:mm AM|PM` in 12-hour form
  (`1:05 PM`) and `HH:mm` in 24-hour form (`13:05`), with no zone label. Chart
  tooltips must use the second form with its zone label.
- **FR-037:** Relative times (`just now`, `5m ago`, `in 10m`) must not change
  in any zone or hour cycle. The `<RelativeTime>` title must use the second form
  of the current preference.
- **FR-038:** The dashboard must accept a server zone name from the API only
  when it meets the FR-002 shape rules (length, segments, characters, not
  `Local`) **and** the browser's `Intl.DateTimeFormat` accepts it without
  throwing. Any other value (missing, non-string, empty, rejected by shape,
  rejected by `Intl`) must be treated as UTC.
- **FR-039:** No string that came from the API or from storage may be passed to
  `Intl` as a time zone unless it has passed FR-038.

### 4.5 Preference and the header control

- **FR-040:** The time-zone preference must have exactly three values,
  `Server`, `UTC` and `Local`. The default is `Server`. The hour-cycle
  preference must have exactly two values, `12h` and `24h`. The default is
  `12h`.
- **FR-041:** The header clock must show the current time in the time form
  (FR-031/FR-032) for the current zone and hour cycle, updated every second.
- **FR-042:** The header clock must be a button. Activating it must open a
  time-display panel, and the button must expose `aria-expanded` with the
  panel's state.
- **FR-043:** The panel must contain a radio group named "Time zone" with
  options whose accessible names are `Server (<server zone>)`, `Local (<browser
  IANA zone>)` and `UTC`. It must also contain a radio group named "Clock" with
  the options `12-hour` and `24-hour`. The current values must be checked.
  `<server zone>` is the accepted server zone name, or `UTC` when FR-038 rejects
  it or it is not yet known.
- **FR-044:** Selecting an option must apply right away to the header clock and
  every rendered absolute time, without a reload or remount, and must persist it.
- **FR-045:** The panel must close on Escape, on a click outside it, and on a
  second activation of the clock. Focus must return to the clock button when the
  panel closes through Escape. Selecting an option must not close the panel.
- **FR-046:** The header clock's accessible name must be
  `Time display: <zone>, <cycle>. Change time display`. `<zone>` is
  `Server (<server zone>)`, `Local (<browser IANA zone>)` or `UTC`, and
  `<cycle>` is `12-hour` or `24-hour`. For example:
  `Time display: Server (America/New_York), 12-hour. Change time display`.
- **FR-047:** Both preferences must be stored in localStorage as enum values and
  restored on the next load. The stored zone preference must never hold a zone
  name.
- **FR-048:** An unreadable, throwing or unrecognized stored value must fall back
  to `Server` for the zone and to `12h` for the hour cycle, without showing or
  throwing an error. If writing fails, the selection must still apply for the
  current session.
- **FR-049:** A zone preference written by a build before this story must
  resolve as follows: `UTC` becomes `Server` and `Local` stays `Local` (DEC-5).
  After this story, a `UTC` selection must persist as UTC across reloads.
- **FR-050:** While the server zone is unknown (request pending or failed), the
  Server zone must format as UTC with the label `UTC`. When the zone arrives,
  every rendered time must update without a reload.

### 4.6 Dashboard call sites

- **FR-060:** Every `<Timestamp>` and `<RelativeTime>` call site must follow
  both preferences through the canonical formatter. This covers ActivityLog,
  ContainerDetail (events and log lines), SelfUpdateDetails, Alerts, DeviceDetail
  (certificates, logs, events), DeviceProbes, docker/ActivityPage rows,
  ProbeDetail, settings/CertificateSettings and settings/LogSettings.
- **FR-061:** The charts (MetricChart, ContainerMetricChart,
  docker/IOMetricChart, dashboard/PerDeviceCharts) must follow both preferences
  per FR-036.
- **FR-062:** Activity labels and titles (`ActivityLabel.tsx`,
  `AgentUpdatingVersion.tsx`) must render their clock times in the clock form
  with a zone label (FR-033, DEC-9). This covers "back by", "settling until",
  "Powered off … at" and "started".
- **FR-063:** Cron and timer run dates (`utils/cron.ts` `formatRunDate`) must
  render in the minute form of the current preferences at every call site. No
  call site may rely on the UTC default argument.
- **FR-064:** Systemd calendar text must follow the hour-cycle preference with
  no zone conversion and no zone label, as listed in Section 7.5. "midnight"
  wording must not change, and `AM`/`PM` must stay uppercase in every phrase
  (DEC-10).

### 4.7 Surfaces that must not change

- **FR-065:** The maintenance-window editor in
  `pages/settings/AgentManagement.tsx` must keep its current behavior and label
  text (DEC-11).
- **FR-066:** The Docker activity page "From (UTC)" and "To (UTC)" filter inputs
  must keep their labels and UTC meaning (DEC-12).
- **FR-067:** `formatUtc` must keep its exact output (DEC-18).

### 4.8 Demo build

- **FR-068:** The demo API must supply the server zone `America/New_York`.
- **FR-069:** The demo patch-badge `as_of` text must match the new server format:
  `2026-10-18 10:00 PM EDT (just now)`, which is the existing instant
  2026-10-19 02:00 UTC in New York.

### 4.9 Documentation and tests

- **FR-070:** `README.md` "Time Display" must describe: the Server/Local/UTC
  zones and the 12-/24-hour choice; the defaults; the clock panel; the storage
  keys and their fallbacks; the legacy-value mapping (FR-049);
  `RIOT_DISPLAY_TZ` with its fallback chain (FR-001) and validation; that
  server-written text (patch badge, summary, event messages and ntfy pushes)
  uses the display zone in 12-hour form; and that APIs and stored data stay
  UTC. The sentence "Reboot dates in the patch badge and maintenance-window
  times always show UTC" must be replaced. The API section must list the
  display-zone field.
- **FR-070a:** `README.md` must list `RIOT_DISPLAY_TZ` wherever server
  environment variables are listed.
- **FR-071:** `.env.example` and `docker-compose.yml` must each carry a
  commented example `RIOT_DISPLAY_TZ=America/New_York` with a one-line
  explanation. `web/src/components/ui/README.md` must document the new formats.
- **FR-072:** Existing tests tagged with superseded UIF ACs must be updated to
  name their zone and hour cycle explicitly. They must not be deleted or skipped.

## 5. Non-Functional Requirements

- **NFR-001 [Bounded growth]:** The story must add no database table, column,
  index or migration, no file on disk, and no server-side persisted state. Its
  only growth sources are bounded as follows:
  (a) Logs: at most 1 informational line and 2 warnings per process start (FR-004,
  FR-005). Nothing is logged per request or per render.
  (b) New event messages: at most 24 bytes longer than today (two times growing
  from `15:04` to at most `12:59 PM GMT+5:45`). Existing 90-day event retention
  bounds them.
  (c) Browser: at most 2 localStorage keys, each holding a value of at most
  8 characters.
  (d) Dashboard formatter cache: at most 12 entries per page lifetime, whatever
  the number of format calls or rejected zone strings.
- **NFR-002 [Security]:** Zone strings are validated before use on both sides
  (FR-002, FR-038, FR-039). A rejected value is logged sanitized and truncated
  (FR-004). The zone name is disclosed only to authenticated sessions (FR-011).
  The stored preferences stay enum-whitelisted (FR-047).
- **NFR-003 [Availability]:** Zone resolution or formatting failures must
  degrade to UTC and never stop the server, panic, throw in the browser, or blank
  a page.
- **NFR-004 [Performance]:** The header clock's one-second tick must not
  re-render anything outside the clock (UIF NFR-008 still holds). Resolving the
  server zone costs one zone-database load at startup.
- **NFR-005 [Dependencies]:** `web/package.json` dependencies and
  devDependencies, and the `go.mod` require set, must be identical to `main`.
  Standard-library packages are not dependencies.
- **NFR-006 [Compatibility]:** A dashboard from before this story against a new
  server, and a new dashboard against a server that does not report a zone, must
  both work, the latter treating Server as UTC.
- **NFR-007 [Quality]:** `go test ./...`, `npm run test:run`, `npx tsc --noEmit`
  and both web builds (with and without `VITE_DEMO=true`) must pass. Neither
  test count may drop below `main`.
- **NFR-008 [Accessibility]:** The panel must be operable by keyboard alone:
  Tab reaches the clock, Enter or Space opens the panel, arrow keys move and
  select within each radio group, and Escape closes. Every control shows the
  shared focus-visible ring.

## 6. Business Rules

- **BR-001:** Stored data and API time fields are UTC (RFC 3339). Zone and
  hour-cycle conversions happen only when text is rendered for a person.
- **BR-002:** Every absolute time shown to a person carries a zone label. There
  are three exceptions: chart axis ticks (their tooltip carries the label),
  date-only labels (FR-023, FR-025), and host-local schedule text (FR-064).
- **BR-003:** Partition, quota, retention and maintenance-window boundaries are
  computed in UTC whatever `RIOT_DISPLAY_TZ` or `TZ` is set to.
- **BR-004:** Server-written text uses the server display zone in 12-hour form,
  independent of any browser preference.
- **BR-005:** The dashboard defaults to the Server zone in 12-hour form.
- **BR-006:** A zone name from configuration or from the API is used only after
  validation. A failed validation means UTC. Browser storage never supplies a
  zone name.
- **BR-007:** Zone configuration problems never stop the server or break a page.
- **BR-008:** Relative times are unchanged.

## 7. Data Requirements

### 7.1 Entities

| Entity | Fields | Rules |
|--------|--------|-------|
| DisplayZone (server, in memory) | name (IANA string or `UTC`), source (`RIOT_DISPLAY_TZ` \| `TZ` \| `default`) | Resolved once at startup (FR-001..FR-008). Never persisted. |
| TimePreference (browser) | zone: `Server` \| `UTC` \| `Local`; hourCycle: `12h` \| `24h` | Default Server / 12h. Enum-whitelisted storage (FR-047..FR-049). |
| ServerZone (browser, in memory) | name or unknown | From the API, validated by FR-038. Not stored. |

### 7.2 Validation rules

- A zone name is at most 64 characters, 1 to 3 `/`-separated segments, each
  `[A-Za-z][A-Za-z0-9_+-]*`, and never `Local`. It must also load in the server's
  zone database (server side) or be accepted by `Intl.DateTimeFormat` (browser
  side).
- Surrounding whitespace is trimmed on the server. The browser does not trim:
  a value with surrounding whitespace is rejected.

### 7.3 Server display-zone resolution

| `RIOT_DISPLAY_TZ` | `TZ` | Effective zone | Warnings |
|-------------------|------|----------------|----------|
| valid | any | `RIOT_DISPLAY_TZ` value | 0 |
| unset/empty | valid | `TZ` value | 0 |
| unset/empty | unset/empty | `UTC` | 0 |
| invalid | valid | `TZ` value | 1 |
| invalid | unset/empty | `UTC` | 1 |
| unset/empty | invalid | `UTC` | 1 |
| invalid | invalid | `UTC` | 2 |

### 7.4 Browser preference resolution

Zone preference:

| Stored zone value | Resolved |
|-------------------|----------|
| absent | Server |
| legacy `UTC` (pre-story) | Server |
| legacy `Local` (pre-story) | Local |
| post-story Server / UTC / Local | same |
| anything else, including an IANA name | Server |
| storage throws | Server |

Hour-cycle preference:

| Stored hour-cycle value | Resolved |
|-------------------------|----------|
| absent | 12h |
| `12h` | 12h |
| `24h` | 24h |
| anything else | 12h |
| storage throws | 12h |

The architect chooses the storage key names and how pre-story and post-story
values are told apart.

### 7.5 Systemd calendar text (FR-064)

| Expression | 24-hour (unchanged) | 12-hour |
|------------|---------------------|---------|
| `*-*-* 00:00:00` | Daily at midnight | Daily at midnight |
| `*-*-* 06:00:00` | Daily at 06:00 | Daily at 6:00 AM |
| `*-*-* 12:00:00` | Daily at 12:00 | Daily at 12:00 PM |
| `*-*-* 00:07:00` | Daily at 00:07 | Daily at 12:07 AM |
| `*-*-* 06,18:00:00` | Daily at 06:00, 18:00 | Daily at 6:00 AM, 6:00 PM |
| `*-*-* 00,12:00:00` | Daily at 00:00, 12:00 | Daily at 12:00 AM, 12:00 PM |
| `Sun *-*-* 03:10:00` | Every Sunday at 03:10 | Every Sunday at 3:10 AM |
| `Mon *-*-* 00:00:00` | Every Monday at midnight | Every Monday at midnight |
| `*-*-01 00:00:00` | Monthly on day 1 at midnight | Monthly on day 1 at midnight |
| `*-*-* *:00/10:00` | Every 10 minutes | Every 10 minutes |
| `*-*-* *:00:00` | Every hour | Every hour |

### 7.6 Time-display inventory (goes into the PR body)

The examples use the instant `2026-10-27T17:05:09Z`, display zone
America/New_York, and the default preference (Server, 12-hour) unless noted.

| # | Surface | Where | Current | Target | In scope |
|---|---------|-------|---------|--------|----------|
| 1 | Header clock | `HeaderClock.tsx` | `17:05:09 UTC` (24h, UTC default); click toggles UTC/Local | `1:05:09 PM EDT`; click opens Time zone / Clock panel | Yes |
| 2 | Absolute timestamp, second | `<Timestamp>` in ActivityLog, ContainerDetail events, SelfUpdateDetails, Alerts, DeviceDetail logs/events, DeviceProbes, docker/ActivityPage rows, ProbeDetail, CertificateSettings keys, LogSettings | `2026-10-27 17:05:09 UTC` | `2026-10-27 1:05:09 PM EDT` | Yes |
| 3 | Absolute timestamp, minute | `<Timestamp precision="minute">` in DeviceDetail certificates, CertificateSettings cert expiry | `2026-10-27 17:05 UTC` | `2026-10-27 1:05 PM EDT` | Yes |
| 4 | Log line time | ContainerDetail log lines (`precision="time"`) | `17:05:09 UTC` | `1:05:09 PM EDT` | Yes |
| 5 | Relative time text | `<RelativeTime>` | `5m ago` | `5m ago` (unchanged) | Unchanged |
| 6 | Relative time title | `<RelativeTime>` title | `2026-10-27 17:05:09 UTC` | `2026-10-27 1:05:09 PM EDT` | Yes |
| 7 | Chart axis ticks | MetricChart, ContainerMetricChart, docker/IOMetricChart | `17:05` | `1:05 PM` (no zone label) | Yes |
| 8 | Chart tooltips | the charts above and dashboard/PerDeviceCharts | `2026-10-27 17:05:09 UTC` | `2026-10-27 1:05:09 PM EDT` | Yes |
| 9 | Activity labels | `ActivityLabel.tsx`, `AgentUpdatingVersion.tsx` via `utils/activity.ts` | `Rebooting, back by 17:20` (no zone) | `Rebooting, back by 1:20 PM EDT` | Yes |
| 10 | Activity titles | same, "· started" | `· started 17:05` | `· started 1:05 PM EDT` | Yes |
| 11 | Cron/timer run dates | `utils/cron.ts` `formatRunDate` | `2026-10-27 17:05 UTC` | `2026-10-27 1:05 PM EDT` | Yes |
| 12 | Systemd calendar text | `utils/cron.ts` `parseSystemdCalendar` | `Daily at 06:00` | `Daily at 6:00 AM` (host-local, no conversion) | Yes |
| 13 | Maintenance-window editor | `settings/AgentManagement.tsx` | browser-local `HH:MM` input, "Times are shown in your local timezone…" | unchanged | No (DEC-11) |
| 14 | Docker activity filters | `docker/ActivityPage.tsx` | "From (UTC)" / "To (UTC)" | unchanged | No (DEC-12) |
| 15 | `formatUtc` | `utils/format.ts` (no production callers) | `2026-10-27 17:05 UTC` | unchanged | No (DEC-18) |
| 16 | Demo patch badge as-of | `api/demo-data.ts` | `2026-10-19 02:00 UTC (just now)` | `2026-10-18 10:00 PM EDT (just now)` | Yes |
| 17 | Patch badge label | `patch_badge.go` | `Auto (reboot on 2026-10-28)` (UTC date) | `Auto (reboot on 2026-10-27)` for a 2026-10-28T02:00Z reboot (display-zone date) | Yes |
| 18 | Patch badge "automatic" line | `patch_badge.go` | `Yes. Next window starts 2026-10-28 06:00 UTC.` | `Yes. Next window starts 2026-10-28 2:00 AM EDT.` | Yes |
| 19 | Patch badge reboot line | `patch_badge.go` `rebootLine` | `… on 2026-10-28 06:00 UTC (estimate).` | `… on 2026-10-28 2:00 AM EDT (estimate).` | Yes |
| 20 | Patch badge as-of | `patch_badge.go` `asOfText` | `2026-10-27 17:05 UTC (just now)` | `2026-10-27 1:05 PM EDT (just now)` | Yes |
| 21 | Device summary Generated | `summary/summary.go` | `2026-10-27 17:05:09 UTC` | `2026-10-27 1:05:09 PM EDT` | Yes |
| 22 | Device summary filename | `handlers.go` summary download | `web-01-summary-2026-10-28.md` at 2026-10-28T02:00Z (UTC date) | `web-01-summary-2026-10-27.md` (display-zone date) | Yes |
| 23 | Powered-off event / ntfy | `models/expect.go` | `Device web-01 powered off by ryan at 17:05` (process zone, no label) | `… at 1:05 PM EDT` | Yes |
| 24 | Expected-offline event / ntfy | `models/expect.go` | `…(expected, back by 17:20)` | `…(expected, back by 1:20 PM EDT)` | Yes |
| 25 | Held-escalation summary / ntfy | `expect/messages.go` | `…were held since 17:05; see Alerts` | `…were held since 1:05 PM EDT; see Alerts` | Yes |
| 26 | API time fields | all handlers | RFC 3339 UTC | unchanged | Unchanged |
| 27 | Stored timestamps, partition/quota/retention, maintenance-window math | db, `partition/`, `quota/daily.go`, `auto_update.go` | UTC | unchanged | Unchanged |
| 28 | Event messages already stored | events table | old text | unchanged (DEC-17) | No |
| 29 | Agent journalctl `--since` args | `internal/agent/…` | machine arguments, not display | unchanged | No |
| 30 | Server logs (slog) | server | RFC 3339 | unchanged | No |

## 8. Acceptance Criteria

Verification tags: **[vitest]** is a vitest / Testing Library test, which may be
a static source scan. **[go test]** is a Go unit or handler test with an
injected clock and zone. **[build]** is a command-line build or test run.
**[doc-check]** is a review of the named file. **[screenshot: name]** is a
capture from the demo build saved to
`docs/implementation/TIME-12H-001-screenshots/`.

Unless stated otherwise: instant `2026-10-27T17:05:09Z`; server zone
America/New_York; browser (Local) zone America/Los_Angeles through the existing
test seam.

### 8.1 Dashboard formats

**AC-001 — 12-hour second form [FR-031]** [vitest]
Given the Server zone, 12-hour, and the example instant
When it is formatted at second precision
Then the output is `2026-10-27 1:05:09 PM EDT`.

**AC-002 — 12-hour minute form [FR-031]** [vitest]
Given the Server zone, 12-hour, and the example instant
When it is formatted at minute precision
Then the output is `2026-10-27 1:05 PM EDT`.

**AC-003 — 12-hour time form [FR-031]** [vitest]
Given the Server zone, 12-hour, and the example instant
When it is formatted at time precision
Then the output is `1:05:09 PM EDT`.

**AC-004 — Clock form [FR-033]** [vitest]
Given the Server zone and the example instant
When it is formatted in clock form with 12-hour and then with 24-hour
Then the outputs are `1:05 PM EDT` and `13:05 EDT`.

**AC-005 — 24-hour forms unchanged [FR-032]** [vitest]
Given the Server zone, 24-hour, and the example instant
When it is formatted at second, minute and time precision
Then the outputs are `2026-10-27 13:05:09 EDT`, `2026-10-27 13:05 EDT` and `13:05:09 EDT`.

**AC-006 — UTC zone [FR-031, FR-032, FR-034]** [vitest]
Given the UTC zone and the example instant
When it is formatted at second precision with 12-hour and then with 24-hour
Then the outputs are `2026-10-27 5:05:09 PM UTC` and `2026-10-27 17:05:09 UTC`
And with 24-hour the instant `2026-10-17T14:03:22Z` gives `2026-10-17 14:03:22 UTC` (UIF AC-104 preserved).

**AC-007 — Local zone [FR-031, FR-032]** [vitest]
Given the Local zone (America/Los_Angeles) and the example instant
When it is formatted at second precision with 12-hour and then with 24-hour
Then the outputs are `2026-10-27 10:05:09 AM PDT` and `2026-10-27 10:05:09 PDT`.

**AC-008 — Midnight [FR-031, FR-032]** [vitest]
Given the Server zone and the instant `2026-10-27T04:00:00Z`
When it is formatted at second precision with 12-hour and then with 24-hour
Then the outputs are `2026-10-27 12:00:00 AM EDT` and `2026-10-27 00:00:00 EDT`.

**AC-009 — Noon [FR-031, FR-032]** [vitest]
Given the Server zone and the instant `2026-10-27T16:00:00Z`
When it is formatted at second precision with 12-hour and then with 24-hour
Then the outputs are `2026-10-27 12:00:00 PM EDT` and `2026-10-27 12:00:00 EDT`.

**AC-010 — Day boundary differs from UTC [FR-031]** [vitest]
Given the Server zone, 12-hour
When `2026-10-28T03:59:59Z` and `2026-10-28T02:00:00Z` are formatted at second precision
Then the outputs are `2026-10-27 11:59:59 PM EDT` and `2026-10-27 10:00:00 PM EDT`.

**AC-011 — DST fall-back repeated hour [FR-031, FR-034]** [vitest]
Given the Server zone, 12-hour
When `2026-11-01T05:30:00Z` and `2026-11-01T06:30:00Z` are formatted at second precision
Then the outputs are `2026-11-01 1:30:00 AM EDT` and `2026-11-01 1:30:00 AM EST`.

**AC-012 — DST spring-forward gap [FR-031, FR-034]** [vitest]
Given the Server zone, 12-hour
When `2026-03-08T06:59:59Z` and `2026-03-08T07:00:00Z` are formatted at second precision
Then the outputs are `2026-03-08 1:59:59 AM EST` and `2026-03-08 3:00:00 AM EDT`.

**AC-013 — Chart ticks and tooltips [FR-036, FR-061]** [vitest]
Given the Server zone
When the example instant is formatted as a chart tick with 12-hour and then with 24-hour, and `2026-10-27T04:00:00Z` as a 12-hour tick
Then the ticks are `1:05 PM`, `13:05` and `12:00 AM`, none containing a zone label
And the 12-hour tooltip for the example instant is `2026-10-27 1:05:09 PM EDT`.

**AC-014 — Invalid input [FR-035]** [vitest]
Given each of `"not-a-date"`, `undefined` and `""`
When each is formatted in every precision, the clock form and the tick form, under both hour cycles
Then every output is `—`
And no call throws.

**AC-015 — Relative times unchanged [FR-037, BR-008]** [vitest]
Given a fixed now and the instants 30 s, 5 min, 3 h and 2 d in the past and 10 min in the future
When they are formatted under each zone and hour-cycle combination
Then the outputs are always `just now`, `5m ago`, `3h ago`, `2d ago` and `in 10m`.

**AC-016 — RelativeTime title [FR-037]** [vitest]
Given `<RelativeTime>` for the example instant with the default preference
When it renders
Then its `title` is `2026-10-27 1:05:09 PM EDT`.

### 8.2 Server zone on the dashboard

**AC-017 — Valid server zone used [FR-038]** [vitest]
Given the API reports the server zone `America/New_York`
When the Server zone formats the example instant at second precision
Then the output ends with `EDT`
And the Server option's accessible name is `Server (America/New_York)`.

**AC-018 — Rejected server zones fall back to UTC [FR-038, BR-006, NFR-003]** [vitest]
Given the API reports, in turn: `Mars/Olympus`, `Local`, `""`, `"   "`, `" America/New_York"`, `"<img src=x>"`, `"America/New_York\n"`, `"../etc/passwd"`, a 65-character letter string, the number `5`, `null`, and a missing field
When the Server zone formats the example instant at second precision with 12-hour
Then each output is `2026-10-27 5:05:09 PM UTC`
And the Server option's accessible name is `Server (UTC)`
And no error is thrown.

**AC-019 — Unknown, then known [FR-050]** [vitest]
Given the server-zone request has not resolved
When the header renders with the default preference
Then the clock ends with `UTC`
And when the request resolves with `America/New_York`
Then the clock ends with `EDT` without a remount.

**AC-020 — Intl never sees an unvalidated zone [FR-039]** [vitest]
Given a spy on `Intl.DateTimeFormat` and the API reporting `Mars/Olympus`, and separately localStorage holding an IANA name as the zone preference
When the app formats times under every zone option
Then every `timeZone` passed to `Intl.DateTimeFormat` for formatting is `UTC`, a value that passed FR-038, the Local test-seam zone, or undefined (browser zone)
And `Mars/Olympus` is passed to `Intl` only by the FR-038 acceptance check, never for formatting.

### 8.3 Preference and header control

**AC-021 — Defaults [FR-040, FR-041, BR-005]** [vitest]
Given empty localStorage, a fixed now of the example instant, and the server zone `America/New_York`
When the header renders
Then the clock text is `1:05:09 PM EDT`.

**AC-022 — Accessible name [FR-046]** [vitest]
Given the defaults from AC-021
When the clock button is queried by role
Then its accessible name is `Time display: Server (America/New_York), 12-hour. Change time display`
And after the user selects UTC and 24-hour, it is `Time display: UTC, 24-hour. Change time display`.

**AC-023 — Panel contents [FR-042, FR-043]** [vitest]
Given the defaults
When the user activates the clock
Then the clock has `aria-expanded="true"`
And a radio group named "Time zone" holds the radios `Server (America/New_York)`, `Local (America/Los_Angeles)` and `UTC`, with Server checked
And a radio group named "Clock" holds `12-hour` and `24-hour`, with 12-hour checked.

**AC-024 — Selecting a zone applies immediately [FR-044]** [vitest]
Given an open panel and a page showing a `<Timestamp>` for the example instant
When the user selects `UTC`
Then the clock text is `5:05:09 PM UTC`
And the timestamp reads `2026-10-27 5:05:09 PM UTC` without a remount
And the panel stays open.

**AC-025 — Selecting the hour cycle applies immediately [FR-044]** [vitest]
Given an open panel with the Server zone
When the user selects `24-hour`
Then the clock text is `13:05:09 EDT`.

**AC-026 — Closing the panel [FR-045]** [vitest]
Given an open panel
When the user presses Escape
Then the panel closes and focus is on the clock button
And when it is reopened and the user clicks outside it
Then it closes.

**AC-027 — Persistence [FR-047]** [vitest]
Given the user selected, in turn, each of Server, UTC and Local, each combined with 12-hour and 24-hour
When the app is unmounted and mounted again after each selection
Then the restored zone and hour cycle equal the last selection.

**AC-028 — Legacy values [FR-049, DEC-5]** [vitest]
Given storage that holds the pre-story zone value `UTC`
When the app mounts
Then the zone is Server
And given storage that holds the pre-story value `Local`
Then the zone is Local
And after the user selects UTC and the app remounts
Then the zone is UTC.

**AC-029 — Tampered or failing storage [FR-047, FR-048]** [vitest]
Given, in turn: a zone value `banana`, a zone value `America/Chicago`, an hour-cycle value `banana`, `getItem` throwing, and `setItem` throwing
When the app mounts (and, for `setItem`, the user selects UTC)
Then the zone resolves to Server and the hour cycle to 12h in the first four cases, with no error
And no `Intl` call receives `America/Chicago`
And when `setItem` throws, the UTC selection still applies for the session.

**AC-030 — Keyboard operation [NFR-008]** [vitest]
Given the header
When the user tabs to the clock, presses Enter, moves within "Time zone" with arrow keys to `UTC`, and presses Escape
Then UTC is selected, the panel is closed, and focus is on the clock.

**AC-031 — Header panel visual [FR-042, FR-043]** [screenshot: after-header-time-panel.png]
Given the demo build
When the clock is clicked
Then the panel shows the two groups with Server (America/New_York) and 12-hour selected, and the clock reads 12-hour time with `EDT` or `EST`.

### 8.4 Dashboard call sites

**AC-032 — Timestamp call sites [FR-060, BR-002]** [vitest]
Given ActivityLog, Alerts and CertificateSettings rendered with the example instant and the default preference
When their timestamps are read
Then each reads `2026-10-27 1:05:09 PM EDT`, or `2026-10-27 1:05 PM EDT` where the site uses minute precision.

**AC-033 — Container log line [FR-060]** [vitest]
Given a container log line at the example instant and the default preference
When ContainerDetail renders it
Then its time reads `1:05:09 PM EDT`.

**AC-034 — Activity labels [FR-062, DEC-9]** [vitest]
Given the default preference and activities with `back_by` = `2026-10-27T17:20:00Z` (reboot, return phase), `back_by` = `2026-10-27T17:20:00Z` (os_update, return phase, device online), and `powered_off_at` = example instant with actor phrase `by ryan`
When their labels are built
Then they read `Rebooting, back by 1:20 PM EDT`, `Patched, settling until 1:20 PM EDT` and `Powered off by ryan at 1:05 PM EDT`
And with 24-hour the first reads `Rebooting, back by 13:20 EDT`.

**AC-035 — Activity titles [FR-062]** [vitest]
Given a compact fleet-row ActivityLabel and an AgentUpdatingVersion for an activity started at the example instant
When their `title` attributes are read
Then each ends with `· started 1:05 PM EDT`.

**AC-036 — Cron run dates [FR-063]** [vitest]
Given the default preference
When `formatRunDate` formats the example instant as the ISO string `2026-10-27T17:05:09Z` and as the unix-seconds string `1793120709` through a rendered DeviceDetail cron/timer section
Then both read `2026-10-27 1:05 PM EDT`
And a source scan finds no production call to `formatRunDate` that omits the preference argument.

**AC-037 — Systemd calendar, 12-hour [FR-064]** [vitest]
Given the 12-hour cycle
When each expression in Section 7.5 is parsed
Then each output equals the 12-hour column exactly, including uppercase `AM`/`PM` in `Every Sunday at 3:10 AM`.

**AC-038 — Systemd calendar, 24-hour [FR-064]** [vitest]
Given the 24-hour cycle
When each expression in Section 7.5 is parsed
Then each output equals the 24-hour column
And all pre-existing `parseSystemdCalendar` tests pass unchanged.

**AC-039 — Untouched surfaces [FR-065, FR-066, FR-067]** [vitest]
Given the default preference
When AgentManagement's maintenance-window section and the Docker activity page render, and `formatUtc('2026-10-17T14:03:00Z')` is called
Then the maintenance-window label text equals its pre-story text
And the filter labels read "From (UTC)" and "To (UTC)"
And `formatUtc` returns `2026-10-17 14:03 UTC`.

### 8.5 Demo build

**AC-040 — Demo server zone [FR-068]** [vitest]
Given the demo API client
When the server zone is requested
Then it returns `America/New_York`.

**AC-041 — Demo patch as-of [FR-069]** [vitest]
Given the demo patch data
When the badge explanation is read
Then `as_of` is `2026-10-18 10:00 PM EDT (just now)`.

### 8.6 Server display zone

**AC-042 — RIOT_DISPLAY_TZ used [FR-001, FR-005, FR-008]** [go test]
Given `RIOT_DISPLAY_TZ=America/New_York` and `TZ` unset
When the configuration loads
Then the effective zone is `America/New_York` with source `RIOT_DISPLAY_TZ`
And exactly one informational line names both, and no warning is logged.

**AC-043 — Trimming [FR-002, FR-008]** [go test]
Given `RIOT_DISPLAY_TZ="  America/New_York  "`
When the configuration loads
Then the effective zone name is exactly `America/New_York`.

**AC-044 — TZ fallback [FR-001]** [go test]
Given `RIOT_DISPLAY_TZ` unset and `TZ=America/Chicago`
When the configuration loads
Then the effective zone is `America/Chicago` with source `TZ`.

**AC-045 — Default UTC [FR-001, FR-003]** [go test]
Given `RIOT_DISPLAY_TZ` and `TZ` both unset, and separately both set to `"   "`
When the configuration loads
Then the effective zone is `UTC` with source `default`
And no warning is logged.

**AC-046 — Invalid values rejected [FR-002, FR-004, FR-006]** [go test]
Given `TZ` unset and, in turn, `RIOT_DISPLAY_TZ` = `Mars/Olympus`, `Local`, `../../etc/passwd`, `America/New_York;rm -rf /`, `:/etc/localtime`, a 65-character letter string, and a value containing a newline and an ANSI escape
When the configuration loads
Then the effective zone is `UTC` in every case
And exactly one warning names `RIOT_DISPLAY_TZ` and contains the value with control characters removed and cut to at most 64 characters
And no panic occurs.

**AC-047 — Invalid first source, valid second [FR-001, FR-004]** [go test]
Given `RIOT_DISPLAY_TZ=Mars/Olympus` and `TZ=America/Denver`
When the configuration loads
Then the effective zone is `America/Denver`
And exactly one warning is logged.

**AC-048 — Both invalid [FR-004, NFR-001]** [go test]
Given `RIOT_DISPLAY_TZ=Mars/Olympus` and `TZ=:/etc/localtime`
When the configuration loads
Then the effective zone is `UTC`
And exactly two warnings and one informational line are logged.

**AC-049 — Process zone is not used [FR-007]** [go test]
Given the display zone America/New_York and a process local zone of Asia/Tokyo
When the powered-off message for the example instant is built
Then it ends with `at 1:05 PM EDT`
And `time.Local` is unchanged after configuration loads.

**AC-050 — Zone exposure [FR-010, FR-011, FR-012]** [go test]
Given the display zone America/New_York
When the designated endpoint is called with a valid admin session, with no session, and with only an `X-rIOt-Key` header
Then only the admin-session response contains `America/New_York`
And every pre-existing field of that response keeps its name, type and value.

**AC-051 — Exposure after fallback [FR-008, FR-010]** [go test]
Given `RIOT_DISPLAY_TZ=Mars/Olympus` and `TZ` unset
When the endpoint is called with a valid admin session
Then the reported zone is `UTC`.

### 8.7 Server-written text

**AC-052 — Next window line [FR-020]** [go test]
Given now = the example instant, a window schedule starting 06:00 UTC, and the display zone America/New_York
When the patch badge is computed
Then the automatic line is `Yes. Next window starts 2026-10-28 2:00 AM EDT.`

**AC-053 — Dated reboot line [FR-021]** [go test]
Given the conditions of AC-052 with a dated automatic reboot
When the reboot line is computed
Then it ends with `on 2026-10-28 2:00 AM EDT (estimate).`

**AC-054 — Badge label date in the display zone [FR-023, FR-029b, DEC-14]** [go test]
Given a window start of 02:00 UTC, so the next start is `2026-10-28T02:00:00Z`, and the display zone America/New_York
When the badge is computed and serialized
Then the label is `Auto (reboot on 2026-10-27)`
And `reboot_at` in the JSON is `2026-10-28T02:00:00Z`.

**AC-055 — As-of [FR-022]** [go test]
Given `updates_at` = the example instant
When the badge is computed with now = the example instant and then with now = 3 hours later
Then the as-of texts are `2026-10-27 1:05 PM EDT (just now)` and `2026-10-27 1:05 PM EDT (3 h ago)`.

**AC-056 — UTC display zone keeps 12-hour [FR-020, BR-004]** [go test]
Given the display zone UTC and the conditions of AC-052
When the badge is computed
Then the automatic line is `Yes. Next window starts 2026-10-28 6:00 AM UTC.`

**AC-057 — Summary Generated [FR-024]** [go test]
Given an injected now = the example instant and the display zone America/New_York
When a device summary is rendered
Then its Generated value is `2026-10-27 1:05:09 PM EDT`.

**AC-058 — Summary filename [FR-025, DEC-13]** [go test]
Given an injected now = `2026-10-28T02:00:00Z`, the display zone America/New_York, and the hostname `web-01`
When the summary is downloaded
Then the Content-Disposition filename is `web-01-summary-2026-10-27.md`.

**AC-059 — Powered-off message [FR-026]** [go test]
Given a powered-off coverage for host `web-01`, actor phrase `by ryan`, and ShutdownAt = the example instant
When its message is built
Then it is `Device web-01 powered off by ryan at 1:05 PM EDT`.

**AC-060 — Back-by message [FR-027]** [go test]
Given a window coverage with BackBy = `2026-10-27T17:20:00Z`
When its message is built
Then it ends with `(expected, back by 1:20 PM EDT)`
And with BackBy nil the message is identical to its pre-story text.

**AC-061 — Held summary message [FR-028]** [go test]
Given 3 held notifications for `web-01` since the example instant
When the summary message is built
Then it is `3 escalation notifications for web-01 were held since 1:05 PM EDT; see Alerts`.

**AC-062 — Server midnight, noon and DST [FR-026, FR-029a, DEC-8]** [go test]
Given the display zone America/New_York
When the powered-off message is built for `2026-10-27T04:00:00Z`, `2026-10-27T16:00:00Z`, `2026-11-01T05:30:00Z`, `2026-11-01T06:30:00Z`, `2026-03-08T06:59:59Z` and `2026-03-08T07:00:00Z`
Then the times read `12:00 AM EDT`, `12:00 PM EDT`, `1:30 AM EDT`, `1:30 AM EST`, `1:59 AM EST` and `3:00 AM EDT`.

**AC-063 — Numeric abbreviations [FR-029a, DEC-7]** [go test]
Given the example instant
When the summary Generated value is rendered with the display zones America/Sao_Paulo, Asia/Kathmandu and Asia/Kolkata
Then the values are `2026-10-27 2:05:09 PM GMT-3`, `2026-10-27 10:50:09 PM GMT+5:45` and `2026-10-27 10:35:09 PM IST`.

**AC-064 — Push carries the text [FR-029]** [go test]
Given a mock notifier and a powered-off event for the example instant
When the event is dispatched
Then the notification body contains `at 1:05 PM EDT`.

**AC-065 — API and storage stay UTC [FR-029b, BR-001]** [go test]
Given the display zone America/New_York
When the fleet patch-status response and a stored event are read
Then every time field is RFC 3339 with a `Z` suffix
And the stored event `created_at` is the UTC instant.

**AC-066 — UTC boundaries unaffected [FR-029b, BR-003]** [go test]
Given a process local zone of Pacific/Kiritimati (UTC+14) and the display zone America/New_York
When the partition plan, the daily quota day boundary, and `inMaintenanceWindow` / `nextMaintenanceWindowStart` are computed for fixed instants
Then every result equals the result computed with the process zone UTC and the display zone UTC.

**AC-067 — No crash on a missing zone [FR-006, NFR-003]** [go test]
Given a server-text formatter given no zone (zero value)
When it formats the example instant in the full form
Then it returns `2026-10-27 5:05:09 PM UTC`
And it does not panic.

### 8.8 Documentation and non-functional

**AC-068 — README [FR-070, FR-070a]** [doc-check]
Given `README.md`
When the "Time Display" section, the environment-variable list and the API section are read
Then they cover every item listed in FR-070
And the "always show UTC" sentence is gone.

**AC-069 — Config examples [FR-071]** [doc-check]
Given `.env.example`, `docker-compose.yml` and `web/src/components/ui/README.md`
When they are read
Then the first two each contain a commented `RIOT_DISPLAY_TZ=America/New_York` line with an explanation
And the UI README documents the 12-hour forms and the clock form.

**AC-070 — No stored state or migration [NFR-001]** [build]
Given `git diff --name-only main...HEAD`
When it is listed
Then no path is under `cmd/riot-server/migrations/`.

**AC-071 — Formatter cache bound [NFR-001(d)]** [vitest]
Given each zone option under both hour cycles and every precision, formatted 1,000 times each, plus 1,000 distinct rejected server-zone strings
When the formatter cache size is read
Then it is at most 12.

**AC-072 — Clock tick isolation [NFR-004]** [vitest]
Given a page with a `<Timestamp>` render spy and fake timers
When 5 seconds elapse with no preference change
Then the timestamp consumer has not re-rendered.

**AC-073 — No new dependencies [NFR-005]** [build]
Given `web/package.json` and `go.mod` on this branch and on `main`
When the dependency sets are compared
Then they are identical.

**AC-074 — Suites and builds green [NFR-007, FR-072]** [build]
Given the branch
When `go test ./...`, `npm run test:run`, `npx tsc --noEmit`, `npm run build` and `VITE_DEMO=true npm run build` run
Then all pass, neither test count is lower than on `main`, and no test is skipped
And every test tagged with a superseded UIF AC (AC-104, AC-105, AC-113..AC-118) still exists.

## 9. Out of Scope

- Editing Hawaii's compose override. The PR body tells the coordinator to add
  `RIOT_DISPLAY_TZ: America/New_York` to `/home/docker/riot`, and nobody in
  this story edits Hawaii.
- Tagging, merging or deploying.
- The maintenance-window editor (DEC-11) and the Docker activity UTC filters
  (DEC-12).
- Changing how maintenance windows are stored (still `HH:MM` UTC).
- Rewriting event messages already stored (DEC-17).
- Removing `formatUtc` (DEC-18).
- Agent-side time handling (`internal/agent/…`).
- Server log timestamps.
- Locales other than `en-US` (month names, other date orders). Dates stay
  `YYYY-MM-DD`.
- A per-user, server-stored preference, and syncing the preference across
  browsers or tabs.
- Changing relative-time wording or thresholds.
- Converting host-local schedule text to another zone.

## 10. Assumptions

- **A-001:** The server's zone database (Alpine `tzdata` in the image; Go's own
  database in tests) contains the zones named in the ACs. Where it is missing,
  FR-002 rejects the zone and UTC applies.
- **A-002:** The browser's `Intl` with `en-US` returns `EDT`/`EST`/`PDT` for US
  zones and `GMT±H[:MM]` for zones without a US abbreviation. Tests run under
  Node with full ICU.
- **A-003:** The server zone is fixed for the life of a process. A change takes
  effect after a restart and is picked up by the dashboard on its next page load.
- **A-004:** The inventory in Section 7.6 is complete for `origin/main` 8a16f2a,
  per the grounding note and Clod's re-read of the named files. The architect
  confirms this with a repository search (OQ-001).
- **A-005:** Because the process zone is not changed (FR-007), setting `TZ` only
  affects rIOt's display through the FR-001 fallback. Its side effects on other
  process output, such as log offsets, are the operator's choice and are not
  covered here.
- **A-006:** The 12-hour header clock changes width at 9:59:59 to 10:00:00 and
  at 12:59:59 to 1:00:00. A minor header reflow at those moments is acceptable.

## 11. Open Questions

None blocks implementation.

- **OQ-001 (architect):** Confirm, with a search of non-test code in
  `internal/server` and `internal/models` for `.Local()` and for `Format(`
  calls with clock layouts, that no human-text time render exists beyond
  inventory rows 17–25. Any extra site found joins Section 4.3 under the same
  rules.
- **OQ-002 (coordinator/Ryan):** Add `RIOT_DISPLAY_TZ: America/New_York` to
  Hawaii's riot compose override when this ships. Until then, Hawaii shows UTC
  in 12-hour form.

## 12. Dependencies

- UI-FOUNDATION-001 formatters, `TimePreferenceProvider`, `HeaderClock`,
  `<Timestamp>`/`<RelativeTime>` and the static guards in
  `web/src/test/static-guards.test.ts`. The FR-039 guard must coexist with UIF
  AC-110 (no `toLocale*`).
- PATCH-FLOW-001 / EXTRA-UPD-001 patch badge (`handlers/patch_badge.go`) and its
  tests.
- EXPECT-001 / EXPECT-002 event messages (`models/expect.go`,
  `expect/messages.go`) and the notification path that pushes them to ntfy.
- The device summary (`summary/summary.go`) and its download handler.
- Admin authentication middleware, for FR-010/FR-011.
- `tzdata` in the runtime image (`Dockerfile`, already present).
- Pipeline order: architect (ADD), then security review, then engineering, then
  QA, then technical writer.

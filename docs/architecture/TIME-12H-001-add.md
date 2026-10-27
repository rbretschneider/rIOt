# Architecture Decision Document

- **Story ID:** TIME-12H-001
- **FRD Reference:** `docs/requirements/TIME-12H-001-frd.md` (FINAL; DEC-1 to DEC-18 decided on Ryan's behalf, 74 ACs)
- **Security review:** `docs/security/TIME-12H-001-security-review.md` (APPROVED WITH CONDITIONS C-1 to C-4). This revision folds them in; see the Security addendum
- **Builds on:** UI-FOUNDATION-001 (`utils/format.ts`, `TimePreferenceProvider`, `HeaderClock`, `<Timestamp>`/`<RelativeTime>`, static guards; its AD-007 and AD-010), PATCH-FLOW-001 / EXTRA-UPD-001 (patch badge), EXPECT-001 / EXPECT-002 (event messages, publisher, held summary), HARDEN-002 AD-002 (validate env values in `Start` after slog is configured)
- **Author:** Architect Agent
- **Date:** 2026-10-27
- **Status:** FINAL, revision 2 (security conditions folded in; approved for implementation)
- **Code baseline:** `origin/main` 8a16f2a (v2.83.0 + PR #44), branch `feat/time-12h-001`

## Changelog

### Revision 2: security review conditions C-1 to C-4, INFO-3, INFO-5

| Item | Source | What changed |
|---|---|---|
| `DisplayZoneLabel` uses the zone database's abbreviation only when it is 1 to 6 ASCII letters; anything else gets the offset form. So the label is always ≤ 9 bytes, either `[A-Za-z]{1,6}` or `GMT[±H[:MM]]`. New DT test `TestTIME12H_SEC001_HostileAbbreviationFallsBackToOffset` (uses `time.LoadLocationFromTZData`). S-8 now states the property | C-1 (SEC-001) | AD-003, AD-015, Section 8, Section 8.2, Section 10 S-8 |
| `sanitizeZoneValue` first makes the value valid UTF-8, then removes every Cc **and** Cf rune (bidi overrides and isolates, zero-width characters), then cuts to 64 runes. AC-046 gains a subtest with U+202E, U+2066 and U+200B | C-2 (SEC-002) | AD-001, Section 8 AC-046, Section 10 S-2 |
| New web guard: an `as AcceptedZone` cast appears only in `utils/format.ts`; `localTimeZone` is named only in `TimePreferenceProvider.tsx` and `utils/format.ts` and is never a production JSX prop. Both have self-tests. AD-007 now says the FR-039 enforcement is the brand plus this guard | C-3 (SEC-003) | AD-007, AD-014, Section 4, Section 8, Section 8.2 |
| QA gates: the route is inside `adminAuth`. AC-050 adds an expired JWT and a bad-signature JWT. No captured log contains `unknown time zone` or `invalid location name`. `ResolveDisplayZone` runs once, from `Start` only. AC-018/020/054/065/066/071 and `[UIF SEC-005]` stay green | C-4 | AD-001, Section 8 AC-046/AC-048/AC-050, Section 8.2, Section 12, Section 13 |
| The README presents `RIOT_DISPLAY_TZ` as the preferred setting and says that `TZ` also changes the process zone (log offsets) | INFO-3 | Section 4 (README row), Section 12 note 18 |
| The NFR-006 test also covers an older server that answers the new path with `200 text/html` from the SPA catch-all | INFO-5 | Section 8.1 |

### Revision 1

The first version.

---

## 0. Stage 1 outcome (FRD review)

The FRD can be implemented. DEC-1 to DEC-18 stand as decided; none is reopened. The review found four points that need an architect decision. They are recorded here and resolved in the ADs named.

| # | Finding | Kind | Resolution |
|---|---|---|---|
| R-1 | **FR-029 and AC-064 assume the powered-off and expected-offline event messages become ntfy pushes. They do not.** `events.Generator.DeviceOfflineAt` stores a covered offline (`OfflineKindWindow`) and a powered-off transition through `createEvent` only, as an `info` event, and never dispatches it (EXPECT-001 AD-005; `generator.go:363-379`; `generator_expect_test.go:241` asserts "no notification"). Of FR-026 to FR-028, only the held-escalation summary (FR-028) is pushed, through `publisher.sendSummary` → `Escalator.PublishEscalation`. AC-064 as written ("a powered-off event … is dispatched … notification body contains") cannot be met without changing EXPECT-001 alerting, which is out of scope. | AC cannot be fulfilled as written | **AD-016.** AC-064 is fulfilled on the path that actually pushes (the held summary). The powered-off message is checked on the stored and broadcast event, plus a regression check that it is still not pushed. Alerting does not change. |
| R-2 | **FR-025 and AC-058 target the server's `Content-Disposition` filename, but the browser never uses it.** `DeviceDetail.tsx:239-256` fetches the Markdown with `api.getDeviceSummary` and builds its own `a.download` name from `new Date().toISOString().slice(0, 10)`, which is the UTC date. Changing only the server header would leave the file Ryan actually saves on the UTC date, contradicting DEC-13. | Missing inventory site (OQ-001) | **AD-012.** Both change: the server header (FR-025 as written) and the browser filename. The browser uses the server display-zone date, so the name matches the `Generated` line inside the file. |
| R-3 | **FR-029a does not define a numeric abbreviation with a zero offset.** Applied literally, `+00` (for example Antarctica/Troll in winter) would become `GMT+0`. The browser's `en-US` short name for a zero-offset zone without an abbreviation is `GMT`, and DEC-7 says the server form exists "to match the browser form". | Ambiguity | **AD-003.** A numeric abbreviation with offset 0 renders `GMT`. Every case FR-029a lists is unchanged. |
| R-4 | **README line 860 is a second stale statement** outside the "Time Display" section: "Times in event messages, such as `back by 14:32`, use the server's local time zone". After this story they use the display zone, in 12-hour form, with a label. | Docs scope | **AD-015 / Section 4.** The sentence is replaced along with the FR-070 changes. |

No questions for the Business Developer: all four are technical.

### OQ-001 answer (inventory completeness)

Searched at 8a16f2a:
- **Go** (non-test code in `internal/server` and `internal/models`): `.Local()`, `time.Local`, every `.Format(` call, the `time.Kitchen`/`Stamp`/`DateTime` family of constants, `.Hour()`/`.Minute()` used to build strings, `%v`/`String()` on times, `Content-Disposition`, and the notifier package.
- **TypeScript** (non-test code in `web/src`): `<Timestamp`, `<RelativeTime`, `useTimeFormatters`, `useTimePreference`, `formatChartTick`, `formatRunDate`, `parseSystemdCalendar`, `Intl.DateTimeFormat`, `toLocale*`, `getHours`/`getUTCHours`, `toISOString().slice`, and `a.download`.

**Go: the inventory is complete.** Rows 17 to 25 are the only places that render a time as text for a person.
- `.Local()` appears only at `models/expect.go:412,416` and `expect/messages.go:64`. `time.Local` is never referenced.
- The remaining `.Format` calls produce machine values and stay UTC:
  - `quota/daily.go:79,105,109` (day key);
  - `partition/plan.go:78` (partition name);
  - `partition/manager.go:296-306` and `:507` (slog fields and a SQL literal);
  - `handlers/docker_api.go:426` (cursor).
- The notifier package (`internal/server/notify/*`) formats no times. It forwards `Event.Message` unchanged.
- `internal/agent` is out of scope (row 29).

**Web: two sites were missing; the rest are confirmed.** Additions to the FRD Section 7.6 table, for the PR body:

| # | Surface | Where | Current | Target | In scope |
|---|---|---|---|---|---|
| 31 | Docker log viewer line times, "History up to …", clock-skew note | `components/docker/LogView.tsx:441,521,526-527` via `useTimeFormatters().timestamp` | `2026-10-27 17:05:09 UTC` | `2026-10-27 1:05:09 PM EDT` (follows both preferences through the shared hook; no source change) | Yes (automatic) |
| 32 | `<RelativeTime>` sites not named in FR-060 | `FleetOverview.tsx:367` (last heartbeat), `Probes.tsx:202`, `dashboard/ActivityRiver.tsx:118`, `ActivityLabel.tsx:61` | text unchanged; title 24-hour | title in the current preference (rows 5 and 6) | Yes (automatic) |
| 33 | **Device summary download filename, as saved by the browser** | `pages/DeviceDetail.tsx:246` (`new Date().toISOString().slice(0, 10)`) | UTC date | server display-zone date (R-2, AD-012) | **Yes (new)** |
| 34 | Docker log download filename | `LogView.tsx:305-308` | `<host>_<name>_20261027T170509Z.log` | unchanged: a machine filename, explicitly `Z`-labelled UTC and sortable | No |
| 35 | Docker log custom range inputs | `LogView.tsx:556-580` (ISO text inputs) | ISO input | unchanged: input semantics, same reasoning as DEC-12 | No |
| 36 | Downloaded Docker log file content | `utils/logBuffer.ts:49` | raw agent ISO times | unchanged: machine export | No |
| 37 | Maintenance-window editor zone label | `AgentManagement.tsx:167-192` (`Intl.DateTimeFormat` without `timeZone`, `getHours`) | browser-local | unchanged (DEC-11) | No |

Docs: README line 860 (R-4) is added to the FR-070 changes.

---

## 1. Summary

**Server.**
- At startup the server resolves one **display zone** from `RIOT_DISPLAY_TZ`, then `TZ`, then UTC. A pure resolver in `internal/server/config.go` checks the shape and loads the zone with `time.LoadLocation`. It never touches `time.Local`.
- The resulting `*time.Location` is injected explicitly into the handlers, the expected-activity tracker and the summary renderer.
- Every human-readable time the server writes goes through one helper in `internal/models/display_time.go`. It writes the 12-hour form with a zone label (`2026-10-27 1:05:09 PM EDT`). Numeric abbreviations, and any abbreviation that is not 1 to 6 ASCII letters, become an offset label (`GMT-3`).
- The server binary embeds `time/tzdata` as a fallback zone database.

**Dashboard.**
- It reads the zone name once per page load from a new admin-only endpoint, `GET /api/v1/server/time-display`, which returns `{"display_zone": "America/New_York"}`.
- It accepts the value only after an IANA-shape check and an `Intl` acceptance check. Anything else means UTC.
- `utils/format.ts` gains a `Server` zone, a required `hourCycle` (`12h` | `24h`) and a `clock` precision. The 12-hour form is derived arithmetically from the existing `h23` parts. The `Intl` formatter cache therefore stays at one entry per zone, with a hard cap of 12.
- `TimePreferenceProvider` stores two enum keys. The hour-cycle key doubles as the marker for values written after this story, which implements DEC-5 without a third key.
- The header clock becomes a button that opens a small non-modal panel with two native radio groups.

**Unchanged.** No migration, no stored server state, no new dependency. APIs, stored data, and the partition, quota, retention and maintenance-window math stay UTC.

## 2. Technical Context

### 2.1 What exists (verified at 8a16f2a)

| Area | Where | Relevant facts |
|---|---|---|
| Server config | `internal/server/config.go` | Env-only `LoadConfig()`, called in `cmd/riot-server/main.go:43` **before** `Start` configures slog. Precedent: `PprofAddr` is stored raw and validated in `Start` after slog is set up (HARDEN-002 AD-002) |
| Server start | `internal/server/server.go:97-300` | JSON slog at l.99. The DB log handler and the log budget at l.155-158. `EventGen` at l.213. `expect.New(Options{…})` at l.224. The router is built later, in `setupRouter` (`router.go:85` `handlers.New(HandlerDeps{…})`), which `applyTLSAndRestart` re-runs |
| Image | `Dockerfile` | `alpine:3.19` with `apk add … tzdata`. The binary is `CGO_ENABLED=0`. No `TZ` in the container |
| Handlers composition | `handlers/handlers.go:41-311` | `HandlerDeps` → `Handlers`. `h.clock()` returns UTC from the injected `now` |
| Patch badge | `handlers/patch_badge.go` | `computePatchBadge(in patchBadgeInput, now)` is pure and has about 150 test callers. Time text at l.370 (label date), l.380 (Automatic), l.566 (`rebootLine` via `rebootFacts.at`), l.789 (`asOfText`). `b.RebootAt = &at` is serialised as `reboot_at` |
| Fleet patch status | `handlers/fleet.go:73-99` | Builds `patchBadgeInput`. Has the `NextWindowStart` JSON field |
| Summary | `summary/summary.go:117-133`; `handlers.go:1602-1639` | `Render(device, snapshot)` reads `time.Now()`. The handler builds the filename from `time.Now().UTC()` |
| Event messages | `models/expect.go:409-419`; `expect/messages.go:63-65`; `expect/tracker.go:497-543`; `expect/publisher.go:237-272` | `OfflineCoverage` is built in exactly one place (`tracker.go:497`) and returned to `Generator.DeviceOfflineAt`, which calls `cov.Message(host)`. The held summary is built in `publisher.sendSummary` and pushed |
| Push path | `events/generator.go:199-219,363-400` | Covered offline and powered-off: `createEvent` only, no push. Held summary: `PublishEscalation` (push) |
| UTC-critical math | `handlers/auto_update.go:216-306,510-523`; `partition/plan.go:77-142`; `quota/daily.go:76-110` | All normalise with `.UTC()` and build dates with `time.UTC` |
| Web formatters | `web/src/utils/format.ts:42-223` | `TimeZonePref = 'UTC' \| 'Local'`. One cached `Intl.DateTimeFormat('en-US', {…, hourCycle: 'h23', timeZoneName: 'short'})` per zone key. Exports `formatTimestamp`, `formatChartTick`, `formatChartTooltip`, `formatUtc` |
| Preference | `web/src/contexts/TimePreferenceProvider.tsx` | Key `riot-time-zone`. Only `Local` selects Local. The no-provider default is UTC (UIF AD-007, so untouched tests need no wrapper). Has `toggleZone` |
| Header clock | `web/src/components/HeaderClock.tsx`; mounted at `App.tsx:276` inside the authenticated branch | Has its own 1 s interval (UIF NFR-008). A click toggles the zone |
| Provider mount | `web/src/main.tsx:26` | Wraps `<App />`, including the login screen |
| Popover patterns | `components/ui/Popover.tsx`, `ui/DropdownMenu.tsx`, `PatchBadge.tsx:36-54` | An outside `pointerdown` closes. Escape returns focus to the trigger. `PatchBadge` uses `@floating-ui/react` `flip`/`shift`/`size` to stay in the viewport, with `overflow-y-auto scrollbar-thin` |
| Header | `components/layout/AppShell.tsx:121` | `sticky top-0 z-30`, with no transform or backdrop filter, so a `fixed` floating element positions correctly inside it |
| API client | `web/src/api/client.ts:23-33` | `fetchJSON` reloads the page on 401, so admin endpoints must only be queried when authenticated |
| Demo parity | `web/src/api/demo-parity.test.ts` | The `api` key sets must match, and every `get*` reader must resolve to a defined value (UIF AC-127, AC-134) |
| Static guards | `web/src/test/static-guards.test.ts` | Raw-source scans of non-test files (UIF AC-110 bans `toLocale*`) |
| SPA catch-all | `router.go:389-405` | Any unmatched path gets `index.html` with `200` (INFO-5) |

### 2.2 What is missing

- A display zone on the server, and a way to pass it to text renderers without reading the process zone.
- A 12-hour server formatter with zone labels.
- A way for the dashboard to learn the server zone.
- A `Server` zone, an hour cycle, a clock form and a panel on the dashboard.

### 2.3 What must change

See Section 4. No migration, no agent change, no dependency change.

---

## 3. Architecture Decisions

### AD-001: Display-zone resolution: raw values in `LoadConfig`, pure resolver called in `Start`

**Decision.**
- `Config` gains two raw fields, which `LoadConfig` reads verbatim (untrimmed):
  - `DisplayTZRaw` (`RIOT_DISPLAY_TZ`);
  - `TZRaw` (`TZ`).
- `config.go` gains:
  - `type DisplayZone struct { Location *time.Location; Name string; Source string }`, where `Name == Location.String()`.
  - Source constants: `DisplayZoneSourceEnv = "RIOT_DISPLAY_TZ"`, `DisplayZoneSourceTZ = "TZ"`, `DisplayZoneSourceDefault = "default"`.
  - `func ResolveDisplayZone(displayTZ, tz string, log *slog.Logger) DisplayZone`. It is pure apart from the zone-database read and the log.
  - `func validDisplayZoneName(s string) bool` (shape only) and `func sanitizeZoneValue(s string) string`.
- **Algorithm**, applied to each source in order (`RIOT_DISPLAY_TZ`, then `TZ`):
  1. `v := strings.TrimSpace(raw)`. If it is empty, the source is unset: no log, go to the next source (FR-003).
  2. Check the shape (FR-002): `len(v) ≤ 64`, `v != "Local"`, and `v` matches `^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z][A-Za-z0-9_+-]*){0,2}$`.
     - This check runs before the zone database is touched.
     - It rejects `.`, `:`, `;`, spaces, `..`, absolute and relative paths, and the POSIX `:/etc/localtime` form.
  3. `loc, err := time.LoadLocation(v)`. An error means the value is invalid.
  4. If step 2 or step 3 failed:
     - log exactly one `log.Warn("ignoring invalid display time zone", "source", <name>, "value", sanitizeZoneValue(v))`;
     - go to the next source.
  5. If the value is valid, return `{loc, v, source}`.
  6. If no source is valid, return `{time.UTC, "UTC", "default"}`.
- After the loop, log exactly one `log.Info("display time zone", "zone", Name, "source", Source)` (FR-005).
- **`sanitizeZoneValue`** (C-2; stricter than FR-004's "control characters removed"):
  1. `strings.ToValidUTF8(s, "\uFFFD")`, so an invalid UTF-8 byte never reaches the log raw.
  2. Drop every rune of category **Cc or Cf** (`unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r)`). This removes:
     - `\n`, `\r`, `\t`, ESC and NUL;
     - the bidi embeddings, overrides and isolates (U+202A..U+202E, U+2066..U+2069);
     - the zero-width characters (U+200B..U+200F) and U+FEFF.

     This matches the stricter precedent of the EXPECT-002 parser.
  3. Cut the result to its first 64 runes.
- **Ordering and logged text (C-4).**
  - The shape checks (length, `Local`, regex) always run **before** `time.LoadLocation`.
  - `LoadLocation`'s error is never logged, because it echoes the raw input (`unknown time zone …`, `invalid location name`).
  - No log record the resolver produces may contain either phrase.
- **Call site (C-4).**
  - `Server.Start` calls `ResolveDisplayZone` exactly once, immediately after the DB log handler and the log budget are installed (`server.go:158`). This is before `EventGen` (l.213), `expect.New` (l.224) and the router.
  - The result is stored in a new field, `Server.DisplayZone *time.Location`. Only the `Location` is kept; the name is `Location.String()`.
  - It is **not** called from `setupRouter`, which `applyTLSAndRestart` re-runs. The router reads `s.DisplayZone`. One call per process keeps the AD-015 log bound.
- **The process zone.**
  - The server **never assigns `time.Local`**, and no server text reads it (FR-007).
  - Go's runtime still derives `time.Local` from `TZ` on its own. That affects nothing this story renders (FRD A-005), but it does change the offsets in slog timestamps. That is why the README presents `RIOT_DISPLAY_TZ` as the preferred setting (INFO-3).

**Rationale.**
- It follows HARDEN-002 AD-002 exactly. `LoadConfig` runs before slog is configured, so a warning logged there would bypass the JSON handler and the DB log store.
- Calling it after l.158 means a rejected value also reaches Settings → Logs (WARN records are stored there), which is where Ryan would look.
- A pure function with an injected logger lets AC-042 to AC-048 be table tests with a captured handler.
- Trimming happens in the resolver, so `Config` holds exactly what the environment holds.

**Alternatives considered.**
- *Resolve in `LoadConfig`.* Rejected: it would log before the JSON and DB handlers exist, so the FR-004 warning would never appear in the dashboard. Every `LoadConfig` test would also touch the zone database.
- *Set `time.Local` from the resolved zone.* Rejected by FR-007. It would also silently move the wall-clock readings of `time.Now()` that other code formats.
- *Fail start on an invalid value.* Rejected by DEC-2 and FR-006.

**Consequences.** At most two zone-database reads (one per source) at start. The effective zone is fixed for the life of the process (FRD A-003).

### AD-002: Embed `time/tzdata` in the server binary as a fallback

**Decision.**
- `cmd/riot-server/main.go` adds the blank import `_ "time/tzdata"`.
- The Dockerfile keeps `apk add tzdata`.
- New Go test files that load named zones also blank-import `time/tzdata`, so the tests behave the same on any machine.

**Rationale.**
- `time.LoadLocation` searches in this order: `ZONEINFO`, the system zone directories, `$GOROOT/lib/time/zoneinfo.zip`, and only then the embedded copy.
- In the Docker image the system database is used (it is updated with each image build), so behaviour there is unchanged.
- The embedded copy makes binaries built with `make build-server` correct where no system database exists:
  - hosts without `/usr/share/zoneinfo` (minimal or `scratch` containers, some NAS images);
  - Windows, which has no system database and usually no `GOROOT` at runtime.
- Without it, such a host would reject a valid `RIOT_DISPLAY_TZ` and fall back to UTC, with a warning that looks like Ryan's mistake.
- C-1 bounds what any zone database, embedded or not, can inject into labels.

**Cost.** About 450 KB added to a binary that already embeds the frontend (tens of MB). It is standard library, not a dependency (NFR-005).

**Alternatives considered.**
- *The build tag `-tags timetzdata`.* Equivalent, but easy to drop from one of the three build paths (Makefile, Dockerfile, release workflow). The import lives in one place.
- *No embedding.* Rejected for the reason above.

### AD-003: One server formatter: `internal/models/display_time.go`

**Decision.** A new file in package `models`, which already holds the shared formatting helpers `FormatDurationShort`, `SafeName` and `SafeText`:

| Function | Output for `2026-10-27T17:05:09Z`, America/New_York |
|---|---|
| `FormatDisplayFull(t time.Time, loc *time.Location) string` | `2026-10-27 1:05:09 PM EDT` (layout `2006-01-02 3:04:05 PM`, then a space and the label) |
| `FormatDisplayMinute(t, loc)` | `2026-10-27 1:05 PM EDT` |
| `FormatDisplayClock(t, loc)` | `1:05 PM EDT` |
| `FormatDisplayDate(t, loc)` | `2026-10-27` (no label; the BR-002 date-only exception) |
| `DisplayZoneLabel(t time.Time) string` | the label for `t`'s own location |

- **Nil zone.** A nil `loc` means `time.UTC` (AC-067). The helpers never call `t.In(nil)`, which panics.
- **No mutation.** Each helper works on a copy (`in := t.In(loc)`). The caller's value is never modified.
- **`DisplayZoneLabel`** starts with `name, offset := in.Zone()`:
  - **Abbreviation (C-1).** If `name` is **1 to 6 ASCII letters** (`[A-Za-z]{1,6}`, checked byte by byte), return `name` (`EDT`, `EST`, `IST`, `UTC`, `GMT`, `BST`).
    - Go's TZif parser returns the abbreviation bytes unconstrained.
    - Without this check, a hostile or corrupt zone database could put content into event messages, the held-summary push to every channel type, and the Markdown summary: CR/LF, markup, Slack `<!channel>` mentions, or text of any length. Such a database could come from a compromised `tzdata` package or a `ZONEINFO` directory.
  - **Offset form.** In every other case the label is built from `offset` (in seconds, which avoids parsing the abbreviation string). Those cases are a numeric name (`-03`, `+0545`), an empty name, a name longer than 6, or one containing anything except ASCII letters.
    - `offset == 0` → `GMT` (R-3).
    - Otherwise: `GMT`, then the sign, then the hours without a leading zero, then `:MM` only when the minutes are not zero. Seconds are dropped; they only occur in historical LMT offsets.
    - Examples: `-03` → `GMT-3`, `+0530` → `GMT+5:30`, `+0545` → `GMT+5:45`, `-0930` → `GMT-9:30`.
  - **Property.** The label is always one of two things:
    - 1 to 6 ASCII letters;
    - `GMT`, optionally followed by a sign, 1 to 2 digits and an optional `:MM`. This is at most 9 bytes. The digits are bounded because the minutes come from `|offset| % 3600` and the hours are below 100 for any value a 32-bit TZif offset can hold in practice.
- **12-hour form.** Go's `PM` layout element writes uppercase `AM`/`PM`. The `3` element writes the hour without a leading zero, and `12` at midnight and noon (DEC-8).

**Rationale.**
- One helper means FR-020 to FR-028 cannot drift apart, and the AD-014 static guard can ban 24-hour human layouts everywhere else.
- Every consumer already imports `models` (`handlers`, `summary`, `expect`, and `models` itself for `OfflineCoverage.Message`). `models` imports nothing new, so there is no import cycle.
- Taking a `*time.Location` keeps the helpers pure and testable.
- The letters-only predicate is the narrowest rule that still keeps every real abbreviation listed in AC-063.

**Alternatives considered.**
- *A new package `internal/timefmt`.* Equivalent, but `models.OfflineCoverage.Message` would then import a sibling package for four functions, and `models` already holds the formatting helpers.
- *Use `Format("MST")` and rewrite the result as a string.* Rejected: parsing `+0545` is fragile, and the offset is the authoritative value.
- *Escape or strip bad bytes from the abbreviation instead of replacing it.* Rejected: escaping is channel-specific (Slack, Discord, Markdown), and a partly stripped abbreviation is misleading. The offset form is always correct.

### AD-004: Explicit zone injection; no package-level mutable state

**Decision.** The resolved `*time.Location` flows from `Server.DisplayZone` along the existing composition roots. A nil location anywhere means UTC.

| Consumer | Plumbing |
|---|---|
| Handlers | `HandlerDeps.DisplayZone *time.Location` → `Handlers.displayZone`. The method `func (h *Handlers) zone() *time.Location` returns it, or `time.UTC` if it is nil. `router.go` passes `s.DisplayZone` |
| Patch badge | `patchBadgeInput.Zone *time.Location` (nil = UTC), set by `fleet.go` from `h.zone()`. `computePatchBadge`'s signature is unchanged, so its ~150 test callers compile unchanged. `rebootFacts` gains `zone *time.Location`. `asOfText(at *time.Time, now time.Time, loc *time.Location)` |
| Summary | `summary.Render(device, snapshot, generated time.Time, zone *time.Location)`; `buildSummaryData` takes both. The handler passes `h.clock()` and `h.zone()` |
| Covered-offline and powered-off messages | `models.OfflineCoverage` gains `Zone *time.Location`. `expect.Options` gains `DisplayZone *time.Location`. The Tracker sets `cov.Zone = t.opt.DisplayZone` at its single construction point (`tracker.go:497`), so both the window and the powered-off variants carry it. `Message(host)` keeps its signature and uses `FormatDisplayClock(…, c.Zone)` |
| Held summary | `newPublisher` copies `o.DisplayZone` into `publisher.zone`. New signature: `heldSummaryMessage(n int, host string, since time.Time, loc *time.Location)` |
| Server wiring | `server.go` passes `DisplayZone: s.DisplayZone` in `expect.Options{…}` |

The three `.Local()` calls (`expect.go:412,416`, `messages.go:64`) are replaced by the helper with the injected location.

**Rationale.**
- It matches how the codebase already injects collaborators (`HandlerDeps`, `expect.Options`, `Generator.SetClock`).
- Putting the zone on the coverage value leaves `events.Generator` untouched: it already calls `cov.Message(hostname)`, and the Tracker is the only producer of coverage values.
- Because nil means UTC, every existing struct-literal `Handlers{}` and `expect.Options{}` in tests stays valid.

**Alternatives considered.**
- *A package-level `models.DisplayLocation` variable set at start.* Rejected: global mutable state, ordering hazards in parallel tests, and it would hide the dependency that the static guard checks.
- *`Message(host, loc)` with a `Generator.SetDisplayZone` setter.* It works, but it adds a second injection path and touches `generator.go` for no gain.
- *Change the signature to `computePatchBadge(in, now, loc)`.* Rejected: about 150 test call-site edits for no behaviour gain, and the input struct is already documented as "everything the badge depends on".

**Consequences.** Every server text renderer can be tested with any zone and is unaffected by the process zone (AC-049, AC-066).

### AD-005: Server text rules (patch badge, summary)

**Decision.**
- **Patch badge** (`computePatchBadge`): `now = now.UTC()` and all maintenance-window math stay as they are. Only the rendering changes:
  - **Label:** `"Auto (reboot on " + models.FormatDisplayDate(at, in.Zone) + ")"`.
    - `b.RebootAt = &at` keeps the **UTC** value `at` (AC-054, AC-065). The date is computed from a converted copy.
    - The pointer must never point at a `t.In(loc)` value: that would serialise with `-04:00`.
  - **Automatic:** `"Yes. Next window starts " + models.FormatDisplayMinute(nextStart, in.Zone) + "."`.
  - **Reboot line:** `head + " on " + models.FormatDisplayMinute(f.at, f.zone) + " (estimate)."`.
  - **As-of:** `models.FormatDisplayMinute(t, loc) + " (" + ago + ")"`. The age thresholds do not change (FR-022).
  - Comments that say "in UTC" (`asOfText`) are updated.
- **Summary:** `Generated: models.FormatDisplayFull(generated, zone)`.
- **Summary filename** (`DeviceSummary`): `dateStr := models.FormatDisplayDate(h.clock(), h.zone())`. Sanitising is unchanged, and the handler no longer reads `time.Now()`.

**Consequences.** On a server with neither `RIOT_DISPLAY_TZ` nor `TZ` set (Hawaii today), text changes from `06:00 UTC` to `6:00 AM UTC` (FRD OQ-002).

### AD-006: Zone delivery: `GET /api/v1/server/time-display` behind the admin session

**Decision.**
- A new handler, `func (h *Handlers) TimeDisplay(w, r)`, in `internal/server/handlers/time_display.go`.
  - Response: `{"display_zone": "<name>"}`, status `200`, header `Cache-Control: no-store`.
  - The name is `h.zone().String()`, which is the FR-008 name (`UTC` after a fallback).
- Route: inside the existing `r.Group(func(r chi.Router) { r.Use(adminAuth) … })`, next to `/api/v1/update/server`: `r.Get("/api/v1/server/time-display", h.TimeDisplay)`. QA confirms this placement (C-4).
- JSON field name: `display_zone`.
- `GET /api/v1/auth/check` is **not** changed.

**Rationale.**
- `adminAuth` (`AdminAuthSameOrigin`) is the gate that has already been reviewed. These requests all get the existing `401 {"error":"unauthorized"}` (FR-011):
  - no cookie;
  - an invalid, expired or wrongly signed JWT;
  - a request carrying only `X-rIOt-Key`.
- `router_activity_test.go` already proves this shape for `/api/v1/activity`.
- `auth/check` is public, parses the JWT itself, and is called by the login screen. Adding configuration to it would put a disclosure decision into a second, hand-written auth branch.
- A dedicated read keeps FR-012 trivially true: no existing response changes.

**Alternatives considered.**
- *The authenticated branch of `auth/check`.* Rejected for the reasons above.
- *A field on `GET /api/v1/update/server`.* Rejected: that response is the GitHub update check. The dashboard caches it for an hour and refetches it every 6 h, so it would mix two concerns.
- *An unauthenticated endpoint.* Rejected by DEC-15.

**Consequences.** One extra small request per page load: an admin read of an in-memory string.

### AD-007: Dashboard acceptance of the zone, and the sync component

**Decision.**
- **Validation in `utils/format.ts`.** It exports:
  - `type AcceptedZone = string & { readonly __brand: 'AcceptedZone' }`;
  - `const ZONE_NAME_PATTERN = /^[A-Za-z][A-Za-z0-9_+-]*(\/[A-Za-z][A-Za-z0-9_+-]*){0,2}$/`;
  - `function acceptServerZone(raw: unknown): AcceptedZone | null`. It accepts `raw` only when all of these hold:
    - `typeof raw === 'string'`;
    - `raw.length <= 64`;
    - `raw !== 'Local'`;
    - `ZONE_NAME_PATTERN.test(raw)`. There is no trimming: surrounding whitespace is rejected (FRD 7.2);
    - `new Intl.DateTimeFormat('en-US', { timeZone: raw })` does not throw (checked inside a `try`).

    The acceptance construction passes **only** `{ timeZone }`, so the AC-020 spy can tell it apart from formatting constructions. The result is not cached, so rejected strings cause no growth. This function is the only place in production source that casts to `AcceptedZone`.
- `ZoneOpts.serverTimeZone` is typed `AcceptedZone | null | undefined`.
- **FR-039 enforcement (C-3).** The rule is enforced by **the brand plus the AD-014 static guard together**, not by the brand alone:
  - the brand makes a plain string a compile error;
  - the guard fails on any `as AcceptedZone` (or `as unknown as AcceptedZone`) outside `utils/format.ts`;
  - the guard keeps `localTimeZone` — the one unbranded zone input, a test seam — out of every production module except the provider and `utils/format.ts`, and out of every production JSX prop;
  - the AC-020 runtime spy is a third, path-specific check.
- **Provider state.** `TimePreferenceProvider` holds `serverZone: AcceptedZone | null`, initially `null`. It exposes `setServerZone(raw: unknown)`, which runs `acceptServerZone` and changes state only when the accepted value differs.
- **Sync component.** A new `web/src/components/ServerZoneSync.tsx` that renders `null`:
  - it calls `useQuery({ queryKey: ['time-display'], queryFn: api.getTimeDisplay, staleTime: Infinity, gcTime: Infinity, retry: 1 })`;
  - it calls `useEffect(() => setServerZone(data?.display_zone), [data, setServerZone])`;
  - it is mounted in `App.tsx` in the authenticated branch, next to `<GlobalWSHandler />`. It never runs before login, because a 401 from `fetchJSON` would reload the page.
- **API client.** `api.getTimeDisplay: () => fetchJSON<TimeDisplayInfo>(`${BASE}/server/time-display`)`, with `interface TimeDisplayInfo { display_zone: string }` in `client.ts`. The type is only a hint: the value is always passed to `acceptServerZone` as `unknown`.
- **Demo parity.**
  - `demo-client.ts` gets `getTimeDisplay: () => Promise.resolve({ display_zone: 'America/New_York' })` (FR-068). The existing key-set parity test and the AC-134 reader test pick it up automatically.
  - `demo-data.ts:743` `as_of` becomes `'2026-10-18 10:00 PM EDT (just now)'` (FR-069).
- **Fallback.** In each of these cases Server formats as UTC with the label `UTC` (FR-050, NFR-006):
  - the request has not resolved yet;
  - the request failed;
  - an older server answered with a 404, or with `200 text/html` from the SPA catch-all, so `res.json()` throws;
  - the value was rejected.

**Rationale.** React Query already owns fetching. `staleTime: Infinity` gives "once per page load, not cached across loads", since the cache lives in memory only.

### AD-008: Formatter design (`utils/format.ts`)

**Decision.**
- **Types:**
  - `type TimeZonePref = 'Server' | 'UTC' | 'Local'`;
  - `type HourCycle = '12h' | '24h'`;
  - `interface ZoneOpts { zone: TimeZonePref; hourCycle: HourCycle; localTimeZone?: string; serverTimeZone?: AcceptedZone | null }`. **`hourCycle` is required**, so `tsc` finds every caller that has not chosen one;
  - `type TimestampPrecision = 'second' | 'minute' | 'time' | 'clock'`.
- **Zone resolution.** An internal `resolveZone(opts)` returns `{ key, timeZone, literalUtc }`:

  | Preference | Cache key | `Intl` `timeZone` | Label |
  |---|---|---|---|
  | `UTC` | `UTC` | `'UTC'` | literal `UTC` |
  | `Server` with `serverTimeZone` | `S:<zone>` | the accepted zone | the `timeZoneName` part |
  | `Server` without one | `UTC` | `'UTC'` | literal `UTC` |
  | `Local` | `L:<localTimeZone>` or `L:<runtime>` | the test seam, or `undefined` (browser zone) | the `timeZoneName` part |

- **One `Intl.DateTimeFormat` per zone key**, with today's exact options: `en-US`, 2-digit fields, `hourCycle: 'h23'`, `timeZoneName: 'short'`.
- **12-hour form.** It is computed from the `h23` hour:
  - `h = Number(hour)`;
  - the period is `h < 12 ? 'AM' : 'PM'`;
  - `h12 = h % 12 === 0 ? 12 : h % 12`, with no leading zero.

  This avoids the locale-dependent `dayPeriod` part and does not double the cache.
- **Cache bound.**
  - The `Map` is capped at 12 entries. Inserting a 13th key evicts the oldest (the first key in insertion order).
  - In practice it holds 3 entries (`UTC`, one `S:`, one `L:`).
  - Rejected server zones never become keys.
  - `export function formatterCacheSize(): number` exists for the AC-071 test.
- **Outputs.** In 12-hour form the hour has no leading zero and is followed by ` AM` or ` PM`:

  | Function / precision | 12-hour | 24-hour (unchanged) |
  |---|---|---|
  | `formatTimestamp` `second` | `YYYY-MM-DD h:mm:ss AM ZONE` | `YYYY-MM-DD HH:mm:ss ZONE` |
  | `minute` | `YYYY-MM-DD h:mm AM ZONE` | `YYYY-MM-DD HH:mm ZONE` |
  | `time` | `h:mm:ss AM ZONE` | `HH:mm:ss ZONE` |
  | `clock` (new, FR-033) | `h:mm AM ZONE` | `HH:mm ZONE` |
  | `formatChartTick` | `h:mm AM` | `HH:mm` |
  | `formatChartTooltip` | second form | second form |
  | `formatDateOnly(value, opts)` (new) | `YYYY-MM-DD` in the resolved zone | same |

- **Bad input.** Missing or unparseable input gives `—` in every function and cycle, and nothing throws (FR-035). `parseInstant` is unchanged.
- **Local zone name.** `export function localZoneName(localTimeZone?: string): string` returns, in order of preference:
  - the test seam;
  - `new Intl.DateTimeFormat().resolvedOptions().timeZone`;
  - `''` if that throws (it is called inside a `try`).

  It is called once per provider (memoised), never per render.
- `formatRelative` and `formatUtc` are unchanged (FR-037, FR-067).

**Rationale.**
- Keeping the `h23` formatter makes every 24-hour string byte-identical to today (FR-032; AC-005; the second half of AC-006).
- The hour arithmetic is trivially correct at midnight and noon (AC-008, AC-009).
- The zone label still comes from `Intl`, so ICU handles DST (AC-011, AC-012).

**Alternatives considered.** *A second `Intl` formatter per zone with `hourCycle: 'h12'`.* Rejected: it doubles the cache and depends on the case of the `dayPeriod` part and on engine quirks.

### AD-009: Preference storage, legacy mapping and the no-provider default

**Decision.**
- **Keys.** Both are exported from the provider:
  - `TIME_ZONE_STORAGE_KEY = 'riot-time-zone'`: unchanged name; values `Server`, `UTC` or `Local`;
  - `HOUR_CYCLE_STORAGE_KEY = 'riot-hour-cycle'`: values `12h` or `24h`.

  That is two keys, with values of at most 6 characters (NFR-001 c).
- **Telling pre-story from post-story values (FRD 7.4).** The hour-cycle key is the marker: no build before this story ever wrote it, and every write after this story writes it.
  - Read the cycle key first.
  - If it is exactly `12h` or `24h`, the zone key holds a post-story value. `Server`, `UTC` and `Local` map to themselves; anything else gives `Server`.
  - Otherwise (absent, tampered or unreadable), the zone key holds a legacy value. Exactly `Local` gives `Local`; anything else, including `UTC`, gives `Server` (DEC-5). The hour cycle is `12h`.
  - If a read throws, the result is `Server` and `12h` (FR-048).
- **Writes.**
  - `setZone(z)` and `setHourCycle(c)` update state first, then write **both** keys: zone first, then cycle. Each write is in its own `try`.
  - The cycle key is written only if the zone write did not throw. A half-written pair therefore can never make a legacy `UTC` look like a post-story choice.
  - A failed write never throws, and the session keeps the choice (FR-048).
- `toggleZone` is removed; nothing calls it after AD-010.
- **Context value.** `{ zone, hourCycle, setZone, setHourCycle, serverZone, setServerZone, localZoneName, zoneOpts, now }`, memoised on `[zone, hourCycle, serverZone, localTimeZone, now]`.
  - It changes only when a preference or the server zone changes.
  - So the 1 s clock tick re-renders nothing outside the clock (NFR-004).
- **Provider props.** `storage`, `localTimeZone` and `now` are unchanged. A new test seam, `serverZone?: unknown`, is the initial value; it goes through `acceptServerZone` exactly like an API value.
  - Inside the provider, `localTimeZone` is forwarded only into `zoneOpts` and `localZoneName`, both of which live in `utils/format.ts`.
  - No production module passes it (C-3).
- **No-provider default.** `DEFAULT_VALUE` is used only when no provider is mounted: `zone: 'UTC'`, `hourCycle: '24h'`, no server zone, no-op setters.
  - Production always mounts the provider (`main.tsx`), and a static guard asserts it.
  - This keeps UIF AD-007's property: the many page tests that render `<Timestamp>` without a provider need no wrapper and keep their strings.
  - The production default, with empty storage, is Server / 12h (BR-005).

**Rationale.** No third key and no `removeItem`, so the bound is exactly two keys. The marker rule is a pure function of the two stored strings and can be tested exhaustively.

**Alternatives considered.**
- *A new key `riot-time-zone-v2`.* Rejected: the legacy key would stay behind (three keys) unless it is deleted.
- *Encoding post-story values differently (for example `UTC2`).* Rejected: the values readable in storage are part of the README contract (FR-070).

### AD-010: Header clock panel

**Decision.** `HeaderClock.tsx` keeps its own 1 s interval and the `<time>` element. Its structure:

- **Root.** A `div` (`relative`) that owns the outside-click and Escape behaviour, following `ui/Popover.tsx`:
  - while the panel is open, a `useEffect` listens for `pointerdown` on `document`; a press outside the root closes the panel;
  - `onKeyDown` on the root: Escape closes the panel and focuses the clock button (FR-045);
  - `onBlur` on the root: when focus leaves the root (Tab out), the panel closes.
- **Clock button.** `button[type=button]` with:
  - `focusRing`;
  - `aria-expanded={open}`;
  - `aria-controls={open ? panelId : undefined}`;
  - `aria-label` and `title` both set to `Time display: <zone>, <cycle>. Change time display` (FR-046):
    - `<zone>` is `Server (<serverZone ?? 'UTC'>)`, `Local (<localZoneName>)` (just `Local` when the name is empty), or `UTC`;
    - `<cycle>` is `12-hour` or `24-hour`.

  Activating it toggles `open`, so a second activation closes the panel (FR-045).
- **Panel.** Rendered while open, inside the root, with no portal, so the root's containment checks work. It is a `div role="group" aria-label="Time display" id={panelId}` that contains two radio groups:
  - **Time zone:** a `div role="radiogroup" aria-labelledby={zoneHeadingId}` with the heading text `Time zone`. It holds native `input type="radio" name={`${id}-zone`}` options, in this order: `Server (<zone>)`, `Local (<IANA>)`, `UTC`.
  - **Clock:** a `div role="radiogroup" aria-labelledby={cycleHeadingId}` with the heading text `Clock`. It holds `12-hour` and `24-hour` (`name={`${id}-cycle`}`).
  - Each radio is wrapped in a `<label>` whose text is its accessible name, and carries `focusRing`.
  - `onChange` calls `setZone` or `setHourCycle`. Selecting never closes the panel (FR-045).
  - Native radios give arrow-key move-and-select within a group, and Tab moves between groups (NFR-008). `@testing-library/user-event` 14 implements arrow keys on radios.
  - When the panel opens, focus moves to the checked radio of the "Time zone" group.
- **Positioning.** `@floating-ui/react` (already a dependency), with the same middleware as `PatchBadge`: `useFloating({ open, placement: 'bottom-end', strategy: 'fixed', whileElementsMounted: autoUpdate, middleware: [offset(4), flip({ padding: 8 }), shift({ padding: 8 }), size({ padding: 8, apply → maxHeight = max(availableHeight, 120) }) ] })`.
- **Classes.** Tokens, as in `ui/Popover.tsx`: `z-(--z-dropdown) w-64 max-w-[calc(100vw-16px)] overflow-y-auto scrollbar-thin rounded-lg border border-border-strong bg-raised p-3 shadow-xl`.
- **Clock text.** `formatTimestamp(nowMs, zoneOpts, 'time')`, so it follows both preferences (FR-041).

**Rationale.**
- DEC-4 fixes the interaction.
- Native radios are the most robust accessible radio group and need no roving-focus code.
- Mirroring `Popover`'s close rules keeps one dismissal model in the app.
- `floating-ui` keeps the panel inside the viewport on narrow screens, as it does for `PatchBadge`.

**Alternatives considered.**
- *Reuse `ui/Popover` as is.* Rejected: its trigger is a labelled secondary `Button`, and it positions with `absolute left-0`, which overflows in the right-hand header slot.
- *`role="menu"` with `menuitemradio`.* Rejected: menus close on selection, which FR-045 forbids, and they need roving focus.

### AD-011: Dashboard call sites

**Decision.**
- **`ActivityLabel.tsx` and `AgentUpdatingVersion.tsx`:** `const { timestamp } = useTimeFormatters(); const formatTime = (iso: string) => timestamp(iso, 'clock')`.
  - `formatChartTick` is no longer imported in either file (DEC-9).
  - `utils/activity.ts` is unchanged; it receives `formatTime` by injection.
- **`utils/cron.ts`:**
  - `formatRunDate(value, fallback = '-', opts: ZoneOpts = { zone: 'UTC', hourCycle: '24h' })`. The default keeps every existing test's string. Production calls pass `zoneOpts`, and a guard enforces it.
  - `parseSystemdCalendar(calendar, hourCycle: HourCycle = '24h')`. The default keeps every pre-existing test unchanged (AC-038).
  - A new internal `clockText(h, mm, cycle)`:
    - 24h gives `HH:MM` (today's output);
    - 12h gives `${h % 12 || 12}:${MM} ${h < 12 ? 'AM' : 'PM'}`.
  - The single-hour `00:00` → `Daily at midnight` rule is checked numerically and applies to both cycles.
  - Hour lists render every entry with `clockText`. There is no midnight word inside a list (FRD 7.5).
  - If any hour is above 23, the 24-hour text is returned unchanged, so no meaningless AM/PM appears.
  - **Lower-casing fix.** `parts.push(`at ${…toLowerCase()}`)` (`cron.ts:114`) lower-cases the whole phrase, which would turn `3:10 AM` into `3:10 am`. It is replaced by lower-casing the **first character only**. Every pre-existing output is identical (`midnight`, `every hour`, `every 10 minutes`, `every hour at :07`, `every 6 hours`), because only the first letter was ever upper-case.
- **`pages/DeviceDetail.tsx`:**
  - `parseSystemdCalendar(timer.calendar, zoneOpts.hourCycle)`. `formatRunDate(…, '-', zoneOpts)` already passes `zoneOpts`.
  - Download filename (R-2): `const { zoneOpts, now } = useTimePreference()`, then `const day = formatDateOnly(now(), { ...zoneOpts, zone: 'Server' })`. This is the server display-zone date when it is known, otherwise UTC. `new Date().toISOString()` is removed, so there is no time bomb.
- **Automatic sites.** Every `<Timestamp>`, `<RelativeTime>`, chart and `LogView` site follows through `zoneOpts` / `useTimeFormatters` with no source change (FR-060, FR-061; OQ-001 rows 31 and 32).
- **Unchanged:**
  - `AgentManagement.tsx` (DEC-11);
  - the `docker/ActivityPage.tsx` filters (DEC-12);
  - `formatUtc` (DEC-18);
  - `PatchBadge.tsx`, which renders server text as React text.

### AD-012: Summary download filename on both sides (R-2)

**Decision.**
- Server `Content-Disposition`: see AD-005.
- Browser `a.download`: see AD-011.
- Both use the display-zone date of the moment the summary is generated. Each side's sanitising rules are unchanged.

**Rationale.**
- FR-025's intent (DEC-13: the filename date matches the `Generated` line) is only met if the name the browser saves changes.
- The browser cannot read the server's header today, because `api.getDeviceSummary` returns only the body text.
- Changing that API would be a bigger change than computing the same date with the server zone the dashboard already knows.

**Consequences.** If the zone request failed, the browser falls back to the UTC date while the file says, for example, EDT. That is visible, bounded, and corrects itself on the next page load (residual RS-2).

### AD-013: Existing tests that change (deliberately, never loosened)

**Rule.**
- A changed expectation either states the **new default** explicitly or **pins** the zone and hour cycle explicitly.
- No `toMatch` loosening, no deletion, no skip.
- Every describe title that carries a superseded UIF tag (AC-104, AC-105, AC-113 to AC-118) keeps that tag and gains the TIME tag that supersedes it (FR-072, AC-074).

**Go**

| File | Lines | Change |
|---|---|---|
| `internal/models/expect_test.go` `TestEX_Messages_Templates` | 162, 171-175 | Build `at` and `back` in `time.UTC`, not `time.Local`. With `Zone` nil, expect `… (expected, back by 2:32 PM UTC)` and `… powered off by ryan at 2:02 PM UTC`. Today's test depends on the machine's zone; the new one does not |
| `internal/server/expect/harness_test.go` | 781 | `h.local(t)` becomes `models.FormatDisplayClock(t, h.displayZone)`. The harness zone is nil by default (UTC). The consumers `selfupdate_test.go:65,891`, `publisher_test.go:134` and `agent_shutdown_test.go:91` need no source change |
| `internal/server/events/generator_expect_test.go` | 252 | `expectBase.Local().Format("15:04")` → `models.FormatDisplayClock(expectBase, nil)` |
| `internal/server/handlers/patch_badge_test.go` | 128, 131, 222, 653, 829, 918 | 12-hour UTC (zone nil): `… on 2026-10-25 3:00 AM UTC (estimate).`, `Yes. Next window starts 2026-10-25 3:00 AM UTC.`, `2026-10-23 10:00 AM UTC (26 h ago)`, `2026-10-21 12:00 PM UTC (3 d ago)`. Line 127 (`Auto (reboot on 2026-10-25)`) is unchanged |
| `internal/server/handlers/patch_badge_extra_test.go` `frozenPreStoryLines` | 470, 475, 479 | The frozen Automatic line uses `models.FormatDisplayMinute(nextStart, nil)`. `asOfText(in.UpdatesAt, now, nil)`. `rebootFacts{…}` is unchanged (zero `zone` = UTC). The oracle still freezes the **structure**; only the shared time format moves |
| `internal/server/summary/summary_test.go` | 185, 296, 321, 332, 386, 427, 433, 479 | `Render(device, snap)` → `Render(device, snap, fixedGenerated, nil)` (signature only) |

**Web**

| File | Change |
|---|---|
| `utils/format.test.ts` | The `UTC` and `LA` constants gain `hourCycle: '24h'`; every expected string is unchanged. The `[UIF AC-104]`, `[UIF AC-105]` and `[UIF AC-118]` titles gain `[TIME-12H-001 AC-005]`, `[AC-007]` and `[AC-013]` respectively, and say "24-hour" |
| `utils/cron.test.ts` l.72 | `{ zone: 'Local', localTimeZone }` gains `hourCycle: '24h'`; the expected string is unchanged. Every other existing case is untouched |
| `contexts/TimePreferenceProvider.test.tsx` | Changes listed below this table |
| `components/HeaderClock.test.tsx` | Changes listed below this table |
| `components/ui/Timestamp.test.tsx` `[UIF AC-117]` | `Toggle` calls `setZone('Local')`. The provider storage is seeded with `UTC` + `24h`, so the strings are unchanged |
| `components/ui/RelativeTime.test.tsx` | `renderAt` seeds `UTC` + `24h`. The Local case seeds `Local` + `24h`. Both strings are unchanged |
| `components/chartTimeWiring.test.tsx` `[UIF AC-118]` | The storage stub answers per key. The "by default" cases move to the new default (Server, 12h, no server zone): tick `2:03 PM`, tooltip `2026-10-17 2:03:22 PM UTC`. The Local cases seed `Local` + `24h`, so their strings are unchanged |
| `pages/FleetOverview.activity.test.tsx` l.179, 193, 218, 234, 235, 309, 347 | No provider, so UTC/24h. Labels gain the zone label (DEC-9), for example `Rebooting, back by 14:12 UTC`, `Powered off by ryan at 14:02 UTC`, and the title `… · started 14:00 UTC` |
| `pages/DeviceDetail.activity.test.tsx` l.136, 173 | `Rebooting, back by 14:15 UTC`, `Powered off by ryan at 14:02 UTC` |

`contexts/TimePreferenceProvider.test.tsx`:
- `Probe` uses `setZone` instead of `toggleZone`, and `memoryStorage` seeds keys individually.
- The `[UIF AC-116]` and `[UIF AC-117]` tests seed `UTC` + `24h` and switch with `setZone('Local')`. Their strings are unchanged.
- `[UIF AC-103]` "defaults to UTC" becomes "defaults to Server, 12-hour": `2026-10-17 2:03:22 PM UTC` with no server zone.
- `[UIF SEC-006]`: tampered values now resolve to `Server` and `2026-10-17 2:03:22 PM UTC`.
- QA default-storage block:
  - a legacy `Local` resolves to Local, 12h: `2026-10-17 7:03:22 AM PDT`;
  - after `setZone('UTC')`, storage holds `UTC` and `riot-hour-cycle` = `12h`.
- "works without a provider (UTC default)" is unchanged.

`components/HeaderClock.test.tsx`:
- `clock()` queries `/Time display/`.
- `[UIF AC-113]`: seeded with `UTC` + `24h`. It shows `14:03:22 UTC` with the name `Time display: UTC, 24-hour. Change time display`. The `<time>` element is kept.
- `[UIF AC-114]`: activating the clock opens the panel, and selecting `Local (America/Los_Angeles)` gives `07:03:22 PDT` (seeded 24h). The Enter/Space test opens and closes the panel.
- `[UIF AC-115]`: the selection persists both keys and is restored on remount.
- `[UIF AC-116]`: throwing storage, `banana` and an IANA value all fall back to Server / 12h: `2:03:22 PM UTC`.
- The focus-ring and tick tests seed `UTC` + `24h`, so their strings are unchanged.

Unchanged on purpose:
- `utils/activity.test.ts`, which injects its own `formatTime`.
- Every page test without a provider that asserts `… UTC` timestamps (`Alerts`, `ActivityLog`, `CertificateSettings`, `SelfUpdateDetails`).
- Server-text fixtures (`as_of`, event messages) in `PatchBadge*`, `FleetOverview*`, `SelfUpdateDetails`, `DeviceDetail.selfupdate` and `command_events_test.go`. These are stored old text (DEC-17).

### AD-014: Static guards

**Go** (`internal/server/time12h_guard_test.go`, package `server`). It walks `../models` and `.` recursively with `filepath.WalkDir`, non-test `.go` files only:
- `TestTIME12H_AC049_NoProcessLocalZoneInServerText`: no match of `\.Local\(\)` or `\btime\.Local\b` anywhere in `internal/server/**` or `internal/models/**`.
- `TestTIME12H_OQ001_NoTwentyFourHourHumanLayouts`: in those trees, no string literal contains `15:04`, and no literal ending in ` UTC"` is passed to `Format`. `models/display_time.go` is the only file allowed to define clock layouts (it uses `3:04`).
- Self-test: the patterns catch synthetic `t.Format("2006-01-02 15:04")` and `.Local()` fragments.
- Each guard prints how many files it scanned and asserts the count is above 100.

**Web** (additive describes in `web/src/test/static-guards.test.ts`):
- `TIME-12H-001 [AC-020] only utils/format.ts constructs a zoned Intl.DateTimeFormat`: in non-test source, `Intl.DateTimeFormat(` appears only in `utils/format.ts` and `pages/settings/AgentManagement.tsx`. The AgentManagement occurrence contains no `timeZone` (DEC-11).
- **`TIME-12H-001 [SEC-003] the AcceptedZone brand and the localTimeZone seam cannot be bypassed`** (C-3). In non-test source:
  - (a) a type assertion to `AcceptedZone` matches `/\bas\s+(unknown\s+as\s+)?AcceptedZone\b/` (after `stripComments`) only in `utils/format.ts`;
  - (b) the identifier `localTimeZone` appears only in `contexts/TimePreferenceProvider.tsx` and `utils/format.ts`;
  - (c) no production file sets it as a JSX prop: `/\blocalTimeZone\s*=/` matches in no `.tsx` file other than the provider. Inside the provider it is only destructured from props and forwarded into `zoneOpts`/`localZoneName`.
  - Self-tests show the patterns catch `x as AcceptedZone`, `x as unknown as AcceptedZone` and `<TimePreferenceProvider localTimeZone="Mars/Olympus">`, and do not match `type AcceptedZone = …` or `AcceptedZone | null`.
- `TIME-12H-001 [AC-036] production calls pass the preference to formatRunDate and parseSystemdCalendar`: every `formatRunDate(` call outside `utils/cron.ts` has three top-level arguments, and every `parseSystemdCalendar(` call has two.
- `TIME-12H-001 [FR-047] only TimePreferenceProvider names the time storage keys`: the literals `riot-time-zone` and `riot-hour-cycle` appear in no other non-test source.
- `TIME-12H-001 [FR-062] activity labels do not use the chart tick formatter`: neither `components/ActivityLabel.tsx` nor `components/AgentUpdatingVersion.tsx` references `formatChartTick`.
- `TIME-12H-001 [BR-005] the provider is mounted at the root and the zone sync in the authenticated app`: `main.tsx` contains `<TimePreferenceProvider>`, and `App.tsx` contains `<ServerZoneSync />`.
- The existing `[UIF SEC-005]` HTML-sink guard is unchanged and must stay green (C-4).

### AD-015: Bounds (Ryan's standing rule)

| Thing | Bound | Cleanup |
|---|---|---|
| Database | **No** table, column, index or migration (AC-070) | n/a |
| Server persisted state, files | None. The zone is one `*time.Location` in memory for the life of the process | Process exit |
| Server logs | At most 1 Info and 2 Warn per process start (one Warn per invalid source), because the resolver runs once, from `Start` only (C-4). Nothing per request or per render. Each logged value is at most 64 runes with no Cc/Cf (C-2) | Existing log retention. The at most 2 WARN rows also enter the DB log store (Settings → Logs), under its existing retention and the global log budget |
| Zone label | Always 1 to 6 ASCII letters or `GMT[±H[:MM]]` (at most 9 bytes), whatever the zone database contains (C-1) | n/a |
| Event message length | Each affected message carries one time. `15:04` (5 bytes) becomes at most `12:59 PM GMT+5:45` (17 bytes): **+12 bytes per new event**. This now holds even with a hostile zone database (C-1); the FRD's ≤ 24 bytes holds | 90-day event retention |
| Old stored messages | Untouched (DEC-17) | Retention |
| Endpoint | One response of about 90 bytes or less per page load per browser | n/a |
| Binary size | About +450 KB for `time/tzdata` (AD-002) | n/a |
| Browser storage | Exactly 2 keys (`riot-time-zone`, `riot-hour-cycle`), values of at most 6 characters | Overwritten in place |
| Formatter cache | At most 12 entries: a hard cap with oldest-first eviction, typically 3. Rejected zone strings never create entries, and the acceptance check caches nothing (AC-071) | Page lifetime |
| React Query | One cache entry, `['time-display']` | Page lifetime |
| Timers | Unchanged: the clock's single 1 s interval, cleared on unmount | Unmount |

### AD-016: AC-064 is fulfilled on the path that actually pushes (R-1)

**Decision.**
- The held-escalation summary (FR-028) is the only one of the FR-026 to FR-028 messages that becomes a notification. AC-064 is verified there: the push published through `Escalator.PublishEscalation` carries `… were held since 1:05 PM EDT; see Alerts` unchanged (FR-029).
- For FR-026 and FR-027, the test checks that the **stored and broadcast** event message carries the labelled time, and that `Dispatcher.Dispatch` is still **not** called (EXPECT-001 AD-005 is unchanged).
- No alerting behaviour changes.

**Rationale.**
- Making powered-off or expected-offline transitions push would reverse EXPECT-001's core decision that expected activity is silent.
- It is also outside the scope of DEC-6, which is about the text, not the routing.
- The FRD's statement that "the ntfy pushes read 'at 17:05'" is accurate only for the held summary. The other two texts appear only on the dashboard: Alerts, device events and activity history.

**Consequence for Ryan.** Recorded under "Open points": the README must not claim that powered-off or expected-offline messages are pushed.

---

## 4. Component Changes

| Action | File Path | Purpose |
|---|---|---|
| MODIFY | `cmd/riot-server/main.go` | `_ "time/tzdata"` (AD-002) |
| MODIFY | `internal/server/config.go` | `DisplayTZRaw` and `TZRaw`, read in `LoadConfig`; `DisplayZone`, the source constants, `ResolveDisplayZone`, `validDisplayZoneName`, `sanitizeZoneValue` (Cc+Cf, valid UTF-8, 64 runes) (AD-001, C-2) |
| CREATE | `internal/server/config_display_zone_test.go` | C: AC-042 to AC-049, the C-2 subtest, the C-4 log-phrase checks |
| MODIFY | `internal/server/server.go` | `Server.DisplayZone`; resolve once after l.158; pass `DisplayZone` in `expect.Options` |
| MODIFY | `internal/server/router.go` | `HandlerDeps.DisplayZone: s.DisplayZone`; route `GET /api/v1/server/time-display` in the `adminAuth` group (AD-006) |
| CREATE | `internal/server/router_time_display_test.go` | RT: AC-050 (incl. expired and bad-signature JWT), AC-051 |
| CREATE | `internal/server/time12h_guard_test.go` | SG: the AD-014 Go guards, and the C-4 single-call-site check |
| CREATE | `internal/server/time12h_docs_test.go` | D: AC-068, AC-069 |
| CREATE | `internal/models/display_time.go` | `FormatDisplayFull`/`Minute`/`Clock`/`Date`, `DisplayZoneLabel` with the 1 to 6 ASCII-letter rule (AD-003, C-1) |
| CREATE | `internal/models/display_time_test.go` | DT: AC-063 (label rules), AC-067, SEC-001 |
| MODIFY | `internal/models/expect.go` | `OfflineCoverage.Zone`; `Message` uses `FormatDisplayClock` (no `.Local()`) |
| MODIFY | `internal/models/expect_test.go` | `TestEX_Messages_Templates` (AD-013) |
| CREATE | `internal/models/expect_time_test.go` | MT: AC-049 (message part), AC-059, AC-060, AC-062 |
| MODIFY | `internal/server/expect/tracker.go` | `Options.DisplayZone`; `cov.Zone` at l.497 |
| MODIFY | `internal/server/expect/publisher.go` | `publisher.zone`, passed to `heldSummaryMessage` |
| MODIFY | `internal/server/expect/messages.go` | `heldSummaryMessage(n, host, since, loc)` (no `.Local()`) |
| MODIFY | `internal/server/expect/harness_test.go` | Additive `displayZone` option; `h.local` uses the helper (AD-013) |
| CREATE | `internal/server/expect/messages_time_test.go` | XM: AC-061, AC-064 (push) |
| MODIFY | `internal/server/events/generator_expect_test.go` | l.252 (AD-013) |
| CREATE | `internal/server/events/generator_time_test.go` | G: AC-064 (not pushed), AC-065 (stored `created_at`) |
| MODIFY | `internal/server/handlers/handlers.go` | `HandlerDeps.DisplayZone`, `Handlers.displayZone`, `zone()`. `DeviceSummary` uses `h.clock()`, `h.zone()`, `Render(…, generated, zone)`, and `FormatDisplayDate` for the filename |
| CREATE | `internal/server/handlers/time_display.go` | The `TimeDisplay` handler (AD-006) |
| MODIFY | `internal/server/handlers/patch_badge.go` | `patchBadgeInput.Zone`; `rebootFacts.zone`; `asOfText(…, loc)`; the four renders via the helper (AD-005); comments |
| MODIFY | `internal/server/handlers/fleet.go` | `Zone: h.zone()` in the badge input |
| MODIFY | `internal/server/handlers/patch_badge_test.go` | AD-013 |
| MODIFY | `internal/server/handlers/patch_badge_extra_test.go` | AD-013 |
| CREATE | `internal/server/handlers/patch_badge_time_test.go` | PB: AC-052 to AC-056, AC-065 (JSON), AC-066 (window math) |
| CREATE | `internal/server/handlers/summary_handler_time_test.go` | SH: AC-058 |
| MODIFY | `internal/server/summary/summary.go` | `Render(device, snapshot, generated, zone)`; `Generated` via `FormatDisplayFull` |
| MODIFY | `internal/server/summary/summary_test.go` | Signature (AD-013) |
| CREATE | `internal/server/summary/summary_time_test.go` | SM: AC-057, AC-063 |
| CREATE | `internal/server/partition/plan_time_test.go` | PT: AC-066 |
| CREATE | `internal/server/quota/daily_time_test.go` | QT: AC-066 (package-internal; reads `m.get(id, now).Day`) |
| MODIFY | `web/src/utils/format.ts` | AD-007, AD-008 (the only production `as AcceptedZone`) |
| MODIFY | `web/src/utils/format.test.ts` | AD-013 |
| CREATE | `web/src/utils/format.time12h.test.ts` | WF: AC-001 to AC-015, AC-018 (acceptance), AC-020, AC-071 |
| MODIFY | `web/src/contexts/TimePreferenceProvider.tsx` | AD-007, AD-009 |
| MODIFY | `web/src/contexts/TimePreferenceProvider.test.tsx` | AD-013 |
| CREATE | `web/src/contexts/TimePreferenceProvider.time12h.test.tsx` | WP: AC-017, AC-027 to AC-029, AC-072 |
| MODIFY | `web/src/components/HeaderClock.tsx` | AD-010 |
| MODIFY | `web/src/components/HeaderClock.test.tsx` | AD-013 |
| CREATE | `web/src/components/HeaderClock.time12h.test.tsx` | WH: AC-021 to AC-026, AC-030 |
| CREATE | `web/src/components/ServerZoneSync.tsx` | AD-007 |
| CREATE | `web/src/components/ServerZoneSync.test.tsx` | WS: AC-018 (API path), AC-019, NFR-006 (404 and `200 text/html`) |
| MODIFY | `web/src/App.tsx` | Mount `<ServerZoneSync />` next to `<GlobalWSHandler />` |
| MODIFY | `web/src/api/client.ts` | `TimeDisplayInfo`, `api.getTimeDisplay` |
| MODIFY | `web/src/api/demo-client.ts` | `getTimeDisplay` → `America/New_York` |
| MODIFY | `web/src/api/demo-data.ts` | l.743 `as_of` (FR-069) |
| CREATE | `web/src/api/demo-time.test.ts` | WD: AC-040, AC-041 |
| MODIFY | `web/src/components/ActivityLabel.tsx`, `web/src/components/AgentUpdatingVersion.tsx` | Clock form (AD-011) |
| CREATE | `web/src/components/ActivityLabel.time12h.test.tsx` | WA: AC-034, AC-035 |
| MODIFY | `web/src/utils/cron.ts` | Hour cycle, `clockText`, first-letter lower-casing, default opts (AD-011) |
| MODIFY | `web/src/utils/cron.test.ts` | l.72 pin (AD-013); additive AC-037 and AC-038 describes |
| MODIFY | `web/src/pages/DeviceDetail.tsx` | `parseSystemdCalendar(…, zoneOpts.hourCycle)`; download filename via `formatDateOnly` and `now()` (AD-011, AD-012) |
| CREATE | `web/src/pages/DeviceDetail.time12h.test.tsx` | WDD: AC-036, AC-058 (browser filename) |
| CREATE | `web/src/pages/time12h.callsites.test.tsx` | WC: AC-032, AC-033, AC-039 |
| CREATE | `web/src/components/ui/Timestamp.time12h.test.tsx` | WT: AC-016 |
| MODIFY | `web/src/components/ui/Timestamp.test.tsx`, `web/src/components/ui/RelativeTime.test.tsx`, `web/src/components/chartTimeWiring.test.tsx` | AD-013. `chartTimeWiring` also gains the AC-013 wiring cases |
| MODIFY | `web/src/pages/FleetOverview.activity.test.tsx`, `web/src/pages/DeviceDetail.activity.test.tsx` | AD-013 |
| MODIFY | `web/src/test/static-guards.test.ts` | The AD-014 web guards (additive), including the SEC-003 guard and its self-tests |
| MODIFY | `.env.example` | A commented `# RIOT_DISPLAY_TZ=America/New_York` with a one-line explanation, near `RIOT_TRUSTED_PROXIES` (FR-071) |
| MODIFY | `docker-compose.yml` | A commented `# - RIOT_DISPLAY_TZ=America/New_York` in the `riot-server` `environment:` list, in the same style as l.29 (FR-071) |
| MODIFY | `README.md` | Written by engineering so QA can check AC-068; the technical writer polishes it. Changes listed below this table |
| MODIFY | `web/src/components/ui/README.md` | Formatter table (12-hour forms, `clock`, `formatDateOnly`, `acceptServerZone`, the brand rule). Preference paragraph (three zones, hour cycle, two keys, legacy mapping) (FR-071) |
| MODIFY | `CHANGELOG.md` | Technical writer |

`README.md` changes:
- the "Time Display" section covers the FR-070 items, and the "always show UTC" sentence is replaced;
- the env-var table gets a row for `RIOT_DISPLAY_TZ` (FR-070a). It presents `RIOT_DISPLAY_TZ` as the preferred setting over `TZ`, and says that `TZ` also changes the process zone and log offsets (INFO-3);
- the API section lists `GET /api/v1/server/time-display`;
- the l.860 sentence is replaced (R-4).

Not changed:
- `Dockerfile`, `go.mod`/`go.sum`, `web/package.json` and its lockfile;
- `internal/server/events/generator.go` and `internal/server/notify/*`;
- `web/src/utils/activity.ts`, `web/src/components/PatchBadge.tsx`, `web/src/pages/settings/AgentManagement.tsx`, `web/src/pages/docker/ActivityPage.tsx`, `web/src/components/docker/LogView.tsx`;
- any migration, and any `internal/agent` file.

---

## 5. Data Model Changes

**None in the database.** No migration under `cmd/riot-server/migrations/` (AC-070).

In-memory and browser structures:

```
// internal/server/config.go (in memory, per process)
Config.DisplayTZRaw string   // RIOT_DISPLAY_TZ, verbatim
Config.TZRaw        string   // TZ, verbatim
DisplayZone { Location *time.Location; Name string; Source "RIOT_DISPLAY_TZ" | "TZ" | "default" }
Server.DisplayZone *time.Location

// internal/models/expect.go
OfflineCoverage.Zone *time.Location   // nil = UTC; not serialised

// web (localStorage)
riot-time-zone   : "Server" | "UTC" | "Local"     (legacy "UTC"/"Local" read per AD-009)
riot-hour-cycle  : "12h" | "24h"                    (a valid value here marks a post-story write)

// web (memory)
TimePreferenceValue.serverZone : AcceptedZone | null
```

`TimeZonePref` changes from `'UTC' | 'Local'` to `'Server' | 'UTC' | 'Local'`. `ZoneOpts` gains the required `hourCycle` and the optional `serverTimeZone`.

---

## 6. API / Interface Contract

### 6.1 `GET /api/v1/server/time-display` (new)

- **Auth:** the admin session cookie `riot_session` (the `adminAuth` group). `X-rIOt-Key` is not accepted.
- **Request:** no parameters, no body.
- **200** `application/json`, with `Cache-Control: no-store`:
  ```json
  { "display_zone": "America/New_York" }
  ```
  `display_zone` is always a string: the validated, trimmed source value (FR-008), or `"UTC"`.
- **401** for no cookie, an expired or invalid session (including a bad signature), or a device key only. The body is the middleware's existing `{"error":"unauthorized"}`.
- The handler produces no other status codes.

### 6.2 Unchanged APIs

`GET /api/v1/auth/check` and every other response keep their fields (FR-012). Every time field stays RFC 3339 in UTC: `patch_badge.reboot_at`, `next_window_start`, `updates_at`, and event `created_at` (FR-029b).

### 6.3 Go interfaces (signatures)

```
func ResolveDisplayZone(displayTZ, tz string, log *slog.Logger) DisplayZone          // internal/server
func FormatDisplayFull(t time.Time, loc *time.Location) string                       // internal/models
func FormatDisplayMinute(t time.Time, loc *time.Location) string
func FormatDisplayClock(t time.Time, loc *time.Location) string
func FormatDisplayDate(t time.Time, loc *time.Location) string
func DisplayZoneLabel(t time.Time) string
func Render(device *models.Device, snapshot *models.TelemetrySnapshot, generated time.Time, zone *time.Location) (string, error)  // summary
func heldSummaryMessage(n int, host string, since time.Time, loc *time.Location) string        // expect (internal)
func asOfText(at *time.Time, now time.Time, loc *time.Location) string                         // handlers (internal)
```

### 6.4 Web interfaces

```
// utils/format.ts
type TimeZonePref = 'Server' | 'UTC' | 'Local'
type HourCycle = '12h' | '24h'
type AcceptedZone = string & { readonly __brand: 'AcceptedZone' }   // cast only in this file (SEC-003 guard)
interface ZoneOpts { zone: TimeZonePref; hourCycle: HourCycle; localTimeZone?: string; serverTimeZone?: AcceptedZone | null }
type TimestampPrecision = 'second' | 'minute' | 'time' | 'clock'
function acceptServerZone(raw: unknown): AcceptedZone | null
function formatTimestamp(value: InstantInput, opts: ZoneOpts, precision?: TimestampPrecision): string
function formatChartTick(ts: number, opts: ZoneOpts): string
function formatChartTooltip(ts: number, opts: ZoneOpts): string
function formatDateOnly(value: InstantInput, opts: ZoneOpts): string
function localZoneName(localTimeZone?: string): string
function formatterCacheSize(): number

// contexts/TimePreferenceProvider.tsx
const TIME_ZONE_STORAGE_KEY = 'riot-time-zone'
const HOUR_CYCLE_STORAGE_KEY = 'riot-hour-cycle'
interface TimePreferenceValue {
  zone: TimeZonePref; hourCycle: HourCycle
  setZone(z: TimeZonePref): void; setHourCycle(c: HourCycle): void
  serverZone: AcceptedZone | null; setServerZone(raw: unknown): void
  localZoneName: string; zoneOpts: ZoneOpts; now: () => number
}
props: { children; storage?; localTimeZone?; now?; serverZone?: unknown }

// utils/cron.ts
function formatRunDate(value: string | undefined, fallback?: string, opts?: ZoneOpts): string   // default { zone: 'UTC', hourCycle: '24h' }
function parseSystemdCalendar(calendar: string | undefined, hourCycle?: HourCycle): string | null  // default '24h'

// api/client.ts
interface TimeDisplayInfo { display_zone: string }
api.getTimeDisplay(): Promise<TimeDisplayInfo>
```

---

## 7. Sequence / Flow

### 7.1 Server start
1. `main` calls `LoadConfig()`, which reads `RIOT_DISPLAY_TZ` and `TZ` verbatim.
2. `Start` configures slog, the DB, migrations, the repositories and the DB log handler (l.99-158).
3. `ResolveDisplayZone(cfg.DisplayTZRaw, cfg.TZRaw, slog.Default())` runs. This is its only call site. It logs at most 2 Warn and exactly 1 Info. `s.DisplayZone = dz.Location`.
4. `expect.New(Options{…, DisplayZone: s.DisplayZone})` runs. Later, `setupRouter` calls `handlers.New(HandlerDeps{…, DisplayZone: s.DisplayZone})`. When `applyTLSAndRestart` rebuilds the router, it reuses the same `s.DisplayZone` and does not resolve again.

### 7.2 Dashboard load
1. `main.tsx` mounts `TimePreferenceProvider`. It reads both keys (AD-009), starts with `serverZone = null`, and memoises `localZoneName`.
2. The user logs in if needed. In the authenticated app, `ServerZoneSync` queries `GET /api/v1/server/time-display`.
3. While the request is pending, or after it failed (404, HTML, network error), Server renders as UTC with the label `UTC`, and the panel shows `Server (UTC)`.
4. When it resolves: `setServerZone(data.display_zone)` → `acceptServerZone` → state change → the context value changes once → every rendered time updates without a remount (FR-050).

### 7.3 Changing the display
1. Ryan activates the clock. The panel opens with focus on the checked zone radio.
2. A radio `change` calls `setZone` or `setHourCycle`. That updates state, then storage (zone key, then cycle key). The context value changes and consumers re-render. The panel stays open.
3. Escape closes the panel and returns focus to the clock. An outside pointerdown, tabbing out, or activating the clock also closes it.

### 7.4 Server text
- `GET /api/v1/fleet/patch-status` calls `computePatchBadge(in{Zone: h.zone()}, h.clock())`. The math runs in UTC, then the helper renders the text.
- Offline detector → `Generator.DeviceOfflineAt` → `Tracker.CoverOffline` returns `cov` with `Zone` set → `cov.Message(host)` → a stored info event (no push).
- Publisher → `sendSummary` → `heldSummaryMessage(…, p.zone)` → a stored warning event, a broadcast, and `PublishEscalation` (push).
- `GET …/summary` calls `Render(…, h.clock(), h.zone())`. The filename comes from `FormatDisplayDate(h.clock(), h.zone())`.

### 7.5 Summary download (browser)
`handleDownloadSummary` → `api.getDeviceSummary(id)` → `a.download = <host>-summary-<formatDateOnly(now(), {…zoneOpts, zone: 'Server'})>.md`.

---

## 8. Acceptance Criteria Mapping

**Test files.**
- Go:
  - **C** = `internal/server/config_display_zone_test.go`
  - **RT** = `internal/server/router_time_display_test.go`
  - **SG** = `internal/server/time12h_guard_test.go`
  - **D** = `internal/server/time12h_docs_test.go`
  - **DT** = `internal/models/display_time_test.go`
  - **MT** = `internal/models/expect_time_test.go`
  - **XM** = `internal/server/expect/messages_time_test.go`
  - **G** = `internal/server/events/generator_time_test.go`
  - **PB** = `internal/server/handlers/patch_badge_time_test.go`
  - **SH** = `internal/server/handlers/summary_handler_time_test.go`
  - **SM** = `internal/server/summary/summary_time_test.go`
  - **PT** = `internal/server/partition/plan_time_test.go`
  - **QT** = `internal/server/quota/daily_time_test.go`
- Web:
  - **WF** = `web/src/utils/format.time12h.test.ts`
  - **WP** = `web/src/contexts/TimePreferenceProvider.time12h.test.tsx`
  - **WH** = `web/src/components/HeaderClock.time12h.test.tsx`
  - **WS** = `web/src/components/ServerZoneSync.test.tsx`
  - **WT** = `web/src/components/ui/Timestamp.time12h.test.tsx`
  - **WC** = `web/src/pages/time12h.callsites.test.tsx`
  - **WA** = `web/src/components/ActivityLabel.time12h.test.tsx`
  - **WCR** = `web/src/utils/cron.test.ts` (additive describes)
  - **WDD** = `web/src/pages/DeviceDetail.time12h.test.tsx`
  - **WD** = `web/src/api/demo-time.test.ts`
  - **WCW** = `web/src/components/chartTimeWiring.test.tsx` (additive describes)
  - **WG** = `web/src/test/static-guards.test.ts` (additive describes)

**Conventions.**
- Go tests are named `TestTIME12H_ACxxx_…`, or `TestTIME12H_SECxxx_…` for the security conditions.
- Web `describe` titles start with `TIME-12H-001 [AC-0xx]` or `TIME-12H-001 [SEC-00x]`.
- Unless stated otherwise:
  - the instant is `2026-10-27T17:05:09Z`;
  - the server zone is `America/New_York`;
  - the Local zone is `America/Los_Angeles`, set through the `localTimeZone` seam, never `process.env.TZ`.

| AC | Fulfilled by | Test |
|---|---|---|
| AC-001 | AD-008 `second`, 12h | WF: `TIME-12H-001 [AC-001] 12-hour second form` |
| AC-002 | AD-008 `minute` | WF: `TIME-12H-001 [AC-002] 12-hour minute form` |
| AC-003 | AD-008 `time` | WF: `TIME-12H-001 [AC-003] 12-hour time form` |
| AC-004 | AD-008 `clock` | WF: `TIME-12H-001 [AC-004] clock form in both hour cycles` |
| AC-005 | AD-008 (the 24h path is today's) | WF: `TIME-12H-001 [AC-005] 24-hour forms are unchanged`; format.test.ts `[UIF AC-104] [TIME-12H-001 AC-005] …` |
| AC-006 | AD-008 UTC literal | WF: `TIME-12H-001 [AC-006] UTC zone in both hour cycles` (incl. `2026-10-17 14:03:22 UTC`) |
| AC-007 | AD-008 Local | WF: `TIME-12H-001 [AC-007] Local zone in both hour cycles`; format.test.ts `[UIF AC-105] [TIME-12H-001 AC-007] …` |
| AC-008 | 12h arithmetic | WF: `TIME-12H-001 [AC-008] midnight is 12 AM and 00` |
| AC-009 | 12h arithmetic | WF: `TIME-12H-001 [AC-009] noon is 12 PM and 12` |
| AC-010 | Zone day boundary | WF: `TIME-12H-001 [AC-010] the server-zone date differs from the UTC date` |
| AC-011 | Intl label | WF: `TIME-12H-001 [AC-011] DST fall-back repeats 1:30 AM as EDT then EST` |
| AC-012 | Intl label | WF: `TIME-12H-001 [AC-012] DST spring-forward skips from 1:59:59 AM EST to 3:00:00 AM EDT` |
| AC-013 | `formatChartTick`, chart wiring | WF: `TIME-12H-001 [AC-013] chart ticks follow the hour cycle without a label and tooltips carry it`; WCW: `[UIF AC-118] [TIME-12H-001 AC-013] MetricChart and ContainerMetricChart ticks follow the hour cycle` |
| AC-014 | `parseInstant`, em dash | WF: `TIME-12H-001 [AC-014] invalid input renders an em dash in every form and never throws` |
| AC-015 | `formatRelative` unchanged | WF: `TIME-12H-001 [AC-015] relative times are identical in every zone and hour cycle` |
| AC-016 | `RelativeTime` title via `formatTimestamp` | WT: `TIME-12H-001 [AC-016] RelativeTime title uses the default Server 12-hour form` |
| AC-017 | AD-007, AD-010 | WP: `TIME-12H-001 [AC-017] an accepted server zone formats with its abbreviation`; WH: `TIME-12H-001 [AC-017] the Server option is named with the zone` |
| AC-018 | `acceptServerZone`, sync | WF: `TIME-12H-001 [AC-018] acceptServerZone rejects every malformed value` (each listed value, `null`, `5`, missing); WS: `TIME-12H-001 [AC-018] a rejected API zone formats as UTC and names Server (UTC)` |
| AC-019 | AD-007 state update | WS: `TIME-12H-001 [AC-019] the clock shows UTC until the zone arrives, then EDT on the same node` |
| AC-020 | Brand plus SEC-003 guard, acceptance-only construction | WF: `TIME-12H-001 [AC-020] Intl formats only with UTC, accepted, seam or browser zones`. The spy distinguishes `{ timeZone }`-only acceptance calls from formatting calls, and covers `Mars/Olympus` from the API and `America/Chicago` in storage. WG: `TIME-12H-001 [AC-020] only utils/format.ts constructs a zoned Intl.DateTimeFormat`, `TIME-12H-001 [SEC-003] the AcceptedZone brand and the localTimeZone seam cannot be bypassed` |
| AC-021 | AD-009 defaults, AD-010 | WH: `TIME-12H-001 [AC-021] defaults to Server 12-hour` |
| AC-022 | FR-046 name | WH: `TIME-12H-001 [AC-022] the clock's accessible name states zone and cycle` |
| AC-023 | Panel structure | WH: `TIME-12H-001 [AC-023] the panel holds the Time zone and Clock radio groups` |
| AC-024 | Context propagation | WH: `TIME-12H-001 [AC-024] selecting UTC updates the clock and a Timestamp without remount and keeps the panel open` |
| AC-025 | `setHourCycle` | WH: `TIME-12H-001 [AC-025] selecting 24-hour updates the clock` |
| AC-026 | AD-010 close rules | WH: `TIME-12H-001 [AC-026] Escape returns focus to the clock and an outside click closes` |
| AC-027 | AD-009 writes | WP: `TIME-12H-001 [AC-027] each zone and cycle combination survives a remount` (6 cases) |
| AC-028 | AD-009 marker rule | WP: `TIME-12H-001 [AC-028] legacy UTC becomes Server, legacy Local stays, a new UTC persists` |
| AC-029 | AD-009 fallbacks | WP: `TIME-12H-001 [AC-029] tampered and failing storage fall back to Server and 12h` (5 cases; asserts that no `Intl` call receives `America/Chicago`) |
| AC-030 | Native radios, AD-010 | WH: `TIME-12H-001 [AC-030] the panel is operable by keyboard alone` |
| AC-031 | Demo build | Screenshot `docs/implementation/TIME-12H-001-screenshots/after-header-time-panel.png`, captured by engineering from a `VITE_DEMO=true npm run build` preview |
| AC-032 | Call sites via `zoneOpts` | WC: `TIME-12H-001 [AC-032] ActivityLog, Alerts and CertificateSettings timestamps use the default form` |
| AC-033 | `precision="time"` | WC: `TIME-12H-001 [AC-033] container log line time is 12-hour with a label` |
| AC-034 | AD-011 clock form | WA: `TIME-12H-001 [AC-034] activity labels carry labelled 12-hour clock times` (incl. 24-hour `Rebooting, back by 13:20 EDT`) |
| AC-035 | AD-011 titles | WA: `TIME-12H-001 [AC-035] fleet-row and agent-update titles end with the labelled start time` |
| AC-036 | AD-011, AD-014 | WDD: `TIME-12H-001 [AC-036] cron and timer run dates use the default form` (ISO and unix-seconds `1793120709`); WG: `TIME-12H-001 [AC-036] production calls pass the preference to formatRunDate and parseSystemdCalendar` |
| AC-037 | `clockText`, first-letter lower-casing | WCR: `TIME-12H-001 [AC-037] systemd calendar text in 12-hour form` (all 11 rows of FRD 7.5) |
| AC-038 | Default `'24h'` | WCR: `TIME-12H-001 [AC-038] systemd calendar text in 24-hour form` (all 11 rows); every pre-existing `parseSystemdCalendar` case unmodified |
| AC-039 | Untouched surfaces | WC: `TIME-12H-001 [AC-039] maintenance-window label, Docker filter labels and formatUtc are unchanged` |
| AC-040 | AD-007 demo | WD: `TIME-12H-001 [AC-040] the demo reports America/New_York` |
| AC-041 | `demo-data.ts` | WD: `TIME-12H-001 [AC-041] the demo patch as-of uses the server format` |
| AC-042 | AD-001 | C: `TestTIME12H_AC042_RiotDisplayTZUsed` (one Info naming zone and source, no Warn) |
| AC-043 | Trim | C: `TestTIME12H_AC043_ValueTrimmed` |
| AC-044 | `TZ` fallback | C: `TestTIME12H_AC044_TZFallback` |
| AC-045 | Blank = unset | C: `TestTIME12H_AC045_UnsetAndBlankDefaultToUTCWithoutWarning` |
| AC-046 | Shape, LoadLocation, sanitising (C-2, C-4) | C: `TestTIME12H_AC046_InvalidValuesRejectedSanitizedOneWarning`, with one subtest per listed value plus the subtest `format characters` (a value containing U+202E, U+2066 and U+200B) and an invalid-UTF-8 subtest. It asserts that the logged `value` contains no Cc or Cf rune, is valid UTF-8 and is at most 64 runes; that no captured record contains `unknown time zone` or `invalid location name`; and that nothing panics |
| AC-047 | Order | C: `TestTIME12H_AC047_InvalidFirstValidSecond` |
| AC-048 | Two warnings (C-4) | C: `TestTIME12H_AC048_BothInvalidTwoWarningsOneInfo` (also asserts that neither LoadLocation phrase appears in any record) |
| AC-049 | AD-001, AD-004, AD-014 | C: `TestTIME12H_AC049_ResolveLeavesTimeLocalUnchanged`; MT: `TestTIME12H_AC049_PoweredOffIgnoresProcessZone` (sets `time.Local` to Asia/Tokyo, restores it in `t.Cleanup`, does not run in parallel); SG: `TestTIME12H_AC049_NoProcessLocalZoneInServerText` |
| AC-050 | AD-006 (C-4) | RT: `TestTIME12H_AC050_TimeDisplayRequiresAdminSession` (all 401: no session, an `X-rIOt-Key` header only, an **expired** JWT, a JWT with a **bad signature**); `TestTIME12H_AC050_TimeDisplayReportsZone` (exact body `{"display_zone":"America/New_York"}`, `Cache-Control: no-store`); `TestTIME12H_AC050_AuthCheckResponseUnchanged` (key set and values of `auth/check` for both branches) |
| AC-051 | AD-001 + AD-006 | RT: `TestTIME12H_AC051_FallbackZoneReportedAsUTC` (`ResolveDisplayZone("Mars/Olympus", "", …)` wired into the server) |
| AC-052 | AD-005 | PB: `TestTIME12H_AC052_NextWindowLineInDisplayZone` |
| AC-053 | AD-005 | PB: `TestTIME12H_AC053_DatedRebootLineInDisplayZone` |
| AC-054 | AD-005 | PB: `TestTIME12H_AC054_LabelDateInDisplayZoneRebootAtUTC` (marshals the badge; `reboot_at` = `2026-10-28T02:00:00Z`) |
| AC-055 | AD-005 | PB: `TestTIME12H_AC055_AsOfInDisplayZoneAgeUnchanged` |
| AC-056 | AD-005 | PB: `TestTIME12H_AC056_UTCDisplayZoneKeepsTwelveHour` |
| AC-057 | AD-004, AD-005 | SM: `TestTIME12H_AC057_GeneratedInDisplayZone` |
| AC-058 | AD-005, AD-012 | SH: `TestTIME12H_AC058_FilenameUsesDisplayZoneDate` (injected clock `2026-10-28T02:00:00Z`); WDD: `TIME-12H-001 [AC-058] the saved filename uses the server-zone date` (fake `now`, stubbed `URL.createObjectURL`, captured `a.download` = `web-01-summary-2026-10-27.md`) |
| AC-059 | AD-004 | MT: `TestTIME12H_AC059_PoweredOffMessage` |
| AC-060 | AD-004 | MT: `TestTIME12H_AC060_BackByMessage`, `TestTIME12H_AC060_NoBackByUnchanged` |
| AC-061 | AD-004 | XM: `TestTIME12H_AC061_HeldSummaryMessage` |
| AC-062 | AD-003 | MT: `TestTIME12H_AC062_MidnightNoonAndDST` (six instants) |
| AC-063 | AD-003 label rules | SM: `TestTIME12H_AC063_NumericAbbreviationsNormalised` (Sao_Paulo, Kathmandu, Kolkata); DT: `TestTIME12H_AC063_ZoneLabelRules` (incl. zero offset → `GMT`, `GMT+5:30`, `GMT-9:30`, and `EDT`, `EST`, `IST`, `UTC`, `GMT`, `BST` unchanged) |
| AC-064 | AD-016 | XM: `TestTIME12H_AC064_HeldSummaryPushCarriesLabelledTime` (the harness escalator receives `since 1:05 PM EDT`); G: `TestTIME12H_AC064_PoweredOffEventLabelledAndNeverPushed` |
| AC-065 | AD-005 | PB: `TestTIME12H_AC065_PatchStatusTimeFieldsStayUTC` (every RFC 3339 field ends in `Z`); G: `TestTIME12H_AC065_StoredEventCreatedAtIsUTC` |
| AC-066 | UTC math untouched | PT: `TestTIME12H_AC066_PartitionPlanIgnoresProcessZone`; QT: `TestTIME12H_AC066_QuotaDayIgnoresProcessZone`; PB: `TestTIME12H_AC066_MaintenanceWindowIgnoresProcessAndDisplayZone`. Each sets `time.Local` to Pacific/Kiritimati, restores it in `t.Cleanup`, and does not run in parallel |
| AC-067 | AD-003 nil guard | DT: `TestTIME12H_AC067_NilZoneFormatsUTCWithoutPanic` |
| AC-068 | README | D: `TestTIME12H_AC068_ReadmeTimeDisplay`. It asserts `RIOT_DISPLAY_TZ`, `riot-hour-cycle`, `Server`, the legacy mapping, `/api/v1/server/time-display`, and that `RIOT_DISPLAY_TZ` is named as preferred over `TZ`. It asserts that the "always show UTC" sentence and the l.860 sentence are gone. Plus a QA doc-check |
| AC-069 | Config examples, UI README | D: `TestTIME12H_AC069_ConfigExamplesAndUIReadme`; QA doc-check |
| AC-070 | No migration | QA: `git diff --name-only main...HEAD -- cmd/riot-server/migrations/` is empty |
| AC-071 | AD-008 cap | WF: `TIME-12H-001 [AC-071] the formatter cache never exceeds 12 entries` (1,000 calls per option, cycle and precision, plus 1,000 rejected zones); `TIME-12H-001 [AC-071] 20 distinct accepted zones still leave at most 12 entries` |
| AC-072 | AD-009 memo | WP: `TIME-12H-001 [AC-072] the clock tick does not re-render timestamp consumers` (fake timers, 5 s) |
| AC-073 | No dependency | QA: `git diff main...HEAD -- go.mod go.sum web/package.json web/package-lock.json` is empty |
| AC-074 | All suites | QA runs `go test ./...`, `npm run test:run`, `npx tsc --noEmit`, `npm run build` and `VITE_DEMO=true npm run build`; compares test counts to `main`; checks that every `[UIF AC-104]`, `[UIF AC-105]` and `[UIF AC-113]` to `[UIF AC-118]` describe still exists |

### 8.1 Supporting tests

| Topic | Test |
|---|---|
| OQ-001 layout guard | SG: `TestTIME12H_OQ001_NoTwentyFourHourHumanLayouts` and its self-test `TestTIME12H_OQ001_GuardCatchesForbiddenForms` |
| Storage key ownership | WG: `TIME-12H-001 [FR-047] only TimePreferenceProvider names the time storage keys` |
| Activity labels use the clock form | WG: `TIME-12H-001 [FR-062] activity labels do not use the chart tick formatter` |
| Provider and sync mounted | WG: `TIME-12H-001 [BR-005] the provider is mounted at the root and the zone sync in the authenticated app` |
| Old server compatibility (NFR-006, INFO-5) | WS: `TIME-12H-001 [NFR-006] a 404 from an older server leaves Server as UTC`; `TIME-12H-001 [NFR-006] an older server answering 200 text/html (SPA catch-all) leaves Server as UTC` |
| Focus-visible ring (NFR-008) | WH: `TIME-12H-001 [NFR-008] every panel control carries the shared focus ring` |
| Hour > 23 in calendar text | WCR: `TIME-12H-001 [FR-064] an out-of-range hour keeps the 24-hour text` |

### 8.2 Security condition tests and QA gates

| Condition | Tests / gate |
|---|---|
| C-1 (SEC-001) | DT: **`TestTIME12H_SEC001_HostileAbbreviationFallsBackToOffset`**. It builds locations with `time.LoadLocationFromTZData` whose abbreviations are `EDT\r\nX-Injected: 1`, `<!channel>`, `A B`, `ÉDT` (a non-ASCII letter), a 7-letter `ABCDEFG`, and a 200-byte string. Each must yield the offset form (`GMT-4`, etc.), and the label must be ≤ 9 bytes. A second subtest asserts that `EDT`, `EST`, `IST`, `UTC`, `GMT` and `BST` from real zones are unchanged. `TestTIME12H_AC063_ZoneLabelRules` stays green |
| C-2 (SEC-002) | C: the `format characters` and invalid-UTF-8 subtests of `TestTIME12H_AC046_InvalidValuesRejectedSanitizedOneWarning` |
| C-3 (SEC-003) | WG: `TIME-12H-001 [SEC-003] the AcceptedZone brand and the localTimeZone seam cannot be bypassed`, with its self-tests |
| C-4 gate 1 | Code review: `GET /api/v1/server/time-display` is registered inside the `adminAuth` group in `router.go`. Test: RT `TestTIME12H_AC050_TimeDisplayRequiresAdminSession` (no cookie, device key only, expired JWT, bad-signature JWT: all 401) and `TestTIME12H_AC050_TimeDisplayReportsZone` (exact body, `no-store`) |
| C-4 gate 2 | Code review: in `ResolveDisplayZone`, the length, `Local` and regex checks run before `time.LoadLocation`. Test: no captured record from AC-046/AC-048 contains `unknown time zone` or `invalid location name` |
| C-4 gate 3 | Code review: `ResolveDisplayZone` has exactly one non-test caller, `Server.Start`, and is not reachable from `setupRouter` or `applyTLSAndRestart`. Test: SG `TestTIME12H_C4_ResolveDisplayZoneCalledOnlyFromStart` (source scan: exactly one non-test call site, inside `server.go`'s `Start`), plus the AC-048 log counts |
| C-4 gate 4 | AC-054, AC-065 and AC-066 tests pass |
| C-4 gate 5 | AC-018, AC-020 and AC-071 tests pass, and the `[UIF SEC-005]` HTML-sink guard stays green |

---

## 9. Error Handling

| Failure | Behaviour | Log | User-visible |
|---|---|---|---|
| `RIOT_DISPLAY_TZ` or `TZ` is blank | Treated as unset | none | none |
| A value fails the shape check or `LoadLocation` | Next source, then UTC (FR-001, FR-006) | 1 Warn per invalid source, with `source` and the sanitised `value` (no Cc/Cf, valid UTF-8, ≤ 64 runes). The LoadLocation error text is never logged | Text in UTC; Settings → Logs shows the warning |
| Zone database missing on the host | The `time/tzdata` fallback (AD-002). If a name is unknown even there, as above | as above | as above |
| The zone database has a non-letter or over-long abbreviation | Offset-form label (C-1) | none | `GMT±H[:MM]` |
| A nil location reaches a helper | Treated as UTC; `t.In(nil)` is never called | none | UTC text |
| `/server/time-display` returns 401 | `fetchJSON` reloads the page (the session has expired), as for every admin read | none | Login screen |
| `/server/time-display` returns 404, 5xx, a network error or non-JSON (`200 text/html` from an older server) | The query errors after one retry; `serverZone` stays `null` | none (no console output beyond React Query's default) | Server shown as UTC, `Server (UTC)` |
| The API zone is rejected by shape or `Intl` | `null` | none | as above |
| `localStorage` access, `getItem` or `setItem` throws | Server / 12h on read; on write, the choice is kept for the session (FR-048) | none | none |
| Unparseable instant | `—` | none | `—` |
| `resolvedOptions().timeZone` unavailable | `localZoneName` = `''` | none | `Local` without a name |
| `summary.Render` template error | Unchanged (500) | unchanged | unchanged |

No new HTTP error shapes.

## 10. Security Considerations

| # | Vector | Mitigation |
|---|---|---|
| S-1 | Path traversal or file probing through the zone name (`../../etc/passwd`, `:/etc/localtime`, absolute paths) reaching `time.LoadLocation` | The shape check runs first and allows no `.`, no leading `/`, no `:` and no `\`, so `LoadLocation` only ever sees names shaped like `Area/City`. Go's own `LoadLocation` also rejects `..` and leading `/` or `\` (verified, review INFO-1). The error text, which echoes the input, is never logged |
| S-2 | Log injection or display spoofing through the rejected value | The value is made valid UTF-8, then every Cc **and** Cf rune is removed (control characters, bidi overrides and isolates, zero-width characters, BOM), then it is cut to 64 runes. It goes through slog JSON encoding and is logged at most once per source per start (C-2) |
| S-3 | Script or markup injection from the API zone into the DOM | The zone is shape-validated (letters, digits and `_+-/` only) and `Intl`-validated. It is rendered only as React text, in labels and in the `aria-label`/`title` attributes. There are no HTML sinks (the UIF SEC-005 guard) |
| S-4 | An unvalidated zone reaching `Intl` (exceptions, cache keyed by attacker strings) | The `AcceptedZone` brand **plus** the SEC-003 static guard: casts only in `utils/format.ts`, and `localTimeZone` confined to the provider and `utils/format.ts`, never a production JSX prop (C-3). Also the AC-020 runtime spy test and the AD-014 `Intl` placement guard |
| S-5 | Information disclosure | The zone name is returned only to an admin session (FR-011). Agent keys, anonymous callers and expired or forged sessions get 401. Its sensitivity is low: it reveals the operator's chosen zone |
| S-6 | Storage tampering | Both keys are checked against an enum whitelist, and storage never supplies a zone name (BR-006). A 100 KB value is compared, not parsed |
| S-7 | Header injection in `Content-Disposition` | The date comes from `Format("2006-01-02")`, so it contains only digits and `-`. Hostname sanitising is unchanged |
| S-8 | Content from the zone database reaching event messages, pushes and the Markdown summary | **Property:** the label is always either 1 to 6 ASCII letters, or `GMT` followed by an optional sign, digits and an optional `:MM` (at most 9 bytes), whatever bytes the zone database holds (C-1). Host and actor text still pass `SafeName`. Message growth is bounded at +12 bytes (AD-015) |
| S-9 | Denial of service | Inputs are capped at 64 characters. Both regexes are anchored with no nested quantifiers, so matching is linear. One `Intl` construction per API response. The cache is capped at 12 entries |
| S-10 | Process-wide side effects | `time.Local` is never assigned. Tests that change it restore it in `t.Cleanup` and do not run in parallel |

No new secret, permission or sudoers change.

## 11. Performance Considerations

- **Server:**
  - at most two `LoadLocation` calls, at start;
  - per patch-status request, at most 4 `t.In(loc)` plus `Format` calls per device, each sub-microsecond;
  - one per summary, and one per event message;
  - the C-1 letter check is at most 6 byte comparisons;
  - no caching is needed.
- **Dashboard:**
  - `formatTimestamp` costs the same as today: one cached `formatToParts`, and the 12-hour step is integer arithmetic;
  - the header clock formats once per second inside its own component (NFR-004);
  - the context value changes only when a preference or the server zone changes;
  - one extra GET per page load.
- No indexes, no query changes, and no rate limiting needed (the handler is admin-only and constant time).

## 12. Implementation Notes for Engineers

1. **Go environment on Ryan's machine.** Set `GOCACHE=D:\rIOt_gocache` and `GOTMPDIR=D:\rIOt_fuzz_tmp` (C: is nearly full). On Windows, use `go test ./...` without `-race`.
2. **Never call `t.In(nil)`.** It panics. Every helper maps nil to `time.UTC`.
3. **`RebootAt` must stay UTC.** In `computePatchBadge`, keep `at := nextStart` (UTC) for the pointer, and format a converted copy for the label. A `time.Time` in America/New_York marshals as `…-04:00` and would break AC-054 and AC-065.
4. **Go layouts.** Use `3:04 PM` (no leading zero, uppercase) for human text, never `03:04` or `15:04`. The OQ-001 guard fails on any `15:04` literal in `internal/server` or `internal/models`.
5. **`DisplayZoneLabel` (C-1).** Check the abbreviation byte by byte for `'A'..'Z' | 'a'..'z'` and a length of 1 to 6. Do not use `unicode.IsLetter`, which accepts non-ASCII letters. Build test locations for `TestTIME12H_SEC001_…` with `time.LoadLocationFromTZData`. A minimal TZif v1 blob (header, one type, the abbreviation bytes) can be assembled in the test. Do not commit binary fixtures.
6. **`sanitizeZoneValue` (C-2).** Order: `strings.ToValidUTF8(s, "\uFFFD")`, then drop runes where `unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r)`, then cut to 64 runes. Never log `err.Error()` from `LoadLocation`.
7. **Remove every `.Local()`** in `internal/models` and `internal/server`. The guard enforces it.
8. **Tests that set `time.Local`** (AC-049, AC-066):
   - save the old value and restore it in `t.Cleanup`;
   - never call `t.Parallel()`;
   - say why in a comment.

   Load named zones in tests with `time.LoadLocation`, and blank-import `time/tzdata` in those test files.
9. **Capturing logs** (AC-042 to AC-048). Pass a `slog.New(handler)` with a recording handler to `ResolveDisplayZone`; do not touch `slog.Default()`.
10. **One call site (C-4).** Call `ResolveDisplayZone` only from `Server.Start`. `setupRouter` reads `s.DisplayZone`. The SG scan asserts exactly one non-test call.
11. **The `summary.Render` signature change** breaks the 8 calls in `summary_test.go`; update them in the same commit. Also search for any mock or interface that wraps `Render` (there is none at 8a16f2a).
12. **Web: `Intl.DateTimeFormat` only in `utils/format.ts`.**
    - The acceptance check must construct with exactly `{ timeZone }` and nothing else.
    - Every formatting construction keeps `timeZoneName: 'short'`, so the AC-020 spy can tell the two apart.
13. **`AcceptedZone` (C-3).**
    - The only production cast is the `return raw as AcceptedZone` inside `acceptServerZone`.
    - Never cast elsewhere, and never pass an API or storage value as `localTimeZone`.
    - Tests may cast (the guard scans non-test source only).
14. **12-hour from `h23`.** Derive the hour and the period from the `hour` part with integer arithmetic; never read `dayPeriod`.
15. **Cache.** The cap of 12 is a hard limit with oldest-first eviction. Do not key the cache by hour cycle.
16. **`hourCycle` is required in `ZoneOpts`.**
    - Let `tsc` find the callers, and do not add a default in `ZoneOpts` itself.
    - The only defaults are the `formatRunDate` and `parseSystemdCalendar` parameters, which keep the pre-existing tests valid.
    - **Cron lower-casing:** replace `.toLowerCase()` at `cron.ts:114` with lower-casing of the first character only. Run the existing cron tests unmodified to prove it.
    - **The no-provider default stays UTC / 24h** (AD-009). Changing it would churn dozens of unrelated page tests.
17. **Panel and sync.**
    - Use native `input type="radio"`, with a per-instance `name` from `useId()`, inside `div role="radiogroup" aria-labelledby`.
    - Do not portal the panel. Put `scrollbar-thin` on the overflow container.
    - Use tokens (`bg-raised`, `border-border-strong`, `z-(--z-dropdown)`), not raw `gray-*` classes or hex.
    - Mount `ServerZoneSync` only inside the authenticated branch of `App.tsx`.
18. **Docs.**
    - In `docker-compose.yml`, the example goes in the `environment:` list. The compose file lists the environment explicitly, so a value only in `.env` never reaches the container.
    - The README says so, as it does for `RIOT_PPROF_ADDR`.
    - The README presents **`RIOT_DISPLAY_TZ` as the preferred setting**. It says `TZ` is honoured as a fallback but also changes the process zone, which shifts the offsets of server log timestamps (INFO-3).
    - The README must not say that powered-off or expected-offline messages are pushed (AD-016).
19. **Demo.** `getTimeDisplay` is a reader (its name starts with `get`), so `demo-parity.test.ts` checks it automatically. It needs no new mutation entry.
20. **Commit plan.** Format: `[TIME-12H-001] <type>: <what>`.
    1. `feat`: models display-time helpers (incl. C-1) + tests.
    2. `feat`: config resolution (incl. C-2), `time/tzdata`, server wiring + tests and guards.
    3. `feat`: patch badge, summary, filename, endpoint + tests (incl. the AD-013 Go updates).
    4. `feat`: expect messages and publisher zone + tests (incl. the harness and generator test updates).
    5. `feat`: web formatters and preference + tests (incl. the AD-013 web updates).
    6. `feat`: header clock panel and server-zone sync + tests.
    7. `feat`: call sites (activity, cron, DeviceDetail), demo + tests.
    8. `test`: static guards (Go, and web incl. SEC-003).
    9. `config`: `.env.example` and `docker-compose.yml`; then the README and UI README.
21. **PR body.** Include:
    - the FRD 7.6 inventory plus rows 31 to 37 above;
    - the decisions made on Ryan's behalf (DEC-1 to DEC-18, and R-1 to R-4);
    - the security conditions C-1 to C-4 and how each is met;
    - an instruction for the coordinator to add `RIOT_DISPLAY_TZ: America/New_York` to Hawaii's `/home/docker/riot` override (out of scope for this story).

## 13. Definition of Done

- [ ] All component changes in Section 4 implemented; nothing outside it touched.
- [ ] Every AC in Section 8 and every condition in Section 8.2 has its named test, and the names carry the AC or SEC IDs.
- [ ] The AD-013 test updates are done as specified: no deletion, no skip, no loosened matcher. Every `[UIF AC-104/105/113..118]` describe is still present.
- [ ] `go test ./...` is green (with `GOCACHE`/`GOTMPDIR` set on Windows), and `go vet ./...` is clean.
- [ ] In `web`: `npm run test:run`, `npx tsc --noEmit`, `npm run lint` (no new errors), `npm run build` and `VITE_DEMO=true npm run build` all pass.
- [ ] Go and web test counts are at least those on `main`.
- [ ] No file under `cmd/riot-server/migrations/` changed (AC-070).
- [ ] `go.mod`, `go.sum`, `web/package.json` and the lockfile are identical to `main` (AC-073).
- [ ] No `.Local()`, `time.Local` or `15:04` human layout in non-test code under `internal/server` or `internal/models` (guard green).
- [ ] No `Intl.DateTimeFormat` outside `utils/format.ts`, except the unchanged AgentManagement call. No `toLocale*` (UIF guard green). No `as AcceptedZone` outside `utils/format.ts`, and no production use of `localTimeZone` outside the provider and `utils/format.ts` (SEC-003 guard green).
- [ ] **C-4 gates verified by QA:**
  - the route is inside `adminAuth`, and AC-050 covers the expired and bad-signature JWTs, the exact body and `no-store`;
  - no log record contains `unknown time zone` or `invalid location name`;
  - `ResolveDisplayZone` is called once, from `Start` only;
  - AC-018, AC-020, AC-054, AC-065, AC-066 and AC-071 pass;
  - `[UIF SEC-005]` is green.
- [ ] Screenshot `docs/implementation/TIME-12H-001-screenshots/after-header-time-panel.png` captured from the demo build (AC-031).
- [ ] README, `.env.example`, `docker-compose.yml` and the UI README are updated, with `RIOT_DISPLAY_TZ` presented as preferred over `TZ` (AC-068 and AC-069 docs tests green).
- [ ] Implementation report at `docs/implementation/TIME-12H-001-impl-report.md`. No uncommitted changes. No tag, merge or deploy.

---

## Security addendum (review: APPROVED WITH CONDITIONS C-1 to C-4)

| Condition | Finding | Where it is met in this ADD |
|---|---|---|
| C-1 | SEC-001: Go returns zone abbreviations unconstrained, so a hostile zone database could inject into pushes and the Markdown summary, and break the growth bound | AD-003 (1 to 6 ASCII letters, otherwise the offset form; label at most 9 bytes), AD-015, S-8 restated as a property, test `TestTIME12H_SEC001_HostileAbbreviationFallsBackToOffset` |
| C-2 | SEC-002: `unicode.IsControl` keeps Cf runes (bidi overrides, zero-width), which spoof Settings → Logs | AD-001 `sanitizeZoneValue` (valid UTF-8, then strip Cc + Cf, then 64 runes), S-2, the AC-046 subtests |
| C-3 | SEC-003: a cast defeats the brand, and `localTimeZone` is an unbranded path into `Intl` | AD-007 (brand plus guard is the FR-039 enforcement), AD-014 SEC-003 guard with self-tests, S-4, note 13 |
| C-4 | QA gates | Section 8.2 gates 1 to 5, Section 13, the SG single-call-site scan |
| INFO-3 | Recommendation: `RIOT_DISPLAY_TZ` preferred over `TZ` | Section 4 README row, note 18, the AC-068 docs test |
| INFO-5 | Recommendation: an older server answers `200 text/html` | Section 8.1 NFR-006 case, Section 9 |

INFO-1, INFO-2, INFO-4 and INFO-6 to INFO-9 need no change: they confirm decisions already in revision 1.

## FRD Deviations and Clarifications

| ID | FRD item | Deviation or clarification | Reason |
|---|---|---|---|
| DV-1 | FR-029, AC-064 | AC-064 is verified on the held-summary push. The FR-026 and FR-027 texts are verified on the stored and broadcast events, which are not pushed today and stay unpushed | R-1, AD-016: powered-off and expected-offline events are never dispatched (EXPECT-001 AD-005) |
| DV-2 | FR-025, AC-058 | The browser download name (`DeviceDetail.tsx`) also uses the display-zone date. The server header changes as written | R-2, AD-012: the browser ignores `Content-Disposition` |
| DV-3 | FR-029a | A numeric abbreviation with a zero offset gives `GMT`, not `GMT+0` | R-3: matches the browser's `en-US` form (the intent of DEC-7) |
| DV-4 | FR-029a, DEC-7 | An abbreviation that is not 1 to 6 ASCII letters also gives the offset form. No real tzdata abbreviation in use is affected | C-1 (SEC-001) |
| DV-5 | FR-004 | The rejected value has Cf runes removed as well as control characters, and is made valid UTF-8 first. This is stricter than, and satisfies, "control characters removed" | C-2 (SEC-002) |
| DV-6 | FR-070 | The README l.860 sentence is also replaced, and `RIOT_DISPLAY_TZ` is presented as preferred over `TZ` | R-4, INFO-3 |
| DV-7 | FR-002, FR-008 | The effective name is the trimmed source value as `LoadLocation` accepted it, with no canonicalisation (`US/Eastern` stays `US/Eastern`) | `Location.String()` returns the name given to `LoadLocation` |
| DV-8 | AC-050 "every pre-existing field keeps its name, type and value" | The endpoint is new, so this check is made on `GET /api/v1/auth/check` instead | AD-006 adds a new read rather than a field |
| DV-9 | 7.6 inventory | Rows 31 to 37 added (OQ-001) | Section 0 |

## Residuals

- **RS-1:** Ryan's existing Hawaii deployment shows UTC in 12-hour form until the coordinator adds `RIOT_DISPLAY_TZ: America/New_York` to the compose override (FRD OQ-002).
- **RS-2:** If the dashboard cannot read the server zone, the saved summary filename uses the UTC date while the file's `Generated` line uses the display zone. This corrects itself on the next successful load.
- **RS-3:** For non-US zones the server label and the browser label can differ (for example `BST` versus `GMT+1`), as accepted in DEC-7.
- **RS-4:** Event messages stored before the upgrade keep their old unlabelled 24-hour text until retention removes them (DEC-17).
- **RS-5:** The `time/tzdata` copy embedded in the binary is frozen at the Go toolchain version. It is used only where no system database exists. A real-world rule change reaches such hosts only with a rebuild (review INFO-6; correctness, not security).
- **RS-6:** On Windows with `ZONEINFO` set to a directory, a name with a valid shape such as `CON` is opened inside that directory (review INFO-1). Production runs in a Linux container, and `ZONEINFO` is set by the operator.

## Open points for Ryan

None of these block implementation. They go in the PR body:
1. Powered-off and expected-offline messages are not ntfy pushes today; only the held-escalation summary is. This story labels their times on the dashboard; it does not start pushing them. Pushing them would be a separate EXPECT story, if Ryan wants it.
2. Hawaii needs `RIOT_DISPLAY_TZ: America/New_York` in `/home/docker/riot` after this ships. That is a coordinator action, outside this story.

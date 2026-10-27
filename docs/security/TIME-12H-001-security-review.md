# Security Review Report
**Story ID:** TIME-12H-001
**Reviewer:** Security Researcher Agent
**Date:** 2026-10-27
**Verdict:** APPROVED WITH CONDITIONS

Inputs:
- `docs/requirements/TIME-12H-001-frd.md` (FINAL, working tree on `feat/time-12h-001`)
- `docs/architecture/TIME-12H-001-add.md` (DRAFT revision 1, working tree on `feat/time-12h-001`)
- Code at `origin/main` 8a16f2a:
  - `internal/server/config.go`, `server.go` (`Start`, the log handler and log budget), `router.go` (the `adminAuth`
    group, the `/api/v1/auth` group, the SPA catch-all)
  - `internal/server/middleware/admin_auth.go`, `sameorigin.go`, `cors.go`, `security_headers.go`
  - `internal/server/handlers/auth.go` (`AuthCheck`, session cookie), `patch_badge.go`, `auto_update.go`
    (maintenance-window math), `handlers.go` (`DeviceSummary`)
  - `internal/server/summary/summary.go`, `internal/models/expect.go`, `internal/server/expect/messages.go`,
    `internal/server/notify/ntfy.go`, `smtp.go` (where event text goes)
  - `internal/server/partition/plan.go`, `manager.go`, `internal/server/quota/daily.go`, the migrations (column types)
  - `web/src/utils/format.ts`, `web/src/contexts/TimePreferenceProvider.tsx`, `web/src/components/HeaderClock.tsx`,
    `web/src/api/client.ts` (`fetchJSON`), `web/src/pages/settings/LogSettings.tsx` (how stored WARN attrs render),
    `web/src/test/static-guards.test.ts`

Empirical checks: two throwaway Go programs (Go 1.26.0, `time/tzdata` imported) outside the repository:
- `time.LoadLocation` against the ADD's shape regex, for traversal, absolute, POSIX `:`-prefixed, NUL, bidi, `Local`,
  empty and zone-directory non-zone names;
- `time.LoadLocationFromTZData` with hostile TZif abbreviations.

Results are cited in INFO-1 and SEC-001. No code, FRD or ADD was modified. No production system was queried.

---

## Threat Model Summary

### Assets
- **Integrity of server-written text.** Event messages, the held-escalation push, the patch badge and the device
  summary now carry a zone label. Event messages go to the dashboard and, for the held summary, to every configured
  notification channel (ntfy, SMTP, Slack, Discord, Telegram, Gotify, Pushover, webhook).
- **Integrity of UTC-critical math.** Partition boundaries, quota day keys, retention, maintenance-window start and end
  (which decide when hosts patch and reboot), and every RFC 3339 API field must not move with the display zone or the
  process zone.
- **The server's filesystem** as reached through `time.LoadLocation` from an environment value.
- **The admin dashboard DOM**, which renders an API-supplied zone name and a stored preference.
- **Logs and the DB log store** (Settings → Logs), which receive a rejected environment value.
- **Bounded growth** (Ryan's standing rule): logs per start, event message length, formatter cache, browser storage.

### Threat Actors
- **Unauthenticated network attacker.** Can reach the new route but has no session. The route must return 401.
- **Device agent holding a valid `X-rIOt-Key`.** Must not read the zone (FR-011). It has no other new input in this
  story: no agent-supplied value reaches the new code.
- **Authenticated admin (single role).** Reads the zone. Can tamper with its own `localStorage`. It is not a meaningful
  attacker against itself.
- **Cross-origin web page in the admin's browser.** Can trigger GETs with the `SameSite=Lax` cookie only on top-level
  navigation, and cannot read responses (CORS reflects only configured origins).
- **Whoever controls the server's environment or image.** Sets `RIOT_DISPLAY_TZ`, `TZ` and `ZONEINFO`, and supplies the
  zone database. This is the operator, or a supply-chain compromise of the image's `tzdata` package or of the Go
  toolchain's embedded copy. This actor already has broad control, so findings against it are defense in depth.
- **Future developer (regression).** Casts an unvalidated string to the branded zone type, reads `time.Local`, or adds
  a 24-hour human layout.

### Attack Surface Introduced
- `GET /api/v1/server/time-display` (admin session), returning one configuration string.
- Two environment values (`RIOT_DISPLAY_TZ`, `TZ`) parsed at start and passed to `time.LoadLocation`.
- One WARN line per invalid source, stored in the DB log store and shown in Settings → Logs.
- Zone abbreviations from the zone database, copied into event messages, push bodies, the patch badge and the summary.
- An API-supplied string passed to `Intl.DateTimeFormat` after validation.
- A new `localStorage` key (`riot-hour-cycle`) and three values for the existing key.
- A header panel with two radio groups. It renders the zone name as text and in `aria-label`/`title`.
- `time/tzdata` embedded in the server binary (standard library; no new dependency).

---

## Findings

There are no CRITICAL, HIGH or MEDIUM findings.

The two focus questions come out well:
- **Traversal through the zone name is blocked twice before any file is opened** (INFO-1). The ADD's shape check
  allows no `.`, `:`, `\` or leading `/`. Go's own `LoadLocation` also rejects `..`, a leading `/` and a leading `\`.
- **The endpoint sits behind the vetted admin chain** (INFO-2). It exposes one low-sensitivity string and is not
  cacheable.

The three LOW findings are defense in depth. Each has a cheap, local resolution.

### LOW

#### SEC-001: The server zone label is copied verbatim from the zone database, which Go does not constrain
**Severity:** LOW
**Domain:** Output integrity / notification content injection / bounded growth
**Location:** ADD AD-003 (`DisplayZoneLabel`: "If `name` is non-empty and does not start with `+` or `-`, return
`name`"), Section 10 S-8 ("The label comes from the zone database (ASCII)"), AD-015 (event message growth "+12 bytes")

**Description:**
- S-8 assumes the abbreviation is short ASCII letters. Go's TZif parser enforces none of that: it reads the
  abbreviation bytes up to the NUL and returns them as the zone name.
- Verified with `time.LoadLocationFromTZData`. Each of these abbreviations came back unchanged from `Zone()` and from
  `Format("3:04 PM MST")`:
  - `EDT\r\nX-Injected: 1`;
  - `<!channel> @everyone`;
  - `<img src=x onerror=alert(1)>`;
  - `\x1b[2J`.
- Under the ADD the label reaches:
  - **stored event messages**, rendered as React text (safe);
  - **the held-escalation push** to every channel type. A Slack `<!channel>` or a Discord/Telegram mention or markdown
    link is interpreted by the receiving service. ntfy and SMTP put the message in the body only (no header sink;
    `notify/ntfy.go` sets `Title` from the rule name).
  - **the Markdown summary** `Generated` line. A link or image there renders when Ryan opens the file in a Markdown
    viewer.
  - **patch badge text**, rendered as React text (safe).
- The AD-015 bound ("+12 bytes per new event") holds only for real tzdata. A hostile abbreviation of any length grows
  every affected event row and push by that length.

**Attack Scenario:**
1. A compromised `tzdata` package lands in the image at the next rebuild (supply chain), or someone with container
   access sets `ZONEINFO` to a directory they control. `ZONEINFO` is searched before the system database and the
   embedded copy.
2. `America/New_York` in that database carries the abbreviation `<!channel> see https://evil.example/x`.
3. The next held-escalation summary posts `... were held since 1:05 PM <!channel> see https://evil.example/x; see
   Alerts` to Ryan's Slack channel. It pings everyone and plants a link in a trusted alert stream. Every affected event
   row also grows by the abbreviation's length.

The precondition already implies broad control, so this is LOW. It is still the only path from this story into
notification content, and closing it costs one predicate.

**Required Resolution (properties):**
- (a) `DisplayZoneLabel` returns the zone database's abbreviation only when it consists solely of ASCII letters and is
  at most 6 characters long. Anything else (non-letters, longer, numeric, empty) yields the offset-derived form
  (`GMT`, `GMT-3`, `GMT+5:45`) that AD-003 already defines. The label is therefore always either letters (≤ 6) or
  `GMT` plus a sign, digits and `:` (≤ 9 bytes), so the AD-015 growth bound holds whatever the zone database says.
- (b) A DT test builds locations with `time.LoadLocationFromTZData` whose abbreviations contain CR/LF, `<`, a space, a
  non-ASCII letter and a 200-byte string. It asserts the offset form. A second case asserts every real abbreviation
  in the AC-063 set (`EDT`, `EST`, `IST`, `UTC`, `GMT`, `BST`) is unchanged.
- (c) The architect corrects S-8 so it states the property, not the assumption.

**Blocks:** No. Condition C-1.

---

#### SEC-002: The rejected-value sanitizer keeps Unicode format characters (bidi overrides, zero-width), which spoof the Settings → Logs view
**Severity:** LOW
**Domain:** Log integrity
**Location:** ADD AD-001 step 4 and `sanitizeZoneValue` ("drop every rune for which `unicode.IsControl` is true");
FRD FR-004; `web/src/pages/settings/LogSettings.tsx:116` (attrs rendered as `JSON.stringify(v)` text)

**Description:**
- `unicode.IsControl` covers category Cc only. It keeps Cf: U+202A..U+202E, U+2066..U+2069 (bidi embeddings,
  overrides and isolates), U+200B..U+200F, U+FEFF.
- Verified: the specified sanitizer turns `a\nb\r\x1b[31mred‮RLO​ZW…` into `ab[31mred‮RLO​ZW…`.
  The bidi override and the zero-width space survive.
- slog's JSON handler escapes neither. `JSON.stringify` in the log viewer escapes neither. They reach the DOM as text,
  where the browser applies them.
- The output is still text (no XSS), but the displayed value can be made to read differently from the bytes logged.
- The EXPECT-002 parser already rejects Cf and bidi characters. This sanitizer should match that stricter precedent.

**Attack Scenario:**
1. A value is placed in the compose environment, for example through a shared `.env` that someone other than Ryan can
   edit, or a pasted snippet. The value is `America/New_York‮...` with a right-to-left override and trailing
   text.
2. It fails the shape check (correct), and the WARN is stored.
3. Settings → Logs shows the value with its tail visually reversed or hidden by zero-width characters. Ryan sees what
   looks like a valid zone and cannot tell why it was rejected. In a crafted case the display reads as a different
   message.

The environment is operator-controlled, so this is LOW. It matters because Settings → Logs is where AD-001 says Ryan
looks.

**Required Resolution (properties):**
- (a) The logged `value` contains no rune of category Cc **or** Cf. It is still cut to at most 64 runes after removal,
  and an invalid UTF-8 byte never reaches the log raw.
- (b) AC-046's control-character subtest gains a value containing U+202E, U+2066 and U+200B. It asserts none appears
  in the captured `value`.
- (c) FR-004's wording ("control characters removed") is satisfied and strengthened. No FRD change is needed. The
  architect notes the stricter rule in AD-001.

**Blocks:** No. Condition C-2.

---

#### SEC-003: The `AcceptedZone` brand is a compile-time convention that a cast defeats, and `localTimeZone` is an unbranded path to `Intl`
**Severity:** LOW
**Domain:** Input validation (FR-039 / BR-006 enforcement)
**Location:** ADD AD-007 (`AcceptedZone`, "TypeScript rejects an unvalidated string at compile time"), AD-008
(`ZoneOpts.localTimeZone?: string`), AD-014 web guards

**Description:**
- FR-039 says no API- or storage-sourced string reaches `Intl` as a zone unless it passed FR-038. The ADD enforces
  this three ways:
  - the brand;
  - the AC-020 runtime spy, which covers only the paths its test exercises;
  - a static guard that keeps `Intl.DateTimeFormat(` inside `utils/format.ts`.
- None of them stops a later call site from:
  - writing `serverTimeZone: data.display_zone as AcceptedZone` (or `as unknown as AcceptedZone`);
  - passing an API or storage value as `localTimeZone`, which is typed plain `string`. It is a test seam that
    production never sets today.

  Both compile, both keep the static guard green, and both put an unvalidated string into `Intl`.
- The impact of an unvalidated zone in `Intl` is small: a `RangeError` that the formatter must survive, or text
  rendered through React. That is why this is LOW. But FR-039 is listed under NFR-002 [Security], and the ADD presents
  the brand as the enforcement.

**Attack Scenario (regression):**
1. A later story adds a "pin a zone" feature and stores a zone name. To "reuse the plumbing" it passes it as
   `localTimeZone`, or casts it to `AcceptedZone`.
2. `tsc` and every guard pass. A tampered `localStorage` entry such as a 100 KB string or `Mars/Olympus` now reaches
   `new Intl.DateTimeFormat` on every render.
3. Result: thrown `RangeError`s in render paths that rely on "nothing throws" (FR-035). The formatter cache also
   becomes keyed by attacker-chosen strings (`L:<anything>`), which only the cap of 12 bounds.

**Required Resolution (properties):**
- (a) A web static guard (additive describe in `static-guards.test.ts`) asserts that in non-test source a type
  assertion to `AcceptedZone` appears only in `utils/format.ts`. It includes a self-test that the pattern catches both
  `as AcceptedZone` and `as unknown as AcceptedZone`.
- (b) The same guard asserts that in non-test source the `localTimeZone` prop/field is set only by forwarding the
  provider's own prop (no JSX `localTimeZone=` in production source, and no production module other than the
  provider and `utils/format.ts` names `localTimeZone`).
- (c) The ADD's AD-007 text states that the brand plus guard (a) is the enforcement, not the brand alone.

**Blocks:** No. Condition C-3.

---

### INFORMATIONAL

#### INFO-1: Zone-name path traversal is blocked before the filesystem, verified
- The ADD's order is trim, length ≤ 64, `!= "Local"`, regex, then `LoadLocation`. The regex is anchored, has no
  nested quantifiers, and admits only `[A-Za-z0-9_+-]` segments that start with a letter, joined by `/`, at most 3.
  It runs before `LoadLocation`.
- Empirical results (Go 1.26.0):

  | Value | Shape | `LoadLocation` |
  |---|---|---|
  | `../../etc/passwd` | rejected | `time: invalid location name` (Go's own `..` check) |
  | `/etc/passwd` | rejected | `time: invalid location name` (leading `/`) |
  | `\host\share` | rejected | `time: invalid location name` (leading `\`) |
  | `America/../America/New_York` | rejected | `time: invalid location name` |
  | `:/etc/localtime` | rejected | `unknown time zone` (no file opened outside the zone directory) |
  | `America/New_York;rm -rf /` | rejected | `unknown time zone` |
  | `America/New_York\x00`, `a‮b` | rejected | `unknown time zone …` (the error text echoes the raw input) |
  | `etc/passwd`, `leapseconds`, `CON`, `NUL` | **accepted** | `unknown time zone` (looked up *inside* the zone directory or zip only) |
  | `Local` | rejected (explicit) | Go returns **`time.Local`**, the process zone, with no error |
  | `""` | unset | Go returns UTC |
  | `Factory`, `Etc/GMT+5`, `Antarctica/Troll`, `Asia/Kathmandu` | accepted | loads; abbreviations `-00`, `-05`, `+00`, `+0545` (numeric, so the offset form applies: `GMT`, `GMT-5`, `GMT`, `GMT+5:45`) |

- The explicit `Local` rejection matters. Without it, `RIOT_DISPLAY_TZ=Local` would silently adopt the process zone,
  which FR-007 forbids.
- The ADD never logs `LoadLocation`'s error. That matters because the error echoes the raw input, including NUL and
  bidi characters.
- Residual edge, out of scope: on Windows with `ZONEINFO` set to a **directory**, a shape-valid DOS device name such
  as `CON` is opened inside that directory. Production is a Linux container. On Windows there is no system zone
  directory, and `ZONEINFO` is operator-set.

#### INFO-2: The endpoint's access control, CSRF posture and caching are correct
- **Authentication.** The route is inside the existing `r.Group(... r.Use(adminAuth) ...)`. `adminAuth` is
  `AdminAuthSameOrigin`:
  - the origin check, which passes safe methods by design;
  - then the `riot_session` HMAC JWT check, which ignores `X-rIOt-Key`.

  So an anonymous caller or a device key gets 401 (FR-011).
- **Cross-origin reads.** `SameSite=Lax` means a cross-site page cannot attach the cookie to a `fetch`. `CORS` reflects
  only configured `RIOT_ALLOWED_ORIGINS`.
- **Side effects.** The response is a JSON object, not an array, and the handler has no side effects, so CSRF is not
  applicable.
- **Caching.** `Cache-Control: no-store` keeps it out of shared caches.
- **Disclosure.** The zone name is low sensitivity. The same admin can already infer it from the `EDT` labels in
  server text.
- **Rate limiting.** None needed: the handler is admin-only and constant time.
- Keeping `GET /api/v1/auth/check` untouched (AD-006) is the right call. That handler re-implements JWT parsing
  outside the middleware and is public.

#### INFO-3: Setting `TZ` changes the process zone; today's UTC-critical math is immune, verified
- Go's runtime derives `time.Local` from `TZ` regardless of this story. The ADD never assigns `time.Local` and bans
  reading it in server text (AD-014 guard).
- Verified at 8a16f2a:
  - `inMaintenanceWindow`, `maintenanceWindowEnd` and `nextMaintenanceWindowStart` call `.UTC()` before reading
    `Hour`, `Weekday` or `Day`. `frequencyAllowsDay` receives the UTC value.
  - `partition/plan.go` and `quota/daily.go` normalise with `.UTC()`.
  - The partition SQL literal is RFC 3339 UTC.
  - Every time column is `TIMESTAMPTZ`; there is no naive `timestamp` column for pgx to wall-clock-encode.
  - `TZ` is not `PGTZ`, so the Postgres session zone is unaffected.
- AC-066 locks this in for the paths it names.
- The visible side effect of `TZ` is that slog JSON timestamps carry the local offset. Recommendation (no condition):
  the README presents `RIOT_DISPLAY_TZ` as the preferred knob and says `TZ` also changes the process zone.

#### INFO-4: Browser handling of the API zone, storage and the panel is sound
- **`acceptServerZone`.** It checks `typeof`, then length ≤ 64, then `!= 'Local'`, then the anchored regex (linear),
  then `Intl` construction in a `try` with only `{ timeZone }`. It does not trim and caches nothing, so rejected
  strings cause no growth. AC-018's list includes `<img src=x>`, a newline, `../etc/passwd` and a 65-character value.
- **Formatter cache.** It is a `Map`, so there is no prototype-key hazard. Keys carry an `S:`/`L:` prefix. There is a
  hard cap of 12 with oldest-first eviction.
- **Storage.** Both keys are compared to fixed enums and never parsed. A tampered or 100 KB value resolves to the
  default. The marker rule can only choose among enum values, and storage never supplies a zone name (BR-006).
- **Panel.** The zone name renders as React text and in `aria-label`/`title`. Ids come from `useId`. The panel is not
  portalled. The existing `[UIF SEC-005]` guard bans HTML sinks in all non-test source.
- **`ServerZoneSync`.** It is mounted only in the authenticated branch, so `fetchJSON`'s 401 reload cannot loop from
  the login screen. `staleTime: Infinity` means no refetch storm.

#### INFO-5: An older server answers the new path with `200 text/html`, not `404`
- The SPA catch-all (`router.go:389-405`) serves `index.html` for any unmatched path. `res.json()` then throws, the
  query errors, and Server formats as UTC. The design is safe.
- The NFR-006 test simulates a 404, which is not what a real older server returns. Suggestion for QA (no condition):
  add a `200` HTML case to `ServerZoneSync.test.tsx`.
- The embedded dashboard always ships with its own server, so this only arises with a dev proxy.

#### INFO-6: `time/tzdata` embedding
- It is standard library, so `go.mod` does not change (AC-073).
- Lookup order is `ZONEINFO`, then system directories, then `$GOROOT` zip, then the embedded copy. The image's
  `apk tzdata` therefore still wins in production.
- The embedded copy is frozen at the Go toolchain version, so it can lag a real-world rule change on hosts that rely
  on it. That is a correctness concern, not a security one.
- The supply-chain trust is the Go toolchain's, which the binary already depends on. SEC-001 bounds what any zone
  database can inject.

#### INFO-7: Header and filename sinks
- The server `Content-Disposition` date comes from `Format("2006-01-02")` (digits and `-`). The browser `a.download`
  date comes from `Intl` numeric parts. Hostname sanitising is unchanged on both sides.
- ntfy `Title` is the rule name, not the event message. SMTP puts the message in the body after the headers, and Go's
  `smtp` dot-stuffs it. None of the new strings reaches a header.

#### INFO-8: Bounded growth holds, subject to SEC-001
| Thing | Bound | Note |
|---|---|---|
| Logs | ≤ 1 Info + ≤ 2 Warn per process start | holds only if `ResolveDisplayZone` runs exactly once per process: in `Start`, not in `setupRouter`, which `applyTLSAndRestart` re-runs (C-4) |
| DB log store | ≤ 2 WARN rows per start | existing retention and the 1000-entry log budget |
| Event message | +≤ 12 bytes per affected event | requires SEC-001 (a); unbounded with a hostile zone database otherwise |
| Formatter cache | ≤ 12 entries | rejected zones never become keys |
| Browser storage | 2 keys, values ≤ 6 characters | overwritten in place |
| Server state | one `*time.Location` in memory | no DB, no file, no migration |

#### INFO-9: UTC preservation in API fields
- The one trap is AD-005's `b.RebootAt = &at`. If it pointed at a converted copy, it would serialise `-04:00`. The ADD
  calls this out (Section 12 note 3), and AC-054/AC-065 assert `Z`.
- All other API time fields are untouched by this story.

---

## Positive Observations
- **Validation before the filesystem, and never `Local`.** The shape check makes `LoadLocation` see only `Area/City`
  names, and the explicit `Local` rejection closes the one input where Go silently returns the process zone.
- **No process-wide state.** `time.Local` is never assigned. The zone flows by injection (`HandlerDeps`,
  `expect.Options`, `OfflineCoverage.Zone`). A Go static guard bans `.Local()` and `time.Local` in server and model
  code.
- **The error text that echoes raw input is never logged.** The logged value is sanitized and capped.
- **The env value is validated in `Start` after slog is configured** (the HARDEN-002 AD-002 precedent), so a rejected
  value is visible in Settings → Logs instead of lost on stderr.
- **Admin-only exposure through the vetted middleware chain**, with the public `auth/check` left alone.
- **Double validation in the browser** (shape and `Intl`), no trimming, no caching of rejected values, and a branded
  type. SEC-003 makes the brand enforceable.
- **Enum-only storage** with a marker rule that needs no third key and no `removeItem`.
- **UTC math untouched and tested against a hostile process zone** (AC-066 with `Pacific/Kiritimati`).
- **No new dependency, migration, secret, permission or sudoers change.**

---

## Verdict Rationale

**APPROVED WITH CONDITIONS.** There are no CRITICAL, HIGH or MEDIUM findings.
- Path traversal through the zone name is blocked by the ADD's shape check and again by Go.
- The new endpoint is admin-only, side-effect free and not cacheable.
- UTC-critical math is verified immune to both the display zone and the process zone.

The conditions are small, local and non-architectural, following the project's precedent (EXPECT-002) of carrying
LOW findings as conditions:
- constrain the zone label so the zone database cannot inject into notifications or break the growth bound (SEC-001);
- strip format characters from the logged value (SEC-002);
- make the `AcceptedZone` brand enforceable by a guard (SEC-003).

`senior_dev` may start. The architect folds the three text corrections into the ADD (AD-001, AD-003, AD-007, S-8),
in parallel or as part of revision 2. No FRD change is needed.

### Conditions (architect amends the ADD where stated; senior_dev implements; QA verifies)
1. **C-1 (SEC-001). AD-003 and Section 10 S-8:**
   - `DisplayZoneLabel` uses the zone database's abbreviation only when it is 1 to 6 ASCII letters. Otherwise it uses
     the AD-003 offset form.
   - DT test `TestTIME12H_SEC001_HostileAbbreviationFallsBackToOffset` builds locations with
     `time.LoadLocationFromTZData`. Their abbreviations contain CR/LF, `<`, a space, a non-ASCII letter and a 200-byte
     string, and each must yield the offset form. The AC-063 real abbreviations stay unchanged.
   - S-8 states the property instead of "ASCII".
2. **C-2 (SEC-002). AD-001 `sanitizeZoneValue`:**
   - Remove every rune of category Cc **or** Cf, then cut to 64 runes. Invalid UTF-8 never reaches the log raw.
   - AC-046 gains a subtest with U+202E, U+2066 and U+200B that asserts none survives in the captured `value`.
3. **C-3 (SEC-003). AD-007 and AD-014 (web guards):**
   - A static guard asserts that type assertions to `AcceptedZone` occur in non-test source only in `utils/format.ts`,
     with a self-test for `as AcceptedZone` and `as unknown as AcceptedZone`.
   - The same guard asserts that `localTimeZone` is named in non-test source only by `TimePreferenceProvider.tsx` and
     `utils/format.ts`, and is never set as a JSX prop in production source.
   - AD-007 states that brand plus guard is the FR-039 enforcement.
4. **C-4 (QA gate: properties the ADD states that this review cannot see in code yet):**
   - `GET /api/v1/server/time-display` is registered inside the `adminAuth` group. AC-050 covers no cookie (401),
     `X-rIOt-Key` only (401), an expired or bad-signature JWT (401), and a valid session (200, exact body,
     `Cache-Control: no-store`).
   - In `ResolveDisplayZone`, the length, `Local` and regex checks run before `time.LoadLocation` (code review). No
     captured log record from AC-046/AC-048 contains `unknown time zone` or `invalid location name`.
   - `ResolveDisplayZone` is called exactly once per process, from `Start`. It is not reachable from `setupRouter` or
     `applyTLSAndRestart` (code review plus the AC-048 counts).
   - AC-054, AC-065 and AC-066 pass: every RFC 3339 field in the patch-status response ends in `Z`, and partition,
     quota and maintenance-window results are identical under `Pacific/Kiritimati`.
   - AC-018, AC-020 and AC-071 pass, and the `[UIF SEC-005]` HTML-sink guard stays green.

### For Ryan's decision
None. All conditions are implementation-level hardening with no product trade-off.

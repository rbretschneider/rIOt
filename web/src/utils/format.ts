/** Human-readable byte rate. Picks the unit so values stay in [0, 1024). */
export function formatBytesPerSec(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B/s'
  if (bytes < 1024) return `${bytes.toFixed(0)} B/s`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB/s`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB/s`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB/s`
}

/** Compact byte rate for chart axis ticks ("3K", "1.2M") — no unit suffix, just magnitude. */
export function formatBytesPerSecAxis(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 1) return '0'
  if (bytes < 1024) return bytes.toFixed(0)
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)}K`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)}M`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)}G`
}

/** Percent value without trailing decimals when the integer form is unambiguous.
 * 100.0 → "100%", 99.95 → "100%", 0.7 → "0.7%", 12.34 → "12%". */
export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '—'
  if (value >= 10) return `${Math.round(value)}%`
  return `${value.toFixed(1)}%`
}

/** Plain integer count, never decimals. */
export function formatCount(value: number): string {
  if (!Number.isFinite(value)) return '—'
  return Math.round(value).toString()
}

// ---------------------------------------------------------------------------
// UI-FOUNDATION-001: canonical byte, duration and time formatters.
//
// Everything below is pure. Time formatters take the zone preference and the
// current time as arguments and never read the clock or the environment, so
// they are deterministic under test. The time-zone preference itself lives in
// contexts/TimePreferenceProvider.
// ---------------------------------------------------------------------------

export type TimeZonePref = 'Server' | 'UTC' | 'Local'

/** TIME-12H-001: 12-hour ("1:05 PM") or 24-hour ("13:05") clock text. */
export type HourCycle = '12h' | '24h'

/**
 * A server zone name that passed acceptServerZone (FR-038). The brand makes an
 * unvalidated string a compile error; the only cast to it is inside
 * acceptServerZone, and a static guard (TIME-12H-001 SEC-003) fails on any other.
 */
export type AcceptedZone = string & { readonly __brand: 'AcceptedZone' }

export interface ZoneOpts {
  zone: TimeZonePref
  /** Required, so tsc finds every caller that has not chosen an hour cycle. */
  hourCycle: HourCycle
  /** IANA zone used for the Local preference. Undefined means the runtime zone. Test seam. */
  localTimeZone?: string
  /** The server display zone, once the API reported it and acceptServerZone accepted it. */
  serverTimeZone?: AcceptedZone | null
}

/** `clock` is the hour and minute with a zone label ("1:05 PM EDT"), used for activity text. */
export type TimestampPrecision = 'second' | 'minute' | 'time' | 'clock'

export const EM_DASH = '—'

const KB = 1024
const MB = KB * 1024
const GB = MB * 1024
const TB = GB * 1024

/** Human-readable byte size (base 1024). Non-finite or negative input renders "0 B". */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B'
  if (bytes < KB) return `${bytes} B`
  if (bytes < MB) return `${(bytes / KB).toFixed(1)} KB`
  if (bytes < GB) return `${(bytes / MB).toFixed(1)} MB`
  if (bytes < TB) return `${(bytes / GB).toFixed(1)} GB`
  return `${(bytes / TB).toFixed(1)} TB`
}

/**
 * Compact duration from seconds (may be fractional): 350ms, 42s, 10m, 1h 5m, 1d 3h.
 * Null, undefined, negative and non-finite input render an em dash.
 */
export function formatDuration(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds) || seconds < 0) return EM_DASH
  if (seconds < 1) return `${Math.round(seconds * 1000)}ms`
  if (seconds < 60) return `${Math.floor(seconds)}s`
  const totalMinutes = Math.floor(seconds / 60)
  if (totalMinutes < 60) return `${totalMinutes}m`
  const totalHours = Math.floor(totalMinutes / 60)
  if (totalHours < 24) return `${totalHours}h ${totalMinutes % 60}m`
  return `${Math.floor(totalHours / 24)}d ${totalHours % 24}h`
}

export type InstantInput = string | number | Date | null | undefined

/**
 * Parses an ISO string, epoch-millisecond number or Date. Returns null when the
 * value is missing or unparseable. Digit-only strings are deliberately not
 * interpreted (utils/cron.ts parseTimestamp owns unix-second strings).
 */
export function parseInstant(value: InstantInput): Date | null {
  if (value == null) return null
  if (value instanceof Date) return Number.isNaN(value.getTime()) ? null : value
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) return null
    const d = new Date(value)
    return Number.isNaN(d.getTime()) ? null : d
  }
  const trimmed = value.trim()
  if (trimmed === '' || /^\d+$/.test(trimmed)) return null
  const d = new Date(trimmed)
  return Number.isNaN(d.getTime()) ? null : d
}

/**
 * Relative time against an injected clock: "just now" within 60 s either way,
 * then "5m ago" / "3h ago" / "2d ago" (floor) or "in 10m" / "in 3h" / "in 2d".
 * `pastOnly` clamps a future instant to age zero ("just now"). Use it for ages
 * such as last-seen, where a future value only means an agent clock is ahead of
 * the browser's, so the UI never renders a negative or future age.
 */
export function formatRelative(value: InstantInput, nowMs: number, opts: { pastOnly?: boolean } = {}): string {
  const d = parseInstant(value)
  if (!d) return EM_DASH
  const rawSec = (nowMs - d.getTime()) / 1000
  const diffSec = opts.pastOnly ? Math.max(0, rawSec) : rawSec
  const abs = Math.abs(diffSec)
  if (abs < 60) return 'just now'
  const minutes = Math.floor(abs / 60)
  let unit: string
  if (minutes < 60) unit = `${minutes}m`
  else if (minutes < 60 * 24) unit = `${Math.floor(minutes / 60)}h`
  else unit = `${Math.floor(minutes / (60 * 24))}d`
  return diffSec >= 0 ? `${unit} ago` : `in ${unit}`
}

// ---------------------------------------------------------------------------
// TIME-12H-001: zone resolution, 12-hour text and server-zone acceptance.
// ---------------------------------------------------------------------------

/** Shape of an IANA zone name: 1 to 3 "/"-separated segments of [A-Za-z][A-Za-z0-9_+-]*. */
export const ZONE_NAME_PATTERN = /^[A-Za-z][A-Za-z0-9_+-]*(\/[A-Za-z][A-Za-z0-9_+-]*){0,2}$/

const ZONE_NAME_MAX_LENGTH = 64

/**
 * Accepts a zone name from the API (or any untrusted source) only when it has the
 * IANA shape (no trimming, not "Local", at most 64 characters) and the browser's
 * Intl accepts it. Anything else returns null, which callers treat as UTC (FR-038).
 * The acceptance construction passes only `{ timeZone }` so tests can tell it apart
 * from formatting constructions, and nothing is cached, so rejected strings cannot grow memory.
 */
export function acceptServerZone(raw: unknown): AcceptedZone | null {
  if (typeof raw !== 'string') return null
  if (raw.length === 0 || raw.length > ZONE_NAME_MAX_LENGTH) return null
  if (raw === 'Local') return null
  if (!ZONE_NAME_PATTERN.test(raw)) return null
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: raw })
  } catch {
    return null
  }
  return raw as AcceptedZone
}

/**
 * The browser's own zone name for display ("America/Los_Angeles"), or '' when the
 * runtime cannot say. `localTimeZone` is the test seam and wins when given.
 */
export function localZoneName(localTimeZone?: string): string {
  if (localTimeZone !== undefined) return localTimeZone
  try {
    return new Intl.DateTimeFormat().resolvedOptions().timeZone ?? ''
  } catch {
    return ''
  }
}

interface ResolvedZone {
  key: string
  timeZone: string | undefined
  literalUtc: boolean
}

function resolveZone(opts: ZoneOpts): ResolvedZone {
  if (opts.zone === 'Local') {
    return { key: `L:${opts.localTimeZone ?? '<runtime>'}`, timeZone: opts.localTimeZone, literalUtc: false }
  }
  if (opts.zone === 'Server' && opts.serverTimeZone) {
    return { key: `S:${opts.serverTimeZone}`, timeZone: opts.serverTimeZone, literalUtc: false }
  }
  // UTC, and Server before the zone is known or when it was rejected (FR-050).
  return { key: 'UTC', timeZone: 'UTC', literalUtc: true }
}

/** Hard cap on cached formatters: typically 3 (UTC, one server zone, Local). Oldest is evicted first. */
const FORMATTER_CACHE_LIMIT = 12
const formatterCache = new Map<string, Intl.DateTimeFormat>()

/** Number of cached Intl formatters, for the cache-bound test. */
export function formatterCacheSize(): number {
  return formatterCache.size
}

function buildFormatter(timeZone: string | undefined): Intl.DateTimeFormat {
  return new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
    timeZoneName: 'short',
  })
}

function dateTimeFormat(zone: ResolvedZone): Intl.DateTimeFormat {
  let fmt = formatterCache.get(zone.key)
  if (!fmt) {
    try {
      fmt = buildFormatter(zone.timeZone)
    } catch {
      // A zone Intl rejects (only reachable through the Local test seam) renders as UTC, never throws (FR-035).
      fmt = buildFormatter('UTC')
    }
    if (formatterCache.size >= FORMATTER_CACHE_LIMIT) {
      const oldest = formatterCache.keys().next().value
      if (oldest !== undefined) formatterCache.delete(oldest)
    }
    formatterCache.set(zone.key, fmt)
  }
  return fmt
}

interface ZonedParts {
  year: string
  month: string
  day: string
  hour: string
  minute: string
  second: string
  zone: string
}

function zonedParts(d: Date, opts: ZoneOpts): ZonedParts {
  const resolved = resolveZone(opts)
  const parts: Record<string, string> = {}
  for (const p of dateTimeFormat(resolved).formatToParts(d)) parts[p.type] = p.value
  return {
    year: parts.year,
    month: parts.month,
    day: parts.day,
    hour: parts.hour,
    minute: parts.minute,
    second: parts.second,
    zone: resolved.literalUtc ? 'UTC' : parts.timeZoneName,
  }
}

/**
 * Hour text for the cycle. 24-hour is the zero-padded h23 hour, exactly as before.
 * 12-hour is derived from that hour by integer arithmetic (never from dayPeriod):
 * no leading zero, midnight and noon are 12, and the period is a separate suffix.
 */
function hourText(p: ZonedParts, cycle: HourCycle): { hour: string; period: string } {
  if (cycle === '24h') return { hour: p.hour, period: '' }
  const h = Number(p.hour) % 24
  const h12 = h % 12 === 0 ? 12 : h % 12
  return { hour: String(h12), period: h < 12 ? ' AM' : ' PM' }
}

/**
 * Absolute timestamp in the preferred zone, always zone-labelled. 24-hour:
 * second "YYYY-MM-DD HH:mm:ss ZONE", minute "YYYY-MM-DD HH:mm ZONE", time
 * "HH:mm:ss ZONE", clock "HH:mm ZONE". 12-hour: "YYYY-MM-DD h:mm:ss PM ZONE",
 * "YYYY-MM-DD h:mm PM ZONE", "h:mm:ss PM ZONE", "h:mm PM ZONE". Missing or
 * unparseable input renders an em dash.
 */
export function formatTimestamp(
  value: InstantInput,
  opts: ZoneOpts,
  precision: TimestampPrecision = 'second',
): string {
  const d = parseInstant(value)
  if (!d) return EM_DASH
  const p = zonedParts(d, opts)
  const { hour, period } = hourText(p, opts.hourCycle)
  const date = `${p.year}-${p.month}-${p.day}`
  if (precision === 'clock') return `${hour}:${p.minute}${period} ${p.zone}`
  if (precision === 'time') return `${hour}:${p.minute}:${p.second}${period} ${p.zone}`
  if (precision === 'minute') return `${date} ${hour}:${p.minute}${period} ${p.zone}`
  return `${date} ${hour}:${p.minute}:${p.second}${period} ${p.zone}`
}

/** The date (YYYY-MM-DD) of the instant in the preferred zone, no label. Used for download file names. */
export function formatDateOnly(value: InstantInput, opts: ZoneOpts): string {
  const d = parseInstant(value)
  if (!d) return EM_DASH
  const p = zonedParts(d, opts)
  return `${p.year}-${p.month}-${p.day}`
}

/** Chart X-axis tick: "HH:mm" or "h:mm AM" in the preferred zone, no zone label (the tooltip carries it). */
export function formatChartTick(ts: number, opts: ZoneOpts): string {
  const d = parseInstant(ts)
  if (!d) return EM_DASH
  const p = zonedParts(d, opts)
  const { hour, period } = hourText(p, opts.hourCycle)
  return `${hour}:${p.minute}${period}`
}

/** Chart hover label: the full second-precision timestamp including the zone label. */
export function formatChartTooltip(ts: number, opts: ZoneOpts): string {
  return formatTimestamp(ts, opts, 'second')
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

/**
 * Formats an ISO instant as an absolute UTC string (YYYY-MM-DD HH:MM UTC).
 * UTC getters only — never converts to local time. Returns null for an
 * unparseable input so callers can fall back honestly instead of rendering a
 * partial or placeholder time. Maintenance-window and
 * reboot-date strings ignore the time-zone preference.
 */
export function formatUtc(iso: string): string | null {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  return (
    `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())} ` +
    `${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())} UTC`
  )
}

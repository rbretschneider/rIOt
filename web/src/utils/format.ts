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

export type TimeZonePref = 'UTC' | 'Local'

export interface ZoneOpts {
  zone: TimeZonePref
  /** IANA zone used for the Local preference. Undefined means the runtime zone. Test seam. */
  localTimeZone?: string
}

export type TimestampPrecision = 'second' | 'minute' | 'time'

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
 */
export function formatRelative(value: InstantInput, nowMs: number): string {
  const d = parseInstant(value)
  if (!d) return EM_DASH
  const diffSec = (nowMs - d.getTime()) / 1000
  const abs = Math.abs(diffSec)
  if (abs < 60) return 'just now'
  const minutes = Math.floor(abs / 60)
  let unit: string
  if (minutes < 60) unit = `${minutes}m`
  else if (minutes < 60 * 24) unit = `${Math.floor(minutes / 60)}h`
  else unit = `${Math.floor(minutes / (60 * 24))}d`
  return diffSec >= 0 ? `${unit} ago` : `in ${unit}`
}

const formatterCache = new Map<string, Intl.DateTimeFormat>()

function dateTimeFormat(opts: ZoneOpts): Intl.DateTimeFormat {
  const key = opts.zone === 'UTC' ? 'UTC' : (opts.localTimeZone ?? '<runtime>')
  let fmt = formatterCache.get(key)
  if (!fmt) {
    fmt = new Intl.DateTimeFormat('en-US', {
      timeZone: opts.zone === 'UTC' ? 'UTC' : opts.localTimeZone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hourCycle: 'h23',
      timeZoneName: 'short',
    })
    formatterCache.set(key, fmt)
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
  const parts: Record<string, string> = {}
  for (const p of dateTimeFormat(opts).formatToParts(d)) parts[p.type] = p.value
  return {
    year: parts.year,
    month: parts.month,
    day: parts.day,
    hour: parts.hour,
    minute: parts.minute,
    second: parts.second,
    zone: opts.zone === 'UTC' ? 'UTC' : parts.timeZoneName,
  }
}

/**
 * Absolute timestamp in the preferred zone, 24-hour, always zone-labelled:
 * second "YYYY-MM-DD HH:mm:ss ZONE", minute "YYYY-MM-DD HH:mm ZONE",
 * time "HH:mm:ss ZONE". Missing or unparseable input renders an em dash.
 */
export function formatTimestamp(
  value: InstantInput,
  opts: ZoneOpts,
  precision: TimestampPrecision = 'second',
): string {
  const d = parseInstant(value)
  if (!d) return EM_DASH
  const p = zonedParts(d, opts)
  const date = `${p.year}-${p.month}-${p.day}`
  if (precision === 'time') return `${p.hour}:${p.minute}:${p.second} ${p.zone}`
  if (precision === 'minute') return `${date} ${p.hour}:${p.minute} ${p.zone}`
  return `${date} ${p.hour}:${p.minute}:${p.second} ${p.zone}`
}

/** Chart X-axis tick: HH:mm in the preferred zone, no zone label (the tooltip carries it). */
export function formatChartTick(ts: number, opts: ZoneOpts): string {
  const d = parseInstant(ts)
  if (!d) return EM_DASH
  const p = zonedParts(d, opts)
  return `${p.hour}:${p.minute}`
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
 * partial or placeholder time. Moved verbatim from utils/rebootEta.ts; reboot
 * ETA and maintenance-window strings ignore the time-zone preference.
 */
export function formatUtc(iso: string): string | null {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  return (
    `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())} ` +
    `${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())} UTC`
  )
}

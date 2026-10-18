import { describe, it, expect } from 'vitest'
import {
  formatBytes,
  formatBytesPerSec,
  formatBytesPerSecAxis,
  formatChartTick,
  formatChartTooltip,
  formatCount,
  formatDuration,
  formatPercent,
  formatRelative,
  formatTimestamp,
  formatUtc,
  parseInstant,
} from './format'

describe('formatBytesPerSec', () => {
  it('formats sub-KB as bytes', () => {
    expect(formatBytesPerSec(0)).toBe('0 B/s')
    expect(formatBytesPerSec(512)).toBe('512 B/s')
  })

  it('formats KB range', () => {
    expect(formatBytesPerSec(1024)).toBe('1.0 KB/s')
    expect(formatBytesPerSec(35938)).toBe('35.1 KB/s')
  })

  it('formats MB range', () => {
    expect(formatBytesPerSec(2 * 1024 * 1024)).toBe('2.0 MB/s')
  })

  it('formats GB range', () => {
    expect(formatBytesPerSec(3 * 1024 * 1024 * 1024)).toBe('3.00 GB/s')
  })

  it('handles invalid input', () => {
    expect(formatBytesPerSec(NaN)).toBe('0 B/s')
    expect(formatBytesPerSec(-1)).toBe('0 B/s')
  })
})

describe('formatBytesPerSecAxis', () => {
  it('drops sub-1 to "0"', () => {
    expect(formatBytesPerSecAxis(0)).toBe('0')
    expect(formatBytesPerSecAxis(0.4)).toBe('0')
  })

  it('uses K suffix for KB range', () => {
    expect(formatBytesPerSecAxis(2048)).toBe('2K')
    expect(formatBytesPerSecAxis(35938)).toBe('35K')
  })

  it('uses M suffix for MB range', () => {
    expect(formatBytesPerSecAxis(2 * 1024 * 1024)).toBe('2.0M')
  })
})

describe('formatPercent', () => {
  it('drops decimals at >= 10', () => {
    expect(formatPercent(100)).toBe('100%')
    expect(formatPercent(99.95)).toBe('100%')
    expect(formatPercent(12.34)).toBe('12%')
  })

  it('keeps one decimal under 10', () => {
    expect(formatPercent(0.7)).toBe('0.7%')
    expect(formatPercent(8.0)).toBe('8.0%')
  })

  it('handles invalid input', () => {
    expect(formatPercent(NaN)).toBe('—')
  })
})

describe('formatCount', () => {
  it('rounds to integer', () => {
    expect(formatCount(0)).toBe('0')
    expect(formatCount(0.0)).toBe('0')
    expect(formatCount(3.7)).toBe('4')
  })

  it('handles invalid input', () => {
    expect(formatCount(NaN)).toBe('—')
  })
})

const UTC = { zone: 'UTC' } as const
const LA = { zone: 'Local', localTimeZone: 'America/Los_Angeles' } as const
const INSTANT = '2026-10-17T14:03:22Z'

describe('[UIF AC-101] formatBytes', () => {
  it.each([
    [0, '0 B'],
    [1023, '1023 B'],
    [1024, '1.0 KB'],
    [1536, '1.5 KB'],
    [1048576, '1.0 MB'],
    [1073741824, '1.0 GB'],
    [1099511627776, '1.0 TB'],
    [-5, '0 B'],
    [NaN, '0 B'],
  ])('formatBytes(%s) is %s', (input, expected) => {
    expect(formatBytes(input)).toBe(expected)
  })

  it('treats Infinity as invalid input', () => {
    expect(formatBytes(Infinity)).toBe('0 B')
  })
})

describe('[UIF AC-102] formatDuration', () => {
  it.each([
    [0.35, '350ms'],
    [42, '42s'],
    [600, '10m'],
    [3900, '1h 5m'],
    [97200, '1d 3h'],
    [-1, '—'],
    [NaN, '—'],
  ])('formatDuration(%s) is %s', (input, expected) => {
    expect(formatDuration(input)).toBe(expected)
  })

  it('renders null, undefined and Infinity as an em dash', () => {
    expect(formatDuration(null)).toBe('—')
    expect(formatDuration(undefined)).toBe('—')
    expect(formatDuration(Infinity)).toBe('—')
  })

  it('renders zero as 0ms and the 1 s, 60 s, 1 h and 1 d boundaries in the next unit', () => {
    expect(formatDuration(0)).toBe('0ms')
    expect(formatDuration(1)).toBe('1s')
    expect(formatDuration(59.9)).toBe('59s')
    expect(formatDuration(60)).toBe('1m')
    expect(formatDuration(3599)).toBe('59m')
    expect(formatDuration(3600)).toBe('1h 0m')
    expect(formatDuration(86399)).toBe('23h 59m')
    expect(formatDuration(86400)).toBe('1d 0h')
  })
})

describe('[UIF AC-103] formatRelative', () => {
  const NOW = Date.parse('2026-10-17T12:00:00Z')
  const ago = (seconds: number) => new Date(NOW - seconds * 1000).toISOString()
  const ahead = (seconds: number) => new Date(NOW + seconds * 1000).toISOString()

  it.each([
    [ago(30), 'just now'],
    [ago(5 * 60), '5m ago'],
    [ago(3 * 3600), '3h ago'],
    [ago(2 * 86400), '2d ago'],
    [ahead(10 * 60), 'in 10m'],
  ])('%s relative to a fixed now is %s', (instant, expected) => {
    expect(formatRelative(instant, NOW)).toBe(expected)
  })

  it('treats a future instant under 60 s as just now (DEV-08)', () => {
    expect(formatRelative(ahead(45), NOW)).toBe('just now')
  })

  it('switches units at exactly 60 s, 60 min and 24 h', () => {
    expect(formatRelative(ago(59), NOW)).toBe('just now')
    expect(formatRelative(ago(60), NOW)).toBe('1m ago')
    expect(formatRelative(ago(59 * 60 + 59), NOW)).toBe('59m ago')
    expect(formatRelative(ago(3600), NOW)).toBe('1h ago')
    expect(formatRelative(ago(86400 - 1), NOW)).toBe('23h ago')
    expect(formatRelative(ago(86400), NOW)).toBe('1d ago')
  })

  it('formats future hours and days', () => {
    expect(formatRelative(ahead(3 * 3600), NOW)).toBe('in 3h')
    expect(formatRelative(ahead(2 * 86400), NOW)).toBe('in 2d')
  })

  it('accepts epoch milliseconds and Date objects', () => {
    expect(formatRelative(NOW - 5 * 60_000, NOW)).toBe('5m ago')
    expect(formatRelative(new Date(NOW - 5 * 60_000), NOW)).toBe('5m ago')
  })

  it('renders an em dash for unparseable input', () => {
    expect(formatRelative('nope', NOW)).toBe('—')
  })
})

describe('[UIF AC-104] absolute timestamp UTC', () => {
  it('formats second precision as YYYY-MM-DD HH:mm:ss UTC', () => {
    expect(formatTimestamp(INSTANT, UTC)).toBe('2026-10-17 14:03:22 UTC')
  })

  it('formats minute precision as YYYY-MM-DD HH:mm UTC', () => {
    expect(formatTimestamp(INSTANT, UTC, 'minute')).toBe('2026-10-17 14:03 UTC')
  })

  it('formats time precision as HH:mm:ss UTC', () => {
    expect(formatTimestamp(INSTANT, UTC, 'time')).toBe('14:03:22 UTC')
  })

  it('uses 24-hour time, rendering midnight as 00 and not 24', () => {
    expect(formatTimestamp('2026-10-17T00:05:09Z', UTC)).toBe('2026-10-17 00:05:09 UTC')
  })

  it('accepts epoch milliseconds and Date objects', () => {
    const ms = Date.parse(INSTANT)
    expect(formatTimestamp(ms, UTC)).toBe('2026-10-17 14:03:22 UTC')
    expect(formatTimestamp(new Date(ms), UTC)).toBe('2026-10-17 14:03:22 UTC')
  })
})

describe('[UIF AC-105] absolute timestamp Local (America/Los_Angeles)', () => {
  it('formats the instant in Pacific daylight time with the PDT label', () => {
    expect(formatTimestamp(INSTANT, LA)).toBe('2026-10-17 07:03:22 PDT')
  })

  it('formats winter dates in Pacific standard time', () => {
    expect(formatTimestamp('2026-01-15T14:03:22Z', LA)).toBe('2026-01-15 06:03:22 PST')
  })

  it('formats minute and time precision in the local zone', () => {
    expect(formatTimestamp(INSTANT, LA, 'minute')).toBe('2026-10-17 07:03 PDT')
    expect(formatTimestamp(INSTANT, LA, 'time')).toBe('07:03:22 PDT')
  })

  it('rolls the date back across midnight', () => {
    expect(formatTimestamp('2026-10-17T03:00:00Z', LA)).toBe('2026-10-16 20:00:00 PDT')
  })

  it('keeps Local and UTC formatters independent (cached per zone)', () => {
    expect(formatTimestamp(INSTANT, LA)).toBe('2026-10-17 07:03:22 PDT')
    expect(formatTimestamp(INSTANT, UTC)).toBe('2026-10-17 14:03:22 UTC')
  })
})

describe('[UIF AC-106] invalid timestamp', () => {
  it.each([['not-a-date'], [undefined], [''], [null], ['   '], [NaN], [new Date('x')]])(
    'formatTimestamp(%s) is an em dash',
    input => {
      const out = formatTimestamp(input as string | undefined, UTC)
      expect(out).toBe('—')
      expect(out).not.toContain('Invalid Date')
    },
  )

  it('does not interpret digit-only strings (unix seconds belong to utils/cron)', () => {
    expect(parseInstant('1710456000')).toBeNull()
  })

  it('parseInstant returns a Date for ISO strings, numbers and Dates', () => {
    expect(parseInstant(INSTANT)?.toISOString()).toBe('2026-10-17T14:03:22.000Z')
    expect(parseInstant(0)?.toISOString()).toBe('1970-01-01T00:00:00.000Z')
    const d = new Date(INSTANT)
    expect(parseInstant(d)).toBe(d)
  })
})

describe('[UIF AC-118] chart tick and tooltip formatters', () => {
  const ts = Date.parse(INSTANT)

  it('ticks show the local time of day without a zone label', () => {
    expect(formatChartTick(ts, LA)).toBe('07:03')
  })

  it('ticks show UTC time of day under the UTC preference', () => {
    expect(formatChartTick(ts, UTC)).toBe('14:03')
  })

  it('tooltips end with the local zone label', () => {
    expect(formatChartTooltip(ts, LA)).toBe('2026-10-17 07:03:22 PDT')
  })

  it('tooltips end with UTC under the UTC preference', () => {
    expect(formatChartTooltip(ts, UTC)).toBe('2026-10-17 14:03:22 UTC')
  })

  it('renders an em dash for a non-finite tick', () => {
    expect(formatChartTick(NaN, UTC)).toBe('—')
  })
})

describe('[UIF AC-107] formatUtc is the canonical UTC-only formatter', () => {
  it('keeps the exact YYYY-MM-DD HH:MM UTC output', () => {
    expect(formatUtc('2026-10-17T14:03:00Z')).toBe('2026-10-17 14:03 UTC')
  })

  it('returns null for an unparseable instant', () => {
    expect(formatUtc('not-a-date')).toBeNull()
  })
})

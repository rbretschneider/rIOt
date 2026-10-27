import { describe, it, expect } from 'vitest'
import { parseTimestamp, formatRunDate, parseSystemdCalendar } from './cron'
import { acceptServerZone } from './format'

describe('parseTimestamp', () => {
  it('returns null for empty/undefined/zero', () => {
    expect(parseTimestamp(undefined)).toBeNull()
    expect(parseTimestamp('')).toBeNull()
    expect(parseTimestamp('0')).toBeNull()
  })

  it('parses Unix timestamp in seconds', () => {
    const d = parseTimestamp('1710456000')
    expect(d).toBeInstanceOf(Date)
    expect(d!.getFullYear()).toBe(2024)
  })

  it('parses Unix timestamp in milliseconds', () => {
    const d = parseTimestamp('1710456000000')
    expect(d).toBeInstanceOf(Date)
    expect(d!.getFullYear()).toBe(2024)
  })

  it('parses ISO date string', () => {
    const d = parseTimestamp('2024-03-15T06:00:00Z')
    expect(d).toBeInstanceOf(Date)
    expect(d!.getUTCFullYear()).toBe(2024)
    expect(d!.getUTCMonth()).toBe(2) // March
  })

  it('returns null for garbage input', () => {
    expect(parseTimestamp('not-a-date')).toBeNull()
  })
})

describe('formatRunDate', () => {
  it('returns dash for undefined', () => {
    expect(formatRunDate(undefined)).toBe('-')
  })

  it('returns dash for zero', () => {
    expect(formatRunDate('0')).toBe('-')
  })

  it('returns custom fallback', () => {
    expect(formatRunDate(undefined, 'N/A')).toBe('N/A')
  })

  it('returns formatted string for valid Unix timestamp', () => {
    const result = formatRunDate('1710456000')
    expect(result).not.toBe('-')
    expect(result).not.toBe('Invalid Date')
    expect(result.length).toBeGreaterThan(5)
  })

  it('returns formatted string for ISO date', () => {
    const result = formatRunDate('2024-03-15T06:00:00Z')
    expect(result).not.toBe('-')
    expect(result).not.toBe('Invalid Date')
  })
})

describe('[UIF AC-109] formatRunDate carries a zone label', () => {
  it('renders a unix-seconds timestamp at minute precision in UTC by default', () => {
    expect(formatRunDate('1710456000')).toBe('2024-03-14 22:40 UTC')
  })

  it('renders an ISO date at minute precision in UTC by default', () => {
    expect(formatRunDate('2024-03-15T06:00:00Z')).toBe('2024-03-15 06:00 UTC')
  })

  it('renders in the local zone when the Local preference is passed', () => {
    expect(formatRunDate('2024-03-15T06:00:00Z', '-', { zone: 'Local', hourCycle: '24h', localTimeZone: 'America/Los_Angeles' })).toBe(
      '2024-03-14 23:00 PDT',
    )
  })
})

describe('parseSystemdCalendar', () => {
  it('returns null for empty/undefined', () => {
    expect(parseSystemdCalendar(undefined)).toBeNull()
    expect(parseSystemdCalendar('')).toBeNull()
  })

  it('parses daily at midnight', () => {
    expect(parseSystemdCalendar('*-*-* 00:00:00')).toBe('Daily at midnight')
  })

  it('parses daily at specific time', () => {
    expect(parseSystemdCalendar('*-*-* 06:00:00')).toBe('Daily at 06:00')
  })

  it('parses daily at multiple hours', () => {
    expect(parseSystemdCalendar('*-*-* 00,12:00:00')).toBe('Daily at 00:00, 12:00')
    expect(parseSystemdCalendar('*-*-* 06,18:00:00')).toBe('Daily at 06:00, 18:00')
  })

  it('parses every N minutes', () => {
    expect(parseSystemdCalendar('*-*-* *:00/10:00')).toBe('Every 10 minutes')
    expect(parseSystemdCalendar('*-*-* *:*/5:00')).toBe('Every 5 minutes')
  })

  it('parses every hour', () => {
    expect(parseSystemdCalendar('*-*-* *:00:00')).toBe('Every hour')
  })

  it('parses every hour at specific minute', () => {
    expect(parseSystemdCalendar('*-*-* *:07:00')).toBe('Every hour at :07')
  })

  it('parses day of week', () => {
    const result = parseSystemdCalendar('Sun *-*-* 03:10:00')
    expect(result).toBe('Every Sunday at 03:10')
  })

  it('parses Monday at midnight', () => {
    expect(parseSystemdCalendar('Mon *-*-* 00:00:00')).toBe('Every Monday at midnight')
  })

  it('parses daily at specific minute', () => {
    expect(parseSystemdCalendar('*-*-* 00:07:00')).toBe('Daily at 00:07')
  })

  it('parses monthly', () => {
    const result = parseSystemdCalendar('*-*-01 00:00:00')
    expect(result).toBe('Monthly on day 01 at midnight')
  })

  it('parses yearly', () => {
    const result = parseSystemdCalendar('*-01-01 00:00:00')
    expect(result).toBe('Yearly on Jan 01 at midnight')
  })

  it('returns null for unparseable expressions', () => {
    expect(parseSystemdCalendar('garbage')).toBeNull()
  })

  it('parses hour steps', () => {
    const result = parseSystemdCalendar('*-*-* */6:00:00')
    expect(result).toBe('Every 6 hours')
  })
})
// TIME-12H-001: systemd calendar text follows the hour cycle with no zone conversion and no zone label.
// Rows are FRD 7.5. The monthly row uses day "1" (the FRD table prints "day 1"); the pre-existing
// "*-*-01" case above still reads "day 01" because the day text is echoed verbatim and is unchanged.
const CALENDAR_ROWS: Array<[string, string, string]> = [
  ['*-*-* 00:00:00', 'Daily at midnight', 'Daily at midnight'],
  ['*-*-* 06:00:00', 'Daily at 06:00', 'Daily at 6:00 AM'],
  ['*-*-* 12:00:00', 'Daily at 12:00', 'Daily at 12:00 PM'],
  ['*-*-* 00:07:00', 'Daily at 00:07', 'Daily at 12:07 AM'],
  ['*-*-* 06,18:00:00', 'Daily at 06:00, 18:00', 'Daily at 6:00 AM, 6:00 PM'],
  ['*-*-* 00,12:00:00', 'Daily at 00:00, 12:00', 'Daily at 12:00 AM, 12:00 PM'],
  ['Sun *-*-* 03:10:00', 'Every Sunday at 03:10', 'Every Sunday at 3:10 AM'],
  ['Mon *-*-* 00:00:00', 'Every Monday at midnight', 'Every Monday at midnight'],
  ['*-*-1 00:00:00', 'Monthly on day 1 at midnight', 'Monthly on day 1 at midnight'],
  ['*-*-* *:00/10:00', 'Every 10 minutes', 'Every 10 minutes'],
  ['*-*-* *:00:00', 'Every hour', 'Every hour'],
]

describe('TIME-12H-001 [AC-037] systemd calendar text in 12-hour form', () => {
  it.each(CALENDAR_ROWS)('%s reads as the 12-hour column', (expression, _twentyFour, twelve) => {
    expect(parseSystemdCalendar(expression, '12h')).toBe(twelve)
  })

  it('keeps AM and PM upper case after a day-of-week prefix', () => {
    expect(parseSystemdCalendar('Sun *-*-* 03:10:00', '12h')).toBe('Every Sunday at 3:10 AM')
    expect(parseSystemdCalendar('Sat *-*-* 18:30:00', '12h')).toBe('Every Saturday at 6:30 PM')
  })

  it('keeps AM and PM upper case after a date prefix', () => {
    expect(parseSystemdCalendar('*-01-01 06:00:00', '12h')).toBe('Yearly on Jan 01 at 6:00 AM')
    expect(parseSystemdCalendar('*-*-15 23:45:00', '12h')).toBe('Monthly on day 15 at 11:45 PM')
  })

  it('still writes midnight in words for a single 00:00 hour after a prefix', () => {
    expect(parseSystemdCalendar('*-01-01 00:00:00', '12h')).toBe('Yearly on Jan 01 at midnight')
  })

  it('leaves the non-clock phrases untouched', () => {
    expect(parseSystemdCalendar('*-*-* */6:00:00', '12h')).toBe('Every 6 hours')
    expect(parseSystemdCalendar('*-*-* *:07:00', '12h')).toBe('Every hour at :07')
  })
})

describe('TIME-12H-001 [AC-038] systemd calendar text in 24-hour form', () => {
  it.each(CALENDAR_ROWS)('%s reads as the 24-hour column', (expression, twentyFour) => {
    expect(parseSystemdCalendar(expression, '24h')).toBe(twentyFour)
  })

  it.each(CALENDAR_ROWS)('%s reads the same without an hour-cycle argument (the 24-hour default)', (expression, twentyFour) => {
    expect(parseSystemdCalendar(expression)).toBe(twentyFour)
  })
})

describe('TIME-12H-001 [FR-064] an out-of-range hour keeps the 24-hour text', () => {
  it('does not invent an AM/PM for hour 25', () => {
    expect(parseSystemdCalendar('*-*-* 25:00:00', '12h')).toBe(parseSystemdCalendar('*-*-* 25:00:00', '24h'))
    expect(parseSystemdCalendar('*-*-* 25:00:00', '12h')).not.toMatch(/AM|PM/)
  })

  it('keeps the whole list in 24-hour text when any hour is out of range', () => {
    expect(parseSystemdCalendar('*-*-* 06,30:00:00', '12h')).toBe('Daily at 06:00, 30:00')
  })
})

describe('TIME-12H-001 [AC-036] formatRunDate follows the hour cycle', () => {
  const server = { zone: 'Server', hourCycle: '12h', serverTimeZone: acceptServerZone('America/New_York') } as const

  it('renders an ISO instant in the 12-hour minute form', () => {
    expect(formatRunDate('2026-10-27T17:05:09Z', '-', server)).toBe('2026-10-27 1:05 PM EDT')
  })

  it('renders a unix-seconds string in the 12-hour minute form', () => {
    expect(formatRunDate('1793120709', '-', server)).toBe('2026-10-27 1:05 PM EDT')
  })

  it('renders the 24-hour minute form when asked', () => {
    expect(formatRunDate('2026-10-27T17:05:09Z', '-', { ...server, hourCycle: '24h' })).toBe('2026-10-27 13:05 EDT')
  })

  it('keeps the 24-hour UTC default for callers that pass no options', () => {
    expect(formatRunDate('2026-10-27T17:05:09Z')).toBe('2026-10-27 17:05 UTC')
  })
})

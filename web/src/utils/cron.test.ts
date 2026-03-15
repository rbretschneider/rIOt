import { describe, it, expect } from 'vitest'
import { parseTimestamp, formatRunDate, parseSystemdCalendar } from './cron'

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

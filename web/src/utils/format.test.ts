import { describe, it, expect } from 'vitest'
import { formatBytesPerSec, formatBytesPerSecAxis, formatPercent, formatCount, formatAgo } from './format'

describe('formatAgo', () => {
  const now = Date.parse('2026-10-17T12:00:00Z')
  const ago = (ms: number) => new Date(now - ms).toISOString()

  it('[AC-035] renders a future timestamp as zero age with no minus sign', () => {
    const future = new Date(now + 5 * 60_000).toISOString()
    const text = formatAgo(future, now)
    expect(text).toBe('0s ago')
    expect(text).not.toContain('-')
  })

  it('[AC-035] renders a far-future timestamp (one year ahead) as zero age', () => {
    const future = new Date(now + 365 * 86400_000).toISOString()
    expect(formatAgo(future, now)).toBe('0s ago')
  })

  it('[AC-035] a timestamp equal to now is zero age', () => {
    expect(formatAgo(new Date(now).toISOString(), now)).toBe('0s ago')
  })

  it('returns Never when there is no timestamp', () => {
    expect(formatAgo(undefined, now)).toBe('Never')
    expect(formatAgo('', now)).toBe('Never')
  })

  it('formats seconds, minutes, hours and days', () => {
    expect(formatAgo(ago(30_000), now)).toBe('30s ago')
    expect(formatAgo(ago(5 * 60_000), now)).toBe('5m ago')
    expect(formatAgo(ago(3 * 3600_000), now)).toBe('3h ago')
    expect(formatAgo(ago(2 * 86400_000), now)).toBe('2d ago')
  })

  it('switches unit exactly at the boundaries', () => {
    expect(formatAgo(ago(59_999), now)).toBe('59s ago')
    expect(formatAgo(ago(60_000), now)).toBe('1m ago')
    expect(formatAgo(ago(3600_000), now)).toBe('1h ago')
    expect(formatAgo(ago(86400_000), now)).toBe('1d ago')
  })
})

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

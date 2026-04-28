import { describe, it, expect } from 'vitest'
import { formatBytesPerSec, formatBytesPerSecAxis, formatPercent, formatCount } from './format'

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

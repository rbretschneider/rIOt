import { describe, expect, it } from 'vitest'
import {
  mergeLogLines,
  downloadLogs,
  logIdentity,
  logBoundary,
} from './logBuffer'
describe('Docker history and live boundary', () => {
  it('merges masked copies, retains equal timestamp distinct messages and sorts', () => {
    const t = '2026-10-19T10:00:00.000001Z'
    const result = mergeLogLines(
      [{ t, s: 'o', l: 'password=<masked>' }],
      [
        { t, s: 'o', l: 'password=secret' },
        { t, s: 'o', l: 'second line' },
        { t: '2026-10-19T09:00:00Z', s: 'e', l: 'older' },
      ],
    )
    expect(result.lines).toHaveLength(3)
    expect(result.lines[0].l).toBe('older')
    expect(result.lines.some((l) => l.l?.includes('secret'))).toBe(false)
  })
  it('uses original agent microseconds for skewed line identity', () => {
    expect(
      logIdentity({ t: 'server1', agent_t: 'agent', s: 'o', l: 'same' }),
    ).toBe(logIdentity({ t: 'server2', agent_t: 'agent', s: 'o', l: 'same' }))
  })
  it('bounds count at ten thousand', () => {
    const r = mergeLogLines(
      [],
      Array.from({ length: 11000 }, (_, i) => ({
        t: new Date(1700000000000 + i).toISOString(),
        l: 'line',
        s: 'o',
      })),
    )
    expect(r.dropped).toBe(true)
    expect(r.lines).toHaveLength(10000)
    expect(r.lines[0].t).toBe(new Date(1700000001000).toISOString())
  })
  it('bounds UTF16 text at 32 MiB and each line at 16 KiB', () => {
    const r = mergeLogLines(
      [],
      Array.from({ length: 2000 }, (_, i) => ({
        t: String(i),
        l: 'x'.repeat(20000),
        s: 'o',
      })),
    )
    expect(r.dropped).toBe(true)
    expect(
      r.lines.reduce((sum, l) => sum + 2 * l.l!.length, 0),
    ).toBeLessThanOrEqual(32 * 1024 * 1024)
    expect(r.lines.every((l) => l.l!.length <= 16384)).toBe(true)
  })
  it('exports both timestamps and textual gap markers', () => {
    const output = downloadLogs([
      { t: 'server', agent_t: 'agent', s: 'o', l: 'hello' },
      {
        t: 'server2',
        gap: { count: 20, lower_bound: true, reason: 'quota_sampled' },
      },
    ])
    expect(output).toContain('# rIOt')
    expect(output).toContain('clock skew; agent time agent')
    expect(output).toContain('gap: at least 20 lines; quota_sampled')
  })
  it('does not allow future agent clocks to poison the boundary, independently of browser time', () => {
    const now = Date.parse('2026-10-19T10:00:00Z')
    expect(
      logBoundary(
        [{ t: '2099-01-01T00:00:00Z' }, { t: '2026-10-19T10:01:00Z' }],
        now,
        1000,
      ),
    ).toBe('2026-10-19T10:01:00Z')
  })
})

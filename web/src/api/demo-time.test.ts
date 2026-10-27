import { describe, expect, it } from 'vitest'
import * as demoClient from './demo-client'
import * as demoData from './demo-data'
import { acceptServerZone, formatTimestamp } from '../utils/format'

// Imports the demo modules next to the real formatter (as demo-parity.test.ts does), which is why this
// lives in a test file and nowhere in the production graph.

describe('TIME-12H-001 [AC-040] the demo reports America/New_York', () => {
  it('returns America/New_York from getTimeDisplay', async () => {
    await expect(demoClient.api.getTimeDisplay()).resolves.toEqual({ display_zone: 'America/New_York' })
  })

  it('reports a zone the dashboard accepts', async () => {
    const { display_zone } = await demoClient.api.getTimeDisplay()
    expect(acceptServerZone(display_zone)).toBe('America/New_York')
  })
})

describe('TIME-12H-001 [AC-041] the demo patch as-of uses the server format', () => {
  it('writes as_of as 2026-10-18 10:00 PM EDT (just now) on every demo badge', () => {
    const badges = demoData.getPatchStatus().map(p => p.patch_badge)
    expect(badges.length).toBeGreaterThan(0)
    for (const badge of badges) {
      expect(badge?.explanation.as_of).toBe('2026-10-18 10:00 PM EDT (just now)')
    }
  })

  it('is the same instant the server would render for 2026-10-19T02:00:00Z in New York', () => {
    const zone = acceptServerZone('America/New_York')
    expect(formatTimestamp('2026-10-19T02:00:00Z', { zone: 'Server', hourCycle: '12h', serverTimeZone: zone }, 'minute')).toBe(
      '2026-10-18 10:00 PM EDT',
    )
  })
})

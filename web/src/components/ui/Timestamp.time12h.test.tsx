import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { HOUR_CYCLE_STORAGE_KEY, TIME_ZONE_STORAGE_KEY, TimePreferenceProvider } from '../../contexts/TimePreferenceProvider'
import { RelativeTime } from './RelativeTime'
import { Timestamp } from './Timestamp'

// TIME-12H-001. Instant 2026-10-27T17:05:09Z, server zone America/New_York.
const INSTANT = '2026-10-27T17:05:09Z'
const NOW = Date.parse('2026-10-27T17:10:09Z')

function storageWith(seed: { zone?: string; cycle?: string } = {}) {
  return {
    getItem: (key: string) => (key === TIME_ZONE_STORAGE_KEY ? (seed.zone ?? null) : key === HOUR_CYCLE_STORAGE_KEY ? (seed.cycle ?? null) : null),
    setItem: () => {},
  }
}

function mount(ui: React.ReactElement, seed: { zone?: string; cycle?: string } = {}) {
  return render(
    <TimePreferenceProvider storage={storageWith(seed)} now={() => NOW} localTimeZone="America/Los_Angeles" serverZone="America/New_York">
      {ui}
    </TimePreferenceProvider>,
  )
}

describe('TIME-12H-001 [AC-016] RelativeTime title uses the default Server 12-hour form', () => {
  it('titles the element 2026-10-27 1:05:09 PM EDT with the default preference', () => {
    mount(<RelativeTime value={INSTANT} />)
    expect(screen.getByText('5m ago')).toHaveAttribute('title', '2026-10-27 1:05:09 PM EDT')
  })

  it('follows the 24-hour cycle in the title', () => {
    mount(<RelativeTime value={INSTANT} />, { zone: 'Server', cycle: '24h' })
    expect(screen.getByText('5m ago')).toHaveAttribute('title', '2026-10-27 13:05:09 EDT')
  })

  it('renders a Timestamp in the same default form', () => {
    mount(<Timestamp value={INSTANT} />)
    expect(screen.getByText('2026-10-27 1:05:09 PM EDT').tagName).toBe('TIME')
  })
})

describe('TIME-12H-001 [AC-015] RelativeTime text is identical in every zone and hour cycle', () => {
  const combos = (['Server', 'UTC', 'Local'] as const).flatMap(zone => (['12h', '24h'] as const).map(cycle => [zone, cycle] as const))

  it.each(combos)('reads "5m ago" under %s with %s', (zone, cycle) => {
    mount(<RelativeTime value={INSTANT} />, { zone, cycle })
    expect(screen.getByText('5m ago')).toBeInTheDocument()
  })
})

import { describe, expect, it } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import {
  HOUR_CYCLE_STORAGE_KEY,
  TIME_ZONE_STORAGE_KEY,
  TimePreferenceProvider,
  useTimePreference,
} from '../../contexts/TimePreferenceProvider'
import { RelativeTime } from './RelativeTime'
import { Timestamp } from './Timestamp'

const INSTANT = '2026-10-17T14:03:22Z'

describe('[UIF AC-109] Timestamp always carries a zone label', () => {
  it('renders second precision by default, in UTC', () => {
    render(<Timestamp value={INSTANT} />)
    expect(screen.getByText('2026-10-17 14:03:22 UTC')).toBeInTheDocument()
  })

  it.each([
    ['minute', '2026-10-17 14:03 UTC'],
    ['time', '14:03:22 UTC'],
  ] as const)('renders %s precision', (precision, expected) => {
    render(<Timestamp value={INSTANT} precision={precision} />)
    expect(screen.getByText(expected)).toBeInTheDocument()
  })

  it('wraps the text in <time dateTime=ISO>', () => {
    render(<Timestamp value={INSTANT} />)
    const el = screen.getByText('2026-10-17 14:03:22 UTC')
    expect(el.tagName).toBe('TIME')
    expect(el).toHaveAttribute('dateTime', '2026-10-17T14:03:22.000Z')
  })

  it.each([[undefined], [null], [''], ['garbage']])('renders the em dash fallback for %j', value => {
    render(<Timestamp value={value as string | null | undefined} />)
    expect(screen.getByText('—')).toBeInTheDocument()
    expect(screen.queryByText(/Invalid Date/)).toBeNull()
  })

  it('renders a caller fallback', () => {
    render(<Timestamp value={undefined} fallback="n/a" />)
    expect(screen.getByText('n/a')).toBeInTheDocument()
  })
})

// TIME-12H-001 AD-013: the provider storage is seeded with UTC + 24h so the UIF strings are unchanged.
const UTC_24H_STORAGE = {
  getItem: (key: string) => (key === TIME_ZONE_STORAGE_KEY ? 'UTC' : key === HOUR_CYCLE_STORAGE_KEY ? '24h' : null),
  setItem: () => {},
}

describe('[UIF AC-117] zone switch updates Timestamp and RelativeTime title without remount', () => {
  function Toggle() {
    const { setZone } = useTimePreference()
    return <button onClick={() => setZone('Local')}>toggle</button>
  }

  it('switches both from UTC to the local zone label on the same DOM nodes', () => {
    const now = Date.parse('2026-10-17T14:08:22Z')
    render(
      <TimePreferenceProvider storage={UTC_24H_STORAGE} now={() => now} localTimeZone="America/Los_Angeles">
        <Toggle />
        <Timestamp value={INSTANT} />
        <RelativeTime value={INSTANT} />
      </TimePreferenceProvider>,
    )
    const stamp = screen.getByText('2026-10-17 14:03:22 UTC')
    const relative = screen.getByText('5m ago')
    expect(relative).toHaveAttribute('title', '2026-10-17 14:03:22 UTC')

    act(() => screen.getByRole('button', { name: 'toggle' }).click())

    expect(stamp).toHaveTextContent('2026-10-17 07:03:22 PDT')
    expect(relative).toHaveAttribute('title', '2026-10-17 07:03:22 PDT')
    expect(stamp.isConnected && relative.isConnected, 'nodes must not be remounted').toBe(true)
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import {
  HOUR_CYCLE_STORAGE_KEY,
  TIME_ZONE_STORAGE_KEY,
  TimePreferenceProvider,
  useTimePreference,
} from './TimePreferenceProvider'
import { Timestamp } from '../components/ui/Timestamp'
import HeaderClock from '../components/HeaderClock'
import type { HourCycle, TimeZonePref } from '../utils/format'

// TIME-12H-001. Instant 2026-10-27T17:05:09Z, server zone America/New_York, Local zone America/Los_Angeles
// through the localTimeZone seam.
const INSTANT = '2026-10-27T17:05:09Z'

function memoryStorage(seed: { zone?: string; cycle?: string } = {}) {
  const data = new Map<string, string>()
  if (seed.zone !== undefined) data.set(TIME_ZONE_STORAGE_KEY, seed.zone)
  if (seed.cycle !== undefined) data.set(HOUR_CYCLE_STORAGE_KEY, seed.cycle)
  return {
    data,
    getItem: vi.fn((key: string) => data.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => {
      data.set(key, value)
    }),
  }
}

function Probe() {
  const { zone, hourCycle, setZone, setHourCycle, serverZone, setServerZone } = useTimePreference()
  return (
    <div>
      <span data-testid="zone">{zone}</span>
      <span data-testid="cycle">{hourCycle}</span>
      <span data-testid="server-zone">{serverZone ?? 'none'}</span>
      <Timestamp value={INSTANT} />
      {(['Server', 'UTC', 'Local'] as const).map(z => (
        <button key={z} onClick={() => setZone(z)}>{`zone-${z}`}</button>
      ))}
      {(['12h', '24h'] as const).map(c => (
        <button key={c} onClick={() => setHourCycle(c)}>{`cycle-${c}`}</button>
      ))}
      <button onClick={() => setServerZone('America/New_York')}>api-new-york</button>
      <button onClick={() => setServerZone('Mars/Olympus')}>api-mars</button>
    </div>
  )
}

type ProviderProps = Omit<Parameters<typeof TimePreferenceProvider>[0], 'children'>

function mount(props: ProviderProps) {
  return render(
    <TimePreferenceProvider localTimeZone="America/Los_Angeles" {...props}>
      <Probe />
    </TimePreferenceProvider>,
  )
}

const press = (name: string) => act(() => screen.getByRole('button', { name }).click())
const zoneText = () => screen.getByTestId('zone').textContent
const cycleText = () => screen.getByTestId('cycle').textContent
const stamp = () => screen.getByText(/^2026-10-2/)

describe('TIME-12H-001 [AC-017] an accepted server zone formats with its abbreviation', () => {
  it('formats the example instant in the server zone when the provider is given America/New_York', () => {
    mount({ storage: memoryStorage(), serverZone: 'America/New_York' })
    expect(stamp()).toHaveTextContent('2026-10-27 1:05:09 PM EDT')
    expect(stamp().textContent).toMatch(/EDT$/)
  })

  it('adopts a zone reported later, without a remount', () => {
    mount({ storage: memoryStorage() })
    const node = stamp()
    expect(node).toHaveTextContent('2026-10-27 5:05:09 PM UTC')
    press('api-new-york')
    expect(node).toHaveTextContent('2026-10-27 1:05:09 PM EDT')
    expect(screen.getByTestId('server-zone').textContent).toBe('America/New_York')
    expect(node.isConnected, 'the node must not be remounted').toBe(true)
  })

  it('ignores a reported zone that fails validation', () => {
    mount({ storage: memoryStorage() })
    press('api-mars')
    expect(screen.getByTestId('server-zone').textContent).toBe('none')
    expect(stamp()).toHaveTextContent('2026-10-27 5:05:09 PM UTC')
  })

  it('treats a later rejected report as unknown, so the Server zone formats as UTC', () => {
    mount({ storage: memoryStorage(), serverZone: 'America/New_York' })
    press('api-mars')
    expect(screen.getByTestId('server-zone').textContent).toBe('none')
  })
})

describe('TIME-12H-001 [AC-027] each zone and cycle combination survives a remount', () => {
  const zones: TimeZonePref[] = ['Server', 'UTC', 'Local']
  const cycles: HourCycle[] = ['12h', '24h']
  const combos = zones.flatMap(zone => cycles.map(cycle => [zone, cycle] as const))

  it.each(combos)('restores %s with %s', (zone, cycle) => {
    const storage = memoryStorage()
    const first = mount({ storage })
    press(`zone-${zone}`)
    press(`cycle-${cycle}`)
    first.unmount()

    mount({ storage })
    expect(zoneText(), 'restored zone').toBe(zone)
    expect(cycleText(), 'restored hour cycle').toBe(cycle)
  })

  it('writes exactly the two keys, with enum values of at most 6 characters', () => {
    const storage = memoryStorage()
    mount({ storage })
    press('zone-Local')
    press('cycle-24h')
    expect([...storage.data.keys()].sort()).toEqual(['riot-hour-cycle', 'riot-time-zone'])
    for (const value of storage.data.values()) expect(value.length).toBeLessThanOrEqual(6)
  })

  it('writes the zone key before the hour-cycle key', () => {
    const storage = memoryStorage()
    mount({ storage })
    press('zone-UTC')
    expect(storage.setItem.mock.calls.map(call => call[0])).toEqual(['riot-time-zone', 'riot-hour-cycle'])
  })
})

describe('TIME-12H-001 [AC-028] legacy UTC becomes Server, legacy Local stays, a new UTC persists', () => {
  it('resolves a pre-story UTC (no hour-cycle key) as Server', () => {
    mount({ storage: memoryStorage({ zone: 'UTC' }) })
    expect(zoneText()).toBe('Server')
    expect(cycleText()).toBe('12h')
  })

  it('resolves a pre-story Local (no hour-cycle key) as Local', () => {
    mount({ storage: memoryStorage({ zone: 'Local' }) })
    expect(zoneText()).toBe('Local')
    expect(cycleText()).toBe('12h')
  })

  it('persists a UTC selected after the story across a remount', () => {
    const storage = memoryStorage({ zone: 'UTC' })
    const first = mount({ storage })
    expect(zoneText()).toBe('Server')
    press('zone-UTC')
    first.unmount()

    mount({ storage })
    expect(zoneText()).toBe('UTC')
  })

  it('honours a post-story UTC when the hour-cycle key is present', () => {
    mount({ storage: memoryStorage({ zone: 'UTC', cycle: '24h' }) })
    expect(zoneText()).toBe('UTC')
    expect(cycleText()).toBe('24h')
  })

  it('does not treat an invalid hour-cycle value as the post-story marker', () => {
    mount({ storage: memoryStorage({ zone: 'UTC', cycle: 'banana' }) })
    expect(zoneText(), 'a tampered marker means the zone key is legacy').toBe('Server')
  })

  it('a failing zone write does not write the hour-cycle marker, so a legacy UTC cannot look post-story', () => {
    const storage = memoryStorage({ zone: 'UTC' })
    storage.setItem.mockImplementation(() => {
      throw new DOMException('quota', 'QuotaExceededError')
    })
    mount({ storage })
    press('zone-Local')
    expect(storage.setItem).toHaveBeenCalledTimes(1)
    expect(storage.data.has(HOUR_CYCLE_STORAGE_KEY)).toBe(false)
  })
})

describe('TIME-12H-001 [AC-029] tampered and failing storage fall back to Server and 12h', () => {
  let formattingZones: Array<string | undefined>
  let received: Array<string | undefined>

  beforeEach(() => {
    formattingZones = []
    received = []
    const Original = Intl.DateTimeFormat
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(function (this: unknown, locale?: string | string[], options?: Intl.DateTimeFormatOptions) {
      received.push(options?.timeZone)
      if (options?.timeZoneName === 'short') formattingZones.push(options.timeZone)
      return new Original(locale, options)
    } as unknown as typeof Intl.DateTimeFormat)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  const cases: Array<[string, { zone?: string; cycle?: string }]> = [
    ['a zone value "banana"', { zone: 'banana' }],
    ['a zone value America/Chicago', { zone: 'America/Chicago' }],
    ['an hour-cycle value "banana"', { zone: 'UTC', cycle: 'banana' }],
  ]

  it.each(cases)('%s resolves to Server and 12h without an error and without America/Chicago reaching Intl', (_label, seed) => {
    expect(() => mount({ storage: memoryStorage(seed), serverZone: 'America/New_York' })).not.toThrow()
    expect(zoneText()).toBe('Server')
    expect(cycleText()).toBe('12h')
    expect(stamp()).toHaveTextContent('2026-10-27 1:05:09 PM EDT')
    expect(received).not.toContain('America/Chicago')
  })

  it('getItem throwing resolves to Server and 12h without an error', () => {
    const storage = memoryStorage()
    storage.getItem.mockImplementation(() => {
      throw new Error('SecurityError')
    })
    expect(() => mount({ storage })).not.toThrow()
    expect(zoneText()).toBe('Server')
    expect(cycleText()).toBe('12h')
  })

  it('setItem throwing still applies a UTC selection for the session', () => {
    const storage = memoryStorage()
    storage.setItem.mockImplementation(() => {
      throw new Error('QuotaExceededError')
    })
    mount({ storage, serverZone: 'America/New_York' })
    expect(stamp()).toHaveTextContent('2026-10-27 1:05:09 PM EDT')
    press('zone-UTC')
    expect(zoneText()).toBe('UTC')
    expect(stamp()).toHaveTextContent('2026-10-27 5:05:09 PM UTC')
  })

  it('only the allowed zones are ever used for formatting', () => {
    mount({ storage: memoryStorage({ zone: 'America/Chicago' }), serverZone: 'America/New_York' })
    press('zone-Local')
    press('zone-UTC')
    const allowed = new Set<string | undefined>(['UTC', 'America/New_York', 'America/Los_Angeles'])
    for (const zone of formattingZones) expect(allowed.has(zone), `formatting zone ${String(zone)}`).toBe(true)
  })
})

describe('TIME-12H-001 [AC-072] the clock tick does not re-render timestamp consumers', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(Date.parse(INSTANT))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('leaves a context consumer untouched across 5 seconds of ticks', () => {
    const renders = vi.fn()
    function Consumer() {
      useTimePreference()
      renders()
      return <Timestamp value={INSTANT} />
    }
    render(
      <TimePreferenceProvider storage={memoryStorage()} now={() => Date.now()} serverZone="America/New_York">
        <HeaderClock />
        <Consumer />
      </TimePreferenceProvider>,
    )
    const before = renders.mock.calls.length
    expect(screen.getByText('1:05:09 PM EDT', { selector: 'button time' })).toBeInTheDocument()
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(screen.getByText('1:05:14 PM EDT', { selector: 'button time' }), 'the clock itself did tick').toBeInTheDocument()
    expect(renders.mock.calls.length, 'consumer re-renders caused by the tick').toBe(before)
  })

  it('reporting the same server zone again does not re-render consumers', () => {
    const renders = vi.fn()
    function Consumer() {
      const { setServerZone } = useTimePreference()
      renders()
      return <button onClick={() => setServerZone('America/New_York')}>report</button>
    }
    render(
      <TimePreferenceProvider storage={memoryStorage()} serverZone="America/New_York">
        <Consumer />
      </TimePreferenceProvider>,
    )
    const before = renders.mock.calls.length
    act(() => screen.getByRole('button', { name: 'report' }).click())
    expect(renders.mock.calls.length).toBe(before)
  })
})

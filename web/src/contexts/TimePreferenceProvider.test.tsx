import { afterEach, describe, it, expect, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import {
  HOUR_CYCLE_STORAGE_KEY,
  TIME_ZONE_STORAGE_KEY,
  TimePreferenceProvider,
  useTimeFormatters,
  useTimePreference,
} from './TimePreferenceProvider'

const INSTANT = '2026-10-17T14:03:22Z'

// TIME-12H-001 AD-013: the UIF tests below keep their 24-hour strings by seeding UTC + 24h, and switch
// zones with setZone. Tests that exercise the new default (Server, 12-hour) say so in their title.
interface Seed {
  zone?: string
  cycle?: string
}

function memoryStorage(seed: Seed = {}) {
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

/** A post-TIME-12H-001 pair: the hour-cycle key marks the zone key as a deliberate choice. */
const utc24 = (): Seed => ({ zone: 'UTC', cycle: '24h' })

function Probe() {
  const { zone, hourCycle, setZone, zoneOpts } = useTimePreference()
  const { timestamp, chartTick, chartTooltip } = useTimeFormatters()
  return (
    <div>
      <span data-testid="zone">{zone}</span>
      <span data-testid="cycle">{hourCycle}</span>
      <span data-testid="has-local-tz">{String(zoneOpts.localTimeZone !== undefined)}</span>
      <span data-testid="stamp">{timestamp(INSTANT)}</span>
      <span data-testid="tick">{chartTick(Date.parse(INSTANT))}</span>
      <span data-testid="tooltip">{chartTooltip(Date.parse(INSTANT))}</span>
      <button onClick={() => setZone('Local')}>to-local</button>
      <button onClick={() => setZone('UTC')}>to-utc</button>
    </div>
  )
}

function renderProbe(props: Parameters<typeof TimePreferenceProvider>[0]) {
  return render(<TimePreferenceProvider {...props}><Probe /></TimePreferenceProvider>)
}

describe('[UIF AC-116] write failure keeps session value', () => {
  it('still applies the zone switch for the session when setItem throws', () => {
    const storage = memoryStorage(utc24())
    storage.setItem.mockImplementation(() => {
      throw new DOMException('quota', 'QuotaExceededError')
    })
    renderProbe({ storage, localTimeZone: 'America/Los_Angeles', children: null })
    expect(screen.getByTestId('zone').textContent).toBe('UTC')

    act(() => screen.getByRole('button', { name: 'to-local' }).click())

    expect(screen.getByTestId('zone').textContent).toBe('Local')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 07:03:22 PDT')
  })

  it('persists the choice under riot-time-zone when the write succeeds', () => {
    const storage = memoryStorage(utc24())
    renderProbe({ storage, children: null })
    act(() => screen.getByRole('button', { name: 'to-local' }).click())
    expect(storage.data.get('riot-time-zone')).toBe('Local')
  })

  it('works with no storage at all (null)', () => {
    renderProbe({ storage: null, localTimeZone: 'America/Los_Angeles', children: null })
    act(() => screen.getByRole('button', { name: 'to-local' }).click())
    expect(screen.getByTestId('zone').textContent).toBe('Local')
  })
})

describe('[UIF AC-117] zone switch updates formatters without remount', () => {
  it('switches stamp, chart tick and chart tooltip together', () => {
    renderProbe({ storage: memoryStorage(utc24()), localTimeZone: 'America/Los_Angeles', children: null })
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 14:03:22 UTC')
    expect(screen.getByTestId('tick').textContent).toBe('14:03')
    expect(screen.getByTestId('tooltip').textContent).toBe('2026-10-17 14:03:22 UTC')

    act(() => screen.getByRole('button', { name: 'to-local' }).click())

    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 07:03:22 PDT')
    expect(screen.getByTestId('tick').textContent).toBe('07:03')
    expect(screen.getByTestId('tooltip').textContent).toBe('2026-10-17 07:03:22 PDT')
  })

  it('switches back to UTC on a second selection', () => {
    renderProbe({ storage: memoryStorage(utc24()), localTimeZone: 'America/Los_Angeles', children: null })
    act(() => screen.getByRole('button', { name: 'to-local' }).click())
    act(() => screen.getByRole('button', { name: 'to-utc' }).click())
    expect(screen.getByTestId('zone').textContent).toBe('UTC')
  })
})

describe('[UIF AC-103] [TIME-12H-001 AC-021] default preference', () => {
  it('defaults to Server and 12-hour with empty storage (no server zone yet, so it formats as UTC)', () => {
    renderProbe({ storage: memoryStorage(), children: null })
    expect(screen.getByTestId('zone').textContent).toBe('Server')
    expect(screen.getByTestId('cycle').textContent).toBe('12h')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 2:03:22 PM UTC')
  })

  it('reads a stored Local preference', () => {
    renderProbe({ storage: memoryStorage({ zone: 'Local', cycle: '24h' }), localTimeZone: 'America/Los_Angeles', children: null })
    expect(screen.getByTestId('zone').textContent).toBe('Local')
  })

  it('works without a provider (UTC, 24-hour default)', () => {
    render(<Probe />)
    expect(screen.getByTestId('zone').textContent).toBe('UTC')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 14:03:22 UTC')
  })
})

describe('[UIF SEC-006] tampered riot-time-zone resolves to Server', () => {
  const tampered: Array<[string, string]> = [
    ['banana', 'banana'],
    ['an unknown IANA zone', 'America/Nowhere'],
    ['wrong-case local', 'local'],
    ['JSON', '{"zone":"Local"}'],
    ['quoted JSON string', '"Local"'],
    ['a 100 KB string', 'x'.repeat(100 * 1024)],
    ['an empty string', ''],
  ]

  it.each(tampered)('%s falls back to Server without throwing', (_label, value) => {
    expect(() =>
      renderProbe({ storage: memoryStorage({ zone: value }), localTimeZone: 'America/Los_Angeles', children: null }),
    ).not.toThrow()
    expect(screen.getByTestId('zone').textContent).toBe('Server')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 2:03:22 PM UTC')
  })

  it('falls back to Server when getItem throws', () => {
    const storage = memoryStorage()
    storage.getItem.mockImplementation(() => {
      throw new Error('SecurityError')
    })
    expect(() => renderProbe({ storage, children: null })).not.toThrow()
    expect(screen.getByTestId('zone').textContent).toBe('Server')
  })

  it('never feeds the stored value into the zone options (localTimeZone stays unset)', () => {
    renderProbe({ storage: memoryStorage({ zone: 'America/Nowhere' }), children: null })
    expect(screen.getByTestId('has-local-tz').textContent).toBe('false')
  })
})

// Added by QA Engineer
// Covers AC-116 and SEC-006 on the production path: no `storage` prop, so the provider resolves the
// real window.localStorage itself (safeLocalStorage). Every test above injects a fake storage and
// would stay green if that default path were broken.
describe('[UIF AC-116] QA: default storage path (real window.localStorage)', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.localStorage.clear()
  })

  const mountDefault = () =>
    render(
      <TimePreferenceProvider localTimeZone="America/Los_Angeles">
        <Probe />
      </TimePreferenceProvider>,
    )

  it.each([
    ['banana', 'banana'],
    ['an unknown IANA zone', 'America/Nowhere'],
    ['wrong-case local', 'local'],
    ['JSON', '{"zone":"Local"}'],
    ['a 100 KB string', 'x'.repeat(100 * 1024)],
  ])('a tampered stored value (%s) resolves to Server and renders without throwing', (_label, value) => {
    window.localStorage.setItem(TIME_ZONE_STORAGE_KEY, value)
    expect(() => mountDefault()).not.toThrow()
    expect(screen.getByTestId('zone').textContent).toBe('Server')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 2:03:22 PM UTC')
  })

  it('restores a legacy Local preference (12-hour) and writes a switch back under riot-time-zone and riot-hour-cycle', () => {
    window.localStorage.setItem(TIME_ZONE_STORAGE_KEY, 'Local')
    mountDefault()
    expect(screen.getByTestId('zone').textContent).toBe('Local')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 7:03:22 AM PDT')

    act(() => screen.getByRole('button', { name: 'to-utc' }).click())
    expect(window.localStorage.getItem(TIME_ZONE_STORAGE_KEY)).toBe('UTC')
    expect(window.localStorage.getItem(HOUR_CYCLE_STORAGE_KEY)).toBe('12h')
  })

  it('falls back to Server when reading window.localStorage itself throws (blocked storage)', () => {
    vi.spyOn(window, 'localStorage', 'get').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError')
    })
    expect(() => mountDefault()).not.toThrow()
    expect(screen.getByTestId('zone').textContent).toBe('Server')

    // The switch still works for the session even though nothing can be persisted.
    act(() => screen.getByRole('button', { name: 'to-local' }).click())
    expect(screen.getByTestId('zone').textContent).toBe('Local')
  })

  it('falls back to Server when getItem throws on the real Storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError')
    })
    expect(() => mountDefault()).not.toThrow()
    expect(screen.getByTestId('zone').textContent).toBe('Server')
  })
})

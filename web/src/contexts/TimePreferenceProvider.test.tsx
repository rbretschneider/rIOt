import { describe, it, expect, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import {
  TIME_ZONE_STORAGE_KEY,
  TimePreferenceProvider,
  useTimeFormatters,
  useTimePreference,
} from './TimePreferenceProvider'

const INSTANT = '2026-10-17T14:03:22Z'

function memoryStorage(initial?: string) {
  const data = new Map<string, string>()
  if (initial !== undefined) data.set(TIME_ZONE_STORAGE_KEY, initial)
  return {
    data,
    getItem: vi.fn((key: string) => data.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => {
      data.set(key, value)
    }),
  }
}

function Probe() {
  const { zone, toggleZone, zoneOpts } = useTimePreference()
  const { timestamp, chartTick, chartTooltip } = useTimeFormatters()
  return (
    <div>
      <span data-testid="zone">{zone}</span>
      <span data-testid="has-local-tz">{String(zoneOpts.localTimeZone !== undefined)}</span>
      <span data-testid="stamp">{timestamp(INSTANT)}</span>
      <span data-testid="tick">{chartTick(Date.parse(INSTANT))}</span>
      <span data-testid="tooltip">{chartTooltip(Date.parse(INSTANT))}</span>
      <button onClick={toggleZone}>toggle</button>
    </div>
  )
}

function renderProbe(props: Parameters<typeof TimePreferenceProvider>[0]) {
  return render(<TimePreferenceProvider {...props}><Probe /></TimePreferenceProvider>)
}

describe('[UIF AC-116] write failure keeps session value', () => {
  it('still applies the toggle for the session when setItem throws', () => {
    const storage = memoryStorage()
    storage.setItem.mockImplementation(() => {
      throw new DOMException('quota', 'QuotaExceededError')
    })
    renderProbe({ storage, localTimeZone: 'America/Los_Angeles', children: null })
    expect(screen.getByTestId('zone').textContent).toBe('UTC')

    act(() => screen.getByRole('button', { name: 'toggle' }).click())

    expect(screen.getByTestId('zone').textContent).toBe('Local')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 07:03:22 PDT')
  })

  it('persists the choice under riot-time-zone when the write succeeds', () => {
    const storage = memoryStorage()
    renderProbe({ storage, children: null })
    act(() => screen.getByRole('button', { name: 'toggle' }).click())
    expect(storage.data.get('riot-time-zone')).toBe('Local')
  })

  it('works with no storage at all (null)', () => {
    renderProbe({ storage: null, localTimeZone: 'America/Los_Angeles', children: null })
    act(() => screen.getByRole('button', { name: 'toggle' }).click())
    expect(screen.getByTestId('zone').textContent).toBe('Local')
  })
})

describe('[UIF AC-117] toggle updates formatters without remount', () => {
  it('switches stamp, chart tick and chart tooltip together on toggle', () => {
    renderProbe({ storage: memoryStorage(), localTimeZone: 'America/Los_Angeles', children: null })
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 14:03:22 UTC')
    expect(screen.getByTestId('tick').textContent).toBe('14:03')
    expect(screen.getByTestId('tooltip').textContent).toBe('2026-10-17 14:03:22 UTC')

    act(() => screen.getByRole('button', { name: 'toggle' }).click())

    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 07:03:22 PDT')
    expect(screen.getByTestId('tick').textContent).toBe('07:03')
    expect(screen.getByTestId('tooltip').textContent).toBe('2026-10-17 07:03:22 PDT')
  })

  it('toggles back to UTC on a second activation', () => {
    renderProbe({ storage: memoryStorage(), localTimeZone: 'America/Los_Angeles', children: null })
    const toggle = screen.getByRole('button', { name: 'toggle' })
    act(() => toggle.click())
    act(() => toggle.click())
    expect(screen.getByTestId('zone').textContent).toBe('UTC')
  })
})

describe('[UIF AC-103] default preference', () => {
  it('defaults to UTC with empty storage', () => {
    renderProbe({ storage: memoryStorage(), children: null })
    expect(screen.getByTestId('zone').textContent).toBe('UTC')
  })

  it('reads a stored Local preference', () => {
    renderProbe({ storage: memoryStorage('Local'), localTimeZone: 'America/Los_Angeles', children: null })
    expect(screen.getByTestId('zone').textContent).toBe('Local')
  })

  it('works without a provider (UTC default)', () => {
    render(<Probe />)
    expect(screen.getByTestId('zone').textContent).toBe('UTC')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 14:03:22 UTC')
  })
})

describe('[UIF SEC-006] tampered riot-time-zone resolves to UTC', () => {
  const tampered: Array<[string, string]> = [
    ['banana', 'banana'],
    ['an unknown IANA zone', 'America/Nowhere'],
    ['wrong-case local', 'local'],
    ['JSON', '{"zone":"Local"}'],
    ['quoted JSON string', '"Local"'],
    ['a 100 KB string', 'x'.repeat(100 * 1024)],
    ['an empty string', ''],
  ]

  it.each(tampered)('%s falls back to UTC without throwing', (_label, value) => {
    expect(() =>
      renderProbe({ storage: memoryStorage(value), localTimeZone: 'America/Los_Angeles', children: null }),
    ).not.toThrow()
    expect(screen.getByTestId('zone').textContent).toBe('UTC')
    expect(screen.getByTestId('stamp').textContent).toBe('2026-10-17 14:03:22 UTC')
  })

  it('falls back to UTC when getItem throws', () => {
    const storage = memoryStorage()
    storage.getItem.mockImplementation(() => {
      throw new Error('SecurityError')
    })
    expect(() => renderProbe({ storage, children: null })).not.toThrow()
    expect(screen.getByTestId('zone').textContent).toBe('UTC')
  })

  it('never feeds the stored value into the zone options (localTimeZone stays unset)', () => {
    renderProbe({ storage: memoryStorage('America/Nowhere'), children: null })
    expect(screen.getByTestId('has-local-tz').textContent).toBe('false')
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  HOUR_CYCLE_STORAGE_KEY,
  TIME_ZONE_STORAGE_KEY,
  TimePreferenceProvider,
} from '../contexts/TimePreferenceProvider'
import HeaderClock from './HeaderClock'

const NOW = Date.parse('2026-10-17T14:03:22Z')

// TIME-12H-001 AD-013: the UIF tests seed UTC + 24h so their strings are unchanged. Tests of the new default
// (Server, 12-hour) say so in their title and live in HeaderClock.time12h.test.tsx.
function memoryStorage(seed: { zone?: string; cycle?: string } = { zone: 'UTC', cycle: '24h' }) {
  const data = new Map<string, string>()
  if (seed.zone !== undefined) data.set(TIME_ZONE_STORAGE_KEY, seed.zone)
  if (seed.cycle !== undefined) data.set(HOUR_CYCLE_STORAGE_KEY, seed.cycle)
  return {
    data,
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => {
      data.set(key, value)
    },
  }
}

function mount(storage: ReturnType<typeof memoryStorage> | null = memoryStorage(), now: () => number = () => NOW) {
  return render(
    <TimePreferenceProvider storage={storage} now={now} localTimeZone="America/Los_Angeles">
      <HeaderClock />
    </TimePreferenceProvider>,
  )
}

const clock = () => screen.getByRole('button', { name: /Time display/ })

describe('[UIF AC-113] defaults to UTC HH:mm:ss', () => {
  it('shows the time in 24-hour HH:mm:ss UTC form when UTC and 24-hour are stored', () => {
    mount()
    expect(clock()).toHaveTextContent('14:03:22 UTC')
    expect(clock().textContent).toMatch(/^\d{2}:\d{2}:\d{2} UTC$/)
  })

  it('is a button whose accessible name states the zone, the hour cycle and the action', () => {
    mount()
    expect(clock()).toHaveAccessibleName('Time display: UTC, 24-hour. Change time display')
    expect(clock()).toHaveAttribute('type', 'button')
  })

  it('renders the instant in a <time> element', () => {
    mount()
    expect(screen.getByText('14:03:22 UTC').tagName).toBe('TIME')
  })
})

describe('[UIF AC-114] activating the clock opens the panel and Local shows local time', () => {
  it('shows the PDT time and a Local accessible name after selecting Local', async () => {
    mount()
    await userEvent.click(clock())
    await userEvent.click(screen.getByRole('radio', { name: 'Local (America/Los_Angeles)' }))
    expect(clock()).toHaveTextContent('07:03:22 PDT')
    expect(clock()).toHaveAccessibleName('Time display: Local (America/Los_Angeles), 24-hour. Change time display')
  })

  it('is operable from the keyboard: Enter opens the panel and Space closes it', async () => {
    mount()
    clock().focus()
    await userEvent.keyboard('{Enter}')
    expect(clock()).toHaveAttribute('aria-expanded', 'true')
    clock().focus()
    await userEvent.keyboard(' ')
    expect(clock()).toHaveAttribute('aria-expanded', 'false')
  })

  it('also sets the title to the same description', async () => {
    mount()
    await userEvent.click(clock())
    await userEvent.click(screen.getByRole('radio', { name: 'Local (America/Los_Angeles)' }))
    expect(clock()).toHaveAttribute('title', 'Time display: Local (America/Los_Angeles), 24-hour. Change time display')
  })
})

describe('[UIF AC-115] preference persists across remount', () => {
  it('writes the choice to localStorage (both keys) and restores it on the next mount', async () => {
    const storage = memoryStorage()
    const first = mount(storage)
    await userEvent.click(clock())
    await userEvent.click(screen.getByRole('radio', { name: 'Local (America/Los_Angeles)' }))
    expect(storage.data.get(TIME_ZONE_STORAGE_KEY)).toBe('Local')
    expect(storage.data.get(HOUR_CYCLE_STORAGE_KEY)).toBe('24h')
    first.unmount()

    mount(storage)
    expect(clock()).toHaveTextContent('07:03:22 PDT')
  })
})

describe('[UIF AC-116] storage failure falls back to Server, 12-hour', () => {
  it('shows the Server default (UTC until the zone is known) when getItem throws, without throwing', () => {
    const storage = {
      getItem: () => {
        throw new Error('SecurityError')
      },
      setItem: () => {},
    }
    expect(() =>
      render(
        <TimePreferenceProvider storage={storage} now={() => NOW}>
          <HeaderClock />
        </TimePreferenceProvider>,
      ),
    ).not.toThrow()
    expect(clock()).toHaveTextContent('2:03:22 PM UTC')
  })

  it('shows the Server default when storage holds an unrecognised value ("banana")', () => {
    mount(memoryStorage({ zone: 'banana' }))
    expect(clock()).toHaveTextContent('2:03:22 PM UTC')
  })

  it('treats a stored IANA zone name as Server, never as a zone (SEC-006)', () => {
    mount(memoryStorage({ zone: 'America/Nowhere' }))
    expect(clock()).toHaveTextContent('2:03:22 PM UTC')
  })
})

describe('[UIF AC-012] focus-visible ring on the clock toggle', () => {
  it('carries the shared focus-visible ring', () => {
    mount()
    expect(clock().className).toContain('focus-visible:ring-2')
  })
})

describe('clock tick (NFR-008)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('advances once per second using the injected clock', () => {
    mount(memoryStorage(), () => Date.now())
    expect(clock()).toHaveTextContent('14:03:22 UTC')
    act(() => {
      vi.advanceTimersByTime(3000)
    })
    expect(clock()).toHaveTextContent('14:03:25 UTC')
  })

  it('clears its interval on unmount', () => {
    const { unmount } = mount(memoryStorage(), () => Date.now())
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not re-render sibling consumers on the one-second tick', () => {
    const renders = vi.fn()
    function Sibling() {
      renders()
      return null
    }
    render(
      <TimePreferenceProvider storage={null} now={() => Date.now()}>
        <HeaderClock />
        <Sibling />
      </TimePreferenceProvider>,
    )
    const before = renders.mock.calls.length
    act(() => {
      vi.advanceTimersByTime(5000)
    })
    expect(renders.mock.calls.length).toBe(before)
  })
})

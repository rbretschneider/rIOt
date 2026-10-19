import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TIME_ZONE_STORAGE_KEY, TimePreferenceProvider } from '../contexts/TimePreferenceProvider'
import HeaderClock from './HeaderClock'

const NOW = Date.parse('2026-10-17T14:03:22Z')

function memoryStorage(initial?: string) {
  const data = new Map<string, string>()
  if (initial !== undefined) data.set(TIME_ZONE_STORAGE_KEY, initial)
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

const clock = () => screen.getByRole('button', { name: /Time zone/ })

describe('[UIF AC-113] defaults to UTC HH:mm:ss', () => {
  it('shows the time in 24-hour HH:mm:ss UTC form with empty storage', () => {
    mount()
    expect(clock()).toHaveTextContent('14:03:22 UTC')
    expect(clock().textContent).toMatch(/^\d{2}:\d{2}:\d{2} UTC$/)
  })

  it('is a button whose accessible name states the zone and the action', () => {
    mount()
    expect(clock()).toHaveAccessibleName('Time zone: UTC. Switch to local time')
    expect(clock()).toHaveAttribute('type', 'button')
  })

  it('renders the instant in a <time> element', () => {
    mount()
    expect(screen.getByText('14:03:22 UTC').tagName).toBe('TIME')
  })
})

describe('[UIF AC-114] toggle switches to local', () => {
  it('shows the PDT time and a local-time accessible name after activation', async () => {
    mount()
    await userEvent.click(clock())
    expect(clock()).toHaveTextContent('07:03:22 PDT')
    expect(clock()).toHaveAccessibleName('Time zone: local (PDT). Switch to UTC')
  })

  it('is operable from the keyboard with Enter and Space', async () => {
    mount()
    clock().focus()
    await userEvent.keyboard('{Enter}')
    expect(clock()).toHaveTextContent('PDT')
    await userEvent.keyboard(' ')
    expect(clock()).toHaveTextContent('UTC')
  })

  it('also sets the title to the same description', async () => {
    mount()
    await userEvent.click(clock())
    expect(clock()).toHaveAttribute('title', 'Time zone: local (PDT). Switch to UTC')
  })
})

describe('[UIF AC-115] preference persists across remount', () => {
  it('writes the choice to localStorage and restores it on the next mount', async () => {
    const storage = memoryStorage()
    const first = mount(storage)
    await userEvent.click(clock())
    expect(storage.data.get(TIME_ZONE_STORAGE_KEY)).toBe('Local')
    first.unmount()

    mount(storage)
    expect(clock()).toHaveTextContent('07:03:22 PDT')
  })
})

describe('[UIF AC-116] storage failure falls back to UTC', () => {
  it('shows UTC when getItem throws, without throwing', () => {
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
    expect(clock()).toHaveTextContent('UTC')
  })

  it('shows UTC when storage holds an unrecognised value ("banana")', () => {
    mount(memoryStorage('banana'))
    expect(clock()).toHaveTextContent('14:03:22 UTC')
  })

  it('treats a stored IANA zone name as UTC, never as a zone (SEC-006)', () => {
    mount(memoryStorage('America/Nowhere'))
    expect(clock()).toHaveTextContent('14:03:22 UTC')
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

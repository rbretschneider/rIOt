import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { TimePreferenceProvider } from '../../contexts/TimePreferenceProvider'
import { RelativeTime } from './RelativeTime'

const NOW = Date.parse('2026-10-17T12:00:00Z')

function renderAt(ui: React.ReactElement, opts: { now?: () => number; localTimeZone?: string } = {}) {
  return render(
    <TimePreferenceProvider storage={null} now={opts.now ?? (() => NOW)} localTimeZone={opts.localTimeZone}>
      {ui}
    </TimePreferenceProvider>,
  )
}

describe('[UIF AC-111] markup', () => {
  it('renders a <time> with the ISO dateTime, relative text and a UTC title', () => {
    const instant = new Date(NOW - 5 * 60_000).toISOString()
    renderAt(<RelativeTime value={instant} />)
    const el = screen.getByText('5m ago')
    expect(el.tagName).toBe('TIME')
    expect(el).toHaveAttribute('dateTime', instant)
    expect(el.getAttribute('title')).toMatch(/UTC$/)
    expect(el).toHaveAttribute('title', '2026-10-17 11:55:00 UTC')
  })

  it('accepts epoch milliseconds and normalises dateTime to ISO', () => {
    renderAt(<RelativeTime value={NOW - 3 * 3_600_000} />)
    const el = screen.getByText('3h ago')
    expect(el).toHaveAttribute('dateTime', '2026-10-17T09:00:00.000Z')
  })

  it.each([[undefined], [null], ['not-a-date'], ['']])('renders the fallback for %j', value => {
    renderAt(<RelativeTime value={value as string | null | undefined} />)
    expect(screen.getByText('—').tagName).toBe('SPAN')
    expect(screen.queryByText('Invalid Date')).toBeNull()
  })

  it('renders a custom fallback', () => {
    renderAt(<RelativeTime value={undefined} fallback="never" />)
    expect(screen.getByText('never')).toBeInTheDocument()
  })

  it('titles the element in the local zone under the Local preference', () => {
    render(
      <TimePreferenceProvider
        storage={{ getItem: () => 'Local', setItem: () => {} }}
        now={() => NOW}
        localTimeZone="America/Los_Angeles"
      >
        <RelativeTime value={new Date(NOW - 5 * 60_000)} />
      </TimePreferenceProvider>,
    )
    expect(screen.getByText('5m ago')).toHaveAttribute('title', '2026-10-17 04:55:00 PDT')
  })
})

describe('[UIF AC-112] refreshes over time', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('moves from "just now" to "2m ago" after 120 s', () => {
    render(
      <TimePreferenceProvider storage={null}>
        <RelativeTime value={new Date(NOW)} />
      </TimePreferenceProvider>,
    )
    expect(screen.getByText('just now')).toBeInTheDocument()
    act(() => { vi.advanceTimersByTime(120_000) })
    expect(screen.getByText('2m ago')).toBeInTheDocument()
  })

  it('runs a single interval for many rows', () => {
    const spy = vi.spyOn(globalThis, 'setInterval')
    render(
      <TimePreferenceProvider storage={null}>
        {Array.from({ length: 20 }, (_, i) => <RelativeTime key={i} value={new Date(NOW)} />)}
      </TimePreferenceProvider>,
    )
    expect(spy.mock.calls.filter(c => c[1] === 30_000)).toHaveLength(1)
    spy.mockRestore()
  })
})

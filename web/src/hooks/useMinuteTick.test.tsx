import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render } from '@testing-library/react'
import { useMinuteTick } from './useMinuteTick'

function Tick({ onRender }: { onRender: (tick: number) => void }) {
  onRender(useMinuteTick())
  return null
}

describe('[UIF AC-112] useMinuteTick is one shared 30 s ticker', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts a single interval for many subscribers', () => {
    const spy = vi.spyOn(globalThis, 'setInterval')
    render(
      <>
        <Tick onRender={() => {}} />
        <Tick onRender={() => {}} />
        <Tick onRender={() => {}} />
      </>,
    )
    expect(spy).toHaveBeenCalledTimes(1)
    expect(spy.mock.calls[0][1]).toBe(30_000)
    spy.mockRestore()
  })

  it('re-renders subscribers every 30 s and not before', () => {
    const seen: number[] = []
    render(<Tick onRender={t => seen.push(t)} />)
    const initial = seen.length
    act(() => {
      vi.advanceTimersByTime(29_999)
    })
    expect(seen.length).toBe(initial)
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(seen.length).toBeGreaterThan(initial)
    expect(seen[seen.length - 1]).toBe(seen[seen.length - 2] + 1)
  })

  it('clears the interval when the last subscriber unmounts', () => {
    const spy = vi.spyOn(globalThis, 'clearInterval')
    const { unmount } = render(<Tick onRender={() => {}} />)
    unmount()
    expect(spy).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
    spy.mockRestore()
  })
})

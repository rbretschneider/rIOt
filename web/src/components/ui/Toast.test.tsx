import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { ToastProvider, useToast, type ToastApi } from './Toast'

let api: ToastApi

function Capture() {
  api = useToast()
  return null
}

function setup(props: { duration?: number; max?: number } = {}) {
  return render(
    <ToastProvider {...props}>
      <Capture />
    </ToastProvider>,
  )
}

const region = () => screen.getByRole('region', { name: 'Notifications' })

describe('[UIF AC-051] toast renders in the Notifications region', () => {
  it('mounts an empty region even before any toast is raised', () => {
    setup()
    expect(region()).toBeInTheDocument()
  })

  it('places a raised toast inside the Notifications region', () => {
    setup()
    act(() => { api.success('Saved', 'All good') })
    expect(within(region()).getByText('Saved')).toBeInTheDocument()
    expect(within(region()).getByText('All good')).toBeInTheDocument()
  })

  it('anchors the region bottom-right on the toast layer of the shared z-scale', () => {
    setup()
    expect(region().className).toContain('bottom-4')
    expect(region().className).toContain('right-4')
    expect(region().className).toContain('z-(--z-toast)')
  })

  it('falls back to a per-tone title for an empty title', () => {
    setup()
    act(() => { api.success(''); api.info(''); api.error('') })
    expect(within(region()).getByText('Done')).toBeInTheDocument()
    expect(within(region()).getByText('Notice')).toBeInTheDocument()
    expect(within(region()).getByText('Something went wrong')).toBeInTheDocument()
  })

  it('useToast throws a clear error outside a provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => render(<Capture />)).toThrow('useToast must be used within ToastProvider')
    spy.mockRestore()
  })

  it('returns a stable API object across re-renders', () => {
    const seen: ToastApi[] = []
    function Collect() {
      seen.push(useToast())
      return null
    }
    const { rerender } = render(<ToastProvider><Collect /></ToastProvider>)
    rerender(<ToastProvider><Collect /></ToastProvider>)
    expect(seen[0]).toBe(seen[seen.length - 1])
  })
})

describe('timers', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('[UIF AC-053] success auto-dismisses at 5 s', () => {
    it('is visible at 4,999 ms and gone at 5,000 ms', () => {
      setup()
      act(() => { api.success('Saved') })
      act(() => { vi.advanceTimersByTime(4999) })
      expect(screen.getByText('Saved')).toBeInTheDocument()
      act(() => { vi.advanceTimersByTime(1) })
      expect(screen.queryByText('Saved')).toBeNull()
    })
  })

  describe('[UIF AC-054] info auto-dismisses at 5 s', () => {
    it('is gone after 5,000 ms', () => {
      setup()
      act(() => { api.info('FYI') })
      act(() => { vi.advanceTimersByTime(5000) })
      expect(screen.queryByText('FYI')).toBeNull()
    })
  })

  describe('[UIF AC-055] error persists', () => {
    it('is still visible after 60,000 ms', () => {
      setup()
      act(() => { api.error('Boom') })
      act(() => { vi.advanceTimersByTime(60_000) })
      expect(screen.getByText('Boom')).toBeInTheDocument()
    })
  })

  describe('[UIF AC-057] hover pauses and resumes with remaining time', () => {
    it('pauses at 2 s, resumes with 3 s left', () => {
      setup()
      act(() => { api.success('Hovered') })
      act(() => { vi.advanceTimersByTime(2000) })
      const item = screen.getByText('Hovered').closest('li') as HTMLElement
      fireEvent.mouseEnter(item)
      act(() => { vi.advanceTimersByTime(10_000) })
      expect(screen.getByText('Hovered')).toBeInTheDocument()

      fireEvent.mouseLeave(item)
      act(() => { vi.advanceTimersByTime(2999) })
      expect(screen.getByText('Hovered')).toBeInTheDocument()
      act(() => { vi.advanceTimersByTime(1) })
      expect(screen.queryByText('Hovered')).toBeNull()
    })

    it('accumulates elapsed time across two separate hovers', () => {
      setup()
      act(() => { api.success('Twice') })
      const item = screen.getByText('Twice').closest('li') as HTMLElement
      act(() => { vi.advanceTimersByTime(1000) })
      fireEvent.mouseEnter(item)
      fireEvent.mouseLeave(item)
      act(() => { vi.advanceTimersByTime(2000) })
      fireEvent.mouseEnter(item)
      fireEvent.mouseLeave(item)
      act(() => { vi.advanceTimersByTime(1999) })
      expect(screen.getByText('Twice')).toBeInTheDocument()
      act(() => { vi.advanceTimersByTime(1) })
      expect(screen.queryByText('Twice')).toBeNull()
    })
  })

  describe('[UIF AC-058] focus pauses', () => {
    it('stays visible for 10 s while the close button has focus, then resumes on blur', () => {
      setup()
      act(() => { api.success('Focused') })
      const close = within(region()).getByRole('button', { name: 'Dismiss notification' })
      act(() => { close.focus() })
      act(() => { vi.advanceTimersByTime(10_000) })
      expect(screen.getByText('Focused')).toBeInTheDocument()

      act(() => { close.blur() })
      act(() => { vi.advanceTimersByTime(5000) })
      expect(screen.queryByText('Focused')).toBeNull()
    })
  })

  describe('[UIF AC-142] reduced motion', () => {
    it('only animates the enter transition behind motion-safe', () => {
      setup()
      act(() => { api.success('Moving') })
      const classes = screen.getByText('Moving').closest('li')?.className ?? ''
      expect(classes).toContain('motion-safe:animate-toast-in')
      expect(classes).not.toMatch(/(^|\s)animate-toast-in/)
    })
  })

  it('honours a custom duration', () => {
    setup({ duration: 1000 })
    act(() => { api.success('Quick') })
    act(() => { vi.advanceTimersByTime(1000) })
    expect(screen.queryByText('Quick')).toBeNull()
  })
})

describe('[UIF AC-056] Dismiss notification removes toast', () => {
  it('removes only the toast whose button was activated', () => {
    setup()
    act(() => { api.error('First'); api.error('Second') })
    const second = screen.getByText('Second').closest('li') as HTMLElement
    fireEvent.click(within(second).getByRole('button', { name: 'Dismiss notification' }))
    expect(screen.queryByText('Second')).toBeNull()
    expect(screen.getByText('First')).toBeInTheDocument()
  })

  it('also dismisses programmatically by id', () => {
    setup()
    let id = 0
    act(() => { id = api.error('Gone soon') })
    act(() => { api.dismiss(id) })
    expect(screen.queryByText('Gone soon')).toBeNull()
  })
})

describe('[UIF AC-059] newest on top', () => {
  it('lists B before A in DOM order when A was raised first', () => {
    setup()
    act(() => { api.error('A') })
    act(() => { api.error('B') })
    const titles = Array.from(region().querySelectorAll('li')).map(li => li.querySelector('p')?.textContent)
    expect(titles).toEqual(['B', 'A'])
  })
})

describe('[UIF AC-060] at most four visible', () => {
  it('evicts the oldest when a fifth toast is raised', () => {
    setup()
    act(() => { for (const n of [1, 2, 3, 4]) api.error(`Toast ${n}`) })
    expect(region().querySelectorAll('li')).toHaveLength(4)
    act(() => { api.error('Toast 5') })
    expect(screen.queryByText('Toast 1')).toBeNull()
    for (const n of [2, 3, 4, 5]) expect(screen.getByText(`Toast ${n}`)).toBeInTheDocument()
  })

  it('evicts an error toast too (no hidden queue)', () => {
    setup({ max: 1 })
    act(() => { api.error('old error') })
    act(() => { api.success('new') })
    expect(screen.queryByText('old error')).toBeNull()
    expect(screen.getByText('new')).toBeInTheDocument()
  })
})

describe('[UIF AC-061] live-region politeness', () => {
  it('puts a success toast in the polite list and gives an error toast role=alert', () => {
    setup()
    act(() => { api.success('Fine'); api.error('Bad') })
    const list = within(region()).getByRole('list')
    expect(list).toHaveAttribute('aria-live', 'polite')
    const fine = screen.getByText('Fine').closest('li') as HTMLElement
    expect(list.contains(fine)).toBe(true)
    expect(fine).not.toHaveAttribute('role', 'alert')
    const bad = screen.getByRole('alert')
    expect(bad).toHaveTextContent('Bad')
    expect(fine).not.toBe(bad)
  })
})

describe('[UIF AC-062] toast survives navigation', () => {
  it('is still visible after the router moves to another route', () => {
    function Page({ label }: { label: string }) {
      const navigate = useNavigate()
      const toast = useToast()
      return (
        <>
          <h1>{label}</h1>
          <button onClick={() => toast.success('Raised on device page')}>raise</button>
          <button onClick={() => navigate('/')}>go home</button>
        </>
      )
    }
    render(
      <MemoryRouter initialEntries={['/devices/x']}>
        <ToastProvider>
          <Routes>
            <Route path="/devices/x" element={<Page label="Device" />} />
            <Route path="/" element={<Page label="Fleet" />} />
          </Routes>
        </ToastProvider>
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'raise' }))
    fireEvent.click(screen.getByRole('button', { name: 'go home' }))
    expect(screen.getByRole('heading', { name: 'Fleet' })).toBeInTheDocument()
    expect(within(region()).getByText('Raised on device page')).toBeInTheDocument()
  })
})

describe('[UIF AC-012] focus-visible ring on the toast close button', () => {
  it('carries the shared focus-visible ring', () => {
    setup()
    act(() => { api.error('Ring') })
    expect(within(region()).getByRole('button', { name: 'Dismiss notification' }).className).toContain(
      'focus-visible:ring-2',
    )
  })
})

describe('[UIF SEC-005] long unbroken text wraps and Dismiss stays reachable', () => {
  it('wraps a multi-kilobyte title and message inside a width-capped viewport', () => {
    setup()
    const long = 'h'.repeat(5000)
    act(() => { api.error(long, long) })
    const item = within(region()).getByRole('alert')
    const paragraphs = item.querySelectorAll('p')
    expect(paragraphs).toHaveLength(2)
    for (const p of Array.from(paragraphs)) expect(p.className).toContain('wrap-anywhere')
    expect(region().className).toContain('w-80')
    expect(region().className).toContain('max-w-[calc(100vw-2rem)]')
    expect(within(item).getByRole('button', { name: 'Dismiss notification' })).toBeInTheDocument()
  })

  it('renders markup in a message as inert text, never as elements', () => {
    setup()
    act(() => { api.error('<img src=x onerror=alert(1)>', '<b>bold</b>') })
    const item = within(region()).getByRole('alert')
    expect(item.querySelector('img')).toBeNull()
    expect(item.querySelector('b')).toBeNull()
    expect(item).toHaveTextContent('<img src=x onerror=alert(1)>')
  })
})

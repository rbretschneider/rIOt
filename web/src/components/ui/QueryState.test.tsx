import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ErrorPanel, QueryState } from './QueryState'

const children = (data: string[]) => <ul>{data.map(d => <li key={d}>{d}</li>)}</ul>

describe('[UIF AC-045] loading', () => {
  it('renders a busy status region and no children while loading with no data', () => {
    const child = vi.fn(children)
    render(<QueryState query={{ isLoading: true, data: undefined }}>{child}</QueryState>)
    const status = screen.getByRole('status', { name: 'Loading' })
    expect(status).toHaveAttribute('aria-busy', 'true')
    expect(child).not.toHaveBeenCalled()
  })

  it('shows a Skeleton by default and a custom placeholder when given', () => {
    const { container, rerender } = render(
      <QueryState query={{ isLoading: true, data: undefined }}>{children}</QueryState>,
    )
    expect(container.querySelector('[aria-hidden="true"]')).not.toBeNull()
    rerender(
      <QueryState query={{ isLoading: true, data: undefined }} loading={<p>custom placeholder</p>}>
        {children}
      </QueryState>,
    )
    expect(screen.getByText('custom placeholder')).toBeInTheDocument()
  })
})

describe('[UIF AC-046] error with retry', () => {
  it('shows the message and calls refetch once when Retry is clicked', async () => {
    const refetch = vi.fn()
    render(
      <QueryState query={{ data: undefined, error: new Error('HTTP 502: Bad Gateway'), refetch }}>
        {children}
      </QueryState>,
    )
    expect(screen.getByText('HTTP 502: Bad Gateway')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(refetch).toHaveBeenCalledOnce()
  })

  it('uses the default title and accepts a custom one', () => {
    const { rerender } = render(
      <QueryState query={{ data: undefined, error: new Error('x'), refetch: vi.fn() }}>{children}</QueryState>,
    )
    expect(screen.getByRole('heading', { name: "Couldn't load data" })).toBeInTheDocument()
    rerender(
      <QueryState
        query={{ data: undefined, error: new Error('x'), refetch: vi.fn() }}
        errorTitle="Couldn't load devices"
      >
        {children}
      </QueryState>,
    )
    expect(screen.getByRole('heading', { name: "Couldn't load devices" })).toBeInTheDocument()
  })

  it('announces the failure with role=alert', () => {
    render(<QueryState query={{ data: undefined, error: new Error('x'), refetch: vi.fn() }}>{children}</QueryState>)
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('omits the Retry button when the query cannot refetch', () => {
    render(<QueryState query={{ data: undefined, error: new Error('x') }}>{children}</QueryState>)
    expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull()
  })

  it('falls back to the generic message for a message-less error', () => {
    render(<QueryState query={{ data: undefined, error: {} }}>{children}</QueryState>)
    expect(screen.getByText('Something went wrong. Please try again.')).toBeInTheDocument()
  })

  it('delegates to renderError when provided', async () => {
    const refetch = vi.fn()
    render(
      <QueryState
        query={{ data: undefined, error: new Error('nope'), refetch }}
        renderError={(e, retry) => <button onClick={retry}>custom {(e as Error).message}</button>}
      >
        {children}
      </QueryState>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'custom nope' }))
    expect(refetch).toHaveBeenCalledOnce()
  })
})

describe('[UIF AC-047] empty', () => {
  it('renders the empty content, not the children, when data satisfies isEmpty', () => {
    const child = vi.fn(children)
    render(
      <QueryState query={{ data: [] as string[] }} isEmpty={d => d.length === 0} empty={<p>Nothing yet</p>}>
        {child}
      </QueryState>,
    )
    expect(screen.getByText('Nothing yet')).toBeInTheDocument()
    expect(child).not.toHaveBeenCalled()
  })
})

describe('[UIF AC-048] data', () => {
  it('renders the children with the data', () => {
    render(
      <QueryState query={{ data: ['a', 'b'] }} isEmpty={d => d.length === 0} empty={<p>Nothing yet</p>}>
        {children}
      </QueryState>,
    )
    expect(screen.getByText('a')).toBeInTheDocument()
    expect(screen.getByText('b')).toBeInTheDocument()
    expect(screen.queryByText('Nothing yet')).toBeNull()
  })

  it('renders the children when no isEmpty predicate is given', () => {
    render(<QueryState query={{ data: [] as string[] }}>{d => <p>{d.length} items</p>}</QueryState>)
    expect(screen.getByText('0 items')).toBeInTheDocument()
  })
})

describe('[UIF AC-049] stale data survives a background error', () => {
  it('keeps rendering the children and shows no error panel', () => {
    render(
      <QueryState query={{ data: ['kept'], error: new Error('background failure'), refetch: vi.fn() }}>
        {children}
      </QueryState>,
    )
    expect(screen.getByText('kept')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByText('background failure')).toBeNull()
  })
})

describe('[UIF AC-050] error beats empty', () => {
  it('renders the error panel and not the empty content when there is no data', () => {
    render(
      <QueryState
        query={{ data: undefined, error: new Error('HTTP 500'), refetch: vi.fn() }}
        isEmpty={() => true}
        empty={<p>Add your first device</p>}
      >
        {children}
      </QueryState>,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('HTTP 500')
    expect(screen.queryByText('Add your first device')).toBeNull()
  })
})

describe('[UIF AC-012] focus-visible ring on Retry', () => {
  it('Retry carries the shared focus-visible ring', () => {
    render(<ErrorPanel error={new Error('x')} onRetry={() => {}} />)
    expect(screen.getByRole('button', { name: 'Retry' }).className).toContain('focus-visible:ring-2')
  })
})

describe('[UIF SEC-005] long unbroken text wraps in ErrorPanel', () => {
  it('wraps a multi-kilobyte message and keeps Retry reachable', () => {
    const long = 'e'.repeat(5000)
    render(<ErrorPanel error={new Error(long)} onRetry={() => {}} />)
    expect(screen.getByText(long).className).toContain('break-words')
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })
})

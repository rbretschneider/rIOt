import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EmptyState } from './EmptyState'

describe('[UIF AC-043] title, description and action', () => {
  it('shows the title and description and calls the action handler on click', async () => {
    const onClick = vi.fn()
    render(
      <EmptyState
        title="No probes yet"
        description="Probes check that a service is reachable."
        action={{ label: 'Add probe', onClick }}
      />,
    )
    expect(screen.getByRole('heading', { name: 'No probes yet' })).toBeInTheDocument()
    expect(screen.getByText('Probes check that a service is reachable.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Add probe' }))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it('renders only the title when nothing else is given', () => {
    render(<EmptyState title="Nothing here" />)
    expect(screen.getByRole('heading', { name: 'Nothing here' })).toBeInTheDocument()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('renders custom children such as a back link', () => {
    render(
      <EmptyState title="Device not found">
        <a href="/">Back to Fleet</a>
      </EmptyState>,
    )
    expect(screen.getByRole('link', { name: 'Back to Fleet' })).toBeInTheDocument()
  })

  it('renders the action as a secondary Button primitive', () => {
    render(<EmptyState title="t" action={{ label: 'Go', onClick: () => {} }} />)
    expect(screen.getByRole('button', { name: 'Go' })).toHaveAttribute('data-variant', 'secondary')
  })

  it('renders an optional icon', () => {
    render(<EmptyState title="t" icon={<svg data-testid="icon" />} />)
    expect(screen.getByTestId('icon')).toBeInTheDocument()
  })
})

describe('[UIF SEC-005] long unbroken text wraps in EmptyState', () => {
  it('wraps a multi-kilobyte title and description and keeps the action reachable', () => {
    const long = 'z'.repeat(5000)
    render(<EmptyState title={long} description={long} action={{ label: 'Retry', onClick: () => {} }} />)
    expect(screen.getByRole('heading').className).toMatch(/wrap-anywhere|break-words/)
    expect(screen.getByText(long, { selector: 'div' }).className).toContain('break-words')
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })
})

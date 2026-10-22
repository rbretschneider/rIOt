import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import PatchBadge from './PatchBadge'
import type { PatchBadge as PatchBadgeData } from '../api/client'

const BADGE: PatchBadgeData = {
  kind: 'needs_you',
  label: 'Needs you',
  explanation: {
    automatic: 'No.',
    reboot: 'Manual: you must reboot this host.',
    needs_you: [
      { code: 'auto_patch_off', text: 'Automatic patching is off for this host.', action: 'Turn on Auto-patch for this host.' },
      { code: 'last_run_failed', text: 'The last patch run failed: <b>boom</b>', action: 'Run it again.', command_id: '11111111-1111-1111-1111-111111111111' },
    ],
    needs_you_summary: '2 items need you.',
    as_of: '2026-10-19 02:00 UTC (5 min ago)',
    notes: ['Some checks need a newer agent (v2.75.0 or later).'],
  },
}

describe('PatchBadge', () => {
  it('shows the server label as text', () => {
    render(<PatchBadge badge={BADGE} />)
    expect(screen.getByTestId('patch-badge')).toHaveTextContent('Needs you')
  })

  it('shows the four explanation lines in order on hover', () => {
    render(<PatchBadge badge={BADGE} />)
    fireEvent.mouseEnter(screen.getByTestId('patch-badge'))
    const tip = screen.getByRole('tooltip')
    const text = tip.textContent ?? ''
    const order = ['Automatic patching: No.', 'Reboot: Manual', 'Needs you: 2 items need you.', 'As of: 2026-10-19 02:00 UTC']
    const idx = order.map(s => text.indexOf(s))
    expect(idx.every(i => i >= 0), `all four lines must be present: ${text}`).toBe(true)
    expect([...idx].sort((a, b) => a - b), 'lines must appear in order').toEqual(idx)
  })

  it('opens on keyboard focus, links the popover with aria-describedby and closes on Escape', () => {
    render(<PatchBadge badge={BADGE} />)
    const el = screen.getByTestId('patch-badge')
    expect(el).toHaveAttribute('tabindex', '0')
    fireEvent.focus(el)
    const tip = screen.getByRole('tooltip')
    expect(el.getAttribute('aria-describedby')).toBe(tip.id)
    fireEvent.keyDown(el, { key: 'Escape' })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  it('renders agent-influenced text as text, never as HTML', () => {
    render(<PatchBadge badge={BADGE} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    expect(screen.getByRole('tooltip').querySelector('b')).toBeNull()
    expect(screen.getByRole('tooltip')).toHaveTextContent('<b>boom</b>')
  })

  it('lists the notes', () => {
    render(<PatchBadge badge={BADGE} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    expect(screen.getByRole('tooltip')).toHaveTextContent('Some checks need a newer agent (v2.75.0 or later).')
  })
})

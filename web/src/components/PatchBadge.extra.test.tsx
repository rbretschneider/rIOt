import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import PatchBadge from './PatchBadge'
import type { PatchBadge as PatchBadgeData } from '../api/client'

function badge(over: Partial<PatchBadgeData> = {}, explanation: Partial<PatchBadgeData['explanation']> = {}): PatchBadgeData {
  return {
    kind: 'manual',
    label: 'Manual',
    explanation: {
      automatic: 'No.',
      reboot: 'Not needed.',
      needs_you: [],
      needs_you_summary: 'Nothing.',
      info: [{ code: 'auto_patch_off', text: 'Automatic patching is off for this host.', action: 'Turn on Auto-patch for this host.' }],
      as_of: '2026-10-26 02:00 UTC (just now)',
      notes: [],
      ...explanation,
    },
    ...over,
  }
}

describe('[EXTRA-UPD-001 AC-050] the manual badge', () => {
  it('shows the label in neutral styling, not red', () => {
    render(<PatchBadge badge={badge()} />)
    const el = screen.getByTestId('patch-badge')
    expect(el).toHaveTextContent('Manual')
    expect(el).toHaveAttribute('data-kind', 'manual')
    expect(el.className).not.toMatch(/red-/)
    expect(el.className).toMatch(/gray-/)
  })

  it('falls back to the neutral style for a kind this dashboard does not know', () => {
    render(<PatchBadge badge={badge({ kind: 'from_the_future' as never, label: 'Later' })} />)
    expect(screen.getByTestId('patch-badge').className).not.toMatch(/red-/)
  })

  it('still shows needs_you in red', () => {
    render(<PatchBadge badge={badge({ kind: 'needs_you', label: 'Needs you' })} />)
    expect(screen.getByTestId('patch-badge').className).toMatch(/red-/)
  })
})

describe('[EXTRA-UPD-001 AC-046 AC-050] information items', () => {
  it('lists them under "Information", not under "Needs you", and the summary reads Nothing.', () => {
    render(<PatchBadge badge={badge()} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    const tip = screen.getByRole('tooltip')
    expect(tip).toHaveTextContent('Needs you: Nothing.')
    const info = screen.getByTestId('patch-badge-info')
    expect(info).toHaveTextContent('Information:')
    expect(info).toHaveTextContent('Automatic patching is off for this host.')
    expect(info).toHaveTextContent('Turn on Auto-patch for this host.')
    expect(info.textContent).not.toContain('Needs you')
    // The action list under "Needs you" is not rendered for information items.
    expect(tip.querySelectorAll('ul')).toHaveLength(1)
  })

  it('renders no Information block when there are none, or when an older server omits the field', () => {
    const { rerender } = render(<PatchBadge badge={badge({}, { info: [] })} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    expect(screen.queryByTestId('patch-badge-info')).not.toBeInTheDocument()
    rerender(<PatchBadge badge={badge({}, { info: undefined })} />)
    expect(screen.queryByTestId('patch-badge-info')).not.toBeInTheDocument()
  })

  it('keeps one item per updater when two updaters raise the same code', () => {
    const b = badge({ kind: 'needs_you', label: 'Needs you' }, {
      needs_you: [
        { code: 'extra_update_not_applied', updater: 'dietpi', text: 'DietPi 9 to 10 is waiting; not applied: not opted in.', action: 'Add dietpi.' },
        { code: 'extra_update_not_applied', updater: 'other', text: 'other update is waiting; not applied: not opted in.', action: 'Add other.' },
      ],
      needs_you_summary: '2 items need you.',
    })
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    render(<PatchBadge badge={b} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    expect(screen.getByRole('tooltip')).toHaveTextContent('DietPi 9 to 10 is waiting')
    expect(screen.getByRole('tooltip')).toHaveTextContent('other update is waiting')
    const duplicateKeys = spy.mock.calls.filter(args => args.some(a => String(a).includes('same key')))
    spy.mockRestore()
    expect(duplicateKeys, 'duplicate React keys').toEqual([])
  })

  it('renders item text as text, never as HTML', () => {
    render(<PatchBadge badge={badge({}, { info: [{ code: 'extra_update_unknown', updater: 'dietpi', text: '<b>boom</b>', action: '<i>x</i>' }] })} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    const info = screen.getByTestId('patch-badge-info')
    expect(info.querySelector('b')).toBeNull()
    expect(info.querySelector('i')).toBeNull()
    expect(info).toHaveTextContent('<b>boom</b>')
  })
})

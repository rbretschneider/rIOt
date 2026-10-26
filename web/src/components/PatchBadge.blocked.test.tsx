import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import PatchBadge from './PatchBadge'
import type { PatchBadge as PatchBadgeData } from '../api/client'

// DIETPI-PATH-001 AC-017: the dashboard renders the blocked kind as an amber
// "Blocked" badge with its explanation. Item text is server-written and shown
// as React text only.

const BLOCKED_TEXT = "DietPi update blocked: rIOt could not isolate /run/dietpi; this host's mount layout prevented rIOt from isolating the step"

function blockedBadge(over: Partial<PatchBadgeData['explanation']> = {}): PatchBadgeData {
  return {
    kind: 'blocked',
    label: 'Blocked',
    explanation: {
      automatic: 'Yes. Any time (within the 360-minute cooldown).',
      reboot: 'Not needed.',
      needs_you: [],
      needs_you_summary: 'Nothing.',
      blocked: [{ code: 'extra_blocked', updater: 'dietpi', text: BLOCKED_TEXT, action: 'Run riot-agent doctor on this host.' }],
      info: [],
      as_of: '2026-10-26 02:00 UTC (just now)',
      notes: [],
      ...over,
    },
  }
}

describe('[DIETPI-PATH-001 AC-017] the Blocked badge', () => {
  it('shows an amber "Blocked" label, never red', () => {
    render(<PatchBadge badge={blockedBadge()} />)
    const el = screen.getByTestId('patch-badge')
    expect(el).toHaveTextContent('Blocked')
    expect(el).toHaveAttribute('data-kind', 'blocked')
    expect(el.className).toMatch(/amber-/)
    expect(el.className).not.toMatch(/red-/)
  })

  it('lists the blocked item and its action under "Blocked", and says Needs you: Nothing.', () => {
    render(<PatchBadge badge={blockedBadge()} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    const tip = screen.getByRole('tooltip')
    expect(tip).toHaveTextContent('Needs you: Nothing.')
    const blocked = screen.getByTestId('patch-badge-blocked')
    expect(blocked).toHaveTextContent('Blocked:')
    expect(blocked).toHaveTextContent(BLOCKED_TEXT)
    expect(blocked).toHaveTextContent('Run riot-agent doctor on this host.')
    expect(blocked.textContent).not.toContain('Needs you')
  })

  it('never says the update will be applied automatically', () => {
    render(<PatchBadge badge={blockedBadge()} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    expect(screen.getByRole('tooltip').textContent ?? '').not.toMatch(/will be applied automatically/)
  })

  it('renders item text as text, never as HTML', () => {
    render(<PatchBadge badge={blockedBadge({ blocked: [{ code: 'extra_blocked', updater: 'dietpi', text: '<b>boom</b>', action: '<i>x</i>' }] })} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    const blocked = screen.getByTestId('patch-badge-blocked')
    expect(blocked.querySelector('b')).toBeNull()
    expect(blocked.querySelector('i')).toBeNull()
    expect(blocked).toHaveTextContent('<b>boom</b>')
  })

  it('renders no Blocked block when the list is empty or an older server omits it', () => {
    const { rerender } = render(<PatchBadge badge={blockedBadge({ blocked: [] })} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    expect(screen.queryByTestId('patch-badge-blocked')).not.toBeInTheDocument()
    rerender(<PatchBadge badge={blockedBadge({ blocked: undefined })} />)
    expect(screen.queryByTestId('patch-badge-blocked')).not.toBeInTheDocument()
  })

  it('keeps one item per updater and no duplicate React keys', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    render(<PatchBadge badge={blockedBadge({ blocked: [
      { code: 'extra_blocked', updater: 'dietpi', text: 'DietPi update blocked: a', action: 'x' },
      { code: 'extra_blocked', updater: 'other', text: 'other update blocked: b', action: 'y' },
    ] })} />)
    fireEvent.focus(screen.getByTestId('patch-badge'))
    expect(screen.getByTestId('patch-badge-blocked')).toHaveTextContent('other update blocked: b')
    const duplicateKeys = spy.mock.calls.filter(args => args.some(a => String(a).includes('same key')))
    spy.mockRestore()
    expect(duplicateKeys, 'duplicate React keys').toEqual([])
  })

  it('shows a needs_you badge with a blocked reason beside it (AC-016)', () => {
    render(<PatchBadge badge={{ ...blockedBadge({ needs_you: [{ code: 'manual_reboot_needed', text: 'A reboot is required now.', action: 'Reboot this host now.' }], needs_you_summary: '1 item needs you.' }), kind: 'needs_you', label: 'Needs you' }} />)
    const el = screen.getByTestId('patch-badge')
    expect(el.className).toMatch(/red-/)
    fireEvent.focus(el)
    expect(screen.getByRole('tooltip')).toHaveTextContent('A reboot is required now.')
    expect(screen.getByTestId('patch-badge-blocked')).toHaveTextContent(BLOCKED_TEXT)
  })
})

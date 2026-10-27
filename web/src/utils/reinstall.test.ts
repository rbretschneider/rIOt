import { describe, expect, it } from 'vitest'
import {
  REINSTALL_ACTION_LABEL,
  REINSTALL_ACTIVITY_LABEL,
  REINSTALL_ACTOR_PHRASE,
  isReinstall,
  isReinstallActivity,
  reinstallOutcomeLabel,
  reinstallOutcomeTone,
} from './reinstall'

describe('EXPECT-008 [AC-022] isReinstall and outcome labels', () => {
  it('fixes the wording the FRD names', () => {
    expect(REINSTALL_ACTION_LABEL).toBe('Agent reinstall')
    expect(REINSTALL_ACTOR_PHRASE).toBe('by installer')
    expect(REINSTALL_ACTIVITY_LABEL).toBe('Reinstalling agent')
  })

  it('recognises a command row with origin installer and action agent_update', () => {
    expect(isReinstall({ origin: 'installer', action: 'agent_update' })).toBe(true)
  })

  it.each([
    ['a self-update', { origin: 'agent_self_update' as const, action: 'agent_update' }],
    ['an operator agent update', { origin: 'user' as const, action: 'agent_update' }],
    ['an installer origin on another action', { origin: 'installer' as const, action: 'reboot' }],
    ['a row without an origin (older server)', { origin: undefined, action: 'agent_update' }],
  ])('does not recognise %s', (_name, row) => {
    expect(isReinstall(row), 'only origin installer on agent_update is a reinstall record').toBe(false)
  })

  it('does not recognise a missing row', () => {
    expect(isReinstall(null)).toBe(false)
    expect(isReinstall(undefined)).toBe(false)
  })

  it('recognises an activity of a reinstall window and nothing else', () => {
    expect(isReinstallActivity({ origin: 'installer', action: 'agent_update' })).toBe(true)
    expect(isReinstallActivity({ origin: 'agent_self_update', action: 'agent_update' })).toBe(false)
    expect(isReinstallActivity({ origin: '', action: 'agent_update' })).toBe(false)
    expect(isReinstallActivity({ origin: undefined, action: 'agent_update' })).toBe(false)
    expect(isReinstallActivity({ origin: 'installer', action: 'reboot' })).toBe(false)
    expect(isReinstallActivity(undefined)).toBe(false)
  })

  it.each([
    ['running', 'In progress'],
    ['succeeded', 'Succeeded'],
    ['timed_out', 'Not confirmed'],
  ])('words %s as "%s"', (status, text) => {
    expect(reinstallOutcomeLabel(status)).toBe(text)
  })

  it('falls back to the status label for an unknown status instead of showing a raw value', () => {
    expect(reinstallOutcomeLabel('hasOwnProperty')).not.toBe('')
    expect(reinstallOutcomeLabel(undefined)).not.toBe('undefined')
  })

  it('shows a not-confirmed reinstall as a warning, a success as success, and a live one as info', () => {
    expect(reinstallOutcomeTone('timed_out')).toBe('warn')
    expect(reinstallOutcomeTone('succeeded')).toBe('success')
    expect(reinstallOutcomeTone('running')).toBe('info')
  })
})

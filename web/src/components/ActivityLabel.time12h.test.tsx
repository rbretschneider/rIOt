import { describe, expect, it } from 'vitest'
import { screen } from '@testing-library/react'
import { HOUR_CYCLE_STORAGE_KEY, TIME_ZONE_STORAGE_KEY } from '../contexts/TimePreferenceProvider'
import { renderWithProviders } from '../test/renderWithProviders'
import type { DeviceActivity } from '../types/models'
import ActivityLabel from './ActivityLabel'
import AgentUpdatingVersion from './AgentUpdatingVersion'

// TIME-12H-001. Instant 2026-10-27T17:05:09Z, server zone America/New_York, default preference (Server, 12-hour).
const EXAMPLE = '2026-10-27T17:05:09Z'
const BACK_BY = '2026-10-27T17:20:00Z'

function activity(over: Partial<DeviceActivity> = {}): DeviceActivity {
  return {
    device_id: 'dev-1',
    kind: 'host',
    phase: 'return',
    action: 'reboot',
    command_id: 'cmd-1',
    actor_phrase: 'by ryan',
    started_at: EXAMPLE,
    back_by: null,
    waiting_dpkg_lock: false,
    progress: null,
    agent_from_version: '',
    target: '',
    docker_actions: 0,
    covers_offline: true,
    device_online: true,
    escalation: null,
    powered_off_at: null,
    ...over,
  }
}

const defaults = { storage: null, serverZone: 'America/New_York' }
const hour24 = {
  storage: {
    getItem: (key: string) => (key === TIME_ZONE_STORAGE_KEY ? 'Server' : key === HOUR_CYCLE_STORAGE_KEY ? '24h' : null),
    setItem: () => {},
  },
  serverZone: 'America/New_York',
}

describe('TIME-12H-001 [AC-034] activity labels carry labelled 12-hour clock times', () => {
  it('writes "Rebooting, back by 1:20 PM EDT" for a rebooting host in its return phase', () => {
    renderWithProviders(<ActivityLabel activity={activity({ back_by: BACK_BY })} />, { timeZone: defaults })
    expect(screen.getByText('Rebooting, back by 1:20 PM EDT')).toBeInTheDocument()
  })

  it('writes "Patched, settling until 1:20 PM EDT" for an online host after a patch', () => {
    renderWithProviders(
      <ActivityLabel activity={activity({ action: 'os_update', back_by: BACK_BY, device_online: true })} />,
      { timeZone: defaults },
    )
    expect(screen.getByText('Patched, settling until 1:20 PM EDT')).toBeInTheDocument()
  })

  it('writes "Powered off by ryan at 1:05 PM EDT" for a powered-off device', () => {
    renderWithProviders(
      <ActivityLabel activity={activity({ kind: 'powered_off', phase: 'in_progress', powered_off_at: EXAMPLE })} />,
      { timeZone: defaults },
    )
    expect(screen.getByText('Powered off by ryan at 1:05 PM EDT')).toBeInTheDocument()
  })

  it('writes "Rebooting, back by 13:20 EDT" with the 24-hour cycle', () => {
    renderWithProviders(<ActivityLabel activity={activity({ back_by: BACK_BY })} />, { timeZone: hour24 })
    expect(screen.getByText('Rebooting, back by 13:20 EDT')).toBeInTheDocument()
  })

  it('falls back to UTC with the UTC label while the server zone is unknown', () => {
    renderWithProviders(<ActivityLabel activity={activity({ back_by: BACK_BY })} />, { timeZone: { storage: null } })
    expect(screen.getByText('Rebooting, back by 5:20 PM UTC')).toBeInTheDocument()
  })

  it('shows an em dash for an unparseable back_by instead of throwing', () => {
    renderWithProviders(<ActivityLabel activity={activity({ back_by: 'not-a-date' })} />, { timeZone: defaults })
    expect(screen.getByText('Rebooting, back by —')).toBeInTheDocument()
  })
})

describe('TIME-12H-001 [AC-035] fleet-row and agent-update titles end with the labelled start time', () => {
  it('ends the compact fleet-row title with "· started 1:05 PM EDT"', () => {
    renderWithProviders(<ActivityLabel activity={activity({ back_by: BACK_BY })} compact />, { timeZone: defaults })
    expect(screen.getByTestId('activity-label').getAttribute('title')).toBe(
      'Rebooting, back by 1:20 PM EDT by ryan · started 1:05 PM EDT',
    )
  })

  it('ends the agent-update title with "· started 1:05 PM EDT"', () => {
    renderWithProviders(
      <AgentUpdatingVersion activity={activity({ action: 'agent_update', phase: 'in_progress', agent_from_version: '2.80.0' })} newestAgentVersion="2.83.0" />,
      { timeZone: defaults },
    )
    const title = screen.getByTestId('agent-updating').getAttribute('title')
    expect(title).toBe('Updating agent 2.80.0 → 2.83.0 by ryan · started 1:05 PM EDT')
  })

  it('writes 24-hour titles with the zone label too', () => {
    renderWithProviders(<ActivityLabel activity={activity({ back_by: BACK_BY })} compact />, { timeZone: hour24 })
    expect(screen.getByTestId('activity-label').getAttribute('title')?.endsWith('· started 13:05 EDT')).toBe(true)
  })
})

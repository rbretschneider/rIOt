import { describe, expect, it } from 'vitest'
import type { ActivityResponse, DeviceActivity } from '../types/models'
import {
  activityLabel,
  activityLink,
  applyActivityUpdate,
  indexActivities,
  isEscalated,
  isExpectedOffline,
  showsActorLine,
} from './activity'

// Times are formatted by an injected function so no test depends on a clock or a zone.
const ctx = {
  newestAgentVersion: '2.78.0',
  formatTime: (iso: string) => iso.slice(11, 16),
}

function activity(over: Partial<DeviceActivity> = {}): DeviceActivity {
  return {
    device_id: 'dev-1',
    kind: 'host',
    phase: 'in_progress',
    action: 'reboot',
    command_id: '1a2b3c4d-0000-0000-0000-000000000000',
    actor_phrase: 'by ryan',
    started_at: '2026-10-26T14:00:00Z',
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

describe('[AC-038] label vocabulary', () => {
  it('labels a queued host or agent command "<Action> queued"', () => {
    expect(activityLabel(activity({ phase: 'queued', action: 'reboot' }), ctx)).toBe('Reboot queued')
    expect(activityLabel(activity({ kind: 'agent', phase: 'queued', action: 'agent_update' }), ctx)).toBe('Agent update queued')
    expect(activityLabel(activity({ phase: 'queued', action: 'os_update' }), ctx)).toBe('OS update queued')
  })

  it('labels a running reboot "Rebooting"', () => {
    expect(activityLabel(activity(), ctx)).toBe('Rebooting')
  })

  it('labels a reboot waiting for the dpkg lock "Rebooting (waiting for dpkg lock)"', () => {
    expect(activityLabel(activity({ waiting_dpkg_lock: true }), ctx)).toBe('Rebooting (waiting for dpkg lock)')
  })

  it('labels a reboot in its return phase with the back-by time', () => {
    const a = activity({ phase: 'return', back_by: '2026-10-26T14:32:00Z' })
    expect(activityLabel(a, ctx)).toBe('Rebooting, back by 14:32')
  })

  it('labels a return phase without a back-by time without a dangling clause', () => {
    expect(activityLabel(activity({ phase: 'return', back_by: null }), ctx)).toBe('Rebooting')
  })

  it('labels a running shutdown "Shutting down", with the dpkg variant', () => {
    expect(activityLabel(activity({ action: 'shutdown' }), ctx)).toBe('Shutting down')
    expect(activityLabel(activity({ action: 'shutdown', waiting_dpkg_lock: true }), ctx)).toBe(
      'Shutting down (waiting for dpkg lock)',
    )
  })

  it('keeps "Shutting down" for a succeeded shutdown whose device is still online (X-4)', () => {
    expect(activityLabel(activity({ action: 'shutdown', phase: 'return', device_online: true }), ctx)).toBe('Shutting down')
  })

  it('labels a powered-off device with the actor phrase and the time', () => {
    const a = activity({
      kind: 'powered_off',
      phase: 'powered_off',
      action: 'shutdown',
      powered_off_at: '2026-10-26T14:02:00Z',
    })
    expect(activityLabel(a, ctx)).toBe('Powered off by ryan at 14:02')
  })

  it('labels a running os_update "Patching", with the reported percentage when there is one', () => {
    expect(activityLabel(activity({ action: 'os_update' }), ctx)).toBe('Patching')
    expect(activityLabel(activity({ action: 'os_update', progress: 40 }), ctx)).toBe('Patching (40%)')
  })

  it('labels a progress of zero as "Patching (0%)", not as no progress', () => {
    expect(activityLabel(activity({ action: 'os_update', progress: 0 }), ctx)).toBe('Patching (0%)')
  })

  it('labels an os_update in its return phase while the device is offline "Rebooting after patch, back by <t>"', () => {
    const a = activity({ action: 'os_update', phase: 'return', device_online: false, back_by: '2026-10-26T02:45:00Z' })
    expect(activityLabel(a, ctx)).toBe('Rebooting after patch, back by 02:45')
  })

  it('labels an os_update in its return phase while the device is online "Patched, settling until <t>" (X-4)', () => {
    const a = activity({ action: 'os_update', phase: 'return', device_online: true, back_by: '2026-10-26T02:31:00Z' })
    expect(activityLabel(a, ctx)).toBe('Patched, settling until 02:31')
  })

  it('labels a running agent_update with the from and to versions', () => {
    const a = activity({ kind: 'agent', action: 'agent_update', agent_from_version: '2.77.0' })
    expect(activityLabel(a, ctx)).toBe('Updating agent 2.77.0 → 2.78.0')
  })

  it('falls back to "Updating agent" when either version is unknown', () => {
    const noFrom = activity({ kind: 'agent', action: 'agent_update', agent_from_version: '' })
    expect(activityLabel(noFrom, ctx)).toBe('Updating agent')
    const noTo = activity({ kind: 'agent', action: 'agent_update', agent_from_version: '2.77.0' })
    expect(activityLabel(noTo, { ...ctx, newestAgentVersion: '' })).toBe('Updating agent')
  })

  it('labels an agent_update in its return phase "Restarting agent, back by <t>"', () => {
    const a = activity({ kind: 'agent', action: 'agent_update', phase: 'return', back_by: '2026-10-26T10:10:00Z' })
    expect(activityLabel(a, ctx)).toBe('Restarting agent, back by 10:10')
  })

  it.each([
    ['docker_update', 'updating'],
    ['docker_bulk_update', 'updating'],
    ['docker_restart', 'restarting'],
    ['docker_stop', 'stopping'],
    ['docker_stack_redeploy', 'recreating stack'],
    ['docker_stack_restart', 'restarting stack'],
    ['docker_stack_up', 'starting stack'],
    ['docker_stack_down', 'stopping stack'],
  ])('labels one %s action "Docker: %s <target>"', (action, verb) => {
    const a = activity({ kind: 'docker', action, target: 'media', docker_actions: 1 })
    expect(activityLabel(a, ctx)).toBe(`Docker: ${verb} media`)
  })

  it('labels two or more Docker actions "Docker: <N> actions in progress"', () => {
    const a = activity({ kind: 'docker', action: 'docker_update', target: 'plex', docker_actions: 3 })
    expect(activityLabel(a, ctx)).toBe('Docker: 3 actions in progress')
  })

  it('labels an escalated failure "<Action> failed: offline" for failed, timed_out and no_result', () => {
    for (const reason of ['failed', 'timed_out', 'no_result'] as const) {
      const a = activity({
        kind: 'escalated',
        phase: 'escalated',
        escalation: { reason, bound: '35m', at: '2026-10-26T14:40:00Z' },
      })
      expect(activityLabel(a, ctx), `reason ${reason}`).toBe('Reboot failed: offline')
    }
  })

  it('labels an escalated not_back "Not back <bound> after <Action>"', () => {
    const a = activity({
      kind: 'escalated',
      phase: 'escalated',
      action: 'agent_update',
      escalation: { reason: 'not_back', bound: '10m', at: '2026-10-26T10:11:00Z' },
    })
    expect(activityLabel(a, ctx)).toBe('Not back 10m after Agent update')
  })

  it('humanizes an action the label table does not know instead of showing the raw value', () => {
    const a = activity({ kind: 'escalated', phase: 'escalated', action: 'docker_stack_redeploy', escalation: { reason: 'failed', bound: '', at: '' } })
    expect(activityLabel(a, ctx)).toBe('Docker stack redeploy failed: offline')
  })

  it('renders text from the server as given, with no markup interpretation', () => {
    const a = activity({ kind: 'docker', action: 'docker_update', target: '<img src=x onerror=alert(1)>', docker_actions: 1 })
    expect(activityLabel(a, ctx)).toBe('Docker: updating <img src=x onerror=alert(1)>')
  })
})

describe('[AC-038] the actor line', () => {
  it('is shown for every kind except powered off, whose label already names the actor', () => {
    expect(showsActorLine(activity())).toBe(true)
    expect(showsActorLine(activity({ kind: 'escalated' }))).toBe(true)
    expect(showsActorLine(activity({ kind: 'docker' }))).toBe(true)
    expect(showsActorLine(activity({ kind: 'powered_off' }))).toBe(false)
  })
})

describe('[AC-039] expected offline versus offline', () => {
  it('counts a device that is offline inside a host window as expected', () => {
    expect(isExpectedOffline(activity({ phase: 'return', covers_offline: true }), 'offline')).toBe(true)
  })

  it('counts a device that is offline inside an agent window as expected', () => {
    expect(isExpectedOffline(activity({ kind: 'agent', action: 'agent_update', phase: 'return' }), 'offline')).toBe(true)
  })

  it('counts a powered-off device as expected, whatever covers_offline says', () => {
    expect(isExpectedOffline(activity({ kind: 'powered_off', phase: 'powered_off', covers_offline: false }), 'offline')).toBe(true)
  })

  it('does not count an escalated device as expected: a real outage stays offline', () => {
    expect(isExpectedOffline(activity({ kind: 'escalated', phase: 'escalated', covers_offline: false }), 'offline')).toBe(false)
  })

  it('does not count a Docker activity as expected offline, even if covers_offline were set', () => {
    expect(isExpectedOffline(activity({ kind: 'docker', covers_offline: true }), 'offline')).toBe(false)
  })

  it('does not count a queued command, which covers nothing', () => {
    expect(isExpectedOffline(activity({ phase: 'queued', covers_offline: false }), 'offline')).toBe(false)
  })

  it('does not count a device that is not offline', () => {
    expect(isExpectedOffline(activity({ phase: 'return' }), 'online')).toBe(false)
    expect(isExpectedOffline(activity({ phase: 'return' }), 'warning')).toBe(false)
  })

  it('does not count a device without an activity', () => {
    expect(isExpectedOffline(undefined, 'offline')).toBe(false)
  })
})

describe('[AC-041] the escalated state', () => {
  it('is recognised so the device is shown with failure styling', () => {
    expect(isEscalated(activity({ kind: 'escalated' }))).toBe(true)
    expect(isEscalated(activity())).toBe(false)
    expect(isEscalated(undefined)).toBe(false)
  })
})

describe('[AC-042] the link to the command', () => {
  it('links a Docker action to the Docker activity page with both ids URL-encoded', () => {
    const a = activity({ kind: 'docker', action: 'docker_update', device_id: 'dev/1&x', command_id: 'c d?e' })
    expect(activityLink(a)).toEqual({
      type: 'route',
      to: '/docker/activity?host=dev%2F1%26x&command=c%20d%3Fe',
    })
  })

  it('opens host and agent commands in a modal, since no detail page exists (X-3)', () => {
    expect(activityLink(activity())).toEqual({ type: 'modal' })
    expect(activityLink(activity({ kind: 'agent', action: 'agent_update' }))).toEqual({ type: 'modal' })
  })

  it('links an escalated Docker command to the Docker activity page', () => {
    const a = activity({ kind: 'escalated', action: 'docker_stack_redeploy' })
    expect(activityLink(a)).toEqual({
      type: 'route',
      to: '/docker/activity?host=dev-1&command=1a2b3c4d-0000-0000-0000-000000000000',
    })
  })

  it('has no link when the activity names no command', () => {
    expect(activityLink(activity({ command_id: '' }))).toBeNull()
  })
})

describe('[AC-040] applying an activity_update to the cache', () => {
  const base: ActivityResponse = {
    generated_at: '2026-10-26T14:00:00Z',
    newest_agent_version: '2.78.0',
    activities: [activity({ device_id: 'a' }), activity({ device_id: 'b', action: 'shutdown' })],
  }

  it('adds the activity of a device that had none', () => {
    const next = applyActivityUpdate(base, 'c', activity({ device_id: 'c' }))
    expect(next?.activities.map(a => a.device_id).sort()).toEqual(['a', 'b', 'c'])
  })

  it('replaces the activity of a device that had one, keeping one entry per device', () => {
    const next = applyActivityUpdate(base, 'a', activity({ device_id: 'a', action: 'os_update' }))
    expect(next?.activities.filter(a => a.device_id === 'a')).toHaveLength(1)
    expect(next?.activities.find(a => a.device_id === 'a')?.action).toBe('os_update')
  })

  it('clears the activity of a device when data is null', () => {
    const next = applyActivityUpdate(base, 'a', null)
    expect(next?.activities.map(a => a.device_id)).toEqual(['b'])
  })

  it('keeps the response-level fields', () => {
    const next = applyActivityUpdate(base, 'a', null)
    expect(next?.newest_agent_version).toBe('2.78.0')
    expect(next?.generated_at).toBe('2026-10-26T14:00:00Z')
  })

  it('does not mutate the cached response', () => {
    applyActivityUpdate(base, 'a', null)
    expect(base.activities).toHaveLength(2)
  })

  it('ignores a payload that names a different device than the message', () => {
    const next = applyActivityUpdate(base, 'a', activity({ device_id: 'b', action: 'os_update' }))
    expect(next).toBe(base)
  })

  it('accepts a cached response whose activities arrived as null (a Go nil slice)', () => {
    const emptyAsNull = { ...base, activities: null } as unknown as ActivityResponse
    const next = applyActivityUpdate(emptyAsNull, 'a', activity({ device_id: 'a' }))
    expect(next?.activities.map(a => a.device_id)).toEqual(['a'])
  })

  it('ignores a message before the first fetch has landed', () => {
    expect(applyActivityUpdate(undefined, 'a', activity({ device_id: 'a' }))).toBeUndefined()
  })

  it('ignores a message with no device id', () => {
    expect(applyActivityUpdate(base, undefined, null)).toBe(base)
  })
})

describe('[AC-024] indexing the response', () => {
  it('maps device ids to their activity', () => {
    const map = indexActivities({
      generated_at: '',
      newest_agent_version: '2.78.0',
      activities: [activity({ device_id: 'a' }), activity({ device_id: 'b' })],
    })
    expect([...map.keys()].sort()).toEqual(['a', 'b'])
  })

  it('is empty without a response', () => {
    expect(indexActivities(undefined).size).toBe(0)
  })
})

// EXPECT-002 (AD-009): an agent's own self-update names both versions from the
// record, and its escalation does too. Command windows keep the EXPECT-001 text.
function selfUpdateActivity(over: Partial<DeviceActivity> = {}): DeviceActivity {
  return activity({
    kind: 'agent',
    action: 'agent_update',
    actor_phrase: 'by agent auto-update',
    agent_from_version: '2.78.0',
    agent_to_version: '2.80.0',
    back_by: null,
    ...over,
  })
}

describe('EXPECT-002 [AC-001] self-update label names both versions', () => {
  it('labels an in-progress self-update "Updating agent <from> → <to>" from the record, not the newest version', () => {
    expect(activityLabel(selfUpdateActivity(), { ...ctx, newestAgentVersion: '9.9.9' })).toBe('Updating agent 2.78.0 → 2.80.0')
  })

  it('labels it the same while the device is offline', () => {
    expect(activityLabel(selfUpdateActivity({ device_online: false }), ctx)).toBe('Updating agent 2.78.0 → 2.80.0')
  })

  it('keeps the actor line, which carries "by agent auto-update"', () => {
    const a = selfUpdateActivity()
    expect(showsActorLine(a)).toBe(true)
    expect(a.actor_phrase).toBe('by agent auto-update')
  })

  it('leaves a command agent update on the EXPECT-001 wording (FR-029)', () => {
    const command = activity({ kind: 'agent', action: 'agent_update', agent_from_version: '2.77.0', agent_to_version: '' })
    expect(activityLabel(command, ctx)).toBe('Updating agent 2.77.0 → 2.78.0')
    expect(activityLabel({ ...command, agent_from_version: '' }, ctx)).toBe('Updating agent')
  })

  it('leaves a command agent update from an older server (no agent_to_version field) unchanged', () => {
    const command = activity({ kind: 'agent', action: 'agent_update', agent_from_version: '2.77.0' })
    expect(command.agent_to_version).toBeUndefined()
    expect(activityLabel(command, ctx)).toBe('Updating agent 2.77.0 → 2.78.0')
  })
})

describe('EXPECT-002 [AC-016] hostile or malformed versions are never shown', () => {
  it.each([
    ['markup', '<img src=x onerror=alert(1)>'],
    ['a control character', '2.78.0\u0000'],
    ['a bidi override', '2.78.0‮'],
    ['a name that is not a version', 'dev'],
    ['too many bytes', '2.78.0-' + 'a'.repeat(40)],
    ['the same version as the other end', '2.80.0'],
    ['the same version with a leading v', 'v2.80.0'],
  ])('falls back to the plain wording and never prints <from> when it is %s', (_name, from) => {
    const label = activityLabel(selfUpdateActivity({ agent_from_version: from }), { ...ctx, newestAgentVersion: '' })
    expect(label).toBe('Updating agent')
  })

  it('falls back when <to> is invalid', () => {
    expect(activityLabel(selfUpdateActivity({ agent_to_version: '2.80' }), { ...ctx, newestAgentVersion: '' })).toBe('Updating agent')
  })

  it('accepts a leading v and a build suffix', () => {
    expect(activityLabel(selfUpdateActivity({ agent_from_version: 'v2.78.0', agent_to_version: '2.80.0-rc.1' }), ctx)).toBe(
      'Updating agent v2.78.0 → 2.80.0-rc.1',
    )
  })
})

describe('EXPECT-002 [AC-008] not-back label names versions', () => {
  const escalation = (reason: 'not_back' | 'failed') => ({ reason, bound: '10m', at: '2026-10-26T14:10:00Z' })

  it('labels a not-back self-update "Not back 10m after Agent update <from> → <to>"', () => {
    const a = selfUpdateActivity({ kind: 'escalated', phase: 'escalated', covers_offline: false, escalation: escalation('not_back') })
    expect(activityLabel(a, ctx)).toBe('Not back 10m after Agent update 2.78.0 → 2.80.0')
  })

  it('labels a failed self-update with the device offline "Agent update <from> → <to> failed: offline"', () => {
    const a = selfUpdateActivity({ kind: 'escalated', phase: 'escalated', covers_offline: false, escalation: escalation('failed') })
    expect(activityLabel(a, ctx)).toBe('Agent update 2.78.0 → 2.80.0 failed: offline')
  })

  it('keeps the plain wording when the escalation carries no valid versions', () => {
    const a = selfUpdateActivity({
      kind: 'escalated', phase: 'escalated', covers_offline: false, agent_from_version: '', agent_to_version: '', escalation: escalation('not_back'),
    })
    expect(activityLabel(a, ctx)).toBe('Not back 10m after Agent update')
  })

  it('counts an escalated self-update as offline, not expected offline', () => {
    const a = selfUpdateActivity({ kind: 'escalated', phase: 'escalated', covers_offline: false, escalation: escalation('not_back') })
    expect(isEscalated(a)).toBe(true)
    expect(isExpectedOffline(a, 'offline')).toBe(false)
  })
})

describe('EXPECT-002 [AC-023] label clears on activity_update null', () => {
  const base: ActivityResponse = { generated_at: '', newest_agent_version: '2.80.0', activities: [selfUpdateActivity({ device_id: 'a' })] }

  it('counts an offline device inside the self-update window as expected offline', () => {
    expect(isExpectedOffline(selfUpdateActivity(), 'offline')).toBe(true)
    expect(isExpectedOffline(selfUpdateActivity(), 'online')).toBe(false)
  })

  it('opens the record in a modal (an agent action has no detail page)', () => {
    expect(activityLink(selfUpdateActivity())).toEqual({ type: 'modal' })
  })

  it('removes the entry when the server pushes null for the device', () => {
    const next = applyActivityUpdate(base, 'a', null)
    expect(next?.activities, 'the label must clear with no reload').toEqual([])
  })

  it('replaces the entry when the server pushes the new activity', () => {
    const next = applyActivityUpdate(base, 'a', selfUpdateActivity({ device_id: 'a', agent_to_version: '2.81.0' }))
    expect(next?.activities.map(a => a.agent_to_version)).toEqual(['2.81.0'])
  })
})

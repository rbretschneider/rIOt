import { describe, it, expect } from 'vitest'
import type { Command, CommandUpdate, ContainerInfo } from '../types/models'
import {
  applyActiveUpdate,
  applyCommandUpdate,
  commandLabel,
  composeTargetKey,
  containerTargetKey,
  containerTargetKeys,
  isAllowedStatusFilter,
  isCommandConflictError,
  isTerminal,
  matchesTarget,
  normalizeStatus,
  shouldSuppressExited,
  statusChipLabel,
  STATUS_FILTER_OPTIONS,
  stepLabel,
  targetKeyForContainer,
  updateIndicatorTitle,
  updateStatusNote,
  hasUpdateAvailable,
  upsertActiveCommand,
} from './commandLifecycle'

function makeCommand(overrides: Partial<Command> = {}): Command {
  return {
    id: 'cmd-1',
    device_id: 'dev-1',
    action: 'docker_update',
    params: {},
    status: 'running',
    result_msg: '',
    duration_ms: null,
    exit_code: null,
    created_at: '2026-10-17T10:00:00Z',
    updated_at: '2026-10-17T10:00:00Z',
    step: '',
    target_keys: ['docker:container:web'],
    ...overrides,
  }
}

function makeUpdate(overrides: Partial<CommandUpdate> = {}): CommandUpdate {
  return {
    command_id: 'cmd-1',
    device_id: 'dev-1',
    action: 'docker_update',
    target_key: 'docker:container:web',
    target_keys: ['docker:container:web'],
    status: 'running',
    step: 'pull',
    progress: null,
    message: '',
    failure_step: '',
    origin: 'user',
    updated_at: '2026-10-17T10:00:05Z',
    ...overrides,
  }
}

function makeContainer(overrides: Partial<ContainerInfo> = {}): ContainerInfo {
  return {
    id: 'abc123',
    short_id: 'abc',
    name: 'web',
    image: 'nginx:latest',
    state: 'running',
    status: 'Up',
    created: 0,
    cpu_percent: 0,
    mem_usage: 0,
    mem_limit: 0,
    ...overrides,
  }
}

const composeLabels = {
  'com.docker.compose.project': 'media',
  'com.docker.compose.service': 'web',
  'com.docker.compose.project.working_dir': '/srv/media/',
}

describe('[AC-062] update indicator title states the reason; check_failed and pinned show no indicator', () => {
  it('returns no title (no indicator) for check_failed', () => {
    const c = makeContainer({ update_status: 'check_failed', update_available: null })
    expect(hasUpdateAvailable(c), 'check_failed must never count as update available').toBe(false)
    expect(updateIndicatorTitle(c)).toBeNull()
  })

  it('returns no title (no indicator) for pinned', () => {
    const c = makeContainer({ update_status: 'pinned', update_available: false })
    expect(updateIndicatorTitle(c)).toBeNull()
  })

  it('returns no title for up_to_date', () => {
    expect(updateIndicatorTitle(makeContainer({ update_status: 'up_to_date', update_available: false }))).toBeNull()
  })

  it('states "newer image pulled, container not recreated" for pulled_not_recreated', () => {
    const c = makeContainer({ update_status: 'update_available', update_reason: 'pulled_not_recreated', update_available: true })
    expect(updateIndicatorTitle(c)).toBe('Update available: newer image pulled, container not recreated')
  })

  it('states "newer image in registry" for registry_newer', () => {
    const c = makeContainer({ update_status: 'update_available', update_reason: 'registry_newer', update_available: true })
    expect(updateIndicatorTitle(c)).toBe('Update available: newer image in registry')
  })

  it('falls back to plain "Update available" when the reason is absent', () => {
    const c = makeContainer({ update_status: 'update_available', update_available: true })
    expect(updateIndicatorTitle(c)).toBe('Update available')
  })

  it('treats an agent without update_status as update available when update_available is true (old agent)', () => {
    expect(updateIndicatorTitle(makeContainer({ update_available: true }))).toBe('Update available')
    expect(updateIndicatorTitle(makeContainer({ update_available: false }))).toBeNull()
    expect(updateIndicatorTitle(makeContainer({ update_available: null }))).toBeNull()
  })

  it('lets update_status win over a stale update_available boolean', () => {
    expect(hasUpdateAvailable({ update_status: 'check_failed', update_available: true })).toBe(false)
  })

  it('explains pinned and check_failed in the detail status instead of an indicator', () => {
    expect(updateStatusNote({ update_status: 'pinned' })).toBe('Pinned by digest')
    expect(updateStatusNote({ update_status: 'check_failed' })).toBe('Update check failed')
    expect(updateStatusNote({ update_status: 'update_available' })).toBeNull()
    expect(updateStatusNote({})).toBeNull()
  })
})

describe('[AC-114] every state has its frozen label', () => {
  const update = 'docker_update'
  const table: { name: string; cmd: Pick<Command, 'status' | 'action'> & Partial<Command>; label: string }[] = [
    { name: 'queued', cmd: { status: 'queued', action: update }, label: 'Queued' },
    { name: 'dispatched', cmd: { status: 'dispatched', action: update }, label: 'Sent to agent' },
    { name: 'acknowledged', cmd: { status: 'acknowledged', action: update }, label: 'Starting' },
    { name: 'running at pull', cmd: { status: 'running', action: update, step: 'pull' }, label: 'Updating… Pulling image' },
    { name: 'running at recreate', cmd: { status: 'running', action: update, step: 'recreate' }, label: 'Updating… Recreating' },
    { name: 'running with unknown step', cmd: { status: 'running', action: update, step: 'zzz' }, label: 'Updating… Working' },
    { name: 'running with empty step', cmd: { status: 'running', action: update, step: '' }, label: 'Updating… Working' },
    { name: 'verifying', cmd: { status: 'verifying', action: update }, label: 'Verifying' },
    { name: 'succeeded', cmd: { status: 'succeeded', action: update }, label: 'Updated' },
    { name: 'failed with step and message', cmd: { status: 'failed', action: update, failure_step: 'pull', result_msg: 'denied' }, label: 'Failed at pull: denied' },
    { name: 'failed falls back to step', cmd: { status: 'failed', action: update, step: 'verify', result_msg: 'exited' }, label: 'Failed at verify: exited' },
    { name: 'failed without a step', cmd: { status: 'failed', action: update, result_msg: 'boom' }, label: 'Failed: boom' },
    { name: 'timed_out', cmd: { status: 'timed_out', action: update }, label: 'Timed out: outcome unknown' },
    { name: 'expired', cmd: { status: 'expired', action: update }, label: 'Expired: never delivered' },
    { name: 'cancelled', cmd: { status: 'cancelled', action: update }, label: 'Cancelled' },
  ]

  it.each(table)('labels $name', ({ cmd, label }) => {
    expect(commandLabel(cmd)).toBe(label)
  })

  it('uses "Running…" and "Done" for non-update actions', () => {
    expect(commandLabel({ status: 'running', action: 'docker_restart', step: 'recreate' })).toBe('Running… Recreating')
    expect(commandLabel({ status: 'running', action: 'docker_restart', step: '' })).toBe('Running…')
    expect(commandLabel({ status: 'succeeded', action: 'docker_restart' })).toBe('Done')
  })

  it('labels a bulk update like a single update', () => {
    expect(commandLabel({ status: 'running', action: 'docker_bulk_update', step: 'pull' })).toBe('Updating… Pulling image')
  })

  it('renders legacy statuses sensibly (pending, sent, success, error)', () => {
    expect(commandLabel({ status: 'pending', action: update })).toBe('Queued')
    expect(commandLabel({ status: 'sent', action: update })).toBe('Sent to agent')
    expect(commandLabel({ status: 'success', action: update })).toBe('Updated')
    expect(commandLabel({ status: 'error', action: update, result_msg: 'x' })).toBe('Failed: x')
  })

  it('maps every frozen step to its label and anything else to Working', () => {
    const expected: Record<string, string> = {
      precheck: 'Checking',
      resolve: 'Resolving container',
      pull: 'Pulling image',
      stop_dependents: 'Stopping dependents',
      recreate: 'Recreating',
      verify: 'Verifying',
      restart_dependents: 'Restarting dependents',
      rollback: 'Rolling back',
      cleanup: 'Cleaning up',
    }
    for (const [step, label] of Object.entries(expected)) {
      expect(stepLabel(step), `step ${step}`).toBe(label)
    }
    expect(stepLabel('')).toBe('Working')
    expect(stepLabel(undefined)).toBe('Working')
    expect(stepLabel('<script>')).toBe('Working')
  })

  it('[SEC] passes agent failure text through as an unmodified string, never as markup', () => {
    const hostile = '<img src=x onerror=alert(1)>'
    expect(commandLabel({ status: 'failed', action: update, failure_step: 'pull', result_msg: hostile })).toBe(`Failed at pull: ${hostile}`)
  })
})

describe('[AC-115] legacy history rows show canonical labels', () => {
  it('relabels the legacy values', () => {
    expect(statusChipLabel('pending')).toBe('Queued')
    expect(statusChipLabel('queued')).toBe('Queued')
    expect(statusChipLabel('sent')).toBe('Sent to agent')
    expect(statusChipLabel('success')).toBe('Succeeded')
    expect(statusChipLabel('error')).toBe('Failed')
  })

  it('labels all ten canonical states', () => {
    const expected: [string, string][] = [
      ['queued', 'Queued'], ['dispatched', 'Sent to agent'], ['acknowledged', 'Acknowledged'],
      ['running', 'Running'], ['verifying', 'Verifying'], ['succeeded', 'Succeeded'], ['failed', 'Failed'],
      ['timed_out', 'Timed out'], ['cancelled', 'Cancelled'], ['expired', 'Expired'],
    ]
    for (const [state, label] of expected) {
      expect(statusChipLabel(state), state).toBe(label)
    }
  })

  it('normalizes legacy values to canonical states', () => {
    expect(normalizeStatus('pending')).toBe('queued')
    expect(normalizeStatus('sent')).toBe('dispatched')
    expect(normalizeStatus('success')).toBe('succeeded')
    expect(normalizeStatus('error')).toBe('failed')
    expect(normalizeStatus('queued')).toBe('queued')
  })

  it('maps an unknown value to failed, like the server', () => {
    expect(normalizeStatus('bogus')).toBe('failed')
    expect(normalizeStatus(undefined)).toBe('failed')
  })

  it('offers exactly the ten canonical values as history filters, and nothing else', () => {
    expect(STATUS_FILTER_OPTIONS.map(o => o.value)).toEqual([
      'queued', 'dispatched', 'acknowledged', 'running', 'verifying',
      'succeeded', 'failed', 'timed_out', 'cancelled', 'expired',
    ])
    expect(isAllowedStatusFilter('succeeded')).toBe(true)
    expect(isAllowedStatusFilter('success'), 'legacy values are not sent by the UI').toBe(false)
    expect(isAllowedStatusFilter("x' OR 1=1 --")).toBe(false)
    expect(isAllowedStatusFilter('')).toBe(false)
  })
})

describe('terminal and non-terminal classification', () => {
  it('treats queued through verifying as non-terminal and the rest as terminal', () => {
    for (const s of ['queued', 'dispatched', 'acknowledged', 'running', 'verifying', 'pending', 'sent']) {
      expect(isTerminal(s), s).toBe(false)
    }
    for (const s of ['succeeded', 'failed', 'timed_out', 'cancelled', 'expired', 'success', 'error']) {
      expect(isTerminal(s), s).toBe(true)
    }
  })
})

describe('target keys (ADD 6.7)', () => {
  it('uses the compose key for a container with project, service and working_dir labels, trailing slash removed', () => {
    expect(targetKeyForContainer(makeContainer({ labels: composeLabels }))).toBe('docker:compose:/srv/media')
  })

  it('uses the container name key without a leading slash for a standalone container', () => {
    expect(targetKeyForContainer(makeContainer({ name: '/web' }))).toBe('docker:container:web')
  })

  it('does not use the compose key when a compose label is missing', () => {
    const labels = { 'com.docker.compose.project': 'media', 'com.docker.compose.project.working_dir': '/srv/media' }
    expect(targetKeyForContainer(makeContainer({ labels }))).toBe('docker:container:web')
  })

  it('builds stack and container keys', () => {
    expect(composeTargetKey('/srv/media///')).toBe('docker:compose:/srv/media')
    expect(containerTargetKey('/web')).toBe('docker:container:web')
  })

  it('also matches the id fallback key', () => {
    expect(containerTargetKeys(makeContainer())).toEqual(['docker:container:web', 'docker:id:abc123'])
  })

  it('matches a command when its target_keys include the key', () => {
    const cmd = makeCommand({ target_keys: ['docker:compose:/srv/media'] })
    expect(matchesTarget(cmd, 'docker:compose:/srv/media')).toBe(true)
    expect(matchesTarget(cmd, 'docker:compose:/srv/other')).toBe(false)
    expect(matchesTarget(cmd, null)).toBe(false)
  })

  it('falls back to target_key when target_keys is absent', () => {
    expect(matchesTarget({ target_key: 'host' }, 'host')).toBe(true)
  })
})

describe('applyCommandUpdate (ADD 6.7)', () => {
  it('applies a newer update and keeps the step and message', () => {
    const next = applyCommandUpdate(makeCommand(), makeUpdate({ step: 'recreate', progress: 40, message: 'm' }))
    expect(next.step).toBe('recreate')
    expect(next.progress).toBe(40)
    expect(next.result_msg).toBe('m')
    expect(next.updated_at).toBe('2026-10-17T10:00:05Z')
  })

  it('ignores an update older than the current one', () => {
    const current = makeCommand({ updated_at: '2026-10-17T10:00:09Z', step: 'recreate' })
    expect(applyCommandUpdate(current, makeUpdate({ step: 'pull' }))).toBe(current)
  })

  it('applies an update with the same updated_at (>=)', () => {
    const current = makeCommand({ updated_at: '2026-10-17T10:00:05Z', step: 'pull' })
    expect(applyCommandUpdate(current, makeUpdate({ step: 'recreate' })).step).toBe('recreate')
  })

  it('never moves a command from terminal back to non-terminal', () => {
    const current = makeCommand({ status: 'succeeded', updated_at: '2026-10-17T10:00:01Z' })
    expect(applyCommandUpdate(current, makeUpdate({ status: 'running', updated_at: '2026-10-17T10:00:09Z' }))).toBe(current)
  })

  it('lets a timed_out command take its late final result', () => {
    const current = makeCommand({ status: 'timed_out', updated_at: '2026-10-17T10:00:01Z' })
    expect(applyCommandUpdate(current, makeUpdate({ status: 'succeeded' })).status).toBe('succeeded')
  })

  it('normalizes a legacy status carried by an update', () => {
    expect(applyCommandUpdate(makeCommand(), makeUpdate({ status: 'success' })).status).toBe('succeeded')
  })
})

describe('applyActiveUpdate keeps the per-device list of non-terminal commands', () => {
  it('adds a command first seen through an update', () => {
    const next = applyActiveUpdate(undefined, makeUpdate({ status: 'queued' }))
    expect(next).toHaveLength(1)
    expect(next[0].id).toBe('cmd-1')
    expect(next[0].status).toBe('queued')
  })

  it('updates a known command in place', () => {
    const list = [makeCommand()]
    const next = applyActiveUpdate(list, makeUpdate({ step: 'verify', status: 'verifying' }))
    expect(next).toHaveLength(1)
    expect(next[0].status).toBe('verifying')
  })

  it('removes a command when a terminal update arrives', () => {
    expect(applyActiveUpdate([makeCommand()], makeUpdate({ status: 'succeeded' }))).toEqual([])
  })

  it('returns the same list for a stale or already-handled update', () => {
    const list = [makeCommand({ updated_at: '2026-10-17T10:00:09Z' })]
    expect(applyActiveUpdate(list, makeUpdate())).toBe(list)
    const empty: Command[] = []
    expect(applyActiveUpdate(empty, makeUpdate({ status: 'failed' }))).toBe(empty)
  })

  it('upsertActiveCommand inserts, replaces and removes full commands', () => {
    const a = makeCommand({ id: 'a' })
    const inserted = upsertActiveCommand(undefined, a)
    expect(inserted).toEqual([a])
    const replaced = upsertActiveCommand(inserted, { ...a, step: 'pull' })
    expect(replaced[0].step).toBe('pull')
    expect(upsertActiveCommand(replaced, { ...a, status: 'failed' })).toEqual([])
  })
})

describe('[AC-112] [AC-113] exited suppression predicate', () => {
  const web = makeContainer({ name: 'web', id: 'abc123' })

  it('suppresses while a non-terminal update targets the container by name key', () => {
    expect(shouldSuppressExited([makeCommand({ status: 'running' })], web)).toBe(true)
  })

  it('suppresses for a compose container when the stack key is in flight', () => {
    const c = makeContainer({ labels: composeLabels })
    const cmd = makeCommand({ target_keys: ['docker:compose:/srv/media'] })
    expect(shouldSuppressExited([cmd], c)).toBe(true)
  })

  it('suppresses when the server could only record the id fallback key', () => {
    expect(shouldSuppressExited([makeCommand({ target_keys: ['docker:id:abc123'] })], web)).toBe(true)
  })

  it('suppresses for a bulk update whose keys include the container', () => {
    expect(shouldSuppressExited([makeCommand({ action: 'docker_bulk_update', target_keys: ['docker:container:web', 'docker:container:db'] })], web)).toBe(true)
  })

  it('does not suppress without a tracked command', () => {
    expect(shouldSuppressExited(undefined, web)).toBe(false)
    expect(shouldSuppressExited([], web)).toBe(false)
  })

  it('does not suppress once the command is terminal', () => {
    expect(shouldSuppressExited([makeCommand({ status: 'succeeded' })], web)).toBe(false)
  })

  it('does not suppress for a different target', () => {
    expect(shouldSuppressExited([makeCommand({ target_keys: ['docker:container:db'] })], web)).toBe(false)
  })

  it('does not suppress for a non-update action such as an operator stop', () => {
    expect(shouldSuppressExited([makeCommand({ action: 'docker_stop' })], web)).toBe(false)
  })

  it('treats a legacy sent command as in flight', () => {
    expect(shouldSuppressExited([makeCommand({ status: 'sent' })], web)).toBe(true)
  })
})

describe('[AC-111] conflict error detection', () => {
  it('recognises an error that carries the existing command id', () => {
    const e = Object.assign(new Error('conflict'), { name: 'CommandConflictError', existingCommandId: 'abc' })
    expect(isCommandConflictError(e)).toBe(true)
  })

  it('rejects ordinary errors and missing ids', () => {
    expect(isCommandConflictError(new Error('x'))).toBe(false)
    expect(isCommandConflictError(Object.assign(new Error('x'), { name: 'CommandConflictError' }))).toBe(false)
    expect(isCommandConflictError('conflict')).toBe(false)
    expect(isCommandConflictError(null)).toBe(false)
  })
})

describe('EXPECT-007 [AC-003] wait step labels and failed-in-wait label', () => {
  it('names the dpkg-lock wait and the power step in the command history', () => {
    expect(stepLabel('wait_dpkg_lock')).toBe('Waiting for dpkg lock')
    expect(stepLabel('power_action')).toBe('Initiating')
    expect(commandLabel({ status: 'running', action: 'reboot', step: 'wait_dpkg_lock' })).toBe('Running… Waiting for dpkg lock')
    expect(commandLabel({ status: 'running', action: 'shutdown', step: 'power_action' })).toBe('Running… Initiating')
  })

  it('shows that the command failed in the wait, with the message the agent sent', () => {
    const msg = 'reboot skipped: dpkg lock held past the timeout (10m), held by apt-get (pid 1234, uid 0)'
    expect(
      commandLabel({ status: 'failed', action: 'reboot', step: 'wait_dpkg_lock', failure_step: 'wait_dpkg_lock', result_msg: msg }),
    ).toBe(`Failed at wait_dpkg_lock: ${msg}`)
  })

  it('leaves a command that did not wait exactly as before', () => {
    expect(commandLabel({ status: 'running', action: 'reboot' })).toBe('Running…')
    expect(commandLabel({ status: 'succeeded', action: 'reboot' })).toBe('Done')
    expect(stepLabel('anything_else')).toBe('Working')
  })
})

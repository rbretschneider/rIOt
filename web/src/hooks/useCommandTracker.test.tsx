import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'
import type { Command, CommandUpdate, WSMessage } from '../types/models'

// The WS handler registered by useWebSocket, so tests can push command_update messages.
const wsState = { handler: null as ((msg: WSMessage) => void) | null }

vi.mock('./useWebSocket', () => ({
  useWebSocket: (handler: (msg: WSMessage) => void) => {
    wsState.handler = handler
    return { connected: true }
  },
}))

const mockGetCommand = vi.fn()
const mockGetActiveCommands = vi.fn()
const mockCancelCommand = vi.fn()

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return {
    ...actual,
    api: {
      getCommand: (...args: unknown[]) => mockGetCommand(...args),
      getActiveCommands: (...args: unknown[]) => mockGetActiveCommands(...args),
      cancelCommand: (...args: unknown[]) => mockCancelCommand(...args),
    },
  }
})

import { CommandConflictError } from '../api/client'
import { useCommandTracker, COMMAND_POLL_MS } from './useCommandTracker'

const DEVICE = 'dev-1'
const KEY = 'docker:container:web'
const CMD_ID = '11111111-1111-4111-8111-111111111111'
const OTHER_ID = '22222222-2222-4222-8222-222222222222'

function ts(seconds: number): string {
  return new Date(Date.UTC(2026, 9, 17, 10, 0, seconds)).toISOString()
}

function makeCommand(overrides: Partial<Command> = {}): Command {
  return {
    id: CMD_ID,
    device_id: DEVICE,
    action: 'docker_update',
    params: { container: 'web' },
    status: 'queued',
    result_msg: '',
    duration_ms: null,
    exit_code: null,
    created_at: ts(0),
    updated_at: ts(0),
    step: '',
    progress: null,
    failure_step: '',
    target_key: KEY,
    target_keys: [KEY],
    ...overrides,
  }
}

function makeUpdate(overrides: Partial<CommandUpdate> = {}): CommandUpdate {
  return {
    command_id: CMD_ID,
    device_id: DEVICE,
    action: 'docker_update',
    target_key: KEY,
    target_keys: [KEY],
    status: 'running',
    step: 'pull',
    progress: null,
    message: '',
    failure_step: '',
    origin: 'user',
    updated_at: ts(5),
    ...overrides,
  }
}

function push(update: CommandUpdate) {
  act(() => {
    wsState.handler!({ type: 'command_update', device_id: update.device_id, data: update })
  })
}

function setup(targetKey: string | null = KEY, deviceId: string | undefined = DEVICE) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: React.ReactNode }) =>
    React.createElement(QueryClientProvider, { client: qc }, children)
  const hook = renderHook(() => useCommandTracker(deviceId, targetKey), { wrapper })
  return { qc, ...hook }
}

async function start(result: { current: ReturnType<typeof useCommandTracker> }, created: Command) {
  await act(async () => {
    await result.current.track(() => Promise.resolve(created))
  })
}

// Lets pending promise callbacks and TanStack Query's zero-delay notifications run under fake timers.
async function flush(ms = 0) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

beforeEach(() => {
  wsState.handler = null
  mockGetCommand.mockReset()
  mockGetActiveCommands.mockReset()
  mockCancelCommand.mockReset()
  mockGetActiveCommands.mockResolvedValue([])
})

afterEach(() => {
  vi.useRealTimers()
})

describe('[AC-108] the tracker follows each state and step by command id, with no fixed timer', () => {
  it('moves through queued, sent, starting, step labels and verifying, and is still tracking after 5s', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()

    await start(result, makeCommand())
    expect(result.current.label).toBe('Queued')
    expect(result.current.isActive).toBe(true)

    // Pushes arrive every 3s, so the 5s re-fetch never needs to fire.
    push(makeUpdate({ status: 'dispatched', step: '', updated_at: ts(3) }))
    expect(result.current.label).toBe('Sent to agent')

    await flush(3000)
    push(makeUpdate({ status: 'acknowledged', step: '', updated_at: ts(6) }))
    expect(result.current.label).toBe('Starting')

    await flush(3000)
    push(makeUpdate({ status: 'running', step: 'pull', updated_at: ts(9) }))
    expect(result.current.label).toBe('Updating… Pulling image')

    await flush(3000)
    push(makeUpdate({ status: 'running', step: 'recreate', updated_at: ts(12) }))
    expect(result.current.label).toBe('Updating… Recreating')

    await flush(3000)
    push(makeUpdate({ status: 'verifying', step: 'verify', updated_at: ts(15) }))
    expect(result.current.label).toBe('Verifying')
    expect(result.current.isActive, 'a non-terminal command is still tracked well past 5s').toBe(true)
    expect(mockGetCommand, 'steady pushes must not trigger the polling fallback').not.toHaveBeenCalled()
  })

  it('keeps tracking a non-terminal command for 30s and beyond, relying on re-fetches when pushes stop', async () => {
    vi.useFakeTimers()
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(2) }))
    const { result } = setup()
    await flush()

    await start(result, makeCommand())
    push(makeUpdate({ status: 'running', step: 'pull', updated_at: ts(2) }))
    // 30s in 5s steps, so React re-arms the timer between steps.
    for (let i = 0; i < 6; i++) await flush(COMMAND_POLL_MS)

    expect(result.current.isActive, 'no fixed timer may end tracking of a non-terminal command').toBe(true)
    expect(result.current.label).toBe('Updating… Pulling image')
    expect(mockGetCommand.mock.calls.length).toBeGreaterThanOrEqual(5)
  })

  it('shows Updated for 5s after success, then returns to the normal view', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()

    await start(result, makeCommand())
    push(makeUpdate({ status: 'succeeded', step: 'cleanup', updated_at: ts(8) }))
    expect(result.current.label).toBe('Updated')
    expect(result.current.isActive).toBe(false)

    await flush(4999)
    expect(result.current.command, 'Updated is still shown just before 5s').not.toBeNull()
    await flush(1)
    expect(result.current.command, 'the normal view returns at 5s').toBeNull()
    expect(result.current.label).toBeNull()
  })
})

describe('[AC-109] controls stay disabled while the tracked command is non-terminal', () => {
  it('reports busy from the moment the create request is sent until the command is terminal', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()

    let resolveSend!: (c: Command) => void
    const pending = new Promise<Command>((resolve) => { resolveSend = resolve })
    let tracking!: Promise<unknown>
    act(() => { tracking = result.current.track(() => pending) })
    expect(result.current.isStarting).toBe(true)
    expect(result.current.isBusy, 'busy while the POST is in flight').toBe(true)

    await act(async () => { resolveSend(makeCommand({ status: 'running', step: 'pull' })); await tracking })
    expect(result.current.isStarting).toBe(false)
    expect(result.current.isBusy, 'busy while the command is running').toBe(true)

    push(makeUpdate({ status: 'failed', step: 'pull', failure_step: 'pull', message: 'denied', updated_at: ts(9) }))
    expect(result.current.isBusy, 'free again once the command is terminal').toBe(false)
  })
})

describe('[AC-110] on mount the tracker fetches and tracks the non-terminal command for its target', () => {
  it('shows Updating with the current step for an in-flight command', async () => {
    mockGetActiveCommands.mockResolvedValue([
      makeCommand({ status: 'running', step: 'recreate', updated_at: ts(7) }),
    ])
    const { result } = setup()

    await waitFor(() => expect(result.current.command?.id).toBe(CMD_ID))
    expect(result.current.label).toBe('Updating… Recreating')
    expect(mockGetActiveCommands).toHaveBeenCalledWith(DEVICE)
  })

  it('does not track a command that targets something else', async () => {
    mockGetActiveCommands.mockResolvedValue([
      makeCommand({ id: OTHER_ID, status: 'running', target_keys: ['docker:container:db'], target_key: 'docker:container:db' }),
    ])
    const { result } = setup()

    await waitFor(() => expect(mockGetActiveCommands).toHaveBeenCalled())
    await waitFor(() => expect(result.current.command).toBeNull())
  })

  it('does not fetch without a target key', async () => {
    setup(null)
    await new Promise((r) => setTimeout(r, 20))
    expect(mockGetActiveCommands).not.toHaveBeenCalled()
  })

  it('does not fetch without a device id', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: React.ReactNode }) =>
      React.createElement(QueryClientProvider, { client: qc }, children)
    renderHook(() => useCommandTracker(undefined, KEY), { wrapper })
    await new Promise((r) => setTimeout(r, 20))
    expect(mockGetActiveCommands).not.toHaveBeenCalled()
  })

  it('adopts a command another actor starts for the same target, from the WS push', async () => {
    const { result } = setup()
    await waitFor(() => expect(mockGetActiveCommands).toHaveBeenCalled())

    push(makeUpdate({ command_id: OTHER_ID, action: 'docker_update', status: 'running', step: 'pull', origin: 'auto_update' }))

    expect(result.current.command?.id).toBe(OTHER_ID)
    expect(result.current.label).toBe('Updating… Pulling image')
  })

  it('ignores updates for other devices and for other targets', async () => {
    const { result } = setup()
    await waitFor(() => expect(mockGetActiveCommands).toHaveBeenCalled())

    push(makeUpdate({ device_id: 'dev-2', command_id: OTHER_ID }))
    push(makeUpdate({ command_id: OTHER_ID, target_key: 'docker:container:db', target_keys: ['docker:container:db'] }))

    expect(result.current.command).toBeNull()
  })
})

describe('[AC-111] a 409 conflict attaches to the existing command', () => {
  function conflict(status = 'running') {
    return new CommandConflictError({
      error: 'a command is already in progress for this target',
      code: 'conflict',
      existing_command_id: CMD_ID,
      existing_status: status as Command['status'],
      target_key: KEY,
    })
  }

  it('tracks the existing command id and hydrates it by id', async () => {
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(4) }))
    const { result } = setup()
    await waitFor(() => expect(mockGetActiveCommands).toHaveBeenCalled())

    await act(async () => { await result.current.track(() => Promise.reject(conflict())) })

    await waitFor(() => expect(result.current.label).toBe('Updating… Pulling image'))
    expect(result.current.command?.id).toBe(CMD_ID)
    expect(mockGetCommand).toHaveBeenCalledWith(DEVICE, CMD_ID)
    expect(result.current.error, 'a conflict is attached to, not reported as an error').toBeNull()
  })

  it('shows the existing command at once from the conflict body, before the fetch returns', async () => {
    mockGetCommand.mockReturnValue(new Promise(() => {}))
    const { result } = setup()
    await waitFor(() => expect(mockGetActiveCommands).toHaveBeenCalled())

    await act(async () => { await result.current.track(() => Promise.reject(conflict('dispatched'))) })

    expect(result.current.command?.id).toBe(CMD_ID)
    expect(result.current.label).toBe('Sent to agent')
    expect(result.current.isActive).toBe(true)
  })

  it('reports other create failures as an error and tracks nothing', async () => {
    const { result } = setup()
    await waitFor(() => expect(mockGetActiveCommands).toHaveBeenCalled())

    await act(async () => { await result.current.track(() => Promise.reject(new Error('HTTP 500'))) })

    expect(result.current.error).toBe('HTTP 500')
    expect(result.current.command).toBeNull()
    expect(result.current.isBusy).toBe(false)
  })
})

describe('[AC-114] terminal states are shown and, except Updated, stay until dismissed', () => {
  const sticky: { status: Command['status']; extra: Partial<CommandUpdate>; label: string }[] = [
    { status: 'failed', extra: { step: 'verify', failure_step: 'verify', message: 'replacement exited' }, label: 'Failed at verify: replacement exited' },
    { status: 'timed_out', extra: {}, label: 'Timed out: outcome unknown' },
    { status: 'expired', extra: {}, label: 'Expired: never delivered' },
    { status: 'cancelled', extra: {}, label: 'Cancelled' },
  ]

  it.each(sticky)('$status shows its label and is still shown a minute later', async ({ status, extra, label }) => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand())

    push(makeUpdate({ status, updated_at: ts(9), ...extra }))
    expect(result.current.label).toBe(label)

    await flush(60_000)
    expect(result.current.label, `${status} must stay until dismissed`).toBe(label)
  })

  it('dismiss clears a failed command', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand())
    push(makeUpdate({ status: 'failed', failure_step: 'pull', message: 'denied', updated_at: ts(9) }))

    act(() => { result.current.dismiss() })

    expect(result.current.command).toBeNull()
  })

  it('dismiss does not drop a command that is still running', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'running', step: 'pull' }))

    act(() => { result.current.dismiss() })

    expect(result.current.isActive).toBe(true)
  })

  it('starting a new action replaces a failure left on screen', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand())
    push(makeUpdate({ status: 'failed', failure_step: 'pull', message: 'denied', updated_at: ts(9) }))
    expect(result.current.label).toBe('Failed at pull: denied')

    await start(result, makeCommand({ id: OTHER_ID, status: 'queued', updated_at: ts(20) }))

    expect(result.current.command?.id).toBe(OTHER_ID)
    expect(result.current.label).toBe('Queued')
  })

  it('lets a timed_out command take its late final result', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand())
    push(makeUpdate({ status: 'timed_out', updated_at: ts(9) }))
    push(makeUpdate({ status: 'succeeded', step: 'cleanup', updated_at: ts(10) }))

    expect(result.current.label).toBe('Updated')
  })

  it('never moves a finished command back to running', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand())
    push(makeUpdate({ status: 'failed', failure_step: 'pull', message: 'denied', updated_at: ts(9) }))
    push(makeUpdate({ status: 'running', step: 'pull', updated_at: ts(10) }))

    expect(result.current.label).toBe('Failed at pull: denied')
  })

  it('renders legacy statuses sensibly when an old server returns them', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'pending' }))
    expect(result.current.label).toBe('Queued')

    push(makeUpdate({ status: 'sent', step: '', updated_at: ts(2) }))
    expect(result.current.label).toBe('Sent to agent')

    push(makeUpdate({ status: 'success', step: '', updated_at: ts(3) }))
    expect(result.current.label).toBe('Updated')
  })
})

describe('[AC-116] Cancel is offered only while the command is queued', () => {
  it('is available while queued and gone once the command is dispatched', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'queued' }))
    expect(result.current.canCancel).toBe(true)

    push(makeUpdate({ status: 'dispatched', step: '', updated_at: ts(2) }))
    expect(result.current.canCancel, 'Cancel must disappear once dispatched').toBe(false)
  })

  it('cancels a queued command and shows Cancelled', async () => {
    vi.useFakeTimers()
    mockCancelCommand.mockResolvedValue(makeCommand({ status: 'cancelled', updated_at: ts(3) }))
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'queued' }))

    await act(async () => { await result.current.cancel() })

    expect(mockCancelCommand).toHaveBeenCalledWith(DEVICE, CMD_ID)
    expect(result.current.label).toBe('Cancelled')
    expect(result.current.canCancel).toBe(false)
  })

  it('does nothing for a command that is no longer queued', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'running', step: 'pull' }))

    await act(async () => { await result.current.cancel() })

    expect(mockCancelCommand).not.toHaveBeenCalled()
  })

  it('shows the server message and re-reads the state when the cancel loses the race to a claim', async () => {
    vi.useFakeTimers()
    mockCancelCommand.mockRejectedValue(new Error('command cannot be cancelled after it has been dispatched'))
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'dispatched', updated_at: ts(4) }))
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'queued' }))

    await act(async () => { await result.current.cancel() })
    await flush()

    expect(result.current.error).toBe('command cannot be cancelled after it has been dispatched')
    expect(result.current.label).toBe('Sent to agent')
    expect(result.current.canCancel).toBe(false)
  })
})

describe('[AC-117] with no push for 5s the command is re-fetched, and again every 5s', () => {
  it('does not fetch before 5s, fetches at 5s, and keeps fetching every 5s', async () => {
    vi.useFakeTimers()
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(2) }))
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'running', step: 'pull' }))

    await flush(COMMAND_POLL_MS - 1)
    expect(mockGetCommand, 'nothing before the 5s mark').not.toHaveBeenCalled()

    await flush(1)
    expect(mockGetCommand).toHaveBeenCalledTimes(1)
    expect(mockGetCommand).toHaveBeenCalledWith(DEVICE, CMD_ID)

    await flush(COMMAND_POLL_MS)
    expect(mockGetCommand).toHaveBeenCalledTimes(2)
    await flush(COMMAND_POLL_MS)
    expect(mockGetCommand).toHaveBeenCalledTimes(3)
  })

  it('restarts the 5s wait whenever a push arrives', async () => {
    vi.useFakeTimers()
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'recreate', updated_at: ts(20) }))
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'running', step: 'pull' }))

    await flush(4000)
    push(makeUpdate({ status: 'running', step: 'recreate', updated_at: ts(4) }))
    await flush(4000)
    expect(mockGetCommand, 'the push at 4s restarted the wait').not.toHaveBeenCalled()

    await flush(1000)
    expect(mockGetCommand).toHaveBeenCalledTimes(1)
  })

  it('applies the re-fetched state, including a terminal one the WS never delivered', async () => {
    vi.useFakeTimers()
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'failed', failure_step: 'pull', result_msg: 'denied', updated_at: ts(9) }))
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'running', step: 'pull' }))

    await flush(COMMAND_POLL_MS)

    expect(result.current.label).toBe('Failed at pull: denied')
    expect(result.current.isActive).toBe(false)
  })

  it('keeps polling after a failed re-fetch', async () => {
    vi.useFakeTimers()
    mockGetCommand.mockRejectedValueOnce(new Error('HTTP 500')).mockResolvedValue(makeCommand({ status: 'running', updated_at: ts(9) }))
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'running', step: 'pull' }))

    await flush(COMMAND_POLL_MS)
    expect(mockGetCommand).toHaveBeenCalledTimes(1)
    await flush(COMMAND_POLL_MS)
    expect(mockGetCommand, 'a failed poll is not a failed command').toHaveBeenCalledTimes(2)
    expect(result.current.isActive).toBe(true)
  })

  it('does not poll once the command is terminal', async () => {
    vi.useFakeTimers()
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'running', step: 'pull' }))
    push(makeUpdate({ status: 'failed', failure_step: 'pull', message: 'x', updated_at: ts(3) }))

    await flush(60_000)

    expect(mockGetCommand).not.toHaveBeenCalled()
  })

  it('ignores a re-fetch that is older than what a push already delivered', async () => {
    vi.useFakeTimers()
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(2) }))
    const { result } = setup()
    await flush()
    await start(result, makeCommand({ status: 'running', step: 'pull', updated_at: ts(1) }))
    push(makeUpdate({ status: 'verifying', step: 'verify', updated_at: ts(10) }))
    await flush(4000)
    mockGetCommand.mockClear()
    await flush(1000)

    expect(mockGetCommand).toHaveBeenCalledTimes(1)
    expect(result.current.label, 'a stale snapshot must not move the command backward').toBe('Verifying')
  })
})

describe('shared active-command list', () => {
  it('writes the created command into the device active list and removes it when terminal', async () => {
    vi.useFakeTimers()
    const { result, qc } = setup()
    await flush()

    await start(result, makeCommand({ status: 'running', step: 'pull' }))
    expect(qc.getQueryData<Command[]>(['commands-active', DEVICE])?.map(c => c.id)).toEqual([CMD_ID])

    push(makeUpdate({ status: 'succeeded', updated_at: ts(9) }))
    expect(qc.getQueryData<Command[]>(['commands-active', DEVICE])).toEqual([])
  })
})

describe('[SEC] agent failure text stays a plain string in tracker state', () => {
  it('exposes a hostile result_msg verbatim in the label and never as markup', async () => {
    vi.useFakeTimers()
    const hostile = '<img src=x onerror=alert(1)>'
    const { result } = setup()
    await flush()
    await start(result, makeCommand())

    push(makeUpdate({ status: 'failed', failure_step: 'pull', message: hostile, updated_at: ts(9) }))

    expect(result.current.label).toBe(`Failed at pull: ${hostile}`)
  })
})

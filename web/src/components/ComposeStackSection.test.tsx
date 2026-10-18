import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, act, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Command, CommandUpdate, ContainerInfo, WSMessage } from '../types/models'
import type { ComposeStackGroup } from '../utils/docker'

// All mounted hooks that subscribe to the shared WebSocket receive every pushed message.
const ws = vi.hoisted(() => ({ subscribers: new Set<(msg: unknown) => void>() }))

vi.mock('../hooks/useWebSocket', async () => {
  const React = await import('react')
  return {
    useWebSocket: (handler: (msg: unknown) => void) => {
      const ref = React.useRef(handler)
      ref.current = handler
      React.useEffect(() => {
        const fn = (msg: unknown) => ref.current(msg)
        ws.subscribers.add(fn)
        return () => { ws.subscribers.delete(fn) }
      }, [])
      return { connected: true }
    },
  }
})

const mockSendCommand = vi.fn()
const mockBulkDockerUpdate = vi.fn()
const mockGetCommand = vi.fn()
const mockGetActiveCommands = vi.fn()
const mockCancelCommand = vi.fn()

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return {
    ...actual,
    api: {
      sendCommand: (...args: unknown[]) => mockSendCommand(...args),
      bulkDockerUpdate: (...args: unknown[]) => mockBulkDockerUpdate(...args),
      getCommand: (...args: unknown[]) => mockGetCommand(...args),
      getActiveCommands: (...args: unknown[]) => mockGetActiveCommands(...args),
      cancelCommand: (...args: unknown[]) => mockCancelCommand(...args),
      getAutoUpdates: () => Promise.resolve([]),
      setAutoUpdate: vi.fn(),
    },
  }
})

import { CommandConflictError } from '../api/client'
import ComposeStackSection from './ComposeStackSection'

const DEVICE = 'dev-1'
const WORK_DIR = '/srv/media'
const KEY = 'docker:compose:/srv/media'
const CMD_ID = '11111111-1111-4111-8111-111111111111'

function ts(seconds: number): string {
  return new Date(Date.UTC(2026, 9, 17, 10, 0, seconds)).toISOString()
}

function makeContainer(name: string, overrides: Partial<ContainerInfo> = {}): ContainerInfo {
  return {
    id: `${name}-id`,
    short_id: name,
    name,
    image: `${name}:latest`,
    state: 'running',
    status: 'Up',
    created: 0,
    cpu_percent: 0,
    mem_usage: 0,
    mem_limit: 0,
    update_available: false,
    labels: {
      'com.docker.compose.project': 'media',
      'com.docker.compose.service': name,
      'com.docker.compose.project.working_dir': WORK_DIR,
    },
    ...overrides,
  }
}

function makeStack(): ComposeStackGroup {
  const web = makeContainer('web', { update_available: true, update_status: 'update_available', update_reason: 'registry_newer' })
  const db = makeContainer('db')
  return { name: 'media', workDir: WORK_DIR, containers: [web, db], networkClusters: [], loose: [web, db], updatableCount: 1 }
}

function makeClusterStack(): ComposeStackGroup {
  const vpn = makeContainer('vpn', { update_available: true, update_status: 'update_available' })
  const app = makeContainer('app', { update_available: true, update_status: 'update_available' })
  return {
    name: 'media',
    workDir: WORK_DIR,
    containers: [vpn, app],
    networkClusters: [{ parent: vpn, dependents: [app] }],
    loose: [],
    updatableCount: 2,
  }
}

function makeCommand(overrides: Partial<Command> = {}): Command {
  return {
    id: CMD_ID,
    device_id: DEVICE,
    action: 'docker_update',
    params: {},
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
    const msg: WSMessage = { type: 'command_update', device_id: update.device_id, data: update }
    ws.subscribers.forEach((fn) => fn(msg))
  })
}

function renderStack(stack: ComposeStackGroup = makeStack()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const utils = render(
    <QueryClientProvider client={qc}>
      <ComposeStackSection stack={stack} deviceId={DEVICE} onContainerClick={() => {}} allContainers={stack.containers} />
    </QueryClientProvider>,
  )
  return { qc, ...utils }
}

async function flush(ms = 0) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

async function confirmStackUpdate() {
  fireEvent.click(screen.getByRole('button', { name: /Update Stack/ }))
  fireEvent.click(screen.getByRole('button', { name: 'Confirm Update' }))
  await flush()
}

async function confirmGroupUpdate() {
  fireEvent.click(screen.getByRole('button', { name: /Update Group/ }))
  fireEvent.click(screen.getByRole('button', { name: 'Confirm Update' }))
  await flush()
}

beforeEach(() => {
  ws.subscribers.clear()
  mockSendCommand.mockReset()
  mockBulkDockerUpdate.mockReset()
  mockGetCommand.mockReset()
  mockGetActiveCommands.mockReset()
  mockCancelCommand.mockReset()
  mockGetActiveCommands.mockResolvedValue([])
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('[AC-108] ComposeStackSection follows a stack update by command id', () => {
  it('sends the compose work dir and shows each state and step, still tracking after 30s', async () => {
    mockSendCommand.mockResolvedValue(makeCommand())
    renderStack()
    await flush()

    await confirmStackUpdate()
    expect(mockSendCommand).toHaveBeenCalledWith(DEVICE, 'docker_update', { compose_work_dir: WORK_DIR })
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Queued')

    push(makeUpdate({ status: 'dispatched', step: '', updated_at: ts(2) }))
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Sent to agent')

    push(makeUpdate({ status: 'acknowledged', step: '', updated_at: ts(3) }))
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Starting')

    push(makeUpdate({ status: 'running', step: 'pull', updated_at: ts(4) }))
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Updating… Pulling image')

    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(4) }))
    for (let i = 0; i < 6; i++) await flush(5000)
    expect(screen.getByTestId('stack-command-status'), 'no fixed timer may end tracking').toHaveTextContent('Updating… Pulling image')

    push(makeUpdate({ status: 'running', step: 'stop_dependents', updated_at: ts(40) }))
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Updating… Stopping dependents')

    push(makeUpdate({ status: 'verifying', step: 'verify', updated_at: ts(41) }))
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Verifying')

    push(makeUpdate({ status: 'succeeded', step: 'restart_dependents', updated_at: ts(42) }))
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Updated')
    await flush(5000)
    expect(screen.queryByTestId('stack-command-status')).not.toBeInTheDocument()
  })

  it('tracks a network-group (bulk) update by id as well', async () => {
    mockBulkDockerUpdate.mockResolvedValue({ ...makeCommand({ action: 'docker_bulk_update' }), conflicts: [] })
    renderStack(makeClusterStack())
    await flush()

    await confirmGroupUpdate()

    expect(mockBulkDockerUpdate).toHaveBeenCalledWith(DEVICE, ['vpn-id', 'app-id'])
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Queued')
    push(makeUpdate({ action: 'docker_bulk_update', status: 'running', step: 'recreate', updated_at: ts(4) }))
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Updating… Recreating')
  })

  it('reports targets a bulk update skipped because an update was already in progress', async () => {
    mockBulkDockerUpdate.mockResolvedValue({
      ...makeCommand({ action: 'docker_bulk_update' }),
      conflicts: [{ container_id: 'app-id', target_key: 'docker:container:app', existing_command_id: 'other' }],
    })
    renderStack(makeClusterStack())
    await flush()

    await confirmGroupUpdate()

    expect(screen.getByText(/1 target was skipped: an update is already in progress/)).toBeInTheDocument()
  })
})

describe('[AC-109] the stack update controls are disabled while the command is non-terminal', () => {
  it('disables Update Stack and Update Group while tracking, and re-enables them when it ends', async () => {
    mockBulkDockerUpdate.mockResolvedValue({ ...makeCommand({ action: 'docker_bulk_update', status: 'running', step: 'pull' }), conflicts: [] })
    renderStack(makeClusterStack())
    await flush()
    expect(screen.getByRole('button', { name: /Update Group/ })).toBeEnabled()

    await confirmGroupUpdate()

    // NetworkClusterGroup shows "..." on its button while the group is updating.
    expect(screen.getByRole('button', { name: '...' })).toBeDisabled()
    expect(screen.getByRole('button', { name: /Update Stack/ })).toBeDisabled()

    push(makeUpdate({ action: 'docker_bulk_update', status: 'failed', step: 'pull', failure_step: 'pull', message: 'denied', updated_at: ts(9) }))

    expect(screen.getByRole('button', { name: /Update Group/ })).toBeEnabled()
  })

  it('disables Update Stack while its command runs', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull' }))
    renderStack()
    await flush()
    await confirmStackUpdate()

    expect(screen.getByRole('button', { name: /Update Stack/ })).toBeDisabled()
  })
})

describe('[AC-110] on remount an in-flight stack command is shown with its current step', () => {
  it('shows Updating with the step', async () => {
    mockGetActiveCommands.mockResolvedValue([makeCommand({ status: 'running', step: 'recreate', updated_at: ts(7) })])

    renderStack()
    await flush()

    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Updating… Recreating')
    expect(screen.getByRole('button', { name: /Update Stack/ })).toBeDisabled()
  })

  it('ignores an in-flight command that targets a different project', async () => {
    mockGetActiveCommands.mockResolvedValue([
      makeCommand({ status: 'running', target_key: 'docker:compose:/srv/other', target_keys: ['docker:compose:/srv/other'] }),
    ])

    renderStack()
    await flush()

    expect(screen.queryByTestId('stack-command-status')).not.toBeInTheDocument()
  })
})

describe('[AC-111] a 409 conflict attaches to the existing command', () => {
  function conflict() {
    return new CommandConflictError({
      error: 'a command is already in progress for this target',
      code: 'conflict',
      existing_command_id: CMD_ID,
      existing_status: 'running',
      target_key: KEY,
    })
  }

  it('tracks the existing command when a stack update is refused', async () => {
    mockSendCommand.mockRejectedValue(conflict())
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(4) }))
    renderStack()
    await flush()

    await confirmStackUpdate()

    expect(mockGetCommand).toHaveBeenCalledWith(DEVICE, CMD_ID)
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Updating… Pulling image')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('tracks the existing command when every target of a group update conflicts', async () => {
    mockBulkDockerUpdate.mockRejectedValue(conflict())
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'recreate', updated_at: ts(4) }))
    renderStack(makeClusterStack())
    await flush()

    await confirmGroupUpdate()

    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Updating… Recreating')
  })
})

describe('terminal states and Cancel in the stack section', () => {
  it('offers Cancel only while queued', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'queued' }))
    renderStack()
    await flush()
    await confirmStackUpdate()
    expect(within(screen.getByTestId('stack-command-status')).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()

    push(makeUpdate({ status: 'dispatched', step: '', updated_at: ts(2) }))

    expect(within(screen.getByTestId('stack-command-status')).queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument()
  })

  it('cancels a queued command and shows Cancelled with Dismiss', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'queued' }))
    mockCancelCommand.mockResolvedValue(makeCommand({ status: 'cancelled', updated_at: ts(3) }))
    renderStack()
    await flush()
    await confirmStackUpdate()

    fireEvent.click(within(screen.getByTestId('stack-command-status')).getByRole('button', { name: 'Cancel' }))
    await flush()

    expect(mockCancelCommand).toHaveBeenCalledWith(DEVICE, CMD_ID)
    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Cancelled')
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByTestId('stack-command-status')).not.toBeInTheDocument()
  })

  it('shows a failure with its step until dismissed', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull' }))
    renderStack()
    await flush()
    await confirmStackUpdate()

    push(makeUpdate({ status: 'failed', step: 'restart_dependents', failure_step: 'restart_dependents', message: 'dependents not restored: vpn-client', updated_at: ts(9) }))
    await flush(60_000)

    expect(screen.getByTestId('stack-command-status')).toHaveTextContent('Failed at restart_dependents: dependents not restored: vpn-client')
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByTestId('stack-command-status')).not.toBeInTheDocument()
  })
})

describe('[SEC] agent failure text renders as text', () => {
  it('shows an <img onerror> result_msg literally and creates no element', async () => {
    const hostile = '<img src=x onerror=alert(1)>'
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull' }))
    const { container } = renderStack()
    await flush()
    await confirmStackUpdate()

    push(makeUpdate({ status: 'failed', step: 'pull', failure_step: 'pull', message: hostile, updated_at: ts(9) }))

    const status = screen.getByTestId('stack-command-status')
    expect(status.textContent, 'the markup must appear as literal text').toContain(hostile)
    expect(container.querySelector('img'), 'no <img> element may be created from agent text').toBeNull()
    expect(status.innerHTML).not.toContain('<img')
  })
})

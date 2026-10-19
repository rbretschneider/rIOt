import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, act, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import type { Command, CommandUpdate, ContainerInfo, DeviceDetailResponse, WSMessage } from '../types/models'

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
const mockGetCommand = vi.fn()
const mockGetActiveCommands = vi.fn()
const mockCancelCommand = vi.fn()

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return {
    ...actual,
    api: {
      sendCommand: (...args: unknown[]) => mockSendCommand(...args),
      getCommand: (...args: unknown[]) => mockGetCommand(...args),
      getActiveCommands: (...args: unknown[]) => mockGetActiveCommands(...args),
      cancelCommand: (...args: unknown[]) => mockCancelCommand(...args),
      getDevices: () => Promise.resolve([]),
      getContainerMetricHistory: () => Promise.resolve([]),
      getEvents: () => Promise.resolve([]),
      getDeviceAlertRules: () => Promise.resolve([]),
    },
  }
})

vi.mock('../api/settings', () => ({ settingsApi: { createAlertRule: vi.fn() } }))
vi.mock('./Terminal', () => ({ default: () => null }))

import { CommandConflictError } from '../api/client'
import { ContainerDetailContent } from './ContainerDetail'
import { useDevices } from '../hooks/useDevices'

const DEVICE = 'dev-1'
const KEY = 'docker:container:web'
const CMD_ID = '11111111-1111-4111-8111-111111111111'

function ts(seconds: number): string {
  return new Date(Date.UTC(2026, 9, 17, 10, 0, seconds)).toISOString()
}

function makeContainer(overrides: Partial<ContainerInfo> = {}): ContainerInfo {
  return {
    id: 'abc123',
    short_id: 'abc',
    name: 'web',
    image: 'nginx:latest',
    state: 'running',
    status: 'Up 2 hours',
    created: 0,
    cpu_percent: 0,
    mem_usage: 0,
    mem_limit: 0,
    update_available: true,
    update_status: 'update_available',
    update_reason: 'registry_newer',
    ...overrides,
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

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderDetail(container: ContainerInfo = makeContainer(), qc: QueryClient = makeQC()) {
  const utils = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ContainerDetailContent container={container} deviceId={DEVICE} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return { qc, ...utils }
}

async function flush(ms = 0) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

// Opens the confirm dialog for the header action and confirms it.
async function confirmUpdate() {
  fireEvent.click(screen.getByRole('button', { name: 'Update' }))
  const buttons = screen.getAllByRole('button', { name: 'Update' })
  fireEvent.click(buttons[buttons.length - 1])
  await flush()
}

beforeEach(() => {
  ws.subscribers.clear()
  mockSendCommand.mockReset()
  mockGetCommand.mockReset()
  mockGetActiveCommands.mockReset()
  mockCancelCommand.mockReset()
  mockGetActiveCommands.mockResolvedValue([])
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('[AC-108] ContainerDetail follows the update by command id through every state and step', () => {
  it('sends the container name and id, then shows Queued, Sent to agent, Starting, Updating with the step, Verifying', async () => {
    mockSendCommand.mockResolvedValue(makeCommand())
    renderDetail()
    await flush()

    await confirmUpdate()

    expect(mockSendCommand).toHaveBeenCalledWith(DEVICE, 'docker_update', { container: 'web', container_id: 'abc123' })
    expect(screen.getByTestId('command-status')).toHaveTextContent('Queued')

    push(makeUpdate({ status: 'dispatched', step: '', updated_at: ts(2) }))
    expect(screen.getByTestId('command-status')).toHaveTextContent('Sent to agent')

    push(makeUpdate({ status: 'acknowledged', step: '', updated_at: ts(3) }))
    expect(screen.getByTestId('command-status')).toHaveTextContent('Starting')

    push(makeUpdate({ status: 'running', step: 'pull', updated_at: ts(4) }))
    expect(screen.getByTestId('command-status')).toHaveTextContent('Updating… Pulling image')

    push(makeUpdate({ status: 'running', step: 'recreate', updated_at: ts(5) }))
    expect(screen.getByTestId('command-status')).toHaveTextContent('Updating… Recreating')

    push(makeUpdate({ status: 'verifying', step: 'verify', updated_at: ts(6) }))
    expect(screen.getByTestId('command-status')).toHaveTextContent('Verifying')
  })

  it('is still tracking after 5s and after 30s (no fixed reset timer), re-fetching when pushes stop', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(1) }))
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(2) }))
    renderDetail()
    await flush()
    await confirmUpdate()

    for (let i = 0; i < 6; i++) await flush(5000)

    expect(screen.getByTestId('command-status')).toHaveTextContent('Updating… Pulling image')
    expect(mockGetCommand).toHaveBeenCalled()
  })

  it('uses Running and Done wording for a non-update action', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ action: 'docker_check_updates', target_key: '', target_keys: [], status: 'queued' }))
    renderDetail()
    await flush()

    fireEvent.click(screen.getByRole('button', { name: 'Check Updates' }))
    await flush()
    push(makeUpdate({ action: 'docker_check_updates', target_key: '', target_keys: [], status: 'running', step: 'precheck', updated_at: ts(2) }))
    expect(screen.getByTestId('command-status')).toHaveTextContent('Running… Checking')

    push(makeUpdate({ action: 'docker_check_updates', target_key: '', target_keys: [], status: 'succeeded', step: '', updated_at: ts(3) }))
    expect(screen.getByTestId('command-status')).toHaveTextContent('Done')
  })
})

describe('[AC-109] controls are disabled while the tracked command is non-terminal', () => {
  it('disables Update, Check Updates, Restart and Stop while the update runs, and re-enables them when it ends', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull' }))
    renderDetail()
    await flush()
    await confirmUpdate()

    for (const name of ['Update', 'Check Updates', 'Restart', 'Stop']) {
      expect(screen.getByRole('button', { name }), `${name} must be disabled during the update`).toBeDisabled()
    }

    push(makeUpdate({ status: 'failed', step: 'pull', failure_step: 'pull', message: 'denied', updated_at: ts(9) }))

    for (const name of ['Update', 'Check Updates', 'Restart', 'Stop']) {
      expect(screen.getByRole('button', { name }), `${name} must be enabled again once terminal`).toBeEnabled()
    }
  })
})

describe('[AC-110] on remount an in-flight command is shown with its current step', () => {
  it('shows Updating with the step for a command already running on this container', async () => {
    mockGetActiveCommands.mockResolvedValue([makeCommand({ status: 'running', step: 'recreate', updated_at: ts(7) })])

    renderDetail()
    await flush()

    expect(screen.getByTestId('command-status')).toHaveTextContent('Updating… Recreating')
    expect(screen.getByRole('button', { name: 'Update' })).toBeDisabled()
  })
})

describe('[AC-111] a 409 conflict attaches to the existing command', () => {
  it('tracks the command the server named instead of showing an error', async () => {
    mockSendCommand.mockRejectedValue(new CommandConflictError({
      error: 'a command is already in progress for this target',
      code: 'conflict',
      existing_command_id: CMD_ID,
      existing_status: 'running',
      target_key: KEY,
    }))
    mockGetCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull', updated_at: ts(4) }))
    renderDetail()
    await flush()

    await confirmUpdate()

    expect(mockGetCommand).toHaveBeenCalledWith(DEVICE, CMD_ID)
    expect(screen.getByTestId('command-status')).toHaveTextContent('Updating… Pulling image')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

// Integration with useDevices: the real hook decides whether a stop/die becomes "exited".
function DeviceHarness({ initial }: { initial: ContainerInfo }) {
  useDevices()
  const { data } = useQuery({
    queryKey: ['device', DEVICE],
    queryFn: () => Promise.resolve({
      device: { id: DEVICE },
      latest_telemetry: { id: 1, device_id: DEVICE, timestamp: ts(0), data: { docker: { available: true, containers: [initial] } } },
    } as unknown as DeviceDetailResponse),
    staleTime: Infinity,
  })
  const c = data?.latest_telemetry?.data.docker?.containers?.[0]
  return c ? <ContainerDetailContent container={c} deviceId={DEVICE} /> : null
}

function dockerEvent(action: string): WSMessage {
  return { type: 'docker_update', device_id: DEVICE, data: { container_id: 'abc123', container_name: 'web', action } }
}

function pushWS(msg: WSMessage) {
  act(() => { ws.subscribers.forEach((fn) => fn(msg)) })
}

describe('[AC-112] stop and die during a tracked update show Updating, not exited', () => {
  it.each(['stop', 'die'])('keeps the running badge and shows Updating after a %s event', async (action) => {
    mockSendCommand.mockResolvedValue(makeCommand())
    render(
      <QueryClientProvider client={makeQC()}>
        <MemoryRouter><DeviceHarness initial={makeContainer()} /></MemoryRouter>
      </QueryClientProvider>,
    )
    await flush()
    await confirmUpdate()
    push(makeUpdate({ status: 'running', step: 'recreate', updated_at: ts(4) }))

    pushWS(dockerEvent(action))
    await flush()

    expect(screen.getByTestId('command-status')).toHaveTextContent('Updating… Recreating')
    expect(screen.queryByText('exited'), `a ${action} event during the update must not show exited`).not.toBeInTheDocument()
    expect(screen.getAllByText('running').length).toBeGreaterThan(0)
  })

  it('shows exited for a stop event when no update is tracked (as before)', async () => {
    render(
      <QueryClientProvider client={makeQC()}>
        <MemoryRouter><DeviceHarness initial={makeContainer()} /></MemoryRouter>
      </QueryClientProvider>,
    )
    await flush()

    pushWS(dockerEvent('stop'))
    await flush()

    expect(screen.getAllByText('exited').length).toBeGreaterThan(0)
    expect(screen.queryByTestId('command-status')).not.toBeInTheDocument()
  })
})

describe('[AC-114] terminal states render the frozen labels', () => {
  async function startAndFinish(overrides: Partial<CommandUpdate>) {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull' }))
    renderDetail()
    await flush()
    await confirmUpdate()
    push(makeUpdate({ updated_at: ts(9), ...overrides }))
  }

  it('shows Failed at <step>: <message> with a Dismiss button, until dismissed', async () => {
    await startAndFinish({ status: 'failed', step: 'pull', failure_step: 'pull', message: 'pull access denied' })

    expect(screen.getByTestId('command-status')).toHaveTextContent('Failed at pull: pull access denied')
    await flush(60_000)
    expect(screen.getByTestId('command-status'), 'a failure stays until dismissed').toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByTestId('command-status')).not.toBeInTheDocument()
  })

  it.each([
    ['timed_out', 'Timed out: outcome unknown'],
    ['expired', 'Expired: never delivered'],
    ['cancelled', 'Cancelled'],
  ] as const)('shows %s as "%s" and offers Dismiss', async (status, label) => {
    await startAndFinish({ status })

    expect(screen.getByTestId('command-status')).toHaveTextContent(label)
    expect(screen.getByRole('button', { name: 'Dismiss' })).toBeInTheDocument()
  })

  it('shows Updated for 5s after success, then the normal view returns', async () => {
    await startAndFinish({ status: 'succeeded', step: 'cleanup' })

    expect(screen.getByTestId('command-status')).toHaveTextContent('Updated')
    expect(screen.queryByRole('button', { name: 'Dismiss' })).not.toBeInTheDocument()

    await flush(4999)
    expect(screen.getByTestId('command-status')).toBeInTheDocument()
    await flush(1)
    expect(screen.queryByTestId('command-status')).not.toBeInTheDocument()
  })

  it('starting a new action clears a failure left on screen', async () => {
    await startAndFinish({ status: 'failed', step: 'pull', failure_step: 'pull', message: 'denied' })
    mockSendCommand.mockResolvedValue(makeCommand({ id: '22222222-2222-4222-8222-222222222222', status: 'queued', updated_at: ts(30) }))

    await confirmUpdate()

    expect(screen.getByTestId('command-status')).toHaveTextContent('Queued')
    expect(screen.queryByText(/Failed at pull/)).not.toBeInTheDocument()
  })
})

describe('[AC-116] Cancel is shown only while the command is queued', () => {
  it('is visible while queued and hidden once dispatched', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'queued' }))
    renderDetail()
    await flush()
    await confirmUpdate()

    const status = screen.getByTestId('command-status')
    expect(within(status).getByRole('button', { name: 'Cancel' })).toBeInTheDocument()

    push(makeUpdate({ status: 'dispatched', step: '', updated_at: ts(2) }))

    expect(screen.getByTestId('command-status')).toHaveTextContent('Sent to agent')
    expect(within(screen.getByTestId('command-status')).queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument()
  })

  it('cancels the queued command and shows Cancelled', async () => {
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'queued' }))
    mockCancelCommand.mockResolvedValue(makeCommand({ status: 'cancelled', updated_at: ts(3) }))
    renderDetail()
    await flush()
    await confirmUpdate()

    fireEvent.click(within(screen.getByTestId('command-status')).getByRole('button', { name: 'Cancel' }))
    await flush()

    expect(mockCancelCommand).toHaveBeenCalledWith(DEVICE, CMD_ID)
    expect(screen.getByTestId('command-status')).toHaveTextContent('Cancelled')
  })
})

describe('[AC-062] the Update button title states why an update is available', () => {
  it('uses the pulled-not-recreated reason', async () => {
    renderDetail(makeContainer({ update_reason: 'pulled_not_recreated' }))
    await flush()
    expect(screen.getByRole('button', { name: 'Update' })).toHaveAttribute(
      'title',
      'Update available: newer image pulled, container not recreated',
    )
  })

  it('offers no Update button for a pinned image and says why in the status', async () => {
    renderDetail(makeContainer({ update_status: 'pinned', update_available: false, update_reason: undefined }))
    await flush()
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument()
    expect(screen.getByText('Pinned by digest')).toBeInTheDocument()
  })

  it('offers no Update button after a failed check and says so in the status', async () => {
    renderDetail(makeContainer({ update_status: 'check_failed', update_available: null, update_reason: undefined }))
    await flush()
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument()
    expect(screen.getByText('Update check failed')).toBeInTheDocument()
  })
})

describe('[SEC] agent failure text renders as text', () => {
  it('shows an <img onerror> result_msg literally and creates no element', async () => {
    const hostile = '<img src=x onerror=alert(1)>'
    mockSendCommand.mockResolvedValue(makeCommand({ status: 'running', step: 'pull' }))
    const { container } = renderDetail()
    await flush()
    await confirmUpdate()

    push(makeUpdate({ status: 'failed', step: 'pull', failure_step: 'pull', message: hostile, updated_at: ts(9) }))

    const status = screen.getByTestId('command-status')
    expect(status.textContent, 'the markup must appear as literal text').toContain(hostile)
    expect(container.querySelector('img'), 'no <img> element may be created from agent text').toBeNull()
    expect(status.innerHTML).not.toContain('<img')
  })
})

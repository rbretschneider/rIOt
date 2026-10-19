import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders } from '../../test/renderWithProviders'
import ContainersPage from './ContainersPage'
import InventoryPage from './InventoryPage'
import StacksPage from './StacksPage'
import StackPage from './StackPage'
import LegacyRedirect from './LegacyRedirect'
import LogView from '../../components/docker/LogView'
const mock = vi.hoisted(() => ({
  hosts: vi.fn(),
  containers: vi.fn(),
  bulk: vi.fn(),
  inventory: vi.fn(),
  preview: vi.fn(),
  execute: vi.fn(),
  history: vi.fn(),
  stacks: vi.fn(),
  stack: vi.fn(),
  stackAction: vi.fn(),
  acknowledge: vi.fn(),
  compose: vi.fn(),
  composeFile: vi.fn(),
  env: vi.fn(),
  activity: vi.fn(),
  getDevice: vi.fn(),
  getCommand: vi.fn(),
  sendCommand: vi.fn(),
  getAutoUpdates: vi.fn(),
  getActiveCommands: vi.fn(),
  setAutoUpdate: vi.fn(),
}))
vi.mock('../../api/docker', () => ({ dockerApi: mock }))
vi.mock('../../api/client', () => ({
  api: {
    getDevice: mock.getDevice,
    getCommand: mock.getCommand,
    sendCommand: mock.sendCommand,
    getAutoUpdates: mock.getAutoUpdates,
    getActiveCommands: mock.getActiveCommands,
    setAutoUpdate: mock.setAutoUpdate,
  },
}))
const host = {
  device_id: 'host-1',
  hostname: 'host-one',
  status: 'online',
  docker_available: true,
  capabilities: [
    'logs_live_v1',
    'stack_actions_v1',
    'compose_read_v1',
    'prune_v1',
    'inventory_v1',
  ],
  inventory_enabled: true,
  container_logs: 'enabled',
}
const row = {
  device_id: 'host-1',
  hostname: 'host-one',
  container_id: 'id1',
  short_id: 'short1',
  container_name: 'web',
  image: 'nginx:latest',
  state: 'running',
  status: 'Up',
  stack: 'app',
  cpu_percent: 2,
  mem_usage: 1024,
  mem_limit: 2048,
  restart_count: 0,
  update_available: true,
}
beforeEach(() => {
  vi.clearAllMocks()
  mock.getActiveCommands.mockResolvedValue([])
  mock.getAutoUpdates.mockResolvedValue([])
  mock.setAutoUpdate.mockResolvedValue({})
  mock.hosts.mockResolvedValue([host])
  mock.containers.mockResolvedValue([
    row,
    { ...row, container_id: 'id2', container_name: 'db', state: 'exited' },
  ])
  mock.bulk.mockResolvedValue({
    results: [
      {
        device_id: 'host-1',
        container: 'web',
        outcome: 'skipped',
        reason: 'not_applicable',
      },
    ],
  })
  mock.history.mockResolvedValue({
    entries: [
      { t: '2026-10-19T10:00:00Z', s: 'o', l: 'hello' },
      {
        t: '2026-10-19T10:00:01Z',
        gap: { count: 20, lower_bound: false, reason: 'quota_sampled' },
      },
    ],
    server_now: '2026-10-19T10:00:10Z',
    retention_days: 7,
    pre_recreate_history: true,
    collector: 'enabled',
    has_more: false,
  })
  mock.inventory.mockResolvedValue({
    hosts: [
      {
        ...host,
        state: 'ok',
        stale: false,
        truncated: false,
        collected_at: '2026-10-19T10:00:00Z',
      },
    ],
    items: [
      {
        device_id: 'host-1',
        name: 'data',
        driver: 'local',
        used_by: [],
        size: 1000,
        anonymous: false,
      },
    ],
  })
  mock.preview.mockResolvedValue({
    preview_id: 'token',
    item_kind: 'volume',
    items: [{ id: 'data', name: 'data', selected_default: false, size: 1000 }],
    count: 1,
    reclaimable_bytes: 1000,
    inventory_age_s: 1,
    requires_refresh: false,
    confirmation: { kind: 'host' },
  })
  mock.execute.mockResolvedValue({ id: 'cmd-1' })
  mock.getCommand.mockResolvedValue({
    id: 'cmd-1',
    action: 'docker_remove_items',
    status: 'completed',
    result_msg: 'Removed',
  })
  mock.stacks.mockResolvedValue([
    {
      device_id: 'host-1',
      hostname: 'host-one',
      project: 'app',
      state: 'down',
      running: 0,
      total: 0,
      updates: 0,
      path_state: 'agreed',
      agreed_working_dir: '/srv/app',
      agreed_config_files: ['compose.yaml'],
      last_seen_at: '2026-10-19T10:00:00Z',
    },
  ])
  mock.stack.mockImplementation(async () => (await mock.stacks())[0])
  mock.acknowledge.mockResolvedValue({ path_state: 'agreed' })
  mock.stackAction.mockResolvedValue({ id: 'cmd-1' })
  mock.getDevice.mockResolvedValue({
    device: { id: 'host-1', hostname: 'host-one' },
    latest_telemetry: {
      data: {
        docker: {
          containers: [{ id: 'fullid', short_id: 'shortid', name: 'web' }],
        },
      },
    },
  })
})
describe('Docker fleet', () => {
  it('shows fleet columns, links by host and name, and persists a status filter', async () => {
    const user = userEvent.setup()
    renderWithProviders(<ContainersPage />, {
      route: '/docker/containers',
      showLocation: true,
    })
    expect(await screen.findByRole('link', { name: 'web' })).toHaveAttribute(
      'href',
      '/docker/containers/host-1/web',
    )
    expect(
      screen.getByRole('columnheader', { name: 'CPU' }),
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /stopped \(/ }))
    expect(screen.getByTestId('location')).toHaveTextContent('status=stopped')
    expect(screen.queryByRole('link', { name: 'web' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'db' })).toBeInTheDocument()
  })
  it('reports errors separately from empty results with a retry', async () => {
    mock.containers.mockRejectedValue(new Error('Database unavailable'))
    renderWithProviders(<ContainersPage />)
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Database unavailable',
    )
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    expect(screen.queryByText('No matching containers')).not.toBeInTheDocument()
  })
  it('previews skipped targets and requires the typed count before a bulk stop', async () => {
    const user = userEvent.setup()
    renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    await user.click(
      screen.getByRole('checkbox', { name: 'Select host-1.web' }),
    )
    await user.click(screen.getByRole('button', { name: 'stop' }))
    const confirm = screen.getByRole('button', { name: 'Confirm stop' })
    expect(confirm).toBeDisabled()
    expect(mock.bulk).not.toHaveBeenCalled()
    await user.type(screen.getByLabelText('Confirmation'), '1')
    await user.click(confirm)
    await waitFor(() =>
      expect(mock.bulk).toHaveBeenCalledWith({
        action: 'stop',
        targets: [{ device_id: 'host-1', container: 'web' }],
        confirm_count: 1,
      }),
    )
    expect(await screen.findByText(/web: skipped/)).toBeInTheDocument()
  })
})
describe('Docker legacy links', () => {
  it('resolves old short ids into canonical name URLs', async () => {
    renderWithProviders(<LegacyRedirect />, {
      route: '/devices/host-1/containers/shortid',
      path: '/devices/:id/containers/:cid',
      showLocation: true,
      routes: [
        {
          path: '/docker/containers/:deviceId/:name',
          element: <p>Canonical</p>,
        },
      ],
    })
    expect(await screen.findByText('Canonical')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent(
      '/docker/containers/host-1/web',
    )
  })
  it('handles a missing container without guessing a new target', async () => {
    renderWithProviders(<LegacyRedirect />, {
      route: '/devices/host-1/containers/missing',
      path: '/devices/:id/containers/:cid',
    })
    expect(await screen.findByText('Container not found')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Back to Containers' }),
    ).toHaveAttribute('href', '/docker/containers?host=host-1')
  })
})
describe('One Logs view', () => {
  it('opens stored history with Live off and displays sampled storage gaps', async () => {
    const socket = vi.fn()
    vi.stubGlobal('WebSocket', socket)
    renderWithProviders(<LogView deviceId="host-1" name="web" liveSupported />)
    expect(
      await screen.findByText('hello', { exact: false }),
    ).toBeInTheDocument()
    expect(socket).not.toHaveBeenCalled()
    expect(screen.getByRole('checkbox', { name: 'Live' })).not.toBeChecked()
    expect(
      screen.getByText(
        'Stored history is sampled (storage quota); Live shows every line',
      ),
    ).toBeInTheDocument()
    vi.unstubAllGlobals()
  })
  it('requires a capability before offering Live', async () => {
    renderWithProviders(
      <LogView deviceId="host-1" name="web" liveSupported={false} />,
    )
    expect(screen.getByRole('checkbox', { name: 'Live' })).toBeDisabled()
    expect(
      await screen.findByText('Requires agent update for Live'),
    ).toBeInTheDocument()
  })
  it('attaches only after an explicit toggle and detaches on unmount', async () => {
    const sent: string[] = []
    let socket: any
    class Socket {
      static OPEN = 1
      readyState = 1
      onopen?: () => void
      onmessage?: unknown
      onclose?: () => void
      send = (s: string) => sent.push(s)
      close = vi.fn()
      constructor() {
        socket = this
      }
    }
    vi.stubGlobal('WebSocket', Socket)
    const user = userEvent.setup()
    const view = renderWithProviders(
      <LogView deviceId="host-1" name="web" liveSupported />,
    )
    await screen.findByText('hello', { exact: false })
    await user.click(screen.getByRole('checkbox', { name: 'Live' }))
    socket.onopen()
    expect(JSON.parse(sent[0]).type).toBe('attach')
    expect(JSON.parse(sent[0]).containers[0].boundary).toBe(
      '2026-10-19T10:00:01Z',
    )
    act(() =>
      socket.onmessage({
        data: JSON.stringify({
          type: 'lines',
          container: 'web',
          src: 'backfill',
          lines: [{ t: '2026-10-19T10:00:00Z', s: 'o', l: 'hello' }],
        }),
      }),
    )
    expect(screen.getAllByText('hello', { exact: false })).toHaveLength(1)
    act(() =>
      socket.onmessage({
        data: JSON.stringify({
          type: 'marker',
          container: 'web',
          kind: 'backfill_end',
          skipped: 30,
          skipped_lb: true,
        }),
      }),
    )
    expect(
      screen.getByText(/At least 30 lines not stored \(backfill limit\)/),
    ).toBeInTheDocument()
    view.unmount()
    expect(JSON.parse(sent[1]).type).toBe('detach')
    expect(socket.close).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
describe('Stacks', () => {
  it('checks updates for this stack member IDs and skips members being removed', async () => {
    const user = userEvent.setup()
    const stack = (await mock.stacks())[0]
    mock.stack.mockResolvedValue({
      ...stack,
      state: 'running',
      running: 2,
      total: 2,
      services: [
        row,
        {
          ...row,
          container_id: 'removing-id',
          container_name: 'removing',
          state: 'removing',
        },
      ],
    })
    mock.sendCommand.mockResolvedValue({
      id: 'check-command',
      action: 'docker_check_updates',
      status: 'succeeded',
      params: {},
      updated_at: '2026-10-19T10:00:00Z',
      created_at: '2026-10-19T10:00:00Z',
    })
    renderWithProviders(<StackPage />, {
      route: '/docker/stacks/host-1/app',
      path: '/docker/stacks/:deviceId/:project',
    })
    await screen.findByRole('heading', { name: 'app' })
    await user.click(screen.getByRole('button', { name: 'Check updates' }))
    await waitFor(() =>
      expect(mock.sendCommand).toHaveBeenCalledWith(
        'host-1',
        'docker_check_updates',
        { compose_project: 'app', container_ids: ['id1'] },
      ),
    )
  })
  it('sends the compose project separately from the typed host for a self-stack Down', async () => {
    const user = userEvent.setup()
    const stack = (await mock.stacks())[0]
    mock.stack.mockResolvedValue({
      ...stack,
      state: 'running',
      running: 1,
      total: 1,
      self_role: 'server',
    })
    renderWithProviders(<StackPage />, {
      route: '/docker/stacks/host-1/app',
      path: '/docker/stacks/:deviceId/:project',
    })
    await screen.findByRole('heading', { name: 'app' })
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await user.click(screen.getByRole('menuitem', { name: 'Down' }))
    await user.type(
      screen.getByRole('textbox', { name: 'Stack confirmation' }),
      'host-one',
    )
    await user.click(screen.getByRole('button', { name: 'Confirm down' }))
    await waitFor(() =>
      expect(mock.stackAction).toHaveBeenCalledWith('host-1', 'app', {
        action: 'down',
        confirm: 'app',
        confirm_host: 'host-one',
      }),
    )
  })
  it('keeps down stacks listed with canonical links', async () => {
    renderWithProviders(<StacksPage />)
    expect(await screen.findByRole('link', { name: 'app' })).toHaveAttribute(
      'href',
      '/docker/stacks/host-1/app',
    )
    expect(screen.getByText('down')).toBeInTheDocument()
  })
  it('blocks stack actions when compose paths changed and requires acknowledgement', async () => {
    const user = userEvent.setup()
    const s = (await mock.stacks())[0]
    mock.stack.mockResolvedValue({
      ...s,
      path_state: 'pending_ack',
      observed_working_dir: '/srv/new',
      observed_config_files: ['new.yaml'],
    })
    renderWithProviders(<StackPage />, {
      route: '/docker/stacks/host-1/app',
      path: '/docker/stacks/:deviceId/:project',
    })
    expect(await screen.findByRole('button', { name: 'Up' })).toBeDisabled()
    await user.click(
      screen.getByRole('button', { name: 'Review changed paths' }),
    )
    expect(
      screen.getByRole('button', { name: 'Acknowledge paths' }),
    ).toBeDisabled()
    await user.click(
      screen.getByRole('checkbox', { name: /reviewed these paths/ }),
    )
    await user.click(screen.getByRole('button', { name: 'Acknowledge paths' }))
    await waitFor(() =>
      expect(mock.acknowledge).toHaveBeenCalledWith('host-1', 'app', {
        working_dir: '/srv/new',
        config_files: ['new.yaml'],
      }),
    )
  })
})
describe('Inventory cleanup', () => {
  it('previews first, leaves named volumes unchecked and requires host confirmation', async () => {
    const user = userEvent.setup()
    renderWithProviders(<InventoryPage kind="volumes" />, {
      route: '/docker/volumes?host=host-1',
    })
    await screen.findByRole('cell', { name: 'data' })
    await user.click(
      screen.getByRole('button', { name: 'Preview unused cleanup' }),
    )
    expect(mock.execute).not.toHaveBeenCalled()
    const checkbox = screen
      .getByRole('dialog')
      .querySelector<HTMLInputElement>('input[type=checkbox]')!
    expect(checkbox).not.toBeChecked()
    const button = screen.getByRole('button', { name: 'Remove selected' })
    expect(button).toBeDisabled()
    await user.click(checkbox)
    await user.type(screen.getByLabelText('Cleanup confirmation'), 'host-one')
    await user.click(button)
    await waitFor(() =>
      expect(mock.execute).toHaveBeenCalledWith('host-1', {
        preview_id: 'token',
        selected: ['data'],
        confirm: 'host-one',
      }),
    )
  })
  it('blocks cleanup when the selected host inventory is stale', async () => {
    const inv = await mock.inventory()
    mock.inventory.mockResolvedValue({
      ...inv,
      hosts: inv.hosts.map((h: any) => ({ ...h, stale: true })),
    })
    renderWithProviders(<InventoryPage kind="volumes" />, {
      route: '/docker/volumes?host=host-1',
    })
    expect(
      await screen.findByText('Inventory is stale. Refresh before cleanup.'),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Preview unused cleanup' }),
    ).toBeDisabled()
  })
})

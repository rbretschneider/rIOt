import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {
  makeTestQueryClient,
  renderWithProviders,
} from '../../test/renderWithProviders'
import ContainersPage from './ContainersPage'
import ContainerPage from './ContainerPage'
import InventoryPage from './InventoryPage'
import StacksPage from './StacksPage'
import ActivityPage from './ActivityPage'
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
  localStorage.removeItem('riot-docker-columns')
  localStorage.removeItem('riot-docker-view')
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
  it.each([
    { saved: ['name'] },
    { saved: [] },
    { saved: ['removed-column'] },
  ])('[AC-001] keeps Name compact when saved preferences leave it as the only column: $saved', async ({ saved }) => {
    localStorage.setItem('riot-docker-columns', JSON.stringify(saved))
    renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    const table = screen.getByRole('grid')
    expect(table).toHaveStyle({ width: '228px' })
    expect(table.querySelectorAll('col')[1]).toHaveStyle({ width: '184px' })
    expect(table).toHaveAttribute('aria-colcount', '2')
    expect(screen.getAllByLabelText('Update available')).toHaveLength(2)
  })
  it('[AC-001] keeps Name compact and pinned with default and restored columns', async () => {
    let view = renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    const nameColumn = () => screen.getByRole('grid').querySelectorAll('col')[1]
    expect(nameColumn()).toHaveStyle({ width: '184px' })
    expect(screen.getByRole('columnheader', { name: 'Name' })).toHaveStyle({ left: '44px' })
    view.unmount()
    localStorage.setItem('riot-docker-columns', JSON.stringify(['name', 'stack']))
    view = renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    expect(nameColumn()).toHaveStyle({ width: '184px' })
    expect(screen.queryByRole('columnheader', { name: 'Open' })).not.toBeInTheDocument()
  })
  it('[AC-002][AC-005] keeps updates beside long names with saved hidden Image and without duplicate badges', async () => {
    const user = userEvent.setup()
    const name = 'a-very-long-container-name-that-needs-truncating'
    mock.containers.mockResolvedValue([{ ...row, container_name: name }])
    localStorage.setItem('riot-docker-columns', JSON.stringify(['name', 'stack', 'links']))
    renderWithProviders(<ContainersPage />)
    const link = await screen.findByRole('link', { name })
    expect(link).toHaveAttribute('title', `${name} · nginx:latest`)
    expect(link).toHaveAttribute('href', `/docker/containers/host-1/${name}`)
    expect(screen.getByLabelText('Update available')).toBeVisible()
    expect(screen.queryByRole('columnheader', { name: 'Image' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Columns' }))
    await user.click(screen.getByRole('checkbox', { name: 'Image' }))
    expect(screen.getByRole('columnheader', { name: 'Image' })).toBeInTheDocument()
    expect(screen.getAllByLabelText('Update available')).toHaveLength(1)
  })
  it.each([
    ['update_available', true, true],
    ['update_available', false, true],
    ['pinned', true, false],
    ['check_failed', true, false],
    ['up_to_date', true, false],
    [undefined, true, true],
    [undefined, false, false],
    [undefined, null, false],
    [undefined, undefined, false],
  ])('[AC-003] shows availability using verdict %s and legacy value %s', async (status, available, visible) => {
    mock.containers.mockResolvedValue([{ ...row, update_status: status, update_available: available }])
    renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    expect(screen.queryByLabelText('Update available') !== null).toBe(visible)
  })
  it('[AC-003] shows known updates on stopped containers without dispatching an action', async () => {
    mock.containers.mockResolvedValue([{ ...row, state: 'exited', update_status: 'update_available' }])
    renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    expect(screen.getByLabelText('Update available')).toBeVisible()
    expect(mock.bulk).not.toHaveBeenCalled()
  })
  it('[AC-004] defaults to Stack then host and name, keeping standalone together in either direction', async () => {
    const user = userEvent.setup()
    mock.containers.mockResolvedValue([
      { ...row, container_name: 'zulu', stack: 'alpha', hostname: 'host-z', device_id: 'host-z' },
      { ...row, container_name: 'beta-web', stack: 'beta' },
      { ...row, container_name: 'alpha-web', stack: 'alpha' },
      { ...row, container_name: 'solo-z', stack: '' },
      { ...row, container_name: 'alpha-db', stack: 'alpha' },
      { ...row, container_name: 'solo-a', stack: '' },
    ])
    renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'alpha-db' })
    const names = () => within(screen.getByRole('grid')).getAllByRole('row').slice(1)
      .map((r) => within(r).getAllByRole('link')[0].textContent!.trim())
    expect(screen.getByRole('columnheader', { name: 'Stack' })).toHaveAttribute('aria-sort', 'ascending')
    expect(names()).toEqual(['solo-a', 'solo-z', 'alpha-db', 'alpha-web', 'zulu', 'beta-web'])
    await user.click(screen.getByRole('button', { name: 'Stack' }))
    expect(names()).toEqual(['beta-web', 'alpha-db', 'alpha-web', 'zulu', 'solo-a', 'solo-z'])
  })
  it('[AC-004] honors an explicit Name sort instead of default Stack ordering', async () => {
    mock.containers.mockResolvedValue([
      { ...row, container_name: 'zulu', stack: 'alpha' },
      { ...row, container_name: 'alpha', stack: 'zulu' },
    ])
    renderWithProviders(<ContainersPage />, { route: '/docker/containers?sort=name:asc' })
    await screen.findByRole('link', { name: 'Logs for zulu' })
    const rows = within(screen.getByRole('grid')).getAllByRole('row').slice(1)
    expect(within(rows[0]).getAllByRole('link')[0]).toHaveTextContent('alpha')
    expect(screen.getByRole('columnheader', { name: 'Name' })).toHaveAttribute('aria-sort', 'ascending')
  })
  it('remembers visible columns across visits and restores the default columns', async () => {
    const user = userEvent.setup()
    let view = renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    expect(
      screen.queryByRole('columnheader', { name: 'Image' }),
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Columns' }))
    await user.click(screen.getByRole('checkbox', { name: 'Image' }))
    expect(
      screen.getByRole('columnheader', { name: 'Image' }),
    ).toBeInTheDocument()
    view.unmount()

    view = renderWithProviders(<ContainersPage />)
    expect(
      await screen.findByRole('columnheader', { name: 'Image' }),
    ).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Columns' }))
    await user.click(screen.getByRole('checkbox', { name: 'Image' }))
    expect(
      screen.queryByRole('columnheader', { name: 'Image' }),
    ).not.toBeInTheDocument()
    view.unmount()

    view = renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    expect(
      screen.queryByRole('columnheader', { name: 'Image' }),
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Columns' }))
    await user.click(screen.getByRole('checkbox', { name: 'Image' }))
    await user.click(screen.getByRole('button', { name: 'Reset columns' }))
    expect(
      screen.queryByRole('columnheader', { name: 'Image' }),
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('columnheader', { name: 'CPU' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Name' })).toBeDisabled()
    view.unmount()

    renderWithProviders(<ContainersPage />)
    await screen.findByRole('link', { name: 'web' })
    expect(
      screen.queryByRole('columnheader', { name: 'Image' }),
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('columnheader', { name: 'CPU' }),
    ).toBeInTheDocument()
  })
  it('opens canonical container logs directly and returns to the filtered collection', async () => {
    const user = userEvent.setup()
    const collection =
      '/docker/containers?host=host-1&status=running&q=web&sort=cpu%3Adesc'
    const queryClient = makeTestQueryClient()
    queryClient.setQueryDefaults(['feature-toggles'], { gcTime: Infinity })
    queryClient.setQueryData(['feature-toggles'], {})
    mock.getDevice.mockResolvedValue({
      device: { id: 'host-1', hostname: 'host-one' },
      latest_telemetry: {
        data: { docker: { containers: [{ ...row, id: 'id1', name: 'web' }] } },
      },
    })
    renderWithProviders(<ContainersPage />, {
      route: collection,
      path: '/docker/containers',
      routes: [
        {
          path: '/docker/containers/:deviceId/:name',
          element: <ContainerPage />,
        },
      ],
      showLocation: true,
      queryClient,
    })
    const shortcut = await screen.findByRole('link', { name: 'Logs for web' })
    expect(shortcut).toHaveAttribute(
      'href',
      '/docker/containers/host-1/web?tab=logs',
    )
    await user.click(shortcut)
    expect(
      await screen.findByRole('heading', { name: 'web' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent(
      '/docker/containers/host-1/web?tab=logs',
    )
    expect(
      await screen.findByText('hello', { exact: false }),
    ).toBeInTheDocument()
    const back = screen.getByRole('link', { name: '← Containers' })
    expect(back).toHaveAttribute('href', collection)
    await user.click(back)
    expect(await screen.findByRole('link', { name: 'web' })).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent(collection)
    expect(
      screen.getByRole('textbox', { name: 'Search containers' }),
    ).toHaveValue('web')
    expect(screen.queryByRole('link', { name: 'db' })).not.toBeInTheDocument()
  })
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
  it('[AC-001][AC-007] opens stack update in place and preserves collection filters', async () => {
    const user = userEvent.setup()
    renderWithProviders(<StacksPage />, { route: '/docker/stacks?host=host-1&q=app', showLocation: true })
    await user.click(await screen.findByRole('button', { name: 'Update app on host-one' }))
    expect(await screen.findByRole('dialog', { name: 'Update app' })).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/docker/stacks?host=host-1&q=app')
    expect(mock.stackAction).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Search stacks' })).toHaveValue('app')
  })
  it.each([
    { state: 'running', running: 2, total: 2, updates: 1 },
    { state: 'running', running: 2, total: 2, updates: 0 },
    { state: 'partial', running: 1, total: 2, updates: 1 },
    { state: 'down', running: 0, total: 2, updates: 0 },
  ])('[AC-002] exposes exactly one detail Update for $state with $updates updates', async (state) => {
    mock.stack.mockResolvedValue({ ...(await mock.stacks())[0], ...state })
    renderWithProviders(<StackPage />, { route: '/docker/stacks/host-1/app', path: '/docker/stacks/:deviceId/:project' })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Update' })).toBeEnabled())
    expect(screen.getAllByRole('button', { name: 'Update' })).toHaveLength(1)
    if (state.state !== 'running') expect(screen.getByRole('button', { name: 'Up' })).toBeInTheDocument()
  })
  it('[AC-007] isolates identical project names on different hosts while an old read resolves late', async () => {
    const user = userEvent.setup()
    const first = (await mock.stacks())[0]
    const second = { ...first, device_id: 'host-2', hostname: 'host-two', agreed_working_dir: '/srv/other' }
    mock.stacks.mockResolvedValue([first, second])
    mock.hosts.mockResolvedValue([host, { ...host, device_id: 'host-2', hostname: 'host-two' }])
    let resolveFirst!: (value: typeof first) => void
    mock.stack.mockImplementation((id: string) => id === 'host-1' ? new Promise((done) => { resolveFirst = done }) : Promise.resolve(second))
    mock.stackAction.mockResolvedValue({ id: 'cmd-2', device_id: 'host-2', action: 'docker_update', status: 'running', params: {}, updated_at: '2026-10-20T10:00:00Z' })
    mock.getCommand.mockResolvedValue({ id: 'cmd-2', device_id: 'host-2', action: 'docker_update', status: 'running', params: {}, updated_at: '2026-10-20T10:00:00Z' })
    renderWithProviders(<StacksPage />)
    await user.click(await screen.findByRole('button', { name: 'Update app on host-one' }))
    await user.click(screen.getByRole('button', { name: 'Close' }))
    await user.click(screen.getByRole('button', { name: 'Update app on host-two' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm update' })).toBeEnabled())
    await act(async () => { resolveFirst(first) })
    expect(within(screen.getByRole('dialog')).getByText('host-two')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Confirm update' }))
    await waitFor(() => expect(mock.stackAction).toHaveBeenCalledExactlyOnceWith('host-2', 'app', { action: 'update' }))
    await waitFor(() => expect(mock.getCommand).toHaveBeenCalledWith('host-2', 'cmd-2'))
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

describe('[APP-WORKSPACE AC-008] Docker host options', () => {
  const offline = {
    ...host,
    device_id: 'host-offline',
    hostname: 'offline-docker',
    status: 'offline',
  }
  const unavailable = {
    ...host,
    device_id: 'host-unavailable',
    hostname: 'no-docker',
    docker_available: false,
  }
  beforeEach(() => {
    mock.hosts.mockResolvedValue([host, offline, unavailable])
  })
  it('includes offline Docker hosts in container filters and excludes other machines', async () => {
    const user = userEvent.setup()
    const client = makeTestQueryClient()
    renderWithProviders(<ContainersPage />, {
      route: '/docker/containers',
      queryClient: client,
    })
    await screen.findByRole('link', { name: 'web' })
    await user.click(screen.getByRole('button', { name: 'Hosts: All' }))
    expect(
      screen.getByRole('checkbox', { name: 'host-one' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('checkbox', { name: 'offline-docker' }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('checkbox', { name: 'no-docker' }),
    ).not.toBeInTheDocument()
    expect(client.getQueryData(['docker-hosts'])).toEqual([
      host,
      offline,
      unavailable,
    ])
  })
  it.each([
    ['stacks', 'Stack host'],
    ['inventory', 'Inventory host'],
    ['activity', 'Activity host'],
  ])(
    'filters %s host options using availability rather than online state',
    async (page, label) => {
      mock.stacks.mockResolvedValue([])
      mock.activity.mockResolvedValue({ entries: [] })
      mock.inventory.mockResolvedValue({
        hosts: [host, offline, unavailable].map((h) => ({ ...h, state: 'ok' })),
        items: [],
      })
      renderWithProviders(
        page === 'stacks' ? (
          <StacksPage />
        ) : page === 'activity' ? (
          <ActivityPage />
        ) : (
          <InventoryPage kind="images" />
        ),
        { route: '/docker/' + page },
      )
      const select = screen.getByRole('combobox', { name: label })
      expect(
        await within(select).findByRole('option', { name: 'host-one' }),
      ).toBeInTheDocument()
      expect(
        within(select).getByRole('option', { name: 'offline-docker' }),
      ).toBeInTheDocument()
      expect(
        within(select).queryByRole('option', { name: 'no-docker' }),
      ).not.toBeInTheDocument()
    },
  )
})

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor, within } from '@testing-library/react'
import FleetOverview from './FleetOverview'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'
import type { Device, DeviceActivity, WSMessage } from '../types/models'

// EXPECT-008 (AC-022, FR-024, FR-025): the fleet page for an installer re-run. The real
// useDevices and useActivity run; only the api and the WebSocket are replaced, so a pushed
// activity_update takes the same cache path as in the browser.
vi.mock('../api/client', () => ({
  api: {
    getDevices: vi.fn(),
    getActivity: vi.fn(),
    getServerUpdate: vi.fn(),
    getPatchStatus: vi.fn(),
    getAgentVersions: vi.fn(),
    getPatchStatusDetail: vi.fn(),
    bulkUpdateAgents: vi.fn(),
    bulkPatchDevices: vi.fn(),
  },
}))

const ws = { handlers: new Set<(msg: WSMessage) => void>(), send(msg: WSMessage) { this.handlers.forEach(h => h(msg)) } }
vi.mock('../hooks/useWebSocket', () => ({
  useWebSocket: (handler: (msg: WSMessage) => void) => {
    ws.handlers.add(handler)
    return { connected: true }
  },
}))

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

vi.mock('../components/SetupGuide', () => ({
  default: () => <div data-testid="setup-guide" />,
}))

const NOW = Date.parse('2026-10-27T13:16:00Z')
const timeZone = { now: () => NOW }

function device(id: string, hostname: string, over: Partial<Device> = {}): Device {
  return {
    id,
    hostname,
    short_id: id,
    arch: 'arm64',
    status: 'online',
    agent_version: '2.82.0',
    primary_ip: '10.0.0.1',
    tags: [],
    last_heartbeat: '2026-10-27T13:15:05Z',
    ...over,
  } as Device
}

function reinstall(deviceId: string, over: Partial<DeviceActivity> = {}): DeviceActivity {
  return {
    device_id: deviceId,
    kind: 'agent',
    phase: 'in_progress',
    action: 'agent_update',
    origin: 'installer',
    command_id: `cmd-${deviceId}`,
    actor_phrase: 'by installer',
    started_at: '2026-10-27T13:15:00Z',
    back_by: null,
    waiting_dpkg_lock: false,
    progress: null,
    agent_from_version: '',
    agent_to_version: '',
    target: '',
    docker_actions: 0,
    covers_offline: true,
    device_online: false,
    escalation: null,
    powered_off_at: null,
    ...over,
  }
}

function serveActivities(activities: DeviceActivity[]) {
  vi.mocked(api.getActivity).mockResolvedValue({
    generated_at: '2026-10-27T13:16:00Z',
    newest_agent_version: '2.83.0',
    activities,
  })
}

function statValue(label: string): string {
  return screen.getByText(label).nextElementSibling?.textContent ?? ''
}

async function rowOf(hostname: string): Promise<HTMLElement> {
  const link = await screen.findByRole('link', { name: hostname })
  return link.closest('tr') as HTMLElement
}

beforeEach(() => {
  vi.clearAllMocks()
  ws.handlers.clear()
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.83.0' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getAgentVersions).mockResolvedValue([])
  serveActivities([])
})

describe('EXPECT-008 [AC-022] fleet row shows Reinstalling agent with actor and start in the title, counts expected offline, dot offline', () => {
  it('shows "Reinstalling agent" on one line with the actor phrase and start time in the title', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('c', 'PiCUPS', { status: 'offline' })])
    serveActivities([reinstall('c')])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('PiCUPS')
    const label = await within(row).findByTestId('activity-label')
    expect(within(label).getByText('Reinstalling agent')).toBeInTheDocument()
    expect(label.getAttribute('title'), 'the originator must be named').toContain('by installer')
    expect(label.getAttribute('title'), 'the start time must be shown').toContain('· started ')
    expect(label.querySelector('p, div'), 'the row stays one line: a single label element').toBeNull()
  })

  it('does not turn the reinstall into the Version-cell spinner', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('c', 'PiCUPS', { status: 'offline' })])
    serveActivities([reinstall('c')])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('PiCUPS')
    await within(row).findByTestId('activity-label')
    expect(within(row).queryByTestId('agent-updating'), 'a reinstall has no target version').toBeNull()
    expect(within(row).getByText('2.82.0'), 'the Version cell keeps the installed version').toBeInTheDocument()
  })

  it('counts the offline device as expected offline, not offline, and keeps its offline dot', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('c', 'PiCUPS', { status: 'offline' }), device('o', 'online-one')])
    serveActivities([reinstall('c')])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('PiCUPS')
    await within(row).findByTestId('activity-label')
    expect(statValue('Expected offline')).toBe('1')
    expect(statValue('Offline'), 'a device inside its reinstall window is not a real outage').toBe('0')
    expect(within(row).getByTestId('status-dot-expected-offline'), 'the device must still look offline').toBeInTheDocument()
  })

  it('shows the label on an online device too, with no expected-offline count', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('c', 'PiCUPS')])
    serveActivities([reinstall('c', { device_online: true })])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('PiCUPS')
    expect(await within(row).findByText('Reinstalling agent')).toBeInTheDocument()
    expect(screen.queryByText('Expected offline')).toBeNull()
  })

  it('shows the same label in a browser opened afterwards, because it comes from the server', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('c', 'PiCUPS', { status: 'offline' })])
    serveActivities([reinstall('c')])
    const first = renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByText('Reinstalling agent')
    first.unmount()

    renderWithProviders(<FleetOverview />, { timeZone })
    expect(await screen.findByText('Reinstalling agent')).toBeInTheDocument()
  })

  it('labels each device on its own: two devices back to back, and none on a third', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([
      device('c', 'PiCUPS', { status: 'offline' }),
      device('v', 'PiVPN', { status: 'offline' }),
      device('h', 'hawaii', { status: 'offline' }),
    ])
    serveActivities([reinstall('c'), reinstall('v')])
    renderWithProviders(<FleetOverview />, { timeZone })

    expect(await within(await rowOf('PiCUPS')).findByText('Reinstalling agent')).toBeInTheDocument()
    expect(await within(await rowOf('PiVPN')).findByText('Reinstalling agent')).toBeInTheDocument()
    expect(within(await rowOf('hawaii')).queryByTestId('activity-label')).toBeNull()
    expect(statValue('Expected offline')).toBe('2')
    expect(statValue('Offline'), 'the third device is a real outage').toBe('1')
  })
})

describe('EXPECT-008 [AC-022] the fleet page follows the window live', () => {
  it('shows the label when the window opens and clears it when the window closes, with no reload', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('c', 'PiCUPS')])
    const { queryClient } = renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByRole('link', { name: 'PiCUPS' })
    await waitFor(() => expect(queryClient.getQueryState(['activity'])?.status).toBe('success'))
    expect(screen.queryByText('Reinstalling agent')).toBeNull()

    act(() => {
      ws.send({ type: 'activity_update', device_id: 'c', data: reinstall('c', { device_online: true }) })
    })
    expect(await screen.findByText('Reinstalling agent')).toBeInTheDocument()

    act(() => {
      ws.send({ type: 'activity_update', device_id: 'c', data: null })
    })
    await waitFor(() => expect(screen.queryByText('Reinstalling agent'), 'the label must clear when the window closes').toBeNull())
  })

  it('moves an offline device back to the offline count when the window closes without the device returning', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('c', 'PiCUPS', { status: 'offline' })])
    serveActivities([reinstall('c')])
    renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByText('Reinstalling agent')
    expect(statValue('Expected offline')).toBe('1')

    act(() => {
      ws.send({ type: 'activity_update', device_id: 'c', data: null })
    })

    await waitFor(() => expect(statValue('Offline')).toBe('1'))
    expect(screen.queryByText('Expected offline'), 'no device is expected offline any more').toBeNull()
  })
})

describe('EXPECT-008 [AC-012] the fleet page for a reinstall that was not back in time', () => {
  it('shows "Not back 10m after Agent reinstall" with failure styling and counts the device as offline', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'PiVPN', { status: 'offline' })])
    serveActivities([
      reinstall('v', {
        kind: 'escalated',
        phase: 'escalated',
        covers_offline: false,
        escalation: { reason: 'not_back', bound: '10m', at: '2026-10-27T13:25:00Z' },
      }),
    ])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('PiVPN')
    const label = await within(row).findByText('Not back 10m after Agent reinstall')
    expect(label.className, 'the escalated label must use the danger color').toContain('text-danger-fg')
    expect(statValue('Offline')).toBe('1')
    expect(within(row).queryByTestId('status-dot-expected-offline')).toBeNull()
  })
})

describe('EXPECT-008 [AC-025] a self-update still shows in the Version cell', () => {
  it('keeps the compact Version spinner for an agent self-update next to a reinstall on another device', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('c', 'PiCUPS', { status: 'offline' }), device('v', 'vermont')])
    serveActivities([
      reinstall('c'),
      reinstall('v', {
        origin: 'agent_self_update',
        actor_phrase: 'by agent auto-update',
        agent_from_version: '2.82.0',
        agent_to_version: '2.83.0',
        device_online: true,
      }),
    ])
    renderWithProviders(<FleetOverview />, { timeZone })

    const vermont = await rowOf('vermont')
    expect(await within(vermont).findByTestId('agent-updating')).toBeInTheDocument()
    expect(within(vermont).queryByTestId('activity-label')).toBeNull()
    const cups = await rowOf('PiCUPS')
    expect(await within(cups).findByTestId('activity-label')).toBeInTheDocument()
    expect(within(cups).queryByTestId('agent-updating')).toBeNull()
  })
})

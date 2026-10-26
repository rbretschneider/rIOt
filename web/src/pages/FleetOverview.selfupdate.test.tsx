import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor, within } from '@testing-library/react'
import FleetOverview from './FleetOverview'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'
import type { Device, DeviceActivity, WSMessage } from '../types/models'

// EXPECT-002 (AC-023, FR-024, FR-025, FR-026): the fleet page for an agent's own
// self-update. The real useDevices and useActivity run; only the api and the
// WebSocket are replaced, so a pushed activity_update takes the same cache path as
// in the browser.
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

const NOW = Date.parse('2026-10-26T17:55:00Z')
const timeZone = { now: () => NOW }

function device(id: string, hostname: string, over: Partial<Device> = {}): Device {
  return {
    id,
    hostname,
    short_id: id,
    arch: 'amd64',
    status: 'online',
    agent_version: '2.78.0',
    primary_ip: '10.0.0.1',
    tags: [],
    last_heartbeat: '2026-10-26T17:54:30Z',
    ...over,
  } as Device
}

function selfUpdate(deviceId: string, over: Partial<DeviceActivity> = {}): DeviceActivity {
  return {
    device_id: deviceId,
    kind: 'agent',
    phase: 'in_progress',
    action: 'agent_update',
    command_id: `cmd-${deviceId}`,
    actor_phrase: 'by agent auto-update',
    started_at: '2026-10-26T17:51:00Z',
    back_by: null,
    waiting_dpkg_lock: false,
    progress: null,
    agent_from_version: '2.78.0',
    agent_to_version: '2.80.0',
    target: '',
    docker_actions: 0,
    covers_offline: true,
    device_online: false,
    escalation: null,
    powered_off_at: null,
    ...over,
  }
}

function serveActivities(activities: DeviceActivity[], newest = '2.80.0') {
  vi.mocked(api.getActivity).mockResolvedValue({
    generated_at: '2026-10-26T17:55:00Z',
    newest_agent_version: newest,
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
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.80.0' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getAgentVersions).mockResolvedValue([])
  serveActivities([])
})

describe('EXPECT-002 [AC-023] fleet label, actor, start and expected-offline count', () => {
  it('shows "Updating agent 2.78.0 → 2.80.0", the actor phrase and the start time for an offline device', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'vermont', { status: 'offline' })])
    serveActivities([selfUpdate('v')])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('vermont')
    expect(await within(row).findByText('Updating agent 2.78.0 → 2.80.0')).toBeInTheDocument()
    expect(within(row).getByText(/by agent auto-update/), 'the originator must be named').toBeInTheDocument()
    expect(within(row).getByText('4m ago'), 'the start time must be shown').toBeInTheDocument()
  })

  it('counts the offline device as expected offline, not offline, and keeps its offline dot', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'vermont', { status: 'offline' }), device('o', 'online-one')])
    serveActivities([selfUpdate('v')])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('vermont')
    await within(row).findByText('Updating agent 2.78.0 → 2.80.0')
    expect(statValue('Expected offline')).toBe('1')
    expect(statValue('Offline'), 'a device in its self-update window is not a real outage').toBe('0')
    expect(within(row).getByTestId('status-dot-expected-offline'), 'the device must still look offline').toBeInTheDocument()
  })

  it('uses the versions of the record, not the newest agent version the server knows', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'vermont')])
    serveActivities([selfUpdate('v', { device_online: true })], '9.9.9')
    renderWithProviders(<FleetOverview />, { timeZone })

    expect(await screen.findByText('Updating agent 2.78.0 → 2.80.0')).toBeInTheDocument()
    expect(screen.queryByText(/9\.9\.9/)).toBeNull()
  })

  it('shows the same label in a browser opened afterwards, because it comes from the server', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'vermont', { status: 'offline' })])
    serveActivities([selfUpdate('v')])
    const first = renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByText('Updating agent 2.78.0 → 2.80.0')
    first.unmount()

    renderWithProviders(<FleetOverview />, { timeZone })
    expect(await screen.findByText('Updating agent 2.78.0 → 2.80.0')).toBeInTheDocument()
  })

  it('labels each device of a release with its own versions', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('a', 'hawaii'), device('b', 'PiVPN')])
    serveActivities([
      selfUpdate('a', { device_online: true }),
      selfUpdate('b', { device_online: true, agent_from_version: '2.79.0' }),
    ])
    renderWithProviders(<FleetOverview />, { timeZone })

    expect(await within(await rowOf('hawaii')).findByText('Updating agent 2.78.0 → 2.80.0')).toBeInTheDocument()
    expect(await within(await rowOf('PiVPN')).findByText('Updating agent 2.79.0 → 2.80.0')).toBeInTheDocument()
  })

  it('does not show a label on a device that has no self-update (another device cannot affect it)', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('a', 'hawaii', { status: 'offline' }), device('b', 'vermont', { status: 'offline' })])
    serveActivities([selfUpdate('a')])
    renderWithProviders(<FleetOverview />, { timeZone })

    await within(await rowOf('hawaii')).findByText('Updating agent 2.78.0 → 2.80.0')
    const other = await rowOf('vermont')
    expect(within(other).queryByTestId('activity-label')).toBeNull()
    expect(statValue('Offline'), 'the other device is a real outage').toBe('1')
    expect(statValue('Expected offline')).toBe('1')
  })

  it('never prints a hostile version from the wire', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'vermont')])
    serveActivities([selfUpdate('v', { device_online: true, agent_to_version: '<img src=x onerror=alert(1)>' })], '')
    renderWithProviders(<FleetOverview />, { timeZone })

    expect(await screen.findByText('Updating agent')).toBeInTheDocument()
    expect(document.querySelector('img')).toBeNull()
  })
})

describe('EXPECT-002 [AC-023] the fleet page follows the window live', () => {
  it('shows the label when the window opens and clears it when the window closes, with no reload', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('h', 'hawaii')])
    const { queryClient } = renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByRole('link', { name: 'hawaii' })
    await waitFor(() => expect(queryClient.getQueryState(['activity'])?.status).toBe('success'))
    expect(screen.queryByText(/^Updating agent/)).toBeNull()

    act(() => {
      ws.send({ type: 'activity_update', device_id: 'h', data: selfUpdate('h', { device_online: true }) })
    })
    expect(await screen.findByText('Updating agent 2.78.0 → 2.80.0')).toBeInTheDocument()

    act(() => {
      ws.send({ type: 'activity_update', device_id: 'h', data: null })
    })
    await waitFor(() => expect(screen.queryByText('Updating agent 2.78.0 → 2.80.0'), 'the label must clear when the window closes').toBeNull())
  })

  it('moves an offline device back to offline when the window closes without the device returning', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'vermont', { status: 'offline' })])
    serveActivities([selfUpdate('v')])
    renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByText('Updating agent 2.78.0 → 2.80.0')
    expect(statValue('Expected offline')).toBe('1')

    act(() => {
      ws.send({ type: 'activity_update', device_id: 'v', data: null })
    })

    await waitFor(() => expect(statValue('Offline')).toBe('1'))
    expect(screen.queryByText('Expected offline'), 'no device is expected offline any more').toBeNull()
  })

  it('shows the escalated state naming both versions, with failure styling, and counts the device as offline', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'vermont', { status: 'offline' })])
    serveActivities([
      selfUpdate('v', {
        kind: 'escalated',
        phase: 'escalated',
        covers_offline: false,
        escalation: { reason: 'not_back', bound: '10m', at: '2026-10-26T18:01:00Z' },
      }),
    ])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('vermont')
    const label = await within(row).findByText('Not back 10m after Agent update 2.78.0 → 2.80.0')
    expect(label.className, 'the escalated label must use the danger color').toContain('text-danger-fg')
    expect(statValue('Offline')).toBe('1')
    expect(within(row).queryByTestId('status-dot-expected-offline')).toBeNull()
  })
})

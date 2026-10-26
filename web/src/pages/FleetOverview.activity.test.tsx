import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor, within } from '@testing-library/react'
import FleetOverview from './FleetOverview'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'
import type { Device, DeviceActivity, WSMessage } from '../types/models'

// The real useDevices and useActivity run here; only the api and the WebSocket
// are replaced, so a pushed activity_update goes through the same cache path as
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

// Every subscriber registers a handler (useDevices applies messages, useActivity only reads the
// connection state), so a message is delivered to all of them, like the real provider does.
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

const NOW = Date.parse('2026-10-26T14:05:00Z')
const timeZone = { now: () => NOW }

function device(id: string, hostname: string, over: Partial<Device> = {}): Device {
  return {
    id,
    hostname,
    short_id: id,
    arch: 'amd64',
    status: 'online',
    agent_version: '2.77.0',
    primary_ip: '10.0.0.1',
    tags: [],
    last_heartbeat: '2026-10-26T14:04:30Z',
    ...over,
  } as Device
}

function activity(deviceId: string, over: Partial<DeviceActivity> = {}): DeviceActivity {
  return {
    device_id: deviceId,
    kind: 'host',
    phase: 'in_progress',
    action: 'reboot',
    command_id: `cmd-${deviceId}`,
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

function serveActivities(activities: DeviceActivity[], newest = '2.78.0') {
  vi.mocked(api.getActivity).mockResolvedValue({
    generated_at: '2026-10-26T14:05:00Z',
    newest_agent_version: newest,
    activities,
  })
}

/** The number shown in a summary card, found by its label. */
function statValue(label: string): string {
  const labelEl = screen.getByText(label)
  return labelEl.nextElementSibling?.textContent ?? ''
}

async function rowOf(hostname: string): Promise<HTMLElement> {
  const link = await screen.findByRole('link', { name: hostname })
  return link.closest('tr') as HTMLElement
}

beforeEach(() => {
  vi.clearAllMocks()
  ws.handlers.clear()
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.78.0' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getAgentVersions).mockResolvedValue([])
  serveActivities([])
})

describe('[AC-024] fleet agent update shows what is happening', () => {
  const devices = [device('a', 'alpha'), device('b', 'bravo'), device('c', 'charlie')]
  const agentUpdates = devices.map(d =>
    activity(d.id, { kind: 'agent', action: 'agent_update', agent_from_version: '2.77.0', covers_offline: true }),
  )

  it('shows "Updating agent 2.77.0 → 2.78.0" with the actor and the start time on each device', async () => {
    vi.mocked(api.getDevices).mockResolvedValue(devices)
    serveActivities(agentUpdates)
    renderWithProviders(<FleetOverview />, { timeZone })

    for (const d of devices) {
      const row = await rowOf(d.hostname)
      await within(row).findByText('Updating agent 2.77.0 → 2.78.0')
      expect(within(row).getByText(/by ryan/), `${d.hostname} must name the actor`).toBeInTheDocument()
      expect(within(row).getByText('5m ago'), `${d.hostname} must show when the update started`).toBeInTheDocument()
    }
  })

  it('shows the same labels in a second browser opened afterwards, because they come from the server', async () => {
    vi.mocked(api.getDevices).mockResolvedValue(devices)
    serveActivities(agentUpdates)
    const first = renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findAllByText('Updating agent 2.77.0 → 2.78.0')
    first.unmount()

    renderWithProviders(<FleetOverview />, { timeZone })
    const labels = await screen.findAllByText('Updating agent 2.77.0 → 2.78.0')
    expect(labels, 'every device must show the label in the new session').toHaveLength(3)
  })

  it('falls back to "Updating agent" when the server knows no newer version', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([devices[0]])
    serveActivities([agentUpdates[0]], '')
    renderWithProviders(<FleetOverview />, { timeZone })

    expect(await screen.findByText('Updating agent')).toBeInTheDocument()
  })
})

describe('[AC-028] a powered-off device on the fleet page', () => {
  it('shows "Powered off by ryan at 14:02", a hollow offline dot, and counts it as expected offline', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([
      device('p', 'PiCUPS', { status: 'offline' }),
      device('o', 'online-one'),
    ])
    serveActivities([
      activity('p', {
        kind: 'powered_off',
        phase: 'powered_off',
        action: 'shutdown',
        covers_offline: false,
        device_online: false,
        powered_off_at: '2026-10-26T14:02:00Z',
      }),
    ])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('PiCUPS')
    expect(await within(row).findByText('Powered off by ryan at 14:02')).toBeInTheDocument()
    expect(within(row).getByTestId('status-dot-expected-offline'), 'powered off must stay visibly offline').toBeInTheDocument()
    expect(statValue('Expected offline')).toBe('1')
    expect(statValue('Offline'), 'a powered-off device is not a real outage').toBe('0')
  })

  it('does not repeat the actor on a second line for a powered-off label', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('p', 'PiCUPS', { status: 'offline' })])
    serveActivities([
      activity('p', { kind: 'powered_off', phase: 'powered_off', action: 'shutdown', powered_off_at: '2026-10-26T14:02:00Z' }),
    ])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('PiCUPS')
    await within(row).findByText('Powered off by ryan at 14:02')
    expect(within(row).queryByText(/started/)).toBeNull()
  })
})

describe('[AC-039] offline and expected-offline counts', () => {
  async function renderFourDevices() {
    vi.mocked(api.getDevices).mockResolvedValue([
      device('on', 'online-one'),
      device('rb', 'rebooting', { status: 'offline' }),
      device('po', 'powered-off', { status: 'offline' }),
      device('es', 'escalated', { status: 'offline' }),
    ])
    serveActivities([
      activity('rb', { phase: 'return', back_by: '2026-10-26T14:12:00Z', device_online: false }),
      activity('po', { kind: 'powered_off', phase: 'powered_off', action: 'shutdown', covers_offline: false, device_online: false, powered_off_at: '2026-10-26T13:00:00Z' }),
      activity('es', {
        kind: 'escalated',
        phase: 'escalated',
        covers_offline: false,
        device_online: false,
        escalation: { reason: 'not_back', bound: '15m', at: '2026-10-26T14:00:00Z' },
      }),
    ])
    renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByText('Rebooting, back by 14:12')
  }

  it('counts one offline, two expected offline and one online', async () => {
    await renderFourDevices()

    expect(statValue('Offline'), 'only the escalated device is a real outage').toBe('1')
    expect(statValue('Expected offline')).toBe('2')
    expect(statValue('Online')).toBe('1')
    expect(statValue('Total Devices')).toBe('4')
  })

  it('marks the two expected-offline devices with a hollow dot while their text shows the activity label', async () => {
    await renderFourDevices()

    for (const [hostname, text] of [
      ['rebooting', 'Rebooting, back by 14:12'],
      ['powered-off', 'Powered off by ryan at 13:00'],
    ]) {
      const row = await rowOf(hostname)
      expect(within(row).getByTestId('status-dot-expected-offline'), `${hostname} must still look offline`).toBeInTheDocument()
      expect(within(row).getByText(text)).toBeInTheDocument()
    }
  })

  it('shows the escalated device with failure styling and a plain red offline dot', async () => {
    await renderFourDevices()

    const row = await rowOf('escalated')
    const label = within(row).getByText('Not back 15m after Reboot')
    expect(label.className, 'the escalated label must use the danger color').toContain('text-danger-fg')
    expect(within(row).queryByTestId('status-dot-expected-offline')).toBeNull()
    expect(within(row).getByTitle('Offline')).toBeInTheDocument()
  })

  it('does not show the Expected offline card when no device is expected offline', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('on', 'online-one'), device('off', 'down', { status: 'offline' })])
    renderWithProviders(<FleetOverview />, { timeZone })

    await screen.findByRole('link', { name: 'down' })
    expect(screen.queryByText('Expected offline')).toBeNull()
    expect(statValue('Offline')).toBe('1')
  })
})

describe('[AC-040] the fleet page updates live', () => {
  it('shows "Docker: updating plex" when a schedule dispatches an update, and clears it when the window closes', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('h', 'hawaii')])
    const { queryClient } = renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByRole('link', { name: 'hawaii' })
    await waitFor(() => expect(queryClient.getQueryState(['activity'])?.status).toBe('success'))
    expect(screen.queryByText(/^Docker:/)).toBeNull()

    act(() => {
      ws.send({
        type: 'activity_update',
        device_id: 'h',
        data: activity('h', {
          kind: 'docker',
          action: 'docker_update',
          target: 'plex',
          docker_actions: 1,
          covers_offline: false,
          actor_phrase: 'by auto-update policy for plex',
        }),
      })
    })
    expect(await screen.findByText('Docker: updating plex')).toBeInTheDocument()
    expect(screen.getByText(/by auto-update policy for plex/)).toBeInTheDocument()

    act(() => {
      ws.send({ type: 'activity_update', device_id: 'h', data: null })
    })
    await waitFor(() => expect(screen.queryByText('Docker: updating plex'), 'the label must clear when the window closes').toBeNull())
  })

  it('moves a device from offline to expected offline as soon as its reboot activity arrives', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('v', 'vermont', { status: 'offline' })])
    const { queryClient } = renderWithProviders(<FleetOverview />, { timeZone })
    await screen.findByRole('link', { name: 'vermont' })
    await waitFor(() => expect(queryClient.getQueryState(['activity'])?.status).toBe('success'))
    expect(statValue('Offline')).toBe('1')

    act(() => {
      ws.send({
        type: 'activity_update',
        device_id: 'v',
        data: activity('v', { phase: 'return', device_online: false, back_by: '2026-10-26T14:20:00Z' }),
      })
    })

    expect(await screen.findByText('Rebooting, back by 14:20')).toBeInTheDocument()
    expect(statValue('Offline')).toBe('0')
    expect(statValue('Expected offline')).toBe('1')
  })
})

describe('[AC-043] devices without an activity are displayed as before', () => {
  it('shows no activity label and the plain status dot', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('on', 'online-one')])
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('online-one')
    expect(within(row).getByTitle('Online')).toBeInTheDocument()
    expect(within(row).queryByTestId('activity-label')).toBeNull()
  })

  it('still renders the device list when the activity endpoint fails', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('off', 'down', { status: 'offline' })])
    vi.mocked(api.getActivity).mockRejectedValue(new Error('HTTP 503'))
    renderWithProviders(<FleetOverview />, { timeZone })

    const row = await rowOf('down')
    await waitFor(() => expect(api.getActivity).toHaveBeenCalled())
    expect(within(row).getByTitle('Offline')).toBeInTheDocument()
    expect(statValue('Offline'), 'without activity data every offline device counts as offline').toBe('1')
  })
})

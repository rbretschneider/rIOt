import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceDetail from './DeviceDetail'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'
import type { DeviceActivity } from '../types/models'

vi.mock('../api/client', () => ({
  api: {
    getDevice: vi.fn(),
    getEvents: vi.fn(),
    getServerUpdate: vi.fn(),
    getPatchStatus: vi.fn(),
    getDeviceAlertRules: vi.fn(),
    getDeviceLogs: vi.fn(),
    getHeartbeatHistory: vi.fn(),
    getSecurityScore: vi.fn(),
    getActivity: vi.fn(),
    getCommand: vi.fn(),
    cancelCommand: vi.fn(),
    sendCommand: vi.fn(),
    deleteDevice: vi.fn(),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({ wsConnected: false }),
}))

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

const NOW = Date.parse('2026-10-26T14:05:00Z')
const timeZone = { now: () => NOW }

const COMMAND_ID = '1a2b3c4d-1111-2222-3333-444444444444'

function activity(over: Partial<DeviceActivity> = {}): DeviceActivity {
  return {
    device_id: 'dev-1',
    kind: 'host',
    phase: 'return',
    action: 'reboot',
    command_id: COMMAND_ID,
    actor_phrase: 'by ryan',
    started_at: '2026-10-26T14:00:00Z',
    back_by: '2026-10-26T14:15:00Z',
    waiting_dpkg_lock: false,
    progress: null,
    agent_from_version: '',
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
    generated_at: '2026-10-26T14:05:00Z',
    newest_agent_version: '2.78.0',
    activities,
  })
}

function renderPage() {
  return renderWithProviders(<DeviceDetail />, {
    route: '/devices/dev-1',
    path: '/devices/:id',
    routes: [
      { path: '/', element: <div>Fleet page</div> },
      { path: '/docker/activity', element: <div>Docker activity page</div> },
    ],
    showLocation: true,
    timeZone,
  })
}

async function openPage() {
  renderPage()
  await screen.findByRole('heading', { name: 'vermont' })
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getDevice).mockResolvedValue({
    device: {
      id: 'dev-1',
      hostname: 'vermont',
      short_id: 'abc123',
      arch: 'amd64',
      status: 'offline',
      agent_version: '2.77.0',
      primary_ip: '192.168.1.1',
      tags: [],
      docker_available: false,
      auto_patch: false,
      last_heartbeat: '2026-10-26T14:01:00Z',
      created_at: '2026-10-01T00:00:00Z',
      updated_at: '2026-10-26T00:00:00Z',
    },
    latest_telemetry: null,
    agent_connected: false,
  } as never)
  vi.mocked(api.getEvents).mockResolvedValue([])
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.78.0' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getDeviceAlertRules).mockResolvedValue([])
  vi.mocked(api.getDeviceLogs).mockResolvedValue([])
  vi.mocked(api.getHeartbeatHistory).mockResolvedValue([])
  vi.mocked(api.getSecurityScore).mockResolvedValue({
    overall_score: 72, max_score: 100, grade: 'C', categories: [], evaluated_at: '2026-10-26T12:00:00Z',
  } as never)
  vi.mocked(api.getCommand).mockResolvedValue({
    id: COMMAND_ID,
    device_id: 'dev-1',
    action: 'reboot',
    status: 'succeeded',
    result_msg: 'reboot initiated',
    step: '',
  } as never)
  serveActivities([])
})

describe('[AC-042] header shows label, actor, start and link', () => {
  it('shows the label, the actor phrase and the start time in the device header', async () => {
    serveActivities([activity()])
    await openPage()

    const label = await screen.findByTestId('activity-label')
    expect(within(label).getByText('Rebooting, back by 14:15')).toBeInTheDocument()
    expect(within(label).getByText(/by ryan/)).toBeInTheDocument()
    expect(within(label).getByText('5m ago'), 'the start time must be shown').toBeInTheDocument()
  })

  it('opens the host command in a modal that has no Docker activity link', async () => {
    serveActivities([activity()])
    await openPage()

    await userEvent.click(await screen.findByRole('button', { name: 'View command' }))

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('reboot initiated')).toBeInTheDocument()
    expect(api.getCommand).toHaveBeenCalledWith('dev-1', COMMAND_ID)
    expect(within(dialog).queryByRole('link', { name: 'View steps and output' })).toBeNull()
  })

  it('links a Docker action to the Docker activity page with host and command in the query', async () => {
    serveActivities([
      activity({ kind: 'docker', action: 'docker_update', phase: 'in_progress', target: 'plex', docker_actions: 1, covers_offline: false, device_online: true }),
    ])
    await openPage()

    const link = await screen.findByRole('link', { name: 'View command' })
    expect(link).toHaveAttribute('href', `/docker/activity?host=dev-1&command=${COMMAND_ID}`)
    await userEvent.click(link)
    expect(await screen.findByText('Docker activity page')).toBeInTheDocument()
    expect(screen.getByTestId('location').textContent).toBe(`/docker/activity?host=dev-1&command=${COMMAND_ID}`)
  })

  it('shows the powered-off label without a second actor line', async () => {
    serveActivities([
      activity({ kind: 'powered_off', phase: 'powered_off', action: 'shutdown', powered_off_at: '2026-10-26T14:02:00Z', back_by: null }),
    ])
    await openPage()

    const label = await screen.findByTestId('activity-label')
    expect(within(label).getByText('Powered off by ryan at 14:02')).toBeInTheDocument()
    expect(within(label).queryByText(/started/)).toBeNull()
  })

  it('shows the escalated label with failure styling', async () => {
    serveActivities([
      activity({ kind: 'escalated', phase: 'escalated', covers_offline: false, escalation: { reason: 'timed_out', bound: '35m', at: '2026-10-26T14:00:00Z' } }),
    ])
    await openPage()

    const text = await screen.findByText('Reboot failed: offline')
    expect(text.className).toContain('text-danger-fg')
  })

  it('shows no activity block for a device that has none, and keeps the status badge', async () => {
    serveActivities([activity({ device_id: 'some-other-device' })])
    await openPage()

    await waitFor(() => expect(api.getActivity).toHaveBeenCalled())
    expect(screen.queryByTestId('activity-label')).toBeNull()
    expect(screen.getByText('Offline')).toBeInTheDocument()
  })

  it('still renders the page when the activity endpoint is unavailable', async () => {
    vi.mocked(api.getActivity).mockRejectedValue(new Error('HTTP 503'))
    await openPage()

    await waitFor(() => expect(api.getActivity).toHaveBeenCalled())
    expect(screen.queryByTestId('activity-label')).toBeNull()
  })
})

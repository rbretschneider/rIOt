import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceDetail from './DeviceDetail'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'
import type { Command, DeviceActivity } from '../types/models'

// EXPECT-008 (AC-022, FR-024, FR-026): the device page header for an installer re-run, and
// the read-only record its "View command" link opens.
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
    getCommandEvents: vi.fn(),
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

const NOW = Date.parse('2026-10-27T13:18:00Z')
const timeZone = { now: () => NOW }

const COMMAND_ID = '9f8e7d6c-1111-2222-3333-444444444444'

function reinstall(over: Partial<DeviceActivity> = {}): DeviceActivity {
  return {
    device_id: 'dev-1',
    kind: 'agent',
    phase: 'in_progress',
    action: 'agent_update',
    origin: 'installer',
    command_id: COMMAND_ID,
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
    generated_at: '2026-10-27T13:18:00Z',
    newest_agent_version: '2.83.0',
    activities,
  })
}

async function openPage() {
  renderWithProviders(<DeviceDetail />, {
    route: '/devices/dev-1',
    path: '/devices/:id',
    routes: [{ path: '/docker/activity', element: <div>Docker activity page</div> }],
    showLocation: true,
    timeZone,
  })
  await screen.findByRole('heading', { name: 'PiVPN' })
}

const record: Command = {
  id: COMMAND_ID,
  device_id: 'dev-1',
  action: 'agent_update',
  origin: 'installer',
  params: {},
  status: 'running',
  step: 'waiting_for_restart',
  result_msg: '',
  duration_ms: null,
  exit_code: null,
  created_at: '2026-10-27T13:15:00Z',
  updated_at: '2026-10-27T13:15:00Z',
  started_at: '2026-10-27T13:15:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getDevice).mockResolvedValue({
    device: {
      id: 'dev-1',
      hostname: 'PiVPN',
      short_id: 'abc123',
      arch: 'arm64',
      status: 'offline',
      agent_version: '2.82.0',
      primary_ip: '192.168.1.5',
      tags: [],
      docker_available: false,
      auto_patch: false,
      last_heartbeat: '2026-10-27T13:15:05Z',
      created_at: '2026-10-01T00:00:00Z',
      updated_at: '2026-10-27T00:00:00Z',
    },
    latest_telemetry: null,
    agent_connected: false,
  } as never)
  vi.mocked(api.getEvents).mockResolvedValue([])
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.83.0' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getDeviceAlertRules).mockResolvedValue([])
  vi.mocked(api.getDeviceLogs).mockResolvedValue([])
  vi.mocked(api.getHeartbeatHistory).mockResolvedValue([])
  vi.mocked(api.getSecurityScore).mockResolvedValue({
    overall_score: 72, max_score: 100, grade: 'C', categories: [], evaluated_at: '2026-10-27T12:00:00Z',
  } as never)
  vi.mocked(api.getCommand).mockResolvedValue(record)
  vi.mocked(api.getCommandEvents).mockResolvedValue({
    server_events: [
      {
        id: 7,
        device_id: 'dev-1',
        type: 'device_offline',
        severity: 'info',
        message: 'Device PiVPN went offline during Agent reinstall by installer (expected, back by 13:25)',
        created_at: '2026-10-27T13:15:50Z',
        command_id: COMMAND_ID,
        command_attr: 'expected',
      },
    ],
    server_events_omitted: 0,
    agent_events: [],
    agent_events_omitted: 0,
  })
  serveActivities([])
})

describe('EXPECT-008 [AC-022] header label, actor line, link opens the read-only record', () => {
  it('shows "Reinstalling agent", the actor phrase and the start time in the device header', async () => {
    serveActivities([reinstall()])
    await openPage()

    const label = await screen.findByTestId('activity-label')
    expect(within(label).getByText('Reinstalling agent')).toBeInTheDocument()
    expect(within(label).getByText(/by installer/)).toBeInTheDocument()
    expect(within(label).getByText('3m ago'), 'the start time must be shown').toBeInTheDocument()
  })

  it('shows the same label while the device is online', async () => {
    serveActivities([reinstall({ device_online: true })])
    await openPage()

    expect(within(await screen.findByTestId('activity-label')).getByText('Reinstalling agent')).toBeInTheDocument()
  })

  it('opens the record in a modal with the details and the linked server events', async () => {
    serveActivities([reinstall()])
    await openPage()

    await userEvent.click(await screen.findByRole('button', { name: 'View command' }))

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByTestId('self-update-details')).toBeInTheDocument()
    expect(within(dialog).getAllByText('Agent reinstall').length).toBeGreaterThan(0)
    expect(within(dialog).getByText('by installer')).toBeInTheDocument()
    expect(within(dialog).getByTestId('self-update-outcome').textContent).toBe('In progress')
    expect(await within(dialog).findByText(/went offline during Agent reinstall by installer/)).toBeInTheDocument()
    expect(api.getCommand).toHaveBeenCalledWith('dev-1', COMMAND_ID)
    expect(api.getCommandEvents).toHaveBeenCalledWith('dev-1', COMMAND_ID)
  })

  it('shows the record with no cancel, retry or Docker activity link', async () => {
    serveActivities([reinstall()])
    await openPage()

    await userEvent.click(await screen.findByRole('button', { name: 'View command' }))

    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByTestId('self-update-details')
    expect(within(dialog).queryByText(/cancel/i), 'a reinstall record cannot be cancelled').toBeNull()
    expect(within(dialog).queryByText(/retry/i)).toBeNull()
    expect(within(dialog).queryByRole('link', { name: 'View steps and output' })).toBeNull()
    expect(within(dialog).queryByRole('button', { name: /cancel|retry|update/i })).toBeNull()
    expect(api.cancelCommand).not.toHaveBeenCalled()
    expect(api.sendCommand).not.toHaveBeenCalled()
  })

  it('shows the outcome of a finished record as "Not confirmed" with the server text', async () => {
    vi.mocked(api.getCommand).mockResolvedValue({
      ...record,
      status: 'timed_out',
      result_msg: 'Agent reinstall by installer on PiVPN was not confirmed within 10m (agent did not restart)',
    })
    serveActivities([reinstall()])
    await openPage()

    await userEvent.click(await screen.findByRole('button', { name: 'View command' }))

    const dialog = await screen.findByRole('dialog')
    expect((await within(dialog).findByTestId('self-update-outcome')).textContent).toBe('Not confirmed')
    expect(within(dialog).getByText(/was not confirmed within 10m/)).toBeInTheDocument()
  })

  it('shows no activity block once the window has closed and the server sends no activity', async () => {
    serveActivities([])
    await openPage()

    await waitFor(() => expect(api.getActivity).toHaveBeenCalled())
    expect(screen.queryByTestId('activity-label')).toBeNull()
    expect(screen.queryByText(/Reinstalling agent/)).toBeNull()
  })
})

describe('EXPECT-008 [AC-012] header for a reinstall that was not back in time', () => {
  it('shows "Not back 10m after Agent reinstall" with failure styling', async () => {
    serveActivities([
      reinstall({
        kind: 'escalated',
        phase: 'escalated',
        covers_offline: false,
        escalation: { reason: 'not_back', bound: '10m', at: '2026-10-27T13:25:00Z' },
      }),
    ])
    await openPage()

    const label = await screen.findByText('Not back 10m after Agent reinstall')
    expect(label.className, 'the escalated label must use the danger color').toContain('text-danger-fg')
  })
})

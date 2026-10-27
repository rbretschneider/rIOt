import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceDetail from './DeviceDetail'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'
import type { Command, DeviceActivity } from '../types/models'

// EXPECT-002 (AC-023, FR-024, FR-027): the device page header for an agent's own
// self-update, and the read-only record its "View command" link opens.
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

const NOW = Date.parse('2026-10-26T17:55:00Z')
const timeZone = { now: () => NOW }

const COMMAND_ID = '1a2b3c4d-1111-2222-3333-444444444444'

function selfUpdate(over: Partial<DeviceActivity> = {}): DeviceActivity {
  return {
    device_id: 'dev-1',
    kind: 'agent',
    phase: 'in_progress',
    action: 'agent_update',
    command_id: COMMAND_ID,
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

function serveActivities(activities: DeviceActivity[]) {
  vi.mocked(api.getActivity).mockResolvedValue({
    generated_at: '2026-10-26T17:55:00Z',
    newest_agent_version: '2.80.0',
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
  await screen.findByRole('heading', { name: 'vermont' })
}

const record: Command = {
  id: COMMAND_ID,
  device_id: 'dev-1',
  action: 'agent_update',
  origin: 'agent_self_update',
  params: { from_version: '2.78.0', to_version: '2.80.0' },
  status: 'running',
  step: 'waiting_for_restart',
  result_msg: '',
  duration_ms: null,
  exit_code: null,
  created_at: '2026-10-26T17:51:00Z',
  updated_at: '2026-10-26T17:51:00Z',
  started_at: '2026-10-26T17:51:00Z',
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
      agent_version: '2.78.0',
      primary_ip: '192.168.1.1',
      tags: [],
      docker_available: false,
      auto_patch: false,
      last_heartbeat: '2026-10-26T17:52:00Z',
      created_at: '2026-10-01T00:00:00Z',
      updated_at: '2026-10-26T00:00:00Z',
    },
    latest_telemetry: null,
    agent_connected: false,
  } as never)
  vi.mocked(api.getEvents).mockResolvedValue([])
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.80.0' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getDeviceAlertRules).mockResolvedValue([])
  vi.mocked(api.getDeviceLogs).mockResolvedValue([])
  vi.mocked(api.getHeartbeatHistory).mockResolvedValue([])
  vi.mocked(api.getSecurityScore).mockResolvedValue({
    overall_score: 72, max_score: 100, grade: 'C', categories: [], evaluated_at: '2026-10-26T12:00:00Z',
  } as never)
  vi.mocked(api.getCommand).mockResolvedValue(record)
  vi.mocked(api.getCommandEvents).mockResolvedValue({
    server_events: [
      {
        id: 7,
        device_id: 'dev-1',
        type: 'device_offline',
        severity: 'info',
        message: 'Device vermont went offline during Agent update 2.78.0 → 2.80.0 by agent auto-update (expected, back by 18:01)',
        created_at: '2026-10-26T17:52:45Z',
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

describe('EXPECT-002 [AC-023] header link opens the read-only record', () => {
  it('shows the versioned label, the actor phrase and the start time in the device header', async () => {
    serveActivities([selfUpdate()])
    await openPage()

    const label = await screen.findByTestId('activity-label')
    expect(within(label).getByText('Updating agent 2.78.0 → 2.80.0')).toBeInTheDocument()
    expect(within(label).getByText(/by agent auto-update/)).toBeInTheDocument()
    expect(within(label).getByText('4m ago'), 'the start time must be shown').toBeInTheDocument()
  })

  it('opens the record in a modal with the details and the linked server events', async () => {
    serveActivities([selfUpdate()])
    await openPage()

    await userEvent.click(await screen.findByRole('button', { name: 'View command' }))

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByTestId('self-update-details')).toBeInTheDocument()
    expect(within(dialog).getAllByText('Agent update 2.78.0 → 2.80.0').length).toBeGreaterThan(0)
    expect(within(dialog).getByText('by agent auto-update')).toBeInTheDocument()
    expect(within(dialog).getByTestId('self-update-outcome').textContent).toBe('In progress')
    expect(await within(dialog).findByText(/went offline during Agent update 2\.78\.0 → 2\.80\.0/)).toBeInTheDocument()
    expect(api.getCommand).toHaveBeenCalledWith('dev-1', COMMAND_ID)
    expect(api.getCommandEvents).toHaveBeenCalledWith('dev-1', COMMAND_ID)
  })

  it('shows the record with no cancel, retry or Docker activity link', async () => {
    serveActivities([selfUpdate()])
    await openPage()

    await userEvent.click(await screen.findByRole('button', { name: 'View command' }))

    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByTestId('self-update-details')
    expect(within(dialog).queryByText(/cancel/i), 'a self-update cannot be cancelled').toBeNull()
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
      result_msg: 'Agent update 2.78.0 → 2.80.0 by agent auto-update on vermont was not confirmed within 10m (agent reports 2.78.0)',
    })
    serveActivities([selfUpdate({ kind: 'escalated', phase: 'escalated', covers_offline: false, escalation: { reason: 'not_back', bound: '10m', at: '2026-10-26T18:01:00Z' } })])
    await openPage()

    expect(await screen.findByText('Not back 10m after Agent update 2.78.0 → 2.80.0')).toBeInTheDocument()
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
    expect(screen.queryByText(/Updating agent/)).toBeNull()
  })
})

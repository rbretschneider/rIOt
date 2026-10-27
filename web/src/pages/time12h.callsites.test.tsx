import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Alerts from './Alerts'
import ActivityLog from '../components/ActivityLog'
import { ContainerDetailContent } from '../components/ContainerDetail'
import CertificateSettings from './settings/CertificateSettings'
import AgentManagement from './settings/AgentManagement'
import ActivityPage from './docker/ActivityPage'
import { formatUtc } from '../utils/format'
import { renderWithProviders } from '../test/renderWithProviders'
import type { ContainerInfo } from '../types/models'

// TIME-12H-001 call-site wiring. Instant 2026-10-27T17:05:09Z, server zone America/New_York, default
// preference (Server, 12-hour). The pages run for real; only their data sources are replaced.
const EXAMPLE = '2026-10-27T17:05:09Z'

const mock = vi.hoisted(() => ({
  getEvents: vi.fn(),
  acknowledgeEvent: vi.fn(),
  acknowledgeAllEvents: vi.fn(),
  getDeviceCommands: vi.fn(),
  getCommandOutput: vi.fn(),
  getContainerLogs: vi.fn(),
  getAgentVersions: vi.fn(),
  getServerUpdate: vi.fn(),
  getDevices: vi.fn(),
  bulkUpdateAgents: vi.fn(),
  getActiveCommands: vi.fn(),
  getCommand: vi.fn(),
  sendCommand: vi.fn(),
  cancelCommand: vi.fn(),
  getContainerMetricHistory: vi.fn(),
  getDeviceAlertRules: vi.fn(),
  getCerts: vi.fn(),
  getBootstrapKeys: vi.fn(),
  getAutomationConfig: vi.fn(),
  hosts: vi.fn(),
  stacks: vi.fn(),
  activity: vi.fn(),
}))

vi.mock('../api/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../api/client')>()
  return {
    ...actual,
    api: {
      getEvents: mock.getEvents,
      acknowledgeEvent: mock.acknowledgeEvent,
      acknowledgeAllEvents: mock.acknowledgeAllEvents,
      getDeviceCommands: mock.getDeviceCommands,
      getCommandOutput: mock.getCommandOutput,
      getContainerLogs: mock.getContainerLogs,
      getAgentVersions: mock.getAgentVersions,
      getServerUpdate: mock.getServerUpdate,
      getDevices: mock.getDevices,
      bulkUpdateAgents: mock.bulkUpdateAgents,
      getActiveCommands: mock.getActiveCommands,
      getCommand: mock.getCommand,
      sendCommand: mock.sendCommand,
      cancelCommand: mock.cancelCommand,
      getContainerMetricHistory: mock.getContainerMetricHistory,
      getDeviceAlertRules: mock.getDeviceAlertRules,
    },
  }
})

vi.mock('../api/settings', () => ({
  settingsApi: {
    getCerts: mock.getCerts,
    getBootstrapKeys: mock.getBootstrapKeys,
    revokeCert: vi.fn(),
    createBootstrapKey: vi.fn(),
    deleteBootstrapKey: vi.fn(),
    getAutomationConfig: mock.getAutomationConfig,
    saveAutomationConfig: vi.fn(),
    createAlertRule: vi.fn(),
  },
}))

vi.mock('../api/docker', () => ({ dockerApi: { hosts: mock.hosts, stacks: mock.stacks, activity: mock.activity } }))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({ data: [{ id: 'dev-1', hostname: 'Web-01.lan' }], wsConnected: true }),
}))

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

vi.mock('../hooks/useWebSocket', () => ({
  useWebSocket: () => ({ connected: false }),
}))

vi.mock('../components/Terminal', () => ({ default: () => null }))

const defaults = { storage: null, serverZone: 'America/New_York', now: () => Date.parse(EXAMPLE) }

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  mock.getEvents.mockResolvedValue([
    { id: 1, device_id: 'dev-1', type: 'disk_high', severity: 'warning', message: 'Disk / is 91% full', created_at: EXAMPLE },
  ])
  mock.getDeviceCommands.mockResolvedValue([
    {
      id: 'cmd-1', device_id: 'dev-1', action: 'os_update', params: {}, status: 'success', result_msg: 'Updated',
      duration_ms: 1000, exit_code: 0, created_at: EXAMPLE, updated_at: EXAMPLE,
    },
  ])
  mock.getCommandOutput.mockResolvedValue([])
  mock.getActiveCommands.mockResolvedValue([])
  mock.getDevices.mockResolvedValue([])
  mock.getContainerMetricHistory.mockResolvedValue([])
  mock.getDeviceAlertRules.mockResolvedValue([])
  mock.getAgentVersions.mockResolvedValue([])
  mock.getServerUpdate.mockResolvedValue({ latest_version: '2.83.0' })
  mock.getCerts.mockResolvedValue([
    { id: 1, device_id: 'abcdef1234567890', serial_number: '0123456789abcdef0123', not_before: '2026-01-01T00:00:00Z', not_after: EXAMPLE, revoked: false, created_at: '2026-01-01T00:00:00Z' },
  ])
  mock.getBootstrapKeys.mockResolvedValue([
    { key_hash: 'hash1', label: 'lab key', used: false, created_at: EXAMPLE, expires_at: '2099-12-31T23:59:58Z' },
  ])
  mock.getAutomationConfig.mockResolvedValue({
    os_patch: { mode: 'window', start_time: '02:00', end_time: '04:00', cooldown_minutes: 60, stagger_seconds: 0, reboot_class: 'off' },
    docker_update: { mode: 'window', start_time: '02:00', end_time: '04:00', cooldown_minutes: 60, stagger_seconds: 0, reboot_class: 'off' },
  })
  mock.hosts.mockResolvedValue([])
  mock.stacks.mockResolvedValue([])
  mock.activity.mockResolvedValue({ entries: [] })
})

describe('TIME-12H-001 [AC-032] ActivityLog, Alerts and CertificateSettings timestamps use the default form', () => {
  it('ActivityLog renders the command time as 2026-10-27 1:05:09 PM EDT', async () => {
    renderWithProviders(<ActivityLog deviceId="dev-1" />, { timeZone: defaults })
    const stamps = await screen.findAllByText('2026-10-27 1:05:09 PM EDT')
    expect(stamps.length).toBeGreaterThan(0)
    expect(stamps[0].tagName).toBe('TIME')
  })

  it('Alerts renders the event time as 2026-10-27 1:05:09 PM EDT', async () => {
    renderWithProviders(<Alerts />, { timeZone: defaults })
    const row = (await screen.findByText('Disk / is 91% full')).closest('tr') as HTMLElement
    expect(within(row).getByText('2026-10-27 1:05:09 PM EDT')).toBeInTheDocument()
  })

  it('CertificateSettings renders the certificate expiry to the minute: 2026-10-27 1:05 PM EDT', async () => {
    renderWithProviders(<CertificateSettings />, { timeZone: defaults })
    expect(await screen.findByText('2026-10-27 1:05 PM EDT')).toBeInTheDocument()
  })

  it('CertificateSettings renders the bootstrap key creation to the second: 2026-10-27 1:05:09 PM EDT', async () => {
    renderWithProviders(<CertificateSettings />, { timeZone: defaults })
    expect(await screen.findByText('2026-10-27 1:05:09 PM EDT')).toBeInTheDocument()
  })

  it('every rendered <time> carries a zone label', async () => {
    renderWithProviders(<CertificateSettings />, { timeZone: defaults })
    await screen.findByText('2026-10-27 1:05 PM EDT')
    for (const el of Array.from(document.querySelectorAll('time'))) {
      expect(el.textContent, 'a <time> without a zone label').toMatch(/(EDT|EST|UTC)$/)
    }
  })
})

describe('TIME-12H-001 [AC-033] container log line time is 12-hour with a label', () => {
  const container: ContainerInfo = {
    id: 'abc123', short_id: 'abc', name: 'web', image: 'nginx:latest', state: 'running', status: 'Up 2 hours',
    created: 0, cpu_percent: 0, mem_usage: 0, mem_limit: 0,
  } as ContainerInfo

  it('reads 1:05:09 PM EDT beside the log line', async () => {
    mock.getContainerLogs.mockResolvedValue([
      { id: 1, container_id: 'abc', container_name: 'web', timestamp: EXAMPLE, stream: 'stdout', line: 'listening on :80' },
    ])
    renderWithProviders(<ContainerDetailContent container={container} deviceId="dev-1" />, { timeZone: defaults })
    await userEvent.click(screen.getByRole('button', { name: 'Logs' }))
    const line = (await screen.findByText('listening on :80')).parentElement as HTMLElement
    expect(within(line).getByText('1:05:09 PM EDT')).toBeInTheDocument()
  })
})

describe('TIME-12H-001 [AC-039] maintenance-window label, Docker filter labels and formatUtc are unchanged', () => {
  it('keeps the maintenance-window editor text about local time and stored UTC', async () => {
    renderWithProviders(<AgentManagement />, { timeZone: defaults })
    const intro = await screen.findByText(/Control when automatic OS patching/)
    expect(intro.textContent).toMatch(
      /^Control when automatic OS patching and Docker container updates are allowed to run\. Times are shown in your local timezone \(.+\) and stored internally as UTC\.$/,
    )
  })

  it('keeps the Docker activity filters labelled From (UTC) and To (UTC)', async () => {
    renderWithProviders(<ActivityPage />, { route: '/docker/activity', timeZone: defaults })
    expect(await screen.findByText('From (UTC)')).toBeInTheDocument()
    expect(screen.getByText('To (UTC)')).toBeInTheDocument()
  })

  it('formatUtc still returns 2026-10-17 14:03 UTC', () => {
    expect(formatUtc('2026-10-17T14:03:00Z')).toBe('2026-10-17 14:03 UTC')
    expect(formatUtc('not-a-date')).toBeNull()
  })
})

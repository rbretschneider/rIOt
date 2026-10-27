import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceDetail from './DeviceDetail'
import { api } from '../api/client'
import { HOUR_CYCLE_STORAGE_KEY, TIME_ZONE_STORAGE_KEY } from '../contexts/TimePreferenceProvider'
import { renderWithProviders, type RenderOptions } from '../test/renderWithProviders'

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
    getDeviceCommands: vi.fn(),
    getDeviceSummary: vi.fn(),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({ wsConnected: false }),
}))

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

// TIME-12H-001. Instant 2026-10-27T17:05:09Z (unix seconds 1793120709), server zone America/New_York.
const EXAMPLE = '2026-10-27T17:05:09Z'

function mockDevice(telemetryData: Record<string, unknown>) {
  vi.mocked(api.getDevice).mockResolvedValue({
    device: {
      id: 'dev-1',
      hostname: 'web-01',
      short_id: 'abc123',
      arch: 'amd64',
      status: 'online',
      agent_version: '2.83.0',
      primary_ip: '10.0.0.1',
      tags: [],
      docker_available: false,
      auto_patch: false,
      last_heartbeat: EXAMPLE,
      created_at: '2026-10-01T00:00:00Z',
      updated_at: '2026-10-26T00:00:00Z',
    },
    latest_telemetry: { id: 1, device_id: 'dev-1', timestamp: EXAMPLE, data: telemetryData },
    agent_connected: true,
  } as never)
}

function renderPage(timeZone: NonNullable<RenderOptions['timeZone']>) {
  return renderWithProviders(<DeviceDetail />, {
    route: '/devices/dev-1',
    path: '/devices/:id',
    timeZone,
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getEvents).mockResolvedValue([])
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.83.0' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getDeviceAlertRules).mockResolvedValue([])
  vi.mocked(api.getDeviceLogs).mockResolvedValue([])
  vi.mocked(api.getHeartbeatHistory).mockResolvedValue([])
  vi.mocked(api.getDeviceCommands).mockResolvedValue([])
  vi.mocked(api.getActivity).mockResolvedValue({ generated_at: EXAMPLE, newest_agent_version: '2.83.0', activities: [] })
  vi.mocked(api.getSecurityScore).mockResolvedValue({
    overall_score: 72, max_score: 100, grade: 'C', categories: [], evaluated_at: EXAMPLE,
  } as never)
})

const timers = [
  { name: 'backup.timer', calendar: '*-*-* 06:00:00', next_run: EXAMPLE, last_run: '1793120709', unit: 'backup.service', enabled: true },
]

describe('TIME-12H-001 [AC-036] cron and timer run dates use the default form', () => {
  beforeEach(() => {
    mockDevice({ system: { hostname: 'web-01', arch: 'amd64' }, cron_jobs: { timers } })
  })

  it('renders an ISO run date and a unix-seconds run date as 2026-10-27 1:05 PM EDT', async () => {
    renderPage({ storage: null, serverZone: 'America/New_York', now: () => Date.parse(EXAMPLE) })
    const cells = await screen.findAllByText('2026-10-27 1:05 PM EDT')
    expect(cells, 'next_run (ISO) and last_run (unix seconds) both render').toHaveLength(2)
  })

  it('renders both run dates in 24-hour form when the 24-hour cycle is stored', async () => {
    renderPage({
      storage: {
        getItem: (key: string) => (key === TIME_ZONE_STORAGE_KEY ? 'Server' : key === HOUR_CYCLE_STORAGE_KEY ? '24h' : null),
        setItem: () => {},
      },
      serverZone: 'America/New_York',
    })
    expect(await screen.findAllByText('2026-10-27 13:05 EDT')).toHaveLength(2)
  })

  it('describes the calendar in the 12-hour cycle in the tooltip', async () => {
    renderPage({ storage: null, serverZone: 'America/New_York' })
    await userEvent.hover(await screen.findByText('*-*-* 06:00:00'))
    expect(await screen.findByText('Daily at 6:00 AM')).toBeInTheDocument()
  })

  it('describes the calendar in the 24-hour cycle in the tooltip', async () => {
    renderPage({
      storage: {
        getItem: (key: string) => (key === TIME_ZONE_STORAGE_KEY ? 'UTC' : key === HOUR_CYCLE_STORAGE_KEY ? '24h' : null),
        setItem: () => {},
      },
    })
    await userEvent.hover(await screen.findByText('*-*-* 06:00:00'))
    expect(await screen.findByText('Daily at 06:00')).toBeInTheDocument()
  })
})

describe('TIME-12H-001 [AC-058] the saved filename uses the server-zone date', () => {
  let captured: string | null
  const originalCreateElement = document.createElement.bind(document)

  beforeEach(() => {
    captured = null
    mockDevice({ system: { hostname: 'web-01', arch: 'amd64' } })
    vi.mocked(api.getDeviceSummary).mockResolvedValue('# Device Summary: web-01\n')
    Object.defineProperty(URL, 'createObjectURL', { value: vi.fn().mockReturnValue('blob:test-url'), writable: true, configurable: true })
    Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), writable: true, configurable: true })
    vi.spyOn(document, 'createElement').mockImplementation(((tag: string) => {
      const el = originalCreateElement(tag)
      if (tag === 'a') {
        Object.defineProperty(el, 'download', {
          get: () => captured ?? '',
          set: (value: string) => {
            captured = value
          },
          configurable: true,
        })
      }
      return el
    }) as typeof document.createElement)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  async function download() {
    await userEvent.click(await screen.findByRole('button', { name: /Export device summary/i }))
    await userEvent.click(await screen.findByText('Download as Markdown'))
    await waitFor(() => expect(captured).not.toBeNull())
  }

  it('names the file with the New York date when it is already the 28th in UTC', async () => {
    renderPage({ storage: null, serverZone: 'America/New_York', now: () => Date.parse('2026-10-28T02:00:00Z') })
    await download()
    expect(captured).toBe('web-01-summary-2026-10-27.md')
  })

  it('names the file with the UTC date while the server zone is unknown', async () => {
    renderPage({ storage: null, now: () => Date.parse('2026-10-28T02:00:00Z') })
    await download()
    expect(captured).toBe('web-01-summary-2026-10-28.md')
  })

  it('ignores the chosen display zone: the name always follows the server zone', async () => {
    renderPage({
      storage: {
        getItem: (key: string) => (key === TIME_ZONE_STORAGE_KEY ? 'UTC' : key === HOUR_CYCLE_STORAGE_KEY ? '12h' : null),
        setItem: () => {},
      },
      serverZone: 'America/New_York',
      now: () => Date.parse('2026-10-28T02:00:00Z'),
    })
    await download()
    expect(captured).toBe('web-01-summary-2026-10-27.md')
  })

  it('does not read the real clock: the date comes from the injected now', async () => {
    renderPage({ storage: null, serverZone: 'America/New_York', now: () => Date.parse('2031-01-02T15:00:00Z') })
    await download()
    expect(captured).toBe('web-01-summary-2031-01-02.md')
  })
})

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import Dashboard from '../Dashboard'
import type { FleetHeartbeatsResponse, FleetContainerRow } from '../../types/models'

const DEVICE_1 = {
  id: 'dev-1',
  hostname: 'node-1',
  short_id: 'abc123',
  arch: 'amd64',
  status: 'online' as const,
  location: '',
  tags: [],
  docker_available: true,
  docker_container_count: 2,
  docker_auto_update_container_count: 0,
  auto_patch: false,
  has_auto_update: false,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
}

const DEVICE_2 = {
  ...DEVICE_1,
  id: 'dev-2',
  hostname: 'node-2',
  short_id: 'def456',
  status: 'offline' as const,
  docker_container_count: 0,
}

const HB_RESP: FleetHeartbeatsResponse = {
  since: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
  until: new Date().toISOString(),
  devices: {
    'dev-1': [
      {
        device_id: 'dev-1',
        timestamp: new Date().toISOString(),
        data: {
          uptime: 3600,
          cpu_percent: 45,
          mem_percent: 60,
          load_avg_1m: 1.2,
          disk_root_percent: 70,
        },
      },
    ],
  },
  devices_with_gpu: [],
}

const CONTAINERS: FleetContainerRow[] = [
  {
    device_id: 'dev-1',
    hostname: 'node-1',
    container_id: 'c1',
    container_name: 'plex',
    image: 'linuxserver/plex:latest',
    stack: 'media',
    state: 'running',
    cpu_percent: 12.5,
    mem_usage: 1024 * 1024 * 512,
    mem_limit: 1024 * 1024 * 1024,
    restart_count: 0,
    update_available: false,
  },
]

const BASE_DEVICE_SERIES = [
  {
    deviceId: 'dev-1',
    hostname: 'node-1',
    cpuPoints:   [{ timestamp: '2024-01-01T00:00:00Z', value: 45 }],
    memPoints:   [{ timestamp: '2024-01-01T00:00:00Z', value: 60 }],
    diskPoints:  [{ timestamp: '2024-01-01T00:00:00Z', value: 70 }],
    loadPoints:  [{ timestamp: '2024-01-01T00:00:00Z', value: 30 }],
    netRxPoints: [{ timestamp: '2024-01-01T00:00:00Z', value: 1000 }],
    netTxPoints: [{ timestamp: '2024-01-01T00:00:00Z', value: 500 }],
  },
]

vi.mock('../../components/Sparkline', () => ({
  default: () => <svg data-testid="sparkline-mock" />,
}))

vi.mock('recharts', async () => {
  const actual = await vi.importActual<typeof import('recharts')>('recharts')
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
      <div data-testid="responsive-container">{children}</div>
    ),
  }
})

const mockGetFleetHeartbeats = vi.fn()
const mockGetFleetContainers = vi.fn()
const mockGetEvents = vi.fn()

vi.mock('../../api/client', () => ({
  api: {
    getFleetHeartbeats: (...args: unknown[]) => mockGetFleetHeartbeats(...args),
    getFleetContainers: (...args: unknown[]) => mockGetFleetContainers(...args),
    getEvents: (...args: unknown[]) => mockGetEvents(...args),
    getServerUpdate: vi.fn().mockResolvedValue({ latest_version: '2.0.0' }),
  },
}))

const mockUseDevicesReturn = {
  data: [DEVICE_1, DEVICE_2],
  isLoading: false,
  wsConnected: true,
}

vi.mock('../../hooks/useDevices', () => ({
  useDevices: vi.fn(() => mockUseDevicesReturn),
}))

vi.mock('../../hooks/useWebSocket', () => ({
  useWebSocket: vi.fn(),
}))

vi.mock('../../hooks/useFleetMetrics', () => ({
  useFleetMetrics: vi.fn(() => ({
    perDeviceSeries: BASE_DEVICE_SERIES,
    heartbeatsLoading: false,
  })),
}))

function renderDashboard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/dashboard']}>
        <Dashboard />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(async () => {
  mockGetFleetHeartbeats.mockResolvedValue(HB_RESP)
  mockGetFleetContainers.mockResolvedValue(CONTAINERS)
  mockGetEvents.mockResolvedValue([])

  const { useDevices } = await import('../../hooks/useDevices')
  vi.mocked(useDevices).mockReturnValue({
    data: [DEVICE_1, DEVICE_2],
    isLoading: false,
    wsConnected: true,
  } as unknown as ReturnType<typeof useDevices>)

  const { useFleetMetrics } = await import('../../hooks/useFleetMetrics')
  vi.mocked(useFleetMetrics).mockReturnValue({
    perDeviceSeries: BASE_DEVICE_SERIES,
    heartbeatsLoading: false,
  })
})

describe('Dashboard — route renders at /dashboard', () => {
  it('renders the dashboard page root', async () => {
    renderDashboard()
    expect(await screen.findByTestId('dashboard-page')).toBeInTheDocument()
  })
})

describe('Dashboard — per-device charts', () => {
  it('renders the per-device charts grid', async () => {
    renderDashboard()
    expect(await screen.findByTestId('per-device-charts')).toBeInTheDocument()
  })

  it('renders one card per device with series', async () => {
    renderDashboard()
    expect(await screen.findByTestId('device-chart-dev-1')).toBeInTheDocument()
  })

  it('renders empty state when no series', async () => {
    const { useFleetMetrics } = await import('../../hooks/useFleetMetrics')
    vi.mocked(useFleetMetrics).mockReturnValue({
      perDeviceSeries: [],
      heartbeatsLoading: false,
    })
    renderDashboard()
    expect(await screen.findByTestId('per-device-charts-empty')).toBeInTheDocument()
  })

  it('renders skeleton placeholders while loading', async () => {
    const { useFleetMetrics } = await import('../../hooks/useFleetMetrics')
    vi.mocked(useFleetMetrics).mockReturnValue({
      perDeviceSeries: [],
      heartbeatsLoading: true,
    })
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(screen.queryByTestId('per-device-charts')).not.toBeInTheDocument()
    expect(screen.queryByTestId('per-device-charts-empty')).not.toBeInTheDocument()
  })
})

describe('[AC-012] each device card renders network sub-chart', () => {
  it('[AC-012] device-net-chart testid renders alongside device-chart', async () => {
    renderDashboard()
    expect(await screen.findByTestId('device-chart-dev-1')).toBeInTheDocument()
    expect(await screen.findByTestId('device-net-chart-dev-1')).toBeInTheDocument()
  })
})

describe('[AC-016] all-old-agent fleet renders zero net chart, not error', () => {
  it('[AC-016] net chart renders without error when both series are zero/empty', async () => {
    const { useFleetMetrics } = await import('../../hooks/useFleetMetrics')
    vi.mocked(useFleetMetrics).mockReturnValue({
      perDeviceSeries: [
        {
          deviceId: 'dev-1',
          hostname: 'node-1',
          cpuPoints:   [{ timestamp: '2024-01-01T00:00:00Z', value: 0 }],
          memPoints:   [{ timestamp: '2024-01-01T00:00:00Z', value: 0 }],
          diskPoints:  [{ timestamp: '2024-01-01T00:00:00Z', value: 0 }],
          loadPoints:  [{ timestamp: '2024-01-01T00:00:00Z', value: 0 }],
          netRxPoints: [],
          netTxPoints: [],
        },
      ],
      heartbeatsLoading: false,
    })
    renderDashboard()
    // Chart must render — no error boundary or missing testid
    expect(await screen.findByTestId('device-net-chart-dev-1')).toBeInTheDocument()
  })
})

describe('Dashboard — container leaderboard', () => {
  it('renders the leaderboard section', async () => {
    renderDashboard()
    expect(await screen.findByTestId('container-leaderboard')).toBeInTheDocument()
  })

  it('renders a row for each visible container', async () => {
    renderDashboard()
    expect(await screen.findByTestId('leaderboard-row-plex')).toBeInTheDocument()
  })
})

describe('Dashboard — activity river', () => {
  it('renders the activity river section', async () => {
    renderDashboard()
    expect(await screen.findByTestId('activity-river')).toBeInTheDocument()
  })
})

describe('Dashboard — reuses existing WS singleton', () => {
  it('does not call the WebSocket constructor on dashboard mount', async () => {
    const wsSpy = vi.spyOn(globalThis, 'WebSocket')
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(wsSpy).not.toHaveBeenCalled()
    wsSpy.mockRestore()
  })
})

describe('Dashboard — disconnected banner', () => {
  it('renders banner when wsConnected is false', async () => {
    const { useDevices } = await import('../../hooks/useDevices')
    vi.mocked(useDevices).mockReturnValue({
      data: [DEVICE_1],
      isLoading: false,
      wsConnected: false,
    } as unknown as ReturnType<typeof useDevices>)
    renderDashboard()
    expect(await screen.findByTestId('disconnected-banner')).toBeInTheDocument()
  })

  it('does not render banner when wsConnected is true', async () => {
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(screen.queryByTestId('disconnected-banner')).not.toBeInTheDocument()
  })

  it('per-device charts still render under disconnect', async () => {
    const { useDevices } = await import('../../hooks/useDevices')
    vi.mocked(useDevices).mockReturnValue({
      data: [DEVICE_1],
      isLoading: false,
      wsConnected: false,
    } as unknown as ReturnType<typeof useDevices>)
    renderDashboard()
    expect(await screen.findByTestId('disconnected-banner')).toBeInTheDocument()
    expect(await screen.findByTestId('per-device-charts')).toBeInTheDocument()
  })
})

describe('Dashboard — non-color signals on banner', () => {
  it('banner has SVG icon (aria-hidden) alongside text label', async () => {
    const { useDevices } = await import('../../hooks/useDevices')
    vi.mocked(useDevices).mockReturnValue({
      data: [],
      isLoading: false,
      wsConnected: false,
    } as unknown as ReturnType<typeof useDevices>)
    renderDashboard()
    const banner = await screen.findByTestId('disconnected-banner')
    expect(banner.querySelectorAll('svg[aria-hidden="true"]').length).toBeGreaterThan(0)
    expect(banner.textContent).toContain('disconnected')
  })
})

describe('Dashboard — no map, drag-drop, or drill-down panels', () => {
  it('does not render map, drag-handle, or drilldown-panel testids', async () => {
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(screen.queryByTestId('map')).not.toBeInTheDocument()
    expect(screen.queryByTestId('drag-handle')).not.toBeInTheDocument()
    expect(screen.queryByTestId('drilldown-panel')).not.toBeInTheDocument()
  })
})

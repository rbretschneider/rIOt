import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import Dashboard from '../Dashboard'
import type { FleetHeartbeatsResponse, FleetContainerRow } from '../../types/models'

// ---------------------------------------------------------------------------
// Shared test data
// ---------------------------------------------------------------------------

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

const HB_RESP_NO_GPU: FleetHeartbeatsResponse = {
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

const HB_RESP_WITH_GPU: FleetHeartbeatsResponse = {
  ...HB_RESP_NO_GPU,
  devices_with_gpu: ['dev-1'],
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

const EMPTY_KPIS = {
  onlineCount: 1,
  totalCount: 2,
  cpuAvg: 45,
  cpuPeak: 45,
  cpuAvgPrev: 0,
  cpuPeakPrev: 0,
  memAvg: 60,
  memPeak: 60,
  memAvgPrev: 0,
  memPeakPrev: 0,
  worstDiskPercent: 70,
  worstDiskHostname: 'node-1',
  containersRunning: 1,
  containersTotal: 1,
  alertsBySeverity: { warning: 0, critical: 0 },
  pendingUpdates: 0,
  gpuAvg: 0,
}

// ---------------------------------------------------------------------------
// Module mocks
// ---------------------------------------------------------------------------

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
const mockGetPatchStatus = vi.fn()

vi.mock('../../api/client', () => ({
  api: {
    getFleetHeartbeats: (...args: unknown[]) => mockGetFleetHeartbeats(...args),
    getFleetContainers: (...args: unknown[]) => mockGetFleetContainers(...args),
    getEvents: (...args: unknown[]) => mockGetEvents(...args),
    getPatchStatus: (...args: unknown[]) => mockGetPatchStatus(...args),
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

vi.mock('../../hooks/usePerDevicePulse', () => ({
  usePerDevicePulse: vi.fn(() => ({})),
}))

// Mock useFleetMetrics to avoid the useMemo/setState infinite render loop
vi.mock('../../hooks/useFleetMetrics', () => ({
  useFleetMetrics: vi.fn(() => ({
    kpis: EMPTY_KPIS,
    perDeviceSeries: [],
    fleetTimeSeries: { diskRead: [], diskWrite: [] },
    hasGPU: false,
    containersLoading: false,
    heartbeatsLoading: false,
    flashedKPIs: new Set<string>(),
  })),
}))

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

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
  mockGetFleetHeartbeats.mockResolvedValue(HB_RESP_NO_GPU)
  mockGetFleetContainers.mockResolvedValue(CONTAINERS)
  mockGetEvents.mockResolvedValue([])
  mockGetPatchStatus.mockResolvedValue([])

  // Reset useDevices mock to default
  const { useDevices } = await import('../../hooks/useDevices')
  vi.mocked(useDevices).mockReturnValue({
    data: [DEVICE_1, DEVICE_2],
    isLoading: false,
    wsConnected: true,
  } as unknown as ReturnType<typeof useDevices>)

  // Reset useFleetMetrics to default (no GPU)
  const { useFleetMetrics } = await import('../../hooks/useFleetMetrics')
  vi.mocked(useFleetMetrics).mockReturnValue({
    kpis: EMPTY_KPIS,
    perDeviceSeries: [],
    fleetTimeSeries: { diskRead: [], diskWrite: [] },
    hasGPU: false,
    containersLoading: false,
    heartbeatsLoading: false,
    flashedKPIs: new Set<string>(),
  })
})

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('[AC-001] Dashboard — route renders at /dashboard (FR-001)', () => {
  it('renders the dashboard page root element', async () => {
    renderDashboard()
    expect(await screen.findByTestId('dashboard-page')).toBeInTheDocument()
  })
})

describe('[AC-004] Dashboard — KPI strip renders 7 mandatory tiles (FR-011)', () => {
  it('renders kpi-strip section', async () => {
    renderDashboard()
    expect(await screen.findByTestId('kpi-strip')).toBeInTheDocument()
  })

  it('renders Devices tile', async () => {
    renderDashboard()
    expect(await screen.findByTestId('kpi-devices')).toBeInTheDocument()
  })

  it('renders CPU avg tile', async () => {
    renderDashboard()
    expect(await screen.findByTestId('kpi-cpu')).toBeInTheDocument()
  })

  it('renders RAM avg tile', async () => {
    renderDashboard()
    expect(await screen.findByTestId('kpi-ram')).toBeInTheDocument()
  })

  it('renders Worst disk tile', async () => {
    renderDashboard()
    expect(await screen.findByTestId('kpi-worst-disk')).toBeInTheDocument()
  })

  it('renders Containers tile', async () => {
    renderDashboard()
    expect(await screen.findByTestId('kpi-containers')).toBeInTheDocument()
  })

  it('renders Active alerts tile', async () => {
    renderDashboard()
    expect(await screen.findByTestId('kpi-alerts')).toBeInTheDocument()
  })

  it('renders Pending updates tile', async () => {
    renderDashboard()
    expect(await screen.findByTestId('kpi-updates')).toBeInTheDocument()
  })
})

describe('[AC-005] Dashboard — GPU tile absent when no GPU in fleet (FR-014)', () => {
  it('does not render GPU tile when hasGPU is false', async () => {
    const { useFleetMetrics } = await import('../../hooks/useFleetMetrics')
    vi.mocked(useFleetMetrics).mockReturnValue({
      kpis: EMPTY_KPIS,
      perDeviceSeries: [],
      fleetTimeSeries: { diskRead: [], diskWrite: [] },
      hasGPU: false,
      containersLoading: false,
      heartbeatsLoading: false,
      flashedKPIs: new Set(),
    })
    renderDashboard()
    await screen.findByTestId('kpi-strip')
    expect(screen.queryByTestId('kpi-gpu')).not.toBeInTheDocument()
  })
})

describe('[AC-006] Dashboard — GPU tile renders when fleet has GPU devices (FR-014)', () => {
  it('renders GPU tile when hasGPU is true', async () => {
    mockGetFleetHeartbeats.mockResolvedValue(HB_RESP_WITH_GPU)
    const { useFleetMetrics } = await import('../../hooks/useFleetMetrics')
    vi.mocked(useFleetMetrics).mockReturnValue({
      kpis: { ...EMPTY_KPIS, gpuAvg: 55 },
      perDeviceSeries: [],
      fleetTimeSeries: { diskRead: [], diskWrite: [] },
      hasGPU: true,
      containersLoading: false,
      heartbeatsLoading: false,
      flashedKPIs: new Set(),
    })
    renderDashboard()
    expect(await screen.findByTestId('kpi-gpu')).toBeInTheDocument()
  })
})

describe('[AC-008] Dashboard — worst-disk tile shows highest-disk device (FR-013 / BR-003)', () => {
  it('renders worst-disk hostname from KPI data', async () => {
    renderDashboard()
    // EMPTY_KPIS has worstDiskHostname: 'node-1'
    await screen.findByTestId('kpi-worst-disk')
    expect(screen.getByTestId('kpi-worst-disk').textContent).toContain('node-1')
  })
})

describe('[AC-009] Dashboard — KPITile delta glyph with color+text (AC-034)', () => {
  it('delta zero renders no-change glyph on zero-delta tiles', async () => {
    renderDashboard()
    await screen.findByTestId('kpi-strip')
    const noChanges = screen.queryAllByLabelText('No change')
    expect(noChanges.length).toBeGreaterThan(0)
  })
})

describe('[AC-010] Dashboard — SmallMultiples renders 3 charts (FR-020)', () => {
  it('renders the small-multiples grid', async () => {
    renderDashboard()
    expect(await screen.findByTestId('small-multiples')).toBeInTheDocument()
  })

  it('renders chart for CPU per device', async () => {
    renderDashboard()
    await screen.findByTestId('small-multiples')
    expect(screen.getByTestId('chart-cpu-per-device')).toBeInTheDocument()
  })

  it('renders chart for Memory per device', async () => {
    renderDashboard()
    await screen.findByTestId('small-multiples')
    expect(screen.getByTestId('chart-mem-per-device')).toBeInTheDocument()
  })

  it('renders chart for Disk I/O', async () => {
    renderDashboard()
    await screen.findByTestId('small-multiples')
    expect(screen.getByTestId('chart-disk-io')).toBeInTheDocument()
  })

  it('does not render Network chart (FR-020 deferred)', async () => {
    renderDashboard()
    await screen.findByTestId('small-multiples')
    expect(screen.queryByTestId('chart-network')).not.toBeInTheDocument()
  })
})

describe('[AC-013] Dashboard — HeatmapGrid renders device cards (FR-030)', () => {
  it('renders the heatmap grid', async () => {
    renderDashboard()
    expect(await screen.findByTestId('heatmap-grid')).toBeInTheDocument()
  })

  it('renders a card for each device', async () => {
    renderDashboard()
    expect(await screen.findByTestId('device-card-dev-1')).toBeInTheDocument()
    expect(await screen.findByTestId('device-card-dev-2')).toBeInTheDocument()
  })
})

describe('[AC-018] Dashboard — heatmap empty state when no devices', () => {
  it('renders empty state when device list is empty', async () => {
    const { useDevices } = await import('../../hooks/useDevices')
    vi.mocked(useDevices).mockReturnValue({
      data: [],
      isLoading: false,
      wsConnected: true,
    } as unknown as ReturnType<typeof useDevices>)
    renderDashboard()
    expect(await screen.findByTestId('heatmap-empty-state')).toBeInTheDocument()
  })
})

describe('[AC-022] Dashboard — ContainerLeaderboard renders (FR-040)', () => {
  it('renders the container leaderboard section', async () => {
    renderDashboard()
    expect(await screen.findByTestId('container-leaderboard')).toBeInTheDocument()
  })

  it('renders leaderboard row for the container', async () => {
    renderDashboard()
    expect(await screen.findByTestId('leaderboard-row-plex')).toBeInTheDocument()
  })
})

describe('[AC-023] Dashboard — ActivityRiver renders (FR-050)', () => {
  it('renders the activity river section', async () => {
    renderDashboard()
    expect(await screen.findByTestId('activity-river')).toBeInTheDocument()
  })
})

describe('[AC-027] Dashboard — reuses existing WS singleton, no new WebSocket(...) call (AD-002)', () => {
  it('does not call the WebSocket constructor on dashboard mount', async () => {
    const wsSpy = vi.spyOn(globalThis, 'WebSocket')
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(wsSpy).not.toHaveBeenCalled()
    wsSpy.mockRestore()
  })
})

describe('[AC-029] Dashboard — DisconnectedBanner when wsConnected=false (FR-063)', () => {
  it('renders disconnected banner when wsConnected is false', async () => {
    const { useDevices } = await import('../../hooks/useDevices')
    vi.mocked(useDevices).mockReturnValue({
      data: [DEVICE_1],
      isLoading: false,
      wsConnected: false,
    } as unknown as ReturnType<typeof useDevices>)
    renderDashboard()
    expect(await screen.findByTestId('disconnected-banner')).toBeInTheDocument()
  })

  it('does not render disconnected banner when wsConnected is true', async () => {
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(screen.queryByTestId('disconnected-banner')).not.toBeInTheDocument()
  })

  it('KPI strip still renders (last-known values) when disconnected', async () => {
    const { useDevices } = await import('../../hooks/useDevices')
    vi.mocked(useDevices).mockReturnValue({
      data: [DEVICE_1],
      isLoading: false,
      wsConnected: false,
    } as unknown as ReturnType<typeof useDevices>)
    renderDashboard()
    expect(await screen.findByTestId('disconnected-banner')).toBeInTheDocument()
    expect(await screen.findByTestId('kpi-strip')).toBeInTheDocument()
  })
})

describe('[AC-034] Dashboard — non-color signals on all color-bearing elements', () => {
  it('disconnected banner has SVG icon (aria-hidden) alongside text label', async () => {
    const { useDevices } = await import('../../hooks/useDevices')
    vi.mocked(useDevices).mockReturnValue({
      data: [],
      isLoading: false,
      wsConnected: false,
    } as unknown as ReturnType<typeof useDevices>)
    renderDashboard()
    const banner = await screen.findByTestId('disconnected-banner')
    const svgs = banner.querySelectorAll('svg[aria-hidden="true"]')
    expect(svgs.length).toBeGreaterThan(0)
    expect(banner.textContent).toContain('disconnected')
  })
})

describe('[AC-035] Dashboard — no map, drag-drop, or embedded drill-down panels', () => {
  it('does not render a data-testid="map" element', async () => {
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(screen.queryByTestId('map')).not.toBeInTheDocument()
  })

  it('does not render a drag-handle element', async () => {
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(screen.queryByTestId('drag-handle')).not.toBeInTheDocument()
  })

  it('does not render a drilldown-panel element', async () => {
    renderDashboard()
    await screen.findByTestId('dashboard-page')
    expect(screen.queryByTestId('drilldown-panel')).not.toBeInTheDocument()
  })
})

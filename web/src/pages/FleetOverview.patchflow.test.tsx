import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import FleetOverview from './FleetOverview'
import { api, type DevicePatchInfo } from '../api/client'
import { ToastProvider } from '../components/ui'

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

vi.mock('../api/client', () => ({
  api: {
    getServerUpdate: vi.fn(),
    getPatchStatus: vi.fn(),
    getPatchStatusDetail: vi.fn(),
    getAgentVersions: vi.fn(),
    bulkUpdateAgents: vi.fn(),
    bulkPatchDevices: vi.fn(),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: vi.fn(() => ({
    data: [
      { id: 'dev-1', hostname: 'node-1', short_id: 'abc123', arch: 'amd64', status: 'online', agent_version: '2.0.4', primary_ip: '10.0.0.1', tags: [], last_heartbeat: new Date().toISOString() },
      { id: 'dev-2', hostname: 'node-2', short_id: 'def456', arch: 'amd64', status: 'online', agent_version: '2.0.4', primary_ip: '10.0.0.2', tags: [], last_heartbeat: new Date().toISOString() },
    ],
    isLoading: false,
    wsConnected: false,
  })),
}))

const STALE_LABEL = 'Package lists out of date — refresh pending'

function row(over: Partial<DevicePatchInfo>): DevicePatchInfo {
  return {
    device_id: 'dev-1',
    hostname: 'node-1',
    pending_updates: 0,
    security_count: 0,
    reboot_class_count: 0,
    reboot_required: false,
    package_manager: 'apt',
    lists_freshness: 'fresh',
    ...over,
  }
}

function renderFleet() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ToastProvider>
          <FleetOverview />
        </ToastProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function openPanel() {
  fireEvent.click(await screen.findByRole('button', { name: /Review Patches/ }))
  return screen.findByRole('dialog', { name: 'Pending OS Patches' })
}

function tableRowOf(hostname: string) {
  return screen.getByText(hostname).closest('tr') as HTMLElement
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.0.4' } as never)
  vi.mocked(api.getAgentVersions).mockResolvedValue([])
})

describe('[PATCH-FLOW-001 AC-006] a pending kernel is not a reboot indicator', () => {
  it('shows no reboot-required indicator for a host with only a pending reboot-class update', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([
      row({ pending_updates: 4, reboot_class_count: 1, reboot_required: false }),
    ])
    renderFleet()
    await screen.findByText('node-1')

    expect(screen.queryByText(/reboot required/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/reboot needed/i)).not.toBeInTheDocument()
  })
})

describe('[PATCH-FLOW-001 AC-027] stale package lists in the device table', () => {
  it('shows the stale label instead of the pending count', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([
      row({ pending_updates: 38, lists_freshness: 'stale' }),
      row({ device_id: 'dev-2', hostname: 'node-2', pending_updates: 5 }),
    ])
    renderFleet()
    await screen.findByText('node-1')

    const staleCell = await screen.findByText('Lists out of date')
    const stale = tableRowOf('node-1')
    expect(staleCell).toHaveAttribute('title', STALE_LABEL)
    expect(within(stale).queryByText('38')).not.toBeInTheDocument()
    expect(within(tableRowOf('node-2')).getByText('5')).toBeInTheDocument()
  })

  it('keeps today\'s count when the freshness is unknown or absent (AC-029)', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([
      row({ pending_updates: 38, lists_freshness: 'unknown' }),
      row({ device_id: 'dev-2', hostname: 'node-2', pending_updates: 5, lists_freshness: undefined }),
    ])
    renderFleet()
    await screen.findByText('38')

    expect(within(tableRowOf('node-1')).getByText('38')).toBeInTheDocument()
    expect(within(tableRowOf('node-2')).getByText('5')).toBeInTheDocument()
  })
})

describe('[PATCH-FLOW-001 AC-027, OQ-008] stale hosts in the Pending OS Patches panel', () => {
  beforeEach(() => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([row({ pending_updates: 38, lists_freshness: 'stale' })])
  })

  it('shows the stale label in place of the package count on the host row', async () => {
    vi.mocked(api.getPatchStatusDetail).mockResolvedValue([
      row({ pending_updates: 38, security_count: 2, reboot_class_count: 1, lists_freshness: 'stale' }),
    ])
    renderFleet()

    const panel = await openPanel()

    expect(within(panel).getByText(STALE_LABEL)).toBeInTheDocument()
    expect(within(panel).queryByText(/38 pkgs/)).not.toBeInTheDocument()
    expect(within(panel).queryByText(/reboot-class/)).not.toBeInTheDocument()
  })

  it('keeps the raw security count visible on a stale host, qualified (SEC-007)', async () => {
    vi.mocked(api.getPatchStatusDetail).mockResolvedValue([
      row({ pending_updates: 38, security_count: 2, lists_freshness: 'stale' }),
    ])
    renderFleet()

    const panel = await openPanel()

    expect(within(panel).getByText('2 security (lists out of date)')).toBeInTheDocument()
  })

  it('leaves stale hosts out of the package total and says how many were left out', async () => {
    vi.mocked(api.getPatchStatusDetail).mockResolvedValue([
      row({ pending_updates: 38, lists_freshness: 'stale' }),
      row({ device_id: 'dev-2', hostname: 'node-2', pending_updates: 5 }),
    ])
    renderFleet()

    const panel = await openPanel()

    expect(panel).toHaveTextContent('5 packages across 2 devices')
    expect(panel).toHaveTextContent('1 device with out-of-date package lists not counted')
  })

  it('counts stale hosts in the security total and calls out their share (SEC-007)', async () => {
    vi.mocked(api.getPatchStatusDetail).mockResolvedValue([
      row({ pending_updates: 38, security_count: 3, lists_freshness: 'stale' }),
      row({ device_id: 'dev-2', hostname: 'node-2', pending_updates: 5, security_count: 1 }),
    ])
    renderFleet()

    const panel = await openPanel()

    expect(panel).toHaveTextContent('(4 security, 3 of them from out-of-date lists)')
  })

  it('shows no stale wording when every host is fresh', async () => {
    vi.mocked(api.getPatchStatusDetail).mockResolvedValue([
      row({ pending_updates: 5, security_count: 1 }),
      row({ device_id: 'dev-2', hostname: 'node-2', pending_updates: 2 }),
    ])
    renderFleet()

    const panel = await openPanel()

    expect(panel).toHaveTextContent('7 packages across 2 devices')
    expect(panel).not.toHaveTextContent(/out-of-date/)
    expect(panel).toHaveTextContent('(1 security)')
  })
})

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import FleetOverview from './FleetOverview'
import { api, type DevicePatchInfo, type PatchBadge } from '../api/client'
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
    data: ['node-1', 'node-2', 'node-3', 'node-4'].map((hostname, i) => ({
      id: `dev-${i + 1}`, hostname, short_id: `id${i}`, arch: 'amd64', status: 'online', agent_version: '2.0.4',
      primary_ip: `10.0.0.${i + 1}`, tags: [], last_heartbeat: new Date().toISOString(),
    })),
    isLoading: false,
    wsConnected: false,
  })),
}))

function badge(kind: PatchBadge['kind'], label: string): PatchBadge {
  return {
    kind, label,
    explanation: {
      automatic: 'No.', reboot: 'Not needed.', needs_you: [], needs_you_summary: kind === 'needs_you' ? '1 item needs you.' : 'Nothing.',
      info: [], as_of: '2026-10-26 02:00 UTC (just now)', notes: [],
    },
  }
}

function row(id: number, over: Partial<DevicePatchInfo>): DevicePatchInfo {
  return {
    device_id: `dev-${id}`, hostname: `node-${id}`, pending_updates: 0, security_count: 0,
    reboot_class_count: 0, reboot_required: false, package_manager: 'apt', lists_freshness: 'fresh', ...over,
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

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.0.4' } as never)
  vi.mocked(api.getAgentVersions).mockResolvedValue([])
})

describe('[EXTRA-UPD-001 AC-050] the fleet count includes only hosts whose badge is needs_you', () => {
  it('reads "1 host needs you" for needs_you, manual and auto, and filters to the first host', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([
      row(1, { patch_badge: badge('needs_you', 'Needs you') }),
      row(2, { patch_badge: badge('manual', 'Manual') }),
      row(3, { patch_badge: badge('auto', 'Auto') }),
    ])
    renderFleet()
    const button = await screen.findByRole('button', { name: '1 host needs you' })
    expect(screen.getByText('Manual').className).not.toMatch(/red-/)

    fireEvent.click(button)
    expect(screen.getByText('node-1')).toBeInTheDocument()
    expect(screen.queryByText('node-2')).not.toBeInTheDocument()
    expect(screen.queryByText('node-3')).not.toBeInTheDocument()
  })

  it('shows no needs-you button when every host is manual or auto', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([
      row(1, { patch_badge: badge('manual', 'Manual') }),
      row(2, { patch_badge: badge('auto', 'Auto') }),
    ])
    renderFleet()
    await screen.findByText('node-1')
    expect(screen.queryByRole('button', { name: /needs? you/ })).not.toBeInTheDocument()
  })
})

describe('[EXTRA-UPD-001 AC-038] the Pending OS Patches panel names the updater', () => {
  const rows = [
    row(1, { extra_updaters: [{ id: 'dietpi', status: 'update_available', installed_version: '9.20.1', available_version: '10.8.2', pending_count: 1, readiness: 'enabled' }], patch_badge: badge('auto', 'Auto') }),
    row(2, { pending_updates: 5, patch_badge: badge('auto', 'Auto') }),
  ]

  it('counts the DietPi-only host and lists it as "DietPi 10.8.2"', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue(rows)
    vi.mocked(api.getPatchStatusDetail).mockResolvedValue(rows)
    renderFleet()
    const open = await screen.findByRole('button', { name: 'Review Patches (2)' })
    fireEvent.click(open)
    const panel = await screen.findByRole('dialog', { name: 'Pending OS Patches' })
    const first = within(panel).getByText('node-1').closest('button') as HTMLElement
    expect(within(first).getByText('DietPi 10.8.2')).toBeInTheDocument()
    expect(within(first).queryByText(/pkg/)).not.toBeInTheDocument()
    const second = within(panel).getByText('node-2').closest('button') as HTMLElement
    expect(within(second).getByText('5 pkgs')).toBeInTheDocument()
  })
})

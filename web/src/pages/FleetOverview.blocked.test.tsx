import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import FleetOverview from './FleetOverview'
import { api, type DevicePatchInfo, type PatchBadge } from '../api/client'
import { ToastProvider } from '../components/ui'

// DIETPI-PATH-001 AC-017: the "N hosts need you" count and filter exclude
// Blocked hosts, and Blocked hosts have a count and filter of their own.

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
      blocked: [], info: [], as_of: '2026-10-26 02:00 UTC (just now)', notes: [],
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

describe('[DIETPI-PATH-001 AC-017] Blocked hosts are not hosts that need you', () => {
  it('counts only the needs_you host in "N hosts need you" and shows the blocked host in its own chip', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([
      row(1, { patch_badge: badge('needs_you', 'Needs you') }),
      row(2, { patch_badge: badge('blocked', 'Blocked') }),
      row(3, { patch_badge: badge('blocked', 'Blocked') }),
      row(4, { patch_badge: badge('auto', 'Auto') }),
    ])
    renderFleet()
    expect(await screen.findByRole('button', { name: '1 host needs you' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '2 blocked' })).toBeInTheDocument()
    const label = screen.getAllByText('Blocked')[0]
    expect(label.className).toMatch(/amber-/)
    expect(label.className).not.toMatch(/red-/)
  })

  it('the needs-you filter excludes blocked hosts and the blocked filter shows only them', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([
      row(1, { patch_badge: badge('needs_you', 'Needs you') }),
      row(2, { patch_badge: badge('blocked', 'Blocked') }),
      row(3, { patch_badge: badge('auto', 'Auto') }),
    ])
    renderFleet()
    fireEvent.click(await screen.findByRole('button', { name: '1 host needs you' }))
    expect(screen.getByText('node-1')).toBeInTheDocument()
    expect(screen.queryByText('node-2')).not.toBeInTheDocument()
    expect(screen.queryByText('node-3')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '1 blocked' }))
    expect(screen.getByText('node-2')).toBeInTheDocument()
    expect(screen.queryByText('node-1')).not.toBeInTheDocument()
    expect(screen.queryByText('node-3')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '1 host needs you' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: '1 blocked' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(screen.getByRole('button', { name: '1 blocked' }))
    expect(screen.getByText('node-1')).toBeInTheDocument()
    expect(screen.getByText('node-3')).toBeInTheDocument()
  })

  it('shows no needs-you button when the only open items are blocked', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([
      row(1, { patch_badge: badge('blocked', 'Blocked') }),
      row(2, { patch_badge: badge('auto', 'Auto') }),
    ])
    renderFleet()
    expect(await screen.findByRole('button', { name: '1 blocked' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /needs? you/ })).not.toBeInTheDocument()
  })

  it('shows no blocked chip when no host is blocked', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([row(1, { patch_badge: badge('auto', 'Auto') })])
    renderFleet()
    await screen.findByText('node-1')
    expect(screen.queryByRole('button', { name: /blocked/ })).not.toBeInTheDocument()
  })
})

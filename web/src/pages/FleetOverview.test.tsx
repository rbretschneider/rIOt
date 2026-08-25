import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import FleetOverview from './FleetOverview'
import { api } from '../api/client'

vi.mock('../api/client', () => ({
  api: {
    getDevices: vi.fn().mockResolvedValue([
      {
        id: 'dev-1',
        hostname: 'node-1',
        short_id: 'abc123',
        arch: 'amd64',
        status: 'online',
        agent_version: '2.0.4',
        primary_ip: '192.168.1.1',
        tags: ['prod'],
        last_heartbeat: new Date().toISOString(),
      },
    ]),
    getServerUpdate: vi.fn().mockResolvedValue({ latest_version: '2.0.5' }),
    getPatchStatus: vi.fn().mockResolvedValue([]),
    getPatchStatusDetail: vi.fn().mockResolvedValue([
      {
        device_id: 'dev-1',
        hostname: 'node-1',
        pending_updates: 3,
        security_count: 1,
        reboot_class_count: 2,
        reboot_required: true,
        package_manager: 'apt',
        updates: [
          { name: 'nvidia-driver-550', current_ver: '535.1', new_ver: '550.2', is_security: false, class: 'gpu_driver' },
          { name: 'linux-image-amd64', current_ver: '6.1.0', new_ver: '6.1.1', is_security: true, class: 'kernel' },
          { name: 'curl', current_ver: '8.0.0', new_ver: '8.1.0', is_security: false },
        ],
      },
    ]),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({
    data: [
      {
        id: 'dev-1',
        hostname: 'node-1',
        short_id: 'abc123',
        arch: 'amd64',
        status: 'online',
        agent_version: '2.0.4',
        primary_ip: '192.168.1.1',
        tags: ['prod'],
        last_heartbeat: new Date().toISOString(),
      },
    ],
    isLoading: false,
    wsConnected: false,
  }),
}))

function renderWithProviders() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <FleetOverview />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('FleetOverview', () => {
  it('renders without crashing (smoke test)', async () => {
    renderWithProviders()
    expect(await screen.findByText('node-1')).toBeInTheDocument()
  })

  it('shows outdated label instead of asterisk', async () => {
    renderWithProviders()
    expect(await screen.findByText('(outdated)')).toBeInTheDocument()
    expect(screen.queryByText('*')).not.toBeInTheDocument()
  })
})

describe('[AC-021] Reboot-required and reboot-class surfaced in patch review', () => {
  it('shows a reboot-required badge and reboot-class count for the device', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([
      { device_id: 'dev-1', hostname: 'node-1', pending_updates: 3, security_count: 1, reboot_class_count: 2, reboot_required: true },
    ])
    renderWithProviders()

    const reviewButton = await screen.findByText(/Review Patches/)
    fireEvent.click(reviewButton)

    expect(await screen.findByText('reboot required')).toBeInTheDocument()
    expect(screen.getByText('2 reboot-class')).toBeInTheDocument()
  })

  it('badges reboot-class packages in the expanded package list', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([
      { device_id: 'dev-1', hostname: 'node-1', pending_updates: 3, security_count: 1, reboot_class_count: 2, reboot_required: true },
    ])
    renderWithProviders()

    fireEvent.click(await screen.findByText(/Review Patches/))
    // Expand the device row (the modal row button) to reveal its per-package table.
    const rowButton = (await screen.findByText('2 reboot-class')).closest('button')!
    fireEvent.click(rowButton)

    expect(await screen.findByText('nvidia-driver-550')).toBeInTheDocument()
    expect(screen.getByText('GPU driver')).toBeInTheDocument()
    expect(screen.getByText('kernel')).toBeInTheDocument()
  })
})

describe('[AC-001] Score column removed from Fleet Overview', () => {
  it('does not render a Score column header in the device table', async () => {
    renderWithProviders()
    await screen.findByText('node-1')
    const headers = screen.getAllByRole('columnheader')
    const headerTexts = headers.map(h => h.textContent?.trim() ?? '')
    const hasScoreColumn = headerTexts.some(t => t === 'Score')
    expect(hasScoreColumn).toBe(false)
  })

  it('does not render a MiniScore radial indicator for any device', async () => {
    renderWithProviders()
    await screen.findByText('node-1')
    // MiniScore renders an SVG with a title attribute like "Security: ..."
    const scoreTitles = document.querySelectorAll('[title^="Security:"]')
    expect(scoreTitles.length).toBe(0)
  })
})

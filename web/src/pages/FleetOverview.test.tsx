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

// REBOOT-ETA-001: reboot-required indicators + estimated-reboot tooltip.
const ETA_ROW = {
  device_id: 'dev-1',
  hostname: 'node-1',
  pending_updates: 3,
  security_count: 1,
  reboot_class_count: 2,
  reboot_required: true,
  os_patch_mode: 'window' as const,
  next_window_start: '2026-10-19T02:00:00Z',
  os_patch_cooldown_minutes: 360,
  allow_reboot: true,
}

describe('[AC-001] panel badge renders when reboot required', () => {
  it('shows the reboot required badge in the Pending OS Patches panel', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([ETA_ROW])
    vi.mocked(api.getPatchStatusDetail).mockResolvedValueOnce([ETA_ROW])
    renderWithProviders()

    fireEvent.click(await screen.findByText(/Review Patches/))

    expect(await screen.findByText('reboot required')).toBeInTheDocument()
  })
})

describe('[AC-002] main-grid indicator renders when reboot required', () => {
  it('shows a reboot indicator in the device row', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([ETA_ROW])
    renderWithProviders()

    expect(await screen.findByRole('img', { name: 'Reboot required' })).toBeInTheDocument()
  })

  it('leaves the existing pending-update count in the row intact', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([ETA_ROW])
    renderWithProviders()

    await screen.findByRole('img', { name: 'Reboot required' })
    const row = screen.getByText('node-1').closest('tr')!
    expect(row.textContent, 'the count of pending updates must still render in the row').toContain('3')
    expect(row.textContent).toContain('192.168.1.1')
  })
})

describe('[AC-003] no indicator when reboot not required', () => {
  it('renders no grid indicator when reboot_required is false', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([{ ...ETA_ROW, reboot_required: false }])
    renderWithProviders()

    await screen.findByText(/Review Patches/)
    expect(screen.queryByRole('img', { name: 'Reboot required' })).not.toBeInTheDocument()
  })

  it('renders no grid indicator when the device has no patch-status row', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([{ ...ETA_ROW, device_id: 'some-other-device' }])
    renderWithProviders()

    await screen.findByText(/Review Patches/)
    expect(screen.queryByRole('img', { name: 'Reboot required' })).not.toBeInTheDocument()
  })

  it('renders no panel badge when reboot_required is false', async () => {
    const row = { ...ETA_ROW, reboot_required: false }
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([row])
    vi.mocked(api.getPatchStatusDetail).mockResolvedValueOnce([row])
    renderWithProviders()

    fireEvent.click(await screen.findByText(/Review Patches/))
    await screen.findByText('2 reboot-class')
    expect(screen.queryByText('reboot required')).not.toBeInTheDocument()
  })
})

describe('[AC-004] identical tooltip text in both placements', () => {
  it('gives the grid indicator and the panel badge the same title for one device', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([ETA_ROW])
    vi.mocked(api.getPatchStatusDetail).mockResolvedValueOnce([ETA_ROW])
    renderWithProviders()

    const gridTitle = (await screen.findByRole('img', { name: 'Reboot required' })).getAttribute('title')
    fireEvent.click(await screen.findByText(/Review Patches/))
    const panelTitle = (await screen.findByText('reboot required')).getAttribute('title')

    expect(gridTitle, 'grid indicator must carry a title').toBeTruthy()
    expect(panelTitle).toBe(gridTitle)
  })

  it('shows the same manual-reboot text in both placements when allow_reboot is false', async () => {
    const row = { ...ETA_ROW, allow_reboot: false }
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([row])
    vi.mocked(api.getPatchStatusDetail).mockResolvedValueOnce([row])
    renderWithProviders()

    const gridTitle = (await screen.findByRole('img', { name: 'Reboot required' })).getAttribute('title')
    fireEvent.click(await screen.findByText(/Review Patches/))
    const panelTitle = (await screen.findByText('reboot required')).getAttribute('title')

    expect(panelTitle).toBe(gridTitle)
    expect(gridTitle).toContain('Manual reboot required')
  })
})

describe('[AC-005] native title attribute in both placements', () => {
  it('delivers the explanatory text through the title attribute on both indicators', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([ETA_ROW])
    vi.mocked(api.getPatchStatusDetail).mockResolvedValueOnce([ETA_ROW])
    renderWithProviders()

    const grid = await screen.findByRole('img', { name: 'Reboot required' })
    expect(grid).toHaveAttribute('title')
    fireEvent.click(await screen.findByText(/Review Patches/))
    expect(await screen.findByText('reboot required')).toHaveAttribute('title')
  })
})

describe('[AC-006] window mode shows Est. reboot in the rendered tooltip', () => {
  it('renders the UTC estimate in the grid indicator title', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([ETA_ROW])
    renderWithProviders()

    const grid = await screen.findByRole('img', { name: 'Reboot required' })
    expect(grid.getAttribute('title')).toContain('Est. reboot: 2026-10-19 02:00 UTC')
  })
})

describe('[AC-019] fallback tooltip when no next start was computed', () => {
  it('renders reboot-required with no time in the grid indicator', async () => {
    const { next_window_start: _omit, ...noStart } = ETA_ROW
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([noStart])
    renderWithProviders()

    const grid = await screen.findByRole('img', { name: 'Reboot required' })
    expect(grid.getAttribute('title')).toBe('A reboot is required to activate the installed updates.')
  })
})

describe('[AC-020] no additional network request from the page', () => {
  it('drives the grid indicator from the single existing patch-status query', async () => {
    vi.mocked(api.getPatchStatus).mockClear()
    vi.mocked(api.getPatchStatusDetail).mockClear()
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([ETA_ROW])
    renderWithProviders()

    await screen.findByRole('img', { name: 'Reboot required' })

    expect(api.getPatchStatus).toHaveBeenCalledTimes(1)
    expect(api.getPatchStatusDetail, 'the detail request is only made when the panel is opened').not.toHaveBeenCalled()
  })
})

describe('[AC-022] existing badge non-regression', () => {
  it('still renders the "reboot required" badge text next to the reboot-class count', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([ETA_ROW])
    vi.mocked(api.getPatchStatusDetail).mockResolvedValueOnce([ETA_ROW])
    renderWithProviders()

    fireEvent.click(await screen.findByText(/Review Patches/))

    const badge = await screen.findByText('reboot required')
    expect(badge.textContent).toBe('reboot required')
    expect(badge.getAttribute('title')).toContain('reboot')
    expect(screen.getByText('2 reboot-class')).toBeInTheDocument()
  })
})

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import FleetOverview from './FleetOverview'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import { ToastProvider } from '../components/ui'

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
    getAgentVersions: vi.fn().mockResolvedValue([]),
    bulkUpdateAgents: vi.fn(),
    bulkPatchDevices: vi.fn(),
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
  useDevices: vi.fn(() => ({
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
  })),
}))

function renderWithProviders() {
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

// Added by QA Engineer (REBOOT-ETA-001).
describe('[AC-012] agent that never reported allow_reboot degrades to conditional wording', () => {
  // Covers AC-012, AC-004: unknown (absent) allow_reboot must NOT read as a false "manual"
  // and must NOT be an unconditional guarantee; both placements stay identical.
  it('shows conditional (not manual, not guaranteed) text identically in both placements', async () => {
    const { allow_reboot: _omit, ...unknown } = ETA_ROW
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([unknown])
    vi.mocked(api.getPatchStatusDetail).mockResolvedValueOnce([unknown])
    renderWithProviders()

    const gridTitle = (await screen.findByRole('img', { name: 'Reboot required' })).getAttribute('title')!
    fireEvent.click(await screen.findByText(/Review Patches/))
    const panelTitle = (await screen.findByText('reboot required')).getAttribute('title')

    expect(gridTitle).toContain('Est. reboot: 2026-10-19 02:00 UTC')
    expect(gridTitle).toContain('if this device permits automatic reboot')
    expect(gridTitle).not.toContain('Manual reboot required')
    expect(panelTitle).toBe(gridTitle)
  })
})

describe('[AC-007] anytime mode renders cooldown wording in the grid indicator', () => {
  // Covers AC-007, AC-008: mode wiring from the row to the rendered title
  it('renders anytime wording with no clock time', async () => {
    const row = { ...ETA_ROW, os_patch_mode: 'anytime' as const, next_window_start: undefined }
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([row])
    renderWithProviders()

    const title = (await screen.findByRole('img', { name: 'Reboot required' })).getAttribute('title')!
    expect(title).toContain('next automatic patch run (within the ~360 min cooldown)')
    expect(title).not.toContain('Est. reboot:')
  })

  it('[AC-008] disabled mode renders reboot-required with no time', async () => {
    const row = { ...ETA_ROW, os_patch_mode: 'disabled' as const, next_window_start: undefined }
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([row])
    renderWithProviders()

    const title = (await screen.findByRole('img', { name: 'Reboot required' })).getAttribute('title')
    expect(title).toBe('A reboot is required to activate the installed updates.')
  })
})

// UI-FOUNDATION-001: dialogs, toasts and honest pending states -------------------

const OUTDATED_VERSIONS = [{ version: '2.0.4', count: 3 }]
const PATCH_ROW = {
  device_id: 'dev-1',
  hostname: 'node-1',
  pending_updates: 3,
  security_count: 1,
  reboot_class_count: 0,
  reboot_required: false,
}

function useOutdatedAgents() {
  beforeEach(() => {
    vi.mocked(api.getAgentVersions).mockResolvedValue(OUTDATED_VERSIONS)
    vi.mocked(api.bulkUpdateAgents).mockReset()
  })
  afterEach(() => {
    vi.mocked(api.getAgentVersions).mockResolvedValue([])
  })
}

function usePendingPatches() {
  beforeEach(() => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([PATCH_ROW] as never)
    vi.mocked(api.bulkPatchDevices).mockReset()
  })
  afterEach(() => {
    vi.mocked(api.getPatchStatus).mockResolvedValue([])
  })
}

async function openUpdateDialog() {
  renderWithProviders()
  fireEvent.click(await screen.findByRole('button', { name: /Update Agents \(3\)/ }))
  return screen.getByRole('dialog', { name: 'Update Agents' })
}

async function openPatchReview() {
  renderWithProviders()
  fireEvent.click(await screen.findByRole('button', { name: /Review Patches/ }))
  const dialog = await screen.findByRole('dialog', { name: 'Pending OS Patches' })
  await within(dialog).findByText('node-1')
  return dialog
}

const toastRegion = () => screen.getByRole('region', { name: 'Notifications' })

describe('[UIF AC-030] PatchReviewModal is a named dialog', () => {
  usePendingPatches()

  it('exposes role=dialog named "Pending OS Patches" with aria-modal and a package summary', async () => {
    const dialog = await openPatchReview()
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAccessibleDescription(/3 packages across 1 device/)
  })

  it('focuses Cancel, never the fleet-wide confirm (SEC-001)', async () => {
    const dialog = await openPatchReview()
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(within(dialog).getByRole('button', { name: 'Update All Devices' })).not.toHaveFocus()
  })

  it('closes on Escape and carries no ad-hoc z-index', async () => {
    await openPatchReview()
    expect(screen.getByTestId('modal-backdrop').className).toContain('z-(--z-modal)')
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})

describe('[UIF AC-042] bulk update shows Updating… while in flight', () => {
  useOutdatedAgents()

  it('keeps the dialog open showing "Updating…" until the request settles', async () => {
    let finish: (v: { sent: number; skipped: number; total: number }) => void = () => {}
    vi.mocked(api.bulkUpdateAgents).mockReturnValue(new Promise(res => { finish = res }))
    const dialog = await openUpdateDialog()

    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))

    const busy = await within(dialog).findByRole('button', { name: 'Updating…' })
    expect(busy).toBeDisabled()
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeDisabled()
    expect(screen.getByRole('dialog', { name: 'Update Agents' })).toBeInTheDocument()

    await act(async () => finish({ sent: 3, skipped: 0, total: 3 }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('calls the API once per outdated version and never before confirmation', async () => {
    vi.mocked(api.bulkUpdateAgents).mockResolvedValue({ sent: 3, skipped: 0, total: 3 })
    const dialog = await openUpdateDialog()
    expect(api.bulkUpdateAgents).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))
    await waitFor(() => expect(api.bulkUpdateAgents).toHaveBeenCalledTimes(1))
    expect(vi.mocked(api.bulkUpdateAgents).mock.calls[0][0]).toBe('2.0.4')
  })
})

describe('[UIF AC-075] no inline result banners', () => {
  useOutdatedAgents()
  usePendingPatches()

  it('shows no "Agent update dispatched" banner in the page body after a bulk update', async () => {
    vi.mocked(api.bulkUpdateAgents).mockResolvedValue({ sent: 4, skipped: 1, total: 5 })
    const dialog = await openUpdateDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))
    await within(toastRegion()).findByText('Agent update dispatched')
    const bannerMatches = screen.getAllByText(/Agent update dispatched/)
    expect(bannerMatches.every(el => toastRegion().contains(el)), 'only the toast may carry this text').toBe(true)
  })

  it('shows no "Patch commands dispatched" banner in the page body after patch-all', async () => {
    vi.mocked(api.bulkPatchDevices).mockResolvedValue({ sent: 1, queued: 0, skipped: 0, total: 1 })
    const dialog = await openPatchReview()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update All Devices' }))
    await within(toastRegion()).findByText('Patch commands dispatched')
    const matches = screen.getAllByText(/Patch commands dispatched/)
    expect(matches.every(el => toastRegion().contains(el))).toBe(true)
  })

  it('does not use the eight-second banner timers: nothing lingers in the body once the toast is gone', async () => {
    vi.mocked(api.bulkUpdateAgents).mockResolvedValue({ sent: 1, skipped: 0, total: 1 })
    const dialog = await openUpdateDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))
    await within(toastRegion()).findByText('Agent update dispatched')
    fireEvent.click(within(toastRegion()).getByRole('button', { name: 'Dismiss notification' }))
    expect(screen.queryByText(/Agent update dispatched/)).toBeNull()
  })
})

describe('[UIF AC-076] bulk update success toast counts', () => {
  useOutdatedAgents()

  it('closes the dialog and toasts "4 sent, 1 skipped (offline)"', async () => {
    vi.mocked(api.bulkUpdateAgents).mockResolvedValue({ sent: 4, skipped: 1, total: 5 })
    const dialog = await openUpdateDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))

    expect(await within(toastRegion()).findByText('4 sent, 1 skipped (offline)')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('omits the skipped count when it is zero', async () => {
    vi.mocked(api.bulkUpdateAgents).mockResolvedValue({ sent: 3, skipped: 0, total: 3 })
    const dialog = await openUpdateDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))
    expect(await within(toastRegion()).findByText('3 sent')).toBeInTheDocument()
  })

  it('sums the counts across several outdated versions', async () => {
    vi.mocked(api.getAgentVersions).mockResolvedValue([
      { version: '2.0.4', count: 2 },
      { version: '2.0.3', count: 1 },
    ])
    vi.mocked(api.bulkUpdateAgents)
      .mockResolvedValueOnce({ sent: 2, skipped: 0, total: 2 })
      .mockResolvedValueOnce({ sent: 0, skipped: 1, total: 1 })
    renderWithProviders()
    fireEvent.click(await screen.findByRole('button', { name: /Update Agents \(3\)/ }))
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Update all' }))
    expect(await within(toastRegion()).findByText('2 sent, 1 skipped (offline)')).toBeInTheDocument()
  })
})

describe('[UIF AC-077] bulk update failure inline', () => {
  useOutdatedAgents()

  it('keeps the dialog open with the error inline and raises no toast', async () => {
    vi.mocked(api.bulkUpdateAgents).mockRejectedValue(new Error('HTTP 503: Service Unavailable'))
    const dialog = await openUpdateDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('HTTP 503: Service Unavailable')
    expect(within(toastRegion()).queryByRole('alert')).toBeNull()
    expect(within(dialog).getByRole('button', { name: 'Update all' })).toBeEnabled()
  })
})

describe('[UIF SEC-001] the gesture that opens the fleet-wide update cannot also confirm it', () => {
  useOutdatedAgents()

  it('focuses Cancel when the bulk update dialog opens', async () => {
    const dialog = await openUpdateDialog()
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(within(dialog).getByRole('button', { name: 'Update all' })).not.toHaveFocus()
  })

  it('a held (auto-repeating) Enter on the trigger never dispatches the update', async () => {
    vi.mocked(api.bulkUpdateAgents).mockResolvedValue({ sent: 3, skipped: 0, total: 3 })
    renderWithProviders()
    const trigger = await screen.findByRole('button', { name: /Update Agents \(3\)/ })
    trigger.focus()

    const user = userEvent.setup()
    await user.keyboard('{Enter>6/}')

    expect(api.bulkUpdateAgents, 'repeated Enter must never reach the Confirm button').not.toHaveBeenCalled()
  })

  it('a double click on the trigger never dispatches the update', async () => {
    renderWithProviders()
    const trigger = await screen.findByRole('button', { name: /Update Agents \(3\)/ })
    await userEvent.dblClick(trigger)
    expect(api.bulkUpdateAgents).not.toHaveBeenCalled()
  })
})

describe('[UIF AC-078] patch-all pending control', () => {
  usePendingPatches()

  it('disables the confirm control and shows "Dispatching…" while in flight', async () => {
    let finish: (v: { sent: number; queued: number; skipped: number; total: number }) => void = () => {}
    vi.mocked(api.bulkPatchDevices).mockReturnValue(new Promise(res => { finish = res }))
    const dialog = await openPatchReview()

    fireEvent.click(within(dialog).getByRole('button', { name: 'Update All Devices' }))

    const busy = await within(dialog).findByRole('button', { name: 'Dispatching…' })
    expect(busy).toBeDisabled()
    expect(busy).toHaveAttribute('aria-busy', 'true')
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeDisabled()
    await act(async () => finish({ sent: 1, queued: 0, skipped: 0, total: 1 }))
  })

  it('ignores Escape while the dispatch is in flight', async () => {
    let finish: (v: { sent: number; queued: number; skipped: number; total: number }) => void = () => {}
    vi.mocked(api.bulkPatchDevices).mockReturnValue(new Promise(res => { finish = res }))
    const dialog = await openPatchReview()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update All Devices' }))
    await within(dialog).findByRole('button', { name: 'Dispatching…' })
    await userEvent.keyboard('{Escape}')
    expect(screen.getByRole('dialog', { name: 'Pending OS Patches' })).toBeInTheDocument()
    await act(async () => finish({ sent: 1, queued: 0, skipped: 0, total: 1 }))
  })
})

describe('[UIF AC-079] patch-all success toast', () => {
  usePendingPatches()

  it('closes the modal and toasts the sent, queued, skipped and total counts', async () => {
    vi.mocked(api.bulkPatchDevices).mockResolvedValue({ sent: 3, queued: 1, skipped: 2, total: 6 })
    const dialog = await openPatchReview()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update All Devices' }))

    expect(
      await within(toastRegion()).findByText('3 sent, 1 queued, 2 skipped (6 total)'),
    ).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(api.bulkPatchDevices).toHaveBeenCalledWith('full')
  })
})

describe('[UIF AC-080] patch-all failure toast, modal stays open', () => {
  usePendingPatches()

  it('raises a persistent error toast with the message and keeps the modal open', async () => {
    vi.mocked(api.bulkPatchDevices).mockRejectedValue(new Error('HTTP 500: Internal Server Error'))
    const dialog = await openPatchReview()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update All Devices' }))

    const alert = await within(toastRegion()).findByRole('alert')
    expect(alert).toHaveTextContent("Couldn't dispatch patches")
    expect(alert).toHaveTextContent('HTTP 500: Internal Server Error')
    expect(screen.getByRole('dialog', { name: 'Pending OS Patches' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Update All Devices' })).toBeEnabled()
  })
})

describe('[UIF AC-111] Last Seen is a RelativeTime with the absolute time on hover', () => {
  it('renders a <time> whose title is the zone-labelled absolute time', async () => {
    renderWithProviders()
    await screen.findByText('node-1')
    const row = screen.getByText('node-1').closest('tr') as HTMLElement
    const time = row.querySelector('time') as HTMLElement
    expect(time).not.toBeNull()
    expect(time.getAttribute('title')).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} UTC$/)
    expect(time.textContent).toMatch(/^(just now|\d+m ago)$/)
  })
})

describe('[UIF AC-123] hostname not uppercased', () => {
  it('renders the hostname link in its reported case with no uppercase class', async () => {
    vi.mocked(useDevices).mockReturnValueOnce({
      data: [
        {
          id: 'dev-9',
          hostname: 'Web-01.lan',
          short_id: 'zzz999',
          arch: 'amd64',
          status: 'online',
          agent_version: '2.0.5',
          primary_ip: '10.0.0.9',
          tags: [],
          last_heartbeat: new Date().toISOString(),
        },
      ],
      isLoading: false,
      wsConnected: false,
    } as never)
    renderWithProviders()
    const link = await screen.findByRole('link', { name: 'Web-01.lan' })
    expect(link.textContent).toBe('Web-01.lan')
    expect(link.className).not.toContain('uppercase')
  })
})

// Added by QA Engineer
// Covers AC-078 (patch-all pending control). Regression test for QA-D1: isPending only disables the
// button after a re-render, so two activations queued before it (a busy main thread, a scripted
// double click) must still dispatch the fleet-wide OS patch once, via FleetOverview's in-flight ref.
describe('[UIF AC-078] QA: patch-all double activation dispatches once', () => {
  usePendingPatches()

  it('two clicks on "Update All Devices" before React re-renders call bulkPatchDevices once (QA-D1)', async () => {
    vi.mocked(api.bulkPatchDevices).mockReturnValue(new Promise(() => {}))
    const dialog = await openPatchReview()
    const confirm = within(dialog).getByRole('button', { name: 'Update All Devices' })

    await act(async () => {
      confirm.click()
      confirm.click()
    })

    expect(api.bulkPatchDevices).toHaveBeenCalledTimes(1)
  })
})

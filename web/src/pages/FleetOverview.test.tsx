import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import FleetOverview from './FleetOverview'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import { ToastProvider } from '../components/ui'

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

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
  it('[APP-WORKSPACE AC-007] identifies the complete machine collection as Devices with its registration action', async () => {
    renderWithProviders()
    expect(screen.getByRole('heading', { level: 1, name: 'Devices' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(screen.getByRole('button', { name: '+ Add Device' })).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: 'node-1' })).toHaveAttribute('href', '/devices/dev-1')
  })
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

describe('[AC-021] Reboot-class surfaced in patch review', () => {
  it('shows the reboot-class count for the device', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([
      { device_id: 'dev-1', hostname: 'node-1', pending_updates: 3, security_count: 1, reboot_class_count: 2, reboot_required: true },
    ])
    renderWithProviders()

    const reviewButton = await screen.findByText(/Review Patches/)
    fireEvent.click(reviewButton)

    expect(await screen.findByText('2 reboot-class')).toBeInTheDocument()
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

// Patch badge on the Devices rows, the needs-you filter and the Review Patches panel.
function badge(kind: 'auto' | 'auto_reboot_date' | 'auto_reboot_unknown' | 'needs_you', label: string) {
  return {
    kind,
    label,
    explanation: {
      automatic: 'No.',
      reboot: 'Not needed.',
      needs_you: [],
      needs_you_summary: 'Nothing.',
      as_of: '2026-10-19 02:00 UTC (just now)',
      notes: [],
    },
  }
}

const BADGE_ROW = {
  device_id: 'dev-1',
  hostname: 'node-1',
  pending_updates: 3,
  security_count: 1,
  reboot_class_count: 2,
  reboot_required: true,
  patch_badge: badge('auto_reboot_date', 'Auto (reboot on 2026-10-25)'),
}

function mockDevices(ids: string[]) {
  vi.mocked(useDevices).mockReturnValue({
    data: ids.map(id => ({
      id, hostname: `host-${id}`, short_id: id, arch: 'amd64', status: 'online',
      agent_version: '2.0.4', primary_ip: '10.0.0.1', tags: [], last_heartbeat: new Date().toISOString(),
    })),
    isLoading: false,
    wsConnected: false,
  } as never)
}

describe('patch badge on the Devices rows', () => {
  it('shows the server label in the device row', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([BADGE_ROW])
    renderWithProviders()

    const el = await screen.findByTestId('patch-badge')
    expect(el).toHaveTextContent('Auto (reboot on 2026-10-25)')
    expect(el.closest('tr')!.textContent).toContain('node-1')
  })

  it('requests one row per device', async () => {
    vi.mocked(api.getPatchStatus).mockClear()
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([BADGE_ROW])
    renderWithProviders()

    await screen.findByTestId('patch-badge')
    expect(api.getPatchStatus).toHaveBeenCalledWith(true)
  })

  it('keeps the pending-update count in the row', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([BADGE_ROW])
    renderWithProviders()

    await screen.findByTestId('patch-badge')
    const row = screen.getByText('node-1').closest('tr')!
    expect(row.textContent, 'the count of pending updates must still render in the row').toContain('3')
  })

  it('shows no badge for a device without a row', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([{ ...BADGE_ROW, device_id: 'some-other-device' }])
    renderWithProviders()

    await screen.findByText('node-1')
    expect(screen.queryByTestId('patch-badge')).not.toBeInTheDocument()
  })

  it('shows the same label in the Review Patches panel as in the row', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([BADGE_ROW])
    vi.mocked(api.getPatchStatusDetail).mockResolvedValueOnce([BADGE_ROW])
    renderWithProviders()

    const rowLabel = (await screen.findByTestId('patch-badge')).textContent
    fireEvent.click(await screen.findByText(/Review Patches/))
    await screen.findByText('2 reboot-class')
    const labels = screen.getAllByTestId('patch-badge').map(e => e.textContent)
    expect(labels).toEqual([rowLabel, rowLabel])
  })

  it('does not count fully patched devices as having OS updates', async () => {
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([
      { ...BADGE_ROW, pending_updates: 0, reboot_required: false, patch_badge: badge('auto', 'Auto') },
    ])
    renderWithProviders()

    await screen.findByTestId('patch-badge')
    expect(screen.queryByText(/Review Patches/)).not.toBeInTheDocument()
  })
})

describe('needs-you filter', () => {
  afterEach(() => {
    vi.mocked(useDevices).mockReset()
  })

  function rows() {
    return ['a', 'b', 'c', 'd', 'e'].map((id, i) => ({
      device_id: id, hostname: `host-${id}`, pending_updates: 0, security_count: 0,
      reboot_class_count: 0, reboot_required: false,
      patch_badge: i < 3 ? badge('needs_you', 'Needs you') : badge('auto', 'Auto'),
    }))
  }

  it('counts and lists exactly the hosts whose badge needs you', async () => {
    mockDevices(['a', 'b', 'c', 'd', 'e'])
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce(rows())
    renderWithProviders()

    const toggle = await screen.findByRole('button', { name: '3 hosts need you' })
    expect(toggle).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getAllByRole('row')).toHaveLength(1 + 5)

    fireEvent.click(toggle)

    expect(toggle).toHaveAttribute('aria-pressed', 'true')
    const names = screen.getAllByRole('link').map(l => l.textContent).filter(t => t?.startsWith('host-'))
    expect(names.sort()).toEqual(['host-a', 'host-b', 'host-c'])
  })

  it('shows all hosts again when toggled off', async () => {
    mockDevices(['a', 'b', 'c', 'd', 'e'])
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce(rows())
    renderWithProviders()

    const toggle = await screen.findByRole('button', { name: '3 hosts need you' })
    fireEvent.click(toggle)
    fireEvent.click(toggle)

    expect(toggle).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getAllByRole('row')).toHaveLength(1 + 5)
  })

  it('is absent when no host needs you', async () => {
    mockDevices(['a'])
    vi.mocked(api.getPatchStatus).mockResolvedValueOnce([rows()[4]].map(r => ({ ...r, device_id: 'a' })))
    renderWithProviders()

    await screen.findByTestId('patch-badge')
    expect(screen.queryByRole('button', { name: /need.* you/ })).not.toBeInTheDocument()
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

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import AgentManagement from './AgentManagement'
import { api } from '../../api/client'
import type { AutomationConfig, MaintenanceWindow } from '../../types/models'
import { ToastProvider } from '../../components/ui'

const saveAutomationConfig = vi.fn()
const getAutomationConfig = vi.fn()

vi.mock('../../api/settings', () => ({
  settingsApi: {
    getAutomationConfig: () => getAutomationConfig(),
    saveAutomationConfig: (cfg: AutomationConfig) => saveAutomationConfig(cfg),
  },
}))

vi.mock('../../api/client', () => ({
  api: {
    getAgentVersions: vi.fn().mockResolvedValue([]),
    getServerUpdate: vi.fn().mockResolvedValue({ latest_version: '2.0.5' }),
    getDevices: vi.fn().mockResolvedValue([]),
    bulkUpdateAgents: vi.fn(),
  },
}))

// Both maintenance windows enabled so the Docker card renders too — that lets us
// assert the reboot-class control is scoped to the OS-patch card only.
vi.mock('../../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

function win(overrides: Partial<MaintenanceWindow> = {}): MaintenanceWindow {
  return {
    mode: 'window',
    start_time: '02:00',
    end_time: '04:00',
    cooldown_minutes: 60,
    stagger_seconds: 0,
    reboot_class: 'off',
    ...overrides,
  }
}

function renderWithProviders() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <ToastProvider><AgentManagement /></ToastProvider>
    </QueryClientProvider>,
  )
}

// [AC-013] Reboot-class gating is enabled through the OS-patch maintenance-window
// policy control (FR-022 — server-side half of the two-sided opt-in).
describe('[AC-013] Reboot-class policy control on the OS-patch window', () => {
  beforeEach(() => {
    saveAutomationConfig.mockReset()
    getAutomationConfig.mockReset()
    getAutomationConfig.mockResolvedValue({ os_patch: win(), docker_update: win() } satisfies AutomationConfig)
  })

  it('renders the reboot-class control only on the OS-patch card', async () => {
    renderWithProviders()
    // Exactly one control — the Docker card must not render it.
    expect(await screen.findAllByText('Reboot-Class Packages')).toHaveLength(1)
    expect(screen.getByText('Patch normally')).toBeInTheDocument()
    expect(screen.getByText('Gate to window')).toBeInTheDocument()
  })

  it('saves reboot_class="gated" on the os_patch window when gating is selected', async () => {
    renderWithProviders()
    fireEvent.click(await screen.findByText('Gate to window'))
    fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => expect(saveAutomationConfig).toHaveBeenCalledTimes(1))
    const saved = saveAutomationConfig.mock.calls[0][0] as AutomationConfig
    expect(saved.os_patch.reboot_class).toBe('gated')
    // The policy must not leak onto the Docker window.
    expect(saved.docker_update.reboot_class).toBe('off')
  })

  it('surfaces the two-sided opt-in requirement in helper copy', async () => {
    renderWithProviders()
    await screen.findAllByText('Reboot-Class Packages')
    expect(screen.getByText('commands.hold_reboot_class')).toBeInTheDocument()
    expect(screen.getByText('commands.allow_reboot')).toBeInTheDocument()
  })
})

describe('[UIF AC-068] update all confirm and counts toast', () => {
  beforeEach(() => {
    getAutomationConfig.mockResolvedValue({ os_patch: win(), docker_update: win() } satisfies AutomationConfig)
    vi.mocked(api.getAgentVersions).mockResolvedValue([{ version: 'v2.60.0', count: 3 }])
    vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: 'v2.70.0' } as never)
    vi.mocked(api.bulkUpdateAgents).mockReset()
  })

  afterEach(() => {
    vi.mocked(api.getAgentVersions).mockResolvedValue([])
    vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.0.5' } as never)
  })

  async function openDialog() {
    renderWithProviders()
    fireEvent.click(await screen.findByRole('button', { name: 'Update All' }))
    return screen.getByRole('dialog', { name: 'Update agents' })
  }

  it('shows a primary dialog that states the device count and the version', async () => {
    const dialog = await openDialog()
    expect(dialog).toHaveTextContent('3 devices')
    expect(dialog).toHaveTextContent('v2.60.0')
    expect(within(dialog).getByRole('button', { name: 'Update all' })).toHaveAttribute('data-variant', 'primary')
  })

  it('focuses Cancel, never the fleet-wide confirm, when it opens (SEC-001)', async () => {
    const dialog = await openDialog()
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(within(dialog).getByRole('button', { name: 'Update all' })).not.toHaveFocus()
  })

  it('does not call the API before confirmation and not at all when cancelled', async () => {
    const dialog = await openDialog()
    expect(api.bulkUpdateAgents).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(api.bulkUpdateAgents).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('on confirm calls the API with the version and toasts the sent and skipped counts', async () => {
    vi.mocked(api.bulkUpdateAgents).mockResolvedValue({ sent: 2, skipped: 1, total: 3 })
    const dialog = await openDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))

    await waitFor(() => expect(api.bulkUpdateAgents).toHaveBeenCalledTimes(1))
    expect(vi.mocked(api.bulkUpdateAgents).mock.calls[0][0]).toBe('v2.60.0')
    const region = screen.getByRole('region', { name: 'Notifications' })
    expect(await within(region).findByText('Agent update dispatched')).toBeInTheDocument()
    expect(within(region).getByText('2 sent, 1 skipped (offline)')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('omits the skipped part of the toast when nothing was skipped', async () => {
    vi.mocked(api.bulkUpdateAgents).mockResolvedValue({ sent: 3, skipped: 0, total: 3 })
    const dialog = await openDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))
    const region = screen.getByRole('region', { name: 'Notifications' })
    expect(await within(region).findByText('3 sent')).toBeInTheDocument()
  })

  it('shows the pending label while in flight and leaves no inline result banner behind', async () => {
    let finish: (v: { sent: number; skipped: number; total: number }) => void = () => {}
    vi.mocked(api.bulkUpdateAgents).mockReturnValue(new Promise(res => { finish = res }))
    const dialog = await openDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))
    expect(await within(dialog).findByRole('button', { name: 'Updating…' })).toBeDisabled()
    finish({ sent: 1, skipped: 0, total: 1 })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.queryByText(/Update command sent to/)).toBeNull()
  })

  it('keeps the dialog open with an inline error when the update fails', async () => {
    vi.mocked(api.bulkUpdateAgents).mockRejectedValue(new Error('HTTP 503'))
    const dialog = await openDialog()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Update all' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('HTTP 503')
  })
})

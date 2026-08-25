import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import AgentManagement from './AgentManagement'
import type { AutomationConfig, MaintenanceWindow } from '../../types/models'

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
      <AgentManagement />
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

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceDetail from './DeviceDetail'
import { api } from '../api/client'
import type { UpdateInfo } from '../types/models'
import { renderWithProviders } from '../test/renderWithProviders'

vi.mock('../api/client', () => ({
  api: {
    getDevice: vi.fn(),
    getEvents: vi.fn(),
    getServerUpdate: vi.fn(),
    getPatchStatus: vi.fn(),
    getDeviceAlertRules: vi.fn(),
    getDeviceLogs: vi.fn(),
    getHeartbeatHistory: vi.fn(),
    getSecurityScore: vi.fn(),
    sendCommand: vi.fn(),
    deleteDevice: vi.fn(),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({ wsConnected: false }),
}))

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

const LABEL = 'Reboot automatically if the patch requires it'
const STALE_LABEL = 'Package lists out of date — refresh pending'

function updatesOf(over: Partial<UpdateInfo> = {}): UpdateInfo {
  return {
    package_manager: 'apt',
    total_installed: 1200,
    pending_updates: 3,
    pending_security_count: 0,
    pending_kernel_update: false,
    unattended_upgrades: false,
    allow_reboot: true,
    supports_reboot_if_required: true,
    ...over,
  } as UpdateInfo
}

function deviceWith(updates: UpdateInfo) {
  return {
    device: {
      id: 'dev-1',
      hostname: 'vermont',
      short_id: 'abc123',
      arch: 'amd64',
      status: 'online',
      agent_version: '2.0.4',
      primary_ip: '192.168.1.1',
      tags: [],
      docker_available: false,
      auto_patch: false,
      last_heartbeat: '2026-10-17T12:00:00Z',
      created_at: '2026-10-01T00:00:00Z',
      updated_at: '2026-10-17T00:00:00Z',
    },
    latest_telemetry: {
      id: 1,
      device_id: 'dev-1',
      timestamp: '2026-10-17T12:00:00Z',
      data: { updates },
    },
    agent_connected: true,
  }
}

function renderPage() {
  return renderWithProviders(<DeviceDetail />, {
    route: '/devices/dev-1',
    path: '/devices/:id',
    routes: [{ path: '/', element: <div>Fleet page</div> }],
  })
}

async function showUpdates(updates: UpdateInfo) {
  vi.mocked(api.getDevice).mockResolvedValue(deviceWith(updates) as never)
  renderPage()
  await screen.findByRole('heading', { name: 'vermont' })
}

const patchButton = () => screen.getByRole('button', { name: /^Patch \(\d+\)$/ })
const dialog = () => screen.getByRole('dialog')

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getEvents).mockResolvedValue([])
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.0.4' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getDeviceAlertRules).mockResolvedValue([])
  vi.mocked(api.getDeviceLogs).mockResolvedValue([])
  vi.mocked(api.getHeartbeatHistory).mockResolvedValue([])
  vi.mocked(api.getSecurityScore).mockResolvedValue({
    overall_score: 72, max_score: 100, grade: 'C', categories: [], evaluated_at: '2026-10-17T12:00:00Z',
  } as never)
  vi.mocked(api.sendCommand).mockResolvedValue({ id: 'c1', action: 'os_update', status: 'queued' } as never)
})

describe('[PATCH-FLOW-001 AC-007] the checkbox is unchecked every time the dialog opens', () => {
  it('shows the exact label unchecked, and an abandoned tick does not carry over', async () => {
    await showUpdates(updatesOf())

    await userEvent.click(patchButton())
    let box = within(dialog()).getByRole('checkbox', { name: LABEL })
    expect(box).not.toBeChecked()
    await userEvent.click(box)
    expect(box).toBeChecked()
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Cancel' }))

    await userEvent.click(patchButton())
    box = within(dialog()).getByRole('checkbox', { name: LABEL })
    expect(box).not.toBeChecked()
  })

  it('does not offer the checkbox for reboot, shutdown or agent update', async () => {
    await showUpdates(updatesOf())
    await userEvent.click(screen.getByRole('button', { name: 'Device power actions' }))
    await userEvent.click(screen.getByRole('button', { name: 'Reboot' }))
    expect(within(dialog()).queryByRole('checkbox')).not.toBeInTheDocument()
  })
})

describe('[PATCH-FLOW-001 AC-012] a ticked confirm', () => {
  it('sends reboot_if_required true with the mode', async () => {
    await showUpdates(updatesOf({ pending_reboot_class_count: 1 }))
    await userEvent.click(patchButton())
    await userEvent.click(within(dialog()).getByRole('checkbox', { name: LABEL }))

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))

    await waitFor(() => expect(api.sendCommand).toHaveBeenCalledTimes(1))
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'os_update', { mode: 'full', reboot_if_required: true })
  })
})

describe('[PATCH-FLOW-001 AC-013] an unticked confirm', () => {
  it('sends the mode only, exactly as before', async () => {
    await showUpdates(updatesOf())
    await userEvent.click(patchButton())

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))

    await waitFor(() => expect(api.sendCommand).toHaveBeenCalledTimes(1))
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'os_update', { mode: 'full' })
  })

  it('sends the mode only when the box is disabled by allow_reboot=false', async () => {
    await showUpdates(updatesOf({ allow_reboot: false }))
    await userEvent.click(patchButton())
    const box = within(dialog()).getByRole('checkbox', { name: LABEL })
    expect(box).toBeDisabled()

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))

    await waitFor(() => expect(api.sendCommand).toHaveBeenCalledTimes(1))
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'os_update', { mode: 'full' })
  })

  it('shows a server rejection inline and keeps the dialog open for a retry', async () => {
    vi.mocked(api.sendCommand).mockRejectedValueOnce(new Error('the agent must be updated to authorize a reboot with a patch'))
    await showUpdates(updatesOf())
    await userEvent.click(patchButton())
    await userEvent.click(within(dialog()).getByRole('checkbox', { name: LABEL }))

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))

    expect(await within(dialog()).findByRole('alert')).toHaveTextContent('the agent must be updated to authorize a reboot with a patch')
  })
})

describe('[PATCH-FLOW-001 AC-023] only a reboot is needed', () => {
  it('makes Reboot now the primary action, using the existing reboot confirmation', async () => {
    vi.mocked(api.sendCommand).mockResolvedValue({ id: 'c2', action: 'reboot', status: 'queued' } as never)
    await showUpdates(updatesOf({ pending_updates: 0, reboot_required: true }))

    expect(screen.queryByRole('button', { name: /^Patch/ })).not.toBeInTheDocument()
    const reboot = screen.getByRole('button', { name: 'Reboot now' })
    expect(reboot).toBeEnabled()

    await userEvent.click(reboot)
    expect(screen.getByRole('dialog', { name: 'Reboot Device' })).toBeInTheDocument()
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Reboot' }))

    await waitFor(() => expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'reboot', {}))
  })
})

describe('[PATCH-FLOW-001 AC-024] allow_reboot is off', () => {
  it('disables Reboot now and says the host must be rebooted outside rIOt', async () => {
    await showUpdates(updatesOf({ pending_updates: 0, reboot_required: true, allow_reboot: false }))

    const reboot = screen.getByRole('button', { name: 'Reboot now' })
    expect(reboot).toBeDisabled()
    const reason = screen.getByText(/Reboot this host outside rIOt/)
    expect(reason).toBeVisible()
    expect(reboot).toHaveAccessibleDescription(/outside rIOt/)
  })
})

describe('[PATCH-FLOW-001 AC-006] a pending kernel is not a reboot indicator', () => {
  it('shows no Reboot now for a host whose only signal is a pending reboot-class update', async () => {
    await showUpdates(updatesOf({ pending_updates: 0, pending_reboot_class_count: 1, reboot_required: false }))
    expect(screen.queryByRole('button', { name: 'Reboot now' })).not.toBeInTheDocument()
  })

  it('keeps Patch as the primary action while updates are pending and no reboot is required', async () => {
    await showUpdates(updatesOf({ pending_updates: 7, pending_reboot_class_count: 1 }))
    expect(screen.queryByRole('button', { name: 'Reboot now' })).not.toBeInTheDocument()
    expect(patchButton()).toHaveTextContent('Patch (7)')
  })

  it('keeps Patch primary when packages are pending even though a reboot is required', async () => {
    await showUpdates(updatesOf({ pending_updates: 4, reboot_required: true }))
    expect(screen.queryByRole('button', { name: 'Reboot now' })).not.toBeInTheDocument()
    await userEvent.click(patchButton())
    expect(within(dialog()).getByText(/A reboot is already required on this host/)).toBeVisible()
  })
})

describe('[PATCH-FLOW-001 AC-027] stale package lists', () => {
  it('replaces Patch (N) with the stale label, shows no number, and stays usable', async () => {
    await showUpdates(updatesOf({ pending_updates: 38, lists_freshness: 'stale' }))

    const button = screen.getByRole('button', { name: STALE_LABEL })
    expect(button).not.toHaveTextContent('38')
    expect(button).toHaveAttribute('title', 'Running Patch refreshes the package lists first.')
    expect(screen.queryByRole('button', { name: /^Patch \(/ })).not.toBeInTheDocument()

    await userEvent.click(button)
    expect(screen.getByRole('dialog', { name: 'Patch Device' })).toBeInTheDocument()
  })

  it('keeps the raw security count visible, qualified (SEC-007)', async () => {
    await showUpdates(updatesOf({ pending_updates: 38, pending_security_count: 3, lists_freshness: 'stale' }))
    expect(screen.getByText('3 security updates (lists out of date)')).toBeVisible()
  })

  it('shows no stale qualifier when the lists are fresh', async () => {
    await showUpdates(updatesOf({ pending_updates: 38, pending_security_count: 3, lists_freshness: 'fresh' }))
    expect(screen.queryByText(/lists out of date/)).not.toBeInTheDocument()
  })
})

describe('[PATCH-FLOW-001 AC-029] unknown freshness keeps today\'s display', () => {
  it.each([['absent', undefined], ['unknown', 'unknown' as const], ['bogus', 'bogus' as never]])(
    'shows Patch (N) when freshness is %s',
    async (_name, freshness) => {
      await showUpdates(updatesOf({ pending_updates: 5, lists_freshness: freshness }))
      expect(patchButton()).toHaveTextContent('Patch (5)')
    },
  )
})

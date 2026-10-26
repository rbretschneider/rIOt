import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceDetail from './DeviceDetail'
import { api } from '../api/client'
import type { ExtraUpdater, UpdateInfo } from '../types/models'
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

const BOX = 'Apply DietPi update in this run'

const dietpi: ExtraUpdater = {
  id: 'dietpi', status: 'update_available', installed_version: '9.20.1', available_version: '10.8.2',
  pending_count: 1, readiness: 'enabled', notices: [{ code: 'major_upgrade' }],
}

function updatesOf(over: Partial<UpdateInfo> = {}): UpdateInfo {
  return {
    package_manager: 'apt', total_installed: 1200, pending_updates: 3, pending_security_count: 0,
    pending_kernel_update: false, unattended_upgrades: false, allow_reboot: true,
    supports_reboot_if_required: true, extra_updaters: [dietpi], ...over,
  } as UpdateInfo
}

function deviceWith(updates: UpdateInfo) {
  return {
    device: {
      id: 'dev-1', hostname: 'pivpn', short_id: 'abc123', arch: 'arm64', status: 'online', agent_version: '2.0.4',
      primary_ip: '192.168.1.1', tags: [], docker_available: false, auto_patch: false,
      last_heartbeat: '2026-10-17T12:00:00Z', created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-17T00:00:00Z',
    },
    latest_telemetry: { id: 1, device_id: 'dev-1', timestamp: '2026-10-17T12:00:00Z', data: { updates } },
    agent_connected: true,
  }
}

async function show(updates: UpdateInfo) {
  vi.mocked(api.getDevice).mockResolvedValue(deviceWith(updates) as never)
  renderWithProviders(<DeviceDetail />, {
    route: '/devices/dev-1',
    path: '/devices/:id',
    routes: [{ path: '/', element: <div>Fleet page</div> }],
  })
  await screen.findByRole('heading', { name: 'pivpn' })
}

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

describe('[EXTRA-UPD-001 AC-039] device view and Patch button', () => {
  it('shows DietPi next to apt and the button "Patch (3 + DietPi)", enabled', async () => {
    await show(updatesOf())
    const button = screen.getByRole('button', { name: 'Patch (3 + DietPi)' })
    expect(button).toBeEnabled()
    const row = screen.getByTestId('extra-updater-dietpi')
    expect(row).toHaveTextContent('Installed 9.20.1')
    expect(row).toHaveTextContent('Available 10.8.2')
    expect(row).toHaveTextContent('Update available')
    expect(screen.getByText('Pending Updates (3)')).toBeInTheDocument()
  })

  it('shows "Patch (DietPi)" for a host with only DietPi pending and enabled', async () => {
    await show(updatesOf({ pending_updates: 0 }))
    expect(screen.getByRole('button', { name: 'Patch (DietPi)' })).toBeEnabled()
    expect(screen.getByTestId('extra-updater-dietpi')).toBeInTheDocument()
  })

  it('keeps the old button for a host without the field', async () => {
    await show(updatesOf({ extra_updaters: undefined }))
    expect(screen.getByRole('button', { name: 'Patch (3)' })).toBeEnabled()
    expect(screen.queryByTestId('extra-updaters')).not.toBeInTheDocument()
  })
})

describe('[EXTRA-UPD-001 AC-040] confirm dialog disclosure and exclusion', () => {
  it('ticks the box on each opening, even after an untick and a close', async () => {
    await show(updatesOf())
    await userEvent.click(screen.getByRole('button', { name: 'Patch (3 + DietPi)' }))
    let box = within(dialog()).getByRole('checkbox', { name: BOX })
    expect(box).toBeChecked()
    await userEvent.click(box)
    expect(box).not.toBeChecked()
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Cancel' }))

    await userEvent.click(screen.getByRole('button', { name: 'Patch (3 + DietPi)' }))
    box = within(dialog()).getByRole('checkbox', { name: BOX })
    expect(box).toBeChecked()
  })

  it('states the disclosures in the dialog', async () => {
    await show(updatesOf())
    await userEvent.click(screen.getByRole('button', { name: 'Patch (3 + DietPi)' }))
    const text = dialog().textContent ?? ''
    expect(text).toContain('DietPi 9.20.1 to 10.8.2')
    expect(text).toContain('after the apt packages')
    expect(text).toContain('40 minutes')
    expect(text).toContain('stops and restarts the services it manages')
    expect(text).toContain('apt-get autopurge')
    expect(text).toContain('Tick "Reboot automatically if the patch requires it"')
  })

  it('sends only the mode when the box stays ticked', async () => {
    await show(updatesOf())
    await userEvent.click(screen.getByRole('button', { name: 'Patch (3 + DietPi)' }))
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'os_update', { mode: 'full' })
  })

  it('sends the exclusion parameter when the box is unticked', async () => {
    await show(updatesOf())
    await userEvent.click(screen.getByRole('button', { name: 'Patch (3 + DietPi)' }))
    await userEvent.click(within(dialog()).getByRole('checkbox', { name: BOX }))
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'os_update', { mode: 'full', exclude_extra_updaters: true })
  })

  it('sends the exclusion with the reboot authorization when both are chosen', async () => {
    await show(updatesOf())
    await userEvent.click(screen.getByRole('button', { name: 'Patch (3 + DietPi)' }))
    await userEvent.click(within(dialog()).getByRole('checkbox', { name: BOX }))
    await userEvent.click(within(dialog()).getByRole('checkbox', { name: 'Reboot automatically if the patch requires it' }))
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'os_update', { mode: 'full', reboot_if_required: true, exclude_extra_updaters: true })
  })

  it('has no box and no exclusion for DietPi not enabled, and says it will not be applied', async () => {
    await show(updatesOf({ extra_updaters: [{ ...dietpi, readiness: 'root_side_missing' }] }))
    await userEvent.click(screen.getByRole('button', { name: 'Patch (3)' }))
    expect(within(dialog()).queryByRole('checkbox', { name: BOX })).not.toBeInTheDocument()
    expect(dialog()).toHaveTextContent('DietPi 9.20.1 to 10.8.2 will not be applied in this run')
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'os_update', { mode: 'full' })
  })

  it('says DietPi will not be applied when its status is unknown', async () => {
    await show(updatesOf({ extra_updaters: [{ id: 'dietpi', status: 'unknown', unknown_reason: 'check_disabled', pending_count: 0, readiness: 'enabled' }] }))
    await userEvent.click(screen.getByRole('button', { name: 'Patch (3)' }))
    expect(dialog()).toHaveTextContent('DietPi will not be applied in this run: its update status is unknown')
    expect(within(dialog()).queryByRole('checkbox', { name: BOX })).not.toBeInTheDocument()
  })
})

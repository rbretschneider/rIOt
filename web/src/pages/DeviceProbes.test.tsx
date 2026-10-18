import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceProbes from './DeviceProbes'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'

vi.mock('../api/client', () => ({
  api: {
    getDevice: vi.fn(),
    getDeviceProbes: vi.fn(),
    getDeviceProbeResults: vi.fn().mockResolvedValue([]),
    deleteDeviceProbe: vi.fn(),
    runDeviceProbe: vi.fn(),
    updateDeviceProbe: vi.fn(),
    createDeviceProbe: vi.fn(),
  },
}))

const probe = {
  id: 7,
  device_id: 'dev-1',
  name: 'nas-ping',
  type: 'ping',
  enabled: true,
  interval_seconds: 60,
  timeout_seconds: 10,
  config: { host: '10.0.0.5' },
  assertions: [],
  created_at: '2026-03-01T00:00:00Z',
  updated_at: '2026-03-01T00:00:00Z',
  latest_result: undefined,
  success_rate: 0.99,
  total_checks: 12,
}

function renderPage() {
  return renderWithProviders(<DeviceProbes />, { route: '/devices/dev-1/probes', path: '/devices/:id/probes' })
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getDevice).mockResolvedValue({ device: { id: 'dev-1', hostname: 'Web-01.lan' } } as never)
  vi.mocked(api.getDeviceProbes).mockResolvedValue([probe] as never)
  vi.mocked(api.getDeviceProbeResults).mockResolvedValue([])
  vi.mocked(api.deleteDeviceProbe).mockResolvedValue(undefined as never)
})

const rowDelete = () => screen.getByRole('button', { name: 'Delete' })

describe('[UIF AC-065] delete probe confirm names probe; cancel skips API', () => {
  it('opens a danger ConfirmDialog whose message contains the probe name', async () => {
    renderPage()
    await screen.findByText('nas-ping')
    await userEvent.click(rowDelete())

    const dialog = screen.getByRole('dialog', { name: 'Delete probe' })
    expect(dialog).toHaveTextContent('"nas-ping"')
    expect(within(dialog).getByRole('button', { name: 'Delete' })).toHaveAttribute('data-variant', 'danger')
  })

  it('does not call deleteDeviceProbe when the dialog is cancelled', async () => {
    renderPage()
    await screen.findByText('nas-ping')
    await userEvent.click(rowDelete())
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(api.deleteDeviceProbe).not.toHaveBeenCalled()
  })

  it('deletes the probe, closes the dialog and raises a "Probe deleted" toast on confirm', async () => {
    renderPage()
    await screen.findByText('nas-ping')
    await userEvent.click(rowDelete())
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(api.deleteDeviceProbe).toHaveBeenCalledWith('dev-1', 7))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(within(screen.getByRole('region', { name: 'Notifications' })).getByText('Probe deleted')).toBeInTheDocument()
  })

  it('keeps the dialog open with an inline error when the delete fails', async () => {
    vi.mocked(api.deleteDeviceProbe).mockRejectedValue(new Error('HTTP 500'))
    renderPage()
    await screen.findByText('nas-ping')
    await userEvent.click(rowDelete())
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))

    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('HTTP 500')
  })
})

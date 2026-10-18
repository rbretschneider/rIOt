import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceContainers from './DeviceContainers'
import { api } from '../api/client'
import { ApiError } from '../api/errors'
import { renderWithProviders } from '../test/renderWithProviders'

vi.mock('../api/client', () => ({
  api: {
    getDevice: vi.fn(),
    sendCommand: vi.fn(),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({ wsConnected: false }),
}))

function renderPage() {
  return renderWithProviders(<DeviceContainers />, { route: '/devices/dev-1/containers', path: '/devices/:id/containers' })
}

function deviceResponse(dockerAvailable: boolean) {
  return {
    device: { id: 'dev-1', hostname: 'Web-01.lan', short_id: 'abc', status: 'online' },
    latest_telemetry: { data: { docker: dockerAvailable ? { available: true, running: 0, total_containers: 0, containers: [] } : { available: false } } },
    agent_connected: true,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('[UIF AC-096] 404 shows Device not found', () => {
  it('renders "Device not found" with a link back to Fleet', async () => {
    vi.mocked(api.getDevice).mockRejectedValue(new ApiError(404, 'HTTP 404: Not Found'))
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Device not found' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to Fleet' })).toHaveAttribute('href', '/')
  })
})

describe('[UIF AC-097] non-404 shows error and retry', () => {
  it('renders "Couldn\'t load device", the message and Retry', async () => {
    vi.mocked(api.getDevice).mockRejectedValue(new ApiError(500, 'HTTP 500: Internal Server Error'))
    renderPage()
    expect(await screen.findByRole('heading', { name: "Couldn't load device" })).toBeInTheDocument()
    expect(screen.getByText('HTTP 500: Internal Server Error')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    expect(screen.queryByText('Device not found')).toBeNull()
  })

  it('Retry refetches the device', async () => {
    vi.mocked(api.getDevice).mockRejectedValue(new ApiError(502, 'HTTP 502: Bad Gateway'))
    renderPage()
    const retry = await screen.findByRole('button', { name: 'Retry' })
    vi.mocked(api.getDevice).mockResolvedValue(deviceResponse(true) as never)
    await userEvent.click(retry)
    expect(await screen.findByRole('heading', { name: 'Docker Containers' })).toBeInTheDocument()
  })

  it('treats a network error without a status as a load failure', async () => {
    vi.mocked(api.getDevice).mockRejectedValue(new TypeError('Failed to fetch'))
    renderPage()
    expect(await screen.findByRole('heading', { name: "Couldn't load device" })).toBeInTheDocument()
    expect(screen.getByText('Failed to fetch')).toBeInTheDocument()
  })
})

describe('loading', () => {
  it('shows a Skeleton status instead of "Loading..." text while the device loads', async () => {
    vi.mocked(api.getDevice).mockReturnValue(new Promise(() => {}))
    renderPage()
    expect(await screen.findByRole('status', { name: 'Loading' })).toHaveAttribute('aria-busy', 'true')
    expect(screen.queryByText('Loading...')).toBeNull()
  })
})

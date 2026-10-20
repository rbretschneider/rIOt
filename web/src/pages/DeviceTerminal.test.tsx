import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DeviceTerminal from './DeviceTerminal'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'

vi.mock('../api/client', () => ({ api: { getDevice: vi.fn() } }))
vi.mock('../components/Terminal', () => ({
  default: ({ deviceId, containerId }: { deviceId: string; containerId: string }) => (
    <div role="region" aria-label="Host shell" data-device={deviceId} data-target={containerId} />
  ),
}))

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getDevice).mockResolvedValue({ device: { id: 'dev-1', hostname: 'node-one' } } as never)
})

describe('[APP-WORKSPACE AC-007] host terminal resource context', () => {
  it('preserves the host shell target and canonical device return link', async () => {
    const user = userEvent.setup()
    renderWithProviders(<DeviceTerminal />, {
      route: '/devices/dev-1/terminal',
      path: '/devices/:id/terminal',
      routes: [{ path: '/devices/:id', element: <p>Device resource</p> }],
      showLocation: true,
    })
    expect(screen.getByRole('heading', { level: 1, name: 'Host Terminal' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    const shell = screen.getByRole('region', { name: 'Host shell' })
    expect(shell).toHaveAttribute('data-device', 'dev-1')
    expect(shell).toHaveAttribute('data-target', '_host')
    expect(shell.closest('[data-palette-exempt]')).not.toBeNull()
    const back = await screen.findByRole('link', { name: '← Back to node-one' })
    expect(back).toHaveAttribute('href', '/devices/dev-1')
    await user.click(back)
    expect(await screen.findByText('Device resource')).toBeInTheDocument()
    expect(screen.getByTestId('location')).toHaveTextContent('/devices/dev-1')
    expect(screen.queryByRole('region', { name: 'Host shell' })).not.toBeInTheDocument()
  })
})

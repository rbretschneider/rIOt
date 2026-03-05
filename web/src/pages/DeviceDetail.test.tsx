import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import DeviceDetail from './DeviceDetail'

vi.mock('../api/client', () => ({
  api: {
    getDevice: vi.fn().mockResolvedValue({
      device: {
        id: 'dev-1',
        hostname: 'test-host',
        short_id: 'abc123',
        arch: 'amd64',
        status: 'online',
        agent_version: '2.0.4',
        primary_ip: '192.168.1.1',
        tags: [],
        last_heartbeat: new Date().toISOString(),
      },
      latest_telemetry: null,
    }),
    getEvents: vi.fn().mockResolvedValue([]),
    getServerUpdate: vi.fn().mockResolvedValue({ latest_version: '2.0.5' }),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({ wsConnected: false }),
}))

function renderWithProviders() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/devices/dev-1']}>
        <Routes>
          <Route path="/devices/:id" element={<DeviceDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('DeviceDetail', () => {
  it('renders without crashing (smoke test)', async () => {
    renderWithProviders()
    expect(await screen.findByText('test-host')).toBeInTheDocument()
  })

  it('shows outdated indicator when agent version differs from latest', async () => {
    renderWithProviders()
    expect(await screen.findByText('(outdated)')).toBeInTheDocument()
  })

  it('shows Update Agent button as orange when outdated', async () => {
    renderWithProviders()
    const btn = await screen.findByRole('button', { name: 'Update Agent' })
    expect(btn).not.toBeDisabled()
    expect(btn.className).toContain('amber')
  })
})

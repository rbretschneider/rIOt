import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import DeviceDetail from './DeviceDetail'

const mockGetDevice = vi.fn()
vi.mock('../api/client', () => ({
  api: {
    getDevice: (...args: unknown[]) => mockGetDevice(...args),
    getEvents: vi.fn().mockResolvedValue([]),
    getServerUpdate: vi.fn().mockResolvedValue({ latest_version: '2.0.5' }),
    getDeviceAlertRules: vi.fn().mockResolvedValue([]),
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

const baseDevice = {
  device: {
    id: 'dev-1',
    hostname: 'test-host',
    short_id: 'abc123',
    arch: 'amd64',
    status: 'online' as const,
    agent_version: '2.0.4',
    primary_ip: '192.168.1.1',
    tags: [],
    docker_available: false,
    last_heartbeat: new Date().toISOString(),
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
  latest_telemetry: null as null | { id: number; device_id: string; timestamp: string; data: Record<string, unknown> },
  agent_connected: true,
}

describe('DeviceDetail', () => {
  beforeEach(() => {
    mockGetDevice.mockResolvedValue({ ...baseDevice })
  })

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

  it('renders Web Servers section when telemetry has web_servers data', async () => {
    mockGetDevice.mockResolvedValue({
      ...baseDevice,
      latest_telemetry: {
        id: 1,
        device_id: 'dev-1',
        timestamp: new Date().toISOString(),
        data: {
          web_servers: {
            servers: [{
              name: 'nginx',
              version: '1.24.0',
              status: 'running',
              config_valid: true,
              config_path: '/etc/nginx/nginx.conf',
              sites: [{
                server_names: ['example.com'],
                listen: ['443 ssl'],
                proxy_pass: 'http://127.0.0.1:8080',
                ssl_cert: '/etc/ssl/cert.pem',
                enabled: true,
              }],
              certs: [{
                file_path: '/etc/ssl/cert.pem',
                subject: 'example.com',
                issuer: "Let's Encrypt",
                sans: ['example.com'],
                not_after: new Date(Date.now() + 60 * 86400000).toISOString(),
                days_left: 60,
                key_type: 'ECDSA',
              }],
            }],
          },
        },
      },
    })
    renderWithProviders()
    expect(await screen.findByText('Web Servers')).toBeInTheDocument()
    expect(await screen.findByText('nginx')).toBeInTheDocument()
    expect(await screen.findByText('1.24.0')).toBeInTheDocument()
    // example.com appears in sites table, certs table, and SANs
    const exampleComs = await screen.findAllByText('example.com')
    expect(exampleComs.length).toBeGreaterThanOrEqual(1)
  })

  it('does not render Web Servers section when no web_servers data', async () => {
    mockGetDevice.mockResolvedValue({
      ...baseDevice,
      latest_telemetry: {
        id: 1,
        device_id: 'dev-1',
        timestamp: new Date().toISOString(),
        data: {},
      },
    })
    renderWithProviders()
    expect(await screen.findByText('test-host')).toBeInTheDocument()
    expect(screen.queryByText('Web Servers')).not.toBeInTheDocument()
  })
})

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
    getDeviceCommands: vi.fn().mockResolvedValue([]),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({ wsConnected: false }),
}))

vi.mock('../hooks/useWebSocket', () => ({
  useWebSocket: () => ({ connected: false }),
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

  it('renders Hardware Details section when telemetry has hardware data', async () => {
    mockGetDevice.mockResolvedValue({
      ...baseDevice,
      latest_telemetry: {
        id: 1,
        device_id: 'dev-1',
        timestamp: new Date().toISOString(),
        data: {
          hardware: {
            pci_devices: [{
              slot: '0000:00:02.0',
              vendor_id: '8086',
              device_id: '9a49',
              vendor: 'Intel Corporation',
              device: 'TigerLake-LP GT2 [Iris Xe Graphics]',
              description: 'Intel Corporation TigerLake-LP GT2 [Iris Xe Graphics]',
              class_code: '030000',
              class_name: 'VGA Controller',
              driver: 'i915',
            }],
            disk_drives: [{
              name: 'nvme0n1',
              model: 'Samsung SSD 980 PRO 1TB',
              serial: 'S5XXXX',
              size_bytes: 1000204886016,
              size_gb: 931.5,
              type: 'NVMe',
            }],
            gpus: [{
              vendor: 'Intel Corporation',
              model: 'TigerLake-LP GT2 [Iris Xe Graphics]',
              pci_slot: '0000:00:02.0',
              driver: 'i915',
              description: 'Intel Corporation TigerLake-LP GT2 [Iris Xe Graphics]',
            }],
          },
        },
      },
    })
    renderWithProviders()
    // Check section title contains "Hardware Details"
    const heading = await screen.findByText(/Hardware Details/)
    expect(heading).toBeInTheDocument()
    // Check GPU sub-heading appears
    expect(await screen.findByText(/GPUs/)).toBeInTheDocument()
    // Check disk drives sub-heading appears
    expect(await screen.findByText(/Disk Drives/)).toBeInTheDocument()
  })

  it('does not render Hardware Details section when no hardware data', async () => {
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
    expect(screen.queryByText(/Hardware Details/)).not.toBeInTheDocument()
  })
})

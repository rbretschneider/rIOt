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

  // Added by QA Engineer
  // Covers AC-003
  describe('[AC-003] Pool filesystems render in a distinct subsection', () => {
    it('renders Storage Pools subsection when at least one filesystem has is_pool true', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            disks: {
              filesystems: [
                {
                  mount_point: '/mnt/pool',
                  device: '/dev/md0',
                  fs_type: 'fuse.mergerfs',
                  total_gb: 14000,
                  used_gb: 7000,
                  free_gb: 7000,
                  usage_percent: 50,
                  is_network_mount: false,
                  is_pool: true,
                },
                {
                  mount_point: '/',
                  device: '/dev/sda1',
                  fs_type: 'ext4',
                  total_gb: 100,
                  used_gb: 40,
                  free_gb: 60,
                  usage_percent: 40,
                  is_network_mount: false,
                },
              ],
            },
          },
        },
      })
      renderWithProviders()
      expect(await screen.findByText('Storage Pools')).toBeInTheDocument()
    })

    it('displays mount point and filesystem type in the pool card', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            disks: {
              filesystems: [
                {
                  mount_point: '/mnt/pool',
                  device: '/dev/md0',
                  fs_type: 'fuse.mergerfs',
                  total_gb: 14000,
                  used_gb: 7000,
                  free_gb: 7000,
                  usage_percent: 50,
                  is_network_mount: false,
                  is_pool: true,
                },
              ],
            },
          },
        },
      })
      renderWithProviders()
      expect(await screen.findByText('/mnt/pool')).toBeInTheDocument()
      expect(await screen.findByText('fuse.mergerfs')).toBeInTheDocument()
    })

    it('excludes pool filesystem from the regular filesystem table', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            disks: {
              filesystems: [
                {
                  mount_point: '/mnt/pool',
                  device: '/dev/md0',
                  fs_type: 'fuse.mergerfs',
                  total_gb: 14000,
                  used_gb: 7000,
                  free_gb: 7000,
                  usage_percent: 50,
                  is_network_mount: false,
                  is_pool: true,
                },
                {
                  mount_point: '/',
                  device: '/dev/sda1',
                  fs_type: 'ext4',
                  total_gb: 100,
                  used_gb: 40,
                  free_gb: 60,
                  usage_percent: 40,
                  is_network_mount: false,
                },
              ],
            },
          },
        },
      })
      renderWithProviders()
      // Regular table should exist (contains '/')
      expect(await screen.findByText('/')).toBeInTheDocument()
      // '/mnt/pool' appears exactly once — in the pool card, NOT also in the table
      const poolMountElements = await screen.findAllByText('/mnt/pool')
      expect(poolMountElements).toHaveLength(1)
    })
  })

  // Added by QA Engineer
  // Covers AC-004
  describe('[AC-004] No pool subsection when no pools exist', () => {
    it('does not render Storage Pools subsection when no filesystems have is_pool true', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            disks: {
              filesystems: [
                {
                  mount_point: '/',
                  device: '/dev/sda1',
                  fs_type: 'ext4',
                  total_gb: 100,
                  used_gb: 40,
                  free_gb: 60,
                  usage_percent: 40,
                  is_network_mount: false,
                },
                {
                  mount_point: '/boot',
                  device: '/dev/sda2',
                  fs_type: 'vfat',
                  total_gb: 0.5,
                  used_gb: 0.1,
                  free_gb: 0.4,
                  usage_percent: 20,
                  is_network_mount: false,
                },
              ],
            },
          },
        },
      })
      renderWithProviders()
      // Wait for data to load
      expect(await screen.findByText('/')).toBeInTheDocument()
      expect(screen.queryByText('Storage Pools')).not.toBeInTheDocument()
    })

    it('renders regular filesystem table when no pools exist', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            disks: {
              filesystems: [
                {
                  mount_point: '/',
                  device: '/dev/sda1',
                  fs_type: 'ext4',
                  total_gb: 100,
                  used_gb: 40,
                  free_gb: 60,
                  usage_percent: 40,
                  is_network_mount: false,
                },
              ],
            },
          },
        },
      })
      renderWithProviders()
      expect(await screen.findByText('/')).toBeInTheDocument()
      expect(await screen.findByText('ext4')).toBeInTheDocument()
    })
  })

  // Added by QA Engineer
  // Covers AC-014
  describe('[AC-014] Pool card displays the device path', () => {
    it('renders the device path on the pool card', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            disks: {
              filesystems: [
                {
                  mount_point: '/mnt/storage',
                  device: '/dev/md0',
                  fs_type: 'ext4',
                  total_gb: 4000,
                  used_gb: 1000,
                  free_gb: 3000,
                  usage_percent: 25,
                  is_network_mount: false,
                  is_pool: true,
                },
              ],
            },
          },
        },
      })
      renderWithProviders()
      // Wait for pool card header to confirm section rendered
      expect(await screen.findByText('Storage Pools')).toBeInTheDocument()
      // AC-014: device path must be visible on the pool card
      expect(await screen.findByText('/dev/md0')).toBeInTheDocument()
    })

    it('renders device path for LVM pool card', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            disks: {
              filesystems: [
                {
                  mount_point: '/mnt/data',
                  device: '/dev/mapper/vg0-data',
                  fs_type: 'xfs',
                  total_gb: 2000,
                  used_gb: 500,
                  free_gb: 1500,
                  usage_percent: 25,
                  is_network_mount: false,
                  is_pool: true,
                },
              ],
            },
          },
        },
      })
      renderWithProviders()
      expect(await screen.findByText('Storage Pools')).toBeInTheDocument()
      // AC-014: LVM device path must be visible on the pool card
      expect(await screen.findByText('/dev/mapper/vg0-data')).toBeInTheDocument()
    })
  })

  // GPU-001 tests

  // [AC-007] GPU telemetry card is visible with correct fields when data is present.
  describe('[AC-007] GPU telemetry card renders when gpu_telemetry data is present', () => {
    it('renders GPU Telemetry section with GPU name, temperature, utilization, memory, fan, power', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            gpu_telemetry: {
              gpus: [
                {
                  index: 0,
                  name: 'NVIDIA GeForce RTX 3090',
                  uuid: 'GPU-aaaa-bbbb-cccc-dddd',
                  pci_bus_id: '00000000:01:00.0',
                  temperature_c: 72,
                  fan_speed_percent: 65,
                  utilization_pct: 85,
                  mem_util_pct: 42,
                  mem_used_mib: 10240,
                  mem_total_mib: 24576,
                  power_draw_w: 285.5,
                  power_limit_w: 350,
                },
              ],
            },
          },
        },
      })

      renderWithProviders()

      // Section header
      expect(await screen.findByText(/GPU Telemetry/)).toBeInTheDocument()
      // GPU name
      expect(await screen.findByText('NVIDIA GeForce RTX 3090')).toBeInTheDocument()
      // Temperature
      expect(await screen.findByText('72°C')).toBeInTheDocument()
      // Fan speed
      expect(await screen.findByText('65%')).toBeInTheDocument()
      // Power draw / limit
      expect(await screen.findByText(/285\.5.*\/.*350.*W/)).toBeInTheDocument()
      // Utilization gauge label
      expect(await screen.findByText(/GPU Utilization/)).toBeInTheDocument()
      // Memory gauge label
      expect(await screen.findByText(/Memory.*10240.*\/.*24576/)).toBeInTheDocument()
    })
  })

  // [AC-008] GPU card must not appear when no gpu_telemetry data exists.
  describe('[AC-008] GPU telemetry card is absent when no gpu_telemetry data', () => {
    it('does not render GPU Telemetry section when telemetry has no gpu_telemetry', async () => {
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
      expect(screen.queryByText(/GPU Telemetry/)).not.toBeInTheDocument()
    })

    it('does not render GPU Telemetry section when gpu_telemetry.gpus is empty', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            gpu_telemetry: { gpus: [] },
          },
        },
      })

      renderWithProviders()

      expect(await screen.findByText('test-host')).toBeInTheDocument()
      expect(screen.queryByText(/GPU Telemetry/)).not.toBeInTheDocument()
    })
  })

  // [AC-010] Temperature color-coding thresholds.
  describe('[AC-010] GPU temperature color-coding', () => {
    async function renderWithTemp(temp: number) {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            gpu_telemetry: {
              gpus: [{
                index: 0,
                name: 'Test GPU',
                uuid: 'GPU-test',
                pci_bus_id: '00:01.0',
                temperature_c: temp,
              }],
            },
          },
        },
      })
      renderWithProviders()
      return await screen.findByText(`${temp}°C`)
    }

    it('renders temperature in green (text-emerald-400) when below 60°C', async () => {
      const el = await renderWithTemp(45)
      expect(el.className).toContain('emerald')
    })

    it('renders temperature in amber (text-amber-400) when between 60°C and 79°C', async () => {
      const el = await renderWithTemp(75)
      expect(el.className).toContain('amber')
    })

    it('renders temperature in orange (text-orange-400) when between 80°C and 89°C', async () => {
      const el = await renderWithTemp(85)
      expect(el.className).toContain('orange')
    })

    it('renders temperature in red (text-red-400) when 90°C or above', async () => {
      const el = await renderWithTemp(92)
      expect(el.className).toContain('red')
    })
  })

  // [AC-014] One-click alert button opens CreateAlertDialog pre-filled with gpu_temp.
  describe('[AC-014] GPU Temp Alert button opens alert dialog pre-filled with gpu_temp', () => {
    it('clicking GPU Temp Alert button shows metric label "GPU Temperature" in dialog', async () => {
      mockGetDevice.mockResolvedValue({
        ...baseDevice,
        latest_telemetry: {
          id: 1,
          device_id: 'dev-1',
          timestamp: new Date().toISOString(),
          data: {
            gpu_telemetry: {
              gpus: [{
                index: 0,
                name: 'NVIDIA RTX 3090',
                uuid: 'GPU-test',
                pci_bus_id: '00:01.0',
                temperature_c: 72,
              }],
            },
          },
        },
      })

      const { getByRole } = renderWithProviders()

      // Wait for the GPU section to render
      expect(await screen.findByText(/GPU Telemetry/)).toBeInTheDocument()

      // Click the GPU Temp Alert button
      const alertBtn = getByRole('button', { name: /GPU Temp Alert/i })
      alertBtn.click()

      // CreateAlertDialog should open showing the GPU Temperature label
      expect(await screen.findByText('GPU Temperature')).toBeInTheDocument()
      // The dialog should also show the hint text
      expect(await screen.findByText(/GPU temperature in °C/)).toBeInTheDocument()
    })
  })
})

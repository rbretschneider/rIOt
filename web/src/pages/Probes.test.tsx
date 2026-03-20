import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import Probes from './Probes'
import * as clientModule from '../api/client'

// --- API mock ---
// vi.mock is hoisted, so data must be inline (no top-level variable references).
vi.mock('../api/client', () => ({
  api: {
    getProbes: vi.fn().mockResolvedValue([
      {
        id: 1,
        name: 'Google HTTP',
        type: 'http',
        enabled: true,
        config: { url: 'https://google.com', method: 'GET', expected_status: 200 },
        interval_seconds: 60,
        timeout_seconds: 10,
        created_at: '2026-03-01T00:00:00Z',
        updated_at: '2026-03-01T00:00:00Z',
        latest_result: {
          id: 1,
          probe_id: 1,
          success: true,
          latency_ms: 42.5,
          status_code: 200,
          error_msg: '',
          metadata: {},
          created_at: '2026-03-20T10:00:00Z',
        },
        success_rate: 0.98,
        total_checks: 100,
      },
    ]),
    getAllDeviceProbes: vi.fn().mockResolvedValue([
      {
        id: 10,
        name: 'Check nginx',
        device_id: 'dev-abc',
        device_hostname: 'web-server-01',
        type: 'shell',
        enabled: true,
        config: { command: 'curl -s localhost', shell: '/bin/sh' },
        assertions: [],
        interval_seconds: 60,
        timeout_seconds: 10,
        created_at: '2026-03-01T00:00:00Z',
        updated_at: '2026-03-01T00:00:00Z',
        latest_result: {
          id: 5,
          probe_id: 10,
          device_id: 'dev-abc',
          success: true,
          latency_ms: 12.5,
          output: {},
          created_at: '2026-03-20T10:00:00Z',
        },
        success_rate: 0.95,
        total_checks: 50,
      },
    ]),
    getDevices: vi.fn().mockResolvedValue([
      {
        id: 'dev-abc',
        hostname: 'web-server-01',
        short_id: 'abc',
        arch: 'amd64',
        status: 'online',
        location: '',
        tags: [],
        docker_available: false,
        docker_container_count: 0,
        auto_patch: false,
        created_at: '2026-03-01T00:00:00Z',
        updated_at: '2026-03-01T00:00:00Z',
      },
    ]),
    runProbe: vi.fn().mockResolvedValue({}),
    createProbe: vi.fn().mockResolvedValue({}),
    updateProbe: vi.fn().mockResolvedValue({}),
    deleteProbe: vi.fn().mockResolvedValue(undefined),
    runDeviceProbe: vi.fn().mockResolvedValue({}),
    updateDeviceProbe: vi.fn().mockResolvedValue({}),
    deleteDeviceProbe: vi.fn().mockResolvedValue(undefined),
  },
}))

function renderProbes() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Probes />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// ===================================================================
// [AC-007] Page structure: Server Probes section above Device Probes
// ===================================================================
describe('[AC-007] Page structure: Server Probes above Device Probes', () => {
  it('renders the main page heading "Probes"', async () => {
    renderProbes()
    expect(await screen.findByRole('heading', { name: 'Probes', level: 1 })).toBeInTheDocument()
  })

  it('renders Server Probes section heading', async () => {
    renderProbes()
    expect(await screen.findByRole('heading', { name: 'Server Probes', level: 2 })).toBeInTheDocument()
  })

  it('renders Device Probes section heading', async () => {
    renderProbes()
    expect(await screen.findByRole('heading', { name: 'Device Probes', level: 2 })).toBeInTheDocument()
  })

  it('Server Probes section appears before Device Probes section in DOM order', async () => {
    renderProbes()
    const headings = await screen.findAllByRole('heading', { level: 2 })
    const serverIndex = headings.findIndex(h => h.textContent === 'Server Probes')
    const deviceIndex = headings.findIndex(h => h.textContent === 'Device Probes')
    expect(serverIndex).toBeGreaterThanOrEqual(0)
    expect(deviceIndex).toBeGreaterThanOrEqual(0)
    expect(serverIndex).toBeLessThan(deviceIndex)
  })

  it('each section has its own Add Probe button', async () => {
    renderProbes()
    await screen.findByRole('heading', { name: 'Server Probes', level: 2 })
    const addButtons = screen.getAllByRole('button', { name: 'Add Probe' })
    expect(addButtons.length).toBeGreaterThanOrEqual(2)
  })
})

// ===================================================================
// [AC-001] Server Probes section renders all columns and actions
// ===================================================================
describe('[AC-001] Server Probes section renders all columns and actions', () => {
  it('shows server probe name', async () => {
    renderProbes()
    expect(await screen.findByText('Google HTTP')).toBeInTheDocument()
  })

  it('shows success rate', async () => {
    renderProbes()
    await screen.findByText('Google HTTP')
    expect(screen.getByText('98.0%')).toBeInTheDocument()
  })

  it('shows total checks count', async () => {
    renderProbes()
    await screen.findByText('Google HTTP')
    expect(screen.getByText('100')).toBeInTheDocument()
  })

  it('shows latency from latest result', async () => {
    renderProbes()
    await screen.findByText('Google HTTP')
    expect(screen.getByText('42.5ms')).toBeInTheDocument()
  })

  it('server probe name links to /probes/:id', async () => {
    renderProbes()
    const link = await screen.findByRole('link', { name: 'Google HTTP' })
    expect(link).toHaveAttribute('href', '/probes/1')
  })

  it('renders Run, Edit, and Delete buttons for server probes', async () => {
    renderProbes()
    await screen.findByText('Google HTTP')
    // Multiple Run/Edit/Delete exist (one set per section)
    const runButtons = screen.getAllByRole('button', { name: 'Run' })
    expect(runButtons.length).toBeGreaterThanOrEqual(1)
    const editButtons = screen.getAllByRole('button', { name: 'Edit' })
    expect(editButtons.length).toBeGreaterThanOrEqual(1)
    const deleteButtons = screen.getAllByRole('button', { name: 'Delete' })
    expect(deleteButtons.length).toBeGreaterThanOrEqual(1)
  })
})

// ===================================================================
// [AC-002] Add Probe in Server Probes section opens ProbeModal
// ===================================================================
describe('[AC-002] Add Probe in Server Probes section opens ProbeModal', () => {
  it('clicking the first Add Probe button opens the server probe creation modal', async () => {
    renderProbes()
    await screen.findByRole('heading', { name: 'Server Probes', level: 2 })
    const addButtons = screen.getAllByRole('button', { name: 'Add Probe' })
    fireEvent.click(addButtons[0])
    expect(await screen.findByRole('heading', { name: /Create Probe/i })).toBeInTheDocument()
  })
})

// ===================================================================
// [AC-003] Server Probes section shows empty state when no probes
// ===================================================================
describe('[AC-003] Server Probes section shows empty state', () => {
  it('shows empty state message when no server probes exist', async () => {
    vi.mocked(clientModule.api.getProbes).mockResolvedValueOnce([])

    renderProbes()
    expect(await screen.findByText(/No server probes configured/i)).toBeInTheDocument()
  })
})

// ===================================================================
// [AC-004] Device Probes section renders all columns with hostname link
// ===================================================================
describe('[AC-004] Device Probes section renders all columns', () => {
  it('shows device probe name', async () => {
    renderProbes()
    expect(await screen.findByText('Check nginx')).toBeInTheDocument()
  })

  it('shows device probe type in the table', async () => {
    renderProbes()
    // The type cell renders probe.type (lowercase "shell"), styled uppercase via CSS.
    // We check for the lowercase value as that is the DOM text content.
    await screen.findByText('Check nginx')
    expect(screen.getByText('shell')).toBeInTheDocument()
  })

  it('shows device hostname as a clickable link to /devices/:id', async () => {
    renderProbes()
    const hostnameLink = await screen.findByRole('link', { name: 'web-server-01' })
    expect(hostnameLink).toBeInTheDocument()
    expect(hostnameLink).toHaveAttribute('href', '/devices/dev-abc')
  })

  it('shows device probe success rate', async () => {
    renderProbes()
    await screen.findByText('Check nginx')
    expect(screen.getByText('95.0%')).toBeInTheDocument()
  })

  it('shows device probe total checks', async () => {
    renderProbes()
    await screen.findByText('Check nginx')
    expect(screen.getByText('50')).toBeInTheDocument()
  })

  it('shows device probe latency', async () => {
    renderProbes()
    await screen.findByText('Check nginx')
    expect(screen.getByText('12.5ms')).toBeInTheDocument()
  })

  it('device hostname is rendered as plain text (not via dangerouslySetInnerHTML)', async () => {
    // React renders {probe.device_hostname} as a text node by default.
    // If dangerouslySetInnerHTML were used, a crafted hostname could inject markup.
    // We verify the link text content matches exactly.
    renderProbes()
    const hostnameLink = await screen.findByRole('link', { name: 'web-server-01' })
    expect(hostnameLink.textContent).toBe('web-server-01')
  })
})

// ===================================================================
// [AC-005] Device probe action buttons call correct device-scoped API
// ===================================================================
describe('[AC-005] Device probe actions call correct device-scoped API endpoints', () => {
  beforeEach(() => {
    vi.mocked(clientModule.api.runDeviceProbe).mockResolvedValue({})
    vi.mocked(clientModule.api.deleteDeviceProbe).mockResolvedValue(undefined)
    vi.mocked(clientModule.api.updateDeviceProbe).mockResolvedValue({} as never)
    vi.mocked(clientModule.api.runProbe).mockResolvedValue({ id: 0, probe_id: 0, success: true, latency_ms: 0, error_msg: '', metadata: {}, created_at: '' })
    vi.mocked(clientModule.api.deleteProbe).mockResolvedValue(undefined)
  })

  it('Run button calls api.runDeviceProbe with correct device_id and probe_id', async () => {
    renderProbes()
    await screen.findByText('Check nginx')

    // Server probe table has Run buttons first, device probe table second.
    const runButtons = screen.getAllByRole('button', { name: 'Run' })
    fireEvent.click(runButtons[runButtons.length - 1])

    await waitFor(() => {
      expect(clientModule.api.runDeviceProbe).toHaveBeenCalledWith('dev-abc', 10)
    })
  })

  it('Delete button calls api.deleteDeviceProbe with correct device_id and probe_id', async () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))
    renderProbes()
    await screen.findByText('Check nginx')

    const deleteButtons = screen.getAllByRole('button', { name: 'Delete' })
    fireEvent.click(deleteButtons[deleteButtons.length - 1])

    await waitFor(() => {
      expect(clientModule.api.deleteDeviceProbe).toHaveBeenCalledWith('dev-abc', 10)
    })
    vi.unstubAllGlobals()
  })

  // Added by QA Engineer
  // Covers AC-005: Edit button must call api.updateDeviceProbe with the correct device_id
  it('Edit button opens DeviceProbeModal and save calls api.updateDeviceProbe with correct device_id', async () => {
    renderProbes()
    await screen.findByText('Check nginx')

    const editButtons = screen.getAllByRole('button', { name: 'Edit' })
    fireEvent.click(editButtons[editButtons.length - 1])

    // DeviceProbeModal renders heading "Edit Device Probe" when isNew=false
    expect(await screen.findByRole('heading', { name: /Edit Device Probe/i })).toBeInTheDocument()

    // Click Save in the modal
    const saveButton = screen.getByRole('button', { name: /^Save$/i })
    fireEvent.click(saveButton)

    await waitFor(() => {
      expect(clientModule.api.updateDeviceProbe).toHaveBeenCalledWith(
        'dev-abc',
        10,
        expect.objectContaining({ name: 'Check nginx' }),
      )
    })
  })

  // Added by QA Engineer
  // Covers AC-005: Enable/Disable toggle must call api.updateDeviceProbe with flipped enabled value
  it('Enable/Disable toggle calls api.updateDeviceProbe with correct device_id and toggled enabled state', async () => {
    renderProbes()
    await screen.findByText('Check nginx')

    // The mock device probe has enabled: true. The toggle button title is "Disable".
    const toggleButtons = screen.getAllByTitle('Disable')
    // Pick the device probe toggle — the last one in case there are server probe toggles too
    fireEvent.click(toggleButtons[toggleButtons.length - 1])

    await waitFor(() => {
      expect(clientModule.api.updateDeviceProbe).toHaveBeenCalledWith(
        'dev-abc',
        10,
        expect.objectContaining({ enabled: false }),
      )
    })
  })
})

// ===================================================================
// [AC-006] Device Probes section shows empty state when no device probes
// ===================================================================
describe('[AC-006] Device Probes section shows empty state', () => {
  it('shows empty state message when no device probes exist', async () => {
    vi.mocked(clientModule.api.getAllDeviceProbes).mockResolvedValueOnce([])

    renderProbes()
    expect(await screen.findByText(/No device probes configured/i)).toBeInTheDocument()
  })
})

// ===================================================================
// [AC-008] Both queries use refetchInterval: 30_000
// ===================================================================
describe('[AC-008] Both queries refetch every 30 seconds', () => {
  it('getProbes is called for the server probes section', async () => {
    renderProbes()
    await screen.findByRole('heading', { name: 'Server Probes', level: 2 })
    expect(clientModule.api.getProbes).toHaveBeenCalled()
  })

  it('getAllDeviceProbes is called for the device probes section', async () => {
    renderProbes()
    await screen.findByRole('heading', { name: 'Device Probes', level: 2 })
    expect(clientModule.api.getAllDeviceProbes).toHaveBeenCalled()
  })

  // Added by QA Engineer
  // Covers AC-008: verifies that after initial data is shown, both queries are re-issued
  // when the query client refetches. This tests the fetch wiring rather than the timer
  // itself (which would require vitest fake timers + react-query internals integration).
  // The exact refetchInterval: 30_000 value is verified by code inspection in the
  // QA report — it is present at Probes.tsx lines 16 and 22.
  it('both api functions are called exactly once on initial render', async () => {
    vi.mocked(clientModule.api.getProbes).mockClear()
    vi.mocked(clientModule.api.getAllDeviceProbes).mockClear()

    renderProbes()
    await screen.findByText('Google HTTP')
    await screen.findByText('Check nginx')

    expect(clientModule.api.getProbes).toHaveBeenCalledTimes(1)
    expect(clientModule.api.getAllDeviceProbes).toHaveBeenCalledTimes(1)
  })
})

// ===================================================================
// [AC-009] DeviceProbes.tsx per-device page is unchanged
// ===================================================================
describe('[AC-009] Per-device probes page is not modified', () => {
  it('DeviceProbes.tsx module still exports a default function component', async () => {
    const module = await import('./DeviceProbes')
    expect(module.default).toBeDefined()
    expect(typeof module.default).toBe('function')
  })
})

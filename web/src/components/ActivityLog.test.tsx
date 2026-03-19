import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import ActivityLog from './ActivityLog'

const mockGetDeviceCommands = vi.fn()
const mockGetCommandOutput = vi.fn()

vi.mock('../api/client', () => ({
  api: {
    getDeviceCommands: (...args: unknown[]) => mockGetDeviceCommands(...args),
    getCommandOutput: (...args: unknown[]) => mockGetCommandOutput(...args),
  },
}))

vi.mock('../hooks/useWebSocket', () => ({
  useWebSocket: () => ({ connected: false }),
}))

function renderWithProviders() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <ActivityLog deviceId="dev-1" />
    </QueryClientProvider>,
  )
}

const mockCommands = [
  {
    id: 'cmd-1',
    device_id: 'dev-1',
    action: 'os_update',
    params: { mode: 'full' },
    status: 'success',
    result_msg: 'Updated 12 packages',
    duration_ms: 34000,
    exit_code: 0,
    created_at: '2026-03-19T10:00:00Z',
    updated_at: '2026-03-19T10:00:34Z',
  },
  {
    id: 'cmd-2',
    device_id: 'dev-1',
    action: 'docker_restart',
    params: {},
    status: 'error',
    result_msg: 'Container not found',
    duration_ms: 150,
    exit_code: 1,
    created_at: '2026-03-19T09:00:00Z',
    updated_at: '2026-03-19T09:00:01Z',
  },
  {
    id: 'cmd-3',
    device_id: 'dev-1',
    action: 'reboot',
    params: {},
    status: 'pending',
    result_msg: '',
    duration_ms: null,
    exit_code: null,
    created_at: '2026-03-19T08:00:00Z',
    updated_at: '2026-03-19T08:00:00Z',
  },
]

describe('ActivityLog', () => {
  beforeEach(() => {
    mockGetDeviceCommands.mockReset()
    mockGetCommandOutput.mockReset()
  })

  it('renders table with command data', async () => {
    mockGetDeviceCommands.mockResolvedValue(mockCommands)
    renderWithProviders()

    // Action names appear in both dropdown and table, so use findAllByText
    const osUpdateElements = await screen.findAllByText('OS Update')
    expect(osUpdateElements.length).toBeGreaterThanOrEqual(2) // dropdown + table
    expect(screen.getByText('Updated 12 packages')).toBeInTheDocument()
    expect(screen.getByText('34s')).toBeInTheDocument()
    const dockerElements = screen.getAllByText('Docker Restart')
    expect(dockerElements.length).toBeGreaterThanOrEqual(1)
  })

  it('shows empty state when no commands exist', async () => {
    mockGetDeviceCommands.mockResolvedValue([])
    renderWithProviders()

    expect(await screen.findByText('No command history')).toBeInTheDocument()
  })

  it('shows status badges with correct styling', async () => {
    mockGetDeviceCommands.mockResolvedValue(mockCommands)
    renderWithProviders()

    const successBadge = await screen.findByText('success')
    expect(successBadge.className).toContain('emerald')

    const errorBadge = screen.getByText('error')
    expect(errorBadge.className).toContain('red')

    const pendingBadge = screen.getByText('pending')
    expect(pendingBadge.className).toContain('gray')
  })

  it('highlights non-zero exit codes in red', async () => {
    mockGetDeviceCommands.mockResolvedValue(mockCommands)
    renderWithProviders()

    // Exit code 1 should have red styling
    const exitCode1 = await screen.findByText('1')
    expect(exitCode1.className).toContain('red')

    // Exit code 0 should not have red styling
    const exitCode0 = screen.getByText('0')
    expect(exitCode0.className).not.toContain('red')
  })

  it('expands a row to show output on click', async () => {
    mockGetDeviceCommands.mockResolvedValue(mockCommands)
    mockGetCommandOutput.mockResolvedValue([
      { id: 1, command_id: 'cmd-1', stream: 'stdout', content: 'Updating packages...\n', created_at: '2026-03-19T10:00:01Z' },
      { id: 2, command_id: 'cmd-1', stream: 'stdout', content: 'Done.\n', created_at: '2026-03-19T10:00:34Z' },
    ])
    renderWithProviders()

    // Wait for data, then find OS Update in the table (not the dropdown)
    await screen.findByText('Updated 12 packages')
    const tableCell = screen.getByText('Updated 12 packages').closest('tr')!
    await userEvent.click(tableCell)

    // Should show params
    expect(await screen.findByText('Parameters')).toBeInTheDocument()

    // Should show output content
    expect(await screen.findByText(/Updating packages/)).toBeInTheDocument()

    // Verify getCommandOutput was called
    expect(mockGetCommandOutput).toHaveBeenCalledWith('dev-1', 'cmd-1')
  })

  it('shows "No output captured" when command has no output', async () => {
    mockGetDeviceCommands.mockResolvedValue([mockCommands[2]])
    mockGetCommandOutput.mockResolvedValue([])
    renderWithProviders()

    // Wait for table to render, then find the Reboot row in the tbody (not dropdown)
    const rebootElements = await screen.findAllByText('Reboot')
    const tableReboot = rebootElements.find(el => el.closest('td'))!
    await userEvent.click(tableReboot.closest('tr')!)

    expect(await screen.findByText('No output captured')).toBeInTheDocument()
  })

  it('shows dash for null duration and exit code', async () => {
    mockGetDeviceCommands.mockResolvedValue([mockCommands[2]])
    renderWithProviders()

    // Wait for table to render
    const rebootElements = await screen.findAllByText('Reboot')
    const tableReboot = rebootElements.find(el => el.closest('td'))!
    const row = tableReboot.closest('tr')!
    const cells = within(row).getAllByRole('cell')
    // Duration cell (index 3) and Exit Code cell (index 4) should show '-'
    expect(cells[3].textContent).toBe('-')
    expect(cells[4].textContent).toBe('-')
  })
})

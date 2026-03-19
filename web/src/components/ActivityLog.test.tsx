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

    const successBadges = await screen.findAllByText('success')
    expect(successBadges[0].className).toContain('emerald')

    const errorBadges = screen.getAllByText('error')
    expect(errorBadges[0].className).toContain('red')

    const pendingBadge = screen.getByText('pending')
    expect(pendingBadge.className).toContain('gray')
  })

  it('opens a detail modal on row click', async () => {
    mockGetDeviceCommands.mockResolvedValue(mockCommands)
    mockGetCommandOutput.mockResolvedValue([
      { id: 1, command_id: 'cmd-1', stream: 'stdout', content: 'Updating packages...\n', created_at: '2026-03-19T10:00:01Z' },
      { id: 2, command_id: 'cmd-1', stream: 'stdout', content: 'Done.\n', created_at: '2026-03-19T10:00:34Z' },
    ])
    renderWithProviders()

    // Wait for data, then click the row with the summary text
    await screen.findByText('Updated 12 packages')
    const tableCell = screen.getByText('Updated 12 packages').closest('tr')!
    await userEvent.click(tableCell)

    // Modal should open with command details
    const modal = screen.getByRole('dialog')
    expect(modal).toBeInTheDocument()

    // Should show full summary in modal
    expect(within(modal).getByText('Updated 12 packages')).toBeInTheDocument()

    // Should show duration in modal
    expect(within(modal).getByText('34s')).toBeInTheDocument()

    // Should show exit code in modal
    expect(within(modal).getByText('0')).toBeInTheDocument()

    // Should show params in modal
    expect(within(modal).getByText('Parameters')).toBeInTheDocument()

    // Should show output content
    expect(await within(modal).findByText(/Updating packages/)).toBeInTheDocument()

    // Verify getCommandOutput was called
    expect(mockGetCommandOutput).toHaveBeenCalledWith('dev-1', 'cmd-1')
  })

  it('shows "No output captured" in modal when command has no output', async () => {
    mockGetDeviceCommands.mockResolvedValue([mockCommands[2]])
    mockGetCommandOutput.mockResolvedValue([])
    renderWithProviders()

    // Wait for table to render, then find the Reboot row in the tbody (not dropdown)
    const rebootElements = await screen.findAllByText('Reboot')
    const tableReboot = rebootElements.find(el => el.closest('td'))!
    await userEvent.click(tableReboot.closest('tr')!)

    // Modal should show "No output captured"
    const modal = screen.getByRole('dialog')
    expect(within(modal).getByText('No output captured')).toBeInTheDocument()
  })

  it('shows dash for null duration and exit code in modal', async () => {
    mockGetDeviceCommands.mockResolvedValue([mockCommands[2]])
    mockGetCommandOutput.mockResolvedValue([])
    renderWithProviders()

    // Click the row to open modal
    const rebootElements = await screen.findAllByText('Reboot')
    const tableReboot = rebootElements.find(el => el.closest('td'))!
    await userEvent.click(tableReboot.closest('tr')!)

    const modal = screen.getByRole('dialog')
    // Duration should show '-'
    const durationLabel = within(modal).getByText('Duration')
    const durationValue = durationLabel.parentElement!.querySelectorAll('p')[1]!
    expect(durationValue.textContent).toBe('-')

    // Exit Code should show '-'
    const exitLabel = within(modal).getByText('Exit Code')
    const exitValue = exitLabel.parentElement!.querySelectorAll('p')[1]!
    expect(exitValue.textContent).toBe('-')
  })

  it('highlights non-zero exit code in red in modal', async () => {
    mockGetDeviceCommands.mockResolvedValue([mockCommands[1]])
    mockGetCommandOutput.mockResolvedValue([])
    renderWithProviders()

    // Click the error row
    const dockerElements = await screen.findAllByText('Docker Restart')
    const tableDocker = dockerElements.find(el => el.closest('td'))!
    await userEvent.click(tableDocker.closest('tr')!)

    const modal = screen.getByRole('dialog')
    const exitLabel = within(modal).getByText('Exit Code')
    const exitValue = exitLabel.parentElement!.querySelectorAll('p')[1]!
    expect(exitValue.textContent).toBe('1')
    expect(exitValue.className).toContain('red')
  })

  it('closes modal when close button is clicked', async () => {
    mockGetDeviceCommands.mockResolvedValue(mockCommands)
    mockGetCommandOutput.mockResolvedValue([])
    renderWithProviders()

    // Open modal
    await screen.findByText('Updated 12 packages')
    const tableCell = screen.getByText('Updated 12 packages').closest('tr')!
    await userEvent.click(tableCell)

    expect(screen.getByRole('dialog')).toBeInTheDocument()

    // Close modal
    const closeButton = screen.getByLabelText('Close')
    await userEvent.click(closeButton)

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('closes modal when backdrop is clicked', async () => {
    mockGetDeviceCommands.mockResolvedValue(mockCommands)
    mockGetCommandOutput.mockResolvedValue([])
    renderWithProviders()

    // Open modal
    await screen.findByText('Updated 12 packages')
    const tableCell = screen.getByText('Updated 12 packages').closest('tr')!
    await userEvent.click(tableCell)

    const modal = screen.getByRole('dialog')
    expect(modal).toBeInTheDocument()

    // Click the backdrop (the dialog overlay itself)
    await userEvent.click(modal)

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

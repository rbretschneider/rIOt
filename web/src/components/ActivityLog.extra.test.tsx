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

// The AC-028 result: apt succeeded, DietPi failed, so the command failed.
const RESULT = 'apt: succeeded; dietpi: failed (exit 1)\napt: upgraded 3 packages\ndietpi: failed (exit 1)'

beforeEach(() => {
  vi.clearAllMocks()
  mockGetCommandOutput.mockResolvedValue([])
})

describe('[EXTRA-UPD-001 AC-041] the command result view', () => {
  it('shows the apt and DietPi outcome lines on separate lines and the overall failed outcome', async () => {
    mockGetDeviceCommands.mockResolvedValue([{
      id: 'cmd-eu', device_id: 'dev-1', action: 'os_update', params: { mode: 'full' }, status: 'failed',
      result_msg: RESULT, duration_ms: 2400000, exit_code: 1,
      created_at: '2026-10-26T10:00:00Z', updated_at: '2026-10-26T10:40:00Z',
    }])
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(<QueryClientProvider client={qc}><ActivityLog deviceId="dev-1" /></QueryClientProvider>)

    const cell = await screen.findByText(/apt: succeeded; dietpi: failed \(exit 1\)/)
    await userEvent.click(cell.closest('tr')!)

    const modal = screen.getByRole('dialog')
    expect(within(modal).getByText('Failed')).toBeInTheDocument()
    const summary = within(modal).getByText(/apt: upgraded 3 packages/)
    expect(summary.textContent).toBe(RESULT)
    expect(summary.className).toMatch(/whitespace-pre-wrap/)
  })
})

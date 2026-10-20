import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { api } from '../api/client'
import type { ProbeResult, ProbeWithResult } from '../types/models'
import { renderWithProviders } from '../test/renderWithProviders'
import ProbeDetail from './ProbeDetail'

vi.mock('../api/client', () => ({ api: {
  getProbes: vi.fn(), getProbeResults: vi.fn(), runProbe: vi.fn(), updateProbe: vi.fn(),
} }))

const probe: ProbeWithResult = {
  id: 7, name: 'Public service availability', type: 'http', enabled: false,
  config: { url: 'https://example.test', method: 'GET', expected_status: 200 },
  interval_seconds: 60, timeout_seconds: 10, total_checks: 1,
  created_at: '2026-10-20T12:00:00Z', updated_at: '2026-10-20T12:00:00Z',
}
const result: ProbeResult = {
  id: 1, probe_id: 7, success: true, latency_ms: 14.5, error_msg: '',
  metadata: {}, created_at: '2026-10-20T12:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getProbes).mockResolvedValue([probe])
  vi.mocked(api.getProbeResults).mockResolvedValue([result])
})

describe('[APP-WORKSPACE AC-007] probe resource header preserves operations', () => {
  it('shows one resource title, disabled metadata, history and its collection return link', async () => {
    renderWithProviders(<ProbeDetail />, { path: '/probes/:id', route: '/probes/7' })
    await screen.findByRole('heading', { name: probe.name, level: 1 })
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(screen.getByText('(disabled)')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Back to Probes/ })).toHaveAttribute('href', '/probes')
    expect(await screen.findByRole('cell', { name: '14.5ms' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit' })).toBeEnabled()
  })

  it('keeps Run Now pending until the explicit check finishes and refreshes results', async () => {
    const user = userEvent.setup()
    let finish!: (value: ProbeResult) => void
    vi.mocked(api.runProbe).mockReturnValue(new Promise((resolve) => { finish = resolve }))
    renderWithProviders(<ProbeDetail />, { path: '/probes/:id', route: '/probes/7' })
    await user.click(await screen.findByRole('button', { name: 'Run Now' }))
    expect(api.runProbe).toHaveBeenCalledWith(7)
    expect(screen.getByRole('button', { name: 'Running...' })).toBeDisabled()
    await act(async () => { finish(result) })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Run Now' })).toBeEnabled())
    expect(api.getProbeResults).toHaveBeenCalledWith(7, 100)
    expect(vi.mocked(api.getProbeResults).mock.calls.length).toBeGreaterThan(1)
  })
})

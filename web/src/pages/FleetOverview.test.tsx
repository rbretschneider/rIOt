import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import FleetOverview from './FleetOverview'

vi.mock('../api/client', () => ({
  api: {
    getDevices: vi.fn().mockResolvedValue([
      {
        id: 'dev-1',
        hostname: 'node-1',
        short_id: 'abc123',
        arch: 'amd64',
        status: 'online',
        agent_version: '2.0.4',
        primary_ip: '192.168.1.1',
        tags: ['prod'],
        last_heartbeat: new Date().toISOString(),
      },
    ]),
    getServerUpdate: vi.fn().mockResolvedValue({ latest_version: '2.0.5' }),
    getPatchStatus: vi.fn().mockResolvedValue([]),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({
    data: [
      {
        id: 'dev-1',
        hostname: 'node-1',
        short_id: 'abc123',
        arch: 'amd64',
        status: 'online',
        agent_version: '2.0.4',
        primary_ip: '192.168.1.1',
        tags: ['prod'],
        last_heartbeat: new Date().toISOString(),
      },
    ],
    isLoading: false,
    wsConnected: false,
  }),
}))

function renderWithProviders() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <FleetOverview />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('FleetOverview', () => {
  it('renders without crashing (smoke test)', async () => {
    renderWithProviders()
    expect(await screen.findByText('node-1')).toBeInTheDocument()
  })

  it('shows outdated label instead of asterisk', async () => {
    renderWithProviders()
    expect(await screen.findByText('(outdated)')).toBeInTheDocument()
    expect(screen.queryByText('*')).not.toBeInTheDocument()
  })
})

describe('[AC-001] Score column removed from Fleet Overview', () => {
  it('does not render a Score column header in the device table', async () => {
    renderWithProviders()
    await screen.findByText('node-1')
    const headers = screen.getAllByRole('columnheader')
    const headerTexts = headers.map(h => h.textContent?.trim() ?? '')
    const hasScoreColumn = headerTexts.some(t => t === 'Score')
    expect(hasScoreColumn).toBe(false)
  })

  it('does not render a MiniScore radial indicator for any device', async () => {
    renderWithProviders()
    await screen.findByText('node-1')
    // MiniScore renders an SVG with a title attribute like "Security: ..."
    const scoreTitles = document.querySelectorAll('[title^="Security:"]')
    expect(scoreTitles.length).toBe(0)
  })
})

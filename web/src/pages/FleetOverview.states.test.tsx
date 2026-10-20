import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import { onlineManager } from '@tanstack/react-query'
import userEvent from '@testing-library/user-event'
import FleetOverview from './FleetOverview'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'

// The real useDevices runs here (only its WebSocket and the api are replaced), so
// the page sees exactly the query states a real fetch produces.
vi.mock('../api/client', () => ({
  api: {
    getDevices: vi.fn(),
    getServerUpdate: vi.fn(),
    getPatchStatus: vi.fn(),
    getAgentVersions: vi.fn(),
    getPatchStatusDetail: vi.fn(),
    bulkUpdateAgents: vi.fn(),
    bulkPatchDevices: vi.fn(),
  },
}))

vi.mock('../hooks/useWebSocket', () => ({
  useWebSocket: () => ({ connected: false }),
}))

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

// SetupGuide fetches the server certificate on its own; a marker is enough to know it rendered.
vi.mock('../components/SetupGuide', () => ({
  default: ({ inline }: { inline?: boolean }) => <div data-testid="setup-guide">{inline ? 'inline guide' : 'modal guide'}</div>,
}))

const device = (id: string, hostname: string) => ({
  id,
  hostname,
  short_id: id,
  arch: 'amd64',
  status: 'online',
  agent_version: '2.0.5',
  primary_ip: '10.0.0.1',
  tags: [],
  last_heartbeat: '2026-10-17T12:00:00Z',
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.0.5' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getAgentVersions).mockResolvedValue([])
})

describe('[APP-WORKSPACE AC-008] Devices remains the complete machine collection', () => {
  it('retains machines without Docker and offline Docker hosts alongside online Docker hosts', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([
      { ...device('plain', 'plain-machine'), docker_available: false },
      { ...device('docker-live', 'docker-online'), docker_available: true },
      { ...device('docker-down', 'docker-offline'), docker_available: true, status: 'offline' },
    ] as never)
    renderWithProviders(<FleetOverview />)
    expect(await screen.findByRole('link', { name: 'plain-machine' })).toHaveAttribute('href', '/devices/plain')
    expect(screen.getByRole('link', { name: 'docker-online' })).toHaveAttribute('href', '/devices/docker-live')
    expect(screen.getByRole('link', { name: 'docker-offline' })).toHaveAttribute('href', '/devices/docker-down')
  })
})

describe('[UIF AC-090] fetch error shows error panel, not SetupGuide', () => {
  it('shows "Couldn\'t load devices", the message and a Retry button', async () => {
    vi.mocked(api.getDevices).mockRejectedValue(new Error('HTTP 500: Internal Server Error'))
    renderWithProviders(<FleetOverview />)

    const panel = await screen.findByRole('alert')
    expect(within(panel).getByRole('heading', { name: "Couldn't load devices" })).toBeInTheDocument()
    expect(within(panel).getByText('HTTP 500: Internal Server Error')).toBeInTheDocument()
    expect(within(panel).getByRole('button', { name: 'Retry' })).toBeInTheDocument()
  })

  it('renders no SetupGuide content for a failed fetch', async () => {
    vi.mocked(api.getDevices).mockRejectedValue(new Error('HTTP 500: Internal Server Error'))
    renderWithProviders(<FleetOverview />)
    await screen.findByRole('alert')
    expect(screen.queryByTestId('setup-guide')).toBeNull()
    expect(screen.queryByText(/Add your first device/i)).toBeNull()
  })

  it('treats a network failure the same way', async () => {
    vi.mocked(api.getDevices).mockRejectedValue(new TypeError('Failed to fetch'))
    renderWithProviders(<FleetOverview />)
    expect(await screen.findByText('Failed to fetch')).toBeInTheDocument()
    expect(screen.queryByTestId('setup-guide')).toBeNull()
  })
})

describe('[UIF AC-091] retry recovers', () => {
  it('refetches on Retry and renders both device rows', async () => {
    vi.mocked(api.getDevices)
      .mockRejectedValueOnce(new Error('HTTP 502: Bad Gateway'))
      .mockResolvedValue([device('d1', 'alpha'), device('d2', 'beta')] as never)
    renderWithProviders(<FleetOverview />)

    await userEvent.click(await screen.findByRole('button', { name: 'Retry' }))

    expect(await screen.findByText('alpha')).toBeInTheDocument()
    expect(screen.getByText('beta')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(api.getDevices).toHaveBeenCalledTimes(2)
  })
})

describe('[UIF AC-092] empty fleet shows SetupGuide', () => {
  it('renders the inline guide when the fetch succeeded with zero devices and no search', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([])
    renderWithProviders(<FleetOverview />)
    expect(await screen.findByTestId('setup-guide')).toHaveTextContent('inline guide')
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('replaces the empty device table with the guide', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([])
    renderWithProviders(<FleetOverview />)
    await screen.findByTestId('setup-guide')
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('shows "No devices match your search" (not the guide) when a search hides every device', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('d1', 'alpha')] as never)
    renderWithProviders(<FleetOverview />)
    await screen.findByText('alpha')
    await userEvent.type(screen.getByPlaceholderText('Search devices...'), 'zzz')
    expect(screen.getByText('No devices match your search')).toBeInTheDocument()
    expect(screen.queryByTestId('setup-guide')).toBeNull()
  })
})

// Added by QA Engineer
// Covers AC-092, AC-093, BR-004: the fleet is "positively confirmed empty" only by a successful empty
// response. A fetch that never ran (browser offline, TanStack Query pauses it) and a failure that
// follows an earlier success must both be anything but "Add your first device".
describe('[UIF AC-092] QA: only a successful empty response yields the SetupGuide', () => {
  afterEach(() => {
    onlineManager.setOnline(true)
  })

  it('does not show the SetupGuide while the first fetch is paused because the browser is offline', async () => {
    onlineManager.setOnline(false)
    vi.mocked(api.getDevices).mockResolvedValue([])
    renderWithProviders(<FleetOverview />)
    await new Promise(resolve => setTimeout(resolve, 50))

    expect(api.getDevices).not.toHaveBeenCalled()
    expect(screen.queryByTestId('setup-guide')).toBeNull()
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument()
  })

  it('keeps the last good device list when a background refetch fails (no error panel, no SetupGuide)', async () => {
    vi.mocked(api.getDevices)
      .mockResolvedValueOnce([device('d1', 'alpha')] as never)
      .mockRejectedValue(new Error('HTTP 500: Internal Server Error'))
    const { queryClient } = renderWithProviders(<FleetOverview />)
    await screen.findByText('alpha')

    await queryClient.refetchQueries({ queryKey: ['devices'] }).catch(() => {})

    expect(await screen.findByText('alpha')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByTestId('setup-guide')).toBeNull()
  })
})

describe('[UIF AC-093] loading shows placeholder, no SetupGuide', () => {
  it('shows a busy status placeholder while the first fetch is pending', async () => {
    vi.mocked(api.getDevices).mockReturnValue(new Promise(() => {}))
    renderWithProviders(<FleetOverview />)

    const status = await screen.findByRole('status', { name: 'Loading' })
    expect(status).toHaveAttribute('aria-busy', 'true')
    expect(screen.queryByTestId('setup-guide')).toBeNull()
    expect(screen.queryByText('Loading...')).toBeNull()
  })

  it('swaps the placeholder for the table when data arrives', async () => {
    vi.mocked(api.getDevices).mockResolvedValue([device('d1', 'alpha')] as never)
    renderWithProviders(<FleetOverview />)
    expect(await screen.findByText('alpha')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('status', { name: 'Loading' })).toBeNull())
  })
})

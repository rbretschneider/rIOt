import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { renderWithProviders } from '../../test/renderWithProviders'
import type { DockerHost } from '../../types/docker'
import DockerLayout from './DockerLayout'
import HostsPage from './HostsPage'

const mock = vi.hoisted(() => ({ hosts: vi.fn(), isEnabled: vi.fn() }))
vi.mock('../../api/docker', () => ({ dockerApi: { hosts: mock.hosts } }))
vi.mock('../../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: mock.isEnabled }),
}))

const hosts: DockerHost[] = [
  {
    device_id: 'host/one east',
    hostname: 'Hawaii',
    primary_ip: '10.0.10.11',
    status: 'online',
    docker_available: true,
    capabilities: ['logs_live_v1'],
    inventory_enabled: true,
    container_logs: 'enabled',
  },
  {
    device_id: 'host-two',
    hostname: 'Vermont',
    primary_ip: '10.0.20.12',
    status: 'offline',
    docker_available: true,
    capabilities: [],
    inventory_enabled: false,
    container_logs: 'disabled',
  },
  {
    device_id: 'no-docker',
    hostname: 'PiVPN',
    status: 'online',
    docker_available: false,
    capabilities: [],
    inventory_enabled: true,
    container_logs: 'enabled',
  },
]

beforeEach(() => {
  vi.clearAllMocks()
  mock.isEnabled.mockReturnValue(true)
  mock.hosts.mockResolvedValue(hosts)
})

function renderWorkspace(route = '/docker/containers') {
  return renderWithProviders(
    <Routes>
      <Route path="docker" element={<DockerLayout />}>
        <Route path="*" element={<h1>Child collection</h1>} />
      </Route>
    </Routes>,
    { route },
  )
}

describe('Docker workspace boundary', () => {
  it('[APP-WORKSPACE AC-001] renders the child without a second navigation or host query', () => {
    renderWorkspace('/docker/stacks/host-one/app')
    expect(
      screen.getByRole('heading', { name: 'Child collection' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
    expect(mock.hosts).not.toHaveBeenCalled()
  })
  it('[APP-WORKSPACE AC-009] honors the Docker feature gate without requesting hosts or rendering a child', () => {
    mock.isEnabled.mockReturnValue(false)
    renderWorkspace()
    expect(screen.getByText('Docker is disabled')).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: 'Child collection' }),
    ).not.toBeInTheDocument()
    expect(mock.hosts).not.toHaveBeenCalled()
  })
})

describe('Docker hosts', () => {
  it('[APP-WORKSPACE AC-008] distinguishes no Docker hosts from a search mismatch', async () => {
    mock.hosts.mockResolvedValue([hosts[2]])
    renderWithProviders(<HostsPage />, { route: '/docker/hosts' })
    expect(await screen.findByText('No Docker hosts')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'View Devices' })).toHaveAttribute(
      'href',
      '/',
    )
    expect(screen.queryByText('No matching hosts')).not.toBeInTheDocument()
  })

  it('[APP-WORKSPACE AC-008] preserves failed host requests as an error', async () => {
    mock.hosts.mockRejectedValue(new Error('Host service unavailable'))
    renderWithProviders(<HostsPage />, { route: '/docker/hosts' })
    expect(await screen.findByRole('alert')).toBeInTheDocument()
    expect(screen.queryByText('No Docker hosts')).not.toBeInTheDocument()
  })

  it('[APP-WORKSPACE AC-008] reports known Docker host capabilities and links to collections scoped to that host', async () => {
    renderWithProviders(<HostsPage />, { route: '/docker/hosts' })
    const current = within(
      (await screen.findByRole('heading', { name: 'Hawaii' })).closest(
        'article',
      )!,
    )
    expect(current.getByText('Available')).toBeInTheDocument()
    expect(current.getByText('Supported')).toBeInTheDocument()
    expect(current.getByText('enabled')).toBeInTheDocument()
    expect(current.getByText('Enabled')).toBeInTheDocument()
    for (const collection of ['Containers', 'Stacks', 'Images', 'Activity']) {
      expect(
        current.getByRole('link', { name: `${collection} →` }),
      ).toHaveAttribute(
        'href',
        `/docker/${collection.toLowerCase()}?host=host%2Fone%20east`,
      )
    }
    const legacy = within(
      screen.getByRole('heading', { name: 'Vermont' }).closest('article')!,
    )
    expect(legacy.getByText('Available')).toBeInTheDocument()
    expect(legacy.getByText('Requires agent update')).toBeInTheDocument()
    expect(legacy.getByText('Not enabled')).toBeInTheDocument()
    expect(legacy.getByText('disabled')).toBeInTheDocument()
    expect(legacy.getByText('offline')).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: 'PiVPN' }),
    ).not.toBeInTheDocument()
    expect(hosts).toHaveLength(3)
    expect(hosts[2].hostname).toBe('PiVPN')
  })

  it('searches hostnames without case sensitivity and addresses, with a recoverable no-match state', async () => {
    const user = userEvent.setup()
    renderWithProviders(<HostsPage />, { route: '/docker/hosts' })
    await screen.findByRole('heading', { name: 'Hawaii' })
    const search = screen.getByRole('textbox', { name: 'Search Docker hosts' })
    await user.type(search, 'HAWAII')
    expect(screen.getByRole('heading', { name: 'Hawaii' })).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: 'Vermont' }),
    ).not.toBeInTheDocument()
    await user.clear(search)
    await user.type(search, '10.0.20')
    expect(screen.getByRole('heading', { name: 'Vermont' })).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: 'Hawaii' }),
    ).not.toBeInTheDocument()
    await user.clear(search)
    await user.type(search, 'missing')
    expect(screen.getByText('No matching hosts')).toBeInTheDocument()
    await user.clear(search)
    expect(screen.getByRole('heading', { name: 'Hawaii' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Vermont' })).toBeInTheDocument()
  })
})

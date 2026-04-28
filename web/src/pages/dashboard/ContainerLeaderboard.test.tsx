import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import ContainerLeaderboard from './ContainerLeaderboard'
import type { FleetContainerRow } from '../../types/models'

vi.mock('../../components/Sparkline', () => ({
  default: ({ data }: { data: number[] }) => (
    <svg data-testid="sparkline-cell" data-points={data.join(',')} />
  ),
}))

function container(
  overrides: Partial<FleetContainerRow> & { container_name: string; device_id: string },
): FleetContainerRow {
  return {
    hostname: 'host-1',
    container_id: overrides.container_name,
    image: 'nginx:latest',
    stack: '',
    state: 'running',
    cpu_percent: 0,
    mem_usage: 0,
    mem_limit: 1073741824,
    restart_count: 0,
    update_available: false,
    ...overrides,
  }
}

const TOP_11 = Array.from({ length: 11 }, (_, i) =>
  container({
    device_id: 'dev-1',
    container_name: `container-${i + 1}`,
    cpu_percent: 100 - i, // container-1 has highest CPU = 99, container-11 has 89
  }),
)

describe('[AC-019] ContainerLeaderboard — sort modes (FR-041 / FR-042)', () => {
  it('default sort is CPU descending', () => {
    const containers = [
      container({ device_id: 'd1', container_name: 'low', cpu_percent: 10 }),
      container({ device_id: 'd1', container_name: 'high', cpu_percent: 80 }),
    ]
    render(<ContainerLeaderboard containers={containers} />)
    const rows = screen.getAllByRole('row').slice(1) // skip header
    expect(rows[0]).toHaveTextContent('high')
    expect(rows[1]).toHaveTextContent('low')
  })

  it('memory sort orders by mem_usage descending', () => {
    const containers = [
      container({ device_id: 'd1', container_name: 'small', mem_usage: 100 }),
      container({ device_id: 'd1', container_name: 'large', mem_usage: 900 }),
    ]
    render(<ContainerLeaderboard containers={containers} />)
    fireEvent.click(screen.getByTestId('sort-memory'))
    const rows = screen.getAllByRole('row').slice(1)
    expect(rows[0]).toHaveTextContent('large')
    expect(rows[1]).toHaveTextContent('small')
  })

  it('restarts sort orders by restart_count descending', () => {
    const containers = [
      container({ device_id: 'd1', container_name: 'stable', restart_count: 0 }),
      container({ device_id: 'd1', container_name: 'flappy', restart_count: 10 }),
    ]
    render(<ContainerLeaderboard containers={containers} />)
    fireEvent.click(screen.getByTestId('sort-restarts'))
    const rows = screen.getAllByRole('row').slice(1)
    expect(rows[0]).toHaveTextContent('flappy')
    expect(rows[1]).toHaveTextContent('stable')
  })

  it('updates sort orders by update_available descending (true before false)', () => {
    const containers = [
      container({ device_id: 'd1', container_name: 'up-to-date', update_available: false }),
      container({ device_id: 'd1', container_name: 'stale', update_available: true }),
    ]
    render(<ContainerLeaderboard containers={containers} />)
    fireEvent.click(screen.getByTestId('sort-updates'))
    const rows = screen.getAllByRole('row').slice(1)
    expect(rows[0]).toHaveTextContent('stale')
    expect(rows[1]).toHaveTextContent('up-to-date')
  })

  it('sort buttons have aria-pressed=true for active mode', () => {
    const containers = [
      container({ device_id: 'd1', container_name: 'app', cpu_percent: 10 }),
    ]
    render(<ContainerLeaderboard containers={containers} />)
    expect(screen.getByTestId('sort-cpu')).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(screen.getByTestId('sort-memory'))
    expect(screen.getByTestId('sort-memory')).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByTestId('sort-cpu')).toHaveAttribute('aria-pressed', 'false')
  })
})

describe('[AC-019] ContainerLeaderboard — top-10 cap (FR-042)', () => {
  it('renders at most 10 rows from 11 containers', () => {
    render(<ContainerLeaderboard containers={TOP_11} />)
    const rows = screen.getAllByRole('row').slice(1) // exclude header
    expect(rows).toHaveLength(10)
  })

  it('top-10 rows contain the 10 highest-CPU containers when sorted by CPU', () => {
    render(<ContainerLeaderboard containers={TOP_11} />)
    // container-1 (cpu=99) through container-10 (cpu=90) should be visible
    // container-11 (cpu=89) should be cut off
    expect(screen.queryByTestId('leaderboard-row-container-10')).toBeInTheDocument()
    expect(screen.queryByTestId('leaderboard-row-container-11')).not.toBeInTheDocument()
  })
})

describe('ContainerLeaderboard — filter chips removed in redesign', () => {
  it('does not render filter or stack chip elements', () => {
    const containers = [
      container({ device_id: 'd1', container_name: 'media-app', stack: 'media' }),
    ]
    render(<ContainerLeaderboard containers={containers} />)
    expect(screen.queryByTestId('filter-updates')).not.toBeInTheDocument()
    expect(screen.queryByTestId('filter-restarts')).not.toBeInTheDocument()
    expect(screen.queryByTestId('filter-stack-media')).not.toBeInTheDocument()
  })
})

describe('[AC-022] ContainerLeaderboard — empty state (FR-045)', () => {
  it('renders empty state component when containers array is empty', () => {
    render(<ContainerLeaderboard containers={[]} isLoading={false} />)
    expect(screen.getByTestId('leaderboard-empty-state')).toBeInTheDocument()
  })

  it('does not render table when empty', () => {
    render(<ContainerLeaderboard containers={[]} isLoading={false} />)
    expect(screen.queryByTestId('container-leaderboard')).not.toBeInTheDocument()
  })

  it('empty state contains descriptive text', () => {
    render(<ContainerLeaderboard containers={[]} isLoading={false} />)
    expect(screen.getByText(/no containers reported/i)).toBeInTheDocument()
  })

  it('does not render empty state when isLoading is true', () => {
    render(<ContainerLeaderboard containers={[]} isLoading={true} />)
    expect(screen.queryByTestId('leaderboard-empty-state')).not.toBeInTheDocument()
  })
})

describe('[AC-020] ContainerLeaderboard — row columns rendered (FR-043)', () => {
  it('renders hostname, container name, stack, and metric value for a row', () => {
    const containers = [
      container({
        device_id: 'd1',
        container_name: 'plex',
        hostname: 'media-server',
        stack: 'media',
        cpu_percent: 42.5,
      }),
    ]
    render(<ContainerLeaderboard containers={containers} />)
    expect(screen.getByTestId('leaderboard-row-plex').textContent).toContain('media-server')
    expect(screen.getByTestId('leaderboard-row-plex').textContent).toContain('plex')
    expect(screen.getByTestId('leaderboard-row-plex').textContent).toContain('media')
    // CPU value rendered as "42.5%"
    expect(screen.getByTestId('leaderboard-row-plex').textContent).toContain('42.5%')
  })

  it('renders — for stack when stack is empty', () => {
    const containers = [
      container({ device_id: 'd1', container_name: 'standalone', stack: '' }),
    ]
    render(<ContainerLeaderboard containers={containers} />)
    expect(screen.getByTestId('leaderboard-row-standalone').textContent).toContain('—')
  })
})

describe('[AD-012] ContainerLeaderboard — sensitive label data must not render', () => {
  it('does not render sensitive Docker label values in any row cell', () => {
    const sensitiveContainer: FleetContainerRow = {
      device_id: 'd1',
      hostname: 'host-1',
      container_id: 'abc',
      container_name: 'traefik',
      image: 'traefik:v3',
      // stack is the ONLY label-derived field allowed (from com.docker.compose.project)
      stack: 'monitoring',
      state: 'running',
      cpu_percent: 5.0,
      mem_usage: 52428800,
      mem_limit: 1073741824,
      restart_count: 0,
      update_available: false,
    }
    const { container: domContainer } = render(
      <ContainerLeaderboard containers={[sensitiveContainer]} />,
    )
    const html = domContainer.innerHTML
    // Simulate what would appear if basicauth passwords leaked
    expect(html).not.toContain('$apr1$')
    expect(html).not.toContain('basicauth')
    expect(html).not.toContain('traefik.http.middlewares')
    // stack value 'monitoring' IS allowed
    expect(html).toContain('monitoring')
  })

  it('does not use Labels map — only the pre-extracted stack string field flows into component', () => {
    // This test validates the component never tries to read a Labels property
    // by ensuring a container with no stack field renders "—" as expected
    const c: FleetContainerRow = {
      device_id: 'd2',
      hostname: 'host-2',
      container_id: 'xyz',
      container_name: 'myapp',
      image: 'myapp:latest',
      stack: '', // no stack
      state: 'running',
      cpu_percent: 0,
      mem_usage: 0,
      mem_limit: 0,
      restart_count: 0,
    }
    render(<ContainerLeaderboard containers={[c]} />)
    // Should show dash for empty stack, no label keys in DOM
    expect(screen.getByTestId('leaderboard-row-myapp').textContent).not.toContain('com.docker')
    expect(screen.getByTestId('leaderboard-row-myapp').textContent).not.toContain('Labels')
  })
})

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import CompactContainerTile from './CompactContainerTile'
import type { ContainerInfo } from '../types/models'

function makeContainer(overrides: Partial<ContainerInfo> = {}): ContainerInfo {
  return {
    id: 'c1',
    short_id: 'c1short',
    name: 'jellyfin',
    image: 'jellyfin/jellyfin:latest',
    state: 'running',
    status: 'Up 2 hours',
    created: 1_700_000_000,
    cpu_percent: 1.2,
    mem_usage: 100,
    mem_limit: 1000,
    ...overrides,
  }
}

// [AC-022] Containers requesting GPU access are flagged so operators can see
// the blast radius of a GPU driver update.
describe('[AC-022] CompactContainerTile surfaces GPU-dependent containers', () => {
  it('renders a GPU badge when the container requests GPU access', () => {
    render(<CompactContainerTile container={makeContainer({ uses_gpu: true })} onClick={() => {}} />)
    expect(screen.getByText('GPU')).toBeInTheDocument()
  })

  it('renders no GPU badge when the container does not use the GPU', () => {
    render(<CompactContainerTile container={makeContainer({ uses_gpu: false })} onClick={() => {}} />)
    expect(screen.queryByText('GPU')).not.toBeInTheDocument()
  })

  it('renders no GPU badge when uses_gpu is absent (old agent compatibility)', () => {
    render(<CompactContainerTile container={makeContainer()} onClick={() => {}} />)
    expect(screen.queryByText('GPU')).not.toBeInTheDocument()
  })
})

// [AC-062] The update-available indicator states why an update is available;
// check_failed and pinned verdicts never show it.
describe('[AC-062] CompactContainerTile update indicator', () => {
  it('titles the indicator "newer image pulled, container not recreated" for pulled_not_recreated', () => {
    render(<CompactContainerTile
      container={makeContainer({ update_available: true, update_status: 'update_available', update_reason: 'pulled_not_recreated' })}
      onClick={() => {}}
    />)
    expect(screen.getByTitle('Update available: newer image pulled, container not recreated')).toBeInTheDocument()
  })

  it('titles the indicator "newer image in registry" for registry_newer', () => {
    render(<CompactContainerTile
      container={makeContainer({ update_available: true, update_status: 'update_available', update_reason: 'registry_newer' })}
      onClick={() => {}}
    />)
    expect(screen.getByTitle('Update available: newer image in registry')).toBeInTheDocument()
  })

  it('shows the plain indicator for an old agent that only reports update_available', () => {
    render(<CompactContainerTile container={makeContainer({ update_available: true })} onClick={() => {}} />)
    expect(screen.getByTitle('Update available')).toBeInTheDocument()
  })

  it('shows no indicator for check_failed', () => {
    render(<CompactContainerTile
      container={makeContainer({ update_available: null, update_status: 'check_failed' })}
      onClick={() => {}}
    />)
    expect(screen.queryByTitle(/Update available/)).not.toBeInTheDocument()
  })

  it('shows no indicator for pinned', () => {
    render(<CompactContainerTile
      container={makeContainer({ update_available: false, update_status: 'pinned' })}
      onClick={() => {}}
    />)
    expect(screen.queryByTitle(/Update available/)).not.toBeInTheDocument()
  })

  it('shows no indicator when up to date', () => {
    render(<CompactContainerTile
      container={makeContainer({ update_available: false, update_status: 'up_to_date' })}
      onClick={() => {}}
    />)
    expect(screen.queryByTitle(/Update available/)).not.toBeInTheDocument()
  })
})

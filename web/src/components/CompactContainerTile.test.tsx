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

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import StatusBadge from './StatusBadge'

describe('StatusBadge', () => {
  it('renders online status with green styling', () => {
    render(<StatusBadge status="online" />)
    const badge = screen.getByText('Online')
    expect(badge).toBeInTheDocument()
    expect(badge.className).toContain('emerald')
  })

  it('renders offline status with red styling', () => {
    render(<StatusBadge status="offline" />)
    const badge = screen.getByText('Offline')
    expect(badge).toBeInTheDocument()
    expect(badge.className).toContain('red')
  })

  it('renders warning status with amber styling', () => {
    render(<StatusBadge status="warning" />)
    const badge = screen.getByText('Warning')
    expect(badge).toBeInTheDocument()
    expect(badge.className).toContain('amber')
  })
})

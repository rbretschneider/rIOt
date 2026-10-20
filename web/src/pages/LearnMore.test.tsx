import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders } from '../test/renderWithProviders'
import LearnMore from './LearnMore'

describe('[APP-WORKSPACE AC-007] security guidance shares the resource header', () => {
  it('keeps one title, severity, guidance and a return to its owning Security collection', () => {
    renderWithProviders(<LearnMore />, { route: '/learn/container-mem-limits', path: '/learn/:findingId' })
    expect(screen.getByRole('heading', { name: 'Container Memory Limits', level: 1 })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(screen.getByText('Warning')).toBeInTheDocument()
    expect(screen.getByText('Security guide · Docker')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'How to Fix It' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Back to Security/ })).toHaveAttribute('href', '/security')
  })

  it('preserves code copying as an explicit action', async () => {
    const user = userEvent.setup()
    const clipboard = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    renderWithProviders(<LearnMore />, { route: '/learn/container-mem-limits', path: '/learn/:findingId' })
    expect(clipboard).not.toHaveBeenCalled()
    await user.click(screen.getAllByRole('button', { name: 'Copy' })[0])
    expect(clipboard).toHaveBeenCalledWith(expect.stringContaining('memory: 512M'))
    expect(screen.getByRole('button', { name: 'Copied!' })).toBeInTheDocument()
    clipboard.mockRestore()
  })

  it('keeps missing guidance inside the same header and collection navigation', () => {
    renderWithProviders(<LearnMore />, { route: '/learn/missing', path: '/learn/:findingId' })
    expect(screen.getByRole('heading', { name: 'Article Not Found', level: 1 })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Back to Security/ })).toHaveAttribute('href', '/security')
  })
})

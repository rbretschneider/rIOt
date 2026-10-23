import { describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders } from '../../test/renderWithProviders'
import AutoUpdateSwitch, { WINDOW_DISABLED_NOTICE } from './AutoUpdateSwitch'

const label = 'Auto-update web on host-one'

describe('[AC-001][AC-003] auto-update switch', () => {
  it('shows its state and reports the flipped value', async () => {
    const onChange = vi.fn()
    renderWithProviders(<AutoUpdateSwitch checked={false} label={label} onChange={onChange} />)
    const button = screen.getByRole('switch', { name: label })
    expect(button).toHaveAttribute('aria-checked', 'false')
    await userEvent.setup().click(button)
    expect(onChange).toHaveBeenCalledExactlyOnceWith(true)
  })
  it('cannot be flipped again while saving', async () => {
    const onChange = vi.fn()
    renderWithProviders(<AutoUpdateSwitch checked pending label={label} onChange={onChange} />)
    const button = screen.getByRole('switch', { name: label })
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('aria-busy', 'true')
    await userEvent.setup().click(button)
    expect(onChange).not.toHaveBeenCalled()
  })
  it('disables both directions with a visible reason', () => {
    renderWithProviders(<AutoUpdateSwitch checked={false} disabledReason="Auto-update state unavailable" label={label} onChange={vi.fn()} />)
    expect(screen.getByRole('switch', { name: label })).toBeDisabled()
    expect(screen.getByText('Auto-update state unavailable')).toBeVisible()
  })
})

describe('[AC-002] stack switch blocked from turning on', () => {
  it('shows the reason and blocks turning on', () => {
    renderWithProviders(<AutoUpdateSwitch checked={false} onDisabledReason="Compose paths not agreed" label={label} onChange={vi.fn()} />)
    expect(screen.getByRole('switch', { name: label })).toBeDisabled()
    expect(screen.getByText('Compose paths not agreed')).toBeVisible()
  })
  it('still allows turning off', async () => {
    const onChange = vi.fn()
    renderWithProviders(<AutoUpdateSwitch checked onDisabledReason="Compose paths not agreed" label={label} onChange={onChange} />)
    await userEvent.setup().click(screen.getByRole('switch', { name: label }))
    expect(onChange).toHaveBeenCalledExactlyOnceWith(false)
    expect(screen.queryByText('Compose paths not agreed')).not.toBeInTheDocument()
  })
})

describe('[AC-010] switch controlled by a stack', () => {
  it('is on, read-only and links to the stack', () => {
    renderWithProviders(<AutoUpdateSwitch checked lockedBy={{ project: 'app', to: '/docker/stacks/host-1/app' }} label={label} onChange={vi.fn()} />)
    const button = screen.getByRole('switch', { name: label })
    expect(button).toHaveAttribute('aria-checked', 'true')
    expect(button).toHaveAttribute('aria-readonly', 'true')
    expect(button).toBeDisabled()
    expect(button.parentElement).toHaveTextContent('Controlled by stack app')
    expect(screen.getByRole('link', { name: 'app' })).toHaveAttribute('href', '/docker/stacks/host-1/app')
  })
  it('names the stack without a link when no link is given', () => {
    renderWithProviders(<AutoUpdateSwitch checked lockedBy={{ project: '..' }} label={label} onChange={vi.fn()} />)
    expect(screen.getByText(/Controlled by stack/)).toHaveTextContent('Controlled by stack ..')
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})

describe('[AC-008] window-disabled notice', () => {
  it('shows on an on switch while the window is disabled, with a link to Settings > Agents', () => {
    renderWithProviders(<AutoUpdateSwitch checked windowDisabled label={label} onChange={vi.fn()} />)
    expect(screen.getByRole('switch', { name: label })).toHaveAccessibleDescription(WINDOW_DISABLED_NOTICE)
    expect(screen.getByRole('link', { name: 'Settings > Agents' })).toHaveAttribute('href', '/settings/agents')
  })
  it('shows on a stack-controlled switch too', () => {
    renderWithProviders(<AutoUpdateSwitch checked windowDisabled lockedBy={{ project: 'app', to: '/x' }} label={label} onChange={vi.fn()} />)
    expect(screen.getByRole('link', { name: 'Settings > Agents' })).toBeInTheDocument()
  })
  it.each([
    [false, true],
    [true, false],
  ])('is absent when checked=%s and window disabled=%s', (checked, windowDisabled) => {
    renderWithProviders(<AutoUpdateSwitch checked={checked} windowDisabled={windowDisabled} label={label} onChange={vi.fn()} />)
    expect(screen.queryByRole('link', { name: 'Settings > Agents' })).not.toBeInTheDocument()
  })
})

import { describe, expect, it } from 'vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders } from '../../test/renderWithProviders'
import { AppShell } from './AppShell'

describe('Application workspace', () => {
  it.each([
    ['/', 'Devices'],
    ['/devices/a/terminal', 'Devices'],
    ['/dashboard', 'Overview'],
    ['/docker/containers/a/name', 'Containers'],
    ['/docker/stacks/a/name', 'Stacks'],
    ['/docker/hosts', 'Hosts'],
    ['/docker/images', 'Images'],
    ['/docker/volumes', 'Volumes'],
    ['/docker/networks', 'Networks'],
    ['/docker/activity', 'Activity'],
    ['/probes/a', 'Probes'],
    ['/alerts', 'Alerts'],
    ['/learn/a', 'Security'],
    ['/alert-rules', 'Alert Rules'],
    ['/settings/agents', 'Agents'],
    ['/settings/general', 'Settings'],
  ])(
    '[APP-WORKSPACE AC-002] %s has one owning navigation item',
    (route, label) => {
      renderWithProviders(
        <AppShell dockerEnabled>
          <h1>Content</h1>
        </AppShell>,
        { route },
      )
      const nav = screen.getByRole('navigation', { name: 'Application' })
      const current = within(nav)
        .getAllByRole('link')
        .filter((link) => link.getAttribute('aria-current') === 'page')
      expect(current).toHaveLength(1)
      expect(current[0]).toHaveTextContent(label)
      expect(screen.getAllByRole('main')).toHaveLength(1)
      expect(
        screen.getByRole('link', { name: 'Skip to content' }),
      ).toHaveAttribute('href', '#main-content')
    },
  )

  it('[APP-WORKSPACE AC-001] hides every Docker destination when disabled', () => {
    renderWithProviders(<AppShell dockerEnabled={false}>Content</AppShell>)
    const nav = screen.getByRole('navigation', { name: 'Application' })
    expect(
      within(nav)
        .getAllByRole('link')
        .some((link) => link.getAttribute('href')?.startsWith('/docker')),
    ).toBe(false)
    expect(
      within(nav).getByRole('link', { name: 'Devices' }),
    ).toBeInTheDocument()
  })

  it('[APP-WORKSPACE AC-003] closes the disclosure on Escape and route selection', async () => {
    const user = userEvent.setup()
    renderWithProviders(<AppShell dockerEnabled>Content</AppShell>)
    const toggle = screen.getByRole('button', { name: 'Toggle navigation' })
    const panel = document.getElementById('app-navigation')!
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(panel).toHaveClass('hidden')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await user.click(screen.getByRole('link', { name: 'Devices' }))
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    await user.click(screen.getByRole('link', { name: 'Probes' }))
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    screen.getByRole('link', { name: 'Devices' }).focus()
    await user.keyboard('{Escape}')
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveFocus()
  })

  it('[APP-WORKSPACE AC-003] skips to content without replacing the hash router destination', async () => {
    const user = userEvent.setup()
    renderWithProviders(<AppShell dockerEnabled>Content</AppShell>, {
      route: '/probes',
      showLocation: true,
    })
    await user.click(screen.getByRole('link', { name: 'Skip to content' }))
    expect(screen.getByRole('main')).toHaveFocus()
    expect(screen.getByTestId('location')).toHaveTextContent('/probes')
  })
})

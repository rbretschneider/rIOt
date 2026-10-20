import { useState } from 'react'
import { expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Tabs } from './Tabs'
import { DropdownMenu } from './DropdownMenu'
function TabFixture() {
  const [tab, setTab] = useState('overview')
  return (
    <Tabs
      label="Container details"
      tabs={[
        'overview',
        'logs',
        'activity',
        'inspect',
        'terminal',
        'stats',
      ].map((value) => ({ value, label: value }))}
      value={tab}
      onValueChange={setTab}
    >
      <p>{tab} panel</p>
    </Tabs>
  )
}
it('links every active tab and panel and supports arrow, Home and End keys', () => {
  render(<TabFixture />)
  const overview = screen.getByRole('tab', { name: 'overview' })
  overview.focus()
  fireEvent.keyDown(overview, { key: 'ArrowRight' })
  const logs = screen.getByRole('tab', { name: 'logs' })
  expect(logs).toHaveFocus()
  expect(logs).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByRole('tabpanel')).toHaveAttribute(
    'aria-labelledby',
    logs.id,
  )
  expect(logs).toHaveAttribute('aria-controls', screen.getByRole('tabpanel').id)
  fireEvent.keyDown(logs, { key: 'End' })
  expect(screen.getByRole('tab', { name: 'stats' })).toHaveFocus()
  fireEvent.keyDown(document.activeElement!, { key: 'Home' })
  expect(overview).toHaveFocus()
  expect(
    screen.getAllByRole('tab').filter((t) => t.tabIndex === 0),
  ).toHaveLength(1)
})
it('opens the menu with keyboard, skips disabled items and restores trigger focus on Escape', async () => {
  const select = vi.fn()
  const user = userEvent.setup()
  render(
    <DropdownMenu
      items={[
        { label: 'Copy ID', onSelect: select },
        { label: 'Disabled', onSelect: select, disabled: true },
        { label: 'Stop', danger: true, onSelect: select },
      ]}
    />,
  )
  const trigger = screen.getByRole('button', { name: 'More actions' })
  trigger.focus()
  fireEvent.keyDown(trigger, { key: 'ArrowDown' })
  expect(screen.getByRole('menuitem', { name: 'Copy ID' })).toHaveFocus()
  fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' })
  expect(screen.getByRole('menuitem', { name: 'Stop' })).toHaveFocus()
  fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
  expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  expect(trigger).toHaveFocus()
  await user.click(trigger)
  await user.click(screen.getByRole('menuitem', { name: 'Copy ID' }))
  expect(select).toHaveBeenCalledOnce()
})

import { expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import DataTable from './DataTable'
it('uses a roving row stop and moves keyboard focus across the virtualized boundary to row 45', () => {
  const rows = Array.from({ length: 100 }, (_, i) => ({
    id: `row-${i}`,
    name: `Resource ${i}`,
  }))
  const select = vi.fn()
  const activate = vi.fn()
  render(
    <DataTable
      rows={rows}
      rowKey={(r) => r.id}
      columns={[{ key: 'name', title: 'Name', render: (r) => r.name }]}
      selection={new Set()}
      onSelect={select}
      onActivate={activate}
    />,
  )
  const row = screen.getByRole('row', { name: /Resource 0$/ })
  act(() => row.focus())
  expect(row).toHaveAttribute('tabindex', '0')
  expect(screen.getByRole('row', { name: /Resource 1$/ })).toHaveAttribute(
    'tabindex',
    '-1',
  )
  for (let i = 0; i < 45; i++)
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' })
  const fortyFive = screen.getByRole('row', { name: /Resource 45$/ })
  expect(fortyFive).toHaveFocus()
  expect(fortyFive).toHaveAttribute('aria-rowindex', '47')
  expect(
    screen.queryByRole('row', { name: /Resource 0$/ }),
  ).not.toBeInTheDocument()
  fireEvent.keyDown(fortyFive, { key: ' ' })
  expect(select).toHaveBeenLastCalledWith(rows[45], false)
  fireEvent.keyDown(fortyFive, { key: 'Enter' })
  expect(activate).toHaveBeenCalledWith(rows[45])
  fireEvent.keyDown(fortyFive, { key: 'End' })
  expect(screen.getByRole('row', { name: /Resource 99$/ })).toHaveFocus()
  fireEvent.keyDown(document.activeElement!, { key: 'Home' })
  expect(screen.getByRole('row', { name: /Resource 0$/ })).toHaveFocus()
})

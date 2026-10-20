import { afterEach, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import DataTable from './DataTable'
afterEach(() => vi.unstubAllGlobals())

const resources = Array.from({ length: 100 }, (_, i) => ({
  id: `row-${i}`,
  name: `Resource ${i}`,
}))
const nameColumn = { key: 'name', title: 'Name', render: (r: typeof resources[number]) => r.name }

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

it('shows every filtered result when the browser already clamped a scrolled viewport', () => {
  const props = { rowKey: (r: typeof resources[number]) => r.id, columns: [nameColumn] }
  const { rerender } = render(<DataTable rows={resources} {...props} />)
  const viewport = screen.getByRole('grid').parentElement!
  fireEvent.scroll(viewport, { target: { scrollTop: 3000 } })
  expect(screen.queryByRole('row', { name: 'Resource 0' })).not.toBeInTheDocument()
  // Shrinking the table can reset the browser's offset without emitting a scroll event.
  viewport.scrollTop = 0
  rerender(<DataTable rows={resources.slice(0, 3)} {...props} />)
  expect(screen.getByRole('row', { name: 'Resource 0' })).toHaveAttribute('tabindex', '0')
  expect(screen.getByRole('row', { name: 'Resource 1' })).toBeInTheDocument()
  expect(screen.getByRole('row', { name: 'Resource 2' })).toBeInTheDocument()
})

it('expands the rendered window to fill tall viewports and pages by the visible height', () => {
  let resize = () => {}
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: () => void) { resize = callback }
    observe() {}
    disconnect() {}
  })
  render(<DataTable rows={resources} rowKey={(r) => r.id} columns={[nameColumn]} />)
  const viewport = screen.getByRole('grid').parentElement!
  Object.defineProperty(viewport, 'clientHeight', { configurable: true, value: 2200 })
  act(() => resize())
  expect(screen.getByRole('row', { name: 'Resource 49' })).toBeInTheDocument()

  Object.defineProperty(viewport, 'clientHeight', { configurable: true, value: 440 })
  act(() => resize())
  const first = screen.getByRole('row', { name: 'Resource 0' })
  act(() => first.focus())
  fireEvent.keyDown(first, { key: 'PageDown' })
  expect(screen.getByRole('row', { name: 'Resource 9' })).toHaveFocus()
  fireEvent.keyDown(document.activeElement!, { key: 'PageUp' })
  expect(first).toHaveFocus()
})

it('retains a keyboard entry point when scrolling moves the active row out of the window', () => {
  render(<DataTable rows={resources} rowKey={(r) => r.id} columns={[nameColumn]} />)
  fireEvent.scroll(screen.getByRole('grid').parentElement!, { target: { scrollTop: 3000 } })
  const tabStops = screen.getAllByRole('row').filter((row) => row.tabIndex === 0)
  expect(tabStops).toHaveLength(1)
  act(() => tabStops[0].focus())
  fireEvent.keyDown(tabStops[0], { key: 'ArrowDown' })
  expect(screen.getByRole('row', { name: 'Resource 61' })).toHaveFocus()
})

it('lets nested buttons handle Space and arrow keys without selecting or moving the row', async () => {
  const user = userEvent.setup()
  const select = vi.fn()
  const activate = vi.fn()
  render(<DataTable rows={resources.slice(0, 2)} rowKey={(r) => r.id} onSelect={select}
    columns={[nameColumn, { key: 'action', title: 'Actions', render: (r) => <button onClick={() => activate(r)}>Open {r.id}</button> }]} />)
  const button = screen.getByRole('button', { name: 'Open row-0' })
  act(() => button.focus())
  await user.keyboard(' {ArrowDown}')
  expect(activate).toHaveBeenCalledWith(resources[0])
  expect(select).not.toHaveBeenCalled()
  expect(button).toHaveFocus()
})

it('offsets consecutive pinned columns past selection and previous pinned widths', () => {
  render(<DataTable rows={resources.slice(0, 1)} rowKey={(r) => r.id} onSelect={() => {}}
    columns={[
      { ...nameColumn, width: 200, pinned: true },
      { key: 'id', title: 'ID', width: 120, pinned: true, render: (r) => r.id },
    ]} />)
  expect(screen.getByRole('columnheader', { name: 'Name' })).toHaveStyle({ left: '44px' })
  expect(screen.getByRole('columnheader', { name: 'ID' })).toHaveStyle({ left: '244px' })
  expect(screen.getByRole('cell', { name: 'row-0' })).toHaveStyle({ left: '244px' })
})

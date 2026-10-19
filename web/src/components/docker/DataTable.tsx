import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { focusRing } from '../ui/focusRing'
export interface Column<T> {
  key: string
  title: string
  render: (row: T) => ReactNode
  sortable?: boolean
}
export default function DataTable<T>({
  rows,
  columns,
  rowKey,
  selection,
  onSelect,
  onSort,
  onActivate,
  onSelectAll,
  allSelected,
  sort,
}: {
  rows: T[]
  columns: Column<T>[]
  rowKey: (r: T) => string
  selection?: Set<string>
  onSelect?: (r: T, range?: boolean) => void
  onSort?: (key: string) => void
  onActivate?: (row: T) => void
  onSelectAll?: () => void
  allSelected?: boolean
  sort?: { key: string; direction: string }
}) {
  const [top, setTop] = useState(0)
  const [activeKey, setActiveKey] = useState<string>()
  const viewport = useRef<HTMLDivElement>(null)
  const rowElements = useRef(new Map<string, HTMLTableRowElement>())
  const focusPending = useRef(false)
  const height = 44
  const start = Math.min(
    Math.max(0, rows.length - 1),
    Math.max(0, Math.floor(top / height) - 8),
  )
  const visible = rows.slice(start, start + 40)
  const activeIndex = Math.max(
    0,
    rows.findIndex((row) => rowKey(row) === activeKey),
  )
  function focusRow(index: number) {
    const bounded = Math.min(rows.length - 1, Math.max(0, index))
    if (!rows[bounded]) return
    focusPending.current = true
    setActiveKey(rowKey(rows[bounded]))
    if (bounded < start || bounded >= start + visible.length) {
      const position = Math.max(0, (bounded - 8) * height)
      if (viewport.current) viewport.current.scrollTop = position
      setTop(position)
    }
  }
  useLayoutEffect(() => {
    if (!focusPending.current) return
    const element =
      rows[activeIndex] && rowElements.current.get(rowKey(rows[activeIndex]))
    if (element) {
      element.focus()
      element.scrollIntoView?.({ block: 'nearest' })
      focusPending.current = false
    }
  }, [activeKey, start, rows])
  return (
    <div
      ref={viewport}
      className="overflow-auto rounded-lg border border-border max-h-[68vh]"
      onScroll={(e) => setTop(e.currentTarget.scrollTop)}
    >
      <table
        role="grid"
        aria-rowcount={rows.length + 1}
        aria-colcount={columns.length + (onSelect ? 1 : 0)}
        className="w-full text-sm whitespace-nowrap"
      >
        <thead className="sticky top-0 z-10 bg-raised-2 text-text-muted">
          <tr role="row">
            {onSelect && (
              <th scope="col" className="p-3">
                {onSelectAll ? (
                  <input
                    type="checkbox"
                    aria-label="Select all matching rows"
                    checked={allSelected ?? false}
                    onChange={onSelectAll}
                  />
                ) : (
                  <span className="sr-only">Select</span>
                )}
              </th>
            )}
            {columns.map((c) => (
              <th
                scope="col"
                key={c.key}
                className="px-3 py-3 text-left"
                aria-sort={
                  onSort && c.sortable !== false
                    ? sort?.key === c.key
                      ? sort.direction === 'desc'
                        ? 'descending'
                        : 'ascending'
                      : 'none'
                    : undefined
                }
              >
                {onSort && c.sortable !== false ? (
                  <button onClick={() => onSort(c.key)}>
                    {c.title}
                    {sort?.key === c.key && (
                      <span aria-hidden="true">
                        {sort.direction === 'desc' ? ' ↓' : ' ↑'}
                      </span>
                    )}
                  </button>
                ) : (
                  c.title
                )}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {start > 0 && (
            <tr aria-hidden="true">
              <td
                colSpan={columns.length + 1}
                style={{ height: start * height }}
              />
            </tr>
          )}
          {visible.map((row, index) => (
            <tr
              role="row"
              aria-rowindex={start + index + 2}
              key={rowKey(row)}
              ref={(element) => {
                if (element) rowElements.current.set(rowKey(row), element)
                else rowElements.current.delete(rowKey(row))
              }}
              tabIndex={start + index === activeIndex ? 0 : -1}
              aria-selected={
                onSelect ? (selection?.has(rowKey(row)) ?? false) : undefined
              }
              className={`border-t border-border hover:bg-raised-2 focus:bg-raised-2 ${focusRing}`}
              style={{ height }}
              onFocus={() => setActiveKey(rowKey(row))}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && e.target === e.currentTarget) {
                  e.preventDefault()
                  onActivate?.(row)
                }
                if (
                  e.key === ' ' &&
                  onSelect &&
                  !(e.target instanceof HTMLInputElement)
                ) {
                  e.preventDefault()
                  onSelect(row, e.shiftKey)
                }
                if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) {
                  e.preventDefault()
                  focusRow(
                    e.key === 'Home'
                      ? 0
                      : e.key === 'End'
                        ? rows.length - 1
                        : start + index + (e.key === 'ArrowDown' ? 1 : -1),
                  )
                }
              }}
            >
              {onSelect && (
                <td className="p-3">
                  <input
                    type="checkbox"
                    aria-label={`Select ${rowKey(row)}`}
                    checked={selection?.has(rowKey(row)) ?? false}
                    onChange={(event) =>
                      onSelect(row, (event.nativeEvent as MouseEvent).shiftKey)
                    }
                  />
                </td>
              )}
              {columns.map((c) => (
                <td key={c.key} className="px-3 py-2 max-w-72 truncate">
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
          {start + visible.length < rows.length && (
            <tr aria-hidden="true">
              <td
                colSpan={columns.length + 1}
                style={{
                  height: (rows.length - start - visible.length) * height,
                }}
              />
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { focusRing } from '../ui/focusRing'
export interface Column<T> {
  key: string
  title: string
  render: (row: T) => ReactNode
  sortable?: boolean
  width?: number
  grow?: boolean
  pinned?: boolean
  align?: 'right'
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
  const [viewportHeight, setViewportHeight] = useState(640)
  const [activeKey, setActiveKey] = useState<string>()
  const viewport = useRef<HTMLDivElement>(null)
  const rowElements = useRef(new Map<string, HTMLTableRowElement>())
  const focusPending = useRef(false)
  const height = 44
  const columnCount = columns.length + (onSelect ? 1 : 0)
  const sized = columns.every((c) => c.width !== undefined)
  const minimumWidth = columns.reduce(
    (sum, column) => sum + (column.width ?? 0),
    onSelect ? 44 : 0,
  )
  const pinnedOnly =
    sized && columns.length > 0 && columns.every((c) => c.pinned && !c.grow)
  const pinnedStyle = (column: Column<T>, index: number) =>
    column.pinned
      ? {
          left: columns
            .slice(0, index)
            .reduce(
              (offset, previous) =>
                offset + (previous.pinned ? (previous.width ?? 0) : 0),
              onSelect ? 44 : 0,
            ),
        }
      : undefined
  useEffect(() => {
    const element = viewport.current
    if (!element || typeof ResizeObserver === 'undefined') return
    setViewportHeight(element.clientHeight)
    const observer = new ResizeObserver(() =>
      setViewportHeight(element.clientHeight),
    )
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  useLayoutEffect(() => {
    const element = viewport.current
    if (!element) return
    const max = Math.max(0, (rows.length + 1) * height - element.clientHeight)
    const nextTop = Math.min(element.scrollTop, max)
    element.scrollTop = nextTop
    // Browsers may clamp scrollTop before this effect after a filter removes rows.
    // Keep the virtual window in sync even when no scroll event was emitted.
    setTop(nextTop)
  }, [rows.length, viewportHeight])
  const start = Math.min(
    Math.max(0, rows.length - 1),
    Math.max(0, Math.floor(top / height) - 8),
  )
  const visible = rows.slice(
    start,
    start + Math.max(40, Math.ceil(viewportHeight / height) + 16),
  )
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
      className="isolate min-w-0 overflow-auto rounded-lg border border-border max-h-[68vh] [scrollbar-gutter:stable]"
      onScroll={(e) => setTop(e.currentTarget.scrollTop)}
    >
      <table
        role="grid"
        aria-rowcount={rows.length + 1}
        aria-colcount={columnCount}
        className={`w-full text-sm whitespace-nowrap ${sized ? 'table-fixed' : ''}`}
        style={
          sized
            ? {
                minWidth: minimumWidth,
                width: pinnedOnly ? minimumWidth : undefined,
              }
            : undefined
        }
      >
        <colgroup>
          {onSelect && <col style={{ width: 44 }} />}
          {columns.map((c, index) => (
            <col
              key={c.key}
              style={{
                width:
                  c.grow ||
                  (sized &&
                    !columns.some((column) => column.grow) &&
                    !c.pinned &&
                    index === columns.length - 1)
                    ? undefined
                    : c.width,
              }}
            />
          ))}
        </colgroup>
        <thead className="sticky top-0 z-20 bg-raised-2 text-text-muted">
          <tr role="row">
            {onSelect && (
              <th scope="col" className="sticky left-0 z-10 bg-raised-2 p-3">
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
            {columns.map((c, columnIndex) => (
              <th
                scope="col"
                key={c.key}
                className={`px-3 py-3 font-medium ${c.align === 'right' ? 'text-right' : 'text-left'} ${c.pinned ? 'sticky z-10 bg-raised-2' : ''}`}
                style={pinnedStyle(c, columnIndex)}
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
                  <button
                    className={`rounded ${focusRing}`}
                    onClick={() => onSort(c.key)}
                  >
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
              <td colSpan={columnCount} style={{ height: start * height }} />
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
              tabIndex={
                start + index ===
                Math.min(
                  start + visible.length - 1,
                  Math.max(start, activeIndex),
                )
                  ? 0
                  : -1
              }
              aria-selected={
                onSelect ? (selection?.has(rowKey(row)) ?? false) : undefined
              }
              className={`group border-t border-border bg-raised hover:bg-raised-2 focus-within:bg-raised-2 ${selection?.has(rowKey(row)) ? 'bg-raised-2' : ''} ${focusRing}`}
              style={{ height }}
              onFocus={() => setActiveKey(rowKey(row))}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && e.target === e.currentTarget) {
                  e.preventDefault()
                  onActivate?.(row)
                }
                if (e.key === ' ' && onSelect && e.target === e.currentTarget) {
                  e.preventDefault()
                  onSelect(row, e.shiftKey)
                }
                if (
                  e.target === e.currentTarget &&
                  [
                    'ArrowDown',
                    'ArrowUp',
                    'Home',
                    'End',
                    'PageDown',
                    'PageUp',
                  ].includes(e.key)
                ) {
                  e.preventDefault()
                  focusRow(
                    e.key === 'Home'
                      ? 0
                      : e.key === 'End'
                        ? rows.length - 1
                        : start +
                          index +
                          (e.key.startsWith('Page')
                            ? (e.key === 'PageDown' ? 1 : -1) *
                              Math.max(
                                1,
                                Math.floor(viewportHeight / height) - 1,
                              )
                            : e.key === 'ArrowDown'
                              ? 1
                              : -1),
                  )
                }
              }}
            >
              {onSelect && (
                <td className="sticky left-0 z-10 bg-inherit px-3 py-2">
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
              {columns.map((c, columnIndex) => (
                <td
                  key={c.key}
                  style={pinnedStyle(c, columnIndex)}
                  className={`px-3 py-2 truncate ${sized ? '' : 'max-w-72'} ${c.align === 'right' ? 'text-right tabular-nums' : ''} ${c.pinned ? 'sticky z-10 bg-inherit border-r border-border' : ''}`}
                >
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
          {start + visible.length < rows.length && (
            <tr aria-hidden="true">
              <td
                colSpan={columnCount}
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

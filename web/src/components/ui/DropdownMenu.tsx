import { useEffect, useId, useRef, useState } from 'react'
import { Button } from './Button'
import { focusRing } from './focusRing'
export interface MenuItem {
  label: string
  onSelect: () => void
  disabled?: boolean
  danger?: boolean
  title?: string
}

/** A resource action menu with roving focus and focus returned to its trigger. */
export function DropdownMenu({
  label = 'More',
  items,
}: {
  label?: string
  items: MenuItem[]
}) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const id = useId()
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const buttons = useRef<(HTMLButtonElement | null)[]>([])
  const enabled = items
    .map((item, index) => (item.disabled ? -1 : index))
    .filter((index) => index >= 0)
  function close(restore = true) {
    setOpen(false)
    if (restore) trigger.current?.focus()
  }
  function show(last = false) {
    const next = last ? enabled[enabled.length - 1] : enabled[0]
    setActive(next ?? 0)
    setOpen(true)
  }
  useEffect(() => {
    if (open) buttons.current[active]?.focus()
  }, [open, active])
  useEffect(() => {
    if (!open) return
    const outside = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) close(false)
    }
    document.addEventListener('pointerdown', outside)
    return () => document.removeEventListener('pointerdown', outside)
  }, [open])
  return (
    <div ref={root} className="relative">
      <Button
        ref={trigger}
        variant="secondary"
        aria-label={label === 'More' ? 'More actions' : label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => (open ? close() : show())}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
            e.preventDefault()
            show(e.key === 'ArrowUp')
          }
        }}
      >
        {label}
      </Button>
      {open && (
        <div
          id={id}
          role="menu"
          aria-label={label}
          className="absolute right-0 top-full z-(--z-dropdown) mt-2 min-w-48 rounded-lg border border-border-strong bg-raised p-1 shadow-xl"
          onBlur={(e) => {
            if (
              !e.currentTarget.contains(e.relatedTarget) &&
              e.relatedTarget !== trigger.current
            )
              close(false)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.preventDefault()
              e.stopPropagation()
              close()
            }
            if (e.key === 'Tab') close(false)
            if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) {
              e.preventDefault()
              const current = enabled.indexOf(active)
              setActive(
                e.key === 'Home'
                  ? enabled[0]
                  : e.key === 'End'
                    ? enabled[enabled.length - 1]
                    : enabled[
                        (current +
                          (e.key === 'ArrowDown' ? 1 : -1) +
                          enabled.length) %
                          enabled.length
                      ],
              )
            }
          }}
        >
          {items.map((item, index) => (
            <button
              ref={(element) => {
                buttons.current[index] = element
              }}
              key={item.label}
              type="button"
              role="menuitem"
              tabIndex={index === active ? 0 : -1}
              title={item.title}
              disabled={item.disabled}
              className={`block w-full rounded px-3 py-2 text-left text-sm disabled:opacity-50 hover:bg-raised-2 ${focusRing} ${item.danger ? 'text-danger-fg' : 'text-text'} ${item.danger && index > 0 && !items[index - 1].danger ? 'border-t border-border mt-1' : ''}`}
              onFocus={() => setActive(index)}
              onClick={() => {
                close()
                item.onSelect()
              }}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

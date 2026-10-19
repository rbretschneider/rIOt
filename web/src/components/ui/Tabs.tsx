import { useId, useRef, type ReactNode } from 'react'
import { focusRing } from './focusRing'

export interface ResourceTab {
  value: string
  label: string
  disabled?: boolean
  title?: string
}

/** Controlled resource tabs, with one tab stop and linked active panel. */
export function Tabs({
  label,
  tabs,
  value,
  onValueChange,
  children,
}: {
  label: string
  tabs: ResourceTab[]
  value: string
  onValueChange: (value: string) => void
  children: ReactNode
}) {
  const id = useId()
  const refs = useRef<(HTMLButtonElement | null)[]>([])
  function move(key: string) {
    const enabled = tabs
      .map((t, i) => (t.disabled ? -1 : i))
      .filter((i) => i >= 0)
    const current = enabled.indexOf(tabs.findIndex((t) => t.value === value))
    const next =
      key === 'Home'
        ? enabled[0]
        : key === 'End'
          ? enabled[enabled.length - 1]
          : enabled[
              (current + (key === 'ArrowRight' ? 1 : -1) + enabled.length) %
                enabled.length
            ]
    if (next === undefined) return
    onValueChange(tabs[next].value)
    refs.current[next]?.focus()
  }
  return (
    <div className="space-y-4">
      <div role="tablist" aria-label={label} className="flex flex-wrap gap-2">
        {tabs.map((tab, index) => (
          <button
            key={tab.value}
            ref={(element) => {
              refs.current[index] = element
            }}
            id={`${id}-tab-${tab.value}`}
            type="button"
            role="tab"
            title={tab.title}
            disabled={tab.disabled}
            tabIndex={tab.value === value ? 0 : -1}
            aria-selected={tab.value === value}
            aria-controls={`${id}-panel-${tab.value}`}
            className={`rounded px-3 py-2 text-sm transition-colors disabled:opacity-50 ${focusRing} ${tab.value === value ? 'bg-raised-2 text-text' : 'text-text-muted hover:text-text'}`}
            onClick={() => onValueChange(tab.value)}
            onKeyDown={(event) => {
              if (
                ['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)
              ) {
                event.preventDefault()
                move(event.key)
              }
            }}
          >
            {tab.label}
          </button>
        ))}
      </div>
      <div
        id={`${id}-panel-${value}`}
        role="tabpanel"
        tabIndex={0}
        aria-labelledby={`${id}-tab-${value}`}
        className={`rounded-lg border border-border p-5 space-y-5 ${focusRing}`}
      >
        {children}
      </div>
    </div>
  )
}

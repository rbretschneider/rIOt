import { useState, useMemo } from 'react'
import { eventCategory, type EventCategory } from '../../utils/eventCategory'
import type { Event } from '../../types/models'

interface Props {
  events: Event[]
}

type ChipKey = Exclude<EventCategory, 'other'>

/**
 * Severity → Tailwind class lookup. Uses a whitelist (never raw string interpolation)
 * per SEC-005 defense-in-depth note — prevents CSS class injection from agent-controlled
 * event.severity values.
 */
const SEVERITY_CLASSES: Record<string, { bg: string; text: string; label: string }> = {
  info:     { bg: 'bg-gray-700',   text: 'text-gray-300',   label: 'Info' },
  warning:  { bg: 'bg-amber-900/60',  text: 'text-amber-300',  label: 'Warning' },
  critical: { bg: 'bg-red-900/60',    text: 'text-red-300',    label: 'Critical' },
}

const DEFAULT_SEVERITY = SEVERITY_CLASSES.info

const CHIP_LABELS: Record<ChipKey, string> = {
  alerts:   'Alerts',
  docker:   'Docker',
  agent:    'Agent',
  security: 'Security',
}

const CHIP_KEYS: ChipKey[] = ['alerts', 'docker', 'agent', 'security']

/**
 * FR-050 / FR-051 / FR-052 / FR-053 / FR-054 / AC-023 / AC-024 / AC-025 / AC-026
 *
 * Reads from the ['events'] React Query cache (passed as a prop from Dashboard.tsx
 * which reads the cache). Applies chip filters client-side. Caps at 20 events.
 * Uses a CSS enter class for fade-in on new events at top.
 */
export default function ActivityRiver({ events }: Props) {
  const [activeChips, setActiveChips] = useState<Set<ChipKey>>(new Set())

  function toggleChip(key: ChipKey) {
    setActiveChips(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  // Apply filter and cap at 20 (FR-053 / AC-025)
  const visibleEvents = useMemo(() => {
    let filtered = events
    if (activeChips.size > 0) {
      filtered = events.filter(e => {
        const cat = eventCategory(e)
        // "alerts" chip matches warning/critical from any category
        if (activeChips.has('alerts') && (e.severity === 'warning' || e.severity === 'critical')) return true
        // Other chips use the category directly
        return cat !== 'other' && activeChips.has(cat as ChipKey)
      })
    }
    return filtered.slice(0, 20)
  }, [events, activeChips])

  return (
    <div
      className="bg-gray-900/60 border border-gray-800 rounded-xl p-4"
      data-testid="activity-river"
      role="region"
      aria-label="Activity river"
    >
      {/* Header + chips */}
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="text-sm font-medium text-gray-300">Activity</span>
        {CHIP_KEYS.map(key => (
          <button
            key={key}
            onClick={() => toggleChip(key)}
            className={`px-2 py-0.5 rounded text-xs font-medium transition-colors ${
              activeChips.has(key)
                ? 'bg-blue-700 text-white'
                : 'bg-gray-800 text-gray-400 hover:text-white'
            }`}
            aria-pressed={activeChips.has(key)}
            aria-label={`Filter by ${CHIP_LABELS[key]}`}
            data-testid={`river-chip-${key}`}
          >
            {CHIP_LABELS[key]}
          </button>
        ))}
      </div>

      {/* Event list */}
      <div className="space-y-1 max-h-48 overflow-y-auto scrollbar-thin" data-testid="river-events">
        {visibleEvents.length === 0 && (
          <p className="text-sm text-gray-600 py-2 text-center">No events match the selected filters</p>
        )}
        {visibleEvents.map((e, i) => {
          const style = SEVERITY_CLASSES[e.severity] ?? DEFAULT_SEVERITY
          const isNew = i === 0
          return (
            <div
              key={e.id}
              className={`flex items-start gap-2 rounded px-2 py-1 text-xs ${style.bg} ${style.text} ${isNew ? 'animate-river-enter' : ''}`}
              role="listitem"
              data-testid={`river-event-${e.id}`}
              aria-label={`${style.label}: ${e.message}`}
            >
              {/* Severity badge — non-color signal per AC-034 / SEC-005 */}
              <span className="shrink-0 font-semibold uppercase tracking-wide text-[9px] mt-0.5 min-w-[36px]">
                {style.label}
              </span>
              <span className="truncate flex-1">{e.message}</span>
              <time
                dateTime={e.created_at}
                className="shrink-0 text-gray-500 tabular-nums"
              >
                {new Date(e.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}
              </time>
            </div>
          )
        })}
      </div>
    </div>
  )
}

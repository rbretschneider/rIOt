import { useEffect, useId, useRef, useState, type Ref } from 'react'
import { autoUpdate, flip, offset, shift, size, useFloating } from '@floating-ui/react'
import { useTimePreference } from '../contexts/TimePreferenceProvider'
import { formatTimestamp, type HourCycle, type TimeZonePref } from '../utils/format'
import { focusRing } from './ui/focusRing'

interface RadioOption<T extends string> {
  value: T
  label: string
}

function RadioGroup<T extends string>({
  heading,
  name,
  value,
  options,
  onChange,
  groupRef,
}: {
  heading: string
  name: string
  value: T
  options: RadioOption<T>[]
  onChange: (value: T) => void
  groupRef?: Ref<HTMLDivElement>
}) {
  const headingId = useId()
  return (
    <div>
      <p id={headingId} className="mb-1 text-xs font-semibold text-text-muted">
        {heading}
      </p>
      <div ref={groupRef} role="radiogroup" aria-labelledby={headingId} className="space-y-1">
        {options.map(option => (
          <label key={option.value} className="flex cursor-pointer items-center gap-2 rounded-md px-1 py-1 text-sm text-text hover:bg-raised-2">
            <input
              type="radio"
              name={name}
              value={option.value}
              checked={value === option.value}
              onChange={() => onChange(option.value)}
              className={`h-3.5 w-3.5 accent-accent ${focusRing}`}
            />
            {option.label}
          </label>
        ))}
      </div>
    </div>
  )
}

const CYCLE_OPTIONS: RadioOption<HourCycle>[] = [
  { value: '12h', label: '12-hour' },
  { value: '24h', label: '24-hour' },
]

/**
 * Header clock and the entry point for the app-wide time display. The button
 * shows the current time in the chosen zone and hour cycle; activating it opens
 * a small non-modal panel with a "Time zone" and a "Clock" radio group (FR-042
 * to FR-046). The panel closes on Escape (focus returns to the button), on an
 * outside press, when focus leaves, and on a second activation. Its one-second
 * tick lives in this component's own state, so only the clock re-renders each second.
 */
export default function HeaderClock() {
  const { zone, hourCycle, zoneOpts, setZone, setHourCycle, serverZone, localZoneName, now } = useTimePreference()
  const [nowMs, setNowMs] = useState(() => now())
  const [open, setOpen] = useState(false)
  const panelId = useId()
  const zoneName = useId()
  const cycleName = useId()
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const zoneGroup = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const id = setInterval(() => setNowMs(now()), 1000)
    return () => clearInterval(id)
  }, [now])

  // Same positioning as PatchBadge: flips above when there is no room below, shifts to
  // stay inside the viewport, and scrolls what still does not fit.
  const { refs, floatingStyles } = useFloating({
    open,
    placement: 'bottom-end',
    strategy: 'fixed',
    whileElementsMounted: autoUpdate,
    middleware: [
      offset(4),
      flip({ padding: 8 }),
      shift({ padding: 8 }),
      size({
        padding: 8,
        apply({ availableHeight, elements }) {
          elements.floating.style.maxHeight = `${Math.max(availableHeight, 120)}px`
        },
      }),
    ],
  })

  useEffect(() => {
    if (!open) return
    zoneGroup.current?.querySelector<HTMLInputElement>('input[type="radio"]:checked')?.focus()
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', outside)
    return () => document.removeEventListener('pointerdown', outside)
  }, [open])

  const zoneDescription =
    zone === 'Server'
      ? `Server (${serverZone ?? 'UTC'})`
      : zone === 'Local'
        ? localZoneName ? `Local (${localZoneName})` : 'Local'
        : 'UTC'
  const cycleDescription = hourCycle === '12h' ? '12-hour' : '24-hour'
  const description = `Time display: ${zoneDescription}, ${cycleDescription}. Change time display`

  const zoneOptions: RadioOption<TimeZonePref>[] = [
    { value: 'Server', label: `Server (${serverZone ?? 'UTC'})` },
    { value: 'Local', label: localZoneName ? `Local (${localZoneName})` : 'Local' },
    { value: 'UTC', label: 'UTC' },
  ]

  const label = formatTimestamp(nowMs, zoneOpts, 'time')

  return (
    <div
      ref={root}
      className="relative"
      onKeyDown={event => {
        if (open && event.key === 'Escape') {
          event.preventDefault()
          event.stopPropagation()
          setOpen(false)
          trigger.current?.focus()
        }
      }}
      onBlur={event => {
        if (open && !event.currentTarget.contains(event.relatedTarget)) setOpen(false)
      }}
    >
      <button
        type="button"
        ref={node => {
          trigger.current = node
          refs.setReference(node)
        }}
        onClick={() => setOpen(value => !value)}
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        aria-label={description}
        title={description}
        className={`whitespace-nowrap rounded-md px-1.5 py-1 font-mono text-xs tabular-nums text-text-muted transition-colors hover:bg-raised-2 hover:text-text ${focusRing}`}
      >
        <time dateTime={new Date(nowMs).toISOString()}>{label}</time>
      </button>
      {open && (
        <div
          ref={refs.setFloating}
          id={panelId}
          role="group"
          aria-label="Time display"
          tabIndex={-1}
          style={floatingStyles}
          className="z-(--z-dropdown) w-64 max-w-[calc(100vw-16px)] space-y-3 overflow-y-auto scrollbar-thin rounded-lg border border-border-strong bg-raised p-3 shadow-xl focus:outline-none"
        >
          <RadioGroup heading="Time zone" name={zoneName} value={zone} options={zoneOptions} onChange={setZone} groupRef={zoneGroup} />
          <RadioGroup heading="Clock" name={cycleName} value={hourCycle} options={CYCLE_OPTIONS} onChange={setHourCycle} />
        </div>
      )}
    </div>
  )
}

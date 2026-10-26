import { useId, useRef, useState } from 'react'
import type { PatchBadge as PatchBadgeData } from '../api/client'

// Per-host patch badge. The label and the four explanation lines are written by
// the server and rendered here as React text only (never as HTML).

const TONE: Record<PatchBadgeData['kind'], string> = {
  auto: 'bg-emerald-900/40 text-emerald-400 border-emerald-800/50',
  auto_reboot_date: 'bg-emerald-900/40 text-emerald-400 border-emerald-800/50',
  auto_reboot_unknown: 'bg-amber-900/30 text-amber-300 border-amber-800/50',
  needs_you: 'bg-red-900/30 text-red-300 border-red-800/50',
  // EXTRA-UPD-001 FR-081: nothing needs Ryan and nothing patches the host by itself. Neutral, never red.
  manual: 'bg-gray-800/60 text-gray-300 border-gray-700/60',
}

const NEUTRAL_TONE = TONE.manual

// One item per code, or per code and updater for the extra_update_* items.
function itemKey(r: { code: string; updater?: string }): string {
  return r.updater ? `${r.code}:${r.updater}` : r.code
}

export default function PatchBadge({ badge }: { badge: PatchBadgeData }) {
  const popoverId = useId()
  const ref = useRef<HTMLSpanElement>(null)
  const [hover, setHover] = useState(false)
  const [focus, setFocus] = useState(false)
  const [dismissed, setDismissed] = useState(false)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  const open = (hover || focus) && !dismissed
  const ex = badge.explanation
  const info = ex.info ?? []

  function show() {
    const r = ref.current?.getBoundingClientRect()
    if (r) setPos({ top: r.bottom + 4, left: Math.max(8, r.left) })
    setDismissed(false)
  }

  return (
    <span className="relative inline-block">
      <span
        ref={ref}
        tabIndex={0}
        data-testid="patch-badge"
        data-kind={badge.kind}
        aria-describedby={open ? popoverId : undefined}
        onMouseEnter={() => { show(); setHover(true) }}
        onMouseLeave={() => { setHover(false); setDismissed(false) }}
        onFocus={() => { show(); setFocus(true) }}
        onBlur={() => { setFocus(false); setDismissed(false) }}
        onKeyDown={e => { if (e.key === 'Escape') setDismissed(true) }}
        className={`px-1.5 py-0.5 rounded border text-[10px] font-medium whitespace-nowrap cursor-help focus:outline-none focus:ring-1 focus:ring-blue-500 ${TONE[badge.kind] ?? NEUTRAL_TONE}`}
      >
        {badge.label}
      </span>
      {open && pos && (
        <div
          id={popoverId}
          role="tooltip"
          style={{ position: 'fixed', top: pos.top, left: pos.left }}
          className="z-50 w-80 max-w-[90vw] rounded-lg border border-gray-700 bg-gray-900 p-3 text-xs text-gray-200 shadow-xl space-y-1.5"
        >
          <p><span className="font-semibold text-gray-400">Automatic patching: </span>{ex.automatic}</p>
          <p><span className="font-semibold text-gray-400">Reboot: </span>{ex.reboot}</p>
          <div>
            <p><span className="font-semibold text-gray-400">Needs you: </span>{ex.needs_you_summary}</p>
            {ex.needs_you.length > 0 && (
              <ul className="mt-1 ml-4 list-disc space-y-1">
                {ex.needs_you.map(r => (
                  <li key={itemKey(r)}>
                    {r.text} <span className="text-gray-400">{r.action}</span>
                    {r.command_id && <span className="block font-mono text-[10px] text-gray-500">Command {r.command_id}</span>}
                  </li>
                ))}
              </ul>
            )}
          </div>
          {info.length > 0 && (
            <div data-testid="patch-badge-info">
              <p><span className="font-semibold text-gray-400">Information: </span>Nothing here needs you.</p>
              <ul className="mt-1 ml-4 list-disc space-y-1 text-gray-300">
                {info.map(r => (
                  <li key={itemKey(r)}>
                    {r.text} <span className="text-gray-500">{r.action}</span>
                    {r.command_id && <span className="block font-mono text-[10px] text-gray-500">Command {r.command_id}</span>}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <p><span className="font-semibold text-gray-400">As of: </span>{ex.as_of}</p>
          {ex.notes.length > 0 && (
            <ul className="border-t border-gray-800 pt-1.5 space-y-1 text-gray-400">
              {ex.notes.map(n => <li key={n}>{n}</li>)}
            </ul>
          )}
        </div>
      )}
    </span>
  )
}

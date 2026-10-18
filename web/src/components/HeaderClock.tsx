import { useEffect, useState } from 'react'
import { useTimePreference } from '../contexts/TimePreferenceProvider'
import { formatTimestamp } from '../utils/format'
import { focusRing } from './ui/focusRing'

/**
 * Header clock. It doubles as the app-wide UTC / Local switch: activating it
 * toggles the time-zone preference. Its one-second tick lives in this
 * component's own state, so only the clock re-renders each second.
 */
export default function HeaderClock() {
  const { zone, zoneOpts, toggleZone, now } = useTimePreference()
  const [nowMs, setNowMs] = useState(() => now())

  useEffect(() => {
    const id = setInterval(() => setNowMs(now()), 1000)
    return () => clearInterval(id)
  }, [now])

  const label = formatTimestamp(nowMs, zoneOpts, 'time')
  const localAbbreviation = label.split(' ').pop()
  const description =
    zone === 'UTC'
      ? 'Time zone: UTC. Switch to local time'
      : `Time zone: local (${localAbbreviation}). Switch to UTC`

  return (
    <button
      type="button"
      onClick={toggleZone}
      aria-label={description}
      title={description}
      className={`whitespace-nowrap rounded-md px-1.5 py-1 font-mono text-xs tabular-nums text-text-muted transition-colors hover:bg-raised-2 hover:text-text ${focusRing}`}
    >
      <time dateTime={new Date(nowMs).toISOString()}>{label}</time>
    </button>
  )
}

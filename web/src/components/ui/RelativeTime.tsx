import { useTimePreference } from '../../contexts/TimePreferenceProvider'
import { useMinuteTick } from '../../hooks/useMinuteTick'
import { formatRelative, formatTimestamp, parseInstant, type InstantInput } from '../../utils/format'

export interface RelativeTimeProps {
  value: InstantInput
  fallback?: string
  className?: string
}

/** "5m ago" text with the absolute, zone-labelled time in the title. Refreshes every 30 s. */
export function RelativeTime({ value, fallback = '—', className }: RelativeTimeProps) {
  const { zoneOpts, now } = useTimePreference()
  useMinuteTick()
  const instant = parseInstant(value)
  if (!instant) return <span className={className}>{fallback}</span>
  return (
    <time
      dateTime={instant.toISOString()}
      title={formatTimestamp(instant, zoneOpts)}
      className={className}
    >
      {formatRelative(instant, now())}
    </time>
  )
}

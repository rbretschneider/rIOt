import { useTimePreference } from '../../contexts/TimePreferenceProvider'
import { useMinuteTick } from '../../hooks/useMinuteTick'
import { formatRelative, formatTimestamp, parseInstant, type InstantInput } from '../../utils/format'

export interface RelativeTimeProps {
  value: InstantInput
  fallback?: string
  className?: string
  /** Clamp a future instant to "just now" (ages such as last-seen; agent clock skew). */
  pastOnly?: boolean
}

/** "5m ago" text with the absolute, zone-labelled time in the title. Refreshes every 30 s. */
export function RelativeTime({ value, fallback = '—', className, pastOnly = false }: RelativeTimeProps) {
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
      {formatRelative(instant, now(), { pastOnly })}
    </time>
  )
}

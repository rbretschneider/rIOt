import { useTimePreference } from '../../contexts/TimePreferenceProvider'
import { formatTimestamp, parseInstant, type InstantInput, type TimestampPrecision } from '../../utils/format'

export interface TimestampProps {
  value: InstantInput
  precision?: TimestampPrecision
  fallback?: string
  className?: string
}

/** Absolute time in the preferred zone, always zone-labelled ("2026-10-17 14:03:22 UTC"). */
export function Timestamp({ value, precision = 'second', fallback = '—', className }: TimestampProps) {
  const { zoneOpts } = useTimePreference()
  const instant = parseInstant(value)
  if (!instant) return <span className={className}>{fallback}</span>
  return (
    <time dateTime={instant.toISOString()} className={className}>
      {formatTimestamp(instant, zoneOpts, precision)}
    </time>
  )
}

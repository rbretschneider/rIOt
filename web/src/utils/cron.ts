/**
 * Parse a timestamp value that may be:
 * - A Unix timestamp as a string (seconds since epoch, e.g. "1710456000")
 * - An ISO 8601 date string
 * - A Windows date string (e.g. "3/14/2026 6:00:00 AM")
 * - Empty/undefined/zero
 *
 * Returns a valid Date or null.
 */
export function parseTimestamp(value: string | undefined): Date | null {
  if (!value || value === '0') return null

  // Try as Unix timestamp (all digits)
  if (/^\d+$/.test(value)) {
    const n = Number(value)
    if (n > 0) {
      // Heuristic: if value > 1e12 it's already milliseconds, otherwise seconds
      const ms = n > 1e12 ? n : n * 1000
      const d = new Date(ms)
      if (!isNaN(d.getTime())) return d
    }
    return null
  }

  // Try as ISO or other parseable string
  const d = new Date(value)
  if (!isNaN(d.getTime())) return d

  return null
}

/** Format a parsed timestamp for display, or return fallback */
export function formatRunDate(value: string | undefined, fallback = '-'): string {
  const d = parseTimestamp(value)
  if (!d) return fallback
  return d.toLocaleString()
}

/**
 * Parse a systemd calendar expression into a human-readable string.
 *
 * Common forms:
 *   *-*-* 00:00:00          → "Daily at midnight"
 *   *-*-* 06:00:00          → "Daily at 06:00"
 *   *-*-* 06,18:00:00       → "Daily at 06:00, 18:00"
 *   *-*-* 00,12:00:00       → "Daily at 00:00, 12:00"
 *   *-*-* *:00/10:00        → "Every 10 minutes"
 *   *-*-* *:00:00           → "Every hour"
 *   *-*-* 00:07:00          → "Daily at 00:07"
 *   Sun *-*-* 03:10:00      → "Every Sunday at 03:10"
 *   Mon *-*-* 00:00:00      → "Every Monday at midnight"
 *   *-*-01 00:00:00         → "Monthly on day 1 at midnight"
 *   *-01-01 00:00:00        → "Yearly on Jan 1 at midnight"
 */
export function parseSystemdCalendar(calendar: string | undefined): string | null {
  if (!calendar || !calendar.trim()) return null

  const s = calendar.trim()

  // Match: [DayOfWeek] Year-Month-Day Hour:Minute:Second
  const match = s.match(
    /^(?:(Mon|Tue|Wed|Thu|Fri|Sat|Sun)(?:,(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun))*)?\s*(\S+)-(\S+)-(\S+)\s+(\S+):(\S+):(\S+)$/
  )
  if (!match) return null

  const [, dow, year, month, day, hour, minute, _second] = match

  const parts: string[] = []

  // Day of week
  const dayOfWeekFull: Record<string, string> = {
    Mon: 'Monday', Tue: 'Tuesday', Wed: 'Wednesday', Thu: 'Thursday',
    Fri: 'Friday', Sat: 'Saturday', Sun: 'Sunday',
  }

  // Frequency prefix
  if (dow) {
    const days = dow.split(',').map(d => dayOfWeekFull[d] || d)
    if (days.length === 1) {
      parts.push(`Every ${days[0]}`)
    } else {
      parts.push(`Every ${days.join(', ')}`)
    }
  }

  // Date part
  const isWildYear = year === '*'
  const isWildMonth = month === '*'
  const isWildDay = day === '*'

  if (!isWildMonth && !isWildDay) {
    const monthNames = ['', 'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
    const m = parseInt(month, 10)
    const monthStr = m >= 1 && m <= 12 ? monthNames[m] : month
    if (isWildYear) {
      parts.push(`Yearly on ${monthStr} ${day}`)
    } else {
      parts.push(`On ${year}-${monthStr}-${day}`)
    }
  } else if (isWildMonth && !isWildDay) {
    parts.push(`Monthly on day ${day}`)
  }

  // Time part
  const timeStr = formatTimePart(hour, minute)
  if (timeStr) {
    if (parts.length === 0) parts.push(timeStr)
    else parts.push(`at ${timeStr.replace(/^(Daily|Every\s\w+)\s+at\s+/, '').replace(/^At /, '').toLowerCase()}`)
  }

  return parts.length > 0 ? parts.join(' ') : null
}

function formatTimePart(hour: string, minute: string): string | null {
  // Every N minutes: hour=*, minute has step like 00/10 or */10
  const minuteStep = minute.match(/^(?:\*|00?)\/(\d+)$/)
  if (hour === '*' && minuteStep) {
    const n = parseInt(minuteStep[1], 10)
    return `Every ${n} minutes`
  }

  // Every hour: hour=*, minute is fixed
  if (hour === '*' && /^\d{1,2}$/.test(minute)) {
    const m = parseInt(minute, 10)
    if (m === 0) return 'Every hour'
    return `Every hour at :${minute.padStart(2, '0')}`
  }

  // Hour step: e.g. */6
  const hourStep = hour.match(/^\*\/(\d+)$/)
  if (hourStep) {
    return `Every ${hourStep[1]} hours`
  }

  // Fixed hour(s), possibly with comma-separated values
  if (/^[\d,]+$/.test(hour)) {
    const hours = hour.split(',')
    const min = /^\d{1,2}$/.test(minute) ? minute.padStart(2, '0') : '00'

    if (hours.length === 1) {
      const h = hours[0].padStart(2, '0')
      const timeLabel = `${h}:${min}`
      if (timeLabel === '00:00') return 'Daily at midnight'
      return `Daily at ${timeLabel}`
    }

    const labels = hours.map(h => `${h.padStart(2, '0')}:${min}`)
    return `Daily at ${labels.join(', ')}`
  }

  return null
}

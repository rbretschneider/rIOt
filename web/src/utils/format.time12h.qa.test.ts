import { describe, expect, it } from 'vitest'
import { acceptServerZone, formatChartTick, formatDateOnly, formatTimestamp, type AcceptedZone, type ZoneOpts } from './format'

// Added by QA Engineer (TIME-12H-001). Cross-checks the dashboard against the server formatter
// (internal/models/display_time.go): both must label a zone the same way, because the same
// instant is shown in the header and in server-written text.

const INSTANT = '2026-10-27T17:05:09Z'

const accepted = (name: string): AcceptedZone => {
  const zone = acceptServerZone(name)
  if (!zone) throw new Error(`test setup: ${name} was not accepted`)
  return zone
}
const server = (name: string, hourCycle: '12h' | '24h' = '12h'): ZoneOpts => ({
  zone: 'Server',
  hourCycle,
  serverTimeZone: accepted(name),
})

describe('TIME-12H-001 [AC-063] numeric zone labels match the server formatter (QA)', () => {
  // Covers AC-063 / FR-034 on the dashboard side. The strings are the ones AC-063 pins in Go.
  it.each([
    ['America/Sao_Paulo', '2026-10-27 2:05:09 PM GMT-3'],
    ['Asia/Kathmandu', '2026-10-27 10:50:09 PM GMT+5:45'],
    ['Asia/Kolkata', '2026-10-27 10:35:09 PM GMT+5:30'], // en-US Intl has no IST; the server writes IST (FRD DEC-7: accepted difference)
    ['Etc/GMT+5', '2026-10-27 12:05:09 PM GMT-5'],
  ])('%s reads %s', (zone, expected) => {
    expect(formatTimestamp(INSTANT, server(zone))).toBe(expected)
  })
})

describe('TIME-12H-001 [AC-011] [AC-012] DST edges across hour cycles and precisions (QA)', () => {
  // Covers AC-011 and AC-012 for the forms the shipped tests do not pin: clock, time and 24-hour.
  const ny = server('America/New_York')
  const ny24 = server('America/New_York', '24h')

  it('fall-back repeats 1:30 in the clock form and the label settles it', () => {
    expect(formatTimestamp('2026-11-01T05:30:00Z', ny, 'clock')).toBe('1:30 AM EDT')
    expect(formatTimestamp('2026-11-01T06:30:00Z', ny, 'clock')).toBe('1:30 AM EST')
  })

  it('fall-back in 24-hour keeps the repeated hour distinguishable', () => {
    expect(formatTimestamp('2026-11-01T05:30:00Z', ny24, 'time')).toBe('01:30:00 EDT')
    expect(formatTimestamp('2026-11-01T06:30:00Z', ny24, 'time')).toBe('01:30:00 EST')
  })

  it('spring-forward skips 2:xx and the chart tick never shows it', () => {
    expect(formatChartTick(Date.parse('2026-03-08T06:59:00Z'), ny)).toBe('1:59 AM')
    expect(formatChartTick(Date.parse('2026-03-08T07:00:00Z'), ny)).toBe('3:00 AM')
  })

  it('the date-only form follows the zone day, not the UTC day, across the boundary', () => {
    expect(formatDateOnly('2026-10-28T02:00:00Z', ny)).toBe('2026-10-27')
    expect(formatDateOnly('2026-10-28T04:00:00Z', ny)).toBe('2026-10-28')
  })
})

describe('TIME-12H-001 [AC-008] [AC-009] midnight and noon in the 12-hour clock form (QA)', () => {
  it('00:xx is 12:xx AM and 12:xx is 12:xx PM, with no 0:xx or 24:xx', () => {
    const ny = server('America/New_York')
    expect(formatTimestamp('2026-10-27T04:00:00Z', ny, 'clock')).toBe('12:00 AM EDT')
    expect(formatTimestamp('2026-10-27T04:59:59Z', ny, 'time')).toBe('12:59:59 AM EDT')
    expect(formatTimestamp('2026-10-27T16:00:00Z', ny, 'clock')).toBe('12:00 PM EDT')
    expect(formatTimestamp('2026-10-27T16:59:59Z', ny, 'time')).toBe('12:59:59 PM EDT')
    expect(formatTimestamp('2026-10-27T17:00:00Z', ny, 'clock')).toBe('1:00 PM EDT')
  })
})

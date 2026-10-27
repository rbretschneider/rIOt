import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  acceptServerZone,
  formatChartTick,
  formatChartTooltip,
  formatDateOnly,
  formatRelative,
  formatTimestamp,
  formatterCacheSize,
  localZoneName,
  type AcceptedZone,
  type HourCycle,
  type TimestampPrecision,
  type TimeZonePref,
  type ZoneOpts,
} from './format'

// TIME-12H-001. Unless stated otherwise: instant 2026-10-27T17:05:09Z, server zone America/New_York,
// Local (browser) zone America/Los_Angeles through the localTimeZone test seam, never process.env.TZ.

const INSTANT = '2026-10-27T17:05:09Z'

const accepted = (name: string): AcceptedZone => {
  const zone = acceptServerZone(name)
  if (!zone) throw new Error(`test setup: ${name} was not accepted`)
  return zone
}
const NEW_YORK = accepted('America/New_York')

const server = (hourCycle: HourCycle): ZoneOpts => ({ zone: 'Server', hourCycle, serverTimeZone: NEW_YORK })
const utc = (hourCycle: HourCycle): ZoneOpts => ({ zone: 'UTC', hourCycle })
const local = (hourCycle: HourCycle): ZoneOpts => ({ zone: 'Local', hourCycle, localTimeZone: 'America/Los_Angeles' })

describe('TIME-12H-001 [AC-001] 12-hour second form', () => {
  it('formats the example instant as YYYY-MM-DD h:mm:ss AM|PM ZONE in the server zone', () => {
    expect(formatTimestamp(INSTANT, server('12h'), 'second')).toBe('2026-10-27 1:05:09 PM EDT')
  })

  it('uses the second form when no precision is given', () => {
    expect(formatTimestamp(INSTANT, server('12h'))).toBe('2026-10-27 1:05:09 PM EDT')
  })

  it('writes the hour without a leading zero and the period in upper case', () => {
    expect(formatTimestamp('2026-10-27T13:05:09Z', server('12h'))).toBe('2026-10-27 9:05:09 AM EDT')
  })
})

describe('TIME-12H-001 [AC-002] 12-hour minute form', () => {
  it('formats the example instant as YYYY-MM-DD h:mm AM|PM ZONE', () => {
    expect(formatTimestamp(INSTANT, server('12h'), 'minute')).toBe('2026-10-27 1:05 PM EDT')
  })
})

describe('TIME-12H-001 [AC-003] 12-hour time form', () => {
  it('formats the example instant as h:mm:ss AM|PM ZONE', () => {
    expect(formatTimestamp(INSTANT, server('12h'), 'time')).toBe('1:05:09 PM EDT')
  })
})

describe('TIME-12H-001 [AC-004] clock form in both hour cycles', () => {
  it('formats 12-hour as h:mm AM|PM ZONE', () => {
    expect(formatTimestamp(INSTANT, server('12h'), 'clock')).toBe('1:05 PM EDT')
  })

  it('formats 24-hour as HH:mm ZONE', () => {
    expect(formatTimestamp(INSTANT, server('24h'), 'clock')).toBe('13:05 EDT')
  })

  it('zero-pads a 24-hour morning hour', () => {
    expect(formatTimestamp('2026-10-27T10:07:00Z', server('24h'), 'clock')).toBe('06:07 EDT')
  })
})

describe('TIME-12H-001 [AC-005] 24-hour forms are unchanged', () => {
  it('formats the example instant at second, minute and time precision exactly as before', () => {
    expect(formatTimestamp(INSTANT, server('24h'), 'second')).toBe('2026-10-27 13:05:09 EDT')
    expect(formatTimestamp(INSTANT, server('24h'), 'minute')).toBe('2026-10-27 13:05 EDT')
    expect(formatTimestamp(INSTANT, server('24h'), 'time')).toBe('13:05:09 EDT')
  })
})

describe('TIME-12H-001 [AC-006] UTC zone in both hour cycles', () => {
  it('formats 12-hour UTC with a literal UTC label', () => {
    expect(formatTimestamp(INSTANT, utc('12h'))).toBe('2026-10-27 5:05:09 PM UTC')
  })

  it('formats 24-hour UTC', () => {
    expect(formatTimestamp(INSTANT, utc('24h'))).toBe('2026-10-27 17:05:09 UTC')
  })

  it('keeps the UIF AC-104 output for 2026-10-17T14:03:22Z in 24-hour UTC', () => {
    expect(formatTimestamp('2026-10-17T14:03:22Z', utc('24h'))).toBe('2026-10-17 14:03:22 UTC')
  })
})

describe('TIME-12H-001 [AC-007] Local zone in both hour cycles', () => {
  it('formats 12-hour Local in Pacific daylight time', () => {
    expect(formatTimestamp(INSTANT, local('12h'))).toBe('2026-10-27 10:05:09 AM PDT')
  })

  it('formats 24-hour Local in Pacific daylight time', () => {
    expect(formatTimestamp(INSTANT, local('24h'))).toBe('2026-10-27 10:05:09 PDT')
  })
})

describe('TIME-12H-001 [AC-008] midnight is 12 AM and 00', () => {
  const midnight = '2026-10-27T04:00:00Z'

  it('renders 12-hour midnight as 12:00:00 AM', () => {
    expect(formatTimestamp(midnight, server('12h'))).toBe('2026-10-27 12:00:00 AM EDT')
  })

  it('renders 24-hour midnight as 00:00:00 and never 24', () => {
    expect(formatTimestamp(midnight, server('24h'))).toBe('2026-10-27 00:00:00 EDT')
  })
})

describe('TIME-12H-001 [AC-009] noon is 12 PM and 12', () => {
  const noon = '2026-10-27T16:00:00Z'

  it('renders 12-hour noon as 12:00:00 PM', () => {
    expect(formatTimestamp(noon, server('12h'))).toBe('2026-10-27 12:00:00 PM EDT')
  })

  it('renders 24-hour noon as 12:00:00', () => {
    expect(formatTimestamp(noon, server('24h'))).toBe('2026-10-27 12:00:00 EDT')
  })
})

describe('TIME-12H-001 [AC-010] the server-zone date differs from the UTC date', () => {
  it('renders 03:59:59Z on the 28th as 11:59:59 PM on the 27th', () => {
    expect(formatTimestamp('2026-10-28T03:59:59Z', server('12h'))).toBe('2026-10-27 11:59:59 PM EDT')
  })

  it('renders 02:00:00Z on the 28th as 10:00:00 PM on the 27th', () => {
    expect(formatTimestamp('2026-10-28T02:00:00Z', server('12h'))).toBe('2026-10-27 10:00:00 PM EDT')
  })

  it('formatDateOnly returns the server-zone date, not the UTC date', () => {
    expect(formatDateOnly('2026-10-28T02:00:00Z', { ...server('12h'), zone: 'Server' })).toBe('2026-10-27')
    expect(formatDateOnly('2026-10-28T02:00:00Z', utc('12h'))).toBe('2026-10-28')
  })
})

describe('TIME-12H-001 [AC-011] DST fall-back repeats 1:30 AM as EDT then EST', () => {
  it('labels the first 1:30 AM EDT', () => {
    expect(formatTimestamp('2026-11-01T05:30:00Z', server('12h'))).toBe('2026-11-01 1:30:00 AM EDT')
  })

  it('labels the repeated 1:30 AM EST', () => {
    expect(formatTimestamp('2026-11-01T06:30:00Z', server('12h'))).toBe('2026-11-01 1:30:00 AM EST')
  })
})

describe('TIME-12H-001 [AC-012] DST spring-forward skips from 1:59:59 AM EST to 3:00:00 AM EDT', () => {
  it('renders the last second before the gap in standard time', () => {
    expect(formatTimestamp('2026-03-08T06:59:59Z', server('12h'))).toBe('2026-03-08 1:59:59 AM EST')
  })

  it('renders the first second after the gap in daylight time', () => {
    expect(formatTimestamp('2026-03-08T07:00:00Z', server('12h'))).toBe('2026-03-08 3:00:00 AM EDT')
  })
})

describe('TIME-12H-001 [AC-013] chart ticks follow the hour cycle without a label and tooltips carry it', () => {
  const ts = Date.parse(INSTANT)

  it('ticks are h:mm AM|PM in 12-hour form', () => {
    expect(formatChartTick(ts, server('12h'))).toBe('1:05 PM')
  })

  it('ticks are HH:mm in 24-hour form', () => {
    expect(formatChartTick(ts, server('24h'))).toBe('13:05')
  })

  it('renders a 12-hour midnight tick as 12:00 AM', () => {
    expect(formatChartTick(Date.parse('2026-10-27T04:00:00Z'), server('12h'))).toBe('12:00 AM')
  })

  it('no tick contains a zone label', () => {
    for (const cycle of ['12h', '24h'] as const) {
      expect(formatChartTick(ts, server(cycle)), `${cycle} tick`).not.toMatch(/EDT|EST|UTC|GMT/)
    }
  })

  it('the 12-hour tooltip is the second form with its zone label', () => {
    expect(formatChartTooltip(ts, server('12h'))).toBe('2026-10-27 1:05:09 PM EDT')
  })
})

describe('TIME-12H-001 [AC-014] invalid input renders an em dash in every form and never throws', () => {
  const inputs: Array<[string, string | undefined]> = [
    ['"not-a-date"', 'not-a-date'],
    ['undefined', undefined],
    ['""', ''],
  ]
  const precisions: TimestampPrecision[] = ['second', 'minute', 'time', 'clock']
  const cycles: HourCycle[] = ['12h', '24h']
  const zones: TimeZonePref[] = ['Server', 'UTC', 'Local']

  it.each(inputs)('formatTimestamp(%s) is an em dash for every precision, zone and hour cycle', (_label, value) => {
    for (const cycle of cycles) {
      for (const zone of zones) {
        const opts: ZoneOpts = { zone, hourCycle: cycle, serverTimeZone: NEW_YORK, localTimeZone: 'America/Los_Angeles' }
        for (const precision of precisions) {
          expect(() => formatTimestamp(value, opts, precision)).not.toThrow()
          expect(formatTimestamp(value, opts, precision), `${zone}/${cycle}/${precision}`).toBe('—')
        }
        expect(formatDateOnly(value, opts), `${zone}/${cycle} date only`).toBe('—')
      }
    }
  })

  it.each(inputs)('formatChartTick(%s) is an em dash in both hour cycles', (_label, value) => {
    for (const cycle of cycles) {
      expect(() => formatChartTick(value as unknown as number, server(cycle))).not.toThrow()
      expect(formatChartTick(value as unknown as number, server(cycle))).toBe('—')
      expect(formatChartTooltip(value as unknown as number, server(cycle))).toBe('—')
    }
  })

  it('a non-finite tick is an em dash', () => {
    expect(formatChartTick(Number.NaN, server('12h'))).toBe('—')
  })
})

describe('TIME-12H-001 [AC-015] relative times are identical in every zone and hour cycle', () => {
  const NOW = Date.parse('2026-10-27T17:05:09Z')
  const cases: Array<[string, number, string]> = [
    ['30 s ago', NOW - 30_000, 'just now'],
    ['5 min ago', NOW - 5 * 60_000, '5m ago'],
    ['3 h ago', NOW - 3 * 3_600_000, '3h ago'],
    ['2 d ago', NOW - 2 * 86_400_000, '2d ago'],
    ['10 min ahead', NOW + 10 * 60_000, 'in 10m'],
  ]

  it.each(cases)('%s reads as the same text under all six zone and hour-cycle combinations', (_label, instant, expected) => {
    for (const cycle of ['12h', '24h'] as const) {
      for (const opts of [server(cycle), utc(cycle), local(cycle)]) {
        // formatRelative takes no zone input at all; the loop proves the preference cannot reach it.
        expect(formatRelative(instant, NOW), `${opts.zone}/${cycle}`).toBe(expected)
      }
    }
  })
})

describe('TIME-12H-001 [AC-018] acceptServerZone rejects every malformed value', () => {
  const rejected: Array<[string, unknown]> = [
    ['an unknown IANA name', 'Mars/Olympus'],
    ['Local', 'Local'],
    ['an empty string', ''],
    ['whitespace only', '   '],
    ['a leading space', ' America/New_York'],
    ['a trailing newline', 'America/New_York\n'],
    ['markup', '<img src=x>'],
    ['path traversal', '../etc/passwd'],
    ['65 letters', 'a'.repeat(65)],
    ['the number 5', 5],
    ['null', null],
    ['undefined', undefined],
    ['an object', { zone: 'America/New_York' }],
    ['four segments', 'A/B/C/D'],
    ['a segment starting with a digit', 'America/1York'],
  ]

  it.each(rejected)('rejects %s', (_label, value) => {
    expect(() => acceptServerZone(value)).not.toThrow()
    expect(acceptServerZone(value)).toBeNull()
  })

  it('accepts a real IANA name and returns it unchanged', () => {
    expect(acceptServerZone('America/New_York')).toBe('America/New_York')
    expect(acceptServerZone('UTC')).toBe('UTC')
    expect(acceptServerZone('America/Argentina/Buenos_Aires')).toBe('America/Argentina/Buenos_Aires')
  })

  it('rejects a 64-character name with a valid shape that Intl does not know (length is not the only gate)', () => {
    expect(acceptServerZone('a'.repeat(64))).toBeNull()
  })

  it('a rejected zone formats as UTC through the Server preference', () => {
    const opts: ZoneOpts = { zone: 'Server', hourCycle: '12h', serverTimeZone: acceptServerZone('Mars/Olympus') }
    expect(formatTimestamp(INSTANT, opts)).toBe('2026-10-27 5:05:09 PM UTC')
  })
})

describe('TIME-12H-001 [AC-020] Intl formats only with UTC, accepted, seam or browser zones', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.resetModules()
  })

  interface Call {
    timeZone: string | undefined
    keys: string[]
    formatting: boolean
  }

  async function withSpy() {
    const Original = Intl.DateTimeFormat
    const calls: Call[] = []
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(function (this: unknown, locale?: string | string[], options?: Intl.DateTimeFormatOptions) {
      calls.push({
        timeZone: options?.timeZone,
        keys: Object.keys(options ?? {}),
        formatting: options?.timeZoneName === 'short',
      })
      return new Original(locale, options)
    } as unknown as typeof Intl.DateTimeFormat)
    vi.resetModules()
    const fresh = await import('./format')
    return { calls, fresh }
  }

  it('a rejected API zone never reaches a formatting construction; the acceptance check is the only caller', async () => {
    const { calls, fresh } = await withSpy()

    const hostile = fresh.acceptServerZone('Mars/Olympus')
    expect(hostile).toBeNull()
    const good = fresh.acceptServerZone('America/New_York')
    const zones: Array<ZoneOpts> = [
      { zone: 'Server', hourCycle: '12h', serverTimeZone: hostile },
      { zone: 'Server', hourCycle: '12h', serverTimeZone: good },
      { zone: 'UTC', hourCycle: '12h' },
      { zone: 'Local', hourCycle: '12h', localTimeZone: 'America/Los_Angeles' },
      { zone: 'Local', hourCycle: '24h' },
    ]
    for (const opts of zones) fresh.formatTimestamp(INSTANT, opts)

    const formatting = calls.filter(c => c.formatting)
    expect(formatting.length, 'formatting constructions were observed').toBeGreaterThan(0)
    const allowed = new Set<string | undefined>(['UTC', 'America/New_York', 'America/Los_Angeles', undefined])
    for (const call of formatting) {
      expect(allowed.has(call.timeZone), `formatting used timeZone ${String(call.timeZone)}`).toBe(true)
    }

    const marsCalls = calls.filter(c => c.timeZone === 'Mars/Olympus')
    expect(marsCalls.length, 'Mars/Olympus reaches Intl only through the acceptance check').toBeGreaterThan(0)
    for (const call of marsCalls) {
      expect(call.formatting).toBe(false)
      expect(call.keys, 'the acceptance check passes only { timeZone }').toEqual(['timeZone'])
    }
  })

  it('a zone name that came from storage is never used: the Server option has no zone until the API supplies one', async () => {
    const { calls, fresh } = await withSpy()
    fresh.formatTimestamp(INSTANT, { zone: 'Server', hourCycle: '24h', serverTimeZone: null })
    expect(calls.filter(c => c.formatting).map(c => c.timeZone)).toEqual(['UTC'])
  })
})

const MANY_ZONES = [
  'Europe/London', 'Europe/Paris', 'Europe/Berlin', 'Europe/Madrid', 'Europe/Rome', 'Europe/Athens',
  'Asia/Tokyo', 'Asia/Seoul', 'Asia/Kolkata', 'Asia/Kathmandu', 'Asia/Dubai', 'Asia/Singapore',
  'Australia/Sydney', 'Australia/Perth', 'Pacific/Auckland', 'Pacific/Honolulu',
  'America/Denver', 'America/Chicago', 'America/Sao_Paulo', 'America/Halifax', 'America/Anchorage',
]

describe('TIME-12H-001 [AC-071] the formatter cache never exceeds 12 entries', () => {
  it('stays bounded over 1,000 calls per zone option, hour cycle and precision, plus 1,000 rejected zones', () => {
    const precisions: TimestampPrecision[] = ['second', 'minute', 'time', 'clock']
    for (const cycle of ['12h', '24h'] as const) {
      for (const opts of [server(cycle), utc(cycle), local(cycle)]) {
        for (const precision of precisions) {
          for (let i = 0; i < 1000; i++) formatTimestamp(1_793_120_709_000 + i * 1000, opts, precision)
        }
      }
    }
    for (let i = 0; i < 1000; i++) {
      const rejected = acceptServerZone(`Mars/Crater${i}`)
      expect(rejected).toBeNull()
      formatTimestamp(INSTANT, { zone: 'Server', hourCycle: '12h', serverTimeZone: rejected })
    }
    expect(formatterCacheSize(), 'cache entries').toBeLessThanOrEqual(12)
  })

  it('20 distinct accepted zones still leave at most 12 entries', () => {
    const names = MANY_ZONES.filter(name => acceptServerZone(name) !== null).slice(0, 20)
    expect(names.length, 'the runtime knows twenty of the listed zones').toBe(20)
    for (const name of names) {
      formatTimestamp(INSTANT, { zone: 'Server', hourCycle: '12h', serverTimeZone: accepted(name) })
    }
    expect(formatterCacheSize()).toBeLessThanOrEqual(12)
  })

  it('still formats correctly after older entries were evicted', () => {
    for (const name of MANY_ZONES) {
      const zone = acceptServerZone(name)
      if (zone) formatTimestamp(INSTANT, { zone: 'Server', hourCycle: '12h', serverTimeZone: zone })
    }
    expect(formatTimestamp(INSTANT, server('12h'))).toBe('2026-10-27 1:05:09 PM EDT')
  })
})

describe('TIME-12H-001 [FR-034] zone labels', () => {
  it('uses the en-US short name for a zone without a US abbreviation', () => {
    const sao = accepted('America/Sao_Paulo')
    expect(formatTimestamp(INSTANT, { zone: 'Server', hourCycle: '12h', serverTimeZone: sao })).toBe('2026-10-27 2:05:09 PM GMT-3')
  })

  it('shows UTC for a Server zone that is UTC itself', () => {
    const zone = accepted('UTC')
    expect(formatTimestamp(INSTANT, { zone: 'Server', hourCycle: '12h', serverTimeZone: zone })).toBe('2026-10-27 5:05:09 PM UTC')
  })
})

describe('TIME-12H-001 [FR-050] the Server zone is UTC until a zone is accepted', () => {
  it('formats a Server preference without a zone as UTC with the label UTC', () => {
    expect(formatTimestamp(INSTANT, { zone: 'Server', hourCycle: '12h' })).toBe('2026-10-27 5:05:09 PM UTC')
    expect(formatTimestamp(INSTANT, { zone: 'Server', hourCycle: '12h', serverTimeZone: null })).toBe('2026-10-27 5:05:09 PM UTC')
  })
})

describe('TIME-12H-001 [FR-035] a zone Intl rejects through the Local seam renders as UTC and never throws', () => {
  it('falls back to UTC for an unknown localTimeZone', () => {
    const opts: ZoneOpts = { zone: 'Local', hourCycle: '24h', localTimeZone: 'Mars/Olympus' }
    expect(() => formatTimestamp(INSTANT, opts)).not.toThrow()
    expect(formatTimestamp(INSTANT, opts)).toBe('2026-10-27 17:05:09 UTC')
  })
})

describe('TIME-12H-001 [FR-043] localZoneName', () => {
  it('returns the test seam when given', () => {
    expect(localZoneName('America/Los_Angeles')).toBe('America/Los_Angeles')
  })

  it('returns the runtime zone, or an empty string, without the seam', () => {
    expect(typeof localZoneName()).toBe('string')
  })

  it('returns an empty string when the runtime cannot resolve a zone', () => {
    const original = Intl.DateTimeFormat
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(function () {
      throw new Error('no Intl')
    } as unknown as typeof Intl.DateTimeFormat)
    try {
      expect(localZoneName()).toBe('')
    } finally {
      vi.restoreAllMocks()
      expect(Intl.DateTimeFormat).toBe(original)
    }
  })
})

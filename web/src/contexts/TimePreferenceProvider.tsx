import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  acceptServerZone,
  formatChartTick,
  formatChartTooltip,
  formatTimestamp,
  localZoneName as resolveLocalZoneName,
  type AcceptedZone,
  type HourCycle,
  type InstantInput,
  type TimestampPrecision,
  type TimeZonePref,
  type ZoneOpts,
} from '../utils/format'

// App-wide time-display preference: a zone (Server, UTC or Local) and an hour
// cycle (12h or 24h). Both are whitelisted enums: whatever is in storage, only
// the exact enum strings are honoured, so a tampered entry can never reach Intl
// as a zone identifier. The Server zone name comes only from the API, through
// acceptServerZone. The IANA zone used for Local comes only from the
// `localTimeZone` prop, which tests set and production never does.

export const TIME_ZONE_STORAGE_KEY = 'riot-time-zone'
export const HOUR_CYCLE_STORAGE_KEY = 'riot-hour-cycle'

type PreferenceStorage = Pick<Storage, 'getItem' | 'setItem'>

export interface TimePreferenceValue {
  zone: TimeZonePref
  hourCycle: HourCycle
  setZone: (zone: TimeZonePref) => void
  setHourCycle: (cycle: HourCycle) => void
  /** The server display zone once the API reported it and it passed validation; null while unknown or rejected. */
  serverZone: AcceptedZone | null
  /** Validates `raw` (any API value) and stores it when accepted. */
  setServerZone: (raw: unknown) => void
  /** The browser's IANA zone name for the Local option, or '' when unavailable. */
  localZoneName: string
  zoneOpts: ZoneOpts
  now: () => number
}

// Used only when no provider is mounted. Production always mounts the provider
// (main.tsx), whose default is Server / 12h. Keeping UTC / 24h here lets the many
// page tests that render <Timestamp> without a provider keep their strings.
const DEFAULT_VALUE: TimePreferenceValue = {
  zone: 'UTC',
  hourCycle: '24h',
  setZone: () => {},
  setHourCycle: () => {},
  serverZone: null,
  setServerZone: () => {},
  localZoneName: '',
  zoneOpts: { zone: 'UTC', hourCycle: '24h' },
  now: () => Date.now(),
}

const TimePreferenceContext = createContext<TimePreferenceValue>(DEFAULT_VALUE)

/** window.localStorage, or null when the browser denies access to it. */
export function safeLocalStorage(): Storage | null {
  try {
    return window.localStorage
  } catch {
    // Access itself can throw (private mode, blocked storage). The preference
    // then lives in memory only, which is the documented fallback.
    return null
  }
}

interface StoredPreference {
  zone: TimeZonePref
  hourCycle: HourCycle
}

const DEFAULT_PREFERENCE: StoredPreference = { zone: 'Server', hourCycle: '12h' }

const isHourCycle = (value: unknown): value is HourCycle => value === '12h' || value === '24h'

function asZone(value: unknown): TimeZonePref | null {
  return value === 'Server' || value === 'UTC' || value === 'Local' ? value : null
}

/**
 * Resolves both stored values (FRD 7.4). The hour-cycle key is the marker for
 * "written after TIME-12H-001": no earlier build wrote it and every later write does.
 * - A valid cycle means the zone key is a post-story value: Server, UTC and Local map to
 *   themselves and anything else is Server.
 * - Otherwise the zone key is a legacy value. Only Local survives; a legacy UTC (which the
 *   old build wrote whenever the clock was toggled back) becomes Server (DEC-5).
 * - A throwing read means the defaults (FR-048).
 */
function readStoredPreference(storage: PreferenceStorage | null): StoredPreference {
  try {
    const cycle = storage?.getItem(HOUR_CYCLE_STORAGE_KEY)
    const zone = storage?.getItem(TIME_ZONE_STORAGE_KEY)
    if (isHourCycle(cycle)) return { zone: asZone(zone) ?? DEFAULT_PREFERENCE.zone, hourCycle: cycle }
    return { zone: zone === 'Local' ? 'Local' : DEFAULT_PREFERENCE.zone, hourCycle: DEFAULT_PREFERENCE.hourCycle }
  } catch {
    // An unreadable preference means the default, never an error (FR-048).
    return DEFAULT_PREFERENCE
  }
}

interface TimePreferenceProviderProps {
  children: ReactNode
  /** Where the preference is persisted. Defaults to window.localStorage; null disables persistence. */
  storage?: PreferenceStorage | null
  /** IANA zone used for the Local preference. Test seam; undefined means the browser zone. */
  localTimeZone?: string
  /** Injected clock for relative times and the header clock. */
  now?: () => number
  /** Initial server zone (test seam). Validated exactly like an API value. */
  serverZone?: unknown
}

const defaultNow = () => Date.now()

export function TimePreferenceProvider({
  children,
  storage,
  localTimeZone,
  now = defaultNow,
  serverZone: initialServerZone,
}: TimePreferenceProviderProps) {
  const resolvedStorage = useRef<PreferenceStorage | null | undefined>(undefined)
  if (resolvedStorage.current === undefined) {
    resolvedStorage.current = storage === undefined ? safeLocalStorage() : storage
  }
  const [preference, setPreference] = useState<StoredPreference>(() => readStoredPreference(resolvedStorage.current ?? null))
  // The latest pair, so each setter can write both keys without a stale closure.
  const latest = useRef(preference)
  const [serverZone, setServerZoneState] = useState<AcceptedZone | null>(() => acceptServerZone(initialServerZone))

  const persist = useCallback((next: StoredPreference) => {
    latest.current = next
    setPreference(next)
    const target = resolvedStorage.current
    if (!target) return
    // Zone first, then the cycle marker, each in its own try. If the zone write throws the
    // marker is not written, so a half-written pair can never make a legacy UTC look like
    // a post-story choice. A failed write only costs persistence; the choice still applies
    // for this session (FR-048).
    try {
      target.setItem(TIME_ZONE_STORAGE_KEY, next.zone)
    } catch {
      return
    }
    try {
      target.setItem(HOUR_CYCLE_STORAGE_KEY, next.hourCycle)
    } catch {
      // Same: persistence is best effort.
    }
  }, [])

  const setZone = useCallback((zone: TimeZonePref) => persist({ ...latest.current, zone }), [persist])
  const setHourCycle = useCallback((hourCycle: HourCycle) => persist({ ...latest.current, hourCycle }), [persist])

  const setServerZone = useCallback((raw: unknown) => {
    const accepted = acceptServerZone(raw)
    setServerZoneState(previous => (previous === accepted ? previous : accepted))
  }, [])

  const localZoneName = useMemo(() => resolveLocalZoneName(localTimeZone), [localTimeZone])

  const { zone, hourCycle } = preference
  const value = useMemo<TimePreferenceValue>(
    () => ({
      zone,
      hourCycle,
      setZone,
      setHourCycle,
      serverZone,
      setServerZone,
      localZoneName,
      zoneOpts: localTimeZone === undefined ? { zone, hourCycle, serverTimeZone: serverZone } : { zone, hourCycle, serverTimeZone: serverZone, localTimeZone },
      now,
    }),
    [zone, hourCycle, setZone, setHourCycle, serverZone, setServerZone, localZoneName, localTimeZone, now],
  )

  return <TimePreferenceContext.Provider value={value}>{children}</TimePreferenceContext.Provider>
}

export function useTimePreference(): TimePreferenceValue {
  return useContext(TimePreferenceContext)
}

export interface TimeFormatters {
  timestamp: (value: InstantInput, precision?: TimestampPrecision) => string
  chartTick: (ts: number) => string
  chartTooltip: (ts: number) => string
}

/** Time formatters bound to the current preference. Stable until the preference changes. */
export function useTimeFormatters(): TimeFormatters {
  const { zoneOpts } = useTimePreference()
  return useMemo(
    () => ({
      timestamp: (value, precision) => formatTimestamp(value, zoneOpts, precision),
      chartTick: ts => formatChartTick(ts, zoneOpts),
      chartTooltip: ts => formatChartTooltip(ts, zoneOpts),
    }),
    [zoneOpts],
  )
}

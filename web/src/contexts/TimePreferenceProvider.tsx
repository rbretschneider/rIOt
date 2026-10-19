import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  formatChartTick,
  formatChartTooltip,
  formatTimestamp,
  type InstantInput,
  type TimestampPrecision,
  type TimeZonePref,
  type ZoneOpts,
} from '../utils/format'

// App-wide UTC / Local display preference. The value is a whitelisted enum:
// whatever is in storage, only the exact string 'Local' selects Local, so a
// tampered entry can never reach Intl as a zone identifier. The IANA zone used
// for Local comes only from the `localTimeZone` prop, which tests set and
// production never does.

export const TIME_ZONE_STORAGE_KEY = 'riot-time-zone'

type PreferenceStorage = Pick<Storage, 'getItem' | 'setItem'>

export interface TimePreferenceValue {
  zone: TimeZonePref
  setZone: (zone: TimeZonePref) => void
  toggleZone: () => void
  zoneOpts: ZoneOpts
  now: () => number
}

const DEFAULT_VALUE: TimePreferenceValue = {
  zone: 'UTC',
  setZone: () => {},
  toggleZone: () => {},
  zoneOpts: { zone: 'UTC' },
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

function readStoredZone(storage: PreferenceStorage | null): TimeZonePref {
  try {
    return storage?.getItem(TIME_ZONE_STORAGE_KEY) === 'Local' ? 'Local' : 'UTC'
  } catch {
    // An unreadable preference means the default, never an error (FR-103).
    return 'UTC'
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
}

const defaultNow = () => Date.now()

export function TimePreferenceProvider({
  children,
  storage,
  localTimeZone,
  now = defaultNow,
}: TimePreferenceProviderProps) {
  const resolvedStorage = useRef<PreferenceStorage | null | undefined>(undefined)
  if (resolvedStorage.current === undefined) {
    resolvedStorage.current = storage === undefined ? safeLocalStorage() : storage
  }
  const [zone, setZoneState] = useState<TimeZonePref>(() => readStoredZone(resolvedStorage.current ?? null))

  const setZone = useCallback((next: TimeZonePref) => {
    setZoneState(next)
    try {
      resolvedStorage.current?.setItem(TIME_ZONE_STORAGE_KEY, next)
    } catch {
      // A failed write only costs persistence; the choice still applies for this session (FR-103).
    }
  }, [])

  const toggleZone = useCallback(() => setZone(zone === 'UTC' ? 'Local' : 'UTC'), [setZone, zone])

  const value = useMemo<TimePreferenceValue>(
    () => ({
      zone,
      setZone,
      toggleZone,
      zoneOpts: localTimeZone === undefined ? { zone } : { zone, localTimeZone },
      now,
    }),
    [zone, setZone, toggleZone, localTimeZone, now],
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

import { useEffect, useRef, useState } from 'react'

/**
 * Returns a tick counter that increments every `intervalMs` milliseconds.
 *
 * Components subscribe to this hook to batch chart re-renders on a fixed
 * cadence (5 seconds) per FR-062 / NFR-003. KPI numerics update independently
 * on every heartbeat; only chart-heavy children gate on this tick.
 *
 * Using a numeric counter (rather than a Date) makes tests deterministic with
 * fake timers: advance timers by 5 s, assert tick incremented by 1.
 */
export function useDashboardTick(intervalMs = 5_000): number {
  const [tick, setTick] = useState(0)
  const intervalRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  useEffect(() => {
    intervalRef.current = setInterval(() => {
      setTick(t => t + 1)
    }, intervalMs)
    return () => {
      if (intervalRef.current !== undefined) {
        clearInterval(intervalRef.current)
      }
    }
  }, [intervalMs])

  return tick
}

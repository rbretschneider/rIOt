import { useSyncExternalStore } from 'react'

// One shared ticker for every relative-time display. A 50-row table would
// otherwise run 50 intervals (useDashboardTick keeps one per consumer). The
// interval starts with the first subscriber and stops with the last one.

const TICK_MS = 30_000

let tick = 0
let timer: ReturnType<typeof setInterval> | null = null
const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  if (timer === null) {
    timer = setInterval(() => {
      tick += 1
      listeners.forEach(l => l())
    }, TICK_MS)
  }
  return () => {
    listeners.delete(listener)
    if (listeners.size === 0 && timer !== null) {
      clearInterval(timer)
      timer = null
    }
  }
}

function getSnapshot(): number {
  return tick
}

/** Re-renders the caller every 30 s. Returns a counter that changes on each tick. */
export function useMinuteTick(): number {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}

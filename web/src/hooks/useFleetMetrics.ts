import { useCallback, useRef, useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useWebSocket } from './useWebSocket'
import { useDashboardTick } from './useDashboardTick'
import type { Device, Event, HeartbeatData, WSMessage } from '../types/models'

/** One data point in a time-series chart */
export interface ChartPoint {
  timestamp: string
  value: number
}

/** Per-device time series for the per-device chart cards. CPU, RAM, and disk
 * are native percentages. Load is normalized as min(load_avg_1m * 25, 100) so
 * all four lines share a 0-100% Y-axis. */
export interface DeviceSeries {
  deviceId: string
  hostname: string
  cpuPoints: ChartPoint[]
  memPoints: ChartPoint[]
  diskPoints: ChartPoint[]
  loadPoints: ChartPoint[]
}

export interface UseFleetMetricsResult {
  perDeviceSeries: DeviceSeries[]
  heartbeatsLoading: boolean
}

const MAX_POINTS = 240 // 60 min @ 15s cadence

function loadPctOf(hb: HeartbeatData): number {
  return Math.min((hb.load_avg_1m ?? 0) * 25, 100)
}

/**
 * Loads 60 min of heartbeats per device on mount, then appends new points to
 * each device's series on a 5-second tick driven by WS heartbeats. Charts
 * re-render at the tick cadence rather than per-heartbeat to keep frame budget
 * predictable on large fleets. */
// devices/events params are kept on the signature for future extensions
// (e.g. annotating series with online/offline cuts). Unused for now.
export function useFleetMetrics(devices: Device[], _events: Event[]): UseFleetMetricsResult {
  const { data: heartbeatsResp, isLoading: heartbeatsLoading } = useQuery({
    queryKey: ['fleet-heartbeats'],
    queryFn: () => api.getFleetHeartbeats('60m'),
    staleTime: 60_000,
  })

  const hbBufferRef = useRef<Map<string, HeartbeatData>>(new Map())
  const [perDeviceSeries, setPerDeviceSeries] = useState<DeviceSeries[]>([])

  const heartbeatsMap = heartbeatsResp?.devices ?? {}
  const hostnameByID = useMemo(() => {
    const m = new Map<string, string>()
    for (const d of devices) m.set(d.id, d.hostname)
    return m
  }, [devices])

  const historicalSeries = useMemo((): DeviceSeries[] => {
    return Object.entries(heartbeatsMap).map(([deviceId, hbs]) => ({
      deviceId,
      hostname: hostnameByID.get(deviceId) ?? deviceId,
      cpuPoints:  hbs.map(h => ({ timestamp: h.timestamp, value: h.data.cpu_percent ?? 0 })),
      memPoints:  hbs.map(h => ({ timestamp: h.timestamp, value: h.data.mem_percent ?? 0 })),
      diskPoints: hbs.map(h => ({ timestamp: h.timestamp, value: h.data.disk_root_percent ?? 0 })),
      loadPoints: hbs.map(h => ({ timestamp: h.timestamp, value: loadPctOf(h.data) })),
    }))
  }, [heartbeatsMap, hostnameByID])

  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type === 'heartbeat' && msg.device_id) {
      hbBufferRef.current.set(msg.device_id, msg.data as HeartbeatData)
    }
  }, [])
  useWebSocket(handleWS)

  const tick = useDashboardTick(5_000)

  useMemo(() => {
    const updated = historicalSeries.map(s => {
      const hb = hbBufferRef.current.get(s.deviceId)
      if (!hb) return s
      const ts = new Date().toISOString()
      return {
        ...s,
        cpuPoints:  [...s.cpuPoints,  { timestamp: ts, value: hb.cpu_percent ?? 0 }].slice(-MAX_POINTS),
        memPoints:  [...s.memPoints,  { timestamp: ts, value: hb.mem_percent ?? 0 }].slice(-MAX_POINTS),
        diskPoints: [...s.diskPoints, { timestamp: ts, value: hb.disk_root_percent ?? 0 }].slice(-MAX_POINTS),
        loadPoints: [...s.loadPoints, { timestamp: ts, value: loadPctOf(hb) }].slice(-MAX_POINTS),
      }
    })

    // Add series for new devices not yet in historical (just joined fleet)
    for (const [deviceId, hb] of hbBufferRef.current) {
      if (!updated.find(s => s.deviceId === deviceId)) {
        const ts = new Date().toISOString()
        updated.push({
          deviceId,
          hostname: hostnameByID.get(deviceId) ?? deviceId,
          cpuPoints:  [{ timestamp: ts, value: hb.cpu_percent ?? 0 }],
          memPoints:  [{ timestamp: ts, value: hb.mem_percent ?? 0 }],
          diskPoints: [{ timestamp: ts, value: hb.disk_root_percent ?? 0 }],
          loadPoints: [{ timestamp: ts, value: loadPctOf(hb) }],
        })
      }
    }

    setPerDeviceSeries(updated)
  }, [tick]) // eslint-disable-line react-hooks/exhaustive-deps

  // Seed series from REST data on initial load
  useMemo(() => {
    if (heartbeatsLoading) return
    setPerDeviceSeries(historicalSeries)
  }, [heartbeatsLoading, historicalSeries])

  return {
    perDeviceSeries,
    heartbeatsLoading,
  }
}

import { useCallback, useEffect, useRef, useState, useMemo } from 'react'
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

/** Per-device time series for the per-device chart cards. CPU, RAM, and disk-I/O
 * are native percentages (disk-I/O is the busy-time of the most-utilised drive,
 * matching `disk_io_percent` on DeviceDetail). Load is normalized per device as
 * min(load_avg_1m / cpu_threads * 100, 100) so a fully-saturated runqueue maps
 * to 100% regardless of core count (a load of 4 on a 4-thread box = 100%; on an
 * 8-thread box = 50%). Falls back to cpu_cores, then 1, if hardware_profile is
 * unavailable. netRxPoints / netTxPoints are bytes/sec for the sub-chart added
 * by FLEET-NET. */
export interface DeviceSeries {
  deviceId: string
  hostname: string
  cpuPoints: ChartPoint[]
  memPoints: ChartPoint[]
  diskPoints: ChartPoint[]
  loadPoints: ChartPoint[]
  netRxPoints: ChartPoint[]
  netTxPoints: ChartPoint[]
}

export interface UseFleetMetricsResult {
  perDeviceSeries: DeviceSeries[]
  heartbeatsLoading: boolean
}

const MAX_POINTS = 240 // 60 min @ 15s cadence

function loadPctOf(hb: HeartbeatData, cpuCount: number): number {
  const denom = cpuCount > 0 ? cpuCount : 1
  return Math.min(((hb.load_avg_1m ?? 0) / denom) * 100, 100)
}

/**
 * Coerces a value to a non-negative finite number.
 * Guards against NaN, +Infinity, -Infinity, and negative values that could
 * arrive from malformed or malicious heartbeat payloads (SEC-FLEET-NET-001).
 * The ?? 0 operator only handles null/undefined; this handles non-finite
 * IEEE-754 values that would otherwise corrupt recharts axis range computation.
 */
function finiteOrZero(v: number | undefined): number {
  return Number.isFinite(v) && (v as number) >= 0 ? (v as number) : 0
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
  // Logical CPU count per device, used to normalize load average into a percent
  // of capacity. Prefer cpu_threads (logical), fall back to cpu_cores (physical),
  // then 1 so we never divide by zero for devices missing hardware_profile.
  const cpuCountByID = useMemo(() => {
    const m = new Map<string, number>()
    for (const d of devices) {
      const hp = d.hardware_profile
      const n = hp?.cpu_threads ?? hp?.cpu_cores ?? 0
      if (n > 0) m.set(d.id, n)
    }
    return m
  }, [devices])

  const historicalSeries = useMemo((): DeviceSeries[] => {
    return Object.entries(heartbeatsMap).map(([deviceId, hbs]) => {
      const cpuCount = cpuCountByID.get(deviceId) ?? 1
      return {
        deviceId,
        hostname: hostnameByID.get(deviceId) ?? deviceId,
        cpuPoints:    hbs.map(h => ({ timestamp: h.timestamp, value: h.data.cpu_percent ?? 0 })),
        memPoints:    hbs.map(h => ({ timestamp: h.timestamp, value: h.data.mem_percent ?? 0 })),
        diskPoints:   hbs.map(h => ({ timestamp: h.timestamp, value: h.data.disk_io_percent ?? 0 })),
        loadPoints:   hbs.map(h => ({ timestamp: h.timestamp, value: loadPctOf(h.data, cpuCount) })),
        netRxPoints:  hbs.map(h => ({ timestamp: h.timestamp, value: finiteOrZero(h.data.net_rx_bytes_sec) })),
        netTxPoints:  hbs.map(h => ({ timestamp: h.timestamp, value: finiteOrZero(h.data.net_tx_bytes_sec) })),
      }
    })
  }, [heartbeatsMap, hostnameByID, cpuCountByID])

  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type === 'heartbeat' && msg.device_id) {
      hbBufferRef.current.set(msg.device_id, msg.data as HeartbeatData)
    }
  }, [])
  useWebSocket(handleWS)

  const tick = useDashboardTick(5_000)

  useEffect(() => {
    const updated = historicalSeries.map(s => {
      const hb = hbBufferRef.current.get(s.deviceId)
      if (!hb) return s
      const cpuCount = cpuCountByID.get(s.deviceId) ?? 1
      const ts = new Date().toISOString()
      return {
        ...s,
        cpuPoints:   [...s.cpuPoints,   { timestamp: ts, value: hb.cpu_percent ?? 0 }].slice(-MAX_POINTS),
        memPoints:   [...s.memPoints,   { timestamp: ts, value: hb.mem_percent ?? 0 }].slice(-MAX_POINTS),
        diskPoints:  [...s.diskPoints,  { timestamp: ts, value: hb.disk_io_percent ?? 0 }].slice(-MAX_POINTS),
        loadPoints:  [...s.loadPoints,  { timestamp: ts, value: loadPctOf(hb, cpuCount) }].slice(-MAX_POINTS),
        netRxPoints: [...s.netRxPoints, { timestamp: ts, value: finiteOrZero(hb.net_rx_bytes_sec) }].slice(-MAX_POINTS),
        netTxPoints: [...s.netTxPoints, { timestamp: ts, value: finiteOrZero(hb.net_tx_bytes_sec) }].slice(-MAX_POINTS),
      }
    })

    // Add series for new devices not yet in historical (just joined fleet)
    for (const [deviceId, hb] of hbBufferRef.current) {
      if (!updated.find(s => s.deviceId === deviceId)) {
        const cpuCount = cpuCountByID.get(deviceId) ?? 1
        const ts = new Date().toISOString()
        updated.push({
          deviceId,
          hostname:    hostnameByID.get(deviceId) ?? deviceId,
          cpuPoints:   [{ timestamp: ts, value: hb.cpu_percent ?? 0 }],
          memPoints:   [{ timestamp: ts, value: hb.mem_percent ?? 0 }],
          diskPoints:  [{ timestamp: ts, value: hb.disk_io_percent ?? 0 }],
          loadPoints:  [{ timestamp: ts, value: loadPctOf(hb, cpuCount) }],
          netRxPoints: [{ timestamp: ts, value: finiteOrZero(hb.net_rx_bytes_sec) }],
          netTxPoints: [{ timestamp: ts, value: finiteOrZero(hb.net_tx_bytes_sec) }],
        })
      }
    }

    setPerDeviceSeries(updated)
  }, [tick]) // eslint-disable-line react-hooks/exhaustive-deps

  // Seed series from REST data on initial load
  useEffect(() => {
    if (heartbeatsLoading) return
    setPerDeviceSeries(historicalSeries)
  }, [heartbeatsLoading, historicalSeries])

  return {
    perDeviceSeries,
    heartbeatsLoading,
  }
}

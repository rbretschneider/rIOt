import { useCallback, useRef, useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useWebSocket } from './useWebSocket'
import { useDashboardTick } from './useDashboardTick'
import type { Device, Event, FleetContainerRow, FleetHeartbeat, HeartbeatData, WSMessage } from '../types/models'

/** One data point in a time-series chart */
export interface ChartPoint {
  timestamp: string
  value: number
}

/** Per-device CPU/memory time series for the small-multiples row */
export interface DeviceSeries {
  deviceId: string
  hostname: string
  cpuPoints: ChartPoint[]
  memPoints: ChartPoint[]
}

/** Fleet-summed disk I/O series. Network series deferred to a follow-up
 * story — heartbeats do not yet carry per-interface byte rates. */
export interface FleetTimeSeries {
  diskRead: ChartPoint[]
  diskWrite: ChartPoint[]
}

/** Fleet KPI values */
export interface FleetKPIs {
  onlineCount: number
  totalCount: number
  cpuAvg: number
  cpuPeak: number
  cpuAvgPrev: number
  cpuPeakPrev: number
  memAvg: number
  memPeak: number
  memAvgPrev: number
  memPeakPrev: number
  worstDiskPercent: number
  worstDiskHostname: string
  containersRunning: number
  containersTotal: number
  alertsBySeverity: { warning: number; critical: number }
  pendingUpdates: number
  gpuAvg: number
}

export interface UseFleetMetricsResult {
  kpis: FleetKPIs
  perDeviceSeries: DeviceSeries[]
  fleetTimeSeries: FleetTimeSeries
  hasGPU: boolean
  containersLoading: boolean
  heartbeatsLoading: boolean
  /** Flashed device IDs: set by heartbeat handler, cleared after 300ms by tile */
  flashedKPIs: Set<string>
}

const EMPTY_KPIs: FleetKPIs = {
  onlineCount: 0,
  totalCount: 0,
  cpuAvg: 0,
  cpuPeak: 0,
  cpuAvgPrev: 0,
  cpuPeakPrev: 0,
  memAvg: 0,
  memPeak: 0,
  memAvgPrev: 0,
  memPeakPrev: 0,
  worstDiskPercent: 0,
  worstDiskHostname: '',
  containersRunning: 0,
  containersTotal: 0,
  alertsBySeverity: { warning: 0, critical: 0 },
  pendingUpdates: 0,
  gpuAvg: 0,
}

/** Compute fleet KPIs from the current live device list and heartbeat buffer */
function computeKPIs(
  devices: Device[],
  latestHBByDevice: Map<string, HeartbeatData>,
  events: Event[],
  containers: FleetContainerRow[],
  patchSums: number,
): FleetKPIs {
  const online = devices.filter(d => d.status === 'online')
  const cpuValues = online.map(d => latestHBByDevice.get(d.id)?.cpu_percent ?? 0)
  const memValues = online.map(d => latestHBByDevice.get(d.id)?.mem_percent ?? 0)

  const cpuAvg = cpuValues.length ? cpuValues.reduce((a, b) => a + b, 0) / cpuValues.length : 0
  const cpuPeak = cpuValues.length ? Math.max(...cpuValues) : 0
  const memAvg = memValues.length ? memValues.reduce((a, b) => a + b, 0) / memValues.length : 0
  const memPeak = memValues.length ? Math.max(...memValues) : 0

  let worstDiskPercent = 0
  let worstDiskHostname = ''
  for (const d of online) {
    const disk = latestHBByDevice.get(d.id)?.disk_root_percent ?? 0
    if (disk > worstDiskPercent) {
      worstDiskPercent = disk
      worstDiskHostname = d.hostname
    }
  }

  // Active alerts from event cache (unacknowledged warning/critical)
  const alertsBySeverity = { warning: 0, critical: 0 }
  for (const e of events) {
    if (!e.acknowledged_at) {
      if (e.severity === 'warning') alertsBySeverity.warning++
      else if (e.severity === 'critical') alertsBySeverity.critical++
    }
  }

  const containersRunning = containers.filter(c => c.state === 'running').length
  const containersTotal = containers.length

  return {
    onlineCount: online.length,
    totalCount: devices.length,
    cpuAvg,
    cpuPeak,
    cpuAvgPrev: 0, // populated from historical window comparison
    cpuPeakPrev: 0,
    memAvg,
    memPeak,
    memAvgPrev: 0,
    memPeakPrev: 0,
    worstDiskPercent,
    worstDiskHostname,
    containersRunning,
    containersTotal,
    alertsBySeverity,
    pendingUpdates: patchSums,
    gpuAvg: 0,
  }
}

/**
 * Central fleet metrics hook. Fetches /api/v1/fleet/heartbeats and
 * /api/v1/fleet/containers on mount, then updates KPIs on every WS heartbeat
 * and flushes charts on the 5-second tick (AD-001, AD-002, FR-061, FR-062).
 */
export function useFleetMetrics(devices: Device[], events: Event[]): UseFleetMetricsResult {
  // --- REST queries ---
  const { data: heartbeatsResp, isLoading: heartbeatsLoading } = useQuery({
    queryKey: ['fleet-heartbeats'],
    queryFn: () => api.getFleetHeartbeats('60m'),
    staleTime: 60_000,
  })

  const { data: containers = [], isLoading: containersLoading } = useQuery({
    queryKey: ['fleet-containers'],
    queryFn: api.getFleetContainers,
    staleTime: 30_000,
  })

  // Patch status for pending-updates KPI
  const { data: patchStatus = [] } = useQuery({
    queryKey: ['patch-status'],
    queryFn: api.getPatchStatus,
    staleTime: 120_000,
  })

  const patchSums = useMemo(
    () => patchStatus.reduce((sum, p) => sum + (p.pending_updates ?? 0), 0),
    [patchStatus],
  )

  // --- Ring buffer (ref — no re-render on heartbeat) ---
  const hbBufferRef = useRef<Map<string, HeartbeatData>>(new Map())

  // Latest heartbeat per device (for per-tick KPI recompute)
  const latestHBRef = useRef<Map<string, HeartbeatData>>(new Map())

  // KPI state updated on every heartbeat (FR-061)
  const [kpis, setKPIs] = useState<FleetKPIs>(EMPTY_KPIs)

  // Flashed KPI set (for KPI tile flash animation, FR-012)
  const [flashedKPIs, setFlashedKPIs] = useState<Set<string>>(new Set())

  // Chart series state updated on 5-second tick (FR-062)
  const [perDeviceSeries, setPerDeviceSeries] = useState<DeviceSeries[]>([])
  const [fleetTimeSeries, setFleetTimeSeries] = useState<FleetTimeSeries>({
    diskRead: [],
    diskWrite: [],
  })

  // Populate latest HB map from initial REST load
  const heartbeatsMap = heartbeatsResp?.devices ?? {}
  const hostnameByID = useMemo(() => {
    const m = new Map<string, string>()
    for (const d of devices) m.set(d.id, d.hostname)
    return m
  }, [devices])

  // Build per-device time series from historical data (on initial load)
  const historicalSeries = useMemo((): DeviceSeries[] => {
    return Object.entries(heartbeatsMap).map(([deviceId, hbs]) => ({
      deviceId,
      hostname: hostnameByID.get(deviceId) ?? deviceId,
      cpuPoints: hbs.map(h => ({ timestamp: h.timestamp, value: h.data.cpu_percent ?? 0 })),
      memPoints: hbs.map(h => ({ timestamp: h.timestamp, value: h.data.mem_percent ?? 0 })),
    }))
  }, [heartbeatsMap, hostnameByID])

  // Build fleet disk I/O time series (summed) from historical data
  const historicalFleetSeries = useMemo((): FleetTimeSeries => {
    const byTime = new Map<string, { diskRead: number; diskWrite: number }>()

    for (const hbs of Object.values(heartbeatsMap)) {
      for (const h of hbs) {
        const ts = h.timestamp
        const prev = byTime.get(ts) ?? { diskRead: 0, diskWrite: 0 }
        byTime.set(ts, {
          diskRead: prev.diskRead + (h.data.disk_read_bytes_sec ?? 0),
          diskWrite: prev.diskWrite + (h.data.disk_write_bytes_sec ?? 0),
        })
      }
    }

    const sorted = [...byTime.entries()].sort((a, b) => a[0].localeCompare(b[0]))
    return {
      diskRead: sorted.map(([ts, v]) => ({ timestamp: ts, value: v.diskRead })),
      diskWrite: sorted.map(([ts, v]) => ({ timestamp: ts, value: v.diskWrite })),
    }
  }, [heartbeatsMap])

  // Populate initial latestHBRef from the last heartbeat of each device
  useMemo(() => {
    for (const [deviceId, hbs] of Object.entries(heartbeatsMap)) {
      if (hbs.length > 0) {
        latestHBRef.current.set(deviceId, hbs[hbs.length - 1].data)
      }
    }
  }, [heartbeatsMap])

  // --- WS handler: update KPIs immediately; push to ring buffer for chart tick ---
  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type === 'heartbeat' && msg.device_id) {
      const hb = msg.data as HeartbeatData
      const deviceId = msg.device_id

      // Update ring buffer (for chart batch flush)
      hbBufferRef.current.set(deviceId, hb)
      // Update latest HB map (for immediate KPI recompute)
      latestHBRef.current.set(deviceId, hb)

      // Recompute KPIs immediately (FR-061) — this is a lightweight op
      setKPIs(prev => {
        const updated = computeKPIs(devices, latestHBRef.current, events, containers, patchSums)
        // Detect changes for flash
        const changed: string[] = []
        if (Math.abs(updated.cpuAvg - prev.cpuAvg) > 0.1) changed.push('cpu')
        if (Math.abs(updated.memAvg - prev.memAvg) > 0.1) changed.push('mem')
        if (updated.worstDiskPercent !== prev.worstDiskPercent) changed.push('disk')
        if (changed.length > 0) {
          setFlashedKPIs(new Set(changed))
          setTimeout(() => setFlashedKPIs(new Set()), 300)
        }
        return updated
      })
    }
  }, [devices, events, containers, patchSums])

  useWebSocket(handleWS)

  // --- 5-second tick: flush ring buffer to chart state (FR-062) ---
  const tick = useDashboardTick(5_000)

  useMemo(() => {
    // On each tick, derive updated series from accumulated buffer
    // Merge buffer entries into the historical series
    const updatedSeries = historicalSeries.map(s => {
      const bufferedHB = hbBufferRef.current.get(s.deviceId)
      if (!bufferedHB) return s
      const ts = new Date().toISOString()
      const maxPoints = 240 // 60 min @ 15s cadence
      return {
        ...s,
        cpuPoints: [...s.cpuPoints, { timestamp: ts, value: bufferedHB.cpu_percent ?? 0 }].slice(-maxPoints),
        memPoints: [...s.memPoints, { timestamp: ts, value: bufferedHB.mem_percent ?? 0 }].slice(-maxPoints),
      }
    })

    // Add series for new devices not yet in historical (just joined fleet)
    for (const [deviceId, hb] of hbBufferRef.current) {
      if (!updatedSeries.find(s => s.deviceId === deviceId)) {
        const ts = new Date().toISOString()
        updatedSeries.push({
          deviceId,
          hostname: hostnameByID.get(deviceId) ?? deviceId,
          cpuPoints: [{ timestamp: ts, value: hb.cpu_percent ?? 0 }],
          memPoints: [{ timestamp: ts, value: hb.mem_percent ?? 0 }],
        })
      }
    }

    setPerDeviceSeries(updatedSeries)
    setFleetTimeSeries(historicalFleetSeries)
  }, [tick]) // eslint-disable-line react-hooks/exhaustive-deps

  // Compute initial KPIs from REST data
  useMemo(() => {
    if (heartbeatsLoading) return
    const updated = computeKPIs(devices, latestHBRef.current, events, containers, patchSums)
    setKPIs(updated)
    setPerDeviceSeries(historicalSeries)
    setFleetTimeSeries(historicalFleetSeries)
  }, [heartbeatsLoading, devices, events, containers, patchSums, historicalSeries, historicalFleetSeries])

  const hasGPU = (heartbeatsResp?.devices_with_gpu?.length ?? 0) > 0

  return {
    kpis,
    perDeviceSeries,
    fleetTimeSeries,
    hasGPU,
    containersLoading,
    heartbeatsLoading,
    flashedKPIs,
  }
}

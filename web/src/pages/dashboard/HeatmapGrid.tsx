import { useMemo } from 'react'
import DeviceCard from './DeviceCard'
import { stressScore } from '../../utils/stress'
import type { Device, Event, FleetHeartbeat } from '../../types/models'

interface Props {
  devices: Device[]
  /** Map of deviceId → heartbeat array (chronological) from fleet heartbeats endpoint */
  deviceHeartbeats: Record<string, FleetHeartbeat[]>
  /** Map of deviceId → lastPulseAt timestamp for status-dot pulse */
  pulseMap: Record<string, number>
  /** Unacknowledged warning/critical events, used to derive per-device alert state */
  events: Event[]
}

/**
 * FR-030 / FR-034 / AC-013 / AC-016 / AC-018:
 * Grid of DeviceCards sorted by stress score descending.
 * Offline devices sort to the end. Empty fleet shows empty state.
 */
export default function HeatmapGrid({ devices, deviceHeartbeats, pulseMap, events }: Props) {
  // Build a set of device IDs with active (unacknowledged) alerts (AC-015)
  const alertDeviceIds = useMemo(() => {
    const s = new Set<string>()
    for (const e of events) {
      if (!e.acknowledged_at && (e.severity === 'warning' || e.severity === 'critical')) {
        s.add(e.device_id)
      }
    }
    return s
  }, [events])

  // Compute latest heartbeat data per device
  const latestHBByDevice = useMemo(() => {
    const m = new Map<string, FleetHeartbeat>()
    for (const [deviceId, hbs] of Object.entries(deviceHeartbeats)) {
      if (hbs.length > 0) m.set(deviceId, hbs[hbs.length - 1])
    }
    return m
  }, [deviceHeartbeats])

  // Build 5-minute CPU sparkline per device (last 20 points ≈ 5 min @ 15s)
  const sparklineByDevice = useMemo(() => {
    const m = new Map<string, number[]>()
    for (const [deviceId, hbs] of Object.entries(deviceHeartbeats)) {
      const last20 = hbs.slice(-20)
      m.set(deviceId, last20.map(h => h.data.cpu_percent ?? 0))
    }
    return m
  }, [deviceHeartbeats])

  // Sort devices by stress score descending; offline last
  const sortedDevices = useMemo(() => {
    return [...devices].sort((a, b) => {
      const aHB = latestHBByDevice.get(a.id)?.data
      const bHB = latestHBByDevice.get(b.id)?.data
      const aOnline = a.status === 'online'
      const bOnline = b.status === 'online'
      const scoreA = stressScore(aHB, aOnline)
      const scoreB = stressScore(bHB, bOnline)
      return scoreB - scoreA
    })
  }, [devices, latestHBByDevice])

  if (devices.length === 0) {
    return (
      <div
        className="bg-gray-900/60 border border-gray-800 rounded-xl p-8 text-center"
        data-testid="heatmap-empty-state"
        role="status"
        aria-label="No devices"
      >
        <p className="text-gray-400 mb-1">No devices registered</p>
        <p className="text-sm text-gray-600">
          Install the rIOt agent on your devices and they will appear here.
        </p>
      </div>
    )
  }

  return (
    <div
      className="grid grid-cols-2 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8 xl:grid-cols-10 gap-2"
      data-testid="heatmap-grid"
      role="region"
      aria-label="Device heatmap"
    >
      {sortedDevices.map(device => (
        <DeviceCard
          key={device.id}
          device={device}
          heartbeat={latestHBByDevice.get(device.id)?.data}
          lastPulseAt={pulseMap[device.id]}
          hasAlert={alertDeviceIds.has(device.id)}
          containerCount={device.docker_container_count ?? 0}
          cpuSparkline={sparklineByDevice.get(device.id) ?? []}
        />
      ))}
    </div>
  )
}

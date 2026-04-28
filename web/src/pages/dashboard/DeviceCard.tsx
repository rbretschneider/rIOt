import { Link } from 'react-router-dom'
import Sparkline from '../../components/Sparkline'
import type { Device, HeartbeatData } from '../../types/models'

interface Props {
  device: Device
  heartbeat?: HeartbeatData
  /** Timestamp (ms) of last pulse from usePerDevicePulse — re-keys the dot CSS animation */
  lastPulseAt?: number
  hasAlert: boolean
  containerCount: number
  cpuSparkline: number[]
}

/**
 * FR-031 / AC-013: Single heatmap device card.
 * - Status dot that pulses on heartbeat (FR-032 / AC-014)
 * - Hostname, CPU/mem/disk mini-bars, 5-min sparkline, container-count badge
 * - Hairline red border when device has active alert (FR-033 / AC-015)
 * - Navigates to /devices/:id on click (FR-035 / AC-017)
 */
export default function DeviceCard({ device, heartbeat, lastPulseAt, hasAlert, containerCount, cpuSparkline }: Props) {
  const cpu = heartbeat?.cpu_percent ?? 0
  const mem = heartbeat?.mem_percent ?? 0
  const disk = heartbeat?.disk_root_percent ?? 0

  const isOnline = device.status === 'online'

  function barColor(pct: number): string {
    if (pct > 90) return 'bg-red-500'
    if (pct > 75) return 'bg-amber-500'
    return 'bg-emerald-500'
  }

  return (
    <Link
      to={`/devices/${device.id}`}
      className={`block bg-gray-900/60 rounded-xl p-2 hover:bg-gray-800/60 transition-colors focus:outline-none focus:ring-2 focus:ring-emerald-500 ${
        hasAlert ? 'border border-red-500/40' : 'border border-gray-800'
      }`}
      aria-label={`${device.hostname} — ${device.status}`}
      data-testid={`device-card-${device.id}`}
    >
      {/* Header: status dot + hostname + container badge */}
      <div className="flex items-center justify-between gap-1 mb-1.5">
        <div className="flex items-center gap-1.5 min-w-0">
          {/* Status dot — FR-032: pulses on heartbeat via animation re-key */}
          <span
            key={lastPulseAt ?? 0}
            className={`h-2 w-2 rounded-full shrink-0 ${
              isOnline
                ? `bg-emerald-400 ${lastPulseAt ? 'animate-ping-once' : ''}`
                : 'bg-gray-600'
            }`}
            aria-label={isOnline ? 'Online' : 'Offline'}
            data-testid={`status-dot-${device.id}`}
          />
          <span className="text-xs font-medium text-white truncate" title={device.hostname}>
            {device.hostname}
          </span>
        </div>
        {/* Container count badge */}
        {containerCount > 0 && (
          <span
            className="text-[10px] bg-gray-700 text-gray-300 rounded px-1 shrink-0"
            aria-label={`${containerCount} containers`}
            data-testid={`container-badge-${device.id}`}
          >
            {containerCount}
          </span>
        )}
      </div>

      {/* Mini-bars: CPU / Memory / Disk */}
      <div className="space-y-0.5 mb-1.5" data-testid={`mini-bars-${device.id}`}>
        {[
          { label: 'CPU', value: cpu },
          { label: 'RAM', value: mem },
          { label: 'Disk', value: disk },
        ].map(({ label, value }) => (
          <div key={label} className="flex items-center gap-1">
            <span className="text-[9px] text-gray-500 w-6 shrink-0">{label}</span>
            <div className="flex-1 h-1 bg-gray-800 rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full transition-all duration-500 ${barColor(value)}`}
                style={{ width: `${Math.min(value, 100)}%` }}
                aria-label={`${label} ${value.toFixed(0)}%`}
              />
            </div>
            <span className="text-[9px] text-gray-500 w-6 text-right shrink-0">{value.toFixed(0)}%</span>
          </div>
        ))}
      </div>

      {/* 5-minute CPU sparkline */}
      {cpuSparkline.length > 1 && (
        <div data-testid={`sparkline-${device.id}`} aria-hidden="true">
          <Sparkline data={cpuSparkline} color="#10b981" height={20} />
        </div>
      )}
    </Link>
  )
}

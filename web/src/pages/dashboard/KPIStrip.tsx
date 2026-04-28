import GaugeBar from '../../components/GaugeBar'
import KPITile from './KPITile'
import type { FleetKPIs } from '../../hooks/useFleetMetrics'

interface Props {
  kpis: FleetKPIs
  hasGPU: boolean
  flashedKPIs: Set<string>
  cpuSparkline: number[]
  memSparkline: number[]
}

/**
 * FR-010: The KPI strip renders up to 8 tiles (GPU tile is conditional per
 * FR-014 / AC-005 / AC-006).
 */
export default function KPIStrip({ kpis, hasGPU, flashedKPIs, cpuSparkline, memSparkline }: Props) {
  const alertTotal = kpis.alertsBySeverity.warning + kpis.alertsBySeverity.critical

  return (
    <div
      className="grid grid-cols-2 sm:grid-cols-4 xl:grid-cols-8 gap-3"
      data-testid="kpi-strip"
      role="region"
      aria-label="Fleet KPI summary"
    >
      {/* 1. Devices online / total */}
      <KPITile
        label="Devices"
        value={`${kpis.onlineCount}/${kpis.totalCount}`}
        subtext={`${kpis.totalCount - kpis.onlineCount} offline`}
        delta={0}
        data-testid="kpi-devices"
      />

      {/* 2. Fleet CPU avg + peak */}
      <KPITile
        label="CPU avg"
        value={`${kpis.cpuAvg.toFixed(1)}%`}
        subtext={`Peak ${kpis.cpuPeak.toFixed(1)}%`}
        delta={kpis.cpuAvg - kpis.cpuAvgPrev}
        sparklineData={cpuSparkline}
        sparklineColor="rgba(251,191,36,0.6)"
        flash={flashedKPIs.has('cpu')}
        data-testid="kpi-cpu"
      />

      {/* 3. Fleet memory avg + peak */}
      <KPITile
        label="RAM avg"
        value={`${kpis.memAvg.toFixed(1)}%`}
        subtext={`Peak ${kpis.memPeak.toFixed(1)}%`}
        delta={kpis.memAvg - kpis.memAvgPrev}
        sparklineData={memSparkline}
        sparklineColor="rgba(99,102,241,0.6)"
        flash={flashedKPIs.has('mem')}
        data-testid="kpi-ram"
      />

      {/* 4. Worst-disk device (AC-008, BR-003) */}
      <div
        className="bg-gray-900/60 border border-gray-800 rounded-xl p-3 flex flex-col gap-1"
        data-testid="kpi-worst-disk"
        role="group"
        aria-label={`Worst disk: ${kpis.worstDiskHostname || 'none'}`}
      >
        <p className="text-xs text-gray-400">Worst disk</p>
        {kpis.worstDiskHostname ? (
          <>
            <p className="text-sm font-medium text-white truncate">{kpis.worstDiskHostname}</p>
            <GaugeBar label="" value={kpis.worstDiskPercent} />
          </>
        ) : (
          <p className="text-2xl font-bold text-gray-600">—</p>
        )}
      </div>

      {/* 5. Containers running / total */}
      <KPITile
        label="Containers"
        value={`${kpis.containersRunning}/${kpis.containersTotal}`}
        subtext={`${kpis.containersTotal - kpis.containersRunning} stopped`}
        delta={0}
        data-testid="kpi-containers"
      />

      {/* 6. Active alerts by severity */}
      <div
        className="bg-gray-900/60 border border-gray-800 rounded-xl p-3 flex flex-col gap-1"
        data-testid="kpi-alerts"
        role="group"
        aria-label={`Active alerts: ${alertTotal}`}
      >
        <p className="text-xs text-gray-400">Active alerts</p>
        <p className="text-2xl font-bold text-white tabular-nums">{alertTotal}</p>
        <div className="flex gap-2 text-xs mt-0.5">
          <span className="flex items-center gap-1 text-amber-400">
            {/* Warning icon — non-color signal per AC-034 */}
            <svg xmlns="http://www.w3.org/2000/svg" className="h-3 w-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92z" clipRule="evenodd" />
            </svg>
            <span aria-label={`${kpis.alertsBySeverity.warning} warning`}>{kpis.alertsBySeverity.warning}W</span>
          </span>
          <span className="flex items-center gap-1 text-red-400">
            <svg xmlns="http://www.w3.org/2000/svg" className="h-3 w-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
              <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7 4a1 1 0 11-2 0 1 1 0 012 0zm-1-9a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
            </svg>
            <span aria-label={`${kpis.alertsBySeverity.critical} critical`}>{kpis.alertsBySeverity.critical}C</span>
          </span>
        </div>
      </div>

      {/* 7. Pending updates */}
      <KPITile
        label="Pending updates"
        value={kpis.pendingUpdates}
        delta={0}
        data-testid="kpi-updates"
      />

      {/* 8. GPU utilization — conditional (FR-014, AC-005, AC-006) */}
      {hasGPU && (
        <KPITile
          label="GPU avg"
          value={`${kpis.gpuAvg.toFixed(1)}%`}
          delta={0}
          data-testid="kpi-gpu"
        />
      )}
    </div>
  )
}

import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import { useFleetMetrics } from '../hooks/useFleetMetrics'
import { usePerDevicePulse } from '../hooks/usePerDevicePulse'
import DisconnectedBanner from './dashboard/DisconnectedBanner'
import KPIStrip from './dashboard/KPIStrip'
import SmallMultiples from './dashboard/SmallMultiples'
import HeatmapGrid from './dashboard/HeatmapGrid'
import ContainerLeaderboard from './dashboard/ContainerLeaderboard'
import ActivityRiver from './dashboard/ActivityRiver'

/**
 * Fleet dashboard page — /dashboard route (FLEET-DASH).
 *
 * Composes the five sections:
 *   1. KPI strip (FR-010)
 *   2. Small-multiples charts (FR-020)
 *   3. Device heatmap grid (FR-030)
 *   4. Container leaderboard (FR-040)
 *   5. Activity river (FR-050)
 *
 * Reuses the existing singleton WebSocket connection from useDevices (AC-027, AD-002).
 * Does NOT call new WebSocket(...) anywhere.
 */
export default function Dashboard() {
  // Reuse the existing singleton WS connection (FR-060 / AC-027)
  const { data: devices = [], wsConnected } = useDevices()

  // Events from the existing ['events'] cache (AD-007 / FR-060)
  const { data: events = [] } = useQuery({
    queryKey: ['events'],
    queryFn: () => api.getEvents(50),
    staleTime: 60_000,
  })

  // Fleet containers for the leaderboard (AD-006 / AD-011)
  const { data: containers = [], isLoading: containersLoading } = useQuery({
    queryKey: ['fleet-containers'],
    queryFn: api.getFleetContainers,
    staleTime: 30_000,
  })

  // Fleet heartbeats for time-series data (AD-001)
  const { data: heartbeatsResp, isLoading: heartbeatsLoading } = useQuery({
    queryKey: ['fleet-heartbeats'],
    queryFn: () => api.getFleetHeartbeats('60m'),
    staleTime: 60_000,
  })

  const deviceHeartbeats = heartbeatsResp?.devices ?? {}
  const hasGPU = (heartbeatsResp?.devices_with_gpu?.length ?? 0) > 0

  // Fleet metrics aggregation (AD-001, AD-003, FR-061, FR-062)
  const { kpis, perDeviceSeries, fleetTimeSeries, flashedKPIs } = useFleetMetrics(devices, events)

  // Per-device pulse map for status-dot animation (FR-032 / AC-014)
  const pulseMap = usePerDevicePulse()

  // Compute CPU sparkline for KPI tile from fleet heartbeats
  const cpuSparkline = useMemo(() => {
    // Average CPU across devices at each time step
    const byTime = new Map<string, number[]>()
    for (const hbs of Object.values(deviceHeartbeats)) {
      for (const h of hbs) {
        const vals = byTime.get(h.timestamp) ?? []
        vals.push(h.data.cpu_percent ?? 0)
        byTime.set(h.timestamp, vals)
      }
    }
    return [...byTime.entries()]
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([, vals]) => vals.reduce((a, b) => a + b, 0) / vals.length)
  }, [deviceHeartbeats])

  const memSparkline = useMemo(() => {
    const byTime = new Map<string, number[]>()
    for (const hbs of Object.values(deviceHeartbeats)) {
      for (const h of hbs) {
        const vals = byTime.get(h.timestamp) ?? []
        vals.push(h.data.mem_percent ?? 0)
        byTime.set(h.timestamp, vals)
      }
    }
    return [...byTime.entries()]
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([, vals]) => vals.reduce((a, b) => a + b, 0) / vals.length)
  }, [deviceHeartbeats])

  return (
    <div className="space-y-6" data-testid="dashboard-page">
      {/* FR-063 / AC-029: Disconnected banner */}
      {!wsConnected && <DisconnectedBanner />}

      {/* Section 1: KPI strip (FR-010) */}
      <section aria-labelledby="kpi-heading">
        <h2 id="kpi-heading" className="sr-only">Fleet KPIs</h2>
        {heartbeatsLoading ? (
          <div className="grid grid-cols-2 sm:grid-cols-4 xl:grid-cols-8 gap-3">
            {[...Array(7)].map((_, i) => (
              <div key={i} className="bg-gray-900/60 border border-gray-800 rounded-xl p-3 h-20 animate-pulse" />
            ))}
          </div>
        ) : (
          <KPIStrip
            kpis={kpis}
            hasGPU={hasGPU}
            flashedKPIs={flashedKPIs}
            cpuSparkline={cpuSparkline}
            memSparkline={memSparkline}
          />
        )}
      </section>

      {/* Section 2: Small-multiples charts (FR-020) */}
      <section aria-labelledby="charts-heading">
        <h2 id="charts-heading" className="sr-only">Fleet time-series charts</h2>
        {heartbeatsLoading ? (
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="bg-gray-900/60 border border-gray-800 rounded-xl p-3 h-40 animate-pulse" />
            ))}
          </div>
        ) : (
          <SmallMultiples
            perDeviceSeries={perDeviceSeries}
            fleetTimeSeries={fleetTimeSeries}
          />
        )}
      </section>

      {/* Section 3: Device heatmap (FR-030) */}
      <section aria-labelledby="heatmap-heading">
        <h2 id="heatmap-heading" className="text-sm font-semibold text-gray-400 mb-2">Devices</h2>
        <HeatmapGrid
          devices={devices}
          deviceHeartbeats={deviceHeartbeats}
          pulseMap={pulseMap}
          events={events}
        />
      </section>

      {/* Section 4: Container leaderboard (FR-040) */}
      <section aria-labelledby="leaderboard-heading">
        <h2 id="leaderboard-heading" className="text-sm font-semibold text-gray-400 mb-2">Container leaderboard</h2>
        <ContainerLeaderboard containers={containers} isLoading={containersLoading} />
      </section>

      {/* Section 5: Activity river (FR-050) */}
      <section aria-labelledby="river-heading">
        <h2 id="river-heading" className="sr-only">Activity river</h2>
        <ActivityRiver events={events} />
      </section>
    </div>
  )
}

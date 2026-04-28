import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import { useFleetMetrics } from '../hooks/useFleetMetrics'
import DisconnectedBanner from './dashboard/DisconnectedBanner'
import PerDeviceCharts from './dashboard/PerDeviceCharts'
import ContainerLeaderboard from './dashboard/ContainerLeaderboard'
import ActivityRiver from './dashboard/ActivityRiver'

/**
 * Fleet dashboard at /dashboard. Three sections:
 *   1. Per-device performance charts (CPU/RAM/Disk/Load over 60 min, one card per device)
 *   2. Container leaderboard (sortable, top 10)
 *   3. Activity river (recent events)
 */
export default function Dashboard() {
  const { data: devices = [], wsConnected } = useDevices()

  const { data: events = [] } = useQuery({
    queryKey: ['events'],
    queryFn: () => api.getEvents(50),
    staleTime: 60_000,
  })

  const { data: containers = [], isLoading: containersLoading } = useQuery({
    queryKey: ['fleet-containers'],
    queryFn: api.getFleetContainers,
    staleTime: 30_000,
  })

  const { perDeviceSeries, heartbeatsLoading } = useFleetMetrics(devices, events)

  return (
    <div className="space-y-6" data-testid="dashboard-page">
      {!wsConnected && <DisconnectedBanner />}

      <section aria-labelledby="charts-heading">
        <h2 id="charts-heading" className="text-sm font-semibold text-gray-400 mb-2">Devices</h2>
        {heartbeatsLoading ? (
          <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="bg-gray-900/60 border border-gray-800 rounded-xl p-4 h-72 animate-pulse" />
            ))}
          </div>
        ) : (
          <PerDeviceCharts series={perDeviceSeries} />
        )}
      </section>

      <section aria-labelledby="leaderboard-heading">
        <h2 id="leaderboard-heading" className="text-sm font-semibold text-gray-400 mb-2">Container leaderboard</h2>
        <ContainerLeaderboard containers={containers} isLoading={containersLoading} />
      </section>

      <section aria-labelledby="river-heading">
        <h2 id="river-heading" className="text-sm font-semibold text-gray-400 mb-2">Activity</h2>
        <ActivityRiver events={events} />
      </section>
    </div>
  )
}

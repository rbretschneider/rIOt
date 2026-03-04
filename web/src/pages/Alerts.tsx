import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import SeverityBadge from '../components/SeverityBadge'

export default function Alerts() {
  const [filter, setFilter] = useState<string>('')
  // useDevices sets up WS that pushes new events into the ['events'] cache
  const { data: devices, wsConnected } = useDevices()
  const { data: events, isLoading } = useQuery({
    queryKey: ['events'],
    queryFn: () => api.getEvents(200, 0),
    refetchInterval: wsConnected ? false : 15_000, // Only poll when WS is down
  })

  const deviceMap = new Map(devices?.map(d => [d.id, d]))

  const filtered = events?.filter(e => {
    if (!filter) return true
    const device = deviceMap.get(e.device_id)
    return e.type.includes(filter) ||
      e.severity.includes(filter) ||
      e.message.toLowerCase().includes(filter.toLowerCase()) ||
      device?.hostname.toLowerCase().includes(filter.toLowerCase())
  }) ?? []

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold text-white">Events & Alerts</h1>
        <input
          type="text"
          placeholder="Filter events..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="bg-gray-900 border border-gray-700 rounded-lg px-4 py-2 text-sm text-gray-100 placeholder-gray-500 focus:outline-none focus:border-blue-500"
        />
      </div>

      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-gray-500">Loading...</div>
        ) : (
          <table className="w-full">
            <thead className="bg-gray-900/50 border-b border-gray-800">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Time</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Severity</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Device</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Type</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Message</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {filtered.map((e) => {
                const device = deviceMap.get(e.device_id)
                return (
                  <tr key={e.id} className="hover:bg-gray-800/30">
                    <td className="px-4 py-3 text-sm text-gray-400 font-mono whitespace-nowrap">
                      {new Date(e.created_at).toLocaleString()}
                    </td>
                    <td className="px-4 py-3"><SeverityBadge severity={e.severity} /></td>
                    <td className="px-4 py-3">
                      {device ? (
                        <Link to={`/devices/${device.id}`} className="text-blue-400 hover:text-blue-300 text-sm">
                          {device.hostname}
                        </Link>
                      ) : (
                        <span className="text-sm text-gray-500 font-mono">{e.device_id.slice(0, 8)}</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm text-gray-400">{e.type.replace(/_/g, ' ')}</td>
                    <td className="px-4 py-3 text-sm text-gray-200">{e.message}</td>
                  </tr>
                )
              })}
              {filtered.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-gray-500">
                    No events found
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

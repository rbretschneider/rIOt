import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import SeverityBadge from '../components/SeverityBadge'

export default function Alerts() {
  const [filter, setFilter] = useState<string>('')
  const [showUnackOnly, setShowUnackOnly] = useState(false)
  const qc = useQueryClient()
  // useDevices sets up WS that pushes new events into the ['events'] cache
  const { data: devices, wsConnected } = useDevices()
  const { data: events, isLoading } = useQuery({
    queryKey: ['events'],
    queryFn: () => api.getEvents(200, 0),
    refetchInterval: wsConnected ? false : 15_000,
  })

  const ackMutation = useMutation({
    mutationFn: (id: number) => api.acknowledgeEvent(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['events'] })
      qc.invalidateQueries({ queryKey: ['unread-count'] })
    },
  })

  const ackAllMutation = useMutation({
    mutationFn: () => api.acknowledgeAllEvents(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['events'] })
      qc.invalidateQueries({ queryKey: ['unread-count'] })
    },
  })

  const deviceMap = new Map(devices?.map(d => [d.id, d]))

  const filtered = events?.filter(e => {
    if (showUnackOnly && e.acknowledged_at) return false
    if (!filter) return true
    const device = deviceMap.get(e.device_id)
    return e.type.includes(filter) ||
      e.severity.includes(filter) ||
      e.message.toLowerCase().includes(filter.toLowerCase()) ||
      device?.hostname.toLowerCase().includes(filter.toLowerCase())
  }) ?? []

  const hasUnack = events?.some(e => !e.acknowledged_at) ?? false

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h1 className="text-xl font-bold text-white">Events & Alerts</h1>
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
            <input
              type="checkbox"
              checked={showUnackOnly}
              onChange={(e) => setShowUnackOnly(e.target.checked)}
              className="rounded bg-gray-800 border-gray-600"
            />
            Unacknowledged only
          </label>
          {hasUnack && (
            <button
              onClick={() => ackAllMutation.mutate()}
              disabled={ackAllMutation.isPending}
              className="px-3 py-1.5 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-md transition-colors disabled:opacity-50"
            >
              {ackAllMutation.isPending ? 'Acknowledging...' : 'Acknowledge All'}
            </button>
          )}
          <input
            type="text"
            placeholder="Filter events..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="bg-gray-900 border border-gray-700 rounded-lg px-4 py-2 text-sm text-gray-100 placeholder-gray-500 focus:outline-none focus:border-blue-500"
          />
        </div>
      </div>

      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-x-auto">
        {isLoading ? (
          <div className="p-8 text-center text-gray-500">Loading...</div>
        ) : (
          <table className="w-full min-w-[640px]">
            <thead className="bg-gray-900/50 border-b border-gray-800">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Time</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Severity</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Device</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Type</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Message</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase w-10"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {filtered.map((e) => {
                const device = deviceMap.get(e.device_id)
                const isAcked = !!e.acknowledged_at
                return (
                  <tr
                    key={e.id}
                    className={`hover:bg-gray-800/30 ${isAcked ? 'opacity-50' : ''}`}
                    style={!isAcked && (e.severity === 'critical' || e.severity === 'warning') ? {
                      borderLeft: `3px solid ${e.severity === 'critical' ? '#f87171' : '#fbbf24'}`
                    } : undefined}
                  >
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
                    <td className="px-4 py-3">
                      {!isAcked && (
                        <button
                          onClick={() => ackMutation.mutate(e.id)}
                          disabled={ackMutation.isPending}
                          className="text-gray-500 hover:text-emerald-400 transition-colors"
                          title="Acknowledge"
                        >
                          <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                            <path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" />
                          </svg>
                        </button>
                      )}
                    </td>
                  </tr>
                )
              })}
              {filtered.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-gray-500">
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

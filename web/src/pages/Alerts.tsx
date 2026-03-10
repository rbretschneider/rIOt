import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import SeverityBadge from '../components/SeverityBadge'

const PAGE_SIZE = 25

export default function Alerts() {
  const [filter, setFilter] = useState<string>('')
  const [showUnackOnly, setShowUnackOnly] = useState(() => localStorage.getItem('alerts-unack-only') === 'true')
  const [page, setPage] = useState(0)
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

  const filtered = useMemo(() => events?.filter(e => {
    if (showUnackOnly && e.acknowledged_at) return false
    if (!filter) return true
    const device = deviceMap.get(e.device_id)
    return e.type.includes(filter) ||
      e.severity.includes(filter) ||
      e.message.toLowerCase().includes(filter.toLowerCase()) ||
      device?.hostname.toLowerCase().includes(filter.toLowerCase())
  }) ?? [], [events, filter, showUnackOnly, deviceMap])

  // Reset to first page when filters change
  const totalPages = Math.ceil(filtered.length / PAGE_SIZE)
  const safePage = Math.min(page, Math.max(0, totalPages - 1))
  const paged = filtered.slice(safePage * PAGE_SIZE, (safePage + 1) * PAGE_SIZE)

  const hasUnack = events?.some(e => !e.acknowledged_at) ?? false

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h1 className="text-xl font-bold text-white">Events & Alerts</h1>
        <div className="flex items-center gap-3 flex-wrap">
          <label className="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
            <input
              type="checkbox"
              checked={showUnackOnly}
              onChange={(e) => { setShowUnackOnly(e.target.checked); localStorage.setItem('alerts-unack-only', String(e.target.checked)); setPage(0) }}
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
            onChange={(e) => { setFilter(e.target.value); setPage(0) }}
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
              {paged.map((e) => {
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

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between text-sm">
          <span className="text-gray-500">
            {safePage * PAGE_SIZE + 1}–{Math.min((safePage + 1) * PAGE_SIZE, filtered.length)} of {filtered.length}
          </span>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setPage(0)}
              disabled={safePage === 0}
              className="px-2 py-1 rounded text-gray-400 hover:text-white hover:bg-gray-800 disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-gray-400"
            >
              &laquo;
            </button>
            <button
              onClick={() => setPage(p => Math.max(0, p - 1))}
              disabled={safePage === 0}
              className="px-2 py-1 rounded text-gray-400 hover:text-white hover:bg-gray-800 disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-gray-400"
            >
              &lsaquo;
            </button>
            {Array.from({ length: totalPages }, (_, i) => i)
              .filter(i => i === 0 || i === totalPages - 1 || Math.abs(i - safePage) <= 2)
              .reduce<(number | 'gap')[]>((acc, i) => {
                if (acc.length > 0) {
                  const prev = acc[acc.length - 1]
                  if (typeof prev === 'number' && i - prev > 1) acc.push('gap')
                }
                acc.push(i)
                return acc
              }, [])
              .map((item, idx) =>
                item === 'gap' ? (
                  <span key={`gap-${idx}`} className="px-1 text-gray-600">...</span>
                ) : (
                  <button
                    key={item}
                    onClick={() => setPage(item)}
                    className={`px-2.5 py-1 rounded text-sm ${
                      item === safePage
                        ? 'bg-blue-600 text-white'
                        : 'text-gray-400 hover:text-white hover:bg-gray-800'
                    }`}
                  >
                    {item + 1}
                  </button>
                )
              )}
            <button
              onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
              disabled={safePage >= totalPages - 1}
              className="px-2 py-1 rounded text-gray-400 hover:text-white hover:bg-gray-800 disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-gray-400"
            >
              &rsaquo;
            </button>
            <button
              onClick={() => setPage(totalPages - 1)}
              disabled={safePage >= totalPages - 1}
              className="px-2 py-1 rounded text-gray-400 hover:text-white hover:bg-gray-800 disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-gray-400"
            >
              &raquo;
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

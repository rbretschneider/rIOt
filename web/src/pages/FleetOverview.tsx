import { useState, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import StatusBadge from '../components/StatusBadge'

type SortKey = 'hostname' | 'status' | 'arch' | 'last_heartbeat' | 'short_id' | 'agent_version'
type SortDir = 'asc' | 'desc'

export default function FleetOverview() {
  const { data: devices, isLoading, wsConnected } = useDevices()
  const { data: serverUpdate } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    staleTime: 60 * 60 * 1000,
  })

  const latestVersion = serverUpdate?.latest_version

  // Derive summary from live device data — no separate polling needed
  const summary = useMemo(() => {
    if (!devices) return null
    return {
      total_devices: devices.length,
      online_count: devices.filter(d => d.status === 'online').length,
      offline_count: devices.filter(d => d.status === 'offline').length,
      warning_count: devices.filter(d => d.status === 'warning').length,
    }
  }, [devices])
  const [search, setSearch] = useState('')
  const [sortKey, setSortKey] = useState<SortKey>('hostname')
  const [sortDir, setSortDir] = useState<SortDir>('asc')

  const filtered = useMemo(() => {
    if (!devices) return []
    let list = devices.filter((d) => {
      const q = search.toLowerCase()
      return d.hostname.toLowerCase().includes(q) ||
        d.short_id.toLowerCase().includes(q) ||
        d.arch.toLowerCase().includes(q) ||
        d.tags?.some(t => t.toLowerCase().includes(q))
    })
    list.sort((a, b) => {
      const va = a[sortKey] ?? ''
      const vb = b[sortKey] ?? ''
      const cmp = String(va).localeCompare(String(vb))
      return sortDir === 'asc' ? cmp : -cmp
    })
    return list
  }, [devices, search, sortKey, sortDir])

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc')
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

  const SortHeader = ({ k, children }: { k: SortKey; children: React.ReactNode }) => (
    <th
      className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider cursor-pointer hover:text-gray-200"
      onClick={() => toggleSort(k)}
    >
      {children} {sortKey === k ? (sortDir === 'asc' ? '\u2191' : '\u2193') : ''}
    </th>
  )

  function formatAgo(ts?: string) {
    if (!ts) return 'Never'
    const diff = Date.now() - new Date(ts).getTime()
    if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
    if (diff < 3600_000) return `${Math.floor(diff / 60_000)}m ago`
    if (diff < 86400_000) return `${Math.floor(diff / 3600_000)}h ago`
    return `${Math.floor(diff / 86400_000)}d ago`
  }

  function isAgentOutdated(version?: string) {
    if (!version || !latestVersion || version === 'dev') return false
    return version !== latestVersion
  }

  return (
    <div className="space-y-6">
      {summary && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
          <StatCard label="Total Devices" value={summary.total_devices} />
          <StatCard label="Online" value={summary.online_count} color="text-emerald-400" />
          <StatCard label="Offline" value={summary.offline_count} color="text-red-400" />
          <StatCard label="Warnings" value={summary.warning_count} color="text-amber-400" />
        </div>
      )}

      <div className="flex items-center gap-4">
        <input
          type="text"
          placeholder="Search devices..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 bg-gray-900 border border-gray-700 rounded-lg px-4 py-2 text-sm text-gray-100 placeholder-gray-500 focus:outline-none focus:border-blue-500"
        />
      </div>

      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
        {isLoading ? (
          <div className="p-8 text-center text-gray-500">Loading...</div>
        ) : (
          <table className="w-full">
            <thead className="bg-gray-900/50 border-b border-gray-800">
              <tr>
                <SortHeader k="hostname">Device</SortHeader>
                <SortHeader k="short_id">ID</SortHeader>
                <SortHeader k="status">Status</SortHeader>
                <SortHeader k="arch">Arch</SortHeader>
                <SortHeader k="agent_version">Version</SortHeader>
                <SortHeader k="last_heartbeat">Last Seen</SortHeader>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">IP</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Tags</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {filtered.map((d) => (
                <tr key={d.id} className="hover:bg-gray-800/30 transition-colors">
                  <td className="px-4 py-3">
                    <Link to={`/devices/${d.id}`} className="text-blue-400 hover:text-blue-300 font-medium">
                      {d.hostname}
                    </Link>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-gray-500">{d.short_id}</td>
                  <td className="px-4 py-3"><StatusBadge status={d.status} /></td>
                  <td className="px-4 py-3 text-sm text-gray-400">{d.arch}</td>
                  <td className="px-4 py-3 text-sm font-mono">
                    {d.agent_version ? (
                      <span className={isAgentOutdated(d.agent_version) ? 'text-amber-400' : 'text-gray-400'}>
                        {d.agent_version}
                        {isAgentOutdated(d.agent_version) && (
                          <span className="ml-1.5 text-xs italic text-gray-500" title={`Latest: ${latestVersion}`}>(outdated)</span>
                        )}
                      </span>
                    ) : (
                      <span className="text-gray-600">-</span>
                    )}
                  </td>
                  <td className="px-4 py-3 text-sm text-gray-400">{formatAgo(d.last_heartbeat)}</td>
                  <td className="px-4 py-3 text-sm text-gray-400 font-mono">{d.primary_ip || '-'}</td>
                  <td className="px-4 py-3">
                    <div className="flex gap-1 flex-wrap">
                      {d.tags?.map(t => (
                        <span key={t} className="px-1.5 py-0.5 rounded bg-gray-800 text-xs text-gray-400">{t}</span>
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
              {filtered.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-4 py-8 text-center text-gray-500">
                    {search ? 'No devices match your search' : 'No devices registered yet'}
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

function StatCard({ label, value, color = 'text-white' }: { label: string; value: number; color?: string }) {
  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
      <p className="text-sm text-gray-400">{label}</p>
      <p className={`text-2xl font-bold ${color}`}>{value}</p>
    </div>
  )
}

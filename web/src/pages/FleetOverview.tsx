import { useState, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import { isVersionOlder } from '../utils/version'
import StatusBadge from '../components/StatusBadge'
import ConfirmModal from '../components/ConfirmModal'
import SetupGuide from '../components/SetupGuide'

type SortKey = 'hostname' | 'status' | 'arch' | 'last_heartbeat' | 'short_id' | 'agent_version'
type SortDir = 'asc' | 'desc'

export default function FleetOverview() {
  const { data: devices, isLoading, wsConnected } = useDevices()
  const { data: serverUpdate } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    staleTime: 60 * 60 * 1000,
  })
  const { data: patchStatus } = useQuery({
    queryKey: ['patch-status'],
    queryFn: api.getPatchStatus,
    refetchInterval: wsConnected ? 120_000 : 60_000,
  })

  const latestVersion = serverUpdate?.latest_version
  const patchMap = useMemo(() => {
    const m = new Map<string, number>()
    patchStatus?.forEach(p => m.set(p.device_id, p.pending_updates))
    return m
  }, [patchStatus])
  const queryClient = useQueryClient()
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; hostname: string; status: string } | null>(null)
  const [showGuide, setShowGuide] = useState(false)
  const [showPatchConfirm, setShowPatchConfirm] = useState(false)
  const [patchResult, setPatchResult] = useState<{ sent: number; queued: number; skipped: number; total: number } | null>(null)

  async function handleDelete() {
    if (!deleteTarget) return
    const uninstall = deleteTarget.status === 'online'
    try {
      await api.deleteDevice(deleteTarget.id, uninstall)
      queryClient.invalidateQueries({ queryKey: ['devices'] })
    } catch { /* ignore */ }
    setDeleteTarget(null)
  }

  async function handlePatchAll() {
    try {
      const result = await api.bulkPatchDevices('full')
      setPatchResult(result)
    } catch { /* ignore */ }
    setShowPatchConfirm(false)
  }

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
    return isVersionOlder(version, latestVersion)
  }

  return (
    <div className="space-y-6">
      {summary && (
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-4">
          <StatCard label="Total Devices" value={summary.total_devices} />
          <StatCard label="Online" value={summary.online_count} color="text-emerald-400" />
          <StatCard label="Offline" value={summary.offline_count} color="text-red-400" />
          <StatCard label="Warnings" value={summary.warning_count} color="text-amber-400" />
          <StatCard label="Need Updates" value={patchMap.size} color={patchMap.size > 0 ? 'text-cyan-400' : 'text-white'} />
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
        <button
          onClick={() => setShowPatchConfirm(true)}
          className="px-4 py-2 text-sm text-cyan-400 hover:text-cyan-300 border border-cyan-800/50 rounded-lg font-medium transition-colors whitespace-nowrap"
        >
          Patch All
        </button>
        <button
          onClick={() => setShowGuide(true)}
          className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors whitespace-nowrap"
        >
          + Add Device
        </button>
      </div>

      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-x-auto">
        {isLoading ? (
          <div className="p-8 text-center text-gray-500">Loading...</div>
        ) : (
          <table className="w-full min-w-[640px]">
            <thead className="bg-gray-900/50 border-b border-gray-800">
              <tr>
                <SortHeader k="hostname">Device</SortHeader>
                <SortHeader k="short_id">ID</SortHeader>
                <SortHeader k="status">Status</SortHeader>
                <SortHeader k="arch">Arch</SortHeader>
                <SortHeader k="agent_version">Version</SortHeader>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">Updates</th>
                <SortHeader k="last_heartbeat">Last Seen</SortHeader>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">IP</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Tags</th>
                <th className="px-4 py-3"></th>
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
                  <td className="px-4 py-3 text-sm">
                    {(() => {
                      const count = patchMap.get(d.id)
                      if (!count) return <span className="text-gray-600">-</span>
                      return <span className="text-cyan-400">{count}</span>
                    })()}
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
                  <td className="px-4 py-3 text-right">
                    <button
                      onClick={(e) => { e.stopPropagation(); setDeleteTarget({ id: d.id, hostname: d.hostname, status: d.status }) }}
                      className="text-gray-600 hover:text-red-400 transition-colors p-1"
                      title="Delete device"
                    >
                      <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                        <path fillRule="evenodd" d="M9 2a1 1 0 00-.894.553L7.382 4H4a1 1 0 000 2v10a2 2 0 002 2h8a2 2 0 002-2V6a1 1 0 100-2h-3.382l-.724-1.447A1 1 0 0011 2H9zM7 8a1 1 0 012 0v6a1 1 0 11-2 0V8zm5-1a1 1 0 00-1 1v6a1 1 0 102 0V8a1 1 0 00-1-1z" clipRule="evenodd" />
                      </svg>
                    </button>
                  </td>
                </tr>
              ))}
              {filtered.length === 0 && search && (
                <tr>
                  <td colSpan={10} className="px-4 py-8 text-center text-gray-500">
                    No devices match your search
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>

      {!search && filtered.length === 0 && !isLoading && (
        <SetupGuide inline />
      )}

      {showGuide && <SetupGuide onClose={() => setShowGuide(false)} />}

      {patchResult && (
        <div className="px-4 py-2 bg-cyan-900/30 border border-cyan-800 rounded text-sm text-cyan-400 flex items-center justify-between">
          <span>Patch commands dispatched: {patchResult.sent} sent, {patchResult.queued} queued, {patchResult.skipped} skipped ({patchResult.total} total)</span>
          <button onClick={() => setPatchResult(null)} className="text-cyan-600 hover:text-cyan-400 ml-4">&times;</button>
        </div>
      )}

      {showPatchConfirm && (
        <ConfirmModal
          title="Patch All Devices"
          message="Run OS package updates on all online devices? Each device must have commands.allow_patching enabled in its agent config to apply updates."
          confirmLabel="Patch All"
          confirmVariant="primary"
          onConfirm={handlePatchAll}
          onCancel={() => setShowPatchConfirm(false)}
        />
      )}

      {deleteTarget && (
        <ConfirmModal
          title="Delete Device"
          message={deleteTarget.status === 'online'
            ? `Remove "${deleteTarget.hostname}" from the fleet? The agent will be uninstalled from the device.`
            : `Remove "${deleteTarget.hostname}" from the fleet? The device is offline — you may need to manually uninstall the agent.`
          }
          confirmLabel="Delete"
          confirmVariant="danger"
          onConfirm={handleDelete}
          onCancel={() => setDeleteTarget(null)}
        />
      )}
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

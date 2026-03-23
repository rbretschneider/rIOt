import { useState, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type DevicePatchInfo } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import { useFeatures } from '../hooks/useFeatures'
import { isVersionOlder } from '../utils/version'

import ConfirmModal from '../components/ConfirmModal'
import SetupGuide from '../components/SetupGuide'

type SortKey = 'hostname' | 'status' | 'arch' | 'last_heartbeat' | 'short_id' | 'agent_version'
type SortDir = 'asc' | 'desc'

export default function FleetOverview() {
  const { data: devices, isLoading, wsConnected } = useDevices()
  const { isEnabled } = useFeatures()
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
  const [showGuide, setShowGuide] = useState(false)
  const [showPatchReview, setShowPatchReview] = useState(false)
  const [patchDetail, setPatchDetail] = useState<DevicePatchInfo[] | null>(null)
  const [patchDetailLoading, setPatchDetailLoading] = useState(false)
  const [patchResult, setPatchResult] = useState<{ sent: number; queued: number; skipped: number; total: number } | null>(null)
  const [updateResult, setUpdateResult] = useState<{ sent: number; skipped: number } | null>(null)
  const [showUpdateConfirm, setShowUpdateConfirm] = useState(false)
  const { data: agentVersions = [] } = useQuery({
    queryKey: ['agent-versions'],
    queryFn: api.getAgentVersions,
    refetchInterval: 30_000,
  })
  const outdatedVersions = useMemo(() => {
    if (!latestVersion) return []
    return agentVersions.filter(v => v.version !== 'dev' && v.version !== 'unknown' && isVersionOlder(v.version, latestVersion))
  }, [agentVersions, latestVersion])
  const outdatedCount = outdatedVersions.reduce((n, v) => n + v.count, 0)

  const bulkUpdateMutation = useMutation({
    mutationFn: async () => {
      let totalSent = 0, totalSkipped = 0
      for (const v of outdatedVersions) {
        const result = await api.bulkUpdateAgents(v.version)
        totalSent += result.sent
        totalSkipped += result.skipped
      }
      return { sent: totalSent, skipped: totalSkipped }
    },
    onSuccess: (data) => {
      setUpdateResult(data)
      queryClient.invalidateQueries({ queryKey: ['agent-versions'] })
      setTimeout(() => setUpdateResult(null), 8000)
    },
  })

  async function handleOpenPatchReview() {
    setPatchDetailLoading(true)
    setShowPatchReview(true)
    try {
      const detail = await api.getPatchStatusDetail()
      setPatchDetail(detail)
    } catch { setPatchDetail(null) }
    setPatchDetailLoading(false)
  }

  async function handlePatchAll() {
    try {
      const result = await api.bulkPatchDevices('full')
      setPatchResult(result)
      setTimeout(() => setPatchResult(null), 8000)
    } catch { /* ignore */ }
    setShowPatchReview(false)
    setPatchDetail(null)
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
          <StatCard label="OS Updates" value={patchMap.size} color={patchMap.size > 0 ? 'text-cyan-400' : 'text-white'} />
        </div>
      )}

      <div className="flex items-center gap-4 flex-wrap">
        <input
          type="text"
          placeholder="Search devices..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 min-w-0 bg-gray-900 border border-gray-700 rounded-lg px-4 py-2 text-sm text-gray-100 placeholder-gray-500 focus:outline-none focus:border-blue-500"
        />
        <div className="flex gap-4">
          {outdatedCount > 0 && (
            <button
              onClick={() => setShowUpdateConfirm(true)}
              className="px-4 py-2 text-sm text-amber-400 hover:text-amber-300 border border-amber-800/50 rounded-lg font-medium transition-colors whitespace-nowrap"
            >
              Update Agents ({outdatedCount})
            </button>
          )}
          {patchMap.size > 0 && (
            <button
              onClick={handleOpenPatchReview}
              className="px-4 py-2 text-sm text-cyan-400 hover:text-cyan-300 border border-cyan-800/50 rounded-lg font-medium transition-colors whitespace-nowrap"
            >
              Review Patches ({patchMap.size})
            </button>
          )}
          <button
            onClick={() => setShowGuide(true)}
            className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors whitespace-nowrap"
          >
            + Add Device
          </button>
        </div>
      </div>

      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-x-auto scrollbar-thin">
        {isLoading ? (
          <div className="p-8 text-center text-gray-500">Loading...</div>
        ) : (
          <table className="w-full min-w-[640px]">
            <thead className="bg-gray-900/50 border-b border-gray-800">
              <tr>
                <th className="pl-4 pr-0 py-3 w-4" onClick={() => toggleSort('status')} title="Sort by status">
                  <span className="sr-only">Status</span>
                </th>
                <SortHeader k="hostname">Device</SortHeader>
                {isEnabled('docker') && (
                <th className="px-2 py-3 text-center text-xs font-medium text-gray-400 w-10" title="Docker">
                  <svg className="w-5 h-5 mx-auto text-gray-400" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M13.983 11.078h2.119a.186.186 0 00.186-.185V9.006a.186.186 0 00-.186-.186h-2.119a.186.186 0 00-.185.186v1.887c0 .102.083.185.185.185m-2.954-5.43h2.118a.186.186 0 00.186-.186V3.574a.186.186 0 00-.186-.185h-2.118a.186.186 0 00-.185.185v1.888c0 .102.082.186.185.186m0 2.716h2.118a.187.187 0 00.186-.186V6.29a.186.186 0 00-.186-.185h-2.118a.186.186 0 00-.185.185v1.887c0 .102.082.186.185.186m-2.93 0h2.12a.186.186 0 00.184-.186V6.29a.185.185 0 00-.185-.185H8.1a.186.186 0 00-.185.185v1.887c0 .102.083.186.185.186m-2.964 0h2.119a.186.186 0 00.185-.186V6.29a.186.186 0 00-.185-.185H5.136a.186.186 0 00-.186.185v1.887c0 .102.084.186.186.186m5.893 2.715h2.118a.186.186 0 00.186-.185V9.006a.186.186 0 00-.186-.186h-2.118a.186.186 0 00-.185.186v1.887c0 .102.082.185.185.185m-2.93 0h2.12a.185.185 0 00.184-.185V9.006a.185.185 0 00-.184-.186h-2.12a.185.185 0 00-.184.186v1.887c0 .102.083.185.185.185m-2.964 0h2.119a.186.186 0 00.185-.185V9.006a.186.186 0 00-.185-.186H5.136a.186.186 0 00-.186.186v1.887c0 .102.084.185.186.185m-2.92 0h2.12a.185.185 0 00.184-.185V9.006a.185.185 0 00-.184-.186h-2.12a.186.186 0 00-.186.186v1.887c0 .102.084.185.186.185M23.763 9.89c-.065-.051-.672-.51-1.954-.51-.338.001-.676.03-1.01.087-.248-1.7-1.653-2.53-1.716-2.566l-.344-.199-.226.327c-.284.438-.49.922-.612 1.43-.23.97-.09 1.882.403 2.661-.595.332-1.55.413-1.744.42H.751a.751.751 0 00-.75.748 11.376 11.376 0 00.692 4.062c.545 1.428 1.355 2.48 2.41 3.124 1.18.723 3.1 1.137 5.275 1.137.983.003 1.963-.086 2.93-.266a12.248 12.248 0 003.823-1.389c.98-.567 1.86-1.288 2.61-2.136 1.252-1.418 1.998-2.997 2.553-4.4h.221c1.372 0 2.215-.549 2.68-1.009.309-.293.55-.65.707-1.046l.098-.288z" />
                  </svg>
                </th>
                )}
                <SortHeader k="short_id">ID</SortHeader>
                <SortHeader k="arch">Arch</SortHeader>
                <SortHeader k="agent_version">Version</SortHeader>
                <SortHeader k="last_heartbeat">Last Seen</SortHeader>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">IP</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">Updates</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Tags</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {filtered.map((d) => (
                <tr key={d.id} className="hover:bg-gray-800/30 transition-colors">
                  <td className="pl-4 pr-0 py-3 w-4">
                    <span
                      className={`block w-2.5 h-2.5 rounded-full ${d.status === 'online' ? 'bg-emerald-400' : d.status === 'warning' ? 'bg-amber-400' : 'bg-red-400'}`}
                      title={d.status.charAt(0).toUpperCase() + d.status.slice(1)}
                    />
                  </td>
                  <td className="px-4 py-3">
                    <Link to={`/devices/${d.id}`} className="text-blue-400 hover:text-blue-300 font-medium uppercase flex items-center gap-1.5">
                      {d.hostname}
                      {serverUpdate?.server_host_device_id === d.id && (
                        <span title="rIOt Server Host" className="text-violet-400 flex-shrink-0">
                          <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                            <path fillRule="evenodd" d="M2 5a2 2 0 012-2h12a2 2 0 012 2v2a2 2 0 01-2 2H4a2 2 0 01-2-2V5zm14 1a1 1 0 11-2 0 1 1 0 012 0zM2 13a2 2 0 012-2h12a2 2 0 012 2v2a2 2 0 01-2 2H4a2 2 0 01-2-2v-2zm14 1a1 1 0 11-2 0 1 1 0 012 0z" clipRule="evenodd" />
                          </svg>
                        </span>
                      )}
                    </Link>
                  </td>
                  {isEnabled('docker') && (
                  <td className="px-2 py-3 pr-4 text-center">
                    {d.docker_available ? (
                      <Link
                        to={`/devices/${d.id}/containers`}
                        className="inline-flex items-center gap-1 justify-center px-1.5 h-7 rounded-md border border-gray-700 bg-gray-800 hover:bg-gray-700 hover:border-blue-500/50 text-blue-400 hover:text-blue-300 transition-all cursor-pointer"
                        title={`${d.docker_container_count || 0} container${d.docker_container_count === 1 ? '' : 's'}`}
                      >
                        <svg className="w-4 h-4 flex-shrink-0" viewBox="0 0 24 24" fill="currentColor">
                          <path d="M13.983 11.078h2.119a.186.186 0 00.186-.185V9.006a.186.186 0 00-.186-.186h-2.119a.186.186 0 00-.185.186v1.887c0 .102.083.185.185.185m-2.954-5.43h2.118a.186.186 0 00.186-.186V3.574a.186.186 0 00-.186-.185h-2.118a.186.186 0 00-.185.185v1.888c0 .102.082.186.185.186m0 2.716h2.118a.187.187 0 00.186-.186V6.29a.186.186 0 00-.186-.185h-2.118a.186.186 0 00-.185.185v1.887c0 .102.082.186.185.186m-2.93 0h2.12a.186.186 0 00.184-.186V6.29a.185.185 0 00-.185-.185H8.1a.186.186 0 00-.185.185v1.887c0 .102.083.186.185.186m-2.964 0h2.119a.186.186 0 00.185-.186V6.29a.186.186 0 00-.185-.185H5.136a.186.186 0 00-.186.185v1.887c0 .102.084.186.186.186m5.893 2.715h2.118a.186.186 0 00.186-.185V9.006a.186.186 0 00-.186-.186h-2.118a.186.186 0 00-.185.186v1.887c0 .102.082.185.185.185m-2.93 0h2.12a.185.185 0 00.184-.185V9.006a.185.185 0 00-.184-.186h-2.12a.185.185 0 00-.184.186v1.887c0 .102.083.185.185.185m-2.964 0h2.119a.186.186 0 00.185-.185V9.006a.186.186 0 00-.185-.186H5.136a.186.186 0 00-.186.186v1.887c0 .102.084.185.186.185m-2.92 0h2.12a.185.185 0 00.184-.185V9.006a.185.185 0 00-.184-.186h-2.12a.186.186 0 00-.186.186v1.887c0 .102.084.185.186.185M23.763 9.89c-.065-.051-.672-.51-1.954-.51-.338.001-.676.03-1.01.087-.248-1.7-1.653-2.53-1.716-2.566l-.344-.199-.226.327c-.284.438-.49.922-.612 1.43-.23.97-.09 1.882.403 2.661-.595.332-1.55.413-1.744.42H.751a.751.751 0 00-.75.748 11.376 11.376 0 00.692 4.062c.545 1.428 1.355 2.48 2.41 3.124 1.18.723 3.1 1.137 5.275 1.137.983.003 1.963-.086 2.93-.266a12.248 12.248 0 003.823-1.389c.98-.567 1.86-1.288 2.61-2.136 1.252-1.418 1.998-2.997 2.553-4.4h.221c1.372 0 2.215-.549 2.68-1.009.309-.293.55-.65.707-1.046l.098-.288z" />
                        </svg>
                        <span className="text-xs font-medium">{d.docker_container_count || 0}</span>
                      </Link>
                    ) : (
                      <span className="text-gray-700">-</span>
                    )}
                  </td>
                  )}
                  <td className="px-4 py-3 font-mono text-xs text-gray-500">{d.short_id}</td>
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
                  <td className="px-4 py-3 text-sm">
                    {(() => {
                      const count = patchMap.get(d.id)
                      if (!count) return <span className="text-gray-600">-</span>
                      return <span className="text-cyan-400">{count}</span>
                    })()}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex gap-1 flex-wrap">
                      {d.tags?.map(t => (
                        <span key={t} className="px-1.5 py-0.5 rounded bg-gray-800 text-xs text-gray-400">{t}</span>
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
              {filtered.length === 0 && search && (
                <tr>
                  <td colSpan={11} className="px-4 py-8 text-center text-gray-500">
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

      {updateResult && (
        <div className="px-4 py-2 bg-amber-900/30 border border-amber-800 rounded text-sm text-amber-400 flex items-center justify-between">
          <span>Agent update dispatched: {updateResult.sent} sent{updateResult.skipped > 0 && `, ${updateResult.skipped} skipped (offline)`}</span>
          <button onClick={() => setUpdateResult(null)} className="text-amber-600 hover:text-amber-400 ml-4">&times;</button>
        </div>
      )}

      {showPatchReview && (
        <PatchReviewModal
          loading={patchDetailLoading}
          patches={patchDetail}
          onConfirm={handlePatchAll}
          onClose={() => { setShowPatchReview(false); setPatchDetail(null) }}
        />
      )}

      {showUpdateConfirm && (
        <ConfirmModal
          title="Update Agents"
          message={`Send update command to ${outdatedCount} outdated agent${outdatedCount !== 1 ? 's' : ''}? They will download and install the latest version (${latestVersion}).`}
          confirmLabel={bulkUpdateMutation.isPending ? 'Updating...' : 'Update All'}
          confirmVariant="primary"
          onConfirm={() => { bulkUpdateMutation.mutate(); setShowUpdateConfirm(false) }}
          onCancel={() => setShowUpdateConfirm(false)}
        />
      )}

    </div>
  )
}

function PatchReviewModal({ loading, patches, onConfirm, onClose }: {
  loading: boolean
  patches: DevicePatchInfo[] | null
  onConfirm: () => void
  onClose: () => void
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const toggle = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  const totalPkgs = patches?.reduce((n, d) => n + d.pending_updates, 0) ?? 0
  const totalSecurity = patches?.reduce((n, d) => n + d.security_count, 0) ?? 0

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-3xl mx-4 max-h-[85vh] flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-4 border-b border-gray-800">
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-semibold text-white">Pending OS Patches</h3>
            <button onClick={onClose} className="text-gray-500 hover:text-white transition-colors">
              <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
              </svg>
            </button>
          </div>
          {!loading && patches && (
            <p className="text-sm text-gray-400 mt-1">
              {totalPkgs} package{totalPkgs !== 1 ? 's' : ''} across {patches.length} device{patches.length !== 1 ? 's' : ''}
              {totalSecurity > 0 && <span className="text-red-400 ml-2">({totalSecurity} security)</span>}
            </p>
          )}
        </div>

        <div className="flex-1 overflow-y-auto scrollbar-thin px-6 py-4">
          {loading ? (
            <div className="text-center text-gray-500 py-8">Loading patch details...</div>
          ) : !patches || patches.length === 0 ? (
            <div className="text-center text-gray-500 py-8">No pending updates on any device.</div>
          ) : (
            <div className="space-y-2">
              {patches.map(d => (
                <div key={d.device_id} className="border border-gray-800 rounded-lg overflow-hidden">
                  <button
                    onClick={() => toggle(d.device_id)}
                    className="w-full flex items-center justify-between px-4 py-3 hover:bg-gray-800/50 transition-colors text-left"
                  >
                    <div className="flex items-center gap-3">
                      <span className="text-white font-medium">{d.hostname || d.device_id}</span>
                      {d.package_manager && <span className="text-xs text-gray-500">{d.package_manager}</span>}
                    </div>
                    <div className="flex items-center gap-3 text-sm">
                      {d.security_count > 0 && (
                        <span className="text-red-400">{d.security_count} security</span>
                      )}
                      <span className="text-cyan-400">{d.pending_updates} pkg{d.pending_updates !== 1 ? 's' : ''}</span>
                      <span className="text-gray-500">{expanded.has(d.device_id) ? '\u25B2' : '\u25BC'}</span>
                    </div>
                  </button>
                  {expanded.has(d.device_id) && d.updates && (
                    <div className="border-t border-gray-800 max-h-64 overflow-y-auto scrollbar-thin">
                      <table className="w-full text-sm">
                        <thead>
                          <tr className="text-gray-500 text-xs uppercase">
                            <th className="text-left px-4 py-2">Package</th>
                            <th className="text-left px-4 py-2">Current</th>
                            <th className="text-left px-4 py-2">New</th>
                            <th className="text-left px-4 py-2">Sec</th>
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-gray-800/50">
                          {d.updates.map(u => (
                            <tr key={u.name}>
                              <td className="px-4 py-1.5 font-mono text-xs text-gray-200">{u.name}</td>
                              <td className="px-4 py-1.5 font-mono text-xs text-gray-500">{u.current_ver || '-'}</td>
                              <td className="px-4 py-1.5 font-mono text-xs text-gray-400">{u.new_ver}</td>
                              <td className="px-4 py-1.5">{u.is_security ? <span className="text-red-400 text-xs">Yes</span> : ''}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="px-6 py-4 border-t border-gray-800 flex items-center justify-between">
          <p className="text-xs text-gray-500">Devices must have <code className="text-gray-400">commands.allow_patching</code> enabled.</p>
          <div className="flex gap-3">
            <button onClick={onClose} className="px-4 py-2 text-sm text-gray-400 hover:text-white">Cancel</button>
            <button
              onClick={onConfirm}
              disabled={loading || !patches?.length}
              className="px-4 py-2 bg-cyan-600 hover:bg-cyan-500 text-white text-sm rounded-md transition-colors disabled:opacity-50"
            >
              Update All Devices
            </button>
          </div>
        </div>
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


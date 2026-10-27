import { useState, useMemo, useRef } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type DevicePatchInfo } from '../api/client'
import { errorMessage } from '../api/errors'
import { useDevices } from '../hooks/useDevices'
import { useFeatures } from '../hooks/useFeatures'
import { useActivity } from '../hooks/useActivity'
import { isAgentUpdating, isExpectedOffline } from '../utils/activity'
import { isVersionOlder } from '../utils/version'
import { STALE_PATCH_LABEL, STALE_SECURITY_QUALIFIER, isBlocked, isNeedsYou, normalizeFreshness } from '../utils/patchFlow'
import { pendingExtraNames } from '../utils/extraUpdaters'

import ActivityLabel from '../components/ActivityLabel'
import AgentUpdatingVersion from '../components/AgentUpdatingVersion'
import PatchBadge from '../components/PatchBadge'
import RebootClassBadge from '../components/RebootClassBadge'
import SetupGuide from '../components/SetupGuide'
import { Button, Card, ConfirmDialog, Modal, PageHeader, QueryState, RelativeTime, Skeleton, useToast } from '../components/ui'

type SortKey = 'hostname' | 'status' | 'arch' | 'last_heartbeat' | 'short_id' | 'agent_version'
type SortDir = 'asc' | 'desc'

export default function FleetOverview() {
  const devicesQuery = useDevices()
  const { data: devices, wsConnected } = devicesQuery
  const toast = useToast()
  const { isEnabled } = useFeatures()
  // EXPECT-001: server-computed activity per device; empty until it loads or when it is unavailable.
  const { byDevice: activityByDevice, newestAgentVersion } = useActivity()
  const { data: serverUpdate } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    staleTime: 60 * 60 * 1000,
  })
  const { data: patchStatus } = useQuery({
    queryKey: ['patch-status', 'all'],
    queryFn: () => api.getPatchStatus(true),
    refetchInterval: wsConnected ? 120_000 : 60_000,
  })

  const latestVersion = serverUpdate?.latest_version
  // Every registered device has a row (all=true); patchMap holds them all for the
  // badge, pendingPatchCount only those with updates or a reboot requirement.
  const patchMap = useMemo(() => {
    const m = new Map<string, DevicePatchInfo>()
    patchStatus?.forEach(p => m.set(p.device_id, p))
    return m
  }, [patchStatus])
  const pendingPatchCount = useMemo(
    () => (patchStatus ?? []).filter(p => p.pending_updates > 0 || p.reboot_required || pendingExtraNames(p.extra_updaters).length > 0).length,
    [patchStatus],
  )
  const needsYouIds = useMemo(
    () => new Set((patchStatus ?? []).filter(p => isNeedsYou(p.patch_badge?.kind)).map(p => p.device_id)),
    [patchStatus],
  )
  // Blocked hosts have their own count and filter and are not part of "N hosts need you".
  const blockedIds = useMemo(
    () => new Set((patchStatus ?? []).filter(p => isBlocked(p.patch_badge?.kind)).map(p => p.device_id)),
    [patchStatus],
  )
  const [needsYouOnly, setNeedsYouOnly] = useState(false)
  const [blockedOnly, setBlockedOnly] = useState(false)
  const queryClient = useQueryClient()
  const [showGuide, setShowGuide] = useState(false)
  const [showPatchReview, setShowPatchReview] = useState(false)
  const [patchDetail, setPatchDetail] = useState<DevicePatchInfo[] | null>(null)
  const [patchDetailLoading, setPatchDetailLoading] = useState(false)
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
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agent-versions'] })
    },
  })

  const patchAllMutation = useMutation({
    mutationFn: () => api.bulkPatchDevices('full'),
    onSuccess: (result) => {
      toast.success(
        'Patch commands dispatched',
        `${result.sent} sent, ${result.queued} queued, ${result.skipped} skipped (${result.total} total)`,
      )
      setShowPatchReview(false)
      setPatchDetail(null)
      queryClient.invalidateQueries({ queryKey: ['patch-status'] })
    },
    onError: (e) => {
      toast.error("Couldn't dispatch patches", errorMessage(e))
    },
    onSettled: () => { patchAllInFlight.current = false },
  })
  // Synchronous double-submit guard: `isPending` only disables the button after a
  // re-render, so two clicks in one frame would otherwise patch the fleet twice.
  const patchAllInFlight = useRef(false)
  function handlePatchAll() {
    if (patchAllInFlight.current) return
    patchAllInFlight.current = true
    patchAllMutation.mutate()
  }

  async function handleOpenPatchReview() {
    setPatchDetailLoading(true)
    setShowPatchReview(true)
    try {
      const detail = await api.getPatchStatusDetail()
      setPatchDetail(detail)
    } catch { setPatchDetail(null) }
    setPatchDetailLoading(false)
  }

  // Derive summary from live device data â€” no separate polling needed
  const summary = useMemo(() => {
    if (!devices) return null
    // FR-038: offline inside a window, or powered off, is counted apart from real
    // outages. An escalated device is not expected and stays in the offline count.
    const expectedOffline = devices.filter(d => isExpectedOffline(activityByDevice.get(d.id), d.status)).length
    return {
      total_devices: devices.length,
      online_count: devices.filter(d => d.status === 'online').length,
      offline_count: devices.filter(d => d.status === 'offline').length - expectedOffline,
      expected_offline_count: expectedOffline,
      warning_count: devices.filter(d => d.status === 'warning').length,
    }
  }, [devices, activityByDevice])
  const [search, setSearch] = useState('')
  const [sortKey, setSortKey] = useState<SortKey>('hostname')
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const filtered = useMemo(() => {
    if (!devices) return []
    let list = devices.filter((d) => {
      if (needsYouOnly && !needsYouIds.has(d.id)) return false
      if (blockedOnly && !blockedIds.has(d.id)) return false
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
  }, [devices, search, sortKey, sortDir, needsYouOnly, needsYouIds, blockedOnly, blockedIds])

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

  function isAgentOutdated(version?: string) {
    if (!version || !latestVersion || version === 'dev') return false
    return isVersionOlder(version, latestVersion)
  }

  return (
    <div className="min-w-0 space-y-6">
      <PageHeader
        title="Devices"
        description="All registered machines, their health and available updates."
        actions={<Button variant="primary" onClick={() => setShowGuide(true)}>+ Add Device</Button>}
      />
      {summary && (
        <div className={`grid grid-cols-2 gap-4 ${summary.expected_offline_count > 0 ? 'sm:grid-cols-6' : 'sm:grid-cols-5'}`}>
          <StatCard label="Total Devices" value={summary.total_devices} />
          <StatCard label="Online" value={summary.online_count} color="text-emerald-400" />
          <StatCard label="Offline" value={summary.offline_count} color="text-red-400" />
          {summary.expected_offline_count > 0 && (
            <StatCard label="Expected offline" value={summary.expected_offline_count} color="text-sky-400" />
          )}
          <StatCard label="Warnings" value={summary.warning_count} color="text-amber-400" />
          <StatCard label="OS Updates" value={pendingPatchCount} color={pendingPatchCount > 0 ? 'text-cyan-400' : 'text-white'} />
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
        <div className="flex flex-wrap gap-3">
          {needsYouIds.size > 0 && (
            <button
              type="button"
              aria-pressed={needsYouOnly}
              onClick={() => { setNeedsYouOnly(v => !v); setBlockedOnly(false) }}
              className={`px-4 py-2 text-sm border rounded-lg font-medium transition-colors whitespace-nowrap ${
                needsYouOnly ? 'bg-red-900/40 text-red-200 border-red-700' : 'text-red-300 hover:text-red-200 border-red-800/50'
              }`}
            >
              {needsYouIds.size} {needsYouIds.size === 1 ? 'host needs' : 'hosts need'} you
            </button>
          )}
          {blockedIds.size > 0 && (
            <button
              type="button"
              aria-pressed={blockedOnly}
              onClick={() => { setBlockedOnly(v => !v); setNeedsYouOnly(false) }}
              className={`px-4 py-2 text-sm border rounded-lg font-medium transition-colors whitespace-nowrap ${
                blockedOnly ? 'bg-amber-900/40 text-amber-200 border-amber-700' : 'text-amber-300 hover:text-amber-200 border-amber-800/50'
              }`}
            >
              {blockedIds.size} blocked
            </button>
          )}
          {outdatedCount > 0 && (
            <button
              onClick={() => setShowUpdateConfirm(true)}
              className="px-4 py-2 text-sm text-amber-400 hover:text-amber-300 border border-amber-800/50 rounded-lg font-medium transition-colors whitespace-nowrap"
            >
              Update Agents ({outdatedCount})
            </button>
          )}
          {pendingPatchCount > 0 && (
            <button
              onClick={handleOpenPatchReview}
              className="px-4 py-2 text-sm text-cyan-400 hover:text-cyan-300 border border-cyan-800/50 rounded-lg font-medium transition-colors whitespace-nowrap"
            >
              Review Patches ({pendingPatchCount})
            </button>
          )}
        </div>
      </div>

      <QueryState
        query={devicesQuery}
        errorTitle="Couldn't load devices"
        isEmpty={list => list.length === 0 && !search}
        empty={<SetupGuide inline />}
        loading={<DeviceTableSkeleton />}
      >
        {() => (
      <Card padding="none" className="overflow-x-auto scrollbar-thin">
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
                {isEnabled('tags') && (
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase">Tags</th>
                )}
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {filtered.map((d) => (
                <tr key={d.id} className="hover:bg-gray-800/30 transition-colors">
                  <td className="pl-4 pr-0 py-3 w-4">
                    {isExpectedOffline(activityByDevice.get(d.id), d.status) ? (
                      // FR-043: offline stays visible as a hollow dot, distinct from both online and a real outage.
                      <span
                        className="block w-2.5 h-2.5 rounded-full border-2 border-sky-400 bg-transparent"
                        title="Offline (expected)"
                        data-testid="status-dot-expected-offline"
                      />
                    ) : (
                      <span
                        className={`block w-2.5 h-2.5 rounded-full ${d.status === 'online' ? 'bg-emerald-400' : d.status === 'warning' ? 'bg-amber-400' : 'bg-red-400'}`}
                        title={d.status.charAt(0).toUpperCase() + d.status.slice(1)}
                      />
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <Link to={`/devices/${d.id}`} className="text-blue-400 hover:text-blue-300 font-medium flex items-center gap-1.5">
                      {d.hostname}
                      {serverUpdate?.server_host_device_id === d.id && (
                        <span title="rIOt Server Host" className="text-violet-400 flex-shrink-0">
                          <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                            <path fillRule="evenodd" d="M2 5a2 2 0 012-2h12a2 2 0 012 2v2a2 2 0 01-2 2H4a2 2 0 01-2-2V5zm14 1a1 1 0 11-2 0 1 1 0 012 0zM2 13a2 2 0 012-2h12a2 2 0 012 2v2a2 2 0 01-2 2H4a2 2 0 01-2-2v-2zm14 1a1 1 0 11-2 0 1 1 0 012 0z" clipRule="evenodd" />
                          </svg>
                        </span>
                      )}
                    </Link>
                    {activityByDevice.get(d.id) && !isAgentUpdating(activityByDevice.get(d.id)) && (
                      <div className="mt-1">
                        <ActivityLabel activity={activityByDevice.get(d.id)!} newestAgentVersion={newestAgentVersion} />
                      </div>
                    )}
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
                    {isAgentUpdating(activityByDevice.get(d.id)) ? (
                      <AgentUpdatingVersion activity={activityByDevice.get(d.id)!} newestAgentVersion={newestAgentVersion} />
                    ) : d.agent_version ? (
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
                  <td className="px-4 py-3 text-sm text-gray-400"><RelativeTime value={d.last_heartbeat} fallback="Never" pastOnly /></td>
                  <td className="px-4 py-3 text-sm text-gray-400 font-mono">{d.primary_ip || '-'}</td>
                  <td className="px-4 py-3 text-sm">
                    <div className="flex items-center gap-2">
                      {(() => {
                        const row = patchMap.get(d.id)
                        // A count read from stale package lists is not actionable (BR-005).
                        if (normalizeFreshness(row?.lists_freshness) === 'stale') {
                          return <span className="text-amber-400 text-xs" title={STALE_PATCH_LABEL}>Lists out of date</span>
                        }
                        const count = row?.pending_updates
                        if (!count) return <span className="text-gray-600">-</span>
                        return <span className="text-cyan-400">{count}</span>
                      })()}
                      {(() => {
                        const badge = patchMap.get(d.id)?.patch_badge
                        return badge ? <PatchBadge badge={badge} /> : null
                      })()}
                      {isEnabled('docker') && (
                        d.docker_container_count > 0 && d.docker_auto_update_container_count > 0 ? (
                          <span
                            className="px-1.5 py-0.5 rounded bg-blue-900/40 text-[10px] font-medium text-blue-400 border border-blue-800/50"
                            title={`${d.docker_auto_update_container_count} of ${d.docker_container_count} container${d.docker_container_count === 1 ? '' : 's'} on auto-update`}
                          >
                            Auto: <span className="font-mono">{d.docker_auto_update_container_count}/{d.docker_container_count}</span>
                          </span>
                        ) : d.has_auto_update ? (
                          <span className="px-1.5 py-0.5 rounded bg-blue-900/40 text-[10px] font-medium text-blue-400 border border-blue-800/50" title="Docker auto-update enabled">Auto</span>
                        ) : null
                      )}
                    </div>
                  </td>
                  {isEnabled('tags') && (
                  <td className="px-4 py-3">
                    <div className="flex gap-1 flex-wrap">
                      {d.tags?.map(t => (
                        <span key={t} className="px-1.5 py-0.5 rounded bg-gray-800 text-xs text-gray-400">{t}</span>
                      ))}
                    </div>
                  </td>
                  )}
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
      </Card>
        )}
      </QueryState>

      {showGuide && <SetupGuide onClose={() => setShowGuide(false)} />}

      {showPatchReview && (
        <PatchReviewModal
          loading={patchDetailLoading}
          patches={patchDetail}
          pending={patchAllMutation.isPending}
          onConfirm={handlePatchAll}
          onClose={() => { setShowPatchReview(false); setPatchDetail(null) }}
        />
      )}

      <ConfirmDialog
        open={showUpdateConfirm}
        variant="primary"
        title="Update Agents"
        message={`Send update command to ${outdatedCount} outdated agent${outdatedCount !== 1 ? 's' : ''}? They will download and install the latest version (${latestVersion}).`}
        confirmLabel="Update all"
        pendingLabel="Updating…"
        onConfirm={() => bulkUpdateMutation.mutateAsync()}
        successToast={result => ({
          title: 'Agent update dispatched',
          message: `${result.sent} sent${result.skipped > 0 ? `, ${result.skipped} skipped (offline)` : ''}`,
        })}
        onClose={() => setShowUpdateConfirm(false)}
      />

    </div>
  )
}

function DeviceTableSkeleton() {
  return (
    <Card padding="none">
      <div className="space-y-px">
        {Array.from({ length: 5 }, (_, i) => (
          <div key={i} className="flex items-center gap-4 px-4 py-3">
            <Skeleton className="h-2.5 w-2.5 rounded-full" />
            <Skeleton className="h-4 w-40" />
            <Skeleton className="ml-auto h-4 w-24" />
          </div>
        ))}
      </div>
    </Card>
  )
}

function PatchReviewModal({ loading, patches, pending, onConfirm, onClose }: {
  loading: boolean
  patches: DevicePatchInfo[] | null
  pending: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  // Focus Cancel, never the fleet-wide confirm, so the gesture that opened the
  // review can't also dispatch patches (SEC-001).
  const cancelRef = useRef<HTMLButtonElement>(null)

  const toggle = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  // Package totals leave out hosts whose lists are stale (OQ-008, BR-005). Security
  // counts never disappear (SEC-007): stale hosts stay in the security total, and the
  // part that comes from out-of-date lists is called out.
  const isStale = (d: DevicePatchInfo) => normalizeFreshness(d.lists_freshness) === 'stale'
  const staleCount = patches?.filter(isStale).length ?? 0
  const totalPkgs = patches?.reduce((n, d) => n + (isStale(d) ? 0 : d.pending_updates), 0) ?? 0
  const totalSecurity = patches?.reduce((n, d) => n + d.security_count, 0) ?? 0
  const staleSecurity = patches?.reduce((n, d) => n + (isStale(d) ? d.security_count : 0), 0) ?? 0

  return (
    <Modal
      open
      size="xl"
      title="Pending OS Patches"
      description={
        !loading && patches ? (
          <>
            {totalPkgs} package{totalPkgs !== 1 ? 's' : ''} across {patches.length} device{patches.length !== 1 ? 's' : ''}
            {totalSecurity > 0 && (
              <span className="text-danger-fg ml-2">
                ({totalSecurity} security{staleSecurity > 0 ? `, ${staleSecurity} of them from out-of-date lists` : ''})
              </span>
            )}
            {staleCount > 0 && (
              <span className="text-text-muted">
                {' · '}{staleCount} device{staleCount !== 1 ? 's' : ''} with out-of-date package lists not counted
              </span>
            )}
          </>
        ) : undefined
      }
      pending={pending}
      initialFocusRef={cancelRef}
      onClose={onClose}
      bodyClassName="py-4"
      footer={
        <div className="flex w-full flex-wrap items-center justify-between gap-3">
          <p className="text-xs text-text-muted">Devices must have <code className="text-text-muted">commands.allow_patching</code> enabled.</p>
          <div className="flex gap-3">
            <Button ref={cancelRef} variant="secondary" disabled={pending} onClick={onClose}>Cancel</Button>
            <Button
              variant="primary"
              onClick={onConfirm}
              pending={pending}
              pendingLabel="Dispatching…"
              disabled={loading || !patches?.length}
            >
              Update All Devices
            </Button>
          </div>
        </div>
      }
    >
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
                      {d.patch_badge && <PatchBadge badge={d.patch_badge} />}
                      {d.security_count > 0 && (
                        <span className="text-red-400">
                          {d.security_count} security{isStale(d) ? ` ${STALE_SECURITY_QUALIFIER}` : ''}
                        </span>
                      )}
                      {d.reboot_class_count > 0 && !isStale(d) && (
                        <span className="text-violet-300" title="GPU driver / kernel packages — applied only during the maintenance window when reboot-class gating is on">
                          {d.reboot_class_count} reboot-class
                        </span>
                      )}
                      {pendingExtraNames(d.extra_updaters).map(name => (
                        <span key={name} className="text-cyan-400" title="Platform updater outside the package manager">{name}</span>
                      ))}
                      {isStale(d) ? (
                        <span className="text-amber-400">{STALE_PATCH_LABEL}</span>
                      ) : (d.pending_updates > 0 || pendingExtraNames(d.extra_updaters).length === 0) && (
                        <span className="text-cyan-400">{d.pending_updates} pkg{d.pending_updates !== 1 ? 's' : ''}</span>
                      )}
                      <span className="text-gray-500">{expanded.has(d.device_id) ? '▲' : '▼'}</span>
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
                              <td className="px-4 py-1.5 font-mono text-xs text-gray-200">
                                {u.name}
                                <RebootClassBadge cls={u.class} />
                              </td>
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
    </Modal>
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


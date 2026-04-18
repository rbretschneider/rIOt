import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { settingsApi } from '../api/settings'
import { Link } from 'react-router-dom'
import type { ContainerInfo, AlertRule, ContainerLogEntry } from '../types/models'
import { displayName, formatBytes, formatContainerUptime, isSensitiveKey, maskValue, matchesContainerScope, statusColor } from '../utils/docker'
import ContainerStatusBadge from './ContainerStatusBadge'
import ContainerMetricChart from './ContainerMetricChart'
import GaugeBar from './GaugeBar'
import ConfirmModal from './ConfirmModal'
import Terminal from './Terminal'
import { Section, InfoItem } from './Section'

const EVENTS_PER_PAGE = 15

type Tab = 'general' | 'network' | 'volumes' | 'logs' | 'terminal'

/** Reusable content (tabs + actions) for a container — used both in the full page and any other context. */
export function ContainerDetailContent({ container: c, deviceId, terminalEnabled }: { container: ContainerInfo; deviceId?: string; terminalEnabled?: boolean }) {
  const [tab, setTab] = useState<Tab>('general')
  const [confirmAction, setConfirmAction] = useState<string | null>(null)
  const name = displayName(c.riot, c.name)

  const commandMutation = useMutation({
    mutationFn: ({ action, params }: { action: string; params: Record<string, unknown> }) =>
      api.sendCommand(deviceId!, action, params),
    onSuccess: () => { setTimeout(() => commandMutation.reset(), 5000) },
    onError: () => { setTimeout(() => commandMutation.reset(), 5000) },
  })

  const tabs: { key: Tab; label: string; show: boolean }[] = [
    { key: 'general', label: 'General', show: true },
    { key: 'network', label: 'Network', show: true },
    { key: 'volumes', label: 'Volumes', show: true },
    { key: 'logs', label: 'Logs', show: !!deviceId },
    { key: 'terminal', label: 'Terminal', show: !!terminalEnabled },
  ]

  return (
    <div>
      {/* Header bar with status + actions */}
      <div className="flex items-center justify-between flex-wrap gap-3 mb-4">
        <div className="flex items-center gap-2">
          <ContainerStatusBadge state={c.state} />
          <span className="text-xs text-gray-500 font-mono">{c.image}</span>
        </div>
        {deviceId && (
          <div className="flex gap-1">
            {c.update_available && (
              <ActionBtn label="Update" onClick={() => setConfirmAction('docker_update')} pending={commandMutation.isPending} />
            )}
            {c.state === 'running' && (
              <ActionBtn
                label={commandMutation.isSuccess && commandMutation.variables?.action === 'docker_check_updates' ? 'Queued' : 'Check Updates'}
                onClick={() => commandMutation.mutate({ action: 'docker_check_updates', params: {} })}
                pending={commandMutation.isPending}
              />
            )}
            {c.state === 'running' ? (
              <>
                <ActionBtn label="Restart" onClick={() => setConfirmAction('docker_restart')} pending={commandMutation.isPending} />
                <ActionBtn label="Stop" variant="danger" onClick={() => setConfirmAction('docker_stop')} pending={commandMutation.isPending} />
              </>
            ) : (
              <ActionBtn label="Start" onClick={() => setConfirmAction('docker_start')} pending={commandMutation.isPending} />
            )}
          </div>
        )}
      </div>

      {/* Tabs */}
      <div className="flex gap-1 mb-4">
        {tabs.filter(t => t.show).map(t => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={`px-3 py-1.5 text-xs rounded-md transition-colors ${
              tab === t.key
                ? 'bg-gray-700 text-white'
                : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Command feedback */}
      {commandMutation.isSuccess && (
        <div className="mb-4 px-3 py-2 bg-emerald-900/30 border border-emerald-800 rounded text-sm text-emerald-400">
          Command sent ({commandMutation.data?.status})
        </div>
      )}
      {commandMutation.isError && (
        <div className="mb-4 px-3 py-2 bg-red-900/30 border border-red-800 rounded text-sm text-red-400">
          {(commandMutation.error as Error).message}
        </div>
      )}

      {/* Confirm modal */}
      {confirmAction && (
        <ConfirmModal
          title={`${confirmAction.replace('docker_', '').replace(/^\w/, c => c.toUpperCase())} Container`}
          message={`Are you sure you want to ${confirmAction.replace('docker_', '')} "${name}"?`}
          confirmLabel={confirmAction.replace('docker_', '').replace(/^\w/, c => c.toUpperCase())}
          confirmVariant={confirmAction === 'docker_stop' ? 'danger' : 'primary'}
          onConfirm={() => {
            commandMutation.mutate({ action: confirmAction, params: { container_id: c.id } })
            setConfirmAction(null)
          }}
          onCancel={() => setConfirmAction(null)}
        />
      )}

      {/* Tab content */}
      <div className="space-y-5">
        {tab === 'general' && <GeneralTab container={c} deviceId={deviceId} />}
        {tab === 'network' && <NetworkTab container={c} />}
        {tab === 'volumes' && <VolumesTab container={c} />}
        {tab === 'logs' && deviceId && (
          <ContainerLogsTab containerID={c.short_id} containerName={c.name} deviceId={deviceId} />
        )}
        {tab === 'terminal' && deviceId && c.state === 'running' && (
          <div className="h-96">
            <Terminal deviceId={deviceId} containerId={c.id} />
          </div>
        )}
        {tab === 'terminal' && (!deviceId || c.state !== 'running') && (
          <p className="text-sm text-gray-500">
            {c.state !== 'running' ? 'Container must be running to open a terminal.' : 'Terminal requires device context.'}
          </p>
        )}
      </div>
    </div>
  )
}

interface Props {
  container: ContainerInfo
  onClose: () => void
  terminalEnabled?: boolean
  deviceId?: string
}

/** Slide-out panel wrapper — kept for backwards compatibility but no longer used by DeviceContainers. */
export default function ContainerDetail({ container: c, onClose, terminalEnabled, deviceId }: Props) {
  const name = displayName(c.riot, c.name)

  return (
    <div className="fixed inset-0 z-50 flex justify-end" onClick={onClose}>
      <div className="absolute inset-0 bg-black/50" />
      <div
        className="relative w-full max-w-2xl bg-gray-900 border-l border-gray-800 overflow-y-auto scrollbar-thin"
        onClick={e => e.stopPropagation()}
      >
        <div className="sticky top-0 bg-gray-900 border-b border-gray-800 p-5 z-10">
          <div className="flex items-start justify-between">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                {c.riot?.icon && <span className="text-xl">{c.riot.icon}</span>}
                <h2 className="text-lg font-bold text-white truncate">{name}</h2>
              </div>
              <p className="text-xs text-gray-600 font-mono">{c.short_id}</p>
            </div>
            <button onClick={onClose} className="text-gray-500 hover:text-white text-xl ml-1">&times;</button>
          </div>
        </div>

        <div className="p-5">
          <ContainerDetailContent container={c} deviceId={deviceId} terminalEnabled={terminalEnabled} />
        </div>
      </div>
    </div>
  )
}

function GeneralTab({ container: c, deviceId }: { container: ContainerInfo; deviceId?: string }) {
  const [metricHours, setMetricHours] = useState(() => {
    const saved = localStorage.getItem('container-metric-hours')
    return saved ? Number(saved) : 24
  })
  const memPct = c.mem_limit > 0 ? (c.mem_usage / c.mem_limit) * 100 : 0

  const { data: metrics = [] } = useQuery({
    queryKey: ['container-metrics', deviceId, c.name, metricHours],
    queryFn: () => api.getContainerMetricHistory(deviceId!, c.name, metricHours),
    refetchInterval: 60000,
    enabled: !!deviceId,
  })

  const { data: events = [] } = useQuery({
    queryKey: ['container-events', deviceId, c.name],
    queryFn: async () => {
      const allEvents = await api.getEvents(200, 0)
      const cname = c.name.replace(/^\//, '').toLowerCase()
      return allEvents.filter(e => {
        if (e.device_id !== deviceId) return false
        // Prefer the structured container_name field; fall back to a substring
        // match on the message for events written before the field existed.
        if (e.container_name) {
          return e.container_name.replace(/^\//, '').toLowerCase() === cname
        }
        return e.message.toLowerCase().includes(cname)
      })
    },
    enabled: !!deviceId,
  })
  const [eventsPage, setEventsPage] = useState(0)

  const { data: alertRules = [] } = useQuery({
    queryKey: ['container-alert-rules', deviceId, c.name],
    queryFn: async () => {
      const rules = await api.getDeviceAlertRules(deviceId!)
      // Rule applies to this container if:
      //  - it's a container-scoped metric,
      //  - the container is in the rule's include/exclude scope, and
      //  - for threshold rules, target_name matches (or is empty = any container).
      const cname = c.name.toLowerCase()
      return rules.filter((r: AlertRule) => {
        const m = r.metric
        const isEvent = m === 'container_died' || m === 'container_oom'
        const isThreshold = m === 'container_cpu_percent' || m === 'container_mem_percent' || m === 'container_cpu_limit_percent'
        if (!isEvent && !isThreshold) return false
        if (!matchesContainerScope(r.include_containers, r.exclude_containers, c.name)) return false
        if (isThreshold && r.target_name && r.target_name.toLowerCase() !== cname) return false
        return true
      })
    },
    enabled: !!deviceId,
  })

  const ruleConditionLabel = (r: AlertRule): string => {
    if (r.metric === 'container_died') return 'on die'
    if (r.metric === 'container_oom') return 'on OOM'
    return `${r.operator} ${r.threshold}%`
  }

  const totalEventPages = Math.ceil(events.length / EVENTS_PER_PAGE)
  const safeEventsPage = Math.min(eventsPage, Math.max(0, totalEventPages - 1))
  const pagedEvents = events.slice(safeEventsPage * EVENTS_PER_PAGE, (safeEventsPage + 1) * EVENTS_PER_PAGE)

  return (
    <div className="space-y-6">
      <Section title="Status">
        <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
          <InfoItem label="State" value={c.state} valueClassName={statusColor(c.state)} />
          <InfoItem label="Status" value={c.status} />
          {c.state === 'running' && <InfoItem label="Up" value={formatContainerUptime(c.created)} />}
          {c.restart_policy && <InfoItem label="Restart Policy" value={c.restart_policy} />}
          {(c.restart_count ?? 0) > 0 && <InfoItem label="Restart Count" value={String(c.restart_count)} />}
          {c.health_status && <InfoItem label="Health" value={c.health_status} />}
          {c.update_available === true && <InfoItem label="Image" value="Newer version available" valueClassName="text-amber-400" />}
          {c.update_available === false && <InfoItem label="Image" value="Up to date" valueClassName="text-emerald-400/70" />}
        </div>
      </Section>

      {/* Live metrics */}
      {c.state === 'running' && (c.cpu_percent > 0 || c.mem_usage > 0) && (() => {
        const cpuLimitCores = c.cpu_limit ? c.cpu_limit / 1e9 : 0
        const cpuLimitPct = cpuLimitCores * 100
        const cpuOfLimit = cpuLimitPct > 0 ? (c.cpu_percent / cpuLimitPct) * 100 : 0
        return (
          <Section title="Live Metrics">
            <div className="grid grid-cols-2 gap-4">
              <GaugeBar label={cpuLimitCores > 0 ? `CPU (of ${cpuLimitCores.toFixed(1)} cores)` : 'CPU'} value={cpuLimitCores > 0 ? cpuOfLimit : c.cpu_percent} />
              <GaugeBar label="Memory" value={memPct} />
            </div>
            <div className="grid grid-cols-2 gap-3 mt-4">
              {c.cpu_percent > 0 && (
                <InfoItem label="CPU" value={cpuLimitCores > 0 ? `${c.cpu_percent.toFixed(1)}% (${cpuOfLimit.toFixed(0)}% of limit)` : `${c.cpu_percent.toFixed(1)}%`} />
              )}
              {c.mem_usage > 0 && (
                <InfoItem
                  label="Memory"
                  value={`${formatBytes(c.mem_usage)} / ${c.mem_limit > 0 ? formatBytes(c.mem_limit) : '\u221E'}`}
                />
              )}
            </div>
          </Section>
        )
      })()}

      {/* Metric history charts */}
      {deviceId && metrics.length > 0 && (
        <Section title="Metric History">
          <div className="flex gap-1 mb-3">
            {[{ label: '1h', value: 1 }, { label: '6h', value: 6 }, { label: '24h', value: 24 }, { label: '7d', value: 168 }].map(tr => (
              <button
                key={tr.value}
                onClick={() => { setMetricHours(tr.value); localStorage.setItem('container-metric-hours', String(tr.value)) }}
                className={`px-2 py-0.5 text-[10px] rounded transition-colors ${
                  metricHours === tr.value
                    ? 'bg-gray-700 text-white'
                    : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
                }`}
              >
                {tr.label}
              </button>
            ))}
          </div>
          <ContainerMetricChart metrics={metrics} mode="cpu" label="CPU Usage" color="#3b82f6" />
          <div className="mt-3" />
          <ContainerMetricChart metrics={metrics} mode="memory" label="Memory Usage" color="#8b5cf6" />
        </Section>
      )}

      {/* Alert rules applicable to this container — matches the DeviceDetail
          "Alert Rules" table styling, wrapped for horizontal scroll on mobile
          so long rule names don't break the card layout. */}
      {deviceId && (
        <Section title="Alert Rules">
          {alertRules.length > 0 ? (
            <div className="overflow-x-auto scrollbar-thin -mx-5 px-5">
              <table className="w-full text-sm min-w-[500px]">
                <thead>
                  <tr className="text-gray-500 text-xs uppercase">
                    <th className="text-left py-2 w-6"></th>
                    <th className="text-left py-2 pr-3">Name</th>
                    <th className="text-left py-2 pr-3">Metric</th>
                    <th className="text-left py-2 pr-3">Condition</th>
                    <th className="text-left py-2 pr-3">Severity</th>
                    <th className="text-left py-2 pr-3">Scope</th>
                    <th className="text-left py-2 pr-3"></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-800/50">
                  {alertRules.map((rule: AlertRule) => {
                    const isGlobal = !rule.target_name && !rule.include_containers
                    return (
                      <tr key={rule.id}>
                        <td className="py-1.5 pr-3">
                          <span className={`w-2 h-2 rounded-full inline-block ${rule.enabled ? 'bg-emerald-400' : 'bg-gray-600'}`} />
                        </td>
                        <td className="py-1.5 pr-3 text-gray-200">{rule.name}</td>
                        <td className="py-1.5 pr-3 text-gray-400 font-mono text-xs whitespace-nowrap">{rule.metric}</td>
                        <td className="py-1.5 pr-3 text-gray-400 font-mono text-xs whitespace-nowrap">{ruleConditionLabel(rule)}</td>
                        <td className="py-1.5 pr-3">
                          <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                            rule.severity === 'critical' ? 'bg-red-500/20 text-red-400'
                              : rule.severity === 'info' ? 'bg-blue-500/20 text-blue-400'
                              : 'bg-amber-500/20 text-amber-400'
                          }`}>
                            {rule.severity}
                          </span>
                        </td>
                        <td className="py-1.5 pr-3 text-[10px]">
                          {isGlobal ? (
                            <span className="text-gray-500" title="Applies to all containers on this device">global</span>
                          ) : (
                            <span className="text-gray-400">scoped</span>
                          )}
                        </td>
                        <td className="py-1.5 pr-3">
                          <Link
                            to={`/alert-rules?edit=${rule.id}`}
                            className="text-xs text-blue-400 hover:text-blue-300"
                          >
                            Edit
                          </Link>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="text-sm text-gray-500 mb-3">No alert rules apply to this container.</p>
          )}
          <div className="mt-3 flex items-center gap-4">
            <AddContainerAlertButton containerName={c.name} deviceId={deviceId} />
            <Link to="/alert-rules" className="text-xs text-blue-400 hover:text-blue-300">
              Manage all rules
            </Link>
          </div>
        </Section>
      )}

      {/* Recent events — same list + pagination pattern as DeviceDetail. */}
      {deviceId && events.length > 0 && (
        <Section title={`Recent Events (${events.length})`}>
          <div className="space-y-2">
            {pagedEvents.map(evt => (
              <div key={evt.id} className="flex items-center gap-3 text-sm">
                <span className={`w-2 h-2 rounded-full flex-shrink-0 ${
                  evt.severity === 'critical' ? 'bg-red-400' : evt.severity === 'warning' ? 'bg-amber-400' : 'bg-blue-400'
                }`} />
                <span className="text-gray-400 font-mono text-xs">{new Date(evt.created_at).toLocaleString()}</span>
                <span className="text-gray-200">{evt.message}</span>
              </div>
            ))}
          </div>
          {totalEventPages > 1 && (
            <div className="flex items-center justify-between mt-3 pt-3 border-t border-gray-800">
              <span className="text-xs text-gray-500">
                {safeEventsPage * EVENTS_PER_PAGE + 1}–{Math.min((safeEventsPage + 1) * EVENTS_PER_PAGE, events.length)} of {events.length}
              </span>
              <div className="flex items-center gap-1">
                <button
                  onClick={() => setEventsPage(p => Math.max(0, p - 1))}
                  disabled={safeEventsPage === 0}
                  className="px-2 py-0.5 rounded text-xs text-gray-400 hover:text-white hover:bg-gray-800 disabled:opacity-30"
                >
                  Prev
                </button>
                <span className="text-xs text-gray-500 px-1">{safeEventsPage + 1} / {totalEventPages}</span>
                <button
                  onClick={() => setEventsPage(p => Math.min(totalEventPages - 1, p + 1))}
                  disabled={safeEventsPage >= totalEventPages - 1}
                  className="px-2 py-0.5 rounded text-xs text-gray-400 hover:text-white hover:bg-gray-800 disabled:opacity-30"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </Section>
      )}

      {c.repo_url && (
        <Section title="Source">
          <a href={c.repo_url} target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:underline text-sm break-all">
            {c.repo_url}
          </a>
        </Section>
      )}

      {c.riot?.description && (
        <Section title="Description">
          <p className="text-sm text-gray-300">{c.riot.description}</p>
        </Section>
      )}

      {c.env && c.env.length > 0 && (
        <Section title="Environment">
          <div className="space-y-1 max-h-64 overflow-y-auto scrollbar-thin">
            {c.env.map((e, i) => (
              <div key={i} className="flex gap-2 text-xs font-mono">
                <span className="text-gray-400 flex-shrink-0">{e.key}=</span>
                <span className={`break-all ${isSensitiveKey(e.key) ? 'text-amber-400/70' : 'text-gray-500'}`}>
                  {maskValue(e.key, e.value)}
                </span>
              </div>
            ))}
          </div>
        </Section>
      )}

      {c.labels && Object.keys(c.labels).length > 0 && (
        <Section title="Labels">
          <div className="space-y-1 max-h-64 overflow-y-auto scrollbar-thin">
            {Object.entries(c.labels).map(([k, v]) => (
              <div key={k} className="flex gap-2 text-xs font-mono">
                <span className="text-gray-400 flex-shrink-0">{k}=</span>
                <span className="text-gray-500 break-all">{v}</span>
              </div>
            ))}
          </div>
        </Section>
      )}
    </div>
  )
}

function NetworkTab({ container: c }: { container: ContainerInfo }) {
  const hasPorts = c.ports && c.ports.length > 0
  const hasNetworks = c.networks && c.networks.length > 0

  if (!hasPorts && !hasNetworks) {
    return (
      <Section title="Network">
        <p className="text-sm text-gray-500">No network information available.</p>
      </Section>
    )
  }

  return (
    <div className="space-y-6">
      {hasPorts && (
        <Section title="Ports">
          <div className="overflow-x-auto scrollbar-thin -mx-5 px-5">
            <table className="w-full text-sm min-w-[400px]">
              <thead>
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2 pr-3">Container</th>
                  <th className="text-left py-2 pr-3">Host</th>
                  <th className="text-left py-2 pr-3">Protocol</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {c.ports!.map((p, i) => (
                  <tr key={i}>
                    <td className="py-1.5 pr-3 font-mono text-gray-300">{p.container_port}</td>
                    <td className="py-1.5 pr-3 font-mono text-gray-400">{p.host_port ? `${p.host_ip || '0.0.0.0'}:${p.host_port}` : '-'}</td>
                    <td className="py-1.5 pr-3 text-gray-500">{p.protocol}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {hasNetworks && (
        <Section title="Networks">
          <div className="overflow-x-auto scrollbar-thin -mx-5 px-5">
            <table className="w-full text-sm min-w-[400px]">
              <thead>
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2 pr-3">Name</th>
                  <th className="text-left py-2 pr-3">IP Address</th>
                  <th className="text-left py-2 pr-3">Gateway</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {c.networks!.map((n, i) => (
                  <tr key={i}>
                    <td className="py-1.5 pr-3 font-mono text-gray-300 whitespace-nowrap">{n.name}</td>
                    <td className="py-1.5 pr-3 font-mono text-gray-400 whitespace-nowrap">{n.ip_address || '-'}</td>
                    <td className="py-1.5 pr-3 font-mono text-gray-500 whitespace-nowrap">{n.gateway || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}
    </div>
  )
}

function VolumesTab({ container: c }: { container: ContainerInfo }) {
  if (!c.mounts || c.mounts.length === 0) {
    return (
      <Section title="Volumes">
        <p className="text-sm text-gray-500">No volumes or mounts.</p>
      </Section>
    )
  }
  return (
    <Section title="Mounts">
      <div className="overflow-x-auto scrollbar-thin -mx-5 px-5">
        <table className="w-full text-sm min-w-[500px]">
          <thead>
            <tr className="text-gray-500 text-xs uppercase">
              <th className="text-left py-2 pr-3">Type</th>
              <th className="text-left py-2 pr-3">Source</th>
              <th className="text-left py-2 pr-3">Destination</th>
              <th className="text-left py-2 pr-3">Mode</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800/50">
            {c.mounts.map((m, i) => (
              <tr key={i}>
                <td className="py-1.5 pr-3 text-gray-400">{m.type}</td>
                <td className="py-1.5 pr-3 font-mono text-xs text-gray-400 break-all">{m.source}</td>
                <td className="py-1.5 pr-3 font-mono text-xs text-gray-300 break-all">{m.destination}</td>
                <td className="py-1.5 pr-3">{m.read_only ? <span className="text-amber-400">RO</span> : <span className="text-gray-500">RW</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  )
}

function ContainerLogsTab({ containerID, containerName, deviceId }: { containerID: string; containerName: string; deviceId: string }) {
  const [streamFilter, setStreamFilter] = useState<'' | 'stdout' | 'stderr'>('')
  const [limit, setLimit] = useState(200)

  const { data: logs = [], isLoading } = useQuery({
    queryKey: ['container-logs', deviceId, containerID, limit, streamFilter],
    queryFn: () => api.getContainerLogs(deviceId, containerID, limit, streamFilter || undefined),
    refetchInterval: 15_000,
  })

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <div className="flex gap-1">
          {(['', 'stdout', 'stderr'] as const).map(s => (
            <button
              key={s}
              onClick={() => setStreamFilter(s)}
              className={`px-2 py-0.5 text-[10px] rounded transition-colors ${
                streamFilter === s
                  ? 'bg-gray-700 text-white'
                  : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
              }`}
            >
              {s || 'all'}
            </button>
          ))}
        </div>
        <div className="flex gap-1 ml-auto">
          {[200, 500, 1000].map(n => (
            <button
              key={n}
              onClick={() => setLimit(n)}
              className={`px-2 py-0.5 text-[10px] rounded transition-colors ${
                limit === n
                  ? 'bg-gray-700 text-white'
                  : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
              }`}
            >
              {n}
            </button>
          ))}
        </div>
      </div>

      {isLoading && <p className="text-xs text-gray-500">Loading logs...</p>}
      {!isLoading && logs.length === 0 && (
        <p className="text-xs text-gray-500">No logs available. The <code className="text-gray-400">container_logs</code> collector must be enabled on the agent.</p>
      )}
      {logs.length > 0 && (
        <div className="max-h-96 overflow-auto scrollbar-thin bg-gray-950 rounded border border-gray-800 p-2 font-mono text-xs">
          {logs.map((entry: ContainerLogEntry, i: number) => (
            <div key={entry.id ?? i} className="flex gap-2 hover:bg-gray-900/50 py-px">
              <span className="text-gray-600 flex-shrink-0 select-none">
                {new Date(entry.timestamp).toLocaleTimeString()}
              </span>
              <span className={`flex-shrink-0 w-10 ${entry.stream === 'stderr' ? 'text-red-400' : 'text-blue-400'}`}>
                {entry.stream === 'stderr' ? 'ERR' : 'OUT'}
              </span>
              <span className="text-gray-300 whitespace-pre-wrap break-all">{entry.line}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

const ALERT_PRESETS = [
  { label: 'CPU > 80%', metric: 'container_cpu_percent', operator: '>', threshold: 80, severity: 'warning' },
  { label: 'Memory > 90%', metric: 'container_mem_percent', operator: '>', threshold: 90, severity: 'warning' },
  { label: 'CPU Limit > 90%', metric: 'container_cpu_limit_percent', operator: '>', threshold: 90, severity: 'warning' },
] as const

function AddContainerAlertButton({ containerName, deviceId }: { containerName: string; deviceId: string }) {
  const [open, setOpen] = useState(false)
  const qc = useQueryClient()
  const createMutation = useMutation({
    mutationFn: (rule: Partial<AlertRule>) => settingsApi.createAlertRule(rule),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['container-alert-rules', deviceId, containerName] })
      setOpen(false)
    },
  })

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 hover:bg-gray-800 rounded transition-colors"
      >
        + Add Alert
      </button>
      {open && (
        <div className="absolute left-0 mt-1 z-10 bg-gray-800 border border-gray-700 rounded-lg shadow-xl p-2 space-y-1 min-w-[200px]">
          {ALERT_PRESETS.map(preset => (
            <button
              key={preset.metric}
              onClick={() => createMutation.mutate({
                name: `${containerName} ${preset.label}`,
                enabled: true,
                metric: preset.metric,
                operator: preset.operator,
                threshold: preset.threshold,
                target_name: containerName,
                target_state: '',
                severity: preset.severity,
                include_devices: '',
                exclude_devices: '',
                include_containers: '',
                exclude_containers: '',
                cooldown_seconds: 900,
                notify: true,
              })}
              disabled={createMutation.isPending}
              className="w-full text-left px-3 py-1.5 text-xs text-gray-300 hover:bg-gray-700 rounded transition-colors disabled:opacity-50"
            >
              {preset.label}
            </button>
          ))}
          {createMutation.isError && (
            <p className="text-[10px] text-red-400 px-3">{(createMutation.error as Error).message}</p>
          )}
        </div>
      )}
    </div>
  )
}

function ActionBtn({ label, onClick, variant = 'default', pending }: { label: string; onClick: () => void; variant?: 'default' | 'danger'; pending: boolean }) {
  return (
    <button
      onClick={onClick}
      disabled={pending}
      className={`px-2 py-1 text-xs rounded transition-colors disabled:opacity-50 ${
        variant === 'danger'
          ? 'text-red-400 hover:bg-red-900/30 border border-red-800/50'
          : 'text-gray-400 hover:text-white border border-gray-700 hover:bg-gray-800'
      }`}
    >
      {pending ? '...' : label}
    </button>
  )
}

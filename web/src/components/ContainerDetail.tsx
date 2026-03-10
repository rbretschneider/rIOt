import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { settingsApi } from '../api/settings'
import type { ContainerInfo, AlertRule } from '../types/models'
import { displayName, formatBytes, formatContainerUptime, isSensitiveKey, maskValue, statusColor } from '../utils/docker'
import ContainerStatusBadge from './ContainerStatusBadge'
import ContainerMetricChart from './ContainerMetricChart'
import GaugeBar from './GaugeBar'
import ConfirmModal from './ConfirmModal'
import Terminal from './Terminal'

type Tab = 'general' | 'network' | 'volumes' | 'terminal'

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
        className="relative w-full max-w-2xl bg-gray-900 border-l border-gray-800 overflow-y-auto"
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
  const [metricHours, setMetricHours] = useState(24)
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
      const allEvents = await api.getEvents(50, 0)
      return allEvents.filter(e =>
        e.device_id === deviceId &&
        e.message.toLowerCase().includes(c.name.toLowerCase())
      )
    },
    enabled: !!deviceId,
  })

  const { data: alertRules = [] } = useQuery({
    queryKey: ['container-alert-rules', deviceId, c.name],
    queryFn: async () => {
      const rules = await api.getDeviceAlertRules(deviceId!)
      return rules.filter((r: AlertRule) =>
        (r.metric === 'container_cpu_percent' || r.metric === 'container_mem_percent' || r.metric === 'container_cpu_limit_percent') &&
        r.target_name.toLowerCase() === c.name.toLowerCase()
      )
    },
    enabled: !!deviceId,
  })

  return (
    <div className="space-y-5">
      <DetailSection title="Status">
        <div className="grid grid-cols-2 gap-3">
          <DetailItem label="State" value={c.state} valueClass={statusColor(c.state)} />
          <DetailItem label="Status" value={c.status} />
          {c.state === 'running' && <DetailItem label="Up" value={formatContainerUptime(c.created)} />}
          {c.restart_policy && <DetailItem label="Restart Policy" value={c.restart_policy} />}
          {(c.restart_count ?? 0) > 0 && <DetailItem label="Restart Count" value={String(c.restart_count)} />}
          {c.health_status && <DetailItem label="Health" value={c.health_status} />}
          {c.update_available === true && <DetailItem label="Image" value="Newer version available" valueClass="text-amber-400" />}
          {c.update_available === false && <DetailItem label="Image" value="Up to date" valueClass="text-emerald-400/70" />}
        </div>
      </DetailSection>

      {/* Live metrics */}
      {c.state === 'running' && (c.cpu_percent > 0 || c.mem_usage > 0) && (() => {
        const cpuLimitCores = c.cpu_limit ? c.cpu_limit / 1e9 : 0
        const cpuLimitPct = cpuLimitCores * 100
        const cpuOfLimit = cpuLimitPct > 0 ? (c.cpu_percent / cpuLimitPct) * 100 : 0
        return (
          <DetailSection title="Resources">
            <div className="grid grid-cols-2 gap-4">
              <GaugeBar label={cpuLimitCores > 0 ? `CPU (of ${cpuLimitCores.toFixed(1)} cores)` : 'CPU'} value={cpuLimitCores > 0 ? cpuOfLimit : c.cpu_percent} />
              <GaugeBar label="Memory" value={memPct} />
            </div>
            <div className="grid grid-cols-2 gap-3 mt-3">
              {c.cpu_percent > 0 && (
                <DetailItem label="CPU" value={cpuLimitCores > 0 ? `${c.cpu_percent.toFixed(1)}% (${cpuOfLimit.toFixed(0)}% of limit)` : `${c.cpu_percent.toFixed(1)}%`} />
              )}
              {c.mem_usage > 0 && (
                <DetailItem
                  label="Memory"
                  value={`${formatBytes(c.mem_usage)} / ${c.mem_limit > 0 ? formatBytes(c.mem_limit) : '\u221E'}`}
                />
              )}
            </div>
          </DetailSection>
        )
      })()}

      {/* Metric history charts */}
      {deviceId && metrics.length > 0 && (
        <DetailSection title="Metric History">
          <div className="flex gap-1 mb-3">
            {[{ label: '1h', value: 1 }, { label: '6h', value: 6 }, { label: '24h', value: 24 }, { label: '7d', value: 168 }].map(tr => (
              <button
                key={tr.value}
                onClick={() => setMetricHours(tr.value)}
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
        </DetailSection>
      )}

      {/* Alert rules for this container */}
      {deviceId && (
        <DetailSection title="Alert Rules">
          {alertRules.length > 0 && (
            <div className="space-y-1 mb-2">
              {alertRules.map((rule: AlertRule) => (
                <div key={rule.id} className="flex items-center gap-2 text-xs">
                  <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${rule.enabled ? 'bg-emerald-400' : 'bg-gray-600'}`} />
                  <span className="text-gray-300">{rule.name}</span>
                  <span className="text-gray-500 font-mono">{rule.operator} {rule.threshold}%</span>
                  <span className={`px-1.5 py-0.5 rounded text-[10px] ${
                    rule.severity === 'critical' ? 'bg-red-900/50 text-red-400' :
                    rule.severity === 'warning' ? 'bg-amber-900/50 text-amber-400' :
                    'bg-blue-900/50 text-blue-400'
                  }`}>
                    {rule.severity}
                  </span>
                </div>
              ))}
            </div>
          )}
          <AddContainerAlertButton containerName={c.name} deviceId={deviceId} />
        </DetailSection>
      )}

      {/* Recent events */}
      {deviceId && events.length > 0 && (
        <DetailSection title="Recent Events">
          <div className="space-y-1 max-h-48 overflow-y-auto">
            {events.slice(0, 20).map(evt => (
              <div key={evt.id} className="flex items-start gap-2 text-xs">
                <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 mt-1.5 ${
                  evt.severity === 'critical' ? 'bg-red-400' :
                  evt.severity === 'warning' ? 'bg-amber-400' :
                  'bg-blue-400'
                }`} />
                <span className="text-gray-400 flex-shrink-0">{new Date(evt.created_at).toLocaleString()}</span>
                <span className="text-gray-300">{evt.message}</span>
              </div>
            ))}
          </div>
        </DetailSection>
      )}

      {c.repo_url && (
        <DetailSection title="Source">
          <a href={c.repo_url} target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:underline text-sm break-all">
            {c.repo_url}
          </a>
        </DetailSection>
      )}

      {c.riot?.description && (
        <DetailSection title="Description">
          <p className="text-sm text-gray-300">{c.riot.description}</p>
        </DetailSection>
      )}

      {c.env && c.env.length > 0 && (
        <DetailSection title="Environment">
          <div className="space-y-1 max-h-48 overflow-y-auto">
            {c.env.map((e, i) => (
              <div key={i} className="flex gap-2 text-xs font-mono">
                <span className="text-gray-400 flex-shrink-0">{e.key}=</span>
                <span className={isSensitiveKey(e.key) ? 'text-amber-400/70' : 'text-gray-500'}>
                  {maskValue(e.key, e.value)}
                </span>
              </div>
            ))}
          </div>
        </DetailSection>
      )}

      {c.labels && Object.keys(c.labels).length > 0 && (
        <DetailSection title="Labels">
          <div className="space-y-1 max-h-48 overflow-y-auto">
            {Object.entries(c.labels).map(([k, v]) => (
              <div key={k} className="flex gap-2 text-xs font-mono">
                <span className="text-gray-400 flex-shrink-0">{k}=</span>
                <span className="text-gray-500 break-all">{v}</span>
              </div>
            ))}
          </div>
        </DetailSection>
      )}
    </div>
  )
}

function NetworkTab({ container: c }: { container: ContainerInfo }) {
  return (
    <div className="space-y-5">
      {c.ports && c.ports.length > 0 && (
        <DetailSection title="Ports">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs uppercase">
                <th className="text-left py-1">Container</th>
                <th className="text-left py-1">Host</th>
                <th className="text-left py-1">Protocol</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {c.ports.map((p, i) => (
                <tr key={i}>
                  <td className="py-1.5 font-mono text-gray-300">{p.container_port}</td>
                  <td className="py-1.5 font-mono text-gray-400">{p.host_port ? `${p.host_ip || '0.0.0.0'}:${p.host_port}` : '-'}</td>
                  <td className="py-1.5 text-gray-500">{p.protocol}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </DetailSection>
      )}

      {c.networks && c.networks.length > 0 && (
        <DetailSection title="Networks">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs uppercase">
                <th className="text-left py-1">Name</th>
                <th className="text-left py-1">IP Address</th>
                <th className="text-left py-1">Gateway</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {c.networks.map((n, i) => (
                <tr key={i}>
                  <td className="py-1.5 font-mono text-gray-300">{n.name}</td>
                  <td className="py-1.5 font-mono text-gray-400">{n.ip_address || '-'}</td>
                  <td className="py-1.5 font-mono text-gray-500">{n.gateway || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </DetailSection>
      )}

      {(!c.ports || c.ports.length === 0) && (!c.networks || c.networks.length === 0) && (
        <p className="text-sm text-gray-500">No network information available.</p>
      )}
    </div>
  )
}

function VolumesTab({ container: c }: { container: ContainerInfo }) {
  if (!c.mounts || c.mounts.length === 0) {
    return <p className="text-sm text-gray-500">No volumes or mounts.</p>
  }
  return (
    <DetailSection title="Mounts">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-gray-500 text-xs uppercase">
            <th className="text-left py-1">Type</th>
            <th className="text-left py-1">Source</th>
            <th className="text-left py-1">Destination</th>
            <th className="text-left py-1">Mode</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-800/50">
          {c.mounts.map((m, i) => (
            <tr key={i}>
              <td className="py-1.5 text-gray-400">{m.type}</td>
              <td className="py-1.5 font-mono text-xs text-gray-400 break-all">{m.source}</td>
              <td className="py-1.5 font-mono text-xs text-gray-300">{m.destination}</td>
              <td className="py-1.5">{m.read_only ? <span className="text-amber-400">RO</span> : <span className="text-gray-500">RW</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </DetailSection>
  )
}

function DetailSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">{title}</h3>
      {children}
    </div>
  )
}

function DetailItem({ label, value, valueClass }: { label: string; value: string; valueClass?: string }) {
  return (
    <div>
      <p className="text-xs text-gray-500">{label}</p>
      <p className={`text-sm ${valueClass || 'text-gray-200'}`}>{value}</p>
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
                device_filter: deviceId,
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

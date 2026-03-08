import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ContainerInfo } from '../types/models'
import { displayName, formatBytes, formatContainerUptime, isSensitiveKey, maskValue, statusColor } from '../utils/docker'
import ContainerStatusBadge from './ContainerStatusBadge'
import ConfirmModal from './ConfirmModal'
import Terminal from './Terminal'

interface Props {
  container: ContainerInfo
  onClose: () => void
  terminalEnabled?: boolean
  deviceId?: string
}

type Tab = 'general' | 'network' | 'volumes' | 'terminal'

export default function ContainerDetail({ container: c, onClose, terminalEnabled, deviceId }: Props) {
  const [tab, setTab] = useState<Tab>('general')
  const [confirmAction, setConfirmAction] = useState<string | null>(null)
  const name = displayName(c.riot, c.name)

  const commandMutation = useMutation({
    mutationFn: ({ action, params }: { action: string; params: Record<string, unknown> }) =>
      api.sendCommand(deviceId!, action, params),
  })

  const tabs: { key: Tab; label: string; show: boolean }[] = [
    { key: 'general', label: 'General', show: true },
    { key: 'network', label: 'Network', show: true },
    { key: 'volumes', label: 'Volumes', show: true },
    { key: 'terminal', label: 'Terminal', show: !!terminalEnabled },
  ]

  return (
    <div className="fixed inset-0 z-50 flex justify-end" onClick={onClose}>
      <div className="absolute inset-0 bg-black/50" />
      <div
        className="relative w-full max-w-2xl bg-gray-900 border-l border-gray-800 overflow-y-auto"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="sticky top-0 bg-gray-900 border-b border-gray-800 p-5 z-10">
          <div className="flex items-start justify-between">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                {c.riot?.icon && <span className="text-xl">{c.riot.icon}</span>}
                <h2 className="text-lg font-bold text-white truncate">{name}</h2>
              </div>
              <p className="text-xs text-gray-500 font-mono mt-1">{c.image}</p>
              <p className="text-xs text-gray-600 font-mono">{c.short_id}</p>
            </div>
            <div className="flex items-center gap-2 flex-shrink-0">
              <ContainerStatusBadge state={c.state} />
              {deviceId && (
                <div className="flex gap-1 ml-2">
                  {c.update_available && (
                    <ActionBtn label="Update" onClick={() => setConfirmAction('docker_update')} pending={commandMutation.isPending} />
                  )}
                  {c.state === 'running' && (
                    <ActionBtn
                      label={commandMutation.isSuccess && commandMutation.variables?.action === 'docker_check_updates' ? 'Queued' : 'Check'}
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
              <button onClick={onClose} className="text-gray-500 hover:text-white text-xl ml-1">&times;</button>
            </div>
          </div>

          {/* Tabs */}
          <div className="flex gap-1 mt-4">
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
        </div>

        {/* Command feedback */}
        {commandMutation.isSuccess && (
          <div className="mx-5 mt-3 px-3 py-2 bg-emerald-900/30 border border-emerald-800 rounded text-sm text-emerald-400">
            Command sent ({commandMutation.data?.status})
          </div>
        )}
        {commandMutation.isError && (
          <div className="mx-5 mt-3 px-3 py-2 bg-red-900/30 border border-red-800 rounded text-sm text-red-400">
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

        {/* Content */}
        <div className="p-5 space-y-5">
          {tab === 'general' && <GeneralTab container={c} />}
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
    </div>
  )
}

function GeneralTab({ container: c }: { container: ContainerInfo }) {
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

      {c.state === 'running' && (c.cpu_percent > 0 || c.mem_usage > 0) && (
        <DetailSection title="Resources">
          <div className="grid grid-cols-2 gap-3">
            {c.cpu_percent > 0 && <DetailItem label="CPU" value={`${c.cpu_percent.toFixed(1)}%`} />}
            {c.mem_usage > 0 && (
              <DetailItem
                label="Memory"
                value={`${formatBytes(c.mem_usage)} / ${c.mem_limit > 0 ? formatBytes(c.mem_limit) : '∞'}`}
              />
            )}
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

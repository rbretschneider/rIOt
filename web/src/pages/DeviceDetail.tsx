import { useState, useMemo } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import StatusBadge from '../components/StatusBadge'
import GaugeBar from '../components/GaugeBar'
import ConfirmModal from '../components/ConfirmModal'
import CreateAlertDialog from '../components/CreateAlertDialog'

export default function DeviceDetail() {
  const { id } = useParams<{ id: string }>()
  // useDevices sets up the WS listener that pushes heartbeat/telemetry/events
  // directly into the ['device', id] and ['events'] query caches
  const { wsConnected } = useDevices()
  const { data, isLoading } = useQuery({
    queryKey: ['device', id],
    queryFn: () => api.getDevice(id!),
    refetchInterval: wsConnected ? false : 15_000, // Only poll when WS is down
    enabled: !!id,
  })
  const { data: events } = useQuery({
    queryKey: ['events'],
    queryFn: () => api.getEvents(50, 0),
    refetchInterval: wsConnected ? false : 30_000,
  })
  const { data: serverUpdate } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    staleTime: 60 * 60 * 1000,
  })

  const [confirmAction, setConfirmAction] = useState<string | null>(null)
  const [alertDialog, setAlertDialog] = useState<{ metric: string; targetName: string; targetState?: string } | null>(null)
  const commandMutation = useMutation({
    mutationFn: ({ action, params }: { action: string; params?: Record<string, unknown> }) =>
      api.sendCommand(id!, action, params || {}),
  })

  const latestVersion = serverUpdate?.latest_version
  const agentOutdated = useMemo(() => {
    const v = data?.device.agent_version
    if (!v || !latestVersion || v === 'dev') return false
    return v !== latestVersion
  }, [data?.device.agent_version, latestVersion])

  if (isLoading) return <div className="text-gray-500">Loading...</div>
  if (!data) return <div className="text-gray-500">Device not found</div>

  const { device, latest_telemetry, agent_connected } = data
  const tel = latest_telemetry?.data
  const canCommand = device.status === 'online' && agent_connected !== false

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-2xl font-bold text-white truncate">{device.hostname}</h1>
          <p className="text-sm text-gray-500 font-mono">
            {device.short_id} &middot; {device.arch}
            {device.agent_version && (
              <>
                {' '}&middot;{' '}
                <span className={agentOutdated ? 'text-amber-400' : undefined}>
                  v{device.agent_version}
                </span>
                {agentOutdated && (
                  <span className="ml-1.5 italic text-gray-500" title={`Latest: ${latestVersion}`}>(outdated)</span>
                )}
              </>
            )}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex gap-2">
            {tel?.docker?.available && tel.docker.total_containers > 0 && (
              <Link
                to={`/devices/${id}/containers`}
                className="px-3 py-1.5 text-xs text-blue-400 hover:text-blue-300 border border-blue-800/50 rounded-md transition-colors"
              >
                Docker ({tel.docker.total_containers})
              </Link>
            )}
            {device.status === 'online' && (
              <>
                {canCommand ? (
                  <Link
                    to={`/devices/${id}/terminal`}
                    className="px-3 py-1.5 text-xs text-gray-400 hover:text-white border border-gray-700 rounded-md transition-colors cursor-pointer"
                  >
                    Terminal
                  </Link>
                ) : (
                  <span className="px-3 py-1.5 text-xs text-gray-600 border border-gray-700/50 rounded-md opacity-50 cursor-not-allowed">
                    Terminal
                  </span>
                )}
                <button
                  onClick={() => setConfirmAction('agent_update')}
                  disabled={!agentOutdated || commandMutation.isPending}
                  className={`px-3 py-1.5 text-xs rounded-md transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                    agentOutdated
                      ? 'text-amber-400 hover:text-amber-300 border border-amber-600/50 hover:border-amber-500/50'
                      : 'text-gray-600 border border-gray-700/50'
                  }`}
                >
                  Update Agent
                </button>
                <button
                  onClick={() => setConfirmAction('reboot')}
                  disabled={commandMutation.isPending}
                  className="px-3 py-1.5 text-xs text-red-400 hover:text-red-300 border border-red-800/50 rounded-md transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Reboot
                </button>
              </>
            )}
          </div>
          <StatusBadge status={device.status} />
        </div>
      </div>

      {/* Agent not connected warning */}
      {device.status === 'online' && agent_connected === false && (
        <div className="px-4 py-2 bg-amber-900/30 border border-amber-800 rounded text-sm text-amber-400">
          Agent WebSocket not connected — terminal is unavailable. Commands will be queued and delivered on next heartbeat.
        </div>
      )}

      {/* Command feedback */}
      {commandMutation.isSuccess && (
        <div className="px-4 py-2 bg-emerald-900/30 border border-emerald-800 rounded text-sm text-emerald-400">
          Command sent: {commandMutation.data?.action} ({commandMutation.data?.status})
        </div>
      )}
      {commandMutation.isError && (
        <div className="px-4 py-2 bg-red-900/30 border border-red-800 rounded text-sm text-red-400">
          {(commandMutation.error as Error).message}
        </div>
      )}

      {/* Confirm modal */}
      {confirmAction && (
        <ConfirmModal
          title={confirmAction === 'reboot' ? 'Reboot Device' : 'Update Agent'}
          message={
            confirmAction === 'reboot'
              ? `Are you sure you want to reboot "${device.hostname}"? This will temporarily take the device offline.`
              : `Trigger an agent update check on "${device.hostname}"?`
          }
          confirmLabel={confirmAction === 'reboot' ? 'Reboot' : 'Update'}
          confirmVariant={confirmAction === 'reboot' ? 'danger' : 'primary'}
          onConfirm={() => {
            commandMutation.mutate({ action: confirmAction })
            setConfirmAction(null)
          }}
          onCancel={() => setConfirmAction(null)}
        />
      )}

      {/* Hardware Profile */}
      {device.hardware_profile && (
        <Section title="Hardware">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <InfoItem label="CPU" value={device.hardware_profile.cpu_model} />
            <InfoItem label="Cores / Threads" value={`${device.hardware_profile.cpu_cores} / ${device.hardware_profile.cpu_threads}`} />
            <InfoItem label="RAM" value={`${device.hardware_profile.total_ram_mb} MB`} />
            <InfoItem label="Board" value={device.hardware_profile.board_model || '-'} />
            {device.hardware_profile.virtualization && (
              <InfoItem label="Virtualization" value={device.hardware_profile.virtualization} />
            )}
          </div>
        </Section>
      )}

      {/* Live Metrics */}
      {tel && (
        <Section title="Live Metrics">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {tel.cpu && <GaugeBar label="CPU Usage" value={tel.cpu.usage_percent} />}
            {tel.memory && <GaugeBar label="RAM Usage" value={tel.memory.usage_percent} />}
            {tel.disks?.filesystems?.[0] && (
              <GaugeBar label={`Disk (${tel.disks.filesystems[0].mount_point})`} value={tel.disks.filesystems[0].usage_percent} />
            )}
          </div>
          {tel.cpu && (
            <div className="mt-4 grid grid-cols-3 gap-4">
              <InfoItem label="Load (1m/5m/15m)" value={`${tel.cpu.load_avg_1m.toFixed(2)} / ${tel.cpu.load_avg_5m.toFixed(2)} / ${tel.cpu.load_avg_15m.toFixed(2)}`} />
              {tel.cpu.temperature != null && <InfoItem label="CPU Temp" value={`${tel.cpu.temperature.toFixed(1)} C`} />}
              {tel.os && <InfoItem label="Uptime" value={formatUptime(tel.os.uptime)} />}
            </div>
          )}
        </Section>
      )}

      {/* OS Info */}
      {tel?.os && (
        <Section title="Operating System">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <InfoItem label="OS" value={tel.os.name} />
            <InfoItem label="Kernel" value={tel.os.kernel} />
            <InfoItem label="Arch" value={tel.os.kernel_arch} />
            <InfoItem label="Timezone" value={tel.os.timezone || '-'} />
          </div>
        </Section>
      )}

      {/* Network */}
      {tel?.network?.interfaces && tel.network.interfaces.length > 0 && (
        <NetworkSection
          interfaces={tel.network.interfaces}
          onCreateAlert={(name) => setAlertDialog({ metric: 'nic_state', targetName: name, targetState: 'DOWN' })}
        />
      )}

      {/* Filesystems */}
      {tel?.disks?.filesystems && tel.disks.filesystems.length > 0 && (
        <Section title="Filesystems">
          <div className="max-h-64 overflow-auto">
            <table className="w-full text-sm min-w-[480px]">
              <thead>
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2">Mount</th>
                  <th className="text-left py-2">Device</th>
                  <th className="text-left py-2">Type</th>
                  <th className="text-right py-2">Used</th>
                  <th className="text-right py-2">Total</th>
                  <th className="text-right py-2">Usage</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {tel.disks.filesystems.map((fs) => (
                  <tr key={fs.mount_point}>
                    <td className="py-2 font-mono">{fs.mount_point}</td>
                    <td className="py-2 font-mono text-gray-400">{fs.device}</td>
                    <td className="py-2 text-gray-400">{fs.fs_type}</td>
                    <td className="py-2 text-right font-mono">{fs.used_gb.toFixed(1)} GB</td>
                    <td className="py-2 text-right font-mono text-gray-400">{fs.total_gb.toFixed(1)} GB</td>
                    <td className="py-2 text-right">
                      <span className={fs.usage_percent > 90 ? 'text-red-400' : fs.usage_percent > 75 ? 'text-amber-400' : 'text-emerald-400'}>
                        {fs.usage_percent.toFixed(1)}%
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {/* Services */}
      {tel?.services && tel.services.length > 0 && (
        <Section title="Services">
          <div className="max-h-64 overflow-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2">Name</th>
                  <th className="text-left py-2">State</th>
                  <th className="text-left py-2">Enabled</th>
                  <th className="py-2 w-8"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {tel.services.map((svc) => (
                  <tr key={svc.name}>
                    <td className="py-1.5 font-mono text-xs">{svc.name}</td>
                    <td className="py-1.5">
                      <span className={svc.state.includes('running') ? 'text-emerald-400' : svc.state.includes('failed') ? 'text-red-400' : 'text-gray-400'}>
                        {svc.state}
                      </span>
                    </td>
                    <td className="py-1.5 text-gray-400">{svc.enabled ? 'Yes' : 'No'}</td>
                    <td className="py-1.5">
                      <button
                        onClick={() => setAlertDialog({ metric: 'service_state', targetName: svc.name, targetState: 'stopped' })}
                        className="text-gray-600 hover:text-amber-400 transition-colors"
                        title="Create alert for this service"
                      >
                        <AlertIcon />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {/* Top Processes */}
      {tel?.processes?.top_by_cpu && tel.processes.top_by_cpu.length > 0 && (
        <Section title="Top Processes (by CPU)">
          <div className="overflow-x-auto">
          <table className="w-full text-sm min-w-[480px]">
            <thead>
              <tr className="text-gray-500 text-xs uppercase">
                <th className="text-left py-2">PID</th>
                <th className="text-left py-2">Name</th>
                <th className="text-right py-2">CPU %</th>
                <th className="text-right py-2">MEM %</th>
                <th className="text-left py-2">User</th>
                <th className="py-2 w-8"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {tel.processes.top_by_cpu.slice(0, 10).map((p) => (
                <tr key={p.pid}>
                  <td className="py-1.5 font-mono text-gray-500">{p.pid}</td>
                  <td className="py-1.5 font-mono">{p.name}</td>
                  <td className="py-1.5 text-right font-mono">{p.cpu_percent.toFixed(1)}</td>
                  <td className="py-1.5 text-right font-mono">{p.mem_percent.toFixed(1)}</td>
                  <td className="py-1.5 text-gray-400">{p.user}</td>
                  <td className="py-1.5">
                    <button
                      onClick={() => setAlertDialog({ metric: 'process_missing', targetName: p.name, targetState: 'absent' })}
                      className="text-gray-600 hover:text-amber-400 transition-colors"
                      title="Create alert for this process"
                    >
                      <AlertIcon />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
        </Section>
      )}

      {/* Updates */}
      {tel?.updates && tel.updates.pending_updates > 0 && (
        <Section title={`Pending Updates (${tel.updates.pending_updates})`}>
          <div className="max-h-48 overflow-y-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2">Package</th>
                  <th className="text-left py-2">New Version</th>
                  <th className="text-left py-2">Security</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {tel.updates.updates?.map((u) => (
                  <tr key={u.name}>
                    <td className="py-1.5 font-mono">{u.name}</td>
                    <td className="py-1.5 text-gray-400">{u.new_ver}</td>
                    <td className="py-1.5">{u.is_security ? <span className="text-red-400">Yes</span> : '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {/* Security */}
      {tel?.security && (
        <Section title="Security">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <InfoItem label="Firewall" value={tel.security.firewall_status || '-'} />
            <InfoItem label="SELinux" value={tel.security.selinux || '-'} />
            <InfoItem label="AppArmor" value={tel.security.apparmor || '-'} />
            <InfoItem label="Failed Logins (24h)" value={String(tel.security.failed_logins_24h)} />
            <InfoItem label="Logged-in Users" value={String(tel.security.logged_in_users)} />
            {tel.security.open_ports && tel.security.open_ports.length > 0 && (
              <div className="col-span-2">
                <p className="text-xs text-gray-500">Open Ports</p>
                <p className="text-sm text-gray-200 font-mono">{tel.security.open_ports.join(', ')}</p>
              </div>
            )}
          </div>
        </Section>
      )}

      {/* Recent Events */}
      {events && events.filter(e => e.device_id === id).length > 0 && (
        <Section title="Recent Events">
          <div className="space-y-2">
            {events.filter(e => e.device_id === id).slice(0, 10).map(e => (
              <div key={e.id} className="flex items-center gap-3 text-sm">
                <span className={`w-2 h-2 rounded-full flex-shrink-0 ${
                  e.severity === 'critical' ? 'bg-red-400' : e.severity === 'warning' ? 'bg-amber-400' : 'bg-blue-400'
                }`} />
                <span className="text-gray-400 font-mono text-xs">{new Date(e.created_at).toLocaleString()}</span>
                <span className="text-gray-200">{e.message}</span>
              </div>
            ))}
          </div>
        </Section>
      )}

      {/* Create Alert Dialog */}
      {alertDialog && (
        <CreateAlertDialog
          metric={alertDialog.metric}
          targetName={alertDialog.targetName}
          targetState={alertDialog.targetState}
          deviceFilter={device.id}
          onClose={() => setAlertDialog(null)}
        />
      )}
    </div>
  )
}

const VIRTUAL_PREFIXES = ['veth', 'br-', 'docker', 'cni', 'flannel', 'cali', 'tunl', 'vxlan']

function isVirtualInterface(name: string) {
  return VIRTUAL_PREFIXES.some(p => name.startsWith(p))
}

function AlertIcon() {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
      <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-11a1 1 0 10-2 0v2H7a1 1 0 100 2h2v2a1 1 0 102 0v-2h2a1 1 0 100-2h-2V7z" clipRule="evenodd" />
    </svg>
  )
}

function NicTable({ interfaces, onCreateAlert }: { interfaces: import('../types/models').NetworkInterface[]; onCreateAlert?: (name: string) => void }) {
  return (
    <div className="overflow-x-auto">
    <table className="w-full text-sm min-w-[480px]">
      <thead>
        <tr className="text-gray-500 text-xs uppercase">
          <th className="text-left py-2">Name</th>
          <th className="text-left py-2">State</th>
          <th className="text-left py-2">IPv4</th>
          <th className="text-left py-2">MAC</th>
          <th className="text-right py-2">TX / RX</th>
          {onCreateAlert && <th className="py-2 w-8"></th>}
        </tr>
      </thead>
      <tbody className="divide-y divide-gray-800/50">
        {interfaces.map((iface) => (
          <tr key={iface.name}>
            <td className="py-2 font-mono">{iface.name}</td>
            <td className="py-2">
              <span className={iface.state === 'UP' ? 'text-emerald-400' : 'text-gray-500'}>{iface.state}</span>
            </td>
            <td className="py-2 font-mono text-gray-400">{iface.ipv4?.join(', ') || '-'}</td>
            <td className="py-2 font-mono text-gray-500">{iface.mac || '-'}</td>
            <td className="py-2 text-right font-mono text-gray-400">
              {formatBytes(iface.bytes_sent)} / {formatBytes(iface.bytes_recv)}
            </td>
            {onCreateAlert && (
              <td className="py-2">
                <button
                  onClick={() => onCreateAlert(iface.name)}
                  className="text-gray-600 hover:text-amber-400 transition-colors"
                  title="Create alert for this interface"
                >
                  <AlertIcon />
                </button>
              </td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
    </div>
  )
}

function NetworkSection({ interfaces, onCreateAlert }: { interfaces: import('../types/models').NetworkInterface[]; onCreateAlert?: (name: string) => void }) {
  const physical = interfaces.filter(i => !isVirtualInterface(i.name))
  const virtual = interfaces.filter(i => isVirtualInterface(i.name))
  const [showVirtual, setShowVirtual] = useState(false)

  return (
    <Section title="Network Interfaces">
      <NicTable interfaces={physical} onCreateAlert={onCreateAlert} />
      {virtual.length > 0 && (
        <div className="mt-3">
          <button
            onClick={() => setShowVirtual(!showVirtual)}
            className="text-xs text-gray-500 hover:text-gray-300 transition-colors"
          >
            {showVirtual ? 'Hide' : 'Show'} {virtual.length} virtual interface{virtual.length !== 1 ? 's' : ''}
            {showVirtual ? ' \u25B2' : ' \u25BC'}
          </button>
          {showVirtual && (
            <div className="mt-2 max-h-48 overflow-y-auto">
              <NicTable interfaces={virtual} />
            </div>
          )}
        </div>
      )}
    </Section>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-5">
      <h2 className="text-sm font-semibold text-gray-300 uppercase tracking-wider mb-4">{title}</h2>
      {children}
    </div>
  )
}

function InfoItem({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs text-gray-500">{label}</p>
      <p className="text-sm text-gray-200">{value}</p>
    </div>
  )
}

function formatUptime(seconds: number) {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  if (d > 0) return `${d}d ${h}h`
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
}

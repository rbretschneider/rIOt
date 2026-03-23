import { useState, useMemo } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import { isVersionOlder } from '../utils/version'
import StatusBadge from '../components/StatusBadge'
import GaugeBar from '../components/GaugeBar'
import BatteryGauge from '../components/BatteryGauge'
import MetricChart from '../components/MetricChart'
import ConfirmModal from '../components/ConfirmModal'
import CreateAlertDialog from '../components/CreateAlertDialog'
import SecurityScoreGauge from '../components/SecurityScoreGauge'
import SecurityScoreModal from '../components/SecurityScoreModal'
import ActivityLog from '../components/ActivityLog'
import { useFeatures } from '../hooks/useFeatures'
import cronstrue from 'cronstrue'
import { formatRunDate, parseSystemdCalendar } from '../utils/cron'
import Tooltip from '../components/Tooltip'

export default function DeviceDetail() {
  const { id } = useParams<{ id: string }>()
  // useDevices sets up the WS listener that pushes heartbeat/telemetry/events
  // directly into the ['device', id] and ['events'] query caches
  const { wsConnected } = useDevices()
  const { isEnabled } = useFeatures()
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

  const { data: alertRules } = useQuery({
    queryKey: ['device-alert-rules', id],
    queryFn: () => api.getDeviceAlertRules(id!),
    enabled: !!id,
  })

  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [confirmAction, setConfirmAction] = useState<string | null>(null)
  const [alertDialog, setAlertDialog] = useState<{ metric: string; targetName: string; targetState?: string } | null>(null)
  const [tagInput, setTagInput] = useState('')
  const [locationEdit, setLocationEdit] = useState<string | null>(null)
  const [metricHours, setMetricHours] = useState(24)
  const [showSecurityModal, setShowSecurityModal] = useState(false)
  const [logPriority, setLogPriority] = useState(7)
  const [eventsPage, setEventsPage] = useState(0)
  const { data: deviceLogs } = useQuery({
    queryKey: ['device-logs', id, logPriority],
    queryFn: () => api.getDeviceLogs(id!, logPriority, 500, logPriority !== 7),
    enabled: !!id,
    refetchInterval: 60_000,
  })
  const { data: heartbeats } = useQuery({
    queryKey: ['heartbeat-history', id, metricHours],
    queryFn: () => api.getHeartbeatHistory(id!, metricHours),
    enabled: !!id,
    refetchInterval: 60_000,
  })
  const { data: securityScore } = useQuery({
    queryKey: ['security-score', id],
    queryFn: () => api.getSecurityScore(id!),
    enabled: !!id,
    staleTime: 5 * 60_000,
  })
  const tagsMutation = useMutation({
    mutationFn: (tags: string[]) => api.updateDeviceTags(id!, tags),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['device', id] })
      queryClient.invalidateQueries({ queryKey: ['devices'] })
    },
  })
  const locationMutation = useMutation({
    mutationFn: (location: string) => api.updateDeviceLocation(id!, location),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['device', id] })
      queryClient.invalidateQueries({ queryKey: ['devices'] })
      setLocationEdit(null)
    },
  })
  const commandMutation = useMutation({
    mutationFn: ({ action, params }: { action: string; params?: Record<string, unknown> }) =>
      api.sendCommand(id!, action, params || {}),
    onSuccess: () => { setTimeout(() => commandMutation.reset(), 5000) },
    onError: () => { setTimeout(() => commandMutation.reset(), 5000) },
  })
  const fetchLogsMutation = useMutation({
    mutationFn: (params: { hours: number; priority: number }) =>
      api.sendCommand(id!, 'fetch_logs', params),
    onSuccess: () => {
      // Refetch logs after a short delay to let the agent push them
      setTimeout(() => queryClient.invalidateQueries({ queryKey: ['device-logs', id] }), 3000)
      setTimeout(() => fetchLogsMutation.reset(), 5000)
    },
    onError: () => { setTimeout(() => fetchLogsMutation.reset(), 5000) },
  })
  const autoPatchMutation = useMutation({
    mutationFn: (enabled: boolean) => api.setAutoPatch(id!, enabled),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['device', id] })
      queryClient.invalidateQueries({ queryKey: ['security-score', id] })
    },
  })
  const deleteMutation = useMutation({
    mutationFn: () => api.deleteDevice(id!, data?.device.status === 'online'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['devices'] })
      navigate('/')
    },
    onError: (err) => {
      console.error('Delete device failed:', err)
      alert(`Failed to delete device: ${err instanceof Error ? err.message : 'Unknown error'}`)
    },
  })

  const latestVersion = serverUpdate?.latest_version
  const agentOutdated = useMemo(() => {
    const v = data?.device.agent_version
    if (!v || !latestVersion || v === 'dev') return false
    return isVersionOlder(v, latestVersion)
  }, [data?.device.agent_version, latestVersion])

  if (isLoading) return <div className="text-gray-500">Loading...</div>
  if (!data) return <div className="text-gray-500">Device not found</div>

  const { device, latest_telemetry, agent_connected } = data
  const tel = latest_telemetry?.data
  const canCommand = device.status === 'online' && agent_connected !== false

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-start justify-between gap-3">
        {/* Left: name, subtitle, navigation links */}
        <div className="min-w-0">
          <h1 className="text-2xl font-bold text-white truncate uppercase">{device.hostname}</h1>
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
          <div className="flex items-center gap-4 mt-2">
            {isEnabled('docker') && tel?.docker?.available && (
              <Link
                to={`/devices/${id}/containers`}
                className="text-sm text-gray-400 hover:text-white transition-colors"
              >
                Docker
              </Link>
            )}
            <Link
              to={`/devices/${id}/probes`}
              className="text-sm text-gray-400 hover:text-white transition-colors"
            >
              Probes
            </Link>
            {isEnabled('device_terminal') && device.status === 'online' && (
              canCommand ? (
                <Link
                  to={`/devices/${id}/terminal`}
                  className="text-sm text-gray-400 hover:text-white transition-colors"
                >
                  Terminal
                </Link>
              ) : (
                <span className="text-sm text-gray-600 cursor-not-allowed">
                  Terminal
                </span>
              )
            )}
            <button
              onClick={() => setConfirmAction('delete')}
              className="text-sm text-gray-600 hover:text-red-400 transition-colors"
            >
              Delete
            </button>
          </div>
        </div>
        {/* Right: security gauge + status badge + action buttons */}
        <div className="flex items-center gap-3 flex-shrink-0">
          {isEnabled('security_score') && securityScore && (
            <SecurityScoreGauge score={securityScore} onClick={() => setShowSecurityModal(true)} />
          )}
          <StatusBadge status={device.status} />
          {device.status === 'online' && (
            <div className="flex gap-2">
              <button
                onClick={() => setConfirmAction('agent_update')}
                disabled={!agentOutdated || commandMutation.isPending || (commandMutation.isSuccess && commandMutation.variables?.action === 'agent_update')}
                className={`px-3 py-1.5 text-xs rounded-md transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
                  agentOutdated
                    ? 'text-amber-400 hover:text-amber-300 border border-amber-600/50 hover:border-amber-500/50'
                    : 'text-gray-600 border border-gray-700/50'
                }`}
              >
                {commandMutation.isSuccess && commandMutation.variables?.action === 'agent_update' ? 'Update Sent' : 'Update Agent'}
              </button>
              {tel?.updates && tel.updates.pending_updates > 0 && (
                <button
                  onClick={() => setConfirmAction('os_update')}
                  disabled={commandMutation.isPending}
                  className="px-3 py-1.5 text-xs text-cyan-400 hover:text-cyan-300 border border-cyan-800/50 rounded-md transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Patch ({tel.updates.pending_updates})
                </button>
              )}
              <button
                onClick={() => setConfirmAction('reboot')}
                disabled={commandMutation.isPending}
                className="px-3 py-1.5 text-xs text-red-400 hover:text-red-300 border border-red-800/50 rounded-md transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
              >
                Reboot
              </button>
              <label className="flex items-center gap-1.5 ml-2 cursor-pointer" title="Automatically apply OS patches when updates are detected">
                <span className="text-xs text-gray-500">Auto-patch</span>
                <button
                  role="switch"
                  aria-checked={device.auto_patch}
                  onClick={() => autoPatchMutation.mutate(!device.auto_patch)}
                  disabled={autoPatchMutation.isPending}
                  className={`relative inline-flex h-4 w-7 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 ${
                    device.auto_patch ? 'bg-cyan-600' : 'bg-gray-700'
                  }`}
                >
                  <span className={`inline-block h-3 w-3 rounded-full bg-white transition-transform ${
                    device.auto_patch ? 'translate-x-3.5' : 'translate-x-0.5'
                  }`} />
                </button>
              </label>
            </div>
          )}
        </div>
      </div>

      {/* Location & Tags */}
      <div className="flex items-center gap-4 flex-wrap">
        <div className="flex items-center gap-1.5">
          <span className="text-xs text-gray-500">Location:</span>
          {locationEdit !== null ? (
            <input
              type="text"
              autoFocus
              value={locationEdit}
              onChange={e => setLocationEdit(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter') {
                  locationMutation.mutate(locationEdit)
                } else if (e.key === 'Escape') {
                  setLocationEdit(null)
                }
              }}
              onBlur={() => {
                if (locationEdit !== (device.location || '')) {
                  locationMutation.mutate(locationEdit)
                } else {
                  setLocationEdit(null)
                }
              }}
              className="px-2 py-0.5 bg-transparent border border-gray-600 rounded text-xs text-gray-300 focus:outline-none focus:border-gray-400 w-40"
            />
          ) : (
            <button
              onClick={() => setLocationEdit(device.location || '')}
              className="text-xs text-gray-300 hover:text-white transition-colors cursor-pointer"
              title="Click to edit location"
            >
              {device.location || <span className="text-gray-600 italic">not set</span>}
            </button>
          )}
        </div>
        <span className="text-gray-700">|</span>
      <div className="flex items-center gap-2 flex-wrap">
        {(device.tags ?? []).map(tag => (
          <span key={tag} className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-gray-800 text-xs text-gray-300">
            {tag}
            <button
              onClick={() => tagsMutation.mutate((device.tags ?? []).filter(t => t !== tag))}
              className="text-gray-500 hover:text-red-400 transition-colors"
            >
              &times;
            </button>
          </span>
        ))}
        <input
          type="text"
          value={tagInput}
          onChange={e => setTagInput(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter' && tagInput.trim()) {
              const newTag = tagInput.trim()
              if (!(device.tags ?? []).includes(newTag)) {
                tagsMutation.mutate([...(device.tags ?? []), newTag])
              }
              setTagInput('')
            }
          }}
          placeholder="Add tag..."
          className="px-2 py-0.5 bg-transparent border border-gray-700 rounded text-xs text-gray-300 placeholder-gray-600 focus:outline-none focus:border-gray-500 w-24"
        />
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

      {/* Security score modal */}
      {isEnabled('security_score') && showSecurityModal && securityScore && (
        <SecurityScoreModal
          score={securityScore}
          hostname={device.hostname}
          onClose={() => setShowSecurityModal(false)}
          canCommand={canCommand}
          onRunCommand={(action, params) => commandMutation.mutate({ action, params })}
        />
      )}

      {/* Confirm modal */}
      {confirmAction && confirmAction !== 'delete' && (
        <ConfirmModal
          title={
            confirmAction === 'reboot' ? 'Reboot Device'
              : confirmAction === 'os_update' ? 'Patch Device'
              : 'Update Agent'
          }
          message={
            confirmAction === 'reboot'
              ? `Are you sure you want to reboot "${device.hostname}"? This will temporarily take the device offline.`
              : confirmAction === 'os_update'
              ? `Run OS package updates on "${device.hostname}"? This will upgrade all pending packages.`
              : `Trigger an agent update check on "${device.hostname}"?`
          }
          confirmLabel={confirmAction === 'reboot' ? 'Reboot' : confirmAction === 'os_update' ? 'Patch' : 'Update'}
          confirmVariant={confirmAction === 'reboot' ? 'danger' : 'primary'}
          onConfirm={() => {
            const params = confirmAction === 'os_update' ? { mode: 'full' } : {}
            commandMutation.mutate({ action: confirmAction, params })
            setConfirmAction(null)
          }}
          onCancel={() => setConfirmAction(null)}
        />
      )}
      {confirmAction === 'delete' && (
        <ConfirmModal
          title="Delete Device"
          message={device.status === 'online'
            ? `Remove "${device.hostname}" from the fleet? The agent will be uninstalled from the device.`
            : `Remove "${device.hostname}" from the fleet? The device is offline — you may need to manually uninstall the agent.`
          }
          confirmLabel="Delete"
          confirmVariant="danger"
          onConfirm={() => { deleteMutation.mutate(); setConfirmAction(null) }}
          onCancel={() => setConfirmAction(null)}
        />
      )}

      {/* Hardware + OS (side by side) */}
      {(device.hardware_profile || tel?.os) && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {device.hardware_profile && (
            <Section title="Hardware">
              <div className="grid grid-cols-2 gap-4">
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
          {tel?.os && (
            <Section title="Operating System">
              <div className="grid grid-cols-2 gap-4">
                <InfoItem label="OS" value={tel.os.name} />
                <InfoItem label="Kernel" value={tel.os.kernel} />
                <InfoItem label="Arch" value={tel.os.kernel_arch} />
                <InfoItem label="Timezone" value={tel.os.timezone || '-'} />
                {tel.os.uptime != null && <InfoItem label="Uptime" value={formatUptime(tel.os.uptime)} />}
              </div>
            </Section>
          )}
        </div>
      )}

      {/* Live Metrics */}
      {tel && (
        <Section title="Live Metrics">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {tel.cpu && <GaugeBar label="CPU Usage" value={tel.cpu.usage_percent} />}
            {tel.memory && <GaugeBar label="RAM Usage" value={tel.memory.usage_percent} />}
            {tel.disks?.filesystems && tel.disks.filesystems.length > 0 && (() => {
              const boot = tel.disks!.filesystems!.find(fs => fs.mount_point === '/' || fs.mount_point === 'C:\\')
                || tel.disks!.filesystems![0]
              return <GaugeBar label={`Disk Space (${boot.mount_point})`} value={boot.usage_percent} />
            })()}
          </div>
          {tel.cpu && (
            <div className="mt-4 grid grid-cols-3 gap-4">
              <InfoItem label="Load (1m/5m/15m)" value={`${tel.cpu.load_avg_1m.toFixed(2)} / ${tel.cpu.load_avg_5m.toFixed(2)} / ${tel.cpu.load_avg_15m.toFixed(2)}`} />
              {tel.cpu.temperature != null && <InfoItem label="CPU Temp" value={`${tel.cpu.temperature.toFixed(1)}°C`} valueClassName={tel.cpu.temperature > 88 ? 'text-red-400' : tel.cpu.temperature > 75 ? 'text-orange-400' : tel.cpu.temperature > 60 ? 'text-amber-400' : 'text-emerald-400'} />}
            </div>
          )}
        </Section>
      )}

      {/* UPS */}
      {isEnabled('ups') && tel?.ups?.name && (
        <Section title="UPS">
          {tel.ups.on_battery && (
            <div className={`px-4 py-2 mb-4 rounded text-sm ${
              tel.ups.low_battery
                ? 'bg-red-900/30 border border-red-800 text-red-400'
                : 'bg-amber-900/30 border border-amber-800 text-amber-400'
            }`}>
              {tel.ups.low_battery
                ? `UPS ${tel.ups.name} — LOW BATTERY` + (tel.ups.battery_charge != null ? ` (${tel.ups.battery_charge.toFixed(0)}%)` : '')
                : `UPS ${tel.ups.name} — Running on battery`}
            </div>
          )}
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-4">
            <div>
              <p className="text-xs text-gray-500">Status</p>
              <span className={`inline-block px-2 py-0.5 rounded text-xs font-medium mt-1 ${
                tel.ups.low_battery ? 'bg-red-500/20 text-red-400'
                  : tel.ups.on_battery ? 'bg-amber-500/20 text-amber-400'
                  : 'bg-emerald-500/20 text-emerald-400'
              }`}>
                {tel.ups.status || 'Unknown'}
              </span>
            </div>
            <InfoItem label="UPS Name" value={tel.ups.name} />
            {tel.ups.model && <InfoItem label="Model" value={tel.ups.model} />}
            {tel.ups.manufacturer && <InfoItem label="Manufacturer" value={tel.ups.manufacturer} />}
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {tel.ups.battery_charge != null && (
              <BatteryGauge label="Battery Charge" value={tel.ups.battery_charge} />
            )}
            {tel.ups.load != null && (
              <GaugeBar label="UPS Load" value={tel.ups.load} />
            )}
          </div>
          <div className="mt-4 grid grid-cols-2 md:grid-cols-4 gap-4">
            {tel.ups.battery_runtime != null && (
              <InfoItem label="Runtime Remaining" value={formatUptime(tel.ups.battery_runtime)} />
            )}
            {tel.ups.input_voltage != null && (
              <InfoItem label="Input Voltage" value={`${tel.ups.input_voltage.toFixed(1)} V`} />
            )}
            {tel.ups.output_voltage != null && (
              <InfoItem label="Output Voltage" value={`${tel.ups.output_voltage.toFixed(1)} V`} />
            )}
          </div>
          <div className="mt-4 flex gap-2">
            <button
              onClick={() => setAlertDialog({ metric: 'ups_on_battery', targetName: tel.ups!.name })}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-400 hover:text-amber-400 border border-gray-700 hover:border-amber-600/50 rounded-md transition-colors"
            >
              <AlertIcon /> On Battery Alert
            </button>
            <button
              onClick={() => setAlertDialog({ metric: 'ups_battery_percent', targetName: tel.ups!.name })}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-400 hover:text-amber-400 border border-gray-700 hover:border-amber-600/50 rounded-md transition-colors"
            >
              <AlertIcon /> Low Battery Alert
            </button>
          </div>
        </Section>
      )}

      {/* Metric History */}
      {heartbeats && heartbeats.length > 0 && (
        <Section title="Metric History">
          <div className="flex gap-2 mb-4">
            {([1, 6, 24, 168] as const).map(h => (
              <button
                key={h}
                onClick={() => setMetricHours(h)}
                className={`px-2 py-1 text-xs rounded-md transition-colors ${
                  metricHours === h ? 'bg-gray-700 text-white' : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
                }`}
              >
                {h <= 24 ? `${h}h` : '7d'}
              </button>
            ))}
          </div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <MetricChart heartbeats={heartbeats} metricKey="cpu_percent" label="CPU %" color="#3b82f6" />
            <MetricChart heartbeats={heartbeats} metricKey="mem_percent" label="Memory %" color="#8b5cf6" />
            <MetricChart heartbeats={heartbeats} metricKey="disk_io_percent" label="Disk I/O %" color="#f59e0b" subtitle="(all drives)" />
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 mt-6">
            <MetricChart heartbeats={heartbeats} metricKey="disk_read_bytes_sec" label="Disk Read" color="#34d399" unit="bytes/s" subtitle="(all drives)" />
            <MetricChart heartbeats={heartbeats} metricKey="disk_write_bytes_sec" label="Disk Write" color="#fb923c" unit="bytes/s" subtitle="(all drives)" />
          </div>
        </Section>
      )}

      {/* Network */}
      {isEnabled('network') && tel?.network?.interfaces && tel.network.interfaces.length > 0 && (
        <NetworkSection
          interfaces={tel.network.interfaces}
          onCreateAlert={(name) => setAlertDialog({ metric: 'nic_state', targetName: name })}
        />
      )}

      {/* Filesystems */}
      {isEnabled('disk') && tel?.disks?.filesystems && tel.disks.filesystems.length > 0 && (
        <Section title="Filesystems">
          <div className="max-h-64 overflow-auto scrollbar-thin">
            <table className="w-full text-sm min-w-[480px]">
              <thead>
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2 pr-3">Mount</th>
                  <th className="text-left py-2 pr-3">Device</th>
                  <th className="text-left py-2 pr-3">Type</th>
                  <th className="text-right py-2 pr-3">Used</th>
                  <th className="text-right py-2 pr-3">Total</th>
                  <th className="text-right py-2 pr-3">Usage</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {tel.disks.filesystems.map((fs) => (
                  <tr key={fs.mount_point}>
                    <td className="py-2 pr-3font-mono">{fs.mount_point}</td>
                    <td className="py-2 pr-3font-mono text-gray-400">{fs.device}</td>
                    <td className="py-2 pr-3text-gray-400">{fs.fs_type}</td>
                    <td className="py-2 pr-3text-right font-mono">{fs.used_gb >= 1000 ? `${(fs.used_gb / 1024).toFixed(2)} TB` : `${fs.used_gb.toFixed(1)} GB`}</td>
                    <td className="py-2 pr-3text-right font-mono text-gray-400">{fs.total_gb >= 1000 ? `${(fs.total_gb / 1024).toFixed(2)} TB` : `${fs.total_gb.toFixed(1)} GB`}</td>
                    <td className="py-2 pr-3text-right">
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

      {/* Services & Top Processes — side by side */}
      {isEnabled('services') && (
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
      {tel?.services && tel.services.length > 0 && (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-5 h-96 flex flex-col">
          <h2 className="text-sm font-semibold text-gray-300 uppercase tracking-wider mb-4">Services</h2>
          <div className="flex-1 min-h-0 overflow-auto scrollbar-thin">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-gray-900">
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2 pr-3">Name</th>
                  <th className="text-left py-2 pr-3">State</th>
                  <th className="text-left py-2 pr-3">Enabled</th>
                  <th className="py-2 w-10"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {[...tel.services].sort((a, b) => {
                  const rank = (s: string) => s.includes('running') ? 0 : s.includes('failed') ? 2 : 1
                  return rank(a.state) - rank(b.state)
                }).map((svc) => (
                  <tr key={svc.name}>
                    <td className="py-1.5 pr-3 font-mono text-xs break-all">{svc.name}</td>
                    <td className="py-1.5 pr-3">
                      <span className={svc.state.includes('running') ? 'text-emerald-400' : svc.state.includes('failed') ? 'text-red-400' : 'text-gray-400'}>
                        {svc.state}
                      </span>
                    </td>
                    <td className="py-1.5 pr-3 text-gray-400">{svc.enabled ? 'Yes' : 'No'}</td>
                    <td className="py-1.5 pr-3 text-right">
                      <AlertButton
                        onClick={() => setAlertDialog({ metric: 'service_state', targetName: svc.name })}
                        title="Create alert for this service"
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {tel?.processes?.top_by_cpu && tel.processes.top_by_cpu.length > 0 && (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-5 h-96 flex flex-col">
          <h2 className="text-sm font-semibold text-gray-300 uppercase tracking-wider mb-4">Top Processes (by CPU)</h2>
          <div className="flex-1 min-h-0 overflow-auto scrollbar-thin">
          <table className="w-full text-sm min-w-[480px]">
            <thead className="sticky top-0 bg-gray-900">
              <tr className="text-gray-500 text-xs uppercase">
                <th className="text-left py-2 pr-3">PID</th>
                <th className="text-left py-2 pr-3">Name</th>
                <th className="text-right py-2 pr-3">CPU %</th>
                <th className="text-right py-2 pr-3">MEM %</th>
                <th className="text-left py-2 pr-3">User</th>
                <th className="py-2 w-10"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {tel.processes.top_by_cpu.slice(0, 10).map((p) => (
                <tr key={p.pid}>
                  <td className="py-1.5 pr-3 font-mono text-gray-500">{p.pid}</td>
                  <td className="py-1.5 pr-3 font-mono">{p.name}</td>
                  <td className="py-1.5 pr-3 text-right font-mono">{p.cpu_percent.toFixed(1)}</td>
                  <td className="py-1.5 pr-3 text-right font-mono">{p.mem_percent.toFixed(1)}</td>
                  <td className="py-1.5 pr-3 text-gray-400">{p.user}</td>
                  <td className="py-1.5 pr-3 text-right">
                    <AlertButton
                      onClick={() => setAlertDialog({ metric: 'process_missing', targetName: p.name })}
                      title="Create alert for this process"
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
        </div>
      )}
      </div>
      )}

      {/* Updates */}
      {isEnabled('updates') && tel?.updates && tel.updates.pending_updates > 0 && (
        <Section title={`Pending Updates (${tel.updates.pending_updates})`}>
          <div className="max-h-48 overflow-y-auto scrollbar-thin">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2 pr-3">Package</th>
                  <th className="text-left py-2 pr-3">New Version</th>
                  <th className="text-left py-2 pr-3">Security</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {tel.updates.updates?.map((u) => (
                  <tr key={u.name}>
                    <td className="py-1.5 pr-3 font-mono">{u.name}</td>
                    <td className="py-1.5 pr-3 text-gray-400">{u.new_ver}</td>
                    <td className="py-1.5 pr-3">{u.is_security ? <span className="text-red-400">Yes</span> : '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {/* Alert Rules */}
      {alertRules && alertRules.length > 0 && (() => {
        const deviceRules = alertRules.filter(r => !!r.include_devices || !!r.exclude_devices)
        if (deviceRules.length === 0) return null
        return (
          <Section title="Alert Rules">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2 w-6"></th>
                  <th className="text-left py-2 pr-3">Name</th>
                  <th className="text-left py-2 pr-3">Metric</th>
                  <th className="text-left py-2 pr-3">Condition</th>
                  <th className="text-left py-2 pr-3">Severity</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {deviceRules.map(rule => (
                  <tr key={rule.id}>
                    <td className="py-1.5 pr-3">
                      <span className={`w-2 h-2 rounded-full inline-block ${rule.enabled ? 'bg-emerald-400' : 'bg-gray-600'}`} />
                    </td>
                    <td className="py-1.5 pr-3 text-gray-200">{rule.name}</td>
                    <td className="py-1.5 pr-3 text-gray-400 font-mono text-xs">{rule.metric}</td>
                    <td className="py-1.5 pr-3 text-gray-400 font-mono text-xs">
                      {rule.target_state
                        ? `state in [${rule.target_state.replace(/,/g, ', ')}]`
                        : `${rule.operator} ${rule.threshold}`}
                    </td>
                    <td className="py-1.5 pr-3">
                      <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                        rule.severity === 'critical' ? 'bg-red-500/20 text-red-400'
                          : rule.severity === 'info' ? 'bg-blue-500/20 text-blue-400'
                          : 'bg-amber-500/20 text-amber-400'
                      }`}>
                        {rule.severity}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <Link to="/settings/alert-rules" className="text-xs text-blue-400 hover:text-blue-300 mt-3 inline-block">
              Edit rules
            </Link>
          </Section>
        )
      })()}

      {/* Security */}
      {isEnabled('security') && tel?.security && (
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

      {/* USB Devices */}
      {isEnabled('usb') && tel?.usb?.devices && tel.usb.devices.length > 0 && (
        <Section title={`USB Devices (${tel.usb.devices.length})`}>
          <div className="max-h-80 overflow-auto scrollbar-thin">
            <table className="w-full text-sm min-w-[640px]">
              <thead className="sticky top-0 bg-gray-900">
                <tr className="text-gray-500 text-xs uppercase">
                  <th className="text-left py-2 pr-3">Description</th>
                  <th className="text-left py-2 pr-3">Vendor:Product</th>
                  <th className="text-left py-2 pr-3">Serial</th>
                  <th className="text-left py-2 pr-3">Class</th>
                  <th className="text-right py-2 pr-3">Speed</th>
                  <th className="py-2 w-10"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {tel.usb.devices.map((dev, i) => (
                  <tr key={dev.sys_path || i}>
                    <td className="py-2 pr-3text-gray-200">{dev.description}</td>
                    <td className="py-2 pr-3font-mono text-gray-400">{dev.vendor_id}:{dev.product_id}</td>
                    <td className="py-2 pr-3font-mono text-gray-500 text-xs">{dev.serial || '-'}</td>
                    <td className="py-2 pr-3text-gray-400">{dev.device_class || '-'}</td>
                    <td className="py-2 pr-3text-right font-mono text-gray-400">
                      {dev.speed_mbps ? `${dev.speed_mbps} Mbps` : '-'}
                    </td>
                    <td className="py-2 pr-3text-right">
                      <AlertButton
                        onClick={() => setAlertDialog({
                          metric: 'usb_missing',
                          targetName: dev.serial || `${dev.vendor_id}:${dev.product_id}`,
                        })}
                        title="Alert if this device disconnects"
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}

      {/* Hardware Details */}
      {isEnabled('hardware') && tel?.hardware && (
        (tel.hardware.pci_devices && tel.hardware.pci_devices.length > 0) ||
        (tel.hardware.disk_drives && tel.hardware.disk_drives.length > 0) ||
        (tel.hardware.serial_ports && tel.hardware.serial_ports.length > 0) ||
        (tel.hardware.gpus && tel.hardware.gpus.length > 0)
      ) && (
        <Section title={`Hardware Details (${
          (tel.hardware.pci_devices?.length ?? 0) +
          (tel.hardware.disk_drives?.length ?? 0) +
          (tel.hardware.serial_ports?.length ?? 0) +
          (tel.hardware.gpus?.length ?? 0)
        })`}>
          {/* GPUs */}
          {tel.hardware.gpus && tel.hardware.gpus.length > 0 && (
            <div className="mb-4">
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">GPUs ({tel.hardware.gpus.length})</h3>
              <div className="overflow-x-auto scrollbar-thin">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-gray-500 text-xs border-b border-gray-800">
                      <th className="py-2 pr-4 font-medium">Description</th>
                      <th className="py-2 pr-4 font-medium">Driver</th>
                      <th className="py-2 pr-4 font-medium">VRAM</th>
                      <th className="py-2 pr-4 font-medium">PCI Slot</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tel.hardware.gpus.map((gpu, i) => (
                      <tr key={i} className="border-b border-gray-800/50 text-gray-300">
                        <td className="py-1.5 pr-4 text-xs">{gpu.description}</td>
                        <td className="py-1.5 pr-4 text-xs font-mono">{gpu.driver || '-'}</td>
                        <td className="py-1.5 pr-4 text-xs">{gpu.vram_mb ? `${gpu.vram_mb} MB` : '-'}</td>
                        <td className="py-1.5 pr-4 text-xs font-mono text-gray-500">{gpu.pci_slot}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Disk Drives */}
          {tel.hardware.disk_drives && tel.hardware.disk_drives.length > 0 && (
            <div className="mb-4">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-3">
                  <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">Disk Drives ({tel.hardware.disk_drives.length})</h3>
                  <span className="text-[10px] text-gray-600">SMART health alerts are always active</span>
                </div>
                <button
                  onClick={() => setAlertDialog({ metric: 'disk_smart_temp', targetName: '' })}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-400 hover:text-amber-400 border border-gray-700 hover:border-amber-600/50 rounded-md transition-colors"
                >
                  <AlertIcon /> Temperature Alert
                </button>
              </div>
              <div className="overflow-x-auto scrollbar-thin">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-gray-500 text-xs border-b border-gray-800">
                      <th className="py-2 pr-4 font-medium">Name</th>
                      <th className="py-2 pr-4 font-medium">Model</th>
                      <th className="py-2 pr-4 font-medium">Serial</th>
                      <th className="py-2 pr-4 font-medium">Size</th>
                      <th className="py-2 pr-4 font-medium">Type</th>
                      <th className="py-2 pr-4 font-medium">Health</th>
                      <th className="py-2 pr-4 font-medium">Temp</th>
                      <th className="py-2 pr-4 font-medium">Power On</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tel.hardware.disk_drives.map((d, i) => (
                      <tr key={i} className="border-b border-gray-800/50 text-gray-300">
                        <td className="py-1.5 pr-4 text-xs font-mono font-medium">{d.name}</td>
                        <td className="py-1.5 pr-4 text-xs">{d.model || '-'}</td>
                        <td className="py-1.5 pr-4 text-xs font-mono text-gray-500">{d.serial || '-'}</td>
                        <td className="py-1.5 pr-4 text-xs">{d.size_gb >= 1000 ? `${(d.size_gb / 1024).toFixed(2)} TB` : d.size_gb >= 1 ? `${d.size_gb.toFixed(1)} GB` : `${(d.size_gb * 1024).toFixed(0)} MB`}</td>
                        <td className="py-1.5 pr-4 text-xs">
                          {d.type && (
                            <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                              d.type === 'NVMe' ? 'bg-purple-900/50 text-purple-400' :
                              d.type === 'SSD' ? 'bg-blue-900/50 text-blue-400' :
                              d.type === 'HDD' ? 'bg-gray-700 text-gray-300' :
                              'bg-gray-800 text-gray-400'
                            }`}>
                              {d.type}
                            </span>
                          )}
                        </td>
                        <td className="py-1.5 pr-4 text-xs">
                          {d.smart_available ? (
                            <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                              d.smart_health === 'PASSED' ? 'bg-emerald-900/50 text-emerald-400' :
                              d.smart_health === 'FAILED' ? 'bg-red-900/50 text-red-400' :
                              'bg-gray-800 text-gray-500'
                            }`}>
                              {d.smart_health || 'UNKNOWN'}
                              {(d.smart_reallocated != null && d.smart_reallocated > 0) && ` (${d.smart_reallocated} realloc)`}
                              {(d.smart_pending_sector != null && d.smart_pending_sector > 0) && ` (${d.smart_pending_sector} pending)`}
                            </span>
                          ) : (
                            <span className="text-gray-600">-</span>
                          )}
                        </td>
                        <td className="py-1.5 pr-4 text-xs">
                          {d.smart_temp != null ? (
                            <span className={d.smart_temp > 55 ? 'text-red-400' : d.smart_temp > 45 ? 'text-orange-400' : d.smart_temp > 35 ? 'text-amber-400' : 'text-emerald-400'}>
                              {d.smart_temp}&deg;C
                            </span>
                          ) : <span className="text-gray-600">-</span>}
                        </td>
                        <td className="py-1.5 pr-4 text-xs text-gray-400">
                          {d.smart_power_on_hours != null ? (
                            d.smart_power_on_hours >= 8760
                              ? `${(d.smart_power_on_hours / 8760).toFixed(1)}y`
                              : d.smart_power_on_hours >= 720
                                ? `${(d.smart_power_on_hours / 720).toFixed(0)}mo`
                                : `${d.smart_power_on_hours}h`
                          ) : <span className="text-gray-600">-</span>}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* PCI Devices */}
          {tel.hardware.pci_devices && tel.hardware.pci_devices.length > 0 && (
            <div className="mb-4">
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">PCI Devices ({tel.hardware.pci_devices.length})</h3>
              <div className="overflow-x-auto scrollbar-thin max-h-80 overflow-y-auto">
                <table className="w-full text-sm">
                  <thead className="sticky top-0 bg-gray-900">
                    <tr className="text-left text-gray-500 text-xs border-b border-gray-800">
                      <th className="py-2 pr-4 font-medium">Description</th>
                      <th className="py-2 pr-4 font-medium">Class</th>
                      <th className="py-2 pr-4 font-medium">Vendor:Device</th>
                      <th className="py-2 pr-4 font-medium">Driver</th>
                      <th className="py-2 pr-4 font-medium">Slot</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tel.hardware.pci_devices.map((d, i) => (
                      <tr key={i} className="border-b border-gray-800/50 text-gray-300">
                        <td className="py-1.5 pr-4 text-xs">{d.description}</td>
                        <td className="py-1.5 pr-4 text-xs text-gray-400">{d.class_name || d.class_code}</td>
                        <td className="py-1.5 pr-4 text-xs font-mono text-gray-500">{d.vendor_id}:{d.device_id}</td>
                        <td className="py-1.5 pr-4 text-xs font-mono">{d.driver || '-'}</td>
                        <td className="py-1.5 pr-4 text-xs font-mono text-gray-500">{d.slot}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* Serial Ports */}
          {tel.hardware.serial_ports && tel.hardware.serial_ports.length > 0 && (
            <div>
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Serial Ports ({tel.hardware.serial_ports.length})</h3>
              <div className="overflow-x-auto scrollbar-thin">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-gray-500 text-xs border-b border-gray-800">
                      <th className="py-2 pr-4 font-medium">Name</th>
                      <th className="py-2 pr-4 font-medium">Path</th>
                      <th className="py-2 pr-4 font-medium">Type</th>
                      <th className="py-2 pr-4 font-medium">Driver</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tel.hardware.serial_ports.map((p, i) => (
                      <tr key={i} className="border-b border-gray-800/50 text-gray-300">
                        <td className="py-1.5 pr-4 text-xs font-mono font-medium">{p.name}</td>
                        <td className="py-1.5 pr-4 text-xs font-mono text-gray-500">{p.path}</td>
                        <td className="py-1.5 pr-4 text-xs">{p.type || '-'}</td>
                        <td className="py-1.5 pr-4 text-xs font-mono">{p.driver || '-'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </Section>
      )}

      {/* Web Servers */}
      {isEnabled('web_servers') && tel?.web_servers?.servers && tel.web_servers.servers.length > 0 && (
        <Section title="Web Servers">
          <div className="space-y-6">
            {tel.web_servers.servers.map((srv, srvIdx) => {
              const hasSecurityConfig = srv.security_config && (
                (srv.security_config.security_headers && Object.keys(srv.security_config.security_headers).length > 0) ||
                (srv.security_config.rate_limiting && srv.security_config.rate_limiting.length > 0) ||
                (srv.security_config.access_controls && srv.security_config.access_controls.length > 0) ||
                (srv.security_config.allowed_methods && srv.security_config.allowed_methods.length > 0) ||
                (srv.security_config.cors_origins && srv.security_config.cors_origins.length > 0)
              )
              const hasRightColumn = (srv.certs && srv.certs.length > 0) ||
                (srv.upstreams && srv.upstreams.length > 0)

              return (
              <div key={srv.name} className="space-y-4">
                {/* Header */}
                <div className="flex items-center gap-3 flex-wrap">
                  <span className="text-gray-200 font-semibold capitalize">{srv.name}</span>
                  {srv.version && (
                    <span className="px-2 py-0.5 rounded text-xs bg-gray-700 text-gray-300 font-mono">{srv.version}</span>
                  )}
                  <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium ${
                    srv.status === 'running' ? 'bg-emerald-500/20 text-emerald-400'
                      : srv.status === 'stopped' ? 'bg-red-500/20 text-red-400'
                      : 'bg-gray-500/20 text-gray-400'
                  }`}>
                    <span className={`w-1.5 h-1.5 rounded-full ${
                      srv.status === 'running' ? 'bg-emerald-400' : srv.status === 'stopped' ? 'bg-red-400' : 'bg-gray-400'
                    }`} />
                    {srv.status}
                  </span>
                  {srv.config_valid !== undefined && (
                    <span className={`text-xs ${srv.config_valid ? 'text-emerald-400' : 'text-red-400'}`}>
                      config {srv.config_valid ? 'valid' : 'invalid'}
                    </span>
                  )}
                  {srv.config_path && (
                    <span className="text-xs text-gray-500 font-mono">{srv.config_path}</span>
                  )}
                </div>

                {srv.config_error && (
                  <div className="px-3 py-2 rounded text-xs bg-red-900/30 border border-red-800 text-red-400 font-mono whitespace-pre-wrap">
                    {srv.config_error}
                  </div>
                )}

                {/* Security Config (full width, above tables) */}
                {hasSecurityConfig && srv.security_config && (
                  <div>
                    <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Security Config</h3>
                    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
                      {srv.security_config.security_headers && Object.keys(srv.security_config.security_headers).length > 0 && (
                        <div>
                          <p className="text-xs text-gray-500 mb-1">Headers</p>
                          <div className="space-y-0.5">
                            {Object.entries(srv.security_config.security_headers).map(([k, v]) => (
                              <div key={k} className="flex gap-2 text-xs">
                                <span className="text-gray-400 font-mono shrink-0">{k}:</span>
                                <span className="text-gray-300 font-mono break-all">{v}</span>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                      {srv.security_config.rate_limiting && srv.security_config.rate_limiting.length > 0 && (
                        <div>
                          <p className="text-xs text-gray-500 mb-1">Rate Limiting</p>
                          <div className="space-y-0.5">
                            {srv.security_config.rate_limiting.map((r, i) => (
                              <div key={i} className="text-xs font-mono text-gray-300">
                                {r.zone}: {r.rate}{r.burst ? ` burst=${r.burst}` : ''}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                      {srv.security_config.access_controls && srv.security_config.access_controls.length > 0 && (
                        <div>
                          <p className="text-xs text-gray-500 mb-1">Access Controls</p>
                          <div className="space-y-0.5">
                            {srv.security_config.access_controls.map((r, i) => (
                              <div key={i} className="text-xs font-mono">
                                <span className={r.directive === 'allow' ? 'text-emerald-400' : 'text-red-400'}>{r.directive}</span>
                                {' '}<span className="text-gray-300">{r.value}</span>
                                {r.location && <span className="text-gray-500"> ({r.location})</span>}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                      <div className="flex flex-wrap gap-x-6 gap-y-2">
                        {srv.security_config.allowed_methods && srv.security_config.allowed_methods.length > 0 && (
                          <div>
                            <p className="text-xs text-gray-500 mb-1">Allowed Methods</p>
                            <p className="text-xs font-mono text-gray-300">{srv.security_config.allowed_methods.join(', ')}</p>
                          </div>
                        )}
                        {srv.security_config.cors_origins && srv.security_config.cors_origins.length > 0 && (
                          <div>
                            <p className="text-xs text-gray-500 mb-1">CORS Origins</p>
                            <p className="text-xs font-mono text-gray-300">{srv.security_config.cors_origins.join(', ')}</p>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                )}

                {/* Two-column layout: Sites (left) | Certs + Upstreams (right) */}
                <div className={`grid gap-4 ${hasRightColumn ? 'grid-cols-1 xl:grid-cols-[1fr_1fr]' : ''}`}>
                  {/* Sites */}
                  {srv.sites && srv.sites.length > 0 && (
                    <div>
                      <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Sites</h3>
                      <div className="max-h-80 overflow-auto scrollbar-thin">
                        <table className="w-full text-sm min-w-[480px]">
                          <thead className="sticky top-0 bg-gray-900 z-10">
                            <tr className="text-gray-500 text-xs uppercase">
                              <th className="text-left py-2 pr-3">Server Name(s)</th>
                              <th className="text-left py-2 pr-3">Listen</th>
                              <th className="text-left py-2 pr-3">Target</th>
                              <th className="text-left py-2 pr-3">SSL</th>
                              <th className="text-left py-2 pr-3">Config</th>
                            </tr>
                          </thead>
                          <tbody className="divide-y divide-gray-800/50">
                            {srv.sites.map((site, i) => (
                              <tr key={i}>
                                <td className="py-1.5 pr-3 font-mono text-xs">{site.server_names?.join(', ') || '-'}</td>
                                <td className="py-1.5 pr-3 font-mono text-xs text-gray-400 max-w-[150px] break-words">{site.listen?.join(', ') || '-'}</td>
                                <td className="py-1.5 pr-3 font-mono text-xs text-gray-400 max-w-[200px] truncate" title={site.proxy_pass || site.root}>{site.proxy_pass || site.root || '-'}</td>
                                <td className="py-1.5 pr-3">
                                  {site.ssl_cert ? (
                                    <span className="text-emerald-400 text-xs">Yes</span>
                                  ) : (
                                    <span className="text-gray-500 text-xs">No</span>
                                  )}
                                </td>
                                <td className="py-1.5 pr-3 font-mono text-xs text-gray-500 max-w-[140px] truncate" title={site.config_file}>
                                  {site.config_file ? site.config_file.split('/').pop() : '-'}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    </div>
                  )}

                  {/* Right column: Certs, Upstreams, Security Config */}
                  {hasRightColumn && (
                    <div className="space-y-4">
                      {/* Certificates */}
                      {srv.certs && srv.certs.length > 0 && (
                        <div>
                          <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Certificates</h3>
                          <div className="max-h-48 overflow-auto scrollbar-thin">
                            <table className="w-full text-sm">
                              <thead className="sticky top-0 bg-gray-900">
                                <tr className="text-gray-500 text-xs uppercase">
                                  <th className="text-left py-2 pr-3">Subject</th>
                                  <th className="text-left py-2 pr-3">Expires</th>
                                  <th className="text-left py-2 pr-3">Days Left</th>
                                  <th className="text-left py-2 pr-3">Issuer</th>
                                </tr>
                              </thead>
                              <tbody className="divide-y divide-gray-800/50">
                                {srv.certs.map((cert, i) => {
                                  const expiryColor = cert.days_left <= 0 ? 'text-red-400'
                                    : cert.days_left <= 7 ? 'text-red-400'
                                    : cert.days_left <= 30 ? 'text-amber-400'
                                    : 'text-emerald-400'
                                  return (
                                    <tr key={i}>
                                      <td className="py-1.5 pr-3 font-mono text-xs" title={cert.sans?.join(', ')}>{cert.subject || '-'}</td>
                                      <td className="py-1.5 pr-3 text-xs text-gray-400">
                                        {cert.not_after ? new Date(cert.not_after).toLocaleDateString() : '-'}
                                      </td>
                                      <td className={`py-1.5 pr-3 text-xs font-medium ${expiryColor}`}>
                                        {cert.days_left <= 0 ? (
                                          <span className="px-1.5 py-0.5 rounded bg-red-500/20 text-red-400 font-semibold">EXPIRED</span>
                                        ) : (
                                          `${cert.days_left}d`
                                        )}
                                      </td>
                                      <td className="py-1.5 pr-3 text-xs text-gray-500">{cert.issuer || '-'}</td>
                                    </tr>
                                  )
                                })}
                              </tbody>
                            </table>
                          </div>
                        </div>
                      )}

                      {/* Upstreams */}
                      {srv.upstreams && srv.upstreams.length > 0 && (
                        <div>
                          <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Upstreams</h3>
                          <div className="space-y-1.5">
                            {srv.upstreams.map((up, i) => (
                              <div key={i} className="flex items-start gap-2">
                                <span className="font-mono text-xs text-gray-300 shrink-0">{up.name}:</span>
                                <div className="flex flex-wrap gap-1">
                                  {up.servers?.map((s, j) => (
                                    <span key={j} className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs font-mono ${
                                      s.down ? 'bg-red-500/20 text-red-400' : s.backup ? 'bg-amber-500/20 text-amber-400' : 'bg-gray-700 text-gray-300'
                                    }`}>
                                      {s.address}
                                      {s.weight ? ` w:${s.weight}` : ''}
                                      {s.backup && ' (backup)'}
                                      {s.down && ' (down)'}
                                    </span>
                                  )) || <span className="text-xs text-gray-500">-</span>}
                                </div>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                    </div>
                  )}
                </div>

                {/* Separator between servers */}
                {srvIdx < tel.web_servers!.servers!.length - 1 && (
                  <hr className="border-gray-800" />
                )}
              </div>
              )
            })}
          </div>
        </Section>
      )}

      {/* Cron Jobs & Scheduled Tasks */}
      {(isEnabled('cron') || isEnabled('systemd_timers')) && tel?.cron_jobs && (
        (isEnabled('cron') && tel.cron_jobs.jobs && tel.cron_jobs.jobs.length > 0) ||
        (isEnabled('systemd_timers') && tel.cron_jobs.timers && tel.cron_jobs.timers.length > 0)
      ) && (
        <Section title={`Cron Jobs & Scheduled Tasks (${
          (isEnabled('cron') ? tel.cron_jobs.jobs?.length ?? 0 : 0) +
          (isEnabled('systemd_timers') ? tel.cron_jobs.timers?.length ?? 0 : 0)
        })`}>
          {isEnabled('cron') && tel.cron_jobs.jobs && tel.cron_jobs.jobs.length > 0 && (
            <div className="mb-4">
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Cron Jobs ({tel.cron_jobs.jobs.length})</h3>
              <div className="overflow-x-auto scrollbar-thin">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-gray-500 text-xs border-b border-gray-800">
                      <th className="py-2 pr-4 font-medium">User</th>
                      <th className="py-2 pr-4 font-medium">Schedule</th>
                      <th className="py-2 pr-4 font-medium">Command</th>
                      <th className="py-2 pr-4 font-medium">Source</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tel.cron_jobs.jobs.map((job, i) => (
                      <tr key={i} className="border-b border-gray-800/50 text-gray-300">
                        <td className="py-1.5 pr-4 font-mono text-xs">{job.user}</td>
                        <td className="py-1.5 pr-4 font-mono text-xs">
                          <Tooltip content={(() => { try { return cronstrue.toString(job.schedule) } catch { return null } })()}>
                            <span className="cursor-help border-b border-dotted border-gray-600">{job.schedule}</span>
                          </Tooltip>
                        </td>
                        <td className="py-1.5 pr-4 text-xs max-w-md truncate" title={job.command}>{job.command}</td>
                        <td className="py-1.5 pr-4 text-xs text-gray-500">{job.source}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
          {isEnabled('systemd_timers') && tel.cron_jobs.timers && tel.cron_jobs.timers.length > 0 && (
            <div>
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Systemd Timers ({tel.cron_jobs.timers.length})</h3>
              <div className="overflow-x-auto scrollbar-thin">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-gray-500 text-xs border-b border-gray-800">
                      <th className="py-2 pr-4 font-medium">Name</th>
                      <th className="py-2 pr-4 font-medium">Calendar</th>
                      <th className="py-2 pr-4 font-medium">Next Run</th>
                      <th className="py-2 pr-4 font-medium">Last Run</th>
                      <th className="py-2 pr-4 font-medium">Unit</th>
                      <th className="py-2 pr-4 font-medium">Enabled</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tel.cron_jobs.timers.map((timer, i) => (
                      <tr key={i} className="border-b border-gray-800/50 text-gray-300">
                        <td className="py-1.5 pr-4 text-xs font-medium">{timer.name}</td>
                        <td className="py-1.5 pr-4 font-mono text-xs">
                          <Tooltip content={parseSystemdCalendar(timer.calendar)}>
                            <span className="cursor-help border-b border-dotted border-gray-600">{timer.calendar}</span>
                          </Tooltip>
                        </td>
                        <td className="py-1.5 pr-4 text-xs text-gray-400">{formatRunDate(timer.next_run)}</td>
                        <td className="py-1.5 pr-4 text-xs text-gray-400">{formatRunDate(timer.last_run)}</td>
                        <td className="py-1.5 pr-4 font-mono text-xs">{timer.unit}</td>
                        <td className="py-1.5 pr-4">
                          <span className={`inline-block w-2 h-2 rounded-full ${timer.enabled ? 'bg-emerald-400' : 'bg-gray-600'}`} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </Section>
      )}

      {/* Device Logs */}
      {isEnabled('logs') && (
      <Section title="Device System Logs">
        <div className="flex items-center justify-between mb-3">
          <div className="flex gap-2">
            {([
              { label: 'All', value: 7 },
              { label: 'Info', value: 6 },
              { label: 'Notice', value: 5 },
              { label: 'Warning', value: 4 },
              { label: 'Error', value: 3 },
              { label: 'Crit', value: 2 },
            ] as const).map(p => (
              <button
                key={p.value}
                onClick={() => setLogPriority(p.value)}
                className={`px-2 py-1 text-xs rounded-md transition-colors ${
                  logPriority === p.value ? 'bg-gray-700 text-white' : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>
          {canCommand && (
            <button
              onClick={() => fetchLogsMutation.mutate({ hours: 24, priority: 6 })}
              disabled={fetchLogsMutation.isPending}
              className="px-3 py-1.5 text-xs bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-md transition-colors disabled:opacity-50 flex items-center gap-1.5"
            >
              {fetchLogsMutation.isPending ? 'Fetching...' : 'Fetch Last 24h'}
            </button>
          )}
        </div>
        {fetchLogsMutation.isSuccess && (
          <p className="text-xs text-emerald-400 mb-2">Logs fetched — refreshing...</p>
        )}
        {fetchLogsMutation.isError && (
          <p className="text-xs text-red-400 mb-2">Failed to fetch logs: {(fetchLogsMutation.error as Error).message}</p>
        )}
        {deviceLogs && deviceLogs.length > 0 ? (
          <div className="max-h-96 overflow-y-auto scrollbar-thin">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-gray-900 z-10">
                <tr className="text-gray-500 text-xs uppercase border-b border-gray-700">
                  <th className="text-left py-2 pr-3">Time</th>
                  <th className="text-left py-2 pr-3">Priority</th>
                  <th className="text-left py-2 pr-3">Unit</th>
                  <th className="text-left py-2 pr-3">Message</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {deviceLogs.map((log, i) => {
                  const rowColor = log.priority <= 2 ? 'text-red-400'
                    : log.priority === 3 ? 'text-red-400/80'
                    : log.priority === 4 ? 'text-amber-400'
                    : log.priority === 5 ? 'text-gray-300'
                    : log.priority === 6 ? 'text-blue-400/80'
                    : 'text-gray-500'
                  return (
                    <tr key={i} className={rowColor}>
                      <td className="py-1.5 pr-3 font-mono text-xs whitespace-nowrap opacity-70">{new Date(log.timestamp).toLocaleString()}</td>
                      <td className="py-1.5 pr-3">
                        <span className="text-xs font-medium">
                          {log.priority <= 2 ? 'CRIT' : log.priority === 3 ? 'ERR' : log.priority === 4 ? 'WARN' : log.priority === 5 ? 'NOTICE' : log.priority === 6 ? 'INFO' : 'DEBUG'}
                        </span>
                      </td>
                      <td className="py-1.5 pr-3 font-mono text-xs opacity-70">{log.unit || '-'}</td>
                      <td className="py-1.5 pr-3 text-xs max-w-md truncate">{log.message}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="text-center py-6 text-gray-500 text-sm">
            No logs stored yet.{canCommand && ' Click "Fetch Last 24h" to pull logs from this device.'}
          </div>
        )}
      </Section>
      )}

      {/* Recent Events */}
      {events && events.filter(e => e.device_id === id).length > 0 && (() => {
        const EVENTS_PER_PAGE = 10
        const deviceEvents = events.filter(e => e.device_id === id)
        const totalEventPages = Math.ceil(deviceEvents.length / EVENTS_PER_PAGE)
        const safeEventsPage = Math.min(eventsPage, Math.max(0, totalEventPages - 1))
        const pagedEvents = deviceEvents.slice(safeEventsPage * EVENTS_PER_PAGE, (safeEventsPage + 1) * EVENTS_PER_PAGE)
        return (
          <Section title={`Recent Events (${deviceEvents.length})`}>
            <div className="space-y-2">
              {pagedEvents.map(e => (
                <div key={e.id} className="flex items-center gap-3 text-sm">
                  <span className={`w-2 h-2 rounded-full flex-shrink-0 ${
                    e.severity === 'critical' ? 'bg-red-400' : e.severity === 'warning' ? 'bg-amber-400' : 'bg-blue-400'
                  }`} />
                  <span className="text-gray-400 font-mono text-xs">{new Date(e.created_at).toLocaleString()}</span>
                  <span className="text-gray-200">{e.message}</span>
                </div>
              ))}
            </div>
            {totalEventPages > 1 && (
              <div className="flex items-center justify-between mt-3 pt-3 border-t border-gray-800">
                <span className="text-xs text-gray-500">
                  {safeEventsPage * EVENTS_PER_PAGE + 1}–{Math.min((safeEventsPage + 1) * EVENTS_PER_PAGE, deviceEvents.length)} of {deviceEvents.length}
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
        )
      })()}

      {/* Activity Log */}
      <Section title="Agent Activity Log">
        <ActivityLog deviceId={id!} />
      </Section>

      {/* Create Alert Dialog */}
      {alertDialog && (
        <CreateAlertDialog
          metric={alertDialog.metric}
          targetName={alertDialog.targetName}
          targetState={alertDialog.targetState}
          includeDevices={device.hostname}
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
    <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
      <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-11a1 1 0 10-2 0v2H7a1 1 0 100 2h2v2a1 1 0 102 0v-2h2a1 1 0 100-2h-2V7z" clipRule="evenodd" />
    </svg>
  )
}

function AlertButton({ onClick, title }: { onClick: () => void; title: string }) {
  return (
    <button
      onClick={onClick}
      className="p-1.5 rounded hover:bg-gray-800 text-gray-600 hover:text-amber-400 transition-colors"
      title={title}
    >
      <AlertIcon />
    </button>
  )
}

function NicTable({ interfaces, onCreateAlert }: { interfaces: import('../types/models').NetworkInterface[]; onCreateAlert?: (name: string) => void }) {
  return (
    <div className="overflow-x-auto scrollbar-thin">
    <table className="w-full text-sm min-w-[480px]">
      <thead>
        <tr className="text-gray-500 text-xs uppercase">
          <th className="text-left py-2 pr-3">Name</th>
          <th className="text-left py-2 pr-3">State</th>
          <th className="text-left py-2 pr-3">IPv4</th>
          <th className="text-left py-2 pr-3">MAC</th>
          <th className="text-right py-2 pr-3">TX / RX</th>
          {onCreateAlert && <th className="py-2 w-10"></th>}
        </tr>
      </thead>
      <tbody className="divide-y divide-gray-800/50">
        {interfaces.map((iface) => (
          <tr key={iface.name}>
            <td className="py-2 pr-3font-mono">{iface.name}</td>
            <td className="py-2 pr-3">
              <span className={iface.state === 'UP' ? 'text-emerald-400' : 'text-gray-500'}>{iface.state}</span>
            </td>
            <td className="py-2 pr-3font-mono text-gray-400">{iface.ipv4?.join(', ') || '-'}</td>
            <td className="py-2 pr-3font-mono text-gray-500">{iface.mac || '-'}</td>
            <td className="py-2 pr-3text-right font-mono text-gray-400">
              {formatBytes(iface.bytes_sent)} / {formatBytes(iface.bytes_recv)}
            </td>
            {onCreateAlert && (
              <td className="py-2 pr-3text-right">
                <AlertButton
                  onClick={() => onCreateAlert(iface.name)}
                  title="Create alert for this interface"
                />
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
            <div className="mt-2 max-h-48 overflow-y-auto scrollbar-thin">
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

function InfoItem({ label, value, valueClassName }: { label: string; value: string; valueClassName?: string }) {
  return (
    <div>
      <p className="text-xs text-gray-500">{label}</p>
      <p className={`text-sm ${valueClassName || 'text-gray-200'}`}>{value}</p>
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
  if (bytes < 1024 * 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
  return `${(bytes / 1024 / 1024 / 1024 / 1024).toFixed(1)} TB`
}

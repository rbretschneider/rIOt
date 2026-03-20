import { useState, useRef, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { DeviceProbeWithResultEnriched, ProbeWithResult } from '../types/models'
import ProbeModal, { emptyProbe, probeToForm, getTarget, type ProbeForm } from '../components/ProbeModal'
import DeviceProbeModal, { type DeviceProbeForm } from '../components/DeviceProbeModal'

export default function Probes() {
  const qc = useQueryClient()
  const navigate = useNavigate()

  const { data: probes = [], isLoading: serverLoading } = useQuery({
    queryKey: ['probes'],
    queryFn: api.getProbes,
    refetchInterval: 30_000,
  })

  const { data: allDeviceProbes = [], isLoading: deviceLoading } = useQuery({
    queryKey: ['all-device-probes'],
    queryFn: api.getAllDeviceProbes,
    refetchInterval: 30_000,
  })

  const { data: devices = [] } = useQuery({
    queryKey: ['devices'],
    queryFn: api.getDevices,
    staleTime: 60_000,
  })

  // --- Server probe state ---
  const [editingServer, setEditingServer] = useState<ProbeForm | null>(null)
  const [isNewServer, setIsNewServer] = useState(false)

  // --- Device probe state ---
  const [editingDevice, setEditingDevice] = useState<DeviceProbeForm | null>(null)
  // editingDeviceId holds the device_id for the probe being edited, since DeviceProbeForm
  // does not carry device_id (that field lives on the DeviceProbe model, not the form).
  const editingDeviceId = useRef<string>('')
  const [showDevicePicker, setShowDevicePicker] = useState(false)
  const devicePickerRef = useRef<HTMLDivElement>(null)

  // Close device picker on outside click
  useEffect(() => {
    if (!showDevicePicker) return
    function handleClick(e: MouseEvent) {
      if (devicePickerRef.current && !devicePickerRef.current.contains(e.target as Node)) {
        setShowDevicePicker(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [showDevicePicker])

  // --- Server probe mutations ---
  const serverRunMutation = useMutation({
    mutationFn: api.runProbe,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  const serverSaveMutation = useMutation({
    mutationFn: (probe: ProbeForm) =>
      isNewServer ? api.createProbe(probe) : api.updateProbe(probe.id!, probe),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['probes'] })
      setEditingServer(null)
    },
  })

  const serverDeleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteProbe(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  const serverToggleMutation = useMutation({
    mutationFn: (probe: ProbeWithResult) =>
      api.updateProbe(probe.id, { ...probe, enabled: !probe.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  // --- Device probe mutations ---
  const deviceRunMutation = useMutation({
    mutationFn: ({ deviceId, probeId }: { deviceId: string; probeId: number }) =>
      api.runDeviceProbe(deviceId, probeId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['all-device-probes'] }),
  })

  const deviceSaveMutation = useMutation({
    mutationFn: (probe: DeviceProbeForm) =>
      api.updateDeviceProbe(editingDeviceId.current, probe.id!, probe),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['all-device-probes'] })
      setEditingDevice(null)
    },
  })

  const deviceDeleteMutation = useMutation({
    mutationFn: ({ deviceId, probeId }: { deviceId: string; probeId: number }) =>
      api.deleteDeviceProbe(deviceId, probeId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['all-device-probes'] }),
  })

  const deviceToggleMutation = useMutation({
    mutationFn: (probe: DeviceProbeWithResultEnriched) =>
      api.updateDeviceProbe(probe.device_id, probe.id, { ...probe, enabled: !probe.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['all-device-probes'] }),
  })

  if (serverLoading || deviceLoading) return <div className="text-gray-500">Loading...</div>

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-white">Probes</h1>

      {/* Server Probes Section */}
      <div>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">Server Probes</h2>
          <button
            onClick={() => { setEditingServer({ ...emptyProbe }); setIsNewServer(true) }}
            className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
          >
            Add Probe
          </button>
        </div>

        <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-x-auto scrollbar-thin">
          <table className="w-full text-sm min-w-[700px]">
            <thead>
              <tr className="text-left text-gray-400 border-b border-gray-800">
                <th className="px-4 py-3">Enabled</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">Type</th>
                <th className="px-4 py-3">Target</th>
                <th className="px-4 py-3">Interval</th>
                <th className="px-4 py-3">Success Rate</th>
                <th className="px-4 py-3">Checks</th>
                <th className="px-4 py-3">Latency</th>
                <th className="px-4 py-3">Last Check</th>
                <th className="px-4 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {probes.map(probe => {
                const lr = probe.latest_result
                const status = lr ? (lr.success ? 'up' : 'down') : 'unknown'
                return (
                  <tr key={probe.id} className="border-b border-gray-800/50 text-gray-300">
                    <td className="px-4 py-3">
                      <button
                        onClick={() => serverToggleMutation.mutate(probe)}
                        className={`w-8 h-4 rounded-full transition-colors relative flex-shrink-0 ${
                          probe.enabled ? 'bg-emerald-600' : 'bg-gray-600'
                        }`}
                        title={probe.enabled ? 'Disable' : 'Enable'}
                      >
                        <span className={`absolute top-0.5 w-3 h-3 rounded-full bg-white transition-transform ${
                          probe.enabled ? 'left-4' : 'left-0.5'
                        }`} />
                      </button>
                    </td>
                    <td className="px-4 py-3">
                      <span className={`w-3 h-3 rounded-full inline-block ${
                        status === 'up' ? 'bg-emerald-500' : status === 'down' ? 'bg-red-500' : 'bg-gray-600'
                      }`} />
                    </td>
                    <td className="px-4 py-3 text-white">
                      <Link to={`/probes/${probe.id}`} className="hover:text-blue-400 transition-colors">
                        {probe.name}
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-gray-400 uppercase text-xs">{probe.type}</td>
                    <td className="px-4 py-3 text-gray-400 max-w-[160px] truncate">{getTarget(probe)}</td>
                    <td className="px-4 py-3 text-gray-400">{probe.interval_seconds}s</td>
                    <td className="px-4 py-3">
                      {probe.success_rate != null ? (
                        <span className={`font-mono text-xs font-semibold ${
                          probe.success_rate >= 0.95 ? 'text-emerald-400' :
                          probe.success_rate >= 0.8 ? 'text-amber-400' : 'text-red-400'
                        }`}>
                          {(probe.success_rate * 100).toFixed(1)}%
                        </span>
                      ) : (
                        <span className="text-gray-600">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-gray-400">{probe.total_checks || '—'}</td>
                    <td className="px-4 py-3 font-mono text-xs">
                      {lr ? (
                        <span className={lr.success ? 'text-emerald-400' : 'text-red-400'}>
                          {lr.latency_ms.toFixed(1)}ms
                        </span>
                      ) : (
                        <span className="text-gray-600">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-gray-400 text-xs">
                      {lr ? new Date(lr.created_at).toLocaleTimeString() : '—'}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={() => serverRunMutation.mutate(probe.id)}
                          disabled={serverRunMutation.isPending}
                          className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors disabled:opacity-50"
                        >
                          Run
                        </button>
                        <button
                          onClick={() => { setEditingServer(probeToForm(probe)); setIsNewServer(false) }}
                          className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors"
                        >
                          Edit
                        </button>
                        <button
                          onClick={() => { if (confirm('Delete this probe?')) serverDeleteMutation.mutate(probe.id) }}
                          className="px-2 py-1 text-xs text-red-400/70 hover:text-red-400 border border-red-900/50 hover:border-red-700 rounded transition-colors"
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
              {probes.length === 0 && (
                <tr>
                  <td colSpan={11} className="px-4 py-8 text-center text-gray-500">
                    No server probes configured. Click "Add Probe" to start monitoring.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Device Probes Section */}
      <div className="mt-8">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">Device Probes</h2>
          <div className="relative" ref={devicePickerRef}>
            <button
              onClick={() => setShowDevicePicker(v => !v)}
              className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
            >
              Add Probe
            </button>
            {showDevicePicker && (
              <div className="absolute right-0 mt-1 w-56 bg-gray-800 border border-gray-700 rounded-lg shadow-lg z-10 overflow-y-auto max-h-64 scrollbar-thin">
                {devices.length === 0 ? (
                  <p className="px-4 py-3 text-sm text-gray-400">No devices available</p>
                ) : (
                  <>
                    <p className="px-4 py-2 text-xs text-gray-500 border-b border-gray-700">Select a device</p>
                    {devices.map(device => (
                      <button
                        key={device.id}
                        onClick={() => {
                          setShowDevicePicker(false)
                          navigate(`/devices/${device.id}/probes`)
                        }}
                        className="w-full text-left px-4 py-2 text-sm text-gray-300 hover:bg-gray-700 hover:text-white transition-colors"
                      >
                        {device.hostname}
                      </button>
                    ))}
                  </>
                )}
              </div>
            )}
          </div>
        </div>

        <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-x-auto scrollbar-thin">
          <table className="w-full text-sm min-w-[700px]">
            <thead>
              <tr className="text-left text-gray-400 border-b border-gray-800">
                <th className="px-4 py-3">Enabled</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">Type</th>
                <th className="px-4 py-3">Device</th>
                <th className="px-4 py-3">Interval</th>
                <th className="px-4 py-3">Success Rate</th>
                <th className="px-4 py-3">Checks</th>
                <th className="px-4 py-3">Latency</th>
                <th className="px-4 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {allDeviceProbes.map(probe => {
                const lr = probe.latest_result
                const status = lr ? (lr.success ? 'up' : 'down') : 'unknown'
                return (
                  <tr key={probe.id} className="border-b border-gray-800/50 text-gray-300">
                    <td className="px-4 py-3">
                      <button
                        onClick={() => deviceToggleMutation.mutate(probe)}
                        className={`w-8 h-4 rounded-full transition-colors relative flex-shrink-0 ${
                          probe.enabled ? 'bg-emerald-600' : 'bg-gray-600'
                        }`}
                        title={probe.enabled ? 'Disable' : 'Enable'}
                      >
                        <span className={`absolute top-0.5 w-3 h-3 rounded-full bg-white transition-transform ${
                          probe.enabled ? 'left-4' : 'left-0.5'
                        }`} />
                      </button>
                    </td>
                    <td className="px-4 py-3">
                      <span className={`w-3 h-3 rounded-full inline-block ${
                        status === 'up' ? 'bg-emerald-500' : status === 'down' ? 'bg-red-500' : 'bg-gray-600'
                      }`} />
                    </td>
                    <td className="px-4 py-3 text-white">{probe.name}</td>
                    <td className="px-4 py-3 text-gray-400 uppercase text-xs">{probe.type}</td>
                    <td className="px-4 py-3">
                      <Link
                        to={`/devices/${probe.device_id}`}
                        className="text-blue-400 hover:text-blue-300 transition-colors"
                      >
                        {probe.device_hostname || probe.device_id}
                      </Link>
                    </td>
                    <td className="px-4 py-3 text-gray-400">{probe.interval_seconds}s</td>
                    <td className="px-4 py-3">
                      {probe.success_rate != null ? (
                        <span className={`font-mono text-xs font-semibold ${
                          probe.success_rate >= 0.95 ? 'text-emerald-400' :
                          probe.success_rate >= 0.8 ? 'text-amber-400' : 'text-red-400'
                        }`}>
                          {(probe.success_rate * 100).toFixed(1)}%
                        </span>
                      ) : (
                        <span className="text-gray-600">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-gray-400">{probe.total_checks || '—'}</td>
                    <td className="px-4 py-3 font-mono text-xs">
                      {lr ? (
                        <span className={lr.success ? 'text-emerald-400' : 'text-red-400'}>
                          {lr.latency_ms.toFixed(1)}ms
                        </span>
                      ) : (
                        <span className="text-gray-600">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={() => deviceRunMutation.mutate({ deviceId: probe.device_id, probeId: probe.id })}
                          disabled={deviceRunMutation.isPending}
                          className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors disabled:opacity-50"
                        >
                          Run
                        </button>
                        <button
                          onClick={() => {
                            editingDeviceId.current = probe.device_id
                            setEditingDevice({
                              id: probe.id,
                              name: probe.name,
                              type: probe.type,
                              enabled: probe.enabled,
                              config: { ...probe.config },
                              assertions: [...probe.assertions],
                              interval_seconds: probe.interval_seconds,
                              timeout_seconds: probe.timeout_seconds,
                            })
                          }}
                          className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors"
                        >
                          Edit
                        </button>
                        <button
                          onClick={() => {
                            if (confirm('Delete this device probe?')) {
                              deviceDeleteMutation.mutate({ deviceId: probe.device_id, probeId: probe.id })
                            }
                          }}
                          className="px-2 py-1 text-xs text-red-400/70 hover:text-red-400 border border-red-900/50 hover:border-red-700 rounded transition-colors"
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
              {allDeviceProbes.length === 0 && (
                <tr>
                  <td colSpan={10} className="px-4 py-8 text-center text-gray-500">
                    No device probes configured. Click "Add Probe" to create one on a device.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Server probe modal */}
      {editingServer && (
        <ProbeModal
          editing={editingServer}
          isNew={isNewServer}
          saving={serverSaveMutation.isPending}
          onClose={() => setEditingServer(null)}
          onChange={setEditingServer}
          onSave={() => serverSaveMutation.mutate(editingServer)}
        />
      )}

      {/* Device probe edit modal (edit only — create navigates to device page) */}
      {editingDevice && (
        <DeviceProbeModal
          editing={editingDevice}
          isNew={false}
          saving={deviceSaveMutation.isPending}
          onClose={() => setEditingDevice(null)}
          onChange={setEditingDevice}
          onSave={() => deviceSaveMutation.mutate(editingDevice)}
        />
      )}
    </div>
  )
}

import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { DeviceProbeWithResult, DeviceProbeResult } from '../types/models'
import DeviceProbeModal, { emptyDeviceProbe, type DeviceProbeForm } from '../components/DeviceProbeModal'

export default function DeviceProbes() {
  const { id } = useParams<{ id: string }>()
  const qc = useQueryClient()

  const { data: deviceData } = useQuery({
    queryKey: ['device', id],
    queryFn: () => api.getDevice(id!),
    enabled: !!id,
  })

  const { data: probes = [], isLoading } = useQuery({
    queryKey: ['device-probes', id],
    queryFn: () => api.getDeviceProbes(id!),
    enabled: !!id,
    refetchInterval: 30_000,
  })

  const [editing, setEditing] = useState<DeviceProbeForm | null>(null)
  const [isNew, setIsNew] = useState(false)
  const [viewingResults, setViewingResults] = useState<number | null>(null)

  const { data: probeResults = [] } = useQuery({
    queryKey: ['device-probe-results', id, viewingResults],
    queryFn: () => api.getDeviceProbeResults(id!, viewingResults!, 50),
    enabled: !!id && viewingResults != null,
  })

  const runMutation = useMutation({
    mutationFn: (probeId: number) => api.runDeviceProbe(id!, probeId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['device-probes', id] }),
  })

  const saveMutation = useMutation({
    mutationFn: (probe: DeviceProbeForm) =>
      isNew ? api.createDeviceProbe(id!, probe) : api.updateDeviceProbe(id!, probe.id!, probe),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['device-probes', id] })
      setEditing(null)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (probeId: number) => api.deleteDeviceProbe(id!, probeId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['device-probes', id] }),
  })

  const toggleMutation = useMutation({
    mutationFn: (probe: DeviceProbeWithResult) =>
      api.updateDeviceProbe(id!, probe.id, { ...probe, enabled: !probe.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['device-probes', id] }),
  })

  function probeToForm(probe: DeviceProbeWithResult): DeviceProbeForm {
    return {
      id: probe.id,
      name: probe.name,
      type: probe.type,
      enabled: probe.enabled,
      interval_seconds: probe.interval_seconds,
      timeout_seconds: probe.timeout_seconds,
      config: { ...probe.config },
      assertions: [...(probe.assertions || [])],
    }
  }

  if (isLoading) return <div className="text-gray-500">Loading...</div>

  const device = deviceData?.device

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <Link to={`/devices/${id}`} className="text-sm text-gray-400 hover:text-white transition-colors">
          &larr; Back to <span className="uppercase">{device?.hostname ?? id}</span>
        </Link>
        <div className="flex items-center justify-between mt-2">
          <h1 className="text-2xl font-bold text-white">Device Probes</h1>
          <button
            onClick={() => { setEditing({ ...emptyDeviceProbe }); setIsNew(true) }}
            className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
          >
            Add Probe
          </button>
        </div>
      </div>

      {/* Probe List */}
      {probes.length === 0 ? (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-8 text-center text-gray-500">
          No device probes configured. Click "Add Probe" to start monitoring.
        </div>
      ) : (
        <div className="space-y-3">
          {probes.map(probe => (
            <div key={probe.id} className="bg-gray-900 border border-gray-800 rounded-lg p-4 overflow-hidden">
              <div className="flex flex-col sm:flex-row sm:items-center gap-3">
                <div className="flex items-center gap-3 min-w-0 flex-1">
                  {/* Status indicator */}
                  <span className={`w-2.5 h-2.5 rounded-full flex-shrink-0 ${
                    !probe.enabled ? 'bg-gray-600'
                    : probe.latest_result == null ? 'bg-gray-500'
                    : probe.latest_result.success ? 'bg-emerald-400'
                    : 'bg-red-400'
                  }`} />

                  {/* Name & type */}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-medium text-white">{probe.name}</span>
                      <span className="px-1.5 py-0.5 text-[10px] uppercase font-medium rounded bg-gray-800 text-gray-400 border border-gray-700">
                        {probe.type}
                      </span>
                      {!probe.enabled && (
                        <span className="text-xs text-gray-600">disabled</span>
                      )}
                    </div>
                    <div className="flex items-center gap-3 mt-0.5 text-xs text-gray-500 flex-wrap">
                      {probe.success_rate != null && (
                        <span className={probe.success_rate >= 95 ? 'text-emerald-400' : probe.success_rate >= 80 ? 'text-amber-400' : 'text-red-400'}>
                          {probe.success_rate.toFixed(1)}% success
                        </span>
                      )}
                      <span>{probe.total_checks} checks</span>
                      {probe.latest_result && (
                        <span>{probe.latest_result.latency_ms.toFixed(0)}ms</span>
                      )}
                      <span>every {probe.interval_seconds}s</span>
                    </div>
                  </div>
                </div>

                {/* Actions */}
                <div className="flex items-center gap-2 flex-shrink min-w-0 flex-wrap">
                  <button
                    onClick={() => setViewingResults(viewingResults === probe.id ? null : probe.id)}
                    className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 hover:border-gray-600 rounded transition-colors"
                  >
                    {viewingResults === probe.id ? 'Hide' : 'History'}
                  </button>
                  <button
                    onClick={() => runMutation.mutate(probe.id)}
                    disabled={runMutation.isPending}
                    className="px-2 py-1 text-xs text-blue-400 hover:text-blue-300 border border-blue-800 hover:border-blue-700 rounded transition-colors disabled:opacity-50"
                  >
                    Run
                  </button>
                  <button
                    onClick={() => toggleMutation.mutate(probe)}
                    className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 hover:border-gray-600 rounded transition-colors"
                  >
                    {probe.enabled ? 'Disable' : 'Enable'}
                  </button>
                  <button
                    onClick={() => { setEditing(probeToForm(probe)); setIsNew(false) }}
                    className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 hover:border-gray-600 rounded transition-colors"
                  >
                    Edit
                  </button>
                  <button
                    onClick={() => { if (confirm(`Delete probe "${probe.name}"?`)) deleteMutation.mutate(probe.id) }}
                    className="px-2 py-1 text-xs text-gray-500 hover:text-red-400 border border-gray-700 hover:border-red-800 rounded transition-colors"
                  >
                    Delete
                  </button>
                </div>
              </div>

              {/* Latest result error */}
              {probe.latest_result && !probe.latest_result.success && probe.latest_result.error_msg && (
                <div className="mt-2 px-3 py-1.5 bg-red-900/20 border border-red-800/30 rounded text-xs text-red-400">
                  {probe.latest_result.error_msg}
                </div>
              )}

              {/* Failed assertions */}
              {probe.latest_result?.failed_assertions && probe.latest_result.failed_assertions.length > 0 && (
                <div className="mt-2 px-3 py-1.5 bg-amber-900/20 border border-amber-800/30 rounded text-xs text-amber-400">
                  Failed: {probe.latest_result.failed_assertions.map(a => `${a.field} ${a.operator} ${a.value}`).join(', ')}
                </div>
              )}

              {/* Result History */}
              {viewingResults === probe.id && (
                <div className="mt-3 pt-3 border-t border-gray-800">
                  <h4 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Recent Results</h4>
                  {probeResults.length === 0 ? (
                    <p className="text-xs text-gray-600">No results yet.</p>
                  ) : (
                    <div className="overflow-x-auto thin-scrollbar">
                      <table className="w-full text-xs">
                        <thead>
                          <tr className="text-left text-gray-500 border-b border-gray-800">
                            <th className="py-1.5 pr-3 font-medium">Status</th>
                            <th className="py-1.5 pr-3 font-medium">Latency</th>
                            <th className="py-1.5 pr-3 font-medium">Time</th>
                            <th className="py-1.5 font-medium">Error</th>
                          </tr>
                        </thead>
                        <tbody>
                          {probeResults.map(r => (
                            <tr key={r.id} className="border-b border-gray-800/50">
                              <td className="py-1.5 pr-3">
                                <span className={`inline-block w-2 h-2 rounded-full ${r.success ? 'bg-emerald-400' : 'bg-red-400'}`} />
                              </td>
                              <td className="py-1.5 pr-3 font-mono text-gray-400">{r.latency_ms.toFixed(0)}ms</td>
                              <td className="py-1.5 pr-3 text-gray-400">{new Date(r.created_at).toLocaleString()}</td>
                              <td className="py-1.5 text-red-400 truncate max-w-xs">{r.error_msg || '-'}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Edit/Create Modal */}
      {editing && (
        <DeviceProbeModal
          editing={editing}
          isNew={isNew}
          saving={saveMutation.isPending}
          onClose={() => setEditing(null)}
          onChange={setEditing}
          onSave={() => saveMutation.mutate(editing)}
        />
      )}
    </div>
  )
}

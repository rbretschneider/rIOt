import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ProbeWithResult } from '../types/models'
import ProbeModal, { emptyProbe, probeToForm, getTarget, type ProbeForm } from '../components/ProbeModal'

export default function Probes() {
  const qc = useQueryClient()
  const { data: probes = [], isLoading } = useQuery({
    queryKey: ['probes'],
    queryFn: api.getProbes,
    refetchInterval: 30_000,
  })

  const [editing, setEditing] = useState<ProbeForm | null>(null)
  const [isNew, setIsNew] = useState(false)

  const runMutation = useMutation({
    mutationFn: api.runProbe,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  const saveMutation = useMutation({
    mutationFn: (probe: ProbeForm) =>
      isNew ? api.createProbe(probe) : api.updateProbe(probe.id!, probe),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['probes'] })
      setEditing(null)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteProbe(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  const toggleMutation = useMutation({
    mutationFn: (probe: ProbeWithResult) =>
      api.updateProbe(probe.id, { ...probe, enabled: !probe.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  if (isLoading) return <div className="text-gray-500">Loading...</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-white">Probes</h1>
        <button
          onClick={() => { setEditing({ ...emptyProbe }); setIsNew(true) }}
          className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
        >
          Add Probe
        </button>
      </div>

      {probes.length === 0 ? (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-8 text-center text-gray-500">
          No probes configured. Click "Add Probe" to start monitoring.
        </div>
      ) : (
        <div className="grid gap-3">
          {probes.map(probe => (
            <ProbeCard
              key={probe.id}
              probe={probe}
              onRun={() => runMutation.mutate(probe.id)}
              running={runMutation.isPending}
              onEdit={() => { setEditing(probeToForm(probe)); setIsNew(false) }}
              onDelete={() => { if (confirm('Delete this probe?')) deleteMutation.mutate(probe.id) }}
              onToggle={() => toggleMutation.mutate(probe)}
            />
          ))}
        </div>
      )}

      {editing && (
        <ProbeModal
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

function ProbeCard({ probe, onRun, running, onEdit, onDelete, onToggle }: {
  probe: ProbeWithResult
  onRun: () => void
  running: boolean
  onEdit: () => void
  onDelete: () => void
  onToggle: () => void
}) {
  const lr = probe.latest_result
  const status = lr ? (lr.success ? 'up' : 'down') : 'unknown'

  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 overflow-hidden">
      <div className="flex flex-col sm:flex-row sm:items-center gap-3">
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <button
            onClick={onToggle}
            className={`w-8 h-4 rounded-full transition-colors relative flex-shrink-0 ${
              probe.enabled ? 'bg-emerald-600' : 'bg-gray-600'
            }`}
            title={probe.enabled ? 'Disable' : 'Enable'}
          >
            <span className={`absolute top-0.5 w-3 h-3 rounded-full bg-white transition-transform ${
              probe.enabled ? 'left-4' : 'left-0.5'
            }`} />
          </button>
          <span className={`w-3 h-3 rounded-full flex-shrink-0 ${
            status === 'up' ? 'bg-emerald-500' : status === 'down' ? 'bg-red-500' : 'bg-gray-600'
          }`} />
          <div className="min-w-0">
            <Link to={`/probes/${probe.id}`} className="text-white font-medium hover:text-blue-400 transition-colors">
              {probe.name}
            </Link>
            <p className="text-xs text-gray-500 truncate">
              {probe.type.toUpperCase()} &middot; {getTarget(probe)} &middot; every {probe.interval_seconds}s
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3 flex-shrink-0 flex-wrap min-w-0">
          {probe.success_rate != null && (
            <div className="text-right hidden sm:block">
              <p className={`text-sm font-mono font-semibold ${
                probe.success_rate >= 0.95 ? 'text-emerald-400' :
                probe.success_rate >= 0.8 ? 'text-amber-400' : 'text-red-400'
              }`}>
                {(probe.success_rate * 100).toFixed(1)}%
              </p>
              <p className="text-xs text-gray-500">
                {probe.total_checks} checks
              </p>
            </div>
          )}
          {lr && (
            <div className="text-right hidden sm:block">
              <p className={`text-sm font-mono ${lr.success ? 'text-emerald-400' : 'text-red-400'}`}>
                {lr.latency_ms.toFixed(1)}ms
              </p>
              <p className="text-xs text-gray-500">
                {new Date(lr.created_at).toLocaleTimeString()}
              </p>
            </div>
          )}
          <button
            onClick={onRun}
            disabled={running}
            className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors disabled:opacity-50"
          >
            Run
          </button>
          <button
            onClick={onEdit}
            className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors"
          >
            Edit
          </button>
          <button
            onClick={onDelete}
            className="px-2 py-1 text-xs text-red-400/70 hover:text-red-400 border border-red-900/50 hover:border-red-700 rounded transition-colors"
          >
            Delete
          </button>
        </div>
      </div>
      {lr && !lr.success && lr.error_msg && (
        <p className="mt-2 text-xs text-red-400 ml-14">{lr.error_msg}</p>
      )}
    </div>
  )
}

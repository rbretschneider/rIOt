import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import ProbeModal, { probeToForm, type ProbeForm } from '../components/ProbeModal'

export default function ProbeDetail() {
  const { id } = useParams<{ id: string }>()
  const probeId = Number(id)
  const qc = useQueryClient()

  const { data: probes } = useQuery({
    queryKey: ['probes'],
    queryFn: api.getProbes,
  })
  const probe = probes?.find(p => p.id === probeId)

  const { data: results = [] } = useQuery({
    queryKey: ['probe-results', probeId],
    queryFn: () => api.getProbeResults(probeId, 100),
    refetchInterval: 30_000,
    enabled: !!probeId,
  })

  const runMutation = useMutation({
    mutationFn: () => api.runProbe(probeId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['probe-results', probeId] })
      qc.invalidateQueries({ queryKey: ['probes'] })
    },
  })

  const saveMutation = useMutation({
    mutationFn: (form: ProbeForm) => api.updateProbe(form.id!, form),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['probes'] })
      setEditing(null)
    },
  })

  const [editing, setEditing] = useState<ProbeForm | null>(null)

  if (!probe) return <div className="text-gray-500">Probe not found</div>

  const successCount = results.filter(r => r.success).length
  const successRate = results.length > 0 ? ((successCount / results.length) * 100).toFixed(1) : '-'
  const avgLatency = results.length > 0
    ? (results.reduce((sum, r) => sum + r.latency_ms, 0) / results.length).toFixed(1)
    : '-'

  return (
    <div className="space-y-6">
      <div>
        <Link to="/probes" className="text-sm text-gray-400 hover:text-white transition-colors">
          &larr; Back to Probes
        </Link>
        <div className="flex flex-col sm:flex-row sm:items-center justify-between mt-2 gap-2">
          <div>
            <h1 className="text-2xl font-bold text-white">{probe.name}</h1>
            <p className="text-sm text-gray-500">
              {probe.type.toUpperCase()} &middot; every {probe.interval_seconds}s &middot; timeout {probe.timeout_seconds}s
              {!probe.enabled && <span className="ml-2 text-amber-500">(disabled)</span>}
            </p>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            <button
              onClick={() => setEditing(probeToForm(probe))}
              className="px-3 py-1.5 text-sm text-gray-400 hover:text-white border border-gray-700 rounded-md transition-colors"
            >
              Edit
            </button>
            <button
              onClick={() => runMutation.mutate()}
              disabled={runMutation.isPending}
              className="px-3 py-1.5 text-sm text-gray-400 hover:text-white border border-gray-700 rounded-md transition-colors disabled:opacity-50"
            >
              {runMutation.isPending ? 'Running...' : 'Run Now'}
            </button>
          </div>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <p className="text-xs text-gray-500 uppercase">Success Rate</p>
          <p className="text-2xl font-bold text-white">{successRate}%</p>
        </div>
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <p className="text-xs text-gray-500 uppercase">Avg Latency</p>
          <p className="text-2xl font-bold text-white">{avgLatency}ms</p>
        </div>
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <p className="text-xs text-gray-500 uppercase">Checks</p>
          <p className="text-2xl font-bold text-white">{results.length}</p>
        </div>
      </div>

      {/* Results History */}
      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
        <div className="px-4 py-3 border-b border-gray-800">
          <h2 className="text-sm font-semibold text-gray-300 uppercase">Recent Results</h2>
        </div>
        <div className="max-h-96 overflow-auto scrollbar-thin">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs uppercase">
                <th className="text-left px-4 py-2">Status</th>
                <th className="text-right px-4 py-2">Latency</th>
                <th className="text-left px-4 py-2 hidden sm:table-cell">Error</th>
                <th className="text-right px-4 py-2">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800/50">
              {results.map(r => (
                <tr key={r.id}>
                  <td className="px-4 py-2">
                    <span className={`inline-block w-2 h-2 rounded-full ${r.success ? 'bg-emerald-500' : 'bg-red-500'}`} />
                  </td>
                  <td className="px-4 py-2 text-right font-mono text-gray-300">
                    {r.latency_ms.toFixed(1)}ms
                  </td>
                  <td className="px-4 py-2 text-gray-500 text-xs truncate max-w-xs hidden sm:table-cell">
                    {r.error_msg || '-'}
                  </td>
                  <td className="px-4 py-2 text-right text-gray-500 text-xs whitespace-nowrap">
                    {new Date(r.created_at).toLocaleString()}
                  </td>
                </tr>
              ))}
              {results.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-8 text-center text-gray-500">
                    No results yet. Click "Run Now" to execute the probe.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {editing && (
        <ProbeModal
          editing={editing}
          isNew={false}
          saving={saveMutation.isPending}
          onClose={() => setEditing(null)}
          onChange={setEditing}
          onSave={() => saveMutation.mutate(editing)}
        />
      )}
    </div>
  )
}

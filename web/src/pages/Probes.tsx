import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ProbeWithResult } from '../types/models'

export default function Probes() {
  const qc = useQueryClient()
  const { data: probes = [], isLoading } = useQuery({
    queryKey: ['probes'],
    queryFn: api.getProbes,
    refetchInterval: 30_000,
  })

  const runMutation = useMutation({
    mutationFn: api.runProbe,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  if (isLoading) return <div className="text-gray-500">Loading...</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-white">Probes</h1>
        <Link
          to="/settings/probes"
          className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
        >
          Manage Probes
        </Link>
      </div>

      {probes.length === 0 ? (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-8 text-center text-gray-500">
          No probes configured. <Link to="/settings/probes" className="text-blue-400 hover:underline">Create one</Link> to start monitoring.
        </div>
      ) : (
        <div className="grid gap-3">
          {probes.map(probe => (
            <ProbeCard key={probe.id} probe={probe} onRun={() => runMutation.mutate(probe.id)} running={runMutation.isPending} />
          ))}
        </div>
      )}
    </div>
  )
}

function ProbeCard({ probe, onRun, running }: { probe: ProbeWithResult; onRun: () => void; running: boolean }) {
  const lr = probe.latest_result
  const status = lr ? (lr.success ? 'up' : 'down') : 'unknown'

  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className={`w-3 h-3 rounded-full ${
            status === 'up' ? 'bg-emerald-500' : status === 'down' ? 'bg-red-500' : 'bg-gray-600'
          }`} />
          <div>
            <Link to={`/probes/${probe.id}`} className="text-white font-medium hover:text-blue-400 transition-colors">
              {probe.name}
            </Link>
            <p className="text-xs text-gray-500">
              {probe.type.toUpperCase()} &middot; every {probe.interval_seconds}s
              {!probe.enabled && <span className="ml-2 text-amber-500">(disabled)</span>}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {lr && (
            <div className="text-right">
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
            Run Now
          </button>
        </div>
      </div>
      {lr && !lr.success && lr.error_msg && (
        <p className="mt-2 text-xs text-red-400">{lr.error_msg}</p>
      )}
    </div>
  )
}

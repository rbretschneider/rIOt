import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'

export default function AgentManagement() {
  const qc = useQueryClient()
  const { data: versions = [], isLoading } = useQuery({
    queryKey: ['agent-versions'],
    queryFn: api.getAgentVersions,
    refetchInterval: 30_000,
  })
  const { data: serverUpdate } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    staleTime: 60 * 60 * 1000,
  })

  const [updateResult, setUpdateResult] = useState<{ sent: number; skipped: number } | null>(null)

  const bulkMutation = useMutation({
    mutationFn: api.bulkUpdateAgents,
    onSuccess: (data) => {
      setUpdateResult(data)
      qc.invalidateQueries({ queryKey: ['agent-versions'] })
    },
  })

  const latestVersion = serverUpdate?.latest_version
  const totalDevices = versions.reduce((sum, v) => sum + v.count, 0)

  if (isLoading) return <div className="text-gray-400">Loading...</div>

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-white">Agent Version Management</h2>

      {/* Version Distribution */}
      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
        <div className="px-4 py-3 border-b border-gray-800">
          <h3 className="text-sm font-semibold text-gray-300 uppercase">Version Distribution</h3>
        </div>
        <div className="p-4 space-y-3">
          {versions.length === 0 ? (
            <p className="text-gray-500 text-sm">No devices registered.</p>
          ) : (
            versions.map(v => {
              const pct = totalDevices > 0 ? (v.count / totalDevices) * 100 : 0
              const isLatest = latestVersion && v.version === latestVersion
              const isOutdated = latestVersion && v.version !== latestVersion && v.version !== 'dev' && v.version !== 'unknown'
              return (
                <div key={v.version}>
                  <div className="flex items-center justify-between mb-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-mono text-white">{v.version}</span>
                      {isLatest && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-emerald-900/50 text-emerald-400">latest</span>
                      )}
                      {isOutdated && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-amber-900/50 text-amber-400">outdated</span>
                      )}
                    </div>
                    <div className="flex items-center gap-3">
                      <span className="text-sm text-gray-400">
                        {v.count} device{v.count !== 1 ? 's' : ''}
                      </span>
                      {isOutdated && (
                        <button
                          onClick={() => {
                            if (confirm(`Send update command to all ${v.count} device(s) running ${v.version}?`))
                              bulkMutation.mutate(v.version)
                          }}
                          disabled={bulkMutation.isPending}
                          className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors disabled:opacity-50"
                        >
                          {bulkMutation.isPending ? 'Updating...' : 'Update All'}
                        </button>
                      )}
                    </div>
                  </div>
                  <div className="w-full bg-gray-800 rounded-full h-2">
                    <div
                      className={`h-2 rounded-full transition-all ${
                        isLatest ? 'bg-emerald-500' : isOutdated ? 'bg-amber-500' : 'bg-gray-600'
                      }`}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                </div>
              )
            })
          )}
        </div>
      </div>

      {/* Bulk Update Result */}
      {updateResult && (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <p className="text-sm text-gray-300">
            Update command sent to <span className="text-white font-medium">{updateResult.sent}</span> device{updateResult.sent !== 1 ? 's' : ''}.
            {updateResult.skipped > 0 && (
              <span className="text-gray-500"> ({updateResult.skipped} skipped — offline or disconnected)</span>
            )}
          </p>
        </div>
      )}

      {/* Info */}
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <p className="text-sm text-gray-400">
          {latestVersion ? (
            <>Latest release: <span className="text-white font-mono">{latestVersion}</span></>
          ) : (
            'Unable to determine latest agent version.'
          )}
        </p>
        <p className="text-xs text-gray-500 mt-2">
          Agents check for updates at startup and can be updated via the "Update All" button or individually from the device detail page.
        </p>
      </div>
    </div>
  )
}

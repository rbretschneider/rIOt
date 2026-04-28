import { useState, useMemo } from 'react'
import type { FleetContainerRow } from '../../types/models'

type SortMode = 'cpu' | 'memory' | 'restarts' | 'updates'

interface Props {
  containers: FleetContainerRow[]
  isLoading?: boolean
}

const SORT_LABELS: Record<SortMode, string> = {
  cpu: 'Top CPU',
  memory: 'Top RAM',
  restarts: 'Restarts',
  updates: 'Updates',
}

/**
 * AD-012: Projects to a strict allowlist of FleetContainerRow fields. The
 * Labels map is never referenced; `stack` is a server-derived plain string. */
export default function ContainerLeaderboard({ containers, isLoading }: Props) {
  const [sortMode, setSortMode] = useState<SortMode>('cpu')

  const visibleRows = useMemo((): FleetContainerRow[] => {
    const sorted = [...containers].sort((a, b) => {
      switch (sortMode) {
        case 'cpu':      return b.cpu_percent - a.cpu_percent
        case 'memory':   return b.mem_usage - a.mem_usage
        case 'restarts': return b.restart_count - a.restart_count
        case 'updates':  return (b.update_available ? 1 : 0) - (a.update_available ? 1 : 0)
      }
    })
    return sorted.slice(0, 10)
  }, [containers, sortMode])

  if (!isLoading && containers.length === 0) {
    return (
      <div
        className="bg-gray-900/60 border border-gray-800 rounded-xl p-8 text-center"
        data-testid="leaderboard-empty-state"
        role="status"
        aria-label="No containers"
      >
        <p className="text-gray-400 mb-1">No containers reported</p>
        <p className="text-sm text-gray-600">Devices running Docker containers will appear here.</p>
      </div>
    )
  }

  return (
    <div
      className="bg-gray-900/60 border border-gray-800 rounded-xl p-4"
      data-testid="container-leaderboard"
      role="region"
      aria-label="Container leaderboard"
    >
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <span className="text-xs uppercase tracking-wide text-gray-500 mr-1">Sort by</span>
        {(['cpu', 'memory', 'restarts', 'updates'] as SortMode[]).map(mode => (
          <button
            key={mode}
            onClick={() => setSortMode(mode)}
            className={`px-2.5 py-1 rounded text-xs font-medium transition-colors ${
              sortMode === mode
                ? 'bg-emerald-700 text-white'
                : 'bg-gray-800 text-gray-400 hover:text-white'
            }`}
            aria-pressed={sortMode === mode}
            data-testid={`sort-${mode}`}
          >
            {SORT_LABELS[mode]}
          </button>
        ))}
      </div>

      <div className="overflow-x-auto scrollbar-thin" data-testid="leaderboard-table-wrapper">
        <table className="w-full text-sm" aria-label={`Container leaderboard sorted by ${SORT_LABELS[sortMode]}`}>
          <thead>
            <tr className="text-left text-xs text-gray-500 border-b border-gray-800">
              <th scope="col" className="pb-2 pr-3 font-medium">Device</th>
              <th scope="col" className="pb-2 pr-3 font-medium">Container</th>
              <th scope="col" className="pb-2 pr-3 font-medium">Stack</th>
              <th scope="col" className="pb-2 pr-3 font-medium text-right">CPU</th>
              <th scope="col" className="pb-2 pr-3 font-medium text-right">RAM</th>
              <th scope="col" className="pb-2 pr-3 font-medium text-right">Restarts</th>
              <th scope="col" className="pb-2 font-medium">Updates</th>
            </tr>
          </thead>
          <tbody>
            {visibleRows.map((c, i) => (
              <tr
                key={`${c.device_id}-${c.container_id ?? c.container_name}-${i}`}
                className="border-b border-gray-800/40 last:border-0"
                data-testid={`leaderboard-row-${c.container_name}`}
              >
                <td className="py-2 pr-3 text-gray-300 truncate max-w-[140px]">{c.hostname}</td>
                <td className="py-2 pr-3 text-white font-medium truncate max-w-[180px]">
                  <span className={`inline-block w-2 h-2 rounded-full mr-1.5 align-middle ${c.state === 'running' ? 'bg-emerald-400' : 'bg-gray-600'}`} aria-label={c.state} />
                  {c.container_name}
                </td>
                <td className="py-2 pr-3 text-gray-400 truncate max-w-[140px]">{c.stack || '—'}</td>
                <td className="py-2 pr-3 text-gray-200 tabular-nums text-right">{c.cpu_percent.toFixed(1)}%</td>
                <td className="py-2 pr-3 text-gray-200 tabular-nums text-right">{(c.mem_usage / 1024 / 1024).toFixed(0)} MiB</td>
                <td className="py-2 pr-3 text-gray-200 tabular-nums text-right">{c.restart_count}</td>
                <td className="py-2">
                  {c.update_available ? (
                    <span className="text-amber-400 text-xs font-medium" aria-label="Update available">●</span>
                  ) : (
                    <span className="text-gray-700">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

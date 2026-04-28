import { useState, useMemo } from 'react'
import Sparkline from '../../components/Sparkline'
import type { FleetContainerRow } from '../../types/models'

type SortMode = 'cpu' | 'memory' | 'restarts' | 'updates'

interface Props {
  containers: FleetContainerRow[]
  isLoading?: boolean
}

/**
 * AD-012: This component projects to a strict allowlist of FleetContainerRow fields.
 * The Labels map is NOT referenced here except indirectly via the `stack` field
 * which was already extracted server-side. The `stack` field in FleetContainerRow
 * is a plain string derived from com.docker.compose.project on the server.
 * No other label key flows into this component.
 *
 * FR-040 / FR-041 / FR-042 / FR-043 / FR-044 / FR-045 / AC-019 / AC-020 / AC-021 / AC-022
 */
export default function ContainerLeaderboard({ containers, isLoading }: Props) {
  const [sortMode, setSortMode] = useState<SortMode>('cpu')
  const [activeFilters, setActiveFilters] = useState<Set<string>>(new Set())

  // Derive all stacks present in the fleet for per-stack chips (FR-044)
  const allStacks = useMemo(() => {
    const s = new Set<string>()
    for (const c of containers) {
      if (c.stack) s.add(c.stack)
    }
    return [...s].sort()
  }, [containers])

  // Apply filter chips then sort, then cap at top 10 (FR-042)
  const visibleRows = useMemo((): FleetContainerRow[] => {
    let filtered = containers

    if (activeFilters.size > 0) {
      filtered = containers.filter(c => {
        if (activeFilters.has('updates') && c.update_available) return true
        if (activeFilters.has('restarts') && c.restart_count > 3) return true
        // Stack chips: check if the container's stack is in the active filters
        if (c.stack && activeFilters.has(`stack:${c.stack}`)) return true
        return false
      })
    }

    const sorted = [...filtered].sort((a, b) => {
      switch (sortMode) {
        case 'cpu':
          return b.cpu_percent - a.cpu_percent
        case 'memory':
          return b.mem_usage - a.mem_usage
        case 'restarts':
          // FR-041 / BR-002: v1 approximation — restart_count > 3
          return b.restart_count - a.restart_count
        case 'updates':
          return (b.update_available ? 1 : 0) - (a.update_available ? 1 : 0)
        default:
          return 0
      }
    })

    return sorted.slice(0, 10)
  }, [containers, sortMode, activeFilters])

  function toggleFilter(key: string) {
    setActiveFilters(prev => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  function sortLabel(mode: SortMode): string {
    const labels: Record<SortMode, string> = {
      cpu: 'Top CPU',
      memory: 'Top Memory',
      restarts: 'Restart anomalies',
      updates: 'Update available',
    }
    return labels[mode]
  }

  function metricValue(c: FleetContainerRow): string {
    switch (sortMode) {
      case 'cpu': return `${c.cpu_percent.toFixed(1)}%`
      case 'memory': return `${(c.mem_usage / 1024 / 1024).toFixed(0)} MiB`
      case 'restarts': return `${c.restart_count} restarts`
      case 'updates': return c.update_available ? 'Update available' : '—'
      default: return ''
    }
  }

  function sparklineData(c: FleetContainerRow): number[] {
    // For the leaderboard we show the current value as a flat sparkline.
    // Full sparkline data is only fetched for the per-container metrics endpoint
    // (lazy per visible row as described in AD-006). For now, single-point sparkline.
    switch (sortMode) {
      case 'cpu': return [c.cpu_percent]
      case 'memory': return [c.mem_usage / 1024 / 1024]
      default: return [0]
    }
  }

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
      <div className="flex flex-wrap items-center gap-2 mb-3">
        <span className="text-sm font-medium text-gray-300">Sort:</span>
        {(['cpu', 'memory', 'restarts', 'updates'] as SortMode[]).map(mode => (
          <button
            key={mode}
            onClick={() => setSortMode(mode)}
            className={`px-2 py-0.5 rounded text-xs font-medium transition-colors ${
              sortMode === mode
                ? 'bg-emerald-700 text-white'
                : 'bg-gray-800 text-gray-400 hover:text-white'
            }`}
            aria-pressed={sortMode === mode}
            aria-label={sortLabel(mode)}
            data-testid={`sort-${mode}`}
          >
            {sortLabel(mode)}
          </button>
        ))}

        {/* Filter chips (FR-044) */}
        <span className="text-sm font-medium text-gray-300 ml-2">Filter:</span>
        {(['updates', 'restarts'] as const).map(key => (
          <button
            key={key}
            onClick={() => toggleFilter(key)}
            className={`px-2 py-0.5 rounded text-xs font-medium transition-colors ${
              activeFilters.has(key)
                ? 'bg-blue-700 text-white'
                : 'bg-gray-800 text-gray-400 hover:text-white'
            }`}
            aria-pressed={activeFilters.has(key)}
            aria-label={key === 'updates' ? 'With updates' : 'Restarted recently'}
            data-testid={`filter-${key}`}
          >
            {key === 'updates' ? 'With updates' : 'Restarted recently'}
          </button>
        ))}
        {allStacks.map(stack => (
          <button
            key={`stack:${stack}`}
            onClick={() => toggleFilter(`stack:${stack}`)}
            className={`px-2 py-0.5 rounded text-xs font-medium transition-colors ${
              activeFilters.has(`stack:${stack}`)
                ? 'bg-violet-700 text-white'
                : 'bg-gray-800 text-gray-400 hover:text-white'
            }`}
            aria-pressed={activeFilters.has(`stack:${stack}`)}
            aria-label={`Stack: ${stack}`}
            data-testid={`filter-stack-${stack}`}
          >
            {stack}
          </button>
        ))}
      </div>

      <div className="overflow-x-auto scrollbar-thin" data-testid="leaderboard-table-wrapper">
        <table className="w-full text-sm" aria-label={`Container leaderboard sorted by ${sortLabel(sortMode)}`}>
          <thead>
            <tr className="text-left text-xs text-gray-500 border-b border-gray-800">
              <th scope="col" className="pb-1 pr-3 font-medium">Device</th>
              <th scope="col" className="pb-1 pr-3 font-medium">Container</th>
              <th scope="col" className="pb-1 pr-3 font-medium">Stack</th>
              <th scope="col" className="pb-1 pr-3 font-medium">{sortLabel(sortMode)}</th>
              <th scope="col" className="pb-1 font-medium">Trend</th>
            </tr>
          </thead>
          <tbody>
            {visibleRows.map((c, i) => (
              <tr
                key={`${c.device_id}-${c.container_id ?? c.container_name}-${i}`}
                className="border-b border-gray-800/50 last:border-0"
                data-testid={`leaderboard-row-${c.container_name}`}
              >
                <td className="py-1.5 pr-3 text-gray-300 truncate max-w-[120px]">{c.hostname}</td>
                <td className="py-1.5 pr-3 text-white font-medium truncate max-w-[120px]">
                  <span className={`inline-block w-2 h-2 rounded-full mr-1.5 ${c.state === 'running' ? 'bg-emerald-400' : 'bg-gray-600'}`} aria-label={c.state} />
                  {c.container_name}
                </td>
                <td className="py-1.5 pr-3 text-gray-400 truncate max-w-[100px]">{c.stack || '—'}</td>
                <td className="py-1.5 pr-3 text-gray-200 tabular-nums">{metricValue(c)}</td>
                <td className="py-1.5 w-16">
                  <Sparkline data={sparklineData(c)} color="#10b981" height={20} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

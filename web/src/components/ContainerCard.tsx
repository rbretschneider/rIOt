import type { ContainerInfo } from '../types/models'
import { displayName, formatContainerUptime, formatBytes } from '../utils/docker'

interface Props {
  container: ContainerInfo
  onClick: (c: ContainerInfo) => void
  autoUpdate?: boolean
  selected?: boolean
  onSelect?: (c: ContainerInfo, selected: boolean) => void
  networkParent?: string
}

function barColor(pct: number): string {
  if (pct > 90) return 'bg-red-500'
  if (pct > 75) return 'bg-amber-500'
  return 'bg-emerald-500'
}

function statusDotColor(state: string): string {
  switch (state) {
    case 'running': return 'bg-emerald-400'
    case 'paused': return 'bg-amber-400'
    default: return 'bg-red-400'
  }
}

export default function ContainerCard({ container: c, onClick, autoUpdate, selected, onSelect, networkParent }: Props) {
  const name = displayName(c.riot, c.name)
  const memPct = c.mem_limit > 0 ? (c.mem_usage / c.mem_limit) * 100 : 0
  const isRunning = c.state === 'running'

  return (
    <div
      onClick={() => onClick(c)}
      className="border border-gray-700/40 rounded bg-gray-800/30 hover:bg-gray-700/30 px-3 py-2 cursor-pointer transition-colors"
    >
      {/* Header row: checkbox + icon + name + stats + status dot */}
      <div className="flex items-center gap-2 min-w-0">
        {onSelect && (
          <input
            type="checkbox"
            checked={selected ?? false}
            onChange={e => onSelect(c, e.target.checked)}
            onClick={e => e.stopPropagation()}
            className="w-3.5 h-3.5 rounded border-gray-600 bg-gray-700 text-blue-500 focus:ring-0 flex-shrink-0 cursor-pointer"
          />
        )}
        {c.riot?.icon && <span className="text-sm flex-shrink-0">{c.riot.icon}</span>}
        <span className="text-sm font-medium text-white truncate" title={c.name}>{name}</span>
        {networkParent && (
          <span className="text-[10px] text-gray-500 flex-shrink-0" title={`Network via container:${networkParent}`}>
            via {networkParent}
          </span>
        )}
        <div className="flex-1" />
        {isRunning ? (
          <div className="flex items-center gap-2.5 flex-shrink-0 text-xs font-mono text-gray-400">
            <span className={c.cpu_percent > 90 ? 'text-red-400' : c.cpu_percent > 75 ? 'text-amber-400' : ''}>{c.cpu_percent.toFixed(0)}%</span>
            <span className={memPct > 90 ? 'text-red-400' : memPct > 75 ? 'text-amber-400' : ''}>{formatBytes(c.mem_usage)}</span>
          </div>
        ) : (
          <span className="text-xs text-gray-600 flex-shrink-0">{c.state}</span>
        )}
        {c.update_available && !autoUpdate && (
          <span className="w-2 h-2 rounded-full bg-amber-400 flex-shrink-0" title="Update available" />
        )}
        <span className={`w-2 h-2 rounded-full flex-shrink-0 ${statusDotColor(c.state)}`} title={c.state} />
      </div>
    </div>
  )
}

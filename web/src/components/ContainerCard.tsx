import type { ContainerInfo } from '../types/models'
import { displayName, formatContainerUptime, formatBytes } from '../utils/docker'

interface Props {
  container: ContainerInfo
  onClick: (c: ContainerInfo) => void
  autoUpdate?: boolean
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

export default function ContainerCard({ container: c, onClick, autoUpdate }: Props) {
  const name = displayName(c.riot, c.name)
  const memPct = c.mem_limit > 0 ? (c.mem_usage / c.mem_limit) * 100 : 0
  const isRunning = c.state === 'running'

  return (
    <div
      onClick={() => onClick(c)}
      className="border border-gray-700/40 rounded bg-gray-800/30 hover:bg-gray-700/30 px-2.5 py-1.5 cursor-pointer transition-colors"
    >
      {/* Header row: icon + name + stats + status dot */}
      <div className="flex items-center gap-1.5 min-w-0">
        {c.riot?.icon && <span className="text-xs flex-shrink-0">{c.riot.icon}</span>}
        <span className="text-xs font-medium text-white truncate" title={c.name}>{name}</span>
        <div className="flex-1" />
        {isRunning ? (
          <div className="flex items-center gap-2 flex-shrink-0 text-[10px] font-mono text-gray-400">
            <span className={c.cpu_percent > 90 ? 'text-red-400' : c.cpu_percent > 75 ? 'text-amber-400' : ''}>{c.cpu_percent.toFixed(0)}%</span>
            <span className={memPct > 90 ? 'text-red-400' : memPct > 75 ? 'text-amber-400' : ''}>{formatBytes(c.mem_usage)}</span>
          </div>
        ) : (
          <span className="text-[10px] text-gray-600 flex-shrink-0">{c.state}</span>
        )}
        {c.update_available && !autoUpdate && (
          <span className="w-1.5 h-1.5 rounded-full bg-amber-400 flex-shrink-0" title="Update available" />
        )}
        <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${statusDotColor(c.state)}`} title={c.state} />
      </div>
    </div>
  )
}

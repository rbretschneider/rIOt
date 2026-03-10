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
  const imageShort = c.image.split('/').pop() || c.image

  return (
    <div
      onClick={() => onClick(c)}
      className="border border-gray-700/50 rounded-lg bg-gray-800/40 hover:bg-gray-700/40 p-3 cursor-pointer transition-colors group flex flex-col h-[140px]"
    >
      {/* Header: icon + name + status dot */}
      <div className="flex items-center gap-2 mb-1 min-w-0">
        {c.riot?.icon && <span className="text-sm flex-shrink-0">{c.riot.icon}</span>}
        <span className="text-sm font-medium text-white truncate flex-1" title={c.name}>{name}</span>
        <span className={`w-2 h-2 rounded-full flex-shrink-0 ${statusDotColor(c.state)}`} title={c.state} />
      </div>

      {/* Image tag */}
      <p className="text-[11px] text-gray-600 truncate mb-2" title={c.image}>{imageShort}</p>

      {/* Separator */}
      <div className="border-t border-gray-700/40 mb-2" />

      {isRunning ? (
        <div className="flex-1 flex flex-col justify-between">
          {/* CPU bar */}
          <div className="flex items-center gap-2 text-[11px]">
            <span className="text-gray-500 w-7 flex-shrink-0">CPU</span>
            <div className="flex-1 h-1.5 bg-gray-700/60 rounded-full overflow-hidden">
              <div className={`h-full rounded-full ${barColor(c.cpu_percent)}`} style={{ width: `${Math.min(c.cpu_percent, 100)}%` }} />
            </div>
            <span className="text-gray-400 w-12 text-right font-mono">{c.cpu_percent.toFixed(1)}%</span>
          </div>

          {/* MEM bar */}
          <div className="flex items-center gap-2 text-[11px]">
            <span className="text-gray-500 w-7 flex-shrink-0">MEM</span>
            <div className="flex-1 h-1.5 bg-gray-700/60 rounded-full overflow-hidden">
              <div className={`h-full rounded-full ${barColor(memPct)}`} style={{ width: `${Math.min(memPct, 100)}%` }} />
            </div>
            <span className="text-gray-400 w-12 text-right font-mono">{memPct.toFixed(1)}%</span>
          </div>

          {/* Uptime + badges */}
          <div className="flex items-center gap-1.5 mt-1">
            <span className="text-[10px] text-gray-600">{formatContainerUptime(c.created)}</span>
            <div className="flex-1" />
            {c.update_available && !autoUpdate && (
              <span className="px-1 py-0.5 rounded text-[9px] font-semibold bg-amber-500/20 text-amber-400 border border-amber-500/30">
                Update
              </span>
            )}
          </div>
        </div>
      ) : (
        <div className="flex-1 flex flex-col justify-center">
          <span className="text-xs text-gray-500">{c.status}</span>
        </div>
      )}
    </div>
  )
}

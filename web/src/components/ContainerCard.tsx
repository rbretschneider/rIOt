import type { ContainerInfo } from '../types/models'
import { displayName, formatContainerUptime, formatBytes } from '../utils/docker'
import ContainerStatusBadge from './ContainerStatusBadge'

interface Props {
  container: ContainerInfo
  onClick: (c: ContainerInfo) => void
  autoUpdate?: boolean
  onAutoUpdateToggle?: (enabled: boolean) => void
}

export default function ContainerCard({ container: c, onClick, autoUpdate, onAutoUpdateToggle }: Props) {
  const name = displayName(c.riot, c.name)
  const memPct = c.mem_limit > 0 ? ((c.mem_usage / c.mem_limit) * 100).toFixed(1) : null

  return (
    <div
      onClick={() => onClick(c)}
      className="flex items-center gap-3 px-3 py-2 hover:bg-gray-700/30 rounded transition-colors cursor-pointer group"
    >
      {/* Icon + Name */}
      <div className="flex items-center gap-2 min-w-0 flex-1">
        {c.riot?.icon && <span className="text-sm flex-shrink-0">{c.riot.icon}</span>}
        <span className="text-sm font-medium text-white truncate">{name}</span>
        <span className="text-xs text-gray-600 truncate hidden sm:inline">{c.image.split('/').pop()}</span>
      </div>

      {/* Stats (running only) */}
      {c.state === 'running' && (
        <div className="hidden md:flex items-center gap-3 text-xs text-gray-500 flex-shrink-0">
          <span>{formatContainerUptime(c.created)}</span>
          {c.cpu_percent > 0 && <span>{c.cpu_percent.toFixed(1)}%</span>}
          {c.mem_usage > 0 && <span>{formatBytes(c.mem_usage)}{memPct ? ` (${memPct}%)` : ''}</span>}
        </div>
      )}
      {c.state === 'exited' && (
        <span className="text-xs text-red-400/70 flex-shrink-0 hidden md:inline">{c.status}</span>
      )}

      {/* Badges */}
      <div className="flex items-center gap-1.5 flex-shrink-0">
        {c.update_available && !autoUpdate && (
          <span className="px-1.5 py-0.5 rounded-full text-[10px] font-semibold bg-amber-500/20 text-amber-400 border border-amber-500/30">
            Update
          </span>
        )}
        <ContainerStatusBadge state={c.state} />
      </div>

      {/* Auto-update toggle */}
      {onAutoUpdateToggle && (
        <button
          onClick={e => { e.stopPropagation(); onAutoUpdateToggle(!autoUpdate) }}
          className={`flex-shrink-0 w-7 h-4 rounded-full transition-colors relative ${autoUpdate ? 'bg-emerald-500' : 'bg-gray-600'}`}
          title={autoUpdate ? 'Auto-update enabled' : 'Enable auto-update'}
        >
          <span className={`absolute top-0.5 w-3 h-3 rounded-full bg-white transition-transform ${autoUpdate ? 'left-3.5' : 'left-0.5'}`} />
        </button>
      )}
    </div>
  )
}

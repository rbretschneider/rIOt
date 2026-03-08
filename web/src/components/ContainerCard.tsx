import type { ContainerInfo } from '../types/models'
import { displayName, formatContainerUptime, formatPorts, formatBytes } from '../utils/docker'
import ContainerStatusBadge from './ContainerStatusBadge'

interface Props {
  container: ContainerInfo
  onClick: (c: ContainerInfo) => void
}

export default function ContainerCard({ container: c, onClick }: Props) {
  const name = displayName(c.riot, c.name)
  const ports = formatPorts(c)
  const memPct = c.mem_limit > 0 ? ((c.mem_usage / c.mem_limit) * 100).toFixed(1) : null

  return (
    <div
      onClick={() => onClick(c)}
      className="bg-gray-800/50 border border-gray-700/50 rounded-lg p-4 hover:border-gray-600 transition-colors cursor-pointer"
    >
      <div className="flex items-start justify-between gap-2 mb-2">
        <div className="flex items-center gap-2 min-w-0">
          {c.riot?.icon && <span className="text-lg flex-shrink-0">{c.riot.icon}</span>}
          <h3 className="text-sm font-medium text-white truncate">{name}</h3>
        </div>
        <div className="flex items-center gap-1.5">
          <ContainerStatusBadge state={c.state} />
          {c.update_available && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-semibold bg-amber-500/20 text-amber-400 border border-amber-500/30">
              Update
            </span>
          )}
        </div>
      </div>

      <p className="text-xs text-gray-500 truncate mb-3">{c.image}</p>

      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-400">
        {c.state === 'running' && (
          <>
            <span>Up {formatContainerUptime(c.created)}</span>
            {c.cpu_percent > 0 && <span>CPU {c.cpu_percent.toFixed(1)}%</span>}
            {c.mem_usage > 0 && (
              <span>Mem {formatBytes(c.mem_usage)}{memPct && ` (${memPct}%)`}</span>
            )}
          </>
        )}
        {c.state === 'exited' && <span className="text-red-400/70">{c.status}</span>}
      </div>

      {ports && (
        <div className="mt-2 text-xs font-mono text-gray-500 truncate">{ports}</div>
      )}

      {c.riot?.tags && c.riot.tags.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1">
          {c.riot.tags.map(tag => (
            <span key={tag} className="px-1.5 py-0.5 bg-gray-700/50 rounded text-xs text-gray-400">{tag}</span>
          ))}
        </div>
      )}
    </div>
  )
}

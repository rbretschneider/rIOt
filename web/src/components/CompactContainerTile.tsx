import type { ContainerInfo } from '../types/models'
import { displayName, formatBytes } from '../utils/docker'

interface Props {
  container: ContainerInfo
  onClick: (c: ContainerInfo) => void
  updating?: boolean
  crossStackParent?: string
}

function statusDotColor(state: string): string {
  switch (state) {
    case 'running': return 'bg-emerald-400'
    case 'paused': return 'bg-amber-400'
    default: return 'bg-red-400'
  }
}

export default function CompactContainerTile({ container: c, onClick, updating, crossStackParent }: Props) {
  const name = displayName(c.riot, c.name)
  const isRunning = c.state === 'running'
  const imageTag = c.image.includes(':') ? c.image.split(':').pop()! : 'latest'

  return (
    <div
      onClick={() => onClick(c)}
      className={`inline-flex flex-col px-2 py-1 rounded border cursor-pointer transition-colors
        ${c.update_available ? 'border-l-2 border-l-amber-400 border-y-gray-700/40 border-r-gray-700/40' : 'border-gray-700/40'}
        ${updating ? 'animate-pulse border-l-2 border-l-blue-400' : ''}
        ${isRunning ? 'bg-gray-800/30 hover:bg-gray-700/30' : 'bg-gray-800/20 opacity-60 hover:opacity-80'}
      `}
    >
      <div className="flex items-center gap-1.5 min-w-0">
        <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${statusDotColor(c.state)}`} />
        <span className="text-[11px] font-medium text-white truncate max-w-[140px]" title={c.name}>{name}</span>
        {c.update_available && <span className="w-1.5 h-1.5 rounded-full bg-amber-400 flex-shrink-0" title="Update available" />}
      </div>
      <div className="flex items-center gap-1.5 min-w-0">
        <span className="text-[9px] font-mono text-gray-600 truncate max-w-[80px]">{imageTag}</span>
        {isRunning && (
          <span className="text-[9px] font-mono text-gray-500">
            {c.cpu_percent.toFixed(0)}%
            <span className="text-gray-600 mx-0.5">/</span>
            {formatBytes(c.mem_usage)}
          </span>
        )}
        {crossStackParent && (
          <span className="text-[9px] text-blue-400/70 truncate" title={`Network via ${crossStackParent}`}>
            via {crossStackParent}
          </span>
        )}
      </div>
    </div>
  )
}

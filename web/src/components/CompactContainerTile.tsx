import type { ContainerInfo } from '../types/models'
import { displayName } from '../utils/docker'

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
      className={`min-w-[170px] px-2.5 py-1.5 rounded border cursor-pointer transition-colors
        ${c.update_available ? 'border-l-2 border-l-amber-400 border-y-gray-700/40 border-r-gray-700/40' : 'border-gray-700/40'}
        ${updating ? 'animate-pulse border-l-2 border-l-blue-400' : ''}
        ${isRunning ? 'bg-gray-800/30 hover:bg-gray-700/30' : 'bg-gray-800/20 opacity-60 hover:opacity-80'}
      `}
    >
      <div className="flex items-center gap-1.5 min-w-0">
        <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${statusDotColor(c.state)}`} />
        <span className="text-xs font-medium text-white truncate" title={c.name}>{name}</span>
      </div>
      <div className="flex items-center gap-1.5 mt-0.5 min-w-0">
        <span className="text-[10px] font-mono text-gray-500 truncate">{imageTag}</span>
        {c.update_available && <span className="w-1.5 h-1.5 rounded-full bg-amber-400 flex-shrink-0" title="Update available" />}
        {crossStackParent && (
          <span className="text-[9px] text-blue-400/70 truncate" title={`Network via ${crossStackParent}`}>
            via {crossStackParent}
          </span>
        )}
      </div>
    </div>
  )
}

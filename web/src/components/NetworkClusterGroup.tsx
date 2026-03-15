import { useState } from 'react'
import type { ContainerInfo } from '../types/models'
import type { NetworkCluster } from '../utils/docker'
import CompactContainerTile from './CompactContainerTile'
import UpdateConfirmModal from './UpdateConfirmModal'

interface Props {
  cluster: NetworkCluster
  onContainerClick: (c: ContainerInfo) => void
  onBulkUpdate: (containerIds: string[]) => void
  updating?: boolean
}

export default function NetworkClusterGroup({ cluster, onContainerClick, onBulkUpdate, updating }: Props) {
  const [showConfirm, setShowConfirm] = useState(false)
  const allContainers = [cluster.parent, ...cluster.dependents]
  const updatable = allContainers.filter(c => c.update_available)

  return (
    <div className="mt-3 mb-2">
      <div className="flex items-center gap-2 mb-1.5">
        <span className="text-[10px] font-semibold text-blue-400/60 uppercase tracking-wider">
          Network: {cluster.parent.name}
        </span>
        <div className="flex-1" />
        {updatable.length > 0 && (
          <button
            onClick={() => setShowConfirm(true)}
            disabled={updating}
            className="px-2 py-0.5 text-[10px] font-medium rounded bg-amber-500/15 text-amber-400 hover:bg-amber-500/25 transition-colors disabled:opacity-50"
          >
            {updating ? '...' : `Update Group (${updatable.length})`}
          </button>
        )}
      </div>

      {/* Network parent down warning */}
      {cluster.parent.state !== 'running' && cluster.dependents.length > 0 && (
        <div className="px-2 py-1 mb-1.5 bg-red-900/20 border border-red-800/30 rounded text-[10px] text-red-400">
          Network parent <span className="font-medium">{cluster.parent.name}</span> is {cluster.parent.state} — {cluster.dependents.length} dependent{cluster.dependents.length > 1 ? 's' : ''} may lose connectivity
        </div>
      )}

      {/* Parent tile */}
      <div className="border-l-2 border-blue-500/40 pl-2">
        <CompactContainerTile container={cluster.parent} onClick={onContainerClick} updating={updating} />
      </div>

      {/* Dependent tiles */}
      {cluster.dependents.length > 0 && (
        <div className="ml-6 mt-1 border-l border-gray-700/40 pl-2">
          <div className="flex flex-wrap gap-1.5">
            {cluster.dependents.map(c => (
              <CompactContainerTile
                key={c.id}
                container={c}
                onClick={onContainerClick}
                updating={updating}
                networkParentDown={cluster.parent.state !== 'running'}
              />
            ))}
          </div>
        </div>
      )}

      {showConfirm && (
        <UpdateConfirmModal
          title={`Update Network Group: ${cluster.parent.name}`}
          containers={updatable}
          networkOrder={updatable.length > 1 ? {
            parent: cluster.parent.name,
            dependents: cluster.dependents.filter(c => c.update_available).map(c => c.name),
          } : undefined}
          onConfirm={() => {
            onBulkUpdate(updatable.map(c => c.id))
            setShowConfirm(false)
          }}
          onCancel={() => setShowConfirm(false)}
        />
      )}
    </div>
  )
}

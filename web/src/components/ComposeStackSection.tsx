import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ContainerInfo, AutoUpdatePolicy } from '../types/models'
import type { ComposeStackGroup } from '../utils/docker'
import { getNetworkParent } from '../utils/docker'
import CompactContainerTile from './CompactContainerTile'
import NetworkClusterGroup from './NetworkClusterGroup'
import UpdateConfirmModal from './UpdateConfirmModal'

interface Props {
  stack: ComposeStackGroup
  deviceId: string
  onContainerClick: (c: ContainerInfo) => void
  allContainers: ContainerInfo[]
}

export default function ComposeStackSection({ stack, deviceId, onContainerClick, allContainers }: Props) {
  const [showStackConfirm, setShowStackConfirm] = useState(false)
  const queryClient = useQueryClient()

  const { data: policies } = useQuery({
    queryKey: ['auto-updates', deviceId],
    queryFn: () => api.getAutoUpdates(deviceId),
    enabled: !!deviceId,
  })

  const policyMap = useMemo(() => {
    const m = new Map<string, AutoUpdatePolicy>()
    for (const p of policies ?? []) m.set(p.target, p)
    return m
  }, [policies])

  const stackAutoUpdate = policyMap.get(stack.name)
  const isStackAutoUpdate = stackAutoUpdate?.enabled ?? false

  const stackMutation = useMutation({
    mutationFn: () => api.sendCommand(deviceId, 'docker_update', { compose_work_dir: stack.workDir }),
    onSuccess: () => { setTimeout(() => stackMutation.reset(), 5000) },
    onError: () => { setTimeout(() => stackMutation.reset(), 5000) },
  })

  const bulkUpdateMutation = useMutation({
    mutationFn: (containerIds: string[]) => api.bulkDockerUpdate(deviceId, containerIds),
    onSuccess: () => { setTimeout(() => bulkUpdateMutation.reset(), 5000) },
    onError: () => { setTimeout(() => bulkUpdateMutation.reset(), 5000) },
  })

  const autoUpdateMutation = useMutation({
    mutationFn: (enabled: boolean) =>
      api.setAutoUpdate(deviceId, stack.name, true, stack.workDir, enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['auto-updates', deviceId] }),
  })

  const runningCount = stack.containers.filter(c => c.state === 'running').length
  const updatable = stack.containers.filter(c => c.update_available)

  // Find cross-stack network parents for loose containers
  const crossStackMap = useMemo(() => {
    const map = new Map<string, string>()
    const stackNames = new Set(stack.containers.map(c => c.name))
    for (const c of stack.containers) {
      const parent = getNetworkParent(c)
      if (parent && !stackNames.has(parent)) {
        // Parent is in another stack — find its compose project
        const parentContainer = allContainers.find(ac => ac.name === parent)
        const parentProject = parentContainer?.labels?.['com.docker.compose.project']
        map.set(c.id, parentProject ? `${parent} (${parentProject})` : parent)
      }
    }
    return map
  }, [stack.containers, allContainers])

  return (
    <section className="border border-gray-700/30 rounded-lg bg-gray-900/30 p-4">
      {/* Stack Header */}
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        <svg className="w-4 h-4 text-gray-500 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
        </svg>
        <h2 className="text-sm font-semibold text-white uppercase tracking-wider">{stack.name}</h2>
        <span className="text-xs text-gray-600">{runningCount}/{stack.containers.length}</span>

        <div className="flex-1" />

        <button
          onClick={() => autoUpdateMutation.mutate(!isStackAutoUpdate)}
          className={`flex items-center gap-1.5 px-2 py-0.5 text-xs rounded transition-colors ${
            isStackAutoUpdate
              ? 'bg-emerald-500/15 text-emerald-400'
              : 'text-gray-600 hover:text-gray-400'
          }`}
          title={isStackAutoUpdate ? 'Auto-update enabled' : 'Enable auto-update'}
        >
          <span className={`w-1.5 h-1.5 rounded-full ${isStackAutoUpdate ? 'bg-emerald-400' : 'bg-gray-600'}`} />
          Auto
        </button>

        {updatable.length > 0 && (
          <button
            onClick={() => setShowStackConfirm(true)}
            disabled={stackMutation.isPending}
            className="px-2.5 py-0.5 text-xs font-medium rounded bg-amber-500/15 text-amber-400 hover:bg-amber-500/25 transition-colors disabled:opacity-50"
          >
            {stackMutation.isPending ? 'Updating...' : `Update Stack (${updatable.length})`}
          </button>
        )}
      </div>

      {stackMutation.isSuccess && (
        <div className="px-2 py-1 mb-2 bg-emerald-900/20 rounded text-xs text-emerald-400">Stack update command sent</div>
      )}
      {stackMutation.isError && (
        <div className="px-2 py-1 mb-2 bg-red-900/20 rounded text-xs text-red-400">{(stackMutation.error as Error).message}</div>
      )}
      {bulkUpdateMutation.isSuccess && (
        <div className="px-2 py-1 mb-2 bg-emerald-900/20 rounded text-xs text-emerald-400">Network group update sent</div>
      )}
      {bulkUpdateMutation.isError && (
        <div className="px-2 py-1 mb-2 bg-red-900/20 rounded text-xs text-red-400">{(bulkUpdateMutation.error as Error).message}</div>
      )}

      {/* Loose containers grid */}
      {stack.loose.length > 0 && (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(170px,1fr))] gap-1.5">
          {stack.loose.map(c => (
            <CompactContainerTile
              key={c.id}
              container={c}
              onClick={onContainerClick}
              crossStackParent={crossStackMap.get(c.id)}
            />
          ))}
        </div>
      )}

      {/* Network clusters */}
      {stack.networkClusters.map(cluster => (
        <NetworkClusterGroup
          key={cluster.parent.id}
          cluster={cluster}
          onContainerClick={onContainerClick}
          onBulkUpdate={ids => bulkUpdateMutation.mutate(ids)}
          updating={bulkUpdateMutation.isPending}
        />
      ))}

      {showStackConfirm && (
        <UpdateConfirmModal
          title={`Update Stack: ${stack.name}`}
          containers={updatable}
          onConfirm={() => {
            stackMutation.mutate()
            setShowStackConfirm(false)
          }}
          onCancel={() => setShowStackConfirm(false)}
        />
      )}
    </section>
  )
}

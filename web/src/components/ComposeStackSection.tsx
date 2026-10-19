import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ContainerInfo, AutoUpdatePolicy } from '../types/models'
import type { ComposeStackGroup } from '../utils/docker'
import { getNetworkParent, resolveNetworkParent } from '../utils/docker'
import { composeTargetKey, hasUpdateAvailable, isTerminal, normalizeStatus } from '../utils/commandLifecycle'
import { useCommandTracker } from '../hooks/useCommandTracker'
import CompactContainerTile from './CompactContainerTile'
import NetworkClusterGroup from './NetworkClusterGroup'
import UpdateConfirmModal from './UpdateConfirmModal'

function statusTone(status: string): string {
  const state = normalizeStatus(status)
  if (state === 'succeeded') return 'bg-emerald-900/20 text-emerald-400'
  if (state === 'failed') return 'bg-red-900/20 text-red-400'
  if (isTerminal(state)) return 'bg-amber-900/20 text-amber-400'
  return 'bg-blue-900/20 text-blue-300'
}

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

  // CMD-LIFECYCLE-001: a stack update and a network-group (bulk) update both
  // run against the stack's compose project, so one tracker follows whichever
  // command is in flight for it, by id, until it is terminal.
  const tracker = useCommandTracker(deviceId, stack.workDir ? composeTargetKey(stack.workDir) : null)
  const [skippedTargets, setSkippedTargets] = useState(0)

  const updateStack = () => {
    setSkippedTargets(0)
    void tracker.track(() => api.sendCommand(deviceId, 'docker_update', { compose_project: stack.name, compose_work_dir: stack.workDir }))
  }

  const updateGroup = (containerIds: string[]) => {
    setSkippedTargets(0)
    void tracker.track(() => api.bulkDockerUpdate(deviceId, containerIds)).then((created) => {
      setSkippedTargets(created?.conflicts?.length ?? 0)
    })
  }

  const autoUpdateMutation = useMutation({
    mutationFn: (enabled: boolean) =>
      api.setAutoUpdate(deviceId, stack.name, true, stack.workDir, enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['auto-updates', deviceId] }),
  })

  const runningCount = stack.containers.filter(c => c.state === 'running').length
  const updatable = stack.containers.filter(c => hasUpdateAvailable(c))

  // Find cross-stack network parents for loose containers
  const crossStackMap = useMemo(() => {
    const map = new Map<string, string>()
    const stackNames = new Set(stack.containers.map(c => c.name))
    for (const c of stack.containers) {
      const parentRef = getNetworkParent(c)
      if (!parentRef) continue
      // Resolve ID references to container names
      const parentName = resolveNetworkParent(parentRef, allContainers) ?? parentRef
      if (!stackNames.has(parentName)) {
        // Parent is in another stack — find its compose project
        const parentContainer = allContainers.find(ac => ac.name === parentName)
        const parentProject = parentContainer?.labels?.['com.docker.compose.project']
        map.set(c.id, parentProject ? `${parentName} (${parentProject})` : parentName)
      }
    }
    return map
  }, [stack.containers, allContainers])

  return (
    <section className="border border-gray-700/30 rounded-lg bg-gray-900/30 p-4">
      {/* Stack Header */}
      <div className="flex items-center gap-2 mb-3">
        <svg className="w-4 h-4 text-gray-500 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
        </svg>
        <h2 className="text-sm font-semibold text-white uppercase tracking-wider">{stack.name}</h2>
        <span className="text-xs text-gray-600">{runningCount}/{stack.containers.length}</span>

        <div className="flex-1" />

        {updatable.length > 0 && (
          <button
            onClick={() => setShowStackConfirm(true)}
            disabled={tracker.isBusy}
            className="px-2.5 py-0.5 text-xs font-medium rounded bg-amber-500/15 text-amber-400 hover:bg-amber-500/25 transition-colors disabled:opacity-50"
          >
            {tracker.isStarting ? 'Updating...' : `Update Stack (${updatable.length})`}
          </button>
        )}

        <button
          onClick={() => autoUpdateMutation.mutate(!isStackAutoUpdate)}
          className={`flex items-center gap-1.5 px-2 py-0.5 text-xs rounded transition-colors flex-shrink-0 ${
            isStackAutoUpdate
              ? 'bg-emerald-500/15 text-emerald-400'
              : 'text-gray-600 hover:text-gray-400'
          }`}
          title={isStackAutoUpdate ? 'Auto-update enabled' : 'Enable auto-update'}
        >
          <span className={`w-1.5 h-1.5 rounded-full ${isStackAutoUpdate ? 'bg-emerald-400' : 'bg-gray-600'}`} />
          Auto
        </button>
      </div>

      {/* Live state of the tracked command (FR-090, FR-095). The label can carry
          agent-originated failure text, so it is rendered as a text node only. */}
      {tracker.command && tracker.label && (
        <div
          role="status"
          aria-live="polite"
          data-testid="stack-command-status"
          className={`px-2 py-1 mb-2 rounded text-xs flex items-start gap-2 ${statusTone(tracker.command.status)}`}
        >
          <span className="flex-1 min-w-0 break-words whitespace-pre-wrap">{tracker.label}</span>
          {tracker.canCancel && (
            <button
              onClick={() => { void tracker.cancel() }}
              disabled={tracker.isCancelling}
              className="px-1.5 py-px rounded border border-current hover:bg-white/10 disabled:opacity-50 flex-shrink-0"
            >
              Cancel
            </button>
          )}
          {isTerminal(tracker.command.status) && normalizeStatus(tracker.command.status) !== 'succeeded' && (
            <button
              onClick={tracker.dismiss}
              className="px-1.5 py-px rounded border border-current hover:bg-white/10 flex-shrink-0"
            >
              Dismiss
            </button>
          )}
        </div>
      )}
      {tracker.error && (
        <div role="alert" className="px-2 py-1 mb-2 bg-red-900/20 rounded text-xs text-red-400">{tracker.error}</div>
      )}
      {skippedTargets > 0 && (
        <div className="px-2 py-1 mb-2 bg-amber-900/20 rounded text-xs text-amber-400">
          {skippedTargets} {skippedTargets === 1 ? 'target was' : 'targets were'} skipped: an update is already in progress
        </div>
      )}

      {/* Loose containers */}
      {stack.loose.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {stack.loose.map(c => (
            <CompactContainerTile
              key={c.id}
              container={c}
              onClick={onContainerClick}
              updating={tracker.isActive}
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
          onBulkUpdate={updateGroup}
          updating={tracker.isBusy}
        />
      ))}

      {showStackConfirm && (
        <UpdateConfirmModal
          title={`Update Stack: ${stack.name}`}
          containers={updatable}
          networkOrder={stack.networkClusters.length > 0 ? {
            parent: stack.networkClusters.map(nc => nc.parent.name).join(', '),
            dependents: stack.networkClusters.flatMap(nc => nc.dependents.map(d => d.name)),
          } : undefined}
          onConfirm={() => {
            updateStack()
            setShowStackConfirm(false)
          }}
          onCancel={() => setShowStackConfirm(false)}
        />
      )}
    </section>
  )
}

import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ContainerInfo, AutoUpdatePolicy } from '../types/models'
import type { ContainerGroup as ContainerGroupType } from '../utils/docker'
import { getNetworkParent } from '../utils/docker'
import ContainerCard from './ContainerCard'
import ConfirmModal from './ConfirmModal'

interface Props {
  group: ContainerGroupType
  onContainerClick: (c: ContainerInfo) => void
  deviceId?: string
  selectedIds?: Set<string>
  onSelectionChange?: (id: string, selected: boolean) => void
}

interface ComposeStack {
  workDir: string
  project: string
  updatableCount: number
}

export default function ContainerGroup({ group, onContainerClick, deviceId, selectedIds, onSelectionChange }: Props) {
  const [confirmStack, setConfirmStack] = useState<ComposeStack | null>(null)
  const queryClient = useQueryClient()

  // Fetch auto-update policies for this device
  const { data: policies } = useQuery({
    queryKey: ['auto-updates', deviceId],
    queryFn: () => api.getAutoUpdates(deviceId!),
    enabled: !!deviceId,
  })

  const policyMap = useMemo(() => {
    const m = new Map<string, AutoUpdatePolicy>()
    for (const p of policies ?? []) {
      m.set(p.target, p)
    }
    return m
  }, [policies])

  // Find compose stacks in this group that have updatable containers
  const composeStack = useMemo(() => {
    const stacks = new Map<string, ComposeStack>()
    for (const c of group.containers) {
      const workDir = c.labels?.['com.docker.compose.project.working_dir']
      const project = c.labels?.['com.docker.compose.project']
      if (!workDir || !project) continue
      const existing = stacks.get(workDir)
      if (existing) {
        if (c.update_available) existing.updatableCount++
      } else {
        stacks.set(workDir, { workDir, project, updatableCount: c.update_available ? 1 : 0 })
      }
    }
    return Array.from(stacks.values())
  }, [group.containers])

  const updatableStacks = composeStack.filter(s => s.updatableCount > 0)

  // Build network parent map for this group
  const networkParentMap = useMemo(() => {
    const map = new Map<string, string>()
    for (const c of group.containers) {
      const parent = getNetworkParent(c)
      if (parent) map.set(c.id, parent)
    }
    return map
  }, [group.containers])

  const stackMutation = useMutation({
    mutationFn: ({ workDir }: { workDir: string }) =>
      api.sendCommand(deviceId!, 'docker_update', { compose_work_dir: workDir }),
    onSuccess: () => { setTimeout(() => stackMutation.reset(), 5000) },
    onError: () => { setTimeout(() => stackMutation.reset(), 5000) },
  })

  const autoUpdateMutation = useMutation({
    mutationFn: (args: { target: string; isStack: boolean; composeWorkDir: string; enabled: boolean }) =>
      api.setAutoUpdate(deviceId!, args.target, args.isStack, args.composeWorkDir, args.enabled),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['auto-updates', deviceId] }),
  })

  // Check if this group is a compose stack and has a stack-level auto-update
  const stackInfo = composeStack.length === 1 ? composeStack[0] : null
  const stackAutoUpdate = stackInfo ? policyMap.get(stackInfo.project) : null
  const isStackAutoUpdate = stackAutoUpdate?.enabled ?? false

  const runningCount = group.containers.filter(c => c.state === 'running').length

  // Updatable containers in this group
  const updatableInGroup = group.containers.filter(c => c.update_available)
  const allUpdatableSelected = updatableInGroup.length > 0 && selectedIds != null &&
    updatableInGroup.every(c => selectedIds.has(c.id))

  function handleSelectAllUpdatable() {
    if (!onSelectionChange) return
    const newSelected = !allUpdatableSelected
    for (const c of updatableInGroup) {
      onSelectionChange(c.id, newSelected)
    }
  }

  return (
    <div className="break-inside-avoid mb-4">
      {/* Group Header */}
      <div className="flex items-center gap-2 mb-1.5 px-1">
        {onSelectionChange && updatableInGroup.length > 0 && (
          <input
            type="checkbox"
            checked={allUpdatableSelected}
            onChange={handleSelectAllUpdatable}
            className="w-3 h-3 rounded border-gray-600 bg-gray-700 text-blue-500 focus:ring-0 cursor-pointer"
            title="Select all updatable"
          />
        )}
        {group.icon && <span className="text-sm">{group.icon}</span>}
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wider">{group.name}</h3>
        <span className="text-xs text-gray-700">{runningCount}/{group.containers.length}</span>

        <div className="flex-1" />

        {/* Stack auto-update toggle */}
        {deviceId && stackInfo && (
          <button
            onClick={() => autoUpdateMutation.mutate({
              target: stackInfo.project,
              isStack: true,
              composeWorkDir: stackInfo.workDir,
              enabled: !isStackAutoUpdate,
            })}
            className={`flex items-center gap-1.5 px-2 py-0.5 text-xs rounded transition-colors ${
              isStackAutoUpdate
                ? 'bg-emerald-500/15 text-emerald-400'
                : 'text-gray-600 hover:text-gray-400'
            }`}
            title={isStackAutoUpdate ? 'Auto-update enabled for stack' : 'Enable auto-update for stack'}
          >
            <span className={`w-1.5 h-1.5 rounded-full ${isStackAutoUpdate ? 'bg-emerald-400' : 'bg-gray-600'}`} />
            Auto
          </button>
        )}

        {/* Update stack button */}
        {deviceId && updatableStacks.map(stack => (
          <button
            key={stack.workDir}
            onClick={() => setConfirmStack(stack)}
            disabled={stackMutation.isPending}
            className="px-2 py-0.5 text-xs font-medium rounded bg-amber-500/15 text-amber-400 hover:bg-amber-500/25 transition-colors disabled:opacity-50"
          >
            {stackMutation.isPending ? '...' : `Update (${stack.updatableCount})`}
          </button>
        ))}
      </div>

      {stackMutation.isSuccess && (
        <div className="px-2 py-1 mb-1 bg-emerald-900/20 rounded text-xs text-emerald-400">
          Stack update command sent
        </div>
      )}
      {stackMutation.isError && (
        <div className="px-2 py-1 mb-1 bg-red-900/20 rounded text-xs text-red-400">
          {(stackMutation.error as Error).message}
        </div>
      )}

      {/* Container List */}
      <div className="space-y-1.5">
        {group.containers.map(c => {
          const containerPolicy = !stackInfo ? policyMap.get(c.name) : null
          return (
            <ContainerCard
              key={c.id}
              container={c}
              onClick={onContainerClick}
              autoUpdate={isStackAutoUpdate || (containerPolicy?.enabled ?? false)}
              selected={selectedIds?.has(c.id)}
              onSelect={onSelectionChange ? (container, sel) => onSelectionChange(container.id, sel) : undefined}
              networkParent={networkParentMap.get(c.id)}
            />
          )
        })}
      </div>

      {confirmStack && (
        <ConfirmModal
          title="Update Compose Stack"
          message={`This will pull new images and recreate all services in the "${confirmStack.project}" stack (${confirmStack.updatableCount} updatable). Continue?`}
          confirmLabel="Update Stack"
          confirmVariant="primary"
          onConfirm={() => {
            stackMutation.mutate({ workDir: confirmStack.workDir })
            setConfirmStack(null)
          }}
          onCancel={() => setConfirmStack(null)}
        />
      )}
    </div>
  )
}

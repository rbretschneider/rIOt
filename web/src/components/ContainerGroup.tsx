import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ContainerInfo, AutoUpdatePolicy } from '../types/models'
import type { ContainerGroup as ContainerGroupType } from '../utils/docker'
import ContainerCard from './ContainerCard'
import ConfirmModal from './ConfirmModal'

interface Props {
  group: ContainerGroupType
  onContainerClick: (c: ContainerInfo) => void
  deviceId?: string
}

interface ComposeStack {
  workDir: string
  project: string
  updatableCount: number
}

export default function ContainerGroup({ group, onContainerClick, deviceId }: Props) {
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

  const stackMutation = useMutation({
    mutationFn: ({ workDir }: { workDir: string }) =>
      api.sendCommand(deviceId!, 'docker_update', { compose_work_dir: workDir }),
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

  return (
    <div className="border border-gray-700/60 rounded-lg bg-gray-800/30 overflow-hidden">
      {/* Group Header */}
      <div className="flex items-center gap-2 px-3 py-2 bg-gray-800/50 border-b border-gray-700/40">
        {group.icon && <span className="text-sm">{group.icon}</span>}
        <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">{group.name}</h3>
        <span className="text-[11px] text-gray-600">{runningCount}/{group.containers.length}</span>

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
            className={`flex items-center gap-1.5 px-2 py-0.5 text-[10px] rounded transition-colors ${
              isStackAutoUpdate
                ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
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
            className="px-2 py-0.5 text-[10px] font-medium rounded bg-amber-500/15 text-amber-400 border border-amber-500/30 hover:bg-amber-500/25 transition-colors disabled:opacity-50"
          >
            {stackMutation.isPending ? 'Updating...' : `Update (${stack.updatableCount})`}
          </button>
        ))}
      </div>

      {stackMutation.isSuccess && (
        <div className="px-3 py-1.5 bg-emerald-900/20 border-b border-emerald-800/30 text-xs text-emerald-400">
          Stack update command sent
        </div>
      )}
      {stackMutation.isError && (
        <div className="px-3 py-1.5 bg-red-900/20 border-b border-red-800/30 text-xs text-red-400">
          {(stackMutation.error as Error).message}
        </div>
      )}

      {/* Container Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2 p-2">
        {group.containers.map(c => {
          const containerPolicy = !stackInfo ? policyMap.get(c.name) : null
          return (
            <ContainerCard
              key={c.id}
              container={c}
              onClick={onContainerClick}
              autoUpdate={isStackAutoUpdate || (containerPolicy?.enabled ?? false)}
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

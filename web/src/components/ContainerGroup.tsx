import { useState, useMemo } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api } from '../api/client'
import type { ContainerInfo } from '../types/models'
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

  // Find compose stacks in this group that have updatable containers
  const updatableStacks = useMemo(() => {
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
    return Array.from(stacks.values()).filter(s => s.updatableCount > 0)
  }, [group.containers])

  const stackMutation = useMutation({
    mutationFn: ({ workDir }: { workDir: string }) =>
      api.sendCommand(deviceId!, 'docker_update', { compose_work_dir: workDir }),
  })

  return (
    <div>
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        {group.icon && <span className="text-base">{group.icon}</span>}
        <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">{group.name}</h3>
        <span className="text-xs text-gray-600">({group.containers.length})</span>
        {deviceId && updatableStacks.map(stack => (
          <button
            key={stack.workDir}
            onClick={() => setConfirmStack(stack)}
            disabled={stackMutation.isPending}
            className="px-2 py-0.5 text-[11px] font-medium rounded-md bg-amber-500/15 text-amber-400 border border-amber-500/30 hover:bg-amber-500/25 transition-colors disabled:opacity-50"
          >
            {stackMutation.isPending ? 'Updating...' : `Update Stack (${stack.updatableCount})`}
          </button>
        ))}
      </div>

      {stackMutation.isSuccess && (
        <div className="mb-3 px-3 py-2 bg-emerald-900/30 border border-emerald-800 rounded text-sm text-emerald-400">
          Stack update command sent
        </div>
      )}
      {stackMutation.isError && (
        <div className="mb-3 px-3 py-2 bg-red-900/30 border border-red-800 rounded text-sm text-red-400">
          {(stackMutation.error as Error).message}
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
        {group.containers.map(c => (
          <ContainerCard key={c.id} container={c} onClick={onContainerClick} />
        ))}
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

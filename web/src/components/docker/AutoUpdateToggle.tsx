import { useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'
import { autoUpdateKey, useAutoUpdateSave } from '../../hooks/useAutoUpdateSave'
export default function AutoUpdateToggle({
  deviceId,
  target,
  workingDir,
  stack = false,
  disabled = false,
}: {
  deviceId: string
  target: string
  workingDir: string
  stack?: boolean
  disabled?: boolean
}) {
  const query = useQuery({
    queryKey: ['auto-updates', deviceId],
    queryFn: () => api.getAutoUpdates(deviceId),
  })
  const { save, display } = useAutoUpdateSave()
  // A container and a stack may share a name on one host; only the policy of
  // this toggle's kind is its own.
  const policy = query.data?.find(
    (p) => p.target === target && p.is_stack === stack,
  )
  const { checked, pending } = display(
    autoUpdateKey(deviceId, stack, target),
    policy?.enabled ?? false,
  )
  return (
    <div>
      <label className="flex gap-2 items-center text-sm">
        <input
          type="checkbox"
          checked={checked}
          aria-busy={pending || undefined}
          disabled={disabled || pending || query.isPending}
          onChange={(e) =>
            void save({
              deviceId,
              target,
              stack,
              workingDir,
              enabled: e.target.checked,
            })
          }
        />
        Auto update
      </label>
    </div>
  )
}

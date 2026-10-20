import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'
import { ErrorPanel } from '../ui'
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
  const qc = useQueryClient()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const policy = query.data?.find((p) => p.target === target)
  return (
    <div>
      <label className="flex gap-2 items-center text-sm">
        <input
          type="checkbox"
          checked={policy?.enabled ?? false}
          disabled={disabled || pending || query.isPending}
          onChange={async (e) => {
            setPending(true)
            setError(undefined)
            try {
              await api.setAutoUpdate(
                deviceId,
                target,
                stack,
                workingDir,
                e.target.checked,
              )
              await qc.invalidateQueries({
                queryKey: ['auto-updates', deviceId],
              })
            } catch (err) {
              setError(err)
            } finally {
              setPending(false)
            }
          }}
        />
        Auto update
      </label>
      {error ? <ErrorPanel error={error} /> : null}
    </div>
  )
}

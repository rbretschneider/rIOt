import { useCallback, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { errorMessage, isApiError } from '../api/errors'
import { useToast } from '../components/ui'

export const AUTO_UPDATE_NAME_CLASH = 'auto_update_name_clash'

export interface AutoUpdateSave {
  deviceId: string
  hostname?: string
  target: string
  stack: boolean
  workingDir: string
  enabled: boolean
}

interface Override {
  value: boolean
  phase: 'saving' | 'saved'
}

export const autoUpdateKey = (deviceId: string, stack: boolean, target: string) =>
  `${deviceId}|${stack ? 'stack' : 'container'}|${target}`

function clashMessage(e: unknown) {
  const kind = isApiError(e) ? e.detail?.existing_kind : undefined
  const other = kind === 'stack' || kind === 'container' ? `A ${kind}` : 'A policy of the other kind'
  return `${other} with the same name on this host has auto-update on. Turn that off first.`
}

/**
 * Saves auto-update switches at once. A key (host, kind, target) saves one at a
 * time: a flip while it is saving is ignored. The new value shows while saving;
 * success refreshes the lists, failure reverts and raises an error toast.
 * Turning off stores enabled=false; the policy is never deleted.
 */
export function useAutoUpdateSave() {
  const qc = useQueryClient()
  const toast = useToast()
  const saving = useRef(new Set<string>())
  const [overrides, setOverrides] = useState<Record<string, Override>>({})
  const setOverride = (key: string, value?: Override) =>
    setOverrides((current) => {
      const next = { ...current }
      if (value) next[key] = value
      else delete next[key]
      return next
    })

  const save = useCallback(
    async (args: AutoUpdateSave) => {
      const key = autoUpdateKey(args.deviceId, args.stack, args.target)
      if (saving.current.has(key)) return
      saving.current.add(key)
      setOverride(key, { value: args.enabled, phase: 'saving' })
      try {
        await api.setAutoUpdate(args.deviceId, args.target, args.stack, args.workingDir, args.enabled)
      } catch (e) {
        saving.current.delete(key)
        setOverride(key)
        const where = args.hostname ? ` on ${args.hostname}` : ''
        toast.error(
          `Auto-update not changed for ${args.stack ? 'stack' : 'container'} ${args.target}${where}`,
          isApiError(e) && e.code === AUTO_UPDATE_NAME_CLASH ? clashMessage(e) : errorMessage(e),
        )
        return
      }
      setOverride(key, { value: args.enabled, phase: 'saved' })
      try {
        await Promise.all([
          qc.invalidateQueries({ queryKey: ['docker-containers'] }),
          qc.invalidateQueries({ queryKey: ['docker-stacks'] }),
          qc.invalidateQueries({ queryKey: ['auto-updates', args.deviceId] }),
          ...(args.stack
            ? [qc.invalidateQueries({ queryKey: ['docker-stack', args.deviceId, args.target] })]
            : []),
        ])
      } finally {
        saving.current.delete(key)
        setOverride(key)
      }
    },
    [qc, toast],
  )

  /** The switch to show for a key: the value being saved, else the server's. */
  const display = useCallback(
    (key: string, serverValue: boolean) => {
      const override = overrides[key]
      return {
        checked: override?.value ?? serverValue,
        pending: override?.phase === 'saving',
      }
    },
    [overrides],
  )

  return { save, display }
}

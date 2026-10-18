// CMD-LIFECYCLE-001 (AD-019, FR-089..099): follow a command by ID until it is
// terminal, instead of resetting a mutation on a fixed timer.
//
// Sources of truth, in order of preference:
//   1. the WebSocket `command_update` push for the tracked command;
//   2. a re-fetch of the command by ID when no push arrived for 5s, repeated
//      every 5s while the command is non-terminal (FR-099);
//   3. the device's non-terminal commands, fetched on mount and shared through
//      the ['commands-active', deviceId] query (FR-092).
//
// Nothing here ends tracking of a non-terminal command on a timer (FR-096). The
// only timer that clears the tracked command is the 5s "Updated" display after
// a success. Every failure shape except success stays on screen until dismissed
// or until a new action starts (FR-095).
//
// This file reads no clock: all timing is setTimeout, so vitest fake timers
// drive it deterministically.

import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useWebSocket } from './useWebSocket'
import {
  applyCommandUpdate,
  commandFromUpdate,
  commandLabel,
  isCommandConflictError,
  isTerminal,
  matchesTarget,
  normalizeStatus,
  upsertActiveCommand,
} from '../utils/commandLifecycle'
import type { Command, CommandUpdate, WSMessage } from '../types/models'

/** Re-fetch interval when no push update arrives (FR-099). */
export const COMMAND_POLL_MS = 5000
/** How long "Updated" stays on screen after a success (FR-095). */
export const SUCCEEDED_DISPLAY_MS = 5000

// A placeholder updated_at that every real server value outranks.
const UNKNOWN_TIME = '1970-01-01T00:00:00Z'

export interface CommandTrackerOptions {
  pollMs?: number
  succeededDisplayMs?: number
}

export interface CommandTracker {
  /** The tracked command, including a terminal one that is still on screen. */
  command: Command | null
  /** True while the tracked command is non-terminal. */
  isActive: boolean
  /** True while a create request is in flight. */
  isStarting: boolean
  /** isStarting or isActive: controls for the target stay disabled while true. */
  isBusy: boolean
  /** Display label from the frozen FR-090/FR-095 tables, or null when nothing is tracked. */
  label: string | null
  /** Cancel is offered only while the command is queued (FR-098). */
  canCancel: boolean
  isCancelling: boolean
  /** Message of the last failed create or cancel request, if any. */
  error: string | null
  /**
   * Runs a create request and tracks the command it returns. On a 409 conflict
   * it attaches to the existing command instead (FR-093).
   */
  track: <T extends Command>(send: () => Promise<T>) => Promise<T | undefined>
  cancel: () => Promise<void>
  /** Clears a terminal command (or a request error) from view. */
  dismiss: () => void
}

function isNewer(next: Command, current: Command | null): boolean {
  if (!current || current.id !== next.id) return true
  // A non-terminal snapshot must never replace a terminal one.
  if (isTerminal(current.status) && !isTerminal(next.status)) return false
  const cur = Date.parse(current.updated_at)
  const nxt = Date.parse(next.updated_at)
  if (Number.isNaN(cur) || Number.isNaN(nxt)) return true
  return nxt >= cur
}

export function useCommandTracker(
  deviceId: string | undefined,
  targetKey: string | null | undefined,
  options: CommandTrackerOptions = {},
): CommandTracker {
  const pollMs = options.pollMs ?? COMMAND_POLL_MS
  const succeededMs = options.succeededDisplayMs ?? SUCCEEDED_DISPLAY_MS
  const queryClient = useQueryClient()

  const [tracked, setTracked] = useState<Command | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [isStarting, setIsStarting] = useState(false)
  const [isCancelling, setIsCancelling] = useState(false)
  // Bumped on every push, fetch and failed fetch so the 5s timer restarts.
  const [activity, setActivity] = useState(0)

  // Async callbacks read the latest tracked command from here, not from a stale closure.
  const trackedRef = useRef<Command | null>(null)

  const setCurrent = useCallback((cmd: Command | null) => {
    trackedRef.current = cmd
    setTracked(cmd)
  }, [])

  // Writes a full command into the shared active list (removing it once terminal).
  const syncActiveList = useCallback((cmd: Command) => {
    if (!deviceId) return
    queryClient.setQueryData<Command[]>(['commands-active', deviceId], (old) => {
      if (old === undefined && isTerminal(cmd.status)) return undefined
      return upsertActiveCommand(old, cmd)
    })
  }, [deviceId, queryClient])

  const commit = useCallback((cmd: Command) => {
    if (!isNewer(cmd, trackedRef.current)) {
      setActivity((n) => n + 1)
      return
    }
    const normalized: Command = { ...cmd, status: normalizeStatus(cmd.status) }
    setCurrent(normalized)
    syncActiveList(normalized)
    setActivity((n) => n + 1)
  }, [setCurrent, syncActiveList])

  // FR-092: fetch the device's non-terminal commands on mount and track the one
  // for this target. The query is shared by every tracker on the device.
  const activeQuery = useQuery({
    queryKey: ['commands-active', deviceId],
    queryFn: () => api.getActiveCommands(deviceId!),
    enabled: !!deviceId && !!targetKey,
    refetchOnMount: 'always',
  })

  const activeCommands = activeQuery.data
  useEffect(() => {
    if (!targetKey) return
    const current = trackedRef.current
    if (current && !isTerminal(current.status)) return
    const match = activeCommands?.find((c) => matchesTarget(c, targetKey) && !isTerminal(c.status))
    if (match && match.id !== current?.id) {
      setCurrent({ ...match, status: normalizeStatus(match.status) })
      setError(null)
    }
  }, [activeCommands, targetKey, setCurrent])

  // Push updates (FR-089, FR-079): apply to the tracked command, or adopt a new
  // non-terminal command that targets this component and was started elsewhere.
  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type !== 'command_update' || !deviceId) return
    const update = msg.data as CommandUpdate | undefined
    if (!update || (update.device_id ?? msg.device_id) !== deviceId) return

    const current = trackedRef.current
    if (current && current.id === update.command_id) {
      const next = applyCommandUpdate(current, update)
      if (next !== current) commit(next)
      return
    }
    if (
      targetKey &&
      !isTerminal(update.status) &&
      matchesTarget({ target_keys: update.target_keys, target_key: update.target_key }, targetKey) &&
      (!current || isTerminal(current.status))
    ) {
      commit(commandFromUpdate(update))
    }
  }, [deviceId, targetKey, commit])
  useWebSocket(handleWS)

  // FR-099: with no push for pollMs, re-fetch the command by ID, and keep doing
  // so every pollMs for as long as it is non-terminal.
  const trackedId = tracked?.id
  const trackedTerminal = tracked ? isTerminal(tracked.status) : true
  useEffect(() => {
    if (!deviceId || !trackedId || trackedTerminal) return
    const timer = setTimeout(() => {
      api.getCommand(deviceId, trackedId)
        .then((fresh) => {
          if (trackedRef.current?.id === trackedId) commit(fresh)
        })
        .catch(() => {
          // A failed poll is not a failed command: keep following it.
          setActivity((n) => n + 1)
        })
    }, pollMs)
    return () => clearTimeout(timer)
  }, [deviceId, trackedId, trackedTerminal, activity, pollMs, commit])

  // FR-095: "Updated" is shown for 5s, then the normal view returns.
  const trackedState = tracked ? normalizeStatus(tracked.status) : null
  useEffect(() => {
    if (trackedState !== 'succeeded') return
    const timer = setTimeout(() => {
      if (trackedRef.current?.id === trackedId) setCurrent(null)
    }, succeededMs)
    return () => clearTimeout(timer)
  }, [trackedState, trackedId, succeededMs, setCurrent])

  const attach = useCallback((commandId: string, statusHint: string | undefined) => {
    if (!deviceId) return
    // Show the existing command at once from what the 409 told us, then hydrate.
    const placeholder: Command = {
      id: commandId,
      device_id: deviceId,
      action: '',
      params: {},
      status: normalizeStatus(statusHint ?? 'queued'),
      result_msg: '',
      duration_ms: null,
      exit_code: null,
      created_at: UNKNOWN_TIME,
      updated_at: UNKNOWN_TIME,
      target_keys: targetKey ? [targetKey] : [],
    }
    setCurrent(placeholder)
    setActivity((n) => n + 1)
    api.getCommand(deviceId, commandId)
      .then((fresh) => {
        if (trackedRef.current?.id === commandId) commit(fresh)
      })
      .catch(() => {
        // The placeholder stays and the 5s re-fetch keeps trying.
        setActivity((n) => n + 1)
      })
  }, [deviceId, targetKey, setCurrent, commit])

  const track = useCallback(async <T extends Command>(send: () => Promise<T>): Promise<T | undefined> => {
    setError(null)
    // A new action replaces a failure that was left on screen.
    if (trackedRef.current && isTerminal(trackedRef.current.status)) setCurrent(null)
    setIsStarting(true)
    try {
      const created = await send()
      commit(created)
      return created
    } catch (e) {
      if (isCommandConflictError(e)) {
        attach(e.existingCommandId, e.existingStatus)
        return undefined
      }
      setError(e instanceof Error ? e.message : String(e))
      return undefined
    } finally {
      setIsStarting(false)
    }
  }, [commit, attach, setCurrent])

  const cancel = useCallback(async () => {
    const current = trackedRef.current
    if (!deviceId || !current || normalizeStatus(current.status) !== 'queued') return
    setIsCancelling(true)
    try {
      commit(await api.cancelCommand(deviceId, current.id))
    } catch (e) {
      // Most likely 409: the command was dispatched while the click was in
      // flight. Show the server's message, then re-read the real state.
      setError(e instanceof Error ? e.message : String(e))
      api.getCommand(deviceId, current.id)
        .then((fresh) => {
          if (trackedRef.current?.id === current.id) commit(fresh)
        })
        .catch(() => {
          // The periodic re-fetch will pick the state up.
          setActivity((n) => n + 1)
        })
    } finally {
      setIsCancelling(false)
    }
  }, [deviceId, commit])

  const dismiss = useCallback(() => {
    setError(null)
    if (trackedRef.current && isTerminal(trackedRef.current.status)) setCurrent(null)
  }, [setCurrent])

  const isActive = tracked ? !isTerminal(tracked.status) : false
  return {
    command: tracked,
    isActive,
    isStarting,
    isBusy: isStarting || isActive,
    label: tracked ? commandLabel(tracked) : null,
    canCancel: trackedState === 'queued',
    isCancelling,
    error,
    track,
    cancel,
    dismiss,
  }
}

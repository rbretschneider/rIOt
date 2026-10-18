// CMD-LIFECYCLE-001 (AD-019): pure helpers for the canonical command lifecycle.
//
// Everything here is side-effect free and clock free, so the tracker hook, the
// device cache and the components can share one definition of "active",
// "terminal", labels and target keys. The rules are frozen in ADD 6.7.
//
// Rendering rule (SEC C-11): every string produced from agent-originated text
// (result_msg, step, failure_step) is returned as plain text. Callers must
// render it as a React text node only, never as markup, href, src or title.

import type { Command, CommandState, CommandUpdate, ContainerInfo } from '../types/models'

// ---------------------------------------------------------------------------
// States
// ---------------------------------------------------------------------------

export const NON_TERMINAL_STATES: readonly CommandState[] = [
  'queued',
  'dispatched',
  'acknowledged',
  'running',
  'verifying',
]

export const TERMINAL_STATES: readonly CommandState[] = [
  'succeeded',
  'failed',
  'timed_out',
  'cancelled',
  'expired',
]

const CANONICAL_STATES: ReadonlySet<string> = new Set<string>([...NON_TERMINAL_STATES, ...TERMINAL_STATES])

// FR-083: legacy stored values and their canonical meaning.
const LEGACY_TO_CANONICAL: ReadonlyMap<string, CommandState> = new Map<string, CommandState>([
  ['pending', 'queued'],
  ['sent', 'dispatched'],
  ['success', 'succeeded'],
  ['error', 'failed'],
])

/**
 * Maps any status string to a canonical state. Legacy values are translated;
 * an unrecognised value maps to `failed`, mirroring the server's CanonicalStatus.
 */
export function normalizeStatus(status: string | null | undefined): CommandState {
  if (status && CANONICAL_STATES.has(status)) return status as CommandState
  if (status) {
    const legacy = LEGACY_TO_CANONICAL.get(status)
    if (legacy) return legacy
  }
  return 'failed'
}

/** True for succeeded, failed, timed_out, cancelled and expired. */
export function isTerminal(status: string | null | undefined): boolean {
  return TERMINAL_STATES.includes(normalizeStatus(status))
}

/** True while a command can still change: queued through verifying. */
export function isActiveStatus(status: string | null | undefined): boolean {
  return !isTerminal(status)
}

// ---------------------------------------------------------------------------
// Labels
// ---------------------------------------------------------------------------

const STEP_LABELS: ReadonlyMap<string, string> = new Map([
  ['precheck', 'Checking'],
  ['resolve', 'Resolving container'],
  ['pull', 'Pulling image'],
  ['stop_dependents', 'Stopping dependents'],
  ['recreate', 'Recreating'],
  ['verify', 'Verifying'],
  ['restart_dependents', 'Restarting dependents'],
  ['rollback', 'Rolling back'],
  ['cleanup', 'Cleaning up'],
])

/** Human label for an agent step; anything unknown, including "", is "Working". */
export function stepLabel(step: string | null | undefined): string {
  return (step && STEP_LABELS.get(step)) || 'Working'
}

const UPDATE_ACTIONS: ReadonlySet<string> = new Set(['docker_update', 'docker_bulk_update'])

/** Update actions get the "Updating…" wording; every other action is "Running…". */
export function isUpdateAction(action: string | null | undefined): boolean {
  return !!action && UPDATE_ACTIONS.has(action)
}

export type CommandLabelInput =
  Pick<Command, 'status' | 'action'> &
  Partial<Pick<Command, 'step' | 'failure_step' | 'result_msg'>>

/**
 * The live label for a tracked command (FR-090, FR-095, ADD 6.7). Legacy
 * statuses are normalised first, so a pre-lifecycle server still reads sensibly.
 */
export function commandLabel(cmd: CommandLabelInput): string {
  const state = normalizeStatus(cmd.status)
  const update = isUpdateAction(cmd.action)
  switch (state) {
    case 'queued': return 'Queued'
    case 'dispatched': return 'Sent to agent'
    case 'acknowledged': return 'Starting'
    case 'running':
      if (update) return `Updating… ${stepLabel(cmd.step)}`
      return cmd.step ? `Running… ${stepLabel(cmd.step)}` : 'Running…'
    case 'verifying': return 'Verifying'
    case 'succeeded': return update ? 'Updated' : 'Done'
    case 'failed': {
      const step = cmd.failure_step || cmd.step || ''
      const msg = cmd.result_msg || ''
      if (step && msg) return `Failed at ${step}: ${msg}`
      if (step) return `Failed at ${step}`
      return msg ? `Failed: ${msg}` : 'Failed'
    }
    case 'timed_out': return 'Timed out: outcome unknown'
    case 'expired': return 'Expired: never delivered'
    case 'cancelled': return 'Cancelled'
  }
}

/** History chip text (ADD 6.7): canonical labels for canonical and legacy rows. */
const HISTORY_CHIP_LABELS: Readonly<Record<CommandState, string>> = {
  queued: 'Queued',
  dispatched: 'Sent to agent',
  acknowledged: 'Acknowledged',
  running: 'Running',
  verifying: 'Verifying',
  succeeded: 'Succeeded',
  failed: 'Failed',
  timed_out: 'Timed out',
  cancelled: 'Cancelled',
  expired: 'Expired',
}

export function statusChipLabel(status: string | null | undefined): string {
  return HISTORY_CHIP_LABELS[normalizeStatus(status)]
}

/**
 * The only status values the history filter may send. The server answers any
 * other value with 400 invalid_filter (SEC C-6), so the UI never builds a
 * filter from free text.
 */
export const STATUS_FILTER_OPTIONS: readonly { value: CommandState; label: string }[] =
  (Object.keys(HISTORY_CHIP_LABELS) as CommandState[]).map(value => ({ value, label: HISTORY_CHIP_LABELS[value] }))

export function isAllowedStatusFilter(value: string): value is CommandState {
  return CANONICAL_STATES.has(value)
}

// ---------------------------------------------------------------------------
// Target keys (ADD 6.7, mirrors models.TargetKey*)
// ---------------------------------------------------------------------------

const LABEL_PROJECT = 'com.docker.compose.project'
const LABEL_SERVICE = 'com.docker.compose.service'
const LABEL_WORKING_DIR = 'com.docker.compose.project.working_dir'

export function composeTargetKey(workDir: string): string {
  return `docker:compose:${workDir.replace(/\/+$/, '')}`
}

export function containerTargetKey(name: string): string {
  return `docker:container:${name.replace(/^\/+/, '')}`
}

export function containerIdTargetKey(id: string): string {
  return `docker:id:${id}`
}

/** The key the server assigns to a container: compose key for a full compose container, else its name. */
export function targetKeyForContainer(c: Pick<ContainerInfo, 'id' | 'name' | 'labels'>): string {
  const wd = c.labels?.[LABEL_WORKING_DIR]
  if (wd && c.labels?.[LABEL_PROJECT] && c.labels?.[LABEL_SERVICE]) return composeTargetKey(wd)
  if (c.name.replace(/^\/+/, '')) return containerTargetKey(c.name)
  return containerIdTargetKey(c.id)
}

/**
 * Every key the server could have recorded for a container. The id key is the
 * server's fallback when it cannot resolve the container from telemetry, so a
 * match on it is still a match for this container.
 */
export function containerTargetKeys(c: Pick<ContainerInfo, 'id' | 'name' | 'labels'>): string[] {
  const keys = [targetKeyForContainer(c)]
  if (c.id) keys.push(containerIdTargetKey(c.id))
  return keys
}

/** A command matches a target when its target_keys include the key. */
export function matchesTarget(cmd: Pick<Command, 'target_keys' | 'target_key'>, key: string | null | undefined): boolean {
  if (!key) return false
  if (cmd.target_keys && cmd.target_keys.length > 0) return cmd.target_keys.includes(key)
  return cmd.target_key === key
}

// ---------------------------------------------------------------------------
// Applying updates (ADD 6.7 "Applying a command_update")
// ---------------------------------------------------------------------------

function parseTime(iso: string | undefined): number {
  if (!iso) return Number.NaN
  return Date.parse(iso)
}

/**
 * Applies a `command_update` to a command only if it is not older than what is
 * already known and does not move the command from terminal back to
 * non-terminal. Returns the same object when the update is ignored, so callers
 * can detect a no-op by identity.
 */
export function applyCommandUpdate(current: Command, update: CommandUpdate): Command {
  const next = normalizeStatus(update.status)
  if (isTerminal(current.status) && !TERMINAL_STATES.includes(next)) return current

  const cur = parseTime(current.updated_at)
  const upd = parseTime(update.updated_at)
  if (!Number.isNaN(cur) && !Number.isNaN(upd) && upd < cur) return current

  return {
    ...current,
    action: update.action || current.action,
    status: next,
    step: update.step,
    progress: update.progress,
    result_msg: update.message,
    failure_step: update.failure_step,
    origin: update.origin || current.origin,
    target_key: update.target_key || current.target_key,
    target_keys: update.target_keys && update.target_keys.length > 0 ? update.target_keys : current.target_keys,
    updated_at: update.updated_at || current.updated_at,
  }
}

/** Builds a Command from a `command_update` for a command the dashboard has not seen yet. */
export function commandFromUpdate(update: CommandUpdate): Command {
  return {
    id: update.command_id,
    device_id: update.device_id,
    action: update.action,
    params: {},
    status: normalizeStatus(update.status),
    result_msg: update.message,
    duration_ms: null,
    exit_code: null,
    created_at: update.updated_at,
    updated_at: update.updated_at,
    step: update.step,
    progress: update.progress,
    failure_step: update.failure_step,
    origin: update.origin,
    target_key: update.target_key,
    target_keys: update.target_keys,
  }
}

/**
 * Maintains the per-device list of non-terminal commands from a `command_update`:
 * a non-terminal update upserts, a terminal update removes. Returns the same
 * array when nothing changed.
 */
export function applyActiveUpdate(list: readonly Command[] | undefined, update: CommandUpdate): Command[] {
  const current = list ?? []
  const idx = current.findIndex(c => c.id === update.command_id)
  if (isTerminal(update.status)) {
    if (idx < 0) return current as Command[]
    return current.filter(c => c.id !== update.command_id)
  }
  if (idx < 0) return [...current, commandFromUpdate(update)]
  const applied = applyCommandUpdate(current[idx], update)
  if (applied === current[idx]) return current as Command[]
  const copy = [...current]
  copy[idx] = applied
  return copy
}

/** Inserts or replaces a full Command in an active list; a terminal command is removed. */
export function upsertActiveCommand(list: readonly Command[] | undefined, cmd: Command): Command[] {
  const current = list ?? []
  const idx = current.findIndex(c => c.id === cmd.id)
  if (isTerminal(cmd.status)) {
    if (idx < 0) return current as Command[]
    return current.filter(c => c.id !== cmd.id)
  }
  if (idx < 0) return [...current, cmd]
  const copy = [...current]
  copy[idx] = cmd
  return copy
}

// ---------------------------------------------------------------------------
// Update indicator (FR-053, ADD 6.7)
// ---------------------------------------------------------------------------

/**
 * True only when the agent's verdict is update_available. `pinned` and
 * `check_failed` never count. An agent that predates the verdict falls back to
 * the boolean.
 */
export function hasUpdateAvailable(c: Pick<ContainerInfo, 'update_status' | 'update_available'>): boolean {
  if (c.update_status !== undefined) return c.update_status === 'update_available'
  return c.update_available === true
}

/** Title for the update-available indicator, built from server enums only. Null when there is no indicator. */
export function updateIndicatorTitle(c: Pick<ContainerInfo, 'update_status' | 'update_available' | 'update_reason'>): string | null {
  if (!hasUpdateAvailable(c)) return null
  switch (c.update_reason) {
    case 'registry_newer': return 'Update available: newer image in registry'
    case 'pulled_not_recreated': return 'Update available: newer image pulled, container not recreated'
    default: return 'Update available'
  }
}

/** ContainerDetail status text for verdicts that render no indicator; null otherwise. */
export function updateStatusNote(c: Pick<ContainerInfo, 'update_status'>): string | null {
  if (c.update_status === 'pinned') return 'Pinned by digest'
  if (c.update_status === 'check_failed') return 'Update check failed'
  return null
}

// ---------------------------------------------------------------------------
// Exited suppression (FR-094)
// ---------------------------------------------------------------------------

/**
 * True when a non-terminal update command targets this container, so a docker
 * stop/die event must not be shown as `exited`: the container is being recreated.
 */
export function shouldSuppressExited(
  active: readonly Command[] | undefined,
  container: Pick<ContainerInfo, 'id' | 'name' | 'labels'>,
): boolean {
  if (!active || active.length === 0) return false
  const keys = containerTargetKeys(container)
  return active.some(
    cmd => isUpdateAction(cmd.action) && isActiveStatus(cmd.status) && keys.some(k => matchesTarget(cmd, k)),
  )
}

// ---------------------------------------------------------------------------
// 409 conflict detection (FR-093)
// ---------------------------------------------------------------------------

export interface CommandConflictLike extends Error {
  existingCommandId: string
  existingStatus?: string
}

/**
 * Duck-typed so it works when a test replaces the API module and so the demo
 * client does not need to share the class identity.
 */
export function isCommandConflictError(e: unknown): e is CommandConflictLike {
  if (!(e instanceof Error) || e.name !== 'CommandConflictError') return false
  const id = (e as Error & { existingCommandId?: unknown }).existingCommandId
  return typeof id === 'string' && id !== ''
}

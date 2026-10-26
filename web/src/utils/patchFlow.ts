import type { UpdateInfo } from '../types/models'
import type { PatchBadgeKind } from '../api/client'
import { enabledPendingExtras, extraName } from './extraUpdaters'

/**
 * Whether a badge kind means "this host needs Ryan" (EXTRA-UPD-001 FR-082, FR-083).
 * Only needs_you does: the neutral manual kind and the automatic kinds do not, and a kind
 * this dashboard does not know (a newer server) is not treated as needing action.
 */
export function isNeedsYou(kind: PatchBadgeKind | string | undefined): boolean {
  return kind === 'needs_you'
}

/**
 * Whether a badge kind is a refusal Ryan cannot act on (DIETPI-PATH-001 FR-017). Blocked hosts
 * are counted and filtered on their own and are never part of "N hosts need you".
 */
export function isBlocked(kind: PatchBadgeKind | string | undefined): boolean {
  return kind === 'blocked'
}

// PATCH-FLOW-001: pure decision helpers for the manual patch flow. They take only
// data the device page already loads (NFR-011) and never fetch or render.

export type ListsFreshness = 'fresh' | 'stale' | 'unknown'

/** Shown instead of "Patch (N)" while the package lists are older than the apt configuration. */
export const STALE_PATCH_LABEL = 'Package lists out of date — refresh pending'

/** Tooltip for the stale label: what the operator can do about it. */
export const STALE_PATCH_HINT = 'Running Patch refreshes the package lists first.'

/** Qualifier appended to a security count that was read from stale lists (SEC-007). */
export const STALE_SECURITY_QUALIFIER = '(lists out of date)'

export const REBOOT_OPTION_LABEL = 'Reboot automatically if the patch requires it'

const REBOOT_OFF_REASON = 'Reboots are turned off on this agent (commands.allow_reboot: false).'
const AGENT_UPDATE_REASON = 'The agent must be updated to use this option.'
const REBOOT_OUTSIDE_REASON = 'Reboots are turned off on this agent (commands.allow_reboot: false). Reboot this host outside rIOt.'

const STATEMENT_REBOOT_REQUIRED = 'A reboot is already required on this host. With the box ticked, the host reboots after the run.'
const STATEMENT_TICKED = 'Kernel and GPU driver updates will be installed. The host reboots after the run if a reboot is required.'
const STATEMENT_HELD = 'Kernel and GPU driver updates are held by rIOt and will not be installed by this run.'
const STATEMENT_UNHELD = 'Kernel and GPU driver updates will be installed. A reboot will be needed afterwards, and rIOt will not reboot the host.'
const STATEMENT_STALE = 'Package lists are out of date. Running Patch refreshes them first, so the packages installed may differ from the pending list.'

/** Only the three known values pass; anything else (older server, bad data) is unknown. */
export function normalizeFreshness(value: unknown): ListsFreshness {
  return value === 'fresh' || value === 'stale' ? value : 'unknown'
}

export type PatchPrimary = 'none' | 'patch' | 'reboot'

export interface DevicePatchState {
  freshness: ListsFreshness
  /** Pending package count that is worth acting on: 0 while the lists are stale. */
  actionablePending: number
  rebootRequired: boolean
  primary: PatchPrimary
  /** The Patch button text, or null when the button is not shown. */
  patchLabel: string | null
  showPatch: boolean
  /** Why the primary Reboot action is disabled, or null when it is usable. */
  rebootDisabledReason: string | null
}

/**
 * Maps a device's update telemetry to the primary action (FRD section 7, S1 to S6).
 * Stale lists (S6) win over a count because a count read from stale lists is not
 * actionable (BR-005); Patch stays available because a run refreshes the lists first.
 */
export function devicePatchState(updates: UpdateInfo | undefined): DevicePatchState {
  const freshness = normalizeFreshness(updates?.lists_freshness)
  const pending = updates?.pending_updates ?? 0
  const rebootRequired = updates?.reboot_required === true
  const base = { freshness, rebootRequired, rebootDisabledReason: null }

  if (!updates) {
    return { ...base, actionablePending: 0, primary: 'none', patchLabel: null, showPatch: false }
  }
  if (freshness === 'stale') {
    return { ...base, actionablePending: 0, primary: 'patch', patchLabel: STALE_PATCH_LABEL, showPatch: true }
  }
  // EXTRA-UPD-001 FR-055: a pending, enabled extra updater is patchable work too,
  // and the label names it.
  const extras = enabledPendingExtras(updates).map(e => extraName(e.id))
  if (pending > 0 || extras.length > 0) {
    const parts = [...(pending > 0 ? [String(pending)] : []), ...extras]
    return { ...base, actionablePending: pending, primary: 'patch', patchLabel: `Patch (${parts.join(' + ')})`, showPatch: true }
  }
  if (rebootRequired) {
    const forbidden = updates.allow_reboot === false
    return {
      ...base,
      actionablePending: 0,
      primary: 'reboot',
      patchLabel: null,
      showPatch: false,
      rebootDisabledReason: forbidden ? REBOOT_OUTSIDE_REASON : null,
    }
  }
  return { ...base, actionablePending: 0, primary: 'none', patchLabel: null, showPatch: false }
}

export interface RebootOption {
  enabled: boolean
  disabledReason: string | null
}

/**
 * Whether the "Reboot automatically if the patch requires it" box can be used
 * (FR-010, FR-011). Only an explicit allow_reboot plus the capability fact enables it;
 * anything the telemetry does not say is treated as "not available".
 */
export function rebootOption(updates: UpdateInfo | undefined): RebootOption {
  if (updates?.allow_reboot === false) {
    return { enabled: false, disabledReason: REBOOT_OFF_REASON }
  }
  if (updates?.allow_reboot === undefined || updates.supports_reboot_if_required !== true) {
    return { enabled: false, disabledReason: AGENT_UPDATE_REASON }
  }
  return { enabled: true, disabledReason: null }
}

function holdsActive(updates: UpdateInfo): boolean {
  return updates.hold_enforcement === 'active' && (updates.held_packages?.length ?? 0) > 0
}

function rebootClassStatement(updates: UpdateInfo, ticked: boolean): string | null {
  if ((updates.pending_reboot_class_count ?? 0) <= 0) {
    return null
  }
  if (ticked) {
    return STATEMENT_TICKED
  }
  return holdsActive(updates) ? STATEMENT_HELD : STATEMENT_UNHELD
}

/**
 * What the dialog tells the operator about the run (FR-012, FR-013): an already
 * required reboot, then what happens to pending kernel/GPU updates, then the
 * stale-lists caveat. Statements that do not apply are omitted.
 */
export function patchDialogStatements(updates: UpdateInfo | undefined, ticked: boolean): string[] {
  if (!updates) {
    return []
  }
  const statements: string[] = []
  if (updates.reboot_required === true) {
    statements.push(STATEMENT_REBOOT_REQUIRED)
  }
  const rebootClass = rebootClassStatement(updates, ticked)
  if (rebootClass) {
    statements.push(rebootClass)
  }
  if (normalizeFreshness(updates.lists_freshness) === 'stale') statements.push(STATEMENT_STALE)
  return statements
}

/** "N security updates (lists out of date)" for a stale host, or null when there is nothing to say. */
export function staleSecurityNote(updates: UpdateInfo | undefined): string | null {
  const count = updates?.pending_security_count ?? 0
  if (!updates || normalizeFreshness(updates.lists_freshness) !== 'stale' || count <= 0) {
    return null
  }
  return `${count} security update${count === 1 ? '' : 's'} ${STALE_SECURITY_QUALIFIER}`
}

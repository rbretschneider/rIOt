import type { Command } from '../types/models'
import { enumLabel } from '../components/ui/labels'

// EXPECT-002 (ADD AD-009, AD-011): helpers for an agent's own self-update record.
//
// The server owns the record and its texts. Versions in `params` came from an
// agent's message and were validated by the server, but the web re-validates them
// with the same closed grammar before building a label, so a corrupted or
// hostile value is never shown. Everything returned is plain text for a React
// text node, never markup.

/** The actor phrase of every self-update (FR-027). A Command carries no actor phrase of its own. */
export const SELF_UPDATE_ACTOR_PHRASE = 'by agent auto-update'

const SELF_UPDATE_ORIGIN = 'agent_self_update'

// ADD AD-011: 1 to 32 bytes, optional leading v, three groups of 1 to 5 digits, optional -/+ suffix.
const VERSION_PATTERN = /^v?[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}(?:[-+][A-Za-z0-9.-]{1,16})?$/

/** True when the string is a version the server's grammar accepts. */
export function isValidAgentVersion(value: unknown): value is string {
  return typeof value === 'string' && value.length <= 32 && VERSION_PATTERN.test(value)
}

const stripV = (version: string) => (version.startsWith('v') ? version.slice(1) : version)

export interface SelfUpdateVersions {
  from: string
  to: string
}

/**
 * Both versions, only when both are valid and differ (equal after removing one
 * leading "v" counts as equal). Anything else returns null and the caller shows
 * the plain "Agent update" wording.
 */
export function validSelfUpdateVersions(from: unknown, to: unknown): SelfUpdateVersions | null {
  if (!isValidAgentVersion(from) || !isValidAgentVersion(to)) return null
  if (stripV(from) === stripV(to)) return null
  return { from, to }
}

/** True for a record the agent made on its own (never a command sent to an agent). */
export function isSelfUpdate(command: Pick<Command, 'origin' | 'action'> | null | undefined): boolean {
  return command?.origin === SELF_UPDATE_ORIGIN && command.action === 'agent_update'
}

/** The re-validated versions from a self-update row's `params`, or null. */
export function selfUpdateVersions(command: Pick<Command, 'params'> | null | undefined): SelfUpdateVersions | null {
  const params = command?.params
  if (!params || typeof params !== 'object') return null
  return validSelfUpdateVersions(params['from_version'], params['to_version'])
}

/** "Agent update 2.78.0 → 2.80.0", or "Agent update" when the versions are not valid. */
export function selfUpdateActionLabel(versions: SelfUpdateVersions | null): string {
  const base = enumLabel('commandAction', 'agent_update')
  return versions ? `${base} ${versions.from} → ${versions.to}` : base
}

const OUTCOME_LABELS: Record<string, string> = {
  running: 'In progress',
  verifying: 'Returned, confirming',
  succeeded: 'Succeeded',
  failed: 'Failed',
  timed_out: 'Not confirmed',
}

/** The outcome wording of FR-015 for a self-update row's status. Unknown values fall back to the status label. */
export function selfUpdateOutcomeLabel(status: string | null | undefined): string {
  if (status && Object.prototype.hasOwnProperty.call(OUTCOME_LABELS, status)) return OUTCOME_LABELS[status]
  return enumLabel('commandStatus', status)
}

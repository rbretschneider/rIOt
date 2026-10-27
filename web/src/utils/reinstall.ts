import type { Command, DeviceActivity } from '../types/models'
import { enumLabel } from '../components/ui/labels'

// EXPECT-008 (ADD AD-010): helpers for the server's record of an installer re-run.
//
// The server owns the record and its texts. A reinstall record is never a command
// sent to an agent: it has no versions, no controls, and the only things the web
// shows are fixed wording plus server-authored text rendered as React text.

const REINSTALL_ORIGIN = 'installer'

/** Action label of the record in the command history and the record view (FR-017, FR-021). */
export const REINSTALL_ACTION_LABEL = 'Agent reinstall'

/** Actor phrase of every reinstall (DEC-3). A Command carries no actor phrase of its own. */
export const REINSTALL_ACTOR_PHRASE = 'by installer'

/** Fleet and device page label while the window is open (FR-024). */
export const REINSTALL_ACTIVITY_LABEL = 'Reinstalling agent'

/** True for the server's record of an install.sh re-run (never a command sent to an agent). */
export function isReinstall(command: Pick<Command, 'origin' | 'action'> | null | undefined): boolean {
  return command?.origin === REINSTALL_ORIGIN && command.action === 'agent_update'
}

/** True for an activity (window or escalation) that belongs to a reinstall record. */
export function isReinstallActivity(activity: Pick<DeviceActivity, 'origin' | 'action'> | null | undefined): boolean {
  return activity?.origin === REINSTALL_ORIGIN && activity.action === 'agent_update'
}

const OUTCOME_LABELS: Record<string, string> = {
  running: 'In progress',
  succeeded: 'Succeeded',
  timed_out: 'Not confirmed',
}

/** The outcome wording for a reinstall row's status. Unknown values fall back to the status label. */
export function reinstallOutcomeLabel(status: string | null | undefined): string {
  if (status && Object.prototype.hasOwnProperty.call(OUTCOME_LABELS, status)) return OUTCOME_LABELS[status]
  return enumLabel('commandStatus', status)
}

/** Badge tone: a reinstall that was not confirmed is a warning, never an error (FR-022 stores a warning). */
export function reinstallOutcomeTone(status: string | null | undefined): 'success' | 'warn' | 'info' {
  if (status === 'succeeded') return 'success'
  if (status === 'timed_out') return 'warn'
  return 'info'
}

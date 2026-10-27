import type { ActivityResponse, Device, DeviceActivity } from '../types/models'
import { enumLabel } from '../components/ui/labels'
import { selfUpdateActionLabel, validSelfUpdateVersions } from './selfUpdate'
import { REINSTALL_ACTION_LABEL, REINSTALL_ACTIVITY_LABEL, isReinstallActivity } from './reinstall'

// EXPECT-001 (ADD 6.2): the server decides which activity a device shows and
// sends structured fields; this module only turns them into text. Nothing here
// builds HTML, and every string from the wire is rendered by React as text.

export interface ActivityLabelContext {
  /** The newest agent version the server knows (ActivityResponse.newest_agent_version). */
  newestAgentVersion?: string
  /** Clock time for an RFC 3339 instant, in the user's time-zone preference. */
  formatTime: (iso: string) => string
}

// Verbs for "Docker: <verb> <target>" (ADD 6.2).
const DOCKER_VERBS: Record<string, string> = {
  docker_update: 'updating',
  docker_bulk_update: 'updating',
  docker_restart: 'restarting',
  docker_stop: 'stopping',
  docker_stack_redeploy: 'recreating stack',
  docker_stack_restart: 'restarting stack',
  docker_stack_up: 'starting stack',
  docker_stack_down: 'stopping stack',
}

function actionLabel(action: string): string {
  return enumLabel('commandAction', action)
}

function waitingSuffix(a: DeviceActivity): string {
  return a.waiting_dpkg_lock ? ' (waiting for dpkg lock)' : ''
}

function backBy(a: DeviceActivity, ctx: ActivityLabelContext): string {
  return a.back_by ? `, back by ${ctx.formatTime(a.back_by)}` : ''
}

function dockerLabel(a: DeviceActivity): string {
  if (a.docker_actions >= 2) return `Docker: ${a.docker_actions} actions in progress`
  const verb = DOCKER_VERBS[a.action] ?? actionLabel(a.action).toLowerCase()
  return a.target ? `Docker: ${verb} ${a.target}` : `Docker: ${verb}`
}

/**
 * EXPECT-002 (AD-009): the versions of a self-update activity, re-validated. Only an
 * agent_update activity can carry them; a command window never has agent_to_version.
 */
function selfUpdateVersionsOf(a: DeviceActivity) {
  return a.action === 'agent_update' ? validSelfUpdateVersions(a.agent_from_version, a.agent_to_version) : null
}

function escalatedLabel(a: DeviceActivity): string {
  const versions = selfUpdateVersionsOf(a)
  // EXPECT-008 (AD-010): an installer re-run has no versions; it is named "Agent reinstall".
  const action = isReinstallActivity(a)
    ? REINSTALL_ACTION_LABEL
    : versions
      ? selfUpdateActionLabel(versions)
      : actionLabel(a.action)
  if (a.escalation?.reason === 'not_back') {
    return a.escalation.bound ? `Not back ${a.escalation.bound} after ${action}` : `Not back after ${action}`
  }
  return `${action} failed: offline`
}

function hostLabel(a: DeviceActivity, ctx: ActivityLabelContext): string {
  if (a.phase === 'queued') return `${actionLabel(a.action)} queued`
  const returning = a.phase === 'return'
  switch (a.action) {
    case 'reboot':
      return returning ? `Rebooting${backBy(a, ctx)}` : `Rebooting${waitingSuffix(a)}`
    case 'shutdown':
      // X-4: a succeeded shutdown whose device is still online stays "Shutting down".
      return `Shutting down${waitingSuffix(a)}`
    case 'os_update':
      if (returning) {
        if (a.device_online) return a.back_by ? `Patched, settling until ${ctx.formatTime(a.back_by)}` : 'Patched, settling'
        return `Rebooting after patch${backBy(a, ctx)}`
      }
      return a.progress != null ? `Patching (${a.progress}%)` : 'Patching'
    case 'agent_update':
      // EXPECT-008 (FR-024): the server's installer window, shown whether the device is online or offline.
      if (isReinstallActivity(a)) return REINSTALL_ACTIVITY_LABEL
      if (returning) return `Restarting agent${backBy(a, ctx)}`
      {
        // EXPECT-002 (FR-024): a self-update names both versions from the record itself.
        const self = selfUpdateVersionsOf(a)
        if (self) return `Updating agent ${self.from} → ${self.to}`
      }
      if (a.agent_from_version && ctx.newestAgentVersion) {
        return `Updating agent ${a.agent_from_version} → ${ctx.newestAgentVersion}`
      }
      return 'Updating agent'
    default:
      return actionLabel(a.action)
  }
}

/** The label shown in place of the plain status text (FR-035). */
export function activityLabel(a: DeviceActivity, ctx: ActivityLabelContext): string {
  switch (a.kind) {
    case 'powered_off':
      return `Powered off ${a.actor_phrase} at ${ctx.formatTime(a.powered_off_at ?? a.started_at)}`
    case 'escalated':
      return escalatedLabel(a)
    case 'docker':
      return dockerLabel(a)
    default:
      return hostLabel(a, ctx)
  }
}

/**
 * True for an agent update that is still running (queued, updating or restarting).
 * The fleet row shows these compactly in its Version column; an escalated update
 * is a real outage and keeps the full label. EXPECT-008: an installer re-run has no
 * target version, so it keeps the one-line label row instead of the Version spinner.
 */
export function isAgentUpdating(a: DeviceActivity | undefined): a is DeviceActivity {
  return a?.action === 'agent_update' && (a.kind === 'host' || a.kind === 'agent') && !isReinstallActivity(a)
}

/** The version an agent update moves to, or '' when neither the record nor the server knows it. */
export function agentUpdateTarget(a: DeviceActivity, newestAgentVersion?: string): string {
  const self = selfUpdateVersionsOf(a)
  if (self) return self.to
  return a.agent_from_version && newestAgentVersion ? newestAgentVersion : ''
}

/** "by ryan · started 3m ago" is shown beside every label except powered off, whose label already names the actor. */
export function showsActorLine(a: DeviceActivity): boolean {
  return a.kind !== 'powered_off'
}

export function isEscalated(a: DeviceActivity | undefined): boolean {
  return a?.kind === 'escalated'
}

/**
 * True when the device is offline and that is expected (FR-038): powered off on
 * purpose, or offline inside a host or agent window that covers offline. An
 * escalated device is a real outage and stays in the offline count. The device
 * status comes from the device list, the same source as the status dot.
 */
export function isExpectedOffline(a: DeviceActivity | undefined, status: Device['status']): boolean {
  if (!a || status !== 'offline') return false
  if (a.kind === 'powered_off') return true
  return (a.kind === 'host' || a.kind === 'agent') && a.covers_offline
}

export type ActivityLink = { type: 'route'; to: string } | { type: 'modal' } | null

/**
 * Where the device page sends the operator for this command (X-3). Docker
 * actions have an activity page; host and agent actions have no detail page, so
 * they open the command in a modal. The URL parts are encoded.
 */
export function activityLink(a: DeviceActivity): ActivityLink {
  if (!a.command_id) return null
  if (a.kind === 'docker' || a.action.startsWith('docker_')) {
    return {
      type: 'route',
      to: `/docker/activity?host=${encodeURIComponent(a.device_id)}&command=${encodeURIComponent(a.command_id)}`,
    }
  }
  return { type: 'modal' }
}

export function indexActivities(response: ActivityResponse | undefined): Map<string, DeviceActivity> {
  const byDevice = new Map<string, DeviceActivity>()
  for (const a of response?.activities ?? []) byDevice.set(a.device_id, a)
  return byDevice
}

/**
 * Applies one `activity_update` WebSocket message to the cached response:
 * data replaces the device's entry (or adds one), null clears it. A message
 * whose payload names a different device than its envelope is ignored, and so
 * is any message before the first fetch has landed (that fetch is current).
 */
export function applyActivityUpdate(
  old: ActivityResponse | undefined,
  deviceId: string | undefined,
  data: DeviceActivity | null | undefined,
): ActivityResponse | undefined {
  if (!old || !deviceId) return old
  if (data != null && data.device_id !== deviceId) return old
  // A Go nil slice marshals as null, so an empty list may arrive as null.
  const rest = (old.activities ?? []).filter(a => a.device_id !== deviceId)
  return { ...old, activities: data ? [...rest, data] : rest }
}

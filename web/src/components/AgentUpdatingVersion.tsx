import type { DeviceActivity } from '../types/models'
import { Spinner } from './ui'
import { useTimeFormatters } from '../contexts/TimePreferenceProvider'
import { activityLabel, agentUpdateTarget } from '../utils/activity'

interface AgentUpdatingVersionProps {
  activity: DeviceActivity
  newestAgentVersion?: string
}

/**
 * The fleet Version cell while an agent update runs: a spinner, the version it is
 * moving to and "(updating)". The full label, actor and start time are in the
 * hover title so the row stays one line tall. Every string is React text.
 */
export default function AgentUpdatingVersion({ activity, newestAgentVersion }: AgentUpdatingVersionProps) {
  const { timestamp } = useTimeFormatters()
  // Clock times in activity text carry a zone label (TIME-12H-001 DEC-9).
  const formatTime = (iso: string) => timestamp(iso, 'clock')
  const label = activityLabel(activity, { newestAgentVersion, formatTime })
  const target = agentUpdateTarget(activity, newestAgentVersion)
  const state = activity.phase === 'queued' ? 'queued' : 'updating'
  const title = `${label} ${activity.actor_phrase} · started ${formatTime(activity.started_at)}`

  return (
    <span className="inline-flex items-center gap-1.5 text-info-fg" title={title} data-testid="agent-updating">
      <Spinner className="h-3.5 w-3.5 flex-shrink-0" />
      {target && <span>{target}</span>}
      <span className="text-xs italic text-gray-500">({state})</span>
    </span>
  )
}

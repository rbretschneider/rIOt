import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { DeviceActivity } from '../types/models'
import { Modal, RelativeTime, Spinner } from './ui'
import CommandProgress from './docker/CommandProgress'
import { useTimeFormatters } from '../contexts/TimePreferenceProvider'
import { activityLabel, activityLink, isEscalated, showsActorLine } from '../utils/activity'

interface ActivityLabelProps {
  activity: DeviceActivity
  newestAgentVersion?: string
  /** Show the link to the command (device page, FR-039). The fleet row shows the label only. */
  showLink?: boolean
  /**
   * One line for the fleet table: a spinner and the label, with the actor and
   * start time in the hover title so a long actor name cannot make the row tall.
   */
  compact?: boolean
}

/**
 * The activity label with its actor phrase and start time (FR-034, FR-036).
 * Every string comes from the server as data and is rendered as React text; the
 * command link is built with encodeURIComponent and there is no raw HTML.
 */
export default function ActivityLabel({ activity, newestAgentVersion, showLink = false, compact = false }: ActivityLabelProps) {
  const { timestamp } = useTimeFormatters()
  const [modalOpen, setModalOpen] = useState(false)

  // Clock times in activity text carry a zone label (TIME-12H-001 DEC-9).
  const formatTime = (iso: string) => timestamp(iso, 'clock')
  const label = activityLabel(activity, { newestAgentVersion, formatTime })
  const link = showLink ? activityLink(activity) : null
  const failed = isEscalated(activity)

  if (compact) {
    const title = showsActorLine(activity)
      ? `${label} ${activity.actor_phrase} · started ${formatTime(activity.started_at)}`
      : label
    return (
      <p
        className="flex min-w-0 items-center gap-1.5 text-xs font-medium"
        title={title}
        data-testid="activity-label"
        data-activity-kind={activity.kind}
      >
        {!failed && activity.kind !== 'powered_off' && <Spinner className="h-3 w-3 flex-shrink-0 text-info-fg" />}
        <span className={`truncate ${failed ? 'text-danger-fg' : 'text-info-fg'}`}>{label}</span>
      </p>
    )
  }

  return (
    <div className="min-w-0 text-xs" data-testid="activity-label" data-activity-kind={activity.kind}>
      <p className={`font-medium ${failed ? 'text-danger-fg' : 'text-info-fg'}`}>{label}</p>
      {showsActorLine(activity) && (
        <p className="text-text-muted">
          {activity.actor_phrase} &middot; started <RelativeTime value={activity.started_at} pastOnly />
        </p>
      )}
      {link?.type === 'route' && (
        <Link className="text-link" to={link.to}>
          View command
        </Link>
      )}
      {link?.type === 'modal' && (
        <>
          <button type="button" className="text-link hover:underline" onClick={() => setModalOpen(true)}>
            View command
          </button>
          <Modal open={modalOpen} title={label} size="md" onClose={() => setModalOpen(false)}>
            <div className="max-h-[60vh] overflow-y-auto scrollbar-thin">
              <CommandProgress
                deviceId={activity.device_id}
                commandId={activity.command_id}
                showActivityLink={false}
              />
            </div>
          </Modal>
        </>
      )}
    </div>
  )
}

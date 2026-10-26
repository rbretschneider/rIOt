import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { DeviceActivity } from '../types/models'
import { Modal, RelativeTime } from './ui'
import CommandProgress from './docker/CommandProgress'
import { useTimePreference } from '../contexts/TimePreferenceProvider'
import { activityLabel, activityLink, isEscalated, showsActorLine } from '../utils/activity'
import { formatChartTick, parseInstant } from '../utils/format'

interface ActivityLabelProps {
  activity: DeviceActivity
  newestAgentVersion?: string
  /** Show the link to the command (device page, FR-039). The fleet row shows the label only. */
  showLink?: boolean
}

/**
 * The activity label with its actor phrase and start time (FR-034, FR-036).
 * Every string comes from the server as data and is rendered as React text; the
 * command link is built with encodeURIComponent and there is no raw HTML.
 */
export default function ActivityLabel({ activity, newestAgentVersion, showLink = false }: ActivityLabelProps) {
  const { zoneOpts } = useTimePreference()
  const [modalOpen, setModalOpen] = useState(false)

  const formatTime = (iso: string) => {
    const instant = parseInstant(iso)
    return instant ? formatChartTick(instant.getTime(), zoneOpts) : '—'
  }
  const label = activityLabel(activity, { newestAgentVersion, formatTime })
  const link = showLink ? activityLink(activity) : null
  const failed = isEscalated(activity)

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

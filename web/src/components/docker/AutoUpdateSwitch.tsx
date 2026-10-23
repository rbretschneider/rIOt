import { useId } from 'react'
import { Link } from 'react-router-dom'
import { Spinner } from '../ui'

export const WINDOW_DISABLED_NOTICE =
  'Auto-update is on, but the Docker update window is disabled.'

/**
 * One-click auto-update switch for a table row. It only presents state: the
 * caller decides what a flip saves.
 */
export default function AutoUpdateSwitch({
  checked,
  pending = false,
  label,
  lockedBy,
  disabledReason,
  onDisabledReason,
  windowDisabled = false,
  onChange,
}: {
  checked: boolean
  pending?: boolean
  /** Accessible name, e.g. "Auto-update web on host-one". */
  label: string
  /** The stack whose policy decides instead; the switch is then read-only. */
  lockedBy?: { project: string; to?: string }
  /** Blocks both directions, e.g. while the state is unknown. */
  disabledReason?: string
  /** Blocks turning on only; turning off stays possible. */
  onDisabledReason?: string
  /** The Docker update window is disabled, so an on switch never fires. */
  windowDisabled?: boolean
  onChange: (next: boolean) => void
}) {
  const noticeId = useId()
  const blockedOn = !checked && !!onDisabledReason
  const disabled = pending || !!lockedBy || !!disabledReason || blockedOn
  const reason = disabledReason ?? (blockedOn ? onDisabledReason : undefined)
  const notice = checked && windowDisabled
  return (
    <span className="inline-flex min-w-0 items-center gap-2 text-xs">
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        aria-busy={pending || undefined}
        aria-readonly={lockedBy ? true : undefined}
        aria-describedby={notice ? noticeId : undefined}
        title={reason}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={`relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${checked ? 'bg-info' : 'bg-raised-2 border border-border'}`}
      >
        <span
          className={`inline-block h-3 w-3 rounded-full bg-white transition-transform ${checked ? 'translate-x-3.5' : 'translate-x-0.5'}`}
        />
      </button>
      {pending && <Spinner className="h-3 w-3 text-text-muted" />}
      {lockedBy && (
        <span className="truncate text-text-muted">
          Controlled by stack{' '}
          {lockedBy.to ? (
            <Link className="text-link" to={lockedBy.to}>
              {lockedBy.project}
            </Link>
          ) : (
            lockedBy.project
          )}
        </span>
      )}
      {!lockedBy && reason && (
        <span className="truncate text-text-muted" title={reason}>
          {reason}
        </span>
      )}
      {notice && (
        <span className="truncate text-warn-fg" title={WINDOW_DISABLED_NOTICE}>
          <span id={noticeId} className="sr-only">
            {WINDOW_DISABLED_NOTICE}
          </span>
          <span aria-hidden="true">Window disabled · </span>
          <Link className="text-link" to="/settings/agents">
            Settings &gt; Agents
          </Link>
        </span>
      )}
    </span>
  )
}

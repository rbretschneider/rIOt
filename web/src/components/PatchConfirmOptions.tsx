import { useId } from 'react'
import type { UpdateInfo } from '../types/models'
import { REBOOT_OPTION_LABEL, patchDialogStatements, rebootOption } from '../utils/patchFlow'

interface PatchConfirmOptionsProps {
  /** The host's latest update telemetry; undefined when none was reported. */
  updates: UpdateInfo | undefined
  checked: boolean
  onChange: (checked: boolean) => void
}

/**
 * Body of the manual Patch confirmation (PATCH-FLOW-001): the opt-in
 * "Reboot automatically if the patch requires it" box, the reason when it is
 * unavailable, and what the run will do about pending kernel/GPU updates.
 * The checked state belongs to the caller, which resets it on every open.
 */
export function PatchConfirmOptions({ updates, checked, onChange }: PatchConfirmOptionsProps) {
  const option = rebootOption(updates)
  const ticked = checked && option.enabled
  const statements = patchDialogStatements(updates, ticked)
  const reasonId = useId()

  return (
    <div className="mt-3 space-y-3 text-sm">
      <div>
        <label className={`flex items-center gap-2 ${option.enabled ? 'cursor-pointer' : 'cursor-not-allowed opacity-70'}`}>
          <input
            type="checkbox"
            checked={ticked}
            disabled={!option.enabled}
            aria-describedby={option.disabledReason ? reasonId : undefined}
            onChange={event => onChange(event.target.checked)}
          />
          <span>{REBOOT_OPTION_LABEL}</span>
        </label>
        {option.disabledReason && (
          <p id={reasonId} className="mt-1 text-xs text-text-muted">
            {option.disabledReason}
          </p>
        )}
      </div>
      {statements.length > 0 && (
        <ul className="max-h-40 list-disc space-y-1 overflow-y-auto pl-5 text-text-muted scrollbar-thin">
          {statements.map(statement => (
            <li key={statement}>{statement}</li>
          ))}
        </ul>
      )}
    </div>
  )
}

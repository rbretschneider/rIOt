import type { DevicePatchInfo } from '../api/client'

// REBOOT-ETA-001: one pure helper maps the patch-status row to the reboot-ETA
// tooltip, shared by the Fleet Overview grid indicator and the Pending OS
// Patches badge so both show byte-identical text. The caller renders the
// indicator only when reboot_required is true. Output is plain text for a
// native title attribute — never markup.

export type RebootEtaInfo = Pick<
  DevicePatchInfo,
  'os_patch_mode' | 'next_window_start' | 'os_patch_cooldown_minutes' | 'allow_reboot'
> &
  // Optional so callers and tests may omit it; a missing/zero count means no
  // reboot-class update is pending (⇒ standing manual reboot, never a window ETA).
  Partial<Pick<DevicePatchInfo, 'reboot_class_count'>>

const REBOOT_REQUIRED_NO_TIME = 'A reboot is required to activate the installed updates.'

const MANUAL_REBOOT =
  'Manual reboot required — this device does not permit automatic reboot. Reboot it manually to activate the installed updates.'

// A reboot is only ever applied automatically as part of installing a PENDING
// reboot-class (kernel / GPU-driver) update during a patch run. When none is
// pending, the reboot requirement comes from an already-installed update and no
// patch run will clear it — it must be rebooted manually.
const STANDING_MANUAL_REBOOT =
  'Reboot required — reboot manually to activate. No reboot-class update is pending, so no automatic reboot is scheduled.'

const ESTIMATE_CAVEAT =
  "estimate, not a guarantee. Depends on reboot-class gating, the cooldown, and the device's allow_reboot / hold_reboot_class settings."

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

/**
 * Formats an ISO instant as an absolute UTC string (YYYY-MM-DD HH:MM UTC).
 * UTC getters only — never converts to local time. Returns null for an
 * unparseable input so callers can fall back honestly instead of rendering a
 * partial or placeholder time.
 */
export function formatUtc(iso: string): string | null {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  return (
    `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())} ` +
    `${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())} UTC`
  )
}

/** Resolves the reboot-ETA tooltip text; first matching rule wins. */
export function rebootEtaTooltip(info: RebootEtaInfo): string {
  if (info.allow_reboot === false) return MANUAL_REBOOT

  // An automatic reboot only happens while INSTALLING a pending reboot-class
  // (kernel / GPU-driver) update. With none pending, the requirement is
  // standing and no patch run will clear it — so the window/anytime ETA
  // branches below only apply when a reboot-class update is actually pending;
  // otherwise it is a manual reboot regardless of the window configuration.
  const hasPendingRebootClass = !!info.reboot_class_count

  if (info.os_patch_mode === 'anytime') {
    if (!hasPendingRebootClass) return STANDING_MANUAL_REBOOT
    const cooldown = info.os_patch_cooldown_minutes
    const within = cooldown ? ` (within the ~${cooldown} min cooldown)` : ''
    return (
      `Reboot required. Will occur on the next automatic patch run${within}. ` +
      `Estimate, not a guarantee; depends on reboot-class gating, the cooldown, and the device's allow_reboot / hold_reboot_class settings.`
    )
  }

  if (info.os_patch_mode === 'window' && info.next_window_start) {
    const when = formatUtc(info.next_window_start)
    if (when) {
      if (!hasPendingRebootClass) return STANDING_MANUAL_REBOOT
      if (info.allow_reboot === true) {
        return `Est. reboot: ${when} — ${ESTIMATE_CAVEAT}`
      }
      return `Est. reboot: ${when} — if this device permits automatic reboot; ${ESTIMATE_CAVEAT}`
    }
  }

  return REBOOT_REQUIRED_NO_TIME
}

import type { DevicePatchInfo } from '../api/client'

// REBOOT-ETA-001: one pure helper maps the patch-status row to the reboot-ETA
// tooltip, shared by the Fleet Overview grid indicator and the Pending OS
// Patches badge so both show byte-identical text. The caller renders the
// indicator only when reboot_required is true. Output is plain text for a
// native title attribute — never markup.

export type RebootEtaInfo = Pick<
  DevicePatchInfo,
  'os_patch_mode' | 'next_window_start' | 'os_patch_cooldown_minutes' | 'allow_reboot'
>

const REBOOT_REQUIRED_NO_TIME = 'A reboot is required to activate the installed updates.'

const MANUAL_REBOOT =
  'Manual reboot required — this device does not permit automatic reboot. Reboot it manually to activate the installed updates.'

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

  if (info.os_patch_mode === 'anytime') {
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
      if (info.allow_reboot === true) {
        return `Est. reboot: ${when} — ${ESTIMATE_CAVEAT}`
      }
      return `Est. reboot: ${when} — if this device permits automatic reboot; ${ESTIMATE_CAVEAT}`
    }
  }

  return REBOOT_REQUIRED_NO_TIME
}

import type { ExtraUpdater, UpdateInfo } from '../types/models'

// EXTRA-UPD-001: pure helpers for extra updaters (DietPi first). They take only data the
// pages already load and never fetch or render. Every string shown comes from the fixed
// tables below or from a bounded server field; nothing is interpreted as HTML.

/** Label of the per-run box in the Patch dialog (FR-056). */
export const EXTRA_OPTION_LABEL = 'Apply DietPi update in this run'

/** The step limit the agent enforces on one extra-updater step (FR-030), in words. */
const STEP_LIMIT_WORDS = '40 minutes'

const NAMES: Record<string, string> = { dietpi: 'DietPi' }

/** The name an updater goes by in text; an unknown identifier is shown as sent. */
export function extraName(id: string): string {
  return NAMES[id] ?? id
}

export function isPending(e: ExtraUpdater): boolean {
  return e.status === 'update_available'
}

export function isEnabled(e: ExtraUpdater): boolean {
  return e.readiness === 'enabled'
}

/** Updaters a Patch run would apply: update waiting and the agent says it would apply it. */
export function enabledPendingExtras(updates: UpdateInfo | undefined): ExtraUpdater[] {
  return (updates?.extra_updaters ?? []).filter(e => isPending(e) && isEnabled(e))
}

const UNKNOWN_REASONS: Record<string, string> = {
  check_disabled: 'its own update check is turned off',
  config_unreadable: 'its settings file could not be read',
  not_checked_since_boot: 'it has not run its update check since the host booted',
  version_unreadable: 'the installed version could not be read',
  detection_failed: 'detection did not finish',
}

/** The unknown reason in words (FR-054). */
export function unknownReasonWords(reason: string | undefined): string {
  return (reason && UNKNOWN_REASONS[reason]) || 'the reason was not reported'
}

const NOT_APPLIED_REASONS: Record<string, string> = {
  not_opted_in: 'it is not opted in (commands.allow_extra_updaters and the root-side opt-in)',
  root_side_missing: 'the root-side opt-in is missing (re-run install.sh as root on this host)',
  patching_disabled: 'patching is turned off on the agent (commands.allow_patching)',
  unsupported_helper: 'the installed maintenance helper is too old (re-run install.sh as root)',
  non_default_update_source: 'its update source is not the default MichaIng repository',
}

/** Why a pending updater would not be applied, in words (FR-058). */
export function notAppliedWords(readiness: string | undefined): string {
  return (readiness && NOT_APPLIED_REASONS[readiness]) || 'its apply readiness is not known'
}

export function statusWords(e: ExtraUpdater): string {
  switch (e.status) {
    case 'update_available':
      return 'Update available'
    case 'up_to_date':
      return 'Up to date'
    default:
      return `Unknown: ${unknownReasonWords(e.unknown_reason)}`
  }
}

/** One notice in words (FR-054). */
export function noticeWords(n: { code: string; branch?: string; owner?: string }): string {
  switch (n.code) {
    case 'live_patches_available':
      return 'Live patches are available.'
    case 'dietpi_auto_apt_upgrade':
      return 'DietPi upgrades apt packages by itself (CONFIG_CHECK_APT_UPDATES=2); rIOt only reports this.'
    case 'non_default_update_source':
      return `Its update source is ${n.owner ?? 'unreadable'}/${n.branch ?? 'unreadable'}, not MichaIng/master; rIOt will not apply it.`
    case 'major_upgrade':
      return 'This is a major upgrade: the kernel or firmware may change and a reboot is likely to be needed.'
    default:
      return 'A notice this dashboard does not know.'
  }
}

/** "9.20.1 to 10.8.2", or the part that is known. */
export function versionChange(e: ExtraUpdater): string {
  if (e.installed_version && e.available_version) return `${e.installed_version} to ${e.available_version}`
  return e.available_version ? `update to ${e.available_version}` : 'update'
}

export interface ExtraDialogState {
  /** Updaters that appear as a per-run box (pending and enabled). */
  options: ExtraUpdater[]
  /** Statements for the run, depending on whether the box is ticked. */
  statements: string[]
}

/**
 * What the Patch dialog says about extra updaters (FR-056 to FR-058). `ticked` is the
 * state of the "Apply DietPi update in this run" box; it only matters for pending,
 * enabled updaters. Pending updaters that would not be applied, and unknown ones, get
 * a "will not be applied" statement and no box.
 */
export function extraDialogState(updates: UpdateInfo | undefined, ticked: boolean): ExtraDialogState {
  const statements: string[] = []
  const options = enabledPendingExtras(updates)
  for (const e of updates?.extra_updaters ?? []) {
    const name = extraName(e.id)
    if (isPending(e) && isEnabled(e)) {
      if (!ticked) {
        statements.push(`${name} ${versionChange(e)} will not be applied in this run: you unticked "${EXTRA_OPTION_LABEL}".`)
        continue
      }
      statements.push(`${name} ${versionChange(e)} will be applied after the apt packages. It may take up to ${STEP_LIMIT_WORDS}.`)
      statements.push(`${name} stops and restarts the services it manages during the run (for example CUPS or VPN software), and runs apt-get autopurge, which removes packages apt considers no longer needed.`)
      if (e.notices?.some(n => n.code === 'major_upgrade')) {
        statements.push(`${name} is a major upgrade: the kernel or firmware may change and a reboot is likely to be needed. Tick "Reboot automatically if the patch requires it" to let rIOt reboot afterwards.`)
      }
    } else if (isPending(e)) {
      statements.push(`${name} ${versionChange(e)} will not be applied in this run: ${notAppliedWords(e.readiness)}.`)
    } else if (e.status === 'unknown') {
      statements.push(`${name} will not be applied in this run: its update status is unknown (${unknownReasonWords(e.unknown_reason)}).`)
    }
  }
  return { options, statements }
}

/**
 * Parameters of a manual os_update (FR-050, FR-056). The exclusion is sent only when the
 * dialog offered the box and it was unticked; nothing else changes the request.
 */
export function osUpdateParams(
  updates: UpdateInfo | undefined,
  choices: { rebootTicked: boolean; rebootAllowed: boolean; extrasTicked: boolean },
): Record<string, unknown> {
  const params: Record<string, unknown> = { mode: 'full' }
  if (choices.rebootTicked && choices.rebootAllowed) {
    params.reboot_if_required = true
  }
  if (enabledPendingExtras(updates).length > 0 && !choices.extrasTicked) {
    params.exclude_extra_updaters = true
  }
  return params
}

/** Pending extra updaters named for a list row: "DietPi 10.8.2" (FR-053). */
export function pendingExtraNames(extras: ExtraUpdater[] | undefined): string[] {
  return (extras ?? []).filter(isPending).map(e => (e.available_version ? `${extraName(e.id)} ${e.available_version}` : extraName(e.id)))
}

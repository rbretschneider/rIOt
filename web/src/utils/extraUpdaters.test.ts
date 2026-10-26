import { describe, expect, it } from 'vitest'
import type { ExtraUpdater, UpdateInfo } from '../types/models'
import { devicePatchState, isNeedsYou } from './patchFlow'
import {
  EXTRA_OPTION_LABEL,
  enabledPendingExtras,
  extraDialogState,
  noticeWords,
  osUpdateParams,
  pendingExtraNames,
  statusWords,
} from './extraUpdaters'

function dietpi(over: Partial<ExtraUpdater> = {}): ExtraUpdater {
  return {
    id: 'dietpi',
    status: 'update_available',
    installed_version: '9.20.1',
    available_version: '10.8.2',
    pending_count: 1,
    readiness: 'enabled',
    ...over,
  }
}

function updates(over: Partial<UpdateInfo> = {}): UpdateInfo {
  return {
    package_manager: 'apt',
    total_installed: 300,
    pending_updates: 0,
    pending_security_count: 0,
    pending_kernel_update: false,
    unattended_upgrades: false,
    ...over,
  } as UpdateInfo
}

describe('[EXTRA-UPD-001 AC-039] the Patch button names a pending, enabled updater', () => {
  it('reads "Patch (3 + DietPi)" with apt updates and DietPi pending and enabled', () => {
    const state = devicePatchState(updates({ pending_updates: 3, extra_updaters: [dietpi()] }))
    expect(state.patchLabel).toBe('Patch (3 + DietPi)')
    expect(state.showPatch).toBe(true)
    expect(state.primary).toBe('patch')
  })

  it('reads "Patch (DietPi)" when only DietPi is pending and enabled', () => {
    const state = devicePatchState(updates({ extra_updaters: [dietpi()] }))
    expect(state.patchLabel).toBe('Patch (DietPi)')
    expect(state.showPatch).toBe(true)
    expect(state.actionablePending).toBe(0)
  })

  it('keeps "Patch (3)" when the updater is not enabled, unknown or up to date', () => {
    for (const extra of [dietpi({ readiness: 'not_opted_in' }), dietpi({ status: 'unknown' }), dietpi({ status: 'up_to_date' })]) {
      expect(devicePatchState(updates({ pending_updates: 3, extra_updaters: [extra] })).patchLabel).toBe('Patch (3)')
    }
  })

  it('shows no Patch button when only a non-enabled updater is pending', () => {
    const state = devicePatchState(updates({ extra_updaters: [dietpi({ readiness: 'root_side_missing' })] }))
    expect(state.showPatch).toBe(false)
    expect(state.patchLabel).toBeNull()
  })

  it('leaves a host without an updater list exactly as before', () => {
    expect(devicePatchState(updates({ pending_updates: 3 })).patchLabel).toBe('Patch (3)')
    expect(devicePatchState(updates()).showPatch).toBe(false)
  })

  it('still shows the stale label first when the lists are stale', () => {
    const state = devicePatchState(updates({ lists_freshness: 'stale', extra_updaters: [dietpi()] }))
    expect(state.patchLabel).toMatch(/out of date/)
  })
})

describe('[EXTRA-UPD-001 AC-040] the dialog statements', () => {
  it('states the version change, the order, the limit, the service stop and the autopurge', () => {
    const { options, statements } = extraDialogState(updates({ extra_updaters: [dietpi()] }), true)
    expect(options).toHaveLength(1)
    const text = statements.join('\n')
    expect(text).toContain('DietPi 9.20.1 to 10.8.2')
    expect(text).toContain('after the apt packages')
    expect(text).toContain('40 minutes')
    expect(text).toContain('stops and restarts the services it manages')
    expect(text).toContain('apt-get autopurge')
    expect(text).not.toContain('major upgrade')
  })

  it('points a major upgrade at the reboot checkbox', () => {
    const { statements } = extraDialogState(updates({ extra_updaters: [dietpi({ notices: [{ code: 'major_upgrade' }] })] }), true)
    const text = statements.join('\n')
    expect(text).toContain('kernel or firmware may change')
    expect(text).toContain('Tick "Reboot automatically if the patch requires it"')
  })

  it('says DietPi is skipped when the box is unticked', () => {
    const { statements } = extraDialogState(updates({ extra_updaters: [dietpi()] }), false)
    expect(statements).toEqual([`DietPi 9.20.1 to 10.8.2 will not be applied in this run: you unticked "${EXTRA_OPTION_LABEL}".`])
  })

  it('says why DietPi will not be applied when it is pending but not enabled, with no box', () => {
    const { options, statements } = extraDialogState(updates({ extra_updaters: [dietpi({ readiness: 'not_opted_in' })] }), true)
    expect(options).toHaveLength(0)
    expect(statements).toHaveLength(1)
    expect(statements[0]).toContain('will not be applied in this run')
    expect(statements[0]).toContain('not opted in')
  })

  it('says why DietPi will not be applied when its status is unknown, with no box', () => {
    const { options, statements } = extraDialogState(updates({ extra_updaters: [dietpi({ status: 'unknown', unknown_reason: 'check_disabled' })] }), true)
    expect(options).toHaveLength(0)
    expect(statements[0]).toContain('will not be applied in this run')
    expect(statements[0]).toContain('its own update check is turned off')
  })

  it('says nothing for an up to date updater or a host without the list', () => {
    expect(extraDialogState(updates({ extra_updaters: [dietpi({ status: 'up_to_date' })] }), true).statements).toEqual([])
    expect(extraDialogState(updates(), true)).toEqual({ options: [], statements: [] })
    expect(extraDialogState(undefined, true)).toEqual({ options: [], statements: [] })
  })
})

describe('[EXTRA-UPD-001 AC-040] the os_update parameters', () => {
  const choices = { rebootTicked: false, rebootAllowed: true, extrasTicked: true }

  it('sends only the mode when the box stays ticked', () => {
    expect(osUpdateParams(updates({ extra_updaters: [dietpi()] }), choices)).toEqual({ mode: 'full' })
  })

  it('sends the exclusion when the box is unticked', () => {
    expect(osUpdateParams(updates({ extra_updaters: [dietpi()] }), { ...choices, extrasTicked: false })).toEqual({ mode: 'full', exclude_extra_updaters: true })
  })

  it('never sends the exclusion when no box was offered', () => {
    expect(osUpdateParams(updates({ pending_updates: 3 }), { ...choices, extrasTicked: false })).toEqual({ mode: 'full' })
    expect(osUpdateParams(updates({ extra_updaters: [dietpi({ readiness: 'not_opted_in' })] }), { ...choices, extrasTicked: false })).toEqual({ mode: 'full' })
  })

  it('keeps the reboot authorization independent of the exclusion', () => {
    expect(osUpdateParams(updates({ extra_updaters: [dietpi()] }), { rebootTicked: true, rebootAllowed: true, extrasTicked: false }))
      .toEqual({ mode: 'full', reboot_if_required: true, exclude_extra_updaters: true })
    expect(osUpdateParams(updates(), { rebootTicked: true, rebootAllowed: false, extrasTicked: true })).toEqual({ mode: 'full' })
  })
})

describe('[EXTRA-UPD-001 AC-038 AC-054] helpers for lists and words', () => {
  it('names pending updaters with the available version for the fleet panel', () => {
    expect(pendingExtraNames([dietpi()])).toEqual(['DietPi 10.8.2'])
    expect(pendingExtraNames([dietpi({ status: 'unknown' }), dietpi({ id: 'snap', available_version: undefined })])).toEqual(['snap'])
    expect(pendingExtraNames(undefined)).toEqual([])
  })

  it('selects only pending and enabled updaters', () => {
    expect(enabledPendingExtras(updates({ extra_updaters: [dietpi(), dietpi({ id: 'a', readiness: 'not_opted_in' }), dietpi({ id: 'b', status: 'unknown' })] })).map(e => e.id)).toEqual(['dietpi'])
  })

  it('puts the unknown reason in words and never says up to date for unknown', () => {
    const words = statusWords(dietpi({ status: 'unknown', unknown_reason: 'not_checked_since_boot' }))
    expect(words).toContain('has not run its update check since the host booted')
    expect(words.toLowerCase()).not.toContain('up to date')
  })

  it('puts each notice in words, and treats a notice it does not know as unknown text', () => {
    expect(noticeWords({ code: 'live_patches_available' })).toMatch(/Live patches/)
    expect(noticeWords({ code: 'dietpi_auto_apt_upgrade' })).toMatch(/CONFIG_CHECK_APT_UPDATES=2/)
    expect(noticeWords({ code: 'non_default_update_source', owner: 'x', branch: 'beta' })).toContain('x/beta')
    expect(noticeWords({ code: 'major_upgrade' })).toMatch(/major upgrade/)
    expect(noticeWords({ code: '<img src=x>' })).toBe('A notice this dashboard does not know.')
  })
})

describe('[EXTRA-UPD-001 AC-050] only needs_you counts', () => {
  it('treats manual, the automatic kinds, unknown kinds and absence as not needing action', () => {
    expect(isNeedsYou('needs_you')).toBe(true)
    for (const kind of ['manual', 'auto', 'auto_reboot_date', 'auto_reboot_unknown', 'something_new', undefined]) {
      expect(isNeedsYou(kind), String(kind)).toBe(false)
    }
  })
})

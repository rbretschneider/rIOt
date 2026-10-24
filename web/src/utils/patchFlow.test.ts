import { describe, expect, it } from 'vitest'
import type { UpdateInfo } from '../types/models'
import {
  STALE_PATCH_LABEL,
  devicePatchState,
  normalizeFreshness,
  patchDialogStatements,
  rebootOption,
  staleSecurityNote,
} from './patchFlow'

function updates(over: Partial<UpdateInfo> = {}): UpdateInfo {
  return {
    total_installed: 100,
    pending_updates: 0,
    pending_security_count: 0,
    pending_kernel_update: false,
    unattended_upgrades: false,
    allow_reboot: true,
    supports_reboot_if_required: true,
    ...over,
  } as UpdateInfo
}

describe('[PATCH-FLOW-001 AC-029] normalizeFreshness', () => {
  it.each([
    ['fresh', 'fresh'],
    ['stale', 'stale'],
    ['unknown', 'unknown'],
    [undefined, 'unknown'],
    [null, 'unknown'],
    ['bogus', 'unknown'],
    ['STALE', 'unknown'],
    [42, 'unknown'],
    [{ a: 1 }, 'unknown'],
  ])('maps %j to %s', (input, expected) => {
    expect(normalizeFreshness(input)).toBe(expected)
  })
})

describe('[PATCH-FLOW-001 AC-023, AC-027] devicePatchState follows the FRD section 7 table', () => {
  it('S1: fresh, nothing pending, no reboot required: no action', () => {
    expect(devicePatchState(updates())).toMatchObject({ primary: 'none', showPatch: false, patchLabel: null })
  })

  it('S2: pending packages show Patch (N)', () => {
    expect(devicePatchState(updates({ pending_updates: 7, lists_freshness: 'fresh' }))).toMatchObject({
      primary: 'patch', showPatch: true, patchLabel: 'Patch (7)', actionablePending: 7,
    })
  })

  it('S3: pending kernel/GPU packages still show Patch (N)', () => {
    expect(devicePatchState(updates({ pending_updates: 7, pending_reboot_class_count: 1 }))).toMatchObject({
      primary: 'patch', patchLabel: 'Patch (7)',
    })
  })

  it('S4: only a reboot is needed: Reboot is primary and Patch is hidden', () => {
    expect(devicePatchState(updates({ reboot_required: true }))).toMatchObject({
      primary: 'reboot', showPatch: false, patchLabel: null, rebootDisabledReason: null,
    })
  })

  it('S4 with allow_reboot=false: Reboot is primary but disabled with the outside-rIOt reason', () => {
    const state = devicePatchState(updates({ reboot_required: true, allow_reboot: false }))
    expect(state.primary).toBe('reboot')
    expect(state.rebootDisabledReason).toContain('commands.allow_reboot: false')
    expect(state.rebootDisabledReason).toContain('outside rIOt')
  })

  it('S4 with an older agent (allow_reboot unreported): Reboot stays usable', () => {
    expect(devicePatchState(updates({ reboot_required: true, allow_reboot: undefined })).rebootDisabledReason).toBeNull()
  })

  it('S5: pending packages and a required reboot: Patch is primary', () => {
    expect(devicePatchState(updates({ pending_updates: 4, reboot_required: true }))).toMatchObject({
      primary: 'patch', patchLabel: 'Patch (4)', rebootRequired: true,
    })
  })

  it('S6: stale lists replace the count with the stale label and keep Patch usable', () => {
    expect(devicePatchState(updates({ pending_updates: 38, lists_freshness: 'stale' }))).toMatchObject({
      primary: 'patch', showPatch: true, patchLabel: STALE_PATCH_LABEL, actionablePending: 0,
    })
  })

  it('S6: stale lists show the label even with a zero raw count', () => {
    expect(devicePatchState(updates({ pending_updates: 0, lists_freshness: 'stale' })).patchLabel).toBe(STALE_PATCH_LABEL)
  })

  it('S6: stale lists beat a required reboot as the primary action (Patch refreshes first)', () => {
    expect(devicePatchState(updates({ reboot_required: true, lists_freshness: 'stale' })).primary).toBe('patch')
  })

  it('unknown or absent freshness keeps the count display', () => {
    expect(devicePatchState(updates({ pending_updates: 5, lists_freshness: 'unknown' })).patchLabel).toBe('Patch (5)')
    expect(devicePatchState(updates({ pending_updates: 5 })).patchLabel).toBe('Patch (5)')
  })

  it('no updates telemetry: nothing to do', () => {
    expect(devicePatchState(undefined)).toMatchObject({ primary: 'none', showPatch: false, freshness: 'unknown' })
  })
})

describe('[PATCH-FLOW-001 AC-008, AC-009] rebootOption', () => {
  it('is enabled when allow_reboot is true and the capability is reported', () => {
    expect(rebootOption(updates())).toEqual({ enabled: true, disabledReason: null })
  })

  it('is disabled with the allow_reboot reason when allow_reboot is false', () => {
    const option = rebootOption(updates({ allow_reboot: false }))
    expect(option.enabled).toBe(false)
    expect(option.disabledReason).toContain('Reboots are turned off on this agent (commands.allow_reboot: false)')
  })

  it('allow_reboot=false wins over a missing capability', () => {
    expect(rebootOption(updates({ allow_reboot: false, supports_reboot_if_required: undefined })).disabledReason)
      .toContain('commands.allow_reboot: false')
  })

  it('is disabled with the update-the-agent reason when allow_reboot is not reported', () => {
    expect(rebootOption(updates({ allow_reboot: undefined, supports_reboot_if_required: undefined }))).toEqual({
      enabled: false, disabledReason: 'The agent must be updated to use this option.',
    })
  })

  it('is disabled when allow_reboot is true but the capability is missing or not exactly true', () => {
    expect(rebootOption(updates({ supports_reboot_if_required: undefined })).enabled).toBe(false)
    expect(rebootOption(updates({ supports_reboot_if_required: 'true' as unknown as boolean })).enabled).toBe(false)
  })

  it('is disabled without update telemetry', () => {
    expect(rebootOption(undefined).enabled).toBe(false)
  })
})

describe('[PATCH-FLOW-001 AC-010, AC-011] patchDialogStatements', () => {
  const held = { pending_reboot_class_count: 1, hold_enforcement: 'active' as const, held_packages: ['linux-image-amd64'] }

  it('(a) holds active and unticked: kernel/GPU updates are held back', () => {
    expect(patchDialogStatements(updates(held), false)).toEqual([
      'Kernel and GPU driver updates are held by rIOt and will not be installed by this run.',
    ])
  })

  it('(b) ticked: kernel/GPU updates install and the host reboots if required', () => {
    expect(patchDialogStatements(updates(held), true)).toEqual([
      'Kernel and GPU driver updates will be installed. The host reboots after the run if a reboot is required.',
    ])
  })

  it('(c) holds not active and unticked: installed, reboot needed afterwards, rIOt will not reboot', () => {
    expect(patchDialogStatements(updates({ pending_reboot_class_count: 2 }), false)).toEqual([
      'Kernel and GPU driver updates will be installed. A reboot will be needed afterwards, and rIOt will not reboot the host.',
    ])
  })

  it('treats holds as inactive unless enforcement is active and packages are held', () => {
    expect(patchDialogStatements(updates({ pending_reboot_class_count: 1, hold_enforcement: 'active', held_packages: [] }), false)[0]).toContain('will be installed')
    expect(patchDialogStatements(updates({ pending_reboot_class_count: 1, hold_enforcement: 'no_privilege', held_packages: ['x'] }), false)[0]).toContain('will be installed')
  })

  it('shows none of (a), (b), (c) when no reboot-class update is pending', () => {
    expect(patchDialogStatements(updates(), false)).toEqual([])
    expect(patchDialogStatements(updates(), true)).toEqual([])
    expect(patchDialogStatements(updates({ pending_reboot_class_count: 0, hold_enforcement: 'active', held_packages: ['x'] }), false)).toEqual([])
  })

  it('states an already-required reboot first', () => {
    expect(patchDialogStatements(updates({ reboot_required: true, pending_updates: 3 }), false)).toEqual([
      'A reboot is already required on this host. With the box ticked, the host reboots after the run.',
    ])
    expect(patchDialogStatements(updates({ reboot_required: true, ...held }), true)[0]).toContain('already required')
  })

  it('adds the stale-lists caveat last', () => {
    const statements = patchDialogStatements(updates({ ...held, lists_freshness: 'stale' }), false)
    expect(statements).toHaveLength(2)
    expect(statements[1]).toBe(
      'Package lists are out of date. Running Patch refreshes them first, so the packages installed may differ from the pending list.',
    )
  })

  it('has nothing to say without update telemetry', () => {
    expect(patchDialogStatements(undefined, true)).toEqual([])
  })
})

describe('[PATCH-FLOW-001 SEC-007] staleSecurityNote keeps the raw security count visible', () => {
  it('qualifies the count while the lists are stale', () => {
    expect(staleSecurityNote(updates({ lists_freshness: 'stale', pending_security_count: 3 }))).toBe('3 security updates (lists out of date)')
    expect(staleSecurityNote(updates({ lists_freshness: 'stale', pending_security_count: 1 }))).toBe('1 security update (lists out of date)')
  })

  it('says nothing when fresh, unknown, or when there are no security updates', () => {
    expect(staleSecurityNote(updates({ lists_freshness: 'fresh', pending_security_count: 3 }))).toBeNull()
    expect(staleSecurityNote(updates({ pending_security_count: 3 }))).toBeNull()
    expect(staleSecurityNote(updates({ lists_freshness: 'stale', pending_security_count: 0 }))).toBeNull()
    expect(staleSecurityNote(undefined)).toBeNull()
  })
})

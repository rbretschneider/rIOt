import { describe, it, expect } from 'vitest'
import { formatUtc, rebootEtaTooltip } from './rebootEta'

const START = '2026-10-19T02:00:00Z'

describe('formatUtc', () => {
  it('formats an instant as an absolute UTC string', () => {
    expect(formatUtc('2026-10-19T02:05:00Z')).toBe('2026-10-19 02:05 UTC')
  })

  it('uses UTC getters, ignoring a non-UTC offset in the input', () => {
    // 2026-10-19 23:30 at UTC-5 is 2026-10-20 04:30 UTC.
    expect(formatUtc('2026-10-19T23:30:00-05:00')).toBe('2026-10-20 04:30 UTC')
  })

  it('returns null for an unparseable instant instead of a placeholder', () => {
    expect(formatUtc('not-a-date')).toBeNull()
  })
})

describe('[AC-006] window mode shows Est. reboot as an absolute UTC time', () => {
  it('contains "Est. reboot:" followed by the UTC time when the agent permits reboot', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START, allow_reboot: true })
    expect(t).toContain('Est. reboot: 2026-10-19 02:00 UTC')
  })

  it('contains "Est. reboot:" followed by the UTC time when allow_reboot is unknown', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START })
    expect(t).toContain('Est. reboot: 2026-10-19 02:00 UTC')
  })

  it('does not convert to a local timezone (input offset resolves to UTC)', () => {
    const t = rebootEtaTooltip({
      os_patch_mode: 'window',
      next_window_start: '2026-10-19T23:30:00-05:00',
      allow_reboot: true,
    })
    expect(t).toContain('Est. reboot: 2026-10-20 04:30 UTC')
  })

  it('words an unknown allow_reboot conditionally, never as a guarantee', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START })
    expect(t).toContain('if this device permits automatic reboot')
  })
})

describe('[AC-007] anytime mode shows cooldown wording and no clock time', () => {
  const t = rebootEtaTooltip({ os_patch_mode: 'anytime', os_patch_cooldown_minutes: 360, allow_reboot: true })

  it('states the reboot occurs on the next automatic patch run within the cooldown', () => {
    expect(t).toContain('next automatic patch run (within the ~360 min cooldown)')
  })

  it('does not contain "Est. reboot:" or a clock time', () => {
    expect(t).not.toContain('Est. reboot:')
    expect(t).not.toMatch(/\d{2}:\d{2}/)
    expect(t).not.toContain('UTC')
  })

  it('ignores a stray next_window_start in anytime mode', () => {
    const withStart = rebootEtaTooltip({
      os_patch_mode: 'anytime',
      os_patch_cooldown_minutes: 60,
      next_window_start: START,
    })
    expect(withStart).not.toContain('2026-10-19')
  })

  it('still reads sensibly when the cooldown is absent', () => {
    const noCooldown = rebootEtaTooltip({ os_patch_mode: 'anytime' })
    expect(noCooldown).toContain('next automatic patch run.')
    expect(noCooldown).not.toContain('undefined')
  })
})

describe('[AC-008] disabled mode states reboot required with no time', () => {
  const t = rebootEtaTooltip({ os_patch_mode: 'disabled', allow_reboot: true })

  it('states a reboot is required', () => {
    expect(t).toBe('A reboot is required to activate the installed updates.')
  })

  it('contains no estimated time even if a start is supplied', () => {
    const withStart = rebootEtaTooltip({ os_patch_mode: 'disabled', next_window_start: START })
    expect(withStart).not.toContain('Est. reboot:')
    expect(withStart).not.toContain('2026-10-19')
  })
})

describe('[AC-009] no window configured states reboot required with no time', () => {
  it('returns the reboot-required text when no mode is present', () => {
    expect(rebootEtaTooltip({})).toBe('A reboot is required to activate the installed updates.')
  })

  it('contains no estimated time', () => {
    expect(rebootEtaTooltip({ allow_reboot: true })).not.toContain('Est. reboot:')
  })
})

describe('[AC-010] estimate-not-guarantee labelling', () => {
  it('labels the window estimate (allow_reboot true) as an estimate, not a guarantee', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START, allow_reboot: true })
    expect(t).toContain('estimate, not a guarantee')
  })

  it('labels the window estimate (allow_reboot unknown) as an estimate, not a guarantee', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START })
    expect(t).toContain('estimate, not a guarantee')
  })

  it('labels the anytime wording as an estimate, not a guarantee', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'anytime', os_patch_cooldown_minutes: 60 })
    expect(t).toContain('Estimate, not a guarantee')
  })
})

describe('[AC-011] caveat discloses what the estimate depends on', () => {
  it('names gating, cooldown, allow_reboot and hold_reboot_class on a window estimate', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START, allow_reboot: true })
    expect(t).toContain('reboot-class gating')
    expect(t).toContain('the cooldown')
    expect(t).toContain('allow_reboot')
    expect(t).toContain('hold_reboot_class')
  })

  it('names the same dependencies on the anytime wording', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'anytime', os_patch_cooldown_minutes: 60 })
    expect(t).toContain('reboot-class gating')
    expect(t).toContain('allow_reboot')
    expect(t).toContain('hold_reboot_class')
  })
})

describe('[AC-012] allow_reboot=false shows manual reboot with no timestamp', () => {
  it('states a manual reboot is required in window mode', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START, allow_reboot: false })
    expect(t).toContain('Manual reboot required')
  })

  it('does not show an estimated time in window mode', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START, allow_reboot: false })
    expect(t).not.toContain('Est. reboot:')
    expect(t).not.toContain('2026-10-19')
  })

  it('takes precedence over anytime mode', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'anytime', os_patch_cooldown_minutes: 60, allow_reboot: false })
    expect(t).toContain('Manual reboot required')
    expect(t).not.toContain('next automatic patch run')
  })
})

describe('[AC-019] window mode with no computed start falls back honestly', () => {
  it('shows reboot-required with no time when next_window_start is absent', () => {
    expect(rebootEtaTooltip({ os_patch_mode: 'window', allow_reboot: true })).toBe(
      'A reboot is required to activate the installed updates.',
    )
  })

  it('does not render an empty, partial or placeholder time for an empty start', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: '' })
    expect(t).not.toContain('Est. reboot:')
    expect(t).not.toContain('Invalid')
    expect(t).not.toContain('NaN')
  })

  it('does not render a placeholder for an unparseable start', () => {
    const t = rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: 'garbage' })
    expect(t).toBe('A reboot is required to activate the installed updates.')
  })
})

describe('[AC-005] tooltip is plain text for a native title attribute', () => {
  it('never contains markup for any resolved variant', () => {
    const variants = [
      rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START, allow_reboot: true }),
      rebootEtaTooltip({ os_patch_mode: 'window', next_window_start: START }),
      rebootEtaTooltip({ os_patch_mode: 'anytime', os_patch_cooldown_minutes: 60 }),
      rebootEtaTooltip({ os_patch_mode: 'disabled' }),
      rebootEtaTooltip({ allow_reboot: false }),
    ]
    for (const v of variants) {
      expect(v, 'tooltip text must not contain HTML angle brackets').not.toMatch(/[<>]/)
    }
  })
})

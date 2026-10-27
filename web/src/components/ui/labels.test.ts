import { describe, it, expect } from 'vitest'
import {
  COMMAND_ACTIONS,
  COMMAND_STATUSES,
  EVENT_TYPES,
  LABELS,
  SEVERITIES,
  enumLabel,
  humanize,
  toneFor,
} from './labels'

describe('[UIF AC-014] severity labels', () => {
  it.each(SEVERITIES)('maps %s to an explicit sentence-case label', value => {
    const label = enumLabel('severity', value)
    expect(label).toBe(LABELS.severity[value])
    expect(label[0]).toBe(label[0].toUpperCase())
    expect(label, `${value} must not render as its raw value`).not.toBe(value)
  })

  it('maps warning to Warning', () => {
    expect(enumLabel('severity', 'warning')).toBe('Warning')
  })

  it('covers every value of the severity union', () => {
    expect(Object.keys(LABELS.severity).sort()).toEqual([...SEVERITIES].sort())
  })
})

describe('[UIF AC-015] command status labels', () => {
  it.each(COMMAND_STATUSES)('maps %s to a capitalised label', value => {
    const label = enumLabel('commandStatus', value)
    expect(label).toBe(LABELS.commandStatus[value])
    expect(label).not.toBe(value)
  })

  it('maps queued to Queued', () => {
    expect(enumLabel('commandStatus', 'queued')).toBe('Queued')
  })

  it('covers every value of the command status union', () => {
    expect(Object.keys(LABELS.commandStatus).sort()).toEqual([...COMMAND_STATUSES].sort())
  })
})

describe('[UIF AC-016] event type labels have no underscores', () => {
  it('knows all 56 server event types', () => {
    // EXPECT-008 adds agent_reinstall_unconfirmed and agent_reinstall_paused to the 54 before it.
    expect(EVENT_TYPES).toHaveLength(56)
    expect(new Set(EVENT_TYPES).size).toBe(56)
  })

  it.each(EVENT_TYPES)('maps %s to an explicit label without underscores', value => {
    const label = enumLabel('eventType', value)
    expect(label).toBe(LABELS.eventType[value])
    expect(label).not.toContain('_')
    expect(label).not.toBe(value)
  })

  it('covers every value of the event type union', () => {
    expect(Object.keys(LABELS.eventType).sort()).toEqual([...EVENT_TYPES].sort())
  })

  it('maps device_offline to Device offline', () => {
    expect(enumLabel('eventType', 'device_offline')).toBe('Device offline')
  })
})

describe('[UIF AC-017] update kind labels', () => {
  it('maps os_update to "OS update" and agent_update to "Agent update"', () => {
    expect(enumLabel('commandAction', 'os_update')).toBe('OS update')
    expect(enumLabel('commandAction', 'agent_update')).toBe('Agent update')
  })

  it.each(COMMAND_ACTIONS)('maps action %s without underscores', value => {
    const label = enumLabel('commandAction', value)
    expect(label).toBe(LABELS.commandAction[value])
    expect(label).not.toContain('_')
  })

  it('covers every agent command action', () => {
    expect(Object.keys(LABELS.commandAction).sort()).toEqual([...COMMAND_ACTIONS].sort())
  })
})

describe('[UIF AC-018] unknown value humanized', () => {
  it('turns some_new_value into "Some new value"', () => {
    expect(enumLabel('eventType', 'some_new_value')).toBe('Some new value')
  })

  it('lower-cases the rest and capitalises the first letter', () => {
    expect(humanize('CPU_HIGH')).toBe('Cpu high')
    expect(humanize('cpu_high')).toBe('Cpu high')
  })

  it('does not resolve inherited object keys as labels', () => {
    expect(enumLabel('eventType', 'constructor')).toBe('Constructor')
    expect(enumLabel('severity', '__proto__')).toBe(humanize('__proto__'))
    expect(enumLabel('severity', 'toString')).toBe('Tostring')
  })

  it('humanizes an unmapped value in any category', () => {
    expect(enumLabel('severity', 'notice')).toBe('Notice')
    expect(enumLabel('commandStatus', 'timed_out')).toBe('Timed out')
  })
})

describe('[UIF AC-019] empty value fallback', () => {
  it.each([[null], [undefined], ['']])('renders %j as an em dash', value => {
    expect(enumLabel('severity', value as string | null | undefined)).toBe('—')
    expect(enumLabel('eventType', value as string | null | undefined)).toBe('—')
  })
})

describe('[UIF AC-020] toneFor maps values to tones', () => {
  it.each([
    ['critical', 'danger'],
    ['warning', 'warn'],
    ['info', 'info'],
    ['pass', 'success'],
  ])('severity %s is tone %s', (value, tone) => {
    expect(toneFor('severity', value)).toBe(tone)
  })

  it.each([
    ['success', 'success'],
    ['error', 'danger'],
    ['sent', 'warn'],
    ['queued', 'warn'],
    ['pending', 'neutral'],
  ])('command status %s is tone %s', (value, tone) => {
    expect(toneFor('commandStatus', value)).toBe(tone)
  })

  it('is neutral for other categories and unknown values', () => {
    expect(toneFor('eventType', 'device_offline')).toBe('neutral')
    expect(toneFor('commandAction', 'reboot')).toBe('neutral')
    expect(toneFor('severity', 'mystery')).toBe('neutral')
    expect(toneFor('severity', null)).toBe('neutral')
  })
})

import { describe, expect, it } from 'vitest'
import {
  isSelfUpdate,
  isValidAgentVersion,
  selfUpdateActionLabel,
  selfUpdateOutcomeLabel,
  selfUpdateVersions,
  validSelfUpdateVersions,
} from './selfUpdate'

describe('EXPECT-002 [AC-016] version grammar is re-validated in the web', () => {
  it.each(['2.80.0', 'v2.80.0', '0.0.1', '12345.12345.12345', '2.80.0-rc.1', '2.80.0+build-5'])('accepts %s', v => {
    expect(isValidAgentVersion(v), `${v} is a valid version`).toBe(true)
  })

  it.each([
    '',
    'dev',
    '2.80',
    '2.80.0.1',
    '123456.0.0',
    'vv2.80.0',
    '2.80.0 ',
    ' 2.80.0',
    '2.80.0-',
    '2.80.0-' + 'a'.repeat(17),
    '2.80.0\n',
    '2.80.0\u0000',
    '2.80.0‮',
    '<b>2.80.0</b>',
    '2.80.0-' + 'a'.repeat(40),
  ])('rejects %j', v => {
    expect(isValidAgentVersion(v), `${JSON.stringify(v)} must not be a valid version`).toBe(false)
  })

  it('rejects values that are not strings', () => {
    for (const v of [undefined, null, 280, {}, ['2.80.0']]) expect(isValidAgentVersion(v)).toBe(false)
  })

  it('requires the two versions to differ, ignoring one leading v', () => {
    expect(validSelfUpdateVersions('2.78.0', '2.80.0')).toEqual({ from: '2.78.0', to: '2.80.0' })
    expect(validSelfUpdateVersions('2.80.0', 'v2.80.0')).toBeNull()
    expect(validSelfUpdateVersions('2.80.0', '2.80.0')).toBeNull()
  })
})

describe('EXPECT-002 [AC-023] self-update record helpers', () => {
  const row = {
    origin: 'agent_self_update' as const,
    action: 'agent_update',
    params: { from_version: '2.78.0', to_version: '2.80.0' },
  }

  it('recognises only an agent_update row with the self-update origin', () => {
    expect(isSelfUpdate(row)).toBe(true)
    expect(isSelfUpdate({ ...row, origin: 'user' })).toBe(false)
    expect(isSelfUpdate({ ...row, origin: undefined })).toBe(false)
    expect(isSelfUpdate({ ...row, action: 'reboot' })).toBe(false)
    expect(isSelfUpdate(undefined)).toBe(false)
    expect(isSelfUpdate(null)).toBe(false)
  })

  it('reads the versions from params and labels the action', () => {
    const versions = selfUpdateVersions(row)
    expect(versions).toEqual({ from: '2.78.0', to: '2.80.0' })
    expect(selfUpdateActionLabel(versions)).toBe('Agent update 2.78.0 → 2.80.0')
  })

  it('returns no versions for missing, non-string or hostile params and labels the action plainly', () => {
    expect(selfUpdateVersions({ params: {} })).toBeNull()
    expect(selfUpdateVersions({ params: { from_version: 2, to_version: 3 } })).toBeNull()
    expect(selfUpdateVersions({ params: { from_version: '<img>', to_version: '2.80.0' } })).toBeNull()
    expect(selfUpdateVersions(undefined)).toBeNull()
    expect(selfUpdateActionLabel(null)).toBe('Agent update')
  })

  it('words each status as the outcome of FR-015', () => {
    expect(selfUpdateOutcomeLabel('running')).toBe('In progress')
    expect(selfUpdateOutcomeLabel('verifying')).toBe('Returned, confirming')
    expect(selfUpdateOutcomeLabel('succeeded')).toBe('Succeeded')
    expect(selfUpdateOutcomeLabel('failed')).toBe('Failed')
    expect(selfUpdateOutcomeLabel('timed_out')).toBe('Not confirmed')
  })

  it('does not resolve a prototype member as an outcome and falls back for unknown statuses', () => {
    expect(selfUpdateOutcomeLabel('constructor')).toBe('Constructor')
    expect(selfUpdateOutcomeLabel('cancelled')).toBe('Cancelled')
    expect(selfUpdateOutcomeLabel('')).toBe('—')
  })
})

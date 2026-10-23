import { describe, expect, it } from 'vitest'
import { FRESHNESS_LABEL, freshnessOf, isOutOfDate } from './dockerFreshness'

describe('[AC-009] freshness shown on every container row', () => {
  it.each([
    ['update_available', undefined, 'Out of date'],
    ['up_to_date', undefined, 'Up to date'],
    ['pinned', undefined, 'Pinned'],
    ['check_failed', undefined, 'Unknown'],
    ['something_new', undefined, 'Unknown'],
    ['up_to_date', true, 'Up to date'],
    ['check_failed', false, 'Unknown'],
    [undefined, true, 'Out of date'],
    [undefined, false, 'Up to date'],
    ['', true, 'Out of date'],
    [undefined, null, 'Unknown'],
    [undefined, undefined, 'Unknown'],
  ] as const)('verdict %j with older field %j reads %s', (status, available, label) => {
    expect(FRESHNESS_LABEL[freshnessOf({ update_status: status, update_available: available })]).toBe(label)
  })
  it('never reports a failed check as up to date', () => {
    expect(freshnessOf({ update_status: 'check_failed', update_available: false })).not.toBe('up_to_date')
  })
})

describe('[AC-006] Out of date filter rule', () => {
  it.each([
    ['update_available', false, true],
    ['up_to_date', true, false],
    ['pinned', true, false],
    ['check_failed', true, false],
    [undefined, true, true],
    [undefined, false, false],
    [undefined, null, false],
  ] as const)('verdict %j with older field %j matches: %s', (status, available, expected) => {
    expect(isOutOfDate({ update_status: status, update_available: available })).toBe(expected)
  })
})

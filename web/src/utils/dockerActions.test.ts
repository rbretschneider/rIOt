import { expect, it } from 'vitest'
import {
  bulkApplicability,
  containerPrimaryAction,
  stackPrimaryAction,
} from './dockerActions'
it.each(['exited', 'created', 'dead'])(
  'prioritizes Start over a pending image update for %s containers',
  (state) =>
    expect(containerPrimaryAction({ state, update_available: true })).toBe(
      'start',
    ),
)
it.each(['running', 'paused', 'restarting', 'removing'])(
  'offers no primary action for a healthy %s container',
  (state) =>
    expect(
      containerPrimaryAction({ state, health_status: 'healthy' }),
    ).toBeUndefined(),
)
it('prioritizes Update over Restart and suppresses primary actions during a command', () => {
  expect(
    containerPrimaryAction({
      state: 'running',
      health_status: 'unhealthy',
      update_available: true,
    }),
  ).toBe('update')
  expect(
    containerPrimaryAction({ state: 'running', health_status: 'unhealthy' }),
  ).toBe('restart')
  expect(containerPrimaryAction({ state: 'exited' }, true)).toBeUndefined()
})
it('uses the exact bulk state matrix', () => {
  expect(bulkApplicability('stop', { state: 'restarting' })).toBe(true)
  expect(bulkApplicability('restart', { state: 'paused' })).toBe(false)
  expect(bulkApplicability('start', { state: 'paused' })).toBe(false)
  expect(
    bulkApplicability('update', { state: 'exited', update_available: true }),
  ).toBe(true)
  expect(bulkApplicability('stop', { state: 'running' }, true)).toBe(false)
})
it('offers Up for partial stacks and no primary action for a healthy stack', () => {
  expect(
    stackPrimaryAction({ state: 'partial', running: 1, total: 3, updates: 2 }),
  ).toBe('up')
  expect(
    stackPrimaryAction({ state: 'running', running: 3, total: 3, updates: 0 }),
  ).toBeUndefined()
  expect(
    stackPrimaryAction({ state: 'running', running: 3, total: 3, updates: 1 }),
  ).toBe('update')
})

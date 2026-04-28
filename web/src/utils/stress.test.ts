import { describe, it, expect } from 'vitest'
import { stressScore } from './stress'
import type { HeartbeatData } from '../types/models'

const hb = (cpu: number, mem: number, disk: number, load: number): HeartbeatData => ({
  uptime: 0,
  cpu_percent: cpu,
  mem_percent: mem,
  disk_root_percent: disk,
  load_avg_1m: load,
})

describe('[AC-016] stressScore — FR-034 / BR-004 formula', () => {
  it('computes the correct weighted sum for a typical device', () => {
    // cpu=50, mem=40, disk=60, load_avg_1m=2.0
    // load_norm = min(2.0*25, 100) = 50
    // score = 0.4*50 + 0.3*40 + 0.2*60 + 0.1*50 = 20+12+12+5 = 49
    const score = stressScore(hb(50, 40, 60, 2.0), true)
    expect(score).toBeCloseTo(49, 5)
  })

  it('caps load_norm at 100 when load_avg_1m * 25 > 100', () => {
    // load_avg_1m=5.0 → load_norm = min(125, 100) = 100
    // score = 0.4*0 + 0.3*0 + 0.2*0 + 0.1*100 = 10
    const score = stressScore(hb(0, 0, 0, 5.0), true)
    expect(score).toBeCloseTo(10, 5)
  })

  it('returns NEGATIVE_INFINITY for offline devices', () => {
    const score = stressScore(hb(99, 99, 99, 9), false)
    expect(score).toBe(Number.NEGATIVE_INFINITY)
  })

  it('offline devices sort after online devices regardless of values', () => {
    const onlineScore = stressScore(hb(1, 1, 1, 0), true)
    const offlineScore = stressScore(hb(99, 99, 99, 9), false)
    expect(offlineScore).toBeLessThan(onlineScore)
  })

  it('returns 0 for online device with all-zero heartbeat', () => {
    const score = stressScore(hb(0, 0, 0, 0), true)
    expect(score).toBe(0)
  })

  it('returns 0 for online device with undefined heartbeat', () => {
    const score = stressScore(undefined, true)
    expect(score).toBe(0)
  })

  it('handles load_avg_1m exactly at the cap boundary (load=4.0 → load_norm=100)', () => {
    // load_avg_1m=4.0 → min(100, 100) = 100
    const score = stressScore(hb(0, 0, 0, 4.0), true)
    expect(score).toBeCloseTo(10, 5) // 0.1 * 100
  })

  it('handles load_avg_1m just below cap (load=3.99 → load_norm=99.75)', () => {
    const score = stressScore(hb(0, 0, 0, 3.99), true)
    expect(score).toBeCloseTo(0.1 * 99.75, 5)
  })

  it('is deterministic — same inputs always produce same output', () => {
    const h = hb(60, 70, 80, 1.5)
    expect(stressScore(h, true)).toBe(stressScore(h, true))
  })
})

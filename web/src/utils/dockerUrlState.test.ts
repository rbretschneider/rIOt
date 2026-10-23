import { describe, it, expect } from 'vitest'
import {
  containerPath,
  stackPath,
  parseDockerState,
  filterDockerRows,
  applicable,
  canonicalDockerSearch,
  isAutoUpdateOff,
  parseStackState,
  filterStackRows,
} from './dockerUrlState'
import type { DockerRow, DockerStack } from '../types/docker'
const row = {
  device_id: 'host-1',
  hostname: 'server',
  container_name: 'web',
  container_id: 'cid',
  image: 'nginx:latest',
  state: 'running',
  stack: 'app',
  health_status: 'unhealthy',
  cpu_percent: 1,
  mem_usage: 1,
  mem_limit: 10,
  restart_count: 0,
} satisfies DockerRow
describe('Docker fleet URL state', () => {
  it('encodes resource path segments and canonicalizes leading slash', () => {
    expect(containerPath('host 1', '/web:app')).toBe(
      '/docker/containers/host%201/web%3Aapp',
    )
    expect(stackPath('host', 'a/b')).toBe('/docker/stacks/host/a%2Fb')
  })
  it('drops unknown status and sort parameters and bounds search', () => {
    const s = parseDockerState(
      '?status=running,evil&sort=env:desc&q=' + 'x'.repeat(300),
    )
    expect(s.status).toEqual(['running'])
    expect(s.sort).toBe('stack')
    expect(s.q).toHaveLength(200)
  })
  it('[AC-004] defaults to Stack ascending without overriding explicit sorts', () => {
    expect(parseDockerState('')).toMatchObject({ sort: 'stack', direction: 'asc' })
    expect(parseDockerState('?sort=unknown:desc')).toMatchObject({ sort: 'stack', direction: 'asc' })
    for (const sort of ['name', 'status', 'host', 'image', 'cpu', 'mem', 'uptime', 'stack']) {
      expect(parseDockerState(`?sort=${sort}:desc`)).toMatchObject({ sort, direction: 'desc' })
    }
  })
  it('combines categories with AND and status chips with OR', () => {
    expect(
      filterDockerRows(
        [row],
        parseDockerState('?host=host-1&status=stopped,unhealthy&q=nginx'),
      ),
    ).toEqual([row])
    expect(filterDockerRows([row], parseDockerState('?host=other'))).toEqual([])
  })
  it('separates stacks with the same name across hosts', () => {
    expect(
      filterDockerRows([row], parseDockerState('?stack=host-2:app')),
    ).toEqual([])
  })
  it('calculates action applicability from current state and update verdict', () => {
    expect(applicable('start', row)).toBe(false)
    expect(applicable('stop', row)).toBe(true)
    expect(applicable('update', { ...row, update_available: true })).toBe(true)
    expect(applicable('restart', { ...row, state: 'exited' })).toBe(false)
  })
  it('filters two thousand rows quickly', () => {
    const rows = Array.from({ length: 2000 }, (_, i) => ({
      ...row,
      container_name: `web-${i}`,
    }))
    const start = performance.now()
    expect(
      filterDockerRows(rows, parseDockerState('?q=web-1')).length,
    ).toBeGreaterThan(0)
    expect(performance.now() - start).toBeLessThan(50)
  })
})

describe('[AC-005][AC-006][AC-007] Out of date and Auto-update off filters', () => {
  const on = { target: 'web', enabled: true }
  const off = { target: 'web', enabled: false }
  it('reads the new keys and ignores other values', () => {
    expect(parseDockerState('?outdated=1&autoupdate=off')).toMatchObject({ outOfDate: true, autoOff: true })
    expect(parseDockerState('?outdated=yes&autoupdate=on')).toMatchObject({ outOfDate: false, autoOff: false })
    expect(parseDockerState('')).toMatchObject({ outOfDate: false, autoOff: false })
  })
  it('opens legacy status=updates links with Out of date applied', () => {
    const s = parseDockerState('?status=running,updates')
    expect(s.outOfDate).toBe(true)
    expect(s.status).toEqual(['running'])
  })
  it('rewrites legacy status=updates to outdated=1 and leaves other searches alone', () => {
    expect(canonicalDockerSearch('?status=running,updates&q=web')).toBe('status=running&q=web&outdated=1')
    expect(canonicalDockerSearch('?status=updates')).toBe('outdated=1')
    expect(canonicalDockerSearch('?status=running&outdated=1')).toBeNull()
    expect(canonicalDockerSearch('')).toBeNull()
  })
  it('combines Out of date with status, host and search by AND', () => {
    const outdated = { ...row, update_status: 'update_available' as const }
    const current = { ...row, container_name: 'db', update_status: 'up_to_date' as const }
    const stopped = { ...outdated, container_name: 'old', state: 'exited' }
    const rows = [outdated, current, stopped]
    expect(filterDockerRows(rows, parseDockerState('?outdated=1'))).toEqual([outdated, stopped])
    expect(filterDockerRows(rows, parseDockerState('?status=running&outdated=1'))).toEqual([outdated])
    expect(filterDockerRows(rows, parseDockerState('?status=running,updates'))).toEqual([outdated])
    expect(filterDockerRows(rows, parseDockerState('?outdated=1&q=old'))).toEqual([stopped])
    expect(filterDockerRows(rows, parseDockerState('?outdated=1&host=other'))).toEqual([])
  })
  it('never matches pinned or failed checks as out of date', () => {
    const rows = [
      { ...row, update_status: 'pinned' as const, update_available: true },
      { ...row, update_status: 'check_failed' as const, update_available: true },
    ]
    expect(filterDockerRows(rows, parseDockerState('?outdated=1'))).toEqual([])
  })
  it('treats only targetable rows without an enabled or covering policy as off', () => {
    expect(isAutoUpdateOff({ ...row, auto_update_policy: off })).toBe(true)
    expect(isAutoUpdateOff({ ...row, auto_update_policy: on })).toBe(false)
    expect(isAutoUpdateOff({ ...row, auto_update_policy: { ...off, covered_by: 'app' } })).toBe(false)
    expect(isAutoUpdateOff({ ...row, auto_update_policy: { target: '', enabled: false } })).toBe(false)
    expect(isAutoUpdateOff(row)).toBe(false)
  })
  it('combines Auto-update off and Out of date by AND', () => {
    const a = { ...row, container_name: 'a', update_available: true, auto_update_policy: { ...off, target: 'a' } }
    const b = { ...row, container_name: 'b', update_available: true, auto_update_policy: { ...on, target: 'b' } }
    const c = { ...row, container_name: 'c', update_available: false, auto_update_policy: { ...off, target: 'c' } }
    expect(filterDockerRows([a, b, c], parseDockerState('?autoupdate=off'))).toEqual([a, c])
    expect(filterDockerRows([a, b, c], parseDockerState('?autoupdate=off&outdated=1'))).toEqual([a])
  })
})
describe('[AC-005][AC-006][AC-007] Stacks URL filters', () => {
  const stack: DockerStack = {
    device_id: 'host-1',
    hostname: 'server',
    project: 'app',
    state: 'running',
    running: 1,
    total: 1,
    updates: 0,
    out_of_date: 0,
    auto_update: false,
    last_seen_at: '',
    path_state: 'agreed',
    agreed_working_dir: '/srv/app',
    agreed_config_files: [],
  }
  const outdated = { ...stack, project: 'media', out_of_date: 2 }
  const auto = { ...stack, project: 'db', auto_update: true }
  const unknown = { ...stack, project: 'gone', auto_update_unavailable: true }
  it('parses search, host and the new keys', () => {
    expect(parseStackState('?q=me&host=host-1&outdated=1&autoupdate=off')).toEqual({ q: 'me', host: 'host-1', outOfDate: true, autoOff: true })
    expect(parseStackState('?outdated=0&autoupdate=x')).toEqual({ q: '', host: '', outOfDate: false, autoOff: false })
  })
  it('filters by out-of-date members and by stack auto-update off, combined by AND', () => {
    const rows = [stack, outdated, auto, unknown]
    expect(filterStackRows(rows, parseStackState('?outdated=1'))).toEqual([outdated])
    expect(filterStackRows(rows, parseStackState('?autoupdate=off'))).toEqual([stack, outdated])
    expect(filterStackRows(rows, parseStackState('?autoupdate=off&outdated=1&q=med'))).toEqual([outdated])
    expect(filterStackRows(rows, parseStackState('?outdated=1&host=host-2'))).toEqual([])
  })
})

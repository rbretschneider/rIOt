import { describe, it, expect } from 'vitest'
import {
  containerPath,
  stackPath,
  parseDockerState,
  filterDockerRows,
  applicable,
} from './dockerUrlState'
import type { DockerRow } from '../types/docker'
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

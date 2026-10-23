import type { DockerRow, DockerStack } from '../types/docker'
import { bulkApplicability } from './dockerActions'
import { isOutOfDate } from './dockerFreshness'
export const containerPath = (host: string, name: string) =>
  `/docker/containers/${encodeURIComponent(host)}/${encodeURIComponent(name.replace(/^\//, ''))}`
export const stackPath = (host: string, project: string) =>
  `/docker/stacks/${encodeURIComponent(host)}/${encodeURIComponent(project)}`
export const rowKey = (r: DockerRow) => `${r.device_id}.${r.container_name}`
const statuses = ['running', 'stopped', 'unhealthy']
// The former `updates` status chip; links that still carry it open with the
// Out of date filter instead.
const legacyUpdatesStatus = 'updates'
const columns = [
  'status',
  'name',
  'stack',
  'host',
  'image',
  'cpu',
  'mem',
  'uptime',
]
export function parseDockerState(search: string, preferredView = 'table') {
  const p = new URLSearchParams(search)
  const sort = p.get('sort')?.split(':') ?? []
  const status = (p.get('status') ?? '').split(',')
  return {
    q: (p.get('q') ?? '').trim().slice(0, 200),
    status: status.filter((s) => statuses.includes(s)),
    outOfDate: p.get('outdated') === '1' || status.includes(legacyUpdatesStatus),
    autoOff: p.get('autoupdate') === 'off',
    host: (p.get('host') ?? '').split(',').filter(Boolean),
    stack: (p.get('stack') ?? '').split(',').filter(Boolean),
    sort: columns.includes(sort[0]) ? sort[0] : 'stack',
    direction: columns.includes(sort[0]) && sort[1] === 'desc' ? 'desc' : 'asc',
    view: (p.get('view') ?? preferredView) === 'tiles' ? 'tiles' : 'table',
    selection: (p.get('sel') ?? '').split(',').filter(Boolean).slice(0, 200),
  }
}
export function filterDockerRows(
  rows: DockerRow[],
  state: ReturnType<typeof parseDockerState>,
) {
  return rows.filter(
    (r) =>
      (!state.host.length || state.host.includes(r.device_id)) &&
      (!state.stack.length ||
        state.stack.includes(`${r.device_id}:${r.stack}`)) &&
      (!state.q ||
        [
          r.container_name,
          r.hostname,
          r.image,
          r.stack,
          r.riot_name,
          r.riot_group,
          ...(r.riot_tags ?? []),
        ]
          .join(' ')
          .toLowerCase()
          .includes(state.q.toLowerCase())) &&
      (!state.status.length ||
        state.status.some((s) =>
          s === 'running'
            ? r.state === 'running'
            : s === 'stopped'
              ? ['exited', 'created', 'dead'].includes(r.state)
              : r.health_status === 'unhealthy' || r.state === 'restarting',
        )) &&
      (!state.outOfDate || isOutOfDate(r)) &&
      (!state.autoOff || isAutoUpdateOff(r)),
  )
}
/**
 * The search with a legacy `status=updates` rewritten to `outdated=1`, or null
 * when there is nothing to rewrite. Pure: the page applies it with a replace.
 */
export function canonicalDockerSearch(search: string): string | null {
  const p = new URLSearchParams(search)
  const status = (p.get('status') ?? '').split(',').filter(Boolean)
  if (!status.includes(legacyUpdatesStatus)) return null
  const rest = status.filter((s) => s !== legacyUpdatesStatus)
  if (rest.length) p.set('status', rest.join(','))
  else p.delete('status')
  p.set('outdated', '1')
  return p.toString()
}
/**
 * The "Auto-update off" rule for a container row: no enabled own policy and
 * not covered by an enabled stack policy. Rows whose state is unknown or that
 * cannot be targeted never count as off.
 */
export function isAutoUpdateOff(r: DockerRow) {
  const policy = r.auto_update_policy
  return (
    policy !== undefined &&
    policy.target !== '' &&
    !policy.enabled &&
    !policy.covered_by
  )
}
export function parseStackState(search: string) {
  const p = new URLSearchParams(search)
  return {
    q: p.get('q') ?? '',
    host: p.get('host') ?? '',
    outOfDate: p.get('outdated') === '1',
    autoOff: p.get('autoupdate') === 'off',
  }
}
export function filterStackRows(
  rows: DockerStack[],
  state: ReturnType<typeof parseStackState>,
) {
  return rows.filter(
    (s) =>
      (!state.host || s.device_id === state.host) &&
      `${s.project} ${s.hostname}`
        .toLowerCase()
        .includes(state.q.toLowerCase()) &&
      (!state.outOfDate || (s.out_of_date ?? 0) > 0) &&
      (!state.autoOff || (!s.auto_update_unavailable && !s.auto_update)),
  )
}
export function applicable(action: string, r: DockerRow) {
  return bulkApplicability(action, r)
}

import type { DockerRow } from '../types/docker'
import { bulkApplicability } from './dockerActions'
export const containerPath = (host: string, name: string) =>
  `/docker/containers/${encodeURIComponent(host)}/${encodeURIComponent(name.replace(/^\//, ''))}`
export const stackPath = (host: string, project: string) =>
  `/docker/stacks/${encodeURIComponent(host)}/${encodeURIComponent(project)}`
export const rowKey = (r: DockerRow) => `${r.device_id}.${r.container_name}`
const statuses = ['running', 'stopped', 'unhealthy', 'updates']
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
  return {
    q: (p.get('q') ?? '').trim().slice(0, 200),
    status: (p.get('status') ?? '')
      .split(',')
      .filter((s) => statuses.includes(s)),
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
              : s === 'unhealthy'
                ? r.health_status === 'unhealthy' || r.state === 'restarting'
                : bulkApplicability('update', r),
        )),
  )
}
export function applicable(action: string, r: DockerRow) {
  return bulkApplicability(action, r)
}

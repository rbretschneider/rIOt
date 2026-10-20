import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { EmptyState, QueryState } from '../../components/ui'
import DataTable, { type Column } from '../../components/docker/DataTable'
import type { DockerStack } from '../../types/docker'
import { stackPath } from '../../utils/dockerUrlState'
export default function StacksPage() {
  const location = useLocation()
  const [params, setParams] = useSearchParams()
  const query = useQuery({
    queryKey: ['docker-stacks'],
    queryFn: dockerApi.stacks,
    refetchInterval: 15000,
  })
  const search = params.get('q') ?? ''
  const host = params.get('host') ?? ''
  return (
    <div className="space-y-4">
      <header className="border-b border-border pb-4">
        <h1 className="text-2xl font-semibold">Stacks</h1>
        <p className="mt-1 text-sm text-text-muted">
          Compose projects, services and combined logs across your hosts.
        </p>
      </header>
      <div className="flex flex-wrap gap-3">
        <input
          aria-label="Search stacks"
          placeholder="Search stacks"
          value={search}
          onChange={(e) => {
            const p = new URLSearchParams(params)
            p.set('q', e.target.value)
            setParams(p, { replace: true })
          }}
          className="min-w-0 flex-1 bg-surface border border-border rounded p-2 text-sm"
        />
        <select
          aria-label="Stack host"
          value={host}
          onChange={(e) => {
            const p = new URLSearchParams(params)
            e.target.value ? p.set('host', e.target.value) : p.delete('host')
            setParams(p)
          }}
          className="bg-surface rounded p-2"
        >
          <option value="">All hosts</option>
          {[
            ...new Map(
              query.data?.map((s) => [s.device_id, s.hostname]),
            ).entries(),
          ].map(([id, name]) => (
            <option key={id} value={id}>
              {name}
            </option>
          ))}
        </select>
      </div>
      <QueryState
        query={query}
        isEmpty={(s) => !s.length}
        empty={
          <EmptyState
            title="No compose stacks"
            description="Stacks discovered from Docker compose labels appear here, including stopped stacks."
          />
        }
      >
        {(stacks) => (
          <DataTable
            rows={stacks.filter(
              (s) =>
                (!host || s.device_id === host) &&
                `${s.project} ${s.hostname}`
                  .toLowerCase()
                  .includes(search.toLowerCase()),
            )}
            rowKey={(s) => s.device_id + s.project}
            columns={(
              [
                {
                  key: 'project',
                  title: 'Stack',
                  render: (s) => (
                    <Link
                      className="text-link font-medium"
                      to={stackPath(s.device_id, s.project)}
                      state={{
                        dockerCollection: location.pathname + location.search,
                      }}
                    >
                      {s.project}
                    </Link>
                  ),
                },
                { key: 'host', title: 'Host', render: (s) => s.hostname },
                { key: 'state', title: 'State', render: (s) => s.state },
                {
                  key: 'services',
                  title: 'Services',
                  render: (s) => `${s.running}/${s.total} running`,
                },
                { key: 'updates', title: 'Updates', render: (s) => s.updates },
                {
                  key: 'auto',
                  title: 'Auto update',
                  render: (s) => (s.auto_update ? 'Enabled' : 'Disabled'),
                },
                {
                  key: 'paths',
                  title: 'Compose paths',
                  render: (s) => s.path_state,
                },
                {
                  key: 'last',
                  title: 'Last seen',
                  render: (s) => s.last_seen_at,
                },
              ] satisfies Column<DockerStack>[]
            ).map((column) => ({
              ...column,
              width: (
                {
                  project: 220,
                  host: 144,
                  state: 112,
                  services: 128,
                  updates: 100,
                  auto: 128,
                  paths: 160,
                  last: 208,
                } as Record<string, number>
              )[column.key],
              pinned: column.key === 'project',
            }))}
          />
        )}
      </QueryState>
    </div>
  )
}

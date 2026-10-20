import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { EmptyState, QueryState } from '../../components/ui'
import DataTable from '../../components/docker/DataTable'
import { stackPath } from '../../utils/dockerUrlState'
export default function StacksPage() {
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
      <h2 className="text-lg font-semibold">Stacks</h2>
      <div className="flex gap-3">
        <input
          aria-label="Search stacks"
          placeholder="Search stacks"
          value={search}
          onChange={(e) => {
            const p = new URLSearchParams(params)
            p.set('q', e.target.value)
            setParams(p, { replace: true })
          }}
          className="bg-surface border border-border rounded p-2"
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
            columns={[
              {
                key: 'project',
                title: 'Stack',
                render: (s) => (
                  <Link
                    className="text-link font-medium"
                    to={stackPath(s.device_id, s.project)}
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
            ]}
          />
        )}
      </QueryState>
    </div>
  )
}

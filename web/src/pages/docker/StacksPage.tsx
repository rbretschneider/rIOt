import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { Button, EmptyState, PageHeader, QueryState } from '../../components/ui'
import StackUpdateDialog from '../../components/docker/StackUpdateDialog'
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
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
  })
  const search = params.get('q') ?? ''
  const host = params.get('host') ?? ''
  const [updating, setUpdating] = useState<{ deviceId: string; project: string }>()
  return (
    <div className="space-y-4">
      <PageHeader
        title="Stacks"
        description="Compose projects, services and combined logs across your hosts."
      />
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
          {hosts.data
            ?.filter((h) => h.docker_available === true)
            .map((h) => (
              <option key={h.device_id} value={h.device_id}>
                {h.hostname}
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
                {
                  key: 'actions',
                  title: 'Actions',
                  sortable: false,
                  render: (s) => (
                    <Button size="sm" className="h-6" aria-label={`Update ${s.project} on ${s.hostname}`}
                      onClick={() => setUpdating({ deviceId: s.device_id, project: s.project })}>
                      Update
                    </Button>
                  ),
                },
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
                  actions: 112,
                  last: 208,
                } as Record<string, number>
              )[column.key],
              pinned: column.key === 'project',
            }))}
          />
        )}
      </QueryState>
      {updating && <StackUpdateDialog
        key={`${updating.deviceId}:${updating.project}`}
        deviceId={updating.deviceId}
        project={updating.project}
        onClose={() => setUpdating(undefined)}
      />}
    </div>
  )
}

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { EmptyState, PageHeader, QueryState } from '../../components/ui'
import DockerIcon from '../../components/docker/DockerIcon'

export default function HostsPage() {
  const [search, setSearch] = useState('')
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
    refetchInterval: 15000,
  })
  return (
    <div className="space-y-5">
      <PageHeader
        title="Hosts"
        description="Docker availability, log collection and supported operations across your Docker hosts."
      />
      <input
        aria-label="Search Docker hosts"
        placeholder="Find a host…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        className="w-full rounded-md border border-border bg-surface px-3 py-2 text-sm sm:max-w-sm"
      />
      <QueryState
        query={hosts}
        isEmpty={(data) => !data.some((host) => host.docker_available === true)}
        empty={
          <EmptyState
            title="No Docker hosts"
            description="Machines that report Docker availability appear here. View all registered machines in Devices."
            children={
              <Link className="text-link" to="/">
                View Devices
              </Link>
            }
          />
        }
      >
        {(data) => {
          const visible = data
            .filter((h) => h.docker_available === true)
            .filter((h) =>
              `${h.hostname} ${h.primary_ip ?? ''}`
                .toLowerCase()
                .includes(search.toLowerCase()),
            )
          if (!visible.length)
            return (
              <EmptyState
                title="No matching hosts"
                description="Try another hostname or address."
              />
            )
          return (
            <div className="grid gap-4 xl:grid-cols-2 2xl:grid-cols-3">
              {visible.map((h) => {
                const scope = `?host=${encodeURIComponent(h.device_id)}`
                return (
                  <article
                    key={h.device_id}
                    className="rounded-lg border border-border bg-raised p-4"
                  >
                    <header className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <h2 className="flex items-center gap-2 font-semibold">
                          <DockerIcon kind="hosts" />
                          <span className="truncate">{h.hostname}</span>
                        </h2>
                        <p className="mt-1 font-mono text-xs text-text-muted">
                          {h.primary_ip || 'Address unavailable'}
                        </p>
                      </div>
                      <span
                        className={`rounded px-2 py-1 text-xs ${h.status === 'online' ? 'bg-success/10 text-success-fg' : 'bg-raised-2 text-text-muted'}`}
                      >
                        {h.status}
                      </span>
                    </header>
                    <dl className="my-4 grid grid-cols-2 gap-3 text-xs">
                      <div>
                        <dt className="text-text-muted">Docker</dt>
                        <dd className="mt-1">
                          {h.docker_available ? 'Available' : 'Not reported'}
                        </dd>
                      </div>
                      <div>
                        <dt className="text-text-muted">Stored logs</dt>
                        <dd className="mt-1 capitalize">{h.container_logs}</dd>
                      </div>
                      <div>
                        <dt className="text-text-muted">Live logs</dt>
                        <dd className="mt-1">
                          {h.capabilities.includes('logs_live_v1')
                            ? 'Supported'
                            : 'Requires agent update'}
                        </dd>
                      </div>
                      <div>
                        <dt className="text-text-muted">Inventory</dt>
                        <dd className="mt-1">
                          {h.inventory_enabled ? 'Enabled' : 'Not enabled'}
                        </dd>
                      </div>
                    </dl>
                    <div className="flex flex-wrap gap-3 border-t border-border pt-3 text-xs text-link">
                      <Link to={`/docker/containers${scope}`}>
                        Containers →
                      </Link>
                      <Link to={`/docker/stacks${scope}`}>Stacks →</Link>
                      <Link to={`/docker/images${scope}`}>Images →</Link>
                      <Link to={`/docker/activity${scope}`}>Activity →</Link>
                    </div>
                  </article>
                )
              })}
            </div>
          )
        }}
      </QueryState>
    </div>
  )
}

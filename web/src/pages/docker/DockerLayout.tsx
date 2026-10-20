import { NavLink, Outlet } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { EmptyState } from '../../components/ui'
import { useFeatures } from '../../hooks/useFeatures'
import DockerIcon from '../../components/docker/DockerIcon'

const groups = [
  {
    label: 'Workloads',
    items: [
      ['containers', 'Containers'],
      ['stacks', 'Stacks'],
      ['hosts', 'Hosts'],
    ],
  },
  {
    label: 'Resources',
    items: [
      ['images', 'Images'],
      ['volumes', 'Volumes'],
      ['networks', 'Networks'],
    ],
  },
  { label: 'History', items: [['activity', 'Activity']] },
]

export default function DockerLayout() {
  const { isEnabled } = useFeatures()
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
    refetchInterval: 15000,
    enabled: isEnabled('docker'),
  })
  if (!isEnabled('docker'))
    return (
      <EmptyState
        title="Docker is disabled"
        description="Enable Docker in Settings to view this area."
      />
    )
  const available = hosts.data?.filter((h) => h.docker_available) ?? []
  return (
    <div className="docker-workspace grid min-w-0 gap-6 lg:grid-cols-[184px_minmax(0,1fr)]">
      <aside className="min-w-0 lg:sticky lg:top-5 lg:self-start">
        <div className="mb-4 flex items-center gap-2 px-3 text-sm font-semibold tracking-wide text-text">
          <DockerIcon kind="containers" /> Docker
        </div>
        <nav
          aria-label="Docker"
          className="flex gap-4 overflow-x-auto pb-2 lg:flex-col lg:gap-5"
        >
          {groups.map((group) => (
            <div key={group.label} className="shrink-0 space-y-1">
              <p className="mb-2 hidden px-3 text-[11px] font-medium uppercase tracking-wider text-text-muted lg:block">
                {group.label}
              </p>
              <div className="flex gap-1 lg:flex-col">
                {group.items.map(([path, label]) => (
                  <NavLink
                    key={path}
                    to={`/docker/${path}`}
                    className={({ isActive }) =>
                      `flex items-center gap-2.5 rounded-md border px-3 py-2 text-sm transition-colors ${isActive ? 'border-border-strong bg-raised-2 font-medium text-text' : 'border-transparent text-text-muted hover:bg-raised hover:text-text'}`
                    }
                  >
                    <DockerIcon kind={path} />
                    {label}
                  </NavLink>
                ))}
              </div>
            </div>
          ))}
        </nav>
        <div className="mt-6 hidden border-t border-border px-3 pt-4 text-xs text-text-muted lg:block">
          {hosts.error
            ? 'Host status unavailable'
            : hosts.data
              ? `${available.filter((h) => h.status === 'online').length} / ${available.length} Docker hosts online`
              : 'Loading host status…'}
          <p className="mt-1">Stored history + live logs</p>
        </div>
      </aside>
      <section aria-label="Docker workspace" className="min-w-0">
        <Outlet />
      </section>
    </div>
  )
}

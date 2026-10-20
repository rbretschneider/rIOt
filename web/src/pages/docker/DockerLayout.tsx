import { NavLink, Outlet } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { EmptyState, QueryState } from '../../components/ui'
import { useFeatures } from '../../hooks/useFeatures'
import { DOCKER_SHIPPED } from './phases'
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
  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-2xl font-bold text-text">Docker</h1>
        <p className="text-sm text-text-muted">
          Containers, stacks and resources across your fleet
        </p>
      </div>
      <nav
        aria-label="Docker"
        className="flex flex-wrap gap-2 border-b border-border pb-3"
      >
        {[
          ['containers', 'Containers', DOCKER_SHIPPED.containers],
          ['stacks', 'Stacks', DOCKER_SHIPPED.stacks],
          ['images', 'Images', DOCKER_SHIPPED.inventory],
          ['volumes', 'Volumes', DOCKER_SHIPPED.inventory],
          ['networks', 'Networks', DOCKER_SHIPPED.inventory],
          ['activity', 'Activity', DOCKER_SHIPPED.activity],
        ]
          .filter(([, , shipped]) => shipped)
          .map(([path, label]) => (
            <NavLink
              key={String(path)}
              to={`/docker/${path}`}
              className={({ isActive }) =>
                `rounded px-3 py-2 text-sm ${isActive ? 'bg-raised-2 text-text' : 'text-text-muted hover:text-text'}`
              }
            >
              {label}
            </NavLink>
          ))}
      </nav>
      <QueryState
        query={hosts}
        isEmpty={(h) => !h.some((x) => x.docker_available)}
        empty={
          <EmptyState
            title="No host reports Docker"
            description="Enable the Docker collector on a host to get started."
          />
        }
      >
        {() => <Outlet />}
      </QueryState>
    </div>
  )
}

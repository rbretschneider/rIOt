import { Navigate, Link, useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'
import DeviceQueryFallback from '../../components/DeviceQueryFallback'
import { EmptyState } from '../../components/ui'
import { containerPath } from '../../utils/dockerUrlState'
export default function LegacyRedirect() {
  const { id, cid } = useParams()
  const query = useQuery({
    queryKey: ['device', id],
    queryFn: () => api.getDevice(id!),
    retry: false,
  })
  if (!query.data)
    return (
      <DeviceQueryFallback
        isPending={query.isPending}
        error={query.error}
        onRetry={() => {
          void query.refetch()
        }}
      />
    )
  if (!cid)
    return (
      <Navigate
        replace
        to={`/docker/containers?host=${encodeURIComponent(id!)}`}
      />
    )
  const c = query.data.latest_telemetry?.data?.docker?.containers?.find(
    (c) => c.short_id === cid || c.id === cid,
  )
  return c ? (
    <Navigate replace to={containerPath(id!, c.name)} />
  ) : (
    <EmptyState title="Container not found">
      <Link to={`/docker/containers?host=${encodeURIComponent(id!)}`}>
        Back to Containers
      </Link>
    </EmptyState>
  )
}

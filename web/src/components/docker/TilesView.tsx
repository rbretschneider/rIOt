import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api } from '../../api/client'
import { ContainerLayout } from '../../pages/DeviceContainers'
import { QueryState } from '../ui'
import { containerPath } from '../../utils/dockerUrlState'
import type { DockerRow } from '../../types/docker'
function HostTiles({
  deviceId,
  rows,
}: {
  deviceId: string
  rows: DockerRow[]
}) {
  const navigate = useNavigate()
  const query = useQuery({
    queryKey: ['device', deviceId],
    queryFn: () => api.getDevice(deviceId),
    refetchInterval: 15000,
  })
  const visible = new Set(rows.map((r) => r.container_name.replace(/^\//, '')))
  return (
    <section className="space-y-3">
      <h3 className="text-lg font-semibold">{rows[0]?.hostname}</h3>
      <QueryState query={query}>
        {(data) => (
          <ContainerLayout
            allContainers={(
              data.latest_telemetry?.data?.docker?.containers ?? []
            ).filter((c) => visible.has(c.name.replace(/^\//, '')))}
            search=""
            deviceId={deviceId}
            onContainerClick={(c) => navigate(containerPath(deviceId, c.name))}
          />
        )}
      </QueryState>
    </section>
  )
}
export default function TilesView({ rows }: { rows: DockerRow[] }) {
  return (
    <div className="space-y-6">
      {[...new Set(rows.map((r) => r.device_id))].map((id) => (
        <HostTiles
          key={id}
          deviceId={id}
          rows={rows.filter((r) => r.device_id === id)}
        />
      ))}
    </div>
  )
}

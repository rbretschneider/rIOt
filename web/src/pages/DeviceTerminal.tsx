import { useParams, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import Terminal from '../components/Terminal'
import { PageHeader } from '../components/ui'

export default function DeviceTerminal() {
  const { id } = useParams<{ id: string }>()
  const { data } = useQuery({
    queryKey: ['device', id],
    queryFn: () => api.getDevice(id!),
    enabled: !!id,
  })

  const device = data?.device

  return (
    <div className="min-w-0 space-y-4">
      <Link to={`/devices/${id}`} className="inline-flex text-sm text-text-muted hover:text-text transition-colors">
        &larr; Back to {device?.hostname || 'device'}
      </Link>
      <PageHeader
        eyebrow="Device access"
        title="Host Terminal"
        description={<>{device?.hostname || id} &middot; Shell session via agent</>}
      />
      <div data-palette-exempt className="min-w-0 bg-raised border border-border rounded-lg overflow-hidden" style={{ height: 'max(20rem, calc(100dvh - 240px))' }}>
        {id && <Terminal deviceId={id} containerId="_host" />}
      </div>
    </div>
  )
}

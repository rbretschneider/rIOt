import { useParams, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import { useFeatures } from '../hooks/useFeatures'
import { displayName } from '../utils/docker'
import { ContainerDetailContent } from '../components/ContainerDetail'

export default function ContainerDetailPage() {
  const { id, cid } = useParams<{ id: string; cid: string }>()
  const { wsConnected } = useDevices()
  const { isEnabled } = useFeatures()
  const { data, isLoading } = useQuery({
    queryKey: ['device', id],
    queryFn: () => api.getDevice(id!),
    refetchInterval: wsConnected ? false : 15_000,
    enabled: !!id,
  })

  if (isLoading) return <div className="text-gray-500">Loading...</div>
  if (!data) return <div className="text-gray-500">Device not found</div>

  const { device, latest_telemetry } = data
  const containers = latest_telemetry?.data?.docker?.containers ?? []
  const container = containers.find(c => c.short_id === cid)

  if (!container) {
    return (
      <div className="space-y-4">
        <Link to={`/devices/${id}/containers`} className="text-sm text-gray-400 hover:text-white transition-colors">
          &larr; Back to Containers
        </Link>
        <p className="text-gray-500">Container not found.</p>
      </div>
    )
  }

  const name = displayName(container.riot, container.name)

  return (
    <div className="space-y-6">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm">
        <Link to={`/devices/${id}`} className="text-gray-400 hover:text-white uppercase transition-colors">
          {device.hostname}
        </Link>
        <span className="text-gray-600">/</span>
        <Link to={`/devices/${id}/containers`} className="text-gray-400 hover:text-white transition-colors">
          Containers
        </Link>
        <span className="text-gray-600">/</span>
        <span className="text-gray-200">{name}</span>
      </div>

      {/* Container name + ID */}
      <div>
        <div className="flex items-center gap-2">
          {container.riot?.icon && <span className="text-xl">{container.riot.icon}</span>}
          <h1 className="text-2xl font-bold text-white">{name}</h1>
        </div>
        <p className="text-xs text-gray-600 font-mono mt-1">{container.short_id}</p>
      </div>

      {/* Content */}
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-5">
        <ContainerDetailContent container={container} deviceId={id} terminalEnabled={isEnabled('docker_terminal')} />
      </div>
    </div>
  )
}

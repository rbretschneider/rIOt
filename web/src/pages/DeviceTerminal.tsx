import { useParams, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import Terminal from '../components/Terminal'

export default function DeviceTerminal() {
  const { id } = useParams<{ id: string }>()
  const { data } = useQuery({
    queryKey: ['device', id],
    queryFn: () => api.getDevice(id!),
    enabled: !!id,
  })

  const device = data?.device

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <Link to={`/devices/${id}`} className="text-sm text-gray-400 hover:text-white transition-colors">
            &larr; Back to {device?.hostname || 'device'}
          </Link>
          <h1 className="text-2xl font-bold text-white mt-2">Host Terminal</h1>
          <p className="text-sm text-gray-500">
            {device?.hostname || id} &middot; Shell session via agent
          </p>
        </div>
      </div>
      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden" style={{ height: 'calc(100vh - 220px)' }}>
        {id && <Terminal deviceId={id} containerId="_host" />}
      </div>
    </div>
  )
}

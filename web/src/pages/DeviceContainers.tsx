import { useState, useMemo } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { useQuery, useMutation } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import ContainerGroup from '../components/ContainerGroup'
import type { ContainerInfo } from '../types/models'
import { groupContainers, displayName } from '../utils/docker'

export default function DeviceContainers() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { wsConnected } = useDevices()
  const { data, isLoading } = useQuery({
    queryKey: ['device', id],
    queryFn: () => api.getDevice(id!),
    refetchInterval: wsConnected ? false : 15_000,
    enabled: !!id,
  })

  const [containerSearch, setContainerSearch] = useState('')

  const checkUpdatesMutation = useMutation({
    mutationFn: () => api.sendCommand(id!, 'docker_check_updates', {}),
  })

  if (isLoading) return <div className="text-gray-500">Loading...</div>
  if (!data) return <div className="text-gray-500">Device not found</div>

  const { device, latest_telemetry } = data
  const tel = latest_telemetry?.data
  const docker = tel?.docker

  if (!docker?.available) {
    return (
      <div className="space-y-4">
        <Link to={`/devices/${id}`} className="text-sm text-gray-400 hover:text-white transition-colors">
          &larr; Back to {device.hostname}
        </Link>
        <p className="text-gray-500">Docker is not available on this device.</p>
      </div>
    )
  }

  function handleContainerClick(c: ContainerInfo) {
    navigate(`/devices/${id}/containers/${c.short_id}`)
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <Link to={`/devices/${id}`} className="text-sm text-gray-400 hover:text-white transition-colors">
          &larr; Back to {device.hostname}
        </Link>
        <h1 className="text-2xl font-bold text-white mt-2">
          Docker Containers
        </h1>
        <div className="flex items-center gap-3 mt-1">
          <p className="text-sm text-gray-500">
            {docker.running} running / {docker.total_containers} total
            {docker.paused ? ` / ${docker.paused} paused` : ''}
            {(() => {
              const updatable = (docker.containers ?? []).filter(c => c.update_available).length
              return updatable > 0 ? (
                <span className="text-amber-400 ml-1">/ {updatable} updatable</span>
              ) : null
            })()}
          </p>
          <button
            onClick={() => checkUpdatesMutation.mutate()}
            disabled={checkUpdatesMutation.isPending}
            className="px-2 py-0.5 text-[11px] text-gray-500 hover:text-gray-300 border border-gray-700 hover:border-gray-600 rounded transition-colors disabled:opacity-50"
            title="Clear image cache and re-check registries on next telemetry cycle"
          >
            {checkUpdatesMutation.isPending ? 'Checking...' : checkUpdatesMutation.isSuccess ? 'Check queued' : 'Check for Updates'}
          </button>
        </div>
      </div>

      {/* Search */}
      <div>
        <input
          type="text"
          value={containerSearch}
          onChange={e => setContainerSearch(e.target.value)}
          placeholder="Search containers..."
          className="w-full md:w-64 px-3 py-1.5 bg-gray-800 border border-gray-700 rounded-md text-sm text-gray-300 placeholder-gray-600 focus:outline-none focus:border-gray-500"
        />
      </div>

      {/* Container Groups */}
      <ContainerList
        docker={docker}
        search={containerSearch}
        onContainerClick={handleContainerClick}
        deviceId={id}
      />
    </div>
  )
}

function ContainerList({
  docker,
  search,
  onContainerClick,
  deviceId,
}: {
  docker: NonNullable<import('../types/models').FullTelemetryData['docker']>
  search: string
  onContainerClick: (c: ContainerInfo) => void
  deviceId?: string
}) {
  const filtered = useMemo(() => {
    let containers = docker.containers ?? []
    if (search) {
      const q = search.toLowerCase()
      containers = containers.filter(c => {
        const name = displayName(c.riot, c.name).toLowerCase()
        return (
          name.includes(q) ||
          c.image.toLowerCase().includes(q) ||
          c.state.includes(q) ||
          (c.riot?.group?.toLowerCase().includes(q)) ||
          (c.riot?.tags?.some(t => t.toLowerCase().includes(q)))
        )
      })
    }
    return containers
  }, [docker.containers, search])

  const groups = useMemo(() => groupContainers(filtered), [filtered])

  if (groups.length === 0) {
    return <p className="text-sm text-gray-500">No containers match your search.</p>
  }

  return (
    <div className="space-y-6">
      {groups.map(g => (
        <ContainerGroup key={g.name} group={g} onContainerClick={onContainerClick} deviceId={deviceId} />
      ))}
    </div>
  )
}

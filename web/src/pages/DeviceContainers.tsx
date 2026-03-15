import { useState, useMemo } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { useQuery, useMutation } from '@tanstack/react-query'
import { api } from '../api/client'
import { useDevices } from '../hooks/useDevices'
import ComposeStackSection from '../components/ComposeStackSection'
import CompactContainerTile from '../components/CompactContainerTile'
import type { ContainerInfo } from '../types/models'
import { groupContainersV2, displayName, getNetworkParent } from '../utils/docker'

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
    onSuccess: () => { setTimeout(() => checkUpdatesMutation.reset(), 5000) },
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
          &larr; Back to <span className="uppercase">{device.hostname}</span>
        </Link>
        <p className="text-gray-500">Docker is not available on this device.</p>
      </div>
    )
  }

  const allContainers = docker.containers ?? []
  const allUpdatable = allContainers.filter(c => c.update_available)

  function handleContainerClick(c: ContainerInfo) {
    navigate(`/devices/${id}/containers/${c.short_id}`)
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <Link to={`/devices/${id}`} className="text-sm text-gray-400 hover:text-white transition-colors">
          &larr; Back to <span className="uppercase">{device.hostname}</span>
        </Link>
        <h1 className="text-2xl font-bold text-white mt-2">
          Docker Containers
        </h1>
        <div className="flex items-center gap-3 mt-1">
          <p className="text-sm text-gray-500">
            {docker.running} running / {docker.total_containers} total
            {docker.paused ? ` / ${docker.paused} paused` : ''}
            {allUpdatable.length > 0 && (
              <span className="text-amber-400 ml-1">/ {allUpdatable.length} updatable</span>
            )}
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

      {/* Container Layout */}
      <ContainerLayout
        allContainers={allContainers}
        search={containerSearch}
        onContainerClick={handleContainerClick}
        deviceId={id!}
      />
    </div>
  )
}

function ContainerLayout({
  allContainers,
  search,
  onContainerClick,
  deviceId,
}: {
  allContainers: ContainerInfo[]
  search: string
  onContainerClick: (c: ContainerInfo) => void
  deviceId: string
}) {
  const filtered = useMemo(() => {
    if (!search) return allContainers
    const q = search.toLowerCase()
    return allContainers.filter(c => {
      const name = displayName(c.riot, c.name).toLowerCase()
      return (
        name.includes(q) ||
        c.image.toLowerCase().includes(q) ||
        c.state.includes(q) ||
        c.riot?.group?.toLowerCase().includes(q) ||
        c.riot?.tags?.some(t => t.toLowerCase().includes(q)) ||
        c.labels?.['com.docker.compose.project']?.toLowerCase().includes(q)
      )
    })
  }, [allContainers, search])

  const layout = useMemo(() => groupContainersV2(filtered), [filtered])

  // Build cross-stack network parent labels for standalone containers
  const standaloneCrossStack = useMemo(() => {
    const map = new Map<string, string>()
    for (const c of layout.standalone) {
      const parent = getNetworkParent(c)
      if (parent) {
        const parentContainer = allContainers.find(ac => ac.name === parent)
        const project = parentContainer?.labels?.['com.docker.compose.project']
        map.set(c.id, project ? `${parent} (${project})` : parent)
      }
    }
    return map
  }, [layout.standalone, allContainers])

  if (layout.composeStacks.length === 0 && layout.standalone.length === 0) {
    return <p className="text-sm text-gray-500">No containers match your search.</p>
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        {layout.composeStacks.map(stack => (
          <ComposeStackSection
            key={stack.name}
            stack={stack}
            deviceId={deviceId}
            onContainerClick={onContainerClick}
            allContainers={allContainers}
          />
        ))}
      </div>

      {layout.standalone.length > 0 && (
        <section className="border border-gray-700/30 rounded-lg bg-gray-900/30 p-4">
          <div className="flex items-center gap-2 mb-3">
            <svg className="w-4 h-4 text-gray-500 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" />
            </svg>
            <h2 className="text-sm font-semibold text-gray-500 uppercase tracking-wider">Standalone Containers</h2>
            <span className="text-xs text-gray-600">{layout.standalone.length}</span>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {layout.standalone.map(c => (
              <CompactContainerTile
                key={c.id}
                container={c}
                onClick={onContainerClick}
                crossStackParent={standaloneCrossStack.get(c.id)}
              />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

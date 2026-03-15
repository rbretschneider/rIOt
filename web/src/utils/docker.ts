import type { ContainerInfo, RiotLabels } from '../types/models'

const sensitiveKeys = [
  'password', 'passwd', 'secret', 'token', 'key', 'apikey',
  'api_key', 'auth', 'credential', 'private',
]

export function isSensitiveKey(key: string): boolean {
  const lower = key.toLowerCase()
  return sensitiveKeys.some(s => lower.includes(s))
}

export function maskValue(key: string, value: string): string {
  if (!isSensitiveKey(key)) return value
  if (value.length <= 4) return '****'
  return value.slice(0, 2) + '*'.repeat(value.length - 4) + value.slice(-2)
}

export function statusColor(state: string): string {
  switch (state) {
    case 'running': return 'text-emerald-400'
    case 'paused': return 'text-amber-400'
    case 'restarting': return 'text-blue-400'
    case 'exited':
    case 'dead': return 'text-red-400'
    case 'created': return 'text-gray-400'
    default: return 'text-gray-400'
  }
}

export function statusBgColor(state: string): string {
  switch (state) {
    case 'running': return 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30'
    case 'paused': return 'bg-amber-500/20 text-amber-400 border-amber-500/30'
    case 'restarting': return 'bg-blue-500/20 text-blue-400 border-blue-500/30'
    case 'exited':
    case 'dead': return 'bg-red-500/20 text-red-400 border-red-500/30'
    case 'created': return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
    default: return 'bg-gray-500/20 text-gray-400 border-gray-500/30'
  }
}

export function displayName(riot: RiotLabels | undefined, containerName: string): string {
  if (riot?.name) return riot.name
  return containerName
}

export type GroupSource = 'riot' | 'compose' | 'ungrouped'

export function groupName(riot: RiotLabels | undefined, labels?: Record<string, string>): { name: string; source: GroupSource } {
  if (riot?.group) return { name: riot.group, source: 'riot' }
  const composeProject = labels?.['com.docker.compose.project']
  if (composeProject) return { name: composeProject, source: 'compose' }
  return { name: 'Ungrouped', source: 'ungrouped' }
}

export interface ContainerGroup {
  name: string
  source: GroupSource
  icon?: string
  priority: number
  containers: ContainerInfo[]
}

export function groupContainers(containers: ContainerInfo[]): ContainerGroup[] {
  const map = new Map<string, ContainerGroup>()

  for (const c of containers) {
    if (c.riot?.hide) continue
    const { name: gName, source } = groupName(c.riot, c.labels)
    let group = map.get(gName)
    if (!group) {
      group = {
        name: gName,
        source,
        icon: c.riot?.icon,
        priority: c.riot?.priority ?? 50,
        containers: [],
      }
      map.set(gName, group)
    }
    group.containers.push(c)
    if ((c.riot?.priority ?? 50) < group.priority) {
      group.priority = c.riot?.priority ?? 50
    }
  }

  const groups = Array.from(map.values())

  // Sort containers within each group by priority then name
  for (const g of groups) {
    g.containers.sort((a, b) => {
      const pa = a.riot?.priority ?? 50
      const pb = b.riot?.priority ?? 50
      if (pa !== pb) return pa - pb
      return displayName(a.riot, a.name).localeCompare(displayName(b.riot, b.name))
    })
  }

  // Sort groups by priority then name
  groups.sort((a, b) => {
    if (a.priority !== b.priority) return a.priority - b.priority
    return a.name.localeCompare(b.name)
  })

  return groups
}

export function formatContainerUptime(createdTimestamp: number): string {
  const now = Math.floor(Date.now() / 1000)
  const seconds = now - createdTimestamp
  if (seconds < 0) return 'just now'
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ${minutes % 60}m`
  const days = Math.floor(hours / 24)
  return `${days}d ${hours % 24}h`
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`
}

export function formatPorts(c: ContainerInfo): string {
  if (!c.ports || c.ports.length === 0) return ''
  return c.ports
    .map(p => {
      if (p.host_port) {
        const host = p.host_ip && p.host_ip !== '0.0.0.0' ? `${p.host_ip}:` : ''
        return `${host}${p.host_port}→${p.container_port}`
      }
      return p.container_port
    })
    .join(', ')
}

// --- V2 grouping types ---

export interface NetworkCluster {
  parent: ContainerInfo
  dependents: ContainerInfo[]
}

export interface ComposeStackGroup {
  name: string
  workDir: string
  containers: ContainerInfo[]
  networkClusters: NetworkCluster[]
  loose: ContainerInfo[]
  updatableCount: number
}

export interface ContainerLayout {
  composeStacks: ComposeStackGroup[]
  standalone: ContainerInfo[]
}

export function groupContainersV2(containers: ContainerInfo[]): ContainerLayout {
  const visible = containers.filter(c => !c.riot?.hide)
  const composeMap = new Map<string, ContainerInfo[]>()
  const standalone: ContainerInfo[] = []

  for (const c of visible) {
    const project = c.labels?.['com.docker.compose.project']
    if (project) {
      const list = composeMap.get(project) || []
      list.push(c)
      composeMap.set(project, list)
    } else {
      standalone.push(c)
    }
  }

  const composeStacks: ComposeStackGroup[] = []
  for (const [name, members] of composeMap) {
    const workDir = members.find(c => c.labels?.['com.docker.compose.project.working_dir'])
      ?.labels?.['com.docker.compose.project.working_dir'] ?? ''

    // Build network clusters within this stack
    const networkGraph = buildNetworkGraph(members)
    const memberNames = new Set(members.map(c => c.name))
    const inCluster = new Set<string>()
    const clusters: NetworkCluster[] = []

    for (const [parentName, deps] of networkGraph) {
      // Only cluster if the parent is in this same stack
      if (!memberNames.has(parentName)) continue
      const parent = members.find(c => c.name === parentName)
      if (!parent) continue
      inCluster.add(parent.id)
      for (const d of deps) inCluster.add(d.id)
      clusters.push({ parent, dependents: deps })
    }

    const loose = members.filter(c => !inCluster.has(c.id))
    const updatableCount = members.filter(c => c.update_available).length

    composeStacks.push({ name, workDir, containers: members, networkClusters: clusters, loose, updatableCount })
  }

  composeStacks.sort((a, b) => a.name.localeCompare(b.name))
  standalone.sort((a, b) => displayName(a.riot, a.name).localeCompare(displayName(b.riot, b.name)))

  return { composeStacks, standalone }
}

/** Returns the parent container name for containers using network_mode: container:<name> */
export function getNetworkParent(c: ContainerInfo): string | null {
  if (!c.network_mode) return null
  if (c.network_mode.startsWith('container:')) {
    return c.network_mode.slice('container:'.length)
  }
  return null
}

/** Builds a map of parent container name -> list of dependent container names */
export function buildNetworkGraph(containers: ContainerInfo[]): Map<string, ContainerInfo[]> {
  const graph = new Map<string, ContainerInfo[]>()
  for (const c of containers) {
    const parent = getNetworkParent(c)
    if (parent) {
      const deps = graph.get(parent) || []
      deps.push(c)
      graph.set(parent, deps)
    }
  }
  return graph
}

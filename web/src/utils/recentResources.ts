import { containerPath, stackPath } from './dockerUrlState'
export interface RecentResource {
  type: 'host' | 'container' | 'stack'
  key: string
}
export function resourceFromPath(path: string): RecentResource | undefined {
  const host = path.match(/^\/devices\/([^/]+)$/)
  if (host) {
    try {
      return { type: 'host', key: decodeURIComponent(host[1]) }
    } catch {
      return
    }
  }
  const resource = path.match(
    /^\/docker\/(containers|stacks)\/([^/]+)\/([^/]+)$/,
  )
  if (resource) {
    try {
      return {
        type: resource[1] === 'containers' ? 'container' : 'stack',
        key: `${decodeURIComponent(resource[2])}:${decodeURIComponent(resource[3])}`,
      }
    } catch {
      return
    }
  }
}
export function resourcePath(resource: RecentResource) {
  if (resource.type === 'host')
    return `/devices/${encodeURIComponent(resource.key)}`
  const colon = resource.key.indexOf(':')
  if (colon < 1 || colon === resource.key.length - 1) return
  return resource.type === 'container'
    ? containerPath(resource.key.slice(0, colon), resource.key.slice(colon + 1))
    : stackPath(resource.key.slice(0, colon), resource.key.slice(colon + 1))
}
/** Reads the first-release path array and the documented {type,key} format. */
export function parseRecentResources(value: unknown): RecentResource[] {
  if (!Array.isArray(value)) return []
  const result: RecentResource[] = []
  for (const entry of value) {
    const resource =
      typeof entry === 'string'
        ? resourceFromPath(entry)
        : entry &&
            typeof entry === 'object' &&
            ['host', 'container', 'stack'].includes(entry.type) &&
            typeof entry.key === 'string' &&
            entry.key.length <= 1024
          ? ({ type: entry.type, key: entry.key } as RecentResource)
          : undefined
    if (
      resource &&
      resourcePath(resource) &&
      !result.some((r) => r.type === resource.type && r.key === resource.key)
    )
      result.push(resource)
    if (result.length === 8) break
  }
  return result
}

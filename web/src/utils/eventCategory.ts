import type { Event } from '../types/models'

export type EventCategory = 'alerts' | 'docker' | 'agent' | 'security' | 'other'

/** Set of event types considered "security" by the activity river chip filter (AD-007). */
const SECURITY_TYPES = new Set([
  'auth_failure',
  'cert_expiring',
  'cert_expired',
  'disk_smart_failing',
  'disk_smart_temp',
])

/**
 * Maps an Event to one of the activity river filter chip categories (FR-054 / AD-007).
 *
 * - "alerts": severity warning or critical
 * - "docker": type starts with "container_"
 * - "agent": type starts with "agent_"
 * - "security": type in the SECURITY_TYPES set
 * - "other": everything else
 */
export function eventCategory(event: Event): EventCategory {
  if (event.severity === 'warning' || event.severity === 'critical') {
    return 'alerts'
  }
  if (event.type.startsWith('container_')) {
    return 'docker'
  }
  if (event.type.startsWith('agent_')) {
    return 'agent'
  }
  if (SECURITY_TYPES.has(event.type)) {
    return 'security'
  }
  return 'other'
}

import { describe, it, expect } from 'vitest'
import { eventCategory } from './eventCategory'
import type { Event } from '../types/models'

function evt(type: string, severity: Event['severity'] = 'info'): Event {
  return {
    id: 1,
    device_id: 'dev-1',
    type,
    severity,
    message: 'test',
    created_at: new Date().toISOString(),
  }
}

describe('[AC-026] eventCategory — activity river chip mapping (FR-054 / AD-007)', () => {
  it('maps warning severity to "alerts"', () => {
    expect(eventCategory(evt('some_event', 'warning'))).toBe('alerts')
  })

  it('maps critical severity to "alerts"', () => {
    expect(eventCategory(evt('some_event', 'critical'))).toBe('alerts')
  })

  it('maps container_* events to "docker"', () => {
    expect(eventCategory(evt('container_start'))).toBe('docker')
    expect(eventCategory(evt('container_stop'))).toBe('docker')
    expect(eventCategory(evt('container_die'))).toBe('docker')
  })

  it('maps agent_* events to "agent"', () => {
    expect(eventCategory(evt('agent_update_available'))).toBe('agent')
    expect(eventCategory(evt('agent_connected'))).toBe('agent')
  })

  it('maps auth_failure to "security"', () => {
    expect(eventCategory(evt('auth_failure'))).toBe('security')
  })

  it('maps cert_expiring to "security"', () => {
    expect(eventCategory(evt('cert_expiring'))).toBe('security')
  })

  it('maps cert_expired to "security"', () => {
    expect(eventCategory(evt('cert_expired'))).toBe('security')
  })

  it('maps disk_smart_failing to "security"', () => {
    expect(eventCategory(evt('disk_smart_failing'))).toBe('security')
  })

  it('maps disk_smart_temp to "security"', () => {
    expect(eventCategory(evt('disk_smart_temp'))).toBe('security')
  })

  it('maps info severity non-matching events to "other"', () => {
    expect(eventCategory(evt('service_stopped', 'info'))).toBe('other')
    expect(eventCategory(evt('probe_failed', 'info'))).toBe('other')
  })

  it('priority rule: severity wins over type prefix for warning+container', () => {
    // If both severity=warning AND type starts with container_, result is 'alerts'
    expect(eventCategory(evt('container_die', 'warning'))).toBe('alerts')
  })
})

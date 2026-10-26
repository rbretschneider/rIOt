import { describe, expect, it } from 'vitest'
import { getDeviceDetail, getDevices, getPatchStatus } from './demo-data'

describe('[EXTRA-UPD-001 AC-041] demo data', () => {
  const byHost = (hostname: string) => {
    const device = getDevices().find(d => d.hostname === hostname)!
    return getDeviceDetail(device.id)!.latest_telemetry!.data.updates!
  }

  it('has a host with a pending DietPi update', () => {
    const extras = byHost('pi-dns').extra_updaters!
    expect(extras).toHaveLength(1)
    expect(extras[0]).toMatchObject({ id: 'dietpi', status: 'update_available', readiness: 'enabled', installed_version: '9.20.1', available_version: '10.8.2' })
  })

  it('has a host whose DietPi status is unknown', () => {
    const extras = byHost('pi-cameras').extra_updaters!
    expect(extras[0]).toMatchObject({ id: 'dietpi', status: 'unknown', unknown_reason: 'check_disabled' })
  })

  it('has no extra updaters elsewhere', () => {
    expect(byHost('proxmox-01').extra_updaters).toBeUndefined()
  })

  it('serves a manual badge, a DietPi row and information items', () => {
    const rows = getPatchStatus()
    expect(rows.find(r => r.hostname === 'nas-synology')?.patch_badge?.kind).toBe('manual')
    const dns = rows.find(r => r.hostname === 'pi-dns')!
    expect(dns.extra_updaters?.[0].available_version).toBe('10.8.2')
    expect(dns.patch_badge?.explanation.info?.[0]).toMatchObject({ code: 'extra_update_pending', updater: 'dietpi' })
    const cams = rows.find(r => r.hostname === 'pi-cameras')!
    expect(cams.patch_badge?.explanation.info?.[0].code).toBe('extra_update_unknown')
    expect(cams.patch_badge?.explanation.needs_you).toEqual([])
  })

  it('never has a manual or auto badge with action items', () => {
    for (const r of getPatchStatus()) {
      const ex = r.patch_badge!.explanation
      expect(r.patch_badge!.kind === 'needs_you', r.hostname).toBe(ex.needs_you.length > 0)
    }
  })
})

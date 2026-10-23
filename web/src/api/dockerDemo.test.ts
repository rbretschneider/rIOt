import { expect, it, vi } from 'vitest'
vi.mock('../api/client', async () => {
  const demo = await import('./demo-data')
  return {
    api: {
      getDevices: async () => demo.getDevices(),
      getFleetContainers: async () => demo.getFleetContainers(),
      getDeviceContainers: async () => demo.getContainers(),
      sendCommand: async () => demo.getCommands()[0],
      getAutoUpdates: async (id: string) => demo.getAutoUpdatePolicies(id),
      setAutoUpdate: async (id: string, target: string, isStack: boolean, workDir: string, enabled: boolean) =>
        demo.setAutoUpdatePolicy(id, target, isStack, workDir, enabled),
    },
  }
})
import { dockerDemo } from './dockerDemo'
import { api } from '../api/client'
it('demonstrates current and legacy hosts without making network requests', async () => {
  const fetcher = vi.fn()
  vi.stubGlobal('fetch', fetcher)
  const hosts = await dockerDemo.hosts()
  const rows = await dockerDemo.containers()
  expect(rows.some((r) => !!r.started_at && !!r.ports?.length)).toBe(true)
  expect(
    rows.some((r) => r.device_id === hosts[1].device_id && !r.started_at),
  ).toBe(true)
  expect(hosts[1].capabilities).toEqual([])
  expect((await dockerDemo.stacks()).some((s) => s.state === 'down')).toBe(true)
  expect((await dockerDemo.activity()).entries.length).toBeGreaterThan(0)
  expect(
    (await dockerDemo.activityDetail('demo-update', hosts[0].device_id)).steps
      .length,
  ).toBeGreaterThan(0)
  await dockerDemo.inspect(hosts[0].device_id, 'web')
  await dockerDemo.history(hosts[0].device_id, 'web', '')
  expect(fetcher).not.toHaveBeenCalled()
  vi.unstubAllGlobals()
})
it('uses anonymous-only defaults and host confirmation for demo volume cleanup', async () => {
  const hosts = await dockerDemo.hosts()
  const preview = await dockerDemo.preview(hosts[0].device_id, {
    kind: 'volumes',
  })
  expect(preview.items).toHaveLength(2)
  expect(preview.items.map((i) => i.selected_default)).toEqual([true, false])
  expect(preview.confirmation.kind).toBe('host')
})
it('[AC-001][AC-002][AC-006] carries auto-update state and out-of-date counts', async () => {
  const rows = await dockerDemo.containers()
  expect(rows.every((r) => r.auto_update_policy?.target === r.container_name)).toBe(true)
  expect(rows.find((r) => r.container_name === 'plex' && r.update_status)?.auto_update_policy).toEqual({ target: 'plex', enabled: true })
  expect(rows.find((r) => r.container_name === 'traefik' && r.update_status)?.auto_update_policy).toEqual({ target: 'traefik', enabled: false, covered_by: 'networking' })
  const stacks = await dockerDemo.stacks()
  expect(stacks.every((s) => typeof s.out_of_date === 'number')).toBe(true)
  const media = stacks.find((s) => s.project === 'media' && s.device_id === rows[0].device_id)!
  expect(media.out_of_date).toBe(1)
  expect(stacks.find((s) => s.project === 'networking' && s.device_id === rows[0].device_id)?.auto_update).toBe(true)
})
it('[AC-003][AC-011] round-trips a switch and refuses a name clash like the server', async () => {
  const host = (await dockerDemo.hosts())[0].device_id
  await api.setAutoUpdate(host, 'grafana', false, '', true)
  expect((await dockerDemo.containers()).find((r) => r.container_name === 'grafana')?.auto_update_policy?.enabled).toBe(true)
  await api.setAutoUpdate(host, 'grafana', false, '', false)
  expect((await dockerDemo.containers()).find((r) => r.container_name === 'grafana')?.auto_update_policy?.enabled).toBe(false)
  await expect(api.setAutoUpdate(host, 'networking', false, '', true)).rejects.toMatchObject({ status: 409, code: 'auto_update_name_clash' })
})

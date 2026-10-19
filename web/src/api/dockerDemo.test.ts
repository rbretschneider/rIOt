import { expect, it, vi } from 'vitest'
vi.mock('../api/client', async () => {
  const demo = await import('./demo-data')
  return {
    api: {
      getDevices: async () => demo.getDevices(),
      getFleetContainers: async () => demo.getFleetContainers(),
      getDeviceContainers: async () => demo.getContainers(),
      sendCommand: async () => demo.getCommands()[0],
    },
  }
})
import { dockerDemo } from './dockerDemo'
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

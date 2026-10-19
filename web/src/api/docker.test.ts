import { beforeEach, expect, it, vi } from 'vitest'
import { dockerApi, DockerRequestError } from './docker'
const fetcher = vi.fn()
beforeEach(() => {
  fetcher.mockReset()
  vi.stubGlobal('fetch', fetcher)
  fetcher.mockResolvedValue({ ok: true, status: 200, json: async () => ({}) })
})
it('sends same-origin JSON for Docker mutations', async () => {
  await dockerApi.bulk({
    action: 'stop',
    targets: [{ device_id: 'host', container: 'web' }],
  })
  expect(fetcher).toHaveBeenCalledWith(
    '/api/v1/docker/bulk-actions',
    expect.objectContaining({
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        action: 'stop',
        targets: [{ device_id: 'host', container: 'web' }],
      }),
    }),
  )
})
it('encodes host/resource segments and marks side-effecting reads', async () => {
  await dockerApi.inspect('host 1', 'app:one')
  expect(fetcher).toHaveBeenCalledWith(
    '/api/v1/devices/host%201/docker/containers/app%3Aone/inspect',
    expect.objectContaining({
      method: 'GET',
      credentials: 'same-origin',
      headers: { 'X-Riot-Request': '1' },
    }),
  )
  await dockerApi.composeFile('host', 'app name', 2)
  expect(fetcher.mock.calls[1][0]).toBe(
    '/api/v1/devices/host/docker/stacks/app%20name/compose/files/2',
  )
})
it('preserves blocking-command identity for conflict links', async () => {
  fetcher.mockResolvedValue({
    ok: false,
    status: 409,
    json: async () => ({
      code: 'conflict',
      existing_command_id: 'cmd-1',
      error: 'Target locked',
    }),
  })
  await expect(
    dockerApi.execute('host', { preview_id: 'token', selected: ['id'] }),
  ).rejects.toEqual(
    expect.objectContaining({
      name: 'CommandConflictError',
      existingCommandId: 'cmd-1',
      message: 'Already in progress',
    }),
  )
  expect(new DockerRequestError('test', 'code')).toBeInstanceOf(Error)
})

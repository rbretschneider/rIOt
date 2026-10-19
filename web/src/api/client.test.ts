import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ApiError } from './errors'

// We need to test fetchJSON and the api object
// Since fetchJSON is not exported, we test through the api methods

describe('api client', () => {
  const originalFetch = globalThis.fetch
  const originalReload = window.location.reload

  beforeEach(() => {
    vi.restoreAllMocks()
    // Mock window.location.reload
    Object.defineProperty(window, 'location', {
      value: { ...window.location, reload: vi.fn() },
      writable: true,
    })
  })

  // Helper to dynamically import the client (fresh module per test group)
  async function getApi() {
    return (await import('./client')).api
  }

  async function getAuthApi() {
    return (await import('./client')).authApi
  }

  it('fetchJSON returns parsed JSON on success', async () => {
    const mockDevices = [{ id: 'dev-1', hostname: 'test' }]
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(mockDevices),
    })

    const api = await getApi()
    const result = await api.getDevices()
    expect(result).toEqual(mockDevices)
    expect(fetch).toHaveBeenCalledWith('/api/v1/devices', { credentials: 'same-origin' })
  })

  it('fetchJSON triggers reload on 401', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      statusText: 'Unauthorized',
    })

    const api = await getApi()
    await expect(api.getDevices()).rejects.toThrow('Unauthorized')
    expect(window.location.reload).toHaveBeenCalled()
  })

  it('fetchJSON throws on non-ok response', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
    })

    const api = await getApi()
    await expect(api.getDevices()).rejects.toThrow('HTTP 500')
  })

  it('getDevice calls correct URL', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ device: {}, latest_telemetry: null }),
    })

    const api = await getApi()
    await api.getDevice('abc-123')
    expect(fetch).toHaveBeenCalledWith('/api/v1/devices/abc-123', { credentials: 'same-origin' })
  })

  it('deleteDevice uses DELETE method', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
    })

    const api = await getApi()
    await api.deleteDevice('dev-1')
    expect(fetch).toHaveBeenCalledWith('/api/v1/devices/dev-1', {
      method: 'DELETE',
      credentials: 'same-origin',
    })
  })

  it('sendCommand sends POST with JSON body', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ id: 'cmd-1', status: 'pending' }),
    })

    const api = await getApi()
    await api.sendCommand('dev-1', 'docker_restart', { container_id: 'abc' })
    expect(fetch).toHaveBeenCalledWith('/api/v1/devices/dev-1/commands', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'docker_restart', params: { container_id: 'abc' } }),
    })
  })

  it('authApi.login returns true on success', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true })

    const authApi = await getAuthApi()
    const result = await authApi.login('password')
    expect(result).toBe(true)
  })

  it('authApi.login returns false on failure', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false })

    const authApi = await getAuthApi()
    const result = await authApi.login('wrong')
    expect(result).toBe(false)
  })

  it('authApi.check returns true when authenticated', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ authenticated: true }),
    })

    const authApi = await getAuthApi()
    const result = await authApi.check()
    expect(result).toBe(true)
  })
})

describe('[UIF AC-088] typed error carries status', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  async function rejectionFrom(status: number, statusText: string, call: (api: typeof import('./client').api) => Promise<unknown>) {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status, statusText })
    const { api } = await import('./client')
    return call(api).then(
      () => { throw new Error('expected the call to reject') },
      (e: unknown) => e,
    )
  }

  it('rejects a 404 with an Error whose status is 404 and message is exactly "HTTP 404: Not Found"', async () => {
    const e = await rejectionFrom(404, 'Not Found', api => api.getDevice('missing'))
    expect(e).toBeInstanceOf(Error)
    expect(e).toBeInstanceOf(ApiError)
    expect((e as ApiError).status).toBe(404)
    expect((e as ApiError).message).toBe('HTTP 404: Not Found')
  })

  it('keeps the plain "HTTP <status>" message for client mutations and carries the status', async () => {
    const e = await rejectionFrom(500, 'Internal Server Error', api => api.deleteDevice('dev-1'))
    expect(e).toBeInstanceOf(ApiError)
    expect((e as ApiError).status).toBe(500)
    expect((e as ApiError).message).toBe('HTTP 500')
  })

  it('uses the server error text for sendCommand while keeping the status', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      statusText: 'Conflict',
      json: () => Promise.resolve({ error: 'command already queued' }),
    })
    const { api } = await import('./client')
    const e = await api.sendCommand('dev-1', 'reboot').catch((err: unknown) => err)
    expect(e).toBeInstanceOf(ApiError)
    expect((e as ApiError).status).toBe(409)
    expect((e as ApiError).message).toBe('command already queued')
  })

  it('falls back to "HTTP <status>" for sendCommand when the body has no error', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      statusText: 'Bad Gateway',
      json: () => Promise.reject(new Error('not json')),
    })
    const { api } = await import('./client')
    const e = await api.sendCommand('dev-1', 'reboot').catch((err: unknown) => err)
    expect((e as ApiError).message).toBe('HTTP 502')
    expect((e as ApiError).status).toBe(502)
  })

  it('does not give a network failure a status (stays a native TypeError)', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))
    const { api } = await import('./client')
    const e = await api.getDevices().catch((err: unknown) => err)
    expect(e).toBeInstanceOf(TypeError)
    expect(e).not.toBeInstanceOf(ApiError)
  })

  it('re-exports the same ApiError class that api/errors defines', async () => {
    const client = await import('./client')
    expect(client.ApiError).toBe(ApiError)
  })

  it('settings fetchJSON and mutate throw ApiError with the unchanged message', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 404, statusText: 'Not Found' })
    const { settingsApi } = await import('./settings')
    const read = await settingsApi.getAlertRules().catch((err: unknown) => err)
    const write = await settingsApi.deleteAlertRule(1).catch((err: unknown) => err)
    expect(read).toBeInstanceOf(ApiError)
    expect((read as ApiError).message).toBe('HTTP 404: Not Found')
    expect((read as ApiError).status).toBe(404)
    expect(write).toBeInstanceOf(ApiError)
    expect((write as ApiError).message).toBe('HTTP 404: Not Found')
  })
})

describe('[UIF AC-089] 401 still reloads and stays a plain Error', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    vi.restoreAllMocks()
    Object.defineProperty(window, 'location', {
      value: { ...window.location, reload: vi.fn() },
      writable: true,
    })
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('fetchJSON reloads and throws a plain Error("Unauthorized"), not an ApiError', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 401, statusText: 'Unauthorized' })
    const { api } = await import('./client')
    const e = await api.getDevices().catch((err: unknown) => err)
    expect(window.location.reload).toHaveBeenCalledTimes(1)
    expect(e).toBeInstanceOf(Error)
    expect(e).not.toBeInstanceOf(ApiError)
    expect((e as Error).message).toBe('Unauthorized')
  })

  it('sendCommand reloads and throws a plain Error("Unauthorized")', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 401, statusText: 'Unauthorized' })
    const { api } = await import('./client')
    const e = await api.sendCommand('dev-1', 'reboot').catch((err: unknown) => err)
    expect(window.location.reload).toHaveBeenCalledTimes(1)
    expect(e).not.toBeInstanceOf(ApiError)
    expect((e as Error).message).toBe('Unauthorized')
  })

  it('settings fetchJSON reloads and throws a plain Error("Unauthorized")', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 401, statusText: 'Unauthorized' })
    const { settingsApi } = await import('./settings')
    const e = await settingsApi.getAlertRules().catch((err: unknown) => err)
    expect(window.location.reload).toHaveBeenCalledTimes(1)
    expect(e).not.toBeInstanceOf(ApiError)
    expect((e as Error).message).toBe('Unauthorized')
  })

  it('settings mutate reloads and throws a plain Error("Unauthorized")', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 401, statusText: 'Unauthorized' })
    const { settingsApi } = await import('./settings')
    const e = await settingsApi.deleteAlertRule(1).catch((err: unknown) => err)
    expect(window.location.reload).toHaveBeenCalledTimes(1)
    expect(e).not.toBeInstanceOf(ApiError)
    expect((e as Error).message).toBe('Unauthorized')
  })
})

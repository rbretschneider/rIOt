import { describe, it, expect, vi, beforeEach } from 'vitest'

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

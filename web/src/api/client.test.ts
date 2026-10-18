import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

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

// ---------------------------------------------------------------------------
// CMD-LIFECYCLE-001: command lifecycle client additions
// ---------------------------------------------------------------------------

describe('[AC-111] a 409 conflict carries the existing command id', () => {
  const originalFetch = globalThis.fetch
  const EXISTING = '11111111-1111-4111-8111-111111111111'

  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  async function getClient() {
    return await import('./client')
  }

  function conflictResponse(body: unknown) {
    return {
      ok: false,
      status: 409,
      statusText: 'Conflict',
      json: () => Promise.resolve(body),
    }
  }

  it('sendCommand rejects with CommandConflictError exposing existing_command_id and status', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(conflictResponse({
      error: 'a command is already in progress for this target',
      code: 'conflict',
      existing_command_id: EXISTING,
      existing_status: 'running',
      target_key: 'docker:container:web',
    }))

    const { api, CommandConflictError } = await getClient()
    const err = await api.sendCommand('dev-1', 'docker_update', { container: 'web' }).catch((e: unknown) => e)

    expect(err, 'a conflict must surface as CommandConflictError').toBeInstanceOf(CommandConflictError)
    const conflict = err as InstanceType<typeof CommandConflictError>
    expect(conflict.existingCommandId).toBe(EXISTING)
    expect(conflict.existingStatus).toBe('running')
    expect(conflict.targetKey).toBe('docker:container:web')
    expect(conflict.message).toBe('a command is already in progress for this target')
  })

  it('sendCommand keeps a plain Error for a 409 that is not a conflict', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue(conflictResponse({ error: 'something else', code: 'invalid_transition' }))

    const { api, CommandConflictError } = await getClient()
    const err = await api.sendCommand('dev-1', 'reboot').catch((e: unknown) => e)

    expect(err).toBeInstanceOf(Error)
    expect(err).not.toBeInstanceOf(CommandConflictError)
    expect((err as Error).message).toBe('something else')
  })

  it('bulkDockerUpdate rejects with CommandConflictError carrying the skipped targets when every target conflicts', async () => {
    const conflicts = [{ container_id: 'c1', target_key: 'docker:container:web', existing_command_id: EXISTING }]
    globalThis.fetch = vi.fn().mockResolvedValue(conflictResponse({
      error: 'a command is already in progress for this target',
      code: 'conflict',
      existing_command_id: EXISTING,
      conflicts,
    }))

    const { api, CommandConflictError } = await getClient()
    const err = await api.bulkDockerUpdate('dev-1', ['c1']).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(CommandConflictError)
    expect((err as InstanceType<typeof CommandConflictError>).existingCommandId).toBe(EXISTING)
    expect((err as InstanceType<typeof CommandConflictError>).conflicts).toEqual(conflicts)
  })

  it('bulkDockerUpdate returns the created command with its partial conflicts on 201', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: () => Promise.resolve({
        id: 'cmd-9',
        status: 'queued',
        conflicts: [{ container_id: 'c1', target_key: 'docker:container:web', existing_command_id: EXISTING }],
      }),
    })

    const { api } = await getClient()
    const result = await api.bulkDockerUpdate('dev-1', ['c1', 'c2'])

    expect(result.id).toBe('cmd-9')
    expect(result.conflicts).toHaveLength(1)
  })
})

describe('[AC-110] [AC-111] command lookup endpoints', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('getCommand fetches the command by id', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve({ id: 'cmd-1' }) })
    const { api } = await import('./client')
    await api.getCommand('dev-1', 'cmd-1')
    expect(fetch).toHaveBeenCalledWith('/api/v1/devices/dev-1/commands/cmd-1', { credentials: 'same-origin' })
  })

  it('getActiveCommands asks for active commands, optionally narrowed by an encoded target key', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve([]) })
    const { api } = await import('./client')

    await api.getActiveCommands('dev-1')
    expect(fetch).toHaveBeenLastCalledWith('/api/v1/devices/dev-1/commands?active=true', { credentials: 'same-origin' })

    await api.getActiveCommands('dev-1', "docker:container:it's 100%")
    const url = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[1][0] as string
    expect(url).toContain('active=true')
    expect(url, 'target key must travel as an encoded query parameter').toContain('target_key=docker%3Acontainer%3Ait%27s+100%25')
  })

  it('cancelCommand POSTs to the cancel endpoint and returns the cancelled command', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve({ id: 'cmd-1', status: 'cancelled' }) })
    const { api } = await import('./client')
    const cmd = await api.cancelCommand('dev-1', 'cmd-1')
    expect(fetch).toHaveBeenCalledWith('/api/v1/devices/dev-1/commands/cmd-1/cancel', { method: 'POST', credentials: 'same-origin' })
    expect(cmd.status).toBe('cancelled')
  })

  it('cancelCommand surfaces the server message when the command can no longer be cancelled', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: () => Promise.resolve({ error: 'command cannot be cancelled after it has been dispatched', code: 'not_cancellable', status: 'dispatched' }),
    })
    const { api } = await import('./client')
    await expect(api.cancelCommand('dev-1', 'cmd-1')).rejects.toThrow('command cannot be cancelled after it has been dispatched')
  })
})

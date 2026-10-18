import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as realClient from './client'
import * as demoClient from './demo-client'
import * as realSettings from './settings'
import * as demoSettings from './demo-settings'
import * as demoData from './demo-data'
import { ApiError } from './errors'
import { FEATURES } from '../hooks/featureCatalog'

// These tests import the real and the demo modules side by side (vitest has no demo alias),
// which is why they live in a test file and nowhere in the production graph.

const sorted = (o: object) => Object.keys(o).sort()

describe('[UIF AC-127] demo api parity', () => {
  it('exposes exactly the same api methods as the real client', () => {
    expect(sorted(demoClient.api)).toEqual(sorted(realClient.api))
  })

  it('exposes exactly the same authApi methods as the real client', () => {
    expect(sorted(demoClient.authApi)).toEqual(sorted(realClient.authApi))
  })

  it('exposes the same value exports (api, authApi, ApiError)', () => {
    expect(sorted(demoClient)).toEqual(sorted(realClient))
    expect(sorted(realClient)).toEqual(['ApiError', 'api', 'authApi'])
  })

  it('re-exports the one shared ApiError class', () => {
    expect(demoClient.ApiError).toBe(ApiError)
    expect(realClient.ApiError).toBe(ApiError)
  })

  it('implements every method as a function', () => {
    for (const [name, fn] of Object.entries(demoClient.api)) {
      expect(typeof fn, `demo api.${name}`).toBe('function')
    }
  })
})

describe('[UIF AC-128] demo settings parity', () => {
  it('exposes exactly the same settingsApi methods as the real client', () => {
    expect(sorted(demoSettings.settingsApi)).toEqual(sorted(realSettings.settingsApi))
  })

  it('exposes the same value exports', () => {
    expect(sorted(demoSettings)).toEqual(sorted(realSettings))
  })
})

describe('[UIF AC-129] demo heartbeats per device ascending', () => {
  it('returns a non-empty, time-ascending series for every demo device', async () => {
    const response = await demoClient.api.getFleetHeartbeats()
    const devices = demoData.getDevices()
    expect(Object.keys(response.devices).sort()).toEqual(devices.map(d => d.id).sort())
    for (const device of devices) {
      const series = response.devices[device.id]
      expect(series.length, `${device.hostname} has no heartbeats`).toBeGreaterThan(1)
      const times = series.map(h => Date.parse(h.timestamp))
      expect(times, `${device.hostname} is not ascending`).toEqual([...times].sort((a, b) => a - b))
      expect(series.every(h => h.device_id === device.id)).toBe(true)
    }
  })

  it('carries the metrics the fleet charts plot, within sane ranges and with some variation', async () => {
    const response = await demoClient.api.getFleetHeartbeats()
    const series = Object.values(response.devices)[0]
    for (const hb of series) {
      expect(hb.data.cpu_percent).toBeGreaterThan(0)
      expect(hb.data.cpu_percent).toBeLessThan(100)
      expect(hb.data.mem_percent).toBeGreaterThan(0)
      expect(hb.data.net_rx_bytes_sec).toBeGreaterThan(0)
      expect(hb.data.net_tx_bytes_sec).toBeGreaterThan(0)
      expect(hb.data.disk_io_percent).toBeGreaterThan(0)
    }
    expect(new Set(series.map(h => h.data.cpu_percent)).size, 'cpu should not be a flat line').toBeGreaterThan(5)
  })

  it('is deterministic for a given snapshot', () => {
    const snapshot = demoData.getDeviceHistory(demoData.getDevices()[0].id)[0]
    expect(demoData.toHeartbeatData(snapshot, 3)).toEqual(demoData.toHeartbeatData(snapshot, 3))
  })

  it('serves the single-device heartbeat history oldest first too', async () => {
    const id = demoData.getDevices()[0].id
    const history = await demoClient.api.getHeartbeatHistory(id)
    const times = history.map(h => Date.parse(h.timestamp))
    expect(times).toEqual([...times].sort((a, b) => a - b))
  })
})

describe('[UIF AC-130] demo containers and toggles', () => {
  it('returns containers attributed to the first demo device', async () => {
    const rows = await demoClient.api.getFleetContainers()
    const host = demoData.getDevices()[0]
    expect(rows.length).toBeGreaterThan(0)
    expect(rows.every(r => r.device_id === host.id && r.hostname === host.hostname)).toBe(true)
    expect(rows.find(r => r.container_name === 'plex')?.stack).toBe('Media')
  })

  it('enables every feature toggle', async () => {
    const toggles = await demoSettings.settingsApi.getFeatureToggles()
    expect(Object.keys(toggles).sort()).toEqual(Object.keys(FEATURES).sort())
    expect(Object.values(toggles).every(v => v === true)).toBe(true)
  })

  it('returns an automation config and TLS SANs the settings pages can render', async () => {
    const config = await demoSettings.settingsApi.getAutomationConfig()
    expect(config.os_patch.mode).toBe('window')
    expect(config.docker_update.mode).toBe('anytime')
    expect(await demoSettings.settingsApi.getTLSSANs()).toEqual({ sans: [] })
  })

  it('returns device probes for the Docker host only, with results', async () => {
    const host = demoData.getDevices()[0]
    const forHost = await demoClient.api.getDeviceProbes(host.id)
    const forOther = await demoClient.api.getDeviceProbes('not-a-device')
    expect(forHost.length).toBe(2)
    expect(forOther).toEqual([])
    expect((await demoClient.api.getAllDeviceProbes()).every(p => p.device_hostname === host.hostname)).toBe(true)
    expect((await demoClient.api.getDeviceProbeResults(host.id, forHost[0].id)).length).toBeGreaterThan(0)
  })

  it('returns container log lines and a device summary', async () => {
    const container = demoData.getContainers()[0]
    const logs = await demoClient.api.getContainerLogs('dev', container.id)
    expect(logs.length).toBeGreaterThan(5)
    expect(logs.every(l => l.container_id === container.id)).toBe(true)
    const host = demoData.getDevices()[0]
    expect(await demoClient.api.getDeviceSummary(host.id)).toContain(`# ${host.hostname}`)
  })
})

describe('[UIF AC-131] demo mutations never fetch', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  const hostId = () => demoData.getDevices()[0].id

  // Arguments plausible enough for every mutating method; the keys must stay in step with the clients.
  const apiCalls: Record<string, () => unknown[]> = {
    deleteDevice: () => [hostId(), true],
    acknowledgeEvent: () => [1],
    acknowledgeAllEvents: () => [],
    sendCommand: () => [hostId(), 'reboot', {}],
    createProbe: () => [{ name: 'p' }],
    updateProbe: () => [1, { name: 'p' }],
    deleteProbe: () => [1],
    runProbe: () => [1],
    bulkUpdateAgents: () => ['0.9.0'],
    bulkPatchDevices: () => ['full'],
    updateDeviceTags: () => [hostId(), ['a']],
    updateDeviceLocation: () => [hostId(), 'rack 2'],
    setAutoPatch: () => [hostId(), true],
    setAutoUpdate: () => [hostId(), 'plex', false, '', true],
    deleteAutoUpdate: () => [hostId(), 1],
    bulkDockerUpdate: () => [hostId(), ['c1']],
    createDeviceProbe: () => [hostId(), { name: 'p' }],
    updateDeviceProbe: () => [hostId(), 101, { name: 'p' }],
    deleteDeviceProbe: () => [hostId(), 101],
    runDeviceProbe: () => [hostId(), 101],
  }

  const settingsCalls: Record<string, () => unknown[]> = {
    createAlertRule: () => [{ name: 'r' }],
    updateAlertRule: () => [1, { name: 'r' }],
    deleteAlertRule: () => [1],
    createNotificationChannel: () => [{ name: 'c' }],
    updateNotificationChannel: () => [1, { name: 'c' }],
    deleteNotificationChannel: () => [1],
    testNotificationChannel: () => [1],
    revokeCert: () => ['serial'],
    createBootstrapKey: () => [{ label: 'k', expires_in_hours: 1 }],
    deleteBootstrapKey: () => ['hash'],
    saveRegistrationKey: () => ['key'],
    changePassword: () => ['old', 'new'],
    clearLogs: () => [],
    saveServerErrorAlert: () => [{ enabled: false }],
    saveTLSSANs: () => [['a.lan']],
    regenerateTLS: () => [],
    saveFeatureToggles: () => [{ docker: true }],
    saveAutomationConfig: () => [demoData.getAutomationConfig()],
  }

  const authCalls: Record<string, () => unknown[]> = {
    login: () => ['pw'],
    logout: () => [],
  }

  const isReader = (name: string) => name.startsWith('get') || name === 'check'

  it('has an argument entry for every non-read method (so a new mutator cannot slip past)', () => {
    expect(Object.keys(apiCalls).sort()).toEqual(Object.keys(demoClient.api).filter(n => !isReader(n)).sort())
    expect(Object.keys(settingsCalls).sort()).toEqual(
      Object.keys(demoSettings.settingsApi).filter(n => !isReader(n)).sort(),
    )
    expect(Object.keys(authCalls).sort()).toEqual(Object.keys(demoClient.authApi).filter(n => !isReader(n)).sort())
  })

  it('resolves every mutating method without ever calling fetch', async () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)
    const invoke = (target: Record<string, unknown>, calls: Record<string, () => unknown[]>) =>
      Object.entries(calls).map(([name, args]) => {
        const fn = target[name] as (...a: unknown[]) => Promise<unknown>
        return fn(...args()).then(() => name)
      })

    const pending = Promise.all([
      ...invoke(demoClient.api as unknown as Record<string, unknown>, apiCalls),
      ...invoke(demoSettings.settingsApi as unknown as Record<string, unknown>, settingsCalls),
      ...invoke(demoClient.authApi as unknown as Record<string, unknown>, authCalls),
    ])
    await vi.runAllTimersAsync()
    const resolved = await pending

    expect(resolved.length).toBe(Object.keys(apiCalls).length + Object.keys(settingsCalls).length + Object.keys(authCalls).length)
    expect(fetchSpy, 'a demo mutation reached the network').not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('returns plausible payloads for the new mutations', async () => {
    const bulk = demoClient.api.bulkDockerUpdate(hostId(), ['c1'])
    const location = demoClient.api.updateDeviceLocation(hostId(), 'rack 2')
    const patch = demoClient.api.setAutoPatch(hostId(), true)
    const tags = demoClient.api.updateDeviceTags(hostId(), ['a', 'b'])
    const sans = demoSettings.settingsApi.saveTLSSANs(['a.lan'])
    const regen = demoSettings.settingsApi.regenerateTLS()
    await vi.runAllTimersAsync()
    expect((await bulk).status).toBe('queued')
    expect(await location).toEqual({ location: 'rack 2' })
    expect(await patch).toEqual({ auto_patch: true })
    expect(await tags).toEqual({ tags: ['a', 'b'] })
    expect(await sans).toEqual({ ok: true, sans: ['a.lan'] })
    expect((await regen).message).toBe('Demo mode: certificate not regenerated')
  })
})

describe('[UIF AC-132] unknown demo device is 404', () => {
  it('rejects getDevice for an unknown id with an ApiError whose status is 404', async () => {
    const error = await demoClient.api.getDevice('does-not-exist').catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ApiError)
    expect((error as ApiError).status).toBe(404)
    expect((error as ApiError).message).toBe('HTTP 404: Not Found')
  })

  it('rejects getContainerDetail for an unknown container with a 404', async () => {
    const error = await demoClient.api.getContainerDetail('any', 'nope').catch((e: unknown) => e)
    expect((error as ApiError).status).toBe(404)
  })

  it('still resolves a known device and container', async () => {
    const host = demoData.getDevices()[0]
    expect((await demoClient.api.getDevice(host.id)).device.hostname).toBe(host.hostname)
    const container = demoData.getContainers()[0]
    expect((await demoClient.api.getContainerDetail(host.id, container.short_id)).name).toBe(container.name)
  })
})

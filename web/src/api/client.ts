import type { AlertRule, AutoUpdatePolicy, Command, ContainerInfo, Device, DeviceDetailResponse, Event, FleetSummary, PendingUpdate, Probe, ProbeResult, ProbeWithResult, TelemetrySnapshot, UpdateInfo } from '../types/models'

const BASE = '/api/v1'

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { credentials: 'same-origin' })
  if (res.status === 401) {
    window.location.reload()
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}: ${res.statusText}`)
  }
  return res.json()
}

export interface DevicePatchInfo {
  device_id: string
  hostname: string
  pending_updates: number
  security_count: number
  package_manager?: string
  updates?: PendingUpdate[]
}

export const api = {
  getDevices: () => fetchJSON<Device[]>(`${BASE}/devices`),

  getDevice: (id: string) => fetchJSON<DeviceDetailResponse>(`${BASE}/devices/${id}`),

  getDeviceHistory: (id: string, limit = 50, offset = 0) =>
    fetchJSON<TelemetrySnapshot[]>(`${BASE}/devices/${id}/history?limit=${limit}&offset=${offset}`),

  deleteDevice: async (id: string, uninstall?: boolean) => {
    const params = uninstall ? '?uninstall=true' : ''
    const res = await fetch(`${BASE}/devices/${id}${params}`, { method: 'DELETE', credentials: 'same-origin' })
    if (res.status === 401) {
      window.location.reload()
      throw new Error('Unauthorized')
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
  },

  getSummary: () => fetchJSON<FleetSummary>(`${BASE}/summary`),

  getEvents: (limit = 100, offset = 0) =>
    fetchJSON<Event[]>(`${BASE}/events?limit=${limit}&offset=${offset}`),

  getUnreadEventCount: () =>
    fetchJSON<{ count: number }>(`${BASE}/events/unread-count`),

  acknowledgeEvent: async (id: number) => {
    const res = await fetch(`${BASE}/events/${id}/acknowledge`, { method: 'POST', credentials: 'same-origin' })
    if (res.status === 401) { window.location.reload(); throw new Error('Unauthorized') }
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  acknowledgeAllEvents: async () => {
    const res = await fetch(`${BASE}/events/acknowledge-all`, { method: 'POST', credentials: 'same-origin' })
    if (res.status === 401) { window.location.reload(); throw new Error('Unauthorized') }
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  getHealth: () => fetchJSON<{ status: string; database: boolean }>('/health'),

  getServerUpdate: () => fetchJSON<UpdateInfo>(`${BASE}/update/server`),

  getDeviceContainers: (id: string) =>
    fetchJSON<ContainerInfo[]>(`${BASE}/devices/${id}/containers`),

  getContainerDetail: (id: string, cid: string) =>
    fetchJSON<ContainerInfo>(`${BASE}/devices/${id}/containers/${cid}`),

  sendCommand: async (deviceId: string, action: string, params: Record<string, unknown> = {}): Promise<Command> => {
    const res = await fetch(`${BASE}/devices/${deviceId}/commands`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, params }),
    })
    if (res.status === 401) {
      window.location.reload()
      throw new Error('Unauthorized')
    }
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    return res.json()
  },

  getDeviceCommands: (id: string, limit = 50) =>
    fetchJSON<Command[]>(`${BASE}/devices/${id}/commands?limit=${limit}`),

  // Probes
  getProbes: () => fetchJSON<ProbeWithResult[]>(`${BASE}/probes`),

  createProbe: async (probe: Partial<Probe>): Promise<Probe> => {
    const res = await fetch(`${BASE}/probes`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(probe),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  updateProbe: async (id: number, probe: Partial<Probe>): Promise<Probe> => {
    const res = await fetch(`${BASE}/probes/${id}`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(probe),
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  deleteProbe: async (id: number) => {
    const res = await fetch(`${BASE}/probes/${id}`, { method: 'DELETE', credentials: 'same-origin' })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
  },

  runProbe: async (id: number): Promise<ProbeResult> => {
    const res = await fetch(`${BASE}/probes/${id}/run`, { method: 'POST', credentials: 'same-origin' })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  getProbeResults: (id: number, limit = 100) =>
    fetchJSON<ProbeResult[]>(`${BASE}/probes/${id}/results?limit=${limit}`),

  // Fleet
  getAgentVersions: () =>
    fetchJSON<{ version: string; count: number }[]>(`${BASE}/fleet/agent-versions`),

  getPatchStatus: () =>
    fetchJSON<DevicePatchInfo[]>(`${BASE}/fleet/patch-status`),

  getPatchStatusDetail: () =>
    fetchJSON<DevicePatchInfo[]>(`${BASE}/fleet/patch-status?detail=true`),

  getSecurityOverview: () =>
    fetchJSON<{
      total_devices: number
      devices_reporting: number
      total_failed_logins: number
      total_logged_in: number
      firewall_active: number
      firewall_inactive: number
      selinux_enforcing: number
      apparmor_enabled: number
    }>(`${BASE}/security/overview`),

  getSecurityDevices: () =>
    fetchJSON<{
      device_id: string
      hostname: string
      status: string
      selinux: string
      apparmor: string
      firewall_status: string
      failed_logins_24h: number
      logged_in_users: number
      open_ports: number[]
    }[]>(`${BASE}/security/devices`),

  bulkUpdateAgents: async (version: string): Promise<{ sent: number; skipped: number; total: number }> => {
    const res = await fetch(`${BASE}/fleet/bulk-update`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version }),
    })
    if (res.status === 401) {
      window.location.reload()
      throw new Error('Unauthorized')
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  getDeviceAlertRules: (id: string) =>
    fetchJSON<AlertRule[]>(`${BASE}/devices/${id}/alert-rules`),

  getHeartbeatHistory: (id: string, hours = 24) =>
    fetchJSON<import('../types/models').Heartbeat[]>(`${BASE}/devices/${id}/heartbeats?hours=${hours}`),

  getDeviceLogs: (id: string, priority = 7, limit = 100) =>
    fetchJSON<import('../types/models').LogEntry[]>(`${BASE}/devices/${id}/logs?priority=${priority}&limit=${limit}`),

  updateDeviceTags: async (id: string, tags: string[]): Promise<{ tags: string[] }> => {
    const res = await fetch(`${BASE}/devices/${id}/tags`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tags }),
    })
    if (res.status === 401) { window.location.reload(); throw new Error('Unauthorized') }
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  getAutoUpdates: (id: string) =>
    fetchJSON<AutoUpdatePolicy[]>(`${BASE}/devices/${id}/auto-updates`),

  setAutoUpdate: async (deviceId: string, target: string, isStack: boolean, composeWorkDir: string, enabled: boolean) => {
    const res = await fetch(`${BASE}/devices/${deviceId}/auto-updates`, {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target, is_stack: isStack, compose_work_dir: composeWorkDir, enabled }),
    })
    if (res.status === 401) { window.location.reload(); throw new Error('Unauthorized') }
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  deleteAutoUpdate: async (deviceId: string, target: string) => {
    const res = await fetch(`${BASE}/devices/${deviceId}/auto-updates/${encodeURIComponent(target)}`, {
      method: 'DELETE',
      credentials: 'same-origin',
    })
    if (res.status === 401) { window.location.reload(); throw new Error('Unauthorized') }
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },

  bulkPatchDevices: async (mode: string = 'full'): Promise<{ sent: number; queued: number; skipped: number; total: number }> => {
    const res = await fetch(`${BASE}/fleet/bulk-patch`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode }),
    })
    if (res.status === 401) {
      window.location.reload()
      throw new Error('Unauthorized')
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    return res.json()
  },
}

export const authApi = {
  login: async (password: string): Promise<boolean> => {
    const res = await fetch(`${BASE}/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ password }),
    })
    return res.ok
  },

  logout: async (): Promise<void> => {
    await fetch(`${BASE}/auth/logout`, {
      method: 'POST',
      credentials: 'same-origin',
    }).catch(() => {})
  },

  check: async (): Promise<boolean> => {
    try {
      const res = await fetch(`${BASE}/auth/check`, { credentials: 'same-origin' })
      const data = await res.json()
      return data.authenticated === true
    } catch {
      return false
    }
  },
}

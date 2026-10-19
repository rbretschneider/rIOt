/**
 * Demo replacement for api/client — returns mock data, mutations are delayed no-ops.
 * Vite aliases this module over client.ts when VITE_DEMO=true.
 *
 * `api` is typed as the real client, so a method added to client.ts and forgotten
 * here fails `tsc`. Nothing in this file may call fetch: demo mutations only wait and
 * resolve with plausible payloads (asserted by api/demo-parity.test.ts).
 */
import * as demo from './demo-data'
import { ApiError } from './errors'
import type { ApiClient, AuthClient, DevicePatchInfo } from './client'
import type { Command } from '../types/models'

const delay = (ms = 400) => new Promise(r => setTimeout(r, ms))

const notFound = () => Promise.reject(new ApiError(404, 'HTTP 404: Not Found'))

export { type DevicePatchInfo }
// Re-exported so the demo and real client modules expose the same value exports.
export { ApiError }
// Shared with client.ts so `instanceof` and the demo build agree on one class.
export { CommandConflictError } from './client'

export const api: ApiClient = {
  getDevices: () => Promise.resolve(demo.getDevices()),

  // OIDC-001: SSO is never available in demo builds — the availability
  // query itself is disabled in demo mode (Login.tsx), but this keeps the
  // demo build typechecking against the same `api` shape as client.ts.
  getSSOAvailability: () => Promise.resolve({ available: false, label: '' }),
  getDevice: (id: string) => {
    const detail = demo.getDeviceDetail(id)
    return detail ? Promise.resolve(detail) : notFound()
  },
  getDeviceHistory: (id: string) => Promise.resolve(demo.getDeviceHistory(id)),
  deleteDevice: async () => { await delay(); },
  getSummary: () => Promise.resolve(demo.getSummary()),
  getEvents: () => Promise.resolve(demo.getEvents()),
  getUnreadEventCount: () => Promise.resolve({ count: demo.getUnreadCount() }),
  acknowledgeEvent: async () => { await delay(); return { status: 'ok' } },
  acknowledgeAllEvents: async () => { await delay(); return { status: 'ok' } },
  getHealth: () => Promise.resolve(demo.getHealth()),
  // The real client types this as the telemetry-side UpdateInfo merged with the server-side
  // one (two interfaces share a name in types/models), so the extra fields are present here too.
  getServerUpdate: () => Promise.resolve({
    total_installed: 0,
    pending_updates: 0,
    pending_security_count: 0,
    pending_kernel_update: false,
    unattended_upgrades: false,
    ...demo.getServerUpdate(),
  }),
  getDeviceContainers: () => Promise.resolve(demo.getContainers()),
  getContainerDetail: (_id: string, cid: string) => {
    const container = demo.getContainers().find(c => c.id === cid || c.short_id === cid)
    return container ? Promise.resolve(container) : notFound()
  },
  getContainerLogs: (_deviceId: string, containerID: string) => Promise.resolve(demo.getContainerLogs(containerID)),
  sendCommand: async () => { await delay(); return demo.getCommands()[0] },
  getDeviceCommands: (_id: string, _params?: { limit?: number; offset?: number; status?: string; action?: string }) => Promise.resolve(demo.getCommands()),
  getCommandOutput: () => Promise.resolve([]),
  // CMD-LIFECYCLE-001: demo commands are never in flight, so there is nothing to track or cancel.
  getCommand: (_deviceId: string, commandId: string): Promise<Command> => Promise.resolve(demo.getCommands().find(c => c.id === commandId) ?? demo.getCommands()[0]),
  getActiveCommands: (_deviceId: string, _targetKey?: string): Promise<Command[]> => Promise.resolve([]),
  cancelCommand: async (_deviceId: string, _commandId: string): Promise<Command> => { await delay(); return demo.getCommands()[0] },
  getProbes: () => Promise.resolve(demo.getProbes()),
  createProbe: async (probe: any) => { await delay(); return { id: 99, ...probe, created_at: new Date().toISOString(), updated_at: new Date().toISOString() } },
  updateProbe: async (_id: number, probe: any) => { await delay(); return { id: _id, ...probe, updated_at: new Date().toISOString() } },
  deleteProbe: async () => { await delay(); },
  runProbe: async (id: number) => { await delay(); const p = demo.getProbes().find(p => p.id === id); return p?.latest_result ?? { id: 0, probe_id: id, success: true, latency_ms: 10, error_msg: '', metadata: {}, created_at: new Date().toISOString() } },
  getProbeResults: (id: number) => Promise.resolve(demo.getProbeResults(id)),
  getAgentVersions: () => Promise.resolve(demo.getAgentVersions()),
  getPatchStatus: () => Promise.resolve(demo.getPatchStatus()),
  getPatchStatusDetail: () => Promise.resolve(demo.getPatchStatus()),
  getSecurityOverview: () => Promise.resolve(demo.getSecurityOverview()),
  getSecurityDevices: () => Promise.resolve(demo.getSecurityDevices()),
  bulkUpdateAgents: async () => { await delay(); return { sent: 2, skipped: 6, total: 8 } },
  getSecurityScore: (id: string) => Promise.resolve(demo.getSecurityScore(id)),
  bulkPatchDevices: async () => { await delay(); return { sent: 3, queued: 0, skipped: 5, total: 8 } },
  getDeviceAlertRules: () => Promise.resolve(demo.getAlertRules()),
  getHeartbeatHistory: (id: string) =>
    Promise.resolve(
      demo.getDeviceHistory(id).slice().reverse().map((s, i) => ({
        id: s.id, device_id: s.device_id, timestamp: s.timestamp, data: demo.toHeartbeatData(s, i),
      })),
    ),
  getDeviceLogs: () => Promise.resolve([]),
  updateDeviceTags: async (_id: string, tags: string[]) => { await delay(); return { tags } },
  updateDeviceLocation: async (_id: string, location: string) => { await delay(); return { location } },
  setAutoPatch: async (_id: string, enabled: boolean) => { await delay(); return { auto_patch: enabled } },
  getContainerMetricHistory: () => Promise.resolve([]),
  getAutoUpdates: () => Promise.resolve([]),
  setAutoUpdate: async () => { await delay(); },
  deleteAutoUpdate: async () => { await delay(); },
  bulkDockerUpdate: async () => { await delay(); return { ...demo.getCommands()[0], action: 'docker_bulk_update', status: 'queued' } },

  // Device probes
  getAllDeviceProbes: () => Promise.resolve(demo.getDeviceProbes()),
  getDeviceProbes: (deviceId: string) => Promise.resolve(demo.getDeviceProbes().filter(p => p.device_id === deviceId)),
  createDeviceProbe: async (deviceId: string, probe) => {
    await delay()
    const now = new Date().toISOString()
    return { id: 199, name: '', type: 'shell', enabled: true, config: {}, assertions: [], interval_seconds: 60, timeout_seconds: 10, ...probe, device_id: deviceId, created_at: now, updated_at: now }
  },
  updateDeviceProbe: async (deviceId: string, probeId: number, probe) => {
    await delay()
    return { id: probeId, name: '', type: 'shell', enabled: true, config: {}, assertions: [], interval_seconds: 60, timeout_seconds: 10, ...probe, device_id: deviceId, created_at: new Date().toISOString(), updated_at: new Date().toISOString() }
  },
  deleteDeviceProbe: async () => { await delay(); },
  runDeviceProbe: async (deviceId: string, probeId: number) => {
    await delay()
    return demo.getDeviceProbes().find(p => p.id === probeId)?.latest_result
      ?? { id: 0, probe_id: probeId, device_id: deviceId, success: true, latency_ms: 10, output: {}, created_at: new Date().toISOString() }
  },
  getDeviceProbeResults: (_deviceId: string, probeId: number) => Promise.resolve(demo.getDeviceProbeResults(probeId)),

  getDeviceSummary: (id: string) => Promise.resolve(demo.getDeviceSummaryMarkdown(id)),

  // Fleet dashboard
  getFleetHeartbeats: () => Promise.resolve(demo.getFleetHeartbeats()),
  getFleetContainers: () => Promise.resolve(demo.getFleetContainers()),
}

export const authApi: AuthClient = {
  login: async () => true,
  logout: async () => {},
  check: async () => true,
}

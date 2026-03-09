/**
 * Demo replacement for api/client — returns mock data, mutations are delayed no-ops.
 * Vite aliases this module over client.ts when VITE_DEMO=true.
 */
import * as demo from './demo-data'
import type { DevicePatchInfo } from './client'

const delay = (ms = 400) => new Promise(r => setTimeout(r, ms))

export { type DevicePatchInfo }

export const api = {
  getDevices: () => Promise.resolve(demo.getDevices()),
  getDevice: (id: string) => Promise.resolve(demo.getDeviceDetail(id)!),
  getDeviceHistory: (id: string) => Promise.resolve(demo.getDeviceHistory(id)),
  deleteDevice: async () => { await delay(); },
  getSummary: () => Promise.resolve(demo.getSummary()),
  getEvents: () => Promise.resolve(demo.getEvents()),
  getUnreadEventCount: () => Promise.resolve({ count: demo.getUnreadCount() }),
  acknowledgeEvent: async () => { await delay(); return { status: 'ok' } },
  acknowledgeAllEvents: async () => { await delay(); return { status: 'ok' } },
  getHealth: () => Promise.resolve(demo.getHealth()),
  getServerUpdate: () => Promise.resolve(demo.getServerUpdate()),
  getDeviceContainers: () => Promise.resolve(demo.getContainers()),
  getContainerDetail: (_id: string, cid: string) => Promise.resolve(demo.getContainers().find(c => c.id === cid || c.short_id === cid)!),
  sendCommand: async () => { await delay(); return demo.getCommands()[0] },
  getDeviceCommands: () => Promise.resolve(demo.getCommands()),
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
  bulkPatchDevices: async () => { await delay(); return { sent: 3, queued: 0, skipped: 5, total: 8 } },
}

export const authApi = {
  login: async () => true,
  logout: async () => {},
  check: async () => true,
}

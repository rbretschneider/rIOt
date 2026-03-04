import type { Device, DeviceDetailResponse, Event, FleetSummary, TelemetrySnapshot } from '../types/models'

const BASE = '/api/v1'

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}: ${res.statusText}`)
  }
  return res.json()
}

export const api = {
  getDevices: () => fetchJSON<Device[]>(`${BASE}/devices`),

  getDevice: (id: string) => fetchJSON<DeviceDetailResponse>(`${BASE}/devices/${id}`),

  getDeviceHistory: (id: string, limit = 50, offset = 0) =>
    fetchJSON<TelemetrySnapshot[]>(`${BASE}/devices/${id}/history?limit=${limit}&offset=${offset}`),

  deleteDevice: async (id: string) => {
    const res = await fetch(`${BASE}/devices/${id}`, { method: 'DELETE' })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
  },

  getSummary: () => fetchJSON<FleetSummary>(`${BASE}/summary`),

  getEvents: (limit = 100, offset = 0) =>
    fetchJSON<Event[]>(`${BASE}/events?limit=${limit}&offset=${offset}`),

  getHealth: () => fetchJSON<{ status: string; database: boolean }>('/health'),
}

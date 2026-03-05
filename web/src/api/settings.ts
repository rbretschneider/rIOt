import type { AlertRule, NotificationChannel, NotificationLog } from '../types/models'

const BASE = '/api/v1/settings'

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { credentials: 'same-origin' })
  if (res.status === 401) {
    window.location.reload()
    throw new Error('Unauthorized')
  }
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`)
  return res.json()
}

async function mutate<T>(url: string, method: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    credentials: 'same-origin',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (res.status === 401) {
    window.location.reload()
    throw new Error('Unauthorized')
  }
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${res.statusText}`)
  return res.json()
}

export const settingsApi = {
  // Alert Rules
  getAlertRules: () => fetchJSON<AlertRule[]>(`${BASE}/alert-rules`),

  createAlertRule: (rule: Partial<AlertRule>) =>
    mutate<AlertRule>(`${BASE}/alert-rules`, 'POST', rule),

  updateAlertRule: (id: number, rule: Partial<AlertRule>) =>
    mutate<AlertRule>(`${BASE}/alert-rules/${id}`, 'PUT', rule),

  deleteAlertRule: (id: number) =>
    mutate<{ status: string }>(`${BASE}/alert-rules/${id}`, 'DELETE'),

  // Notification Channels
  getNotificationChannels: () => fetchJSON<NotificationChannel[]>(`${BASE}/notification-channels`),

  createNotificationChannel: (ch: Partial<NotificationChannel>) =>
    mutate<NotificationChannel>(`${BASE}/notification-channels`, 'POST', ch),

  updateNotificationChannel: (id: number, ch: Partial<NotificationChannel>) =>
    mutate<NotificationChannel>(`${BASE}/notification-channels/${id}`, 'PUT', ch),

  deleteNotificationChannel: (id: number) =>
    mutate<{ status: string }>(`${BASE}/notification-channels/${id}`, 'DELETE'),

  testNotificationChannel: (id: number) =>
    mutate<{ success: boolean; error?: string }>(`${BASE}/notification-channels/${id}/test`, 'POST'),

  // Notification Log
  getNotificationLog: (limit = 50, offset = 0) =>
    fetchJSON<NotificationLog[]>(`${BASE}/notifications/log?limit=${limit}&offset=${offset}`),
}

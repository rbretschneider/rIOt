import type { AlertRule, AlertTemplate, NotificationChannel, NotificationLog } from '../types/models'

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

  // Alert Templates
  getAlertTemplates: () => fetchJSON<AlertTemplate[]>(`${BASE}/alert-templates`),

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

  // Certificates (mTLS)
  getCerts: () => fetchJSON<unknown[]>(`${BASE}/certs`),

  revokeCert: (serial: string) =>
    mutate<{ status: string }>(`${BASE}/certs/${serial}/revoke`, 'POST'),

  // Bootstrap Keys
  getBootstrapKeys: () => fetchJSON<unknown[]>(`${BASE}/bootstrap-keys`),

  createBootstrapKey: (data: { label: string; expires_in_hours: number }) =>
    mutate<{ key: string; key_hash: string; label: string; expires_at: string }>(
      `${BASE}/bootstrap-keys`, 'POST', data
    ),

  deleteBootstrapKey: (hash: string) =>
    mutate<{ status: string }>(`${BASE}/bootstrap-keys/${hash}`, 'DELETE'),

  // Registration key
  getRegistrationKey: () =>
    fetchJSON<{ registration_key: string }>(`${BASE}/registration`),

  saveRegistrationKey: (key: string) =>
    mutate<{ status: string }>(`${BASE}/registration`, 'PUT', { registration_key: key }),

  // Password
  changePassword: (currentPassword: string, newPassword: string) =>
    mutate<{ status: string }>('/api/v1/auth/change-password', 'POST', {
      current_password: currentPassword,
      new_password: newPassword,
    }),

  // Server certificate
  getServerCert: () =>
    fetchJSON<{ fingerprint?: string }>('/api/v1/server-cert'),

  // Feature Toggles
  getFeatureToggles: () => fetchJSON<Record<string, boolean>>(`${BASE}/features`),

  saveFeatureToggles: (toggles: Record<string, boolean>) =>
    mutate<Record<string, boolean>>(`${BASE}/features`, 'PUT', toggles),

  // Server logs
  getLogs: (level: string, limit: number, before?: string) => {
    let url = `${BASE}/logs?limit=${limit}`
    if (level) url += `&level=${level}`
    if (before) url += `&before=${encodeURIComponent(before)}`
    return fetchJSON<{ id: number; timestamp: string; level: string; message: string; attrs?: Record<string, unknown>; source?: string }[]>(url)
  },
}

/**
 * Demo replacement for api/settings — returns mock data, mutations are delayed no-ops.
 * Vite aliases this module over settings.ts when VITE_DEMO=true.
 */
import * as demo from './demo-data'

const delay = (ms = 400) => new Promise(r => setTimeout(r, ms))

export const settingsApi = {
  // Alert Rules
  getAlertRules: () => Promise.resolve(demo.getAlertRules()),
  createAlertRule: async (rule: any) => { await delay(); return { id: 99, ...rule, created_at: new Date().toISOString(), updated_at: new Date().toISOString() } },
  updateAlertRule: async (_id: number, rule: any) => { await delay(); return { id: _id, ...rule, updated_at: new Date().toISOString() } },
  deleteAlertRule: async () => { await delay(); return { status: 'ok' } },

  // Alert Templates
  getAlertTemplates: () => Promise.resolve(demo.getAlertTemplates()),

  // Notification Channels
  getNotificationChannels: () => Promise.resolve(demo.getNotificationChannels()),
  createNotificationChannel: async (ch: any) => { await delay(); return { id: 99, ...ch, created_at: new Date().toISOString(), updated_at: new Date().toISOString() } },
  updateNotificationChannel: async (_id: number, ch: any) => { await delay(); return { id: _id, ...ch, updated_at: new Date().toISOString() } },
  deleteNotificationChannel: async () => { await delay(); return { status: 'ok' } },
  testNotificationChannel: async () => { await delay(); return { success: true } },

  // Notification Log
  getNotificationLog: () => Promise.resolve(demo.getNotificationLog()),

  // Certificates (mTLS)
  getCerts: () => Promise.resolve(demo.getCerts()),
  revokeCert: async () => { await delay(); return { status: 'ok' } },

  // Bootstrap Keys
  getBootstrapKeys: () => Promise.resolve(demo.getBootstrapKeys()),
  createBootstrapKey: async (data: any) => { await delay(); return { key: 'demo-bootstrap-key-xxx', key_hash: 'demo-hash-new', label: data.label, expires_at: new Date(Date.now() + data.expires_in_hours * 3600000).toISOString() } },
  deleteBootstrapKey: async () => { await delay(); return { status: 'ok' } },

  // Registration key
  getRegistrationKey: () => Promise.resolve({ registration_key: '' }),
  saveRegistrationKey: async () => { await delay(); return { status: 'ok' } },

  // Password
  changePassword: async () => { await delay(); return { status: 'ok' } },

  // Server certificate
  getServerCert: () => Promise.resolve(demo.getServerCert()),

  // Server logs
  getLogs: () => Promise.resolve(demo.getServerLogs()),
}

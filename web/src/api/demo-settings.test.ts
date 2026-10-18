import { describe, it, expect } from 'vitest'
import { settingsApi } from './demo-settings'
import { REDACTED_SECRET, SECRET_KEYS } from '../utils/secrets'

describe('[HARDEN-002][AC-040] demo settings client', () => {
  it('[HARDEN-002][AC-040] demo channels expose only sentinel or empty secrets', async () => {
    const channels = await settingsApi.getNotificationChannels()
    expect(channels.length).toBeGreaterThan(0)

    for (const ch of channels) {
      const keys = SECRET_KEYS[ch.type]
      if (!keys) {
        // Unknown type: every value must be the sentinel or empty.
        for (const [k, v] of Object.entries(ch.config)) {
          expect([REDACTED_SECRET, ''], `${ch.type}.${k}`).toContain(v)
        }
        continue
      }
      for (const key of keys) {
        const v = ch.config[key]
        if (v === undefined) continue
        expect([REDACTED_SECRET, ''], `${ch.type}.${key} must be the sentinel or empty, got ${String(v)}`).toContain(v)
      }
    }
  })

  it('[HARDEN-002][AC-040] the mock ntfy channel no longer carries a plaintext topic or a stray url key', async () => {
    const [ntfy] = (await settingsApi.getNotificationChannels()).filter(c => c.type === 'ntfy')
    expect(ntfy.config.topic).toBe(REDACTED_SECRET)
    expect(ntfy.config.server_url).toBe('https://ntfy.sh')
    expect(ntfy.config).not.toHaveProperty('url')
    expect(JSON.stringify(ntfy)).not.toContain('homelab-alerts')
  })

  it('[HARDEN-002][AC-040] create and update echo redacted config, like the real server', async () => {
    const created = await settingsApi.createNotificationChannel({
      name: 'tg',
      type: 'telegram',
      enabled: true,
      config: { bot_token: '123:ABC', chat_id: '42' },
    })
    expect(created.config.bot_token).toBe(REDACTED_SECRET)
    expect(created.config.chat_id).toBe('42')

    const updated = await settingsApi.updateNotificationChannel(1, {
      name: 'nt',
      type: 'ntfy',
      enabled: true,
      config: { server_url: 'https://alice:S3cret@ntfy.home', topic: 'secret-topic', token: '' },
    })
    expect(updated.config.topic).toBe(REDACTED_SECRET)
    expect(updated.config.token).toBe('')
    expect(updated.config.server_url).toBe('https://[rIOt-redacted]@ntfy.home')
    expect(JSON.stringify(updated)).not.toContain('S3cret')
    expect(JSON.stringify(updated)).not.toContain('secret-topic')
  })
})

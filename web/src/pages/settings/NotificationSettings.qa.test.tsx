// Added by QA Engineer (HARDEN-002). Probes the write-only secret contract in the
// editor: an untouched edit must never send "" for a stored secret, and a type
// change must never carry a sentinel.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import NotificationSettings from './NotificationSettings'
import { settingsApi } from '../../api/settings'
import { REDACTED_SECRET, SECRET_KEYS } from '../../utils/secrets'
import type { NotificationChannel } from '../../types/models'

vi.mock('../../api/settings', () => ({
  settingsApi: {
    getNotificationChannels: vi.fn(),
    createNotificationChannel: vi.fn(),
    updateNotificationChannel: vi.fn(),
    deleteNotificationChannel: vi.fn(),
    testNotificationChannel: vi.fn(),
  },
}))

const api = settingsApi as unknown as {
  getNotificationChannels: ReturnType<typeof vi.fn>
  updateNotificationChannel: ReturnType<typeof vi.fn>
}

function channel(type: string, config: Record<string, unknown>): NotificationChannel {
  return { id: 1, name: 'Chan', type, enabled: true, config, created_at: '2026-10-17T00:00:00Z', updated_at: '2026-10-17T00:00:00Z' }
}

function renderPage(ch: NotificationChannel) {
  api.getNotificationChannels.mockResolvedValue([ch])
  api.updateNotificationChannel.mockImplementation(async (_id: number, c: unknown) => c)
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <NotificationSettings />
    </QueryClientProvider>,
  )
}

async function renameAndSave() {
  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
  fireEvent.change(screen.getByDisplayValue('Chan'), { target: { value: 'Chan renamed' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(api.updateNotificationChannel).toHaveBeenCalled())
  const calls = api.updateNotificationChannel.mock.calls
  return calls[calls.length - 1][1] as { name: string; type: string; config: Record<string, unknown> }
}

beforeEach(() => {
  vi.clearAllMocks()
})

// Fully configured channel of every type, secrets as the server lists them.
const configured: Array<[string, Record<string, unknown>]> = [
  ['smtp', { host: 'mail.example', port: 587, username: 'u', password: REDACTED_SECRET, from: 'a@x', to: ['b@x'], starttls: true }],
  ['ntfy', { server_url: 'https://[rIOt-redacted]@ntfy.home', topic: REDACTED_SECRET, token: REDACTED_SECRET, priority: 'default' }],
  ['telegram', { bot_token: REDACTED_SECRET, chat_id: '42' }],
  ['discord', { webhook_url: REDACTED_SECRET }],
  ['slack', { webhook_url: REDACTED_SECRET }],
  ['pushover', { api_token: REDACTED_SECRET, user_key: REDACTED_SECRET }],
  ['gotify', { server_url: 'https://g.example', app_token: REDACTED_SECRET }],
  ['webhook', { url: REDACTED_SECRET, headers: { Authorization: REDACTED_SECRET } }],
]

describe('[HARDEN-002][AC-036] untouched secrets are never sent as an empty string', () => {
  it.each(configured)('[HARDEN-002][AC-036] name-only edit of a %s channel keeps every secret as the sentinel', async (type, config) => {
    renderPage(channel(type, config))

    const body = await renameAndSave()

    expect(body.name).toBe('Chan renamed')
    for (const key of SECRET_KEYS[type]) {
      expect(body.config[key], `${type}.${key} must be the sentinel or omitted, never ""`).toSatisfy(
        (v: unknown) => v === REDACTED_SECRET || v === undefined,
      )
      expect(body.config[key]).not.toBe('')
    }
    // No string anywhere in the submitted config may be an empty replacement for a stored secret.
    for (const [k, v] of Object.entries(body.config)) {
      if (SECRET_KEYS[type].includes(k)) expect(v, `${type}.${k}`).not.toBe('')
    }
  })

  it('[HARDEN-002][AC-036] a webhook header map round-trips with its sentinel values and names intact', async () => {
    renderPage(channel('webhook', { url: REDACTED_SECRET, headers: { Authorization: REDACTED_SECRET, 'X-Env': REDACTED_SECRET } }))

    const body = await renameAndSave()

    expect(body.config.headers).toEqual({ Authorization: REDACTED_SECRET, 'X-Env': REDACTED_SECRET })
  })

  it('[HARDEN-002][AC-036] an unset optional secret is not turned into the sentinel', async () => {
    renderPage(channel('ntfy', { server_url: 'https://ntfy.sh', topic: REDACTED_SECRET, token: '' }))

    const body = await renameAndSave()

    expect(body.config.token).toBe('')
    expect(body.config.topic).toBe(REDACTED_SECRET)
  })
})

describe('[HARDEN-002][AC-037] a type change carries no secret state', () => {
  it('[HARDEN-002][AC-037] switching smtp to telegram submits a config with no sentinel and no stored password', async () => {
    renderPage(channel('smtp', { host: 'mail.example', password: REDACTED_SECRET, username: 'u' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))

    fireEvent.change(screen.getByDisplayValue('Email (SMTP)'), { target: { value: 'telegram' } })
    expect(screen.queryByText('Configured (unchanged)')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.updateNotificationChannel).toHaveBeenCalled())

    const body = api.updateNotificationChannel.mock.calls[0][1] as { type: string; config: Record<string, unknown> }
    expect(body.type).toBe('telegram')
    expect(JSON.stringify(body.config)).not.toContain(REDACTED_SECRET)
    expect(body.config).not.toHaveProperty('password')
  })
})

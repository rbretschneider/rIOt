import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import NotificationSettings from './NotificationSettings'
import { renderWithProviders } from '../../test/renderWithProviders'
import { settingsApi } from '../../api/settings'
import { REDACTED_SECRET } from '../../utils/secrets'
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
  createNotificationChannel: ReturnType<typeof vi.fn>
  updateNotificationChannel: ReturnType<typeof vi.fn>
  testNotificationChannel: ReturnType<typeof vi.fn>
}

function channel(id: number, name: string, type: string, config: Record<string, unknown>): NotificationChannel {
  return { id, name, type, enabled: true, config, created_at: '2026-10-17T00:00:00Z', updated_at: '2026-10-17T00:00:00Z' }
}

function renderPage(channels: NotificationChannel[]) {
  api.getNotificationChannels.mockResolvedValue(channels)
  api.updateNotificationChannel.mockImplementation(async (_id: number, ch: unknown) => ch)
  api.createNotificationChannel.mockImplementation(async (ch: unknown) => ch)
  api.testNotificationChannel.mockResolvedValue({ success: true })
  return renderWithProviders(<NotificationSettings />)
}

async function openEditor() {
  fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
}

function lastUpdate() {
  const calls = api.updateNotificationChannel.mock.calls
  expect(calls.length, 'updateNotificationChannel must have been called').toBeGreaterThan(0)
  const [id, body] = calls[calls.length - 1] as [number, { config: Record<string, unknown>; name: string }]
  return { id, body }
}

async function save() {
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(api.updateNotificationChannel).toHaveBeenCalled())
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('[HARDEN-002][AC-036] editor round-trip preserves secrets', () => {
  it('[HARDEN-002][AC-036] saving after a name-only edit submits password as the sentinel, never ""', async () => {
    renderPage([channel(1, 'Mail', 'smtp', { host: 'mail.example', port: 587, username: 'alerts', password: REDACTED_SECRET, starttls: true })])
    await openEditor()

    fireEvent.change(screen.getByDisplayValue('Mail'), { target: { value: 'Mail renamed' } })
    await save()

    const { id, body } = lastUpdate()
    expect(id).toBe(1)
    expect(body.name).toBe('Mail renamed')
    expect(body.config.password, 'password must be the sentinel').toBe(REDACTED_SECRET)
    expect(body.config.password, 'password must never be an empty string').not.toBe('')
    expect(body.config.host).toBe('mail.example')
  })

  it('[HARDEN-002][AC-036] erasing text typed over a configured secret reverts to the sentinel', async () => {
    renderPage([channel(1, 'TG', 'telegram', { bot_token: REDACTED_SECRET, chat_id: '42' })])
    await openEditor()

    const input = screen.getByLabelText('Bot Token')
    fireEvent.change(input, { target: { value: 'typing' } })
    expect(screen.queryByText('Configured (unchanged)')).toBeNull()
    fireEvent.change(input, { target: { value: '' } })

    expect(screen.getByText('Configured (unchanged)')).toBeTruthy()
    await save()
    expect(lastUpdate().body.config.bot_token).toBe(REDACTED_SECRET)
  })

  it('[HARDEN-002][AC-036][C1] a userinfo server_url round-trips exactly as the server listed it', async () => {
    renderPage([channel(1, 'Notify', 'ntfy', { server_url: 'https://[rIOt-redacted]@ntfy.home', topic: REDACTED_SECRET, token: '' })])
    await openEditor()
    expect((screen.getByPlaceholderText('https://ntfy.sh') as HTMLInputElement).value).toBe('https://[rIOt-redacted]@ntfy.home')

    await save()
    const { body } = lastUpdate()
    expect(body.config.server_url).toBe('https://[rIOt-redacted]@ntfy.home')
    expect(body.config.topic).toBe(REDACTED_SECRET)
    expect(body.config.token, 'an unset optional secret stays empty').toBe('')
  })
})

describe('[HARDEN-002][AC-037] editor shows "configured" and sends new values', () => {
  it('[HARDEN-002][AC-037] redacted bot_token shows "Configured (unchanged)", is not editable text, and typing NEWTOKEN submits it', async () => {
    renderPage([channel(1, 'TG', 'telegram', { bot_token: REDACTED_SECRET, chat_id: '42' })])
    await openEditor()

    const input = screen.getByLabelText('Bot Token') as HTMLInputElement
    expect(input.value, 'the sentinel must not be editable text').toBe('')
    expect(screen.queryByDisplayValue(REDACTED_SECRET)).toBeNull()
    expect(screen.getByText('Configured (unchanged)')).toBeTruthy()
    expect(input.placeholder).toBe('Leave blank to keep the current value')

    fireEvent.change(input, { target: { value: 'NEWTOKEN' } })
    expect(screen.queryByText('Configured (unchanged)')).toBeNull()
    await save()

    expect(lastUpdate().body.config.bot_token).toBe('NEWTOKEN')
  })

  it('[HARDEN-002][AC-037] a new channel shows no configured state and submits what is typed', async () => {
    renderPage([])
    fireEvent.click(await screen.findByRole('button', { name: 'Add Channel' }))
    fireEvent.change(screen.getByDisplayValue('ntfy'), { target: { value: 'telegram' } })

    expect(screen.queryByText('Configured (unchanged)')).toBeNull()
    fireEvent.change(screen.getByLabelText('Bot Token'), { target: { value: '123:ABC' } })
    fireEvent.change(screen.getAllByRole('textbox')[0], { target: { value: 'My bot' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(api.createNotificationChannel).toHaveBeenCalled())
    const [body] = api.createNotificationChannel.mock.calls[0] as [{ config: Record<string, unknown> }]
    expect(body.config.bot_token).toBe('123:ABC')
  })

  it('[HARDEN-002][AC-037] changing the channel type drops the configured state', async () => {
    renderPage([channel(1, 'Hook', 'discord', { webhook_url: REDACTED_SECRET })])
    await openEditor()
    expect(screen.getByText('Configured (unchanged)')).toBeTruthy()

    fireEvent.change(screen.getByDisplayValue('Discord'), { target: { value: 'slack' } })

    expect(screen.queryByText('Configured (unchanged)'), 'a stored secret must not appear to carry over to another type').toBeNull()
  })

  it('[HARDEN-002][AC-037] the webhook URL, discord/slack URL and pushover keys also use the configured state', async () => {
    renderPage([channel(1, 'Push', 'pushover', { api_token: REDACTED_SECRET, user_key: REDACTED_SECRET })])
    await openEditor()
    expect(screen.getAllByText('Configured (unchanged)')).toHaveLength(2)
  })
})

describe('[HARDEN-002][AC-037][C3] browser autofill opt-out', () => {
  const editors: Array<[string, string, Record<string, unknown>]> = [
    ['smtp', 'Email (SMTP)', { host: 'h', password: REDACTED_SECRET, username: 'u' }],
    ['ntfy', 'ntfy', { server_url: 'https://ntfy.sh', topic: REDACTED_SECRET, token: REDACTED_SECRET }],
    ['telegram', 'Telegram', { bot_token: REDACTED_SECRET, chat_id: '1' }],
    ['pushover', 'Pushover', { api_token: REDACTED_SECRET, user_key: REDACTED_SECRET }],
    ['gotify', 'Gotify', { server_url: 'https://g.example', app_token: REDACTED_SECRET }],
  ]

  it.each(editors)('[HARDEN-002][AC-037][C3] secret inputs opt out of browser credential autofill (%s)', async (type, _label, config) => {
    renderPage([channel(1, 'Chan', type, config)])
    await openEditor()

    const modal = document.querySelector('.fixed.inset-0') as HTMLElement
    expect(modal, 'the editor modal must be open').toBeTruthy()
    const inputs = Array.from(modal.querySelectorAll('input'))

    const passwords = inputs.filter(i => i.type === 'password')
    expect(passwords.length, `${type} editor should have password inputs`).toBeGreaterThan(0)
    for (const p of passwords) {
      expect(p.getAttribute('autocomplete'), `${p.name} autocomplete`).toBe('new-password')
      expect(p.hasAttribute('data-1p-ignore'), `${p.name} data-1p-ignore`).toBe(true)
      expect(p.getAttribute('data-lpignore'), `${p.name} data-lpignore`).toBe('true')
      expect(p.getAttribute('data-bwignore'), `${p.name} data-bwignore`).toBe('true')
      expect(p.getAttribute('data-form-type'), `${p.name} data-form-type`).toBe('other')
      expect(p.name.startsWith('riot-secret-'), `${p.name} must be a non-credential-looking name`).toBe(true)
      expect(p.id).toBe(p.name)
    }

    const secretTexts = inputs.filter(i => i.type === 'text' && i.name.startsWith('riot-secret-'))
    for (const t of secretTexts) {
      expect(t.getAttribute('autocomplete'), `${t.name} autocomplete`).toBe('off')
      expect(t.getAttribute('data-lpignore'), `${t.name} data-lpignore`).toBe('true')
    }

    for (const i of inputs) {
      expect(i.closest('form'), `input ${i.name || i.type} must not be inside a <form>`).toBeNull()
    }
  })

  it('[HARDEN-002][AC-037][C3] the SMTP username and ntfy/gotify server_url break the text-then-password login heuristic', async () => {
    renderPage([channel(1, 'Mail', 'smtp', { host: 'h', username: 'alerts', password: REDACTED_SECRET })])
    await openEditor()
    const username = screen.getByDisplayValue('alerts')
    expect(username.getAttribute('autocomplete')).toBe('off')
    expect(username.getAttribute('data-lpignore')).toBe('true')
  })

  it.each([
    ['ntfy', { server_url: 'https://ntfy.sh', topic: REDACTED_SECRET }, 'https://ntfy.sh'],
    ['gotify', { server_url: 'https://g.example', app_token: REDACTED_SECRET }, 'https://gotify.example.com'],
  ])('[HARDEN-002][AC-037][C3] %s server_url opts out of autofill', async (type, config, placeholder) => {
    renderPage([channel(1, 'Chan', type, config)])
    await openEditor()
    const url = screen.getByPlaceholderText(placeholder)
    expect(url.getAttribute('autocomplete')).toBe('off')
    expect(url.getAttribute('data-lpignore')).toBe('true')
  })
})

describe('[HARDEN-002][AC-038] clearing an optional secret', () => {
  it('[HARDEN-002][AC-038] Clear on ntfy token submits token ""', async () => {
    renderPage([channel(1, 'Notify', 'ntfy', { server_url: 'https://ntfy.sh', topic: REDACTED_SECRET, token: REDACTED_SECRET })])
    await openEditor()

    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    expect(screen.getByText(/Will be cleared on save/)).toBeTruthy()
    await save()

    const { body } = lastUpdate()
    expect(body.config.token).toBe('')
    expect(body.config.topic, 'the required secret is untouched').toBe(REDACTED_SECRET)
  })

  it('[HARDEN-002][AC-038] Undo restores the sentinel', async () => {
    renderPage([channel(1, 'Notify', 'ntfy', { server_url: 'https://ntfy.sh', topic: REDACTED_SECRET, token: REDACTED_SECRET })])
    await openEditor()

    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await save()

    expect(lastUpdate().body.config.token).toBe(REDACTED_SECRET)
  })

  it('[HARDEN-002][AC-038] Clear is offered only for the ntfy token and the SMTP password', async () => {
    renderPage([channel(1, 'TG', 'telegram', { bot_token: REDACTED_SECRET, chat_id: '1' })])
    await openEditor()
    expect(screen.queryByRole('button', { name: 'Clear' })).toBeNull()
  })

  it('[HARDEN-002][AC-038] Clear on the SMTP password submits password ""', async () => {
    renderPage([channel(1, 'Mail', 'smtp', { host: 'h', password: REDACTED_SECRET })])
    await openEditor()
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    await save()
    expect(lastUpdate().body.config.password).toBe('')
  })
})

describe('[HARDEN-002][AC-039] test button needs no secret re-entry', () => {
  it('[HARDEN-002][AC-039] Test calls testNotificationChannel with the id only', async () => {
    renderPage([channel(7, 'Hook', 'slack', { webhook_url: REDACTED_SECRET })])

    fireEvent.click(await screen.findByRole('button', { name: 'Test' }))

    await waitFor(() => expect(api.testNotificationChannel).toHaveBeenCalled())
    expect(api.testNotificationChannel).toHaveBeenCalledWith(7)
    expect(document.querySelector('input'), 'no secret input may be required to test').toBeNull()
    await screen.findByText('Test sent!')
  })
})

describe('[HARDEN-002][AC-037] list rows never render a secret or the sentinel', () => {
  it('[HARDEN-002][AC-037] summaries say "configured" instead of showing secret values', async () => {
    renderPage([
      channel(1, 'N', 'ntfy', { server_url: 'https://ntfy.sh', topic: REDACTED_SECRET }),
      channel(2, 'N2', 'ntfy', { server_url: 'https://ntfy.sh', topic: '' }),
      channel(3, 'W', 'webhook', { url: REDACTED_SECRET }),
      channel(4, 'W2', 'webhook', { url: '' }),
      channel(5, 'D', 'discord', { webhook_url: REDACTED_SECRET }),
      channel(6, 'S', 'slack', { webhook_url: '' }),
      channel(7, 'P', 'pushover', { api_token: REDACTED_SECRET, user_key: REDACTED_SECRET }),
      channel(8, 'P2', 'pushover', { api_token: '', user_key: '' }),
      channel(9, 'G', 'gotify', { server_url: 'https://[rIOt-redacted]@gotify.home', app_token: REDACTED_SECRET }),
    ])

    await screen.findByText('N')
    const text = document.body.textContent ?? ''
    expect(text).toContain('https://ntfy.sh/(topic hidden)')
    expect(text).toContain('https://ntfy.sh/(no topic)')
    expect(text).toContain('URL configured')
    expect(text).toContain('(no URL)')
    expect(text).toContain('Webhook URL configured')
    expect(text).toContain('(no webhook URL)')
    expect(text).toContain('User key configured')
    expect(text).toContain('(not set)')

    // The only place the sentinel may appear is the already-redacted gotify server URL.
    const withoutGotify = text.replace('https://[rIOt-redacted]@gotify.home', '')
    expect(withoutGotify, 'the sentinel must not be rendered in any row').not.toContain(REDACTED_SECRET)
  })
})

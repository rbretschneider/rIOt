import { describe, it, expect } from 'vitest'
import { REDACTED_SECRET, SECRET_KEYS, isRedacted, isSecretSet, redactChannelConfig } from './secrets'

describe('[HARDEN-002] secrets helpers', () => {
  it('pins the sentinel literal shared with the Go server', () => {
    expect(REDACTED_SECRET).toBe('[rIOt-redacted]')
  })

  it('isRedacted is true only for the exact sentinel', () => {
    expect(isRedacted(REDACTED_SECRET)).toBe(true)
    expect(isRedacted('')).toBe(false)
    expect(isRedacted(undefined)).toBe(false)
    expect(isRedacted('[rIOt-redacted] ')).toBe(false)
  })

  it('isSecretSet is false for undefined, null and empty string only', () => {
    expect(isSecretSet(undefined)).toBe(false)
    expect(isSecretSet(null)).toBe(false)
    expect(isSecretSet('')).toBe(false)
    expect(isSecretSet(REDACTED_SECRET)).toBe(true)
    expect(isSecretSet('x')).toBe(true)
    expect(isSecretSet(0)).toBe(true)
  })

  it('mirrors the Go registry (T-1)', () => {
    expect(SECRET_KEYS).toEqual({
      smtp: ['password'],
      ntfy: ['token', 'topic'],
      telegram: ['bot_token'],
      discord: ['webhook_url'],
      slack: ['webhook_url'],
      pushover: ['api_token', 'user_key'],
      gotify: ['app_token'],
      webhook: ['url'],
    })
  })
})

describe('[HARDEN-002][AC-040] redactChannelConfig', () => {
  it('replaces set secrets with the sentinel and keeps non-secret keys', () => {
    expect(redactChannelConfig('smtp', { host: 'mail', password: 'p', username: 'u' })).toEqual({
      host: 'mail',
      password: REDACTED_SECRET,
      username: 'u',
    })
  })

  it('returns "" for an unset secret, never the sentinel', () => {
    expect(redactChannelConfig('ntfy', { token: '', topic: 't' })).toEqual({ token: '', topic: REDACTED_SECRET })
  })

  it('does not invent keys that are absent', () => {
    expect(redactChannelConfig('ntfy', { server_url: 'https://ntfy.sh' })).toEqual({ server_url: 'https://ntfy.sh' })
  })

  it('redacts every key of an unknown type', () => {
    expect(redactChannelConfig('futurething', { a: 'x1', b: '', c: 5 })).toEqual({
      a: REDACTED_SECRET,
      b: '',
      c: REDACTED_SECRET,
    })
  })

  it('redacts webhook header values and keeps their names', () => {
    expect(
      redactChannelConfig('webhook', { url: 'https://h/x', headers: { Authorization: 'Bearer a', 'X-Empty': '' } }),
    ).toEqual({ url: REDACTED_SECRET, headers: { Authorization: REDACTED_SECRET, 'X-Empty': '' } })
  })

  it('replaces a non-map webhook headers value wholesale', () => {
    expect(redactChannelConfig('webhook', { url: 'https://h/x', headers: 'Authorization: Bearer LEAK' }).headers).toBe(
      REDACTED_SECRET,
    )
  })

  it('does not mutate its input', () => {
    const input = { password: 'p' }
    redactChannelConfig('smtp', input)
    expect(input.password).toBe('p')
  })
})

describe('[HARDEN-002][AC-019][C1] URL userinfo rewrite', () => {
  const cases: Array<[string, string]> = [
    ['https://alice:S3cret@ntfy.home', 'https://[rIOt-redacted]@ntfy.home'],
    ['http://bob:Pw2@gotify.home:8080/p?q=1#f', 'http://[rIOt-redacted]@gotify.home:8080/p?q=1#f'],
    ['https://tokenuser@ntfy.home', 'https://[rIOt-redacted]@ntfy.home'],
    ['https://ntfy.home/x', 'https://ntfy.home/x'],
    ['https://ntfy.home/u@x', 'https://ntfy.home/u@x'],
    ['alerts@example.com', 'alerts@example.com'],
    ['', ''],
    ['https://a:p@ss@h.example/x', 'https://[rIOt-redacted]@h.example/x'],
  ]
  it.each(cases)('rewrites %s', (input, expected) => {
    expect(redactChannelConfig('ntfy', { server_url: input }).server_url).toBe(expected)
    expect(redactChannelConfig('gotify', { server_url: input }).server_url).toBe(expected)
  })

  it('leaves non-URL strings with @ alone in non-secret keys', () => {
    expect(redactChannelConfig('smtp', { from: 'a@x.example' }).from).toBe('a@x.example')
  })
})

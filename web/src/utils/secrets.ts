/**
 * Write-only notification secrets (HARDEN-002 AD-009).
 *
 * The server never returns a stored secret: it sends REDACTED_SECRET in its
 * place (or '' when unset). A PUT that sends the sentinel back keeps the
 * stored value. This module is a UX-only mirror of the Go registry in
 * internal/server/notify/secrets.go; the Go side is authoritative, and a Go
 * test pins REDACTED_SECRET to the same literal.
 */

export const REDACTED_SECRET = '[rIOt-redacted]'

/** Dedicated secret config keys per channel type. Webhook header values are secret too. */
export const SECRET_KEYS: Record<string, string[]> = {
  smtp: ['password'],
  ntfy: ['token', 'topic'],
  telegram: ['bot_token'],
  discord: ['webhook_url'],
  slack: ['webhook_url'],
  pushover: ['api_token', 'user_key'],
  gotify: ['app_token'],
  webhook: ['url'],
}

/** True when v is the redaction sentinel. */
export function isRedacted(v: unknown): boolean {
  return v === REDACTED_SECRET
}

/** True when a secret has a value: anything other than undefined, null or ''. */
export function isSecretSet(v: unknown): boolean {
  return v !== undefined && v !== null && v !== ''
}

interface UserinfoSplit {
  prefix: string
  userinfo: string
  rest: string
}

/**
 * Splits scheme://[userinfo@]host... without parsing it as a URL, because the
 * redacted form https://[rIOt-redacted]@host is not a parseable URL. Mirrors
 * splitURLUserinfo in Go. Returns null when v does not start with a valid
 * scheme followed by "://".
 */
function splitUrlUserinfo(v: string): UserinfoSplit | null {
  const idx = v.indexOf('://')
  if (idx < 1) return null
  const scheme = v.slice(0, idx)
  if (!/^[A-Za-z][A-Za-z0-9+.-]*$/.test(scheme)) return null
  const prefix = v.slice(0, idx + 3)
  const afterScheme = v.slice(idx + 3)
  const authEnd = afterScheme.search(/[/?#]/)
  const authority = authEnd < 0 ? afterScheme : afterScheme.slice(0, authEnd)
  const at = authority.lastIndexOf('@')
  if (at < 0) return { prefix, userinfo: '', rest: afterScheme }
  return { prefix, userinfo: afterScheme.slice(0, at), rest: afterScheme.slice(at + 1) }
}

function redactUrlUserinfo(v: string): string {
  const parts = splitUrlUserinfo(v)
  if (!parts || parts.userinfo === '') return v
  return `${parts.prefix}${REDACTED_SECRET}@${parts.rest}`
}

function sentinelOrEmpty(v: unknown): string {
  return isSecretSet(v) ? REDACTED_SECRET : ''
}

/**
 * Mirrors RedactChannel in Go for the demo client, which has no server:
 * secret keys (every key for an unknown type) become the sentinel when set and
 * '' when unset; webhook header values are redacted with names kept; and
 * credentials in the userinfo of any other URL value are redacted.
 */
export function redactChannelConfig(type: string, config: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = { ...config }
  const handled = new Set<string>()
  const keys = SECRET_KEYS[type]

  if (!keys) {
    for (const k of Object.keys(out)) {
      out[k] = sentinelOrEmpty(out[k])
      handled.add(k)
    }
  } else {
    for (const k of keys) {
      if (k in out) out[k] = sentinelOrEmpty(out[k])
      handled.add(k)
    }
    if (type === 'webhook') {
      handled.add('headers')
      const headers = out.headers
      if (headers && typeof headers === 'object' && !Array.isArray(headers)) {
        out.headers = Object.fromEntries(
          Object.keys(headers as Record<string, unknown>).map(name => [name, sentinelOrEmpty((headers as Record<string, unknown>)[name])]),
        )
      } else if (headers !== undefined && headers !== null) {
        out.headers = REDACTED_SECRET
      }
    }
  }

  for (const k of Object.keys(out)) {
    if (handled.has(k)) continue
    const v = out[k]
    if (typeof v === 'string') out[k] = redactUrlUserinfo(v)
  }
  return out
}

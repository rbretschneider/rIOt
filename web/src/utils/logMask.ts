const sensitiveParts = [
  'PASSWORD',
  'PASSWD',
  'PASS',
  'PWD',
  'SECRET',
  'TOKEN',
  'APIKEY',
  'ACCESSKEY',
  'PRIVATE',
  'CREDENTIAL',
  'AUTH',
  'COOKIE',
  'SESSION',
  'DSN',
  'CONNECTIONSTRING',
  'SALT',
  'BEARER',
  'JWT',
  'WEBHOOK',
  'SIGNING',
  'SIGNATURE',
  'ENCRYPT',
  'HMAC',
  'PRIVKEY',
  'PASSPHRASE',
  'PSK',
  'LICENSE',
  'CONNSTR',
]
export function sensitiveDockerKey(key: string) {
  const parts = key
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .replace(/([A-Z])([A-Z][a-z])/g, '$1_$2')
    .toUpperCase()
    .split(/[_./\s-]+/)
  const joined = parts.join('')
  return (
    sensitiveParts.some((p) => joined.includes(p)) ||
    parts.some(
      (p) => p.endsWith('KEY') || ['PW', 'OTP', 'SAS', 'SIG'].includes(p),
    )
  )
}
const safeEnv = new Set([
  'TZ',
  'PUID',
  'PGID',
  'UMASK',
  'UID',
  'GID',
  'LANG',
  'LANGUAGE',
  'LC_ALL',
  'PATH',
  'HOME',
  'HOSTNAME',
  'TERM',
  'NODE_ENV',
  'PYTHONUNBUFFERED',
])
export function maskEnvValue(key: string, value: string) {
  return safeEnv.has(key) ? maskText(value, '<redacted>') : '<redacted>'
}
/** Browser mirror of internal/redact. Mask before identity comparisons and rendering. */
export function maskText(input: string, marker: string) {
  if (
    !/[=:]/.test(input) &&
    !input.includes('eyJ') &&
    !input.includes('-----BEGIN')
  )
    return input
  let text = input.replace(
    /\b[a-zA-Z][a-zA-Z0-9+.-]*:\/\/(?:(?:<(?:masked|redacted)>)|[^\s"'<>])+/g,
    (token) => {
      const start = token.indexOf('://') + 3
      const at = token.lastIndexOf('@')
      if (at >= start) {
        const colon = token.indexOf(':', start)
        if (colon >= start && colon < at)
          token = token.slice(0, colon + 1) + marker + token.slice(at)
      }
      const q = token.indexOf('?')
      if (q >= 0) {
        const hash = token.indexOf('#', q)
        const end = hash < 0 ? token.length : hash
        const parts = token
          .slice(q + 1, end)
          .split('&')
          .map((p) => {
            const eq = p.indexOf('=')
            if (eq < 0) return p
            try {
              return sensitiveDockerKey(
                decodeURIComponent(p.slice(0, eq).replace(/\+/g, ' ')),
              )
                ? p.slice(0, eq + 1) + marker
                : p
            } catch {
              return p
            }
          })
        token = token.slice(0, q + 1) + parts.join('&') + token.slice(end)
      }
      return token
    },
  )
  const auth = /(authorization\s*[:=]\s*)(?:(?:bearer|basic)\s+)?[^\r\n,;]+/gi
  text = text.replace(auth, (match, prefix: string) =>
    match.includes(marker) ? match : prefix + marker,
  )
  text = text.replace(
    /(["']?)(\b[a-zA-Z_][a-zA-Z0-9_./-]*)(["']?\s*[:=]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^\s,;}&]+)/g,
    (match, opening: string, key: string, separator: string, value: string) => {
      if (!sensitiveDockerKey(key)) return match
      const quote = /^["']/.test(value) ? value[0] : ''
      return opening + key + separator + quote + marker + quote
    },
  )
  return text
    .replace(auth, (match, prefix: string) =>
      match.includes(marker) ? match : prefix + marker,
    )
    .replace(/\beyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\b/g, marker)
    .replace(
      /-----BEGIN [A-Z0-9 ]+-----[\s\S]*?(?:-----END [A-Z0-9 ]+-----|$)/g,
      marker,
    )
}
export const maskLogLine = (input: string) => maskText(input, '<masked>')
export function maskDockerValue(key: string, value: string) {
  return sensitiveDockerKey(key) ? '<redacted>' : maskText(value, '<redacted>')
}

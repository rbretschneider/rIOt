export function safeUrl(value: string | undefined): string | undefined {
  if (!value) return
  try {
    const url = new URL(value)
    if (
      !['http:', 'https:'].includes(url.protocol) ||
      url.username ||
      url.password
    )
      return
    return url.href
  } catch {
    return
  }
}
export function parseIP(value: string): string | undefined {
  if (/^(?:\d{1,3}\.){3}\d{1,3}$/.test(value))
    return value
      .split('.')
      .every((n) => Number(n) <= 255 && String(Number(n)) === n)
      ? value
      : undefined
  if (!value.includes(':') || /[^0-9a-f:.]/i.test(value)) return
  try {
    const host = new URL(`http://[${value}]/`).hostname
    return host.slice(1, -1)
  } catch {
    return
  }
}
export function portUrl(
  ip: string | undefined,
  port: string | undefined,
  scheme = 'http',
): string | undefined {
  if (
    !ip ||
    !port ||
    !/^\d+$/.test(port) ||
    !['http', 'https'].includes(scheme)
  )
    return
  const parsed = parseIP(ip)
  const n = Number(port)
  if (!parsed || n < 1 || n > 65535) return
  if (
    parsed.startsWith('127.') ||
    parsed === '::1' ||
    parsed.startsWith('::ffff:7f')
  )
    return
  return `${scheme}://${parsed.includes(':') ? `[${parsed}]` : parsed}:${n}/`
}
export function publishedPortUrl(
  binding: string | undefined,
  primary: string | undefined,
  hostPort: string | undefined,
  containerPort: string,
  protocol: string,
) {
  if (protocol.toLowerCase() !== 'tcp') return
  const address =
    binding && !['0.0.0.0', '::', ''].includes(binding) ? binding : primary
  return portUrl(
    address,
    hostPort,
    ['443', '8443'].includes(containerPort) ? 'https' : 'http',
  )
}

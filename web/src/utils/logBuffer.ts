import { maskLogLine } from './logMask'
import type { LogLine } from '../types/docker'
export const stripAnsi = (text: string) =>
  text
    .replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, '')
    .replace(/\u001b\][^\u0007]*(?:\u0007|\u001b\\)/g, '')
export function timestampMicros(value: string) {
  const ms = Date.parse(value)
  if (!Number.isFinite(ms)) return 0
  const fraction = value.match(/\.(\d+)/)?.[1] ?? ''
  return ms * 1000 + Number(fraction.padEnd(6, '0').slice(3, 6))
}
export function logIdentity(line: LogLine) {
  return [
    line.container ?? '',
    timestampMicros(line.agent_t ?? line.t),
    line.s,
    line.l,
    line.gap?.reason,
    line.gap?.count,
  ].join('|')
}
export function mergeLogLines(existing: LogLine[], incoming: LogLine[]) {
  const lines = new Map(existing.map((l) => [logIdentity(l), l]))
  for (const line of incoming) {
    const safe = {
      ...line,
      l:
        line.l === undefined
          ? undefined
          : maskLogLine(stripAnsi(line.l.slice(0, 16384))),
    }
    lines.set(logIdentity(safe), safe)
  }
  const sorted = [...lines.values()].sort(
    (a, b) =>
      timestampMicros(a.t) - timestampMicros(b.t) || a.t.localeCompare(b.t),
  )
  let bytes = 0
  let start = sorted.length
  while (start > 0 && sorted.length - start < 10000) {
    const size = 2 * (sorted[start - 1].l?.length ?? 100)
    if (bytes + size > 32 * 1024 * 1024) break
    bytes += size
    start--
  }
  return { lines: sorted.slice(start), dropped: start > 0 }
}
export function downloadLogs(lines: LogLine[]) {
  return (
    '# rIOt Docker logs — times are agent asserted; known secrets masked\n' +
    lines
      .map(
        (l) =>
          `${l.t}${l.agent_t ? ` [clock skew; agent time ${l.agent_t}]` : ''} ${l.container ? `[${l.container}] ` : ''}${l.gap ? `[gap: ${l.gap.lower_bound ? 'at least ' : ''}${l.gap.count} lines; ${l.gap.reason}]` : `[${l.s === 'e' ? 'stderr' : 'stdout'}] ${stripAnsi(l.l ?? '')}`}`,
      )
      .join('\n')
  )
}
export function logBoundary(
  lines: LogLine[],
  serverNow: number,
  elapsedMs: number,
) {
  return lines
    .filter((l) => Date.parse(l.t) <= serverNow + elapsedMs + 300000)
    .reduce(
      (newest, l) =>
        timestampMicros(l.t) > timestampMicros(newest) ? l.t : newest,
      '',
    )
}

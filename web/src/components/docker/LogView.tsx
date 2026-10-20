import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { Button, ErrorPanel, Modal, QueryState } from '../ui'
import {
  downloadLogs,
  mergeLogLines,
  stripAnsi,
  timestampMicros,
} from '../../utils/logBuffer'
import type { LogLine } from '../../types/docker'
import { useTimeFormatters } from '../../contexts/TimePreferenceProvider'
function Highlight({ text, filter }: { text: string; filter: string }) {
  if (!filter) return <>{text}</>
  const nodes = []
  const source = text.toLowerCase()
  const query = filter.toLowerCase()
  let offset = 0
  let match = source.indexOf(query)
  while (match >= 0) {
    nodes.push(
      text.slice(offset, match),
      <mark key={match}>{text.slice(match, match + query.length)}</mark>,
    )
    offset = match + query.length
    match = source.indexOf(query, offset)
  }
  nodes.push(text.slice(offset))
  return <>{nodes}</>
}
export default function LogView({
  deviceId,
  name,
  liveSupported,
  hostname,
  stack = false,
  services = [],
}: {
  deviceId: string
  name: string
  liveSupported: boolean
  hostname?: string
  stack?: boolean
  services?: string[]
}) {
  const [params, setParams] = useSearchParams()
  const { timestamp } = useTimeFormatters()
  const viewer = useRef<HTMLDivElement>(null)
  const range = ['15m', '1h', '6h', '24h', '7d'].includes(
    params.get('range') ?? '',
  )
    ? params.get('range')!
    : '1h'
  const [lines, setLines] = useState<LogLine[]>([])
  const [dropped, setDropped] = useState(false)
  const [live, setLive] = useState(false)
  const [paused, setPaused] = useState<LogLine[] | null>(null)
  const [stream, setStream] = useState('all')
  const [filter, setFilter] = useState('')
  const [wrap, setWrap] = useState(true)
  const [timestamps, setTimestamps] = useState(true)
  const [status, setStatus] = useState('History')
  const [olderError, setOlderError] = useState<unknown>()
  const [cursor, setCursor] = useState<string>()
  const [hasOlder, setHasOlder] = useState(false)
  const [chosen, setChosen] = useState<string[]>(services.slice(0, 5))
  const boundaries = useRef(new Map<string, string>())
  const newest = useRef('')
  const clock = useRef({ server: 0, monotonic: 0 })
  const retry = useRef(0)
  const [reconnect, setReconnect] = useState(0)
  const [customOpen, setCustomOpen] = useState(false)
  const [customFrom, setCustomFrom] = useState('')
  const [customTo, setCustomTo] = useState('')
  const validCustom =
    Number.isFinite(Date.parse(customFrom)) &&
    Number.isFinite(Date.parse(customTo)) &&
    Date.parse(customFrom) <= Date.parse(customTo)
  const hours: Record<string, number> = {
    '15m': 0.25,
    '1h': 1,
    '6h': 6,
    '24h': 24,
    '7d': 168,
  }
  const windowStart = useRef(new Date(Date.now() - 3600000).toISOString())
  const rangeKey = `${range}|${params.get('from')}|${params.get('to')}`
  useEffect(() => {
    setLive(false)
    windowStart.current = new Date(
      Date.now() - hours[range] * 3600000,
    ).toISOString()
    newest.current = ''
    boundaries.current.clear()
    setLines([])
    setCursor(undefined)
    setHasOlder(false)
    setPaused(null)
  }, [deviceId, name, rangeKey])
  const baseQuery = () => {
    const p = new URLSearchParams({
      from: params.get('from') || windowStart.current,
      limit: '5000',
    })
    if (params.get('to')) p.set('to', params.get('to')!)
    if (stack && services.length)
      p.set('services', services.slice(0, 25).join(','))
    return p
  }
  const add = (incoming: LogLine[]) => {
    const normalized = incoming.map((line) => ({
      ...line,
      container: line.container || name,
    }))
    setLines((old) => {
      const merged = mergeLogLines(old, normalized)
      if (merged.dropped) setDropped(true)
      return merged.lines
    })
    const bound =
      clock.current.server +
      (performance.now() - clock.current.monotonic) +
      300000
    for (const l of incoming)
      if (Date.parse(l.t) <= bound) {
        const svc = l.container || name
        const prior = boundaries.current.get(svc) || ''
        if (timestampMicros(l.t) > timestampMicros(prior))
          boundaries.current.set(svc, l.t)
        if (timestampMicros(l.t) > timestampMicros(newest.current))
          newest.current = l.t
      }
  }
  const history = useQuery({
    queryKey: ['docker-log-history', deviceId, name, rangeKey],
    queryFn: async () => {
      const p = baseQuery()
      if (newest.current) p.set('after', newest.current)
      const data = await dockerApi.history(deviceId, name, p.toString(), stack)
      if (data.server_now)
        clock.current = {
          server: Date.parse(data.server_now),
          monotonic: performance.now(),
        }
      add(data.entries ?? [])
      if (!newest.current || data.has_more) {
        setCursor(data.next_before)
        setHasOlder(data.has_more)
      }
      return data
    },
    refetchInterval: live || params.has('to') ? false : 15000,
  })
  useEffect(() => {
    if (!live) return
    let closed = false
    let socket: WebSocket | undefined
    let timer: ReturnType<typeof setTimeout> | undefined
    let hiddenTimer: ReturnType<typeof setTimeout> | undefined
    const names = stack ? chosen.slice(0, 5) : [name]
    if (import.meta.env.VITE_DEMO === 'true') {
      setStatus('Live · demo')
      timer = setInterval(
        () =>
          add([
            {
              t: new Date().toISOString(),
              s: 'o',
              l: 'Demo: request completed',
              container: names[0],
            },
          ]),
        1500,
      )
      return () => clearInterval(timer)
    }
    setStatus('Connecting')
    socket = new WebSocket(
      `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/docker/logs/${encodeURIComponent(deviceId)}`,
    )
    socket.onopen = () => {
      retry.current = 0
      socket?.send(
        JSON.stringify({
          type: 'attach',
          containers: names.map((n) => ({
            name: n,
            ...(boundaries.current.get(n)
              ? { boundary: boundaries.current.get(n) }
              : {}),
          })),
          merged: stack,
        }),
      )
    }
    socket.onmessage = (e) => {
      try {
        const data = JSON.parse(String(e.data))
        if (data.type === 'session') {
          setStatus(data.state)
          if (data.server_now)
            clock.current = {
              server: Date.parse(data.server_now),
              monotonic: performance.now(),
            }
        }
        if (data.type === 'lines' && Array.isArray(data.lines))
          add(
            data.lines.map((l: LogLine) => ({
              ...l,
              container: data.container,
            })),
          )
        if (
          data.type === 'marker' &&
          (data.kind !== 'backfill_end' || data.skipped > 0 || data.skipped_lb)
        )
          add([
            {
              t: new Date(
                clock.current.server +
                  performance.now() -
                  clock.current.monotonic,
              ).toISOString(),
              container: data.container,
              gap: {
                count: data.n ?? data.skipped ?? 0,
                lower_bound: !!data.skipped_lb,
                reason:
                  data.kind === 'backfill_end' ? 'backfill limit' : data.kind,
              },
            },
          ])
        if (data.type === 'error') {
          setStatus(data.code)
          if (
            [
              'session_expired',
              'unsupported',
              'not_found',
              'client_too_slow',
            ].includes(data.code)
          ) {
            setLive(false)
            socket?.close()
          }
        }
      } catch {
        setStatus('Invalid log frame')
      }
    }
    socket.onclose = () => {
      if (!closed) {
        setStatus('Reconnecting')
        timer = setTimeout(
          () => setReconnect((v) => v + 1),
          Math.min(30000, 1000 * 2 ** retry.current++),
        )
      }
    }
    const visibility = () => {
      clearTimeout(hiddenTimer)
      if (document.hidden)
        hiddenTimer = setTimeout(() => {
          setLive(false)
          setStatus('Live stopped while hidden')
        }, 60000)
    }
    document.addEventListener('visibilitychange', visibility)
    return () => {
      closed = true
      clearTimeout(timer)
      clearTimeout(hiddenTimer)
      document.removeEventListener('visibilitychange', visibility)
      if (socket?.readyState === WebSocket.OPEN)
        socket.send(JSON.stringify({ type: 'detach' }))
      socket?.close()
    }
  }, [live, deviceId, name, stack, chosen.join(','), reconnect])
  const shown = (paused ?? lines).filter(
    (l) =>
      (stream === 'all' || l.gap || l.s === stream) &&
      (!filter ||
        (l.l ?? l.gap?.reason ?? '')
          .toLowerCase()
          .includes(filter.toLowerCase())),
  )
  useEffect(() => {
    if (!paused && viewer.current)
      viewer.current.scrollTop = viewer.current.scrollHeight
  }, [lines, paused])
  function download() {
    const blob = new Blob(
      [
        downloadLogs(
          lines.filter((l) => stream === 'all' || l.gap || l.s === stream),
        ),
      ],
      { type: 'text/plain;charset=utf-8' },
    )
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${hostname || deviceId}_${name}_${new Date()
      .toISOString()
      .replace(/[-:]/g, '')
      .replace(/\.\d+Z/, 'Z')}.log`.replace(/[^A-Za-z0-9._-]/g, '_')
    a.click()
    URL.revokeObjectURL(url)
  }
  return (
    <div className="space-y-3">
      <p className="text-xs text-text-muted">
        Known secret patterns are masked, but logs may still contain sensitive
        data. Times are agent asserted.
      </p>
      <div className="flex flex-wrap gap-2 items-center">
        <select
          aria-label="History range"
          value={params.has('from') && params.has('to') ? 'custom' : range}
          onChange={(e) => {
            if (e.target.value === 'custom') {
              setCustomFrom(
                params.get('from') ||
                  new Date(Date.now() - 3600000).toISOString(),
              )
              setCustomTo(params.get('to') || new Date().toISOString())
              setCustomOpen(true)
              return
            }
            const p = new URLSearchParams(params)
            p.set('range', e.target.value)
            p.delete('from')
            p.delete('to')
            setParams(p)
          }}
          className="bg-surface p-2 rounded"
        >
          {Object.keys(hours).map((r) => (
            <option key={r}>{r}</option>
          ))}
          <option value="custom">Custom range…</option>
        </select>
        <input
          aria-label="Filter log text"
          placeholder="Filter log text"
          className="bg-surface border border-border p-2 rounded"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        <select
          aria-label="Log stream"
          value={stream}
          onChange={(e) => setStream(e.target.value)}
          className="bg-surface p-2 rounded"
        >
          <option value="all">Both streams</option>
          <option value="o">stdout</option>
          <option value="e">stderr</option>
        </select>
        <label>
          <input
            type="checkbox"
            checked={wrap}
            onChange={(e) => setWrap(e.target.checked)}
          />{' '}
          Wrap
        </label>
        <label>
          <input
            type="checkbox"
            checked={timestamps}
            onChange={(e) => setTimestamps(e.target.checked)}
          />{' '}
          Timestamps
        </label>
        <Button size="sm" variant="secondary" onClick={download}>
          Download
        </Button>
        <Button
          size="sm"
          variant="secondary"
          onClick={() => setPaused((p) => (p ? null : [...lines]))}
        >
          {paused
            ? `Follow (${Math.max(0, lines.length - paused.length)} new)`
            : 'Pause'}
        </Button>
        <label title={!liveSupported ? 'Requires agent update' : undefined}>
          <input
            type="checkbox"
            disabled={!liveSupported || (stack && chosen.length === 0)}
            checked={live}
            onChange={(e) => setLive(e.target.checked)}
          />{' '}
          Live
        </label>
        <span role="status" className="text-xs text-text-muted">
          {liveSupported ? status : 'Requires agent update for Live'}
        </span>
      </div>
      {stack && (
        <fieldset className="flex flex-wrap gap-3 text-xs">
          <legend>Live services (up to 5)</legend>
          {services.map((s) => (
            <label key={s}>
              <input
                type="checkbox"
                checked={chosen.includes(s)}
                disabled={!chosen.includes(s) && chosen.length >= 5}
                onChange={() =>
                  setChosen((old) =>
                    old.includes(s) ? old.filter((x) => x !== s) : [...old, s],
                  )
                }
              />
              {s}
            </label>
          ))}
        </fieldset>
      )}
      <QueryState query={history}>
        {(data) => (
          <>
            <p className="text-xs text-text-muted">
              Stored history retained for {data.retention_days} days
              {data.retention_limited_by
                ? ` · limited by ${data.retention_limited_by.replace(/_/g, ' ')}`
                : ''}
              .{' '}
              {data.range_clamped
                ? 'Range clamped to available retention.'
                : ''}{' '}
              {!data.pre_recreate_history
                ? 'History before container recreation is unavailable.'
                : ''}
            </p>
            {data.newest_stored_at && (
              <p className="text-xs text-text-muted">
                History up to {timestamp(data.newest_stored_at)}. New lines
                appear within about one collection interval, or turn on Live.
              </p>
            )}
            {['disabled', 'unknown_no_data'].includes(data.collector) && (
              <p>
                The <code>container_logs</code> collector is not enabled on this
                host.
              </p>
            )}
          </>
        )}
      </QueryState>
      {lines.some((l) => l.gap?.reason === 'quota_sampled') && (
        <p role="status" className="text-warn-fg">
          Stored history is sampled (storage quota); Live shows every line
        </p>
      )}
      {dropped && <p>Older lines were dropped from view</p>}
      {hasOlder && (
        <Button
          size="sm"
          variant="secondary"
          onClick={async () => {
            try {
              const p = baseQuery()
              if (cursor) p.set('before', cursor)
              const data = await dockerApi.history(
                deviceId,
                name,
                p.toString(),
                stack,
              )
              add(data.entries)
              setCursor(data.next_before)
              setHasOlder(data.has_more)
              setOlderError(undefined)
            } catch (e) {
              setOlderError(e)
            }
          }}
        >
          Load older lines
        </Button>
      )}
      {olderError ? <ErrorPanel error={olderError} /> : null}
      <p className="text-xs text-text-muted">
        {filter ? `${shown.length} matching lines` : `${shown.length} lines`}
      </p>
      <div
        ref={viewer}
        role="log"
        aria-label="Container logs"
        aria-live="off"
        className={`h-[32rem] overflow-auto bg-gray-950 border border-border rounded p-3 font-mono text-xs ${wrap ? 'whitespace-pre-wrap break-all' : 'whitespace-pre'}`}
      >
        {!shown.length && (
          <p className="text-text-muted">
            {live ? 'No buffered lines yet.' : 'No stored lines in this range.'}
          </p>
        )}
        {Array.from({ length: Math.ceil(shown.length / 200) }, (_, chunk) => (
          <div
            key={chunk}
            style={{
              contentVisibility: 'auto',
              containIntrinsicSize: 'auto 4000px',
            }}
          >
            {shown.slice(chunk * 200, (chunk + 1) * 200).map((l, i) => (
              <div
                key={`${chunk}-${i}`}
                className={
                  l.gap
                    ? 'text-warn-fg'
                    : l.s === 'e'
                      ? 'text-red-300'
                      : 'text-gray-300'
                }
              >
                {timestamps && `${timestamp(l.t)} `}
                {l.agent_t && (
                  <span
                    title={`Server time ${l.t}; original agent time ${l.agent_t}`}
                  >
                    [clock skew: received {timestamp(l.t)}; agent{' '}
                    {timestamp(l.agent_t)}]{' '}
                  </span>
                )}
                {l.container && `[${l.container}] `}
                {l.s === 'e' && 'ERR '}
                {l.gap ? (
                  `[${l.gap.lower_bound ? 'At least ' : ''}${l.gap.count} lines not stored (${l.gap.reason === 'quota_sampled' ? 'storage quota' : l.gap.reason === 'server_cap' ? 'server ceiling' : l.gap.reason === 'agent_cap' ? 'per-container limit' : l.gap.reason})]`
                ) : (
                  <Highlight text={stripAnsi(l.l ?? '')} filter={filter} />
                )}
                {l.x ? ' [truncated]' : ''}
              </div>
            ))}
          </div>
        ))}
      </div>
      <Modal
        open={customOpen}
        onClose={() => setCustomOpen(false)}
        title="Custom log range"
        footer={
          <>
            <Button variant="secondary" onClick={() => setCustomOpen(false)}>
              Cancel
            </Button>
            <Button
              disabled={!validCustom}
              onClick={() => {
                const p = new URLSearchParams(params)
                p.set('from', new Date(customFrom).toISOString())
                p.set('to', new Date(customTo).toISOString())
                p.delete('range')
                setParams(p)
                setCustomOpen(false)
              }}
            >
              Apply range
            </Button>
          </>
        }
      >
        <p className="text-xs text-text-muted mb-3">
          Enter timestamps with a time zone, for example 2026-10-19T14:00:00Z.
          Ranges are limited to retained history.
        </p>
        <label className="block text-sm">
          Start
          <input
            aria-label="Custom range start"
            className="block w-full rounded border border-border bg-surface p-2"
            value={customFrom}
            onChange={(event) => setCustomFrom(event.target.value)}
          />
        </label>
        <label className="block text-sm mt-3">
          End
          <input
            aria-label="Custom range end"
            className="block w-full rounded border border-border bg-surface p-2"
            value={customTo}
            onChange={(event) => setCustomTo(event.target.value)}
          />
        </label>
      </Modal>
    </div>
  )
}

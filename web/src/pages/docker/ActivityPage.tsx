import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import {
  Button,
  EmptyState,
  Modal,
  QueryState,
  Timestamp,
  enumLabel,
} from '../../components/ui'
import type { ActivityEntry } from '../../types/docker'
import { containerPath, stackPath } from '../../utils/dockerUrlState'
import { stripAnsi } from '../../utils/logBuffer'
export default function ActivityPage({
  deviceId,
  container,
  stack,
}: {
  deviceId?: string
  container?: string
  stack?: string
}) {
  const [params, setParams] = useSearchParams()
  const [cursor, setCursor] = useState('')
  const [chosen, setChosen] = useState<ActivityEntry>()
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
  })
  const stacks = useQuery({
    queryKey: ['docker-stacks'],
    queryFn: dockerApi.stacks,
  })
  const commandId = chosen?.id ?? params.get('command')
  const commandDevice = chosen?.device_id ?? deviceId ?? params.get('host')
  const p = new URLSearchParams()
  for (const key of ['actor', 'state', 'type', 'from', 'to'])
    if (params.get(key)) p.set(key, params.get(key)!)
  if (deviceId || params.get('host'))
    p.set('device_id', deviceId || params.get('host')!)
  if (container) p.set('container', container)
  if (stack || params.get('stack'))
    p.set('stack', stack || params.get('stack')!)
  if (cursor) p.set('cursor', cursor)
  const query = useQuery({
    queryKey: ['docker-activity', p.toString()],
    queryFn: () => dockerApi.activity(p.toString()),
    refetchInterval: 15000,
  })
  const detail = useQuery({
    queryKey: ['docker-activity-detail', commandId, commandDevice],
    queryFn: () => dockerApi.activityDetail(commandId!, commandDevice!),
    enabled:
      !!commandId && !!commandDevice && (!chosen || chosen.kind === 'command'),
  })
  const write = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    value ? next.set(key, value) : next.delete(key)
    setParams(next)
    setCursor('')
  }
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">Activity</h2>
      <div className="flex flex-wrap gap-3">
        {!deviceId && (
          <select
            aria-label="Activity host"
            className="bg-surface p-2 rounded"
            value={params.get('host') ?? ''}
            onChange={(event) => {
              const p = new URLSearchParams(params)
              event.target.value
                ? p.set('host', event.target.value)
                : p.delete('host')
              p.delete('stack')
              setParams(p)
              setCursor('')
            }}
          >
            <option value="">All hosts</option>
            {hosts.data
              ?.filter((host) => host.docker_available)
              .map((host) => (
                <option key={host.device_id} value={host.device_id}>
                  {host.hostname}
                </option>
              ))}
          </select>
        )}
        {!stack && (deviceId || params.get('host')) && (
          <select
            aria-label="Activity stack"
            className="bg-surface p-2 rounded"
            value={params.get('stack') ?? ''}
            onChange={(event) => write('stack', event.target.value)}
          >
            <option value="">All stacks</option>
            {stacks.data
              ?.filter(
                (stack) => stack.device_id === (deviceId || params.get('host')),
              )
              .map((stack) => (
                <option key={stack.project} value={stack.project}>
                  {stack.project}
                </option>
              ))}
          </select>
        )}
        <label className="text-xs text-text-muted">
          From (UTC)
          <input
            aria-label="Activity from"
            type="datetime-local"
            className="block rounded bg-surface p-2"
            value={
              params.get('from') &&
              Number.isFinite(Date.parse(params.get('from')!))
                ? new Date(params.get('from')!).toISOString().slice(0, 16)
                : ''
            }
            onChange={(event) =>
              write(
                'from',
                event.target.value
                  ? new Date(event.target.value + 'Z').toISOString()
                  : '',
              )
            }
          />
        </label>
        <label className="text-xs text-text-muted">
          To (UTC)
          <input
            aria-label="Activity to"
            type="datetime-local"
            className="block rounded bg-surface p-2"
            value={
              params.get('to') && Number.isFinite(Date.parse(params.get('to')!))
                ? new Date(params.get('to')!).toISOString().slice(0, 16)
                : ''
            }
            onChange={(event) =>
              write(
                'to',
                event.target.value
                  ? new Date(event.target.value + 'Z').toISOString()
                  : '',
              )
            }
          />
        </label>
        <select
          aria-label="Activity actor"
          value={params.get('actor') ?? ''}
          onChange={(e) => write('actor', e.target.value)}
          className="bg-surface p-2 rounded"
        >
          <option value="">All actors</option>
          {['operator', 'system', 'agent', 'unknown'].map((a) => (
            <option key={a}>{a}</option>
          ))}
        </select>
        <select
          aria-label="Activity type"
          value={params.get('type') ?? ''}
          onChange={(e) => write('type', e.target.value)}
          className="bg-surface p-2 rounded"
        >
          <option value="">Commands and events</option>
          <option value="command">Commands</option>
          <option value="event">Events</option>
        </select>
        <select
          aria-label="Activity state"
          value={params.get('state') ?? ''}
          onChange={(e) => write('state', e.target.value)}
          className="bg-surface p-2 rounded"
        >
          <option value="">All states</option>
          {[
            'queued',
            'dispatched',
            'running',
            'succeeded',
            'timed_out',
            'failed',
            'cancelled',
            'expired',
          ].map((s) => (
            <option key={s}>{s}</option>
          ))}
        </select>
      </div>
      <QueryState
        query={query}
        isEmpty={(d) => !d.entries.length}
        empty={
          <EmptyState
            title="No Docker activity"
            description="Commands and observed Docker events will appear here."
          />
        }
      >
        {(data) => (
          <div className="space-y-3">
            {data.entries.map((e) => (
              <article
                key={e.kind + e.id}
                className="rounded border border-border p-3 space-y-1"
              >
                <div className="flex flex-wrap gap-3">
                  <Timestamp value={e.requested_at} />
                  <strong>
                    {enumLabel(
                      e.kind === 'command' ? 'commandAction' : 'eventType',
                      e.action,
                    )}
                  </strong>
                  <span>
                    {e.status ? enumLabel('commandStatus', e.status) : ''}
                  </span>
                  <span>{e.hostname}</span>
                  <span className="text-text-muted">
                    {e.actor_kind === 'agent_observed'
                      ? 'Agent (observed)'
                      : e.actor_name || e.actor_kind || 'Unknown'}
                    {e.attribution === 'inferred'
                      ? ' · inferred attribution'
                      : ''}
                  </span>
                </div>
                <p className="text-sm whitespace-pre-wrap">
                  {stripAnsi(e.message)}
                </p>
                {e.failure_step && <p>Failed at {e.failure_step}</p>}
                {e.target_exists && (e.container || e.stack) && (
                  <Link
                    className="text-link text-sm"
                    to={
                      e.container
                        ? containerPath(e.device_id, e.container)
                        : stackPath(e.device_id, e.stack!)
                    }
                  >
                    Open resource
                  </Link>
                )}
                {e.kind === 'command' && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setChosen(e)}
                  >
                    View steps and output
                  </Button>
                )}
              </article>
            ))}
            {data.next_cursor && (
              <Button
                variant="secondary"
                onClick={() => setCursor(data.next_cursor!)}
              >
                Older activity
              </Button>
            )}
            {cursor && (
              <Button variant="ghost" onClick={() => setCursor('')}>
                Newest
              </Button>
            )}
          </div>
        )}
      </QueryState>
      <Modal
        open={!!chosen || !!params.get('command')}
        onClose={() => {
          setChosen(undefined)
          if (params.has('command')) {
            const p = new URLSearchParams(params)
            p.delete('command')
            setParams(p, { replace: true })
          }
        }}
        title="Command details"
      >
        <QueryState query={detail}>
          {(d) => (
            <div className="space-y-3">
              {d.steps.map((s) => (
                <section key={s.seq}>
                  <h3>
                    {s.seq}. {s.name} · {s.outcome}
                  </h3>
                  <pre className="max-h-80 overflow-auto whitespace-pre-wrap text-xs">
                    {stripAnsi(s.output ?? '')}
                    {s.truncated ? '\n[truncated]' : ''}
                  </pre>
                </section>
              ))}
              {d.output_truncated && <p>Output was truncated.</p>}
              {d.target_results.length > 0 && (
                <pre className="text-xs whitespace-pre-wrap">
                  {JSON.stringify(d.target_results, null, 2)}
                </pre>
              )}
              {d.events.map((e, i) => (
                <p key={i}>
                  {e.created_at} {e.type}: {e.message}
                </p>
              ))}
            </div>
          )}
        </QueryState>
      </Modal>
    </div>
  )
}

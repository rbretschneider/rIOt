import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Command, Event } from '../types/models'
import { Badge, Timestamp, enumLabel } from './ui'
import { isTerminal } from '../utils/commandLifecycle'
import {
  SELF_UPDATE_ACTOR_PHRASE,
  selfUpdateActionLabel,
  selfUpdateOutcomeLabel,
  selfUpdateVersions,
} from '../utils/selfUpdate'
import {
  REINSTALL_ACTION_LABEL,
  REINSTALL_ACTOR_PHRASE,
  isReinstall,
  reinstallOutcomeLabel,
} from '../utils/reinstall'

// EXPECT-002 (FR-027, AD-009, ADD 6.3): the read-only view of an agent's own
// self-update record. It is a record of something the agent did, not a command, so
// it has no cancel, retry or any other control. Every string here (the result
// message, event messages) came from the server and is rendered as React text.

const LIVE_REFETCH_MS = 5000

function EventList({ events, testId, label }: { events: Event[]; testId: string; label: string }) {
  if (events.length === 0) return <p className="text-xs italic text-text-muted">None</p>
  return (
    <ul
      aria-label={label}
      data-testid={testId}
      className="max-h-48 space-y-1 overflow-y-auto scrollbar-thin rounded bg-gray-800/50 p-2"
    >
      {events.map(e => (
        <li key={e.id} className="text-xs">
          <div className="flex flex-wrap items-center gap-2">
            <Timestamp value={e.created_at} className="font-mono text-text-muted" />
            <Badge category="severity" value={e.severity} className="text-[10px] px-1.5" />
            <span className="text-text-muted">{enumLabel('eventType', e.type)}</span>
          </div>
          <p className="break-words text-gray-200">{e.message}</p>
        </li>
      ))}
    </ul>
  )
}

export default function SelfUpdateDetails({ deviceId, command }: { deviceId: string; command: Command }) {
  // EXPECT-008 (FR-026): the server's record of an installer re-run uses this same read-only view
  // with its own wording. It has no versions and no controls.
  const reinstall = isReinstall(command)
  const label = reinstall ? REINSTALL_ACTION_LABEL : selfUpdateActionLabel(selfUpdateVersions(command))
  const actorPhrase = reinstall ? REINSTALL_ACTOR_PHRASE : SELF_UPDATE_ACTOR_PHRASE
  const started = command.started_at ?? command.dispatched_at ?? command.created_at

  // The status is part of the key so the list is fetched again when the outcome lands.
  const eventsQuery = useQuery({
    queryKey: ['command-events', deviceId, command.id, command.status],
    queryFn: () => api.getCommandEvents(deviceId, command.id),
    refetchInterval: isTerminal(command.status) ? false : LIVE_REFETCH_MS,
  })
  const data = eventsQuery.data
  const serverEvents = data?.server_events ?? []
  const agentEvents = data?.agent_events ?? []
  const serverOmitted = data?.server_events_omitted ?? 0
  const agentOmitted = data?.agent_events_omitted ?? 0

  return (
    <div className="space-y-3 text-xs" data-testid="self-update-details">
      <div>
        <p className="text-sm font-medium text-gray-100">{label}</p>
        <p className="text-text-muted">{actorPhrase}</p>
      </div>

      <dl className="grid grid-cols-2 gap-3">
        <div>
          <dt className="mb-0.5 text-gray-500">Started</dt>
          <dd className="font-mono text-gray-300">
            <Timestamp value={started} />
          </dd>
        </div>
        <div>
          <dt className="mb-0.5 text-gray-500">Outcome</dt>
          <dd className="text-gray-200" data-testid="self-update-outcome">
            {reinstall ? reinstallOutcomeLabel(command.status) : selfUpdateOutcomeLabel(command.status)}
          </dd>
        </div>
      </dl>

      {command.result_msg && (
        <div>
          <p className="mb-1 text-gray-500">Summary</p>
          <p className="whitespace-pre-wrap break-words rounded bg-gray-800/50 p-2 text-sm text-gray-300">
            {command.result_msg}
          </p>
        </div>
      )}

      <div>
        <p className="mb-1 text-gray-500">Events from the server</p>
        {eventsQuery.isLoading ? (
          <p className="text-text-muted">Loading events...</p>
        ) : data ? (
          <>
            <EventList events={serverEvents} testId="self-update-server-events" label="Events from the server" />
            {serverOmitted > 0 && (
              <p className="mt-1 text-text-muted">{serverOmitted} more server events not shown</p>
            )}
          </>
        ) : (
          <p role="alert" className="text-danger-fg">
            Could not load the linked events.
          </p>
        )}
      </div>

      {data && (
        <div>
          <p className="mb-1 text-gray-500">Events from the agent</p>
          <EventList events={agentEvents} testId="self-update-agent-events" label="Events from the agent" />
          {agentOmitted > 0 && (
            <p className="mt-1 text-text-muted">{agentOmitted} more agent events not shown</p>
          )}
        </div>
      )}
    </div>
  )
}

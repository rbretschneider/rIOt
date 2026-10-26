import { useMutation, useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'
import { Badge, Button, ErrorPanel, QueryState, enumLabel } from '../ui'
import { isTerminal, normalizeStatus } from '../../utils/commandLifecycle'
import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
export default function CommandProgress({
  deviceId,
  commandId,
  onTerminal,
  showActivityLink = true,
}: {
  deviceId: string
  commandId: string
  onTerminal?: () => void
  // EXPECT-001 (X-3): host and agent commands have no Docker activity entry, so the
  // activity modal hides the "View steps and output" link.
  showActivityLink?: boolean
}) {
  const query = useQuery({
    queryKey: ['docker-command', deviceId, commandId],
    queryFn: () => api.getCommand(deviceId, commandId),
    refetchInterval: (q) =>
      q.state.data && isTerminal(q.state.data.status) ? false : 2000,
  })
  const notified = useRef('')
  const cancel = useMutation({
    mutationFn: () => api.cancelCommand(deviceId, commandId),
    onSuccess: () => query.refetch(),
  })
  useEffect(() => {
    const command = query.data
    if (
      command &&
      isTerminal(command.status) &&
      notified.current !== command.id
    ) {
      notified.current = command.id
      onTerminal?.()
    }
  }, [query.data, onTerminal])
  return (
    <QueryState query={query}>
      {(c) => (
        <div className="p-3 rounded border border-border space-y-2">
          <div className="flex gap-3 items-center">
            <span>{enumLabel('commandAction', c.action)}</span>
            <Badge category="commandStatus" value={c.status} />
          </div>
          {c.result_msg && <p role="alert">{c.result_msg}</p>}
          {normalizeStatus(c.status) === 'queued' && (
            <Button
              variant="secondary"
              size="sm"
              pending={cancel.isPending}
              onClick={() => cancel.mutate()}
            >
              Cancel queued command
            </Button>
          )}
          {cancel.error && <ErrorPanel error={cancel.error} />}
          {!isTerminal(c.status) && (
            <p className="text-text-muted text-xs">
              {c.step || 'Waiting for agent'}
            </p>
          )}
          {c.target_results?.map((target, index) => (
            <p key={`${target.container}-${index}`} className="text-sm">
              {target.container}: {target.outcome}
              {target.message ? ` — ${target.message}` : ''}
              {target.step ? ` (${target.step})` : ''}
            </p>
          ))}
          {showActivityLink && (
            <Link
              className="text-link text-xs"
              to={`/docker/activity?host=${encodeURIComponent(deviceId)}&command=${encodeURIComponent(commandId)}`}
            >
              View steps and output
            </Link>
          )}
        </div>
      )}
    </QueryState>
  )
}

import { useCallback, useRef } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { useCommandTracker } from '../../hooks/useCommandTracker'
import { composeTargetKey, normalizeStatus } from '../../utils/commandLifecycle'
import { stackPath } from '../../utils/dockerUrlState'
import { Button, ErrorPanel, Modal, QueryState } from '../ui'
import CommandProgress from './CommandProgress'

export default function StackUpdateDialog({ deviceId, project, onClose }: {
  deviceId: string
  project: string
  onClose: () => void
}) {
  const location = useLocation()
  const qc = useQueryClient()
  const sending = useRef(false)
  const query = useQuery({
    queryKey: ['docker-stack', deviceId, project],
    queryFn: () => dockerApi.stack(deviceId, project),
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
    staleTime: 0,
    refetchOnMount: 'always',
  })
  const stack = query.data
  const host = hosts.data?.find((h) => h.device_id === deviceId)
  const validTarget = stack?.device_id === deviceId && stack.project === project
  const pathsAgreed = validTarget && stack.path_state === 'agreed' && !!stack.agreed_working_dir
  const supported = host?.capabilities.includes('stack_actions_v1') ?? false
  const fresh = query.isSuccess && hosts.isSuccess && !query.isFetching && !hosts.isFetching
  const allowed = fresh && pathsAgreed && supported
  const tracker = useCommandTracker(deviceId, pathsAgreed ? composeTargetKey(stack.agreed_working_dir) : null)
  const refresh = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['docker-stack', deviceId, project] })
    void qc.invalidateQueries({ queryKey: ['docker-stacks'] })
    void qc.invalidateQueries({ queryKey: ['docker-containers'] })
  }, [qc, deviceId, project])
  async function run() {
    if (!allowed || tracker.isBusy || sending.current) return
    sending.current = true
    try {
      await tracker.track(() => dockerApi.stackAction(deviceId, project, { action: 'update' }))
    } finally {
      sending.current = false
    }
  }
  return (
    <Modal
      open
      pending={tracker.isStarting}
      onClose={onClose}
      title={`Update ${project}`}
      closeLabel="Dismiss update dialog"
      footer={<>
        <Button onClick={onClose} disabled={tracker.isStarting}>Close</Button>
        <Button
          variant="primary"
          disabled={!allowed || tracker.isBusy}
          pending={tracker.isStarting}
          pendingLabel="Starting…"
          onClick={() => { void run() }}
        >
          {tracker.isActive ? 'Update in progress' : tracker.command
            ? normalizeStatus(tracker.command.status) === 'succeeded' ? 'Update again' : 'Retry update'
            : 'Confirm update'}
        </Button>
      </>}
    >
      {query.isError ? <ErrorPanel error={query.error} onRetry={() => { void query.refetch() }} /> : (
        <QueryState query={query}>
          {(s) => <div className="space-y-3">
            <p>Update <strong>{project}</strong> on <strong>{s.hostname}</strong>?</p>
            <p>Pulls images and runs Compose up. Changed services may be recreated and stopped services may start.</p>
            <p className="break-words font-mono text-xs">{s.agreed_working_dir}<br />{s.agreed_config_files?.join(', ')}</p>
            {!pathsAgreed && <p className="text-warn-fg">Review this stack’s Compose paths before updating.</p>}
            {s.self_role && <p className="text-warn-fg">This stack contains rIOt infrastructure. The connection may be interrupted.</p>}
          </div>}
        </QueryState>
      )}
      {hosts.isError && <ErrorPanel title="Couldn't verify host capabilities" error={hosts.error} onRetry={() => { void hosts.refetch() }} />}
      {hosts.isSuccess && !supported && <p className="text-warn-fg">Requires an agent with stack actions support.</p>}
      {(query.isFetching || hosts.isFetching) && <p role="status" className="text-xs text-text-muted">Checking current stack and host capabilities…</p>}
      <Link className="text-link text-sm" to={stackPath(deviceId, project)} state={{ dockerCollection: location.pathname + location.search }}>Open stack details</Link>
      {tracker.error && <ErrorPanel title="Couldn't start update" error={tracker.error} />}
      {tracker.command && <div className="mt-3 space-y-2">
        <CommandProgress deviceId={deviceId} commandId={tracker.command.id} onTerminal={refresh} />
        <p className="text-xs text-text-muted">Closing this dialog does not cancel the command.</p>
      </div>}
    </Modal>
  )
}

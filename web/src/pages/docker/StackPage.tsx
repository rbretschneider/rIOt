import { Tabs } from '../../components/ui/Tabs'
import { DropdownMenu } from '../../components/ui/DropdownMenu'
import { stackPrimaryAction } from '../../utils/dockerActions'
import { useEffect, useRef, useState } from 'react'
import {
  Link,
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import {
  Button,
  EmptyState,
  ErrorPanel,
  Modal,
  PageHeader,
  QueryState,
} from '../../components/ui'
import AutoUpdateToggle from '../../components/docker/AutoUpdateToggle'
import DataTable from '../../components/docker/DataTable'
import CommandProgress from '../../components/docker/CommandProgress'
import LogView from '../../components/docker/LogView'
import ActivityPage from './ActivityPage'
import { containerPath } from '../../utils/dockerUrlState'
import type { DockerStack } from '../../types/docker'
import { useCommandTracker } from '../../hooks/useCommandTracker'
import { composeTargetKey } from '../../utils/commandLifecycle'
import { api } from '../../api/client'
function Compose({ deviceId, project }: { deviceId: string; project: string }) {
  const [enabled, setEnabled] = useState(false)
  const [index, setIndex] = useState<number>()
  const [showEnv, setShowEnv] = useState(false)
  const listing = useQuery({
    queryKey: ['docker-compose', deviceId, project],
    queryFn: () => dockerApi.compose(deviceId, project),
    enabled,
    gcTime: 0,
  })
  const file = useQuery({
    queryKey: ['docker-compose-file', deviceId, project, index],
    queryFn: () => dockerApi.composeFile(deviceId, project, index!),
    enabled: index !== undefined,
    gcTime: 0,
  })
  const env = useQuery({
    queryKey: ['docker-compose-env', deviceId, project],
    queryFn: () => dockerApi.env(deviceId, project),
    enabled: showEnv,
    gcTime: 0,
  })
  return (
    <div className="space-y-3">
      <p className="text-sm text-text-muted">
        Read-only compose and environment. Known secrets are masked; review
        content before sharing.
      </p>
      <Button
        variant="secondary"
        onClick={() => {
          setEnabled(true)
          if (enabled) void listing.refetch()
        }}
      >
        Read compose files
      </Button>
      {enabled && (
        <QueryState query={listing}>
          {(data) => (
            <>
              <p className="font-mono text-xs">{data.working_dir}</p>
              <div className="flex flex-wrap gap-2">
                {data.files.map((f) => (
                  <Button
                    key={f.index}
                    variant="secondary"
                    size="sm"
                    disabled={
                      f.status !== 'ok' &&
                      f.status !== 'available' &&
                      f.status !== ''
                    }
                    onClick={() => setIndex(f.index)}
                  >
                    {f.name} · {f.status}
                  </Button>
                ))}
                {data.env.present && (
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setShowEnv(true)}
                  >
                    Read masked environment
                  </Button>
                )}
              </div>
            </>
          )}
        </QueryState>
      )}
      {index !== undefined && (
        <QueryState query={file}>
          {(f) => (
            <>
              <h3>
                {f.name} · {f.redactions} values masked
              </h3>
              <pre className="max-h-[32rem] overflow-auto rounded border border-border p-3 text-xs">
                {f.content}
              </pre>
            </>
          )}
        </QueryState>
      )}
      {showEnv && (
        <QueryState query={env}>
          {(data) => (
            <div>
              <p>{data.redactions} values masked</p>
              <dl className="font-mono text-xs">
                {(data.keys ?? []).map((e) => (
                  <div key={e.key} className="flex gap-2">
                    <dt>{e.key}</dt>
                    <dd>{e.value}</dd>
                  </div>
                ))}
              </dl>
            </div>
          )}
        </QueryState>
      )}
    </div>
  )
}
function Paths({
  stack: s,
  refresh,
}: {
  stack: DockerStack
  refresh: () => unknown
}) {
  const [open, setOpen] = useState(false)
  const [checked, setChecked] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  return (
    <section
      className={`rounded border p-3 ${s.path_state === 'agreed' ? 'border-border' : 'border-warn-fg'}`}
    >
      <p>
        Compose paths: <strong>{s.path_state}</strong>
      </p>
      <p className="font-mono text-xs">
        Agreed: {s.agreed_working_dir} · {s.agreed_config_files?.join(', ')}
      </p>
      {s.path_state === 'conflicted' && (
        <p role="alert">
          Members disagree about compose paths. Actions and reads are blocked.
          Conflicting members: {s.conflict_members?.join(', ')}
        </p>
      )}
      {s.path_state === 'pending_ack' && (
        <>
          <p className="font-mono text-xs">
            Observed: {s.observed_working_dir} ·{' '}
            {s.observed_config_files?.join(', ')}
          </p>
          <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>
            Review changed paths
          </Button>
        </>
      )}
      <Modal
        open={open}
        onClose={() => {
          if (!pending) setOpen(false)
        }}
        title="Acknowledge compose paths"
        footer={
          <>
            <Button
              disabled={pending}
              variant="secondary"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button
              disabled={!checked || pending}
              onClick={async () => {
                setPending(true)
                try {
                  await dockerApi.acknowledge(s.device_id, s.project, {
                    working_dir: s.observed_working_dir,
                    config_files: s.observed_config_files,
                  })
                  setOpen(false)
                  refresh()
                } catch (e) {
                  setError(e)
                } finally {
                  setPending(false)
                }
              }}
            >
              Acknowledge paths
            </Button>
          </>
        }
      >
        <p>Previous working directory: {s.agreed_working_dir}</p>
        <p>Previous files: {s.agreed_config_files?.join(', ')}</p>
        <p>New working directory: {s.observed_working_dir}</p>
        <p>New files: {s.observed_config_files?.join(', ')}</p>
        <label>
          <input
            type="checkbox"
            checked={checked}
            onChange={(e) => setChecked(e.target.checked)}
          />{' '}
          I reviewed these paths on the host and trust their source.
        </label>
        {error ? <ErrorPanel error={error} /> : null}
      </Modal>
    </section>
  )
}
export default function StackPage() {
  const location = useLocation()
  const collection = (location.state as { dockerCollection?: unknown } | null)
    ?.dockerCollection
  const backTo =
    typeof collection === 'string' &&
    (collection === '/docker/stacks' ||
      collection.startsWith('/docker/stacks?'))
      ? collection
      : '/docker/stacks'
  const { deviceId, project } = useParams()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const query = useQuery({
    queryKey: ['docker-stack', deviceId, project],
    queryFn: () => dockerApi.stack(deviceId!, project!),
    refetchInterval: 15000,
  })
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
  })
  const host = hosts.data?.find((h) => h.device_id === deviceId)
  const caps = host?.capabilities ?? []
  const [action, setAction] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const [command, setCommand] = useState('')
  const [commandActive, setCommandActive] = useState(false)
  const updateSubmitting = useRef(false)
  const tracker = useCommandTracker(
    deviceId,
    query.data?.agreed_working_dir
      ? composeTargetKey(query.data.agreed_working_dir)
      : null,
  )
  useEffect(() => {
    if (tracker.command) {
      setCommand(tracker.command.id)
      setCommandActive(tracker.isBusy)
    }
  }, [tracker.command, tracker.isBusy])
  const tabs = ['services', 'logs', 'activity', 'compose']
  const tab = tabs.includes(params.get('tab') ?? '')
    ? params.get('tab')!
    : 'services'
  return (
    <QueryState query={query}>
      {(s) => {
        const self = !!s.self_role
        const required =
          self && ['down', 'redeploy', 'restart'].includes(action)
            ? s.hostname
            : ['down', 'redeploy'].includes(action)
              ? s.project
              : ''
        const primary = stackPrimaryAction(s, commandActive)
        const checkMembers = (s.services ?? []).filter(
          (service) => service.state !== 'removing',
        )
        const allowed =
          s.path_state === 'agreed' && caps.includes('stack_actions_v1')
        const updateAllowed = allowed && !!s.agreed_working_dir && !query.isFetching && !hosts.isFetching && query.isSuccess && hosts.isSuccess
        async function run() {
          const updating = action === 'update'
          if (updating && (!updateAllowed || pending || tracker.isBusy || commandActive || updateSubmitting.current)) return
          if (updating) updateSubmitting.current = true
          setPending(true)
          setError(undefined)
          try {
            if (action === 'forget') {
              await dockerApi.forget(deviceId!, project!)
              await qc.invalidateQueries({ queryKey: ['docker-stacks'] })
              navigate('/docker/stacks')
            } else {
              const c = await tracker.track(() =>
                dockerApi.stackAction(deviceId!, project!, {
                  action,
                  ...(required
                    ? { confirm: action === 'down' ? s.project : confirmation }
                    : {}),
                  ...(self && required ? { confirm_host: confirmation } : {}),
                }),
              )
              if (c) {
                setCommand(c.id)
                setCommandActive(true)
              }
            }
            setAction('')
          } catch (e) {
            setError(e)
          } finally {
            if (updating) updateSubmitting.current = false
            setPending(false)
          }
        }
        return (
          <div className="min-w-0 space-y-4">
            <Link
              className="inline-flex text-sm text-text-muted hover:text-text"
              to={backTo}
            >
              ← Stacks
            </Link>
            <PageHeader
              eyebrow="Compose stack"
              title={s.project}
              actions={
                <div className="flex flex-wrap items-start gap-2">
                  {primary && (
                    <Button
                      disabled={!allowed || commandActive || (primary === 'update' && !updateAllowed)}
                      onClick={() => {
                        setConfirmation('')
                        setAction(primary)
                      }}
                    >
                      {primary[0].toUpperCase() + primary.slice(1)}
                    </Button>
                  )}
                  {['update', 'pull', 'redeploy', 'restart']
                    .filter((a) => a !== primary)
                    .map((a) => (
                      <Button
                        key={a}
                        variant="secondary"
                        disabled={!allowed || commandActive || (a === 'update' && !updateAllowed)}
                        onClick={() => {
                          setConfirmation('')
                          setAction(a)
                        }}
                      >
                        {a[0].toUpperCase() + a.slice(1)}
                      </Button>
                    ))}
                  <Button
                    variant="secondary"
                    disabled={
                      commandActive || tracker.isBusy || !checkMembers.length
                    }
                    onClick={() => {
                      void tracker.track(() =>
                        api.sendCommand(deviceId!, 'docker_check_updates', {
                          compose_project: project,
                          container_ids: checkMembers.map(
                            (service) => service.container_id,
                          ),
                        }),
                      )
                    }}
                  >
                    Check updates
                  </Button>
                  <DropdownMenu
                    items={[
                      {
                        label: 'Copy working directory',
                        onSelect: () => {
                          void navigator.clipboard.writeText(
                            s.agreed_working_dir,
                          )
                        },
                      },
                      ...(s.state === 'down'
                        ? [
                            {
                              label: 'Forget stack',
                              disabled: commandActive,
                              onSelect: () => setAction('forget'),
                            },
                          ]
                        : []),
                      {
                        label: 'Down',
                        danger: true,
                        disabled:
                          !allowed || commandActive || s.state === 'down',
                        onSelect: () => {
                          setConfirmation('')
                          setAction('down')
                        },
                      },
                    ]}
                  />{' '}
                </div>
              }
            >
              <span className="rounded-full border border-border bg-raised-2 px-2 py-0.5 text-xs text-text-muted">
                {s.state}
              </span>
              <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-text-muted">
                <Link
                  className="text-link"
                  to={`/docker/stacks?host=${encodeURIComponent(s.device_id)}`}
                >
                  Host: {s.hostname}
                </Link>
                <span>
                  {s.running}/{s.total} services running
                </span>
                {s.updates > 0 && (
                  <span className="text-info-fg">
                    {s.updates} updates available
                  </span>
                )}
                {self && (
                  <span className="text-warn-fg">rIOt infrastructure</span>
                )}
              </div>
              {!caps.includes('stack_actions_v1') && (
                <p className="text-warn-fg">
                  Requires agent update for stack actions
                </p>
              )}
            </PageHeader>
            {s.path_state !== 'agreed' && (
              <Paths stack={s} refresh={() => query.refetch()} />
            )}
            {tracker.error && <ErrorPanel error={tracker.error} />}
            {command && (
              <>
                <CommandProgress
                  deviceId={deviceId!}
                  commandId={command}
                  onTerminal={() => {
                    setCommandActive(false)
                    void qc.invalidateQueries({ queryKey: ['docker-stack', deviceId, project] })
                    void qc.invalidateQueries({ queryKey: ['docker-stacks'] })
                    void qc.invalidateQueries({ queryKey: ['docker-containers'] })
                  }}
                />
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setCommand('')}
                >
                  Dismiss progress
                </Button>
              </>
            )}
            <Tabs
              label="Stack details"
              tabs={tabs.map((value) => ({
                value,
                label: value[0].toUpperCase() + value.slice(1),
              }))}
              value={tab}
              onValueChange={(value) => {
                const p = new URLSearchParams(params)
                p.set('tab', value)
                setParams(p)
              }}
            >
              {' '}
              {tab === 'services' && (
                <AutoUpdateToggle
                  deviceId={deviceId!}
                  target={project!}
                  stack
                  workingDir={s.agreed_working_dir}
                  disabled={!allowed}
                />
              )}
              {tab === 'services' &&
                (s.services?.length ? (
                  <DataTable
                    rows={s.services}
                    rowKey={(r) => r.container_id}
                    columns={[
                      {
                        key: 'name',
                        title: 'Service',
                        render: (r) => (
                          <Link
                            className="text-link"
                            to={containerPath(r.device_id, r.container_name)}
                          >
                            {r.container_name}
                          </Link>
                        ),
                      },
                      { key: 'state', title: 'State', render: (r) => r.state },
                      { key: 'image', title: 'Image', render: (r) => r.image },
                      {
                        key: 'update',
                        title: 'Update',
                        render: (r) => (r.update_available ? 'Available' : '—'),
                      },
                    ]}
                  />
                ) : (
                  <EmptyState
                    title="Stack is down"
                    description="Compose paths are retained so this stack can be brought up again."
                  />
                ))}
              {tab === 'logs' && (
                <LogView
                  deviceId={deviceId!}
                  name={project!}
                  stack
                  services={(s.services ?? []).map((r) => r.container_name)}
                  liveSupported={caps.includes('logs_live_v1')}
                  hostname={s.hostname}
                />
              )}{' '}
              {tab === 'activity' && (
                <ActivityPage deviceId={deviceId} stack={project} />
              )}{' '}
              {tab === 'compose' && (
                <div className="space-y-4">
                  {s.path_state === 'agreed' && (
                    <Paths stack={s} refresh={() => query.refetch()} />
                  )}
                  {caps.includes('compose_read_v1') &&
                  s.path_state === 'agreed' ? (
                    <Compose deviceId={deviceId!} project={project!} />
                  ) : (
                    <p>
                      {s.path_state !== 'agreed'
                        ? 'Compose reads are blocked until paths are agreed.'
                        : 'Requires agent update'}
                    </p>
                  )}
                </div>
              )}
            </Tabs>
            <Modal
              open={!!action}
              onClose={() => {
                if (!pending) setAction('')
              }}
              title={`${action} stack`}
              footer={
                <>
                  <Button
                    variant="secondary"
                    disabled={pending}
                    onClick={() => setAction('')}
                  >
                    Cancel
                  </Button>
                  <Button
                    variant={
                      ['down', 'forget'].includes(action) ? 'danger' : 'primary'
                    }
                    disabled={pending || confirmation !== required || (action === 'update' && (!updateAllowed || tracker.isBusy || commandActive))}
                    onClick={() => {
                      void run()
                    }}
                  >
                    Confirm {action}
                  </Button>
                </>
              }
            >
              <p>
                {action} {s.project} on {s.hostname}?
              </p>
              <p className="font-mono text-xs my-3">
                {s.agreed_working_dir}
                <br />
                {s.agreed_config_files?.join(', ')}
              </p>
              {action === 'down' && (
                <p>
                  Stops and removes stack containers and networks. Volumes are
                  kept.
                </p>
              )}
              {action === 'redeploy' && <p>Recreates the stack services.</p>}
              {action === 'update' && <p>Pulls images and runs Compose up. Changed services may be recreated and stopped services may start.</p>}
              {self && (
                <p className="text-warn-fg">
                  This stack contains rIOt infrastructure. The connection may be
                  interrupted.
                </p>
              )}
              {required && (
                <label>
                  Type {required}
                  <input
                    className="block w-full p-2 bg-surface border border-border rounded"
                    aria-label="Stack confirmation"
                    value={confirmation}
                    onChange={(e) => setConfirmation(e.target.value)}
                  />
                </label>
              )}
              {error ? <ErrorPanel error={error} /> : null}
            </Modal>
          </div>
        )
      }}
    </QueryState>
  )
}

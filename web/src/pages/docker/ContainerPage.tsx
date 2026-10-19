import { Tabs } from '../../components/ui/Tabs'
import { DropdownMenu } from '../../components/ui/DropdownMenu'
import { containerPrimaryAction } from '../../utils/dockerActions'
import { safeUrl } from '../../utils/safeUrl'
import UpdateConfirmModal from '../../components/UpdateConfirmModal'
import { useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'
import { dockerApi } from '../../api/docker'
import {
  GeneralTab,
  NetworkTab,
  VolumesTab,
} from '../../components/ContainerDetail'
import {
  Button,
  EmptyState,
  ErrorPanel,
  Modal,
  QueryState,
} from '../../components/ui'
import ContainerStatusBadge from '../../components/ContainerStatusBadge'
import ContainerMetricChart from '../../components/ContainerMetricChart'
import AutoUpdateToggle from '../../components/docker/AutoUpdateToggle'
import Terminal from '../../components/Terminal'
import IOMetricChart from '../../components/docker/IOMetricChart'
import CommandProgress from '../../components/docker/CommandProgress'
import LogView from '../../components/docker/LogView'
import ActivityPage from './ActivityPage'
import { useFeatures } from '../../hooks/useFeatures'
import { useCommandTracker } from '../../hooks/useCommandTracker'
import {
  hasUpdateAvailable,
  targetKeyForContainer,
} from '../../utils/commandLifecycle'
import { displayName, formatBytes } from '../../utils/docker'
import { stackPath } from '../../utils/dockerUrlState'
import type { ContainerInfo } from '../../types/models'
function Inspect({ deviceId, name }: { deviceId: string; name: string }) {
  const [enabled, setEnabled] = useState(false)
  const query = useQuery({
    queryKey: ['docker-inspect', deviceId, name],
    queryFn: () => dockerApi.inspect(deviceId, name),
    enabled,
    gcTime: 0,
    staleTime: 0,
  })
  return (
    <div className="space-y-3">
      <p className="text-xs text-text-muted">
        Known secret patterns are masked. Review carefully before sharing.
      </p>
      <Button
        variant="secondary"
        onClick={() => {
          setEnabled(true)
          if (enabled) void query.refetch()
        }}
      >
        Fetch inspect
      </Button>
      {enabled && (
        <QueryState query={query}>
          {(d) => (
            <>
              <p className="text-xs text-text-muted">
                {d.redactions} values masked · fetched {d.fetched_at}
              </p>
              <pre className="overflow-auto rounded border border-border bg-surface p-3 text-xs max-h-[36rem]">
                {d.json}
              </pre>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => navigator.clipboard.writeText(d.json)}
              >
                Copy masked JSON
              </Button>
            </>
          )}
        </QueryState>
      )}
    </div>
  )
}
function Stats({
  deviceId,
  container: c,
}: {
  deviceId: string
  container: ContainerInfo
}) {
  const [hours, setHours] = useState(24)
  const query = useQuery({
    queryKey: ['container-metrics', deviceId, c.name, hours],
    queryFn: () => api.getContainerMetricHistory(deviceId, c.name, hours),
    refetchInterval: 60000,
  })
  return (
    <div className="space-y-4">
      <select
        aria-label="Stats range"
        className="bg-surface p-2 rounded"
        value={hours}
        onChange={(e) => setHours(Number(e.target.value))}
      >
        {[1, 6, 24, 168].map((h) => (
          <option key={h} value={h}>
            {h === 168 ? '7 days' : `${h} hours`}
          </option>
        ))}
      </select>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {[
          ['Network received', c.net_rx_rate],
          ['Network sent', c.net_tx_rate],
          ['Disk read', c.blk_read_rate],
          ['Disk write', c.blk_write_rate],
        ].map(([label, value]) => (
          <div key={label} className="rounded border border-border p-3">
            <p className="text-xs text-text-muted">{label}</p>
            <p>
              {typeof value === 'number'
                ? `${formatBytes(value)}/s`
                : 'Requires agent update'}
            </p>
          </div>
        ))}
      </div>
      <QueryState query={query}>
        {(metrics) => (
          <>
            <ContainerMetricChart
              metrics={metrics}
              mode="cpu"
              label="CPU usage"
              color="#60a5fa"
            />
            <ContainerMetricChart
              metrics={metrics}
              mode="memory"
              label="Memory usage"
              color="#34d399"
            />
            {(
              [
                ['net_rx_rate', 'Network received'],
                ['net_tx_rate', 'Network sent'],
                ['blk_read_rate', 'Disk read'],
                ['blk_write_rate', 'Disk write'],
              ] as const
            ).map(([field, label]) => (
              <IOMetricChart
                key={field}
                metrics={metrics}
                field={field}
                label={label}
              />
            ))}
          </>
        )}
      </QueryState>
    </div>
  )
}
export default function ContainerPage() {
  const { deviceId, name } = useParams()
  const [params, setParams] = useSearchParams()
  const { isEnabled } = useFeatures()
  const device = useQuery({
    queryKey: ['device', deviceId],
    queryFn: () => api.getDevice(deviceId!),
    refetchInterval: 15000,
  })
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
  })
  const last = useRef<ContainerInfo | null>(null)
  const found = device.data?.latest_telemetry?.data?.docker?.containers?.find(
    (c) => c.name.replace(/^\//, '') === name,
  )
  if (found) last.current = found
  const c = found ?? last.current
  const fleet = useQuery({
    queryKey: ['docker-containers'],
    queryFn: dockerApi.containers,
  })
  const host = hosts.data?.find((h) => h.device_id === deviceId)
  const tracker = useCommandTracker(
    deviceId,
    c ? targetKeyForContainer(c) : null,
  )
  const [action, setAction] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [actionError, setActionError] = useState<unknown>()
  const [pending, setPending] = useState(false)
  const tabs = [
    'overview',
    'logs',
    'activity',
    'inspect',
    ...(isEnabled('docker_terminal') ? ['terminal'] : []),
    'stats',
  ]
  const tab = tabs.includes(params.get('tab') ?? '')
    ? params.get('tab')!
    : 'overview'
  const caps = host?.capabilities ?? []
  if (!device.data) return <QueryState query={device}>{() => null}</QueryState>
  if (
    !c &&
    device.data.latest_telemetry?.data?.trimmed &&
    device.data.latest_telemetry.data.dropped_sections?.includes('docker')
  )
    return (
      <EmptyState
        title="Container data unavailable"
        description="Stored telemetry was downsampled. Wait for the next full Docker update."
      />
    )
  if (!c)
    return (
      <EmptyState title="Container not found">
        <Link to={`/docker/containers?host=${encodeURIComponent(deviceId!)}`}>
          Back to Containers
        </Link>
      </EmptyState>
    )
  const primary = containerPrimaryAction(c, tracker.isBusy)
  const self =
    !!fleet.data?.find(
      (r) =>
        r.device_id === deviceId &&
        r.container_name.replace(/^\//, '') === name,
    )?.self_role ||
    c.riot?.role === 'server' ||
    c.riot?.role === 'database'
  const required =
    self && (action === 'stop' || action === 'restart')
      ? device.data.device.hostname
      : ''
  const project = c.labels?.['com.docker.compose.project']
  async function run(actionToRun = action) {
    setPending(true)
    setActionError(undefined)
    try {
      await tracker.track(() =>
        api.sendCommand(
          deviceId!,
          `docker_${actionToRun}`,
          { container: c!.name.replace(/^\//, ''), container_id: c!.id },
          required ? confirmation : undefined,
        ),
      )
      setAction('')
    } catch (e) {
      setActionError(e)
    } finally {
      setPending(false)
    }
  }
  const choose = (a: string) => {
    setConfirmation('')
    setActionError(undefined)
    if (a === 'start') {
      void run('start')
    } else setAction(a)
  }
  return (
    <div className="space-y-5">
      <div className="flex gap-2 text-sm text-text-muted">
        <Link to={`/docker/containers?host=${encodeURIComponent(deviceId!)}`}>
          Containers
        </Link>
        <span>/</span>
        <Link to={`/devices/${encodeURIComponent(deviceId!)}`}>
          {device.data.device.hostname}
        </Link>
        <span>/</span>
        <span>{c.name}</span>
      </div>
      <header className="border border-border rounded-lg p-5 bg-surface flex flex-wrap justify-between gap-4">
        <div>
          <h2 className="text-2xl font-semibold">
            {c.riot?.icon} {displayName(c.riot, c.name)}
          </h2>
          <div className="flex gap-3 items-center text-xs mt-2">
            <ContainerStatusBadge state={c.state} />
            <span>{c.short_id}</span>
            <span>{c.image}</span>
            {self && <span className="text-warn-fg">rIOt infrastructure</span>}
          </div>
          {project && (
            <Link
              className="text-link text-sm"
              to={stackPath(deviceId!, project)}
            >
              {project}
            </Link>
          )}
        </div>
        <div className="flex gap-2 items-start">
          {primary && (
            <Button disabled={tracker.isBusy} onClick={() => choose(primary)}>
              {primary[0].toUpperCase() + primary.slice(1)}
            </Button>
          )}
          {tracker.isBusy && (
            <span role="status" className="text-sm text-info-fg">
              {tracker.label} {tracker.command?.step}
            </span>
          )}
          {c.state === 'running' && primary !== 'restart' && (
            <Button
              variant="secondary"
              disabled={tracker.isBusy}
              onClick={() => choose('restart')}
            >
              Restart
            </Button>
          )}
          {['running', 'exited', 'created', 'dead'].includes(c.state) && (
            <Button
              variant="secondary"
              disabled={tracker.isBusy}
              onClick={() => {
                void tracker.track(() =>
                  api.sendCommand(deviceId!, 'docker_check_updates', {}),
                )
              }}
            >
              Check updates
            </Button>
          )}
          <Button
            variant="ghost"
            onClick={() => {
              const p = new URLSearchParams(params)
              p.set('tab', 'logs')
              setParams(p)
            }}
          >
            Logs
          </Button>
          {isEnabled('docker_terminal') && c.state === 'running' && (
            <Button
              variant="ghost"
              onClick={() => {
                const p = new URLSearchParams(params)
                p.set('tab', 'terminal')
                setParams(p)
              }}
            >
              Terminal
            </Button>
          )}
          <DropdownMenu
            items={[
              {
                label: 'Copy container ID',
                onSelect: () => {
                  void navigator.clipboard.writeText(c.id)
                },
              },
              {
                label: 'Copy image reference',
                onSelect: () => {
                  void navigator.clipboard.writeText(c.image)
                },
              },
              ...(safeUrl(c.repo_url)
                ? [
                    {
                      label: 'Open source repository',
                      onSelect: () => {
                        window.open(
                          safeUrl(c.repo_url),
                          '_blank',
                          'noopener,noreferrer',
                        )
                      },
                    },
                  ]
                : []),
              {
                label: 'Auto-update settings',
                onSelect: () => {
                  const p = new URLSearchParams(params)
                  p.set('tab', 'overview')
                  setParams(p)
                },
              },
              ...(['running', 'paused', 'restarting'].includes(c.state)
                ? [
                    {
                      label: 'Stop',
                      danger: true,
                      disabled: tracker.isBusy,
                      onSelect: () => choose('stop'),
                    },
                  ]
                : []),
            ]}
          />{' '}
        </div>
      </header>
      {tracker.command && (
        <CommandProgress deviceId={deviceId!} commandId={tracker.command.id} />
      )}{' '}
      {tracker.error && <ErrorPanel error={tracker.error} />}
      {device.data.latest_telemetry?.data?.trimmed && (
        <p role="status" className="text-warn-fg text-sm">
          Stored telemetry is downsampled. Last known Docker data may be shown.
        </p>
      )}
      <Tabs
        label="Container details"
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
        {tab === 'overview' && (
          <>
            <AutoUpdateToggle
              deviceId={deviceId!}
              target={c.name.replace(/^\//, '')}
              workingDir={
                c.labels?.['com.docker.compose.project.working_dir'] ?? ''
              }
            />
            <GeneralTab container={c} deviceId={deviceId} hideMetricHistory />
            <NetworkTab container={c} />
            <VolumesTab container={c} />
          </>
        )}
        {tab === 'logs' && (
          <LogView
            deviceId={deviceId!}
            name={c.name.replace(/^\//, '')}
            liveSupported={caps.includes('logs_live_v1')}
            hostname={device.data.device.hostname}
          />
        )}{' '}
        {tab === 'activity' && (
          <ActivityPage
            deviceId={deviceId}
            container={c.name.replace(/^\//, '')}
          />
        )}{' '}
        {tab === 'inspect' &&
          (caps.includes('inspect_v1') ? (
            <Inspect deviceId={deviceId!} name={c.name.replace(/^\//, '')} />
          ) : (
            <p>Requires agent update</p>
          ))}
        {tab === 'terminal' &&
          (c.state === 'running' ? (
            <div data-palette-exempt className="h-96">
              <Terminal deviceId={deviceId!} containerId={c.id} />
            </div>
          ) : (
            <p>Container must be running to open a terminal.</p>
          ))}
        {tab === 'stats' && <Stats deviceId={deviceId!} container={c} />}
      </Tabs>
      {action === 'update' && (
        <UpdateConfirmModal
          title={`Update ${c.name}`}
          containers={[c]}
          networkOrder={{
            parent: c.name,
            dependents: (
              device.data.latest_telemetry?.data?.docker?.containers ?? []
            )
              .filter(
                (member) =>
                  member.network_mode === `container:${c.name}` ||
                  member.network_mode === `container:${c.id}` ||
                  member.network_mode === `service:${c.name}`,
              )
              .map((member) => member.name),
          }}
          onCancel={() => setAction('')}
          onConfirm={() => {
            setAction('')
            void run('update')
          }}
        />
      )}
      <Modal
        open={!!action && action !== 'update'}
        pending={pending}
        onClose={() => {
          if (!pending) setAction('')
        }}
        title={`${action} container`}
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
              variant={action === 'stop' ? 'danger' : 'primary'}
              disabled={pending || confirmation !== required}
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
          {action} {c.name} on {device.data.device.hostname}?
        </p>
        {required && (
          <>
            <p className="text-warn-fg">
              The dashboard may become unavailable, and rIOt may not be able to
              reverse this action.
            </p>
            <label>
              Type {required}
              <input
                aria-label="Confirm host"
                value={confirmation}
                onChange={(e) => setConfirmation(e.target.value)}
                className="block bg-surface border border-border rounded p-2 w-full"
              />
            </label>
          </>
        )}
        {actionError ? <ErrorPanel error={actionError} /> : null}
      </Modal>
    </div>
  )
}

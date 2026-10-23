import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Link,
  useLocation,
  useNavigate,
  useSearchParams,
} from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { api } from '../../api/client'
import {
  Button,
  EmptyState,
  ErrorPanel,
  Modal,
  PageHeader,
  QueryState,
  Timestamp,
  Popover,
} from '../../components/ui'
import MultiSelectFilter from '../../components/docker/MultiSelectFilter'
import TilesView from '../../components/docker/TilesView'
import DataTable, { type Column } from '../../components/docker/DataTable'
import CommandProgress from '../../components/docker/CommandProgress'
import AutoUpdateSwitch from '../../components/docker/AutoUpdateSwitch'
import type { DockerRow, BulkResult } from '../../types/docker'
import {
  applicable,
  canonicalDockerSearch,
  containerPath,
  filterDockerRows,
  parseDockerState,
  rowKey,
  stackPath,
} from '../../utils/dockerUrlState'
import { FRESHNESS_LABEL, freshnessOf } from '../../utils/dockerFreshness'
import {
  DEFAULT_DOCKER_COLUMNS,
  loadDockerColumns,
  saveDockerColumns,
} from '../../utils/dockerColumns'
import { autoUpdateKey, useAutoUpdateSave } from '../../hooks/useAutoUpdateSave'
import { useDockerUpdateMode } from '../../hooks/useDockerUpdateMode'
import { formatBytes } from '../../utils/docker'
import { publishedPortUrl } from '../../utils/safeUrl'
import { formatDuration } from '../../utils/format'
import { hasUpdateAvailable } from '../../utils/commandLifecycle'
import { useTimePreference } from '../../contexts/TimePreferenceProvider'
// The stack-name rule of the Stacks list; other project labels are named
// without a link.
const stackName = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/
export default function ContainersPage() {
  const { now } = useTimePreference()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const location = useLocation()
  const collectionState = {
    dockerCollection: location.pathname + location.search,
  }
  const [preferredView] = useState(() => {
    try {
      return localStorage.getItem('riot-docker-view') ?? 'table'
    } catch {
      return 'table'
    }
  })
  const [visibleColumns, setVisibleColumns] = useState<string[]>(
    loadDockerColumns,
  )
  const chooseColumns = (keys: string[]) => {
    setVisibleColumns(keys)
    saveDockerColumns(keys)
  }
  const state = parseDockerState(params.toString(), preferredView)
  const autoUpdate = useAutoUpdateSave()
  const windowDisabled = useDockerUpdateMode() === 'disabled'
  const [search, setSearch] = useState(state.q)
  const [selected, setSelected] = useState<Set<string>>(
    new Set(state.selection),
  )
  const [action, setAction] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [result, setResult] = useState<BulkResult>()
  const [error, setError] = useState<unknown>()
  const [busy, setBusy] = useState(false)
  const [frozen, setFrozen] = useState(false)
  const frozenRows = useRef<DockerRow[]>([])
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
  })
  const fleet = useQuery({
    queryKey: ['docker-containers'],
    queryFn: dockerApi.containers,
    refetchInterval: 15000,
  })
  const write = (key: string, value: string, replace = false) => {
    const p = new URLSearchParams(params)
    if (value) p.set(key, value)
    else p.delete(key)
    setParams(p, { replace })
  }
  useEffect(() => {
    // Old links used the `updates` status chip; they open with the Out of
    // date filter. Only the URL changes: opening a link never saves anything.
    const next = canonicalDockerSearch(params.toString())
    if (next !== null) setParams(new URLSearchParams(next), { replace: true })
  }, [params])
  useEffect(() => {
    setSearch(state.q)
  }, [state.q])
  useEffect(() => {
    const timer = setTimeout(() => {
      if (search !== state.q) write('q', search.trim().slice(0, 200))
    }, 300)
    return () => clearTimeout(timer)
  }, [search, state.q])
  const filtered = useMemo(
    () => filterDockerRows(fleet.data ?? [], state),
    [fleet.data, params.toString()],
  )
  const sorted = useMemo(() => {
    const value = (r: DockerRow): string | number =>
      ({
        status: r.state,
        name: r.container_name,
        host: r.hostname,
        stack: r.stack,
        image: r.image,
        cpu: r.cpu_percent,
        mem: r.mem_usage,
        uptime: r.started_at ?? '',
      })[state.sort] ?? ''
    return [...filtered].sort((a, b) => {
      const x = value(a),
        y = value(b)
      return (
        (typeof x === 'number' && typeof y === 'number'
          ? x - y
          : String(x).localeCompare(String(y))) *
          (state.direction === 'desc' ? -1 : 1) ||
        (state.sort === 'stack'
          ? a.hostname.localeCompare(b.hostname) ||
            a.container_name.localeCompare(b.container_name)
          : 0) ||
        rowKey(a).localeCompare(rowKey(b))
      )
    })
  }, [filtered, state.sort, state.direction])
  const current = new Map(filtered.map((r) => [rowKey(r), r]))
  const rows =
    frozen && ['cpu', 'mem', 'uptime'].includes(state.sort)
      ? [
          ...frozenRows.current
            .map((r) => current.get(rowKey(r)))
            .filter((r): r is DockerRow => !!r),
          ...sorted.filter(
            (r) => !frozenRows.current.some((f) => rowKey(f) === rowKey(r)),
          ),
        ]
      : sorted
  if (!frozen) frozenRows.current = sorted
  useEffect(() => {
    if (!fleet.data) return
    const visible = new Set(filtered.map(rowKey))
    setSelected(
      (old) =>
        new Set(
          [...(params.get('sel') ? state.selection : old)].filter((k) =>
            visible.has(k),
          ),
        ),
    )
  }, [filtered, fleet.data, params.get('sel')])
  const selectionAnchor = useRef<string | undefined>(undefined)
  const saveSelection = (next: Set<string>) => {
    setSelected(next)
    const encoded = [...next].join(',')
    write(
      'sel',
      next.size <= 200 && encodeURIComponent(encoded).length <= 4096
        ? encoded
        : '',
      true,
    )
  }
  const select = (r: DockerRow, range = false) => {
    const next = new Set(selected)
    const anchor = rows.findIndex(
      (row) => rowKey(row) === selectionAnchor.current,
    )
    const index = rows.findIndex((row) => rowKey(row) === rowKey(r))
    const targets =
      range && anchor >= 0
        ? rows.slice(Math.min(anchor, index), Math.max(anchor, index) + 1)
        : [r]
    const remove = selected.has(rowKey(r))
    for (const row of targets)
      remove ? next.delete(rowKey(row)) : next.add(rowKey(row))
    selectionAnchor.current = rowKey(r)
    saveSelection(next)
  }
  const targets = rows.filter((r) => selected.has(rowKey(r)))
  const eligible = targets.filter((r) => applicable(action, r))
  const selfHosts = [
    ...new Set(
      eligible
        .filter(
          (r) => r.self_role && (action === 'stop' || action === 'restart'),
        )
        .map((r) => r.hostname),
    ),
  ]
  const required = selfHosts.length
    ? selfHosts.join(',')
    : action === 'stop'
      ? String(eligible.length)
      : ''
  async function execute() {
    setBusy(true)
    setError(undefined)
    try {
      const groups =
        selfHosts.length > 1
          ? [...new Set(eligible.map((r) => r.device_id))].map((id) =>
              eligible.filter((r) => r.device_id === id),
            )
          : [eligible]
      const response: BulkResult = { results: [] }
      for (const group of groups) {
        const result = await dockerApi.bulk({
          action,
          targets: group.map((r) => ({
            device_id: r.device_id,
            container: r.container_name,
          })),
          ...(action === 'stop' ? { confirm_count: group.length } : {}),
          ...(selfHosts.length
            ? {
                confirm_host:
                  selfHosts.length > 1 ? group[0].hostname : confirmation,
              }
            : {}),
        })
        response.results.push(...result.results)
      }
      setResult(response)
      setAction('')
    } catch (e) {
      setError(e)
    } finally {
      setBusy(false)
    }
  }
  const columns: Column<DockerRow>[] = [
    {
      key: 'status',
      title: 'Status',
      render: (r) => (
        <span
          className={
            r.health_status === 'unhealthy'
              ? 'rounded bg-danger/10 px-2 py-1 text-xs text-danger-fg'
              : r.state === 'running'
                ? 'rounded bg-success/10 px-2 py-1 text-xs text-success-fg'
                : 'rounded bg-raised-2 px-2 py-1 text-xs text-text-muted'
          }
        >
          {r.health_status === 'unhealthy'
            ? 'Unhealthy'
            : ({
                running: 'Running',
                exited: 'Stopped',
                created: 'Created',
                dead: 'Dead',
                paused: 'Paused',
                restarting: 'Restarting',
                removing: 'Removing',
              }[r.state] ?? r.state)}
        </span>
      ),
    },
    {
      key: 'name',
      title: 'Name',
      render: (r) => (
        <div className="flex min-w-0 items-center gap-2">
          <Link
            className="min-w-0 truncate font-medium text-text hover:text-link"
            title={`${r.container_name} · ${r.image}`}
            to={containerPath(r.device_id, r.container_name)}
            state={collectionState}
          >
            {r.riot_icon} {r.riot_name || r.container_name}
            {r.self_role ? ` · rIOt ${r.self_role}` : ''}
          </Link>
          {hasUpdateAvailable(r) && (
            <span
              aria-label="Update available"
              title="Update available"
              className="shrink-0 rounded bg-warn/15 px-1.5 py-0.5 text-xs text-warn-fg"
            >
              Update
            </span>
          )}
        </div>
      ),
    },
    {
      key: 'freshness',
      title: 'Freshness',
      sortable: false,
      render: (r) => {
        const freshness = freshnessOf(r)
        return (
          <span
            className={
              freshness === 'out_of_date'
                ? 'rounded bg-warn/15 px-1.5 py-0.5 text-xs text-warn-fg'
                : 'rounded bg-raised-2 px-1.5 py-0.5 text-xs text-text-muted'
            }
          >
            {FRESHNESS_LABEL[freshness]}
          </span>
        )
      },
    },
    {
      key: 'auto',
      title: 'Auto-update',
      sortable: false,
      render: (r) => {
        const policy = r.auto_update_policy
        const label = `Auto-update ${r.container_name} on ${r.hostname}`
        if (!policy || !policy.target)
          return (
            <AutoUpdateSwitch
              checked={false}
              label={label}
              disabledReason={
                policy ? 'Name too long to target' : 'Auto-update state unavailable'
              }
              onChange={() => {}}
            />
          )
        if (policy.covered_by)
          return (
            <AutoUpdateSwitch
              checked
              label={label}
              lockedBy={{
                project: policy.covered_by,
                to: stackName.test(policy.covered_by)
                  ? stackPath(r.device_id, policy.covered_by)
                  : undefined,
              }}
              windowDisabled={windowDisabled}
              onChange={() => {}}
            />
          )
        const { checked, pending } = autoUpdate.display(
          autoUpdateKey(r.device_id, false, policy.target),
          policy.enabled,
        )
        return (
          <AutoUpdateSwitch
            checked={checked}
            pending={pending}
            label={label}
            windowDisabled={windowDisabled}
            onChange={(enabled) =>
              void autoUpdate.save({
                deviceId: r.device_id,
                hostname: r.hostname,
                target: policy.target,
                stack: false,
                workingDir: '',
                enabled,
              })
            }
          />
        )
      },
    },
    {
      key: 'stack',
      title: 'Stack',
      render: (r) =>
        r.stack ? (
          <Link className="text-link" to={stackPath(r.device_id, r.stack)}>
            {r.stack}
          </Link>
        ) : (
          'Standalone'
        ),
    },
    {
      key: 'host',
      title: 'Host',
      render: (r) => (
        <Link to={`/docker/containers?host=${encodeURIComponent(r.device_id)}`}>
          {r.hostname}
        </Link>
      ),
    },
    {
      key: 'image',
      title: 'Image',
      render: (r) => (
        <span title={r.image}>
          {r.image}
        </span>
      ),
    },
    {
      key: 'ports',
      title: 'Ports',
      sortable: false,
      render: (r) =>
        r.ports?.map((p, i) => {
          const address =
            p.host_ip && !['0.0.0.0', '::'].includes(p.host_ip)
              ? p.host_ip
              : hosts.data?.find((h) => h.device_id === r.device_id)?.primary_ip
          const url = publishedPortUrl(
            p.host_ip,
            hosts.data?.find((h) => h.device_id === r.device_id)?.primary_ip,
            p.host_port,
            p.container_port,
            p.protocol,
          )
          const text = `${p.host_port || '—'}:${p.container_port}/${p.protocol}`
          return url ? (
            <a
              key={i}
              className="text-link mr-2"
              href={url}
              title={`Open ${address}`}
              target="_blank"
              rel="noopener noreferrer"
            >
              {text}
            </a>
          ) : (
            <span key={i} className="mr-2">
              {text}
            </span>
          )
        }) || '—',
    },
    {
      key: 'cpu',
      title: 'CPU',
      render: (r) =>
        r.state === 'running' ? (
          <div>
            <span>{r.cpu_percent.toFixed(1)}%</span>
            <div aria-hidden="true" className="mt-1 h-0.5 rounded bg-border">
              <div
                className="h-full rounded bg-info"
                style={{
                  width: `${Math.min(100, Math.max(0, r.cpu_percent))}%`,
                }}
              />
            </div>
          </div>
        ) : (
          '—'
        ),
    },
    {
      key: 'mem',
      title: 'Memory',
      render: (r) =>
        r.state === 'running' ? (
          <div
            title={`${formatBytes(r.mem_usage)} used · ${r.mem_limit_configured === false ? 'no configured limit' : `${formatBytes(r.mem_limit)} limit`}`}
          >
            <span>
              {formatBytes(r.mem_usage)}{' '}
              <span className="text-text-muted">
                /{' '}
                {r.mem_limit_configured === false
                  ? '∞'
                  : formatBytes(r.mem_limit)}
              </span>
            </span>
            {r.mem_limit > 0 && r.mem_limit_configured !== false && (
              <div aria-hidden="true" className="mt-1 h-0.5 rounded bg-border">
                <div
                  className={`h-full rounded ${r.mem_usage / r.mem_limit > 0.9 ? 'bg-warn' : 'bg-success'}`}
                  style={{
                    width: `${Math.min(100, Math.max(0, (r.mem_usage / r.mem_limit) * 100))}%`,
                  }}
                />
              </div>
            )}
          </div>
        ) : (
          '—'
        ),
    },
    {
      key: 'links',
      title: 'Open',
      sortable: false,
      render: (r) => (
        <Link
          state={collectionState}
          className="text-xs text-link hover:underline"
          aria-label={`Logs for ${r.container_name}`}
          to={`${containerPath(r.device_id, r.container_name)}?tab=logs`}
        >
          Logs
        </Link>
      ),
    },
    {
      key: 'uptime',
      title: 'Uptime / exit',
      render: (r) =>
        r.state === 'running'
          ? r.started_at
            ? `Up ${formatDuration(Math.max(0, (now() - Date.parse(r.started_at)) / 1000))}`
            : 'Requires agent update'
          : r.state === 'exited' || r.state === 'dead'
            ? r.finished_at
              ? `Exited (${r.exit_code ?? '?'}) ${formatDuration(Math.max(0, (now() - Date.parse(r.finished_at)) / 1000))} ago`
              : (r.status ?? 'Requires agent update')
            : r.state === 'created'
              ? 'Created'
              : r.state === 'paused'
                ? 'Paused'
                : r.state === 'restarting'
                  ? 'Restarting'
                  : (r.status ?? '—'),
    },
  ]
  const widths: Record<string, number> = {
    name: 184,
    status: 100,
    host: 104,
    stack: 112,
    freshness: 112,
    auto: 224,
    image: 240,
    ports: 208,
    cpu: 160,
    mem: 160,
    uptime: 200,
    links: 64,
  }
  const orderedColumns = [
    'name',
    'host',
    'stack',
    'status',
    'freshness',
    'auto',
    'image',
    'cpu',
    'mem',
    'ports',
    'uptime',
    'links',
  ].map((key) => {
    const column = columns.find((c) => c.key === key)!
    return {
      ...column,
      width: widths[key],
      grow: key === 'name',
      pinned: key === 'name',
      align: key === 'cpu' || key === 'mem' ? ('right' as const) : undefined,
    }
  })
  return (
    <div className="min-w-0 space-y-4">
      <PageHeader
        title="Containers"
        description="Runtime state, resource usage and logs across your hosts."
        actions={
          <span className="text-xs tabular-nums text-text-muted">
            {filtered.length} of {fleet.data?.length ?? 0} containers
          </span>
        }
      />
      <div className="flex flex-wrap gap-2 items-center">
        <input
          aria-label="Search containers"
          placeholder="Search name, image, host or tag…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="rounded border border-border bg-surface px-3 py-2 text-sm flex-1 min-w-48"
        />
        <MultiSelectFilter
          label="Hosts"
          selected={state.host}
          onChange={(values) => write('host', values.join(','))}
          options={(hosts.data ?? [])
            .filter((h) => h.docker_available === true)
            .map((h) => ({
              value: h.device_id,
              label: h.hostname,
            }))}
        />
        <MultiSelectFilter
          label="Stacks"
          selected={state.stack}
          onChange={(values) => write('stack', values.join(','))}
          options={[
            ...new Map(
              (fleet.data ?? [])
                .filter((r) => r.stack)
                .map((r) => [
                  `${r.device_id}:${r.stack}`,
                  {
                    value: `${r.device_id}:${r.stack}`,
                    label: `${r.stack} · ${r.hostname}`,
                  },
                ]),
            ).values(),
          ]}
        />
        <Button
          variant="secondary"
          onClick={() => {
            const view = state.view === 'table' ? 'tiles' : 'table'
            localStorage.setItem('riot-docker-view', view)
            write('view', view)
          }}
        >
          {state.view === 'table' ? 'Tiles' : 'Table'}
        </Button>
        {state.view === 'table' && (
          <Popover label="Columns">
            <p className="mb-2 text-xs text-text-muted">
              Choose what to show. Name stays pinned.
            </p>
            {orderedColumns.map((column) => (
              <label
                key={column.key}
                className="flex items-center gap-2 py-1 text-sm"
              >
                <input
                  type="checkbox"
                  checked={visibleColumns.includes(column.key)}
                  disabled={column.key === 'name'}
                  onChange={() =>
                    chooseColumns(
                      visibleColumns.includes(column.key)
                        ? visibleColumns.filter((key) => key !== column.key)
                        : [...visibleColumns, column.key],
                    )
                  }
                />
                {column.title}
              </label>
            ))}
            <Button
              size="sm"
              variant="ghost"
              className="mt-2"
              onClick={() => chooseColumns(DEFAULT_DOCKER_COLUMNS)}
            >
              Reset columns
            </Button>
          </Popover>
        )}
        <Button
          variant="secondary"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            try {
              await Promise.all(
                (hosts.data ?? [])
                  .filter((h) => h.docker_available === true)
                  .filter(
                    (h) =>
                      !state.host.length || state.host.includes(h.device_id),
                  )
                  .map((h) =>
                    api.sendCommand(h.device_id, 'docker_check_updates', {}),
                  ),
              )
            } catch (e) {
              setError(e)
            } finally {
              setBusy(false)
            }
          }}
        >
          Check for Updates
        </Button>
      </div>
      <div className="flex flex-wrap gap-2">
        {['running', 'stopped', 'unhealthy'].map((s) => (
          <Button
            key={s}
            size="sm"
            variant={state.status.includes(s) ? 'primary' : 'secondary'}
            onClick={() =>
              write(
                'status',
                state.status.includes(s)
                  ? state.status.filter((x) => x !== s).join(',')
                  : [...state.status, s].join(','),
              )
            }
          >
            {s} (
            {
              filterDockerRows(fleet.data ?? [], { ...state, status: [s] })
                .length
            }
            )
          </Button>
        ))}
        {(
          [
            ['outdated', '1', 'Out of date', 'outOfDate'],
            ['autoupdate', 'off', 'Auto-update off', 'autoOff'],
          ] as const
        ).map(([key, value, title, field]) => (
          <Button
            key={key}
            size="sm"
            variant={state[field] ? 'primary' : 'secondary'}
            aria-pressed={state[field]}
            onClick={() => write(key, state[field] ? '' : value)}
          >
            {title} (
            {
              filterDockerRows(fleet.data ?? [], { ...state, [field]: true })
                .length
            }
            )
          </Button>
        ))}
        {state.q ||
        state.host.length ||
        state.status.length ||
        state.stack.length ||
        state.outOfDate ||
        state.autoOff ? (
          <Button size="sm" variant="ghost" onClick={() => setParams({})}>
            Clear filters
          </Button>
        ) : null}
      </div>
      {error ? (
        <ErrorPanel
          error={error}
          onRetry={() => {
            void fleet.refetch()
          }}
        />
      ) : null}
      {state.view === 'table' && selected.size > 0 && (
        <div className="sticky top-0 z-20 flex items-center gap-3 rounded border border-border bg-raised-2 p-3">
          <span>{selected.size} selected</span>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              setSelected(new Set())
              write('sel', '', true)
            }}
          >
            Clear
          </Button>
          {['start', 'stop', 'restart', 'update'].map((a) => (
            <Button
              key={a}
              size="sm"
              variant={a === 'stop' ? 'danger' : 'secondary'}
              disabled={
                selected.size > 100 || !targets.some((r) => applicable(a, r))
              }
              onClick={() => {
                setConfirmation('')
                setError(undefined)
                setAction(a)
              }}
            >
              {a}
            </Button>
          ))}
          {selected.size > 100 && (
            <span>Select up to 100 targets per action.</span>
          )}
        </div>
      )}
      <QueryState
        query={fleet}
        isEmpty={() => !rows.length}
        empty={
          <EmptyState
            title="No matching containers"
            description="Change the filters to see more containers."
          />
        }
      >
        {() => (
          <div
            onMouseEnter={() => setFrozen(true)}
            onMouseLeave={() => setFrozen(false)}
            onFocus={() => setFrozen(true)}
            onBlur={(e) => {
              if (!e.currentTarget.contains(e.relatedTarget)) setFrozen(false)
            }}
          >
            {frozen && ['cpu', 'mem', 'uptime'].includes(state.sort) && (
              <p className="text-xs text-text-muted">
                Order paused while interacting
              </p>
            )}
            {state.view === 'tiles' ? (
              <TilesView rows={rows} />
            ) : (
              <DataTable
                rows={rows}
                columns={orderedColumns.filter((column) =>
                  visibleColumns.includes(column.key),
                )}
                sort={{ key: state.sort, direction: state.direction }}
                rowKey={rowKey}
                selection={selected}
                onSelect={select}
                allSelected={
                  rows.length > 0 &&
                  rows.every((row) => selected.has(rowKey(row)))
                }
                onSelectAll={() =>
                  saveSelection(
                    rows.length > 0 &&
                      rows.every((row) => selected.has(rowKey(row)))
                      ? new Set()
                      : new Set(rows.map(rowKey)),
                  )
                }
                onActivate={(r) =>
                  navigate(containerPath(r.device_id, r.container_name), {
                    state: collectionState,
                  })
                }
                onSort={(key) => {
                  if (key !== 'ports')
                    write(
                      'sort',
                      `${key}:${state.sort === key && state.direction === 'asc' ? 'desc' : 'asc'}`,
                    )
                }}
              />
            )}
          </div>
        )}
      </QueryState>
      <p className="text-xs text-text-muted" role="status">
        Showing {rows.length} of {fleet.data?.length ?? 0} containers ·{' '}
        {selected.size} selected
      </p>
      {result && (
        <section aria-label="Bulk action results" className="space-y-2">
          <h3>Action results</h3>
          {result.results.map((r) => (
            <div
              key={r.device_id + r.container}
              className="p-2 border border-border rounded"
            >
              <p>
                {r.container}: {r.outcome} {r.reason}
              </p>
              {(r.command_id || r.existing_command_id) && (
                <CommandProgress
                  deviceId={r.device_id}
                  commandId={(r.command_id || r.existing_command_id)!}
                />
              )}
            </div>
          ))}
        </section>
      )}
      <Modal
        open={!!action}
        onClose={() => {
          if (!busy) setAction('')
        }}
        title={`${action} containers`}
        footer={
          <>
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => setAction('')}
            >
              Cancel
            </Button>
            <Button
              variant={action === 'stop' ? 'danger' : 'primary'}
              disabled={busy || !eligible.length || confirmation !== required}
              onClick={() => {
                void execute()
              }}
            >
              {busy ? 'Starting…' : `Confirm ${action}`}
            </Button>
          </>
        }
      >
        <p>
          {eligible.length} eligible, {targets.length - eligible.length}{' '}
          skipped. Offline hosts queue actions until connected.
        </p>
        <ul className="my-3 max-h-48 overflow-auto">
          {targets.map((r) => (
            <li key={rowKey(r)}>
              {r.hostname} / {r.container_name}
              {!applicable(action, r) ? ' — skipped (not applicable)' : ''}
            </li>
          ))}
        </ul>
        {selfHosts.length > 0 && (
          <p role="alert">
            This affects rIOt itself and may interrupt this connection.
          </p>
        )}
        {required && (
          <label>
            Type {required} to confirm
            <input
              aria-label="Confirmation"
              className="block bg-surface border border-border rounded p-2 w-full"
              value={confirmation}
              onChange={(e) => setConfirmation(e.target.value)}
            />
          </label>
        )}
        {error ? <ErrorPanel error={error} /> : null}
      </Modal>
    </div>
  )
}

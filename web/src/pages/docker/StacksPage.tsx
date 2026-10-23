import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import { Button, EmptyState, PageHeader, QueryState } from '../../components/ui'
import StackUpdateDialog from '../../components/docker/StackUpdateDialog'
import DataTable, { type Column } from '../../components/docker/DataTable'
import AutoUpdateSwitch from '../../components/docker/AutoUpdateSwitch'
import type { DockerHost, DockerStack } from '../../types/docker'
import {
  filterStackRows,
  parseStackState,
  stackPath,
} from '../../utils/dockerUrlState'
import { autoUpdateKey, useAutoUpdateSave } from '../../hooks/useAutoUpdateSave'
import { useDockerUpdateMode } from '../../hooks/useDockerUpdateMode'
// Why a stack's auto-update cannot be turned on from the list: the same rules
// as the stack page's actions. Turning off is always allowed.
function stackOnBlocked(
  s: DockerStack,
  hosts: { isSuccess: boolean; data?: DockerHost[] },
) {
  if (s.path_state !== 'agreed') return 'Compose paths not agreed'
  if (!hosts.isSuccess) return 'Checking host'
  const host = hosts.data?.find((h) => h.device_id === s.device_id)
  if (!host?.capabilities.includes('stack_actions_v1'))
    return 'Requires agent update for stack actions'
}
export default function StacksPage() {
  const location = useLocation()
  const [params, setParams] = useSearchParams()
  const query = useQuery({
    queryKey: ['docker-stacks'],
    queryFn: dockerApi.stacks,
    refetchInterval: 15000,
  })
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
  })
  const state = parseStackState(params.toString())
  const search = state.q
  const host = state.host
  const autoUpdate = useAutoUpdateSave()
  const windowDisabled = useDockerUpdateMode() === 'disabled'
  const [updating, setUpdating] = useState<{ deviceId: string; project: string }>()
  return (
    <div className="space-y-4">
      <PageHeader
        title="Stacks"
        description="Compose projects, services and combined logs across your hosts."
      />
      <div className="flex flex-wrap gap-3">
        <input
          aria-label="Search stacks"
          placeholder="Search stacks"
          value={search}
          onChange={(e) => {
            const p = new URLSearchParams(params)
            p.set('q', e.target.value)
            setParams(p, { replace: true })
          }}
          className="min-w-0 flex-1 bg-surface border border-border rounded p-2 text-sm"
        />
        <select
          aria-label="Stack host"
          value={host}
          onChange={(e) => {
            const p = new URLSearchParams(params)
            e.target.value ? p.set('host', e.target.value) : p.delete('host')
            setParams(p)
          }}
          className="bg-surface rounded p-2"
        >
          <option value="">All hosts</option>
          {hosts.data
            ?.filter((h) => h.docker_available === true)
            .map((h) => (
              <option key={h.device_id} value={h.device_id}>
                {h.hostname}
              </option>
            ))}
        </select>
      </div>
      <div className="flex flex-wrap gap-2">
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
            onClick={() => {
              const p = new URLSearchParams(params)
              state[field] ? p.delete(key) : p.set(key, value)
              setParams(p)
            }}
          >
            {title} (
            {filterStackRows(query.data ?? [], { ...state, [field]: true }).length})
          </Button>
        ))}
        {search || host || state.outOfDate || state.autoOff ? (
          <Button size="sm" variant="ghost" onClick={() => setParams({})}>
            Clear filters
          </Button>
        ) : null}
      </div>
      <QueryState
        query={query}
        isEmpty={(s) => !s.length}
        empty={
          <EmptyState
            title="No compose stacks"
            description="Stacks discovered from Docker compose labels appear here, including stopped stacks."
          />
        }
      >
        {(stacks) => (
          <DataTable
            rows={filterStackRows(stacks, state)}
            rowKey={(s) => s.device_id + s.project}
            columns={(
              [
                {
                  key: 'project',
                  title: 'Stack',
                  render: (s) => (
                    <Link
                      className="text-link font-medium"
                      to={stackPath(s.device_id, s.project)}
                      state={{
                        dockerCollection: location.pathname + location.search,
                      }}
                    >
                      {s.project}
                    </Link>
                  ),
                },
                { key: 'host', title: 'Host', render: (s) => s.hostname },
                {
                  key: 'actions',
                  title: 'Actions',
                  sortable: false,
                  render: (s) => (
                    <Button size="sm"
                      variant={s.updates > 0 ? 'ghost' : 'secondary'}
                      className={s.updates > 0 ? 'h-6 px-2! bg-warn! text-black! hover:bg-warn/85!' : 'h-6'}
                      title={s.updates > 0 ? `${s.updates} container update${s.updates === 1 ? '' : 's'} available` : undefined}
                      aria-label={`Update ${s.project} on ${s.hostname}`}
                      onClick={() => setUpdating({ deviceId: s.device_id, project: s.project })}>
                      {s.updates > 0 ? `Update (${s.updates})` : 'Update'}
                    </Button>
                  ),
                },
                { key: 'state', title: 'State', render: (s) => s.state },
                {
                  key: 'services',
                  title: 'Services',
                  render: (s) => `${s.running}/${s.total} running`,
                },
                {
                  key: 'updates', title: 'Updates',
                  render: (s) => s.updates > 0 ? (
                    <span className="rounded bg-warn/15 px-1.5 py-0.5 text-xs font-medium text-warn-fg">
                      {s.updates} {s.updates === 1 ? 'update' : 'updates'}
                    </span>
                  ) : s.updates,
                },
                {
                  key: 'auto',
                  title: 'Auto-update',
                  sortable: false,
                  render: (s) => {
                    const label = `Auto-update ${s.project} on ${s.hostname}`
                    if (s.auto_update_unavailable)
                      return (
                        <AutoUpdateSwitch
                          checked={false}
                          label={label}
                          disabledReason="Auto-update state unavailable"
                          onChange={() => {}}
                        />
                      )
                    const { checked, pending } = autoUpdate.display(
                      autoUpdateKey(s.device_id, true, s.project),
                      s.auto_update,
                    )
                    return (
                      <AutoUpdateSwitch
                        checked={checked}
                        pending={pending}
                        label={label}
                        onDisabledReason={stackOnBlocked(s, hosts)}
                        windowDisabled={windowDisabled}
                        onChange={(enabled) =>
                          void autoUpdate.save({
                            deviceId: s.device_id,
                            hostname: s.hostname,
                            target: s.project,
                            stack: true,
                            workingDir: s.agreed_working_dir,
                            enabled,
                          })
                        }
                      />
                    )
                  },
                },
                {
                  key: 'paths',
                  title: 'Compose paths',
                  render: (s) => s.path_state,
                },
                {
                  key: 'last',
                  title: 'Last seen',
                  render: (s) => s.last_seen_at,
                },
              ] satisfies Column<DockerStack>[]
            ).map((column) => ({
              ...column,
              width: (
                {
                  project: 220,
                  host: 144,
                  state: 112,
                  services: 128,
                  updates: 100,
                  auto: 200,
                  paths: 160,
                  actions: 112,
                  last: 208,
                } as Record<string, number>
              )[column.key],
              pinned: column.key === 'project',
            }))}
          />
        )}
      </QueryState>
      {updating && <StackUpdateDialog
        key={`${updating.deviceId}:${updating.project}`}
        deviceId={updating.deviceId}
        project={updating.project}
        onClose={() => setUpdating(undefined)}
      />}
    </div>
  )
}

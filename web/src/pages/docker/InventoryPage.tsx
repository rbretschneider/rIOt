import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../../api/docker'
import {
  Button,
  EmptyState,
  ErrorPanel,
  Modal,
  QueryState,
} from '../../components/ui'
import DataTable, { type Column } from '../../components/docker/DataTable'
import CommandProgress from '../../components/docker/CommandProgress'
import type { InventoryItem, PrunePreview } from '../../types/docker'
import { formatBytes } from '../../utils/docker'
import { blockingCommandId } from '../../utils/dockerActions'
const keyOf = (r: InventoryItem) => `${r.device_id}.${r.id || r.name}`
function unused(r: InventoryItem, kind: string) {
  if (r.in_use) return false
  return kind === 'images'
    ? Number(r.containers ?? 0) === 0
    : kind === 'volumes'
      ? !r.used_by?.length
      : !r.built_in && !r.builtin && !((r.containers as string[]) ?? []).length
}
export default function InventoryPage({
  kind,
}: {
  kind: 'images' | 'volumes' | 'networks'
}) {
  const [params, setParams] = useSearchParams()
  const query = useQuery({
    queryKey: ['docker-inventory', kind],
    queryFn: () => dockerApi.inventory(kind),
    refetchInterval: 30000,
  })
  const hosts = useQuery({
    queryKey: ['docker-hosts'],
    queryFn: dockerApi.hosts,
  })
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [preview, setPreview] = useState<PrunePreview>()
  const [previewHost, setPreviewHost] = useState('')
  const [checked, setChecked] = useState<Set<string>>(new Set())
  const [confirm, setConfirm] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const [command, setCommand] = useState<{ id: string; device: string }>()
  const q = params.get('q') ?? ''
  const host = params.get('host') ?? ''
  const status = params.get('status') ?? ''
  const title = kind[0].toUpperCase() + kind.slice(1)
  const write = (key: string, value: string) => {
    const p = new URLSearchParams(params)
    value ? p.set(key, value) : p.delete(key)
    setParams(p)
    if (key !== 'sort') setSelected(new Set())
  }
  const rows = (query.data?.items ?? []).filter(
    (r) =>
      (!host || r.device_id === host) &&
      (!q ||
        [r.name, r.id, ...(r.tags ?? [])]
          .join(' ')
          .toLowerCase()
          .includes(q.toLowerCase())) &&
      (!status || (status === 'unused' ? unused(r, kind) : !unused(r, kind))),
  )
  const [sortKey, sortDirection] = (params.get('sort') ?? 'name:asc').split(':')
  const value = (item: InventoryItem): string | number =>
    sortKey === 'size'
      ? (item.size ?? -1)
      : sortKey === 'host'
        ? (query.data?.hosts.find((host) => host.device_id === item.device_id)
            ?.hostname ?? '')
        : sortKey === 'usage'
          ? unused(item, kind)
            ? 0
            : 1
          : sortKey === 'driver'
            ? (item.driver ?? '')
            : sortKey === 'detail'
              ? (item.id ?? item.mountpoint ?? '')
              : item.tags?.join(', ') || item.name || item.id || ''
  rows.sort((left, right) => {
    const a = value(left),
      b = value(right)
    return (
      (typeof a === 'number' && typeof b === 'number'
        ? a - b
        : String(a).localeCompare(String(b))) *
      (sortDirection === 'desc' ? -1 : 1)
    )
  })
  const selectedRows = rows.filter((r) => selected.has(keyOf(r)))
  const targetHost =
    host || [...new Set(selectedRows.map((r) => r.device_id))][0] || ''
  const sameHost = !selectedRows.some((r) => r.device_id !== targetHost)
  const hostState = query.data?.hosts.find((h) => h.device_id === targetHost)
  const target = hosts.data?.find((h) => h.device_id === targetHost)
  const pruneAllowed =
    !!target?.capabilities?.includes('prune_v1') &&
    hostState?.state === 'ok' &&
    !hostState.stale
  async function loadPreview(kindRequest: string) {
    setPending(true)
    setError(undefined)
    try {
      const p = await dockerApi.preview(targetHost, {
        kind: kindRequest,
        ...(kindRequest === 'selected'
          ? {
              item_kind: kind.slice(0, -1),
              items: selectedRows.map((r) => r.id || r.name),
            }
          : {}),
      })
      setPreviewHost(targetHost)
      setPreview(p)
      setConfirm('')
      setChecked(
        new Set(
          p.items.filter((i) => i.selected_default).map((i) => i.id || i.name),
        ),
      )
    } catch (e) {
      setError(e)
    } finally {
      setPending(false)
    }
  }
  const columns: Column<InventoryItem>[] = [
    {
      key: 'name',
      title: kind === 'images' ? 'Image / tags' : 'Name',
      render: (r) => (
        <span title={r.id}>
          {kind === 'images'
            ? r.tags?.join(', ') || r.id?.slice(0, 19)
            : r.name}
          {r.built_in || r.builtin ? ' · built-in' : ''}
          {r.anonymous ? ' · anonymous' : ''}
        </span>
      ),
    },
    {
      key: 'host',
      title: 'Host',
      render: (r) =>
        query.data?.hosts.find((h) => h.device_id === r.device_id)?.hostname ||
        r.device_id,
    },
    {
      key: 'size',
      title: 'Size',
      render: (r) =>
        typeof r.size === 'number' ? formatBytes(r.size) : 'Unavailable',
    },
    {
      key: 'usage',
      title: 'Usage',
      render: (r) =>
        unused(r, kind)
          ? 'Unused'
          : kind === 'images'
            ? `${r.containers} container(s)`
            : (
                r.used_by ?? (Array.isArray(r.containers) ? r.containers : [])
              ).join(', ') || 'In use',
    },
    {
      key: 'driver',
      title: kind === 'images' ? 'Created' : 'Driver',
      render: (r) => (kind === 'images' ? r.created_at || r.created : r.driver),
    },
    {
      key: 'detail',
      title:
        kind === 'volumes'
          ? 'Project / mountpoint'
          : kind === 'networks'
            ? 'Scope / subnet'
            : 'ID',
      render: (r) =>
        kind === 'volumes'
          ? `${r.project ?? ''} ${r.mountpoint ?? ''}`
          : kind === 'networks'
            ? `${r.scope ?? ''} ${(r.subnets ?? []).map((s) => s.subnet).join(', ')}`
            : r.id,
    },
  ]
  const required =
    preview?.confirmation.kind === 'host'
      ? (query.data?.hosts.find((h) => h.device_id === previewHost)?.hostname ??
        '')
      : preview?.confirmation.kind === 'count'
        ? String(checked.size)
        : ''
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">{title}</h2>
      <div className="flex flex-wrap gap-3">
        <input
          aria-label={`Search ${kind}`}
          placeholder={`Search ${kind}`}
          className="bg-surface border border-border rounded p-2"
          value={q}
          onChange={(e) => write('q', e.target.value)}
        />
        <select
          aria-label="Inventory host"
          value={host}
          onChange={(e) => write('host', e.target.value)}
          className="bg-surface rounded p-2"
        >
          <option value="">All hosts</option>
          {query.data?.hosts.map((h) => (
            <option key={h.device_id} value={h.device_id}>
              {h.hostname}
            </option>
          ))}
        </select>
        <select
          aria-label="Inventory usage"
          value={status}
          onChange={(e) => write('status', e.target.value)}
          className="bg-surface rounded p-2"
        >
          <option value="">All resources</option>
          <option value="unused">Unused</option>
          <option value="used">In use</option>
        </select>
        <Button
          variant="secondary"
          disabled={
            !targetHost ||
            pending ||
            !target?.capabilities?.includes('inventory_v1')
          }
          onClick={async () => {
            setPending(true)
            try {
              const c = await dockerApi.refresh(targetHost)
              setCommand({ id: c.id, device: targetHost })
              void query.refetch()
            } catch (e) {
              setError(e)
            } finally {
              setPending(false)
            }
          }}
        >
          Refresh inventory
        </Button>
      </div>
      <QueryState query={query}>
        {(data) => (
          <>
            <div className="grid md:grid-cols-3 gap-3">
              {data.hosts
                .filter((h) => !host || h.device_id === host)
                .map((h) => (
                  <section
                    key={h.device_id}
                    className="rounded border border-border p-3 text-sm"
                  >
                    <strong>{h.hostname}</strong>
                    <p className="text-text-muted">
                      {h.state === 'not_enabled'
                        ? 'Enable the docker_inventory collector on this host'
                        : h.state === 'unsupported'
                          ? 'Requires agent update'
                          : h.state === 'never_collected'
                            ? 'Inventory has not been collected yet'
                            : h.stale
                              ? 'Inventory is stale. Refresh before cleanup.'
                              : `Collected ${h.collected_at ?? 'recently'}`}
                    </p>
                    {!!h.truncated &&
                      (h.truncated === true ||
                        (typeof h.truncated === 'object' &&
                          Object.values(h.truncated).some(Boolean))) && (
                        <p>Inventory is truncated. Some items are not shown.</p>
                      )}
                    {h.disk_usage &&
                      Object.entries(h.disk_usage).map(([label, size]) => (
                        <p key={label}>
                          {label}: {formatBytes(size.total)} · reclaimable{' '}
                          {formatBytes(size.reclaimable)}
                        </p>
                      ))}
                  </section>
                ))}
            </div>
            {rows.length ? (
              <DataTable
                rows={rows}
                columns={columns}
                sort={{ key: sortKey, direction: sortDirection }}
                onSort={(key) =>
                  write(
                    'sort',
                    `${key}:${sortKey === key && sortDirection === 'asc' ? 'desc' : 'asc'}`,
                  )
                }
                rowKey={keyOf}
                selection={selected}
                onSelect={(r) => {
                  const next = new Set(selected)
                  next.has(keyOf(r))
                    ? next.delete(keyOf(r))
                    : next.add(keyOf(r))
                  setSelected(next)
                }}
              />
            ) : (
              <EmptyState
                title={`No ${kind} match`}
                description="Choose another host or change the filters."
              />
            )}
          </>
        )}
      </QueryState>
      <div className="flex flex-wrap items-center gap-3">
        <span>{selectedRows.length} selected</span>
        <Button
          variant="danger"
          disabled={
            !selectedRows.length || !sameHost || !pruneAllowed || pending
          }
          onClick={() => {
            void loadPreview('selected')
          }}
        >
          Preview removal
        </Button>
        <Button
          variant="secondary"
          disabled={!host || !pruneAllowed || pending}
          onClick={() => {
            void loadPreview(kind)
          }}
        >
          Preview unused cleanup
        </Button>
        {kind === 'images' && (
          <Button
            variant="secondary"
            disabled={!host || !pruneAllowed || pending}
            onClick={() => {
              void loadPreview('images_all')
            }}
          >
            Preview all unused images
          </Button>
        )}
        {!sameHost && <p>Select resources from one host at a time.</p>}
        {targetHost && !target?.capabilities?.includes('prune_v1') && (
          <p>Requires agent update for cleanup</p>
        )}
        {!targetHost && (
          <p className="text-xs text-text-muted">
            Choose one host to preview cleanup.
          </p>
        )}
      </div>
      {error ? (
        <div>
          <ErrorPanel
            error={error}
            onRetry={() => {
              void query.refetch()
            }}
          />
          {blockingCommandId(error) && (
            <Link
              className="text-link text-sm"
              to={`/docker/activity?host=${encodeURIComponent(previewHost || targetHost)}&command=${encodeURIComponent(blockingCommandId(error)!)}`}
            >
              View blocking command
            </Link>
          )}
        </div>
      ) : null}
      {command && (
        <CommandProgress deviceId={command.device} commandId={command.id} />
      )}
      <Modal
        open={!!preview}
        onClose={() => {
          if (!pending) setPreview(undefined)
        }}
        title="Review cleanup preview"
        footer={
          <>
            <Button
              variant="secondary"
              disabled={pending}
              onClick={() => setPreview(undefined)}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={
                pending ||
                !checked.size ||
                confirm !== required ||
                preview?.requires_refresh
              }
              onClick={async () => {
                if (!preview) return
                setPending(true)
                try {
                  const c = await dockerApi.execute(previewHost, {
                    preview_id: preview.preview_id,
                    selected: [...checked],
                    ...(required ? { confirm } : {}),
                  })
                  setCommand({ id: c.id, device: previewHost })
                  setPreview(undefined)
                  setSelected(new Set())
                } catch (e) {
                  setError(e)
                  setPreview(undefined)
                } finally {
                  setPending(false)
                }
              }}
            >
              Remove selected
            </Button>
          </>
        }
      >
        <p>
          {preview?.count} candidates ·{' '}
          {formatBytes(preview?.reclaimable_bytes ?? 0)} reclaimable · inventory{' '}
          {preview?.inventory_age_s}s old.
        </p>
        {kind === 'volumes' && (
          <p className="text-warn-fg">
            Removing volumes permanently deletes their data. Named volumes start
            unchecked.
          </p>
        )}
        {preview?.requires_refresh && (
          <p role="alert">Inventory must be refreshed before removal.</p>
        )}
        <div className="max-h-64 overflow-auto my-3">
          {preview?.items.map((i) => (
            <label key={i.id || i.name} className="block text-sm py-1">
              <input
                type="checkbox"
                checked={checked.has(i.id || i.name)}
                onChange={() => {
                  const next = new Set(checked)
                  next.has(i.id || i.name)
                    ? next.delete(i.id || i.name)
                    : next.add(i.id || i.name)
                  setChecked(next)
                }}
              />{' '}
              {i.name || i.id} {i.tags?.join(', ')}{' '}
              {typeof i.size === 'number' ? formatBytes(i.size) : ''}
            </label>
          ))}
        </div>
        {required && (
          <label>
            Type {required} to confirm
            <input
              aria-label="Cleanup confirmation"
              className="block w-full rounded bg-surface border border-border p-2"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </label>
        )}
        <p className="text-xs text-text-muted">
          The host rechecks usage before removing each item. Changed items are
          skipped. A preview can be used once.
        </p>
      </Modal>
    </div>
  )
}

import { useEffect, useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { dockerApi } from '../api/docker'
import { api } from '../api/client'
import { Modal } from './ui'
import { containerPath, stackPath } from '../utils/dockerUrlState'
import { useFeatures } from '../hooks/useFeatures'
import { paletteRank, type PaletteEntry } from '../utils/paletteRank'
import {
  parseRecentResources,
  resourceFromPath,
  resourcePath,
  type RecentResource,
} from '../utils/recentResources'
export default function CommandPalette() {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [index, setIndex] = useState(0)
  const [recent, setRecent] = useState<RecentResource[]>(() => {
    try {
      const saved = JSON.parse(
        localStorage.getItem('riot-palette-recent') ?? '[]',
      )
      return parseRecentResources(saved)
    } catch {
      return []
    }
  })
  const navigate = useNavigate()
  const location = useLocation()
  const { isEnabled } = useFeatures()
  const devices = useQuery({
    queryKey: ['palette-devices'],
    queryFn: api.getDevices,
    enabled: open,
  })
  const containers = useQuery({
    queryKey: ['docker-containers'],
    queryFn: dockerApi.containers,
    enabled: open && isEnabled('docker'),
  })
  const stacks = useQuery({
    queryKey: ['docker-stacks'],
    queryFn: dockerApi.stacks,
    enabled: open && isEnabled('docker'),
  })
  useEffect(() => {
    const resource = resourceFromPath(location.pathname)
    if (!resource) return
    setRecent((old) => {
      const next = [
        resource,
        ...old.filter(
          (r) => r.type !== resource.type || r.key !== resource.key,
        ),
      ].slice(0, 8)
      try {
        localStorage.setItem('riot-palette-recent', JSON.stringify(next))
      } catch {}
      return next
    })
  }, [location.pathname])
  useEffect(() => {
    const show = () => {
      setOpen(true)
      setQuery('')
      setIndex(0)
    }
    const key = (e: KeyboardEvent) => {
      if (
        (e.metaKey || e.ctrlKey) &&
        e.key.toLowerCase() === 'k' &&
        !(e.target as HTMLElement)?.closest('[data-palette-exempt], .xterm')
      ) {
        e.preventDefault()
        show()
      }
    }
    window.addEventListener('keydown', key)
    window.addEventListener('riot-palette-open', show)
    return () => {
      window.removeEventListener('keydown', key)
      window.removeEventListener('riot-palette-open', show)
    }
  }, [])
  const all = useMemo(() => {
    const rows: PaletteEntry[] = [
      ...(devices.data ?? []).map((d) => ({
        label: d.hostname,
        kind: 'Host' as const,
        path: `/devices/${encodeURIComponent(d.id)}`,
        search: d.hostname,
      })),
      ...(isEnabled('docker') ? (containers.data ?? []) : []).map((c) => ({
        label: c.riot_name || c.container_name,
        kind: 'Container' as const,
        host: c.hostname,
        path: containerPath(c.device_id, c.container_name),
        search: `${c.container_name} ${c.riot_name ?? ''} ${c.hostname} ${c.stack} ${c.image}`,
        offline:
          devices.data?.find((d) => d.id === c.device_id)?.status === 'offline',
      })),
      ...(isEnabled('docker') ? (stacks.data ?? []) : []).map((s) => ({
        label: s.project,
        kind: 'Stack' as const,
        host: s.hostname,
        path: stackPath(s.device_id, s.project),
        search: `${s.project} ${s.hostname}`,
      })),
      ...(isEnabled('docker')
        ? ['Containers', 'Stacks', 'Images', 'Volumes', 'Networks', 'Activity']
        : []
      ).map((page) => ({
        label: page,
        kind: 'Page' as const,
        path: `/docker/${page.toLowerCase()}`,
        search: `Docker ${page}`,
      })),
    ]
    return query.trim()
      ? paletteRank(rows, query)
      : recent
          .map((resource) =>
            rows.find((r) => r.path === resourcePath(resource)),
          )
          .filter((r): r is PaletteEntry => !!r)
  }, [
    query,
    devices.data,
    containers.data,
    stacks.data,
    recent,
    isEnabled('docker'),
  ])
  const results = all.slice(0, 50)
  function choose(path: string) {
    setOpen(false)
    navigate(path)
  }
  return (
    <Modal open={open} onClose={() => setOpen(false)} title="Go to resource">
      <div
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            setIndex((v) => Math.min(v + 1, results.length - 1))
          }
          if (e.key === 'ArrowUp') {
            e.preventDefault()
            setIndex((v) => Math.max(0, v - 1))
          }
          if (e.key === 'Enter' && results[index]) {
            e.preventDefault()
            choose(results[index].path)
          }
        }}
      >
        <input
          autoFocus
          role="combobox"
          aria-label="Search hosts, containers and stacks"
          aria-controls="palette-results"
          aria-expanded={open}
          aria-activedescendant={
            results[index] ? `palette-${index}` : undefined
          }
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setIndex(0)
          }}
          placeholder="Find a device, container, stack or Docker page…"
          className="w-full bg-surface border border-border rounded p-3"
        />
        {!query && (
          <p className="text-xs text-text-muted mt-2">
            Recently visited resources
          </p>
        )}
        <ul
          role="listbox"
          id="palette-results"
          className="max-h-80 overflow-auto mt-3"
        >
          {results.map((r, i) => (
            <li
              role="option"
              aria-selected={i === index}
              id={`palette-${i}`}
              key={r.path}
            >
              <button
                className={`w-full text-left px-3 py-2 rounded ${i === index ? 'bg-raised-2' : ''}`}
                onClick={() => choose(r.path)}
              >
                <span>{r.label}</span>
                <span className="text-xs text-text-muted ml-3">
                  {r.kind}
                  {r.host ? ` · ${r.host}` : ''}
                  {r.offline ? ' · Host offline' : ''}
                </span>
              </button>
            </li>
          ))}
        </ul>
        {all.length > 50 && <p>{all.length - 50} more, refine your search</p>}
        {!results.length && (
          <p className="text-text-muted p-3">
            {query
              ? 'No matching resources.'
              : 'Search to find hosts, containers, stacks and Docker pages.'}
          </p>
        )}
      </div>
    </Modal>
  )
}

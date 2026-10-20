import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { focusRing } from '../ui/focusRing'
import DockerIcon from '../docker/DockerIcon'

interface AppShellProps {
  children: ReactNode
  utilities?: ReactNode
  brand?: ReactNode
  banners?: ReactNode
  dockerEnabled: boolean
}

const groups = [
  {
    label: 'Infrastructure',
    items: [
      ['/dashboard', 'Overview', 'overview'],
      ['/', 'Devices', 'hosts'],
    ],
  },
  {
    label: 'Docker',
    items: [
      'Containers',
      'Stacks',
      'Hosts',
      'Images',
      'Volumes',
      'Networks',
    ].map((label) => [
      `/docker/${label.toLowerCase()}`,
      label,
      label.toLowerCase(),
    ]),
  },
  {
    label: 'Observe',
    items: [
      ['/probes', 'Probes', 'probes'],
      ['/alerts', 'Alerts', 'alerts'],
      ['/security', 'Security', 'security'],
      ['/docker/activity', 'Activity', 'activity'],
    ],
  },
  {
    label: 'Administration',
    items: [
      ['/alert-rules', 'Alert Rules', 'rules'],
      ['/settings/agents', 'Agents', 'hosts'],
      ['/settings', 'Settings', 'settings'],
    ],
  },
]

function NavigationIcon({ kind }: { kind: string }) {
  const paths: Record<string, string> = {
    overview: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
    probes: 'M2 12h5l3-8 4 16 3-8h5',
    alerts: 'M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4',
    security: 'M12 2 3 6v6c0 5 9 10 9 10s9-5 9-10V6l-9-4ZM8 12l3 3 5-6',
    rules: 'M4 5h16M4 12h16M4 19h16M8 2v6M16 9v6M8 16v6',
    settings:
      'm9 3-1 3-3 1-2 5 2 5 3 1 1 3h6l1-3 3-1 2-5-2-5-3-1-1-3H9ZM12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6',
  }
  if (!paths[kind]) return <DockerIcon kind={kind} />
  return (
    <svg
      aria-hidden="true"
      className="h-4 w-4 shrink-0"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={paths[kind]} />
    </svg>
  )
}

function activeRoute(pathname: string, path: string) {
  if (path === '/') return pathname === '/' || pathname.startsWith('/devices/')
  if (path === '/security')
    return pathname === path || pathname.startsWith('/learn/')
  if (path === '/settings')
    return (
      (pathname === path || pathname.startsWith(`${path}/`)) &&
      pathname !== '/settings/agents'
    )
  return pathname === path || pathname.startsWith(`${path}/`)
}

export function AppShell({
  children,
  utilities,
  brand,
  banners,
  dockerEnabled,
}: AppShellProps) {
  const { pathname } = useLocation()
  const [open, setOpen] = useState(false)
  const toggle = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    setOpen(false)
  }, [pathname])
  return (
    <div className="min-h-screen bg-surface text-text">
      <a
        href="#main-content"
        onClick={(event) => {
          event.preventDefault()
          document.getElementById('main-content')?.focus()
        }}
        className={`sr-only z-50 rounded bg-raised p-3 focus:not-sr-only focus:fixed focus:left-3 focus:top-3 ${focusRing}`}
      >
        Skip to content
      </a>
      <header className="sticky top-0 z-30 flex h-14 min-w-0 items-center justify-between gap-2 border-b border-border bg-raised px-3 sm:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <button
            ref={toggle}
            type="button"
            aria-label="Toggle navigation"
            aria-expanded={open}
            aria-controls="app-navigation"
            onClick={() => setOpen((v) => !v)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') setOpen(false)
            }}
            className={`rounded border border-border-strong px-2 py-1.5 text-sm xl:hidden ${focusRing}`}
          >
            ☰
          </button>
          {brand ?? (
            <Link to="/" className="font-semibold">
              rIOt
            </Link>
          )}
        </div>
        <div className="flex min-w-0 items-center gap-1 sm:gap-2">
          {utilities}
        </div>
      </header>
      <div className="xl:grid xl:grid-cols-[216px_minmax(0,1fr)]">
        <aside
          id="app-navigation"
          className={`${open ? 'block' : 'hidden'} border-b border-border bg-surface xl:sticky xl:top-14 xl:block xl:h-[calc(100vh-56px)] xl:overflow-y-auto xl:border-r xl:border-b-0`}
          onKeyDown={(e) => {
            if (e.key === 'Escape' && open) {
              setOpen(false)
              toggle.current?.focus()
            }
          }}
        >
          <nav
            aria-label="Application"
            className="grid grid-cols-2 gap-5 p-4 sm:grid-cols-4 xl:flex xl:flex-col xl:gap-6"
          >
            {groups
              .filter((group) => dockerEnabled || group.label !== 'Docker')
              .map((group) => (
                <div key={group.label} className="min-w-0">
                  <div className="mb-2 px-3 text-[10px] font-medium uppercase tracking-wider text-text-muted">
                    {group.label}
                  </div>
                  <div className="space-y-1">
                    {group.items
                      .filter(
                        ([path]) =>
                          dockerEnabled || !path.startsWith('/docker/'),
                      )
                      .map(([path, label, icon]) => {
                        const active = activeRoute(pathname, path)
                        return (
                          <Link
                            key={path}
                            to={path}
                            onClick={() => setOpen(false)}
                            aria-current={active ? 'page' : undefined}
                            className={`flex items-center gap-2.5 rounded-md border px-3 py-2 text-sm ${focusRing} ${active ? 'border-border-strong bg-raised-2 font-medium text-text' : 'border-transparent text-text-muted hover:bg-raised hover:text-text'}`}
                          >
                            <NavigationIcon kind={icon} />
                            {label}
                          </Link>
                        )
                      })}
                  </div>
                </div>
              ))}
          </nav>
        </aside>
        <div className="min-w-0">
          {banners}
          <main
            id="main-content"
            tabIndex={-1}
            className="mx-auto min-w-0 max-w-[1920px] px-4 py-5 outline-none sm:px-6 xl:px-8"
          >
            {children}
          </main>
        </div>
      </div>
    </div>
  )
}

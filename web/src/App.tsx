import { useCallback, useEffect, useRef, useState } from 'react'
import { Routes, Route, Link, useLocation, useNavigate, Navigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api/client'
import { useAuth } from './hooks/useAuth'
import { WebSocketProvider, useWebSocket } from './contexts/WebSocketProvider'
import type { WSMessage } from './types/models'
import Dashboard from './pages/Dashboard'
import FleetOverview from './pages/FleetOverview'
import DeviceDetail from './pages/DeviceDetail'
import DockerLayout from './pages/docker/DockerLayout'
import ContainersPage from './pages/docker/ContainersPage'
import ContainerPage from './pages/docker/ContainerPage'
import LegacyRedirect from './pages/docker/LegacyRedirect'
import StacksPage from './pages/docker/StacksPage'
import StackPage from './pages/docker/StackPage'
import InventoryPage from './pages/docker/InventoryPage'
import DockerHostsPage from './pages/docker/HostsPage'
import ActivityPage from './pages/docker/ActivityPage'
import CommandPalette from './components/CommandPalette'
import { useFeatures } from './hooks/useFeatures'

import Alerts from './pages/Alerts'
import Login from './pages/Login'
import Setup from './pages/Setup'
import DeviceTerminal from './pages/DeviceTerminal'
import SettingsLayout from './pages/SettingsLayout'
import AlertRuleSettings from './pages/settings/AlertRuleSettings'
import NotificationSettings from './pages/settings/NotificationSettings'
import GeneralSettings from './pages/settings/GeneralSettings'
import AgentManagement from './pages/settings/AgentManagement'
import CertificateSettings from './pages/settings/CertificateSettings'
import LogSettings from './pages/settings/LogSettings'
import FeatureSettings from './pages/settings/FeatureSettings'
import Security from './pages/Security'
import Probes from './pages/Probes'
import ProbeDetail from './pages/ProbeDetail'
import LearnMore from './pages/LearnMore'
import DeviceProbes from './pages/DeviceProbes'
import HeaderClock from './components/HeaderClock'
import { Button, Modal } from './components/ui'
import { AppShell } from './components/layout/AppShell'

function AlertsBell() {
  const location = useLocation()
  const active = location.pathname === '/alerts' || location.pathname.startsWith('/alerts')
  const { data } = useQuery({
    queryKey: ['unread-count'],
    queryFn: api.getUnreadEventCount,
    refetchInterval: 30_000,
  })
  const count = data?.count ?? 0

  return (
    <Link
      to="/alerts"
      className={`relative p-2 rounded-md transition-colors ${
        active ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-800'
      }`}
      title="Alerts"
      aria-label={count ? `Alerts (${count} unread)` : 'Alerts'}
    >
      <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
        <path d="M10 2a6 6 0 00-6 6v3.586l-.707.707A1 1 0 004 14h12a1 1 0 00.707-1.707L16 11.586V8a6 6 0 00-6-6zM10 18a3 3 0 01-3-3h6a3 3 0 01-3 3z" />
      </svg>
      {count > 0 && (
        <span className="absolute -top-0.5 -right-0.5 min-w-[18px] h-[18px] flex items-center justify-center px-1 text-[10px] font-bold bg-red-500 text-white rounded-full leading-none">
          {count > 99 ? '99+' : count}
        </span>
      )}
    </Link>
  )
}

function UserMenu({ logout }: { logout: () => void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    if (open) document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open])

  return (
    <div className="relative" ref={ref} onKeyDown={e => { if (e.key === 'Escape') { setOpen(false); ref.current?.querySelector('button')?.focus() } }}>
      <button
        onClick={() => setOpen(v => !v)}
        className={`p-1.5 rounded-md transition-colors ${
          open ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-800'
        }`}
        title="Account"
        aria-label="Account"
        aria-expanded={open}
      >
        <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
          <path fillRule="evenodd" d="M10 9a3 3 0 100-6 3 3 0 000 6zm-7 9a7 7 0 1114 0H3z" clipRule="evenodd" />
        </svg>
      </button>
      {open && (
        <div className="absolute right-0 mt-1 w-44 bg-gray-900 border border-gray-700 rounded-lg shadow-lg z-50 py-1">
          <button
            onClick={() => { setOpen(false); navigate('/settings/general') }}
            className="w-full text-left px-4 py-2 text-sm text-gray-300 hover:bg-gray-800 hover:text-white transition-colors"
          >
            Change Password
          </button>
          <div className="border-t border-gray-800 my-1" />
          <button
            onClick={() => { setOpen(false); logout() }}
            className="w-full text-left px-4 py-2 text-sm text-red-400 hover:bg-gray-800 hover:text-red-300 transition-colors"
          >
            Logout
          </button>
        </div>
      )}
    </div>
  )
}

/** Global WebSocket handler — keeps alert badge and caches up-to-date on ALL pages. */
function GlobalWSHandler() {
  const queryClient = useQueryClient()
  useWebSocket(useCallback((msg: WSMessage) => {
    if (msg.type === 'event') {
      const evt = msg.data as { id?: number; severity?: string }
      queryClient.setQueryData(['events'], (old: any) => {
        if (!old) return old // only update if events were already fetched
        if (evt.id && old.some((e: any) => e.id === evt.id)) return old
        return [msg.data, ...old]
      })
      if (evt.severity === 'warning' || evt.severity === 'critical') {
        queryClient.invalidateQueries({ queryKey: ['unread-count'] })
      }
    }
  }, [queryClient]))
  return null
}

function ScrollToTop() {
  const { pathname } = useLocation()
  useEffect(() => { window.scrollTo(0, 0) }, [pathname])
  return null
}

function UpdateBanner() {
  const { data: update } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    staleTime: 60 * 60 * 1000,
    refetchInterval: 6 * 60 * 60 * 1000,
  })
  const [dismissed, setDismissed] = useState<string | null>(() =>
    sessionStorage.getItem('riot-update-dismissed')
  )
  const [showModal, setShowModal] = useState(false)

  if (!update?.update_available) return null
  if (dismissed === update.latest_version) return null

  return (
    <>
      <div className="bg-amber-900/40 border-b border-amber-800/50">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-2 flex items-center justify-between text-sm">
          <span className="text-amber-300">
            Server update available: v{update.current_version} &rarr; v{update.latest_version}
          </span>
          <div className="flex items-center gap-3">
            <button
              onClick={() => setShowModal(true)}
              className="text-amber-400 hover:text-amber-300 font-medium underline underline-offset-2"
            >
              View Update
            </button>
            <button
              onClick={() => {
                setDismissed(update.latest_version)
                sessionStorage.setItem('riot-update-dismissed', update.latest_version)
              }}
              className="text-amber-600 hover:text-amber-400"
            >
              &times;
            </button>
          </div>
        </div>
      </div>
      <Modal
        open={showModal}
        onClose={() => setShowModal(false)}
        title="Server Update Available"
        footer={
          <Button variant="secondary" onClick={() => setShowModal(false)}>
            Close
          </Button>
        }
      >
        <div className="space-y-3">
          <p className="text-sm text-text">
            <span className="text-text-muted">Current:</span> v{update.current_version}
            <span className="mx-2 text-text-muted">&rarr;</span>
            <span className="text-text-muted">Latest:</span> v{update.latest_version}
          </p>
          <div className="rounded bg-raised-2 p-3">
            <p className="mb-1 text-xs text-text-muted">To update the server:</p>
            <code className="select-all text-sm text-success-fg">
              docker compose pull && docker compose up -d
            </code>
            <p className="mt-2 text-xs text-text-muted">
              If the server runs on a managed device, you can also update it from the Containers view on that device's page.
            </p>
          </div>
          {update.release_url && (
            <a
              href={update.release_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-block text-sm text-link hover:text-info-fg"
            >
              View release notes &rarr;
            </a>
          )}
        </div>
      </Modal>
    </>
  )
}

function ServerVersion() {
  const { data: update } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    staleTime: 60 * 60 * 1000,
    refetchInterval: 6 * 60 * 60 * 1000,
  })
  if (!update?.current_version) return null
  return (
    <span className={`text-[11px] font-normal ${update.update_available ? 'text-amber-400' : 'text-gray-500'}`}>
      v{update.current_version}{update.update_available ? ` \u2192 v${update.latest_version}` : ''}
    </span>
  )
}

export default function App() {
  const { authenticated, needsSetup, loading, login, logout, recheckAuth } = useAuth()
  const { isEnabled } = useFeatures(authenticated && !loading && !needsSetup)

  if (loading) {
    return (
      <div className="min-h-screen bg-gray-950 flex items-center justify-center">
        <div className="text-gray-400">Loading...</div>
      </div>
    )
  }

  if (needsSetup) {
    return <Setup onComplete={recheckAuth} />
  }

  if (!authenticated) {
    return <Login onLogin={login} />
  }

  return (
    <WebSocketProvider>
      <ScrollToTop />
      <GlobalWSHandler />
      <CommandPalette />
      <AppShell
        dockerEnabled={isEnabled('docker')}
        brand={<Link to="/" className="flex min-w-0 items-center gap-2 text-xl font-bold tracking-tight text-text"><img src={import.meta.env.BASE_URL + 'android-chrome-192x192.png'} alt="" className="h-6 w-6" />rIOt<span className="hidden sm:inline"><ServerVersion /></span></Link>}
        utilities={<>
          <button type="button" className="rounded-md px-2 py-2 text-sm text-text-muted hover:bg-raised-2 hover:text-text focus-visible:ring-2 focus-visible:ring-accent" aria-label="Open command palette" onClick={() => window.dispatchEvent(new Event('riot-palette-open'))}><span className="sm:hidden">Search</span><span className="hidden sm:inline">Search · Ctrl K</span></button>
          <HeaderClock />
          <AlertsBell />
          <UserMenu logout={logout} />
        </>}
        banners={<>
      {import.meta.env.VITE_DEMO === 'true' && (
        <div className="bg-violet-900/40 border-b border-violet-800/50">
          <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-1.5 flex items-center justify-center gap-2 text-sm text-violet-300">
            <span>This is a static demo with simulated data.</span>
            <a href="https://github.com/rbretschneider/rIOt" target="_blank" rel="noopener noreferrer" className="text-violet-400 hover:text-violet-200 font-medium underline underline-offset-2">
              View on GitHub
            </a>
          </div>
        </div>
      )}
      <UpdateBanner />
        </>}
      >
        <Routes>
          <Route path="/" element={<FleetOverview />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/devices/:id" element={<DeviceDetail />} />
          <Route path="/devices/:id/containers" element={<LegacyRedirect />} />
          <Route path="/devices/:id/containers/:cid" element={<LegacyRedirect />} />
          <Route path="/docker" element={<DockerLayout />} >
            <Route index element={<Navigate replace to="containers" />} />
            <Route path="containers" element={<ContainersPage />} />
            <Route path="containers/:deviceId/:name" element={<ContainerPage />} />
            <Route path="stacks" element={<StacksPage />} />
            <Route path="hosts" element={<DockerHostsPage />} />
            <Route path="stacks/:deviceId/:project" element={<StackPage />} />
            <Route path="images" element={<InventoryPage key="images" kind="images" />} />
            <Route path="volumes" element={<InventoryPage key="volumes" kind="volumes" />} />
            <Route path="networks" element={<InventoryPage key="networks" kind="networks" />} />
            <Route path="activity" element={<ActivityPage />} />
          </Route>
          <Route path="/devices/:id/probes" element={<DeviceProbes />} />
          <Route path="/devices/:id/terminal" element={<DeviceTerminal />} />
          <Route path="/alerts" element={<Alerts />} />
          <Route path="/alert-rules" element={<AlertRuleSettings />} />
          <Route path="/probes" element={<Probes />} />
          <Route path="/probes/:id" element={<ProbeDetail />} />
          <Route path="/security" element={<Security />} />
          <Route path="/learn/:findingId" element={<LearnMore />} />
          <Route path="/settings" element={<SettingsLayout />}>
            <Route index element={<Navigate to="/settings/notifications" replace />} />
            <Route path="alert-rules" element={<Navigate to="/alert-rules" replace />} />
            <Route path="notifications" element={<NotificationSettings />} />
            <Route path="agents" element={<AgentManagement />} />
            <Route path="certificates" element={<CertificateSettings />} />
            <Route path="features" element={<FeatureSettings />} />
            <Route path="logs" element={<LogSettings />} />
            <Route path="general" element={<GeneralSettings />} />
          </Route>
        </Routes>
      </AppShell>
    </WebSocketProvider>
  )
}

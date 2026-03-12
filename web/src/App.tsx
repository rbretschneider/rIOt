import { useCallback, useEffect, useState } from 'react'
import { Routes, Route, Link, useLocation, Navigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api/client'
import { useAuth } from './hooks/useAuth'
import { WebSocketProvider, useWebSocket } from './contexts/WebSocketProvider'
import type { WSMessage } from './types/models'
import FleetOverview from './pages/FleetOverview'
import DeviceDetail from './pages/DeviceDetail'
import DeviceContainers from './pages/DeviceContainers'
import ContainerDetailPage from './pages/ContainerDetailPage'
import Alerts from './pages/Alerts'
import Login from './pages/Login'
import Setup from './pages/Setup'
import DeviceTerminal from './pages/DeviceTerminal'
import SettingsLayout from './pages/SettingsLayout'
import AlertRuleSettings from './pages/settings/AlertRuleSettings'
import NotificationSettings from './pages/settings/NotificationSettings'
import GeneralSettings from './pages/settings/GeneralSettings'
import ProbeSettings from './pages/settings/ProbeSettings'
import AgentManagement from './pages/settings/AgentManagement'
import CertificateSettings from './pages/settings/CertificateSettings'
import LogSettings from './pages/settings/LogSettings'
import Security from './pages/Security'
import Probes from './pages/Probes'
import ProbeDetail from './pages/ProbeDetail'
import LearnMore from './pages/LearnMore'

function NavLink({ to, children }: { to: string; children: React.ReactNode }) {
  const location = useLocation()
  const active = location.pathname === to || (to !== '/' && location.pathname.startsWith(to))
  return (
    <Link
      to={to}
      className={`px-3 py-2 rounded-md text-sm font-medium transition-colors whitespace-nowrap ${
        active ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-800'
      }`}
    >
      {children}
    </Link>
  )
}

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
      {showModal && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setShowModal(false)}>
          <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-md mx-4 p-6" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">Server Update Available</h3>
              <button onClick={() => setShowModal(false)} className="text-gray-500 hover:text-white transition-colors">
                <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                  <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
                </svg>
              </button>
            </div>
            <div className="space-y-3">
              <p className="text-sm text-gray-300">
                <span className="text-gray-500">Current:</span> v{update.current_version}
                <span className="text-gray-500 mx-2">&rarr;</span>
                <span className="text-gray-500">Latest:</span> v{update.latest_version}
              </p>
              <div className="bg-gray-800 rounded p-3">
                <p className="text-xs text-gray-400 mb-1">To update the server:</p>
                <code className="text-sm text-emerald-400 select-all">
                  docker compose pull && docker compose up -d
                </code>
                <p className="text-xs text-gray-500 mt-2">
                  If the server runs on a managed device, you can also update it from the Containers view on that device's page.
                </p>
              </div>
              {update.release_url && (
                <a
                  href={update.release_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-sm text-blue-400 hover:text-blue-300 inline-block"
                >
                  View release notes &rarr;
                </a>
              )}
            </div>
            <div className="flex justify-end mt-6">
              <button
                onClick={() => setShowModal(false)}
                className="px-4 py-2 text-sm text-gray-400 hover:text-white"
              >
                Close
              </button>
            </div>
          </div>
        </div>
      )}
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

function MobileMenuButton({ open, onClick }: { open: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="sm:hidden p-1.5 text-gray-400 hover:text-white hover:bg-gray-800 rounded-md transition-colors"
      aria-label="Toggle menu"
    >
      {open ? (
        <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
          <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
        </svg>
      ) : (
        <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
          <path fillRule="evenodd" d="M3 5a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zM3 10a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zM3 15a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1z" clipRule="evenodd" />
        </svg>
      )}
    </button>
  )
}

export default function App() {
  const { authenticated, needsSetup, loading, login, logout, recheckAuth } = useAuth()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)

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
    <div className="min-h-screen bg-gray-950">
      <ScrollToTop />
      <GlobalWSHandler />
      <nav className="bg-gray-900 border-b border-gray-800">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center justify-between h-14">
            <div className="flex items-center gap-4 min-w-0">
              <Link to="/" className="flex items-center gap-2 text-xl font-bold text-white tracking-tight shrink-0">
                <img src={`${import.meta.env.BASE_URL}android-chrome-192x192.png`} alt="rIOt" className="w-6 h-6" />
                rIOt
                <ServerVersion />
              </Link>
              {/* Desktop nav */}
              <div className="hidden sm:flex gap-1">
                <NavLink to="/">Fleet</NavLink>
                <NavLink to="/security">Security</NavLink>
                <NavLink to="/probes">Probes</NavLink>
                <NavLink to="/settings">Settings</NavLink>
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <AlertsBell />
              <button
                onClick={logout}
                className="hidden sm:block px-3 py-1.5 text-sm text-gray-400 hover:text-white hover:bg-gray-800 rounded-md transition-colors"
                title="Sign out"
              >
                Logout
              </button>
              <MobileMenuButton open={mobileMenuOpen} onClick={() => setMobileMenuOpen(v => !v)} />
            </div>
          </div>
        </div>
        {/* Mobile nav dropdown */}
        {mobileMenuOpen && (
          <div className="sm:hidden border-t border-gray-800 px-4 py-2 space-y-1" onClick={() => setMobileMenuOpen(false)}>
            <NavLink to="/">Fleet</NavLink>
            <NavLink to="/security">Security</NavLink>
            <NavLink to="/probes">Probes</NavLink>
            <NavLink to="/alerts">Alerts</NavLink>
            <NavLink to="/settings">Settings</NavLink>
            <button
              onClick={logout}
              className="w-full text-left px-3 py-2 rounded-md text-sm font-medium text-gray-400 hover:text-white hover:bg-gray-800 transition-colors"
            >
              Logout
            </button>
          </div>
        )}
      </nav>
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
      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
        <Routes>
          <Route path="/" element={<FleetOverview />} />
          <Route path="/devices/:id" element={<DeviceDetail />} />
          <Route path="/devices/:id/containers" element={<DeviceContainers />} />
          <Route path="/devices/:id/containers/:cid" element={<ContainerDetailPage />} />
          <Route path="/devices/:id/terminal" element={<DeviceTerminal />} />
          <Route path="/alerts" element={<Alerts />} />
          <Route path="/probes" element={<Probes />} />
          <Route path="/probes/:id" element={<ProbeDetail />} />
          <Route path="/security" element={<Security />} />
          <Route path="/learn/:findingId" element={<LearnMore />} />
          <Route path="/settings" element={<SettingsLayout />}>
            <Route index element={<Navigate to="/settings/alert-rules" replace />} />
            <Route path="alert-rules" element={<AlertRuleSettings />} />
            <Route path="notifications" element={<NotificationSettings />} />
            <Route path="probes" element={<ProbeSettings />} />
            <Route path="agents" element={<AgentManagement />} />
            <Route path="certificates" element={<CertificateSettings />} />
            <Route path="logs" element={<LogSettings />} />
            <Route path="general" element={<GeneralSettings />} />
          </Route>
        </Routes>
      </main>
    </div>
    </WebSocketProvider>
  )
}

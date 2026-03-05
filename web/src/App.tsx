import { useState, useRef, useEffect } from 'react'
import { Routes, Route, Link, useLocation, Navigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from './api/client'
import { useAuth } from './hooks/useAuth'
import FleetOverview from './pages/FleetOverview'
import DeviceDetail from './pages/DeviceDetail'
import DeviceContainers from './pages/DeviceContainers'
import Alerts from './pages/Alerts'
import Login from './pages/Login'
import DeviceTerminal from './pages/DeviceTerminal'
import SettingsLayout from './pages/SettingsLayout'
import AlertRuleSettings from './pages/settings/AlertRuleSettings'
import NotificationSettings from './pages/settings/NotificationSettings'
import GeneralSettings from './pages/settings/GeneralSettings'
import ProbeSettings from './pages/settings/ProbeSettings'
import AgentManagement from './pages/settings/AgentManagement'
import Security from './pages/Security'
import Probes from './pages/Probes'
import ProbeDetail from './pages/ProbeDetail'

function NavLink({ to, children }: { to: string; children: React.ReactNode }) {
  const location = useLocation()
  const active = location.pathname === to || (to !== '/' && location.pathname.startsWith(to))
  return (
    <Link
      to={to}
      className={`px-3 py-2 rounded-md text-sm font-medium transition-colors ${
        active ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-800'
      }`}
    >
      {children}
    </Link>
  )
}

function UpdateBell() {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const { data: update } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    refetchInterval: 6 * 60 * 60 * 1000, // 6 hours
    staleTime: 60 * 60 * 1000, // 1 hour
  })

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  const hasUpdate = update?.update_available

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen(!open)}
        className="relative p-2 text-gray-400 hover:text-white transition-colors"
        title={hasUpdate ? 'Update available' : 'No updates'}
      >
        <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
          <path d="M10 2a6 6 0 00-6 6v3.586l-.707.707A1 1 0 004 14h12a1 1 0 00.707-1.707L16 11.586V8a6 6 0 00-6-6zM10 18a3 3 0 01-3-3h6a3 3 0 01-3 3z" />
        </svg>
        {hasUpdate && (
          <span className="absolute top-1.5 right-1.5 w-2 h-2 bg-amber-400 rounded-full" />
        )}
      </button>

      {open && (
        <div className="absolute right-0 mt-2 w-80 bg-gray-900 border border-gray-700 rounded-lg shadow-xl z-50">
          <div className="p-4">
            {hasUpdate ? (
              <>
                <p className="text-sm font-medium text-amber-400 mb-2">Server Update Available</p>
                <p className="text-sm text-gray-300 mb-1">
                  <span className="text-gray-500">Current:</span> {update.current_version}
                </p>
                <p className="text-sm text-gray-300 mb-3">
                  <span className="text-gray-500">Latest:</span> {update.latest_version}
                </p>
                <div className="bg-gray-800 rounded p-3 mb-3">
                  <p className="text-xs text-gray-400 mb-1">To update the server:</p>
                  <code className="text-xs text-emerald-400 select-all">
                    docker compose pull && docker compose up -d
                  </code>
                </div>
                {update.release_url && (
                  <a
                    href={update.release_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-xs text-blue-400 hover:text-blue-300"
                  >
                    View release notes
                  </a>
                )}
              </>
            ) : (
              <p className="text-sm text-gray-400">
                Server is up to date{update?.current_version ? ` (v${update.current_version})` : ''}.
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export default function App() {
  const { authenticated, loading, login, logout } = useAuth()

  if (loading) {
    return (
      <div className="min-h-screen bg-gray-950 flex items-center justify-center">
        <div className="text-gray-400">Loading...</div>
      </div>
    )
  }

  if (!authenticated) {
    return <Login onLogin={login} />
  }

  return (
    <div className="min-h-screen bg-gray-950">
      <nav className="bg-gray-900 border-b border-gray-800">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center justify-between h-14">
            <div className="flex items-center gap-4">
              <Link to="/" className="text-xl font-bold text-white tracking-tight">
                rIOt
              </Link>
              <div className="flex gap-1">
                <NavLink to="/">Fleet</NavLink>
                <NavLink to="/alerts">Alerts</NavLink>
                <NavLink to="/probes">Probes</NavLink>
                <NavLink to="/security">Security</NavLink>
                <NavLink to="/settings">Settings</NavLink>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <UpdateBell />
              <button
                onClick={logout}
                className="px-3 py-1.5 text-sm text-gray-400 hover:text-white hover:bg-gray-800 rounded-md transition-colors"
                title="Sign out"
              >
                Logout
              </button>
            </div>
          </div>
        </div>
      </nav>
      <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
        <Routes>
          <Route path="/" element={<FleetOverview />} />
          <Route path="/devices/:id" element={<DeviceDetail />} />
          <Route path="/devices/:id/containers" element={<DeviceContainers />} />
          <Route path="/devices/:id/terminal" element={<DeviceTerminal />} />
          <Route path="/alerts" element={<Alerts />} />
          <Route path="/probes" element={<Probes />} />
          <Route path="/probes/:id" element={<ProbeDetail />} />
          <Route path="/security" element={<Security />} />
          <Route path="/settings" element={<SettingsLayout />}>
            <Route index element={<Navigate to="/settings/alert-rules" replace />} />
            <Route path="alert-rules" element={<AlertRuleSettings />} />
            <Route path="notifications" element={<NotificationSettings />} />
            <Route path="probes" element={<ProbeSettings />} />
            <Route path="agents" element={<AgentManagement />} />
            <Route path="general" element={<GeneralSettings />} />
          </Route>
        </Routes>
      </main>
    </div>
  )
}

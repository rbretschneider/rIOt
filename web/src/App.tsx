import { useEffect } from 'react'
import { Routes, Route, Link, useLocation, Navigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from './api/client'
import { useAuth } from './hooks/useAuth'
import FleetOverview from './pages/FleetOverview'
import DeviceDetail from './pages/DeviceDetail'
import DeviceContainers from './pages/DeviceContainers'
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
import Security from './pages/Security'
import Probes from './pages/Probes'
import ProbeDetail from './pages/ProbeDetail'

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

function AlertsNavLink() {
  const location = useLocation()
  const active = location.pathname === '/alerts' || location.pathname.startsWith('/alerts')
  const { data } = useQuery({
    queryKey: ['unread-count'],
    queryFn: api.getUnreadEventCount,
    refetchInterval: 60_000,
  })
  const count = data?.count ?? 0

  return (
    <Link
      to="/alerts"
      className={`px-3 py-2 rounded-md text-sm font-medium transition-colors whitespace-nowrap relative ${
        active ? 'bg-gray-800 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-800'
      }`}
    >
      Alerts
      {count > 0 && (
        <span className="absolute -top-1 -right-1 min-w-[18px] h-[18px] flex items-center justify-center px-1 text-[10px] font-bold bg-red-500 text-white rounded-full leading-none">
          {count > 99 ? '99+' : count}
        </span>
      )}
    </Link>
  )
}

function ScrollToTop() {
  const { pathname } = useLocation()
  useEffect(() => { window.scrollTo(0, 0) }, [pathname])
  return null
}

export default function App() {
  const { authenticated, needsSetup, loading, login, logout, recheckAuth } = useAuth()

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
    <div className="min-h-screen bg-gray-950">
      <ScrollToTop />
      <nav className="bg-gray-900 border-b border-gray-800">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center justify-between h-14">
            <div className="flex items-center gap-4 min-w-0">
              <Link to="/" className="flex items-center gap-2 text-xl font-bold text-white tracking-tight shrink-0">
                <img src="/android-chrome-192x192.png" alt="rIOt" className="w-6 h-6" />
                rIOt
              </Link>
              <div className="flex gap-1 overflow-x-auto scrollbar-hide">
                <NavLink to="/">Fleet</NavLink>
                <AlertsNavLink />
                <NavLink to="/probes">Probes</NavLink>
                <NavLink to="/security">Security</NavLink>
                <NavLink to="/settings">Settings</NavLink>
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
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
            <Route path="certificates" element={<CertificateSettings />} />
            <Route path="general" element={<GeneralSettings />} />
          </Route>
        </Routes>
      </main>
    </div>
  )
}

import { NavLink, Outlet } from 'react-router-dom'

const tabs = [
  { to: '/settings/alert-rules', label: 'Alert Rules' },
  { to: '/settings/notifications', label: 'Notifications' },
  { to: '/settings/probes', label: 'Probes' },
  { to: '/settings/agents', label: 'Agents' },
  { to: '/settings/general', label: 'General' },
]

export default function SettingsLayout() {
  return (
    <div>
      <div className="flex items-center gap-6 mb-6">
        <h1 className="text-2xl font-bold text-white">Settings</h1>
        <nav className="flex gap-1">
          {tabs.map(t => (
            <NavLink
              key={t.to}
              to={t.to}
              className={({ isActive }) =>
                `px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                  isActive
                    ? 'bg-gray-800 text-white'
                    : 'text-gray-400 hover:text-white hover:bg-gray-800'
                }`
              }
            >
              {t.label}
            </NavLink>
          ))}
        </nav>
      </div>
      <Outlet />
    </div>
  )
}

import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'

export default function Security() {
  const { data: overview, isLoading: loadingOverview } = useQuery({
    queryKey: ['security-overview'],
    queryFn: api.getSecurityOverview,
    refetchInterval: 30_000,
  })

  const { data: devices = [], isLoading: loadingDevices } = useQuery({
    queryKey: ['security-devices'],
    queryFn: api.getSecurityDevices,
    refetchInterval: 30_000,
  })

  if (loadingOverview || loadingDevices) return <div className="text-gray-400">Loading...</div>

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-white">Security</h1>

      {/* Overview Cards */}
      {overview && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
          <StatCard
            label="Failed Logins (24h)"
            value={overview.total_failed_logins}
            color={overview.total_failed_logins > 0 ? 'text-amber-400' : 'text-emerald-400'}
          />
          <StatCard label="Logged-in Users" value={overview.total_logged_in} />
          <StatCard
            label="Firewall Active"
            value={`${overview.firewall_active}/${overview.devices_reporting}`}
            color={overview.firewall_inactive > 0 ? 'text-amber-400' : 'text-emerald-400'}
          />
          <StatCard label="Reporting" value={`${overview.devices_reporting}/${overview.total_devices}`} />
        </div>
      )}

      {/* MAC/Security Modules */}
      {overview && (overview.selinux_enforcing > 0 || overview.apparmor_enabled > 0) && (
        <div className="grid grid-cols-2 gap-4">
          {overview.selinux_enforcing > 0 && (
            <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
              <p className="text-xs text-gray-500 uppercase">SELinux Enforcing</p>
              <p className="text-2xl font-bold text-emerald-400">{overview.selinux_enforcing}</p>
            </div>
          )}
          {overview.apparmor_enabled > 0 && (
            <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
              <p className="text-xs text-gray-500 uppercase">AppArmor Enabled</p>
              <p className="text-2xl font-bold text-emerald-400">{overview.apparmor_enabled}</p>
            </div>
          )}
        </div>
      )}

      {/* Per-Device Table */}
      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-x-auto">
        <div className="px-4 py-3 border-b border-gray-800">
          <h2 className="text-sm font-semibold text-gray-300 uppercase">Per-Device Security</h2>
        </div>
        <table className="w-full text-sm min-w-[640px]">
          <thead>
            <tr className="text-left text-gray-400 text-xs uppercase border-b border-gray-800">
              <th className="px-4 py-3">Device</th>
              <th className="px-4 py-3">Firewall</th>
              <th className="px-4 py-3">SELinux</th>
              <th className="px-4 py-3">AppArmor</th>
              <th className="px-4 py-3 text-right">Failed Logins</th>
              <th className="px-4 py-3 text-right">Logged In</th>
              <th className="px-4 py-3 text-right">Open Ports</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800/50">
            {devices.map(d => (
              <tr key={d.device_id} className="text-gray-300">
                <td className="px-4 py-3">
                  <Link to={`/devices/${d.device_id}`} className="text-blue-400 hover:text-blue-300 font-medium">
                    {d.hostname}
                  </Link>
                </td>
                <td className="px-4 py-3">
                  {d.firewall_status ? (
                    <span className={
                      d.firewall_status === 'active' || d.firewall_status === 'enabled'
                        ? 'text-emerald-400'
                        : 'text-amber-400'
                    }>
                      {d.firewall_status}
                    </span>
                  ) : <span className="text-gray-600">-</span>}
                </td>
                <td className="px-4 py-3">
                  {d.selinux ? (
                    <span className={d.selinux === 'enforcing' ? 'text-emerald-400' : 'text-gray-400'}>
                      {d.selinux}
                    </span>
                  ) : <span className="text-gray-600">-</span>}
                </td>
                <td className="px-4 py-3">
                  {d.apparmor ? (
                    <span className={d.apparmor === 'enabled' || d.apparmor === 'active' ? 'text-emerald-400' : 'text-gray-400'}>
                      {d.apparmor}
                    </span>
                  ) : <span className="text-gray-600">-</span>}
                </td>
                <td className="px-4 py-3 text-right">
                  <span className={d.failed_logins_24h > 0 ? 'text-amber-400' : 'text-gray-400'}>
                    {d.failed_logins_24h}
                  </span>
                </td>
                <td className="px-4 py-3 text-right text-gray-400">{d.logged_in_users}</td>
                <td className="px-4 py-3 text-right">
                  <PortsList ports={d.open_ports} />
                </td>
              </tr>
            ))}
            {devices.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-gray-500">
                  No security data available. Security info is collected from device telemetry.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

const PORT_DISPLAY_LIMIT = 10

function PortsList({ ports }: { ports: number[] }) {
  const [open, setOpen] = useState(false)
  const btnRef = useRef<HTMLButtonElement>(null)
  if (!ports || ports.length === 0) return <span className="text-gray-600">-</span>

  const sorted = [...ports].sort((a, b) => a - b)

  if (sorted.length <= PORT_DISPLAY_LIMIT) {
    return <span className="font-mono text-xs text-gray-500">{sorted.join(', ')}</span>
  }

  const rect = btnRef.current?.getBoundingClientRect()

  return (
    <div className="inline-block">
      <button
        ref={btnRef}
        onClick={() => setOpen(!open)}
        className="font-mono text-xs text-blue-400 hover:text-blue-300 transition-colors"
      >
        {sorted.length} ports
      </button>
      {open && rect && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div
            className="fixed z-50 w-72 max-h-64 overflow-y-auto bg-gray-800 border border-gray-700 rounded-lg shadow-xl p-3"
            style={{ top: rect.bottom + 4, right: window.innerWidth - rect.right }}
          >
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs text-gray-400 font-medium">{sorted.length} open ports</span>
              <button onClick={() => setOpen(false)} className="text-gray-500 hover:text-white text-xs">&times;</button>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {sorted.map(p => (
                <span key={p} className="font-mono text-xs text-gray-300 bg-gray-700/50 px-1.5 py-0.5 rounded">
                  {p}
                </span>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  )
}

function StatCard({ label, value, color = 'text-white' }: { label: string; value: number | string; color?: string }) {
  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
      <p className="text-xs text-gray-500 uppercase">{label}</p>
      <p className={`text-2xl font-bold ${color}`}>{value}</p>
    </div>
  )
}

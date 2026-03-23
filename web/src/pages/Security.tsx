import { useRef, useState, useMemo, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { gradeColor, gradeFromScore, gradeStrokeColor } from '../utils/security'
import { type SecurityScoreResult } from '../types/models'
import SecurityScoreModal from '../components/SecurityScoreModal'

type SortKey = 'score' | 'failed_logins' | 'security_updates'
type SortDir = 'asc' | 'desc'

export default function Security() {
  const [sortKey, setSortKey] = useState<SortKey>('score')
  const [sortDir, setSortDir] = useState<SortDir>('asc')
  const [scoreModal, setScoreModal] = useState<{ score: SecurityScoreResult; hostname?: string } | null>(null)

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

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc')
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

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
          {overview.total_certs > 0 && (
            <StatCard
              label="Certs Expiring"
              value={overview.certs_expiring_soon}
              color={overview.certs_expiring_soon > 0 ? 'text-amber-400' : 'text-emerald-400'}
            />
          )}
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
      <SecurityTable
        devices={devices}
        sortKey={sortKey}
        sortDir={sortDir}
        toggleSort={toggleSort}
        onShowModal={setScoreModal}
      />

      {scoreModal && (
        <SecurityScoreModal
          score={scoreModal.score}
          hostname={scoreModal.hostname}
          onClose={() => setScoreModal(null)}
        />
      )}
    </div>
  )
}

// --- SecurityTable ---

interface DeviceRow {
  device_id: string
  hostname: string
  status: string
  selinux: string
  apparmor: string
  firewall_status: string
  failed_logins_24h: number
  logged_in_users: number
  open_ports: number[]
  pending_security_count: number
  unattended_upgrades: boolean | null
  certs_expiring_soon: number
}

interface SecurityTableProps {
  devices: DeviceRow[]
  sortKey: SortKey
  sortDir: SortDir
  toggleSort: (key: SortKey) => void
  onShowModal: (state: { score: SecurityScoreResult; hostname?: string } | null) => void
}

function SecurityTable({ devices, sortKey, sortDir, toggleSort, onShowModal }: SecurityTableProps) {
  // Each device's score is fetched via ScoreCell which owns its own useQuery.
  // We collect resolved scores via a shared map (updated during render) to compute the fleet average.
  // The pattern: render ScoreCell children, fleet average is derived from whatever React Query has cached.
  return (
    <SecurityTableInner
      devices={devices}
      sortKey={sortKey}
      sortDir={sortDir}
      toggleSort={toggleSort}
      onShowModal={onShowModal}
    />
  )
}

function SecurityTableInner({ devices, sortKey, sortDir, toggleSort, onShowModal }: SecurityTableProps) {
  // Collect scores from all ScoreCell hooks by rendering them and reading from cache.
  // We use a Fleet Average Row that reads from individual per-device score queries.
  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-x-auto scrollbar-thin">
      <div className="px-4 py-3 border-b border-gray-800">
        <h2 className="text-sm font-semibold text-gray-300 uppercase">Per-Device Security</h2>
      </div>
      <SortableSecurityTable
        devices={devices}
        sortKey={sortKey}
        sortDir={sortDir}
        toggleSort={toggleSort}
        onShowModal={onShowModal}
      />
    </div>
  )
}

function SortableSecurityTable({ devices, sortKey, sortDir, toggleSort, onShowModal }: SecurityTableProps) {
  // Scores are fetched per-device. We keep a ref-style map of scores for sorting.
  // ScoreCell components update this via a callback when their score resolves.
  const [scoreMap, setScoreMap] = useState<Map<string, SecurityScoreResult>>(new Map())

  const updateScore = (deviceId: string, score: SecurityScoreResult) => {
    setScoreMap(prev => {
      if (prev.get(deviceId) === score) return prev
      const next = new Map(prev)
      next.set(deviceId, score)
      return next
    })
  }

  const sorted = useMemo(() => {
    return [...devices].sort((a, b) => {
      let va: number
      let vb: number
      if (sortKey === 'score') {
        // Devices with no score sort to the bottom
        const sa = scoreMap.get(a.device_id)
        const sb = scoreMap.get(b.device_id)
        va = sa ? sa.overall_score : (sortDir === 'asc' ? Infinity : -Infinity)
        vb = sb ? sb.overall_score : (sortDir === 'asc' ? Infinity : -Infinity)
      } else if (sortKey === 'failed_logins') {
        va = a.failed_logins_24h
        vb = b.failed_logins_24h
      } else {
        va = a.pending_security_count
        vb = b.pending_security_count
      }
      return sortDir === 'asc' ? va - vb : vb - va
    })
  }, [devices, sortKey, sortDir, scoreMap])

  const fleetAvg = useMemo(() => {
    const scores = devices
      .map(d => scoreMap.get(d.device_id)?.overall_score)
      .filter((s): s is number => s !== undefined)
    if (scores.length === 0) return null
    return Math.round(scores.reduce((a, b) => a + b, 0) / scores.length)
  }, [devices, scoreMap])

  const SortHeader = ({ k, children, right }: { k: SortKey; children: React.ReactNode; right?: boolean }) => (
    <th
      className={`px-4 py-3 text-xs font-medium text-gray-400 uppercase tracking-wider cursor-pointer hover:text-gray-200 ${right ? 'text-right' : 'text-left'}`}
      onClick={() => toggleSort(k)}
    >
      {children} {sortKey === k ? (sortDir === 'asc' ? '\u2191' : '\u2193') : ''}
    </th>
  )

  return (
    <>
      {/* Fleet average score banner */}
      {fleetAvg !== null && (
        <div className="px-4 py-2 border-b border-gray-800 flex items-center gap-3">
          <span className="text-xs text-gray-500 uppercase">Fleet Score</span>
          <span className={`text-lg font-bold ${gradeColor(gradeFromScore(fleetAvg))}`}>{fleetAvg}</span>
          <span className={`text-xs font-semibold ${gradeColor(gradeFromScore(fleetAvg))}`}>{gradeFromScore(fleetAvg)}</span>
        </div>
      )}

      <table className="w-full text-sm min-w-[900px]">
        <thead>
          <tr className="text-left text-gray-400 text-xs uppercase border-b border-gray-800">
            <th className="px-4 py-3">Device</th>
            <SortHeader k="score">Score</SortHeader>
            <th className="px-4 py-3">Firewall</th>
            <th className="px-4 py-3">SELinux</th>
            <th className="px-4 py-3">AppArmor</th>
            <SortHeader k="security_updates" right>Sec. Updates</SortHeader>
            <th className="px-4 py-3 text-right">Auto-Updates</th>
            <SortHeader k="failed_logins" right>Failed Logins</SortHeader>
            <th className="px-4 py-3 text-right">Logged In</th>
            <th className="px-4 py-3 text-right">Open Ports</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-800/50">
          {sorted.map(d => (
            <tr key={d.device_id} className="text-gray-300">
              <td className="px-4 py-3">
                <Link to={`/devices/${d.device_id}`} className="text-blue-400 hover:text-blue-300 font-medium uppercase">
                  {d.hostname}
                </Link>
              </td>
              <td className="px-4 py-3">
                <ScoreCell
                  deviceId={d.device_id}
                  hostname={d.hostname}
                  onShowModal={onShowModal}
                  onScoreResolved={updateScore}
                />
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
                {d.unattended_upgrades === null ? (
                  <span className="text-gray-600">-</span>
                ) : d.pending_security_count > 0 ? (
                  <span className="text-red-400 font-medium">{d.pending_security_count}</span>
                ) : (
                  <span className="text-gray-500">0</span>
                )}
              </td>
              <td className="px-4 py-3 text-right">
                {d.unattended_upgrades === null ? (
                  <span className="text-gray-600">-</span>
                ) : d.unattended_upgrades ? (
                  <span className="text-emerald-400">Enabled</span>
                ) : (
                  <span className="text-amber-400">Disabled</span>
                )}
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
              <td colSpan={10} className="px-4 py-8 text-center text-gray-500">
                No security data available. Security info is collected from device telemetry.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </>
  )
}

// --- ScoreCell ---
// Individual per-device score query cell.

interface ScoreCellProps {
  deviceId: string
  hostname: string
  onShowModal: (state: { score: SecurityScoreResult; hostname?: string } | null) => void
  onScoreResolved: (deviceId: string, score: SecurityScoreResult) => void
}

function ScoreCell({ deviceId, hostname, onShowModal, onScoreResolved }: ScoreCellProps) {
  const { data: score } = useQuery({
    queryKey: ['security-score', deviceId],
    queryFn: () => api.getSecurityScore(deviceId),
    staleTime: 5 * 60_000,
  })

  // Notify the parent table of the resolved score outside of render
  useEffect(() => {
    if (score) {
      onScoreResolved(deviceId, score)
    }
  }, [deviceId, score, onScoreResolved])

  if (!score) {
    return <span className="text-gray-600">-</span>
  }

  const r = 10
  const circ = 2 * Math.PI * r
  const offset = circ * (1 - Math.max(0, Math.min(100, score.overall_score)) / 100)

  return (
    <button
      onClick={() => onShowModal({ score, hostname })}
      className="inline-flex items-center gap-1 group cursor-pointer"
      title={`Security: ${score.overall_score}/100 (${score.grade})`}
    >
      <svg viewBox="0 0 24 24" className="w-6 h-6 -rotate-90 flex-shrink-0">
        <circle cx="12" cy="12" r={r} fill="none" className="stroke-gray-700" strokeWidth="3" />
        <circle
          cx="12" cy="12" r={r}
          fill="none"
          className={`${gradeStrokeColor(score.grade)} transition-all duration-500`}
          strokeWidth="3"
          strokeLinecap="round"
          strokeDasharray={circ}
          strokeDashoffset={offset}
        />
      </svg>
      <span className={`text-xs font-semibold ${gradeColor(score.grade)} group-hover:brightness-125 transition`}>
        {score.overall_score}
      </span>
    </button>
  )
}

// --- PortsList ---

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
            className="fixed z-50 w-72 max-h-64 overflow-y-auto scrollbar-thin bg-gray-800 border border-gray-700 rounded-lg shadow-xl p-3"
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

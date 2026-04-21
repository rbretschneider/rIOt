import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'
import { settingsApi } from '../../api/settings'
import { useFeatures } from '../../hooks/useFeatures'
import { isVersionOlder } from '../../utils/version'
import type { AutomationConfig, MaintenanceWindow } from '../../types/models'

export default function AgentManagement() {
  const qc = useQueryClient()
  const { data: versions = [], isLoading } = useQuery({
    queryKey: ['agent-versions'],
    queryFn: api.getAgentVersions,
    refetchInterval: 30_000,
  })
  const { data: serverUpdate } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    staleTime: 60 * 60 * 1000,
  })

  const [updateResult, setUpdateResult] = useState<{ sent: number; skipped: number } | null>(null)

  const bulkMutation = useMutation({
    mutationFn: api.bulkUpdateAgents,
    onSuccess: (data) => {
      setUpdateResult(data)
      qc.invalidateQueries({ queryKey: ['agent-versions'] })
      setTimeout(() => setUpdateResult(null), 8000)
    },
  })

  const latestVersion = serverUpdate?.latest_version
  const totalDevices = versions.reduce((sum, v) => sum + v.count, 0)

  if (isLoading) return <div className="text-gray-400">Loading...</div>

  return (
    <div className="space-y-6">
      <h2 className="text-lg font-semibold text-white">Agent Version Management</h2>

      {/* Version Distribution */}
      <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
        <div className="px-4 py-3 border-b border-gray-800">
          <h3 className="text-sm font-semibold text-gray-300 uppercase">Version Distribution</h3>
        </div>
        <div className="p-4 space-y-3">
          {versions.length === 0 ? (
            <p className="text-gray-500 text-sm">No devices registered.</p>
          ) : (
            versions.map(v => {
              const pct = totalDevices > 0 ? (v.count / totalDevices) * 100 : 0
              const isLatest = latestVersion && v.version === latestVersion
              const isOutdated = latestVersion && v.version !== 'dev' && v.version !== 'unknown' && isVersionOlder(v.version, latestVersion)
              return (
                <div key={v.version}>
                  <div className="flex items-center justify-between mb-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-mono text-white">{v.version}</span>
                      {isLatest && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-emerald-900/50 text-emerald-400">latest</span>
                      )}
                      {isOutdated && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-amber-900/50 text-amber-400">outdated</span>
                      )}
                    </div>
                    <div className="flex items-center gap-3">
                      <span className="text-sm text-gray-400">
                        {v.count} device{v.count !== 1 ? 's' : ''}
                      </span>
                      {isOutdated && (
                        <button
                          onClick={() => {
                            if (confirm(`Send update command to all ${v.count} device(s) running ${v.version}?`))
                              bulkMutation.mutate(v.version)
                          }}
                          disabled={bulkMutation.isPending}
                          className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors disabled:opacity-50"
                        >
                          {bulkMutation.isPending ? 'Updating...' : 'Update All'}
                        </button>
                      )}
                    </div>
                  </div>
                  <div className="w-full bg-gray-800 rounded-full h-2">
                    <div
                      className={`h-2 rounded-full transition-all ${
                        isLatest ? 'bg-emerald-500' : isOutdated ? 'bg-amber-500' : 'bg-gray-600'
                      }`}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                </div>
              )
            })
          )}
        </div>
      </div>

      {/* Bulk Update Result */}
      {updateResult && (
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <p className="text-sm text-gray-300">
            Update command sent to <span className="text-white font-medium">{updateResult.sent}</span> device{updateResult.sent !== 1 ? 's' : ''}.
            {updateResult.skipped > 0 && (
              <span className="text-gray-500"> ({updateResult.skipped} skipped — offline or disconnected)</span>
            )}
          </p>
        </div>
      )}

      {/* Info */}
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
        <p className="text-sm text-gray-400">
          {latestVersion ? (
            <>Latest release: <span className="text-white font-mono">{latestVersion}</span></>
          ) : (
            'Unable to determine latest agent version.'
          )}
        </p>
        <p className="text-xs text-gray-500 mt-2">
          Agents check for updates at startup and can be updated via the "Update All" button or individually from the device detail page.
        </p>
      </div>

      {/* Automation Intervals */}
      <AutomationIntervals />
    </div>
  )
}

const PRESETS: { label: string; start: string; end: string }[] = [
  { label: 'Off-Hours', start: '23:00', end: '05:00' },
  { label: 'Midnight', start: '00:00', end: '02:00' },
  { label: 'Early Morning', start: '03:00', end: '06:00' },
  { label: 'Business Hours', start: '09:00', end: '17:00' },
]

const COOLDOWN_OPTIONS = [
  { label: '15 min', value: 15 },
  { label: '30 min', value: 30 },
  { label: '1 hour', value: 60 },
  { label: '2 hours', value: 120 },
  { label: '4 hours', value: 240 },
  { label: '6 hours', value: 360 },
  { label: '12 hours', value: 720 },
  { label: '24 hours', value: 1440 },
  { label: '2 days', value: 2880 },
  { label: '3 days', value: 4320 },
  { label: '1 week', value: 10080 },
  { label: '2 weeks', value: 20160 },
  { label: '30 days', value: 43200 },
]

// Docker-specific stagger presets — delay between successive auto-update
// dispatches on the same device. 0 disables staggering entirely (safe only
// when pulls go through a local registry cache).
const STAGGER_OPTIONS = [
  { label: 'Disabled (0s)', value: 0 },
  { label: '1 min', value: 60 },
  { label: '5 min', value: 300 },
  { label: '10 min (recommended)', value: 600 },
  { label: '15 min', value: 900 },
  { label: '30 min', value: 1800 },
  { label: '1 hour', value: 3600 },
  { label: '2 hours', value: 7200 },
]

// Local timezone abbreviation (e.g. "EST"), for labeling the UTC-stored time inputs.
function localTZLabel(): string {
  try {
    const parts = new Intl.DateTimeFormat('en-US', { timeZoneName: 'short' }).formatToParts(new Date())
    return parts.find(p => p.type === 'timeZoneName')?.value ?? 'local'
  } catch {
    return 'local'
  }
}

// Convert "HH:MM" in UTC to "HH:MM" in the user's local timezone.
function utcTimeToLocal(hhmm: string): string {
  if (!/^\d{1,2}:\d{2}$/.test(hhmm)) return hhmm
  const [h, m] = hhmm.split(':').map(Number)
  const d = new Date()
  d.setUTCHours(h, m, 0, 0)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// Convert "HH:MM" in the user's local timezone to "HH:MM" UTC (storage format).
function localTimeToUTC(hhmm: string): string {
  if (!/^\d{1,2}:\d{2}$/.test(hhmm)) return hhmm
  const [h, m] = hhmm.split(':').map(Number)
  const d = new Date()
  d.setHours(h, m, 0, 0)
  return `${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}`
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  const mins = Math.round(seconds / 60)
  if (mins < 60) return `${mins} min`
  const hrs = mins / 60
  if (hrs < 24) return `${hrs.toFixed(hrs % 1 === 0 ? 0 : 1)} hr`
  const days = hrs / 24
  return `${days.toFixed(days % 1 === 0 ? 0 : 1)} days`
}

function AutomationIntervals() {
  const qc = useQueryClient()
  const { data: config, isLoading } = useQuery({
    queryKey: ['automation-config'],
    queryFn: settingsApi.getAutomationConfig,
  })
  const [draft, setDraft] = useState<AutomationConfig | null>(null)
  const [saved, setSaved] = useState(false)

  const saveMutation = useMutation({
    mutationFn: settingsApi.saveAutomationConfig,
    onSuccess: (data) => {
      qc.setQueryData(['automation-config'], data)
      setDraft(null)
      setSaved(true)
      setTimeout(() => setSaved(false), 3000)
    },
  })

  const { isEnabled } = useFeatures()
  const dockerEnabled = isEnabled('docker')

  if (isLoading || !config) return null

  const current = draft ?? config
  const isDirty = draft !== null

  function updateWindow(key: 'os_patch' | 'docker_update', patch: Partial<MaintenanceWindow>) {
    const base = draft ?? config!
    setDraft({
      ...base,
      [key]: { ...base[key], ...patch },
    })
  }

  return (
    <>
      <h2 className="text-lg font-semibold text-white">Automation Intervals</h2>
      <p className="text-xs text-gray-500 -mt-4">
        Control when automatic OS patching{dockerEnabled ? ' and Docker container updates' : ''} are allowed to run. Times are shown in your local timezone ({localTZLabel()}) and stored internally as UTC.
      </p>

      <div className={`grid grid-cols-1 ${dockerEnabled ? 'lg:grid-cols-2' : ''} gap-6`}>
        <WindowCard
          title="OS Auto-Patch"
          description="Automatically apply OS security patches when updates are detected on devices with auto-patch enabled."
          window={current.os_patch}
          onChange={patch => updateWindow('os_patch', patch)}
        />
        {dockerEnabled && (
          <WindowCard
            title="Docker Auto-Update"
            description="Automatically update Docker containers when newer images are available, per container/stack policies."
            window={current.docker_update}
            onChange={patch => updateWindow('docker_update', patch)}
            showStagger
          />
        )}
      </div>

      {dockerEnabled && <DockerPullRateLimitHelp window={current.docker_update} />}

      {/* Save bar */}
      <div className="flex items-center gap-3">
        <button
          onClick={() => saveMutation.mutate(current)}
          disabled={!isDirty || saveMutation.isPending}
          className="px-4 py-2 text-sm font-medium text-white bg-blue-600 hover:bg-blue-500 rounded-md transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {saveMutation.isPending ? 'Saving...' : 'Save Changes'}
        </button>
        {isDirty && (
          <button
            onClick={() => setDraft(null)}
            className="px-3 py-2 text-sm text-gray-400 hover:text-white transition-colors"
          >
            Discard
          </button>
        )}
        {saved && <span className="text-sm text-emerald-400">Saved</span>}
        {saveMutation.isError && (
          <span className="text-sm text-red-400">
            Failed: {(saveMutation.error as Error).message}
          </span>
        )}
      </div>
    </>
  )
}

function WindowCard({ title, description, window: w, onChange, showStagger }: {
  title: string
  description: string
  window: MaintenanceWindow
  onChange: (patch: Partial<MaintenanceWindow>) => void
  showStagger?: boolean
}) {
  // Presets are authored as local-time values and compared against the stored
  // UTC window by converting both sides to UTC.
  const activePreset = w.mode === 'window'
    ? PRESETS.find(p => localTimeToUTC(p.start) === w.start_time && localTimeToUTC(p.end) === w.end_time)
    : null
  const tzLabel = localTZLabel()

  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
      <div className="px-4 py-3 border-b border-gray-800">
        <h3 className="text-sm font-semibold text-gray-300 uppercase">{title}</h3>
        <p className="text-xs text-gray-500 mt-0.5">{description}</p>
      </div>
      <div className="p-4 space-y-4">
        {/* Mode selector */}
        <div>
          <label className="text-xs text-gray-500 block mb-1.5">Schedule</label>
          <div className="flex gap-2">
            {(['anytime', 'window', 'disabled'] as const).map(mode => (
              <button
                key={mode}
                onClick={() => onChange({ mode })}
                className={`px-3 py-1.5 text-xs rounded-md transition-colors ${
                  w.mode === mode
                    ? mode === 'disabled'
                      ? 'bg-red-500/20 text-red-400 border border-red-600/50'
                      : 'bg-blue-500/20 text-blue-400 border border-blue-600/50'
                    : 'bg-gray-800 text-gray-400 hover:text-white border border-gray-700'
                }`}
              >
                {mode === 'anytime' ? 'Anytime' : mode === 'window' ? 'Scheduled Window' : 'Disabled'}
              </button>
            ))}
          </div>
        </div>

        {/* Window config */}
        {w.mode === 'window' && (
          <>
            {/* Presets */}
            <div>
              <label className="text-xs text-gray-500 block mb-1.5">Quick Select</label>
              <div className="flex flex-wrap gap-1.5">
                {PRESETS.map(p => (
                  <button
                    key={p.label}
                    onClick={() => onChange({ start_time: localTimeToUTC(p.start), end_time: localTimeToUTC(p.end) })}
                    className={`px-2.5 py-1 text-xs rounded-md transition-colors ${
                      activePreset?.label === p.label
                        ? 'bg-blue-500/20 text-blue-400 border border-blue-600/50'
                        : 'bg-gray-800 text-gray-400 hover:text-white border border-gray-700'
                    }`}
                  >
                    {p.label}
                    <span className="text-gray-600 ml-1">({p.start}–{p.end})</span>
                  </button>
                ))}
              </div>
            </div>

            {/* Custom time inputs — displayed in local time, stored as UTC */}
            <div className="flex items-center gap-3">
              <div>
                <label className="text-xs text-gray-500 block mb-1">Start ({tzLabel})</label>
                <input
                  type="time"
                  value={utcTimeToLocal(w.start_time)}
                  onChange={e => onChange({ start_time: localTimeToUTC(e.target.value) })}
                  className="px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 font-mono"
                />
              </div>
              <span className="text-gray-600 mt-5">to</span>
              <div>
                <label className="text-xs text-gray-500 block mb-1">End ({tzLabel})</label>
                <input
                  type="time"
                  value={utcTimeToLocal(w.end_time)}
                  onChange={e => onChange({ end_time: localTimeToUTC(e.target.value) })}
                  className="px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 font-mono"
                />
              </div>
            </div>
          </>
        )}

        {/* Cooldown */}
        <div>
          <label className="text-xs text-gray-500 block mb-1.5">
            Cooldown Per Target
            <span className="text-gray-600 ml-1">(minimum time between re-runs of the same container/stack)</span>
          </label>
          <div className="flex items-center gap-2">
            <select
              value={COOLDOWN_OPTIONS.find(o => o.value === w.cooldown_minutes)?.value ?? ''}
              onChange={e => onChange({ cooldown_minutes: parseInt(e.target.value) })}
              disabled={w.mode === 'disabled'}
              className="px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 disabled:opacity-50"
            >
              {!COOLDOWN_OPTIONS.some(o => o.value === w.cooldown_minutes) && (
                <option value="" disabled>Custom</option>
              )}
              {COOLDOWN_OPTIONS.map(opt => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
            <input
              type="number"
              min={1}
              value={w.cooldown_minutes}
              onChange={e => onChange({ cooldown_minutes: Math.max(1, parseInt(e.target.value) || 1) })}
              disabled={w.mode === 'disabled'}
              className="w-24 px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 disabled:opacity-50 font-mono"
              aria-label="Custom cooldown in minutes"
            />
            <span className="text-xs text-gray-500">min</span>
          </div>
        </div>

        {/* Stagger — only shown for docker_update */}
        {showStagger && (
          <div>
            <label className="text-xs text-gray-500 block mb-1.5">
              Stagger Between Pulls
              <span className="text-gray-600 ml-1">(delay between consecutive pulls on the same device; 0 disables)</span>
            </label>
            <div className="flex items-center gap-2">
              <select
                value={STAGGER_OPTIONS.find(o => o.value === w.stagger_seconds)?.value ?? ''}
                onChange={e => onChange({ stagger_seconds: parseInt(e.target.value) })}
                disabled={w.mode === 'disabled'}
                className="px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 disabled:opacity-50"
              >
                {!STAGGER_OPTIONS.some(o => o.value === w.stagger_seconds) && (
                  <option value="" disabled>Custom</option>
                )}
                {STAGGER_OPTIONS.map(opt => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </select>
              <input
                type="number"
                min={0}
                value={w.stagger_seconds}
                onChange={e => onChange({ stagger_seconds: Math.max(0, parseInt(e.target.value) || 0) })}
                disabled={w.mode === 'disabled'}
                className="w-24 px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 disabled:opacity-50 font-mono"
                aria-label="Custom stagger in seconds"
              />
              <span className="text-xs text-gray-500">sec</span>
            </div>
            <p className="text-xs text-gray-500 mt-1.5">
              Spreads pulls across time so fleets with many containers don't hit Docker Hub's 100-pull-per-6h limit. Set to 0 if you have a local registry pull-through cache.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}

function DockerPullRateLimitHelp({ window: w }: { window: MaintenanceWindow }) {
  const { data: devices = [] } = useQuery({
    queryKey: ['devices'],
    queryFn: api.getDevices,
    staleTime: 60 * 1000,
  })

  const byDevice = devices
    .map(d => ({ name: d.hostname, count: d.docker_auto_update_container_count }))
    .filter(d => d.count > 0)
    .sort((a, b) => b.count - a.count)

  const maxCount = byDevice.length > 0 ? byDevice[0].count : 0
  const passSeconds = maxCount * Math.max(w.stagger_seconds, 1)
  const estimate = maxCount > 0 && w.stagger_seconds > 0
    ? `On your busiest device (${byDevice[0].name}: ${maxCount} auto-update targets), a full update pass takes ~${formatDuration(passSeconds)}.`
    : null
  const exceeds6h = maxCount > 0 && w.stagger_seconds > 0 && passSeconds <= 6 * 3600 && maxCount > 100
  const safeIn6h = w.stagger_seconds > 0 ? Math.floor((6 * 3600) / w.stagger_seconds) : 0

  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 space-y-2">
      <h3 className="text-sm font-semibold text-gray-300 uppercase">Docker Hub Rate Limits</h3>
      <p className="text-xs text-gray-400">
        Docker Hub throttles unauthenticated pulls to <span className="text-white">100 per 6&nbsp;hours per IP</span> (200 for authenticated free accounts).
        With many auto-update containers on a single host, uncontrolled updates can exhaust this quota within minutes and leave all pulls failing.
      </p>
      {w.mode !== 'disabled' && w.stagger_seconds > 0 && (
        <p className="text-xs text-gray-400">
          Current settings allow at most <span className="text-white">{safeIn6h}</span> pulls per 6&nbsp;hours per device
          (one every {formatDuration(w.stagger_seconds)}).
          {exceeds6h && (
            <span className="text-amber-400"> Some devices have more auto-update targets than this window allows — the oldest will carry over to the next 6h window.</span>
          )}
        </p>
      )}
      {w.mode !== 'disabled' && w.stagger_seconds === 0 && (
        <p className="text-xs text-amber-400">
          Staggering is disabled — all eligible containers will pull in parallel each pass. Safe only if pulls go through a local registry cache.
        </p>
      )}
      {estimate && <p className="text-xs text-gray-500">{estimate}</p>}
      {byDevice.length > 0 && (
        <div className="pt-1">
          <p className="text-xs text-gray-500 mb-1">Auto-update targets by device:</p>
          <div className="grid grid-cols-2 gap-x-4 gap-y-0.5 text-xs">
            {byDevice.slice(0, 8).map(d => (
              <div key={d.name} className="flex justify-between">
                <span className="text-gray-400 truncate">{d.name}</span>
                <span className="text-gray-500 font-mono ml-2">{d.count}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

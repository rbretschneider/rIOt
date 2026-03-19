import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'
import { settingsApi } from '../../api/settings'
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
]

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
        Control when automatic OS patching and Docker container updates are allowed to run. Times are in UTC.
      </p>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <WindowCard
          title="OS Auto-Patch"
          description="Automatically apply OS security patches when updates are detected on devices with auto-patch enabled."
          window={current.os_patch}
          onChange={patch => updateWindow('os_patch', patch)}
        />
        <WindowCard
          title="Docker Auto-Update"
          description="Automatically update Docker containers when newer images are available, per container/stack policies."
          window={current.docker_update}
          onChange={patch => updateWindow('docker_update', patch)}
        />
      </div>

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

function WindowCard({ title, description, window: w, onChange }: {
  title: string
  description: string
  window: MaintenanceWindow
  onChange: (patch: Partial<MaintenanceWindow>) => void
}) {
  const activePreset = w.mode === 'window'
    ? PRESETS.find(p => p.start === w.start_time && p.end === w.end_time)
    : null

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
                    onClick={() => onChange({ start_time: p.start, end_time: p.end })}
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

            {/* Custom time inputs */}
            <div className="flex items-center gap-3">
              <div>
                <label className="text-xs text-gray-500 block mb-1">Start (UTC)</label>
                <input
                  type="time"
                  value={w.start_time}
                  onChange={e => onChange({ start_time: e.target.value })}
                  className="px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 font-mono"
                />
              </div>
              <span className="text-gray-600 mt-5">to</span>
              <div>
                <label className="text-xs text-gray-500 block mb-1">End (UTC)</label>
                <input
                  type="time"
                  value={w.end_time}
                  onChange={e => onChange({ end_time: e.target.value })}
                  className="px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 font-mono"
                />
              </div>
            </div>
          </>
        )}

        {/* Cooldown */}
        <div>
          <label className="text-xs text-gray-500 block mb-1.5">Cooldown Between Runs</label>
          <select
            value={w.cooldown_minutes}
            onChange={e => onChange({ cooldown_minutes: parseInt(e.target.value) })}
            disabled={w.mode === 'disabled'}
            className="px-2 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 focus:outline-none focus:border-gray-500 disabled:opacity-50"
          >
            {COOLDOWN_OPTIONS.map(opt => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
        </div>
      </div>
    </div>
  )
}

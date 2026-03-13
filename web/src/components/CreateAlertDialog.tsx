import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../api/settings'
import type { AlertRule } from '../types/models'

interface CreateAlertDialogProps {
  metric: string
  targetName: string
  targetState?: string
  deviceFilter?: string
  onClose: () => void
}

const SEVERITIES = ['info', 'warning', 'critical']

const TARGET_STATES: Record<string, string[]> = {
  service_state: ['stopped', 'failed', 'inactive', 'dead'],
  nic_state: ['DOWN', 'LOWER_LAYER_DOWN', 'DORMANT', 'UNKNOWN', 'NO-CARRIER'],
  process_missing: ['absent'],
  usb_missing: ['absent'],
}

const TARGET_STATE_DEFAULTS: Record<string, string[]> = {
  service_state: ['stopped', 'failed'],
  nic_state: ['DOWN', 'LOWER_LAYER_DOWN', 'NO-CARRIER'],
  process_missing: ['absent'],
  usb_missing: ['absent'],
}

const METRIC_DEFAULTS: Record<string, { operator: string; threshold: number; severity: string; cooldown: number; hint: string }> = {
  mem_percent:     { operator: '>', threshold: 90, severity: 'warning',  cooldown: 3600, hint: 'Memory usage percentage (0–100)' },
  disk_percent:    { operator: '>', threshold: 90, severity: 'critical', cooldown: 3600, hint: 'Disk usage percentage (0–100)' },
  log_errors:      { operator: '>', threshold: 0,  severity: 'warning',  cooldown: 900,  hint: 'Number of error-level log entries since last heartbeat' },
  container_died:  { operator: '==', threshold: 1, severity: 'warning',  cooldown: 900,  hint: 'Fires when a container exits unexpectedly' },
  container_oom:   { operator: '==', threshold: 1, severity: 'critical', cooldown: 900,  hint: 'Fires when a container is OOM killed' },
  device_offline:  { operator: '==', threshold: 1, severity: 'warning',  cooldown: 300,  hint: 'Fires when a device stops sending heartbeats' },
  ups_on_battery:  { operator: '==', threshold: 1, severity: 'critical', cooldown: 900,  hint: 'Fires when UPS switches to battery power' },
  ups_battery_percent: { operator: '<', threshold: 20, severity: 'critical', cooldown: 300, hint: 'UPS battery charge percentage (0–100)' },
  usb_missing:     { operator: '==', threshold: 1, severity: 'critical', cooldown: 300, hint: 'Fires when the USB device is not found' },
}

export default function CreateAlertDialog({ metric, targetName, targetState, deviceFilter, onClose }: CreateAlertDialogProps) {
  const qc = useQueryClient()
  const isState = ['service_state', 'nic_state', 'process_missing', 'usb_missing'].includes(metric)
  const defaults = METRIC_DEFAULTS[metric]

  const metricLabels: Record<string, string> = {
    service_state: 'Service State',
    nic_state: 'NIC State',
    process_missing: 'Process Missing',
    log_errors: 'Log Errors',
    ups_on_battery: 'UPS On Battery',
    ups_battery_percent: 'UPS Battery %',
    usb_missing: 'USB Device Missing',
  }

  // Use provided targetState or sensible defaults
  const defaultStates = targetState
    ? targetState
    : (TARGET_STATE_DEFAULTS[metric] || []).join(',')

  const [rule, setRule] = useState<Partial<AlertRule>>({
    name: `${metricLabels[metric] || metric}: ${targetName}`,
    enabled: true,
    metric,
    operator: isState ? '==' : (defaults?.operator ?? '>'),
    threshold: isState ? 1 : (defaults?.threshold ?? 90),
    target_name: targetName,
    target_state: defaultStates,
    severity: defaults?.severity ?? 'warning',
    device_filter: deviceFilter || '',
    cooldown_seconds: defaults?.cooldown ?? 900,
    notify: true,
    template_id: '',
  })

  const mutation = useMutation({
    mutationFn: (r: Partial<AlertRule>) => settingsApi.createAlertRule(r),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['alert-rules'] })
      onClose()
    },
  })

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-md mx-4 max-h-[90vh] flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between p-6 pb-0">
          <h3 className="text-lg font-semibold text-white">Create Alert Rule</h3>
          <button onClick={onClose} className="text-gray-500 hover:text-white transition-colors">
            <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
              <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
            </svg>
          </button>
        </div>
        <div className="overflow-y-auto scrollbar-thin p-6 pt-4">
        <div className="space-y-3">
          <div>
            <label className="block text-xs text-gray-400 mb-1">Name</label>
            <input
              value={rule.name || ''}
              onChange={e => setRule({ ...rule, name: e.target.value })}
              className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-gray-400 mb-1">Metric</label>
              <div className="bg-gray-800 border border-gray-700 rounded px-3 py-2 text-gray-400 text-sm">
                {metricLabels[metric] || metric}
              </div>
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">Target</label>
              <div className="bg-gray-800 border border-gray-700 rounded px-3 py-2 text-gray-400 text-sm">
                {targetName}
              </div>
            </div>
          </div>
          {isState && TARGET_STATES[metric] && (
            <div>
              <label className="block text-xs text-gray-400 mb-1">Alert on States</label>
              <div className="bg-gray-800 border border-gray-700 rounded px-3 py-2">
                {TARGET_STATES[metric].length > 1 && (
                  <label className="flex items-center gap-2 text-xs text-gray-400 mb-1.5 pb-1.5 border-b border-gray-700 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={TARGET_STATES[metric].every(s => (rule.target_state || '').split(',').includes(s))}
                      onChange={() => {
                        const all = TARGET_STATES[metric].every(s => (rule.target_state || '').split(',').includes(s))
                        setRule({ ...rule, target_state: all ? '' : TARGET_STATES[metric].join(',') })
                      }}
                      className="rounded bg-gray-700 border-gray-600 text-blue-500"
                    />
                    Any (all states)
                  </label>
                )}
                <div className="flex flex-wrap gap-x-3 gap-y-1">
                  {TARGET_STATES[metric].map(s => {
                    const selected = (rule.target_state || '').split(',').filter(Boolean)
                    return (
                      <label key={s} className="flex items-center gap-1.5 text-sm text-white cursor-pointer">
                        <input
                          type="checkbox"
                          checked={selected.includes(s)}
                          onChange={() => {
                            const next = selected.includes(s)
                              ? selected.filter(x => x !== s)
                              : [...selected, s]
                            setRule({ ...rule, target_state: next.join(',') })
                          }}
                          className="rounded bg-gray-700 border-gray-600 text-blue-500"
                        />
                        {s}
                      </label>
                    )
                  })}
                </div>
              </div>
            </div>
          )}
          {!isState && defaults?.hint && (
            <div>
              <label className="block text-xs text-gray-400 mb-1">Condition</label>
              <div className="flex gap-2">
                <select
                  value={rule.operator}
                  onChange={e => setRule({ ...rule, operator: e.target.value })}
                  className="bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm w-20"
                >
                  {['>', '>=', '<', '<=', '==', '!='].map(op => <option key={op} value={op}>{op}</option>)}
                </select>
                <input
                  type="number"
                  value={rule.threshold ?? 0}
                  onChange={e => setRule({ ...rule, threshold: parseFloat(e.target.value) || 0 })}
                  className="flex-1 bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                />
              </div>
              <p className="text-xs text-gray-500 mt-1">{defaults.hint}</p>
            </div>
          )}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-gray-400 mb-1">Severity</label>
              <select
                value={rule.severity}
                onChange={e => setRule({ ...rule, severity: e.target.value })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
              >
                {SEVERITIES.map(s => <option key={s} value={s}>{s}</option>)}
              </select>
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">Cooldown (seconds)</label>
              <input
                type="number"
                value={rule.cooldown_seconds ?? 900}
                onChange={e => setRule({ ...rule, cooldown_seconds: parseInt(e.target.value) || 900 })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
              />
            </div>
          </div>
          <label className="flex items-center gap-2 text-sm text-gray-300">
            <input
              type="checkbox"
              checked={rule.notify ?? true}
              onChange={e => setRule({ ...rule, notify: e.target.checked })}
              className="rounded bg-gray-800 border-gray-600"
            />
            Send notifications
          </label>
        </div>
        </div>
        <div className="flex justify-end gap-3 p-6 pt-0">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-400 hover:text-white">Cancel</button>
          <button
            onClick={() => mutation.mutate(rule)}
            disabled={mutation.isPending}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors disabled:opacity-50"
          >
            {mutation.isPending ? 'Creating...' : 'Create Rule'}
          </button>
        </div>
        {mutation.isError && (
          <p className="text-xs text-red-400 px-6 pb-4">{(mutation.error as Error).message}</p>
        )}
      </div>
    </div>
  )
}

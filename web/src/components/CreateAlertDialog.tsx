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

export default function CreateAlertDialog({ metric, targetName, targetState, deviceFilter, onClose }: CreateAlertDialogProps) {
  const qc = useQueryClient()
  const isState = ['service_state', 'nic_state', 'process_missing'].includes(metric)

  const metricLabels: Record<string, string> = {
    service_state: 'Service State',
    nic_state: 'NIC State',
    process_missing: 'Process Missing',
  }

  const [rule, setRule] = useState<Partial<AlertRule>>({
    name: `${metricLabels[metric] || metric}: ${targetName}`,
    enabled: true,
    metric,
    operator: isState ? '==' : '>',
    threshold: isState ? 1 : 90,
    target_name: targetName,
    target_state: targetState || '',
    severity: 'warning',
    device_filter: deviceFilter || '',
    cooldown_seconds: 900,
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
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-md p-6" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-white mb-4">Create Alert Rule</h3>
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
        <div className="flex justify-end gap-3 mt-6">
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
          <p className="text-xs text-red-400 mt-2">{(mutation.error as Error).message}</p>
        )}
      </div>
    </div>
  )
}

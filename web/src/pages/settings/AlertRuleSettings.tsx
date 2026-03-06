import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'
import type { AlertRule, AlertTemplate } from '../../types/models'

const METRICS = [
  { value: 'mem_percent', label: 'Memory %' },
  { value: 'disk_percent', label: 'Disk %' },
  { value: 'container_died', label: 'Container Died' },
  { value: 'container_oom', label: 'Container OOM' },
  { value: 'device_offline', label: 'Device Offline' },
  { value: 'service_state', label: 'Service State' },
  { value: 'nic_state', label: 'NIC State' },
  { value: 'process_missing', label: 'Process Missing' },
]

const STATE_METRICS = ['service_state', 'nic_state', 'process_missing']

const TARGET_STATES: Record<string, string[]> = {
  service_state: ['stopped', 'failed', 'inactive', 'dead'],
  nic_state: ['DOWN'],
  process_missing: ['absent'],
}

const OPERATORS = ['>', '<', '>=', '<=', '==', '!=']
const SEVERITIES = ['info', 'warning', 'critical']

const emptyRule: Partial<AlertRule> = {
  name: '',
  enabled: true,
  metric: 'mem_percent',
  operator: '>',
  threshold: 90,
  target_name: '',
  target_state: '',
  severity: 'warning',
  device_filter: '',
  cooldown_seconds: 900,
  notify: true,
  template_id: '',
}

export default function AlertRuleSettings() {
  const qc = useQueryClient()
  const { data: rules = [], isLoading } = useQuery({
    queryKey: ['alert-rules'],
    queryFn: settingsApi.getAlertRules,
  })
  const [editing, setEditing] = useState<Partial<AlertRule> | null>(null)
  const [isNew, setIsNew] = useState(false)
  const [showTemplates, setShowTemplates] = useState(false)

  const saveMutation = useMutation({
    mutationFn: (rule: Partial<AlertRule>) =>
      isNew
        ? settingsApi.createAlertRule(rule)
        : settingsApi.updateAlertRule(rule.id!, rule),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['alert-rules'] })
      setEditing(null)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => settingsApi.deleteAlertRule(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['alert-rules'] }),
  })

  const toggleMutation = useMutation({
    mutationFn: (rule: AlertRule) =>
      settingsApi.updateAlertRule(rule.id, { ...rule, enabled: !rule.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['alert-rules'] }),
  })

  const isStateMetric = editing ? STATE_METRICS.includes(editing.metric || '') : false

  if (isLoading) {
    return <div className="text-gray-400">Loading...</div>
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">Alert Rules</h2>
        <div className="flex gap-2">
          <button
            onClick={() => setShowTemplates(true)}
            className="px-3 py-1.5 bg-gray-800 hover:bg-gray-700 text-gray-300 text-sm rounded-md transition-colors"
          >
            Create from Template
          </button>
          <button
            onClick={() => { setEditing({ ...emptyRule }); setIsNew(true) }}
            className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
          >
            Add Rule
          </button>
        </div>
      </div>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-x-auto">
        <table className="w-full text-sm min-w-[640px]">
          <thead>
            <tr className="text-left text-gray-400 border-b border-gray-800">
              <th className="px-4 py-3">Enabled</th>
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Metric</th>
              <th className="px-4 py-3">Condition</th>
              <th className="px-4 py-3">Severity</th>
              <th className="px-4 py-3">Cooldown</th>
              <th className="px-4 py-3">Notify</th>
              <th className="px-4 py-3"></th>
            </tr>
          </thead>
          <tbody>
            {rules.map(rule => (
              <tr key={rule.id} className="border-b border-gray-800/50 text-gray-300">
                <td className="px-4 py-3">
                  <button
                    onClick={() => toggleMutation.mutate(rule)}
                    className={`w-8 h-4 rounded-full transition-colors relative ${
                      rule.enabled ? 'bg-emerald-600' : 'bg-gray-600'
                    }`}
                  >
                    <span className={`absolute top-0.5 w-3 h-3 rounded-full bg-white transition-transform ${
                      rule.enabled ? 'left-4' : 'left-0.5'
                    }`} />
                  </button>
                </td>
                <td className="px-4 py-3 text-white">{rule.name}</td>
                <td className="px-4 py-3">
                  {METRICS.find(m => m.value === rule.metric)?.label || rule.metric}
                  {rule.target_name && <span className="text-gray-500 ml-1">({rule.target_name})</span>}
                </td>
                <td className="px-4 py-3 font-mono text-xs">
                  {STATE_METRICS.includes(rule.metric)
                    ? (rule.target_state || 'any')
                    : `${rule.operator} ${rule.threshold}`}
                </td>
                <td className="px-4 py-3">
                  <span className={`px-2 py-0.5 rounded text-xs ${
                    rule.severity === 'critical' ? 'bg-red-900/50 text-red-400' :
                    rule.severity === 'warning' ? 'bg-amber-900/50 text-amber-400' :
                    'bg-blue-900/50 text-blue-400'
                  }`}>
                    {rule.severity}
                  </span>
                </td>
                <td className="px-4 py-3 text-gray-400">{formatCooldown(rule.cooldown_seconds)}</td>
                <td className="px-4 py-3">{rule.notify ? 'Yes' : 'No'}</td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => { setEditing({ ...rule }); setIsNew(false) }}
                    className="text-gray-400 hover:text-white mr-2"
                  >
                    Edit
                  </button>
                  <button
                    onClick={() => { if (confirm('Delete this rule?')) deleteMutation.mutate(rule.id) }}
                    className="text-gray-400 hover:text-red-400"
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
            {rules.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-gray-500">
                  No alert rules configured. Click "Add Rule" to create one.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* Edit / Create Modal */}
      {editing && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setEditing(null)}>
          <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg p-6" onClick={e => e.stopPropagation()}>
            <h3 className="text-lg font-semibold text-white mb-4">
              {isNew ? 'Create Alert Rule' : 'Edit Alert Rule'}
            </h3>
            <div className="space-y-4">
              <Field label="Name">
                <input
                  value={editing.name || ''}
                  onChange={e => setEditing({ ...editing, name: e.target.value })}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                />
              </Field>
              <div className="grid grid-cols-3 gap-3">
                <Field label="Metric">
                  <select
                    value={editing.metric}
                    onChange={e => {
                      const m = e.target.value
                      const isState = STATE_METRICS.includes(m)
                      setEditing({
                        ...editing,
                        metric: m,
                        operator: isState ? '==' : '>',
                        threshold: isState ? 1 : 90,
                        target_state: isState ? (TARGET_STATES[m]?.[0] || '') : '',
                      })
                    }}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  >
                    {METRICS.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
                  </select>
                </Field>
                {isStateMetric ? (
                  <>
                    <Field label="Target Name">
                      <input
                        value={editing.target_name || ''}
                        onChange={e => setEditing({ ...editing, target_name: e.target.value })}
                        placeholder="e.g. nginx, eth0"
                        className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      />
                    </Field>
                    <Field label="Target State">
                      <select
                        value={editing.target_state || ''}
                        onChange={e => setEditing({ ...editing, target_state: e.target.value })}
                        className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      >
                        {(TARGET_STATES[editing.metric || ''] || []).map(s =>
                          <option key={s} value={s}>{s}</option>
                        )}
                      </select>
                    </Field>
                  </>
                ) : (
                  <>
                    <Field label="Operator">
                      <select
                        value={editing.operator}
                        onChange={e => setEditing({ ...editing, operator: e.target.value })}
                        className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      >
                        {OPERATORS.map(op => <option key={op} value={op}>{op}</option>)}
                      </select>
                    </Field>
                    <Field label="Threshold">
                      <input
                        type="number"
                        value={editing.threshold ?? 0}
                        onChange={e => setEditing({ ...editing, threshold: parseFloat(e.target.value) || 0 })}
                        className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      />
                    </Field>
                  </>
                )}
              </div>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Severity">
                  <select
                    value={editing.severity}
                    onChange={e => setEditing({ ...editing, severity: e.target.value })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  >
                    {SEVERITIES.map(s => <option key={s} value={s}>{s}</option>)}
                  </select>
                </Field>
                <Field label="Cooldown (seconds)">
                  <input
                    type="number"
                    value={editing.cooldown_seconds ?? 900}
                    onChange={e => setEditing({ ...editing, cooldown_seconds: parseInt(e.target.value) || 900 })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  />
                </Field>
              </div>
              <Field label="Device Filter (comma-separated IDs, empty = all)">
                <input
                  value={editing.device_filter || ''}
                  onChange={e => setEditing({ ...editing, device_filter: e.target.value })}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  placeholder="Leave empty for all devices"
                />
              </Field>
              <label className="flex items-center gap-2 text-sm text-gray-300">
                <input
                  type="checkbox"
                  checked={editing.notify ?? true}
                  onChange={e => setEditing({ ...editing, notify: e.target.checked })}
                  className="rounded bg-gray-800 border-gray-600"
                />
                Send notifications when triggered
              </label>
            </div>
            <div className="flex justify-end gap-3 mt-6">
              <button
                onClick={() => setEditing(null)}
                className="px-4 py-2 text-sm text-gray-400 hover:text-white"
              >
                Cancel
              </button>
              <button
                onClick={() => saveMutation.mutate(editing)}
                disabled={saveMutation.isPending}
                className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors disabled:opacity-50"
              >
                {saveMutation.isPending ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Template Picker Modal */}
      {showTemplates && (
        <TemplatePicker
          onSelect={(tpl) => {
            setShowTemplates(false)
            setEditing({
              ...emptyRule,
              name: tpl.name,
              metric: tpl.metric,
              operator: tpl.operator,
              threshold: tpl.threshold,
              target_state: tpl.target_state || '',
              severity: tpl.severity,
              cooldown_seconds: tpl.cooldown_seconds,
              template_id: tpl.id,
            })
            setIsNew(true)
          }}
          onClose={() => setShowTemplates(false)}
        />
      )}
    </div>
  )
}

function TemplatePicker({ onSelect, onClose }: { onSelect: (t: AlertTemplate) => void; onClose: () => void }) {
  const { data: templates = [] } = useQuery({
    queryKey: ['alert-templates'],
    queryFn: settingsApi.getAlertTemplates,
  })

  const categories = [...new Set(templates.map(t => t.category))]

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg p-6 max-h-[80vh] overflow-y-auto" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-white mb-4">Create from Template</h3>
        {categories.map(cat => (
          <div key={cat} className="mb-4">
            <h4 className="text-xs font-medium text-gray-400 uppercase mb-2">{cat}</h4>
            <div className="space-y-2">
              {templates.filter(t => t.category === cat).map(tpl => (
                <button
                  key={tpl.id}
                  onClick={() => onSelect(tpl)}
                  className="w-full text-left px-4 py-3 bg-gray-800 hover:bg-gray-700 rounded-lg transition-colors"
                >
                  <div className="text-sm text-white font-medium">{tpl.name}</div>
                  <div className="text-xs text-gray-400 mt-1">{tpl.description}</div>
                </button>
              ))}
            </div>
          </div>
        ))}
        <div className="flex justify-end mt-4">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-400 hover:text-white">Cancel</button>
        </div>
      </div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-xs text-gray-400 mb-1">{label}</label>
      {children}
    </div>
  )
}

function formatCooldown(seconds: number): string {
  if (seconds >= 86400) return `${Math.round(seconds / 86400)}d`
  if (seconds >= 3600) return `${Math.round(seconds / 3600)}h`
  if (seconds >= 60) return `${Math.round(seconds / 60)}m`
  return `${seconds}s`
}

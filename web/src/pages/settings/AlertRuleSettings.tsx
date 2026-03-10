import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'
import type { AlertRule, AlertTemplate } from '../../types/models'

const METRICS = [
  { value: 'mem_percent', label: 'Memory %' },
  { value: 'disk_percent', label: 'Disk %' },
  { value: 'container_died', label: 'Container Died' },
  { value: 'container_oom', label: 'Container OOM' },
  { value: 'container_cpu_percent', label: 'Container CPU %' },
  { value: 'container_mem_percent', label: 'Container Memory %' },
  { value: 'container_cpu_limit_percent', label: 'Container CPU % of Limit' },
  { value: 'device_offline', label: 'Device Offline' },
  { value: 'service_state', label: 'Service State' },
  { value: 'nic_state', label: 'NIC State' },
  { value: 'process_missing', label: 'Process Missing' },
  { value: 'log_errors', label: 'Log Errors' },
]

const STATE_METRICS = ['service_state', 'nic_state', 'process_missing']

// Container threshold metrics — require a container name (target_name)
const CONTAINER_THRESHOLD_METRICS = ['container_cpu_percent', 'container_mem_percent', 'container_cpu_limit_percent']

// Event-based metrics where threshold is always 1 (fires when the event occurs)
const EVENT_METRICS = ['container_died', 'container_oom', 'device_offline']

// Per-metric suggested defaults applied when the user changes the dropdown
const METRIC_DEFAULTS: Record<string, { operator: string; threshold: number; severity: string; cooldown: number; hint: string }> = {
  mem_percent:     { operator: '>', threshold: 90, severity: 'warning',  cooldown: 3600, hint: 'Memory usage percentage (0–100)' },
  disk_percent:    { operator: '>', threshold: 90, severity: 'critical', cooldown: 3600, hint: 'Disk usage percentage (0–100)' },
  log_errors:      { operator: '>', threshold: 0,  severity: 'warning',  cooldown: 900,  hint: 'Number of error-level log entries since last heartbeat' },
  container_died:  { operator: '==', threshold: 1, severity: 'warning',  cooldown: 900,  hint: 'Fires when a container exits unexpectedly' },
  container_oom:   { operator: '==', threshold: 1, severity: 'critical', cooldown: 900,  hint: 'Fires when a container is OOM killed' },
  device_offline:          { operator: '==', threshold: 1,  severity: 'warning',  cooldown: 300,  hint: 'Fires when a device stops sending heartbeats' },
  container_cpu_percent:   { operator: '>', threshold: 80, severity: 'warning',  cooldown: 900,  hint: 'Container CPU usage percentage (0–100). Requires container name.' },
  container_mem_percent:   { operator: '>', threshold: 90, severity: 'warning',  cooldown: 900,  hint: 'Container memory usage percentage (0–100). Requires container name.' },
  container_cpu_limit_percent: { operator: '>', threshold: 90, severity: 'warning', cooldown: 900, hint: 'CPU usage as % of compose CPU limit. Requires container name and cpus: in compose.' },
}

const TARGET_STATES: Record<string, string[]> = {
  service_state: ['stopped', 'failed', 'inactive', 'dead'],
  nic_state: ['DOWN', 'LOWER_LAYER_DOWN', 'DORMANT', 'UNKNOWN', 'NO-CARRIER'],
  process_missing: ['absent'],
}

const TARGET_STATE_DEFAULTS: Record<string, string[]> = {
  service_state: ['stopped', 'failed'],
  nic_state: ['DOWN', 'LOWER_LAYER_DOWN', 'NO-CARRIER'],
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
  const isContainerThreshold = editing ? CONTAINER_THRESHOLD_METRICS.includes(editing.metric || '') : false

  const globalRules = useMemo(() => rules.filter(r => !r.device_filter), [rules])
  const deviceRules = useMemo(() => rules.filter(r => !!r.device_filter), [rules])

  if (isLoading) {
    return <div className="text-gray-400">Loading...</div>
  }

  const openEdit = (rule: AlertRule) => {
    const r = { ...rule }
    if (r.target_state?.toLowerCase() === 'any' && TARGET_STATES[r.metric]) {
      r.target_state = TARGET_STATES[r.metric].join(',')
    }
    setEditing(r)
    setIsNew(false)
  }

  return (
    <div>
      {/* Global Alert Rules */}
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">Global Alert Rules</h2>
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

      <RulesTable
        rules={globalRules}
        showDevices={false}
        emptyMessage="No global alert rules configured. Click &quot;Add Rule&quot; to create one."
        onToggle={rule => toggleMutation.mutate(rule)}
        onEdit={openEdit}
        onDelete={id => { if (confirm('Delete this rule?')) deleteMutation.mutate(id) }}
      />

      {/* Device-Specific Alert Rules */}
      <div className="flex items-center justify-between mb-4 mt-8">
        <h2 className="text-lg font-semibold text-white">Device-Specific Alert Rules</h2>
        <button
          onClick={() => { setEditing({ ...emptyRule, device_filter: '' }); setIsNew(true) }}
          className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
        >
          Add Rule
        </button>
      </div>

      <RulesTable
        rules={deviceRules}
        showDevices={true}
        emptyMessage="No device-specific alert rules configured."
        onToggle={rule => toggleMutation.mutate(rule)}
        onEdit={openEdit}
        onDelete={id => { if (confirm('Delete this rule?')) deleteMutation.mutate(id) }}
      />

      {/* Edit / Create Modal */}
      {editing && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setEditing(null)}>
          <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg mx-4 max-h-[90vh] flex flex-col" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between p-6 pb-4">
              <h3 className="text-lg font-semibold text-white">
                {isNew ? 'Create Alert Rule' : 'Edit Alert Rule'}
              </h3>
              <button onClick={() => setEditing(null)} className="text-gray-500 hover:text-white transition-colors">
                <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                  <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
                </svg>
              </button>
            </div>
            <div className="overflow-y-auto px-6">
            <div className="space-y-4">
              <Field label="Name">
                <input
                  value={editing.name || ''}
                  onChange={e => setEditing({ ...editing, name: e.target.value })}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                />
              </Field>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <Field label="Metric">
                  <select
                    value={editing.metric}
                    onChange={e => {
                      const m = e.target.value
                      const isState = STATE_METRICS.includes(m)
                      const isContainerThr = CONTAINER_THRESHOLD_METRICS.includes(m)
                      const defaults = METRIC_DEFAULTS[m]
                      setEditing({
                        ...editing,
                        metric: m,
                        operator: isState ? '==' : (defaults?.operator ?? '>'),
                        threshold: isState ? 1 : (defaults?.threshold ?? 90),
                        severity: defaults?.severity ?? editing.severity,
                        cooldown_seconds: defaults?.cooldown ?? editing.cooldown_seconds,
                        target_state: isState ? (TARGET_STATE_DEFAULTS[m] || []).join(',') : '',
                        target_name: (isState || isContainerThr) ? editing.target_name : '',
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
                    <Field label="Alert on States">
                      <StateMultiSelect
                        options={TARGET_STATES[editing.metric || ''] || []}
                        selected={(editing.target_state || '').split(',').filter(Boolean)}
                        onChange={states => setEditing({ ...editing, target_state: states.join(',') })}
                      />
                    </Field>
                  </>
                ) : isContainerThreshold ? (
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
                ) : EVENT_METRICS.includes(editing.metric || '') ? (
                  <div className="sm:col-span-2 flex items-center">
                    <p className="text-xs text-gray-400 italic">{METRIC_DEFAULTS[editing.metric || '']?.hint}</p>
                  </div>
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
                      {METRIC_DEFAULTS[editing.metric || '']?.hint && (
                        <p className="text-[11px] text-gray-500 mt-1">{METRIC_DEFAULTS[editing.metric || '']?.hint}</p>
                      )}
                    </Field>
                  </>
                )}
              </div>
              {isContainerThreshold && (
                <Field label="Container Name (required)">
                  <input
                    value={editing.target_name || ''}
                    onChange={e => setEditing({ ...editing, target_name: e.target.value })}
                    placeholder="e.g. nginx, postgres"
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  />
                  <p className="text-[11px] text-gray-500 mt-1">{METRIC_DEFAULTS[editing.metric || '']?.hint}</p>
                </Field>
              )}
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
            </div>
            <div className="flex justify-end gap-3 p-6">
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
            // Convert "any" from templates to all available states for that metric
            let targetState = tpl.target_state || ''
            if (targetState.toLowerCase() === 'any' && TARGET_STATES[tpl.metric]) {
              targetState = TARGET_STATES[tpl.metric].join(',')
            }
            setEditing({
              ...emptyRule,
              name: tpl.name,
              metric: tpl.metric,
              operator: tpl.operator,
              threshold: tpl.threshold,
              target_state: targetState,
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

function RulesTable({ rules, showDevices, emptyMessage, onToggle, onEdit, onDelete }: {
  rules: AlertRule[]
  showDevices: boolean
  emptyMessage: string
  onToggle: (rule: AlertRule) => void
  onEdit: (rule: AlertRule) => void
  onDelete: (id: number) => void
}) {
  const colCount = showDevices ? 9 : 8
  return (
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
            {showDevices && <th className="px-4 py-3">Devices</th>}
            <th className="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          {rules.map(rule => (
            <tr key={rule.id} className="border-b border-gray-800/50 text-gray-300">
              <td className="px-4 py-3">
                <button
                  onClick={() => onToggle(rule)}
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
              {showDevices && (
                <td className="px-4 py-3 text-gray-400 font-mono text-xs">{rule.device_filter}</td>
              )}
              <td className="px-4 py-3 text-right">
                <button
                  onClick={() => onEdit(rule)}
                  className="text-gray-400 hover:text-white mr-2"
                >
                  Edit
                </button>
                <button
                  onClick={() => onDelete(rule.id)}
                  className="text-gray-400 hover:text-red-400"
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
          {rules.length === 0 && (
            <tr>
              <td colSpan={colCount} className="px-4 py-8 text-center text-gray-500">
                {emptyMessage}
              </td>
            </tr>
          )}
        </tbody>
      </table>
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
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg mx-4 max-h-[80vh] flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between p-6 pb-4">
          <h3 className="text-lg font-semibold text-white">Create from Template</h3>
          <button onClick={onClose} className="text-gray-500 hover:text-white transition-colors">
            <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
              <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
            </svg>
          </button>
        </div>
        <div className="overflow-y-auto px-6 pb-6">
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

function StateMultiSelect({ options, selected, onChange }: {
  options: string[]
  selected: string[]
  onChange: (states: string[]) => void
}) {
  const toggle = (state: string) => {
    onChange(
      selected.includes(state)
        ? selected.filter(s => s !== state)
        : [...selected, state]
    )
  }

  const allSelected = options.length > 0 && options.every(s => selected.includes(s))

  return (
    <div className="bg-gray-800 border border-gray-700 rounded px-3 py-2">
      {options.length > 1 && (
        <label className="flex items-center gap-2 text-xs text-gray-400 mb-1.5 pb-1.5 border-b border-gray-700 cursor-pointer">
          <input
            type="checkbox"
            checked={allSelected}
            onChange={() => onChange(allSelected ? [] : [...options])}
            className="rounded bg-gray-700 border-gray-600 text-blue-500"
          />
          Any (all states)
        </label>
      )}
      <div className="flex flex-wrap gap-x-3 gap-y-1">
        {options.map(s => (
          <label key={s} className="flex items-center gap-1.5 text-sm text-white cursor-pointer">
            <input
              type="checkbox"
              checked={selected.includes(s)}
              onChange={() => toggle(s)}
              className="rounded bg-gray-700 border-gray-600 text-blue-500"
            />
            {s}
          </label>
        ))}
      </div>
    </div>
  )
}

function formatCooldown(seconds: number): string {
  if (seconds >= 86400) return `${Math.round(seconds / 86400)}d`
  if (seconds >= 3600) return `${Math.round(seconds / 3600)}h`
  if (seconds >= 60) return `${Math.round(seconds / 60)}m`
  return `${seconds}s`
}

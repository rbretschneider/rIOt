import type { ProbeAssertion } from '../types/models'

const TYPES = [
  { value: 'shell', label: 'Shell Command' },
  { value: 'http', label: 'HTTP' },
  { value: 'port', label: 'Port Check' },
  { value: 'file', label: 'File Check' },
  { value: 'container_exec', label: 'Container Exec' },
]

// Available output fields per probe type (matches what the agent returns)
const FIELDS_BY_TYPE: Record<string, { value: string; label: string }[]> = {
  shell: [
    { value: 'exit_code', label: 'Exit Code' },
    { value: 'stdout', label: 'Stdout' },
    { value: 'stderr', label: 'Stderr' },
  ],
  http: [
    { value: 'status_code', label: 'Status Code' },
    { value: 'body', label: 'Response Body' },
    { value: 'latency_ms', label: 'Latency (ms)' },
  ],
  port: [
    { value: 'connected', label: 'Connected' },
    { value: 'latency_ms', label: 'Latency (ms)' },
  ],
  file: [
    { value: 'exists', label: 'Exists' },
    { value: 'size', label: 'Size (bytes)' },
    { value: 'content', label: 'Content' },
  ],
  container_exec: [
    { value: 'exit_code', label: 'Exit Code' },
    { value: 'stdout', label: 'Stdout' },
    { value: 'stderr', label: 'Stderr' },
  ],
}

// Common assertion templates per probe type
const TEMPLATES_BY_TYPE: Record<string, { label: string; assertion: ProbeAssertion }[]> = {
  shell: [
    { label: 'Exit code = 0', assertion: { field: 'exit_code', operator: 'eq', value: '0' } },
    { label: 'Stdout contains...', assertion: { field: 'stdout', operator: 'contains', value: '' } },
    { label: 'Stdout = true', assertion: { field: 'stdout', operator: 'regex', value: '^\\s*true\\s*$' } },
    { label: 'Stderr is empty', assertion: { field: 'stderr', operator: 'eq', value: '' } },
  ],
  http: [
    { label: 'Status 200', assertion: { field: 'status_code', operator: 'eq', value: '200' } },
    { label: 'Status 2xx', assertion: { field: 'status_code', operator: 'regex', value: '^2\\d{2}$' } },
    { label: 'Body contains...', assertion: { field: 'body', operator: 'contains', value: '' } },
    { label: 'Latency < 1000ms', assertion: { field: 'latency_ms', operator: 'lt', value: '1000' } },
  ],
  port: [
    { label: 'Port is open', assertion: { field: 'connected', operator: 'eq', value: 'true' } },
    { label: 'Latency < 100ms', assertion: { field: 'latency_ms', operator: 'lt', value: '100' } },
  ],
  file: [
    { label: 'File exists', assertion: { field: 'exists', operator: 'eq', value: 'true' } },
    { label: 'Size > 0', assertion: { field: 'size', operator: 'gt', value: '0' } },
    { label: 'Content contains...', assertion: { field: 'content', operator: 'contains', value: '' } },
  ],
  container_exec: [
    { label: 'Exit code = 0', assertion: { field: 'exit_code', operator: 'eq', value: '0' } },
    { label: 'Stdout contains...', assertion: { field: 'stdout', operator: 'contains', value: '' } },
    { label: 'Stdout = true', assertion: { field: 'stdout', operator: 'regex', value: '^\\s*true\\s*$' } },
    { label: 'Stderr is empty', assertion: { field: 'stderr', operator: 'eq', value: '' } },
  ],
}

// Operators that match what the Go backend actually handles
const OPERATORS = [
  { value: 'eq', label: '=' },
  { value: 'ne', label: '!=' },
  { value: 'gt', label: '>' },
  { value: 'lt', label: '<' },
  { value: 'contains', label: 'contains' },
  { value: 'regex', label: 'regex' },
]

export interface DeviceProbeForm {
  id?: number
  name: string
  type: string
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  config: Record<string, unknown>
  assertions: ProbeAssertion[]
}

export const emptyDeviceProbe: DeviceProbeForm = {
  name: '',
  type: 'shell',
  enabled: true,
  interval_seconds: 60,
  timeout_seconds: 10,
  config: { command: '', shell: '/bin/sh' },
  assertions: [],
}

function defaultConfig(type: string): Record<string, unknown> {
  switch (type) {
    case 'shell': return { command: '', shell: '/bin/sh' }
    case 'http': return { url: '', method: 'GET', headers: {}, body: '' }
    case 'port': return { host: 'localhost', port: 80 }
    case 'file': return { path: '' }
    case 'container_exec': return { container_id: '', command: '' }
    default: return {}
  }
}

interface Props {
  editing: DeviceProbeForm
  isNew: boolean
  saving: boolean
  onClose: () => void
  onChange: (form: DeviceProbeForm) => void
  onSave: () => void
}

export default function DeviceProbeModal({ editing, isNew, saving, onClose, onChange, onSave }: Props) {
  function setField<K extends keyof DeviceProbeForm>(key: K, value: DeviceProbeForm[K]) {
    onChange({ ...editing, [key]: value })
  }

  function setConfig(key: string, value: unknown) {
    onChange({ ...editing, config: { ...editing.config, [key]: value } })
  }

  function addAssertion(template?: ProbeAssertion) {
    const a = template ? { ...template } : { field: fields[0]?.value || '', operator: 'eq', value: '' }
    onChange({ ...editing, assertions: [...editing.assertions, a] })
  }

  function updateAssertion(idx: number, field: string, value: string) {
    const updated = editing.assertions.map((a, i) => i === idx ? { ...a, [field]: value } : a)
    onChange({ ...editing, assertions: updated })
  }

  function removeAssertion(idx: number) {
    onChange({ ...editing, assertions: editing.assertions.filter((_, i) => i !== idx) })
  }

  const fields = FIELDS_BY_TYPE[editing.type] || []
  const templates = TEMPLATES_BY_TYPE[editing.type] || []

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg mx-4 max-h-[90vh] flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between p-6 pb-4">
          <h3 className="text-lg font-semibold text-white">
            {isNew ? 'Create Device Probe' : 'Edit Device Probe'}
          </h3>
          <button onClick={onClose} className="text-gray-500 hover:text-white transition-colors">
            <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
              <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
            </svg>
          </button>
        </div>

        <div className="overflow-y-auto scrollbar-thin px-6 pb-6 space-y-4">
          {/* Name */}
          <div>
            <label className="block text-xs text-gray-400 mb-1">Name</label>
            <input
              value={editing.name}
              onChange={e => setField('name', e.target.value)}
              className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-gray-500"
              placeholder="My probe"
            />
          </div>

          {/* Type */}
          <div>
            <label className="block text-xs text-gray-400 mb-1">Type</label>
            <select
              value={editing.type}
              onChange={e => {
                const newType = e.target.value
                onChange({ ...editing, type: newType, config: defaultConfig(newType), assertions: [] })
              }}
              className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-gray-500"
            >
              {TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
            </select>
          </div>

          {/* Enabled toggle */}
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={editing.enabled}
              onChange={e => setField('enabled', e.target.checked)}
              className="w-4 h-4 rounded border-gray-600 bg-gray-700 text-blue-500 focus:ring-0"
            />
            <label className="text-sm text-gray-300">Enabled</label>
          </div>

          {/* Interval & Timeout */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-gray-400 mb-1">Interval (seconds)</label>
              <input
                type="number"
                value={editing.interval_seconds}
                onChange={e => setField('interval_seconds', parseInt(e.target.value) || 60)}
                className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-gray-500"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">Timeout (seconds)</label>
              <input
                type="number"
                value={editing.timeout_seconds}
                onChange={e => setField('timeout_seconds', parseInt(e.target.value) || 10)}
                className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-gray-500"
              />
            </div>
          </div>

          {/* Dynamic config fields */}
          <div className="border-t border-gray-800 pt-4">
            <h4 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-3">Configuration</h4>
            {editing.type === 'shell' && (
              <>
                <div className="mb-3">
                  <label className="block text-xs text-gray-400 mb-1">Command</label>
                  <input
                    value={(editing.config.command as string) || ''}
                    onChange={e => setConfig('command', e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white font-mono focus:outline-none focus:border-gray-500"
                    placeholder="echo hello"
                  />
                </div>
                <div>
                  <label className="block text-xs text-gray-400 mb-1">Shell</label>
                  <input
                    value={(editing.config.shell as string) || '/bin/sh'}
                    onChange={e => setConfig('shell', e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white font-mono focus:outline-none focus:border-gray-500"
                  />
                </div>
              </>
            )}
            {editing.type === 'http' && (
              <>
                <div className="mb-3">
                  <label className="block text-xs text-gray-400 mb-1">URL</label>
                  <input
                    value={(editing.config.url as string) || ''}
                    onChange={e => setConfig('url', e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-gray-500"
                    placeholder="https://example.com"
                  />
                </div>
                <div className="mb-3">
                  <label className="block text-xs text-gray-400 mb-1">Method</label>
                  <select
                    value={(editing.config.method as string) || 'GET'}
                    onChange={e => setConfig('method', e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-gray-500"
                  >
                    {['GET', 'POST', 'PUT', 'DELETE', 'HEAD'].map(m => <option key={m} value={m}>{m}</option>)}
                  </select>
                </div>
                <div>
                  <label className="block text-xs text-gray-400 mb-1">Body (optional)</label>
                  <textarea
                    value={(editing.config.body as string) || ''}
                    onChange={e => setConfig('body', e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white font-mono focus:outline-none focus:border-gray-500"
                    rows={3}
                  />
                </div>
              </>
            )}
            {editing.type === 'port' && (
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-gray-400 mb-1">Host</label>
                  <input
                    value={(editing.config.host as string) || 'localhost'}
                    onChange={e => setConfig('host', e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-gray-500"
                  />
                </div>
                <div>
                  <label className="block text-xs text-gray-400 mb-1">Port</label>
                  <input
                    type="number"
                    value={(editing.config.port as number) || 80}
                    onChange={e => setConfig('port', parseInt(e.target.value) || 80)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white focus:outline-none focus:border-gray-500"
                  />
                </div>
              </div>
            )}
            {editing.type === 'file' && (
              <div>
                <label className="block text-xs text-gray-400 mb-1">File Path</label>
                <input
                  value={(editing.config.path as string) || ''}
                  onChange={e => setConfig('path', e.target.value)}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white font-mono focus:outline-none focus:border-gray-500"
                  placeholder="/var/log/app.log"
                />
              </div>
            )}
            {editing.type === 'container_exec' && (
              <>
                <div className="mb-3">
                  <label className="block text-xs text-gray-400 mb-1">Container ID / Name</label>
                  <input
                    value={(editing.config.container_id as string) || ''}
                    onChange={e => setConfig('container_id', e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white font-mono focus:outline-none focus:border-gray-500"
                    placeholder="my-container"
                  />
                </div>
                <div>
                  <label className="block text-xs text-gray-400 mb-1">Command</label>
                  <input
                    value={(editing.config.command as string) || ''}
                    onChange={e => setConfig('command', e.target.value)}
                    className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-sm text-white font-mono focus:outline-none focus:border-gray-500"
                    placeholder="echo hello"
                  />
                </div>
              </>
            )}
          </div>

          {/* Assertions */}
          <div className="border-t border-gray-800 pt-4">
            <div className="flex items-center justify-between mb-3">
              <h4 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">Assertions</h4>
              <button
                onClick={() => addAssertion()}
                className="text-xs text-blue-400 hover:text-blue-300 transition-colors"
              >
                + Add Assertion
              </button>
            </div>

            {/* Templates */}
            {templates.length > 0 && editing.assertions.length === 0 && (
              <div className="mb-3">
                <p className="text-xs text-gray-500 mb-2">Quick add:</p>
                <div className="flex flex-wrap gap-1.5">
                  {templates.map((t, i) => (
                    <button
                      key={i}
                      onClick={() => addAssertion(t.assertion)}
                      className="px-2 py-1 text-xs bg-gray-800 border border-gray-700 rounded text-gray-400 hover:text-white hover:border-gray-500 transition-colors"
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
              </div>
            )}

            {editing.assertions.length === 0 && (
              <p className="text-xs text-gray-600">
                No assertions — probe will always report success if it executes without error. Use this for fire-and-forget tasks.
              </p>
            )}
            {editing.assertions.map((a, i) => (
              <div key={i} className="flex items-center gap-2 mb-2">
                <select
                  value={a.field}
                  onChange={e => updateAssertion(i, 'field', e.target.value)}
                  className="flex-1 px-2 py-1.5 bg-gray-800 border border-gray-700 rounded text-xs text-white focus:outline-none focus:border-gray-500"
                >
                  {!a.field && <option value="">Select field...</option>}
                  {fields.map(f => <option key={f.value} value={f.value}>{f.label}</option>)}
                </select>
                <select
                  value={a.operator}
                  onChange={e => updateAssertion(i, 'operator', e.target.value)}
                  className="px-2 py-1.5 bg-gray-800 border border-gray-700 rounded text-xs text-white focus:outline-none focus:border-gray-500"
                >
                  {OPERATORS.map(op => <option key={op.value} value={op.value}>{op.label}</option>)}
                </select>
                <input
                  value={a.value}
                  onChange={e => updateAssertion(i, 'value', e.target.value)}
                  placeholder="value"
                  className="flex-1 px-2 py-1.5 bg-gray-800 border border-gray-700 rounded text-xs text-white focus:outline-none focus:border-gray-500"
                />
                <button
                  onClick={() => removeAssertion(i)}
                  className="text-gray-500 hover:text-red-400 transition-colors p-1"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
                    <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
                  </svg>
                </button>
              </div>
            ))}

            {/* Show templates inline when assertions already exist */}
            {templates.length > 0 && editing.assertions.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {templates.map((t, i) => (
                  <button
                    key={i}
                    onClick={() => addAssertion(t.assertion)}
                    className="px-2 py-0.5 text-xs text-gray-600 hover:text-gray-400 transition-colors"
                  >
                    + {t.label}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-3 p-6 pt-4 border-t border-gray-800">
          <button onClick={onClose} className="px-4 py-2 text-sm text-gray-400 hover:text-white">
            Cancel
          </button>
          <button
            onClick={onSave}
            disabled={saving || !editing.name}
            className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-500 text-white rounded-md transition-colors disabled:opacity-50"
          >
            {saving ? 'Saving...' : isNew ? 'Create' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

import type { Probe } from '../types/models'

const TYPES = [
  { value: 'http', label: 'HTTP' },
  { value: 'ping', label: 'Ping (ICMP)' },
  { value: 'dns', label: 'DNS' },
]

export interface ProbeForm {
  id?: number
  name: string
  type: string
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  config: Record<string, unknown>
}

export const emptyProbe: ProbeForm = {
  name: '',
  type: 'http',
  enabled: true,
  interval_seconds: 60,
  timeout_seconds: 10,
  config: { url: '', method: 'GET', expected_status: 200 },
}

export function probeToForm(probe: Probe): ProbeForm {
  return {
    id: probe.id,
    name: probe.name,
    type: probe.type,
    enabled: probe.enabled,
    interval_seconds: probe.interval_seconds,
    timeout_seconds: probe.timeout_seconds,
    config: { ...probe.config },
  }
}

function defaultConfig(type: string): Record<string, unknown> {
  switch (type) {
    case 'http': return { url: '', method: 'GET', expected_status: 200 }
    case 'ping': return { target: '', count: 3 }
    case 'dns': return { hostname: '', server: '', expected_ips: '' }
    default: return {}
  }
}

export function getTarget(probe: { type: string; config: Record<string, unknown> }): string {
  switch (probe.type) {
    case 'http': return (probe.config.url as string) || '-'
    case 'ping': return (probe.config.target as string) || '-'
    case 'dns': return (probe.config.hostname as string) || '-'
    default: return '-'
  }
}

interface ProbeModalProps {
  editing: ProbeForm
  isNew: boolean
  saving: boolean
  onClose: () => void
  onChange: (form: ProbeForm) => void
  onSave: () => void
}

export default function ProbeModal({ editing, isNew, saving, onClose, onChange, onSave }: ProbeModalProps) {
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={onClose}>
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg mx-4 max-h-[90vh] flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between p-6 pb-4">
          <h3 className="text-lg font-semibold text-white">
            {isNew ? 'Create Probe' : 'Edit Probe'}
          </h3>
          <button onClick={onClose} className="text-gray-500 hover:text-white transition-colors">
            <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
              <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
            </svg>
          </button>
        </div>
        <div className="overflow-y-auto scrollbar-thin px-6">
          <div className="space-y-4">
            <Field label="Name">
              <input
                value={editing.name}
                onChange={e => onChange({ ...editing, name: e.target.value })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                placeholder="e.g. Homepage Check"
              />
            </Field>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <Field label="Type">
                <select
                  value={editing.type}
                  onChange={e => {
                    const t = e.target.value
                    onChange({ ...editing, type: t, config: defaultConfig(t) })
                  }}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                >
                  {TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                </select>
              </Field>
              <Field label="Interval (s)">
                <input
                  type="number"
                  value={editing.interval_seconds}
                  onChange={e => onChange({ ...editing, interval_seconds: parseInt(e.target.value) || 60 })}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                />
              </Field>
              <Field label="Timeout (s)">
                <input
                  type="number"
                  value={editing.timeout_seconds}
                  onChange={e => onChange({ ...editing, timeout_seconds: parseInt(e.target.value) || 10 })}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                />
              </Field>
            </div>

            {editing.type === 'http' && (
              <>
                <Field label="URL">
                  <input
                    value={(editing.config.url as string) || ''}
                    onChange={e => onChange({ ...editing, config: { ...editing.config, url: e.target.value } })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    placeholder="https://example.com"
                  />
                </Field>
                <div className="grid grid-cols-2 gap-3">
                  <Field label="Method">
                    <select
                      value={(editing.config.method as string) || 'GET'}
                      onChange={e => onChange({ ...editing, config: { ...editing.config, method: e.target.value } })}
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    >
                      {['GET', 'POST', 'PUT', 'HEAD'].map(m => <option key={m} value={m}>{m}</option>)}
                    </select>
                  </Field>
                  <Field label="Expected Status">
                    <input
                      type="number"
                      value={(editing.config.expected_status as number) || 200}
                      onChange={e => onChange({ ...editing, config: { ...editing.config, expected_status: parseInt(e.target.value) || 200 } })}
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    />
                  </Field>
                </div>
                <Field label="Headers (JSON, optional)">
                  <input
                    value={(editing.config.headers as string) || ''}
                    onChange={e => onChange({ ...editing, config: { ...editing.config, headers: e.target.value } })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    placeholder='{"Authorization": "Bearer ..."}'
                  />
                </Field>
              </>
            )}

            {editing.type === 'ping' && (
              <>
                <Field label="Target">
                  <input
                    value={(editing.config.target as string) || ''}
                    onChange={e => onChange({ ...editing, config: { ...editing.config, target: e.target.value } })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    placeholder="192.168.1.1 or hostname"
                  />
                </Field>
                <Field label="Ping Count">
                  <input
                    type="number"
                    value={(editing.config.count as number) || 3}
                    onChange={e => onChange({ ...editing, config: { ...editing.config, count: parseInt(e.target.value) || 3 } })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  />
                </Field>
              </>
            )}

            {editing.type === 'dns' && (
              <>
                <Field label="Domain Name">
                  <input
                    value={(editing.config.hostname as string) || ''}
                    onChange={e => onChange({ ...editing, config: { ...editing.config, hostname: e.target.value } })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    placeholder="example.com"
                  />
                </Field>
                <Field label="DNS Server (optional)">
                  <input
                    value={(editing.config.server as string) || ''}
                    onChange={e => onChange({ ...editing, config: { ...editing.config, server: e.target.value } })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    placeholder="8.8.8.8:53 (leave empty for system default)"
                  />
                </Field>
                <Field label="Expected IPs (comma-separated, optional)">
                  <input
                    value={(editing.config.expected_ips as string) || ''}
                    onChange={e => onChange({ ...editing, config: { ...editing.config, expected_ips: e.target.value } })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    placeholder="1.2.3.4, 5.6.7.8"
                  />
                </Field>
              </>
            )}

            <label className="flex items-center gap-2 text-sm text-gray-300">
              <input
                type="checkbox"
                checked={editing.enabled}
                onChange={e => onChange({ ...editing, enabled: e.target.checked })}
                className="rounded bg-gray-800 border-gray-600"
              />
              Enabled
            </label>
          </div>
        </div>
        <div className="flex justify-end gap-3 p-6">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm text-gray-400 hover:text-white"
          >
            Cancel
          </button>
          <button
            onClick={onSave}
            disabled={saving}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors disabled:opacity-50"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
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

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'
import type { Probe } from '../../types/models'

const TYPES = [
  { value: 'http', label: 'HTTP' },
  { value: 'ping', label: 'Ping (ICMP)' },
  { value: 'dns', label: 'DNS' },
]

interface ProbeForm {
  id?: number
  name: string
  type: string
  enabled: boolean
  interval_seconds: number
  timeout_seconds: number
  config: Record<string, unknown>
}

const emptyProbe: ProbeForm = {
  name: '',
  type: 'http',
  enabled: true,
  interval_seconds: 60,
  timeout_seconds: 10,
  config: { url: '', method: 'GET', expected_status: 200 },
}

function defaultConfig(type: string): Record<string, unknown> {
  switch (type) {
    case 'http': return { url: '', method: 'GET', expected_status: 200 }
    case 'ping': return { target: '', count: 3 }
    case 'dns': return { name: '', server: '', expected_ips: '' }
    default: return {}
  }
}

export default function ProbeSettings() {
  const qc = useQueryClient()
  const { data: probes = [], isLoading } = useQuery({
    queryKey: ['probes'],
    queryFn: api.getProbes,
  })
  const [editing, setEditing] = useState<ProbeForm | null>(null)
  const [isNew, setIsNew] = useState(false)

  const saveMutation = useMutation({
    mutationFn: (probe: ProbeForm) =>
      isNew
        ? api.createProbe(probe)
        : api.updateProbe(probe.id!, probe),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['probes'] })
      setEditing(null)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => api.deleteProbe(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  const toggleMutation = useMutation({
    mutationFn: (probe: Probe) =>
      api.updateProbe(probe.id, { ...probe, enabled: !probe.enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['probes'] }),
  })

  if (isLoading) return <div className="text-gray-400">Loading...</div>

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">Probes</h2>
        <button
          onClick={() => { setEditing({ ...emptyProbe }); setIsNew(true) }}
          className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
        >
          Add Probe
        </button>
      </div>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-x-auto">
        <table className="w-full text-sm min-w-[640px]">
          <thead>
            <tr className="text-left text-gray-400 border-b border-gray-800">
              <th className="px-4 py-3">Enabled</th>
              <th className="px-4 py-3">Name</th>
              <th className="px-4 py-3">Type</th>
              <th className="px-4 py-3">Target</th>
              <th className="px-4 py-3">Interval</th>
              <th className="px-4 py-3">Timeout</th>
              <th className="px-4 py-3"></th>
            </tr>
          </thead>
          <tbody>
            {probes.map(probe => (
              <tr key={probe.id} className="border-b border-gray-800/50 text-gray-300">
                <td className="px-4 py-3">
                  <button
                    onClick={() => toggleMutation.mutate(probe)}
                    className={`w-8 h-4 rounded-full transition-colors relative ${
                      probe.enabled ? 'bg-emerald-600' : 'bg-gray-600'
                    }`}
                  >
                    <span className={`absolute top-0.5 w-3 h-3 rounded-full bg-white transition-transform ${
                      probe.enabled ? 'left-4' : 'left-0.5'
                    }`} />
                  </button>
                </td>
                <td className="px-4 py-3 text-white">{probe.name}</td>
                <td className="px-4 py-3 uppercase text-xs">{probe.type}</td>
                <td className="px-4 py-3 font-mono text-xs text-gray-400 truncate max-w-xs">
                  {getTarget(probe)}
                </td>
                <td className="px-4 py-3 text-gray-400">{probe.interval_seconds}s</td>
                <td className="px-4 py-3 text-gray-400">{probe.timeout_seconds}s</td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => {
                      setEditing({
                        id: probe.id,
                        name: probe.name,
                        type: probe.type,
                        enabled: probe.enabled,
                        interval_seconds: probe.interval_seconds,
                        timeout_seconds: probe.timeout_seconds,
                        config: { ...probe.config },
                      })
                      setIsNew(false)
                    }}
                    className="text-gray-400 hover:text-white mr-2"
                  >
                    Edit
                  </button>
                  <button
                    onClick={() => { if (confirm('Delete this probe?')) deleteMutation.mutate(probe.id) }}
                    className="text-gray-400 hover:text-red-400"
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
            {probes.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-gray-500">
                  No probes configured. Click "Add Probe" to create one.
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
              {isNew ? 'Create Probe' : 'Edit Probe'}
            </h3>
            <div className="space-y-4">
              <Field label="Name">
                <input
                  value={editing.name}
                  onChange={e => setEditing({ ...editing, name: e.target.value })}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  placeholder="e.g. Homepage Check"
                />
              </Field>
              <div className="grid grid-cols-3 gap-3">
                <Field label="Type">
                  <select
                    value={editing.type}
                    onChange={e => {
                      const t = e.target.value
                      setEditing({ ...editing, type: t, config: defaultConfig(t) })
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
                    onChange={e => setEditing({ ...editing, interval_seconds: parseInt(e.target.value) || 60 })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  />
                </Field>
                <Field label="Timeout (s)">
                  <input
                    type="number"
                    value={editing.timeout_seconds}
                    onChange={e => setEditing({ ...editing, timeout_seconds: parseInt(e.target.value) || 10 })}
                    className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                  />
                </Field>
              </div>

              {/* Type-specific config fields */}
              {editing.type === 'http' && (
                <>
                  <Field label="URL">
                    <input
                      value={(editing.config.url as string) || ''}
                      onChange={e => setEditing({ ...editing, config: { ...editing.config, url: e.target.value } })}
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      placeholder="https://example.com"
                    />
                  </Field>
                  <div className="grid grid-cols-2 gap-3">
                    <Field label="Method">
                      <select
                        value={(editing.config.method as string) || 'GET'}
                        onChange={e => setEditing({ ...editing, config: { ...editing.config, method: e.target.value } })}
                        className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      >
                        {['GET', 'POST', 'PUT', 'HEAD'].map(m => <option key={m} value={m}>{m}</option>)}
                      </select>
                    </Field>
                    <Field label="Expected Status">
                      <input
                        type="number"
                        value={(editing.config.expected_status as number) || 200}
                        onChange={e => setEditing({ ...editing, config: { ...editing.config, expected_status: parseInt(e.target.value) || 200 } })}
                        className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      />
                    </Field>
                  </div>
                  <Field label="Headers (JSON, optional)">
                    <input
                      value={(editing.config.headers as string) || ''}
                      onChange={e => setEditing({ ...editing, config: { ...editing.config, headers: e.target.value } })}
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
                      onChange={e => setEditing({ ...editing, config: { ...editing.config, target: e.target.value } })}
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      placeholder="192.168.1.1 or hostname"
                    />
                  </Field>
                  <Field label="Ping Count">
                    <input
                      type="number"
                      value={(editing.config.count as number) || 3}
                      onChange={e => setEditing({ ...editing, config: { ...editing.config, count: parseInt(e.target.value) || 3 } })}
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                    />
                  </Field>
                </>
              )}

              {editing.type === 'dns' && (
                <>
                  <Field label="Domain Name">
                    <input
                      value={(editing.config.name as string) || ''}
                      onChange={e => setEditing({ ...editing, config: { ...editing.config, name: e.target.value } })}
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      placeholder="example.com"
                    />
                  </Field>
                  <Field label="DNS Server (optional)">
                    <input
                      value={(editing.config.server as string) || ''}
                      onChange={e => setEditing({ ...editing, config: { ...editing.config, server: e.target.value } })}
                      className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                      placeholder="8.8.8.8:53 (leave empty for system default)"
                    />
                  </Field>
                  <Field label="Expected IPs (comma-separated, optional)">
                    <input
                      value={(editing.config.expected_ips as string) || ''}
                      onChange={e => setEditing({ ...editing, config: { ...editing.config, expected_ips: e.target.value } })}
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
                  onChange={e => setEditing({ ...editing, enabled: e.target.checked })}
                  className="rounded bg-gray-800 border-gray-600"
                />
                Enabled
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

function getTarget(probe: Probe): string {
  switch (probe.type) {
    case 'http': return (probe.config.url as string) || '-'
    case 'ping': return (probe.config.target as string) || '-'
    case 'dns': return (probe.config.name as string) || '-'
    default: return '-'
  }
}

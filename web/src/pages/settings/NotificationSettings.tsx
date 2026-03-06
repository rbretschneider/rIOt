import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'
import type { NotificationChannel } from '../../types/models'

const CHANNEL_TYPES = [
  { value: 'ntfy', label: 'ntfy' },
  { value: 'webhook', label: 'Webhook' },
]

const emptyChannel: Partial<NotificationChannel> = {
  name: '',
  type: 'ntfy',
  enabled: true,
  config: {},
}

export default function NotificationSettings() {
  const qc = useQueryClient()
  const { data: channels = [], isLoading } = useQuery({
    queryKey: ['notification-channels'],
    queryFn: settingsApi.getNotificationChannels,
  })
  const [editing, setEditing] = useState<Partial<NotificationChannel> | null>(null)
  const [isNew, setIsNew] = useState(false)
  const [testResult, setTestResult] = useState<{ id: number; success: boolean; error?: string } | null>(null)
  const [testingId, setTestingId] = useState<number | null>(null)

  const saveMutation = useMutation({
    mutationFn: (ch: Partial<NotificationChannel>) =>
      isNew
        ? settingsApi.createNotificationChannel(ch)
        : settingsApi.updateNotificationChannel(ch.id!, ch),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notification-channels'] })
      setEditing(null)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => settingsApi.deleteNotificationChannel(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['notification-channels'] }),
  })

  const testMutation = useMutation({
    mutationFn: (id: number) => {
      setTestingId(id)
      return settingsApi.testNotificationChannel(id)
    },
    onSuccess: (result, id) => {
      setTestResult({ id, ...result })
      setTestingId(null)
    },
    onError: (_, id) => {
      setTestResult({ id, success: false, error: 'Request failed' })
      setTestingId(null)
    },
  })

  if (isLoading) {
    return <div className="text-gray-400">Loading...</div>
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">Notification Channels</h2>
        <button
          onClick={() => { setEditing({ ...emptyChannel }); setIsNew(true) }}
          className="px-3 py-1.5 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded-md transition-colors"
        >
          Add Channel
        </button>
      </div>

      <div className="space-y-3">
        {channels.map(ch => (
          <div key={ch.id} className="bg-gray-900 rounded-lg border border-gray-800 p-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <span className={`w-2 h-2 rounded-full ${ch.enabled ? 'bg-emerald-500' : 'bg-gray-600'}`} />
                <div>
                  <span className="text-white font-medium">{ch.name}</span>
                  <span className="ml-2 text-xs text-gray-500 bg-gray-800 px-2 py-0.5 rounded">{ch.type}</span>
                </div>
              </div>
              <div className="flex items-center gap-2">
                {testResult?.id === ch.id && (
                  <span className={`text-xs ${testResult.success ? 'text-emerald-400' : 'text-red-400'}`}>
                    {testResult.success ? 'Test sent!' : testResult.error || 'Failed'}
                  </span>
                )}
                <button
                  onClick={() => testMutation.mutate(ch.id)}
                  disabled={testingId === ch.id}
                  className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors disabled:opacity-50"
                >
                  {testingId === ch.id ? 'Testing...' : 'Test'}
                </button>
                <button
                  onClick={() => { setEditing({ ...ch }); setIsNew(false) }}
                  className="px-2 py-1 text-xs text-gray-400 hover:text-white border border-gray-700 rounded transition-colors"
                >
                  Edit
                </button>
                <button
                  onClick={() => { if (confirm('Delete this channel?')) deleteMutation.mutate(ch.id) }}
                  className="px-2 py-1 text-xs text-gray-400 hover:text-red-400 border border-gray-700 rounded transition-colors"
                >
                  Delete
                </button>
              </div>
            </div>
            <div className="mt-2 text-xs text-gray-500">
              {ch.type === 'ntfy' && (
                <>
                  {(ch.config.server_url as string) || 'https://ntfy.sh'} / {(ch.config.topic as string) || '(no topic)'}
                </>
              )}
              {ch.type === 'webhook' && (
                <>{(ch.config.url as string) || '(no URL)'}</>
              )}
            </div>
          </div>
        ))}
        {channels.length === 0 && (
          <div className="bg-gray-900 rounded-lg border border-gray-800 p-8 text-center text-gray-500">
            No notification channels configured. Click "Add Channel" to create one.
          </div>
        )}
      </div>

      {/* Edit / Create Modal */}
      {editing && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50" onClick={() => setEditing(null)}>
          <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg mx-4 max-h-[90vh] flex flex-col" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between p-6 pb-4">
              <h3 className="text-lg font-semibold text-white">
                {isNew ? 'Add Notification Channel' : 'Edit Notification Channel'}
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
              <Field label="Type">
                <select
                  value={editing.type}
                  onChange={e => setEditing({ ...editing, type: e.target.value, config: {} })}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                >
                  {CHANNEL_TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                </select>
              </Field>
              <label className="flex items-center gap-2 text-sm text-gray-300">
                <input
                  type="checkbox"
                  checked={editing.enabled ?? true}
                  onChange={e => setEditing({ ...editing, enabled: e.target.checked })}
                  className="rounded bg-gray-800 border-gray-600"
                />
                Enabled
              </label>

              {/* Type-specific config fields */}
              {editing.type === 'ntfy' && (
                <NtfyConfig
                  config={(editing.config || {}) as Record<string, unknown>}
                  onChange={config => setEditing({ ...editing, config })}
                />
              )}
              {editing.type === 'webhook' && (
                <WebhookConfig
                  config={(editing.config || {}) as Record<string, unknown>}
                  onChange={config => setEditing({ ...editing, config })}
                />
              )}
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
    </div>
  )
}

function NtfyConfig({ config, onChange }: { config: Record<string, unknown>; onChange: (c: Record<string, unknown>) => void }) {
  return (
    <div className="space-y-3 p-3 bg-gray-800/50 rounded-lg">
      <Field label="Server URL">
        <input
          value={(config.server_url as string) || ''}
          onChange={e => onChange({ ...config, server_url: e.target.value })}
          placeholder="https://ntfy.sh"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <Field label="Topic">
        <input
          value={(config.topic as string) || ''}
          onChange={e => onChange({ ...config, topic: e.target.value })}
          placeholder="riot-alerts"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Auth Token (optional)">
          <input
            type="password"
            value={(config.token as string) || ''}
            onChange={e => onChange({ ...config, token: e.target.value })}
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
          />
        </Field>
        <Field label="Priority">
          <select
            value={(config.priority as string) || 'default'}
            onChange={e => onChange({ ...config, priority: e.target.value })}
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
          >
            <option value="default">Default (auto from severity)</option>
            <option value="min">Min</option>
            <option value="low">Low</option>
            <option value="default">Default</option>
            <option value="high">High</option>
            <option value="urgent">Urgent</option>
          </select>
        </Field>
      </div>
    </div>
  )
}

function WebhookConfig({ config, onChange }: { config: Record<string, unknown>; onChange: (c: Record<string, unknown>) => void }) {
  return (
    <div className="space-y-3 p-3 bg-gray-800/50 rounded-lg">
      <Field label="URL">
        <input
          value={(config.url as string) || ''}
          onChange={e => onChange({ ...config, url: e.target.value })}
          placeholder="https://example.com/webhook"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
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

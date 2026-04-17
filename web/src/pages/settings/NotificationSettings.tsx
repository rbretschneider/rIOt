import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'
import type { NotificationChannel } from '../../types/models'

const CHANNEL_TYPES = [
  { value: 'ntfy', label: 'ntfy' },
  { value: 'webhook', label: 'Webhook' },
  { value: 'smtp', label: 'Email (SMTP)' },
  { value: 'telegram', label: 'Telegram' },
  { value: 'discord', label: 'Discord' },
  { value: 'slack', label: 'Slack' },
  { value: 'pushover', label: 'Pushover' },
  { value: 'gotify', label: 'Gotify' },
]

function ChannelTypeIcon({ type }: { type: string }) {
  const cls = "w-4 h-4 text-gray-400 shrink-0"
  switch (type) {
    case 'ntfy':
      // Bell / push notification icon
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
          <path d="M13.73 21a2 2 0 0 1-3.46 0" />
        </svg>
      )
    case 'webhook':
      // Link/webhook icon
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
          <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
        </svg>
      )
    case 'smtp':
      // Email envelope icon
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="2" y="4" width="20" height="16" rx="2" />
          <path d="M22 4l-10 8L2 4" />
        </svg>
      )
    case 'telegram':
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <line x1="22" y1="2" x2="11" y2="13" /><polygon points="22 2 15 22 11 13 2 9 22 2" />
        </svg>
      )
    case 'discord':
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="currentColor">
          <path d="M20.317 4.37a19.791 19.791 0 00-4.885-1.515.074.074 0 00-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 00-5.487 0 12.64 12.64 0 00-.617-1.25.077.077 0 00-.079-.037A19.736 19.736 0 003.677 4.37a.07.07 0 00-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 00.031.057 19.9 19.9 0 005.993 3.03.078.078 0 00.084-.028c.462-.63.874-1.295 1.226-1.994a.076.076 0 00-.041-.106 13.107 13.107 0 01-1.872-.892.077.077 0 01-.008-.128 10.2 10.2 0 00.372-.292.074.074 0 01.077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 01.078.01c.12.098.246.198.373.292a.077.077 0 01-.006.127 12.299 12.299 0 01-1.873.892.077.077 0 00-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 00.084.028 19.839 19.839 0 006.002-3.03.077.077 0 00.032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 00-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.157 2.418zm7.975 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.157 2.418z" />
        </svg>
      )
    case 'slack':
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="currentColor">
          <path d="M5.042 15.165a2.528 2.528 0 01-2.52 2.523A2.528 2.528 0 010 15.165a2.527 2.527 0 012.522-2.52h2.52v2.52zm1.271 0a2.527 2.527 0 012.521-2.52 2.527 2.527 0 012.521 2.52v6.313A2.528 2.528 0 018.834 24a2.528 2.528 0 01-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 01-2.521-2.52A2.528 2.528 0 018.834 0a2.528 2.528 0 012.521 2.522v2.52H8.834zm0 1.271a2.528 2.528 0 012.521 2.521 2.528 2.528 0 01-2.521 2.521H2.522A2.528 2.528 0 010 8.834a2.528 2.528 0 012.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 012.522-2.521A2.528 2.528 0 0124 8.834a2.528 2.528 0 01-2.522 2.521h-2.522V8.834zm-1.27 0a2.528 2.528 0 01-2.523 2.521 2.527 2.527 0 01-2.52-2.521V2.522A2.527 2.527 0 0115.163 0a2.528 2.528 0 012.523 2.522v6.312zM15.163 18.956a2.528 2.528 0 012.523 2.522A2.528 2.528 0 0115.163 24a2.527 2.527 0 01-2.52-2.522v-2.522h2.52zm0-1.27a2.527 2.527 0 01-2.52-2.523 2.527 2.527 0 012.52-2.52h6.315A2.528 2.528 0 0124 15.163a2.528 2.528 0 01-2.522 2.523h-6.315z" />
        </svg>
      )
    case 'pushover':
    case 'gotify':
      return (
        <svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M22 17H2a3 3 0 003 3h14a3 3 0 003-3z" /><path d="M18 8A6 6 0 006 8c0 7-3 9-3 9h18s-3-2-3-9" />
        </svg>
      )
    default:
      return null
  }
}

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
                <ChannelTypeIcon type={ch.type} />
                <div>
                  <span className="text-white font-medium">{ch.name}</span>
                  <span className="ml-2 text-xs text-gray-500 bg-gray-800 px-2 py-0.5 rounded">
                    {CHANNEL_TYPES.find(t => t.value === ch.type)?.label || ch.type}
                  </span>
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
                  className="px-2.5 py-1 text-xs text-red-400/70 hover:text-red-400 border border-red-900/50 hover:border-red-700 rounded transition-colors"
                >
                  Delete
                </button>
              </div>
            </div>
            <div className="mt-2 text-xs text-gray-500">
              {ch.type === 'ntfy' && (
                <>
                  {((ch.config.server_url as string) || 'https://ntfy.sh').replace(/\/+$/, '')}/{(ch.config.topic as string) || '(no topic)'}
                </>
              )}
              {ch.type === 'webhook' && (
                <>{(ch.config.url as string) || '(no URL)'}</>
              )}
              {ch.type === 'smtp' && (
                <>
                  {(ch.config.host as string) || 'localhost'}:{(ch.config.port as number) || 587}
                  {' → '}{(ch.config.to as string) || '(no recipients)'}
                </>
              )}
              {ch.type === 'telegram' && (
                <>Chat ID: {(ch.config.chat_id as string) || '(not set)'}</>
              )}
              {ch.type === 'discord' && (
                <>{(ch.config.webhook_url as string)?.replace(/^https:\/\/discord\.com\/api\/webhooks\//, '.../')  || '(no webhook URL)'}</>
              )}
              {ch.type === 'slack' && (
                <>{(ch.config.webhook_url as string)?.replace(/^https:\/\/hooks\.slack\.com\//, '.../')  || '(no webhook URL)'}</>
              )}
              {ch.type === 'pushover' && (
                <>User: {(ch.config.user_key as string)?.slice(0, 8) || '(not set)'}...</>
              )}
              {ch.type === 'gotify' && (
                <>{(ch.config.server_url as string) || '(no server URL)'}</>
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
            <div className="overflow-y-auto scrollbar-thin px-6">
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
              {editing.type === 'smtp' && (
                <SmtpConfig
                  config={(editing.config || {}) as Record<string, unknown>}
                  onChange={config => setEditing({ ...editing, config })}
                />
              )}
              {editing.type === 'telegram' && (
                <TelegramConfig
                  config={(editing.config || {}) as Record<string, unknown>}
                  onChange={config => setEditing({ ...editing, config })}
                />
              )}
              {(editing.type === 'discord' || editing.type === 'slack') && (
                <WebhookURLConfig
                  config={(editing.config || {}) as Record<string, unknown>}
                  onChange={config => setEditing({ ...editing, config })}
                  label={editing.type === 'discord' ? 'Discord Webhook URL' : 'Slack Webhook URL'}
                  placeholder={editing.type === 'discord' ? 'https://discord.com/api/webhooks/...' : 'https://hooks.slack.com/services/...'}
                  hint={editing.type === 'discord'
                    ? 'Server Settings > Integrations > Webhooks > New Webhook > Copy Webhook URL'
                    : 'Create an Incoming Webhook app at api.slack.com/apps and copy the webhook URL'}
                />
              )}
              {editing.type === 'pushover' && (
                <PushoverConfig
                  config={(editing.config || {}) as Record<string, unknown>}
                  onChange={config => setEditing({ ...editing, config })}
                />
              )}
              {editing.type === 'gotify' && (
                <GotifyConfig
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

const NTFY_PRIORITIES = ['min', 'low', 'default', 'high', 'urgent'] as const

const DEFAULT_PRIORITY_MAP: Record<string, string> = {
  info: 'default',
  warning: 'high',
  critical: 'high',
}

function NtfyConfig({ config, onChange }: { config: Record<string, unknown>; onChange: (c: Record<string, unknown>) => void }) {
  const priority = (config.priority as string) || 'default'
  const priorityMap = (config.priority_map as Record<string, string>) || DEFAULT_PRIORITY_MAP

  const updatePriorityMap = (severity: string, value: string) => {
    onChange({ ...config, priority_map: { ...priorityMap, [severity]: value } })
  }

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
            value={priority}
            onChange={e => onChange({ ...config, priority: e.target.value })}
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
          >
            <option value="default">Default (auto from severity)</option>
            <option value="min">Min</option>
            <option value="low">Low</option>
            <option value="high">High</option>
            <option value="urgent">Urgent</option>
          </select>
        </Field>
      </div>
      {priority === 'default' && (
        <div className="space-y-2 pt-1">
          <label className="block text-xs text-gray-400">Severity → ntfy Priority</label>
          <div className="grid grid-cols-3 gap-2">
            {(['info', 'warning', 'critical'] as const).map(severity => (
              <Field key={severity} label={severity.charAt(0).toUpperCase() + severity.slice(1)}>
                <select
                  value={priorityMap[severity] || DEFAULT_PRIORITY_MAP[severity]}
                  onChange={e => updatePriorityMap(severity, e.target.value)}
                  className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
                >
                  {NTFY_PRIORITIES.map(p => <option key={p} value={p}>{p}</option>)}
                </select>
              </Field>
            ))}
          </div>
        </div>
      )}
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

function SmtpConfig({ config, onChange }: { config: Record<string, unknown>; onChange: (c: Record<string, unknown>) => void }) {
  return (
    <div className="space-y-3 p-3 bg-gray-800/50 rounded-lg">
      <div className="grid grid-cols-3 gap-3">
        <div className="col-span-2">
          <Field label="SMTP Host">
            <input
              value={(config.host as string) || ''}
              onChange={e => onChange({ ...config, host: e.target.value })}
              placeholder="smtp.gmail.com"
              className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
            />
          </Field>
        </div>
        <Field label="Port">
          <input
            type="number"
            value={(config.port as number) || 587}
            onChange={e => onChange({ ...config, port: parseInt(e.target.value) || 587 })}
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
          />
        </Field>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Username">
          <input
            value={(config.username as string) || ''}
            onChange={e => onChange({ ...config, username: e.target.value })}
            placeholder="user@example.com"
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
          />
        </Field>
        <Field label="Password">
          <input
            type="password"
            value={(config.password as string) || ''}
            onChange={e => onChange({ ...config, password: e.target.value })}
            placeholder="App password"
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
          />
        </Field>
      </div>
      <Field label="From Address">
        <input
          value={(config.from as string) || ''}
          onChange={e => onChange({ ...config, from: e.target.value })}
          placeholder="riot@example.com"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <Field label="To (comma-separated)">
        <input
          value={(config.to as string) || ''}
          onChange={e => onChange({ ...config, to: e.target.value })}
          placeholder="admin@example.com, ops@example.com"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <label className="flex items-center gap-2 text-sm text-gray-300">
        <input
          type="checkbox"
          checked={config.starttls !== false}
          onChange={e => onChange({ ...config, starttls: e.target.checked })}
          className="rounded bg-gray-800 border-gray-600"
        />
        Use STARTTLS
      </label>
      <p className="text-xs text-gray-500">
        For Gmail, use smtp.gmail.com:587 with an App Password. For local relays, disable STARTTLS and leave credentials empty.
      </p>
    </div>
  )
}

function TelegramConfig({ config, onChange }: { config: Record<string, unknown>; onChange: (c: Record<string, unknown>) => void }) {
  return (
    <div className="space-y-3 p-3 bg-gray-800/50 rounded-lg">
      <Field label="Bot Token">
        <input
          type="password"
          value={(config.bot_token as string) || ''}
          onChange={e => onChange({ ...config, bot_token: e.target.value })}
          placeholder="123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <Field label="Chat ID">
        <input
          value={(config.chat_id as string) || ''}
          onChange={e => onChange({ ...config, chat_id: e.target.value })}
          placeholder="-1001234567890"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <p className="text-xs text-gray-500">
        Create a bot via <a href="https://t.me/BotFather" target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:text-blue-300">@BotFather</a> to get the token.
        For the chat ID, add the bot to a group and send a message, then check <span className="font-mono">https://api.telegram.org/bot&lt;TOKEN&gt;/getUpdates</span> for the chat ID.
      </p>
    </div>
  )
}

function WebhookURLConfig({ config, onChange, label, placeholder, hint }: { config: Record<string, unknown>; onChange: (c: Record<string, unknown>) => void; label: string; placeholder: string; hint: string }) {
  return (
    <div className="space-y-3 p-3 bg-gray-800/50 rounded-lg">
      <Field label={label}>
        <input
          value={(config.webhook_url as string) || ''}
          onChange={e => onChange({ ...config, webhook_url: e.target.value })}
          placeholder={placeholder}
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <p className="text-xs text-gray-500">{hint}</p>
    </div>
  )
}

function PushoverConfig({ config, onChange }: { config: Record<string, unknown>; onChange: (c: Record<string, unknown>) => void }) {
  return (
    <div className="space-y-3 p-3 bg-gray-800/50 rounded-lg">
      <div className="grid grid-cols-2 gap-3">
        <Field label="API Token (Application)">
          <input
            type="password"
            value={(config.api_token as string) || ''}
            onChange={e => onChange({ ...config, api_token: e.target.value })}
            placeholder="azGDORePK8gMaC0QOYAMyEEuzJnyUI"
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
          />
        </Field>
        <Field label="User Key">
          <input
            type="password"
            value={(config.user_key as string) || ''}
            onChange={e => onChange({ ...config, user_key: e.target.value })}
            placeholder="uQiRzpo4DXghDmr9QzzfQu27cmVRsG"
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
          />
        </Field>
      </div>
      <p className="text-xs text-gray-500">
        Create an application at <a href="https://pushover.net/apps/build" target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:text-blue-300">pushover.net</a> to get the API token. Your user key is on the Pushover dashboard.
      </p>
    </div>
  )
}

function GotifyConfig({ config, onChange }: { config: Record<string, unknown>; onChange: (c: Record<string, unknown>) => void }) {
  return (
    <div className="space-y-3 p-3 bg-gray-800/50 rounded-lg">
      <Field label="Server URL">
        <input
          value={(config.server_url as string) || ''}
          onChange={e => onChange({ ...config, server_url: e.target.value })}
          placeholder="https://gotify.example.com"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <Field label="App Token">
        <input
          type="password"
          value={(config.app_token as string) || ''}
          onChange={e => onChange({ ...config, app_token: e.target.value })}
          placeholder="AKShieh.F8-kfha"
          className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-white text-sm"
        />
      </Field>
      <p className="text-xs text-gray-500">
        Create an application in Gotify's web UI to get the app token.
      </p>
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

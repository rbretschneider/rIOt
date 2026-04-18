import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { settingsApi, type ServerErrorAlertConfig } from '../../api/settings'

export default function LogSettings() {
  const [level, setLevel] = useState('')
  const [before, setBefore] = useState<string | undefined>()
  const [autoRefresh, setAutoRefresh] = useState(false)

  const { data: logs = [], isLoading, isFetching } = useQuery({
    queryKey: ['server-logs', level, before],
    queryFn: () => settingsApi.getLogs(level, 100, before),
    refetchInterval: autoRefresh ? 5000 : false,
  })

  function loadMore() {
    if (logs.length > 0) {
      setBefore(logs[logs.length - 1].timestamp)
    }
  }

  function resetPagination() {
    setBefore(undefined)
  }

  const levelColors: Record<string, string> = {
    INFO: 'bg-blue-900/50 text-blue-400',
    WARN: 'bg-amber-900/50 text-amber-400',
    WARNING: 'bg-amber-900/50 text-amber-400',
    ERROR: 'bg-red-900/50 text-red-400',
    DEBUG: 'bg-gray-800 text-gray-400',
  }

  return (
    <div>
      <ServerErrorAlertBar />
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">Server Logs</h2>
        <div className="flex items-center gap-3">
          <select
            value={level}
            onChange={e => { setLevel(e.target.value); resetPagination() }}
            className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm text-white"
          >
            <option value="">All Levels</option>
            <option value="INFO">Info</option>
            <option value="WARN">Warning</option>
            <option value="ERROR">Error</option>
          </select>
          <label className="flex items-center gap-1.5 text-sm text-gray-400 cursor-pointer">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={e => setAutoRefresh(e.target.checked)}
              className="rounded bg-gray-800 border-gray-600"
            />
            Auto-refresh
          </label>
          {isFetching && !isLoading && (
            <span className="text-xs text-gray-500">Refreshing...</span>
          )}
        </div>
      </div>

      <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-x-auto scrollbar-thin">
        {isLoading ? (
          <div className="p-8 text-center text-gray-500">Loading...</div>
        ) : (
          <table className="w-full text-sm min-w-[640px]">
            <thead>
              <tr className="text-left text-gray-400 border-b border-gray-800">
                <th className="px-4 py-3 w-44">Timestamp</th>
                <th className="px-4 py-3 w-20">Level</th>
                <th className="px-4 py-3">Message</th>
                <th className="px-4 py-3 w-40">Source</th>
              </tr>
            </thead>
            <tbody>
              {logs.map(log => (
                <tr key={log.id} className="border-b border-gray-800/50 text-gray-300">
                  <td className="px-4 py-2 text-xs text-gray-500 font-mono whitespace-nowrap">
                    {new Date(log.timestamp).toLocaleString()}
                  </td>
                  <td className="px-4 py-2">
                    <span className={`px-2 py-0.5 rounded text-xs font-medium ${levelColors[log.level] || 'bg-gray-800 text-gray-400'}`}>
                      {log.level}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-sm">
                    <span>{log.message}</span>
                    {log.attrs && Object.keys(log.attrs).length > 0 && (
                      <span className="ml-2 text-xs text-gray-500">
                        {Object.entries(log.attrs).map(([k, v]) => `${k}=${JSON.stringify(v)}`).join(' ')}
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-xs text-gray-600 font-mono">{log.source}</td>
                </tr>
              ))}
              {logs.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-8 text-center text-gray-500">
                    No server logs found. Logs at WARN level and above are stored by default.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>

      {logs.length >= 100 && (
        <div className="flex justify-center mt-4">
          <button
            onClick={loadMore}
            className="px-4 py-2 text-sm text-gray-400 hover:text-white border border-gray-700 rounded-md transition-colors"
          >
            Load More
          </button>
        </div>
      )}
    </div>
  )
}

// ServerErrorAlertBar is a compact inline settings strip that lets the user
// opt into notifications when the rIOt server itself starts logging too many
// errors. It sits above the log table because that's the context where the
// operator notices elevated error rates in the first place.
function ServerErrorAlertBar() {
  const qc = useQueryClient()
  const { data: cfg } = useQuery({
    queryKey: ['server-error-alert'],
    queryFn: () => settingsApi.getServerErrorAlert(),
  })
  const { data: channels = [] } = useQuery({
    queryKey: ['notification-channels'],
    queryFn: () => settingsApi.getNotificationChannels(),
  })

  const [local, setLocal] = useState<ServerErrorAlertConfig | null>(null)
  useEffect(() => { if (cfg) setLocal(cfg) }, [cfg])

  const saveMutation = useMutation({
    mutationFn: (c: ServerErrorAlertConfig) => settingsApi.saveServerErrorAlert(c),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['server-error-alert'] }),
  })

  if (!local) return null

  const enabledChannels = channels.filter(c => c.enabled)
  const dirty = cfg && (
    local.enabled !== cfg.enabled ||
    local.channel_id !== cfg.channel_id ||
    local.threshold !== cfg.threshold
  )

  return (
    <div className="mb-4 bg-gray-900 border border-gray-800 rounded-lg px-4 py-3 flex flex-wrap items-center gap-4">
      <label className="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
        <input
          type="checkbox"
          checked={local.enabled}
          onChange={e => setLocal({ ...local, enabled: e.target.checked })}
          className="rounded bg-gray-800 border-gray-600"
        />
        <span className="font-medium">Notify on elevated server errors</span>
      </label>
      <div className="flex items-center gap-2 text-sm text-gray-400">
        <span>when more than</span>
        <input
          type="number"
          min={1}
          value={local.threshold}
          onChange={e => setLocal({ ...local, threshold: parseInt(e.target.value) || 1 })}
          disabled={!local.enabled}
          className="w-16 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-white text-sm disabled:opacity-50"
        />
        <span>errors in {local.window_minutes} min via</span>
        <select
          value={local.channel_id || ''}
          onChange={e => setLocal({ ...local, channel_id: parseInt(e.target.value) || 0 })}
          disabled={!local.enabled || enabledChannels.length === 0}
          className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-white text-sm disabled:opacity-50"
        >
          <option value="">Select channel</option>
          {enabledChannels.map(c => (
            <option key={c.id} value={c.id}>{c.name} ({c.type})</option>
          ))}
        </select>
      </div>
      {enabledChannels.length === 0 && local.enabled && (
        <span className="text-xs text-amber-400">No enabled channels — configure one in Notification Channels.</span>
      )}
      <div className="ml-auto flex items-center gap-2">
        {saveMutation.isError && (
          <span className="text-xs text-red-400">{(saveMutation.error as Error).message}</span>
        )}
        <button
          onClick={() => saveMutation.mutate(local)}
          disabled={!dirty || saveMutation.isPending || (local.enabled && !local.channel_id)}
          className="px-3 py-1 text-xs bg-blue-600 hover:bg-blue-500 text-white rounded transition-colors disabled:opacity-50"
        >
          {saveMutation.isPending ? 'Saving...' : 'Save'}
        </button>
      </div>
    </div>
  )
}

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'

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

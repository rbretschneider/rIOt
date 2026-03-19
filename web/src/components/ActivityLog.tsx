import { useState, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useWebSocket } from '../hooks/useWebSocket'
import type { Command, CommandOutput, WSMessage } from '../types/models'

const PAGE_SIZE = 20

interface Props {
  deviceId: string
}

const UPPERCASE_WORDS = new Set(['os', 'io', 'ip', 'id', 'ssh', 'tls', 'dns', 'usb', 'ups', 'cpu', 'ram', 'gpu'])

function formatAction(action: string): string {
  return action
    .split('_')
    .map(w => UPPERCASE_WORDS.has(w) ? w.toUpperCase() : w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}

function formatDuration(ms: number | null): string {
  if (ms == null) return '-'
  if (ms < 1000) return `${ms}ms`
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const rem = s % 60
  return rem > 0 ? `${m}m ${rem}s` : `${m}m`
}

function statusColor(status: string): string {
  switch (status) {
    case 'success': return 'bg-emerald-500/20 text-emerald-400'
    case 'error': return 'bg-red-500/20 text-red-400'
    case 'pending': return 'bg-gray-500/20 text-gray-400'
    case 'sent': return 'bg-amber-500/20 text-amber-400'
    case 'queued': return 'bg-amber-500/20 text-amber-400'
    default: return 'bg-gray-500/20 text-gray-400'
  }
}

export default function ActivityLog({ deviceId }: Props) {
  const queryClient = useQueryClient()
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [statusFilter, setStatusFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const { data: commands, isLoading } = useQuery({
    queryKey: ['device-commands', deviceId, limit, statusFilter, actionFilter],
    queryFn: () => api.getDeviceCommands(deviceId, {
      limit,
      status: statusFilter || undefined,
      action: actionFilter || undefined,
    }),
    enabled: !!deviceId,
  })

  // Real-time: invalidate commands when a command_result arrives for this device
  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type === 'command_result' && msg.device_id === deviceId) {
      queryClient.invalidateQueries({ queryKey: ['device-commands', deviceId] })
    }
  }, [deviceId, queryClient])
  useWebSocket(handleWS)

  // Compute unique action names from current data for the filter dropdown
  const actionOptions = commands
    ? [...new Set(commands.map(c => c.action))].sort()
    : []

  return (
    <div>
      {/* Filters */}
      <div className="flex items-center gap-3 mb-3">
        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value)}
          className="px-2 py-1 text-xs bg-gray-800 border border-gray-700 rounded-md text-gray-300 focus:outline-none focus:border-gray-500"
        >
          <option value="">All statuses</option>
          <option value="success">Success</option>
          <option value="error">Error</option>
          <option value="pending">Pending</option>
          <option value="sent">Sent</option>
          <option value="queued">Queued</option>
        </select>
        <select
          value={actionFilter}
          onChange={e => setActionFilter(e.target.value)}
          className="px-2 py-1 text-xs bg-gray-800 border border-gray-700 rounded-md text-gray-300 focus:outline-none focus:border-gray-500"
        >
          <option value="">All actions</option>
          {actionOptions.map(a => (
            <option key={a} value={a}>{formatAction(a)}</option>
          ))}
        </select>
      </div>

      {isLoading ? (
        <div className="text-center py-6 text-gray-500 text-sm">Loading...</div>
      ) : !commands || commands.length === 0 ? (
        <div className="text-center py-6 text-gray-500 text-sm">No command history</div>
      ) : (
        <>
          <div className="max-h-96 overflow-y-auto scrollbar-thin">
            <table className="w-full text-sm min-w-[640px]">
              <thead className="sticky top-0 bg-gray-900 z-10">
                <tr className="text-gray-500 text-xs uppercase border-b border-gray-700">
                  <th className="text-left py-2">Timestamp</th>
                  <th className="text-left py-2">Action</th>
                  <th className="text-left py-2">Status</th>
                  <th className="text-right py-2">Duration</th>
                  <th className="text-right py-2">Exit Code</th>
                  <th className="text-left py-2">Summary</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {commands.map(cmd => (
                  <CommandRow
                    key={cmd.id}
                    cmd={cmd}
                    deviceId={deviceId}
                    expanded={expandedId === cmd.id}
                    onToggle={() => setExpandedId(expandedId === cmd.id ? null : cmd.id)}
                  />
                ))}
              </tbody>
            </table>
          </div>
          {commands.length >= limit && (
            <div className="mt-3 text-center">
              <button
                onClick={() => setLimit(l => l + PAGE_SIZE)}
                className="px-3 py-1.5 text-xs text-gray-400 hover:text-white bg-gray-800 hover:bg-gray-700 rounded-md transition-colors"
              >
                Load more
              </button>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function CommandRow({ cmd, deviceId, expanded, onToggle }: {
  cmd: Command
  deviceId: string
  expanded: boolean
  onToggle: () => void
}) {
  return (
    <>
      <tr
        onClick={onToggle}
        className="cursor-pointer hover:bg-gray-800/50 transition-colors"
      >
        <td className="py-1.5 font-mono text-xs text-gray-400 whitespace-nowrap">
          {new Date(cmd.created_at).toLocaleString()}
        </td>
        <td className="py-1.5 text-gray-200">{formatAction(cmd.action)}</td>
        <td className="py-1.5">
          <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${statusColor(cmd.status)}`}>
            {cmd.status}
          </span>
        </td>
        <td className="py-1.5 text-right font-mono text-gray-400">
          {formatDuration(cmd.duration_ms)}
        </td>
        <td className="py-1.5 text-right font-mono">
          {cmd.exit_code != null ? (
            <span className={cmd.exit_code !== 0 ? 'text-red-400' : 'text-gray-400'}>
              {cmd.exit_code}
            </span>
          ) : (
            <span className="text-gray-600">-</span>
          )}
        </td>
        <td className="py-1.5 text-gray-400 max-w-xs truncate" title={cmd.result_msg}>
          {cmd.result_msg || '-'}
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={6} className="p-0">
            <CommandDetail deviceId={deviceId} commandId={cmd.id} params={cmd.params} />
          </td>
        </tr>
      )}
    </>
  )
}

function CommandDetail({ deviceId, commandId, params }: {
  deviceId: string
  commandId: string
  params: Record<string, unknown>
}) {
  const { data: output, isLoading } = useQuery({
    queryKey: ['command-output', deviceId, commandId],
    queryFn: () => api.getCommandOutput(deviceId, commandId),
  })

  return (
    <div className="bg-gray-800/30 border-t border-gray-700 px-4 py-3 space-y-3">
      {/* Command params */}
      {params && Object.keys(params).length > 0 && (
        <div>
          <p className="text-xs text-gray-500 mb-1">Parameters</p>
          <pre className="text-xs text-gray-300 font-mono bg-gray-900 rounded p-2 overflow-x-auto scrollbar-thin">
            {JSON.stringify(params, null, 2)}
          </pre>
        </div>
      )}

      {/* Output */}
      <div>
        <p className="text-xs text-gray-500 mb-1">Output</p>
        {isLoading ? (
          <p className="text-xs text-gray-500">Loading...</p>
        ) : !output || output.length === 0 ? (
          <p className="text-xs text-gray-500 italic">No output captured</p>
        ) : (
          <pre className="text-xs text-gray-300 font-mono bg-gray-900 rounded p-2 max-h-64 overflow-auto scrollbar-thin whitespace-pre-wrap">
            {output.map(o => o.content).join('')}
          </pre>
        )}
      </div>
    </div>
  )
}

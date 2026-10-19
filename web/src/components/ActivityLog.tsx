import { useState, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { useWebSocket } from '../hooks/useWebSocket'
import type { Command, WSMessage } from '../types/models'
import { formatDuration } from '../utils/format'
import { isAllowedStatusFilter, normalizeStatus, STATUS_FILTER_OPTIONS } from '../utils/commandLifecycle'
import { Badge, Modal, Timestamp, enumLabel } from './ui'

const PAGE_SIZE = 20

interface Props {
  deviceId: string
}

export default function ActivityLog({ deviceId }: Props) {
  const queryClient = useQueryClient()
  const [limit, setLimit] = useState(PAGE_SIZE)
  const [statusFilter, setStatusFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [selectedCmd, setSelectedCmd] = useState<Command | null>(null)

  const { data: commands, isLoading } = useQuery({
    queryKey: ['device-commands', deviceId, limit, statusFilter, actionFilter],
    queryFn: () => api.getDeviceCommands(deviceId, {
      limit,
      // Only allowlisted canonical values reach the server, which answers
      // anything else with 400 invalid_filter.
      status: isAllowedStatusFilter(statusFilter) ? statusFilter : undefined,
      action: actionFilter || undefined,
    }),
    enabled: !!deviceId,
  })

  // Real-time: refresh the history when a command changes state for this device.
  // command_update is the lifecycle push; command_result is what a
  // pre-lifecycle server still sends.
  const handleWS = useCallback((msg: WSMessage) => {
    if (msg.type !== 'command_update' && msg.type !== 'command_result') return
    const target = msg.device_id ?? (msg.data as { device_id?: string } | undefined)?.device_id
    if (target === deviceId) {
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
          {STATUS_FILTER_OPTIONS.map(o => (
            <option key={o.value} value={o.value}>{o.label}</option>
          ))}
        </select>
        <select
          value={actionFilter}
          onChange={e => setActionFilter(e.target.value)}
          className="px-2 py-1 text-xs bg-gray-800 border border-gray-700 rounded-md text-gray-300 focus:outline-none focus:border-gray-500"
        >
          <option value="">All actions</option>
          {actionOptions.map(a => (
            <option key={a} value={a}>{enumLabel('commandAction', a)}</option>
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
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-gray-900 z-10">
                <tr className="text-gray-500 text-xs uppercase border-b border-gray-700">
                  <th className="text-left py-2 pr-3">Timestamp</th>
                  <th className="text-left py-2 pr-3">Action</th>
                  <th className="text-left py-2 pr-3">Status</th>
                  <th className="text-left py-2 pr-3 hidden sm:table-cell">Summary</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800/50">
                {commands.map(cmd => (
                  <tr
                    key={cmd.id}
                    onClick={() => setSelectedCmd(cmd)}
                    className="cursor-pointer hover:bg-gray-800/50 transition-colors"
                  >
                    <td className="py-1.5 pr-3 font-mono text-xs text-gray-400 whitespace-nowrap">
                      <Timestamp value={cmd.created_at} />
                    </td>
                    <td className="py-1.5 pr-3 text-gray-200">{enumLabel('commandAction', cmd.action)}</td>
                    <td className="py-1.5 pr-3">
                      <Badge category="commandStatus" value={normalizeStatus(cmd.status)} className="text-[10px] px-1.5" />
                    </td>
                    <td className="py-1.5 pr-3 text-gray-400 max-w-xs truncate hidden sm:table-cell">
                      {cmd.result_msg || '-'}
                    </td>
                  </tr>
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

      {selectedCmd && (
        <CommandDetailModal
          cmd={selectedCmd}
          deviceId={deviceId}
          onClose={() => setSelectedCmd(null)}
        />
      )}
    </div>
  )
}

function CommandDetailModal({ cmd, deviceId, onClose }: {
  cmd: Command
  deviceId: string
  onClose: () => void
}) {
  const { data: output, isLoading } = useQuery({
    queryKey: ['command-output', deviceId, cmd.id],
    queryFn: () => api.getCommandOutput(deviceId, cmd.id),
  })

  return (
    <Modal open size="lg" title={enumLabel('commandAction', cmd.action)} onClose={onClose} bodyClassName="space-y-4 py-4">
          {/* Metadata grid */}
          <div className="grid grid-cols-2 gap-3 text-xs">
            <div>
              <p className="text-gray-500 mb-0.5">Status</p>
              <Badge category="commandStatus" value={normalizeStatus(cmd.status)} className="text-[10px] px-1.5" />
            </div>
            <div>
              <p className="text-gray-500 mb-0.5">Timestamp</p>
              <p className="text-gray-300 font-mono"><Timestamp value={cmd.created_at} /></p>
            </div>
            <div>
              <p className="text-gray-500 mb-0.5">Duration</p>
              <p className="text-gray-300 font-mono">{cmd.duration_ms == null ? '-' : formatDuration(cmd.duration_ms / 1000)}</p>
            </div>
            <div>
              <p className="text-gray-500 mb-0.5">Exit Code</p>
              {cmd.exit_code != null ? (
                <p className={`font-mono ${cmd.exit_code !== 0 ? 'text-red-400' : 'text-gray-300'}`}>
                  {cmd.exit_code}
                </p>
              ) : (
                <p className="text-gray-600">-</p>
              )}
            </div>
          </div>

          {/* Summary */}
          {cmd.result_msg && (
            <div>
              <p className="text-xs text-gray-500 mb-1">Summary</p>
              <p className="text-sm text-gray-300 whitespace-pre-wrap break-words bg-gray-800/50 rounded p-2">
                {cmd.result_msg}
              </p>
            </div>
          )}

          {/* Parameters */}
          {cmd.params && Object.keys(cmd.params).length > 0 && (
            <div>
              <p className="text-xs text-gray-500 mb-1">Parameters</p>
              <pre className="text-xs text-gray-300 font-mono bg-gray-800/50 rounded p-2 overflow-x-auto scrollbar-thin">
                {JSON.stringify(cmd.params, null, 2)}
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
              <pre className="text-xs text-gray-300 font-mono bg-gray-800/50 rounded p-2 max-h-64 overflow-auto scrollbar-thin whitespace-pre-wrap">
                {output.map(o => o.content).join('')}
              </pre>
            )}
          </div>
    </Modal>
  )
}

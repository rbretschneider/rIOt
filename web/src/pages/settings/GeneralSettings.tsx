import { useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'

export default function GeneralSettings() {
  const { data: update } = useQuery({
    queryKey: ['server-update'],
    queryFn: api.getServerUpdate,
    refetchInterval: 6 * 60 * 60 * 1000,
    staleTime: 60 * 60 * 1000,
  })

  const hasUpdate = update?.update_available

  return (
    <div>
      <h2 className="text-lg font-semibold text-white mb-4">General Settings</h2>
      <div className="bg-gray-900 rounded-lg border border-gray-800 p-6 space-y-6">
        {/* Server Update */}
        <div>
          <h3 className="text-sm font-medium text-white mb-2">Server Version</h3>
          {hasUpdate ? (
            <div className="space-y-2">
              <p className="text-sm text-amber-400">Update available</p>
              <p className="text-xs text-gray-400">
                <span className="text-gray-500">Current:</span> {update.current_version}
                <span className="text-gray-500 mx-2">&rarr;</span>
                <span className="text-gray-500">Latest:</span> {update.latest_version}
              </p>
              <div className="bg-gray-800 rounded p-3">
                <p className="text-xs text-gray-400 mb-1">To update the server:</p>
                <code className="text-xs text-emerald-400 select-all">
                  docker compose pull && docker compose up -d
                </code>
              </div>
              {update.release_url && (
                <a
                  href={update.release_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-xs text-blue-400 hover:text-blue-300"
                >
                  View release notes
                </a>
              )}
            </div>
          ) : (
            <p className="text-xs text-gray-500">
              Server is up to date{update?.current_version ? ` (v${update.current_version})` : ''}.
            </p>
          )}
        </div>
        <div>
          <h3 className="text-sm font-medium text-white mb-2">Host Terminal</h3>
          <p className="text-xs text-gray-500 mb-3">
            Host terminal access is controlled per-agent via the agent config file.
            Set <code className="text-gray-400">host_terminal.enabled: true</code> in the agent's YAML config to allow host shell access.
          </p>
        </div>
        <div>
          <h3 className="text-sm font-medium text-white mb-2">Data Retention</h3>
          <p className="text-xs text-gray-500">
            Retention periods are configured via environment variables on the server:
            <code className="text-gray-400 ml-1">RIOT_RETENTION_DAYS</code> (default: 30 days for telemetry, 7 days for heartbeats, 90 days for events).
          </p>
        </div>
      </div>
    </div>
  )
}

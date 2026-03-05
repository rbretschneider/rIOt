export default function GeneralSettings() {
  return (
    <div>
      <h2 className="text-lg font-semibold text-white mb-4">General Settings</h2>
      <div className="bg-gray-900 rounded-lg border border-gray-800 p-6 space-y-6">
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

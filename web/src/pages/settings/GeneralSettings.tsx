import { useState } from 'react'
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
        {/* Password Change */}
        <PasswordChangeSection />

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

function PasswordChangeSection() {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [status, setStatus] = useState<'idle' | 'loading' | 'success' | 'error'>('idle')
  const [error, setError] = useState('')

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError('')

    if (newPassword.length < 8) {
      setError('New password must be at least 8 characters')
      return
    }
    if (newPassword !== confirmPassword) {
      setError('Passwords do not match')
      return
    }

    setStatus('loading')
    try {
      const res = await fetch('/api/v1/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({
          current_password: currentPassword,
          new_password: newPassword,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'Failed to change password' }))
        setError(data.error || 'Failed to change password')
        setStatus('error')
        return
      }
      setStatus('success')
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      setTimeout(() => setStatus('idle'), 3000)
    } catch {
      setError('Network error')
      setStatus('error')
    }
  }

  return (
    <div>
      <h3 className="text-sm font-medium text-white mb-2">Change Password</h3>
      <form onSubmit={handleSubmit} className="space-y-3 max-w-sm">
        <input
          type="password"
          value={currentPassword}
          onChange={e => setCurrentPassword(e.target.value)}
          placeholder="Current password"
          className="w-full px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <input
          type="password"
          value={newPassword}
          onChange={e => setNewPassword(e.target.value)}
          placeholder="New password (min. 8 characters)"
          className="w-full px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <input
          type="password"
          value={confirmPassword}
          onChange={e => setConfirmPassword(e.target.value)}
          placeholder="Confirm new password"
          className="w-full px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        {error && <p className="text-xs text-red-400">{error}</p>}
        {status === 'success' && <p className="text-xs text-emerald-400">Password changed successfully</p>}
        <button
          type="submit"
          disabled={status === 'loading' || !currentPassword || !newPassword || !confirmPassword}
          className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 disabled:text-gray-500 text-white rounded-md font-medium transition-colors"
        >
          {status === 'loading' ? 'Changing...' : 'Change Password'}
        </button>
      </form>
    </div>
  )
}

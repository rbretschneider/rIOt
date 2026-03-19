import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'
import { settingsApi } from '../../api/settings'

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

        {/* Device Registration */}
        <RegistrationKeySection />

        {/* Server Certificate */}
        <ServerCertSection />

        {/* TLS SANs (DDNS / external hostnames) */}
        <TLSSANSection />

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

function RegistrationKeySection() {
  const [key, setKey] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [status, setStatus] = useState<'idle' | 'loading' | 'success' | 'error'>('idle')
  const [error, setError] = useState('')

  useEffect(() => {
    settingsApi.getRegistrationKey()
      .then(data => {
        setKey(data.registration_key || '')
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
  }, [])

  async function handleSave() {
    setStatus('loading')
    setError('')
    try {
      await settingsApi.saveRegistrationKey(key)
      setStatus('success')
      setTimeout(() => setStatus('idle'), 3000)
    } catch (err: any) {
      setError(err?.message || 'Failed to save')
      setStatus('error')
    }
  }

  if (!loaded) return null

  return (
    <div>
      <h3 className="text-sm font-medium text-white mb-2">Device Registration</h3>
      <p className="text-xs text-gray-500 mb-3">
        Optional key to gate device registration. Leave empty for open registration (any device that can reach the server can register).
      </p>
      <div className="flex items-center gap-2 max-w-sm">
        <input
          type="text"
          value={key}
          onChange={e => setKey(e.target.value)}
          placeholder="Open registration (no key)"
          className="flex-1 px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <button
          onClick={handleSave}
          disabled={status === 'loading'}
          className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 disabled:text-gray-500 text-white rounded-md font-medium transition-colors"
        >
          {status === 'loading' ? 'Saving...' : 'Save'}
        </button>
      </div>
      {error && <p className="text-xs text-red-400 mt-1">{error}</p>}
      {status === 'success' && <p className="text-xs text-emerald-400 mt-1">Registration key saved</p>}
    </div>
  )
}

function ServerCertSection() {
  const { data: certInfo } = useQuery({
    queryKey: ['server-cert'],
    queryFn: settingsApi.getServerCert,
    staleTime: 60 * 1000,
  })

  if (!certInfo?.fingerprint) return null

  return (
    <div>
      <h3 className="text-sm font-medium text-white mb-2">Server Certificate</h3>
      <p className="text-xs text-gray-500 mb-2">
        Use this fingerprint with <code className="text-gray-400">--fingerprint</code> during agent install to verify the server on first connect.
      </p>
      <div className="bg-gray-800 rounded p-3">
        <code className="text-xs text-emerald-400 select-all break-all">{certInfo.fingerprint}</code>
      </div>
    </div>
  )
}

function TLSSANSection() {
  const [sans, setSans] = useState<string[]>([])
  const [input, setInput] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [status, setStatus] = useState<'idle' | 'saving' | 'regenerating' | 'success' | 'regen-success' | 'error'>('idle')
  const [error, setError] = useState('')

  useEffect(() => {
    settingsApi.getTLSSANs()
      .then(data => { setSans(data.sans || []); setLoaded(true) })
      .catch(() => setLoaded(true))
  }, [])

  function addSAN() {
    const trimmed = input.trim()
    if (!trimmed) return
    if (sans.includes(trimmed)) { setError('Already added'); return }
    setSans([...sans, trimmed])
    setInput('')
    setError('')
  }

  function removeSAN(i: number) {
    setSans(sans.filter((_, idx) => idx !== i))
  }

  async function handleSave() {
    setStatus('saving')
    setError('')
    try {
      const data = await settingsApi.saveTLSSANs(sans)
      setSans(data.sans || [])
      setStatus('success')
      setTimeout(() => setStatus('idle'), 3000)
    } catch (err: any) {
      setError(err?.message || 'Failed to save')
      setStatus('error')
    }
  }

  async function handleRegenerate() {
    setStatus('regenerating')
    setError('')
    try {
      // Save first, then regenerate
      await settingsApi.saveTLSSANs(sans)
      await settingsApi.regenerateTLS()
      setStatus('regen-success')
      setTimeout(() => setStatus('idle'), 5000)
    } catch (err: any) {
      setError(err?.message || 'Failed to regenerate')
      setStatus('error')
    }
  }

  if (!loaded) return null

  return (
    <div>
      <h3 className="text-sm font-medium text-white mb-2">Certificate SANs</h3>
      <p className="text-xs text-gray-500 mb-3">
        Additional hostnames and IPs to include in the self-signed TLS certificate.
        Add your DDNS domain or external IP here so remote agents can verify the server.
      </p>
      <div className="space-y-3 max-w-md">
        <div className="flex gap-2">
          <input
            type="text"
            value={input}
            onChange={e => { setInput(e.target.value); setError('') }}
            onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addSAN() } }}
            placeholder="e.g. mylab.duckdns.org"
            className="flex-1 px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <button
            type="button"
            onClick={addSAN}
            className="px-3 py-2 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded-md transition-colors"
          >
            Add
          </button>
        </div>
        {sans.length > 0 && (
          <div className="space-y-1">
            {sans.map((s, i) => (
              <div key={i} className="flex items-center justify-between px-3 py-1.5 bg-gray-800 rounded text-sm">
                <code className="text-emerald-400 text-xs">{s}</code>
                <button
                  onClick={() => removeSAN(i)}
                  className="text-gray-500 hover:text-red-400 text-xs transition-colors"
                >
                  remove
                </button>
              </div>
            ))}
          </div>
        )}
        <div className="flex gap-2">
          <button
            onClick={handleSave}
            disabled={status === 'saving' || status === 'regenerating'}
            className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 disabled:text-gray-500 text-white rounded-md font-medium transition-colors"
          >
            {status === 'saving' ? 'Saving...' : 'Save'}
          </button>
          <button
            onClick={handleRegenerate}
            disabled={status === 'saving' || status === 'regenerating'}
            className="px-4 py-2 text-sm bg-amber-600 hover:bg-amber-700 disabled:bg-gray-700 disabled:text-gray-500 text-white rounded-md font-medium transition-colors"
            title="Save SANs and regenerate the TLS certificate"
          >
            {status === 'regenerating' ? 'Regenerating...' : 'Save & Regenerate Cert'}
          </button>
        </div>
        {error && <p className="text-xs text-red-400">{error}</p>}
        {status === 'success' && <p className="text-xs text-emerald-400">SANs saved. Click "Save & Regenerate Cert" to apply.</p>}
        {status === 'regen-success' && <p className="text-xs text-emerald-400">Certificate regenerated with new SANs. Server is restarting.</p>}
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
      await settingsApi.changePassword(currentPassword, newPassword)
      setStatus('success')
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      setTimeout(() => setStatus('idle'), 3000)
    } catch (err: any) {
      setError(err?.message || 'Failed to change password')
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

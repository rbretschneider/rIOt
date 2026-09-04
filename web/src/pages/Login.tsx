import { useState, useEffect, useRef, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { savePassword, loadPassword, clearPassword, hasStoredPassword } from '../utils/credentialStore'
import { api } from '../api/client'

const isDemo = import.meta.env.VITE_DEMO === 'true'

// OIDC-001 §7.4: the fixed, closed error-code vocabulary the callback can
// carry in ?sso_error=. Any other value renders the generic fallback
// message (FR-037) — the raw parameter value is never rendered.
const SSO_ERROR_MESSAGES: Record<string, string> = {
  sso_failed: 'Sign-in could not be completed. Details are in the server log.',
  sso_expired: 'The sign-in attempt timed out. Try again.',
  sso_denied: 'The identity provider refused the sign-in for this account.',
  sso_unavailable: 'The identity provider could not be reached.',
}
const SSO_ERROR_FALLBACK = 'Sign-in could not be completed.'

interface LoginProps {
  onLogin: (password: string) => Promise<boolean>
}

export default function Login({ onLogin }: LoginProps) {
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(() => hasStoredPassword())
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const autoLoginAttempted = useRef(false)

  const [searchParams, setSearchParams] = useSearchParams()
  const ssoErrorCode = searchParams.get('sso_error')
  const [ssoErrorMessage] = useState(() =>
    ssoErrorCode ? (SSO_ERROR_MESSAGES[ssoErrorCode] ?? SSO_ERROR_FALLBACK) : ''
  )

  // Strip sso_error from the visible URL immediately so a subsequent
  // refresh does not re-display it (FR-037). Using setSearchParams (not
  // window.history.replaceState) keeps react-router-dom's internal
  // location in sync (AD-014, §12 note 16).
  useEffect(() => {
    if (!ssoErrorCode) return
    const next = new URLSearchParams(searchParams)
    next.delete('sso_error')
    setSearchParams(next, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // GET /api/v1/auth/oidc on mount — availability failing or timing out is
  // not an error state, it is "no button" (FR-035): no retry storm, no
  // console noise. Disabled entirely in demo builds.
  const { data: sso } = useQuery({
    queryKey: ['sso-availability'],
    queryFn: api.getSSOAvailability,
    retry: false,
    staleTime: Infinity,
    enabled: !isDemo,
  })

  // Auto-login from saved credentials or demo mode
  useEffect(() => {
    if (autoLoginAttempted.current) return
    autoLoginAttempted.current = true

    if (isDemo) {
      onLogin('demo')
      return
    }

    loadPassword().then(async (saved) => {
      if (!saved) return
      setLoading(true)
      const ok = await onLogin(saved)
      if (!ok) {
        clearPassword()
        setRemember(false)
        setError('Saved password is no longer valid')
      }
      setLoading(false)
    })
  }, [onLogin])

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    const ok = await onLogin(password)
    if (ok) {
      if (remember) {
        await savePassword(password)
      } else {
        clearPassword()
      }
    } else {
      setError('Invalid password')
      setPassword('')
    }
    setLoading(false)
  }

  return (
    <div className="min-h-screen bg-gray-950 flex items-center justify-center">
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-8 w-full max-w-sm">
        <h1 className="text-2xl font-bold text-white mb-6 text-center">rIOt</h1>
        {isDemo && (
          <div className="mb-4 px-3 py-2 bg-violet-900/30 border border-violet-800/50 rounded text-sm text-violet-300 text-center">
            Demo mode — type anything and click Sign in
          </div>
        )}
        {ssoErrorMessage && (
          <p className="mb-4 px-3 py-2 bg-red-900/30 border border-red-800/50 rounded text-sm text-red-300 text-center">
            {ssoErrorMessage}
          </p>
        )}
        <form onSubmit={handleSubmit} action="/api/v1/auth/login" method="POST" autoComplete="on">
          <label className="block text-sm text-gray-400 mb-2" htmlFor="username">
            Username
          </label>
          <input
            id="username"
            name="username"
            type="text"
            autoComplete="username"
            defaultValue="admin"
            readOnly
            tabIndex={-1}
            className="w-full px-3 py-2 mb-4 bg-gray-800 border border-gray-700 rounded-md text-gray-400 cursor-default focus:outline-none"
          />
          <label className="block text-sm text-gray-400 mb-2" htmlFor="password">
            Password
          </label>
          <input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            placeholder={isDemo ? 'Any password works' : 'Enter password'}
            autoFocus
            disabled={loading}
          />
          <label className="flex items-center gap-2 mt-3 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={remember}
              onChange={(e) => setRemember(e.target.checked)}
              className="w-4 h-4 rounded border-gray-600 bg-gray-800 text-blue-600 focus:ring-blue-500 focus:ring-offset-0 accent-blue-600"
            />
            <span className="text-sm text-gray-400">Remember me</span>
          </label>
          {error && (
            <p className="mt-2 text-sm text-red-400">{error}</p>
          )}
          <button
            type="submit"
            disabled={loading || (!isDemo && !password)}
            className="mt-4 w-full py-2 px-4 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 disabled:text-gray-500 text-white rounded-md font-medium transition-colors"
          >
            {loading ? 'Signing in...' : 'Sign in'}
          </button>
        </form>
        {sso?.available === true && (
          <>
            <div className="flex items-center gap-3 mt-6 mb-4">
              <div className="flex-1 h-px bg-gray-800" />
              <span className="text-xs text-gray-500">or</span>
              <div className="flex-1 h-px bg-gray-800" />
            </div>
            {/* Plain anchor performing a full-page navigation — never fetch/XHR
                (FR-034, AC-007). Kept outside the <form> so it cannot be
                confused with the password submit (§12 note 17). */}
            <a
              href="/api/v1/auth/oidc/start"
              className="block w-full py-2 px-4 bg-gray-800 hover:bg-gray-700 border border-gray-700 text-white text-center rounded-md font-medium transition-colors"
            >
              {sso.label}
            </a>
          </>
        )}
      </div>
    </div>
  )
}

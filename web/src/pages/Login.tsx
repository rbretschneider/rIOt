import { useState, useEffect, useRef, type FormEvent } from 'react'
import { savePassword, loadPassword, clearPassword, hasStoredPassword } from '../utils/credentialStore'

const isDemo = import.meta.env.VITE_DEMO === 'true'

interface LoginProps {
  onLogin: (password: string) => Promise<boolean>
}

export default function Login({ onLogin }: LoginProps) {
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(() => hasStoredPassword())
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const autoLoginAttempted = useRef(false)

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
      </div>
    </div>
  )
}

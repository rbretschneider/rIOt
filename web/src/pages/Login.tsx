import { useState, useEffect, type FormEvent } from 'react'

const isDemo = import.meta.env.VITE_DEMO === 'true'

interface LoginProps {
  onLogin: (password: string) => Promise<boolean>
}

export default function Login({ onLogin }: LoginProps) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // In demo mode, auto-login immediately
  useEffect(() => {
    if (isDemo) {
      onLogin('demo')
    }
  }, [onLogin])

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    const ok = await onLogin(password)
    setLoading(false)
    if (!ok) {
      setError('Invalid password')
      setPassword('')
    }
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
        <form onSubmit={handleSubmit} autoComplete="on">
          <label className="block text-sm text-gray-400 mb-2" htmlFor="username">
            Username
          </label>
          <input
            id="username"
            name="username"
            type="text"
            autoComplete="username"
            value="admin"
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

import { useState, useEffect, useCallback } from 'react'

const isDemo = import.meta.env.VITE_DEMO === 'true'

interface AuthState {
  authenticated: boolean
  needsSetup: boolean
  loading: boolean
  login: (password: string) => Promise<boolean>
  logout: () => Promise<void>
  recheckAuth: () => Promise<void>
}

const demoAuth: AuthState = {
  authenticated: true,
  needsSetup: false,
  loading: false,
  login: async () => true,
  logout: async () => {},
  recheckAuth: async () => {},
}

export function useAuth(): AuthState {
  const [authenticated, setAuthenticated] = useState(isDemo)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [loading, setLoading] = useState(!isDemo)

  const checkAuth = useCallback(async () => {
    if (isDemo) return
    try {
      const res = await fetch('/api/v1/auth/check', { credentials: 'same-origin' })
      const data = await res.json()
      setAuthenticated(data.authenticated === true)
      setNeedsSetup(data.needs_setup === true)
    } catch {
      setAuthenticated(false)
    }
  }, [])

  useEffect(() => {
    if (isDemo) return
    checkAuth().finally(() => setLoading(false))
  }, [checkAuth])

  const login = useCallback(async (password: string): Promise<boolean> => {
    if (isDemo) { setAuthenticated(true); return true }
    try {
      const res = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ password }),
      })
      if (res.ok) {
        setAuthenticated(true)
        setNeedsSetup(false)
        return true
      }
      return false
    } catch {
      return false
    }
  }, [])

  const logout = useCallback(async () => {
    if (isDemo) return
    await fetch('/api/v1/auth/logout', {
      method: 'POST',
      credentials: 'same-origin',
    }).catch(() => {})
    setAuthenticated(false)
  }, [])

  const recheckAuth = useCallback(async () => {
    if (isDemo) return
    setLoading(true)
    await checkAuth()
    setLoading(false)
  }, [checkAuth])

  if (isDemo) return demoAuth

  return { authenticated, needsSetup, loading, login, logout, recheckAuth }
}

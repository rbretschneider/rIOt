import { useState, useEffect, useCallback } from 'react'

interface AuthState {
  authenticated: boolean
  needsSetup: boolean
  loading: boolean
  login: (password: string) => Promise<boolean>
  logout: () => Promise<void>
  recheckAuth: () => Promise<void>
}

export function useAuth(): AuthState {
  const [authenticated, setAuthenticated] = useState(false)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [loading, setLoading] = useState(true)

  const checkAuth = useCallback(async () => {
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
    checkAuth().finally(() => setLoading(false))
  }, [checkAuth])

  const login = useCallback(async (password: string): Promise<boolean> => {
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
    await fetch('/api/v1/auth/logout', {
      method: 'POST',
      credentials: 'same-origin',
    }).catch(() => {})
    setAuthenticated(false)
  }, [])

  const recheckAuth = useCallback(async () => {
    setLoading(true)
    await checkAuth()
    setLoading(false)
  }, [checkAuth])

  return { authenticated, needsSetup, loading, login, logout, recheckAuth }
}

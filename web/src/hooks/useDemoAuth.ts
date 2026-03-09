/**
 * Demo replacement for hooks/useAuth — always authenticated, no-op login/logout.
 * Vite aliases this module over useAuth.ts when VITE_DEMO=true.
 */
export function useAuth() {
  return {
    authenticated: true,
    needsSetup: false,
    loading: false,
    login: async () => true,
    logout: async () => {},
    recheckAuth: async () => {},
  }
}

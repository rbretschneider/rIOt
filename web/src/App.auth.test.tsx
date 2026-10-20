import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import App from './App'

vi.mock('./contexts/WebSocketProvider', () => ({
  WebSocketProvider: ({ children }: { children: ReactNode }) => children,
  useWebSocket: () => {},
}))

function mountApp() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/learn/unknown']}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

beforeEach(() => localStorage.clear())
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('[DOCKER-UX-001 / OIDC AC-005] Authentication gates protected navigation queries', () => {
  it('keeps the logged-out login and OIDC button stable without protected requests or reloads', async () => {
    const reload = vi.spyOn(window.location, 'reload').mockImplementation(() => {})
    const fetcher = vi.fn(async (url: RequestInfo | URL) => {
      if (String(url) === '/api/v1/auth/check') return json({ authenticated: false, needs_setup: false })
      if (String(url) === '/api/v1/auth/oidc') return json({ available: true, label: 'Sign in with authentik' })
      return json({ error: 'Unauthorized' }, 401)
    })
    vi.stubGlobal('fetch', fetcher)
    mountApp()

    expect(await screen.findByRole('link', { name: 'Sign in with authentik' })).toHaveAttribute('href', '/api/v1/auth/oidc/start')
    expect(fetcher.mock.calls.map(([url]) => String(url))).toEqual([
      '/api/v1/auth/check', '/api/v1/auth/oidc',
    ])
    expect(reload).not.toHaveBeenCalled()
  })

  it('waits for authentication before fetching toggles, then applies the authenticated result', async () => {
    let resolveAuth!: (response: Response) => void
    const pendingAuth = new Promise<Response>(resolve => { resolveAuth = resolve })
    const fetcher = vi.fn(async (url: RequestInfo | URL) => {
      if (String(url) === '/api/v1/auth/check') return pendingAuth
      if (String(url) === '/api/v1/settings/features') return json({ docker: false })
      if (String(url) === '/api/v1/events/unread-count') return json({ count: 0 })
      return json({})
    })
    vi.stubGlobal('fetch', fetcher)
    mountApp()
    expect(screen.getByText('Loading...')).toBeInTheDocument()
    expect(fetcher.mock.calls.map(([url]) => String(url))).toEqual(['/api/v1/auth/check'])

    await act(async () => { resolveAuth(json({ authenticated: true, needs_setup: false })) })
    await waitFor(() => expect(fetcher.mock.calls.some(([url]) => String(url) === '/api/v1/settings/features')).toBe(true))
    await waitFor(() => expect(screen.queryByRole('link', { name: 'Docker' })).not.toBeInTheDocument())
    expect(screen.getByRole('link', { name: 'Fleet' })).toBeInTheDocument()
  })

  it('does not fetch protected settings during first-run setup', async () => {
    const fetcher = vi.fn(async () => json({ authenticated: false, needs_setup: true }))
    vi.stubGlobal('fetch', fetcher)
    mountApp()
    await waitFor(() => expect(screen.queryByText('Loading...')).not.toBeInTheDocument())
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
})

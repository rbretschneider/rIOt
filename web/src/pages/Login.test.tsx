import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import Login from './Login'

// vi.mock is hoisted, so the mock factory must be self-contained.
vi.mock('../api/client', () => ({
  api: {
    getSSOAvailability: vi.fn(),
  },
}))

import { api } from '../api/client'

function renderLogin(onLogin: (password: string) => Promise<boolean>, initialPath = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Login onLogin={onLogin} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.mocked(api.getSSOAvailability).mockReset()
  localStorage.clear()
})

// ===================================================================
// [AC-001] Dormant by default: no button
// ===================================================================
describe('[AC-001] Dormant by default: no button', () => {
  it('renders no SSO anchor when availability reports unavailable', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: false, label: '' })
    const onLogin = vi.fn().mockResolvedValue(true)
    renderLogin(onLogin)

    await waitFor(() => expect(api.getSSOAvailability).toHaveBeenCalled())
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('the password form is present and accepts a valid password', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: false, label: '' })
    const onLogin = vi.fn().mockResolvedValue(true)
    renderLogin(onLogin)

    fireEvent.change(screen.getByPlaceholderText('Enter password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    await waitFor(() => expect(onLogin).toHaveBeenCalledWith('hunter2'))
  })
})

// ===================================================================
// [AC-005] Custom button label
// ===================================================================
describe('[AC-005] Custom button label', () => {
  it('displays the label returned by the availability query', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: true, label: 'Sign in with authentik' })
    renderLogin(vi.fn())

    expect(await screen.findByRole('link', { name: 'Sign in with authentik' })).toBeInTheDocument()
  })
})

// ===================================================================
// [AC-007] SSO button is a full-page navigation
// ===================================================================
describe('[AC-007] SSO button is a full-page navigation', () => {
  it('is an anchor whose href is /api/v1/auth/oidc/start', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: true, label: 'Sign in with SSO' })
    renderLogin(vi.fn())

    const link = await screen.findByRole('link', { name: 'Sign in with SSO' })
    expect(link.tagName).toBe('A')
    expect(link.getAttribute('href')).toBe('/api/v1/auth/oidc/start')
  })

  it('activating it issues no fetch/XHR request', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: true, label: 'Sign in with SSO' })
    const fetchSpy = vi.spyOn(window, 'fetch')
    renderLogin(vi.fn())

    const link = await screen.findByRole('link', { name: 'Sign in with SSO' })
    // jsdom/happy-dom does not perform real navigation for anchor clicks, so
    // the assertion that matters is the negative one: clicking never
    // triggers a fetch/XHR call, which is what would happen if the button
    // were wired to onClick + fetch instead of being a plain anchor.
    fireEvent.click(link)
    expect(fetchSpy).not.toHaveBeenCalled()
  })
})

// ===================================================================
// [AC-017] IdP down: start degrades to the login screen
// ===================================================================
// Added by QA Engineer. The ADD's own §8 AC-017 mapping names
// Login.test.tsx for "message + working password form" but no test
// previously exercised the sso_unavailable code specifically — every
// existing Login.test.tsx case used sso_denied or an unrecognised code, so a
// broken/renamed sso_unavailable entry in Login.tsx's SSO_ERROR_MESSAGES
// table would not have been caught.
describe('[AC-017] IdP down: start degrades to the login screen', () => {
  it('displays the mapped message for sso_unavailable', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: true, label: 'Sign in with SSO' })
    renderLogin(vi.fn(), '/?sso_error=sso_unavailable')

    expect(await screen.findByText('The identity provider could not be reached.')).toBeInTheDocument()
  })

  it('the password form still accepts a valid password when the IdP is down', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: true, label: 'Sign in with SSO' })
    const onLogin = vi.fn().mockResolvedValue(true)
    renderLogin(onLogin, '/?sso_error=sso_unavailable')

    await screen.findByText('The identity provider could not be reached.')
    fireEvent.change(screen.getByPlaceholderText('Enter password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    await waitFor(() => expect(onLogin).toHaveBeenCalledWith('hunter2'))
  })
})

// ===================================================================
// [AC-018] IdP error response lands back on the login screen
// ===================================================================
describe('[AC-018] IdP error response lands back on the login screen', () => {
  it('displays the mapped message for sso_denied', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: true, label: 'Sign in with SSO' })
    renderLogin(vi.fn(), '/?sso_error=sso_denied')

    expect(await screen.findByText('The identity provider refused the sign-in for this account.')).toBeInTheDocument()
  })

  it('displays a generic message for an unrecognised code', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: false, label: '' })
    renderLogin(vi.fn(), '/?sso_error=something_unexpected')

    expect(await screen.findByText('Sign-in could not be completed.')).toBeInTheDocument()
  })

  it('the password form still accepts a valid password after an SSO error', async () => {
    vi.mocked(api.getSSOAvailability).mockResolvedValue({ available: true, label: 'Sign in with SSO' })
    const onLogin = vi.fn().mockResolvedValue(true)
    renderLogin(onLogin, '/?sso_error=sso_denied')

    await screen.findByText('The identity provider refused the sign-in for this account.')
    fireEvent.change(screen.getByPlaceholderText('Enter password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    await waitFor(() => expect(onLogin).toHaveBeenCalledWith('hunter2'))
  })
})

// ===================================================================
// [AC-025] Availability failure does not break the login screen
// ===================================================================
describe('[AC-025] Availability failure does not break the login screen', () => {
  it('renders no SSO button when the availability query rejects', async () => {
    vi.mocked(api.getSSOAvailability).mockRejectedValue(new Error('network error'))
    renderLogin(vi.fn())

    await waitFor(() => expect(api.getSSOAvailability).toHaveBeenCalled())
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('a valid password still signs the user in when availability fails', async () => {
    vi.mocked(api.getSSOAvailability).mockRejectedValue(new Error('network error'))
    const onLogin = vi.fn().mockResolvedValue(true)
    renderLogin(onLogin)

    fireEvent.change(screen.getByPlaceholderText('Enter password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

    await waitFor(() => expect(onLogin).toHaveBeenCalledWith('hunter2'))
  })
})

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TimePreferenceProvider } from '../contexts/TimePreferenceProvider'
import { makeTestQueryClient } from '../test/renderWithProviders'
import { Timestamp } from './ui/Timestamp'
import HeaderClock from './HeaderClock'
import ServerZoneSync from './ServerZoneSync'

// TIME-12H-001. Instant 2026-10-27T17:05:09Z. The real fetchJSON runs against a stubbed fetch, so the
// 404 and HTML cases exercise the same failure paths an older server would produce.
const NOW = Date.parse('2026-10-27T17:05:09Z')
const INSTANT = '2026-10-27T17:05:09Z'

const memoryStorage = () => ({ getItem: () => null, setItem: () => {} })

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function mount() {
  const client = makeTestQueryClient()
  const result = render(
    <QueryClientProvider client={client}>
      <TimePreferenceProvider storage={memoryStorage()} now={() => NOW} localTimeZone="America/Los_Angeles">
        <ServerZoneSync />
        <HeaderClock />
        <Timestamp value={INSTANT} />
      </TimePreferenceProvider>
    </QueryClientProvider>,
  )
  return { ...result, client }
}

const clock = () => screen.getByRole('button', { name: /Time display/ })
const stampText = () => screen.getByText(/^2026-10-27 /)

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('TIME-12H-001 [AC-019] the clock shows UTC until the zone arrives, then EDT on the same node', () => {
  it('formats as UTC while the request is pending and switches to EDT without a remount', async () => {
    let resolve!: (response: Response) => void
    fetchMock.mockImplementation(() => new Promise<Response>(r => { resolve = r }))
    mount()

    const timeNode = clock().querySelector('time')!
    expect(clock()).toHaveTextContent('5:05:09 PM UTC')
    expect(clock().textContent).toMatch(/UTC$/)

    resolve(json({ display_zone: 'America/New_York' }))

    await waitFor(() => expect(clock()).toHaveTextContent('1:05:09 PM EDT'))
    expect(clock().textContent).toMatch(/EDT$/)
    expect(clock().querySelector('time'), 'the clock node must not be remounted').toBe(timeNode)
    expect(stampText()).toHaveTextContent('2026-10-27 1:05:09 PM EDT')
  })

  it('requests the admin endpoint exactly once with the session cookie', async () => {
    fetchMock.mockImplementation(() => Promise.resolve(json({ display_zone: 'America/New_York' })))
    mount()
    await waitFor(() => expect(clock()).toHaveTextContent('EDT'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/server/time-display', { credentials: 'same-origin' })
  })
})

describe('TIME-12H-001 [AC-018] a rejected API zone formats as UTC and names Server (UTC)', () => {
  const rejected: Array<[string, unknown]> = [
    ['Mars/Olympus', { display_zone: 'Mars/Olympus' }],
    ['Local', { display_zone: 'Local' }],
    ['an empty string', { display_zone: '' }],
    ['whitespace only', { display_zone: '   ' }],
    ['a leading space', { display_zone: ' America/New_York' }],
    ['markup', { display_zone: '<img src=x>' }],
    ['a trailing newline', { display_zone: 'America/New_York\n' }],
    ['path traversal', { display_zone: '../etc/passwd' }],
    ['65 letters', { display_zone: 'a'.repeat(65) }],
    ['the number 5', { display_zone: 5 }],
    ['null', { display_zone: null }],
    ['a missing field', {}],
  ]

  it.each(rejected)('%s leaves the Server zone as UTC', async (_label, body) => {
    fetchMock.mockImplementation(() => Promise.resolve(json(body)))
    const { client } = mount()
    await waitFor(() => expect(client.getQueryState(['time-display'])?.status).toBe('success'))

    expect(clock()).toHaveTextContent('5:05:09 PM UTC')
    expect(stampText()).toHaveTextContent('2026-10-27 5:05:09 PM UTC')
    expect(clock()).toHaveAccessibleName('Time display: Server (UTC), 12-hour. Change time display')
    await userEvent.click(clock())
    expect(screen.getByRole('radio', { name: 'Server (UTC)' })).toBeChecked()
  })
})

describe('TIME-12H-001 [NFR-006] an older server leaves Server as UTC', () => {
  it('a 404 from an older server leaves Server as UTC', async () => {
    fetchMock.mockImplementation(() => Promise.resolve(new Response('not found', { status: 404, statusText: 'Not Found' })))
    mount()
    // One automatic retry, then the query settles in error.
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(clock()).toHaveTextContent('5:05:09 PM UTC')
    expect(clock()).toHaveAccessibleName('Time display: Server (UTC), 12-hour. Change time display')
  })

  it('an older server answering 200 text/html (SPA catch-all) leaves Server as UTC', async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(new Response('<!doctype html><html></html>', { status: 200, headers: { 'Content-Type': 'text/html' } })),
    )
    mount()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(clock()).toHaveTextContent('5:05:09 PM UTC')
    expect(stampText()).toHaveTextContent('2026-10-27 5:05:09 PM UTC')
  })

  it('a network error leaves Server as UTC and does not throw', async () => {
    fetchMock.mockImplementation(() => Promise.reject(new TypeError('Failed to fetch')))
    expect(() => mount()).not.toThrow()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(clock()).toHaveTextContent('5:05:09 PM UTC')
  })
})

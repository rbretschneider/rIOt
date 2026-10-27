import type { ReactElement, ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, type RenderResult } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { HOUR_CYCLE_STORAGE_KEY, TIME_ZONE_STORAGE_KEY, TimePreferenceProvider } from '../contexts/TimePreferenceProvider'
import { ToastProvider } from '../components/ui/Toast'

type PreferenceStorage = { getItem: (key: string) => string | null; setItem: (key: string, value: string) => void }

type TimeZoneOptions = {
  localTimeZone?: string
  /**
   * Omitted: the display is pinned to UTC and 24-hour (the pre-TIME-12H-001 default), so page tests keep
   * asserting "14:03:22 UTC" strings. Pass null for the real defaults (Server zone, 12-hour), or a storage to seed.
   */
  storage?: PreferenceStorage | null
  now?: () => number
  /** Initial server display zone, validated by the provider exactly like an API value. */
  serverZone?: unknown
}

/** Read-only storage holding the legacy-test display: UTC, 24-hour. Writes are ignored. */
const PINNED_UTC_24H: PreferenceStorage = {
  getItem: key => (key === TIME_ZONE_STORAGE_KEY ? 'UTC' : key === HOUR_CYCLE_STORAGE_KEY ? '24h' : null),
  setItem: () => {},
}

export interface RenderOptions {
  /** Initial router entry. Defaults to "/". */
  route?: string
  /** Wraps `ui` in <Route path=...> so useParams works. */
  path?: string
  /** Extra routes, for asserting where navigation lands. */
  routes?: Array<{ path: string; element: ReactNode }>
  queryClient?: QueryClient
  timeZone?: TimeZoneOptions
  toast?: { now?: () => number }
  /** Renders the current router location in a [data-testid="location"] element. */
  showLocation?: boolean
}

export function makeTestQueryClient(): QueryClient {
  return new QueryClient({
    // retryDelay 0: queries that set their own retry policy (device pages) still settle at once in tests.
    defaultOptions: { queries: { retry: false, retryDelay: 0, gcTime: 0 }, mutations: { retry: false } },
  })
}

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname + location.search}</div>
}

interface TestProvidersProps extends Omit<RenderOptions, 'route'> {
  children: ReactNode
}

/** QueryClient, router, time preference and toasts: everything a page needs. */
export function TestProviders({
  children,
  path,
  routes,
  queryClient,
  timeZone,
  toast,
  showLocation,
}: TestProvidersProps) {
  const client = queryClient ?? makeTestQueryClient()
  const content =
    path || routes ? (
      <Routes>
        <Route path={path ?? '*'} element={children} />
        {routes?.map(r => <Route key={r.path} path={r.path} element={r.element} />)}
      </Routes>
    ) : (
      children
    )
  return (
    <QueryClientProvider client={client}>
      <TimePreferenceProvider
        storage={timeZone?.storage === undefined ? PINNED_UTC_24H : timeZone.storage}
        localTimeZone={timeZone?.localTimeZone}
        now={timeZone?.now}
        serverZone={timeZone?.serverZone}
      >
        <ToastProvider now={toast?.now}>
          {content}
          {showLocation && <LocationProbe />}
        </ToastProvider>
      </TimePreferenceProvider>
    </QueryClientProvider>
  )
}

export function renderWithProviders(ui: ReactElement, options: RenderOptions = {}): RenderResult & { queryClient: QueryClient } {
  const queryClient = options.queryClient ?? makeTestQueryClient()
  const result = render(
    <MemoryRouter initialEntries={[options.route ?? '/']}>
      <TestProviders {...options} queryClient={queryClient}>
        {ui}
      </TestProviders>
    </MemoryRouter>,
  )
  return Object.assign(result, { queryClient })
}

import type { ReactElement, ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, type RenderResult } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { TimePreferenceProvider } from '../contexts/TimePreferenceProvider'
import { ToastProvider } from '../components/ui/Toast'

type TimeZoneOptions = {
  localTimeZone?: string
  storage?: { getItem: (key: string) => string | null; setItem: (key: string, value: string) => void } | null
  now?: () => number
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
    defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } },
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
      <TimePreferenceProvider storage={timeZone?.storage ?? null} localTimeZone={timeZone?.localTimeZone} now={timeZone?.now}>
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

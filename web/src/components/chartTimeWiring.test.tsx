// Added by QA Engineer
// Covers AC-118 (and the wiring half of AC-119, whose hover screenshot was not captured).
// format.test.ts proves the tick and tooltip formatters; nothing proved that MetricChart and
// ContainerMetricChart actually hand them to recharts. recharts is replaced with recording stubs so the
// formatter props that reach XAxis and Tooltip can be called directly under the Local preference.
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/react'
import { HOUR_CYCLE_STORAGE_KEY, TIME_ZONE_STORAGE_KEY, TimePreferenceProvider } from '../contexts/TimePreferenceProvider'
import MetricChart from './MetricChart'
import ContainerMetricChart from './ContainerMetricChart'

const captured = vi.hoisted(() => ({
  tickFormatter: undefined as undefined | ((v: number) => string),
  labelFormatter: undefined as undefined | ((v: number) => string),
}))

vi.mock('recharts', () => {
  const Pass = ({ children }: { children?: ReactNode }) => <div>{children}</div>
  return {
    ResponsiveContainer: Pass,
    AreaChart: Pass,
    Area: () => null,
    YAxis: () => null,
    XAxis: (props: { tickFormatter?: (v: number) => string }) => {
      captured.tickFormatter = props.tickFormatter
      return null
    },
    Tooltip: (props: { labelFormatter?: (v: number) => string }) => {
      captured.labelFormatter = props.labelFormatter
      return null
    },
  }
})

const INSTANT = Date.parse('2026-10-17T14:03:22Z')
// TIME-12H-001 AD-013: the stub answers per key. No seed is the new default (Server, 12-hour); the Local cases
// seed Local + 24h so their strings are unchanged.
interface Seed {
  zone?: string
  cycle?: string
}
const storage = (seed: Seed = {}) => ({
  getItem: (key: string) => (key === TIME_ZONE_STORAGE_KEY ? (seed.zone ?? null) : key === HOUR_CYCLE_STORAGE_KEY ? (seed.cycle ?? null) : null),
  setItem: () => {},
})

function mount(ui: ReactNode, seed?: Seed, serverZone?: unknown) {
  return render(
    <TimePreferenceProvider storage={storage(seed)} localTimeZone="America/Los_Angeles" serverZone={serverZone}>
      {ui}
    </TimePreferenceProvider>,
  )
}

const LOCAL_24H: Seed = { zone: 'Local', cycle: '24h' }

beforeEach(() => {
  captured.tickFormatter = undefined
  captured.labelFormatter = undefined
})

const heartbeats = [
  {
    id: 1,
    device_id: 'd1',
    timestamp: '2026-10-17T14:03:22Z',
    data: { cpu_percent: 12 },
  },
] as never

const containerMetrics = [
  {
    container_id: 'c1',
    timestamp: '2026-10-17T14:03:22Z',
    cpu_percent: 5,
    mem_usage: 10,
    mem_limit: 100,
  },
] as never

describe('[UIF AC-118] QA: MetricChart passes the zone-aware formatters to recharts', () => {
  it('uses the default preference (Server zone, 12-hour; UTC until the zone is known) for the tick and tooltip', () => {
    mount(<MetricChart heartbeats={heartbeats} metricKey="cpu_percent" label="CPU" color="#000" />)
    expect(captured.tickFormatter?.(INSTANT)).toBe('2:03 PM')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 2:03:22 PM UTC')
  })

  it('shows local time of day on ticks and a zone-labelled tooltip under the Local preference', () => {
    mount(<MetricChart heartbeats={heartbeats} metricKey="cpu_percent" label="CPU" color="#000" />, LOCAL_24H)
    expect(captured.tickFormatter?.(INSTANT)).toBe('07:03')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 07:03:22 PDT')
  })
})

describe('[UIF AC-118] QA: ContainerMetricChart passes the zone-aware formatters to recharts', () => {
  it('uses the default preference (Server zone, 12-hour; UTC until the zone is known) for the tick and tooltip', () => {
    mount(<ContainerMetricChart metrics={containerMetrics} mode="cpu" label="CPU" color="#000" />)
    expect(captured.tickFormatter?.(INSTANT)).toBe('2:03 PM')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 2:03:22 PM UTC')
  })

  it('shows local time of day on ticks and a zone-labelled tooltip under the Local preference', () => {
    mount(<ContainerMetricChart metrics={containerMetrics} mode="cpu" label="CPU" color="#000" />, LOCAL_24H)
    expect(captured.tickFormatter?.(INSTANT)).toBe('07:03')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 07:03:22 PDT')
  })
})

describe('[UIF AC-118] [TIME-12H-001 AC-013] MetricChart and ContainerMetricChart ticks follow the hour cycle', () => {
  const charts: Array<[string, () => ReactNode]> = [
    ['MetricChart', () => <MetricChart heartbeats={heartbeats} metricKey="cpu_percent" label="CPU" color="#000" />],
    ['ContainerMetricChart', () => <ContainerMetricChart metrics={containerMetrics} mode="cpu" label="CPU" color="#000" />],
  ]

  it.each(charts)('%s shows a 12-hour tick without a zone label and a labelled tooltip in the server zone', (_name, chart) => {
    mount(chart(), undefined, 'America/New_York')
    expect(captured.tickFormatter?.(INSTANT), 'tick is h:mm AM|PM with no zone label').toBe('10:03 AM')
    expect(captured.labelFormatter?.(INSTANT), 'tooltip is the second form with its label').toBe('2026-10-17 10:03:22 AM EDT')
  })

  it.each(charts)('%s shows a 24-hour tick when the 24-hour cycle is stored', (_name, chart) => {
    mount(chart(), { zone: 'Server', cycle: '24h' }, 'America/New_York')
    expect(captured.tickFormatter?.(INSTANT)).toBe('10:03')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 10:03:22 EDT')
  })

  it.each(charts)('%s shows midnight as 12:00 AM in a 12-hour tick', (_name, chart) => {
    mount(chart(), undefined, 'America/New_York')
    expect(captured.tickFormatter?.(Date.parse('2026-10-27T04:00:00Z'))).toBe('12:00 AM')
  })
})

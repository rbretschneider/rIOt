// Added by QA Engineer
// Covers AC-118 (and the wiring half of AC-119, whose hover screenshot was not captured).
// format.test.ts proves the tick and tooltip formatters; nothing proved that MetricChart and
// ContainerMetricChart actually hand them to recharts. recharts is replaced with recording stubs so the
// formatter props that reach XAxis and Tooltip can be called directly under the Local preference.
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/react'
import { TimePreferenceProvider } from '../contexts/TimePreferenceProvider'
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
const storage = (initial?: string) => ({
  getItem: () => initial ?? null,
  setItem: () => {},
})

function mount(ui: ReactNode, stored?: string) {
  return render(
    <TimePreferenceProvider storage={storage(stored)} localTimeZone="America/Los_Angeles">
      {ui}
    </TimePreferenceProvider>,
  )
}

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
  it('uses UTC for the tick and tooltip by default', () => {
    mount(<MetricChart heartbeats={heartbeats} metricKey="cpu_percent" label="CPU" color="#000" />)
    expect(captured.tickFormatter?.(INSTANT)).toBe('14:03')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 14:03:22 UTC')
  })

  it('shows local time of day on ticks and a zone-labelled tooltip under the Local preference', () => {
    mount(<MetricChart heartbeats={heartbeats} metricKey="cpu_percent" label="CPU" color="#000" />, 'Local')
    expect(captured.tickFormatter?.(INSTANT)).toBe('07:03')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 07:03:22 PDT')
  })
})

describe('[UIF AC-118] QA: ContainerMetricChart passes the zone-aware formatters to recharts', () => {
  it('uses UTC for the tick and tooltip by default', () => {
    mount(<ContainerMetricChart metrics={containerMetrics} mode="cpu" label="CPU" color="#000" />)
    expect(captured.tickFormatter?.(INSTANT)).toBe('14:03')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 14:03:22 UTC')
  })

  it('shows local time of day on ticks and a zone-labelled tooltip under the Local preference', () => {
    mount(<ContainerMetricChart metrics={containerMetrics} mode="cpu" label="CPU" color="#000" />, 'Local')
    expect(captured.tickFormatter?.(INSTANT)).toBe('07:03')
    expect(captured.labelFormatter?.(INSTANT)).toBe('2026-10-17 07:03:22 PDT')
  })
})

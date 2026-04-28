import { useState, useMemo, memo } from 'react'
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import type { DeviceSeries, FleetTimeSeries } from '../../hooks/useFleetMetrics'

interface PerDeviceChartProps {
  title: string
  series: DeviceSeries[]
  dataKey: 'cpuPoints' | 'memPoints'
  unit?: string
  'data-testid'?: string
}

/** Stable color palette for up to ~20 devices */
const COLORS = [
  '#10b981', '#3b82f6', '#f59e0b', '#ef4444', '#8b5cf6',
  '#06b6d4', '#84cc16', '#f97316', '#ec4899', '#14b8a6',
  '#a855f7', '#eab308', '#6366f1', '#22c55e', '#0ea5e9',
  '#f43f5e', '#d946ef', '#fb923c', '#4ade80', '#60a5fa',
]

function colorForIndex(i: number): string {
  return COLORS[i % COLORS.length]
}

/** Converts per-device time series into recharts data format */
function buildChartData(series: DeviceSeries[], key: 'cpuPoints' | 'memPoints') {
  // Collect all unique timestamps
  const tsSet = new Set<string>()
  for (const s of series) {
    for (const p of s[key]) tsSet.add(p.timestamp)
  }
  const sortedTs = [...tsSet].sort()

  // Build a lookup: deviceId → { timestamp → value }
  const lookup = new Map<string, Map<string, number>>()
  for (const s of series) {
    const m = new Map<string, number>()
    for (const p of s[key]) m.set(p.timestamp, p.value)
    lookup.set(s.deviceId, m)
  }

  return sortedTs.map(ts => {
    const point: Record<string, string | number> = { ts }
    for (const s of series) {
      point[s.deviceId] = lookup.get(s.deviceId)?.get(ts) ?? null as unknown as number
    }
    return point
  })
}

/** Per-device chart (CPU% or Memory%) with hover-to-isolate (FR-022 / AC-011) */
const PerDeviceChart = memo(function PerDeviceChart({
  title,
  series,
  dataKey,
  unit = '%',
  'data-testid': testId,
}: PerDeviceChartProps) {
  const [activeDeviceId, setActiveDeviceId] = useState<string | null>(null)
  const chartData = useMemo(() => buildChartData(series, dataKey), [series, dataKey])

  return (
    <div data-testid={testId ?? `chart-${title.toLowerCase().replace(/\s+/g, '-')}`}>
      <p className="text-xs text-gray-400 mb-1 font-medium">{title}</p>
      <ResponsiveContainer width="100%" height={120}>
        <AreaChart
          data={chartData}
          onMouseMove={e => {
            if (e.activeDataKey != null) {
              setActiveDeviceId(String(e.activeDataKey))
            }
          }}
          onMouseLeave={() => setActiveDeviceId(null)}
        >
          <defs>
            {series.map((s, i) => (
              <linearGradient key={s.deviceId} id={`grad-${s.deviceId.replace(/[^a-z0-9]/gi, '_')}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={colorForIndex(i)} stopOpacity={0.4} />
                <stop offset="95%" stopColor={colorForIndex(i)} stopOpacity={0} />
              </linearGradient>
            ))}
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
          <XAxis dataKey="ts" hide />
          <YAxis domain={[0, 100]} tick={{ fontSize: 10, fill: '#6b7280' }} width={28} tickFormatter={v => `${v}`} />
          <Tooltip
            formatter={(value: number | undefined, key: string | undefined) => {
              const s = series.find(s => s.deviceId === key)
              return [`${(value ?? 0).toFixed(1)}${unit}`, s?.hostname ?? (key ?? '')]
            }}
            contentStyle={{ backgroundColor: '#111827', border: '1px solid #374151', borderRadius: 6 }}
            labelStyle={{ display: 'none' }}
          />
          {series.map((s, i) => {
            const isActive = activeDeviceId === null || activeDeviceId === s.deviceId
            return (
              <Area
                key={s.deviceId}
                type="monotone"
                dataKey={s.deviceId}
                stroke={colorForIndex(i)}
                strokeWidth={isActive ? 2 : 1}
                strokeOpacity={isActive ? 1 : 0.25}
                fill={`url(#grad-${s.deviceId.replace(/[^a-z0-9]/gi, '_')})`}
                fillOpacity={isActive ? 1 : 0.1}
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
                name={s.hostname}
              />
            )
          })}
          {series.length <= 5 && (
            <Legend
              formatter={(value: string) => {
                const s = series.find(s => s.deviceId === value)
                return <span style={{ fontSize: 10, color: '#9ca3af' }}>{s?.hostname ?? value}</span>
              }}
            />
          )}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
})

interface FleetChartProps {
  title: string
  series1: { timestamp: string; value: number }[]
  series2: { timestamp: string; value: number }[]
  label1: string
  label2: string
  color1?: string
  color2?: string
  formatter?: (v: number) => string
  'data-testid'?: string
}

/** Fleet-summed stacked area chart (network, disk I/O) */
const FleetStackedChart = memo(function FleetStackedChart({
  title,
  series1,
  series2,
  label1,
  label2,
  color1 = '#10b981',
  color2 = '#3b82f6',
  formatter,
  'data-testid': testId,
}: FleetChartProps) {
  // Merge both series on timestamp
  const tsSet = new Set<string>()
  for (const p of series1) tsSet.add(p.timestamp)
  for (const p of series2) tsSet.add(p.timestamp)

  const lookup1 = new Map(series1.map(p => [p.timestamp, p.value]))
  const lookup2 = new Map(series2.map(p => [p.timestamp, p.value]))

  const chartData = [...tsSet].sort().map(ts => ({
    ts,
    s1: lookup1.get(ts) ?? 0,
    s2: lookup2.get(ts) ?? 0,
  }))

  const fmt = formatter ?? ((v: number) => `${(v / 1024).toFixed(1)} KB/s`)

  return (
    <div data-testid={testId ?? `chart-${title.toLowerCase().replace(/\s+/g, '-')}`}>
      <p className="text-xs text-gray-400 mb-1 font-medium">{title}</p>
      <ResponsiveContainer width="100%" height={120}>
        <AreaChart data={chartData}>
          <defs>
            <linearGradient id={`grad-${title}-s1`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor={color1} stopOpacity={0.4} />
              <stop offset="95%" stopColor={color1} stopOpacity={0} />
            </linearGradient>
            <linearGradient id={`grad-${title}-s2`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor={color2} stopOpacity={0.4} />
              <stop offset="95%" stopColor={color2} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
          <XAxis dataKey="ts" hide />
          <YAxis tick={{ fontSize: 10, fill: '#6b7280' }} width={28} tickFormatter={v => `${(v / 1024).toFixed(0)}`} />
          <Tooltip
            formatter={(value: number | undefined, key: string | undefined) => [fmt(value ?? 0), key === 's1' ? label1 : label2]}
            contentStyle={{ backgroundColor: '#111827', border: '1px solid #374151', borderRadius: 6 }}
            labelStyle={{ display: 'none' }}
          />
          <Area type="monotone" dataKey="s1" name={label1} stroke={color1} strokeWidth={1.5} fill={`url(#grad-${title}-s1)`} dot={false} isAnimationActive={false} />
          <Area type="monotone" dataKey="s2" name={label2} stroke={color2} strokeWidth={1.5} fill={`url(#grad-${title}-s2)`} dot={false} isAnimationActive={false} />
          <Legend formatter={(v: string) => <span style={{ fontSize: 10, color: '#9ca3af' }}>{v}</span>} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
})

interface SmallMultiplesProps {
  perDeviceSeries: DeviceSeries[]
  fleetTimeSeries: FleetTimeSeries
}

// FR-020 (Network in/out) deferred until heartbeats carry per-interface byte rates.
export default memo(function SmallMultiples({ perDeviceSeries, fleetTimeSeries }: SmallMultiplesProps) {
  return (
    <div
      className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4"
      data-testid="small-multiples"
      role="region"
      aria-label="Fleet time-series charts"
    >
      <div className="bg-gray-900/60 border border-gray-800 rounded-xl p-3">
        <PerDeviceChart
          title="CPU % per device"
          series={perDeviceSeries}
          dataKey="cpuPoints"
          data-testid="chart-cpu-per-device"
        />
      </div>
      <div className="bg-gray-900/60 border border-gray-800 rounded-xl p-3">
        <PerDeviceChart
          title="Memory % per device"
          series={perDeviceSeries}
          dataKey="memPoints"
          data-testid="chart-mem-per-device"
        />
      </div>
      <div className="bg-gray-900/60 border border-gray-800 rounded-xl p-3">
        <FleetStackedChart
          title="Disk I/O read/write"
          series1={fleetTimeSeries.diskRead}
          series2={fleetTimeSeries.diskWrite}
          label1="Read"
          label2="Write"
          color1="#f59e0b"
          color2="#ef4444"
          data-testid="chart-disk-io"
        />
      </div>
    </div>
  )
})

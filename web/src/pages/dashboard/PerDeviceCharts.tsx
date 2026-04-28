import { memo, useMemo } from 'react'
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
import type { DeviceSeries, ChartPoint } from '../../hooks/useFleetMetrics'
import { formatPercent, formatBytesPerSec, formatBytesPerSecAxis } from '../../utils/format'

const METRICS = [
  { key: 'cpu', label: 'CPU',  color: '#3b82f6' },
  { key: 'mem', label: 'RAM',  color: '#10b981' },
  { key: 'disk', label: 'Disk', color: '#f59e0b' },
  { key: 'load', label: 'Load', color: '#a855f7' },
] as const

const NET_METRICS = [
  { key: 'rx', label: 'In',  color: '#06b6d4' },
  { key: 'tx', label: 'Out', color: '#f97316' },
] as const

interface MergedRow {
  ts: string
  cpu: number | null
  mem: number | null
  disk: number | null
  load: number | null
}

interface MergedNetRow {
  ts: string
  rx: number | null
  tx: number | null
}

function mergeSeries(s: DeviceSeries): MergedRow[] {
  const tsSet = new Set<string>()
  for (const p of s.cpuPoints) tsSet.add(p.timestamp)
  for (const p of s.memPoints) tsSet.add(p.timestamp)
  for (const p of s.diskPoints) tsSet.add(p.timestamp)
  for (const p of s.loadPoints) tsSet.add(p.timestamp)

  const cpuMap = new Map(s.cpuPoints.map(p => [p.timestamp, p.value]))
  const memMap = new Map(s.memPoints.map(p => [p.timestamp, p.value]))
  const diskMap = new Map(s.diskPoints.map(p => [p.timestamp, p.value]))
  const loadMap = new Map(s.loadPoints.map(p => [p.timestamp, p.value]))

  return [...tsSet].sort().map(ts => ({
    ts,
    cpu: cpuMap.get(ts) ?? null,
    mem: memMap.get(ts) ?? null,
    disk: diskMap.get(ts) ?? null,
    load: loadMap.get(ts) ?? null,
  }))
}

function mergeNetSeries(rxPoints: ChartPoint[], txPoints: ChartPoint[]): MergedNetRow[] {
  const tsSet = new Set<string>()
  for (const p of rxPoints) tsSet.add(p.timestamp)
  for (const p of txPoints) tsSet.add(p.timestamp)

  const rxMap = new Map(rxPoints.map(p => [p.timestamp, p.value]))
  const txMap = new Map(txPoints.map(p => [p.timestamp, p.value]))

  return [...tsSet].sort().map(ts => ({
    ts,
    rx: rxMap.get(ts) ?? null,
    tx: txMap.get(ts) ?? null,
  }))
}

interface DeviceChartCardProps {
  series: DeviceSeries
}

const DeviceChartCard = memo(function DeviceChartCard({ series }: DeviceChartCardProps) {
  const data = useMemo(() => mergeSeries(series), [series])
  const netData = useMemo(() => mergeNetSeries(series.netRxPoints, series.netTxPoints), [series.netRxPoints, series.netTxPoints])
  const last = data[data.length - 1]
  // Gradient IDs: percent chart uses "grad-<id>-<key>", net chart uses "grad-net-<id>-<key>"
  // to avoid <defs> collisions between the two charts within the same card.
  const gradId = (key: string) => `grad-${series.deviceId.replace(/[^a-z0-9]/gi, '_')}-${key}`
  const netGradId = (key: string) => `grad-net-${series.deviceId.replace(/[^a-z0-9]/gi, '_')}-${key}`

  return (
    <div
      className="bg-gray-900/60 border border-gray-800 rounded-xl p-4"
      data-testid={`device-chart-${series.deviceId}`}
    >
      <div className="flex items-baseline justify-between mb-3">
        <h3 className="text-sm font-semibold text-white truncate">{series.hostname}</h3>
        {last && (
          <div className="flex items-center gap-3 text-[11px] tabular-nums">
            {METRICS.map(m => {
              const v = last[m.key]
              return (
                <span key={m.key} className="flex items-center gap-1" style={{ color: m.color }}>
                  <span className="inline-block w-1.5 h-1.5 rounded-full" style={{ backgroundColor: m.color }} aria-hidden="true" />
                  <span className="text-gray-400">{m.label}</span>
                  <span className="text-gray-200">{v == null ? '—' : formatPercent(v)}</span>
                </span>
              )
            })}
          </div>
        )}
      </div>

      <ResponsiveContainer width="100%" height={220}>
        <AreaChart data={data} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
          <defs>
            {METRICS.map(m => (
              <linearGradient key={m.key} id={gradId(m.key)} x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={m.color} stopOpacity={0.35} />
                <stop offset="95%" stopColor={m.color} stopOpacity={0} />
              </linearGradient>
            ))}
          </defs>
          <CartesianGrid strokeDasharray="2 4" stroke="#1f2937" vertical={false} />
          <XAxis dataKey="ts" hide />
          <YAxis
            domain={[0, 100]}
            ticks={[0, 25, 50, 75, 100]}
            tick={{ fontSize: 10, fill: '#6b7280' }}
            width={32}
            tickFormatter={v => `${v}%`}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip
            formatter={(value: number | undefined, key: string | undefined) => {
              const m = METRICS.find(m => m.key === key)
              return [value == null ? '—' : formatPercent(value), m?.label ?? (key ?? '')]
            }}
            contentStyle={{ backgroundColor: '#111827', border: '1px solid #374151', borderRadius: 6 }}
            labelStyle={{ display: 'none' }}
          />
          <Legend
            wrapperStyle={{ paddingTop: 4 }}
            formatter={(v: string) => {
              const m = METRICS.find(m => m.key === v)
              return <span style={{ fontSize: 10, color: '#9ca3af' }}>{m?.label ?? v}</span>
            }}
          />
          {METRICS.map(m => (
            <Area
              key={m.key}
              type="monotone"
              dataKey={m.key}
              name={m.key}
              stroke={m.color}
              strokeWidth={1}
              fill={`url(#${gradId(m.key)})`}
              dot={false}
              connectNulls={false}
              isAnimationActive={false}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>

      {/* Network in/out sub-chart — bytes/sec Y-axis (FLEET-NET) */}
      <div className="mt-1" data-testid={`device-net-chart-${series.deviceId}`}>
        <div className="flex items-center justify-between mb-1">
          <span className="text-[10px] text-gray-500">Network (B/s)</span>
        </div>
        <ResponsiveContainer width="100%" height={100}>
          <AreaChart data={netData} margin={{ top: 4, right: 8, bottom: 0, left: 0 }}>
            <defs>
              {NET_METRICS.map(m => (
                <linearGradient key={m.key} id={netGradId(m.key)} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor={m.color} stopOpacity={0.35} />
                  <stop offset="95%" stopColor={m.color} stopOpacity={0} />
                </linearGradient>
              ))}
            </defs>
            <CartesianGrid strokeDasharray="2 4" stroke="#1f2937" vertical={false} />
            <XAxis dataKey="ts" hide />
            <YAxis
              tick={{ fontSize: 9, fill: '#6b7280' }}
              width={32}
              tickFormatter={formatBytesPerSecAxis}
              axisLine={false}
              tickLine={false}
            />
            <Tooltip
              formatter={(value: number | undefined, key: string | undefined) => {
                const m = NET_METRICS.find(m => m.key === key)
                return [value == null ? '—' : formatBytesPerSec(value), m?.label ?? (key ?? '')]
              }}
              contentStyle={{ backgroundColor: '#111827', border: '1px solid #374151', borderRadius: 6 }}
              labelStyle={{ display: 'none' }}
            />
            <Legend
              wrapperStyle={{ paddingTop: 2 }}
              formatter={(v: string) => {
                const m = NET_METRICS.find(m => m.key === v)
                return <span style={{ fontSize: 10, color: '#9ca3af' }}>{m?.label ?? v}</span>
              }}
            />
            {NET_METRICS.map(m => (
              <Area
                key={m.key}
                type="monotone"
                dataKey={m.key}
                name={m.key}
                stroke={m.color}
                strokeWidth={1}
                fill={`url(#${netGradId(m.key)})`}
                dot={false}
                connectNulls={false}
                isAnimationActive={false}
              />
            ))}
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
})

interface PerDeviceChartsProps {
  series: DeviceSeries[]
}

export default memo(function PerDeviceCharts({ series }: PerDeviceChartsProps) {
  if (series.length === 0) {
    return (
      <div
        className="bg-gray-900/60 border border-gray-800 rounded-xl p-8 text-center"
        data-testid="per-device-charts-empty"
      >
        <p className="text-gray-400 mb-1">No devices reporting</p>
        <p className="text-sm text-gray-600">Per-device charts appear here once agents start sending heartbeats.</p>
      </div>
    )
  }

  const sorted = [...series].sort((a, b) => a.hostname.localeCompare(b.hostname))

  return (
    <div
      className="grid grid-cols-1 md:grid-cols-2 gap-4"
      data-testid="per-device-charts"
      role="region"
      aria-label="Per-device performance charts"
    >
      {sorted.map(s => (
        <DeviceChartCard key={s.deviceId} series={s} />
      ))}
    </div>
  )
})

import { useMemo } from 'react'
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import type { Heartbeat } from '../types/models'
import { chartColors } from '../theme/chartColors'
import { useTimeFormatters } from '../contexts/TimePreferenceProvider'
import { formatBytesPerSec } from '../utils/format'

interface Props {
  heartbeats: Heartbeat[]
  metricKey: keyof Heartbeat['data']
  label: string
  color: string
  maxY?: number
  unit?: string
  subtitle?: string
  valueTransform?: (v: number) => number
}

export default function MetricChart({ heartbeats, metricKey, label, color, maxY, unit = '%', subtitle, valueTransform }: Props) {
  const { chartTick, chartTooltip } = useTimeFormatters()
  const data = useMemo(() =>
    heartbeats.map(hb => {
      const raw = Number(hb.data[metricKey]) || 0
      return {
        time: new Date(hb.timestamp).getTime(),
        value: valueTransform ? valueTransform(raw) : raw,
      }
    }),
    [heartbeats, metricKey, valueTransform],
  )

  const computedMaxY = useMemo(() => {
    if (maxY !== undefined) return maxY
    if (unit === '%') return 100
    // Auto-scale: find max value and round up
    const maxVal = data.reduce((m, d) => Math.max(m, d.value), 0)
    if (maxVal === 0) return 1
    // Round up to a nice number
    const magnitude = Math.pow(10, Math.floor(Math.log10(maxVal)))
    return Math.ceil(maxVal / magnitude) * magnitude
  }, [maxY, unit, data])

  if (data.length === 0) {
    return (
      <div className="h-48 flex items-center justify-center text-sm text-gray-600">
        No data
      </div>
    )
  }

  const formatValue = (v: number): string => {
    if (unit === 'bytes/s') return formatBytesPerSec(v)
    if (unit === '%') return `${v.toFixed(1)}%`
    return `${v.toFixed(1)}${unit}`
  }

  const formatTick = (v: number): string => {
    if (unit === 'bytes/s') {
      if (computedMaxY >= 1073741824) return `${(v / 1073741824).toFixed(0)}G`
      if (computedMaxY >= 1048576) return `${(v / 1048576).toFixed(0)}M`
      if (computedMaxY >= 1024) return `${(v / 1024).toFixed(0)}K`
      return `${v.toFixed(0)}`
    }
    if (unit === '%') return `${v}%`
    return `${v}`
  }

  return (
    <div>
      <p className="text-xs font-medium text-gray-400 uppercase tracking-wider mb-2">
        {label}
        {subtitle && <span className="ml-1.5 normal-case font-normal text-gray-600">{subtitle}</span>}
      </p>
      <ResponsiveContainer width="100%" height={180}>
        <AreaChart data={data}>
          <defs>
            <linearGradient id={`grad-${metricKey}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor={color} stopOpacity={0.3} />
              <stop offset="95%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          <XAxis
            dataKey="time"
            type="number"
            domain={['dataMin', 'dataMax']}
            tickFormatter={chartTick}
            tick={{ fill: chartColors.axisText, fontSize: 10 }}
            stroke={chartColors.axisLine}
          />
          <YAxis
            domain={[0, computedMaxY]}
            tick={{ fill: chartColors.axisText, fontSize: 10 }}
            stroke={chartColors.axisLine}
            width={40}
            tickFormatter={formatTick}
          />
          <Tooltip
            contentStyle={{ backgroundColor: chartColors.tooltipBg, border: `1px solid ${chartColors.tooltipBorder}`, borderRadius: 6, fontSize: 12 }}
            labelFormatter={ts => chartTooltip(Number(ts))}
            formatter={(value: number | undefined) => [formatValue(value ?? 0), label]}
          />
          <Area
            type="monotone"
            dataKey="value"
            stroke={color}
            fill={`url(#grad-${metricKey})`}
            strokeWidth={1.5}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}

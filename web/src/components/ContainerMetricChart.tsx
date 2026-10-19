import { useMemo } from 'react'
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import type { ContainerMetric } from '../types/models'
import { chartColors } from '../theme/chartColors'
import { useTimeFormatters } from '../contexts/TimePreferenceProvider'

interface Props {
  metrics: ContainerMetric[]
  mode: 'cpu' | 'memory'
  label: string
  color: string
}

export default function ContainerMetricChart({ metrics, mode, label, color }: Props) {
  const { chartTick, chartTooltip } = useTimeFormatters()
  const data = useMemo(() =>
    metrics.map(m => ({
      time: new Date(m.timestamp).getTime(),
      value: mode === 'cpu'
        ? m.cpu_percent
        : (m.mem_limit > 0 ? (m.mem_usage / m.mem_limit) * 100 : 0),
    })),
    [metrics, mode],
  )

  if (data.length === 0) {
    return (
      <div className="h-48 flex items-center justify-center text-sm text-gray-600">
        No data
      </div>
    )
  }

  const gradientId = `grad-container-${mode}`

  return (
    <div>
      <p className="text-xs font-medium text-gray-400 uppercase tracking-wider mb-2">{label}</p>
      <ResponsiveContainer width="100%" height={180}>
        <AreaChart data={data}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
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
            domain={[0, 100]}
            tick={{ fill: chartColors.axisText, fontSize: 10 }}
            stroke={chartColors.axisLine}
            width={35}
            tickFormatter={v => `${v}%`}
          />
          <Tooltip
            contentStyle={{ backgroundColor: chartColors.tooltipBg, border: `1px solid ${chartColors.tooltipBorder}`, borderRadius: 6, fontSize: 12 }}
            labelFormatter={ts => chartTooltip(Number(ts))}
            formatter={(value: number | undefined) => [`${(value ?? 0).toFixed(1)}%`, label]}
          />
          <Area
            type="monotone"
            dataKey="value"
            stroke={color}
            fill={`url(#${gradientId})`}
            strokeWidth={1.5}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}

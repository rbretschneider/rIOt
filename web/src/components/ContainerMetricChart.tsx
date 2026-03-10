import { useMemo } from 'react'
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import type { ContainerMetric } from '../types/models'

interface Props {
  metrics: ContainerMetric[]
  mode: 'cpu' | 'memory'
  label: string
  color: string
}

export default function ContainerMetricChart({ metrics, mode, label, color }: Props) {
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
            tickFormatter={ts => {
              const d = new Date(ts)
              return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`
            }}
            tick={{ fill: '#6b7280', fontSize: 10 }}
            stroke="#374151"
          />
          <YAxis
            domain={[0, 100]}
            tick={{ fill: '#6b7280', fontSize: 10 }}
            stroke="#374151"
            width={35}
            tickFormatter={v => `${v}%`}
          />
          <Tooltip
            contentStyle={{ backgroundColor: '#1f2937', border: '1px solid #374151', borderRadius: 6, fontSize: 12 }}
            labelFormatter={ts => new Date(ts).toLocaleString()}
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

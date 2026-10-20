import {
  Area,
  AreaChart,
  ResponsiveContainer,
  XAxis,
  YAxis,
  Tooltip,
} from 'recharts'
import type { ContainerMetric } from '../../types/models'
import { useTimeFormatters } from '../../contexts/TimePreferenceProvider'
import { formatBytes } from '../../utils/docker'
import { chartColors } from '../../theme/chartColors'
export default function IOMetricChart({
  metrics,
  field,
  label,
}: {
  metrics: ContainerMetric[]
  field: 'net_rx_rate' | 'net_tx_rate' | 'blk_read_rate' | 'blk_write_rate'
  label: string
}) {
  const { chartTick, chartTooltip } = useTimeFormatters()
  const data = metrics
    .filter((m) => typeof m[field] === 'number')
    .map((m) => ({ time: Date.parse(m.timestamp), value: m[field] }))
  if (!data.length)
    return (
      <p className="text-sm text-text-muted">{label}: history unavailable</p>
    )
  return (
    <section>
      <h3 className="text-sm text-text-muted">{label}</h3>
      <ResponsiveContainer width="100%" height={180}>
        <AreaChart data={data}>
          <XAxis
            dataKey="time"
            type="number"
            domain={['dataMin', 'dataMax']}
            tickFormatter={chartTick}
            stroke={chartColors.axisLine}
          />
          <YAxis
            tickFormatter={(n) => formatBytes(Number(n))}
            width={80}
            stroke={chartColors.axisLine}
          />
          <Tooltip
            labelFormatter={(n) => chartTooltip(Number(n))}
            formatter={(n) => [`${formatBytes(Number(n))}/s`, label]}
            contentStyle={{
              background: chartColors.tooltipBg,
              border: `1px solid ${chartColors.tooltipBorder}`,
            }}
          />
          <Area
            type="monotone"
            dataKey="value"
            stroke={chartColors.series.cpu}
            fill={chartColors.series.cpu}
            fillOpacity={0.2}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </section>
  )
}

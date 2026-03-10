interface Props {
  label: string
  value: number
  max?: number
  unit?: string
  invert?: boolean
}

export default function GaugeBar({ label, value, max = 100, unit = '%', invert }: Props) {
  const pct = Math.min((value / max) * 100, 100)
  const color = invert
    ? (pct < 10 ? 'bg-red-500' : pct < 25 ? 'bg-amber-500' : 'bg-emerald-500')
    : (pct > 90 ? 'bg-red-500' : pct > 75 ? 'bg-amber-500' : 'bg-emerald-500')

  return (
    <div className="space-y-1">
      <div className="flex justify-between text-sm">
        <span className="text-gray-400">{label}</span>
        <span className="text-gray-200 font-mono">{value.toFixed(1)}{unit}</span>
      </div>
      <div className="h-2 bg-gray-800 rounded-full overflow-hidden">
        <div className={`h-full ${color} rounded-full transition-all duration-500`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

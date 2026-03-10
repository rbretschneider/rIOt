interface Props {
  label: string
  value: number
}

const SEGMENTS = 10

export default function BatteryGauge({ label, value }: Props) {
  const pct = Math.min(Math.max(value, 0), 100)
  const filled = Math.round((pct / 100) * SEGMENTS)
  const color = pct < 10 ? 'bg-red-500' : pct < 25 ? 'bg-amber-500' : 'bg-emerald-500'

  return (
    <div className="space-y-1">
      <div className="flex justify-between text-sm">
        <span className="text-gray-400">{label}</span>
        <span className="text-gray-200 font-mono">{value.toFixed(1)}%</span>
      </div>
      <div className="flex items-center gap-0.5">
        <div className="flex gap-[3px] flex-1">
          {Array.from({ length: SEGMENTS }, (_, i) => (
            <div
              key={i}
              className={`h-3 flex-1 rounded-sm ${i < filled ? color : 'bg-gray-800'}`}
            />
          ))}
        </div>
        <div className="w-1 h-2 bg-gray-700 rounded-r-sm ml-0.5" />
      </div>
    </div>
  )
}

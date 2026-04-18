// Shared card-style section primitives used on device and container detail
// pages. Kept together so the two detail views stay visually consistent — a
// Section is always the same gray-900 card with an uppercase header.

export function Section({ title, children, className }: {
  title: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <div className={`bg-gray-900 border border-gray-800 rounded-lg p-5 ${className ?? ''}`}>
      <h2 className="text-sm font-semibold text-gray-300 uppercase tracking-wider mb-4">{title}</h2>
      {children}
    </div>
  )
}

export function InfoItem({ label, value, valueClassName }: {
  label: string
  value: string
  valueClassName?: string
}) {
  return (
    <div>
      <p className="text-xs text-gray-500">{label}</p>
      <p className={`text-sm ${valueClassName || 'text-gray-200'}`}>{value}</p>
    </div>
  )
}

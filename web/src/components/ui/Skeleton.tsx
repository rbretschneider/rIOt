const PULSE = 'animate-pulse motion-reduce:animate-none'

export interface SkeletonProps {
  className?: string
  /** Renders this many stacked bars instead of one block. */
  lines?: number
}

/** Loading placeholder. Hidden from assistive technology; pair it with a status region. */
export function Skeleton({ className = '', lines = 1 }: SkeletonProps) {
  if (lines <= 1) {
    return (
      <div aria-hidden="true" className={`h-4 rounded bg-raised-2 ${PULSE} ${className}`.trim()} />
    )
  }
  return (
    <div aria-hidden="true" className="space-y-2">
      {Array.from({ length: lines }, (_, i) => (
        <div
          key={i}
          className={`h-4 rounded bg-raised-2 ${PULSE} ${i === lines - 1 ? 'w-2/3' : 'w-full'} ${className}`.trim()}
        />
      ))}
    </div>
  )
}

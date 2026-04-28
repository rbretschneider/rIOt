import { useEffect, useRef, useState } from 'react'
import Sparkline from '../../components/Sparkline'

export interface KPITileProps {
  label: string
  value: string | number
  /** Optional sub-value or second metric line */
  subtext?: string
  /** Positive number = increased; negative = decreased; 0 = no change */
  delta?: number
  /** 60-minute sparkline data points */
  sparklineData?: number[]
  /** Sparkline color override */
  sparklineColor?: string
  flash?: boolean
  'data-testid'?: string
}

/** Color and glyph maps for delta direction (AC-009, AC-034) */
function DeltaIndicator({ delta }: { delta: number }) {
  if (delta > 0) {
    return (
      <span className="flex items-center gap-0.5 text-xs text-red-400" aria-label={`Up ${delta.toFixed(1)}`}>
        {/* Up arrow glyph */}
        <svg xmlns="http://www.w3.org/2000/svg" className="h-3 w-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path fillRule="evenodd" d="M3.293 9.707a1 1 0 010-1.414l6-6a1 1 0 011.414 0l6 6a1 1 0 01-1.414 1.414L11 5.414V17a1 1 0 11-2 0V5.414L4.707 9.707a1 1 0 01-1.414 0z" clipRule="evenodd" />
        </svg>
        +{delta.toFixed(1)}
      </span>
    )
  }
  if (delta < 0) {
    return (
      <span className="flex items-center gap-0.5 text-xs text-emerald-400" aria-label={`Down ${Math.abs(delta).toFixed(1)}`}>
        {/* Down arrow glyph */}
        <svg xmlns="http://www.w3.org/2000/svg" className="h-3 w-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
          <path fillRule="evenodd" d="M16.707 10.293a1 1 0 010 1.414l-6 6a1 1 0 01-1.414 0l-6-6a1 1 0 111.414-1.414L9 14.586V3a1 1 0 012 0v11.586l4.293-4.293a1 1 0 011.414 0z" clipRule="evenodd" />
        </svg>
        {delta.toFixed(1)}
      </span>
    )
  }
  return (
    <span className="flex items-center gap-0.5 text-xs text-gray-500" aria-label="No change">
      <svg xmlns="http://www.w3.org/2000/svg" className="h-3 w-3" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
        <path fillRule="evenodd" d="M3 10a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1z" clipRule="evenodd" />
      </svg>
      0
    </span>
  )
}

/**
 * KPITile renders one tile in the fleet KPI strip (FR-010, FR-011, FR-012).
 *
 * - Primary numeric value (large)
 * - Delta indicator with color + glyph (AC-009, AC-034)
 * - 60-minute background sparkline (BR-001)
 * - Value flash on change (FR-012 / AC-007)
 */
export default function KPITile({
  label,
  value,
  subtext,
  delta,
  sparklineData = [],
  sparklineColor = 'rgba(16,185,129,0.6)',
  flash,
  'data-testid': testId,
}: KPITileProps) {
  const [flashing, setFlashing] = useState(false)
  const prevFlashRef = useRef(flash)

  useEffect(() => {
    if (flash && !prevFlashRef.current) {
      setFlashing(true)
      const t = setTimeout(() => setFlashing(false), 300)
      return () => clearTimeout(t)
    }
    prevFlashRef.current = flash
  }, [flash])

  return (
    <div
      className="relative bg-gray-900/60 border border-gray-800 rounded-xl p-3 overflow-hidden"
      data-testid={testId ?? `kpi-tile-${label.toLowerCase().replace(/\s+/g, '-')}`}
    >
      {/* Background sparkline */}
      {sparklineData.length > 1 && (
        <div className="absolute inset-0 opacity-20 pointer-events-none" aria-hidden="true">
          <Sparkline data={sparklineData} color={sparklineColor} height={80} />
        </div>
      )}

      <div className="relative z-10">
        <p className="text-xs text-gray-400 truncate mb-1">{label}</p>
        <p
          className={`text-2xl font-bold text-white tabular-nums transition-colors duration-300 ${flashing ? 'text-amber-300' : ''}`}
          aria-label={`${label}: ${value}`}
        >
          {typeof value === 'number' ? value.toFixed(value < 10 ? 1 : 0) : value}
        </p>
        {subtext && (
          <p className="text-xs text-gray-500 mt-0.5 truncate">{subtext}</p>
        )}
        {delta !== undefined && (
          <div className="mt-1">
            <DeltaIndicator delta={delta} />
          </div>
        )}
      </div>
    </div>
  )
}

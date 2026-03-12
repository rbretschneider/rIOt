import type { SecurityScoreResult } from '../types/models'

interface Props {
  score: SecurityScoreResult
  onClick: () => void
  size?: number
}

function scoreColor(grade: string): { stroke: string; text: string } {
  switch (grade) {
    case 'A': return { stroke: 'stroke-emerald-500', text: 'text-emerald-400' }
    case 'B': return { stroke: 'stroke-blue-500', text: 'text-blue-400' }
    case 'C': return { stroke: 'stroke-amber-500', text: 'text-amber-400' }
    case 'D': return { stroke: 'stroke-orange-500', text: 'text-orange-400' }
    default:  return { stroke: 'stroke-red-500', text: 'text-red-400' }
  }
}

export default function SecurityScoreGauge({ score, onClick, size = 64 }: Props) {
  const radius = 26
  const circumference = 2 * Math.PI * radius
  const pct = Math.max(0, Math.min(100, score.overall_score)) / 100
  const offset = circumference * (1 - pct)
  const { stroke, text } = scoreColor(score.grade)

  return (
    <button
      onClick={onClick}
      className="relative flex-shrink-0 group cursor-pointer"
      title={`Security Score: ${score.overall_score}/100 (${score.grade})\nClick for details`}
      style={{ width: size, height: size }}
    >
      <svg viewBox="0 0 64 64" className="w-full h-full -rotate-90">
        {/* Background circle */}
        <circle
          cx="32" cy="32" r={radius}
          fill="none"
          className="stroke-gray-700"
          strokeWidth="5"
        />
        {/* Score arc */}
        <circle
          cx="32" cy="32" r={radius}
          fill="none"
          className={`${stroke} transition-all duration-700 ease-out`}
          strokeWidth="5"
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
        />
      </svg>
      {/* Center text */}
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className={`text-base font-bold leading-none ${text}`}>
          {score.overall_score}
        </span>
        <span className="text-[9px] text-gray-500 uppercase tracking-wider leading-tight">
          {score.grade}
        </span>
      </div>
      {/* Hover ring */}
      <div className="absolute inset-0 rounded-full border border-transparent group-hover:border-gray-600 transition-colors" />
    </button>
  )
}

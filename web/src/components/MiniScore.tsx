import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { gradeColor, gradeStrokeColor } from '../utils/security'
import type { SecurityScoreResult } from '../types/models'

interface Props {
  deviceId: string
  onShowModal: (score: SecurityScoreResult) => void
}

export default function MiniScore({ deviceId, onShowModal }: Props) {
  const { data: score } = useQuery({
    queryKey: ['security-score', deviceId],
    queryFn: () => api.getSecurityScore(deviceId),
    staleTime: 5 * 60_000,
  })

  if (!score) return <span className="text-gray-700">-</span>

  const r = 10
  const circ = 2 * Math.PI * r
  const offset = circ * (1 - Math.max(0, Math.min(100, score.overall_score)) / 100)

  return (
    <button
      onClick={() => onShowModal(score)}
      className="inline-flex items-center gap-1 group cursor-pointer"
      title={`Security: ${score.overall_score}/100 (${score.grade})`}
    >
      <svg viewBox="0 0 24 24" className="w-6 h-6 -rotate-90 flex-shrink-0">
        <circle cx="12" cy="12" r={r} fill="none" className="stroke-gray-700" strokeWidth="3" />
        <circle
          cx="12" cy="12" r={r}
          fill="none"
          className={`${gradeStrokeColor(score.grade)} transition-all duration-500`}
          strokeWidth="3"
          strokeLinecap="round"
          strokeDasharray={circ}
          strokeDashoffset={offset}
        />
      </svg>
      <span className={`text-xs font-semibold ${gradeColor(score.grade)} group-hover:brightness-125 transition`}>
        {score.overall_score}
      </span>
    </button>
  )
}

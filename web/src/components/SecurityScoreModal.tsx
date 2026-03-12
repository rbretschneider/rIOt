import { useState } from 'react'
import type { SecurityScoreResult, SecurityCategoryScore, SecurityFinding } from '../types/models'

// Map finding IDs to the command action that can fix them
const fixableActions: Record<string, { action: string; params?: Record<string, unknown>; label: string; confirm: string }> = {
  'auto-updates': {
    action: 'enable_auto_updates',
    label: 'Enable',
    confirm: 'This will install and enable unattended-upgrades (Debian/Ubuntu) or dnf-automatic (RHEL/Fedora) on this device.',
  },
  'no-security-updates': {
    action: 'os_update',
    params: { mode: 'security' },
    label: 'Patch Now',
    confirm: 'This will install pending security updates on this device.',
  },
  'pending-updates-low': {
    action: 'os_update',
    params: { mode: 'full' },
    label: 'Update All',
    confirm: 'This will run a full system update on this device.',
  },
}

interface Props {
  score: SecurityScoreResult
  onClose: () => void
  onRunCommand?: (action: string, params?: Record<string, unknown>) => void
  canCommand?: boolean
}

function severityBadge(severity: SecurityFinding['severity']) {
  const styles: Record<string, string> = {
    critical: 'bg-red-500/20 text-red-400 border-red-500/30',
    warning:  'bg-amber-500/20 text-amber-400 border-amber-500/30',
    info:     'bg-blue-500/20 text-blue-400 border-blue-500/30',
    pass:     'bg-emerald-500/20 text-emerald-400 border-emerald-500/30',
  }
  return (
    <span className={`px-1.5 py-0.5 text-[10px] font-medium uppercase rounded border ${styles[severity] || styles.info}`}>
      {severity}
    </span>
  )
}

function gradeColor(grade: string): string {
  switch (grade) {
    case 'A': return 'text-emerald-400'
    case 'B': return 'text-blue-400'
    case 'C': return 'text-amber-400'
    case 'D': return 'text-orange-400'
    default:  return 'text-red-400'
  }
}

function barColor(score: number, max: number): string {
  if (max === 0) return 'bg-gray-600'
  const pct = score / max
  if (pct >= 0.9) return 'bg-emerald-500'
  if (pct >= 0.7) return 'bg-blue-500'
  if (pct >= 0.5) return 'bg-amber-500'
  return 'bg-red-500'
}

function FixButton({ finding, onRunCommand }: { finding: SecurityFinding; onRunCommand: (action: string, params?: Record<string, unknown>) => void }) {
  const [confirming, setConfirming] = useState(false)
  const [sent, setSent] = useState(false)
  const fix = fixableActions[finding.id]
  if (!fix) return null

  if (sent) {
    return <span className="text-[10px] text-emerald-400 font-medium whitespace-nowrap">Sent</span>
  }

  if (confirming) {
    return (
      <span className="flex items-center gap-1.5 flex-shrink-0">
        <span className="text-[10px] text-gray-400 max-w-[200px]">{fix.confirm}</span>
        <button
          onClick={() => { onRunCommand(fix.action, fix.params); setSent(true) }}
          className="px-2 py-0.5 text-[10px] font-medium bg-emerald-600 hover:bg-emerald-500 text-white rounded transition-colors whitespace-nowrap cursor-pointer"
        >
          Yes
        </button>
        <button
          onClick={() => setConfirming(false)}
          className="px-2 py-0.5 text-[10px] text-gray-400 hover:text-white transition-colors cursor-pointer"
        >
          No
        </button>
      </span>
    )
  }

  return (
    <button
      onClick={() => setConfirming(true)}
      className="px-2 py-0.5 text-[10px] font-medium text-emerald-400 hover:text-emerald-300 border border-emerald-700/50 hover:border-emerald-600/50 rounded transition-colors whitespace-nowrap flex-shrink-0 cursor-pointer"
    >
      {fix.label}
    </button>
  )
}

function CategorySection({ cat, onRunCommand, canCommand }: { cat: SecurityCategoryScore; onRunCommand?: (action: string, params?: Record<string, unknown>) => void; canCommand?: boolean }) {
  const [expanded, setExpanded] = useState(true)
  const pct = cat.max_score > 0 ? Math.round((cat.score / cat.max_score) * 100) : 0

  // Sort: failures first, then passes
  const sorted = [...cat.findings].sort((a, b) => {
    if (a.passed === b.passed) return 0
    return a.passed ? 1 : -1
  })

  return (
    <div className="border border-gray-700/50 rounded-lg overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-3 px-4 py-3 hover:bg-gray-800/50 transition-colors cursor-pointer"
      >
        <svg
          className={`w-3.5 h-3.5 text-gray-500 transition-transform flex-shrink-0 ${expanded ? 'rotate-90' : ''}`}
          fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}
        >
          <path strokeLinecap="round" strokeLinejoin="round" d="M9 5l7 7-7 7" />
        </svg>
        <span className="text-sm font-medium text-white flex-1 text-left">{cat.label}</span>
        <div className="flex items-center gap-2 flex-shrink-0">
          <div className="w-24 h-1.5 bg-gray-700 rounded-full overflow-hidden">
            <div className={`h-full rounded-full ${barColor(cat.score, cat.max_score)}`} style={{ width: `${pct}%` }} />
          </div>
          <span className="text-xs text-gray-400 w-8 text-right">{pct}%</span>
        </div>
      </button>

      {expanded && (
        <div className="border-t border-gray-700/50">
          {sorted.map(f => (
            <div key={f.id} className={`px-4 py-2.5 flex gap-3 items-start ${f.passed ? 'opacity-60' : ''} border-b border-gray-800/50 last:border-b-0`}>
              <span className="mt-0.5 flex-shrink-0">
                {f.passed ? (
                  <svg className="w-4 h-4 text-emerald-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
                    <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                  </svg>
                ) : (
                  <svg className="w-4 h-4 text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
                    <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
                  </svg>
                )}
              </span>
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-0.5">
                  <span className="text-sm text-white">{f.title}</span>
                  {!f.passed && severityBadge(f.severity)}
                </div>
                <p className="text-xs text-gray-400">{f.description}</p>
                {!f.passed && f.remediation && (
                  <div className="flex items-center gap-2 mt-1">
                    <p className="text-xs text-gray-500 italic">{f.remediation}</p>
                    {canCommand && fixableActions[f.id] && (
                      <FixButton finding={f} onRunCommand={onRunCommand!} />
                    )}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default function SecurityScoreModal({ score, onClose, onRunCommand, canCommand }: Props) {
  const failCount = score.categories.reduce(
    (acc, cat) => acc + cat.findings.filter(f => !f.passed).length, 0
  )
  const passCount = score.categories.reduce(
    (acc, cat) => acc + cat.findings.filter(f => f.passed).length, 0
  )

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]" onClick={onClose}>
      <div
        className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-2xl mx-4 max-h-[85vh] flex flex-col"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-700/50">
          <div className="flex items-center gap-4">
            <div className="text-center">
              <span className={`text-3xl font-bold ${gradeColor(score.grade)}`}>{score.overall_score}</span>
              <span className="text-lg text-gray-500">/100</span>
            </div>
            <div>
              <h3 className="text-lg font-semibold text-white">Security Score</h3>
              <p className="text-xs text-gray-500">
                {passCount} passed · {failCount} {failCount === 1 ? 'issue' : 'issues'} found
              </p>
            </div>
          </div>
          <button onClick={onClose} className="text-gray-500 hover:text-white transition-colors p-1">
            <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* Body */}
        <div className="overflow-y-auto flex-1 px-6 py-4 space-y-3">
          {score.categories.map(cat => (
            <CategorySection key={cat.category} cat={cat} onRunCommand={onRunCommand} canCommand={canCommand} />
          ))}
        </div>
      </div>
    </div>
  )
}

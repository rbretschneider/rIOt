import type { ExtraUpdater } from '../types/models'
import {
  extraName,
  isEnabled,
  isPending,
  noticeWords,
  notAppliedWords,
  statusWords,
} from '../utils/extraUpdaters'

// EXTRA-UPD-001 FR-054: each reported extra updater (DietPi first) shown next to the apt
// updates, with its installed and available versions, status, the unknown reason in words and
// every notice in words. React text only; nothing is rendered as HTML.

export default function ExtraUpdatersList({ extras }: { extras: ExtraUpdater[] | undefined }) {
  if (!extras || extras.length === 0) return null
  return (
    <div data-testid="extra-updaters" className="mb-3 max-h-48 overflow-y-auto scrollbar-thin">
      <p className="mb-1 text-xs uppercase text-gray-500">Other updaters</p>
      <ul className="divide-y divide-gray-800/50 text-sm">
        {extras.map(e => (
          <li key={e.id} data-testid={`extra-updater-${e.id}`} className="py-2">
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className="font-medium text-gray-200">{extraName(e.id)}</span>
              <span className="text-xs text-gray-400">
                Installed <span className="font-mono">{e.installed_version || 'unknown'}</span>
              </span>
              <span className="text-xs text-gray-400">
                Available <span className="font-mono">{e.available_version || '-'}</span>
              </span>
              <span className={`text-xs ${isPending(e) ? 'text-cyan-400' : e.status === 'unknown' ? 'text-amber-400' : 'text-gray-400'}`}>
                {statusWords(e)}
              </span>
            </div>
            {isPending(e) && !isEnabled(e) && (
              <p className="mt-1 text-xs text-gray-400">Will not be applied: {notAppliedWords(e.readiness)}.</p>
            )}
            {(e.notices?.length ?? 0) > 0 && (
              <ul className="mt-1 ml-4 list-disc space-y-0.5 text-xs text-gray-400">
                {e.notices!.map(n => (
                  <li key={`${n.code}:${n.owner ?? ''}:${n.branch ?? ''}`}>{noticeWords(n)}</li>
                ))}
              </ul>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}

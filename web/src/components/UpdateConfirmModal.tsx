import type { ContainerInfo } from '../types/models'
import { displayName } from '../utils/docker'

interface Props {
  title: string
  containers: ContainerInfo[]
  networkOrder?: { parent: string; dependents: string[] }
  onConfirm: () => void
  onCancel: () => void
}

export default function UpdateConfirmModal({ title, containers, networkOrder, onConfirm, onCancel }: Props) {
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]" onClick={onCancel}>
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-md mx-4 p-6" onClick={e => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-white mb-3">{title}</h3>

        <div className="mb-4 space-y-1.5 max-h-60 overflow-y-auto">
          {containers.map(c => (
            <div key={c.id} className="flex items-center gap-2 text-sm">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-400 flex-shrink-0" />
              <span className="text-white">{displayName(c.riot, c.name)}</span>
              <span className="text-gray-500 text-xs font-mono truncate">{c.image}</span>
            </div>
          ))}
        </div>

        {networkOrder && (
          <div className="mb-4 px-3 py-2 bg-blue-900/20 border border-blue-800/30 rounded text-xs text-blue-300 space-y-1">
            <p className="font-medium">Execution order:</p>
            <ol className="list-decimal list-inside space-y-0.5 text-blue-400/80">
              {networkOrder.dependents.length > 0 && (
                <li>Stop dependents: {networkOrder.dependents.join(', ')}</li>
              )}
              <li>Update parent: {networkOrder.parent}</li>
              {networkOrder.dependents.length > 0 && (
                <li>Update &amp; restart dependents: {networkOrder.dependents.join(', ')}</li>
              )}
            </ol>
          </div>
        )}

        <p className="text-xs text-amber-400/70 mb-4">Running services will be briefly interrupted.</p>

        <div className="flex justify-end gap-3">
          <button onClick={onCancel} className="px-4 py-2 text-sm text-gray-400 hover:text-white">
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className="px-4 py-2 text-sm text-white rounded-md bg-amber-600 hover:bg-amber-500 transition-colors"
          >
            Confirm Update
          </button>
        </div>
      </div>
    </div>
  )
}
